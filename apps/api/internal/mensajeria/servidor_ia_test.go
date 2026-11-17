package mensajeria_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

// El servidor de IA de punta a punta: Iris contra la puerta real (TLS de verdad) y un Ollama falso.

type bancoIA struct {
	*bandeja
	banco *vozprueba.Banco
	admin persona
	motor int64
}

func nuevoBancoIA(t *testing.T, o vozprueba.Opciones) *bancoIA {
	t.Helper()
	return bancoIASobre(t, nuevaBandeja(t, func(c *mensajeria.Config) { c.PermitirLoopbackIA = true }), o)
}

// bancoIASobre monta el servidor de IA de prueba sobre una bandeja ya armada.
func bancoIASobre(t *testing.T, b *bandeja, o vozprueba.Opciones) *bancoIA {
	t.Helper()
	x := &bancoIA{bandeja: b}
	// La puerta usa el mismo reloj (fijo) que Iris: las peticiones van firmadas con la hora.
	o.Ahora = func() time.Time { return *x.reloj }
	x.banco = vozprueba.NuevoBanco(t, o)
	x.admin = x.persona([]int64{1}, rolBase("admin"))
	x.admin.empresa = 1
	// Un servidor por instalación: cada prueba empieza sin ninguno.
	x.sql(`DELETE FROM mensajeria.voz_motores`)
	x.sql(`UPDATE mensajeria.asistente_ajustes SET proveedor = 'ollama', activo = false WHERE proveedor = 'servidor_ia'`)
	return x
}

func (x *bancoIA) sql(sql string, args ...any) {
	x.t.Helper()
	if _, err := x.bd.Exec(x.ctx, sql, args...); err != nil {
		x.t.Fatal(err)
	}
}

func (x *bancoIA) exigir(r respuesta, status int, campos ...string) respuesta {
	x.t.Helper()
	x.bandeja.exigir(r, status, campos...)
	return r
}

func (x *bancoIA) ruta(resto string) string { return fmt.Sprintf("/voz/motores/%d%s", x.motor, resto) }

// enlazar hace el camino completo del panel: pedir el código, escribirlo en el servidor y conectar.
func (x *bancoIA) enlazar() respuesta {
	x.t.Helper()
	r := x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "IA principal", "url": x.banco.URL}), http.StatusCreated)
	codigo := r.cuerpo["codigo"].(string)
	x.motor = int64(r.cuerpo["motor"].(map[string]any)["id"].(float64))
	if !voz.CodigoValido(codigo) {
		x.t.Fatalf("código: %q", codigo)
	}
	// En el servidor: `iris-voz pair`.
	if err := x.banco.Almacen.AbrirVentana(codigo, x.banco.Ahora()); err != nil {
		x.t.Fatal(err)
	}
	// El código queda guardado cifrado (nunca en claro) y no aparece en la auditoría ni en eventos.
	var guardado []byte
	if err := x.bd.QueryRow(x.ctx, `SELECT enlace_codigo FROM mensajeria.voz_motores WHERE id = $1`, x.motor).Scan(&guardado); err != nil || guardado == nil ||
		bytes.Contains(guardado, []byte(codigo)) {
		x.t.Fatalf("el código debía quedar cifrado: %v", err)
	}
	var fugas int
	if err := x.bd.QueryRow(x.ctx, `SELECT (SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%'||$1||'%' OR resultado::text LIKE '%'||$1||'%')
		+ (SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%'||$1||'%')`, codigo).Scan(&fugas); err != nil || fugas != 0 {
		x.t.Fatalf("el código de enlace quedó en la auditoría o en un evento (%d): %v", fugas, err)
	}
	return x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/conectar"), nil), http.StatusOK)
}

func (x *bancoIA) activar() respuesta {
	x.t.Helper()
	// La huella que el administrador comparó con la del instalador.
	return x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"huellaTls": x.banco.Huella}), http.StatusOK)
}

