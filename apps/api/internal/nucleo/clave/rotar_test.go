package clave_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/clave"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const contrasena = "contrasena-de-prueba-123"

// claveNueva genera una clave de 32 bytes en base64 (las claves de prueba nunca van al repo).
func claveNueva(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func cifrador(t *testing.T, claves ...string) *nucleo.Cifrador {
	t.Helper()
	c, err := nucleo.NuevoCifrador(claves[0], claves[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// api levanta la API (solo identidad) con el cifrador dado: el login con MFA descifra el TOTP.
func api(t *testing.T, c *nucleo.Cifrador) (*httptest.Server, *identidad.Modulo) {
	t.Helper()
	mod := identidad.Nuevo(identidad.Config{Cifrador: c, CookieSegura: false})
	h, _ := servidor.Nuevo(servidor.Deps{BD: pruebas.Pool(t), Identidad: mod, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, mod
}

type sesion struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func nuevaSesion(t *testing.T, srv *httptest.Server) *sesion {
	jar, _ := cookiejar.New(nil)
	return &sesion{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
}

func (s *sesion) pedir(ruta string, cuerpo any) (int, map[string]any) {
	s.t.Helper()
	b, _ := json.Marshal(cuerpo)
	req, _ := http.NewRequest(http.MethodPost, s.base+ruta, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if s.csrf != "" {
		req.Header.Set(identidad.CabeceraCSRF, s.csrf)
	}
	res, err := s.http.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

// entrarConMFA entra con contraseña y código; devuelve el status del código.
func entrarConMFA(t *testing.T, srv *httptest.Server, correo, codigo string) int {
	t.Helper()
	s := nuevaSesion(t, srv)
	st, r := s.pedir("/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena})
	if st != 200 || r["mfaRequerido"] != true {
		t.Fatalf("login: %d %v", st, r)
	}
	s.csrf, _ = r["csrfToken"].(string)
	st, _ = s.pedir("/api/v1/sesion/mfa", map[string]any{"codigo": codigo})
	return st
}

// usuarioConMFA crea un usuario y le activa el MFA con el módulo dado (y su cifrador). El código
// de activación es el del paso anterior al de ahora, para dejar libres el actual y el siguiente.
// La API valida contra su reloj (time.Now) con ±1 paso (RFC 6238): el código se genera con la hora
// del momento, no con una fijada al empezar la prueba (que podía quedar dos pasos atrás si la prueba
// cruzaba el límite de 30 s), y si justo se cruza el límite entre generarlo y validarlo, se genera de
// nuevo una vez.
func usuarioConMFA(t *testing.T, mod *identidad.Modulo) (int64, string, string) {
	t.Helper()
	pool := pruebas.Pool(t)
	correo := pruebas.Correo("mfa")
	u, err := mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pool), identidad.EntradaCrearUsuario{
		Nombre: "Con MFA", Correo: correo, Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: "lectura"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	uctx := pruebas.ContextoUsuario(pool, u.ID)
	cfg, err := mod.ConfigurarMFA.Ejecutar(uctx, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	for intento := 0; ; intento++ {
		codigo := codigoTOTP(t, cfg.Secreto, -1)
		_, err := mod.ActivarMFA.Ejecutar(uctx, identidad.EntradaActivarMFA{Codigo: codigo})
		if err == nil {
			break
		}
		if intento > 0 {
			t.Fatal(err)
		}
	}
	return u.ID, correo, cfg.Secreto
}

// codigoTOTP es el código del paso de ahora más desplazamiento (−1, 0, +1: lo que la API acepta).
func codigoTOTP(t *testing.T, secreto string, desplazamiento int) string {
	t.Helper()
	codigo, err := totp.GenerateCode(secreto, time.Now().Add(time.Duration(desplazamiento)*30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return codigo
}

// conectorCon canjea un enlace de Iris (como lo haría el otro sistema) con el cifrador del módulo.
func conectorCon(t *testing.T, enlace *auditoria.Caso[struct{}, conector.EnlaceGenerado],
	canje *auditoria.Caso[conector.EntradaCanjear, conector.ConexionCanjeada], sistema, token string,
) conector.ConexionCanjeada {
	t.Helper()
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	e, err := enlace.Ejecutar(ctx, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	_, codigo, _ := strings.Cut(e.Enlace, "#")
	c, err := canje.Ejecutar(ctx, conector.EntradaCanjear{Codigo: codigo, Sistema: sistema, URL: "https://" + sistema + ".ejemplo.gt", Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// ctxBuzones: un administrador de buzones (sin conector: buzones con empresa_id null).
func ctxBuzones(t *testing.T) context.Context {
	return pruebas.ContextoUsuario(pruebas.Pool(t), 1, nucleo.PermBuzonesCrear, nucleo.PermBuzonesEditar, nucleo.PermBuzonesVer)
}

func moduloCorreo(c *nucleo.Cifrador) *soporte.Modulo {
	return soporte.Nuevo(soporte.Config{Cifrador: c, Correo: soporte.ConfigCorreo{Red: correo.Opciones{
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Timeout: 2 * time.Second,
	}}})
}

// probarIMAP prueba el buzón con el módulo dado y devuelve el código del IMAP (cifrado si no
// pudo descifrar la contraseña; conexion si la descifró e intentó conectar).
func probarIMAP(t *testing.T, m *soporte.Modulo, id int64) (string, *string) {
	t.Helper()
	r, err := m.ProbarBuzon.Ejecutar(ctxBuzones(t), soporte.EntradaVerBuzon{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	var smtp *string
	if r.SMTP != nil {
		smtp = r.SMTP.Codigo
	}
	if r.IMAP.Codigo == nil {
		return "ok", smtp
	}
	return *r.IMAP.Codigo, smtp
}

// foto lee todas las columnas cifradas (para comprobar que algo no se escribió).
func foto(t *testing.T) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	filas, err := pruebas.Pool(t).Query(context.Background(), `
		SELECT 'u' || id, totp_secreto FROM nucleo.usuarios WHERE totp_secreto IS NOT NULL
		UNION ALL SELECT 'ct' || id, token_cifrado FROM integracion.conectores
		UNION ALL SELECT 'cs' || id, secreto_webhook_cifrado FROM integracion.conectores
		UNION ALL SELECT 'bi' || id, imap_contrasena FROM soporte.buzones WHERE imap_contrasena IS NOT NULL
		UNION ALL SELECT 'bs' || id, smtp_contrasena FROM soporte.buzones WHERE smtp_contrasena IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	for filas.Next() {
		var k string
		var v []byte
		if err := filas.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		m[k] = v
	}
	return m
}

func iguales(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

func esProblema(err error, tipo string) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Type == "urn:iris:problema:"+tipo
}

// conteo devuelve el conteo de una columna del resultado.
func conteo(t *testing.T, r clave.Resultado, col string) clave.Conteo {
	t.Helper()
	i := slices.IndexFunc(r.Columnas, func(c clave.Conteo) bool { return c.Columna == col })
	if i < 0 {
		t.Fatalf("sin conteo de %s: %+v", col, r)
	}
	return r.Columnas[i]
}

func exigirConteos(t *testing.T, r clave.Resultado, esperado map[string][3]int) {
	t.Helper()
	for col, e := range esperado {
		c := conteo(t, r, col)
		if c.Total != e[0] || c.Rotadas != e[1] || c.YaRotadas != e[2] {
			t.Fatalf("%s: total %d rotadas %d ya %d; esperaba %v (%+v)", col, c.Total, c.Rotadas, c.YaRotadas, e, r)
		}
	}
}

func limpiarCifrados(t *testing.T) {
	t.Helper()
	for _, sql := range []string{
		"UPDATE nucleo.usuarios SET totp_secreto = NULL, totp_activo = false",
		"DELETE FROM integracion.codigos_conexion", "DELETE FROM integracion.conectores",
		"DELETE FROM soporte.buzones",
	} {
		if _, err := pruebas.Pool(t).Exec(context.Background(), sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
}

// Rotación completa con datos de cada tipo (TOTP, conectores Argos y Horus, buzón con IMAP y
// SMTP), descifrado con las dos claves durante la transición, simulación, ilegibles y
// repetición.
func TestRotarClave(t *testing.T) {
	limpiarCifrados(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	sis := pruebas.ContextoSistema(pool)
	vieja, nueva := claveNueva(t), claveNueva(t)
	cViejo, cNuevo, cTransicion := cifrador(t, vieja), cifrador(t, nueva), cifrador(t, nueva, vieja)

	// Datos cifrados con la clave vieja, por los mismos módulos que los escriben en producción.
	_, modViejo := api(t, cViejo)
	_, correoMFA, secretoMFA := usuarioConMFA(t, modViejo)
	argosViejo := argos.Nuevo(argos.Config{Cifrador: cViejo, BaseURL: "https://iris.ejemplo.gt", Origenes: []string{"https://argosisp.ejemplo.gt"}})
	horusViejo := horus.Nuevo(horus.Config{Argos: argosViejo, Origenes: []string{"https://horus.ejemplo.gt"}})
	tokenArgos, tokenHorus := "atk_"+pruebas.Unico()+"-token-de-argos", "hf_pat_"+pruebas.Unico()+"-token-de-horus"
	cA := conectorCon(t, argosViejo.GenerarEnlace, argosViejo.CanjearConexion, "argosisp", tokenArgos)
	cH := conectorCon(t, horusViejo.GenerarEnlace, horusViejo.CanjearConexion, "horus", tokenHorus)
	claveIMAP, claveSMTP := "imap-"+pruebas.Unico(), "smtp-"+pruebas.Unico()
	usuarioSMTP := "soporte"
	buzon, err := moduloCorreo(cViejo).CrearBuzon.Ejecutar(ctxBuzones(t), soporte.EntradaCrearBuzon{DatosBuzon: soporte.DatosBuzon{
		Nombre: "Soporte", Correo: pruebas.Correo("buzon"),
		IMAP: soporte.EntradaIMAP{Host: "127.0.0.1", Puerto: 1, TLS: "tls", Usuario: "soporte", Contrasena: &claveIMAP},
		SMTP: &soporte.EntradaSMTP{Host: "127.0.0.1", Puerto: 1, TLS: "starttls", Usuario: &usuarioSMTP, Contrasena: &claveSMTP},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Un usuario que configuró su MFA ya con la clave nueva (API en transición): no se toca.
	_, modTransicion := api(t, cTransicion)
	usuarioConMFA(t, modTransicion)

	leerConector := func(c *nucleo.Cifrador, id int64) (conector.Leido, error) {
		f, err := conectorbd.New(pool).ConectorPorID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return argos.Nuevo(argos.Config{Cifrador: c}).Nucleo().Leer(f)
	}

	// Solo con la clave nueva, lo de la vieja es ilegible (lo que pasaba antes de la rotación).
	srvNuevo, _ := api(t, cNuevo)
	if _, err := leerConector(cNuevo, cA.ConectorID); err == nil {
		t.Fatal("el conector se leyó solo con la clave nueva antes de rotar")
	}
	if cod, _ := probarIMAP(t, moduloCorreo(cNuevo), buzon.ID); cod != "cifrado" {
		t.Fatalf("buzón antes de rotar con la nueva: %s", cod)
	}
	codigoActual := codigoTOTP(t, secretoMFA, 0)
	if st := entrarConMFA(t, srvNuevo, correoMFA, codigoActual); st == 200 {
		t.Fatal("el MFA se verificó solo con la clave nueva antes de rotar")
	}

	// Transición (nueva + anterior): la API y el worker leen todo, sin caerse.
	for _, id := range []int64{cA.ConectorID, cH.ConectorID} {
		if l, err := leerConector(cTransicion, id); err != nil || (id == cA.ConectorID && l.Token() != tokenArgos) || (id == cH.ConectorID && l.Token() != tokenHorus) {
			t.Fatalf("conector %d en transición: %v", id, err)
		}
	}
	if cod, smtp := probarIMAP(t, moduloCorreo(cTransicion), buzon.ID); cod != "conexion" || smtp == nil || *smtp != "conexion" {
		t.Fatalf("buzón en transición: imap %s smtp %v", cod, smtp)
	}
	srvTransicion, _ := api(t, cTransicion)
	if st := entrarConMFA(t, srvTransicion, correoMFA, codigoActual); st != 200 {
		t.Fatalf("MFA en transición: %d", st)
	}

	rot := clave.Nuevo(cTransicion)
	esperado := map[string][3]int{
		clave.ColumnaTOTP: {2, 1, 1}, clave.ColumnaTokenConector: {2, 2, 0}, clave.ColumnaSecretoWebhook: {2, 2, 0},
		clave.ColumnaIMAP: {1, 1, 0}, clave.ColumnaSMTP: {1, 1, 0},
	}

	// --simular: cuenta y no escribe.
	antes := foto(t)
	r, err := rot.Rotar.Ejecutar(sis, clave.EntradaRotar{Simular: true})
	if err != nil || !r.Simulada || r.Rotadas() != 7 || len(r.Ilegibles) != 0 {
		t.Fatalf("simular: %+v %v", r, err)
	}
	exigirConteos(t, r, esperado)
	if !iguales(antes, foto(t)) {
		t.Fatal("--simular escribió")
	}

	// Un valor que no abre ninguna de las dos claves: la rotación falla sin escribir nada.
	otra := cifrador(t, claveNueva(t))
	var idIlegible int64
	if err := pool.QueryRow(ctx, `INSERT INTO nucleo.usuarios (nombre, correo) VALUES ('Ilegible', $1) RETURNING id`, pruebas.Correo("ilegible")).Scan(&idIlegible); err != nil {
		t.Fatal(err)
	}
	basura, _ := otra.Cifrar([]byte("JBSWY3DPEHPK3PXP"), identidad.DatosAsociadosTOTP(idIlegible))
	if _, err := pool.Exec(ctx, `UPDATE nucleo.usuarios SET totp_secreto = $2 WHERE id = $1`, idIlegible, basura); err != nil {
		t.Fatal(err)
	}
	antes = foto(t)
	if _, err := rot.Rotar.Ejecutar(sis, clave.EntradaRotar{}); !esProblema(err, "cifrados_ilegibles") {
		t.Fatalf("con un ilegible: %v", err)
	}
	if !iguales(antes, foto(t)) {
		t.Fatal("la rotación fallida escribió")
	}
	if r, err := rot.Rotar.Ejecutar(sis, clave.EntradaRotar{Simular: true}); err != nil || len(r.Ilegibles) != 1 ||
		r.Ilegibles[0] != (clave.Ilegible{Columna: clave.ColumnaTOTP, ID: idIlegible}) || conteo(t, r, clave.ColumnaTOTP).Ilegibles != 1 {
		t.Fatalf("simular con un ilegible: %+v %v", r, err)
	}

	// --ignorar-ilegibles: rota lo legible y deja el ilegible como estaba.
	r, err = rot.Rotar.Ejecutar(sis, clave.EntradaRotar{IgnorarIlegibles: true})
	if err != nil || r.Simulada || r.Rotadas() != 7 || len(r.Ilegibles) != 1 {
		t.Fatalf("rotar: %+v %v", r, err)
	}
	exigirConteos(t, r, map[string][3]int{clave.ColumnaTOTP: {3, 1, 1}})
	var ilegible []byte
	_ = pool.QueryRow(ctx, `SELECT totp_secreto FROM nucleo.usuarios WHERE id = $1`, idIlegible).Scan(&ilegible)
	if !bytes.Equal(ilegible, basura) {
		t.Fatal("el ilegible cambió")
	}
	if _, err := pool.Exec(ctx, `UPDATE nucleo.usuarios SET totp_secreto = NULL WHERE id = $1`, idIlegible); err != nil {
		t.Fatal(err)
	}

	// Después: todo se lee solo con la clave nueva (ya se puede quitar la anterior).
	for _, id := range []int64{cA.ConectorID, cH.ConectorID} {
		if _, err := leerConector(cNuevo, id); err != nil {
			t.Fatalf("conector %d con la nueva: %v", id, err)
		}
	}
	if l, _ := leerConector(cNuevo, cA.ConectorID); l.Token() != tokenArgos {
		t.Fatal("el token de Argos cambió")
	}
	if cod, smtp := probarIMAP(t, moduloCorreo(cNuevo), buzon.ID); cod != "conexion" || smtp == nil || *smtp != "conexion" {
		t.Fatalf("buzón con la nueva: imap %s smtp %v", cod, smtp)
	}
	// El paso siguiente al de ahora: posterior al de la activación (la API rechaza un paso ya usado) y
	// válido aunque se cruce el límite de 30 s antes de validarlo (queda como el actual).
	codigoSiguiente := codigoTOTP(t, secretoMFA, +1)
	if st := entrarConMFA(t, srvNuevo, correoMFA, codigoSiguiente); st != 200 {
		t.Fatalf("MFA con la nueva tras rotar: %d", st)
	}
	if _, err := leerConector(cViejo, cA.ConectorID); err == nil {
		t.Fatal("tras rotar, la clave vieja ya no debe abrir nada")
	}

	// Repetida: lo ya rotado se reconoce y no se toca.
	antes = foto(t)
	r, err = rot.Rotar.Ejecutar(sis, clave.EntradaRotar{})
	if err != nil || r.Rotadas() != 0 {
		t.Fatalf("repetida: %+v %v", r, err)
	}
	exigirConteos(t, r, map[string][3]int{
		clave.ColumnaTOTP: {2, 0, 2}, clave.ColumnaTokenConector: {2, 0, 2}, clave.ColumnaSecretoWebhook: {2, 0, 2},
		clave.ColumnaIMAP: {1, 0, 1}, clave.ColumnaSMTP: {1, 0, 1},
	})
	if !iguales(antes, foto(t)) {
		t.Fatal("la rotación repetida reescribió valores")
	}

	// Auditoría: acción de sistema por consola, con conteos y sin secretos ni claves.
	filas, err := pool.Query(ctx, `SELECT actor_tipo, canal, resultado, entrada::text FROM auditoria.acciones WHERE accion = 'clave.rotar' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var resultados []string
	for filas.Next() {
		var actor, canal, resultado, entrada string
		if err := filas.Scan(&actor, &canal, &resultado, &entrada); err != nil {
			t.Fatal(err)
		}
		if actor != "sistema" || canal != "consola" {
			t.Fatalf("actor %s canal %s", actor, canal)
		}
		for _, sensible := range []string{tokenArgos, tokenHorus, claveIMAP, claveSMTP, secretoMFA, vieja, nueva} {
			if strings.Contains(entrada, sensible) {
				t.Fatalf("la auditoría contiene un secreto: %s", entrada)
			}
		}
		if !strings.Contains(entrada, `"rotadas"`) {
			t.Fatalf("la auditoría no trae los conteos: %s", entrada)
		}
		resultados = append(resultados, resultado)
	}
	if !slices.Equal(resultados, []string{"ok", "error", "ok", "ok", "ok"}) {
		t.Fatalf("filas de auditoría: %v", resultados)
	}
}

// Sin la clave anterior, o desde un usuario (aunque sea admin), no se rota.
func TestRotarClaveExigeConsolaYClaveAnterior(t *testing.T) {
	pool := pruebas.Pool(t)
	nueva := claveNueva(t)
	if _, err := clave.Nuevo(cifrador(t, nueva)).Rotar.Ejecutar(pruebas.ContextoSistema(pool), clave.EntradaRotar{}); !esProblema(err, "sin_clave_anterior") {
		t.Fatalf("sin anterior: %v", err)
	}
	if _, err := clave.Nuevo(nil).Rotar.Ejecutar(pruebas.ContextoSistema(pool), clave.EntradaRotar{}); !esProblema(err, "cifrado_no_configurado") {
		t.Fatalf("sin clave: %v", err)
	}
	permisos := make([]nucleo.Permiso, 0, len(nucleo.Catalogo))
	for _, p := range nucleo.Catalogo {
		permisos = append(permisos, p.Codigo)
	}
	uctx := pruebas.ContextoUsuario(pool, 1, permisos...)
	if _, err := clave.Nuevo(cifrador(t, nueva, claveNueva(t))).Rotar.Ejecutar(uctx, clave.EntradaRotar{}); !esProblema(err, "prohibido") {
		t.Fatalf("desde un usuario: %v", err)
	}
}

// Ninguna columna cifrada queda fuera: toda columna bytea de la base es un hash, un dato que
// no se cifra con la clave, o está en clave.Columnas. Si agregas una columna cifrada con
// nucleo.Cifrador, súmala a la rotación; si agregas otro bytea, a esta lista.
func TestTodasLasColumnasCifradasSeRotan(t *testing.T) {
	noCifradas := []string{
		"nucleo.idempotencia.respuesta", // cuerpo de la respuesta guardada (nunca un secreto: NoRepetible)
		"nucleo.adjuntos_partes.datos",  // partes de una subida tus
		// Semilla aleatoria del enlace de la encuesta (no cifrada: el token es HMAC con una clave
		// derivada de IRIS_CLAVE_CIFRADO; tras rotar sin la anterior, el enlace ya no se muestra).
		"soporte.encuestas.semilla",
	}
	filas, err := pruebas.Pool(t).Query(context.Background(), `
		SELECT table_schema || '.' || table_name || '.' || column_name FROM information_schema.columns
		WHERE data_type = 'bytea' AND table_schema NOT IN ('pg_catalog', 'information_schema', 'river')
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var sinRotar []string
	for filas.Next() {
		var col string
		if err := filas.Scan(&col); err != nil {
			t.Fatal(err)
		}
		nombre := col[strings.LastIndex(col, ".")+1:]
		if strings.Contains(nombre, "hash") || slices.Contains(noCifradas, col) || slices.Contains(clave.Columnas, col) {
			continue
		}
		sinRotar = append(sinRotar, col)
	}
	if len(sinRotar) > 0 {
		t.Fatalf("columnas bytea sin clasificar (¿cifradas con IRIS_CLAVE_CIFRADO? súmalas a internal/nucleo/clave): %v", sinRotar)
	}
}

// La contraseña del correo del sistema (notificaciones.correo_ajustes) también se rota. Va después de
// TestRotarClave, que cuenta las filas de auditoría de clave.rotar.
func TestRotarClaveCorreoSistema(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	limpiar := func() {
		if _, err := pool.Exec(ctx, `DELETE FROM notificaciones.correo_ajustes`); err != nil {
			t.Fatal(err)
		}
	}
	limpiar()
	t.Cleanup(limpiar)
	limpiarCifrados(t) // lo que dejó TestRotarClave está cifrado con otras claves
	vieja, nueva := claveNueva(t), claveNueva(t)
	cViejo, cNuevo, cTransicion := cifrador(t, vieja), cifrador(t, nueva), cifrador(t, nueva, vieja)

	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO notificaciones.correo_ajustes (host, puerto, seguridad, usuario, remitente_correo)
		VALUES ('smtp.ejemplo.gt', 587, 'starttls', 'avisos', 'avisos@ejemplo.gt') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	const secreto = "clave-del-smtp-del-sistema"
	cifrada, err := cViejo.Cifrar([]byte(secreto), notificaciones.DatosAsociadosCorreo(id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE notificaciones.correo_ajustes SET contrasena_cifrada = $1 WHERE id = $2`, cifrada, id); err != nil {
		t.Fatal(err)
	}
	leer := func() []byte {
		var b []byte
		if err := pool.QueryRow(ctx, `SELECT contrasena_cifrada FROM notificaciones.correo_ajustes WHERE id = $1`, id).Scan(&b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if _, err := cNuevo.Descifrar(leer(), notificaciones.DatosAsociadosCorreo(id)); err == nil {
		t.Fatal("se leyó con la clave nueva antes de rotar")
	}
	r, err := clave.Nuevo(cTransicion).Rotar.Ejecutar(pruebas.ContextoSistema(pool), clave.EntradaRotar{})
	if err != nil {
		t.Fatal(err)
	}
	exigirConteos(t, r, map[string][3]int{clave.ColumnaCorreoSistema: {1, 1, 0}})
	plano, err := cNuevo.Descifrar(leer(), notificaciones.DatosAsociadosCorreo(id))
	if err != nil || string(plano) != secreto {
		t.Fatalf("tras rotar: %q %v", plano, err)
	}
	// Atada a su fila: con los datos de otra no abre.
	if _, err := cNuevo.Descifrar(leer(), notificaciones.DatosAsociadosCorreo(id+1)); err == nil {
		t.Fatal("la contraseña abrió con los datos asociados de otra fila")
	}
}

// La contraseña del AMI de la centralita se lee con candado (FOR UPDATE), como las demás columnas
// que se rotan: si un administrador la cambia mientras corre la rotación, la rotación espera y no
// pisa la nueva con la vieja re-cifrada.
func TestRotarClaveCentralitaConCandado(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	limpiar := func() {
		for _, sql := range []string{
			`DELETE FROM mensajeria.conv_eventos WHERE conversacion_id IS NULL`,
			`DELETE FROM mensajeria.centralita_receptores`,
			`DELETE FROM mensajeria.llamadas WHERE origen = 'centralita'`,
			`DELETE FROM mensajeria.centralita_extensiones`,
			`DELETE FROM mensajeria.centralitas`,
		} {
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
	}
	limpiar()
	t.Cleanup(limpiar)
	limpiarCifrados(t)
	vieja, nueva := claveNueva(t), claveNueva(t)
	cViejo, cNuevo, cTransicion := cifrador(t, vieja), cifrador(t, nueva), cifrador(t, nueva, vieja)

	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO mensajeria.centralitas (empresa_id, nombre, host, usuario) VALUES (1, 'Oficina', '10.0.0.5', 'iris_lectura')
		RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ad := mensajeria.DatosAsociadosCentralita(id)
	antes, err := cViejo.Cifrar([]byte("la-vieja-del-ami"), ad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mensajeria.centralitas SET secreto_cifrado = $1 WHERE id = $2`, antes, id); err != nil {
		t.Fatal(err)
	}
	// Un administrador guarda otra contraseña (ya con la clave nueva) en una transacción sin terminar.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	despues, err := cNuevo.Cifrar([]byte("la-nueva-del-ami"), ad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE mensajeria.centralitas SET secreto_cifrado = $1 WHERE id = $2`, despues, id); err != nil {
		t.Fatal(err)
	}
	hecho := make(chan error, 1)
	go func() {
		_, err := clave.Nuevo(cTransicion).Rotar.Ejecutar(pruebas.ContextoSistema(pool), clave.EntradaRotar{IgnorarIlegibles: true})
		hecho <- err
	}()
	// La rotación queda esperando el candado de la fila.
	limite := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'
			AND query ILIKE '%mensajeria.centralitas%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("la rotación no llegó a la fila de la centralita")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-hecho; err != nil {
		t.Fatal(err)
	}
	var b []byte
	if err := pool.QueryRow(ctx, `SELECT secreto_cifrado FROM mensajeria.centralitas WHERE id = $1`, id).Scan(&b); err != nil {
		t.Fatal(err)
	}
	plano, err := cNuevo.Descifrar(b, ad)
	if err != nil || string(plano) != "la-nueva-del-ami" {
		t.Fatalf("la rotación pisó la contraseña nueva: %q %v", plano, err)
	}
}
