package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// contratoArgos fija el contrato que informó el conector de la prueba y lo devuelve al terminar.
func (b *bandeja) contratoArgos(v string) {
	b.t.Helper()
	var antes *string
	if err := b.bd.QueryRow(b.ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE id = $1`, b.conector).Scan(&antes); err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, b.conector, v); err != nil {
		b.t.Fatal(err)
	}
	b.t.Cleanup(func() {
		_, _ = b.bd.Exec(b.ctx, `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, b.conector, antes)
	})
}

// conCliente crea una conversación de un cliente de la instantánea (asociación automática y
// verificada) con un servicio suyo.
func (b *bandeja) conCliente() (conv string, cliente int64) {
	b.t.Helper()
	num := numeroCliente()
	cliente = siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Carlos Ruiz', $2, 'C-1', now(), '{1}')`, cliente, "+502 "+num[3:7]+"-"+num[7:]); err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 1, $2, 'S-9', 'Fibra 20 Mbps', 'activo', 'Zona 1', now())`, cliente*10, cliente); err != nil {
		b.t.Fatal(err)
	}
	b.texto(num, fmt.Sprintf("wamid.g%d", cliente), "Hola")
	return cid(b.conv(num)), cliente
}

type pendienteGestion struct {
	ruta, clave, actor string
	contacto           *string
	cuerpo             map[string]any
}

func (b *bandeja) pendienteDeGestion(id string) pendienteGestion {
	b.t.Helper()
	var p pendienteGestion
	var crudo []byte
	if err := b.bd.QueryRow(b.ctx, `SELECT p.ruta, p.idempotency_key, coalesce(p.actor, ''), p.contacto, p.payload FROM integracion.pendientes p
		JOIN mensajeria.gestiones_cobro g ON g.pendiente_id = p.id WHERE g.id = $1`, id).Scan(&p.ruta, &p.clave, &p.actor, &p.contacto, &crudo); err != nil {
		b.t.Fatal(err)
	}
	if err := json.Unmarshal(crudo, &p.cuerpo); err != nil {
		b.t.Fatal(err)
	}
	return p
}

