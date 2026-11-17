package mensajeria_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/clave"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

// Lo que pidió la revisión de seguridad del lado de Iris del servidor de IA.

func problemaDe(err error) string {
	if err == nil {
		return "ok"
	}
	if p, _ := nucleo.AProblema(err); p != nil {
		return p.Codigo()
	}
	return "otro: " + err.Error()
}

// sso es la sesión de autologin del admin (con todo lo necesario para administrar el servidor).
func (x *bancoIA) sso(reautenticada bool) context.Context {
	ctx := pruebas.ContextoUsuarioEnEmpresa(x.bd, x.admin.id, 1, nucleo.PermServidorIAAdministrar, nucleo.PermServidorIAVer, nucleo.PermEmpresasTodas)
	return contexto.ConSesion(ctx, contexto.Sesion{ID: 1, UsuarioID: x.admin.id, Origen: contexto.OrigenSesionSSO, Reautenticada: reautenticada})
}

func (x *bancoIA) columna(col string) []byte {
	x.t.Helper()
	var b []byte
	if err := x.bd.QueryRow(x.ctx, `SELECT `+col+` FROM mensajeria.voz_motores WHERE id = $1`, x.motor).Scan(&b); err != nil {
		x.t.Fatal(err)
	}
	return b
}

// enAuditoria cuenta las filas de auditoría (acciones y cambios) que traen ese texto.
func (x *bancoIA) enAuditoria(texto string) int {
	x.t.Helper()
	return x.contar(`SELECT (SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%'||$1||'%' OR coalesce(error::text, '') LIKE '%'||$1||'%'
			OR resultado LIKE '%'||$1||'%')
		+ (SELECT count(*) FROM auditoria.cambios WHERE coalesce(antes::text, '') LIKE '%'||$1||'%' OR coalesce(despues::text, '') LIKE '%'||$1||'%')`, texto)
}

func (x *bancoIA) generar() error {
	_, _, err := x.proveedor(1).Generar(x.ctx, mensajeria.PedidoIA{Modelo: "modelo-a:latest", Contenido: "hola", MaxTokens: 10})
	return err
}