func (x *bancoIA) latir() {
	x.t.Helper()
	if err := x.m.MantenerServidorIA(x.ctx, x.bd); err != nil {
		x.t.Fatal(err)
	}
}

func (x *bancoIA) estado() string {
	x.t.Helper()
	var s string
	if err := x.bd.QueryRow(x.ctx, `SELECT estado FROM mensajeria.voz_motores WHERE id = $1`, x.motor).Scan(&s); err != nil {
		x.t.Fatal(err)
	}
	return s
}

func (x *bancoIA) elegir(p persona, modelo string, extra map[string]any) respuesta {
	x.t.Helper()
	r := x.exigir(x.api(p, http.MethodGet, "/asistente/ajustes", nil), http.StatusOK)
	cuerpo := map[string]any{"version": r.cuerpo["version"], "activo": true, "proveedor": "servidor_ia", "url": "", "modelo": modelo, "modo": "boton",
		"fuentes": map[string]any{"manuales": true, "argos": true, "horus": true, "historial": true}, "topeTokensMes": 2000000,
		"precioEntradaMillon": nil, "precioSalidaMillon": nil}
	for k, v := range extra {
		cuerpo[k] = v
	}
	return x.api(p, http.MethodPost, "/asistente/ajustes/actualizar", cuerpo)
}

func (x *bancoIA) proveedor(empresa int64) mensajeria.Proveedor {
	x.t.Helper()
	p, err := x.m.ProveedorPara(mensajeria.ConfigProveedor{Proveedor: mensajeria.ProveedorServidorIA, Modelo: "modelo-a:latest", EmpresaID: empresa}, 10*time.Second)
	if err != nil {
		x.t.Fatal(err)
	}
	return p
}

func codigoIA(err error) string {
	var e *mensajeria.ErrorIA
	if errors.As(err, &e) {
		return e.Codigo
	}
	if err == nil {
		return "ok"
	}
	return "otro: " + err.Error()
}

