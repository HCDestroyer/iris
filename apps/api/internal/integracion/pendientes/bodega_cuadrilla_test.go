package pendientes_test

import (
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

// Contrato 0.11.0 §3.9 y 0.13.1 (B26): el consumo que el técnico registró en la app (de la bodega de
// su cuadrilla o de cualquier otra) lleva tecnico (el I1 de quien lo registró) y bodega_id, con su
// X-Iris-Actor, aunque la orden se haya reasignado; el comodato, tecnico sin bodega_id.
func TestConsumoDesdeLaBodegaDeLaCuadrilla(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.13.1' WHERE id = $1`, e.conID)
	t.Cleanup(func() { e.exec(`UPDATE integracion.conectores SET contrato_remoto = NULL WHERE id = $1`, e.conID) })
	tec := e.tecnico("u-26")
	actor := "usuario:" + strconv.FormatInt(tec, 10)
	e.exec(`INSERT INTO integracion.argos_bodegas_cuadrilla (bodega_id, empresa_id, cuadrilla_id) VALUES (8126, 1, 4242)
		ON CONFLICT (bodega_id) DO NOTHING`)
	desdeApp := func(producto, bodega int64, quien any) map[string]any {
		return map[string]any{"producto_id": producto, "cantidad": 1, "bodega_id": bodega, "modalidad": "consumible_gasto",
			"desde_app": true, "registrado_por": quien}
	}

	orden := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-26"},
		"equipos":    []any{map[string]any{"accion": "instalar", "serie": "ONT-26", "bodega_id": 3, "desde_app": true, "registrado_por": tec}},
		"materiales": []any{desdeApp(5, 8126, tec)},
	})
	e.procesar()
	c := e.llamadaA("iris:consumo:" + orden)
	if c.Cuerpo["bodega_id"] != 8126.0 || c.Cuerpo["tecnico"].(map[string]any)["id_externo"] != "u-26" || c.Actor != actor {
		t.Fatalf("consumo desde la cuadrilla: %s actor=%q", c.Crudo, c.Actor)
	}
	if com := e.llamadaA("iris:comodato:" + orden + ":ONT-26"); com.Cuerpo["bodega_id"] != nil || com.Cuerpo["tecnico"] == nil || com.Actor != actor {
		t.Fatalf("comodato sin bodega_id y con actor: %s actor=%q", com.Crudo, com.Actor)
	}

	// La orden se reasignó a otro técnico antes del cierre: el consumo sale con el I1 y el actor de
	// quien registró el material (si no, Argos respondería 403 actor_no_coincide).
	otro := e.tecnico("u-26b")
	reasignada := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": reasignada, "tipo": "instalacion", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": otro, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-26b"},
		"materiales": []any{desdeApp(5, 3, tec)},
		"equipos":    []any{map[string]any{"accion": "instalar", "serie": "ONT-27", "desde_app": true, "registrado_por": tec}},
	})
	e.procesar()
	c = e.llamadaA("iris:consumo:" + reasignada)
	if c.Cuerpo["bodega_id"] != 3.0 || c.Cuerpo["tecnico"].(map[string]any)["id_externo"] != "u-26" || c.Actor != actor {
		t.Fatalf("consumo de una orden reasignada: %s actor=%q", c.Crudo, c.Actor)
	}
	// El comodato que registró en la app también sale con su I1 y su X-Iris-Actor (0.13.1 §3.9).
	if com := e.llamadaA("iris:comodato:" + reasignada + ":ONT-27"); com.Cuerpo["tecnico"].(map[string]any)["id_externo"] != "u-26" ||
		com.Actor != actor || com.Cuerpo["bodega_id"] != nil {
		t.Fatalf("comodato de una orden reasignada: %s actor=%q", com.Crudo, com.Actor)
	}

	// Desde el panel: solo bodega_id de una bodega tipo bodega, sin tecnico ni X-Iris-Actor.
	panel := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": panel, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-26"},
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 3, "modalidad": "consumible_gasto", "bodega_tipo": "bodega"}},
	})
	e.procesar()
	c = e.llamadaA("iris:consumo:" + panel)
	if c.Cuerpo["bodega_id"] != 3.0 || c.Cuerpo["tecnico"] != nil || c.Actor != "" {
		t.Fatalf("consumo del panel: %s actor=%q", c.Crudo, c.Actor)
	}

	// Nace incompleto (nunca a nombre de uno solo ni desde otra bodega por cuenta de Iris): dos
	// bodegas; dos técnicos; una bodega de cuadrilla desde el panel; quien lo registró sin I1.
	sinVinculo := e.tecnico("")
	for nombre, materiales := range map[string][]any{
		"dos bodegas":       {desdeApp(5, 8126, tec), desdeApp(7, 3, tec)},
		"dos técnicos":      {desdeApp(5, 3, tec), desdeApp(7, 3, otro)},
		"cuadrilla y panel": {map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 8126, "modalidad": "consumible_gasto"}},
		"sin vínculo":       {desdeApp(5, 3, sinVinculo)},
	} {
		mezcla := nuevaOrden()
		e.cerrar(map[string]any{
			"orden_id": mezcla, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
			"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-26"}, "materiales": materiales,
		})
		if p := e.pendientes(mezcla)["iris:consumo:"+mezcla]; p.Estado != "fallido_negocio" || !p.Incompleto {
			t.Fatalf("%s: %+v", nombre, p)
		}
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

// M-5: un comodato o una devolución registrados desde el panel nunca llevan tecnico ni el técnico
// de la orden como X-Iris-Actor. Con Argos ≥ 0.11.0 esperan a una persona; con uno anterior, la
// devolución sale con bodega_id y el comodato sin tecnico.
func TestEquiposDelPanelSinTecnicoDeLaOrden(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	tec := e.tecnico("u-48")
	cierre := func(orden string) {
		e.cerrar(map[string]any{
			"orden_id": orden, "tipo": "cambio_equipo", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
			"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-48"},
			"equipos": []any{
				map[string]any{"accion": "retirar", "unidad_id": 61, "condicion": "buena"},
				map[string]any{"accion": "instalar", "serie": "ONT-48"},
			},
		})
	}
	viejo := nuevaOrden()
	cierre(viejo)
	e.procesar()
	if d := e.llamadaA("iris:devolucion:" + viejo + ":61"); d.Cuerpo["tecnico"] != nil || d.Cuerpo["bodega_id"] != 3.0 || d.Actor != "" {
		t.Fatalf("devolución del panel con Argos anterior: %s actor=%q", d.Crudo, d.Actor)
	}
	if c := e.llamadaA("iris:comodato:" + viejo + ":ONT-48"); c.Cuerpo["tecnico"] != nil || c.Actor != "" {
		t.Fatalf("comodato del panel con Argos anterior: %s actor=%q", c.Crudo, c.Actor)
	}

	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.11.0' WHERE id = $1`, e.conID)
	t.Cleanup(func() { e.exec(`UPDATE integracion.conectores SET contrato_remoto = NULL WHERE id = $1`, e.conID) })
	nuevo := nuevaOrden()
	cierre(nuevo)
	for clave, p := range e.pendientes(nuevo) {
		if p.Estado != "fallido_negocio" || p.Error != pendientes.EsperaPersonaEquipoPanel {
			t.Fatalf("%s del panel con Argos ≥ 0.11.0: %+v", clave, p)
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND tecnico_usuario_id IS NOT NULL`, nuevo); n != 0 {
		t.Fatalf("sin el técnico de la orden como actor: %d", n)
	}
}
