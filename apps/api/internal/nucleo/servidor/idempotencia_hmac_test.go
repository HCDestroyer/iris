package servidor_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

func claveB64(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// conClaves arma otra API sobre el mismo módulo de identidad (mismas sesiones) con las claves
// de idempotencia derivadas de IRIS_CLAVE_CIFRADO (actual) y, si se da, la anterior.
func (e *entorno) conClaves(t *testing.T, actual, anterior string) *entorno {
	t.Helper()
	claves, err := nucleo.ClavesIdempotencia(actual, anterior)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: e.mod, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Idempotencia: nucleo.OpcionesIdempotencia{Claves: claves},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &entorno{t: t, srv: srv, mod: e.mod}
}

// misma sesión contra otra instancia de la API (como un reinicio con otra configuración).
func (c *cliente) contra(e *entorno) *cliente {
	return &cliente{t: c.t, base: e.srv.URL, http: c.http, csrf: c.csrf, bearer: c.bearer}
}

func shaPlano(metodo, ruta string, cuerpo []byte) []byte {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\n%s\n", metodo, ruta)
	h.Write(cuerpo)
	return h.Sum(nil)
}

type filaIdem struct {
	hash    []byte
	formato int16
	idClave []byte
}

func leerFila(t *testing.T, actor, clave string) filaIdem {
	t.Helper()
	var f filaIdem
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT hash_peticion, hash_formato, hash_clave_id FROM nucleo.idempotencia
		WHERE actor = $1 AND clave = $2`, actor, clave).Scan(&f.hash, &f.formato, &f.idClave); err != nil {
		t.Fatal(err)
	}
	return f
}

// La tabla no guarda el SHA-256 plano del cuerpo (que lleva la contraseña), y la idempotencia
// sigue igual: misma clave y cuerpo → la respuesta guardada; otro cuerpo → 422.
func TestIdempotenciaGuardaHMAC(t *testing.T) {
	base := nuevoEntorno(t)
	e := base.conClaves(t, claveB64(t), "")
	idAdmin, correoAdmin := base.usuario("admin")
	c := e.cliente()
	c.entrar(correoAdmin)

	cuerpo := map[string]any{"nombre": "Con HMAC", "correo": pruebas.Correo("hmac"), "contrasena": "contrasena-candidata-123"}
	crudo, _ := json.Marshal(cuerpo)
	clave := "hmac-" + pruebas.Unico()
	r1 := c.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if r1.status != 201 {
		t.Fatalf("primera: %d %s", r1.status, r1.crudo)
	}
	f := leerFila(t, fmt.Sprintf("usuario:%d", idAdmin), clave)
	if bytes.Equal(f.hash, shaPlano(http.MethodPost, "/api/v1/usuarios", crudo)) || f.formato != 1 || len(f.idClave) != 8 || len(f.hash) != 32 {
		t.Fatalf("la fila guarda el SHA-256 plano o no es HMAC: formato %d id %x", f.formato, f.idClave)
	}
	r2 := c.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if r2.status != 201 || r2.h.Get(nucleo.CabeceraRepetida) != "true" || !bytes.Equal(r1.crudo, r2.crudo) {
		t.Fatalf("repetida: %d %s", r2.status, r2.crudo)
	}
	otro := map[string]any{"nombre": "Con HMAC", "correo": cuerpo["correo"], "contrasena": "otra-contrasena-456"}
	c.pedir(http.MethodPost, "/api/v1/usuarios", otro, nucleo.CabeceraIdempotencia, clave).problema(t, 422, "idempotencia_reutilizada")

	// Dos APIs con la misma IRIS_CLAVE_CIFRADO (réplicas, reinicio) derivan la misma clave.
	if f2 := leerFila(t, fmt.Sprintf("usuario:%d", idAdmin), clave); !bytes.Equal(f2.hash, f.hash) {
		t.Fatal("la fila cambió al repetir")
	}
}

// Filas de antes (SHA-256 plano, hash_formato 0) siguen valiendo hasta que vencen.
func TestIdempotenciaFilasViejas(t *testing.T) {
	base := nuevoEntorno(t)
	e := base.conClaves(t, claveB64(t), "")
	idAdmin, correoAdmin := base.usuario("admin")
	c := e.cliente()
	c.entrar(correoAdmin)
	ctx := context.Background()

	cuerpo := map[string]any{"nombre": "Viejo", "correo": pruebas.Correo("viejo"), "contrasena": contrasena}
	crudo, _ := json.Marshal(cuerpo)
	clave := "vieja-" + pruebas.Unico()
	guardada := []byte(`{"id":4242,"nombre":"Viejo"}`)
	if _, err := pruebas.Pool(t).Exec(ctx, `INSERT INTO nucleo.idempotencia
		(actor, clave, metodo, ruta, hash_peticion, estado, status, content_type, respuesta, expira_en)
		VALUES ($1, $2, 'POST', '/api/v1/usuarios', $3, 'completa', 201, 'application/json', $4, now() + interval '1 day')`,
		fmt.Sprintf("usuario:%d", idAdmin), clave, shaPlano(http.MethodPost, "/api/v1/usuarios", crudo), guardada); err != nil {
		t.Fatal(err)
	}
	if f := leerFila(t, fmt.Sprintf("usuario:%d", idAdmin), clave); f.formato != 0 {
		t.Fatalf("una fila sin formato es la vieja (0): %d", f.formato)
	}
	r := c.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if r.status != 201 || r.h.Get(nucleo.CabeceraRepetida) != "true" || !bytes.Equal(r.crudo, guardada) {
		t.Fatalf("fila vieja, misma petición: %d %s", r.status, r.crudo)
	}
	otro := map[string]any{"nombre": "Otro", "correo": pruebas.Correo("viejo"), "contrasena": contrasena}
	c.pedir(http.MethodPost, "/api/v1/usuarios", otro, nucleo.CabeceraIdempotencia, clave).problema(t, 422, "idempotencia_reutilizada")
}

// Rotación de IRIS_CLAVE_CIFRADO: con la anterior configurada, las filas de antes se comparan;
// sin ella, no se pueden comparar y responden 422 idempotencia_no_verificable (nunca se
// ejecutan otra vez a ciegas).
func TestIdempotenciaRotacionDeClave(t *testing.T) {
	base := nuevoEntorno(t)
	vieja, nueva := claveB64(t), claveB64(t)
	antes := base.conClaves(t, vieja, "")
	transicion := base.conClaves(t, nueva, vieja)
	despues := base.conClaves(t, nueva, "")
	_, correoAdmin := base.usuario("admin")
	c := antes.cliente()
	c.entrar(correoAdmin)

	cuerpo := map[string]any{"nombre": "Rotado", "correo": pruebas.Correo("rot"), "contrasena": contrasena}
	clave := "rot-" + pruebas.Unico()
	r1 := c.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if r1.status != 201 {
		t.Fatalf("primera: %d %s", r1.status, r1.crudo)
	}
	if r := c.contra(transicion).pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave); r.status != 201 ||
		r.h.Get(nucleo.CabeceraRepetida) != "true" || !bytes.Equal(r.crudo, r1.crudo) {
		t.Fatalf("en transición: %d %s", r.status, r.crudo)
	}
	otro := map[string]any{"nombre": "Otro", "correo": pruebas.Correo("rot"), "contrasena": contrasena}
	c.contra(transicion).pedir(http.MethodPost, "/api/v1/usuarios", otro, nucleo.CabeceraIdempotencia, clave).problema(t, 422, "idempotencia_reutilizada")
	c.contra(despues).pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave).problema(t, 422, "idempotencia_no_verificable")
	// Con otra clave de idempotencia, la nueva instancia trabaja normal (aquí choca con el correo:
	// la primera sí se hizo, que es justo lo que el 422 pide revisar).
	c.contra(despues).pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, "otra-"+clave).problema(t, 409, "correo_en_uso")
}
