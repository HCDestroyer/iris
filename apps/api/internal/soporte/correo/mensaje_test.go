package correo_test

import (
	"net/mail"
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
		c, err := correo.Leer([]byte(crudo), false)
		if err != nil || c.Automatico != esperado {
			t.Fatalf("%q: %q %v", crudo, c.Automatico, err)
		}
	}
	c, err := correo.Leer([]byte("From: =?ISO-8859-1?Q?Jos=E9?= <JOSE@B.GT>\r\nSubject: =?ISO-8859-1?Q?Se=F1al?=\r\n"+
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
