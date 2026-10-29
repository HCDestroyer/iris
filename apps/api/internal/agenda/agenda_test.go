package agenda_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

// Miércoles 2026-10-28, 09:00 en Guatemala (UTC−6).
var lunes = time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC) // 00:00 del lunes en Guatemala

// gt: hora local de Guatemala del día (0 = lunes 26) a la hora dada.
func gt(dia int, hora, minuto int) time.Time {
	return lunes.AddDate(0, 0, dia).Add(time.Duration(hora)*time.Hour + time.Duration(minuto)*time.Minute)
}

type reloj struct {
	mu sync.Mutex
	t  time.Time
}

func (r *reloj) ahora() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.t }

type entorno struct {
	cmp   *campo.Modulo
	ag    *agenda.Modulo
	reloj *reloj
}

func nuevoEntorno() entorno {
	r := &reloj{t: gt(2, 9, 0)}
	cmp := campo.Nuevo(campo.Config{Reloj: r.ahora, Adjuntos: campo.SinAdjuntosObligatorios{}})
	return entorno{cmp: cmp, ag: cmp.Agenda(), reloj: r}
}

var ident = identidad.Nuevo(identidad.Config{})

func usuarioCon(t *testing.T, roles ...identidad.AsignacionRol) int64 {
	t.Helper()
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Persona de agenda", Correo: pruebas.Correo("agenda"), Contrasena: "contrasena-larga-1", Roles: roles,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func rol(r string) identidad.AsignacionRol { return identidad.AsignacionRol{Rol: r} }

func permisosDe(r string) []nucleo.Permiso {
	for _, b := range nucleo.RolesBase {
		if b.Codigo == r {
			return b.PermisosDeRol()
		}
	}
	panic(r)
}

func como(t *testing.T, id int64, r string) context.Context {
	return pruebas.ContextoUsuario(pruebas.Pool(t), id, permisosDe(r)...)
}

func conAlcance(t *testing.T, id int64, mapa map[string][]contexto.Alcance) context.Context {
	ctx := pruebas.ContextoUsuario(pruebas.Pool(t), id)
	return contexto.ConActor(ctx, contexto.ActorDe(ctx), contexto.NuevosPermisos(mapa))
}

func sistema(t *testing.T) context.Context { return pruebas.ContextoSistema(pruebas.Pool(t)) }

func deber[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// detalle de un problem+json esperado (no es un error: el linter no pide revisarlo).
type detalle struct {
	Errors []nucleo.ErrorCampo
	Detail string
}

func problema(t *testing.T, err error, status int, codigo string) detalle {
	t.Helper()
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != status || p.Codigo() != codigo {
		t.Fatalf("esperaba %d %s, llegó %v", status, codigo, err)
	}
	return detalle{Errors: p.Errors, Detail: p.Detail}
}

func ptr[T any](v T) *T { return &v }

func contar(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var cuadrado = map[string]any{"type": "Polygon", "coordinates": []any{[]any{
	[]any{-90.52, 14.60}, []any{-90.50, 14.60}, []any{-90.50, 14.62}, []any{-90.52, 14.62}, []any{-90.52, 14.60}}}}

// fichaCon da ficha al usuario (en la cuadrilla) con turnos de lunes a viernes de 08:00 a 17:00.
func fichaCon(t *testing.T, e entorno, ctx context.Context, usuario int64, cuadrilla *int64) agenda.TecnicoDetalle {
	t.Helper()
	f := deber(e.ag.CrearTecnico.Ejecutar(ctx, agenda.EntradaCrearTecnico{UsuarioID: usuario, CuadrillaID: cuadrilla, BodegaID: ptr(int64(9))}))(t)
	turnos := []agenda.Turno{}
	for d := int16(1); d <= 5; d++ {
		turnos = append(turnos, agenda.Turno{Dia: d, Inicio: "08:00", Fin: "17:00"})
	}
	return deber(e.ag.FijarTurnos.Ejecutar(ctx, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: turnos}))(t)
}

func orden(t *testing.T, e entorno, ctx context.Context, tipo string, zona int64) campo.Orden {
	t.Helper()
	return deber(e.cmp.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: tipo, Direccion: ptr("Zona 10"), ZonaID: &zona}))(t)
}

