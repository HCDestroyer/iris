package mensajeria_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Aprobar y rechazar boletas desde la conversación (contrato con Argos 0.7.0 §7.7, pedido B3):
// Postgres real, la API por HTTP y un Argos falso en httptest (boletas_caja_test.go).

// cajaDe arma la bandeja con el Argos falso de caja.
func cajaDe(t *testing.T, b *bandeja) *cajaFalsa {
	t.Helper()
	c := nuevaCaja(t)
	b.argos.caja = c
	return c
}

type boletaPrueba struct {
	id          string
	comprobante int64
	conv        int64
	numero      string
}

func (x boletaPrueba) ruta(accion string) string { return "/boletas/" + x.id + "/" + accion }

// boletaRegistrada deja una boleta ya registrada en Argos (con su comprobante pendiente) en una
// conversación nueva del número de la bandeja: el cliente mandó la foto hace un momento (ventana de
// 24 h abierta).
func (b *bandeja) boletaRegistrada(empresa int64) boletaPrueba {
	b.t.Helper()
	return b.boletaDe(numeroCliente(), empresa)
}

func (b *bandeja) boletaDe(numero string, empresa int64) boletaPrueba {
	b.t.Helper()
	x := boletaPrueba{numero: numero, comprobante: 500000 + siguienteCliente.Add(1), id: uuid.Must(uuid.NewV7()).String()}
	b.webhook(b.mensajeCliente(numero, "wamid.b."+x.id, `"type":"image","image":{"id":"boleta1","mime_type":"image/jpeg"}`))
	if err := b.bd.QueryRow(b.ctx, `SELECT id FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2 AND coalesce(empresa_id, 0) = $3 ORDER BY id DESC LIMIT 1`,
		b.canal.ID, "+"+numero, empresa).Scan(&x.conv); err != nil {
		b.t.Fatalf("conversación de %s en la empresa %d: %v", numero, empresa, err)
	}
	var mensaje uuid.UUID
	if err := b.bd.QueryRow(b.ctx, `SELECT id FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 ORDER BY seq DESC LIMIT 1`, x.conv).Scan(&mensaje); err != nil {
		b.t.Fatal(err)
	}
	if err := b.m.DescargarMedio(b.ctx, mensaje, false); err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.boletas (id, empresa_id, conversacion_id, cliente_id, adjunto_id, metodo, cuenta_bancaria_id,
			referencia, monto, fecha_deposito, estado, comprobante_id, registrada_por)
		SELECT $1, $2, $3, 1201, adjunto_id, 'deposito', 3, 'REF-' || $4::bigint::text, 250.00, '2026-11-04', 'registrada', $4::bigint, $5
		FROM mensajeria.conversacion_mensajes WHERE id = $6`, x.id, empresa, x.conv, x.comprobante, b.agente.id, mensaje); err != nil {
		b.t.Fatal(err)
	}
	if b.argos.caja != nil {
		b.argos.caja.comprobante(x.comprobante, empresa, "250.00")
	}
	return x
}

// estadoBoleta lee la boleta de la base.
func (b *bandeja) estadoBoleta(x boletaPrueba) (estado string, decididaPor *int64, verificado, aviso *string) {
	b.t.Helper()
	if err := b.bd.QueryRow(b.ctx, `SELECT estado, decidida_por, verificado_por_nombre, aviso_cliente FROM mensajeria.boletas WHERE id = $1`, x.id).
		Scan(&estado, &decididaPor, &verificado, &aviso); err != nil {
		b.t.Fatal(err)
	}
	return
}

// avisos son los mensajes que Iris le mandó al cliente en la conversación (salientes).
func (b *bandeja) avisos(x boletaPrueba) []string {
	b.t.Helper()
	filas, err := b.bd.Query(b.ctx, `SELECT coalesce(texto, '') FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente' ORDER BY seq`, x.conv)
	if err != nil {
		b.t.Fatal(err)
	}
	textos, err := pgx.CollectRows(filas, pgx.RowTo[string])
	if err != nil {
		b.t.Fatal(err)
	}
	return textos
}

func (b *bandeja) eventosBoleta(evento string, x boletaPrueba) int {
	return b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND payload->>'boleta_id' = $2`, evento, x.id)
}

func (b *bandeja) sistemaDe(x boletaPrueba, tipo string) int {
	return b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'sistema' AND evento->>'tipo' = $2`, x.conv, tipo)
}

// webhookComprobante: lo que deja el webhook comprobante.aprobado / comprobante.rechazado de Argos
// en el outbox (argos.comprobante_resuelto) y el relay del worker.
func (b *bandeja) webhookComprobante(x boletaPrueba, estado string, empresa int64, extra map[string]any) {
	b.t.Helper()
	payload := map[string]any{"comprobante_id": x.comprobante, "estado": estado, "empresa_id": empresa, "cliente_id": 1201, "servicio_id": nil,
		"canal": "iris", "monto": "250.00", "pago_id": nil, "motivo": nil, "causa": nil, "servicio_estado": "activo", "entrante_id": 1}
	for k, v := range extra {
		payload[k] = v
	}
	if err := nucleo.EnTx(nucleo.ConBD(b.ctx, b.bd), func(ctx context.Context, tx pgx.Tx) error {
		_, err := integracion.Insertar(ctx, tx, argos.EventoComprobanteResuelto, &empresa, "", payload)
		return err
	}); err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.relay.Ejecutar(b.ctx); err != nil {
		b.t.Fatal(err)
	}
}