// M1, M2, B4, B5 y B7: quién puede tocar las credenciales, qué queda en la auditoría y en qué orden
// se habla con el servidor.
func TestServidorIAPermisosYAuditoria(t *testing.T) {
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	k := x.m.ServidorIA

	// B5: un «Conectar» fallido (el código todavía no está escrito en el servidor) deja su fila de
	// auditoría, sin el código.
	r := x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "IA principal", "url": x.banco.URL}), http.StatusCreated)
	codigo := r.cuerpo["codigo"].(string)
	x.motor = int64(r.cuerpo["motor"].(map[string]any)["id"].(float64))
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/conectar"), nil), http.StatusBadGateway, "type", "urn:iris:problema:motor_no_responde")
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'servidor_ia.conectar_fallido' AND entidad = 'voz_motor' AND entidad_id = $1
		AND actor_id = $2`, fmt.Sprint(x.motor), x.admin.id); n != 1 {
		t.Fatalf("auditoría del conectar fallido: %d", n)
	}
	if n := x.enAuditoria(codigo); n != 0 {
		t.Fatalf("el código de enlace quedó en la auditoría (%d)", n)
	}

	// B4: guardar el enlace con una versión vieja (reenlazaron entretanto) no escribe nada; y sin
	// código pendiente tampoco.
	q := mensajeriabd.New(x.bd)
	f, err := q.MotorIAPorID(x.ctx, x.motor)
	if err != nil {
		t.Fatal(err)
	}
	huella := x.banco.Huella
	guardar := func(version int32) error {
		_, err := q.GuardarEnlaceMotorIA(x.ctx, mensajeriabd.GuardarEnlaceMotorIAParams{ID: x.motor, Version: version, Token: []byte("no-debe-guardarse"), HuellaTls: &huella, Compatible: true})
		return err
	}
	if err := guardar(f.Version - 1); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("una versión vieja no debía guardar el enlace: %v", err)
	}
	if err := x.banco.Almacen.AbrirVentana(codigo, x.banco.Ahora()); err != nil {
		t.Fatal(err)
	}
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/conectar"), nil), http.StatusOK)
	f, _ = q.MotorIAPorID(x.ctx, x.motor)
	if err := guardar(f.Version); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("sin código pendiente no debía guardar el enlace: %v", err)
	}

	// M1: sin el permiso, 403 (no 404) en activar, rotar y probar.
	for ruta, cuerpo := range map[string]any{"/activar": map[string]any{"huellaTls": x.banco.Huella}, "/rotar-token": map[string]any{}, "/probar": nil} {
		x.exigir(x.api(x.supervisor, http.MethodPost, x.ruta(ruta), cuerpo), http.StatusForbidden)
	}
	// M1: una sesión de autologin sin re-autenticar no activa, y no sale ni una petición al servidor.
	antes := len(x.banco.Rutas())
	if _, err := k.Activar.Ejecutar(x.sso(false), mensajeria.EntradaActivarMotor{ID: x.motor, HuellaTLS: x.banco.Huella}); problemaDe(err) != "reautenticacion_requerida" {
		t.Fatalf("activar con autologin sin re-autenticar: %s", problemaDe(err))
	}
	if n := len(x.banco.Rutas()); n != antes {
		t.Fatalf("activar sin re-autenticar llamó al servidor: %v", x.banco.Rutas()[antes:])
	}

	// B7: al activar, lo primero que recibe el servidor es la rotación del token derivado.
	x.activar()
	if rutas := x.banco.Rutas()[antes:]; len(rutas) < 2 || rutas[0] != "POST "+voz.RutaCredencial {
		t.Fatalf("al activar lo primero debía ser rotar: %v", rutas)
	}

	// M1: rotar, «Revocar ahora» y probar tampoco pasan con autologin sin re-autenticar; probar no
	// llega a hacer ninguna llamada de red.
	antes = len(x.banco.Rutas())
	token := x.columna("token")
	for nombre, f := range map[string]func(context.Context) error{
		"rotar": func(ctx context.Context) error {
			_, err := k.Rotar.Ejecutar(ctx, mensajeria.EntradaRotarMotor{ID: x.motor})
			return err
		},
		"revocar": func(ctx context.Context) error {
			_, err := k.Rotar.Ejecutar(ctx, mensajeria.EntradaRotarMotor{ID: x.motor, Revocar: true})
			return err
		},
		"probar": func(ctx context.Context) error { _, err := k.Probar.Ejecutar(ctx, x.motor); return err },
	} {
		if err := f(x.sso(false)); problemaDe(err) != "reautenticacion_requerida" {
			t.Fatalf("%s con autologin sin re-autenticar: %s", nombre, problemaDe(err))
		}
	}
	if n := len(x.banco.Rutas()); n != antes || string(x.columna("token")) != string(token) {
		t.Fatalf("sin re-autenticar no debía tocarse nada: %v", x.banco.Rutas()[antes:])
	}
	// Re-autenticada, sí.
	if _, err := k.Probar.Ejecutar(x.sso(true), x.motor); err != nil {
		t.Fatalf("probar con la sesión re-autenticada: %v", err)
	}

	// M2: tras rotar (con certificado de cliente nuevo), ni la clave privada cifrada ni el token
	// cifrado están en la auditoría, en ninguna de sus formas.
	if _, err := k.Rotar.Ejecutar(x.sso(true), mensajeria.EntradaRotarMotor{ID: x.motor, Revocar: true}); err != nil {
		t.Fatalf("revocar con la sesión re-autenticada: %v", err)
	}
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{}), http.StatusOK)
	for _, col := range []string{"cert_cliente_clave", "token"} {
		valor := x.columna(col)
		if len(valor) < 32 {
			t.Fatalf("%s sin valor", col)
		}
		for forma, texto := range map[string]string{"base64": base64.StdEncoding.EncodeToString(valor)[:40], "base64url": base64.RawURLEncoding.EncodeToString(valor)[:40],
			"hex": hex.EncodeToString(valor)[:40]} {
			if n := x.enAuditoria(texto); n != 0 {
				t.Fatalf("%s (%s) quedó en la auditoría: %d filas", col, forma, n)
			}
		}
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion LIKE 'servidor_ia.%' AND entidad_id = $1
		AND (entrada::text ~* 'PRIVATE KEY|certclave|cert_clave' OR entrada::text ~* '"token"')`, fmt.Sprint(x.motor)); n != 0 {
		t.Fatalf("la auditoría del servidor de IA trae campos de credenciales: %d filas", n)
	}
	// La rotación sí queda auditada (sin secretos).
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'servidor_ia.rotar' AND resultado = 'ok' AND entidad_id = $1`, fmt.Sprint(x.motor)); n != 2 {
		t.Fatalf("rotaciones auditadas: %d", n)
	}
	if err := x.generar(); err != nil {
		t.Fatalf("tras rotar el canal sigue vivo: %v", err)
	}
}

// B1 y B2: una rotación cuya respuesta se pierde no deja a Iris sin canal, y dos rotaciones a la vez
// no se pisan.
func TestServidorIARotacionAMediasYConcurrente(t *testing.T) {
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	x.enlazar()
	x.activar()
	pasar := func() {
		*x.reloj = x.reloj.Add(mensajeria.PlazoRotacionIA + time.Second)
	}
	siguiente := func() bool { return x.columna("token_siguiente") != nil }

	// «Revocar ahora» que llega al servidor y cuya respuesta se pierde: el servidor ya solo acepta
	// lo nuevo (token y certificado), que Iris guardó ANTES de entregarlo.
	if err := x.m.RotacionAMedias(x.ctx, x.bd, x.motor, true); err != nil {
		t.Fatal(err)
	}
	if !siguiente() || x.columna("cert_cliente_clave_siguiente") == nil || x.generar() == nil {
		t.Fatal("la rotación debía quedar a medias: lo vigente en Iris ya no vale en el servidor")
	}
	// B2: mientras esa rotación está en vuelo, otra no entra (ni pisa lo guardado).
	guardado := string(x.columna("token_siguiente"))
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{}), http.StatusConflict, "type", "urn:iris:problema:rotacion_en_curso")
	if err := x.m.RotacionAMedias(x.ctx, x.bd, x.motor, false); problemaDe(err) != "rotacion_en_curso" {
		t.Fatalf("una segunda rotación a la vez: %s", problemaDe(err))
	}
	x.latir() // el latido tampoco la termina mientras está en vuelo
	if string(x.columna("token_siguiente")) != guardado {
		t.Fatal("la rotación en vuelo no debía tocarse")
	}
	// Pasado el plazo, el latido la termina solo con lo guardado: el canal vuelve sin tocar nada.
	pasar()
	x.latir()
	if siguiente() || x.columna("cert_cliente_clave_siguiente") != nil || x.columna("rotacion_desde") != nil {
		t.Fatal("el latido debía terminar la rotación a medias")
	}
	if err := x.generar(); err != nil {
		t.Fatalf("tras terminar la rotación el canal debía vivir: %v", err)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.motor_token_rotado' AND (payload->>'motor_id')::bigint = $1
		AND payload->>'origen' = 'automatica'`, x.motor); n != 1 {
		t.Fatalf("voz.motor_token_rotado de la rotación terminada sola: %d", n)
	}

	// Lo mismo desde el panel: «Rotar» y «Revocar ahora» terminan primero la que quedó a medias.
	for _, revocar := range []bool{false, true} {
		if err := x.m.RotacionAMedias(x.ctx, x.bd, x.motor, revocar); err != nil {
			t.Fatal(err)
		}
		// Lo que garantiza el servidor: aplica la credencial entera o no la aplica. Con convivencia la
		// llave anterior sigue valiendo (24 h) aunque ya esté aplicada la nueva; con «Revocar ahora»
		// deja de valer en el acto y lo que vale es lo que Iris guardó antes de entregar.
		if err := x.generar(); (err == nil) == revocar {
			t.Fatalf("revocar=%v: con la rotación aplicada en el servidor, la llave vigente de Iris: %v", revocar, err)
		}
		if !revocar {
			// Durante la convivencia la puerta acepta también /credencial firmada con el token
			// anterior (el que Iris sigue teniendo como vigente): por eso la recuperación es automática.
			antes := len(x.banco.Rutas())
			r := x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{})
			x.exigir(r, http.StatusConflict, "type", "urn:iris:problema:rotacion_en_curso") // en vuelo: todavía no
			if len(x.banco.Rutas()) != antes {
				t.Fatal("una rotación en vuelo no debía llamar al servidor")
			}
			if err := x.m.CredencialConLoVigente(x.ctx, x.bd, x.motor); err != nil {
				t.Fatalf("la puerta debía aceptar /credencial firmada con el token anterior durante la convivencia: %v", err)
			}
		}
		pasar()
		x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{"revocar": revocar}), http.StatusOK)
		if siguiente() {
			t.Fatalf("revocar=%v: quedó algo a medias", revocar)
		}
		if err := x.generar(); err != nil {
			t.Fatalf("revocar=%v: el canal debía vivir: %v", revocar, err)
		}
	}
	// Una entrega que nunca llegó (el servidor no la aplicó): lo guardado no sirve y la siguiente
	// rotación lo reemplaza.
	x.sql(`UPDATE mensajeria.voz_motores SET token_siguiente = token, rotacion_desde = $2 WHERE id = $1`, x.motor, x.reloj.Add(-time.Hour))
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{}), http.StatusOK)
	if err := x.generar(); err != nil {
		t.Fatalf("tras una entrega que no llegó: %v", err)
	}

	// N3 y N5: lo «siguiente» de una entrega que nunca llegó no se queda guardado, pero solo se borra
	// cuando el servidor lo rechaza de forma explícita (401) en tres latidos seguidos, con lo vigente
	// contestando y el apartado vencido. Mientras está en vuelo no se toca.
	if err := x.m.SiguienteSinEntregar(x.ctx, x.bd, x.motor, false); err != nil {
		t.Fatal(err)
	}
	vigente := string(x.columna("token"))
	x.latir()
	if !siguiente() {
		t.Fatal("en vuelo no debía borrarse")
	}
	pasar()
	x.latir()
	x.latir()
	if !siguiente() {
		t.Fatal("dos rechazos no bastan para descartar")
	}
	// Un sondeo que falla por red (no es un rechazo) vuelve la cuenta a cero.
	x.m.FijarSondeoSiguienteIA(nil)
	x.sql(`UPDATE mensajeria.voz_motores SET siguiente_rechazos = 0 WHERE id = $1`, x.motor)
	x.latir()
	x.latir()
	if !siguiente() {
		t.Fatal("tras volver la cuenta a cero hacen falta otros tres rechazos")
	}
	x.latir()
	if siguiente() || x.columna("rotacion_desde") != nil || string(x.columna("token")) != vigente {
		t.Fatal("al tercer rechazo explícito seguido el latido debía borrar lo siguiente, sin tocar lo vigente")
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'servidor_ia.rotacion_descartada' AND entidad_id = $1`, fmt.Sprint(x.motor)); n != 1 {
		t.Fatalf("auditoría del descarte: %d", n)
	}
	if err := x.generar(); err != nil {
		t.Fatalf("tras descartar, el canal sigue con lo vigente: %v", err)
	}
	// Lo mismo cuando lo que no llegó traía un certificado de cliente nuevo: el servidor lo rechaza
	// en el saludo TLS, que también es un rechazo explícito.
	if err := x.m.SiguienteSinEntregar(x.ctx, x.bd, x.motor, true); err != nil {
		t.Fatal(err)
	}
	pasar()
	x.latir()
	x.latir()
	if !siguiente() {
		t.Fatal("dos rechazos del certificado no bastan para descartar")
	}
	x.latir()
	if siguiente() || x.columna("cert_cliente_clave_siguiente") != nil || x.estado() != "listo" {
		t.Fatal("al tercer rechazo del certificado nuevo debía descartarse lo siguiente")
	}
	// Y al revés: una rotación con convivencia que sí llegó (lo vigente todavía contesta) no se
	// descarta: el latido la termina antes de que lo anterior deje de valer.
	if err := x.m.RotacionAMedias(x.ctx, x.bd, x.motor, false); err != nil {
		t.Fatal(err)
	}
	enServidor, _ := x.banco.Almacen.Leer()
	pasar()
	// N5: el sondeo de lo siguiente falla por red (tiempo agotado), una y varias veces. No es un
	// rechazo: no se descarta, por muchos latidos que pasen.
	x.m.FijarSondeoSiguienteIA(func() error { return context.DeadlineExceeded })
	for range 5 {
		x.latir()
	}
	if !siguiente() || string(x.columna("token")) != vigente {
		t.Fatal("un fallo de red al sondear lo siguiente no debía descartarlo")
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'servidor_ia.rotacion_descartada' AND entidad_id = $1`, fmt.Sprint(x.motor)); n != 2 {
		t.Fatalf("no debía haber otro descarte: %d", n)
	}
	// En cuanto el sondeo vuelve a pasar, el latido siguiente la promueve.
	x.m.FijarSondeoSiguienteIA(nil)
	x.latir()
	if siguiente() || string(x.columna("token")) == vigente {
		t.Fatal("el latido debía promover la rotación que sí llegó al servidor")
	}
	if c, err := x.m.TokenDeMotor(x.ctx, x.bd, x.motor); err != nil || string(c) != string(enServidor.Token) {
		t.Fatalf("Iris debía quedar con el token que tiene el servidor: %v", err)
	}

	// B2: varias a la vez. Ninguna pisa a otra: las que no ganan reciben 409 y el canal sigue vivo.
	pasar()
	var wg sync.WaitGroup
	estados := make([]int, 4)
	for i := range estados {
		wg.Add(1)
		go func() {
			defer wg.Done()
			estados[i] = x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{"revocar": i%2 == 0}).status
		}()
	}
	wg.Wait()
	slices.Sort(estados)
	if estados[0] != http.StatusOK {
		t.Fatalf("rotaciones a la vez: ninguna entró: %v", estados)
	}
	for _, e := range estados {
		if e != http.StatusOK && e != http.StatusConflict {
			t.Fatalf("rotaciones a la vez: %v", estados)
		}
	}
	t.Logf("rotaciones a la vez: %v", estados)
	pasar()
	x.latir()
	if siguiente() || x.estado() != "listo" {
		t.Fatalf("tras las rotaciones a la vez: siguiente=%v estado=%s", siguiente(), x.estado())
	}
	if err := x.generar(); err != nil {
		t.Fatalf("tras las rotaciones a la vez el canal debía vivir: %v", err)
	}
}