func programar(e entorno, ctx context.Context, o campo.Orden, tec int64, inicio time.Time) (campo.Orden, error) {
	return e.cmp.ProgramarOrden.Ejecutar(ctx, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tec, Inicio: inicio})
}

func ultimoEvento(t *testing.T, evento string, ordenID int64) map[string]any {
	t.Helper()
	var crudo []byte
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT payload FROM integracion.outbox WHERE evento = $1 AND (payload->>'orden_id')::bigint = $2 ORDER BY id DESC LIMIT 1",
		evento, ordenID).Scan(&crudo); err != nil {
		t.Fatalf("%s de la orden %d: %v", evento, ordenID, err)
	}
	var p map[string]any
	_ = json.Unmarshal(crudo, &p)
	return p
}

func TestCatalogoZonasYCuadrillas(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")

	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "  Centro  ", Poligono: cuadrado}))(t)
	if z.ID < 1_000_000_000_000 || z.Origen != "local" || z.Nombre != "Centro" || z.Poligono["type"] != "Polygon" || z.EmpresaID != nil {
		t.Fatalf("zona local: %+v", z)
	}
	_, err := e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Mal", Poligono: map[string]any{"type": "Point", "coordinates": []any{1, 2}}})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "poligono" {
		t.Fatalf("polígono: %+v", p.Errors)
	}
	abierto := map[string]any{"type": "Polygon", "coordinates": []any{[]any{[]any{1, 1}, []any{2, 1}, []any{2, 2}, []any{1, 3}}}}
	_, err = e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Abierto", Poligono: abierto})
	problema(t, err, 422, "validacion")
	// Administrar el catálogo exige el permiso sin límite de alcance.
	limitado := conAlcance(t, sup, map[string][]contexto.Alcance{"agenda.crear_zona": {{ZonaID: z.ID}}})
	_, err = e.ag.CrearZona.Ejecutar(limitado, agenda.EntradaCrearZona{Nombre: "Otra"})
	problema(t, err, 403, "prohibido")

	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Norte", ZonaIDs: []int64{z.ID, z.ID}}))(t)
	if len(c.ZonaIDs) != 1 || c.ZonaIDs[0] != z.ID {
		t.Fatalf("cuadrilla: %+v", c)
	}
	_, err = e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "norte"})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "nombre" {
		t.Fatalf("nombre repetido: %+v", p.Errors)
	}
	_, err = e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Sur", ZonaIDs: []int64{424242}})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "zonaIds" {
		t.Fatalf("zona inexistente: %+v", p.Errors)
	}
	c = deber(e.ag.CapacidadCuadrilla.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: c.ID, Version: c.Version,
		Capacidad: []agenda.Capacidad{{Tipo: ptr("visita"), Cupos: ptr(int32(8))}, {Minutos: ptr(int32(900))}}}))(t)
	if len(c.Capacidad) != 2 {
		t.Fatalf("capacidad de la cuadrilla: %+v", c.Capacidad)
	}
	_, err = e.ag.CapacidadCuadrilla.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: c.ID, Version: c.Version,
		Capacidad: []agenda.Capacidad{{Tipo: ptr("visita"), Cupos: ptr(int32(1))}, {Tipo: ptr("visita"), Minutos: ptr(int32(60))}}})
	problema(t, err, 422, "validacion")

	// Editar: quitar el polígono; versión vieja → 409; nada que cambiar → 422.
	z2 := deber(e.ag.EditarZona.Ejecutar(cs, agenda.EntradaEditarZona{ID: z.ID, Version: z.Version, Nombre: ptr("Centro histórico"), QuitarPoligono: true}))(t)
	if z2.Poligono != nil || z2.Nombre != "Centro histórico" || z2.Version != 2 {
		t.Fatalf("editada: %+v", z2)
	}
	_, err = e.ag.EditarZona.Ejecutar(cs, agenda.EntradaEditarZona{ID: z.ID, Version: z.Version, Nombre: ptr("X")})
	problema(t, err, 409, "conflicto_version")
	_, err = e.ag.EditarZona.Ejecutar(cs, agenda.EntradaEditarZona{ID: z.ID, Version: z2.Version, Nombre: ptr("Centro histórico")})
	problema(t, err, 422, "sin_cambios")

	// Una zona inactiva no se usa en órdenes nuevas (ni en tickets).
	z3 := deber(e.ag.DesactivarZona.Ejecutar(cs, agenda.EntradaVersion{ID: z.ID, Version: z2.Version}))(t)
	desp := usuarioCon(t, rol("despacho"))
	_, err = e.cmp.CrearOrden.Ejecutar(como(t, desp, "despacho"), campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ZonaID: &z.ID})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "zonaId" {
		t.Fatalf("zona inactiva: %+v", p.Errors)
	}
	_, err = e.cmp.CrearOrden.Ejecutar(como(t, desp, "despacho"), campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), CuadrillaID: ptr(int64(987654))})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "cuadrillaId" {
		t.Fatalf("cuadrilla inexistente: %+v", p.Errors)
	}
	deber(e.ag.ActivarZona.Ejecutar(cs, agenda.EntradaVersion{ID: z.ID, Version: z3.Version}))(t)
	orden(t, e, como(t, desp, "despacho"), "visita", z.ID)
	lista := deber(e.ag.ListarZonas.Ejecutar(como(t, desp, "despacho"), agenda.EntradaListarCatalogo{}))(t)
	if len(lista.Data) == 0 {
		t.Fatal("despacho ve las zonas")
	}
}

