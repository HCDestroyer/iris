package agenda_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// vinculosDe: los tecnico.argos_* del técnico en el outbox, en orden.
func vinculosDe(t *testing.T, tecnico int64) []map[string]any {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(), `SELECT evento, payload::text FROM integracion.outbox
		WHERE evento LIKE 'tecnico.argos_%' AND (payload->>'tecnico_id')::bigint = $1 ORDER BY id`, tecnico)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	out := []map[string]any{}
	for filas.Next() {
		var evento, crudo string
		if err := filas.Scan(&evento, &crudo); err != nil {
			t.Fatal(err)
		}
		var p map[string]any
		_ = json.Unmarshal([]byte(crudo), &p)
		p["evento"] = evento
		out = append(out, p)
	}
	return out
}

// Contrato 0.14.0 §6.6: el vínculo de cada técnico va a Argos con tecnico.argos_vinculado y
// tecnico.argos_desvinculado, uno por empresa del usuario, cada uno con una version nueva y el
// conector del que salió el id_externo; desactivar la ficha manda el desvinculado con motivo
// desactivado; la carga inicial marca los vinculados con su carga; y GET …/tecnicos/{usuario_id}
// solo responde al conector del vínculo, dentro de su alcance (si no, el mismo 404).
func TestVinculosDelTecnicoHaciaArgos(t *testing.T) {
	ctx := context.Background()
	pool := pruebas.Pool(t)
	var e1, e2 int64 = 7201, 7202
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES ($1, 'Vínculos 1', 'argos'), ($2, 'Vínculos 2', 'argos') ON CONFLICT DO NOTHING`, e1, e2); err != nil {
		t.Fatal(err)
	}
	argosUsr := usuarioCon(t)
	var token, conector, otroToken int64
	for _, id := range []*int64{&token, &otroToken} {
		if err := pool.QueryRow(ctx, `INSERT INTO nucleo.tokens_api (usuario_id, nombre, prefijo, hash) VALUES ($1, 'Argos', 'iris_pat_', $2) RETURNING id`,
			argosUsr, []byte(pruebas.Unico())).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO integracion.conectores (sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, estado, token_entregado_id)
		VALUES ('argosisp', 'http://argos.vinculos', 'enlace_remoto', '\x00', '\x00', 'activo', $1) RETURNING id`, token).Scan(&conector); err != nil {
		t.Fatal(err)
	}
	// Las demás pruebas del paquete corren sin empresas ni conector.
	t.Cleanup(func() {
		for _, paso := range []struct {
			sql string
			arg any
		}{
			{`UPDATE agenda.tecnicos SET argos_conector_id = NULL WHERE argos_conector_id = $1`, conector},
			{`DELETE FROM integracion.conectores WHERE id = $1`, conector},
			{`DELETE FROM nucleo.usuarios_empresas WHERE empresa_id = ANY($1)`, []int64{e1, e2}},
			{`DELETE FROM nucleo.empresas WHERE id = ANY($1)`, []int64{e1, e2}},
		} {
			if _, err := pool.Exec(context.Background(), paso.sql, paso.arg); err != nil {
				t.Errorf("limpiar: %v", err)
			}
		}
	})

	sufijo := pruebas.Unico()
	ext := "u-vinc-" + sufijo
	falso := &argosTecnicosFalso{tecnicos: map[string]agenda.TecnicoArgosVerificado{
		ext: {IDExterno: ext, Nombre: "Técnico", Bodegas: []agenda.BodegaArgosVerificada{{ID: 41, EmpresaID: 7201, Activa: true}}},
	}}
	ag := agenda.Nuevo(agenda.Config{Reloj: func() time.Time { return gt(2, 9, 0) }, Argos: falso})
	ca := contexto.ConEmpresas(como(t, usuarioCon(t, rol("admin")), "admin"),
		contexto.Empresas{Estado: contexto.EmpresaActiva, Activa: e1, Disponibles: []int64{e1, e2}})
	tec := usuarioCon(t, rol("tecnico"))
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, $2), ($1, $3)`, tec, e1, e2); err != nil {
		t.Fatal(err)
	}
	f := deber(ag.CrearTecnico.Ejecutar(ca, agenda.EntradaCrearTecnico{UsuarioID: tec}))(t)
	v := deber(ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: f.Version, IDExterno: ext}))(t)

	vs := vinculosDe(t, f.ID)
	if len(vs) != 2 {
		t.Fatalf("un vinculado por empresa: %v", vs)
	}
	for i, p := range vs {
		if p["evento"] != agenda.EventoVinculado || fmt.Sprint(p["version"]) != fmt.Sprint(i+1) || p["id_externo"] != ext ||
			fmt.Sprint(p["conector_id"]) != fmt.Sprint(conector) || fmt.Sprint(p["usuario_id"]) != fmt.Sprint(tec) {
			t.Fatalf("vinculado %d: %v", i, p)
		}
	}
	if fmt.Sprint(vs[0]["empresa_id"]) != fmt.Sprint(e1) || fmt.Sprint(vs[1]["empresa_id"]) != fmt.Sprint(e2) {
		t.Fatalf("empresas: %v", vs)
	}

	// Desactivar la ficha: desvinculado con motivo desactivado en cada empresa; activarla, de nuevo
	// el vinculado. La versión sigue creciendo.
	d := deber(ag.DesactivarTecnico.Ejecutar(ca, agenda.EntradaVersion{ID: f.ID, Version: v.Version}))(t)
	vs = vinculosDe(t, f.ID)
	if len(vs) != 4 || vs[2]["evento"] != agenda.EventoDesvinculado || vs[2]["motivo"] != agenda.MotivoDesactivado || fmt.Sprint(vs[3]["version"]) != "4" {
		t.Fatalf("desactivado: %v", vs[2:])
	}
	a := deber(ag.ActivarTecnico.Ejecutar(ca, agenda.EntradaVersion{ID: f.ID, Version: d.Version}))(t)
	if vs = vinculosDe(t, f.ID); len(vs) != 6 || vs[5]["evento"] != agenda.EventoVinculado || fmt.Sprint(vs[5]["version"]) != "6" {
		t.Fatalf("reactivado: %v", vs[4:])
	}

	// Carga inicial: un vinculado por empresa, marcados con la carga; corre como sistema.
	carga := uuid.Must(uuid.NewV7())
	n, err := ag.CargarVinculos(sistema(t), conector, carga)
	if err != nil || n != 2 {
		t.Fatalf("carga: %d %v", n, err)
	}
	if c := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'tecnico.argos_vinculado' AND payload->>'carga_id' = $1
		AND (payload->>'tecnico_id')::bigint = $2`, carga.String(), f.ID); c != 2 {
		t.Fatalf("vinculados de la carga: %d", c)
	}
	if _, err := ag.CargarVinculos(como(t, usuarioCon(t, rol("admin")), "admin"), conector, carga); err == nil {
		t.Fatal("solo el sistema carga los vínculos")
	}
	if n, err := ag.CargarVinculos(sistema(t), conector+1000, uuid.Must(uuid.NewV7())); err != nil || n != 0 {
		t.Fatalf("otro conector: %d %v", n, err)
	}

	// Conciliar: el conector del vínculo, en una empresa de su alcance.
	permisos := map[string][]contexto.Alcance{}
	for _, p := range permisosDe("integracion_argos") {
		permisos[string(p)] = []contexto.Alcance{{}}
	}
	conToken := func(tok int64, disponibles ...int64) context.Context {
		c := contexto.ConActor(pruebas.ContextoUsuario(pool, argosUsr), contexto.Actor{Tipo: contexto.ActorToken, ID: argosUsr, TokenID: tok},
			contexto.NuevosPermisos(permisos))
		return contexto.ConEmpresas(c, contexto.Empresas{Estado: contexto.EmpresaPorElegir, Disponibles: disponibles})
	}
	r := deber(ag.TecnicoArgos.Ejecutar(conToken(token, e1, e2), agenda.EntradaTecnicoArgos{UsuarioID: tec, EmpresaID: e2}))(t)
	if r.TecnicoID != f.ID || r.EmpresaID != e2 || r.IDExterno == nil || *r.IDExterno != ext || r.Version != 8 || !r.Activo {
		t.Fatalf("conciliar: %+v", r)
	}
	for _, caso := range []struct {
		nombre string
		ctx    context.Context
		in     agenda.EntradaTecnicoArgos
	}{
		{"fuera del alcance", conToken(token, e1), agenda.EntradaTecnicoArgos{UsuarioID: tec, EmpresaID: e2}},
		{"otro conector", conToken(otroToken, e1, e2), agenda.EntradaTecnicoArgos{UsuarioID: tec, EmpresaID: e1}},
		{"sin ficha", conToken(token, e1), agenda.EntradaTecnicoArgos{UsuarioID: argosUsr, EmpresaID: e1}},
	} {
		_, err := ag.TecnicoArgos.Ejecutar(caso.ctx, caso.in)
		if d := problema(t, err, http.StatusNotFound, "no_encontrado"); d.Detail != "El técnico no existe" {
			t.Fatalf("%s: el mismo 404: %q", caso.nombre, d.Detail)
		}
	}
	// Sin cuadrillas.leer_integracion: 403.
	sinPermiso := contexto.ConEmpresas(contexto.ConActor(pruebas.ContextoUsuario(pool, argosUsr),
		contexto.Actor{Tipo: contexto.ActorToken, ID: argosUsr, TokenID: token}, contexto.NuevosPermisos(map[string][]contexto.Alcance{
			string(nucleo.PermAdjuntosVerificarIntegracion): {{}}})), contexto.Empresas{Estado: contexto.EmpresaPorElegir, Disponibles: []int64{e1}})
	_, err = ag.TecnicoArgos.Ejecutar(sinPermiso, agenda.EntradaTecnicoArgos{UsuarioID: tec, EmpresaID: e1})
	problema(t, err, http.StatusForbidden, "prohibido")

	// Desvincular: activo false y sin id_externo; empresa quitada → desvinculado solo en ella.
	deber(ag.DesvincularArgos.Ejecutar(ca, agenda.EntradaDesvincularArgos{ID: f.ID, Version: a.Version}))(t)
	r = deber(ag.TecnicoArgos.Ejecutar(conToken(token, e1, e2), agenda.EntradaTecnicoArgos{UsuarioID: tec, EmpresaID: e1}))(t)
	if r.Activo || r.IDExterno != nil || r.Version != 10 {
		t.Fatalf("desvinculado: %+v", r)
	}
}