func TestServidorIAEnlazarActivarYUsar(t *testing.T) {
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	ctx := x.ctx

	// Solo quien tiene el permiso; el servidor es de la instalación.
	x.exigir(x.api(x.supervisor, http.MethodGet, "/voz/motores", nil), http.StatusForbidden)
	x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "x", "url": "http://10.0.0.1:8443"}), http.StatusUnprocessableEntity, "type", "urn:iris:problema:url_no_permitida")
	x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "x", "url": "https://169.254.169.254:8443"}), http.StatusUnprocessableEntity)

	r := x.enlazar()
	if r.cuerpo["huellaTls"] != x.banco.Huella || r.cuerpo["enlazado"] != true || r.cuerpo["activado"] != false || r.cuerpo["enlaceVenceEn"] == nil {
		t.Fatalf("tras conectar: %v", r.cuerpo)
	}
	// El código se borra al enlazar; el token (derivado) queda cifrado.
	var codigo, token []byte
	if err := x.bd.QueryRow(ctx, `SELECT enlace_codigo, token FROM mensajeria.voz_motores WHERE id = $1`, x.motor).Scan(&codigo, &token); err != nil || codigo != nil || token == nil {
		t.Fatalf("tras conectar el código debía borrarse: %v", err)
	}
	derivado, _ := x.banco.Almacen.Leer()
	if bytes.Equal(token, derivado.Token) {
		t.Fatal("el token quedó en claro en la base")
	}
	// Conectar otra vez: ya no hay código.
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/conectar"), nil), http.StatusConflict, "type", "urn:iris:problema:enlace_vencido")
	// Hasta activar, ninguna empresa puede elegirlo.
	x.exigir(x.elegir(x.admin, "modelo-a:latest", nil), http.StatusUnprocessableEntity, "type", "urn:iris:problema:servidor_ia_no_disponible")
	if x.eventosIA("voz.motor_enlazado") != 1 {
		t.Fatal("falta voz.motor_enlazado")
	}

	// Activar: lo primero es cambiar el token derivado por uno aleatorio, sin convivencia.
	r = x.activar()
	if r.cuerpo["estado"] != "listo" || r.cuerpo["activado"] != true || r.cuerpo["enlaceVenceEn"] != nil {
		t.Fatalf("tras activar: %v", r.cuerpo)
	}
	despues, _ := x.banco.Almacen.Leer()
	if bytes.Equal(despues.Token, derivado.Token) || despues.TokenAnterior != nil {
		t.Fatal("al activar debía rotarse el token derivado, sin convivencia")
	}
	if x.eventosIA("voz.motor_activado") != 1 || x.eventosIA("voz.motor_token_rotado") != 1 {
		t.Fatal("faltan los eventos de activar")
	}
	cat := r.cuerpo["catalogo"].(map[string]any)["llm"].([]any)
	if len(cat) != 1 || r.cuerpo["capacidad"].(map[string]any)["maxSimultaneas"] != float64(4) || r.cuerpo["paquete"].(map[string]any)["compatible"] != true {
		t.Fatalf("tarjeta: %v", r.cuerpo)
	}
	// Con un servidor activo no se enlaza otro encima.
	x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "x", "url": x.banco.URL}), http.StatusConflict, "type", "urn:iris:problema:motor_ya_enlazado")

	// La empresa lo elige: sin URL ni clave, con un modelo de los que deja usar.
	aj := x.exigir(x.api(x.admin, http.MethodGet, "/asistente/ajustes", nil), http.StatusOK)
	s := aj.cuerpo["servidorIa"].(map[string]any)
	if s["disponible"] != true || fmt.Sprint(s["modelos"]) != "[modelo-a:latest]" {
		t.Fatalf("servidorIa en los ajustes: %v", s)
	}
	x.exigir(x.elegir(x.admin, "modelo-a:latest", map[string]any{"url": "https://otro"}), http.StatusUnprocessableEntity)
	x.exigir(x.elegir(x.admin, "modelo-a:latest", map[string]any{"clave": "sk-secreta"}), http.StatusUnprocessableEntity)
	x.exigir(x.elegir(x.admin, "modelo-b:latest", nil), http.StatusUnprocessableEntity, "type", "urn:iris:problema:servidor_ia_no_disponible")
	x.exigir(x.elegir(x.admin, "modelo-a:latest", nil), http.StatusOK, "proveedor", "servidor_ia")

	// La petición real del adaptador, con herramientas, pasa por la puerta: a Ollama llega solo la
	// lista cerrada de campos, con tool_choice, y ninguna credencial.
	largo := strings.Repeat("palabra ", 600)
	resp, uso, err := x.proveedor(1).Generar(ctx, mensajeria.PedidoIA{Modelo: "modelo-a:latest", Instrucciones: "Eres el asistente.", Contenido: largo, MaxTokens: 300,
		Herramientas: []mensajeria.HerramientaIA{{Nombre: "crear_ticket", Descripcion: "Crea un ticket", Parametros: map[string]any{"asunto": map[string]any{"type": "string"}}}}})
	if err != nil || resp.Texto == "" {
		t.Fatalf("generar por el servidor de IA: %v", err)
	}
	chats := x.banco.Ollama.Chats()
	var llego map[string]json.RawMessage
	if len(chats) != 1 || json.Unmarshal(chats[0].Cuerpo, &llego) != nil || string(llego["tool_choice"]) != `"auto"` || llego["tools"] == nil {
		t.Fatalf("lo que llegó a Ollama: %s", chats[0].Cuerpo)
	}
	if chats[0].Cabeceras.Get("Authorization") != "" || chats[0].Cabeceras.Get(voz.CabFirma) != "" {
		t.Fatal("a Ollama llegó una cabecera de Iris")
	}
	// El servidor declara 12 tokens de entrada para un texto de 4 800 caracteres: Iris cuenta su
	// propia estimación, que es mayor.
	if uso.Entrada < 1000 || uso.Salida < 7 {
		t.Fatalf("uso contado: %+v", uso)
	}
	var peticiones, tokens int64
	if err := x.bd.QueryRow(ctx, `SELECT sum(peticiones), sum(tokens_entrada) FROM mensajeria.servidor_ia_uso WHERE motor_id = $1 AND empresa_id = 1`, x.motor).Scan(&peticiones, &tokens); err != nil ||
		peticiones != 1 || tokens != uso.Entrada {
		t.Fatalf("uso guardado: %d %d %v", peticiones, tokens, err)
	}
	// Los modelos que ve la empresa.
	ms := x.exigir(x.api(x.admin, http.MethodGet, "/asistente/modelos", nil), http.StatusOK)
	if len(ms.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("modelos: %v", ms.cuerpo)
	}

	// Tablero: quien ve todo y el permiso.
	tab := x.exigir(x.api(x.admin, http.MethodGet, "/servidor-ia/tablero?rango=1h", nil), http.StatusOK)
	est := tab.cuerpo["estado"].(map[string]any)
	if est["estado"] != "listo" || est["lecturaOk"] != true || est["empresasActivas"] != float64(1) || len(tab.cuerpo["uso"].(map[string]any)["empresas"].([]any)) != 1 {
		t.Fatalf("tablero: %v", tab.cuerpo)
	}
	// Instalado sin GPU: el servidor respondió y no tiene tarjeta (no es «sin lectura»).
	if g := tab.cuerpo["servidor"].(map[string]any)["gpus"].(map[string]any); g["estado"] != "sin_gpu" || len(g["lista"].([]any)) != 0 {
		t.Fatalf("GPU de un servidor sin tarjeta: %v", g)
	}
	x.exigir(x.api(x.agente, http.MethodGet, "/servidor-ia/tablero", nil), http.StatusNotFound)
	um := x.exigir(x.api(x.admin, http.MethodGet, "/servidor-ia/umbrales", nil), http.StatusOK, "vramPct", "95", "saturacionRechazos", "10")
	x.exigir(x.api(x.admin, http.MethodPost, "/servidor-ia/umbrales/actualizar", map[string]any{"version": um.cuerpo["version"], "vramPct": 90, "vramMinutos": 5, "discoPct": 80,
		"temperaturaC": 80, "temperaturaMinutos": 5, "saturacionRechazos": 20, "saturacionMinutos": 5}), http.StatusOK, "vramPct", "90", "retencionDetalleDias", "7")
	x.exigir(x.api(x.admin, http.MethodPost, "/servidor-ia/umbrales/actualizar", map[string]any{"version": um.cuerpo["version"], "vramPct": 90, "vramMinutos": 5, "discoPct": 80,
		"temperaturaC": 80, "temperaturaMinutos": 5, "saturacionRechazos": 20, "saturacionMinutos": 5}), http.StatusConflict)

	// Modelos que las empresas pueden elegir: Iris no puede ampliar lo que declara el servidor.
	mo := x.exigir(x.api(x.admin, http.MethodGet, "/voz/motores", nil), http.StatusOK).cuerpo["data"].([]any)[0].(map[string]any)
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/modelos/actualizar"), map[string]any{"version": mo["version"], "modelos": []string{"modelo-b:latest"}}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:modelo_no_instalado")
	mo2 := x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/modelos/actualizar"), map[string]any{"version": mo["version"], "modelos": []string{}}), http.StatusOK)
	_, _, err = x.proveedor(1).Generar(ctx, mensajeria.PedidoIA{Modelo: "modelo-a:latest", Contenido: "hola", MaxTokens: 10})
	if codigoIA(err) != mensajeria.ErrIAServidorNoDisponible {
		t.Fatalf("un modelo que ya no se deja elegir: %s", codigoIA(err))
	}
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/modelos/actualizar"), map[string]any{"version": mo2.cuerpo["version"], "modelos": nil}), http.StatusOK)

	// Probar.
	pr := x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/probar"), nil), http.StatusOK)
	prueba := pr.cuerpo["ultimaPrueba"].(map[string]any)
	estados := map[string]string{}
	for _, p := range prueba["pasos"].([]any) {
		estados[p.(map[string]any)["codigo"].(string)] = p.(map[string]any)["estado"].(string)
	}
	if prueba["estado"] != "ok" || estados["conexion"] != "ok" || estados["llm"] != "ok" || estados["stt"] != "omitido" || estados["medios"] != "omitido" || estados["compatibilidad"] != "ok" {
		t.Fatalf("probar: %v", prueba)
	}

	// Rotar: con convivencia y «Revocar ahora» (cambia también el certificado de cliente).
	antes, _ := x.banco.Almacen.Leer()
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{}), http.StatusOK)
	rot, _ := x.banco.Almacen.Leer()
	if bytes.Equal(rot.Token, antes.Token) || !bytes.Equal(rot.TokenAnterior, antes.Token) || rot.HuellaCliente != antes.HuellaCliente {
		t.Fatal("rotar con convivencia")
	}
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{"revocar": true}), http.StatusOK)
	rev, _ := x.banco.Almacen.Leer()
	if rev.TokenAnterior != nil || rev.HuellaClienteAnterior != "" || rev.HuellaCliente == antes.HuellaCliente || bytes.Equal(rev.Token, rot.Token) {
		t.Fatal("revocar ahora debía cambiar token y certificado sin convivencia")
	}
	if _, _, err := x.proveedor(1).Generar(ctx, mensajeria.PedidoIA{Modelo: "modelo-a:latest", Contenido: "hola", MaxTokens: 10}); err != nil {
		t.Fatalf("tras rotar el canal debía seguir vivo: %v", err)
	}

	// Renovación del certificado del servidor: la huella nueva se acepta sola, anunciada por el canal.
	siguiente, err := x.banco.Almacen.PrepararSiguiente("iris-voz-prueba")
	if err != nil {
		t.Fatal(err)
	}
	x.latir()
	x.latir()
	var huella string
	if err := x.bd.QueryRow(ctx, `SELECT huella_tls FROM mensajeria.voz_motores WHERE id = $1`, x.motor).Scan(&huella); err != nil || huella != siguiente {
		t.Fatalf("la huella siguiente debía quedar fijada (%s, quería %s): %v", huella, siguiente, err)
	}

	// Desactivar («Desenlazar» en el panel): deja de usarse en el acto. Con el servidor desactivado
	// nada toca la red (F3): ni generar, ni «Probar» de la tarjeta, ni rotar, ni «Probar» y los
	// modelos del asistente. Todo responde servidor_desactivado.
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/desactivar"), map[string]any{"motivo": "mantenimiento"}), http.StatusOK, "estado", "desactivado")
	rutas := len(x.banco.Rutas())
	_, _, err = x.proveedor(1).Generar(ctx, mensajeria.PedidoIA{Modelo: "modelo-a:latest", Contenido: "hola", MaxTokens: 10})
	if codigoIA(err) != mensajeria.ErrIAServidorDesactivado {
		t.Fatalf("desactivado debía fallar al instante sin llamar: %s", codigoIA(err))
	}
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/probar"), nil), http.StatusConflict, "type", "urn:iris:problema:servidor_desactivado")
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/rotar-token"), map[string]any{}), http.StatusConflict, "type", "urn:iris:problema:servidor_desactivado")
	x.exigir(x.api(x.admin, http.MethodPost, "/asistente/probar", map[string]any{"proveedor": "servidor_ia", "url": "", "modelo": "modelo-a:latest"}), http.StatusConflict, "type", "urn:iris:problema:servidor_desactivado")
	x.exigir(x.api(x.admin, http.MethodGet, "/asistente/modelos", nil), http.StatusConflict, "type", "urn:iris:problema:servidor_desactivado")
	if n := len(x.banco.Rutas()); n != rutas {
		t.Fatalf("con el servidor desactivado no debía salir ninguna petición: %v", x.banco.Rutas()[rutas:])
	}
	if x.eventosIA("voz.motor_desactivado") != 1 {
		t.Fatal("falta voz.motor_desactivado")
	}
	// Salidas de «desactivado»: activar de nuevo (con la huella; rota el token) o enlazar otra vez.
	// (La huella es la que muestra ahora la tarjeta: el servidor renovó su certificado más arriba.)
	if r := x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"huellaTls": siguiente}), http.StatusOK); r.cuerpo["estado"] != "listo" ||
		r.cuerpo["activado"] != true {
		t.Fatalf("activar desde desactivado: %v", r.cuerpo)
	}
	if err := x.generar(); err != nil {
		t.Fatalf("tras activar de nuevo: %v", err)
	}
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/desactivar"), map[string]any{}), http.StatusOK, "estado", "desactivado")
	x.exigir(x.api(x.admin, http.MethodPost, "/voz/motores/enlazar", map[string]any{"nombre": "IA principal", "url": x.banco.URL}), http.StatusCreated)
}