// despacharResoluciones corre el despacho pasada la gracia.
func (b *bandeja) despacharResoluciones(pasada bool) {
	b.t.Helper()
	if pasada {
		*b.reloj = b.reloj.Add(mensajeria.GraciaResolucion + time.Second)
	}
	if err := b.m.DespacharResoluciones(b.ctx, b.bd); err != nil {
		b.t.Fatal(err)
	}
}

func tipoDe(r respuesta) string {
	return strings.TrimPrefix(fmt.Sprint(r.cuerpo["type"]), "urn:iris:problema:")
}

func (b *bandeja) exigirProblema(r respuesta, status int, codigo string) {
	b.t.Helper()
	if r.status != status || tipoDe(r) != codigo {
		b.t.Fatalf("esperaba %d %s, llegó %d %v", status, codigo, r.status, r.cuerpo)
	}
	// Texto en español para la persona, nunca el detalle interno de Argos.
	if d := fmt.Sprint(r.cuerpo["detail"]); d == "" || strings.Contains(d, "detalle interno de Argos") {
		b.t.Fatalf("detalle de %s: %q", codigo, d)
	}
}

func TestBoletaAprobar(t *testing.T) {
	b := nuevaBandeja(t, nil)
	caja := cajaDe(t, b)
	caja.vincular(b.supervisor.id, "Ana Caja", 1)
	x := b.boletaRegistrada(1)

	// Antes: registrada y se puede decidir; quien no tiene el permiso no aprueba.
	v := b.api(b.agente, http.MethodGet, "/boletas/"+x.id, nil)
	b.exigir(v, http.StatusOK, "estado", "registrada", "puedeDecidir", "true", "monto", "250.00", "verificadoPorNombre", "<nil>")
	l := b.api(b.agente, http.MethodGet, fmt.Sprintf("/conversaciones/%d/boletas", x.conv), nil)
	b.exigir(l, http.StatusOK)
	if d := l.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["id"] != x.id || d[0].(map[string]any)["puedeDecidir"] != true {
		t.Fatalf("boletas de la conversación: %v", l.cuerpo)
	}
	b.exigir(b.api(b.agente, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusForbidden)
	b.exigir(b.api(b.lectura, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "no aparece"}), http.StatusForbidden)
	if len(caja.decisiones()) != 0 {
		t.Fatal("sin permiso no se llama a Argos")
	}

	r := b.apiClave(b.supervisor, http.MethodPost, x.ruta("aprobar"), "clave-1", map[string]any{"nota": "Depósito visto en el estado de cuenta", "facturas": []int64{881, 882}})
	b.exigir(r, http.StatusOK, "estado", "aprobada", "puedeDecidir", "false", "verificadoPorNombre", "Ana Caja", "avisoCliente", "enviado", "causaRechazo", "<nil>")
	if r.cuerpo["pagoId"] == nil || r.cuerpo["decididaEn"] == nil || r.cuerpo["decididaPor"].(map[string]any)["id"].(float64) != float64(b.supervisor.id) {
		t.Fatalf("aprobada: %v", r.cuerpo)
	}
	// Lo que llegó a Argos (§7.7): actor, conversación, clave por comprobante y empresa activa.
	ds := caja.decisiones()
	if len(ds) != 1 {
		t.Fatalf("decisiones: %v", ds)
	}
	d := ds[0]
	if d.ruta != fmt.Sprintf("/api/v1/comprobantes/%d/aprobar", x.comprobante) || d.actor != fmt.Sprintf("usuario:%d", b.supervisor.id) ||
		d.conversacion != fmt.Sprintf("iris:conversacion:%d", x.conv) || d.clave != fmt.Sprintf("iris:aprobar:%d", x.comprobante) || d.empresa != "1" {
		t.Fatalf("petición a Argos: %+v", d)
	}
	if d.cuerpo["nota"] != "Depósito visto en el estado de cuenta" || len(d.cuerpo["facturas"].([]any)) != 2 || d.cuerpo["monto"] != nil {
		t.Fatalf("cuerpo a Argos: %v", d.cuerpo)
	}
	// Estado local, auditoría, evento, mensaje de sistema y un solo aviso al cliente.
	if estado, por, nombre, aviso := b.estadoBoleta(x); estado != "aprobada" || por == nil || *por != b.supervisor.id || *nombre != "Ana Caja" || *aviso != "enviado" {
		t.Fatalf("boleta: %s %v", estado, por)
	}
	if b.eventosBoleta("boleta.aprobada", x) != 1 || b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'boleta.aprobada' AND payload->>'boleta_id' = $1
		AND (payload->>'actor_id')::bigint = $2 AND (payload->>'comprobante_id')::bigint = $3 AND payload->>'pago_id' IS NOT NULL AND payload->>'origen' = 'iris'`,
		x.id, b.supervisor.id, x.comprobante) != 1 {
		t.Fatal("boleta.aprobada")
	}
	if b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'boletas.aprobar' AND resultado = 'ok' AND actor_id = $1 AND entidad = 'boleta' AND entidad_id = $2
		AND entrada::text NOT LIKE '%Depósito visto%'`, b.supervisor.id, x.id) != 1 {
		t.Fatal("auditoría de boletas.aprobar (sin el texto de la nota)")
	}
	if b.sistemaDe(x, "boleta_aprobada") != 1 {
		t.Fatal("mensaje de sistema boleta_aprobada")
	}
	if a := b.avisos(x); len(a) != 1 || a[0] != "Confirmamos tu pago de GTQ 250.00. ¡Gracias!" {
		t.Fatalf("aviso al cliente: %v", a)
	}
	if len(b.jobsDe("mensajeria.enviar_mensaje")) != 1 {
		t.Fatal("el aviso queda en cola de envío")
	}

	// Repetir (misma clave o no) la misma decisión: la boleta como quedó, sin volver a Argos.
	b.exigir(b.apiClave(b.supervisor, http.MethodPost, x.ruta("aprobar"), "clave-1", map[string]any{"nota": "Depósito visto en el estado de cuenta", "facturas": []int64{881, 882}}),
		http.StatusOK, "estado", "aprobada", "verificadoPorNombre", "Ana Caja")
	b.exigir(b.apiClave(b.supervisor, http.MethodPost, x.ruta("aprobar"), "clave-2", map[string]any{}), http.StatusOK, "estado", "aprobada")
	if len(caja.decisiones()) != 1 {
		t.Fatal("la repetición volvió a llamar a Argos")
	}
	// Otra decisión, u otra persona: ya está decidida.
	otra := b.persona([]int64{1}, rolBase("supervisor"))
	caja.vincular(otra.id, "Luis Caja", 1)
	b.exigirProblema(b.api(otra, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusConflict, "boleta_ya_decidida")
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "me equivoqué"}), http.StatusConflict, "boleta_ya_decidida")

	// El webhook comprobante.aprobado llega igual después: no avisa dos veces ni emite otro evento.
	b.webhookComprobante(x, "aprobado", 1, map[string]any{"pago_id": 90001})
	b.despacharResoluciones(true)
	if a := b.avisos(x); len(a) != 1 || b.eventosBoleta("boleta.aprobada", x) != 1 || b.sistemaDe(x, "boleta_aprobada") != 1 {
		t.Fatalf("el webhook duplicó el aviso: %v", a)
	}
	if b.contar(`SELECT count(*) FROM mensajeria.boletas WHERE id = $1 AND resolucion IS NULL`, x.id) != 1 {
		t.Fatal("no queda resolución pendiente")
	}
	b.exigir(b.api(b.agente, http.MethodGet, "/boletas/"+x.id, nil), http.StatusOK, "estado", "aprobada", "puedeDecidir", "false", "verificadoPorNombre", "Ana Caja")
}