// B3: lo que cuenta Iris para el tope no depende de lo que declare el servidor.
func TestServidorIAUsoEstimado(t *testing.T) {
	base := mensajeria.PedidoIA{Instrucciones: strings.Repeat("a", 300), Contenido: strings.Repeat("b", 300), MaxTokens: 100}
	solo := mensajeria.EstimarEntradaIA(base)
	conPasos := base
	conPasos.Pasos = []mensajeria.PasoIA{{Respuesta: mensajeria.RespuestaIA{Texto: strings.Repeat("c", 300),
		Llamadas: []mensajeria.LlamadaIA{{Nombre: "crear_ticket", Argumentos: []byte(`"` + strings.Repeat("d", 288) + `"`)}}},
		Resultados: []mensajeria.ResultadoIA{{Contenido: strings.Repeat("e", 600)}}}}
	conHerramientas := base
	conHerramientas.Herramientas = []mensajeria.HerramientaIA{{Nombre: "crear_ticket", Descripcion: strings.Repeat("f", 288),
		Parametros: map[string]any{"asunto": map[string]any{"type": "string"}}}}
	if solo != 201 || mensajeria.EstimarEntradaIA(conPasos) < solo+400 || mensajeria.EstimarEntradaIA(conHerramientas) < solo+100 {
		t.Fatalf("la estimación debía contar pasos y herramientas: %d %d %d", solo, mensajeria.EstimarEntradaIA(conPasos), mensajeria.EstimarEntradaIA(conHerramientas))
	}
	salida := strings.Repeat("g", 300)
	// El servidor declara 0: cuenta la estimación. Declara un disparate: hay techo.
	if u := mensajeria.UsoContado(base, mensajeria.UsoIA{}, salida); u.Entrada != solo || u.Salida != 101 {
		t.Fatalf("declarando 0: %+v", u)
	}
	if u := mensajeria.UsoContado(base, mensajeria.UsoIA{Entrada: 1 << 40, Salida: 1 << 40}, salida); u.Entrada != solo*4+4096 || u.Salida != 2*4096 {
		t.Fatalf("declarando un disparate: %+v", u)
	}
	if u := mensajeria.UsoContado(base, mensajeria.UsoIA{Entrada: 500, Salida: 150}, salida); u.Entrada != 500 || u.Salida != 150 {
		t.Fatalf("un uso declarado razonable se respeta: %+v", u)
	}

	// Un error después de enviar (el servidor ya gastó GPU) anota la estimación de la entrada.
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	x.enlazar()
	x.activar()
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) {
		o.Estado = http.StatusInternalServerError
		o.Respuesta = []byte(`{"error":"falla"}`)
	})
	pedido := mensajeria.PedidoIA{Modelo: "modelo-a:latest", Contenido: strings.Repeat("palabra ", 300), MaxTokens: 50}
	if _, _, err := x.proveedor(1).Generar(x.ctx, pedido); err == nil {
		t.Fatal("debía fallar")
	}
	var peticiones, errores, entrada int64
	if err := x.bd.QueryRow(x.ctx, `SELECT coalesce(sum(peticiones), 0), coalesce(sum((SELECT sum(v::bigint) FROM jsonb_each_text(errores) AS e (k, v))), 0)::bigint, coalesce(sum(tokens_entrada), 0) FROM mensajeria.servidor_ia_uso
		WHERE motor_id = $1 AND empresa_id = 1`, x.motor).Scan(&peticiones, &errores, &entrada); err != nil {
		t.Fatal(err)
	}
	if errores != 1 || entrada != mensajeria.EstimarEntradaIA(pedido) {
		t.Fatalf("uso tras un error después de enviar: peticiones %d, errores %d, tokens de entrada %d", peticiones, errores, entrada)
	}
}