func (x *bancoIA) eventosIA(evento string) int {
	x.t.Helper()
	var n int
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>'motor_id')::bigint = $2`, evento, x.motor).Scan(&n); err != nil {
		x.t.Fatal(err)
	}
	return n
}

func TestServidorIAPlazasCaidaYCompatibilidad(t *testing.T) {
	x := nuevoBancoIA(t, vozprueba.Opciones{})
	ctx := x.ctx
	x.enlazar()
	x.activar()
	x.exigir(x.elegir(x.admin, "modelo-a:latest", nil), http.StatusOK)
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Demora = 400 * time.Millisecond })

	varias := func(empresa int64, n int) []string {
		var wg sync.WaitGroup
		res := make([]string, n)
		for i := range res {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := x.proveedor(empresa).Generar(ctx, mensajeria.PedidoIA{Modelo: "modelo-a:latest", Contenido: "hola", MaxTokens: 10})
				res[i] = codigoIA(err)
			}()
		}
		wg.Wait()
		slices.Sort(res)
		return res
	}
	// Máximo por empresa (2 de fábrica): la tercera a la vez se rechaza al instante, sin cola.
	if got := varias(1, 3); fmt.Sprint(got) != "[ok ok servidor_ia_ocupado]" {
		t.Fatalf("máximo por empresa: %v", got)
	}
	var rechazos int64
	if err := x.bd.QueryRow(ctx, `SELECT coalesce(sum(rechazos), 0) FROM mensajeria.servidor_ia_uso WHERE motor_id = $1`, x.motor).Scan(&rechazos); err != nil || rechazos != 1 {
		t.Fatalf("rechazos anotados: %d %v", rechazos, err)
	}
	var vivas int
	if err := x.bd.QueryRow(ctx, `SELECT count(*) FROM mensajeria.servidor_ia_plazas`).Scan(&vivas); err != nil || vivas != 0 {
		t.Fatalf("plazas sin soltar: %d", vivas)
	}

	// Plaza garantizada: con 4 plazas y otra empresa activa sin nada en curso, la empresa 1 no puede
	// tomar las 4 aunque su máximo se lo permita; la empresa 2 sí consigue la suya.
	mo := x.exigir(x.api(x.admin, http.MethodGet, "/voz/motores", nil), http.StatusOK).cuerpo["data"].([]any)[0].(map[string]any)
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/empresas/actualizar"), map[string]any{"version": mo["version"], "empresas": nil, "simultaneasPorEmpresa": 4}), http.StatusOK)
	admin2 := x.persona([]int64{2}, rolBase("admin"))
	admin2.empresa = 2
	x.exigir(x.elegir(admin2, "modelo-a:latest", nil), http.StatusOK)
	if got := varias(1, 4); fmt.Sprint(got) != "[ok ok ok servidor_ia_ocupado]" {
		t.Fatalf("plaza garantizada: %v", got)
	}
	if got := varias(2, 1); fmt.Sprint(got) != "[ok]" {
		t.Fatalf("la empresa 2 debía tener su plaza: %v", got)
	}
	// Empresas permitidas: quitar la 2 la deja sin servidor (sus ajustes siguen).
	mo = x.exigir(x.api(x.admin, http.MethodGet, "/voz/motores", nil), http.StatusOK).cuerpo["data"].([]any)[0].(map[string]any)
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/empresas/actualizar"), map[string]any{"version": mo["version"], "empresas": []int64{1}, "simultaneasPorEmpresa": 2}), http.StatusOK)
	if got := varias(2, 1); fmt.Sprint(got) != "[servidor_ia_no_disponible]" {
		t.Fatalf("empresa no permitida: %v", got)
	}
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Demora = 0 })

	// Ollama caído: el servidor contesta «degradado» y el asistente falla al instante, sin llamar.
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Caido = true })
	time.Sleep(2100 * time.Millisecond)
	x.latir()
	if x.estado() != "degradado" {
		t.Fatalf("estado con Ollama caído: %s", x.estado())
	}
	n := len(x.banco.Ollama.Peticiones())
	if got := varias(1, 1); fmt.Sprint(got) != "[servidor_ia_no_disponible]" || len(x.banco.Ollama.Peticiones()) != n {
		t.Fatalf("degradado debía fallar sin llamar: %v", got)
	}
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Caido = false })
	time.Sleep(2100 * time.Millisecond)
	x.latir()
	if x.estado() != "listo" {
		t.Fatalf("debía volver solo: %s", x.estado())
	}
	if got := varias(1, 1); fmt.Sprint(got) != "[ok]" {
		t.Fatalf("al volver: %v", got)
	}

	// El servidor se cae del todo: tres latidos fallidos y queda no_disponible, con su evento.
	x.banco.Apagar()
	x.latir()
	x.latir()
	if x.estado() != "listo" {
		t.Fatalf("dos fallos no bastan: %s", x.estado())
	}
	x.latir()
	if x.estado() != "no_disponible" {
		t.Fatalf("tres fallos: %s", x.estado())
	}
	var eventos int
	if err := x.bd.QueryRow(ctx, `SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.motor_estado_cambiado' AND (payload->>'motor_id')::bigint = $1 AND payload->>'a' = 'no_disponible'`, x.motor).Scan(&eventos); err != nil || eventos != 1 {
		t.Fatalf("evento de caída: %d %v", eventos, err)
	}
	inicio := time.Now()
	if got := varias(1, 1); fmt.Sprint(got) != "[servidor_ia_no_disponible]" || time.Since(inicio) > time.Second {
		t.Fatalf("caído debía fallar al instante: %v en %s", got, time.Since(inicio))
	}
	// Y con el servidor caído ninguna empresa puede elegirlo.
	admin3 := x.persona([]int64{1}, rolBase("admin"))
	admin3.empresa = 1
	if s := x.exigir(x.api(admin3, http.MethodGet, "/asistente/ajustes", nil), http.StatusOK).cuerpo["servidorIa"].(map[string]any); s["disponible"] != false {
		t.Fatalf("servidorIa con el servidor caído: %v", s)
	}
}