func TestBoletaRechazar(t *testing.T) {
	b := nuevaBandeja(t, nil)
	caja := cajaDe(t, b)
	caja.vincular(b.supervisor.id, "Ana Caja", 1)
	x := b.boletaRegistrada(1)
	// Validación de la entrada (no llega a Argos).
	b.exigir(b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "no"}), http.StatusUnprocessableEntity)
	b.exigir(b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "   a   "}), http.StatusUnprocessableEntity)
	b.exigir(b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "no se lee", "causa": "porque sí"}), http.StatusUnprocessableEntity)
	b.exigir(b.api(b.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{"nota": strings.Repeat("a", 401)}), http.StatusUnprocessableEntity)
	if len(caja.decisiones()) != 0 {
		t.Fatal("una entrada inválida no llega a Argos")
	}
	r := b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "No se ve el número de boleta", "causa": "falta_dato"})
	b.exigir(r, http.StatusOK, "estado", "rechazada", "causaRechazo", "falta_dato", "motivoRechazo", "No se ve el número de boleta", "pagoId", "<nil>",
		"verificadoPorNombre", "Ana Caja", "avisoCliente", "enviado")
	d := caja.decisiones()[0]
	if d.clave != fmt.Sprintf("iris:rechazar:%d", x.comprobante) || d.cuerpo["motivo"] != "No se ve el número de boleta" || d.cuerpo["causa"] != "falta_dato" {
		t.Fatalf("petición a Argos: %+v", d)
	}
	if a := b.avisos(x); len(a) != 1 || a[0] != "A tu boleta de GTQ 250.00 le falta un dato: No se ve el número de boleta. Envíala de nuevo, por favor." {
		t.Fatalf("aviso boleta_falta_dato: %v", a)
	}
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'boleta.rechazada' AND payload->>'boleta_id' = $1 AND payload->>'causa' = 'falta_dato'
		AND (payload->>'actor_id')::bigint = $2 AND payload->>'pago_id' IS NULL`, x.id, b.supervisor.id) != 1 || b.sistemaDe(x, "boleta_rechazada") != 1 {
		t.Fatal("boleta.rechazada")
	}
	// El webhook comprobante.rechazado después: nada más.
	b.webhookComprobante(x, "rechazado", 1, map[string]any{"motivo": "No se ve el número de boleta", "causa": "falta_dato"})
	b.despacharResoluciones(true)
	if len(b.avisos(x)) != 1 || b.eventosBoleta("boleta.rechazada", x) != 1 {
		t.Fatal("el webhook duplicó el aviso del rechazo")
	}

	// Sin causa, Argos usa «otro» y el cliente recibe boleta_rechazada.
	y := b.boletaRegistrada(1)
	b.exigir(b.api(b.supervisor, http.MethodPost, y.ruta("rechazar"), map[string]any{"motivo": "El depósito no aparece en el banco"}), http.StatusOK, "causaRechazo", "otro")
	if a := b.avisos(y); len(a) != 1 || a[0] != "No pudimos confirmar tu boleta de GTQ 250.00: El depósito no aparece en el banco" {
		t.Fatalf("aviso boleta_rechazada: %v", a)
	}

	// Con la ventana de 24 h cerrada y sin plantilla aprobada en el número, no se le escribe: queda dicho.
	z := b.boletaRegistrada(1)
	*b.reloj = b.reloj.Add(25 * time.Hour)
	b.exigir(b.api(b.supervisor, http.MethodPost, z.ruta("aprobar"), map[string]any{}), http.StatusOK, "estado", "aprobada", "avisoCliente", "sin_aviso")
	if len(b.avisos(z)) != 0 || b.sistemaDe(z, "boleta_aprobada") != 1 {
		t.Fatal("fuera de la ventana y sin plantilla no sale aviso")
	}

	// Fuera de la ventana, con la plantilla aprobada en el número y permiso de contacto del cliente
	// (verificado): sale como plantilla.
	w := b.boletaRegistrada(1)
	cliente := b.cliente(1, "5512-0077")
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $1, cliente_verificado = true, asociacion = 'manual' WHERE id = $2`, cliente, w.conv); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.canales SET plantillas = plantillas || '{"boleta_confirmada": {"nombre": "boleta_confirmada_v1", "idioma": "es"}}'::jsonb
		WHERE id = $1`, b.canal.ID); err != nil {
		t.Fatal(err)
	}
	if c := b.consentir(cliente, "acepta", *b.reloj); c.status/100 != 2 {
		t.Fatalf("permiso de contacto: %d %v", c.status, c.cuerpo)
	}
	*b.reloj = b.reloj.Add(25 * time.Hour)
	b.exigir(b.api(b.supervisor, http.MethodPost, w.ruta("aprobar"), map[string]any{}), http.StatusOK, "estado", "aprobada", "avisoCliente", "enviado")
	if b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente' AND tipo = 'plantilla'
		AND plantilla = 'boleta_confirmada' AND variables->>'monto' = '250.00' AND variables->>'moneda' = 'GTQ' AND autor_id IS NULL`, w.conv) != 1 {
		t.Fatal("fuera de la ventana el aviso sale como plantilla aprobada")
	}
}