// Rotación de IRIS_CLAVE_CIFRADO con filas reales del servidor de IA: el código pendiente, la clave
// del certificado de cliente, el token y lo «siguiente» de una rotación a medias. Tras rotar, una
// Iris que solo tiene la clave nueva sigue conectando.
func TestServidorIARotarClaveDeCifrado(t *testing.T) {
	crudo := make([]byte, 32)
	if _, err := rand.Read(crudo); err != nil {
		t.Fatal(err)
	}
	claveVieja := base64.StdEncoding.EncodeToString(crudo)
	vieja, err := nucleo.NuevoCifrador(claveVieja)
	if err != nil {
		t.Fatal(err)
	}
	// La Iris «de antes» cifra con una clave propia de esta prueba y lee también la de siempre.
	transicionVieja, err := nucleo.NuevoCifrador(claveVieja, claveCifrado)
	if err != nil {
		t.Fatal(err)
	}
	x := bancoIASobre(t, nuevaBandeja(t, func(c *mensajeria.Config) { c.PermitirLoopbackIA = true; c.Cifrador = transicionVieja }), vozprueba.Opciones{})
	// La Iris «de después»: solo la clave de siempre (la nueva, para esta prueba).
	yb := nuevaBandeja(t, func(c *mensajeria.Config) { c.PermitirLoopbackIA = true })
	*yb.reloj = *x.reloj
	y := &bancoIA{bandeja: yb, banco: x.banco}
	y.admin = y.persona([]int64{1}, rolBase("admin"))
	y.admin.empresa = 1
	nueva, err := nucleo.NuevoCifrador(claveCifrado, claveVieja)
	if err != nil {
		t.Fatal(err)
	}
	rotar := func(columnas ...string) {
		t.Helper()
		// Las filas de otras pruebas cifradas con otras claves no son de esta: se dejan como están.
		r, err := clave.Nuevo(nueva).Rotar.Ejecutar(pruebas.ContextoSistema(x.bd), clave.EntradaRotar{IgnorarIlegibles: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, col := range columnas {
			rotadas := 0
			for _, c := range r.Columnas {
				if c.Columna == col {
					rotadas = c.Rotadas
				}
			}
			if rotadas != 1 {
				t.Fatalf("%s: %d filas rotadas (%+v)", col, rotadas, r.Columnas)
			}
		}
	}

	// 1. Código pendiente y clave del certificado de cliente, cifrados con la clave vieja.
	r := x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "IA principal", "url": x.banco.URL}), http.StatusCreated)
	x.motor = int64(r.cuerpo["motor"].(map[string]any)["id"].(float64))
	y.motor = x.motor
	if err := x.banco.Almacen.AbrirVentana(r.cuerpo["codigo"].(string), x.banco.Ahora()); err != nil {
		t.Fatal(err)
	}
	// Antes de rotar, la Iris de la clave nueva no puede leerlos.
	if r := y.api(y.admin, http.MethodPost, y.ruta("/conectar"), nil); r.status < 400 {
		t.Fatalf("antes de rotar no debía poder leerse con la clave nueva: %d", r.status)
	}
	rotar(clave.ColumnaMotorCodigoEnlace, clave.ColumnaMotorClaveCert)
	y.exigir(y.api(y.admin, http.MethodPost, y.ruta("/conectar"), nil), http.StatusOK, "enlazado", "true")

	// 2. Token, y lo «siguiente» de una rotación a medias (token y clave del certificado nuevo).
	x.activar()
	if err := x.m.RotacionAMedias(x.ctx, x.bd, x.motor, true); err != nil {
		t.Fatal(err)
	}
	rotar(clave.ColumnaMotorToken, clave.ColumnaMotorTokenSiguiente, clave.ColumnaMotorClaveCertSig)
	*x.reloj = x.reloj.Add(mensajeria.PlazoRotacionIA + time.Second)
	*y.reloj = *x.reloj
	y.latir()
	if y.estado() != "listo" || y.columna("token_siguiente") != nil {
		t.Fatalf("tras rotar la clave de cifrado Iris debía seguir conectando: %s", y.estado())
	}
	if err := y.generar(); err != nil {
		t.Fatalf("tras rotar la clave de cifrado: %v", err)
	}
	y.exigir(y.api(y.admin, http.MethodPost, y.ruta("/rotar-token"), map[string]any{"revocar": true}), http.StatusOK)
	// Y nada quedó legible solo con la clave vieja.
	for _, col := range []string{"token", "cert_cliente_clave"} {
		if _, err := vieja.Descifrar(y.columna(col), mensajeria.DatosAsociadosMotor(y.motor, col)); err == nil {
			t.Fatalf("%s seguía cifrado con la clave vieja", col)
		}
	}
}

