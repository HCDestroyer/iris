package correo_test

import (
	"context"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

func TestHTMLATexto(t *testing.T) {
	got := correo.HTMLATexto(`<div>Hola<br>mundo</div><script>alert(1)</script><style>x{}</style><p>a &lt;b&gt; <i>c</i></p>`)
	if got != "Hola\nmundo\n\na <b> c" {
		t.Fatalf("%q", got)
	}
}

func TestQuitarCita(t *testing.T) {
	txt := "Gracias, ya funciona.\n\nEl mar, 27 oct 2026 a las 10:00, Soporte <s@isp.gt> escribió:\n> Reinicia el router"
	if got := correo.QuitarCita(txt); got != "Gracias, ya funciona." {
		t.Fatalf("%q", got)
	}
	if got := correo.QuitarCita("> solo cita"); got != "> solo cita" {
		t.Fatalf("sin texto nuevo se deja todo: %q", got)
	}
}

func TestLeerAutomaticosYCharset(t *testing.T) {
	casos := map[string]string{
		"From: a@b.gt\r\nPrecedence: bulk\r\n\r\nx":                          "precedence:bulk",
		"From: a@b.gt\r\nX-Autoreply: yes\r\n\r\nx":                          "autorespuesta",
		"From: postmaster@b.gt\r\n\r\nx":                                     "rebote",
		"From: a@b.gt\r\nAuto-Submitted: no\r\n\r\nx":                        "",
		"From: a@b.gt\r\nContent-Type: multipart/report; boundary=x\r\n\r\n": "rebote",
	}
	for crudo, esperado := range casos {
		c, err := correo.Leer(context.Background(), []byte(crudo), false)
		if err != nil || c.Automatico != esperado {
			t.Fatalf("%q: %q %v", crudo, c.Automatico, err)
		}
	}
	c, err := correo.Leer(context.Background(), []byte("From: =?ISO-8859-1?Q?Jos=E9?= <JOSE@B.GT>\r\nSubject: =?ISO-8859-1?Q?Se=F1al?=\r\n"+
		"Content-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nSe=F1al d=E9bil\r\n"), false)
	if err != nil || c.Remitente != "jose@b.gt" || c.RemitenteNombre != "José" || c.Asunto != "Señal" || c.Texto != "Señal débil" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestComponerSinInyeccionDeCabeceras(t *testing.T) {
	b := string(correo.Componer(correo.Saliente{
		De: mail.Address{Name: "Soporte\r\nBcc: x@evil.gt", Address: "s@isp.gt"}, Para: mail.Address{Address: "c@b.gt"},
		Asunto: "Hola\r\nBcc: x@evil.gt", MessageID: "a@b>\r\nX: y", Texto: "línea 1\nlínea 2", Fecha: time.Now(),
	}))
	cabeceras, _, _ := strings.Cut(b, "\r\n\r\n")
	for _, l := range strings.Split(cabeceras, "\r\n") {
		if strings.HasPrefix(strings.ToLower(l), "bcc:") || strings.HasPrefix(l, "X:") {
			t.Fatalf("cabecera inyectada: %q\n%s", l, b)
		}
	}
}

// anidado arma un correo con n niveles de multipart/mixed y un texto al fondo.
func anidado(n int) []byte {
	var b strings.Builder
	b.WriteString("From: a@b.gt\r\nSubject: anidado\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b0\r\n\r\n")
	for i := 1; i <= n; i++ {
		b.WriteString("--b" + strconv.Itoa(i-1) + "\r\nContent-Type: multipart/mixed; boundary=b" + strconv.Itoa(i) + "\r\n\r\n")
	}
	b.WriteString("--b" + strconv.Itoa(n) + "\r\nContent-Type: text/plain\r\n\r\nhola\r\n")
	for i := n; i >= 0; i-- {
		b.WriteString("--b" + strconv.Itoa(i) + "--\r\n")
	}
	return []byte(b.String())
}

func TestLeerMIMEAnidadoAcotado(t *testing.T) {
	// Profundo: más de MaxDelimitadores marcas → solo cabeceras, y rápido.
	inicio := time.Now()
	c, err := correo.Leer(context.Background(), anidado(20000), false)
	if err != nil || c.Limite != correo.LimiteMIMEAnidado || c.Texto != "" || c.Asunto != "anidado" {
		t.Fatalf("anidado profundo: %q %q %v", c.Limite, c.Texto, err)
	}
	if d := time.Since(inicio); d > time.Second {
		t.Fatalf("tardó %s", d)
	}
	// Por debajo del conteo pero más hondo que MaxProfundidad: profundidad.
	if c, err := correo.Leer(context.Background(), anidado(15), false); err != nil || c.Limite != correo.LimiteProfundidad {
		t.Fatalf("profundidad: %q %v", c.Limite, err)
	}
	// Normal (3 niveles): se lee completo.
	if c, err := correo.Leer(context.Background(), anidado(3), false); err != nil || c.Limite != "" || c.Texto != "hola" {
		t.Fatalf("normal: %q %q %v", c.Limite, c.Texto, err)
	}
	// Más de MaxPartes partes (planas): partes, con el texto ya leído.
	var b strings.Builder
	b.WriteString("From: a@b.gt\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nprimero\r\n")
	for range correo.MaxPartes + 5 {
		b.WriteString("--x\r\nContent-Type: application/octet-stream\r\n\r\nAAAA\r\n")
	}
	b.WriteString("--x--\r\n")
	if c, err := correo.Leer(context.Background(), []byte(b.String()), false); err != nil || c.Limite != correo.LimitePartes || c.Texto != "primero" {
		t.Fatalf("partes: %q %q %v", c.Limite, c.Texto, err)
	}
	// Cuerpo de más de MaxCuerpoAnalizado: bytes.
	grande := "From: a@b.gt\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nhola\r\n" +
		"--x\r\nContent-Type: application/octet-stream\r\n\r\n" + strings.Repeat("A", correo.MaxCuerpoAnalizado+10) + "\r\n--x--\r\n"
	if c, err := correo.Leer(context.Background(), []byte(grande), false); err != nil || c.Limite != correo.LimiteBytes || c.Texto != "hola" {
		t.Fatalf("bytes: %q %q %v", c.Limite, c.Texto, err)
	}
	// Contexto cancelado: tiempo.
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	if c, err := correo.Leer(ctx, anidado(3), false); err != nil || c.Limite != correo.LimiteTiempo {
		t.Fatalf("contexto: %q %v", c.Limite, err)
	}
}

func TestVerificarRemitente(t *testing.T) {
	const srv = "mx.isp.gt"
	casos := []struct {
		nombre     string
		cabeceras  []string
		authserv   string
		remitente  string
		verificado bool
	}{
		{"dmarc pass", []string{"mx.isp.gt; spf=pass smtp.mailfrom=b.gt; dkim=pass header.d=b.gt; dmarc=pass (p=reject) header.from=b.gt"}, srv, "a@b.gt", true},
		{"dkim alineado subdominio", []string{"MX.ISP.GT 1; dkim=pass header.i=@b.gt"}, srv, "a@correo.b.gt", true},
		{"sin authserv configurado", []string{"mx.isp.gt; dmarc=pass header.from=b.gt"}, "", "a@b.gt", false},
		{"otro servidor", []string{"mx.evil.gt; dmarc=pass header.from=b.gt"}, srv, "a@b.gt", false},
		{"dkim de otro dominio", []string{"mx.isp.gt; dkim=pass header.d=evil.gt; dmarc=fail header.from=b.gt"}, srv, "a@b.gt", false},
		{"dkim de sufijo", []string{"mx.isp.gt; dkim=pass header.d=gt"}, srv, "a@b.gt", false},
		{"solo spf", []string{"mx.isp.gt; spf=pass smtp.mailfrom=b.gt"}, srv, "a@b.gt", false},
		{"dmarc de otro from", []string{"mx.isp.gt; dmarc=pass header.from=evil.gt"}, srv, "a@b.gt", false},
		{"comentario no engaña", []string{"mx.isp.gt; dmarc=fail (dmarc=pass header.from=b.gt) header.from=b.gt"}, srv, "a@b.gt", false},
		// El remitente añadió una con nuestro id más abajo: solo cuenta el bloque de arriba.
		{"falsa abajo", []string{"mx.isp.gt; dmarc=fail header.from=b.gt", "relay.otro.gt; none", "mx.isp.gt; dmarc=pass header.from=b.gt"}, srv, "a@b.gt", false},
	}
	for _, c := range casos {
		if v, r := correo.Verificar(c.cabeceras, c.authserv, c.remitente); v != c.verificado {
			t.Errorf("%s: verificado=%v (%s)", c.nombre, v, r)
		}
	}
	// Leer las entrega de arriba abajo.
	crudo := "Authentication-Results: mx.isp.gt; dmarc=pass header.from=b.gt\r\nReceived: x\r\nAuthentication-Results: mx.isp.gt; dmarc=fail\r\nFrom: a@b.gt\r\n\r\nhola"
	c, err := correo.Leer(context.Background(), []byte(crudo), false)
	if err != nil || len(c.Autenticacion) != 2 || !strings.Contains(c.Autenticacion[0], "pass") {
		t.Fatalf("orden: %q %v", c.Autenticacion, err)
	}
}

func TestComponerSinAutoSubmitted(t *testing.T) {
	b := string(correo.Componer(correo.Saliente{De: mail.Address{Address: "s@isp.gt"}, Para: mail.Address{Address: "c@b.gt"}, Asunto: "x", MessageID: "a@b", Texto: "hola", Fecha: time.Now()}))
	if strings.Contains(strings.ToLower(b), "auto-submitted") {
		t.Fatalf("una respuesta de un agente no es automática:\n%s", b)
	}
}