// Cada error de autorización de Argos (403 actor_*) y lo que ve la persona.
func TestBoletaErroresDelVinculo(t *testing.T) {
	b := nuevaBandeja(t, nil)
	caja := cajaDe(t, b)
	x := b.boletaRegistrada(1)
	sigue := func(que string) {
		t.Helper()
		if estado, _, _, _ := b.estadoBoleta(x); estado != "registrada" || len(b.avisos(x)) != 0 {
			t.Fatalf("%s: la boleta debía seguir registrada y sin aviso (%s)", que, estado)
		}
	}
	// Sin vínculo: 409 y el panel ofrece «Vincular».
	r := b.apiClave(b.supervisor, http.MethodPost, x.ruta("aprobar"), "misma-clave", map[string]any{})
	b.exigirProblema(r, http.StatusConflict, "argos_sin_vinculo")
	if r.cuerpo["detail"] != "Vincula tu usuario de Argos para aprobar pagos" {
		t.Fatalf("texto de argos_sin_vinculo: %v", r.cuerpo["detail"])
	}
	sigue("sin vínculo")
	// Usuario de Argos inactivo.
	u := caja.vincular(b.supervisor.id, "Ana Caja", 1)
	u.activo = false
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusForbidden, "argos_usuario_inactivo")
	// Sin el permiso de caja en Argos (el problem+json sin `code`: el código sale del `type`).
	u.activo, u.permisos["aprobar"] = true, false
	caja.sinCode = true
	r = b.api(b.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{})
	b.exigirProblema(r, http.StatusForbidden, "argos_sin_permiso")
	if r.cuerpo["detail"] != "Tu usuario de Argos no tiene permiso de caja" {
		t.Fatalf("texto de argos_sin_permiso: %v", r.cuerpo["detail"])
	}
	caja.sinCode = false
	// La empresa del comprobante fuera de su alcance en Argos.
	u.permisos["aprobar"] = true
	delete(u.empresas, 1)
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "no aparece"}), http.StatusForbidden, "argos_sin_empresa")
	sigue("403 de Argos")
	if b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion IN ('boletas.aprobar', 'boletas.rechazar') AND resultado = 'ok' AND entidad_id = $1`, x.id) != 0 {
		t.Fatal("un rechazo de Argos no audita una decisión")
	}
	// Vinculado y con permiso: «Reintentar» con la MISMA Idempotency-Key del primer intento funciona
	// (el 409 no quedó guardado).
	u.empresas[1] = true
	b.exigir(b.apiClave(b.supervisor, http.MethodPost, x.ruta("aprobar"), "misma-clave", map[string]any{}), http.StatusOK, "estado", "aprobada")
}

// Cada 409 de Argos, Argos caído y el reintento corto de idempotencia_en_curso.
func TestBoletaConflictosYArgosCaido(t *testing.T) {
	b := nuevaBandeja(t, nil)
	caja := cajaDe(t, b)
	caja.vincular(b.supervisor.id, "Ana Caja", 1)
	esperas := argos.EsperasEnCurso
	argos.EsperasEnCurso = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { argos.EsperasEnCurso = esperas })

	// comprobante_revisado: caja lo aprobó en el panel de Argos. Iris lo relee y sincroniza la boleta.
	x := b.boletaRegistrada(1)
	caja.mu.Lock()
	f := caja.comprobantes[x.comprobante]
	pago, nombre := int64(77001), "Caja Central"
	f.estado, f.pago, f.verificadoNombre = "aprobado", &pago, &nombre
	caja.mu.Unlock()
	r := b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "no aparece"})
	b.exigirProblema(r, http.StatusConflict, "comprobante_revisado")
	if !strings.Contains(fmt.Sprint(r.cuerpo["detail"]), "aprobada por Caja Central") {
		t.Fatalf("detalle de comprobante_revisado: %v", r.cuerpo["detail"])
	}
	if estado, por, verificado, _ := b.estadoBoleta(x); estado != "aprobada" || por != nil || *verificado != "Caja Central" {
		t.Fatalf("boleta sincronizada: %s %v", estado, por)
	}
	if a := b.avisos(x); len(a) != 1 || !strings.HasPrefix(a[0], "Confirmamos tu pago") || b.eventosBoleta("boleta.aprobada", x) != 1 {
		t.Fatalf("aviso tras sincronizar: %v", a)
	}
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'boleta.aprobada' AND payload->>'boleta_id' = $1 AND payload->>'actor_id' IS NULL AND payload->>'origen' = 'argos'`, x.id) != 1 {
		t.Fatal("boleta.aprobada de caja: sin actor de Iris")
	}
	v := b.api(b.supervisor, http.MethodGet, "/boletas/"+x.id, nil)
	b.exigir(v, http.StatusOK, "estado", "aprobada", "verificadoPorNombre", "Caja Central", "puedeDecidir", "false")
	if _, hay := v.cuerpo["decididaPor"]; hay {
		t.Fatalf("decididaPor ausente si decidió caja en Argos: %v", v.cuerpo)
	}
	// El webhook de esa aprobación llega después: nada cambia.
	b.webhookComprobante(x, "aprobado", 1, map[string]any{"pago_id": pago})
	b.despacharResoluciones(true)
	if len(b.avisos(x)) != 1 || b.eventosBoleta("boleta.aprobada", x) != 1 {
		t.Fatal("el webhook duplicó el aviso tras la relectura")
	}

	// comprobante_no_es_de_iris.
	y := b.boletaRegistrada(1)
	caja.mu.Lock()
	caja.comprobantes[y.comprobante].canal = "portal"
	caja.mu.Unlock()
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, y.ruta("aprobar"), map[string]any{}), http.StatusConflict, "comprobante_no_es_de_iris")
	caja.mu.Lock()
	caja.comprobantes[y.comprobante].canal = "iris"
	// iris_apagado: Argos tiene apagado su conector con Iris.
	caja.apagado = true
	caja.mu.Unlock()
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, y.ruta("aprobar"), map[string]any{}), http.StatusConflict, "iris_apagado")
	// idempotencia_en_curso que no cede: 409 después de los reintentos cortos.
	caja.mu.Lock()
	caja.apagado, caja.enCurso = false, 50
	caja.mu.Unlock()
	antes := len(caja.decisiones())
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, y.ruta("aprobar"), map[string]any{}), http.StatusConflict, "idempotencia_en_curso")
	if n := len(caja.decisiones()) - antes; n != 3 {
		t.Fatalf("reintentos de idempotencia_en_curso: %d", n)
	}
	// Argos caído: 502 con «Reintentar»; nada queda a medias.
	caja.mu.Lock()
	caja.enCurso, caja.caido = 0, true
	caja.mu.Unlock()
	r = b.apiClave(b.supervisor, http.MethodPost, y.ruta("aprobar"), "reintento", map[string]any{})
	b.exigirProblema(r, http.StatusBadGateway, "argos_no_disponible")
	if estado, _, _, _ := b.estadoBoleta(y); estado != "registrada" || len(b.avisos(y)) != 0 {
		t.Fatalf("con Argos caído la boleta sigue registrada: %s", estado)
	}
	// Vuelve Argos, pero la primera petición «sigue en curso» dos veces: el reintento corto la saca.
	caja.mu.Lock()
	caja.caido, caja.enCurso = false, 2
	caja.mu.Unlock()
	b.exigir(b.apiClave(b.supervisor, http.MethodPost, y.ruta("aprobar"), "reintento", map[string]any{}), http.StatusOK, "estado", "aprobada")
	if len(b.avisos(y)) != 1 {
		t.Fatal("un aviso tras el reintento")
	}

	// El comprobante ya no existe en Argos.
	z := b.boletaRegistrada(1)
	caja.mu.Lock()
	delete(caja.comprobantes, z.comprobante)
	caja.mu.Unlock()
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, z.ruta("aprobar"), map[string]any{}), http.StatusConflict, "comprobante_no_existe")

	// Una boleta que Argos no aceptó (o que aún no sale) no se decide.
	w := b.boletaRegistrada(1)
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.boletas SET estado = 'por_enviar', comprobante_id = NULL WHERE id = $1`, w.id); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.supervisor, http.MethodGet, "/boletas/"+w.id, nil), http.StatusOK, "puedeDecidir", "false")
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, w.ruta("aprobar"), map[string]any{}), http.StatusUnprocessableEntity, "boleta_no_registrada")
}

// La respuesta de Argos se perdió, y la misma clave en manos de otro actor.
func TestBoletaRespuestaPerdidaYOtroActor(t *testing.T) {
	b := nuevaBandeja(t, nil)
	caja := cajaDe(t, b)
	caja.vincular(b.supervisor.id, "Ana Caja", 1)
	otra := b.persona([]int64{1}, rolBase("supervisor"))
	caja.vincular(otra.id, "Luis Caja", 1)
	deAna := func(x boletaPrueba) argos.AtencionIris {
		return argos.AtencionIris{Actor: fmt.Sprintf("usuario:%d", b.supervisor.id), Conversacion: fmt.Sprintf("iris:conversacion:%d", x.conv), EmpresaID: 1}
	}

	// Ana aprobó en Argos pero Iris no llegó a guardar (se perdió la respuesta). Repite igual: Argos
	// devuelve la respuesta guardada (replay) y la boleta queda aprobada a su nombre.
	x := b.boletaRegistrada(1)
	if _, err := caja.cliente().AprobarComprobante(b.ctx, deAna(x), x.comprobante, nil, ""); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusOK, "estado", "aprobada", "verificadoPorNombre", "Ana Caja")
	if _, por, _, _ := b.estadoBoleta(x); por == nil || *por != b.supervisor.id || len(b.avisos(x)) != 1 {
		t.Fatal("replay de Argos: la boleta queda a nombre de quien decidió, con un aviso")
	}

	// Lo mismo, pero repite con otro cuerpo (otra nota): Argos responde 422 idempotency_key_reutilizada;
	// Iris relee, ve que la decidió ella misma y responde la boleta.
	y := b.boletaRegistrada(1)
	if _, err := caja.cliente().AprobarComprobante(b.ctx, deAna(y), y.comprobante, nil, ""); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.supervisor, http.MethodPost, y.ruta("aprobar"), map[string]any{"nota": "otra nota"}), http.StatusOK, "estado", "aprobada")
	if _, por, _, _ := b.estadoBoleta(y); por == nil || *por != b.supervisor.id || len(b.avisos(y)) != 1 {
		t.Fatal("relectura propia: a nombre de quien decidió")
	}

	// Otro actor con la misma clave (Ana ya la usó en Argos): nunca un replay a nombre de Luis.
	z := b.boletaRegistrada(1)
	if _, err := caja.cliente().AprobarComprobante(b.ctx, deAna(z), z.comprobante, nil, ""); err != nil {
		t.Fatal(err)
	}
	r := b.api(otra, http.MethodPost, z.ruta("aprobar"), map[string]any{})
	b.exigirProblema(r, http.StatusConflict, "otra_persona_decidio")
	if !strings.Contains(fmt.Sprint(r.cuerpo["detail"]), "Ana Caja") {
		t.Fatalf("detalle de otra_persona_decidio: %v", r.cuerpo["detail"])
	}
	// El estado local quedó releído: aprobada por Ana (en Argos), no por Luis.
	if estado, por, verificado, _ := b.estadoBoleta(z); estado != "aprobada" || por != nil || *verificado != "Ana Caja" || len(b.avisos(z)) != 1 {
		t.Fatalf("otro actor: %s %v", estado, por)
	}
	if b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'boletas.aprobar' AND resultado = 'ok' AND actor_id = $1 AND entidad_id = $2`, otra.id, z.id) != 0 {
		t.Fatal("Luis no aprobó nada")
	}
}

