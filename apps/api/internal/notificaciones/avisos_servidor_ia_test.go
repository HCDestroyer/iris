package notificaciones_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
)

// Avisos del servidor de IA (contratos/voz.md §4.12): campana y correo a quienes administran la
// instalación, uno por episodio, visibles con cualquier empresa activa.

func (x *entornoAvisos) avisarIA(in notificaciones.EntradaAvisarServidorIA) notificaciones.ResultadoAvisarTope {
	x.t.Helper()
	if in.EventoID == uuid.Nil {
		in.EventoID = uuid.Must(uuid.NewV7())
	}
	r, err := x.notif.AvisarServidorIA.Ejecutar(x.ctx, in)
	if err != nil {
		x.t.Fatal(err)
	}
	return r
}

func (x *entornoAvisos) sql(sql string, args ...any) {
	x.t.Helper()
	if _, err := x.bd.Exec(x.ctx, sql, args...); err != nil {
		x.t.Fatal(err)
	}
}

func (x *entornoAvisos) avisosDe(p persona, tipo string) []map[string]any {
	x.t.Helper()
	var out []map[string]any
	for _, a := range x.esperar(x.api(p, http.MethodGet, "/api/v1/yo/avisos?limit=50", nil), http.StatusOK).cuerpo["data"].([]any) {
		if m := a.(map[string]any); m["tipo"] == tipo {
			out = append(out, m)
		}
	}
	return out
}