// «Activar» liga lo que el administrador comparó: si entre «Conectar» y «Activar» se enlazó otro
// servidor, la huella ya no es la que vio y no se activa.
func TestServidorIAActivarLigaLaHuella(t *testing.T) {
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	x.enlazar() // el administrador ve y compara la huella del servidor A
	a := x.banco
	// Otra persona enlaza otro servidor (B) mientras tanto.
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{Ahora: func() time.Time { return *x.reloj }})
	r := x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "Otro", "url": b.URL}), http.StatusCreated)
	if id := int64(r.cuerpo["motor"].(map[string]any)["id"].(float64)); id != x.motor {
		t.Fatalf("el servidor es uno por instalación: %d", id)
	}
	if err := b.Almacen.AbrirVentana(r.cuerpo["codigo"].(string), b.Ahora()); err != nil {
		t.Fatal(err)
	}
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/conectar"), nil), http.StatusOK, "huellaTls", b.Huella)
	if a.Huella == b.Huella {
		t.Fatal("los dos servidores debían tener huellas distintas")
	}
	// El primero activa con lo que comparó (A): 409, sin tocar ningún servidor.
	enA, enB := len(a.Rutas()), len(b.Rutas())
	activar := func(huella any) respuesta {
		return x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"huellaTls": huella})
	}
	x.exigir(activar(a.Huella), http.StatusConflict, "type", "urn:iris:problema:huella_cambiada")
	if len(a.Rutas()) != enA || len(b.Rutas()) != enB || x.columna("activado_en") != nil {
		t.Fatal("con la huella cambiada no debía activarse ni llamarse a ningún servidor")
	}
	// Sin huella no se activa a ciegas.
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{}), http.StatusUnprocessableEntity)
	x.exigir(activar(strings.Repeat("0", 64)), http.StatusConflict, "type", "urn:iris:problema:huella_cambiada")
	// Con la huella que muestra ahora la tarjeta (B), sí.
	x.exigir(activar(b.Huella), http.StatusOK, "activado", "true", "huellaTls", b.Huella)
	if len(b.Rutas()) == enB || len(a.Rutas()) != enA {
		t.Fatal("la activación debía ir al servidor B y solo a él")
	}
}

