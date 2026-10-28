package campo_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

// reloj controlable para el módulo.
type reloj struct {
	mu sync.Mutex
	t  time.Time
}

func (r *reloj) ahora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *reloj) fijar(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = t
}

func (r *reloj) avanzar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

// adjuntosFalsos: el módulo de adjuntos de I1 simulado (orden → faltantes; adjuntos ajenos).
type adjuntosFalsos struct {
	mu         sync.Mutex
	faltan     map[int64][]string
	requisitos map[int64][]campo.Requisito
	ajenos     map[uuid.UUID]bool
}

func (a *adjuntosFalsos) Faltantes(_ context.Context, orden int64, req []campo.Requisito) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requisitos[orden] = req
	return a.faltan[orden], nil
}

func (a *adjuntosFalsos) AdjuntoDeOrden(_ context.Context, id uuid.UUID, _ int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ajenos[id] {
		return nucleo.NoEncontrado("El adjunto no existe")
	}
	return nil
}

func (a *adjuntosFalsos) fijar(orden int64, faltan ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.faltan[orden] = faltan
}

// inventarioFalso: Argos simulado (bodega → existencias; servicio → comodato).
type inventarioFalso struct {
	existencias map[int64][]campo.Existencia
	comodato    map[int64][]campo.EquipoComodato
	caido       bool
}

func (i *inventarioFalso) EquiposEnComodato(_ context.Context, _, servicio int64) ([]campo.EquipoComodato, error) {
	if i.caido {
		return nil, errors.New("argos caído")
	}
	return i.comodato[servicio], nil
}

func (i *inventarioFalso) BodegaDeEmpresa(_ context.Context, _, bodega int64) (bool, error) {
	if i.caido {
		return false, errors.New("argos caído")
	}
	_, ok := i.existencias[bodega]
	return ok, nil
}

func (i *inventarioFalso) Existencias(_ context.Context, _, bodega int64) ([]campo.Existencia, error) {
	if i.caido {
		return nil, errors.New("argos caído")
	}
	return i.existencias[bodega], nil
}

// 2026-10-28 09:00 en Guatemala.
var inicio = time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)

type entorno struct {
	m        *campo.Modulo
	reloj    *reloj
	adjuntos *adjuntosFalsos
}

func nuevoEntorno(ajustar ...func(*campo.Config)) entorno {
	r := &reloj{t: inicio}
	a := &adjuntosFalsos{faltan: map[int64][]string{}, requisitos: map[int64][]campo.Requisito{}, ajenos: map[uuid.UUID]bool{}}
	cfg := campo.Config{Reloj: r.ahora, Adjuntos: a}
	for _, f := range ajustar {
		f(&cfg)
	}
	return entorno{m: campo.Nuevo(cfg), reloj: r, adjuntos: a}
}

var ident = identidad.Nuevo(identidad.Config{})

// usuarioCon crea un usuario persona con los roles dados.
func usuarioCon(t *testing.T, roles ...identidad.AsignacionRol) int64 {
	t.Helper()
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Usuario de campo", Correo: pruebas.Correo("campo"), Contrasena: "contrasena-larga-1", Roles: roles,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func tecnico(t *testing.T) int64 {
	id := usuarioCon(t, identidad.AsignacionRol{Rol: "tecnico"})
	conAgenda(t, id)
	return id
}

// conAgenda da al usuario ficha de agenda con turnos de 00:00 a 24:00 todos los días (para que
// se le pueda programar sin pensar en turnos).
func conAgenda(t *testing.T, usuario int64) {
	t.Helper()
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(context.Background(), `
		WITH f AS (INSERT INTO agenda.tecnicos (usuario_id) VALUES ($1) ON CONFLICT (usuario_id) DO UPDATE SET activo = true RETURNING id)
		INSERT INTO agenda.turnos (tecnico_id, dia, inicio, fin)
		SELECT f.id, d, '00:00', '24:00' FROM f, generate_series(1, 7) d
		WHERE NOT EXISTS (SELECT 1 FROM agenda.turnos tu WHERE tu.tecnico_id = f.id)`, usuario); err != nil {
		t.Fatal(err)
	}
}

// zonaDe deja en el catálogo una zona de la empresa.
func zonaDe(t *testing.T, id, empresa int64) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO agenda.zonas (id, empresa_id, origen, nombre) VALUES ($1::bigint, $2, 'local', 'Zona ' || $1::bigint::text) ON CONFLICT (id) DO UPDATE SET empresa_id = $2, activa = true", id, empresa); err != nil {
		t.Fatal(err)
	}
}

// asegurarZona deja la zona en el catálogo de agenda (sin empresa: de todas).
func asegurarZona(t *testing.T, id int64) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO agenda.zonas (id, origen, nombre) VALUES ($1::bigint, 'local', 'Zona ' || $1::bigint::text) ON CONFLICT (id) DO UPDATE SET empresa_id = NULL, activa = true", id); err != nil {
		t.Fatal(err)
	}
}
func despacho(t *testing.T) int64 { return usuarioCon(t, identidad.AsignacionRol{Rol: "despacho"}) }

// permisosDe son los permisos del rol base.
func permisosDe(rol string) []nucleo.Permiso {
	for _, r := range nucleo.RolesBase {
		if r.Codigo == rol {
			return r.PermisosDeRol()
		}
	}
	panic("rol " + rol)
}