// B31 (contrato Iris ↔ ArgosISP 0.13.0 §3.12): gestiones de cobro hacia Argos.
func TestGestionesCobro(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.contratoArgos("0.13.0")
	conv, cliente := b.conCliente()
	ruta := "/conversaciones/" + conv + "/gestiones-cobro"
	ahora := *b.reloj
	cuerpo := map[string]any{"tipo": "llamada", "resultado": "promete_pagar", "ocurridoEn": ahora.Add(-time.Hour), "servicioId": cliente * 10}

	r := b.apiClave(b.supervisor, http.MethodPost, ruta, "g-1", cuerpo)
	b.exigir(r, http.StatusCreated, "tipo", "llamada", "resultado", "promete_pagar", "envio", "pendiente")
	id := fmt.Sprint(r.cuerpo["id"])
	p := b.pendienteDeGestion(id)
	if p.ruta != fmt.Sprintf("/clientes/%d/gestiones-cobro", cliente) || p.clave != "iris:gestion:"+id ||
		p.actor != fmt.Sprintf("usuario:%d", b.supervisor.id) || p.contacto != nil {
		t.Fatalf("pendiente: %+v", p)
	}
	// Sin texto libre, montos ni teléfonos: solo los campos de §3.12.
	if len(p.cuerpo) != 6 || p.cuerpo["origen_tipo"] != "conversacion" || p.cuerpo["origen_id"] != "c-"+conv ||
		p.cuerpo["ocurrido_en"] != ahora.Add(-time.Hour).UTC().Format(time.RFC3339) || p.cuerpo["servicio_id"] != float64(cliente*10) {
		t.Fatalf("cuerpo: %v", p.cuerpo)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'gestion_cobro.registrada' AND payload->>'gestion_id' = $1`, id); n != 1 {
		t.Fatalf("evento: %d", n)
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.registrar_gestion_cobro' AND resultado = 'ok'`); n < 1 {
		t.Fatalf("auditoría: %d", n)
	}
	// La misma clave y el mismo cuerpo: la misma gestión (sin otro pendiente); con otro cuerpo, 422.
	b.exigir(b.apiClave(b.supervisor, http.MethodPost, ruta, "g-1", cuerpo), http.StatusCreated, "id", id)
	if n := b.contar(`SELECT count(*) FROM mensajeria.gestiones_cobro WHERE conversacion_id = $1`, conv); n != 1 {
		t.Fatalf("gestiones: %d", n)
	}
	cuerpo2 := map[string]any{"tipo": "mensaje", "resultado": "contesto", "ocurridoEn": ahora.Add(-time.Hour)}
	if r := b.apiClave(b.supervisor, http.MethodPost, ruta, "g-1", cuerpo2); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("clave reutilizada: %d %v", r.status, r.cuerpo)
	}

	// contactoId en el cuerpo: no existe (el contacto sale de la conversación, nunca de quien la registra).
	if r := b.api(b.supervisor, http.MethodPost, ruta, map[string]any{"tipo": "mensaje", "resultado": "contesto", "ocurridoEn": ahora, "contactoId": "77"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("contactoId en el cuerpo: %d %v", r.status, r.cuerpo)
	}

	// ocurrido_en fuera de la ventana (ahora − 7 días a ahora + 5 min) y listas cerradas.
	for _, c := range []map[string]any{
		{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora.Add(-7*24*time.Hour - time.Minute)},
		{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora.Add(6 * time.Minute)},
		{"tipo": "correo", "resultado": "contesto", "ocurridoEn": ahora},
		{"tipo": "llamada", "resultado": "pago 250", "ocurridoEn": ahora},
		{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "nota": "texto libre"},
	} {
		if r := b.api(b.supervisor, http.MethodPost, ruta, c); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("%v: %d %v", c, r.status, r.cuerpo)
		}
	}
	// Servicio de otro cliente: 422.
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": 987654321}),
		http.StatusUnprocessableEntity)

	// Sin permiso (lectura) y la lista.
	b.exigir(b.api(b.lectura, http.MethodPost, ruta, cuerpo2), http.StatusForbidden)
	l := b.api(b.supervisor, http.MethodGet, ruta, nil)
	b.exigir(l, http.StatusOK)
	if len(l.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("lista: %v", l.cuerpo)
	}

	// Cliente sin confirmar: 422; sin cliente: 422.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_verificado = false WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta, cuerpo2), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = NULL, asociacion = NULL WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta, cuerpo2), http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_cliente")

	// Argos con un contrato anterior a 0.13.0: 409.
	conv2, _ := b.conCliente()
	b.contratoArgos("0.12.1")
	b.exigir(b.api(b.supervisor, http.MethodPost, "/conversaciones/"+conv2+"/gestiones-cobro", cuerpo2), http.StatusConflict,
		"type", "urn:iris:problema:argos_sin_gestiones")
}

