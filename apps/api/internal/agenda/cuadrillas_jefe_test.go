package agenda_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// eventoCuadrilla: el último evento de la cuadrilla (de cualquier empresa: una cuadrilla sin empresa
// emite uno por cada empresa registrada, todos con la misma versión y los mismos usuarios).
func eventoCuadrilla(t *testing.T, evento string, cuadrilla int64) map[string]any {
	t.Helper()
	var crudo []byte
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT payload FROM integracion.outbox WHERE evento = $1 AND (payload->>'cuadrilla_id')::bigint = $2 ORDER BY id DESC LIMIT 1",
		evento, cuadrilla).Scan(&crudo); err != nil {
		t.Fatalf("%s de la cuadrilla %d: %v", evento, cuadrilla, err)
	}
	var p map[string]any
	_ = json.Unmarshal(crudo, &p)
	return p
}

// miembrosDe: usuario → id_externo (nil sin vínculo) del payload.
func miembrosDe(p map[string]any) map[string]any {
	out := map[string]any{}
	ms, _ := p["miembros"].([]any)
	for _, m := range ms {
		mm := m.(map[string]any)
		out[fmt.Sprint(mm["usuario_id"])] = mm["id_externo"]
	}
	return out
}

func versionArgos(t *testing.T, id int64) int32 {
	t.Helper()
	return int32(contar(t, "SELECT version_argos FROM agenda.cuadrillas WHERE id = $1", id))
}

func versionCuadrilla(t *testing.T, id int64) int32 {
	t.Helper()
	return int32(contar(t, "SELECT version FROM agenda.cuadrillas WHERE id = $1", id))
}

func jefeDe(t *testing.T, id int64) *int64 {
	t.Helper()
	var jefe *int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT jefe_usuario_id FROM agenda.cuadrillas WHERE id = $1", id).Scan(&jefe); err != nil {
		t.Fatal(err)
	}
	return jefe
}

func vincularI1(t *testing.T, ficha int64, idExterno string) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		`UPDATE agenda.tecnicos SET argos_id_externo = $2, argos_vinculado_en = now(), version = version + 1 WHERE id = $1`, ficha, idExterno); err != nil {
		t.Fatal(err)
	}
}

func versionFicha(t *testing.T, id int64) int32 {
	t.Helper()
	return int32(contar(t, "SELECT version FROM agenda.tecnicos WHERE id = $1", id))
}