// El webhook de Argos y la respuesta a quien decidió compiten: gana el primero y el aviso sale una vez.
func TestBoletaWebhookAntesDeLaRespuesta(t *testing.T) {
	b := nuevaBandeja(t, nil)
	caja := cajaDe(t, b)
	caja.vincular(b.supervisor.id, "Ana Caja", 1)
	x := b.boletaRegistrada(1)
	// Mientras Argos todavía no le responde a Iris, llega su webhook y el despacho lo aplica.
	caja.alDecidir = func(int64) {
		b.webhookComprobante(x, "aprobado", 1, map[string]any{"pago_id": 90001})
		// Dentro de la gracia el despacho espera a la respuesta.
		b.despacharResoluciones(false)
		if estado, _, _, _ := b.estadoBoleta(x); estado != "registrada" {
			t.Errorf("dentro de la gracia la boleta sigue registrada: %s", estado)
		}
		b.despacharResoluciones(true)
		if estado, por, _, _ := b.estadoBoleta(x); estado != "aprobada" || por != nil {
			t.Errorf("el webhook la resolvió sin actor de Iris: %s %v", estado, por)
		}
	}
	r := b.api(b.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{})
	b.exigir(r, http.StatusOK, "estado", "aprobada", "verificadoPorNombre", "Ana Caja", "avisoCliente", "enviado")
	if r.cuerpo["decididaPor"].(map[string]any)["id"].(float64) != float64(b.supervisor.id) {
		t.Fatalf("la respuesta completa quién decidió: %v", r.cuerpo)
	}
	if a := b.avisos(x); len(a) != 1 || b.eventosBoleta("boleta.aprobada", x) != 1 || b.sistemaDe(x, "boleta_aprobada") != 1 {
		t.Fatalf("un solo aviso, un evento y un mensaje de sistema: %v", a)
	}
	caja.alDecidir = nil

	// Caja decide en el panel de Argos (nadie en Iris): solo llega el webhook.
	y := b.boletaRegistrada(1)
	b.webhookComprobante(y, "rechazado", 1, map[string]any{"motivo": "Monto distinto al depósito", "causa": "monto_distinto"})
	// Repetido (Argos reintenta) y de otra empresa: no cambian nada.
	b.webhookComprobante(y, "aprobado", 1, map[string]any{"pago_id": 5})
	b.webhookComprobante(y, "aprobado", 2, map[string]any{"pago_id": 6})
	b.despacharResoluciones(false)
	if estado, _, _, _ := b.estadoBoleta(y); estado != "registrada" {
		t.Fatalf("antes de la gracia: %s", estado)
	}
	b.despacharResoluciones(true)
	b.despacharResoluciones(true)
	v := b.api(b.supervisor, http.MethodGet, "/boletas/"+y.id, nil)
	b.exigir(v, http.StatusOK, "estado", "rechazada", "causaRechazo", "monto_distinto", "motivoRechazo", "Monto distinto al depósito", "avisoCliente", "enviado")
	if a := b.avisos(y); len(a) != 1 || a[0] != "No pudimos confirmar tu boleta de GTQ 250.00: Monto distinto al depósito" || b.eventosBoleta("boleta.rechazada", y) != 1 {
		t.Fatalf("aviso del rechazo de caja: %v", a)
	}
	// Ya resuelta: quien intenta decidirla en Iris recibe 409 sin llamar a Argos.
	antes := len(caja.decisiones())
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, y.ruta("aprobar"), map[string]any{}), http.StatusConflict, "boleta_ya_decidida")
	if len(caja.decisiones()) != antes {
		t.Fatal("una boleta resuelta no vuelve a Argos")
	}
	// Un webhook de un comprobante que Iris no tiene no hace nada.
	b.webhookComprobante(boletaPrueba{comprobante: 999999999}, "aprobado", 1, nil)
	b.despacharResoluciones(true)
}

