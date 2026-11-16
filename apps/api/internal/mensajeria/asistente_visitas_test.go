package mensajeria_test

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

func nucleoPtr[T any](v T) *T { return &v }

// El asistente agenda una visita técnica (contratos/conversaciones.md §17.10).

// visitas es la bandeja del asistente con campo y agenda, una zona con su cuadrilla y un técnico con
// turno todos los días de 08:00 a 17:00. La prueba corre un jueves a las 10:00 de Guatemala.
type visitas struct {
	*bandejaIA
	zona, zonaSinTecnicos int64
	tecnico               persona
	campo                 *campo.Modulo
}

func nuevasVisitas(t *testing.T, acciones map[string]any, cambios map[string]any) *visitas {
	var cmp *campo.Modulo
	x := nuevaBandejaIACon(t, func(c *mensajeria.Config) {
		cmp = campo.Nuevo(campo.Config{Reloj: c.Ahora, Soporte: c.Soporte, Adjuntos: campo.SinAdjuntosObligatorios{}})
		c.Campo = cmp
	})
	q := x.equipo(1)
	v := &visitas{bandejaIA: x, zona: q.zona1, zonaSinTecnicos: q.zona2, tecnico: x.persona([]int64{1}, rolBase("tecnico")), campo: cmp}
	v.conTurno(v.tecnico, q.cuadrilla1)
	c := map[string]any{"perfiles": []any{perfilCon(acciones, "alta")}, "maxTurnos": 20}
	for k, val := range cambios {
		c[k] = val
	}
	x.autonomo("siempre_primero", c)
	return v
}

// conTurno: ficha en la cuadrilla con turno de 08:00 a 17:00 todos los días.
func (v *visitas) conTurno(p persona, cuadrilla int64) {
	v.t.Helper()
	v.ficha(p.id, cuadrilla)
	if _, err := v.bd.Exec(v.ctx, `INSERT INTO agenda.turnos (tecnico_id, dia, inicio, fin)
		SELECT t.id, d, '08:00', '17:00' FROM agenda.tecnicos t, generate_series(1, 7) d WHERE t.usuario_id = $1`, p.id); err != nil {
		v.t.Fatal(err)
	}
}

// cliente: una conversación con un cliente de Argos cuyo servicio está en la zona de la prueba.
func (v *visitas) cliente() (conv, num string, cliente int64) {
	v.t.Helper()
	conv, cliente = v.conClienteArgos()
	if _, err := v.bd.Exec(v.ctx, `UPDATE integracion.argos_servicios SET zona_id = $2, direccion = '4a calle 1-23, zona 10' WHERE cliente_id = $1`, cliente, v.zona); err != nil {
		v.t.Fatal(err)
	}
	*v.reloj = v.reloj.Add(21 * time.Second)
	v.ia.decir(`{"categoria": "Planes y precios", "confianza": 0.95}`)
	return conv, v.numeroDe(conv), cliente
}

type ordenVisita struct {
	id                   int64
	numero, estado, orig string
	tecnico, ticket      *int64
	inicio               *time.Time
}

func (v *visitas) ordenes(cliente int64) []ordenVisita {
	v.t.Helper()
	filas, err := v.bd.Query(v.ctx, `SELECT id, numero, estado, origen, tecnico_id, ticket_id, programada_inicio FROM campo.ordenes WHERE cliente_id = $1 ORDER BY id`, cliente)
	if err != nil {
		v.t.Fatal(err)
	}
	defer filas.Close()
	var out []ordenVisita
	for filas.Next() {
		var o ordenVisita
		if err := filas.Scan(&o.id, &o.numero, &o.estado, &o.orig, &o.tecnico, &o.ticket, &o.inicio); err != nil {
			v.t.Fatal(err)
		}
		out = append(out, o)
	}
	return out
}

const ofrecer = `{"servicio_id": 0, "motivo": "Sin internet desde ayer"}`

