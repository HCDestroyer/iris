package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
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

var claveGestion atomic.Int64

// gestionar registra una gestión con una Idempotency-Key nueva (la ruta la exige).
func (b *bandeja) gestionar(p persona, ruta string, cuerpo any) respuesta {
	b.t.Helper()
	return b.apiClave(p, http.MethodPost, ruta, fmt.Sprintf("gestion-%d", claveGestion.Add(1)), cuerpo)
}

// exigirCampo: el 422 de validación con ese campo y esa regla.
func (b *bandeja) exigirCampo(r respuesta, campo, regla string) {
	b.t.Helper()
	if r.status != http.StatusUnprocessableEntity || r.cuerpo["type"] != "urn:iris:problema:validacion" {
		b.t.Fatalf("esperaba 422 validacion, llegó %d %v", r.status, r.cuerpo)
	}
	errs, _ := r.cuerpo["errors"].([]any)
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok && m["field"] == campo && m["rule"] == regla {
			return
		}
	}
	b.t.Fatalf("esperaba el campo %s (%s): %v", campo, regla, r.cuerpo)
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

	// Sin Idempotency-Key: 400.
	sinClave := b.api(b.supervisor, http.MethodPost, ruta, cuerpo)
	b.exigir(sinClave, http.StatusBadRequest, "type", "urn:iris:problema:idempotency_key_requerida")
	if d := fmt.Sprint(sinClave.cuerpo["detail"]); !strings.Contains(d, "gestión de cobro") || strings.Contains(d, "argos:aviso") {
		t.Fatalf("mensaje propio del 400: %q", d)
	}
	// Un servicio del mismo cliente en otra empresa (instantánea): no es de este cliente en esta empresa.
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 2, $2, 'S-X', 'Fibra', 'activo', 'Zona 1', now())`, cliente*10+5, cliente); err != nil {
		t.Fatal(err)
	}
	b.exigirCampo(b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": cliente*10 + 5}),
		"servicioId", "servicio_del_cliente")
	// contactoId en el cuerpo: no existe (el contacto sale de la conversación, nunca de quien la registra).
	if r := b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "mensaje", "resultado": "contesto", "ocurridoEn": ahora, "contactoId": "77"}); r.status != http.StatusUnprocessableEntity {
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
		if r := b.gestionar(b.supervisor, ruta, c); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("%v: %d %v", c, r.status, r.cuerpo)
		}
	}
	// Servicio de otro cliente: 422.
	b.exigirCampo(b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": 987654321}),
		"servicioId", "servicio_del_cliente")

	// Sin permiso (lectura) y la lista.
	b.exigir(b.gestionar(b.lectura, ruta, cuerpo2), http.StatusForbidden)
	l := b.api(b.supervisor, http.MethodGet, ruta, nil)
	b.exigir(l, http.StatusOK)
	if len(l.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("lista: %v", l.cuerpo)
	}

	// Cliente sin confirmar: 422; sin cliente: 422.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_verificado = false WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.gestionar(b.supervisor, ruta, cuerpo2), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = NULL, asociacion = NULL WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.gestionar(b.supervisor, ruta, cuerpo2), http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_cliente")

	// Argos con un contrato anterior a 0.13.0: 409.
	conv2, _ := b.conCliente()
	b.contratoArgos("0.12.1")
	b.exigir(b.gestionar(b.supervisor, "/conversaciones/"+conv2+"/gestiones-cobro", cuerpo2), http.StatusConflict,
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

// Quién habla sale del rol de la conversación (B22): un contacto va con X-Iris-Contacto, necesita
// «pagar» y solo sus servicios (el único suyo si no dice cuál); el titular sin cabecera; sin rol, 422
// cliente_sin_confirmar (nunca a nombre del titular).
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
	r := b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "promete_pagar", "ocurridoEn": ahora, "servicioId": cliente * 10})
	b.exigir(r, http.StatusCreated, "contactoId", fmt.Sprint(contacto))
	if p := b.pendienteDeGestion(fmt.Sprint(r.cuerpo["id"])); p.contacto == nil || *p.contacto != fmt.Sprint(contacto) {
		t.Fatalf("X-Iris-Contacto: %+v", p)
	}
	// Un servicio del cliente que no es del contacto: 403.
	b.exigir(b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": cliente*10 + 1}),
		http.StatusForbidden, "type", "urn:iris:problema:servicio_fuera_del_contacto")

	// Sin servicio: el único del contacto (sus servicios están acotados).
	r = b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora})
	b.exigir(r, http.StatusCreated)
	if r.cuerpo["servicioId"] != float64(cliente*10) {
		t.Fatalf("el único servicio del contacto: %v", r.cuerpo["servicioId"])
	}
	// Con dos servicios acotados y sin decir cuál: 422 en servicioId.
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.argos_contactos SET servicios = $2 WHERE contacto_id = $1`, contacto, []int64{cliente * 10, cliente*10 + 1}); err != nil {
		t.Fatal(err)
	}
	b.exigirCampo(b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora}), "servicioId", "required")
	// Sin el permiso «pagar»: 403 permiso_de_contacto.
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.argos_contactos SET pagar = false WHERE contacto_id = $1`, contacto); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": cliente * 10}),
		http.StatusForbidden, "type", "urn:iris:problema:permiso_de_contacto")
	// Otro contacto del mismo número (de todos los servicios): si la conversación se asoció por él
	// (contacto_argos_id), la gestión sale a su nombre, no a nombre del primero del número.
	contacto2 := cliente*100 + 2
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_contactos (contacto_id, cliente_id, empresa_id, nombre, relacion, servicios)
		VALUES ($1, $2, 1, 'Ana', 'familiar', NULL)`, contacto2, cliente); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos) VALUES ($1, $2, $3)`,
		contacto2, "+"+num, num); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET contacto_argos_id = $2 WHERE id = $1`, conv, contacto2); err != nil {
		t.Fatal(err)
	}
	r = b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": cliente*10 + 1})
	b.exigir(r, http.StatusCreated, "contactoId", fmt.Sprint(contacto2))
	if p := b.pendienteDeGestion(fmt.Sprint(r.cuerpo["id"])); p.contacto == nil || *p.contacto != fmt.Sprint(contacto2) {
		t.Fatalf("X-Iris-Contacto del contacto asociado: %+v", p)
	}

	// Otro número asociado a mano al mismo cliente («verificado» sin verificado_por), sin ser su teléfono
	// ni un contacto: 422 cliente_sin_confirmar, nunca a nombre del titular.
	otro := numeroCliente()
	b.texto(otro, fmt.Sprintf("wamid.d%d", cliente), "Hola")
	conv2 := cid(b.conv(otro))
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, cliente_verificado = true, asociacion = 'manual' WHERE id = $1`,
		conv2, cliente); err != nil {
		t.Fatal(err)
	}
	ruta2 := "/conversaciones/" + conv2 + "/gestiones-cobro"
	b.exigir(b.gestionar(b.supervisor, ruta2, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
	if n := b.contar(`SELECT count(*) FROM mensajeria.gestiones_cobro WHERE conversacion_id = $1`, conv2); n != 0 {
		t.Fatalf("no se registra: %d", n)
	}
	// Un supervisor confirmó el cliente: el número es del titular también en B31 (sin X-Iris-Contacto).
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET verificado_por = $2 WHERE id = $1`, conv2, b.supervisor.id); err != nil {
		t.Fatal(err)
	}
	r = b.gestionar(b.supervisor, ruta2, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora})
	b.exigir(r, http.StatusCreated)
	if r.cuerpo["contactoId"] != nil {
		t.Fatalf("titular sin contacto: %v", r.cuerpo["contactoId"])
	}
	if p := b.pendienteDeGestion(fmt.Sprint(r.cuerpo["id"])); p.contacto != nil {
		t.Fatalf("titular sin X-Iris-Contacto: %+v", p)
	}
	// Asociada por un contacto que ya no tiene este número (sin rol): nunca sale como titular, aunque un
	// supervisor la haya confirmado.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET contacto_argos_id = $2 WHERE id = $1`, conv2, contacto); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.gestionar(b.supervisor, ruta2, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
}

