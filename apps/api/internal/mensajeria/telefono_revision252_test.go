package mensajeria_test

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// Revisión de seguridad de #252 (fase 2), punto 1 (fail2ban): «Probar registro» no repite una
// contraseña que la Issabel ya rechazó y tiene tope por minuto por extensión y por centralita.

func (b *bancoTelefono) probarRegistro() respuesta {
	b.t.Helper()
	return b.api(b.admin, http.MethodPost, b.rutaExt("/probar-registro"), map[string]any{})
}

func exigirReintentarDespues(t *testing.T, r respuesta, maximo int) {
	t.Helper()
	s, err := strconv.Atoi(r.h.Get("Retry-After"))
	if err != nil || s <= 0 || s > maximo {
		t.Fatalf("Retry-After: %q (máximo %d)", r.h.Get("Retry-After"), maximo)
	}
}

func TestTelefonoProbarRegistroNoRepiteLaRechazada(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": "otra-clave-mala"}), http.StatusOK)
	r := b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "credenciales" || r.cuerpo["extension"].(map[string]any)["sipRechazadoEn"] == nil {
		t.Fatalf("clave mala: %v", r.cuerpo)
	}
	antes := len(b.sip.Recibidos())
	// La misma contraseña no se vuelve a probar: 429 sin tocar la Issabel.
	b.avanzarReloj(5 * time.Minute)
	r = b.probarRegistro()
	b.exigirProblema(r, http.StatusTooManyRequests, "credencial_rechazada")
	exigirReintentarDespues(t, r, 25*60)
	if n := len(b.sip.Recibidos()) - antes; n != 0 {
		t.Fatalf("REGISTER con la contraseña rechazada: %d", n)
	}
	// La lista de extensiones dice que fue rechazada (el panel quita «Probar registro»).
	r = b.api(b.admin, http.MethodGet, b.ruta("/extensiones"), nil)
	b.exigir(r, http.StatusOK)
	var vista map[string]any
	for _, x := range r.cuerpo["data"].([]any) {
		if x.(map[string]any)["id"] == b.extID {
			vista = x.(map[string]any)
		}
	}
	if vista == nil || vista["sipRechazadoEn"] == nil {
		t.Fatalf("lista sin sipRechazadoEn: %v", vista)
	}
	// Pasado el plazo se puede probar otra vez (fail2ban ya olvidó).
	b.avanzarReloj(26 * time.Minute)
	r = b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "credenciales" {
		t.Fatalf("tras el plazo: %v", r.cuerpo)
	}
	// Escribir otra contraseña la deja probar enseguida, y una prueba que pasa borra el rechazo.
	b.avanzarReloj(time.Second)
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": secretoSIP}), http.StatusOK)
	r = b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" || r.cuerpo["extension"].(map[string]any)["sipRechazadoEn"] != nil {
		t.Fatalf("con la buena: %v", r.cuerpo)
	}
}

func TestTelefonoProbarRegistroTopePorMinuto(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.avanzarReloj(2 * time.Minute)
	for i := range 3 {
		r := b.probarRegistro()
		b.exigir(r, http.StatusOK)
		if r.cuerpo["estado"] != "ok" {
			t.Fatalf("prueba %d: %v", i, r.cuerpo)
		}
	}
	antes := len(b.sip.Recibidos())
	r := b.probarRegistro()
	b.exigirProblema(r, http.StatusTooManyRequests, "demasiados_intentos")
	exigirReintentarDespues(t, r, 60)
	if n := len(b.sip.Recibidos()) - antes; n != 0 {
		t.Fatalf("REGISTER pasado el tope: %d", n)
	}
	// Un 429 no alarga la ventana: al minuto se puede otra vez.
	b.avanzarReloj(61 * time.Second)
	b.exigir(b.probarRegistro(), http.StatusOK)
	// El tope de la centralita junta todas sus extensiones.
	b.avanzarReloj(2 * time.Minute)
	b.sql(`UPDATE mensajeria.centralitas SET registro_pruebas = 10, registro_pruebas_desde = $2 WHERE id = $1`, b.id, *b.reloj)
	b.exigirProblema(b.probarRegistro(), http.StatusTooManyRequests, "demasiados_intentos")
}