// De punta a punta: ofrece ventanas, el cliente elige y la visita queda creada y programada, con su
// ticket, ligada a la conversación, con origen «asistente» y sin que nadie vea al técnico.
func TestVisitasOfreceYAgenda(t *testing.T) {
	v := nuevasVisitas(t, nil, nil)
	conv, num, cliente := v.cliente()
	v.guion(llama("ofrecer_visita", ofrecer), dice("Puedo agendarte una visita el viernes 6 de noviembre, entre las 08:00 y las 12:00, o entre las 13:00 y las 17:00. ¿Cuál prefieres?"))
	v.correr(conv)
	if a := strings.Join(v.acciones(conv), ","); a != "ofrecer_visita:ok,responder:ok" {
		t.Fatalf("ofrecer: %s", a)
	}
	// Lo que vio el modelo: tres ventanas con la fecha en hora de Guatemala, sin técnicos.
	cuerpo, _, _ := v.ia.ultimo()
	for _, s := range []string{"V1: viernes 6 de noviembre, entre las 08:00 y las 12:00", "V2: viernes 6 de noviembre, entre las 13:00 y las 17:00", "V3: sábado 7 de noviembre, entre las 08:00 y las 12:00"} {
		if !strings.Contains(cuerpo, s) {
			t.Fatalf("falta %q en lo que vio el modelo", s)
		}
	}
	if len(v.ordenes(cliente)) != 0 {
		t.Fatal("ofrecer no crea órdenes")
	}
	c := v.turno(num, "wamid.vi1", "La de la mañana, por favor", "Planes y precios", llama("agendar_visita", `{"opcion": "V1"}`),
		dice("Listo: tu visita quedó agendada para el viernes 6 de noviembre, entre las 08:00 y las 12:00, en la dirección registrada de tu servicio."))
	os := v.ordenes(cliente)
	if len(os) != 1 || os[0].estado != "programada" || os[0].orig != "asistente" || os[0].tecnico == nil || *os[0].tecnico != v.tecnico.id || os[0].ticket == nil {
		t.Fatalf("orden: %+v (acciones %v)", os, v.acciones(conv))
	}
	// Viernes 6 a las 08:00 de Guatemala = 14:00 UTC; la ventana acordada, en la cita.
	if !os[0].inicio.Equal(time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("inicio: %v", os[0].inicio)
	}
	if n := v.contar(`SELECT count(*) FROM agenda.citas WHERE orden_id = $1 AND ventana_inicio = $2 AND ventana_fin = $3`, os[0].id,
		time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 18, 0, 0, 0, time.UTC)); n != 1 {
		t.Fatal("la cita no guardó la ventana acordada")
	}
	if a := c["actividadAsistente"].(map[string]any); a["tipo"] != "visita_agendada" || !strings.Contains(fmt.Sprint(a["texto"]), os[0].numero) {
		t.Fatalf("actividad: %v", a)
	}
	ms := v.delAsistente(conv)
	if !strings.Contains(textoDe(ms[len(ms)-1]), "viernes 6 de noviembre, entre las 08:00 y las 12:00") {
		t.Fatalf("confirmación: %q", textoDe(ms[len(ms)-1]))
	}
	// Ligada a la conversación (orden y ticket), con su mensaje de sistema.
	if n := v.contar(`SELECT count(*) FROM mensajeria.conversacion_vinculos WHERE conversacion_id = $1 AND tipo = 'orden' AND entidad_id = $2`, conv, fmt.Sprint(os[0].id)); n != 1 {
		t.Fatal("la orden no quedó ligada a la conversación")
	}
	if n := v.contar(`SELECT count(*) FROM mensajeria.conversacion_vinculos WHERE conversacion_id = $1 AND tipo = 'ticket' AND entidad_id = $2`, conv, fmt.Sprint(*os[0].ticket)); n != 1 {
		t.Fatal("el ticket de la visita no quedó ligado a la conversación")
	}
	// Auditoría con el actor asistente_ia y eventos con el origen nuevo.
	for _, accion := range []string{"ordenes.crear", "ordenes.programar"} {
		if n := v.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = $1 AND actor_tipo = 'asistente_ia' AND canal = 'ia' AND resultado = 'ok' AND entidad_id = $2`,
			accion, fmt.Sprint(os[0].id)); n != 1 {
			t.Fatalf("auditoría de %s con el actor asistente_ia: %d", accion, n)
		}
	}
	if n := v.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.creada' AND (payload->>'orden_id')::bigint = $1 AND payload->>'origen' = 'asistente'`, os[0].id); n != 1 {
		t.Fatal("orden.creada con origen asistente")
	}
	if n := v.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.programada' AND (payload->>'orden_id')::bigint = $1 AND payload->>'origen' = 'programar'`, os[0].id); n != 1 {
		t.Fatal("orden.programada")
	}
	if n := v.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'asistente.accion_ejecutada' AND payload->>'conversacion_id' = $1
		AND payload->>'accion' = 'agendar_visita' AND payload->>'resultado' = 'ok' AND payload->>'entidad' = 'orden'`, conv); n != 1 {
		t.Fatal("asistente.accion_ejecutada de agendar_visita")
	}
	// Nadie ve al técnico: ni el modelo ni el cliente.
	var nombre string
	if err := v.bd.QueryRow(v.ctx, `SELECT nombre FROM nucleo.usuarios WHERE id = $1`, v.tecnico.id).Scan(&nombre); err != nil {
		t.Fatal(err)
	}
	for _, cuerpo := range v.ia.cuerposAutonomos() {
		if strings.Contains(cuerpo, nombre) || strings.Contains(cuerpo, "4a calle") {
			t.Fatal("el modelo vio el nombre del técnico o la dirección")
		}
	}
	for _, m := range v.mensajes(v.supervisor, conv) {
		if m["direccion"] == "saliente" && strings.Contains(fmt.Sprint(m["texto"]), nombre) {
			t.Fatal("el cliente recibió el nombre del técnico")
		}
	}
	// La ventana usada ya no vale: no agenda dos veces.
	v.turno(num, "wamid.vi2", "Y otra para el sábado", "Planes y precios", llama("agendar_visita", `{"opcion": "V3"}`), dice("Ya tienes tu visita agendada."))
	if len(v.ordenes(cliente)) != 1 {
		t.Fatal("agendó una segunda visita con una oferta ya usada")
	}
	// Con una visita abierta (el tope es 1), otra pasa a una persona.
	c = v.turno(num, "wamid.vi3", "Quiero otra visita", "Planes y precios", llama("ofrecer_visita", ofrecer), dice("No debería salir"))
	v.exigirPaso(c, "sin_respuesta")
	if len(v.ordenes(cliente)) != 1 {
		t.Fatal("pasó del tope de visitas abiertas")
	}
}

// ausente deja al técnico de la prueba sin agenda en ese rango.
func (v *visitas) ausente(desde, hasta time.Time) {
	v.t.Helper()
	if _, err := v.bd.Exec(v.ctx, `INSERT INTO agenda.ausencias (tecnico_id, inicio, fin, motivo) SELECT id, $2, $3, 'permiso' FROM agenda.tecnicos WHERE usuario_id = $1`,
		v.tecnico.id, desde, hasta); err != nil {
		v.t.Fatal(err)
	}
}

func prohibido(err error) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Status == http.StatusForbidden
}

// Punto 7 del líder. Con la acción apagada no hay herramientas, y el ACL niega crear y programar
// órdenes al actor asistente_ia: esos permisos solo los lleva el contexto de las herramientas de visita.
func TestVisitasApagadaElACLNiega(t *testing.T) {
	v := nuevasVisitas(t, map[string]any{"agendar_visita": false}, nil)
	conv, num, cliente := v.cliente()
	v.guion(llama("ofrecer_visita", ofrecer), llama("agendar_visita", `{"opcion": "V1"}`), dice("No puedo agendar visitas por aquí."))
	v.correr(conv)
	if a := strings.Join(v.acciones(conv), ","); a != "herramienta_no_permitida:rechazada,herramienta_no_permitida:rechazada,responder:ok" {
		t.Fatalf("con la acción apagada: %s", a)
	}
	if hs := v.herramientasOfrecidas(); strings.Contains(hs, `"ofrecer_visita"`) || strings.Contains(hs, `"agendar_visita"`) {
		t.Fatal("se ofrecieron las herramientas de visita con la acción apagada")
	}
	// El actor asistente_ia, con los permisos de su turno, no puede crear ni programar órdenes: ni por
	// la bandeja ni directo en campo.
	id, _ := strconv.ParseInt(conv, 10, 64)
	ia := nucleo.ConBD(mensajeria.CtxAsistenteIA(v.ctx, 1), v.bd)
	if _, err := v.m.Conv.CrearOrden.Ejecutar(ia, mensajeria.EntradaOrdenConv{ID: id, Tipo: "visita"}); !prohibido(err) {
		t.Fatalf("crear la orden desde la bandeja como asistente_ia: %v", err)
	}
	zona := v.zona
	if _, err := v.campo.CrearOrden.Ejecutar(ia, campo.EntradaCrearOrden{Tipo: "visita", ClienteID: &cliente, ZonaID: &zona, Direccion: nucleoPtr("Zona 10")}); !prohibido(err) {
		t.Fatalf("crear la orden en campo como asistente_ia: %v", err)
	}
	var o int64
	if err := v.bd.QueryRow(v.ctx, `INSERT INTO campo.ordenes (empresa_id, tipo_id, zona_id, direccion)
		VALUES (1, (SELECT id FROM campo.tipos_orden WHERE codigo = 'visita'), $1, 'Zona 10') RETURNING id`, v.zona).Scan(&o); err != nil {
		t.Fatal(err)
	}
	if _, err := v.campo.ProgramarOrden.Ejecutar(ia, campo.EntradaProgramarOrden{ID: o, Version: 1, TecnicoID: &v.tecnico.id,
		Inicio: time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)}); !prohibido(err) {
		t.Fatalf("programar como asistente_ia: %v", err)
	}
	if len(v.ordenes(cliente)) != 0 {
		t.Fatal("se creó una orden con la acción apagada")
	}
	// Ni el perfil de fábrica la trae encendida.
	if mensajeria.PerfilPorDefecto().Acciones.AgendarVisita {
		t.Fatal("agendar_visita debe venir apagada de fábrica")
	}
	_ = num
}

// Las reglas: servicio del cliente, verificación, mora, cobertura, ticket, límites y ajustes.
func TestVisitasReglasYAjustes(t *testing.T) {
	v := nuevasVisitas(t, nil, nil)
	// Los ajustes de fábrica y su validación (topes en el servidor).
	r := v.api(v.admin, http.MethodGet, "/asistente/ajustes", nil)
	vis := r.cuerpo["visitas"].(map[string]any)
	if fmt.Sprint(vis["tiposPermitidos"]) != "[visita]" || vis["maxAbiertasPorCliente"] != float64(1) || vis["exigeTicket"] != true || vis["permitirConMora"] != false ||
		vis["anticipacionHoras"] != float64(12) || vis["diasAdelante"] != float64(7) || vis["ofertasPorDia"] != float64(3) || vis["visitasPorDia"] != float64(1) ||
		vis["visitasPorDiaEmpresa"] != float64(20) || fmt.Sprint(vis["bloques"]) != "[map[fin:12:00 inicio:08:00] map[fin:17:00 inicio:13:00]]" {
		t.Fatalf("visitas de fábrica: %v", vis)
	}
	con := func(cambios map[string]any) map[string]any {
		m := map[string]any{}
		for k, val := range vis {
			m[k] = val
		}
		for k, val := range cambios {
			m[k] = val
		}
		return map[string]any{"visitas": m}
	}
	for _, malo := range []map[string]any{
		{"tiposPermitidos": []string{"instalacion"}},
		{"maxAbiertasPorCliente": 6},
		{"visitasPorDiaEmpresa": 501},
		{"diasAdelante": 31},
		{"bloques": []any{map[string]any{"inicio": "13:00", "fin": "12:00"}}},
		{"bloques": []any{map[string]any{"inicio": "08:00", "fin": "12:00"}, map[string]any{"inicio": "11:00", "fin": "15:00"}}},
	} {
		if rr := v.ajustes(con(malo)); rr.status != http.StatusUnprocessableEntity {
			t.Fatalf("ajuste inválido %v: %d", malo, rr.status)
		}
	}

	// Un servicio de otro cliente: nunca.
	conv, num, cliente := v.cliente()
	_, _, otro := v.cliente()
	v.guion(llama("ofrecer_visita", fmt.Sprintf(`{"servicio_id": %d, "motivo": "Sin internet"}`, otro*10)), dice("¿Para cuál de tus servicios es la visita?"))
	v.correr(conv)
	if a := v.acciones(conv); a[0] != "ofrecer_visita:rechazada" || v.contar(`SELECT count(*) FROM mensajeria.asistente_ofertas_visita WHERE conversacion_id = $1`, conv) != 0 {
		t.Fatalf("servicio de otro cliente: %v", a)
	}
	// El tope de ofertas por día (aquí 1): la segunda pasa a una persona.
	v.exigir(v.ajustes(con(map[string]any{"ofertasPorDia": 1})), http.StatusOK)
	v.turno(num, "wamid.vr1", "El de mi casa", "Planes y precios", llama("ofrecer_visita", ofrecer), dice("Tengo el viernes 6 de noviembre, entre las 08:00 y las 12:00."))
	c := v.turno(num, "wamid.vr2", "¿Y otro día?", "Planes y precios", llama("ofrecer_visita", ofrecer), dice("No debería salir"))
	v.exigirPaso(c, "sin_respuesta")
	if len(v.ordenes(cliente)) != 0 {
		t.Fatal("ofrecer no crea órdenes")
	}

	// Mora: con el servicio cortado pasa a una persona, salvo que la empresa lo permita.
	v.exigir(v.ajustes(con(map[string]any{"ofertasPorDia": 3})), http.StatusOK)
	conv2, _, cliente2 := v.cliente()
	if _, err := v.bd.Exec(v.ctx, `UPDATE integracion.argos_servicios SET estado = 'cortado' WHERE cliente_id = $1`, cliente2); err != nil {
		t.Fatal(err)
	}
	v.guion(llama("ofrecer_visita", ofrecer), dice("No debería salir"))
	v.correr(conv2)
	v.exigirPaso(v.conv(v.numeroDe(conv2)), "sin_respuesta")
	v.exigir(v.ajustes(con(map[string]any{"permitirConMora": true})), http.StatusOK)
	conv3, _, cliente3 := v.cliente()
	if _, err := v.bd.Exec(v.ctx, `UPDATE integracion.argos_servicios SET estado = 'cortado' WHERE cliente_id = $1`, cliente3); err != nil {
		t.Fatal(err)
	}
	v.guion(llama("ofrecer_visita", ofrecer), dice("Tengo el viernes 6 de noviembre, entre las 08:00 y las 12:00."))
	v.correr(conv3)
	if a := v.acciones(conv3); a[0] != "ofrecer_visita:ok" {
		t.Fatalf("con mora permitida: %v", a)
	}

	// Zona sin cobertura (nadie de la agenda la atiende): pasa a una persona.
	conv4, _, cliente4 := v.cliente()
	if _, err := v.bd.Exec(v.ctx, `UPDATE integracion.argos_servicios SET zona_id = $2 WHERE cliente_id = $1`, cliente4, v.zonaSinTecnicos); err != nil {
		t.Fatal(err)
	}
	v.guion(llama("ofrecer_visita", ofrecer), dice("No debería salir"))
	v.correr(conv4)
	v.exigirPaso(v.conv(v.numeroDe(conv4)), "sin_respuesta")
	if n := v.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'nota' AND texto LIKE '%ningún técnico de la agenda atiende la zona%'`, conv4); n != 1 {
		t.Fatal("la nota de relevo no dice que la zona no tiene cobertura")
	}

	// Sin exigir ticket: la orden nace sin ticket. Y con verificación fuerte exigida, el número no basta.
	v.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(nil, "alta", map[string]any{"dar_saldo": true, "agendar_visita": true})},
		"visitas": con(map[string]any{"exigeTicket": false, "permitirConMora": false})["visitas"]})
	conv5, num5, cliente5 := v.cliente()
	v.guion(llama("ofrecer_visita", ofrecer), dice("Para agendar una visita necesito verificar tu identidad."))
	v.correr(conv5)
	if a := v.acciones(conv5); a[0] != "ofrecer_visita:rechazada" {
		t.Fatalf("con verificación fuerte exigida y solo el número: %v", a)
	}
	v.fuerte(conv5)
	v.turno(num5, "wamid.vr5", "Ya verifiqué", "Planes y precios", llama("ofrecer_visita", ofrecer), dice("Tengo el viernes 6 de noviembre, entre las 08:00 y las 12:00."))
	v.turno(num5, "wamid.vr6", "Esa", "Planes y precios", llama("agendar_visita", `{"opcion": "V1"}`), dice("Tu visita es el viernes 6 de noviembre, entre las 08:00 y las 12:00."))
	if os := v.ordenes(cliente5); len(os) != 1 || os[0].estado != "programada" || os[0].ticket != nil {
		t.Fatalf("sin exigir ticket: %+v (acciones %v)", os, v.acciones(conv5))
	}
}

