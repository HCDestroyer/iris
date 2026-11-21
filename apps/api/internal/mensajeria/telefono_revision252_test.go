package mensajeria_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"
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
	*b.reloj = b.reloj.Add(5 * time.Minute)
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
	*b.reloj = b.reloj.Add(26 * time.Minute)
	r = b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "credenciales" {
		t.Fatalf("tras el plazo: %v", r.cuerpo)
	}
	// Escribir otra contraseña la deja probar enseguida, y una prueba que pasa borra el rechazo.
	*b.reloj = b.reloj.Add(time.Second)
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": secretoSIP}), http.StatusOK)
	r = b.probarRegistro()
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" || r.cuerpo["extension"].(map[string]any)["sipRechazadoEn"] != nil {
		t.Fatalf("con la buena: %v", r.cuerpo)
	}
}

func TestTelefonoProbarRegistroTopePorMinuto(t *testing.T) {
	b := nuevoBancoTelefono(t)
	*b.reloj = b.reloj.Add(2 * time.Minute)
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
	*b.reloj = b.reloj.Add(61 * time.Second)
	b.exigir(b.probarRegistro(), http.StatusOK)
	// El tope de la centralita junta todas sus extensiones.
	*b.reloj = b.reloj.Add(2 * time.Minute)
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
