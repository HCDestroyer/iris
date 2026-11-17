package notificaciones_test

import (
	"net/http"
	"strings"
	"testing"

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
	// Otro episodio del mismo tipo (otro evento): otro aviso.
	x.avisarIA(notificaciones.EntradaAvisarServidorIA{MotorID: motor, Tipo: "vram", Valor: &valor, Umbral: &umbral})
	if um := x.avisosDe(admin, "servidor_ia_umbral"); len(um) != 2 {
		t.Fatalf("un episodio nuevo debía avisar otra vez: %d", len(um))
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'notificaciones.avisos_creados' AND payload->>'tipo' = 'servidor_ia_umbral'
		AND payload->'usuarios' @> to_jsonb($1::bigint) AND payload->>'empresa_id' IS NULL`, admin.id); n != 2 {
		t.Fatalf("notificaciones.avisos_creados: %d", n)
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