// El puente también marca el rechazo: tras un 403 de la Issabel al navegador, «Probar registro» con
// la misma contraseña responde 429.
func TestTelefonoRechazoDelPuenteBloqueaLaPrueba(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": "otra-clave-mala"}), http.StatusOK)
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_probado_en = now() WHERE id = $1`, int64(b.extID))
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	b.registrar(w)
	w.respuesta(403)
	if c := w.cierre(); c != 4003 {
		t.Fatalf("cierre: %d", c)
	}
	b.hasta(func() bool {
		var rechazado *time.Time
		_ = b.bd.QueryRow(b.ctx, `SELECT sip_rechazado_en FROM mensajeria.centralita_extensiones WHERE id = $1`, int64(b.extID)).Scan(&rechazado)
		return rechazado != nil
	}, "el puente no marcó el rechazo")
	b.exigirProblema(b.probarRegistro(), http.StatusTooManyRequests, "credencial_rechazada")
}

func (b *bancoTelefono) rechazosCentralita() int {
	var n int
	_ = b.bd.QueryRow(b.ctx, `SELECT rechazos FROM mensajeria.centralitas WHERE id = $1`, b.id).Scan(&n)
	return n
}

func (b *bancoTelefono) guardarSecreto(secreto string) {
	b.t.Helper()
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": secreto}), http.StatusOK)
}

// Re-verificación de #252, M1: los rechazos de la Issabel se cuentan por centralita (10 min, tope 3),
// sumando «Probar registro» y el puente. Pasado el tope no se prueba ni se abre el teléfono.
func TestTelefonoTopeDeRechazosPorCentralita(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.guardarSecreto("otra-clave-mala")
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_probado_en = now() WHERE id = $1`, int64(b.extID))
	// Un rechazo en el puente…
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	b.registrar(w)
	w.respuesta(403)
	if c := w.cierre(); c != 4003 {
		t.Fatalf("cierre: %d", c)
	}
	b.hasta(func() bool { return b.rechazosCentralita() == 1 }, "el puente no contó el rechazo")
	// … y dos en «Probar registro», cada uno con otra contraseña (el tope por minuto no los frena).
	for i := range 2 {
		b.avanzarReloj(25 * time.Second)
		b.guardarSecreto("clave-mala-" + strconv.Itoa(i))
		r := b.probarRegistro()
		b.exigir(r, http.StatusOK)
		if r.cuerpo["estado"] != "credenciales" {
			t.Fatalf("rechazo %d: %v", i, r.cuerpo)
		}
	}
	// Con la cuarta contraseña ya no se prueba: 429 sin tocar la Issabel.
	b.avanzarReloj(25 * time.Second)
	b.guardarSecreto(secretoSIP)
	antes := len(b.sip.Recibidos())
	r := b.probarRegistro()
	b.exigirProblema(r, http.StatusTooManyRequests, "demasiados_rechazos")
	exigirReintentarDespues(t, r, 10*60)
	if n := len(b.sip.Recibidos()) - antes; n != 0 {
		t.Fatalf("REGISTER pasado el tope de rechazos: %d", n)
	}
	// Tampoco se abre el teléfono mientras dure.
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_probado_en = now() WHERE id = $1`, int64(b.extID))
	r = b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"})
	b.exigirProblema(r, http.StatusTooManyRequests, "demasiados_rechazos")
	exigirReintentarDespues(t, r, 10*60)
	// Pasada la ventana, todo vuelve.
	b.avanzarReloj(10 * time.Minute)
	r = b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" {
		t.Fatalf("tras la ventana: %v", r.cuerpo)
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"}), http.StatusCreated)
}

// Re-verificación de #252, B2: el resultado de una prueba solo marca la contraseña que se probó. Si
// mientras tanto se guardó otra, la nueva no queda rechazada (ni probada).
func TestTelefonoPruebaDeLaViejaNoMarcaLaNueva(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.guardarSecreto("otra-clave-mala")
	cambiar := func(secreto string) func() {
		return func() {
			b.avanzarReloj(time.Second)
			b.guardarSecreto(secreto)
		}
	}
	t.Cleanup(func() { b.m.FijarAntesDeProbarRegistro(nil) })
	// La vieja (mala) se rechaza, pero ya se guardó la buena: la buena no queda rechazada.
	b.m.FijarAntesDeProbarRegistro(cambiar(secretoSIP))
	r := b.probarRegistro()
	b.exigir(r, http.StatusOK)
	x := r.cuerpo["extension"].(map[string]any)
	if r.cuerpo["estado"] != "credenciales" || x["sipRechazadoEn"] != nil || x["sipProbadoEn"] != nil {
		t.Fatalf("la prueba de la vieja marcó la nueva: %v", r.cuerpo)
	}
	// La vieja (buena) pasa, pero ya se guardó una mala: la mala no queda probada.
	b.m.FijarAntesDeProbarRegistro(cambiar("otra-mala"))
	r = b.probarRegistro()
	b.exigir(r, http.StatusOK)
	x = r.cuerpo["extension"].(map[string]any)
	if r.cuerpo["estado"] != "ok" || x["sipProbadoEn"] != nil {
		t.Fatalf("la prueba de la vieja dio por probada la nueva: %v", r.cuerpo)
	}
}

// B2 en el puente: el 403 de una conexión abierta con la contraseña vieja no marca la nueva (pero
// cuenta para el tope de la centralita: la Issabel lo vio).
func TestTelefonoRechazoDelPuenteConLaVieja(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.guardarSecreto("otra-clave-mala")
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_probado_en = now() WHERE id = $1`, int64(b.extID))
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	b.avanzarReloj(time.Second)
	b.guardarSecreto(secretoSIP)
	b.registrar(w)
	w.respuesta(403)
	_ = w.cierre()
	b.hasta(func() bool { return b.rechazosCentralita() == 1 }, "el puente no contó el rechazo")
	var rechazado *time.Time
	if err := b.bd.QueryRow(b.ctx, `SELECT sip_rechazado_en FROM mensajeria.centralita_extensiones WHERE id = $1`, int64(b.extID)).Scan(&rechazado); err != nil {
		t.Fatal(err)
	}
	if rechazado != nil {
		t.Fatal("el 403 de la contraseña vieja marcó la nueva como rechazada")
	}
	r := b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" {
		t.Fatalf("la nueva: %v", r.cuerpo)
	}
}