// conVersion arma el servidor de IA de prueba con un Iris de esa versión.
func bancoIAConVersion(t *testing.T, version string, o vozprueba.Opciones) *bancoIA {
	t.Helper()
	return bancoIASobre(t, nuevaBandeja(t, func(c *mensajeria.Config) { c.PermitirLoopbackIA = true; c.Version = version }), o)
}

// F4: solo una versión PUBLICADA exacta fuera del rango del paquete es incompatible, y entonces
// «Activar» responde 409 con el motivo en vez de dejar el servidor activado y en rojo.
func TestServidorIAIncompatible(t *testing.T) {
	man := voz.Manifiesto{Version: "0.1.0", Protocolo: voz.Protocolo, IrisMin: "0.28.0", IrisMax: "0.99.0"}
	x := bancoIAConVersion(t, "v0.27.0", vozprueba.Opciones{Manifiesto: &man})
	r := x.enlazar()
	paquete := r.cuerpo["paquete"].(map[string]any)
	if paquete["compatible"] != false || paquete["motivoIncompatible"] != "iris_antiguo" {
		t.Fatalf("paquete: %v", paquete)
	}
	antes := len(x.banco.Rutas())
	a := x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"huellaTls": x.banco.Huella})
	x.exigir(a, http.StatusConflict, "type", "urn:iris:problema:incompatible")
	if !strings.Contains(fmt.Sprint(a.cuerpo["detail"]), "actualizar Iris") || a.cuerpo["errors"].([]any)[0].(map[string]any)["rule"] != "iris_antiguo" ||
		len(x.banco.Rutas()) != antes || x.columna("activado_en") != nil {
		t.Fatalf("activar un servidor incompatible: %v", a.cuerpo)
	}
	// Incompatible = no se usa.
	x.exigir(x.elegir(x.admin, "modelo-a:latest", nil), http.StatusUnprocessableEntity, "type", "urn:iris:problema:servidor_ia_no_disponible")
	_, _, err := x.proveedor(1).Generar(x.ctx, mensajeria.PedidoIA{Modelo: "modelo-a:latest", Contenido: "hola", MaxTokens: 10})
	if codigoIA(err) != mensajeria.ErrIAServidorNoDisponible || len(x.banco.Ollama.Chats()) != 0 {
		t.Fatalf("incompatible debía fallar sin llamar: %s", codigoIA(err))
	}
}