// B25 (§3.8): José está a cargo del servicio de su mamá. Desde su conversación (su número, titular de
// su cliente), una gestión sobre ese servicio va al cliente de la mamá con X-Iris-Contacto = el
// contacto de José en ella, y exige el permiso «pagar» de ese vínculo.
func TestGestionesCobroACargo(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.contratoArgos("0.13.0")
	conv, jose := b.conCliente()
	var numero string
	if err := b.bd.QueryRow(b.ctx, `SELECT numero FROM mensajeria.conversaciones WHERE id = $1`, conv).Scan(&numero); err != nil {
		t.Fatal(err)
	}
	mama := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Rosa', '+502 5888-0000', 'C-3', now(), '{1}')`, mama); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 1, $2, 'S-412', 'Fibra', 'activo', 'Zona 1', now())`, mama*10, mama); err != nil {
		t.Fatal(err)
	}
	contacto := mama*100 + 1
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_contactos (contacto_id, cliente_id, empresa_id, nombre, relacion, servicios, persona_cliente_id)
		VALUES ($1, $2, 1, 'José', 'familiar', NULL, $3)`, contacto, mama, jose); err != nil {
		t.Fatal(err)
	}
	digitos := soloDigitosPrueba(numero)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos) VALUES ($1, $2, $3)`,
		contacto, "+"+digitos, digitos); err != nil {
		t.Fatal(err)
	}
	ruta := "/conversaciones/" + conv + "/gestiones-cobro"
	ahora := *b.reloj
	r := b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "promete_pagar", "ocurridoEn": ahora, "servicioId": mama * 10})
	b.exigir(r, http.StatusCreated, "contactoId", fmt.Sprint(contacto))
	p := b.pendienteDeGestion(fmt.Sprint(r.cuerpo["id"]))
	if p.ruta != fmt.Sprintf("/clientes/%d/gestiones-cobro", mama) || p.contacto == nil || *p.contacto != fmt.Sprint(contacto) ||
		p.cuerpo["servicio_id"] != float64(mama*10) {
		t.Fatalf("a cargo: %+v", p)
	}
	// Su propio servicio sigue saliendo como titular, sin cabecera.
	r = b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": jose * 10})
	b.exigir(r, http.StatusCreated)
	if p := b.pendienteDeGestion(fmt.Sprint(r.cuerpo["id"])); p.contacto != nil || p.ruta != fmt.Sprintf("/clientes/%d/gestiones-cobro", jose) {
		t.Fatalf("propio: %+v", p)
	}
	gestion := func(ruta string, servicio int64) respuesta {
		return b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": servicio})
	}
	// Un servicio de otro cliente sin vínculo y uno que no existe: el mismo 422 (no dice si existe).
	otro := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Otro', '+502 5777-0000', 'C-4', now(), '{1}')`, otro); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 1, $2, 'S-7', 'Fibra', 'activo', 'Zona 1', now())`, otro*10, otro); err != nil {
		t.Fatal(err)
	}
	b.exigirCampo(gestion(ruta, otro*10), "servicioId", "servicio_del_cliente")
	b.exigirCampo(gestion(ruta, 987654321), "servicioId", "servicio_del_cliente")
	// Un servicio de la mamá en otra empresa: igual.
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 2, $2, 'S-8', 'Fibra', 'activo', 'Zona 1', now())`, mama*10+5, mama); err != nil {
		t.Fatal(err)
	}
	b.exigirCampo(gestion(ruta, mama*10+5), "servicioId", "servicio_del_cliente")
	// Una conversación de José confirmada por un supervisor desde un número que no es el suyo (aunque sea
	// de su contacto en la mamá): titular por la confirmación, pero no abre «a cargo».
	otroNum := numeroCliente()
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_contacto_telefonos (contacto_id, numero, digitos) VALUES ($1, $2, $3)`,
		contacto, "+"+otroNum, otroNum); err != nil {
		t.Fatal(err)
	}
	b.texto(otroNum, fmt.Sprintf("wamid.ac%d", jose), "Hola")
	conv2 := cid(b.conv(otroNum))
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, cliente_verificado = true, asociacion = 'manual', verificado_por = $3, contacto_argos_id = NULL
		WHERE id = $1`, conv2, jose, b.supervisor.id); err != nil {
		t.Fatal(err)
	}
	b.exigirCampo(gestion("/conversaciones/"+conv2+"/gestiones-cobro", mama*10), "servicioId", "servicio_del_cliente")
	// Un vínculo acotado a otro servicio de la mamá: el pedido no está a cargo, el mismo 422.
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.argos_contactos SET servicios = $2 WHERE contacto_id = $1`, contacto, []int64{mama*10 + 1}); err != nil {
		t.Fatal(err)
	}
	b.exigirCampo(gestion(ruta, mama*10), "servicioId", "servicio_del_cliente")
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.argos_contactos SET servicios = NULL WHERE contacto_id = $1`, contacto); err != nil {
		t.Fatal(err)
	}
	// Sin «pagar» en ese vínculo: 403.
	if _, err := b.bd.Exec(b.ctx, `UPDATE integracion.argos_contactos SET pagar = false WHERE contacto_id = $1`, contacto); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.gestionar(b.supervisor, ruta, map[string]any{"tipo": "llamada", "resultado": "contesto", "ocurridoEn": ahora, "servicioId": mama * 10}),
		http.StatusForbidden, "type", "urn:iris:problema:permiso_de_contacto")
}

func soloDigitosPrueba(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