// Quién puede: permisos, visibilidad, empresa, y nunca un token, una integración ni el asistente.
func TestBoletaPermisosYActores(t *testing.T) {
	b := nuevaBandeja(t, nil)
	caja := cajaDe(t, b)
	caja.vincular(b.supervisor.id, "Ana Caja", 1)
	x := b.boletaRegistrada(1)
	nada := func(que string) {
		t.Helper()
		if len(caja.decisiones()) != 0 {
			t.Fatalf("%s: no debía llegar a Argos", que)
		}
	}
	// Un token de API de un supervisor (tiene el permiso): nunca.
	conToken := b.entorno.persona([]int64{1}, rolBase("supervisor"))
	caja.vincular(conToken.id, "Token Caja", 1)
	r := b.api(conToken, http.MethodPost, x.ruta("aprobar"), map[string]any{})
	b.exigirProblema(r, http.StatusForbidden, "solo_personas")
	// La integración de Argos: no tiene el permiso.
	b.exigir(b.pedir(http.MethodPost, rutaMsj+x.ruta("aprobar"), b.token, "", map[string]any{}), http.StatusForbidden)
	// El sistema (así actúa el asistente de IA): tiene todos los permisos y aun así no.
	_, err := b.m.DecidirBoletas().Aprobar(b.ctx, mensajeria.EntradaAprobarBoleta{ID: uuid.MustParse(x.id)})
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != http.StatusForbidden || p.Codigo() != "solo_personas" {
		t.Fatalf("el asistente (sistema) aprobando: %v", err)
	}
	_, err = b.m.DecidirBoletas().Rechazar(b.ctx, mensajeria.EntradaRechazarBoleta{ID: uuid.MustParse(x.id), Motivo: "no aparece"})
	if !errors.As(err, &p) || p.Status != http.StatusForbidden || p.Codigo() != "solo_personas" {
		t.Fatalf("el asistente (sistema) rechazando: %v", err)
	}
	// Soporte y lectura ven la boleta pero no deciden; un técnico ni la ve.
	b.exigir(b.api(b.agente, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusForbidden)
	b.exigir(b.api(b.lectura, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusForbidden)
	b.exigir(b.api(b.tecnico, http.MethodGet, "/boletas/"+x.id, nil), http.StatusForbidden)
	// Otra empresa: ni verla (404), aunque sea supervisor.
	deOtra := b.persona([]int64{2}, rolBase("supervisor"))
	caja.vincular(deOtra.id, "Otra Caja", 1, 2)
	b.exigir(b.api(deOtra, http.MethodGet, "/boletas/"+x.id, nil), http.StatusNotFound)
	b.exigir(b.api(deOtra, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusNotFound)
	b.exigir(b.api(deOtra, http.MethodGet, fmt.Sprintf("/conversaciones/%d/boletas", x.conv), nil), http.StatusNotFound)
	// Una boleta que no existe.
	b.exigir(b.api(b.supervisor, http.MethodPost, "/boletas/"+uuid.NewString()+"/aprobar", map[string]any{}), http.StatusNotFound)
	// El rol base caja: decide solo en las conversaciones que ve (las de sus colas).
	cajera := b.persona([]int64{1}, rolBase("caja"))
	caja.vincular(cajera.id, "Marta Caja", 1)
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/tomar", x.conv), map[string]any{"version": ver(b.conv(x.numero))}), http.StatusOK)
	b.exigir(b.api(cajera, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusNotFound)
	nada("sin permiso, sin sesión de persona o sin ver la conversación")
	col := b.api(b.supervisor, http.MethodGet, "/colas", nil)
	b.exigir(col, http.StatusOK)
	var version any
	for _, c := range col.cuerpo["data"].([]any) {
		if int64(c.(map[string]any)["id"].(float64)) == b.cola {
			version = c.(map[string]any)["version"]
		}
	}
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/miembros", b.cola), map[string]any{"version": version, "usuarioIds": []int64{b.agente.id, cajera.id}}), http.StatusOK)
	r = b.api(cajera, http.MethodPost, x.ruta("aprobar"), map[string]any{})
	b.exigir(r, http.StatusOK, "estado", "aprobada", "verificadoPorNombre", "Marta Caja")
	if d := caja.decisiones(); len(d) != 1 || d[0].actor != fmt.Sprintf("usuario:%d", cajera.id) {
		t.Fatalf("decide la cajera: %+v", d)
	}
}

// Número prestado: la boleta es de la empresa que atiende la conversación.
func TestBoletaEnNumeroPrestado(t *testing.T) {
	p := nuevoPrestamo(t)
	caja := cajaDe(t, p.bandeja)
	num := numeroCliente()
	p.clienteArgos(2, "+"+num)
	x := p.boletaDe(num, 2)
	// El usuario de Argos de la prestataria sin esa empresa en su alcance: Argos lo dice.
	u := caja.vincular(p.sup2.id, "Caja Norte", 1)
	p.exigirProblema(p.api(p.sup2, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusForbidden, "argos_sin_empresa")
	// La dueña del número no ve la conversación de la otra empresa.
	caja.vincular(p.supervisor.id, "Ana Caja", 1, 2)
	p.exigir(p.api(p.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusNotFound)
	u.empresas[2] = true
	p.exigir(p.api(p.sup2, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusOK, "estado", "aprobada", "verificadoPorNombre", "Caja Norte")
	ds := caja.decisiones()
	if d := ds[len(ds)-1]; d.empresa != "2" || d.actor != fmt.Sprintf("usuario:%d", p.sup2.id) {
		t.Fatalf("la decisión va con la empresa de la conversación: %+v", d)
	}
	if a := p.avisos(x); len(a) != 1 {
		t.Fatalf("aviso por el número prestado: %v", a)
	}
	// Préstamo revocado: la conversación queda de solo lectura y la boleta se resuelve en Argos.
	y := p.boletaDe(numeroCliente(), 1) // otra conversación, de la dueña: sigue normal
	num2 := numeroCliente()
	p.clienteArgos(2, "+"+num2)
	z := p.boletaDe(num2, 2)
	l := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil)
	pr := l.cuerpo["data"].([]any)[0].(map[string]any)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos/2/revocar", p.canal.ID), map[string]any{"version": pr["version"], "motivo": "Ya tienen su propio número"}), http.StatusOK)
	antes := len(caja.decisiones())
	p.exigirProblema(p.api(p.sup2, http.MethodPost, z.ruta("aprobar"), map[string]any{}), http.StatusUnprocessableEntity, "conversacion_solo_lectura")
	if len(caja.decisiones()) != antes {
		t.Fatal("solo lectura: no llega a Argos")
	}
	// Caja la resuelve en Argos y el webhook la deja resuelta, sin escribirle al cliente por un número que ya no usa.
	p.webhookComprobante(z, "aprobado", 2, map[string]any{"pago_id": 3})
	p.despacharResoluciones(true)
	if estado, _, _, aviso := p.estadoBoleta(z); estado != "aprobada" || *aviso != "sin_aviso" || len(p.avisos(z)) != 0 {
		t.Fatalf("préstamo revocado: %s", estado)
	}
	p.exigir(p.api(p.supervisor, http.MethodPost, y.ruta("aprobar"), map[string]any{}), http.StatusOK, "estado", "aprobada")
}

// Argos anterior al contrato 0.7.0: no se ofrece ni se intenta.
func TestBoletaContratoAnterior(t *testing.T) {
	b := nuevaBandeja(t, nil)
	// Sin Argos de caja (el conector no habla 0.7.0).
	x := b.boletaRegistrada(1)
	b.exigir(b.api(b.supervisor, http.MethodGet, "/boletas/"+x.id, nil), http.StatusOK, "estado", "registrada", "puedeDecidir", "false")
	caja := cajaDe(t, b)
	caja.comprobante(x.comprobante, 1, "250.00")
	caja.vincular(b.supervisor.id, "Ana Caja", 1)
	caja.contrato = false
	b.exigir(b.api(b.supervisor, http.MethodGet, "/boletas/"+x.id, nil), http.StatusOK, "puedeDecidir", "false")
	r := b.api(b.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{})
	b.exigirProblema(r, http.StatusUnprocessableEntity, "argos_sin_aprobacion")
	b.exigirProblema(b.api(b.supervisor, http.MethodPost, x.ruta("rechazar"), map[string]any{"motivo": "no aparece"}), http.StatusUnprocessableEntity, "argos_sin_aprobacion")
	if len(caja.decisiones()) != 0 {
		t.Fatal("con contrato < 0.7.0 no se llama a Argos")
	}
	// Argos se actualiza: ya se puede.
	caja.contrato = true
	b.exigir(b.api(b.supervisor, http.MethodGet, "/boletas/"+x.id, nil), http.StatusOK, "puedeDecidir", "true")
	b.exigir(b.api(b.supervisor, http.MethodPost, x.ruta("aprobar"), map[string]any{}), http.StatusOK, "estado", "aprobada")
}
