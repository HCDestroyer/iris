package servidor_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func cambiar(c *cliente, actual, nueva string) respuesta {
	return c.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": actual, "nueva": nueva})
}

// Con una cookie robada no se prueba la contraseña actual sin límite: al 10.º fallo (como el
// login) responde 429 con Retry-After, cierra la sesión y bloquea el cambio aunque luego se
// acierte.
func TestCambiarContrasenaLimitaIntentos(t *testing.T) {
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	id, correo := e.usuario("lectura")
	c := e.cliente()
	if r := c.entrar(correo); r.status != 200 {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	for i := 1; i < 10; i++ {
		cambiar(c, fmt.Sprintf("no-es-la-actual-%03d", i), "nueva-contrasena-larga-1").problema(t, 422, "validacion")
	}
	r := cambiar(c, "no-es-la-actual-010", "nueva-contrasena-larga-1")
	r.problema(t, 429, "demasiados_intentos")
	if s, err := strconv.Atoi(r.h.Get("Retry-After")); err != nil || s < 60 {
		t.Fatalf("Retry-After: %q", r.h.Get("Retry-After"))
	}
	// La sesión quedó cerrada.
	c.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")

	// Entrar de nuevo (el login cuenta por correo, aparte) no reabre el cambio: sigue bloqueado
	// sin verificar la contraseña, aunque sea la correcta, y vuelve a cerrar la sesión.
	c2 := e.cliente()
	if r := c2.entrar(correo); r.status != 200 {
		t.Fatalf("entrar de nuevo: %d %s", r.status, r.crudo)
	}
	cambiar(c2, contrasena, "nueva-contrasena-larga-1").problema(t, 429, "demasiados_intentos")
	c2.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	if r := e.cliente().entrar(correo); r.status != 200 {
		t.Fatal("la contraseña no cambió: la vieja sigue entrando")
	}
	var bloqueos int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'yo.cambiar_contrasena' AND actor_id = $1
		AND resultado = 'error' AND entrada->'auditoria' ? 'bloqueado_hasta'`, id).Scan(&bloqueos)
	if bloqueos != 1 {
		t.Fatalf("bloqueo auditado: %d", bloqueos)
	}
}

// Acertar limpia la cuenta de fallos.
func TestCambiarContrasenaAciertoLimpiaFallos(t *testing.T) {
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	id, correo := e.usuario("lectura")
	c := e.cliente()
	c.entrar(correo)
	for i := 0; i < 3; i++ {
		cambiar(c, "no-es-la-actual-xyz", "nueva-contrasena-larga-2").problema(t, 422, "validacion")
	}
	clave := fmt.Sprintf("usuario:%d", id)
	var fallos int
	if err := pool.QueryRow(context.Background(), `SELECT fallos FROM nucleo.intentos_login WHERE correo = $1`, clave).Scan(&fallos); err != nil || fallos != 3 {
		t.Fatalf("fallos contados: %d %v", fallos, err)
	}
	if r := cambiar(c, contrasena, "nueva-contrasena-larga-2"); r.status != 200 {
		t.Fatalf("cambiar: %d %s", r.status, r.crudo)
	}
	var quedan int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM nucleo.intentos_login WHERE correo = $1`, clave).Scan(&quedan)
	if quedan != 0 {
		t.Fatal("el acierto no limpió los fallos")
	}
}