func TestEspejoDeZonasDeArgos(t *testing.T) {
	e := nuevoEntorno()
	pool := pruebas.Pool(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { exec("DELETE FROM integracion.argos_zonas") })
	exec("INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en) VALUES (501, 7, 'Mixco', now()), (502, 7, 'Villa Nueva', now())")
	r := deber(e.ag.SincronizarZonas.Ejecutar(sistema(t), struct{}{}))(t)
	if r.Actualizadas != 2 || r.Desactivadas != 0 {
		t.Fatalf("espejo: %+v", r)
	}
	if n := contar(t, "SELECT count(*) FROM agenda.zonas WHERE id IN (501, 502) AND origen = 'argos' AND empresa_id = 7 AND activa"); n != 2 {
		t.Fatalf("zonas espejo: %d", n)
	}
	// Repetir sin cambios no toca nada (ni audita).
	if r := deber(e.ag.SincronizarZonas.Ejecutar(sistema(t), struct{}{}))(t); r.Actualizadas != 0 {
		t.Fatalf("idempotente: %+v", r)
	}
	// Renombrada en Argos, y una que ya no está.
	exec("UPDATE integracion.argos_zonas SET nombre = 'Mixco centro' WHERE id = 501")
	exec("DELETE FROM integracion.argos_zonas WHERE id = 502")
	r = deber(e.ag.SincronizarZonas.Ejecutar(sistema(t), struct{}{}))(t)
	if r.Actualizadas != 1 || r.Desactivadas != 1 {
		t.Fatalf("renombrada y desactivada: %+v", r)
	}
	// El nombre de una zona de Argos no se edita en Iris; su polígono sí.
	var version int32
	_ = pool.QueryRow(ctx, "SELECT version FROM agenda.zonas WHERE id = 501").Scan(&version)
	_, err := e.ag.EditarZona.Ejecutar(sistema(t), agenda.EntradaEditarZona{ID: 501, Version: version, Nombre: ptr("Otro")})
	problema(t, err, 422, "validacion")
	z := deber(e.ag.EditarZona.Ejecutar(sistema(t), agenda.EntradaEditarZona{ID: 501, Version: version, Poligono: cuadrado}))(t)
	if z.Nombre != "Mixco centro" || z.Poligono == nil {
		t.Fatalf("polígono propio en una zona de Argos: %+v", z)
	}
	// Un dato de Argos con una zona que el catálogo aún no trajo crea el espejo «Zona N».
	id, err := agenda.ZonaDeArgos(ctx, pool, ptr(int64(503)), ptr(int64(7)))
	if err != nil || id == nil || *id != 503 || contar(t, "SELECT count(*) FROM agenda.zonas WHERE id = 503 AND nombre = 'Zona 503' AND empresa_id = 7") != 1 {
		t.Fatalf("espejo al vuelo: %v %v", id, err)
	}
	// De otra empresa: no se mezcla.
	if id, err := agenda.ZonaDeArgos(ctx, pool, ptr(int64(503)), ptr(int64(8))); err != nil || id != nil {
		t.Fatalf("zona de otra empresa: %v %v", id, err)
	}
	if err := agenda.ValidarZona(ctx, pool, "zonaId", 503, ptr(int64(8))); err == nil {
		t.Fatal("ValidarZona con otra empresa")
	}

	// Argos 1.3.0: una zona sin empresa sugerida (empresa_id NULL; antes 0, que nadie veía) la
	// usan todas las empresas, también con empresas registradas.
	var creada int64
	if err := pool.QueryRow(ctx, "INSERT INTO nucleo.empresas (id, nombre) VALUES (7, 'Empresa 7') ON CONFLICT (id) DO NOTHING RETURNING id").Scan(&creada); err == nil {
		t.Cleanup(func() { exec("DELETE FROM nucleo.empresas WHERE id = 7") })
	}
	exec("INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en) VALUES (504, NULL, 'Compartida', now()), (505, NULL, 'Otra compartida', now())")
	deber(e.ag.SincronizarZonas.Ejecutar(sistema(t), struct{}{}))(t)
	if n := contar(t, "SELECT count(*) FROM agenda.zonas WHERE id IN (504, 505) AND empresa_id IS NULL AND activa"); n != 2 {
		t.Fatalf("zonas sin empresa en el espejo: %d", n)
	}
	if err := agenda.ValidarZona(ctx, pool, "zonaId", 504, ptr(int64(8))); err != nil {
		t.Fatalf("una zona de Argos sin empresa sirve a cualquiera: %v", err)
	}
	// Una que desaparece del catálogo se desactiva aunque no tenga empresa.
	exec("DELETE FROM integracion.argos_zonas WHERE id = 504")
	if r := deber(e.ag.SincronizarZonas.Ejecutar(sistema(t), struct{}{}))(t); r.Desactivadas != 1 {
		t.Fatalf("desactivar una zona sin empresa: %+v", r)
	}
	// El espejo al vuelo de una zona sin empresa queda sin empresa (no de la del dato).
	exec("INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en) VALUES (506, NULL, 'Nueva compartida', now())")
	if id, err := agenda.ZonaDeArgos(ctx, pool, ptr(int64(506)), ptr(int64(8))); err != nil || id == nil ||
		contar(t, "SELECT count(*) FROM agenda.zonas WHERE id = 506 AND empresa_id IS NULL") != 1 {
		t.Fatalf("espejo al vuelo sin empresa: %v %v", id, err)
	}
}

