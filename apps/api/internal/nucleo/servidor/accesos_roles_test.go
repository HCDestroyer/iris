package servidor_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Pedido 10 del PM: historial de accesos, sesiones activas, actividad de la app de campo y roles
// propios. Contra Postgres real y por HTTP (middleware, CSRF, permisos y auditoría reales).

func filas(t *testing.T, r respuesta) []map[string]any {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("lista: %d %s", r.status, r.crudo)
	}
	crudo, _ := r.cuerpo["data"].([]any)
	salida := make([]map[string]any, 0, len(crudo))
	for _, f := range crudo {
		salida = append(salida, f.(map[string]any))
	}
	return salida
}

func entrarCabeceras(c *cliente, correo, clave string, cabeceras ...string) respuesta {
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": clave}, cabeceras...)
	if r.status == http.StatusOK {
		c.csrf, _ = r.cuerpo["csrfToken"].(string)
	}
	return r
}

func reautenticar(t *testing.T, c *cliente) {
	t.Helper()
	if r := c.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": contrasena}); r.status != http.StatusOK {
		t.Fatalf("reautenticar: %d %s", r.status, r.crudo)
	}
}

func eventos(t *testing.T, evento string, campo string, valor int64) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>$2)::bigint = $3`, evento, campo, valor).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func auditadas(t *testing.T, accion, entidadID string) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM auditoria.acciones WHERE accion = $1 AND entidad_id = $2 AND resultado = 'ok'`, accion, entidadID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHistorialDeAccesos(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	idLec, correoLec := e.usuario("lectura")
	_, correoSop := e.usuario("soporte")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	// Un correo que no existe y una contraseña errónea responden igual (no se enumeran cuentas).
	nadie := entrarCon(e.cliente(), pruebas.Correo("nadie"), "lo-que-sea-1234")
	mala := entrarCon(e.cliente(), correoLec, "no-es-la-clave-123")
	nadie.problema(t, 401, "credenciales_invalidas")
	mala.problema(t, 401, "credenciales_invalidas")
	if nadie.cuerpo["detail"] != mala.cuerpo["detail"] {
		t.Fatalf("las respuestas difieren: %s / %s", nadie.crudo, mala.crudo)
	}

	// Entra por la app nativa, por la PWA de campo y por el panel.
	nativa := e.cliente()
	if r := entrarCabeceras(nativa, correoLec, contrasena, identidad.CabeceraApp, "campo/1.4.0 (android)",
		"User-Agent", "okhttp/4.12"); r.status != 200 {
		t.Fatalf("entrar nativa: %d %s", r.status, r.crudo)
	}
	pwa := e.cliente()
	if r := entrarCabeceras(pwa, correoLec, contrasena, identidad.CabeceraCanal, "campo",
		"User-Agent", "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/130.0 Mobile Safari/537.36"); r.status != 200 {
		t.Fatalf("entrar pwa: %d %s", r.status, r.crudo)
	}
	panel := e.cliente()
	panel.entrar(correoLec)

	// Sin accesos.ver: 403.
	sop := e.cliente()
	sop.entrar(correoSop)
	sop.pedir(http.MethodGet, "/api/v1/accesos", nil).problema(t, 403, "prohibido")

	propios := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos", idLec), nil))
	if len(propios) != 4 {
		t.Fatalf("accesos de lectura: %d %v", len(propios), propios)
	}
	// Del más reciente al más antiguo: panel, PWA, nativa, contraseña errónea.
	esperados := [][2]string{{"panel", "exito"}, {"campo_pwa", "exito"}, {"campo_nativa", "exito"}, {"panel", "contrasena_erronea"}}
	for i, f := range propios {
		if f["canal"] != esperados[i][0] || f["resultado"] != esperados[i][1] {
			t.Fatalf("acceso %d: %v", i, f)
		}
	}
	if propios[1]["dispositivo"] != "Chrome · Android" || propios[2]["dispositivo"] != "App de campo 1.4.0 · Android" || propios[2]["app"] != "campo/1.4.0 (android)" {
		t.Fatalf("dispositivo: %v / %v", propios[1], propios[2])
	}
	if propios[0]["sesionId"] == nil || propios[3]["sesionId"] != nil || propios[0]["ip"] == "" {
		t.Fatalf("sesión e IP: %v", propios)
	}

	// El intento con el correo inexistente queda sin usuario y sin el correo.
	errados := filas(t, admin.pedir(http.MethodGet, "/api/v1/accesos?resultado=contrasena_erronea", nil))
	sinUsuario := 0
	for _, f := range errados {
		if f["usuarioId"] == nil {
			sinUsuario++
			if f["usuarioCorreo"] != nil {
				t.Fatalf("guardó un correo: %v", f)
			}
		}
	}
	if sinUsuario != 1 || len(errados) != 2 {
		t.Fatalf("contraseñas erróneas: %v", errados)
	}
	var correos int
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT count(*) FROM nucleo.accesos a
		WHERE a.usuario_id IS NULL AND (a.user_agent LIKE '%nadie%' OR a.dispositivo LIKE '%nadie%')`).Scan(&correos); err != nil || correos != 0 {
		t.Fatalf("correo guardado: %d %v", correos, err)
	}

	// Filtros por canal y fecha, y paginación por cursor.
	if f := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/accesos?canal=campo_nativa&usuarioId=%d", idLec), nil)); len(f) != 1 {
		t.Fatalf("filtro canal: %v", f)
	}
	futuro := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if f := filas(t, admin.pedir(http.MethodGet, "/api/v1/accesos?desde="+futuro, nil)); len(f) != 0 {
		t.Fatalf("filtro desde: %v", f)
	}
	admin.pedir(http.MethodGet, "/api/v1/accesos?desde="+futuro+"&hasta="+time.Now().UTC().Format(time.RFC3339), nil).problema(t, 422, "validacion")
	p1 := admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos?limit=3", idLec), nil)
	cursor, _ := p1.cuerpo["nextCursor"].(string)
	if len(filas(t, p1)) != 3 || cursor == "" {
		t.Fatalf("página 1: %s", p1.crudo)
	}
	p2 := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos?limit=3&cursor=%s", idLec, cursor), nil))
	if len(p2) != 1 || p2[0]["resultado"] != "contrasena_erronea" {
		t.Fatalf("página 2: %v", p2)
	}
	admin.pedir(http.MethodGet, "/api/v1/usuarios/999999999/accesos", nil).problema(t, 404, "no_encontrado")

	// MFA fallido y bloqueo por fuerza bruta.
	idMFA, correoMFA := e.usuario("lectura")
	conMFA(t, e, correoMFA)
	otra := e.cliente()
	if r := otra.entrar(correoMFA); r.cuerpo["mfaRequerido"] != true {
		t.Fatalf("login con MFA: %s", r.crudo)
	}
	otra.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": "000000"}).problema(t, 401, "codigo_invalido")
	idBloq, correoBloq := e.usuario("lectura")
	for range 10 {
		entrarCon(e.cliente(), correoBloq, "mala-mala-mala-1")
	}
	entrarCon(e.cliente(), correoBloq, contrasena).problema(t, 429, "demasiados_intentos")
	mfa := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos?resultado=mfa_fallido", idMFA), nil))
	bloq := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos?resultado=bloqueado", idBloq), nil))
	if len(mfa) != 1 || len(bloq) != 1 {
		t.Fatalf("mfa %v bloqueado %v", mfa, bloq)
	}
	// Un token deja una fila por hora, no una por petición.
	idTok, _ := e.usuario("lectura")
	bot, tokenID := tokenPara(t, e, idTok)
	for range 3 {
		if r := bot.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
			t.Fatalf("token: %d %s", r.status, r.crudo)
		}
	}
	tok := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos?canal=token", idTok), nil))
	if len(tok) != 1 || tok[0]["tokenId"] != float64(tokenID) {
		t.Fatalf("token: %v", tok)
	}
}

func TestMFAExitoQuedaAlVerificar(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	id, correo := e.usuario("lectura")
	_, secreto, _ := conMFA(t, e, correo)
	admin := e.cliente()
	admin.entrar(correoAdmin)
	antes := len(filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos", id), nil)))
	c := e.cliente()
	c.entrar(correo)
	if n := len(filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos", id), nil))); n != antes {
		t.Fatalf("la contraseña sola no es un acceso: %d → %d", antes, n)
	}
	// conMFA ya usó el código de este paso: el siguiente.
	codigo, _ := totp.GenerateCode(secreto, time.Now().Add(30*time.Second))
	if r := c.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": codigo}); r.status != 200 {
		t.Fatalf("mfa: %d %s", r.status, r.crudo)
	}
	todos := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/accesos", id), nil))
	if len(todos) != antes+1 || todos[0]["resultado"] != "exito" {
		t.Fatalf("éxito tras el MFA: %v", todos)
	}
}

func TestSesionesActivasYCerrarlas(t *testing.T) {
	e := nuevoEntorno(t)
	idAdmin, correoAdmin := e.usuario("admin")
	idLec, correoLec := e.usuario("lectura")
	_, correoSup := e.usuario("supervisor")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	uno, dos := e.cliente(), e.cliente()
	uno.entrar(correoLec)
	entrarCabeceras(dos, correoLec, contrasena, identidad.CabeceraApp, "campo/2.0.1 (pwa)")

	ses := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/sesiones", idLec), nil))
	if len(ses) != 2 {
		t.Fatalf("sesiones: %v", ses)
	}
	canales := []string{ses[0]["canal"].(string), ses[1]["canal"].(string)}
	slices.Sort(canales)
	if !slices.Equal(canales, []string{"campo_pwa", "panel"}) || ses[0]["actual"] != false {
		t.Fatalf("canales: %v", ses)
	}
	var idUno float64
	for _, s := range ses {
		if s["canal"] == "panel" {
			idUno = s["id"].(float64)
		}
	}

	// Supervisor: ni ve las sesiones ni las cierra (accesos.ver y usuarios.cerrar_sesiones son de admin).
	sup := e.cliente()
	sup.entrar(correoSup)
	sup.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/sesiones", idLec), nil).problema(t, 403, "prohibido")
	sup.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", idLec), map[string]any{}).problema(t, 403, "prohibido")

	// Cerrar una: quien la usaba queda fuera; la otra sigue.
	r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/sesiones/%d/cerrar", idLec, int64(idUno)), map[string]any{"motivo": "teléfono perdido"})
	if r.status != 200 || r.cuerpo["cerradas"] != float64(1) {
		t.Fatalf("cerrar una: %d %s", r.status, r.crudo)
	}
	uno.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	if r := dos.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("la otra sigue: %d", r.status)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/sesiones/%d/cerrar", idLec, int64(idUno)), map[string]any{}).problema(t, 404, "no_encontrado")
	// Una sesión de otro usuario no se cierra por la ruta de este.
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/sesiones/%d/cerrar", idAdmin, int64(idUno)), map[string]any{}).problema(t, 404, "no_encontrado")

	// Cerrar todas.
	r = admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", idLec), map[string]any{"motivo": "baja"})
	if r.status != 200 || r.cuerpo["cerradas"] != float64(1) {
		t.Fatalf("cerrar todas: %d %s", r.status, r.crudo)
	}
	dos.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	if n := eventos(t, "usuario.sesiones_cerradas", "usuario_id", idLec); n != 2 {
		t.Fatalf("eventos: %d", n)
	}
	if auditadas(t, "usuarios.cerrar_sesion", fmt.Sprint(idLec)) != 1 || auditadas(t, "usuarios.cerrar_sesiones", fmt.Sprint(idLec)) != 1 {
		t.Fatal("falta la auditoría")
	}
	// Sin sesiones: 0 y sin evento.
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", idLec), map[string]any{}); r.cuerpo["cerradas"] != float64(0) {
		t.Fatalf("sin sesiones: %s", r.crudo)
	}

	// Las propias: todas menos la actual.
	otraAdmin := e.cliente()
	otraAdmin.entrar(correoAdmin)
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", idAdmin), map[string]any{}); r.cuerpo["cerradas"] != float64(1) {
		t.Fatalf("propias: %s", r.crudo)
	}
	otraAdmin.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	if r := admin.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("la actual sigue: %d", r.status)
	}
	actual := filas(t, admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/sesiones", idAdmin), nil))
	if len(actual) != 1 || actual[0]["actual"] != true {
		t.Fatalf("sesión actual: %v", actual)
	}

	// Nadie cierra las sesiones de quien tiene más permisos que él.
	reautenticar(t, admin)
	if r := admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "cierra_sesiones", "nombre": "Cierra sesiones",
		"permisos": []string{"usuarios.cerrar_sesiones", "usuarios.ver"}}); r.status != 201 {
		t.Fatalf("crear rol: %d %s", r.status, r.crudo)
	}
	_, correoCierra := e.usuario("cierra_sesiones")
	cierra := e.cliente()
	cierra.entrar(correoCierra)
	cierra.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", idAdmin), map[string]any{}).problema(t, 403, "prohibido")
}

func TestActividadDeLaAppDeCampo(t *testing.T) {
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	_, correoAdmin := e.usuario("admin")
	_, correoDesp := e.usuario("despacho")
	_, correoLec := e.usuario("lectura")
	idTec, correoTec := e.usuario("tecnico")
	idSin, _ := e.usuario("tecnico")
	for _, id := range []int64{idTec, idSin} {
		if _, err := pool.Exec(ctx, `INSERT INTO agenda.tecnicos (usuario_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	buscar := func(c *cliente, ruta string, id int64) map[string]any {
		t.Helper()
		r := c.pedir(http.MethodGet, ruta, nil)
		if r.status != 200 || r.cuerpo["sinConexionMin"] != float64(30) {
			t.Fatalf("actividad: %d %s", r.status, r.crudo)
		}
		for _, f := range filas(t, r) {
			if f["usuarioId"] == float64(id) {
				return f
			}
		}
		return nil
	}

	// La app manda versión, plataforma y pendientes en cada petición.
	tec := e.cliente()
	entrarCabeceras(tec, correoTec, contrasena, identidad.CabeceraApp, "campo/1.4.0 (android)")
	if r := tec.pedir(http.MethodGet, "/api/v1/yo", nil, identidad.CabeceraApp, "campo/1.4.0 (android)", identidad.CabeceraPendientes, "3"); r.status != 200 {
		t.Fatalf("yo: %d", r.status)
	}

	lec := e.cliente()
	lec.entrar(correoLec)
	lec.pedir(http.MethodGet, "/api/v1/actividad-campo", nil).problema(t, 403, "prohibido")
	desp := e.cliente()
	desp.entrar(correoDesp)
	admin := e.cliente()
	admin.entrar(correoAdmin)

	f := buscar(desp, "/api/v1/actividad-campo", idTec)
	app, _ := f["app"].(map[string]any)
	if f == nil || f["estado"] != "atrasado" || f["pendientes"] != float64(3) || app["version"] != "1.4.0" || app["plataforma"] != "android" || f["ultimaSincronizacion"] != nil {
		t.Fatalf("atrasado: %v", f)
	}
	if s := buscar(desp, "/api/v1/actividad-campo", idSin); s == nil || s["estado"] != "sin_conexion" || s["ultimaConexion"] != nil || s["app"] != nil {
		t.Fatalf("sin conexión: %v", s)
	}

	// Sincroniza (un lote registra el dispositivo), la app informa 0 pendientes: al día.
	if _, err := pool.Exec(ctx, `INSERT INTO campo.dispositivos (id, usuario_id, plataforma, ultimo_contacto) VALUES ($1, $2, 'android', now())`,
		"tel-"+pruebas.Unico(), idTec); err != nil {
		t.Fatal(err)
	}
	tec.pedir(http.MethodGet, "/api/v1/yo", nil, identidad.CabeceraApp, "campo/1.4.0 (android)", identidad.CabeceraPendientes, "0")
	if f := buscar(admin, "/api/v1/actividad-campo?estado=al_dia", idTec); f == nil || f["ultimaSincronizacion"] == nil || f["pendientes"] != float64(0) {
		t.Fatalf("al día: %v", f)
	}
	if f := buscar(admin, "/api/v1/actividad-campo?estado=atrasado", idTec); f != nil {
		t.Fatalf("filtro por estado: %v", f)
	}

	// Rechazos que siguen así y la última posición.
	var dispositivo string
	if err := pool.QueryRow(ctx, `SELECT id FROM campo.dispositivos WHERE usuario_id = $1`, idTec).Scan(&dispositivo); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campo.operaciones_sync (id, dispositivo_id, usuario_id, tipo, estado, resultado)
		VALUES (gen_random_uuid(), $1, $2, 'nota.agregar', 'rechazada', '{}')`, dispositivo, idTec); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campo.posiciones_ultimas (tecnico_id, lat, lon, precision_m, registrada_en, recibida_en)
		VALUES ($1, 14.6, -90.5, 8, now() - interval '1 minute', now())`, idTec); err != nil {
		t.Fatal(err)
	}
	f = buscar(admin, "/api/v1/actividad-campo", idTec)
	pos, _ := f["ultimaPosicion"].(map[string]any)
	if f["rechazadas"] != float64(1) || f["ultimoRechazo"] == nil || pos["lat"] != 14.6 || pos["precisionM"] != float64(8) {
		t.Fatalf("rechazos y posición: %v", f)
	}

	// Hace dos horas que no se conecta: sin conexión.
	for _, q := range []string{
		`UPDATE nucleo.conexiones_app SET ultima_conexion = now() - interval '2 hours' WHERE usuario_id = $1`,
		`UPDATE campo.dispositivos SET ultimo_contacto = now() - interval '2 hours' WHERE usuario_id = $1`,
		`UPDATE campo.posiciones_ultimas SET recibida_en = now() - interval '2 hours' WHERE tecnico_id = $1`,
	} {
		if _, err := pool.Exec(ctx, q, idTec); err != nil {
			t.Fatal(err)
		}
	}
	if f := buscar(admin, "/api/v1/actividad-campo", idTec); f["estado"] != "sin_conexion" || f["ultimaConexion"] == nil {
		t.Fatalf("sin conexión hace 2 h: %v", f)
	}

	// Una cabecera mal formada no se registra.
	tec.pedir(http.MethodGet, "/api/v1/yo", nil, identidad.CabeceraApp, "campo 1.4.0 android")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM nucleo.conexiones_app WHERE usuario_id = $1`, idTec).Scan(&n); err != nil || n != 1 {
		t.Fatalf("conexiones: %d %v", n, err)
	}
}

func TestLeerCabeceraApp(t *testing.T) {
	casos := map[string][3]string{
		"campo/1.4.0 (android)":    {"campo", "1.4.0", "android"},
		"campo/0.9.2-beta.1 (pwa)": {"campo", "0.9.2-beta.1", "pwa"},
		"campo/3":                  {"campo", "3", "desconocida"},
	}
	for valor, esperado := range casos {
		app, version, plataforma, ok := identidad.LeerApp(valor)
		if !ok || app != esperado[0] || version != esperado[1] || plataforma != esperado[2] {
			t.Errorf("%q: %s %s %s %v", valor, app, version, plataforma, ok)
		}
	}
	for _, malo := range []string{"", "campo", "campo/ (android)", "Campo/1 (android)", "campo/1 (Android)", "campo/1 (android) extra"} {
		if _, _, _, ok := identidad.LeerApp(malo); ok {
			t.Errorf("%q no debería aceptarse", malo)
		}
	}
	ahora := time.Now()
	hace := func(d time.Duration) *time.Time { x := ahora.Add(-d); return &x }
	tres, cero := int32(3), int32(0)
	if identidad.EstadoActividad(ahora, nil, nil, nil) != identidad.EstadoSinConexion ||
		identidad.EstadoActividad(ahora, hace(31*time.Minute), hace(time.Minute), &cero) != identidad.EstadoSinConexion ||
		identidad.EstadoActividad(ahora, hace(time.Minute), hace(20*time.Minute), &tres) != identidad.EstadoAtrasado ||
		identidad.EstadoActividad(ahora, hace(time.Minute), nil, &tres) != identidad.EstadoAtrasado ||
		identidad.EstadoActividad(ahora, hace(time.Minute), hace(5*time.Minute), &tres) != identidad.EstadoAlDia ||
		identidad.EstadoActividad(ahora, hace(time.Minute), nil, nil) != identidad.EstadoAlDia {
		t.Error("EstadoActividad")
	}
	if d := identidad.ResumirDispositivo(identidad.ClienteHTTP{UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0"}); d != "Edge · Windows" {
		t.Errorf("Edge: %q", d)
	}
}

func TestRolesPropios(t *testing.T) {
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	_, correoAdmin := e.usuario("admin")
	idLec, correoLec := e.usuario("lectura")
	_, correoSup := e.usuario("supervisor")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	// Catálogo agrupado por módulo y con los sensibles marcados.
	cat := filas(t, admin.pedir(http.MethodGet, "/api/v1/permisos", nil))
	for _, p := range cat {
		if p["codigo"] == "roles.crear" && (p["sensible"] != true || p["modulo"] != "roles" || p["moduloNombre"] != "Roles y permisos") {
			t.Fatalf("roles.crear: %v", p)
		}
		if p["codigo"] == "tickets.ver" && (p["sensible"] != false || p["modulo"] != "soporte") {
			t.Fatalf("tickets.ver: %v", p)
		}
	}

	// Supervisor ve roles pero no los crea.
	sup := e.cliente()
	sup.entrar(correoSup)
	sup.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "x_sup", "nombre": "X", "permisos": []string{"tickets.ver"}}).problema(t, 403, "prohibido")

	// Validación: sin permisos, permiso inexistente, código inválido.
	admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "vacio", "nombre": "Vacío", "permisos": []string{}}).problema(t, 422, "validacion")
	admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "malo", "nombre": "Malo", "permisos": []string{"no.existe"}}).problema(t, 422, "validacion")
	admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "Con Espacio", "nombre": "X", "permisos": []string{"tickets.ver"}}).problema(t, 422, "validacion")

	// Sin permisos sensibles no hace falta re-autenticarse.
	r := admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "mesa_ayuda", "nombre": "Mesa de ayuda",
		"descripcion": "Solo ve tickets", "permisos": []string{"tickets.ver", "tickets.ver"}})
	if r.status != 201 || r.cuerpo["sistema"] != false || r.cuerpo["activo"] != true || len(r.cuerpo["permisos"].([]any)) != 1 {
		t.Fatalf("crear: %d %s", r.status, r.crudo)
	}
	idRol := int64(r.cuerpo["id"].(float64))
	version := r.cuerpo["version"].(float64)
	admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "mesa_ayuda", "nombre": "Otra", "permisos": []string{"tickets.ver"}}).problema(t, 409, "rol_existe")
	admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "admin", "nombre": "Otro admin", "permisos": []string{"tickets.ver"}}).problema(t, 409, "rol_existe")

	// Con un permiso sensible: re-autenticación (desde una sesión con contraseña también).
	admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "jefe_buzones", "nombre": "Jefe de buzones",
		"permisos": []string{"buzones.ver", "buzones.crear"}}).problema(t, 403, "reautenticacion_requerida")
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/editar", idRol), map[string]any{"version": version,
		"permisos": []string{"tickets.ver", "tokens.crear"}}).problema(t, 403, "reautenticacion_requerida")
	// Un token no puede dar permisos sensibles.
	idAdmin2, _ := e.usuario("admin")
	bot, _ := tokenPara(t, e, idAdmin2)
	bot.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "por_token", "nombre": "Por token",
		"permisos": []string{"buzones.crear"}}).problema(t, 403, "requiere_sesion")

	// Los roles base no se editan ni se desactivan.
	roles := filas(t, admin.pedir(http.MethodGet, "/api/v1/roles?limit=200", nil))
	var idLectura, versionLectura float64
	for _, x := range roles {
		if x["codigo"] == "lectura" {
			idLectura, versionLectura = x["id"].(float64), x["version"].(float64)
			if x["sistema"] != true || x["usuarios"].(float64) < 1 {
				t.Fatalf("lectura: %v", x)
			}
		}
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/editar", int64(idLectura)), map[string]any{"version": versionLectura, "nombre": "X"}).problema(t, 422, "rol_base")
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/desactivar", int64(idLectura)), map[string]any{"version": versionLectura}).problema(t, 422, "rol_base")

	// Editar: nombre y permisos (sin sensibles); versión vieja → 409.
	r = admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/editar", idRol), map[string]any{"version": version,
		"nombre": "Mesa de ayuda N1", "permisos": []string{"tickets.ver", "tickets.responder"}})
	if r.status != 200 || r.cuerpo["nombre"] != "Mesa de ayuda N1" || len(r.cuerpo["permisos"].([]any)) != 2 {
		t.Fatalf("editar: %d %s", r.status, r.crudo)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/editar", idRol), map[string]any{"version": version, "nombre": "Y"}).problema(t, 409, "conflicto_version")
	version = r.cuerpo["version"].(float64)

	// Asignar a un usuario, con alcance de zona (con empresa: no es un dato «sin empresa» que
	// otras pruebas del paquete adopten).
	zona := time.Now().UnixNano() % 1_000_000_000
	if _, err := pool.Exec(ctx, `INSERT INTO agenda.zonas (id, empresa_id, origen, nombre) VALUES ($1, 777777, 'local', 'Norte')`, zona); err != nil {
		t.Fatal(err)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-rol", idLec), map[string]any{"rol": "mesa_ayuda", "zonaId": zona, "cuadrillaId": 1}).problema(t, 422, "validacion")
	r = admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-rol", idLec), map[string]any{"rol": "mesa_ayuda", "zonaId": zona})
	if r.status != 200 || len(r.cuerpo["roles"].([]any)) != 2 {
		t.Fatalf("asignar: %d %s", r.status, r.crudo)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-rol", idLec), map[string]any{"rol": "mesa_ayuda", "zonaId": zona}).problema(t, 422, "rol_ya_asignado")
	// Asignar un rol base con permisos sensibles: re-autenticación.
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-rol", idLec), map[string]any{"rol": "admin"}).problema(t, 403, "reautenticacion_requerida")
	// Quien no tiene todos los permisos del rol no lo asigna.
	sup.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-rol", idLec), map[string]any{"rol": "mesa_ayuda"}).problema(t, 403, "prohibido")

	lec := e.cliente()
	lec.entrar(correoLec)
	permisos := func() []any {
		r := lec.pedir(http.MethodGet, "/api/v1/yo", nil)
		ps, _ := r.cuerpo["permisos"].([]any)
		return ps
	}
	if !slices.Contains(permisos(), any("tickets.responder")) {
		t.Fatalf("tras asignar: %v", permisos())
	}

	// Desactivar: deja de dar permisos y no se asigna; activar los devuelve.
	r = admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/desactivar", idRol), map[string]any{"version": version, "motivo": "reorganización"})
	if r.status != 200 || r.cuerpo["activo"] != false {
		t.Fatalf("desactivar: %d %s", r.status, r.crudo)
	}
	version = r.cuerpo["version"].(float64)
	if slices.Contains(permisos(), any("tickets.responder")) {
		t.Fatal("un rol desactivado no da permisos")
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-rol", idLec), map[string]any{"rol": "mesa_ayuda"}).problema(t, 422, "rol_inactivo")
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/desactivar", idRol), map[string]any{"version": version}).problema(t, 422, "rol_inactivo")
	r = admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/roles/%d/activar", idRol), map[string]any{"version": version})
	if r.status != 200 || r.cuerpo["activo"] != true || !slices.Contains(permisos(), any("tickets.responder")) {
		t.Fatalf("activar: %d %s", r.status, r.crudo)
	}

	// Quitar la asignación exacta.
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/quitar-rol", idLec), map[string]any{"rol": "mesa_ayuda"}).problema(t, 422, "rol_no_asignado")
	r = admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/quitar-rol", idLec), map[string]any{"rol": "mesa_ayuda", "zonaId": zona})
	if r.status != 200 || len(r.cuerpo["roles"].([]any)) != 1 || slices.Contains(permisos(), any("tickets.responder")) {
		t.Fatalf("quitar: %d %s", r.status, r.crudo)
	}

	// Tras confirmar la contraseña, sí da permisos sensibles.
	reautenticar(t, admin)
	if r := admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "jefe_buzones", "nombre": "Jefe de buzones",
		"permisos": []string{"buzones.ver", "buzones.crear"}}); r.status != 201 {
		t.Fatalf("crear con sensibles: %d %s", r.status, r.crudo)
	}

	// Auditoría y eventos por outbox.
	for _, ev := range []string{"rol.creado", "rol.actualizado", "rol.desactivado", "rol.activado"} {
		if eventos(t, ev, "rol_id", idRol) != 1 {
			t.Errorf("falta %s", ev)
		}
	}
	if eventos(t, "usuario.rol_asignado", "usuario_id", idLec) != 1 || eventos(t, "usuario.rol_quitado", "usuario_id", idLec) != 1 {
		t.Error("faltan los eventos de asignación")
	}
	for _, a := range []string{"roles.crear", "roles.editar", "roles.desactivar", "roles.activar"} {
		if auditadas(t, a, fmt.Sprint(idRol)) != 1 {
			t.Errorf("falta la auditoría de %s", a)
		}
	}
	if auditadas(t, "usuarios.asignar_rol", fmt.Sprint(idLec)) != 1 || auditadas(t, "usuarios.quitar_rol", fmt.Sprint(idLec)) != 1 {
		t.Error("falta la auditoría de la asignación")
	}

	// Nadie da más de lo que tiene: un rol con roles.crear y tickets.ver no crea uno con ordenes.ver.
	if r := admin.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "creador", "nombre": "Creador",
		"permisos": []string{"roles.crear", "tickets.ver"}}); r.status != 201 {
		t.Fatalf("creador: %d %s", r.status, r.crudo)
	}
	_, correoCreador := e.usuario("creador")
	creador := e.cliente()
	creador.entrar(correoCreador)
	creador.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "mas", "nombre": "Más", "permisos": []string{"ordenes.ver"}}).problema(t, 403, "prohibido")
	if r := creador.pedir(http.MethodPost, "/api/v1/roles", map[string]any{"codigo": "igual", "nombre": "Igual", "permisos": []string{"tickets.ver"}}); r.status != 201 {
		t.Fatalf("igual: %d %s", r.status, r.crudo)
	}
	if r := admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/roles/%d", idRol), nil); r.status != 200 || r.cuerpo["codigo"] != "mesa_ayuda" {
		t.Fatalf("ver rol: %d %s", r.status, r.crudo)
	}
}

// La retención del historial: se purga lo más viejo que la retención (nunca menos de 7 días).
func TestPurgaDelHistorialDeAccesos(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	marca := "purga-" + pruebas.Unico()
	for _, dias := range []int{1, 8, 200} {
		if _, err := pool.Exec(ctx, `INSERT INTO nucleo.accesos (canal, resultado, request_id, created_at)
			VALUES ('panel', 'contrasena_erronea', $1, now() - make_interval(days => $2))`, marca, dias); err != nil {
			t.Fatal(err)
		}
	}
	quedan := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM nucleo.accesos WHERE request_id = $1`, marca).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := identidad.PurgarAccesos(ctx, pool, 0, 1); err != nil || quedan() != 2 {
		t.Fatalf("180 días: %d %v", quedan(), err)
	}
	// Una retención menor que el mínimo se toma como 7 días.
	if _, err := identidad.PurgarAccesos(ctx, pool, time.Hour, 0); err != nil || quedan() != 1 {
		t.Fatalf("mínimo 7 días: %d %v", quedan(), err)
	}
}
