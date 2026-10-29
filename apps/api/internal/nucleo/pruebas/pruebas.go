// Package pruebas prepara Postgres real para las pruebas de integración.
//
//	func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }
//
//	func TestAlgo(t *testing.T) {
//	    pool := pruebas.Pool(t)
//	    ctx := pruebas.ContextoSistema(pool)
//	    …
//	}
//
// La base se toma de IRIS_DB_URL (por defecto
// postgres://iris:iris@localhost:5432/iris_test_i1) o, si está IRIS_DB, de ese nombre
// en el mismo servidor. Por seguridad el nombre debe empezar con iris_test.
//
// Cada paquete de pruebas BORRA y recrea la base, la migra (goose + River) y siembra permisos
// y roles. Como `go test ./...` corre paquetes en paralelo, un advisory lock en la base
// `postgres` los pone en fila: nunca dos paquetes usan la base a la vez.
package pruebas

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

// URLPorDefecto apunta al Postgres compartido de desarrollo.
const URLPorDefecto = "postgres://iris:iris@localhost:5432/iris_test_i1?sslmode=disable"

var pool *pgxpool.Pool

// URL devuelve la cadena de conexión de la base de pruebas.
func URL() (string, error) {
	crudo := os.Getenv("IRIS_DB_URL")
	if crudo == "" {
		crudo = URLPorDefecto
	}
	u, err := url.Parse(crudo)
	if err != nil {
		return "", err
	}
	if nombre := os.Getenv("IRIS_DB"); nombre != "" {
		u.Path = "/" + nombre
	}
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "iris_test") {
		return "", fmt.Errorf("pruebas: la base %q no empieza con iris_test; no se borra", u.Path)
	}
	return u.String(), nil
}

// Correr prepara la base, corre las pruebas del paquete y libera el candado.
func Correr(m *testing.M) int {
	ctx := context.Background()
	destino, err := URL()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	u, _ := url.Parse(destino)
	nombre := strings.TrimPrefix(u.Path, "/")
	mant := *u
	mant.Path = "/postgres"

	admin, err := pgx.Connect(ctx, mant.String())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pruebas: conectar a postgres:", err)
		return 1
	}
	defer func() { _ = admin.Close(ctx) }()
	h := fnv.New64a()
	_, _ = h.Write([]byte(nombre))
	candado := int64(h.Sum64() >> 1) //nolint:gosec // cabe en int64
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", candado); err != nil {
		fmt.Fprintln(os.Stderr, "pruebas: candado:", err)
		return 1
	}
	defer func() { _, _ = admin.Exec(ctx, "SELECT pg_advisory_unlock($1)", candado) }()

	ident := pgx.Identifier{nombre}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
		fmt.Fprintln(os.Stderr, "pruebas: borrar base:", err)
		return 1
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		fmt.Fprintln(os.Stderr, "pruebas: crear base:", err)
		return 1
	}

	pool, err = pgxpool.New(ctx, destino)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pruebas: pool:", err)
		return 1
	}
	defer pool.Close()
	silencio := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := migraciones.Migrar(ctx, pool, silencio); err != nil {
		fmt.Fprintln(os.Stderr, "pruebas: migrar:", err)
		return 1
	}
	if err := identidad.SincronizarCatalogo(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "pruebas: semilla:", err)
		return 1
	}
	return m.Run()
}

// Pool devuelve el pool de la base de pruebas (Correr debe estar en TestMain).
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if pool == nil {
		t.Fatal("pruebas: falta `func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }`")
	}
	return pool
}

// ContextoSistema: actor sistema, canal consola, request id nuevo y la base.
func ContextoSistema(bd nucleo.BD) context.Context {
	return nucleo.ConBD(contexto.Sistema(context.Background(), contexto.CanalConsola), bd)
}

// ContextoUsuario: actor usuario con los permisos dados (globales), canal panel y la empresa
// resuelta en modo «sin conector» (EmpresaDe = 0 = datos con empresa_id null), como la dejaría
// el middleware sin empresas registradas. Para operar en una empresa, ContextoUsuarioEnEmpresa;
// para otros estados, contexto.ConEmpresas encima. Sin estado resuelto, auditoria.Caso
// rechaza las acciones operativas de un usuario (falla cerrado).
func ContextoUsuario(bd nucleo.BD, id int64, permisos ...nucleo.Permiso) context.Context {
	mapa := map[string][]contexto.Alcance{}
	for _, p := range permisos {
		mapa[string(p)] = []contexto.Alcance{{}}
	}
	ctx := contexto.ConActor(context.Background(), contexto.Actor{Tipo: contexto.ActorUsuario, ID: id, Nombre: "prueba"}, contexto.NuevosPermisos(mapa))
	ctx = contexto.ConCanal(ctx, contexto.CanalPanel)
	ctx = contexto.ConRequestID(ctx, contexto.NuevoRequestID())
	ctx = contexto.ConEmpresas(ctx, contexto.Empresas{Estado: contexto.EmpresaSinConector, Disponibles: []int64{}})
	return nucleo.ConBD(ctx, bd)
}

// ContextoUsuarioEnEmpresa es ContextoUsuario con la empresa activa dada (Estado EmpresaActiva),
// como la dejaría el middleware con X-Iris-Empresa o con una sola empresa asignada.
func ContextoUsuarioEnEmpresa(bd nucleo.BD, id, empresa int64, permisos ...nucleo.Permiso) context.Context {
	return contexto.ConEmpresas(ContextoUsuario(bd, id, permisos...), contexto.Empresas{
		Estado: contexto.EmpresaActiva, Activa: empresa, Disponibles: []int64{empresa},
	})
}

// ContextoCliente: actor cliente del portal (cliente y empresa de Argos fijos), canal portal y
// su empresa como activa, como lo deja el middleware con la cookie iris_portal. Para probar
// los casos de uso del portal (Acceso auditoria.Cliente).
func ContextoCliente(bd nucleo.BD, clienteID, empresaID int64) context.Context {
	ctx := contexto.ComoCliente(context.Background(), contexto.Actor{
		Tipo: contexto.ActorCliente, ClienteID: clienteID, EmpresaID: empresaID, Nombre: "cliente de prueba",
	})
	ctx = contexto.ConRequestID(ctx, contexto.NuevoRequestID())
	return nucleo.ConBD(ctx, bd)
}

var contador atomic.Int64

// Unico devuelve un sufijo único para datos de prueba (correos, nombres).
func Unico() string {
	return fmt.Sprintf("%d%d", time.Now().UnixNano()%1_000_000_000, contador.Add(1))
}

// Correo devuelve un correo único.
func Correo(prefijo string) string {
	return prefijo + "." + Unico() + "@prueba.iris.gt"
}

// TokensFalsos implementa conector.Tokens para pruebas con un Argos falso que no valida el
// iris_pat_ del canje: emite uno fijo (sin fila: id 0), confirma y revoca sin hacer nada.
type TokensFalsos struct{}

// EmitirTokenCanje devuelve un iris_pat_ fijo.
func (TokensFalsos) EmitirTokenCanje(context.Context, string, time.Time) (int64, string, error) {
	return 0, "iris_pat_" + strings.Repeat("p", 32), nil
}

// ConfirmarTokenCanje no hace nada.
func (TokensFalsos) ConfirmarTokenCanje(context.Context, int64) error { return nil }

// RevocarTokenIntegracion no hace nada.
func (TokensFalsos) RevocarTokenIntegracion(context.Context, int64, string) error { return nil }