// Dos «Activar» a la vez: el servidor se activa una sola vez y el canal queda vivo.
func TestServidorIAActivarALaVez(t *testing.T) {
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	x.enlazar()
	var wg sync.WaitGroup
	estados := make([]int, 4)
	for i := range estados {
		wg.Add(1)
		go func() {
			defer wg.Done()
			estados[i] = x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"huellaTls": x.banco.Huella}).status
		}()
	}
	wg.Wait()
	slices.Sort(estados)
	t.Logf("activar a la vez: %v", estados)
	if estados[0] != http.StatusOK {
		t.Fatalf("ninguna activó: %v", estados)
	}
	for _, e := range estados {
		// 200: la que activó, o una que llegó cuando ya estaba activo. 409: otra rotación en curso.
		if e != http.StatusOK && e != http.StatusConflict {
			t.Fatalf("activar a la vez: %v", estados)
		}
	}
	if x.eventosIA("voz.motor_activado") != 1 || x.eventosIA("voz.motor_token_rotado") != 1 {
		t.Fatalf("debía activarse una sola vez: %d activaciones, %d rotaciones", x.eventosIA("voz.motor_activado"), x.eventosIA("voz.motor_token_rotado"))
	}
	*x.reloj = x.reloj.Add(mensajeria.PlazoRotacionIA + time.Second)
	x.latir()
	if x.estado() != "listo" || x.columna("token_siguiente") != nil {
		t.Fatalf("tras activar a la vez: %s", x.estado())
	}
	if err := x.generar(); err != nil {
		t.Fatalf("tras activar a la vez el canal debía vivir: %v", err)
	}
	en, _ := x.banco.Almacen.Leer()
	if c, err := x.m.TokenDeMotor(x.ctx, x.bd, x.motor); err != nil || string(c) != string(en.Token) || en.TokenAnterior != nil {
		t.Fatalf("Iris y el servidor debían quedar con el mismo token, sin convivencia: %v", err)
	}
}

