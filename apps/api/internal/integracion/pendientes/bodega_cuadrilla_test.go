package pendientes_test

import (
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

// Contrato 0.11.0 §3.9 (B26): el consumo desde la bodega de la cuadrilla (la que eligió el técnico en
// la app) lleva tecnico y bodega_id a la vez, con X-Iris-Actor del técnico; el comodato, tecnico sin
// bodega_id y también con X-Iris-Actor. El consumo desde la suya sigue solo con tecnico.
func TestConsumoDesdeLaBodegaDeLaCuadrilla(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	tec := e.tecnico("u-26")
	actor := "usuario:" + strconv.FormatInt(tec, 10)
	e.exec(`INSERT INTO integracion.argos_bodegas_cuadrilla (bodega_id, empresa_id, cuadrilla_id) VALUES (8126, 1, 4242)
		ON CONFLICT (bodega_id) DO NOTHING`)

	orden := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-26"},
		"equipos":    []any{map[string]any{"accion": "instalar", "serie": "ONT-26", "bodega_id": 3}},
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 2, "bodega_id": 8126, "modalidad": "consumible_gasto"}},
	})
	e.procesar()
	c := e.llamadaA("iris:consumo:" + orden)
	if c.Cuerpo["bodega_id"] != 8126.0 || c.Cuerpo["tecnico"].(map[string]any)["id_externo"] != "u-26" || c.Actor != actor {
		t.Fatalf("consumo desde la cuadrilla: %s actor=%q", c.Crudo, c.Actor)
	}
	if com := e.llamadaA("iris:comodato:" + orden + ":ONT-26"); com.Cuerpo["bodega_id"] != nil || com.Cuerpo["tecnico"] == nil || com.Actor != actor {
		t.Fatalf("comodato sin bodega_id y con actor: %s actor=%q", com.Crudo, com.Actor)
	}

	// Desde la suya (la de la orden): solo tecnico, como en 0.5.0, y con el actor.
	otra := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": otra, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-26"},
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 3, "modalidad": "consumible_gasto"}},
	})
	e.procesar()
	c = e.llamadaA("iris:consumo:" + otra)
	if _, conBodega := c.Cuerpo["bodega_id"]; conBodega || c.Cuerpo["tecnico"] == nil || c.Actor != actor {
		t.Fatalf("consumo desde la suya: %s actor=%q", c.Crudo, c.Actor)
	}

	// Mezclar la de la cuadrilla con la suya en una orden: un consumo por orden, nace incompleto
	// (nunca se manda todo desde una sola bodega por cuenta de Iris).
	mezcla := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": mezcla, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-26"},
		"materiales": []any{
			map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 8126, "modalidad": "consumible_gasto"},
			map[string]any{"producto_id": 7, "cantidad": 1, "bodega_id": 3, "modalidad": "consumible_gasto"},
		},
	})
	if p := e.pendientes(mezcla)["iris:consumo:"+mezcla]; p.Estado != "fallido_negocio" {
		t.Fatalf("mezcla de bodegas: %+v", p)
	}
}

// B-268-4: un 403 de Argos con un type de negocio (bodega, actor, jefe, empresa o asistente) no se
// reintenta solo; un 403 sin type (permiso del token) sí.
func TestProhibidosDeNegocioNoSeReintentan(t *testing.T) {
	for _, tipo := range []string{"no_es_miembro", "actor_no_coincide", "solo_jefe_recibe", "empresa_no_permitida", "actor_ia_no_permitido"} {
		if !pendientes.EsNegocio(&conector.ErrorRemoto{Status: 403, Tipo: "urn:argosisp:problema:" + tipo}) {
			t.Errorf("403 %s es de negocio", tipo)
		}
	}
	if pendientes.EsNegocio(&conector.ErrorRemoto{Status: 403}) || pendientes.EsNegocio(&conector.ErrorRemoto{Status: 403, Tipo: "urn:argosisp:problema:prohibido"}) {
		t.Error("un 403 de permiso del token se reintenta")
	}
}

// M-268-1: con Argos ≥ 0.11.0, el material que registró el técnico desde la app (aunque sea de su
// bodega personal) sale con tecnico, bodega_id y X-Iris-Actor de quien lo registró.
func TestConsumoDeLaAppConActorDeQuienLoRegistro(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.11.0' WHERE id = $1`, e.conID)
	t.Cleanup(func() { e.exec(`UPDATE integracion.conectores SET contrato_remoto = NULL WHERE id = $1`, e.conID) })
	tec := e.tecnico("u-29")
	orden := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-29"},
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 4, "modalidad": "consumible_gasto",
			"desde_app": true, "registrado_por": tec}},
	})
	e.procesar()
	c := e.llamadaA("iris:consumo:" + orden)
	if c.Cuerpo["bodega_id"] != 4.0 || c.Cuerpo["tecnico"] == nil || c.Actor != "usuario:"+strconv.FormatInt(tec, 10) {
		t.Fatalf("consumo de la app: %s actor=%q", c.Crudo, c.Actor)
	}
}
