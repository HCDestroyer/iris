package argos_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato con Argos 0.8.0 §1.5 (S5) y decisiones del líder sobre la vía de recuperación: una
// instalación ya conectada no se corta al actualizar. La migración marca `recuperacion` (sin
// vencimiento nuevo) los tokens vigentes de los usuarios de integración que no son el entregado por un
// canje; esos tokens y los de `iris integracion crear` (`cli`) de TODOS los usuarios con el rol del
// sistema se revocan al confirmarse un canje nuevo de ese sistema. El token del canje en uso sigue.

// bloqueMarcarRecuperacion: el UPDATE de la migración, entre sus marcas (el mismo texto que corre goose).
func bloqueMarcarRecuperacion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../migraciones/20261119173819_integracion_argos_primera_sincronizacion.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i, j := strings.Index(s, "-- b7:marcar_recuperacion:inicio"), strings.Index(s, "-- b7:marcar_recuperacion:fin")
	if i < 0 || j < i {
		t.Fatal("no se encontró el bloque marcar_recuperacion en la migración")
	}
	return s[i:j]
}

func (e *entorno) tokenVia(id int64) (via string, revocado bool, expira *time.Time) {
	e.t.Helper()
	var v *string
	if err := e.pool.QueryRow(context.Background(), `SELECT via, revocado_en IS NOT NULL, expira_en FROM nucleo.tokens_api WHERE id = $1`, id).
		Scan(&v, &revocado, &expira); err != nil {
		e.t.Fatal(err)
	}
	if v != nil {
		via = *v
	}
	return via, revocado, expira
}

func TestRecuperacionMigracionYCanjeNuevo(t *testing.T) {
	e := nuevoEntorno(t)
	e.limpiarSinc()
	ctx := pruebas.ContextoSistema(e.pool)
	_, conectorID, _ := e.conectado()
	var entregado, usuarioArgos int64
	if err := e.pool.QueryRow(context.Background(), `SELECT c.token_entregado_id, t.usuario_id FROM integracion.conectores c
		JOIN nucleo.tokens_api t ON t.id = c.token_entregado_id WHERE c.id = $1`, conectorID).Scan(&entregado, &usuarioArgos); err != nil {
		t.Fatal(err)
	}

	// Datos previos a esta versión: un token manual del usuario de Argos (sin via, sin vencimiento) y
	// otro usuario con el rol integracion_argos y su token manual.
	var manual int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO nucleo.tokens_api (usuario_id, nombre, prefijo, hash)
		VALUES ($1, 'manual viejo', 'iris_pat_v', sha256(random()::text::bytea)) RETURNING id`, usuarioArgos).Scan(&manual); err != nil {
		t.Fatal(err)
	}
	otro, err := e.ident.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{1},
		Correo: pruebas.Correo("otro-argos")})
	if err != nil {
		t.Fatal(err)
	}
	// La consola marca `cli`, sin vencimiento forzado.
	if via, rev, exp := e.tokenVia(otro.Token.Token.ID); via != "cli" || rev || exp != nil {
		t.Fatalf("iris integracion crear: via %q, revocado %v, expira %v", via, rev, exp)
	}
	if otro.Token.Token.Via == nil || *otro.Token.Token.Via != "cli" {
		t.Fatalf("TokenAPI debía exponer via: %+v", otro.Token.Token)
	}
	if _, err := e.ident.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{1},
		Correo: otro.Usuario.Correo, ExpiraEn: ptrTiempo(time.Now().Add(400 * 24 * time.Hour))}); err == nil {
		t.Fatal("la consola acepta como mucho 365 días")
	}
	conVence, err := e.ident.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{1},
		Correo: otro.Usuario.Correo, ExpiraEn: ptrTiempo(time.Now().Add(10 * 24 * time.Hour))})
	if err != nil || conVence.Token.Token.ExpiraEn == nil {
		t.Fatalf("--expires-in: %v %+v", err, conVence.Token.Token)
	}
	// Como estaban antes de la migración: sin via.
	e.exec(`UPDATE nucleo.tokens_api SET via = NULL WHERE id = ANY($1)`, []int64{entregado, manual, otro.Token.Token.ID})
	e.exec(bloqueMarcarRecuperacion(t))
	if via, rev, _ := e.tokenVia(entregado); via != "" || rev {
		t.Fatalf("el token del canje en uso no se marca: via %q, revocado %v", via, rev)
	}
	if via, rev, exp := e.tokenVia(manual); via != "recuperacion" || rev || exp != nil {
		t.Fatalf("el token manual viejo queda de recuperación, sin vencimiento nuevo: via %q, revocado %v, expira %v", via, rev, exp)
	}
	e.exec(`UPDATE nucleo.tokens_api SET via = 'cli' WHERE id = $1`, otro.Token.Token.ID)
	// El conector activo sigue autenticando (lo usa Argos para leer de Iris).
	if r := e.leerConPat(e.patDelUltimoCanje()); r.status != 200 {
		t.Fatalf("el conector activo dejó de autenticar tras migrar: %d %s", r.status, r.crudo)
	}

	// Canje nuevo del mismo sistema confirmado (reconectar y activar): los manuales de TODOS los
	// usuarios de integración de Argos se revocan; el del canje anterior sigue hasta que Argos use el nuevo.
	e.argos.con(func() { e.argos.version = "1.6.0" })
	admin := e.usuario("admin")
	e.reconectar(admin, false)
	for _, id := range []int64{manual, otro.Token.Token.ID, conVence.Token.Token.ID} {
		if _, rev, _ := e.tokenVia(id); !rev {
			t.Fatalf("el token %d (manual) debía revocarse al confirmarse el canje nuevo", id)
		}
	}
	if _, rev, _ := e.tokenVia(entregado); rev {
		t.Fatal("el token del canje anterior debía seguir hasta que Argos use el nuevo (0.6.3)")
	}
}

func ptrTiempo(t time.Time) *time.Time { return &t }