// como: contexto del usuario con los permisos (globales) de su rol, sin conector.
func como(t *testing.T, id int64, rol string) context.Context {
	return pruebas.ContextoUsuario(pruebas.Pool(t), id, permisosDe(rol)...)
}

// conAlcance: el usuario con permisos limitados (permiso → alcances).
func conAlcance(t *testing.T, id int64, mapa map[string][]contexto.Alcance) context.Context {
	ctx := pruebas.ContextoUsuario(pruebas.Pool(t), id)
	return contexto.ConActor(ctx, contexto.ActorDe(ctx), contexto.NuevosPermisos(mapa))
}

// problema exige el problem+json dado y devuelve sus errores por campo.
func problema(t *testing.T, err error, status int, codigo string) []nucleo.ErrorCampo {
	t.Helper()
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != status || p.Codigo() != codigo {
		t.Fatalf("esperaba %d %s, llegó %v", status, codigo, err)
	}
	return p.Errors
}

func ptr[T any](v T) *T { return &v }

// deber: deber(caso.Ejecutar(ctx, in))(t) falla la prueba si hubo error.
func deber[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// crearOrden crea una orden de instalación (sin conector) como despacho.
func crearOrden(t *testing.T, e entorno, ctx context.Context, tipo string) campo.Orden {
	t.Helper()
	asegurarZona(t, 3)
	return deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{
		Tipo: tipo, Direccion: ptr("Zona 10, 5a avenida 12-34"), Lat: ptr(14.6), Lon: ptr(-90.5),
		ZonaID: ptr(int64(3)), Descripcion: "Trabajo de prueba",
	}))(t)
}

// programada crea una orden y la programa para el técnico, hoy desde las 10:00 de Guatemala:
// cada orden del mismo técnico toma el siguiente hueco libre de 2 h (la agenda no deja que se
// crucen).
func programada(t *testing.T, e entorno, ctxDespacho context.Context, tipo string, tec int64) campo.Orden {
	t.Helper()
	o := crearOrden(t, e, ctxDespacho, tipo)
	for {
		huecos.Lock()
		n := huecos.siguiente[tec]
		huecos.siguiente[tec] = n + 1
		huecos.Unlock()
		p, err := e.m.ProgramarOrden.Ejecutar(ctxDespacho, campo.EntradaProgramarOrden{
			ID: o.ID, Version: o.Version, TecnicoID: &tec, Inicio: inicio.Add(time.Hour + time.Duration(n)*2*time.Hour),
		})
		var pr *nucleo.Problema
		if n < 6 && errors.As(err, &pr) && pr.Codigo() == "solape" {
			continue
		}
		return deber(p, err)(t)
	}
}

// huecos: el siguiente hueco de cada técnico (programada).
var huecos = struct {
	sync.Mutex
	siguiente map[int64]int
}{siguiente: map[int64]int{}}

type evento struct {
	Evento  string
	Empresa *int64
	Payload map[string]any
}

// eventosDe devuelve los eventos del outbox de una orden, en orden.
func eventosDe(t *testing.T, ordenID int64) []evento {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT evento, empresa_id, payload FROM integracion.outbox WHERE (payload->>'orden_id')::bigint = $1 ORDER BY id", ordenID)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var salida []evento
	for filas.Next() {
		var e evento
		var crudo []byte
		if err := filas.Scan(&e.Evento, &e.Empresa, &crudo); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(crudo, &e.Payload)
		salida = append(salida, e)
	}
	return salida
}

func nombres(es []evento) []string {
	salida := make([]string, 0, len(es))
	for _, e := range es {
		salida = append(salida, e.Evento)
	}
	return salida
}

func ultimo(t *testing.T, es []evento, nombre string) map[string]any {
	t.Helper()
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].Evento == nombre {
			return es[i].Payload
		}
	}
	t.Fatalf("falta %s en %v", nombre, nombres(es))
	return nil
}

// completarChecklist marca todos los ítems obligatorios de la orden.
func completarChecklist(t *testing.T, e entorno, ctx context.Context, id int64) {
	t.Helper()
	d := deber(e.m.VerOrden.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), campo.EntradaVerOrden{ID: id}))(t)
	for _, it := range d.Checklist {
		if !it.Obligatorio {
			continue
		}
		var v any
		switch it.Tipo {
		case campo.ItemSiNo:
			v = true
		case campo.ItemTexto:
			v = "Listo"
		case campo.ItemNumero, campo.ItemMedicion:
			v = -19.5
		case campo.ItemFoto:
			v = uuid.Must(uuid.NewV7()).String()
		case campo.ItemSeleccion:
			v = it.Opciones[0]
		}
		if _, err := e.m.MarcarChecklist.Ejecutar(ctx, campo.EntradaMarcarChecklist{ID: id, ItemID: &it.ItemID, Valor: v}); err != nil {
			t.Fatalf("marcar %s: %v", it.Codigo, err)
		}
	}
}

func contar(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// registrarEmpresas da de alta empresas (sale del modo sin conector) y las borra al terminar.
func registrarEmpresas(t *testing.T, ids ...int64) {
	t.Helper()
	pool := pruebas.Pool(t)
	for _, id := range ids {
		if _, err := pool.Exec(context.Background(),
			"INSERT INTO nucleo.empresas (id, nombre) VALUES ($1::bigint, 'Empresa ' || $1::bigint::text)", id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas"} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
}

func asignarEmpresa(t *testing.T, usuario, empresa int64) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, $2)", usuario, empresa); err != nil {
		t.Fatal(err)
	}
}
