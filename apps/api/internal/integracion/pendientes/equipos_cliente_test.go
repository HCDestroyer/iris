package pendientes_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

func (e *entorno) contrato(version string) {
	e.t.Helper()
	var antes *string
	if err := e.pool.QueryRow(context.Background(), `SELECT contrato_remoto FROM integracion.conectores WHERE id = $1`, e.conID).Scan(&antes); err != nil {
		e.t.Fatal(err)
	}
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, e.conID, version)
	e.t.Cleanup(func() { e.exec(`UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, e.conID, antes) })
}

// Contrato 0.12.0 §3.10 (B27): con Argos ≥ 0.12.0, el cambio va en una sola llamada
// (iris:cambio:<orden>:<sale>) y el intento de recuperación a …/no-recuperado, los dos con
// X-Iris-Actor, fecha y recibido_en_iris; el consumo también lleva fecha y recibido_en_iris. Un
// retiro «perdido» de una app anterior nunca sale como pérdida: se omite con aviso.
func TestEquiposDelClienteConArgos012(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.12.0")
	tec := e.tecnico("u-27")
	actor := "usuario:" + strconv.FormatInt(tec, 10)

	cambio := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": cambio, "tipo": "cambio_equipo", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 50,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-27"},
		"equipos": []any{map[string]any{"id": "0193f000-0000-7000-8000-000000000001", "accion": "cambio", "sale_unidad_id": 812,
			"serie": "ZTEGC8A1B2C3", "motivo": "danado", "destino_sale": "revision", "cobro_propuesto": map[string]any{"motivo": "golpe"},
			"fecha": "2026-11-21T16:02:00Z", "recibido_en_iris": "2026-11-21T16:40:11Z"}},
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 3, "modalidad": "consumible_gasto",
			"fecha": "2026-11-21T16:01:00Z", "recibido_en_iris": "2026-11-21T16:40:11Z"}},
	})
	e.procesar()
	c := e.llamadaA("iris:cambio:" + cambio + ":812")
	sale, _ := c.Cuerpo["sale"].(map[string]any)
	entra, _ := c.Cuerpo["entra"].(map[string]any)
	cobro, _ := c.Cuerpo["cobro_propuesto"].(map[string]any)
	if c.Ruta != "/servicios/50/equipos/cambiar" || c.Actor != actor || sale["unidad_id"] != 812.0 || entra["serie"] != "ZTEGC8A1B2C3" ||
		c.Cuerpo["motivo"] != "danado" || c.Cuerpo["destino_sale"] != "revision" || cobro["motivo"] != "golpe" || cobro["monto"] != nil ||
		c.Cuerpo["fecha"] != "2026-11-21T16:02:00Z" || c.Cuerpo["recibido_en_iris"] != "2026-11-21T16:40:11Z" ||
		c.Cuerpo["origen_tipo"] != "iris_orden" || c.Cuerpo["tecnico"] != nil {
		t.Fatalf("cambio en una sola llamada: %s actor=%q ruta=%s", c.Crudo, c.Actor, c.Ruta)
	}
	if ps := e.pendientes(cambio); len(ps) != 2 {
		t.Fatalf("solo el cambio y el consumo (ni devolución ni asignación aparte): %+v", ps)
	}
	if co := e.llamadaA("iris:consumo:" + cambio); co.Cuerpo["fecha"] != "2026-11-21T16:01:00Z" || co.Cuerpo["recibido_en_iris"] != "2026-11-21T16:40:11Z" {
		t.Fatalf("consumo con fecha y recibido_en_iris: %s", co.Crudo)
	}

	retiro := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": retiro, "tipo": "retiro", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 51, "bodega_id": 3,
		"tecnico_id": tec, "tecnico": map[string]any{"id_externo": "u-27"},
		"equipos": []any{
			map[string]any{"accion": "no_recuperado", "unidad_id": 91, "motivo": "se_nego", "foto_ref": "0193f000-0000-7000-8000-0000000000aa",
				"ubicacion": map[string]any{"lat": 14.6, "lng": -90.5, "precision_m": 12}, "fecha": "2026-11-21T16:02:00Z", "recibido_en_iris": "2026-11-21T16:40:11Z"},
			map[string]any{"accion": "retirar", "unidad_id": 92, "serie": "VIEJA", "condicion": "perdido"},
		}})
	e.procesar()
	n := e.llamadaA("iris:no_recuperado:" + retiro + ":91")
	ub, _ := n.Cuerpo["ubicacion"].(map[string]any)
	if n.Ruta != "/servicios/51/equipos/91/no-recuperado" || n.Actor != actor || n.Cuerpo["motivo"] != "se_nego" ||
		n.Cuerpo["foto_ref"] != "0193f000-0000-7000-8000-0000000000aa" || ub["lng"] != -90.5 || n.Cuerpo["fecha"] != "2026-11-21T16:02:00Z" {
		t.Fatalf("intento de recuperación: %s actor=%q", n.Crudo, n.Actor)
	}
	if _, hay := e.pendientes(retiro)["iris:perdida:"+retiro+":92"]; hay {
		t.Fatal("desde 0.12.0 Iris nunca llama a …/perdida")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_omitido' AND payload->>'orden_id' = $1
		AND payload->>'motivo' = $2`, retiro, pendientes.MotivoPerdidaSinIntento); n != 1 {
		t.Fatalf("aviso de la pérdida sin intento: %d", n)
	}
}

// Con Argos < 0.12.0: el cambio es una devolución y una asignación (devuelve dañada salvo «funciona»)
// y el intento de recuperación queda como pérdida para una persona; nunca sale solo.
func TestEquiposDelClienteConArgosAnterior(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.11.0")
	tec := e.tecnico("u-28")
	cambio := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": cambio, "tipo": "cambio_equipo", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 52,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-28"},
		"equipos": []any{map[string]any{"accion": "cambio", "sale_unidad_id": 813, "serie": "NUEVA-1", "motivo": "danado", "destino_sale": "revision"}},
	})
	e.procesar()
	if d := e.llamadaA("iris:devolucion:" + cambio + ":813"); d.Cuerpo["condicion"] != "danada" || d.Cuerpo["tecnico"] == nil {
		t.Fatalf("devolución del cambio: %s", d.Crudo)
	}
	if a := e.llamadaA("iris:comodato:" + cambio + ":NUEVA-1"); a.Cuerpo["serie"] != "NUEVA-1" {
		t.Fatalf("asignación del cambio: %s", a.Crudo)
	}
	retiro := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": retiro, "tipo": "retiro", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 53, "bodega_id": 3,
		"tecnico_id": tec, "tecnico": map[string]any{"id_externo": "u-28"},
		"equipos": []any{map[string]any{"accion": "no_recuperado", "unidad_id": 93, "motivo": "cliente_ausente",
			"ubicacion": map[string]any{"lat": 14.6, "lng": -90.5}}},
	})
	e.procesar()
	p := e.pendientes(retiro)["iris:perdida:"+retiro+":93"]
	if p.Estado != "fallido_negocio" || p.Error != pendientes.EsperaPersonaPerdida {
		t.Fatalf("la pérdida espera a una persona: %+v", p)
	}
	for _, l := range e.argos.copia() {
		if l.Clave == "iris:perdida:"+retiro+":93" {
			t.Fatal("nunca sale sola desde el cierre")
		}
	}
}