func TestAvisoServidorIACampanaYCorreo(t *testing.T) {
	x := nuevoEntornoAvisos(t, true)
	// El admin ve el servidor de la instalación; el supervisor no tiene el permiso, y quien lo tiene
	// sin todas las empresas tampoco (el servidor no es de una empresa).
	admin := x.persona([]int64{1}, "admin")
	sup := x.persona([]int64{1}, "supervisor")
	soloPermiso := x.configurador([]int64{1})
	x.sql(`INSERT INTO nucleo.roles_permisos (rol_id, permiso) SELECT ur.rol_id, 'servidor_ia.ver' FROM nucleo.usuarios_roles ur WHERE ur.usuario_id = $1`, soloPermiso.id)
	const motor = 987654

	// Caído: un aviso de peligro, con su correo.
	evento := uuid.Must(uuid.NewV7())
	r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{EventoID: evento, MotorID: motor, A: "no_disponible"})
	if r.Avisos < 1 || r.CorreosPendientes != r.Avisos {
		t.Fatalf("avisos: %+v", r)
	}
	caidos := x.avisosDe(admin, "servidor_ia_caido")
	if len(caidos) != 1 || caidos[0]["titulo"] != "El servidor de IA no responde" || caidos[0]["nivel"] != "peligro" ||
		caidos[0]["enlace"] != "/administracion/servidor-ia" || caidos[0]["leido"] != false {
		t.Fatalf("aviso de caído: %v", caidos)
	}
	for _, p := range []persona{sup, soloPermiso} {
		if d := x.avisosDe(p, "servidor_ia_caido"); len(d) != 0 {
			t.Fatalf("%s no debía recibirlo: %v", p.nombre, d)
		}
	}
	// Es de la instalación: se ve igual con otra empresa activa.
	otra := admin
	otra.empresa = 2
	x.sql(`INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, 2) ON CONFLICT DO NOTHING`, admin.id)
	if d := x.avisosDe(otra, "servidor_ia_caido"); len(d) != 1 {
		t.Fatalf("con otra empresa activa debía verse: %v", d)
	}
	// El mismo evento otra vez (el relay reintenta): nada nuevo.
	if r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{EventoID: evento, MotorID: motor, A: "no_disponible"}); r.Avisos != 0 {
		t.Fatalf("evento repetido: %+v", r)
	}
	x.mandarCorreos(false)
	c := x.correo.para(admin.correo)
	if len(c) != 1 || c[0].asunto != "El servidor de IA no responde" || !strings.Contains(c[0].texto, "https://iris.prueba/administracion/servidor-ia") {
		t.Fatalf("correo de caído: %+v", c)
	}

	// Volver a «listo» o quedar degradado no avisa.
	for _, a := range []string{"listo", "degradado", "desactivado"} {
		if r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: a}); r.Avisos != 0 {
			t.Fatalf("%s no avisa: %+v", a, r)
		}
	}
	// Incompatible: otro aviso.
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: "incompatible"})
	if d := x.avisosDe(admin, "servidor_ia_incompatible"); len(d) != 1 || d[0]["nivel"] != "peligro" {
		t.Fatalf("aviso de incompatible: %v", d)
	}

	// Umbral: alerta, con el valor y el umbral como números. Un tipo fuera de la lista no avisa.
	valor, umbral := 97.4, 95.0
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, Tipo: "vram", Valor: &valor, Umbral: &umbral})
	um := x.avisosDe(admin, "servidor_ia_umbral")
	if len(um) != 1 || um[0]["titulo"] != "El servidor de IA tiene la memoria de video casi llena" || um[0]["nivel"] != "alerta" {
		t.Fatalf("aviso de umbral: %v", um)
	}
	if d := um[0]["datos"].(map[string]any); d["tipo"] != "vram" || d["valor"] != 97.4 || d["umbral"] != 95.0 {
		t.Fatalf("datos del umbral: %v", d)
	}
	if r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, Tipo: "<script>"}); r.Avisos != 0 {
		t.Fatalf("un tipo desconocido no avisa: %+v", r)
	}
	x.mandarCorreos(false)
	c = x.correo.para(admin.correo)
	if len(c) != 3 || !strings.Contains(c[2].texto, "Valor medido: 97. Umbral: 95.") {
		t.Fatalf("correo del umbral: %+v", c)
	}
	// Otro episodio del mismo tipo en la misma hora: no avisa otra vez. Otro umbral, sí.
	if r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, Tipo: "vram", Valor: &valor, Umbral: &umbral}); r.Avisos != 0 {
		t.Fatalf("otro episodio en la misma hora no debía avisar: %+v", r)
	}
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, Tipo: "disco", Valor: &valor, Umbral: &umbral})
	if um := x.avisosDe(admin, "servidor_ia_umbral"); len(um) != 2 {
		t.Fatalf("avisos de umbral (VRAM y disco): %d", len(um))
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'notificaciones.avisos_creados' AND payload->>'tipo' = 'servidor_ia_umbral'
		AND payload->'usuarios' @> to_jsonb($1::bigint) AND payload->>'empresa_id' IS NULL`, admin.id); n != 3 { // dos nuevos y uno reactivado
		t.Fatalf("notificaciones.avisos_creados: %d", n)
	}
}

// N1 (enfriamiento): un servidor que va y viene (o uno hostil) no inunda la campana ni el correo:
// como mucho un aviso y un correo por tipo y servidor por hora.
func TestAvisoServidorIAEnfriamiento(t *testing.T) {
	x := nuevoEntornoAvisos(t, true)
	admin := x.persona([]int64{1}, "admin")
	const motor = 987656
	valor, umbral := 96.0, 95.0
	// Diez vueltas en cinco minutos: caído ↔ listo, incompatible ↔ listo y el disco cruzando el umbral.
	for range 10 {
		x.reloj = x.reloj.Add(30 * time.Second)
		for _, a := range []string{"no_disponible", "listo", "incompatible", "listo"} {
			x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: a})
		}
		x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, Tipo: "disco", Valor: &valor, Umbral: &umbral})
	}
	x.mandarCorreos(false)
	propios := func(tipo string) int {
		n := 0
		for _, a := range x.avisosDe(admin, tipo) {
			if a["datos"].(map[string]any)["motor_id"] == float64(motor) {
				n++
			}
		}
		return n
	}
	for _, tipo := range []string{"servidor_ia_caido", "servidor_ia_incompatible", "servidor_ia_umbral"} {
		if n := propios(tipo); n != 1 {
			t.Fatalf("%s: %d avisos tras diez vueltas (debía ser 1)", tipo, n)
		}
		// El mismo aviso, no leído, con la cuenta de las diez veces.
		for _, a := range x.avisosDe(admin, tipo) {
			if d := a["datos"].(map[string]any); d["motor_id"] == float64(motor) && (d["veces"] != float64(10) || a["leido"] != false) {
				t.Fatalf("%s: cuenta del aviso: %v", tipo, a)
			}
		}
		if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE usuario_id = $1 AND tipo = $2 AND (datos->>'motor_id')::bigint = $3`, admin.id, tipo, motor); n != 1 {
			t.Fatalf("%s: %d correos tras diez vueltas (debía ser 1)", tipo, n)
		}
	}
	if c := x.correo.para(admin.correo); len(c) != 3 {
		t.Fatalf("correos enviados al admin: %d", len(c))
	}
	// Pasada la hora, si sigue pasando, avisa otra vez (una).
	x.reloj = x.reloj.Add(time.Hour)
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: "no_disponible"})
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: "no_disponible"})
	if n := propios("servidor_ia_caido"); n != 2 {
		t.Fatalf("a la hora siguiente debía avisar una vez más: %d", n)
	}
}