// #252 B5: un solo «Probar registro» a la vez por centralita (el REGISTER corre fuera de la
// transacción: leer el contador de rechazos no basta). La otra recibe 429 con un Retry-After corto.
func TestTelefonoUnaPruebaDeRegistroALaVez(t *testing.T) {
	b := nuevoBancoTelefono(t)
	t.Cleanup(func() { b.m.FijarAntesDeProbarRegistro(nil) })
	// Mientras la primera está por mandar su REGISTER, llega otra.
	enMedio := func() respuesta {
		// Solo la primera llama a otra (sin el turno, la otra también pasaría por aquí).
		var una atomic.Bool
		var otra respuesta
		b.m.FijarAntesDeProbarRegistro(func() {
			if una.CompareAndSwap(false, true) {
				otra = b.probarRegistro()
			}
		})
		r := b.probarRegistro()
		b.m.FijarAntesDeProbarRegistro(nil)
		b.exigir(r, http.StatusOK)
		if r.cuerpo["estado"] != "ok" {
			t.Fatalf("la primera: %v", r.cuerpo)
		}
		return otra
	}
	b.avanzarReloj(2 * time.Minute)
	otra := enMedio()
	b.exigirProblema(otra, http.StatusTooManyRequests, "prueba_en_curso")
	exigirReintentarDespues(t, otra, 10)
	// Con rechazos en la ventana (aunque sean menos que el tope), lo mismo como `demasiados_rechazos`.
	b.avanzarReloj(2 * time.Minute)
	b.sql(`UPDATE mensajeria.centralitas SET rechazos = 2, rechazos_desde = $2 WHERE id = $1`, b.id, *b.reloj)
	antes := len(b.sip.Recibidos())
	otra = enMedio()
	b.exigirProblema(otra, http.StatusTooManyRequests, "demasiados_rechazos")
	exigirReintentarDespues(t, otra, 10)
	// Solo salió el REGISTER de la primera (consulta y desafío: 2 mensajes).
	if n := len(b.sip.Recibidos()) - antes; n != 2 {
		t.Fatalf("REGISTER en la centralita: %d", n)
	}
	// Al terminar la primera, el turno queda libre.
	b.avanzarReloj(2 * time.Minute)
	r := b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" {
		t.Fatalf("después: %v", r.cuerpo)
	}
}

