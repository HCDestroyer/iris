package voz_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/voz"
	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

// Qué cuenta como «el servidor dijo que no» (lo que Iris usa para dar por no entregada una
// credencial): solo lo que nadie en el camino puede falsificar.
func TestRechazoExplicito(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	c, r := b.Enlazar(cert, versionIris)
	if _, err := c.Salud(t.Context()); err != nil {
		t.Fatal(err)
	}

	// El rechazo real del certificado de cliente: la puerta lo corta con una alerta TLS que llega
	// después del saludo (cifrada, con el servidor ya autenticado por su huella). Cuenta.
	otro := b.Cliente(vozprueba.Certificado(t), r.Token, versionIris, r.HuellaServidor)
	_, err := otro.Salud(t.Context())
	var rt *voz.ErrorRechazoTLS
	if !errors.As(err, &rt) || !voz.RechazoExplicito(err) {
		t.Fatalf("el rechazo del certificado de cliente debía ser un rechazo explícito: %T %v", err, err)
	}
	// Un token que el servidor no conoce: 401 por la firma. Cuenta.
	token, _ := voz.NuevoToken()
	_, err = b.Cliente(cert, token, versionIris, r.HuellaServidor).Salud(t.Context())
	if !voz.RechazoExplicito(err) {
		t.Fatalf("un 401 por la firma debía ser un rechazo explícito: %v", err)
	}

	// Alguien en el camino contesta al ClientHello con una alerta TLS en claro («bad certificate»),
	// sin tener ningún certificado. Se ve igual que un rechazo, pero llega durante el saludo: no cuenta.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			con, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = con.Close() }()
				cabecera := make([]byte, 5)
				if _, err := io.ReadFull(con, cabecera); err != nil {
					return
				}
				_, _ = io.CopyN(io.Discard, con, int64(cabecera[3])<<8|int64(cabecera[4]))
				// Registro de alerta (21), TLS 1.2 en el registro, 2 bytes: fatal (2), bad_certificate (42).
				_, _ = con.Write([]byte{21, 3, 3, 0, 2, 2, 42})
			}()
		}
	}()
	falso, err := voz.NuevoCliente(voz.OpcionesCliente{URL: "https://" + l.Addr().String(), Huellas: []string{r.HuellaServidor}, Cert: cert,
		Token: r.Token, VersionIris: versionIris})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(falso.Cerrar)
	_, err = falso.Salud(t.Context())
	var op *net.OpError
	if err == nil || !errors.As(err, &op) || op.Op != "remote error" {
		t.Fatalf("el servidor falso debía cortar con una alerta TLS: %T %v", err, err)
	}
	if errors.As(err, &rt) || voz.RechazoExplicito(err) {
		t.Fatalf("una alerta durante el saludo (falsificable) no es un rechazo explícito: %v", err)
	}

	// Y lo que no es una respuesta del servidor tampoco: tiempo agotado, puerto cerrado, otros errores.
	_ = l.Close()
	_, errCerrado := falso.Salud(t.Context())
	for nombre, e := range map[string]error{"tiempo agotado": context.DeadlineExceeded, "puerto cerrado": errCerrado,
		"reloj desfasado": &voz.ErrorPuerta{Estado: 401, Codigo: "reloj_desfasado"}, "error del servidor": &voz.ErrorPuerta{Estado: 500, Codigo: "error"},
		"huella del servidor distinta": voz.ErrHuella, "ninguno": nil} {
		if voz.RechazoExplicito(e) {
			t.Fatalf("%s no es un rechazo explícito: %v", nombre, e)
		}
	}
}
