package pendientes_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

// A-271-2: el 429 demasiados_intentos de …/no-recuperado y los 409/422 de §3.10 son de negocio; un
// 429 sin ese type (límite de tasa) se reintenta.
func TestDemasiadosIntentosEsNegocio(t *testing.T) {
	if !pendientes.EsNegocio(&conector.ErrorRemoto{Status: 429, Tipo: "urn:argosisp:problema:demasiados_intentos"}) {
		t.Error("429 demasiados_intentos es de negocio")
	}
	if pendientes.EsNegocio(&conector.ErrorRemoto{Status: 429}) {
		t.Error("un 429 de límite de tasa se reintenta")
	}
	for _, s := range []int{409, 422} {
		if !pendientes.EsNegocio(&conector.ErrorRemoto{Status: s, Tipo: "urn:argosisp:problema:unidad_no_es_del_servicio"}) {
			t.Errorf("%d de §3.10 es de negocio", s)
		}
	}
}

// A-271-2: un pendiente de no recuperado que recibe 429 demasiados_intentos queda para una persona
// y no vuelve a llamar a Argos solo.
func TestNoRecuperadoConDemasiadosIntentosNoSeReintenta(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.12.0")
	tec := e.tecnico("u-29")
	retiro := nuevaOrden()
	ruta := "/servicios/54/equipos/94/no-recuperado"
	e.argos.responder(ruta, 429)
	e.argos.tipos[ruta] = "urn:argosisp:problema:demasiados_intentos"
	e.cerrar(map[string]any{
		"orden_id": retiro, "tipo": "retiro", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 54, "bodega_id": 3,
		"tecnico_id": tec, "tecnico": map[string]any{"id_externo": "u-29"},
		"equipos": []any{map[string]any{"accion": "no_recuperado", "unidad_id": 94, "motivo": "se_nego",
			"foto_ref": "0193f000-0000-7000-8000-0000000000ab", "ubicacion": map[string]any{"lat": 14.6, "lng": -90.5, "precision_m": 10},
			"fecha": "2026-11-21T16:02:00Z", "recibido_en_iris": "2026-11-21T16:40:11Z", "registrado_por": tec}},
	})
	e.procesar()
	clave := "iris:no_recuperado:" + retiro + ":94"
	if p := e.pendientes(retiro)[clave]; p.Estado != "fallido_negocio" || p.Intentos != 1 {
		t.Fatalf("429 demasiados_intentos: %+v", p)
	}
	llamadas := len(e.argos.copia())
	e.reloj = e.reloj.Add(48 * 3600e9)
	if e.procesar() != 0 || len(e.argos.copia()) != llamadas {
		t.Fatal("no se reintenta solo")
	}
}

// A-271-1: un cambio o intento sin quien lo registró queda incompleto (nunca sale con el técnico
// actual de la orden); al cerrar de nuevo con el dato se corrige y sale con ese actor.
func TestEquipoSinQuienLoRegistroQuedaIncompleto(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.12.0")
	tec := e.tecnico("u-30")
	otro := e.tecnico("u-31")
	orden := nuevaOrden()
	cierre := func(registro any) {
		eq := map[string]any{"id": "0193f000-0000-7000-8000-000000000002", "accion": "cambio", "sale_unidad_id": 814,
			"serie": "NUEVA-2", "motivo": "danado", "destino_sale": "revision"}
		if registro != nil {
			eq["registrado_por"] = registro
		}
		e.cerrar(map[string]any{
			"orden_id": orden, "tipo": "cambio_equipo", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 55,
			"tecnico_id": otro, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-31"}, "equipos": []any{eq}})
	}
	cierre(nil)
	clave := "iris:cambio:" + orden + ":814"
	if p := e.pendientes(orden)[clave]; p.Estado != "fallido_negocio" || !p.Incompleto {
		t.Fatalf("sin registrado_por: %+v", p)
	}
	cierre(tec)
	e.procesar()
	if c := e.llamadaA(clave); c.Actor != "usuario:"+strconv.FormatInt(tec, 10) {
		t.Fatalf("corregido con quien lo registró: actor=%q", c.Actor)
	}
}

// A-271-1 y B-271-5: el actor es parte de la huella (el mismo cuerpo con otro actor es un conflicto)
// y las notas del técnico nunca van en el evento de conflicto ni en la auditoría.
func TestCierreRepetidoConOtroActorYSinNotas(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.12.0")
	tec := e.tecnico("u-32")
	otro := e.tecnico("u-33")
	orden := nuevaOrden()
	cierre := func(registro int64) {
		e.cerrar(map[string]any{
			"orden_id": orden, "tipo": "retiro", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 56, "bodega_id": 3,
			"tecnico_id": tec, "tecnico": map[string]any{"id_externo": "u-32"},
			"equipos": []any{map[string]any{"accion": "no_recuperado", "unidad_id": 95, "motivo": "se_nego",
				"foto_ref": "0193f000-0000-7000-8000-0000000000ac", "ubicacion": map[string]any{"lat": 14.6, "lng": -90.5, "precision_m": 10},
				"fecha": "2026-11-21T16:02:00Z", "recibido_en_iris": "2026-11-21T16:40:11Z", "registrado_por": registro}},
		})
	}
	cierre(tec)
	e.procesar()
	clave := "iris:no_recuperado:" + orden + ":95"
	// El cuerpo que viajó llevaba la nota del técnico (se lee de la orden; aquí se simula).
	e.exec(`UPDATE integracion.pendientes SET payload = payload || '{"nota":"nota-secreta-1"}'::jsonb WHERE idempotency_key = $1`, clave)
	cierre(tec) // mismo actor, cuerpo sin la nota: conflicto, pero la nota anterior no sale
	if n := e.eventos("integracion.pendiente_conflicto", orden); n != 1 {
		t.Fatalf("conflicto por otros datos: %d", n)
	}
	e.exec(`UPDATE integracion.pendientes SET payload = payload - 'nota', conflicto = NULL, conflicto_payload = NULL WHERE idempotency_key = $1`, clave)
	cierre(tec) // mismo cuerpo y mismo actor: nada
	if n := e.eventos("integracion.pendiente_conflicto", orden); n != 1 {
		t.Fatalf("mismo cierre: %d", n)
	}
	cierre(otro) // mismo cuerpo con otro X-Iris-Actor: conflicto
	if n := e.eventos("integracion.pendiente_conflicto", orden); n != 2 {
		t.Fatalf("otro actor: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%nota-secreta%'`); n != 0 {
		t.Fatalf("la nota salió en %d eventos", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.cambios WHERE coalesce(antes::text, '') || coalesce(despues::text, '') LIKE '%nota-secreta%'`); n != 0 {
		t.Fatalf("la nota quedó en %d cambios auditados", n)
	}
	var actor int64
	if err := e.pool.QueryRow(context.Background(), `SELECT tecnico_usuario_id FROM integracion.pendientes WHERE idempotency_key = $1`, clave).
		Scan(&actor); err != nil || actor != tec {
		t.Fatalf("el pendiente hecho conserva su actor: %d %v", actor, err)
	}
}