// Contrato 0.11.0 §3.9 (B26): jefe de cuadrilla y cuadrilla.* completos hacia Argos.
func TestJefeDeCuadrillaYEventosParaArgos(t *testing.T) {
	e := nuevoEntorno()
	admin := usuarioCon(t, rol("admin"))
	cs := como(t, admin, "admin")
	sup := usuarioCon(t, rol("supervisor"))
	csup := como(t, sup, "supervisor")

	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Jefes " + pruebas.Unico()}))(t)
	p := eventoCuadrilla(t, "cuadrilla.creada", c.ID)
	if fmt.Sprint(p["version"]) != "1" || len(miembrosDe(p)) != 0 || p["jefe_usuario_id"] != nil || p["activa"] != true {
		t.Fatalf("cuadrilla.creada: %v", p)
	}
	empresas := contar(t, "SELECT count(*) FROM nucleo.empresas")
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'cuadrilla.creada' AND (payload->>'cuadrilla_id')::bigint = $1", c.ID); n != max(empresas, 1) {
		t.Fatalf("una cuadrilla sin empresa emite uno por empresa registrada (%d): %d", empresas, n)
	}

	tecA := usuarioCon(t, rol("tecnico"))
	tecB := usuarioCon(t, rol("tecnico"))
	fa := fichaCon(t, e, cs, tecA, &c.ID)
	fichaCon(t, e, cs, tecB, &c.ID)
	p = eventoCuadrilla(t, "cuadrilla.actualizada", c.ID)
	ms := miembrosDe(p)
	if len(ms) != 2 || ms[fmt.Sprint(tecA)] != nil || fmt.Sprint(p["version"]) != fmt.Sprint(versionArgos(t, c.ID)) {
		t.Fatalf("miembros completos y versión nueva al entrar una ficha: %v", p)
	}

	// Sin vínculo I1 no puede ser jefe.
	_, err := e.ag.AsignarJefe.Ejecutar(csup, agenda.EntradaAsignarJefe{ID: c.ID, Version: versionCuadrilla(t, c.ID), JefeUsuarioID: &tecA})
	problema(t, err, http.StatusUnprocessableEntity, "jefe_no_miembro")
	vincularI1(t, fa.ID, "argos-a-"+pruebas.Unico())
	// Alguien de fuera de la cuadrilla tampoco.
	_, err = e.ag.AsignarJefe.Ejecutar(csup, agenda.EntradaAsignarJefe{ID: c.ID, Version: versionCuadrilla(t, c.ID), JefeUsuarioID: &admin})
	problema(t, err, http.StatusUnprocessableEntity, "jefe_no_miembro")
	// Nadie se asigna a sí mismo, aunque tenga el permiso.
	_, err = e.ag.AsignarJefe.Ejecutar(como(t, tecA, "supervisor"), agenda.EntradaAsignarJefe{ID: c.ID, Version: versionCuadrilla(t, c.ID), JefeUsuarioID: &tecA})
	problema(t, err, http.StatusForbidden, "autoasignacion")
	// Solo admin y supervisor: el técnico, el despacho y la integración no.
	for _, r := range []string{"tecnico", "despacho", "integracion_argos"} {
		_, err = e.ag.AsignarJefe.Ejecutar(como(t, tecB, r), agenda.EntradaAsignarJefe{ID: c.ID, Version: versionCuadrilla(t, c.ID), JefeUsuarioID: &tecA})
		var pr *nucleo.Problema
		if !errors.As(err, &pr) || pr.Status != http.StatusForbidden {
			t.Fatalf("%s no asigna jefe: %v", r, err)
		}
	}
	// Con una versión vieja, 409.
	_, err = e.ag.AsignarJefe.Ejecutar(csup, agenda.EntradaAsignarJefe{ID: c.ID, Version: versionCuadrilla(t, c.ID) - 1, JefeUsuarioID: &tecA})
	problema(t, err, http.StatusConflict, "conflicto_version")

	antes := versionCuadrilla(t, c.ID)
	c2 := deber(e.ag.AsignarJefe.Ejecutar(csup, agenda.EntradaAsignarJefe{ID: c.ID, Version: antes, JefeUsuarioID: &tecA}))(t)
	if c2.JefeUsuarioID == nil || *c2.JefeUsuarioID != tecA || c2.Version != antes+1 {
		t.Fatalf("jefe asignado con versión nueva: %+v", c2)
	}
	p = eventoCuadrilla(t, "cuadrilla.actualizada", c.ID)
	if fmt.Sprint(p["jefe_usuario_id"]) != fmt.Sprint(tecA) || miembrosDe(p)[fmt.Sprint(tecA)] == nil || fmt.Sprint(p["version"]) != fmt.Sprint(versionArgos(t, c.ID)) {
		t.Fatalf("cuadrilla.actualizada con el jefe y su id_externo: %v", p)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'cuadrillas.asignar_jefe' AND entidad_id = $1::text AND resultado = 'ok'`, fmt.Sprint(c.ID)); n != 1 {
		t.Fatalf("asignar jefe se audita una vez: %d", n)
	}
	_, err = e.ag.AsignarJefe.Ejecutar(csup, agenda.EntradaAsignarJefe{ID: c.ID, Version: c2.Version, JefeUsuarioID: &tecA})
	problema(t, err, http.StatusUnprocessableEntity, "sin_cambios")

	// Pierde el vínculo I1: la cuadrilla queda sin jefe.
	d := deber(e.ag.DesvincularArgos.Ejecutar(cs, agenda.EntradaDesvincularArgos{ID: fa.ID, Version: versionFicha(t, fa.ID)}))(t)
	if jefeDe(t, c.ID) != nil {
		t.Fatal("sin vínculo I1 deja de ser jefe")
	}
	p = eventoCuadrilla(t, "cuadrilla.actualizada", c.ID)
	if p["jefe_usuario_id"] != nil || miembrosDe(p)[fmt.Sprint(tecA)] != nil {
		t.Fatalf("cuadrilla.actualizada sin jefe y sin id_externo: %v", p)
	}

	// Sale de la cuadrilla: las dos cuadrillas suben su versión y la vieja queda sin jefe.
	vincularI1(t, d.ID, "argos-a2-"+pruebas.Unico())
	deber(e.ag.AsignarJefe.Ejecutar(csup, agenda.EntradaAsignarJefe{ID: c.ID, Version: versionCuadrilla(t, c.ID), JefeUsuarioID: &tecA}))(t)
	otra := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Otra " + pruebas.Unico()}))(t)
	vc, vo := versionArgos(t, c.ID), versionArgos(t, otra.ID)
	deber(e.ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: fa.ID, Version: versionFicha(t, fa.ID), CuadrillaID: &otra.ID}))(t)
	if versionArgos(t, c.ID) != vc+1 || versionArgos(t, otra.ID) != vo+1 || jefeDe(t, c.ID) != nil {
		t.Fatal("al cambiar de cuadrilla, las dos avisan y la vieja queda sin jefe")
	}
	if versionCuadrilla(t, otra.ID) != otra.Version {
		t.Fatal("la versión del panel no cambia cuando cambian los miembros (nadie editó la cuadrilla)")
	}
	if _, sigue := miembrosDe(eventoCuadrilla(t, "cuadrilla.actualizada", c.ID))[fmt.Sprint(tecA)]; sigue {
		t.Fatal("el que salió ya no es miembro")
	}
	if miembrosDe(eventoCuadrilla(t, "cuadrilla.actualizada", otra.ID))[fmt.Sprint(tecA)] == nil {
		t.Fatal("la cuadrilla nueva lo trae con su id_externo")
	}

	// Ficha desactivada: deja de ser miembro.
	fb := deber(e.ag.ListarTecnicos.Ejecutar(cs, agenda.EntradaListarTecnicos{CuadrillaID: c.ID}))(t)
	if len(fb.Data) != 1 {
		t.Fatalf("queda un técnico: %d", len(fb.Data))
	}
	deber(e.ag.DesactivarTecnico.Ejecutar(cs, agenda.EntradaVersion{ID: fb.Data[0].ID, Version: versionFicha(t, fb.Data[0].ID)}))(t)
	if len(miembrosDe(eventoCuadrilla(t, "cuadrilla.actualizada", c.ID))) != 0 {
		t.Fatal("una ficha inactiva no es miembro")
	}
}

// Usuario desactivado: la revisión del sistema (suscriptor de usuario.desactivado) quita al jefe y
// avisa a Argos. Solo la corre el sistema.
func TestJefeDeCuadrillaUsuarioDesactivado(t *testing.T) {
	e := nuevoEntorno()
	admin := usuarioCon(t, rol("admin"))
	cs := como(t, admin, "admin")
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Baja " + pruebas.Unico()}))(t)
	tec := usuarioCon(t, rol("tecnico"))
	f := fichaCon(t, e, cs, tec, &c.ID)
	vincularI1(t, f.ID, "argos-b-"+pruebas.Unico())
	deber(e.ag.AsignarJefe.Ejecutar(cs, agenda.EntradaAsignarJefe{ID: c.ID, Version: versionCuadrilla(t, c.ID), JefeUsuarioID: &tec}))(t)

	var vu int32
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT version FROM nucleo.usuarios WHERE id = $1", tec).Scan(&vu); err != nil {
		t.Fatal(err)
	}
	deber(ident.DesactivarUsuario.Ejecutar(cs, identidad.EntradaDesactivarUsuario{ID: tec, Version: vu, Motivo: "Dejó la empresa"}))(t)

	_, err := e.ag.RevisarUsuario.Ejecutar(cs, agenda.EntradaRevisarUsuario{UsuarioID: tec})
	problema(t, err, http.StatusForbidden, "prohibido")
	v := versionArgos(t, c.ID)
	r := deber(e.ag.RevisarUsuario.Ejecutar(sistema(t), agenda.EntradaRevisarUsuario{UsuarioID: tec}))(t)
	if len(r.Cuadrillas) != 1 || r.Cuadrillas[0] != c.ID || jefeDe(t, c.ID) != nil || versionArgos(t, c.ID) != v+1 {
		t.Fatalf("revisión del usuario desactivado: %+v", r)
	}
	if len(miembrosDe(eventoCuadrilla(t, "cuadrilla.actualizada", c.ID))) != 0 {
		t.Fatal("un usuario desactivado no es miembro")
	}
	if n := contar(t, "SELECT count(*) FROM agenda.cuadrillas WHERE id = $1 AND jefe_usuario_id IS NULL", c.ID); n != 1 {
		t.Fatal("sin jefe")
	}
}

// GET /integraciones/argosisp/cuadrillas/{id}: solo con cuadrillas.leer_integracion y solo en las
// empresas del alcance del token (fuera, 404 igual que una que no existe).
func TestCuadrillaParaConciliarEnArgos(t *testing.T) {
	e := nuevoEntorno()
	ctx := context.Background()
	pool := pruebas.Pool(t)
	var e1, e2 int64 = 7101, 7102
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES ($1, 'Conciliar 1', 'argos'), ($2, 'Conciliar 2', 'argos') ON CONFLICT DO NOTHING`, e1, e2); err != nil {
		t.Fatal(err)
	}
	var c int64
	if err := pool.QueryRow(ctx, `INSERT INTO agenda.cuadrillas (empresa_id, nombre) VALUES ($1, $2) RETURNING id`, e1, "Conciliar "+pruebas.Unico()).Scan(&c); err != nil {
		t.Fatal(err)
	}
	// Las demás pruebas del paquete corren sin empresas registradas (modo sin conector).
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM nucleo.usuarios_empresas WHERE empresa_id = ANY($1)`,
			`UPDATE agenda.tecnicos SET cuadrilla_id = NULL WHERE cuadrilla_id IN (SELECT id FROM agenda.cuadrillas WHERE empresa_id = ANY($1))`,
			`DELETE FROM agenda.cuadrillas WHERE empresa_id = ANY($1)`,
			`DELETE FROM nucleo.empresas WHERE id = ANY($1)`,
		} {
			if _, err := pool.Exec(context.Background(), sql, []int64{e1, e2}); err != nil {
				t.Errorf("limpiar empresas de la prueba: %v", err)
			}
		}
	})
	tec := usuarioCon(t, rol("tecnico"))
	ext := "argos-c-" + pruebas.Unico()
	if _, err := pool.Exec(ctx, `INSERT INTO agenda.tecnicos (usuario_id, cuadrilla_id, argos_id_externo, argos_vinculado_en) VALUES ($1, $2, $3, now())`, tec, c, ext); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agenda.cuadrillas SET jefe_usuario_id = $2 WHERE id = $1`, c, tec); err != nil {
		t.Fatal(err)
	}
	argosUsr := usuarioCon(t)
	token := func(disponibles ...int64) context.Context {
		return contexto.ConEmpresas(pruebas.ContextoUsuario(pool, argosUsr, permisosDe("integracion_argos")...),
			contexto.Empresas{Estado: contexto.EmpresaPorElegir, Disponibles: disponibles})
	}

	// El técnico aún no opera en la empresa: miembro sin id_externo.
	r := deber(e.ag.CuadrillaArgos.Ejecutar(token(e1), agenda.EntradaCuadrillaArgos{ID: c, EmpresaID: e1}))(t)
	if r.CuadrillaID != c || r.EmpresaID == nil || *r.EmpresaID != e1 || len(r.Miembros) != 1 || r.Miembros[0].IDExterno != nil ||
		r.JefeUsuarioID == nil || *r.JefeUsuarioID != tec || !r.Activa {
		t.Fatalf("conciliar: %+v", r)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, $2)`, tec, e1); err != nil {
		t.Fatal(err)
	}
	r = deber(e.ag.CuadrillaArgos.Ejecutar(token(e1), agenda.EntradaCuadrillaArgos{ID: c, EmpresaID: e1}))(t)
	if r.Miembros[0].IDExterno == nil || *r.Miembros[0].IDExterno != ext {
		t.Fatalf("con la empresa asignada, su id_externo: %+v", r.Miembros)
	}

	// Fuera del alcance, de otra empresa o inexistente: el mismo 404.
	for _, caso := range []struct {
		ctx context.Context
		in  agenda.EntradaCuadrillaArgos
	}{
		{token(e2), agenda.EntradaCuadrillaArgos{ID: c, EmpresaID: e1}},
		{token(e1, e2), agenda.EntradaCuadrillaArgos{ID: c, EmpresaID: e2}},
		{token(e1), agenda.EntradaCuadrillaArgos{ID: 987654321, EmpresaID: e1}},
	} {
		_, err := e.ag.CuadrillaArgos.Ejecutar(caso.ctx, caso.in)
		d := problema(t, err, http.StatusNotFound, "no_encontrado")
		if d.Detail != "La cuadrilla no existe" {
			t.Fatalf("el mismo detalle en todos los casos: %q", d.Detail)
		}
	}
	// Sin el permiso (un técnico), 403.
	_, err := e.ag.CuadrillaArgos.Ejecutar(contexto.ConEmpresas(como(t, tec, "tecnico"), contexto.Empresas{Estado: contexto.EmpresaPorElegir, Disponibles: []int64{e1}}),
		agenda.EntradaCuadrillaArgos{ID: c, EmpresaID: e1})
	var pr *nucleo.Problema
	if !errors.As(err, &pr) || pr.Status != http.StatusForbidden {
		t.Fatalf("sin cuadrillas.leer_integracion, 403: %v", err)
	}
}
