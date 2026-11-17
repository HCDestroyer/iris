package vozprueba

import (
	"crypto/tls"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/voz"
)

// Banco es una puerta real (TLS de verdad en un puerto de loopback) delante de un Ollama falso.
type Banco struct {
	T       testing.TB
	Puerta  *voz.Puerta
	Almacen *voz.Almacen
	Ollama  *Ollama
	// URL de la puerta: https://127.0.0.1:puerto.
	URL string
	// Huella del certificado del servidor (la que imprimiría el instalador).
	Huella string

	mu     sync.Mutex
	desfas time.Duration
}

// Opciones del banco.
type Opciones struct {
	Manifiesto     *voz.Manifiesto
	Modelos        []string
	Permitidos     []string
	MaxSimultaneas int
	PlazoLLM       time.Duration
	MedidorRuta    string
}

// Ahora es el reloj de la puerta (el real más el desfase de Adelantar).
func (b *Banco) Ahora() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().Add(b.desfas)
}

// Adelantar mueve el reloj de la puerta.
func (b *Banco) Adelantar(d time.Duration) {
	b.mu.Lock()
	b.desfas += d
	b.mu.Unlock()
}

// NuevoBanco levanta la puerta y el Ollama falso; se cierran al terminar la prueba.
func NuevoBanco(t testing.TB, o Opciones) *Banco {
	t.Helper()
	if o.Modelos == nil {
		o.Modelos = []string{"modelo-a:latest", "modelo-b:latest"}
	}
	if o.Permitidos == nil {
		o.Permitidos = []string{"modelo-a:latest"}
	}
	man := voz.Manifiesto{Version: "0.1.0", Protocolo: voz.Protocolo, IrisMin: "0.0.0", IrisMax: "99.0.0"}
	if o.Manifiesto != nil {
		man = *o.Manifiesto
	}
	b := &Banco{T: t, Ollama: Nuevo(o.Modelos...)}
	falso := httptest.NewServer(b.Ollama)
	t.Cleanup(falso.Close)
	alm, err := voz.AbrirAlmacen(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b.Almacen = alm
	if b.Huella, err = alm.AsegurarCertificado("iris-voz-prueba"); err != nil {
		t.Fatal(err)
	}
	if err := alm.Cambiar(func(e *voz.Estado) error { e.ModelosPermitidos = o.Permitidos; return nil }); err != nil {
		t.Fatal(err)
	}
	simult := o.MaxSimultaneas
	if simult == 0 {
		simult = 4
	}
	b.Puerta, err = voz.Nueva(voz.Config{Almacen: alm, Manifiesto: man, OllamaURL: falso.URL, MedidorRuta: o.MedidorRuta,
		DiscoRuta: t.TempDir(), MaxSimultaneas: simult, PlazoLLM: o.PlazoLLM, Ahora: b.Ahora})
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := voz.Servidor(b.Puerta)
	go func() { _ = voz.Escuchar(srv, l) }()
	t.Cleanup(func() { _ = srv.Close() })
	b.URL = "https://" + l.Addr().String()
	return b
}

// Certificado genera un certificado de cliente como el de Iris.
func Certificado(t testing.TB) tls.Certificate {
	t.Helper()
	c, k, err := voz.GenerarCertificado("iris-prueba", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	par, err := tls.X509KeyPair(c, k)
	if err != nil {
		t.Fatal(err)
	}
	return par
}

// AbrirVentana abre la ventana de enlace en el servidor y devuelve el código (lo que hace
// `iris-voz enlazar` con el código que el admin pega).
func (b *Banco) AbrirVentana() string {
	b.T.Helper()
	codigo, err := voz.NuevoCodigo()
	if err != nil {
		b.T.Fatal(err)
	}
	if err := b.Almacen.AbrirVentana(codigo, b.Ahora()); err != nil {
		b.T.Fatal(err)
	}
	return codigo
}

// Enlazar hace el emparejamiento completo y devuelve el cliente de Iris ya enlazado.
func (b *Banco) Enlazar(cert tls.Certificate, versionIris string) (*voz.Cliente, voz.ResultadoEnlace) {
	b.T.Helper()
	codigo := b.AbrirVentana()
	r, err := voz.Enlazar(b.T.Context(), voz.OpcionesEnlace{URL: b.URL, Codigo: codigo, Cert: cert})
	if err != nil {
		b.T.Fatalf("enlazar: %v", err)
	}
	return b.Cliente(cert, r.Token, versionIris, r.HuellaServidor), r
}

// Cliente arma un cliente de Iris con esas credenciales.
func (b *Banco) Cliente(cert tls.Certificate, token []byte, versionIris string, huellas ...string) *voz.Cliente {
	b.T.Helper()
	c, err := voz.NuevoCliente(voz.OpcionesCliente{URL: b.URL, Huellas: huellas, Cert: cert, Token: token, VersionIris: versionIris})
	if err != nil {
		b.T.Fatal(err)
	}
	b.T.Cleanup(c.Cerrar)
	return c
}