// usuario.empresa_asignada y usuario.empresa_quitada: el vinculado o el desvinculado solo en esa
// empresa; usuario.desactivado: el desvinculado con motivo desactivado.
func TestVinculoAlCambiarEmpresasDelUsuario(t *testing.T) {
	ctx := context.Background()
	pool := pruebas.Pool(t)
	var e1, e2 int64 = 7211, 7212
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES ($1, 'Vínculos 3', 'argos'), ($2, 'Vínculos 4', 'argos') ON CONFLICT DO NOTHING`, e1, e2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM nucleo.usuarios_empresas WHERE empresa_id = ANY($1)`, `DELETE FROM nucleo.empresas WHERE id = ANY($1)`} {
			if _, err := pool.Exec(context.Background(), sql, []int64{e1, e2}); err != nil {
				t.Errorf("limpiar: %v", err)
			}
		}
	})
	e := nuevoEntorno()
	tec := usuarioCon(t, rol("tecnico"))
	ext := "u-emp-" + pruebas.Unico()
	var ficha int64
	if err := pool.QueryRow(ctx, `INSERT INTO agenda.tecnicos (usuario_id, argos_id_externo, argos_vinculado_en) VALUES ($1, $2, now()) RETURNING id`,
		tec, ext).Scan(&ficha); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, $2)`, tec, e1); err != nil {
		t.Fatal(err)
	}
	revisar := func(evento string, empresa *int64) {
		t.Helper()
		in := agenda.EntradaRevisarUsuario{UsuarioID: tec, EmpresaID: empresa, Evento: evento}
		deber(e.ag.RevisarUsuario.Ejecutar(sistema(t), in))(t)
	}
	revisar("usuario.empresa_asignada", &e1)
	revisar("usuario.empresa_quitada", &e2)
	revisar("usuario.desactivado", nil)
	vs := vinculosDe(t, ficha)
	if len(vs) != 3 {
		t.Fatalf("tres eventos: %v", vs)
	}
	if vs[0]["evento"] != agenda.EventoVinculado || fmt.Sprint(vs[0]["empresa_id"]) != fmt.Sprint(e1) ||
		vs[1]["evento"] != agenda.EventoDesvinculado || fmt.Sprint(vs[1]["empresa_id"]) != fmt.Sprint(e2) || vs[1]["motivo"] != nil ||
		vs[2]["evento"] != agenda.EventoDesvinculado || vs[2]["motivo"] != agenda.MotivoDesactivado || fmt.Sprint(vs[2]["version"]) != "3" {
		t.Fatalf("eventos: %v", vs)
	}
}