// La ventana elegida se pierde: la orden no queda huérfana. Queda creada sin programar, a la vista de
// despacho con su nota; se ofrecen ventanas nuevas y el siguiente agendar programa esa misma orden.
// Si ya no hay ventanas, pasa a una persona y la nota dice que la orden quedó sin programar.
func TestVisitasVentanaPerdida(t *testing.T) {
	v := nuevasVisitas(t, nil, nil)
	conv, num, cliente := v.cliente()
	v.guion(llama("ofrecer_visita", ofrecer), dice("Tengo el viernes 6 de noviembre, entre las 08:00 y las 12:00, o entre las 13:00 y las 17:00."))
	v.correr(conv)
	// Mientras el cliente decide, el técnico queda sin la mañana del viernes.
	v.ausente(time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 18, 0, 0, 0, time.UTC))
	v.turno(num, "wamid.vp1", "La de la mañana", "Planes y precios", llama("agendar_visita", `{"opcion": "V1"}`),
		dice("Esa ventana ya no está disponible. Tengo el viernes 6 de noviembre, entre las 13:00 y las 17:00. ¿Te sirve?"))
	os := v.ordenes(cliente)
	if len(os) != 1 || os[0].estado != "pendiente" || os[0].orig != "asistente" || os[0].tecnico != nil {
		t.Fatalf("la orden debía quedar creada sin programar: %+v (acciones %v)", os, v.acciones(conv))
	}
	if n := v.contar(`SELECT count(*) FROM campo.ordenes WHERE id = $1 AND descripcion LIKE '%Si aparece sin programar%'`, os[0].id); n != 1 {
		t.Fatal("la orden sin programar no lleva su nota para despacho")
	}
	cuerpo, _, _ := v.ia.ultimo()
	if !strings.Contains(cuerpo, "esa ventana ya no tiene espacio") || !strings.Contains(cuerpo, "V1: viernes 6 de noviembre, entre las 13:00 y las 17:00") {
		t.Fatal("no volvió a ofrecer ventanas")
	}
	// El cliente elige otra: se programa la MISMA orden.
	v.turno(num, "wamid.vp2", "Sí, esa", "Planes y precios", llama("agendar_visita", `{"opcion": "V1"}`),
		dice("Tu visita es el viernes 6 de noviembre, entre las 13:00 y las 17:00."))
	os2 := v.ordenes(cliente)
	if len(os2) != 1 || os2[0].id != os[0].id || os2[0].estado != "programada" || !os2[0].inicio.Equal(time.Date(2026, 11, 6, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("debía programar la misma orden en la ventana nueva: %+v (acciones %v)", os2, v.acciones(conv))
	}

	// Otro cliente: pierde la ventana y ya no queda ninguna. Pasa a una persona y la nota lo dice.
	conv2, num2, cliente2 := v.cliente()
	v.guion(llama("ofrecer_visita", ofrecer), dice("Tengo el sábado 7 de noviembre, entre las 08:00 y las 12:00."))
	v.correr(conv2)
	v.ausente(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	c := v.turno(num2, "wamid.vp3", "Esa", "Planes y precios", llama("agendar_visita", `{"opcion": "V1"}`), dice("No debería salir"))
	v.exigirPaso(c, "sin_respuesta")
	os3 := v.ordenes(cliente2)
	if len(os3) != 1 || os3[0].estado != "pendiente" || os3[0].orig != "asistente" {
		t.Fatalf("la orden no debe anularse en silencio: %+v", os3)
	}
	if n := v.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'nota' AND texto LIKE '%' || $2 || '%SIN PROGRAMAR%'`, conv2, os3[0].numero); n != 1 {
		t.Fatal("la nota de relevo no dice que la orden quedó sin programar")
	}
}

// Topes contra abuso: una visita por conversación y día, y el tope por empresa y día.
func TestVisitasTopes(t *testing.T) {
	v := nuevasVisitas(t, nil, nil)
	r := v.api(v.admin, http.MethodGet, "/asistente/ajustes", nil)
	vis := r.cuerpo["visitas"].(map[string]any)
	vis["visitasPorDiaEmpresa"], vis["maxAbiertasPorCliente"] = 1, 2
	v.exigir(v.ajustes(map[string]any{"visitas": vis}), http.StatusOK)
	agenda := func(wamid string) (string, string, int64) {
		conv, num, cliente := v.cliente()
		v.guion(llama("ofrecer_visita", ofrecer), dice("Tengo el viernes 6 de noviembre, entre las 08:00 y las 12:00."))
		v.correr(conv)
		v.turno(num, wamid, "Esa", "Planes y precios", llama("agendar_visita", `{"opcion": "V1"}`), dice("Tu visita es el viernes 6 de noviembre, entre las 08:00 y las 12:00."))
		return conv, num, cliente
	}
	conv, num, cliente := agenda("wamid.vt1")
	if os := v.ordenes(cliente); len(os) != 1 || os[0].estado != "programada" {
		t.Fatalf("la primera visita: %+v (%v)", os, v.acciones(conv))
	}
	// La misma conversación pide otra el mismo día (caben 2 abiertas por cliente): tope de 1 por día.
	v.turno(num, "wamid.vt2", "Quiero otra visita", "Planes y precios", llama("ofrecer_visita", ofrecer), dice("Tengo el viernes 6 de noviembre, entre las 13:00 y las 17:00."))
	c := v.turno(num, "wamid.vt3", "Esa", "Planes y precios", llama("agendar_visita", `{"opcion": "V1"}`), dice("No debería salir"))
	v.exigirPaso(c, "sin_respuesta")
	if len(v.ordenes(cliente)) != 1 {
		t.Fatal("agendó dos visitas el mismo día en la misma conversación")
	}
	// Otro cliente, el mismo día: el tope de la empresa (aquí 1) lo pasa a una persona.
	conv2, _, cliente2 := agenda("wamid.vt4")
	v.exigirPaso(v.conv(v.numeroDe(conv2)), "tope")
	if len(v.ordenes(cliente2)) != 0 {
		t.Fatal("pasó del tope de visitas por día de la empresa")
	}
}