// N6: la rotación automática contra un servidor que rechaza /credencial no entra en bucle: tras un
// fallo, un intento por hora como mucho.
func TestServidorIARotacionAutomaticaConEspera(t *testing.T) {
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	x.enlazar()
	x.activar()
	x.latir()
	intentos := func() int {
		n := 0
		for _, r := range x.banco.Rutas() {
			if r == "POST "+voz.RutaCredencial {
				n++
			}
		}
		return n
	}
	base := intentos()
	// El token tiene más de 30 días y el servidor falla al recibir la credencial nueva.
	x.sql(`UPDATE mensajeria.voz_motores SET token_rotado_en = $2 WHERE id = $1`, x.motor, x.reloj.Add(-31*24*time.Hour))
	x.banco.Fallar(voz.RutaCredencial)
	x.latir()
	if intentos() != base+1 || x.columna("rotacion_fallida_en::text::bytea") == nil {
		t.Fatalf("debía intentarse la rotación automática una vez: %d", intentos()-base)
	}
	// Media hora de latidos (uno cada 40 s): ni un intento más, y como mucho un descarte auditado.
	for range 45 {
		*x.reloj = x.reloj.Add(40 * time.Second)
		x.latir()
	}
	if intentos() != base+1 {
		t.Fatalf("la rotación automática fallida no debía repetirse en cada latido: %d intentos", intentos()-base)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'servidor_ia.rotacion_descartada' AND entidad_id = $1`, fmt.Sprint(x.motor)); n != 1 {
		t.Fatalf("descartes auditados en media hora: %d", n)
	}
	if x.columna("token_siguiente") != nil || x.estado() != "listo" {
		t.Fatalf("lo que no llegó debía descartarse y el servidor seguir listo: %s", x.estado())
	}
	if err := x.generar(); err != nil {
		t.Fatalf("mientras tanto el canal sigue con lo vigente: %v", err)
	}
	// Pasada la hora se intenta otra vez (una); y cuando el servidor vuelve a aceptar, rota.
	*x.reloj = x.reloj.Add(31 * time.Minute)
	x.latir()
	if intentos() != base+2 {
		t.Fatalf("a la hora debía intentarse otra vez: %d intentos", intentos()-base)
	}
	x.banco.Fallar("")
	*x.reloj = x.reloj.Add(61 * time.Minute)
	x.latir() // descarta lo que quedó del segundo intento, o rota si ya no queda nada
	for range 4 {
		*x.reloj = x.reloj.Add(40 * time.Second)
		x.latir()
	}
	var rotado time.Time
	if err := x.bd.QueryRow(x.ctx, `SELECT token_rotado_en FROM mensajeria.voz_motores WHERE id = $1`, x.motor).Scan(&rotado); err != nil || x.reloj.Sub(rotado) > time.Hour ||
		x.columna("rotacion_fallida_en::text::bytea") != nil || x.columna("token_siguiente") != nil {
		t.Fatalf("cuando el servidor vuelve a aceptar, la rotación automática entra: rotado %s (%v)", rotado, err)
	}
	if err := x.generar(); err != nil {
		t.Fatalf("tras la rotación automática: %v", err)
	}
}