// F4: un binario de desarrollo (sin versión, o construido con commits después de la etiqueta) no se
// compara con el rango del paquete: enlaza, activa y se usa.
func TestServidorIAVersionDeDesarrollo(t *testing.T) {
	man := voz.Manifiesto{Version: "0.1.0", Protocolo: voz.Protocolo, IrisMin: "0.28.0", IrisMax: "0.99.0"}
	for _, version := range []string{"0.0.0-dev", "v0.27.0-36-g1a2b3c4", "v0.27.0-36-g1a2b3c4-dirty", "prueba"} {
		x := bancoIAConVersion(t, version, vozprueba.Opciones{Manifiesto: &man})
		r := x.enlazar()
		if p := r.cuerpo["paquete"].(map[string]any); p["compatible"] != true || p["motivoIncompatible"] != nil {
			t.Fatalf("%s: un binario de desarrollo debía ser compatible: %v", version, p)
		}
		if a := x.activar(); a.cuerpo["estado"] != "listo" {
			t.Fatalf("%s: estado tras activar: %v", version, a.cuerpo["estado"])
		}
		if err := x.generar(); err != nil {
			t.Fatalf("%s: la puerta tampoco debía rechazar a un Iris de desarrollo: %v", version, err)
		}
	}
	// La versión publicada exacta dentro del rango, también; y una ya activa que deja de ser
	// compatible (Iris se actualizó por encima del rango) queda «incompatible» en el latido siguiente.
	x := bancoIAConVersion(t, "0.28.0", vozprueba.Opciones{Manifiesto: &man})
	x.enlazar()
	if a := x.activar(); a.cuerpo["estado"] != "listo" {
		t.Fatalf("0.28.0: %v", a.cuerpo["estado"])
	}
	yb := nuevaBandeja(t, func(c *mensajeria.Config) { c.PermitirLoopbackIA = true; c.Version = "1.0.0" })
	*yb.reloj = *x.reloj
	y := &bancoIA{bandeja: yb, banco: x.banco, motor: x.motor}
	y.latir()
	if y.estado() != "incompatible" {
		t.Fatalf("un Iris 1.0.0 con un paquete que admite hasta 0.99.0: %s", y.estado())
	}
	var motivo *string
	if err := y.bd.QueryRow(y.ctx, `SELECT motivo_incompatible FROM mensajeria.voz_motores WHERE id = $1`, y.motor).Scan(&motivo); err != nil || motivo == nil || *motivo != "iris_nuevo" {
		t.Fatalf("motivo: %v %v", motivo, err)
	}
}
