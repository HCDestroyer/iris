package pendientes_test

import (
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

// Contrato 0.14.0 §3.9 (T-5): con un Argos ≥ 0.14.0, un consumo por actor y bodega con
// iris:consumo:<orden>:<usuario_id>:<bodega_id>; lo de la app con el I1 y el X-Iris-Actor de su
// usuario, lo del panel solo con bodega_id. Dos técnicos en una orden ya no la dejan incompleta.
func TestConsumoPorActorYBodega014(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	a, b := e.tecnico("u-41"), e.tecnico("u-42")
	despacho := e.tecnico("")
	orden := nuevaOrden()
	linea := func(producto, bodega, quien int64, app bool) map[string]any {
		l := map[string]any{"producto_id": producto, "cantidad": 1, "bodega_id": bodega, "modalidad": "consumible_gasto",
			"desde_app": app, "usuario_id": quien, "fecha": "2026-11-21T16:0" + strconv.FormatInt(producto, 10) + ":00Z",
			"recibido_en_iris": "2026-11-21T16:40:1" + strconv.FormatInt(producto, 10) + "Z"}
		if !app {
			l["bodega_tipo"] = "bodega"
		}
		return l
	}
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": b, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-42"},
		"materiales": []any{linea(5, 3, a, true), linea(6, 4, a, true), linea(7, 3, b, true), linea(8, 9, despacho, false)},
	})
	e.procesar()
	clave := func(u, bodega int64) string {
		return "iris:consumo:" + orden + ":" + strconv.FormatInt(u, 10) + ":" + strconv.FormatInt(bodega, 10)
	}
	for _, c := range []struct {
		usuario, bodega int64
		idExterno       string
	}{{a, 3, "u-41"}, {a, 4, "u-41"}, {b, 3, "u-42"}} {
		l := e.llamadaA(clave(c.usuario, c.bodega))
		tec, _ := l.Cuerpo["tecnico"].(map[string]any)
		if l.Cuerpo["bodega_id"] != float64(c.bodega) || tec["id_externo"] != c.idExterno || l.Actor != "usuario:"+strconv.FormatInt(c.usuario, 10) {
			t.Fatalf("consumo de %d en %d: %s actor=%q", c.usuario, c.bodega, l.Crudo, l.Actor)
		}
	}
	// M-6: cada grupo lleva la fecha más reciente de sus líneas y su recibido_en_iris.
	if l := e.llamadaA(clave(a, 3)); l.Cuerpo["fecha"] != "2026-11-21T16:05:00Z" || l.Cuerpo["recibido_en_iris"] != "2026-11-21T16:40:15Z" {
		t.Fatalf("fecha del grupo: %s", l.Crudo)
	}
	if l := e.llamadaA(clave(b, 3)); l.Cuerpo["fecha"] != "2026-11-21T16:07:00Z" {
		t.Fatalf("fecha del otro grupo: %s", l.Crudo)
	}
	if l := e.llamadaA(clave(despacho, 9)); l.Cuerpo["tecnico"] != nil || l.Actor != "" || l.Cuerpo["bodega_id"] != 9.0 {
		t.Fatalf("consumo del panel: %s actor=%q", l.Crudo, l.Actor)
	}
	if _, viejo := e.pendientes(orden)["iris:consumo:"+orden]; viejo {
		t.Fatal("con 0.14.0 no hay consumo por orden")
	}
}

// Contrato 0.14.0 §3.10 y §4.5: con un Argos ≥ 0.14.0 las claves de cambios e intentos llevan el id
// de la operación; un intento sin foto (o un cambio con cobro sin foto) no sale: queda para una
// persona; lo que ya se encoló al aplicarse no se vuelve a encolar al cerrar.
func TestClavesPorOperacion014(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	tec := e.tecnico("u-43")
	orden := nuevaOrden()
	intento := func(id string, unidad int64, foto string) map[string]any {
		eq := map[string]any{"id": id, "accion": "no_recuperado", "unidad_id": unidad, "motivo": "cliente_ausente",
			"ubicacion": map[string]any{"lat": 14.6, "lng": -90.5, "precision_m": 10}, "usuario_id": tec,
			"fecha": "2026-11-21T16:02:00Z", "recibido_en_iris": "2026-11-21T16:40:11Z"}
		if foto != "" {
			eq["foto_ref"] = foto
		}
		return eq
	}
	const (
		conFoto  = "0193f000-0000-7000-8000-000000000101"
		sinFoto  = "0193f000-0000-7000-8000-000000000102"
		aplicado = "0193f000-0000-7000-8000-000000000103"
	)
	ya := intento(aplicado, 98, "0193f000-0000-7000-8000-0000000001ff")
	ya["enviado_al_aplicar"] = true
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "retiro", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 57, "bodega_id": 3,
		"tecnico_id": tec, "tecnico": map[string]any{"id_externo": "u-43"},
		"equipos": []any{intento(conFoto, 96, "0193f000-0000-7000-8000-0000000001aa"), intento(sinFoto, 97, ""), ya},
	})
	ps := e.pendientes(orden)
	if _, ok := ps["iris:no_recuperado:"+orden+":96:"+conFoto]; !ok {
		t.Fatalf("clave con el intento_id: %v", ps)
	}
	if p := ps["iris:no_recuperado:"+orden+":97:"+sinFoto]; p.Estado != "fallido_negocio" || p.Error != pendientes.EsperaPersonaSinFoto {
		t.Fatalf("intento sin foto: %+v", p)
	}
	for clave := range ps {
		if clave == "iris:no_recuperado:"+orden+":98:"+aplicado || clave == "iris:no_recuperado:"+orden+":98" {
			t.Fatal("lo encolado al aplicarse no se repite al cerrar")
		}
	}

	cambio := nuevaOrden()
	op := "0193f000-0000-7000-8000-000000000104"
	e.cerrar(map[string]any{
		"orden_id": cambio, "tipo": "cambio_equipo", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 58,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-43"},
		"equipos": []any{map[string]any{"id": op, "accion": "cambio", "sale_unidad_id": 815, "serie": "NUEVA-3", "motivo": "danado",
			"destino_sale": "revision", "cobro_propuesto": map[string]any{"motivo": "golpe"}, "usuario_id": tec}},
	})
	if p := e.pendientes(cambio)["iris:cambio:"+cambio+":815:"+op]; p.Estado != "fallido_negocio" || p.Error != pendientes.EsperaPersonaSinFoto {
		t.Fatalf("cambio con cobro sin foto: %+v", p)
	}
}