// Con el asistente de IA, promete_pagar y pago_informado solo con una promesa o un comprobante real.
func TestGestionesCobroAsistente(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.contratoArgos("0.13.0")
	conv, cliente := b.conCliente()
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET asignado_a = NULL, asistente_estado = 'atendiendo' WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	permisos := map[string][]contexto.Alcance{}
	for _, p := range append(nucleo.PermisosAsistenteIA(), nucleo.PermConversacionesRegistrarGestionCobro) {
		permisos[string(p)] = []contexto.Alcance{{}}
	}
	ctx := contexto.ComoAsistenteIA(b.ctx, 1, contexto.NuevosPermisos(permisos))
	id, err := strconv.ParseInt(conv, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	ahora := *b.reloj
	_, err = b.m.RegistrarGestionCobro(ctx, mensajeria.EntradaGestionCobro{ID: id, Tipo: "mensaje", Resultado: "pago_informado", OcurridoEn: ahora})
	if p, ok := err.(*nucleo.Problema); !ok || p.Type != "urn:iris:problema:sin_respaldo" {
		t.Fatalf("sin respaldo: %v", err)
	}
	// Otro resultado sí.
	g, err := b.m.RegistrarGestionCobro(ctx, mensajeria.EntradaGestionCobro{ID: id, Tipo: "mensaje", Resultado: "contesto", OcurridoEn: ahora})
	if err != nil || g.UsuarioID != nil {
		t.Fatalf("contesto: %+v %v", g, err)
	}
	if p := b.pendienteDeGestion(g.ID.String()); p.actor != "asistente_ia" {
		t.Fatalf("actor: %+v", p)
	}
	// Con una promesa real de Argos en la conversación, sí.
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.promesas (id, empresa_id, conversacion_id, cliente_id, servicio_id, vence_el, estado, promesa_argos_id, hash_peticion)
		VALUES (gen_random_uuid(), 1, $1, $2, $3, current_date + 3, 'activa', 501, '\x00')`, id, cliente, cliente*10); err != nil {
		t.Fatal(err)
	}
	if _, err := b.m.RegistrarGestionCobro(ctx, mensajeria.EntradaGestionCobro{ID: id, Tipo: "promesa", Resultado: "promete_pagar", OcurridoEn: ahora}); err != nil {
		t.Fatalf("con promesa: %v", err)
	}
	// Sin el permiso (el rol asistente_ia de fábrica), 403.
	if _, err := b.m.RegistrarGestionCobro(mensajeria.CtxAsistenteIA(b.ctx, 1), mensajeria.EntradaGestionCobro{ID: id, Tipo: "mensaje", Resultado: "contesto", OcurridoEn: ahora}); err == nil {
		t.Fatal("el rol asistente_ia de fábrica no registra gestiones")
	}
}

// El contacto sale del número de la conversación (B22): un contacto conocido va con X-Iris-Contacto
// (solo sobre sus servicios); un número que no es del titular ni de un contacto conocido, 422
// contacto_desconocido (nunca a nombre del titular).
func TestGestionesCobroContacto(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.contratoArgos("0.13.0")
	cliente := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Marta Pérez', '+502 5999-0000', 'C-2', now(), '{1}')`, cliente); err != nil {
		t.Fatal(err)
	}
	for _, s := range []int64{cliente * 10, cliente*10 + 1} {
		if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
			VALUES ($1, 1, $2, 'S-1', 'Fibra', 'activo', 'Zona 1', now())`, s, cliente); err != nil {
			t.Fatal(err)
		}
	}
	// El hijo de Marta: contacto 4400 de su cliente, solo del primer servicio.
	num := numeroCliente()
	contacto := cliente*100 + 1
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_contactos (contacto_id, cliente_id, empresa_id, nombre, relacion, servicios)
		VALUES ($1, $2, 1, 'José', 'familiar', $3)`, contacto, cliente, []int64{cliente * 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos) VALUES ($1, $2, $3)`,
		contacto, "+"+num, num); err != nil {
		t.Fatal(err)
	}
	b.texto(num, fmt.Sprintf("wamid.k%d", cliente), "Hola")
	conv := cid(b.conv(num))
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, cliente_verificado = true, asociacion = 'manual' WHERE id = $1`,
		conv, cliente); err != nil {
		t.Fatal(err)
	}
	ruta := "/conversaciones/" + conv + "/gestiones-cobro"
	ahora := *b.reloj
	r := b.api(b.supervisor, http.MethodPost, ruta, map[string]any{"tipo": "llamada", "resultado": "promete_pagar", "ocurridoEn": ahora, "servicioId": cliente * 10})
	b.exigir(r, http.StatusCreated, "contactoId", fmt.Sprint(contacto))
	if p := b.pendienteDeGestion(fmt.Sprint(r.cuerpo["id"])); p.contacto == nil || *p.contacto != fmt.Sprint(contacto) {
		t.Fatalf("X-Iris-Contacto: %+v", p)
	}
	// Un servicio del cliente que no es del contacto: 403.
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": cliente*10 + 1}),
		http.StatusForbidden, "type", "urn:iris:problema:servicio_fuera_del_contacto")

	// Otro número asociado a mano al mismo cliente, sin ser su teléfono ni un contacto: 422.
	otro := numeroCliente()
	b.texto(otro, fmt.Sprintf("wamid.d%d", cliente), "Hola")
	conv2 := cid(b.conv(otro))
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, cliente_verificado = true, asociacion = 'manual' WHERE id = $1`,
		conv2, cliente); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.supervisor, http.MethodPost, "/conversaciones/"+conv2+"/gestiones-cobro", map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:contacto_desconocido")
	if n := b.contar(`SELECT count(*) FROM mensajeria.gestiones_cobro WHERE conversacion_id = $1`, conv2); n != 0 {
		t.Fatalf("no se registra: %d", n)
	}
}