func TestFichaTurnosAusenciasYAlcance(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	ca := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla A " + pruebas.Unico()}))(t)
	cb := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla B " + pruebas.Unico()}))(t)

	tec := usuarioCon(t, rol("tecnico"))
	f := deber(e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec, CuadrillaID: &ca.ID, Habilidades: []string{"Fibra", "fibra", "altura"}}))(t)
	if len(f.Habilidades) != 2 || f.Habilidades[0] != "altura" || len(f.Turnos) != 0 || !f.Activo || f.UsuarioID != tec {
		t.Fatalf("ficha: %+v", f)
	}
	_, err := e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec})
	problema(t, err, 422, "ficha_existente")
	ventas := usuarioCon(t, rol("ventas"))
	_, err = e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: ventas})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "usuarioId" {
		t.Fatalf("sin permisos de órdenes: %+v", p.Errors)
	}

	// Turnos que se cruzan → 422; los válidos reemplazan.
	_, err = e.ag.FijarTurnos.Ejecutar(cs, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: []agenda.Turno{
		{Dia: 1, Inicio: "08:00", Fin: "12:00"}, {Dia: 1, Inicio: "11:00", Fin: "15:00"}}})
	problema(t, err, 422, "validacion")
	_, err = e.ag.FijarTurnos.Ejecutar(cs, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: []agenda.Turno{{Dia: 2, Inicio: "18:00", Fin: "08:00"}}})
	problema(t, err, 422, "validacion")
	f = deber(e.ag.FijarTurnos.Ejecutar(cs, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: []agenda.Turno{
		{Dia: 1, Inicio: "13:00", Fin: "24:00"}, {Dia: 1, Inicio: "08:00", Fin: "12:00"}}}))(t)
	if len(f.Turnos) != 2 || f.Turnos[0].Inicio != "08:00" || f.Turnos[1].Fin != "24:00" || f.Version != 2 {
		t.Fatalf("turnos: %+v", f)
	}

	// Despacho limitado a la cuadrilla A: ve y fija turnos a sus técnicos; a los de B ni los ve.
	tecB := usuarioCon(t, rol("tecnico"))
	fb := deber(e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tecB, CuadrillaID: &cb.ID}))(t)
	desp := usuarioCon(t, rol("despacho"))
	limitado := conAlcance(t, desp, map[string][]contexto.Alcance{
		"agenda.ver": {{CuadrillaID: ca.ID}}, "agenda.fijar_turnos": {{CuadrillaID: ca.ID}},
		"agenda.registrar_ausencia": {{CuadrillaID: ca.ID}}, "agenda.editar_tecnico": {{CuadrillaID: ca.ID}},
	})
	_, err = e.ag.VerTecnico.Ejecutar(limitado, agenda.EntradaVerTecnico{ID: fb.ID})
	problema(t, err, 404, "no_encontrado")
	lista := deber(e.ag.ListarTecnicos.Ejecutar(limitado, agenda.EntradaListarTecnicos{}))(t)
	if len(lista.Data) != 1 || lista.Data[0].ID != f.ID {
		t.Fatalf("lista con alcance: %+v", lista.Data)
	}
	deber(e.ag.FijarTurnos.Ejecutar(limitado, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: []agenda.Turno{{Dia: 3, Inicio: "07:00", Fin: "15:00"}}}))(t)
	// Pasar su técnico a la cuadrilla B está fuera de su alcance.
	actual := deber(e.ag.VerTecnico.Ejecutar(limitado, agenda.EntradaVerTecnico{ID: f.ID}))(t)
	_, err = e.ag.EditarTecnico.Ejecutar(limitado, agenda.EntradaEditarTecnico{ID: f.ID, Version: actual.Version, CuadrillaID: &cb.ID})
	problema(t, err, 403, "fuera_de_alcance")

	// Ausencia: devuelve las citas que quedan dentro; anularla la deja en el historial.
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona ausencias " + pruebas.Unico()}))(t)
	o := orden(t, e, como(t, desp, "despacho"), "visita", z.ID)
	deber(programar(e, como(t, desp, "despacho"), o, tec, gt(2, 10, 0)))(t)
	r := deber(e.ag.RegistrarAusencia.Ejecutar(limitado, agenda.EntradaRegistrarAusencia{ID: f.ID,
		Inicio: gt(2, 0, 0).Format(time.RFC3339), Fin: gt(3, 0, 0).Format(time.RFC3339), Motivo: "enfermedad"}))(t)
	if len(r.CitasEnConflicto) != 1 || r.CitasEnConflicto[0].OrdenID != o.ID {
		t.Fatalf("citas en conflicto: %+v", r)
	}
	_, err = e.ag.RegistrarAusencia.Ejecutar(limitado, agenda.EntradaRegistrarAusencia{ID: f.ID,
		Inicio: gt(3, 0, 0).Format(time.RFC3339), Fin: gt(2, 0, 0).Format(time.RFC3339), Motivo: "otro"})
	problema(t, err, 422, "validacion")
	a := deber(e.ag.AnularAusencia.Ejecutar(cs, agenda.EntradaAnularAusencia{TecnicoID: f.ID, AusenciaID: r.Ausencia.ID, Version: 1}))(t)
	if a.AnuladaEn == nil {
		t.Fatalf("anulada: %+v", a)
	}
	_, err = e.ag.AnularAusencia.Ejecutar(cs, agenda.EntradaAnularAusencia{TecnicoID: f.ID, AusenciaID: r.Ausencia.ID, Version: a.Version})
	problema(t, err, 422, "sin_cambios")
	_, err = e.ag.AnularAusencia.Ejecutar(cs, agenda.EntradaAnularAusencia{TecnicoID: fb.ID, AusenciaID: r.Ausencia.ID, Version: a.Version})
	problema(t, err, 404, "no_encontrado")
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento IN ('tecnico.ausencia_registrada', 'tecnico.ausencia_anulada') AND (payload->>'ausencia_id')::bigint = $1", r.Ausencia.ID); n != 2 {
		t.Fatalf("eventos de la ausencia: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'agenda.fijar_turnos' AND resultado = 'ok' AND entidad_id = $1::bigint::text", f.ID); n != 2 {
		t.Fatalf("auditoría de turnos: %d", n)
	}
}