// Sin correo del sistema el aviso de campana sale igual y el correo queda omitido.
func TestAvisoServidorIASinCorreo(t *testing.T) {
	x := nuevoEntornoAvisos(t, false)
	admin := x.persona([]int64{1}, "admin")
	r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: 987655, A: "no_disponible"})
	if r.Avisos < 1 || r.CorreosPendientes != 0 || r.CorreosOmitidos != r.Avisos {
		t.Fatalf("sin correo del sistema: %+v", r)
	}
	if d := x.avisosDe(admin, "servidor_ia_caido"); len(d) != 1 {
		t.Fatalf("la campana sale igual: %v", d)
	}
}

// F5: una segunda caída dentro de la misma hora no crea otro aviso ni otro correo, pero la campana
// no se queda callada: el aviso que ya hay vuelve a no leído, con la hora nueva y la cuenta.
func TestAvisoServidorIASegundaCaidaEnLaHora(t *testing.T) {
	x := nuevoEntornoAvisos(t, true)
	admin := x.persona([]int64{1}, "admin")
	const motor = 987657
	uno := func() map[string]any {
		t.Helper()
		var mio []map[string]any
		for _, a := range x.avisosDe(admin, "servidor_ia_caido") {
			if a["datos"].(map[string]any)["motor_id"] == float64(motor) {
				mio = append(mio, a)
			}
		}
		if len(mio) != 1 {
			t.Fatalf("debía haber un solo aviso de caído: %d", len(mio))
		}
		return mio[0]
	}
	x.reloj = x.reloj.Truncate(time.Hour).Add(10 * time.Minute)
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: "no_disponible"})
	primero := uno()
	if primero["datos"].(map[string]any)["veces"] != float64(1) || primero["leido"] != false {
		t.Fatalf("primer aviso: %v", primero)
	}
	// El admin lo lee; el servidor vuelve y, ocho minutos después, se cae otra vez.
	x.esperar(x.api(admin, http.MethodPost, fmt.Sprintf("/api/v1/yo/avisos/%v/leer", primero["id"]), nil), http.StatusOK)
	if uno()["leido"] != true {
		t.Fatal("el aviso debía quedar leído")
	}
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: "listo"})
	x.reloj = x.reloj.Add(8 * time.Minute)
	segundo := uuid.Must(uuid.NewV7())
	r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{EventoID: segundo, MotorID: motor, A: "no_disponible"})
	if r.Avisos != 0 || r.Reactivados < 1 || r.CorreosPendientes != 0 {
		t.Fatalf("segunda caída en la hora: %+v", r)
	}
	a := uno()
	if a["id"] != primero["id"] || a["leido"] != false || a["leidoEn"] != nil || a["datos"].(map[string]any)["veces"] != float64(2) ||
		a["creadoEn"] != x.reloj.UTC().Format(time.RFC3339) {
		t.Fatalf("el aviso debía reactivarse (no leído, hora nueva, 2 veces): %v", a)
	}
	if n := x.esperar(x.api(admin, http.MethodGet, "/api/v1/yo/avisos/resumen", nil), http.StatusOK).cuerpo["noLeidos"]; n.(float64) < 1 {
		t.Fatalf("la campana debía volver a marcar: %v", n)
	}
	// El mismo evento otra vez (el relay reintenta) no suma.
	if r := x.avisarIA(notificaciones.EntradaAvisarServidorIA{EventoID: segundo, MotorID: motor, A: "no_disponible"}); r.Reactivados != 0 || uno()["datos"].(map[string]any)["veces"] != float64(2) {
		t.Fatalf("un evento repetido no debía contar: %+v", r)
	}
	// Un solo correo.
	x.mandarCorreos(false)
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE usuario_id = $1 AND tipo = 'servidor_ia_caido' AND (datos->>'motor_id')::bigint = $2`, admin.id, motor); n != 1 {
		t.Fatalf("correos de las dos caídas: %d", n)
	}
}

// F6: un admin de la instalación con varias empresas que todavía no eligió ninguna ve en la campana
// los avisos de la instalación (el servidor de IA) en vez de un 422; los de una empresa, no.
func TestAvisosSinEmpresaElegida(t *testing.T) {
	x := nuevoEntornoAvisos(t, false)
	admin := x.persona([]int64{1, 2}, "admin")
	const motor = 987658
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, A: "no_disponible"})
	x.avisar(emp(1), 80, "2043-05") // un aviso de la empresa 1 (tope del asistente)
	sin := admin
	sin.empresa = 0
	l := x.esperar(x.api(sin, http.MethodGet, "/api/v1/yo/avisos?limit=50", nil), http.StatusOK)
	vistos := map[string]int{}
	for _, a := range l.cuerpo["data"].([]any) {
		m := a.(map[string]any)
		vistos[m["tipo"].(string)]++
		if m["empresaId"] != nil {
			t.Fatalf("sin empresa elegida solo van los avisos de la instalación: %v", m)
		}
	}
	if vistos["servidor_ia_caido"] < 1 || vistos["asistente_tope"] != 0 {
		t.Fatalf("avisos sin empresa elegida: %v", vistos)
	}
	res := x.esperar(x.api(sin, http.MethodGet, "/api/v1/yo/avisos/resumen", nil), http.StatusOK)
	if res.cuerpo["noLeidos"] != float64(vistos["servidor_ia_caido"]) {
		t.Fatalf("resumen sin empresa elegida: %v (avisos %v)", res.cuerpo, vistos)
	}
	// Con una empresa elegida ve los dos, como siempre.
	if len(x.avisosDe(admin, "asistente_tope")) != 1 || len(x.avisosDe(admin, "servidor_ia_caido")) < 1 {
		t.Fatal("con la empresa 1 activa debía ver el del tope y el del servidor")
	}
	// Leer y leer todos también funcionan sin empresa (solo sobre los de la instalación).
	x.esperar(x.api(sin, http.MethodPost, "/api/v1/yo/avisos/leer-todos", nil), http.StatusOK)
	if n := x.esperar(x.api(sin, http.MethodGet, "/api/v1/yo/avisos/resumen", nil), http.StatusOK).cuerpo["noLeidos"]; n != float64(0) {
		t.Fatalf("tras leer todos sin empresa: %v", n)
	}
	if a := x.avisosDe(admin, "asistente_tope"); len(a) != 1 || a[0]["leido"] != false {
		t.Fatalf("leer todos sin empresa no debía tocar el aviso de la empresa 1: %v", a)
	}
}
