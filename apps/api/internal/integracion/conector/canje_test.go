package conector

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

// tokensDePrueba registra lo que se emite y se revoca; fallar hace fallar la revocación.
type tokensDePrueba struct {
	mu        sync.Mutex
	revocados []int64
	fallar    bool
}

func (t *tokensDePrueba) EmitirTokenCanje(context.Context, string, time.Time) (int64, string, error) {
	return 42, "iris_pat_" + "prueba0123456789abcdef", nil
}
func (t *tokensDePrueba) ConfirmarTokenCanje(context.Context, int64) error { return nil }
func (t *tokensDePrueba) RevocarTokenIntegracion(_ context.Context, id int64, _ string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fallar {
		return errors.New("identidad no disponible")
	}
	t.revocados = append(t.revocados, id)
	return nil
}

type encolados struct {
	mu   sync.Mutex
	jobs []river.JobArgs
}

func (e *encolados) encolar(_ context.Context, _ pgx.Tx, a river.JobArgs) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.jobs = append(e.jobs, a)
	return nil
}

func entornoCanje(t *testing.T, tokens *tokensDePrueba, cola *encolados) (*Conexion, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"atk_prueba_0123456789","webhook":{"url":"https://x","secreto":"s"},"conector_id":"c-1"}`))
	}))
	t.Cleanup(srv.Close)
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	n := Nuevo(Config{Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true, Encolar: cola.encolar})
	c := n.Agregar(Sistema{
		Nombre: "pruebacanje", Remoto: Remoto{Codigo: "prueba", Nombre: "Prueba", VariableOrigenes: "IRIS_PRUEBA_ORIGENES"},
		PrefijoToken: "atk_", RutaCanje: "/canjear", Origenes: []string{srv.URL},
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		TrabajoEntrante: func(int64) river.JobArgs { return nil },
	})
	n.FijarTokens(tokens)
	return c, srv.URL + "/conectar#codigo-de-prueba-0123456789"
}

// Un pánico entre el canje y el guardado revoca el token entregado (defer con bandera).
func TestCanjeConPanicoRevoca(t *testing.T) {
	tokens, cola := &tokensDePrueba{}, &encolados{}
	c, enlace := entornoCanje(t, tokens, cola)
	antesDeGuardar = func() { panic("fallo entre el canje y el guardado") }
	t.Cleanup(func() { antesDeGuardar = func() {} })
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("se esperaba el pánico")
			}
		}()
		_, _ = c.Conectar.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), EntradaConectar{Enlace: enlace})
	}()
	if len(tokens.revocados) != 1 || tokens.revocados[0] != 42 {
		t.Fatalf("revocados tras el pánico: %v", tokens.revocados)
	}
}

// Si la revocación falla, se encola un reintento único por token.
func TestRevocacionFallidaSeEncola(t *testing.T) {
	tokens, cola := &tokensDePrueba{fallar: true}, &encolados{}
	c, enlace := entornoCanje(t, tokens, cola)
	antesDeGuardar = func() { panic("fallo") }
	t.Cleanup(func() { antesDeGuardar = func() {} })
	func() {
		defer func() { _ = recover() }()
		_, _ = c.Conectar.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), EntradaConectar{Enlace: enlace})
	}()
	if len(cola.jobs) != 1 {
		t.Fatalf("reintentos encolados: %v", cola.jobs)
	}
	if a, ok := cola.jobs[0].(ArgsRevocarToken); !ok || a.TokenID != 42 || !a.InsertOpts().UniqueOpts.ByArgs {
		t.Fatalf("reintento: %#v", cola.jobs[0])
	}
}