// #252 B6: el rechazo del puente cuenta para la centralita aunque la extensión ya no sea de la persona
// (o la sesión se haya cerrado): se anota como sistema, antes de buscar la extensión.
func TestTelefonoRechazoDelPuenteCuentaSinExtension(t *testing.T) {
	b := nuevoBancoTelefono(t, func(c *mensajeria.Config) { c.TelefonoLatido = time.Hour })
	b.guardarSecreto("otra-clave-mala")
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_probado_en = now() WHERE id = $1`, int64(b.extID))
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	// La extensión deja de estar (sin el latido, el puente sigue abierto).
	b.sql(`UPDATE mensajeria.centralita_extensiones SET activa = false WHERE id = $1`, int64(b.extID))
	b.registrar(w)
	w.respuesta(403)
	if c := w.cierre(); c != 4003 {
		t.Fatalf("cierre: %d", c)
	}
	b.hasta(func() bool { return b.rechazosCentralita() == 1 }, "el rechazo no contó para la centralita")
}

func (b *bancoTelefono) turnoCentralita() *time.Time {
	var hasta *time.Time
	_ = b.bd.QueryRow(b.ctx, `SELECT registro_en_curso_hasta FROM mensajeria.centralitas WHERE id = $1`, b.id).Scan(&hasta)
	return hasta
}

// #252 M2: si la petición se cancela (o quitan la extensión) después del REGISTER, el rechazo cuenta
// igual y el turno se suelta: se dejan como sistema y sin el contexto de la petición.
func TestTelefonoRechazoCuentaAunqueCortenLaPrueba(t *testing.T) {
	b := nuevoBancoTelefono(t)
	t.Cleanup(func() { b.m.FijarTrasProbarRegistro(nil) })
	b.guardarSecreto("otra-clave-mala")
	// El cliente corta en cuanto salió el REGISTER.
	ctx, cancelar := context.WithCancel(context.Background())
	b.m.FijarTrasProbarRegistro(func(context.Context) { cancelar() })
	_ = b.apiCtx(ctx, b.admin, http.MethodPost, b.rutaExt("/probar-registro"), "", map[string]any{})
	if n := b.rechazosCentralita(); n != 1 {
		t.Fatalf("rechazos tras cancelar: %d", n)
	}
	if h := b.turnoCentralita(); h != nil {
		t.Fatalf("el turno quedó tomado: %v", h)
	}
	// Quitan la extensión a mitad de la prueba: también cuenta.
	b.avanzarReloj(2 * time.Minute)
	b.guardarSecreto("otra-clave-mala-2")
	b.m.FijarTrasProbarRegistro(func(context.Context) {
		b.sql(`UPDATE mensajeria.centralita_extensiones SET activa = false WHERE id = $1`, int64(b.extID))
	})
	b.exigirProblema(b.probarRegistro(), http.StatusNotFound, "no_encontrado")
	if n := b.rechazosCentralita(); n != 2 {
		t.Fatalf("rechazos con la extensión quitada: %d", n)
	}
}

// #252 B7: una prueba solo suelta su propia reserva. Si la suya venció y otro tomó el turno, no lo toca.
func TestTelefonoPruebaSoloSueltaSuTurno(t *testing.T) {
	b := nuevoBancoTelefono(t)
	t.Cleanup(func() { b.m.FijarTrasProbarRegistro(nil) })
	b.avanzarReloj(2 * time.Minute)
	otro := b.reloj.Add(time.Minute)
	b.m.FijarTrasProbarRegistro(func(context.Context) {
		// La reserva de esta prueba venció y otra prueba tomó el turno.
		b.sql(`UPDATE mensajeria.centralitas SET registro_en_curso_hasta = $2 WHERE id = $1`, b.id, otro)
	})
	b.exigir(b.probarRegistro(), http.StatusOK)
	if h := b.turnoCentralita(); h == nil || !h.Equal(otro) {
		t.Fatalf("soltó el turno de otro: %v", h)
	}
}

// #252 B8: con rechazos en la ventana, /telefono/abrir y la conexión del puente toman el mismo turno
// de REGISTER que «Probar registro». El ticket lleva su turno: con él se conecta; sin turno, 429.
func TestTelefonoAbrirYConectarTomanElTurno(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.sql(`UPDATE mensajeria.centralitas SET rechazos = 1, rechazos_desde = $2 WHERE id = $1`, b.id, *b.reloj)
	ticket := b.abrir(b.agente, "panel")
	if b.turnoCentralita() == nil {
		t.Fatal("/abrir no tomó el turno")
	}
	// Mientras lo tiene, ni otro /abrir ni «Probar registro».
	r := b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"})
	b.exigirProblema(r, http.StatusTooManyRequests, "demasiados_rechazos")
	exigirReintentarDespues(t, r, 10)
	b.exigirProblema(b.probarRegistro(), http.StatusTooManyRequests, "demasiados_rechazos")
	// Con su ticket se conecta (ya tiene el turno).
	if _, _, err := b.dial(b.agente, ticket, baseIris); err != nil {
		t.Fatalf("conectar con el turno del ticket: %v", err)
	}
	// Un ticket sin turno (de antes de los rechazos) no conecta si otro lo tiene.
	b.avanzarReloj(31 * time.Second)
	b.sql(`UPDATE mensajeria.centralitas SET rechazos = 0, rechazos_desde = NULL WHERE id = $1`, b.id)
	sinTurno := b.abrir(b.agente, "panel")
	b.sql(`UPDATE mensajeria.centralitas SET rechazos = 1, rechazos_desde = $2, registro_en_curso_hasta = $3 WHERE id = $1`,
		b.id, *b.reloj, b.reloj.Add(20*time.Second))
	if _, status, err := b.dial(b.agente, sinTurno, baseIris); err == nil || status != http.StatusTooManyRequests {
		t.Fatalf("conectar sin turno: %d %v", status, err)
	}
	// Al vencer el turno ajeno, /abrir vuelve.
	b.avanzarReloj(21 * time.Second)
	b.abrir(b.agente, "panel")
}

// CI de #258: el puente marca el rechazo en su propia transacción, después de contarlo. Si la marca
// llega en el mismo instante en que se guarda otra contraseña (el reloj de las pruebas está quieto),
// la nueva no puede quedar como rechazada: guardar otra contraseña borra el rechazo de la anterior.
func TestTelefonoOtraContrasenaBorraElRechazoAunqueSeaAlMismoInstante(t *testing.T) {
	b := nuevoBancoTelefono(t)
	b.guardarSecreto("otra-clave-mala")
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_rechazado_en = $2 WHERE id = $1`, int64(b.extID), *b.reloj)
	b.guardarSecreto(secretoSIP)
	var rechazado *time.Time
	_ = b.bd.QueryRow(b.ctx, `SELECT sip_rechazado_en FROM mensajeria.centralita_extensiones WHERE id = $1`, int64(b.extID)).Scan(&rechazado)
	if rechazado != nil {
		t.Fatalf("sip_rechazado_en tras guardar otra contraseña: %v", rechazado)
	}
	b.exigir(b.probarRegistro(), http.StatusOK)
	// Guardar sin cambiar la contraseña (solo el tipo o el usuario) no borra el rechazo.
	b.sql(`UPDATE mensajeria.centralita_extensiones SET sip_rechazado_en = $2 WHERE id = $1`, int64(b.extID), *b.reloj)
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador"}), http.StatusOK)
	b.exigirProblema(b.probarRegistro(), http.StatusTooManyRequests, "credencial_rechazada")
}
