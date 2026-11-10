package notificaciones_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Avisos del panel y correo del tope del asistente (contratos/conversaciones.md §16.9, entrega 2.1).

// correoFalso guarda lo que «manda» el SMTP del sistema.
type correoFalso struct {
	mu       sync.Mutex
	enviados []correoEnviado
	fallar   bool
}

type correoEnviado struct {
	para, asunto, texto string
}

func (c *correoFalso) Enviar(_ context.Context, para mail.Address, asunto, texto, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fallar {
		return errors.New("smtp caído")
	}
	c.enviados = append(c.enviados, correoEnviado{para: para.Address, asunto: asunto, texto: texto})
	return nil
}

func (c *correoFalso) Dominio() string { return "iris.prueba" }

func (c *correoFalso) para(correo string) []correoEnviado {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []correoEnviado
	for _, e := range c.enviados {
		if e.para == correo {
			out = append(out, e)
		}
	}
	return out
}

type entornoAvisos struct {
	*entorno
	correo  *correoFalso
	correos []int64
}

// nuevoEntornoAvisos: el módulo con un correo del sistema falso (nil = sin correo del sistema).
func nuevoEntornoAvisos(t *testing.T, conCorreo bool) *entornoAvisos {
	t.Helper()
	base := nuevoEntorno(t, false)
	x := &entornoAvisos{entorno: base}
	cfg := notificaciones.Config{BaseURL: "https://iris.prueba/", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Encolar: func(_ context.Context, _ pgx.Tx, a river.JobArgs) error {
			x.mu.Lock()
			defer x.mu.Unlock()
			x.correos = append(x.correos, a.(notificaciones.ArgsEnviarCorreo).CorreoID)
			return nil
		}}
	if conCorreo {
		x.correo = &correoFalso{}
		cfg.Correo = x.correo
	}
	x.notif = notificaciones.Nuevo(cfg)
	x.h, _ = servidor.Nuevo(servidor.Deps{BD: base.bd, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "prueba", Identidad: ident,
		Modulos: []func(huma.API){x.notif.Registrar}})
	return x
}

// configurador: una persona de esas empresas con un rol propio que solo da
// mensajeria.asistente_configurar (sin empresas.todas).
func (x *entornoAvisos) configurador(empresas []int64) persona {
	x.t.Helper()
	p := x.persona(empresas)
	codigo := "asistente_" + strings.ReplaceAll(pruebas.Unico(), "-", "_")
	var rol int64
	if err := x.bd.QueryRow(x.ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, $1) RETURNING id`, codigo).Scan(&rol); err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, 'mensajeria.asistente_configurar')`, rol); err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id) VALUES ($1, $2)`, p.id, rol); err != nil {
		x.t.Fatal(err)
	}
	return p
}

func (x *entornoAvisos) avisar(empresa *int64, porcentaje int, mes string) notificaciones.ResultadoAvisarTope {
	x.t.Helper()
	r, err := x.notif.AvisarTope.Ejecutar(x.ctx, notificaciones.EntradaAvisarTope{EventoID: uuid.Must(uuid.NewV7()), EmpresaID: empresa,
		Porcentaje: porcentaje, Mes: mes, Tokens: 1_600_000, Tope: 2_000_000})
	if err != nil {
		x.t.Fatal(err)
	}
	return r
}

func (x *entornoAvisos) mandarCorreos(ultimo bool) {
	x.t.Helper()
	x.mu.Lock()
	ids := x.correos
	x.correos = nil
	x.mu.Unlock()
	for _, id := range ids {
		if err := x.notif.EnviarCorreo(x.ctx, id, ultimo); err != nil && ultimo {
			x.t.Fatalf("correo %d: %v", id, err)
		}
	}
}

func TestAvisoTopeCampanaYCorreo(t *testing.T) {
	x := nuevoEntornoAvisos(t, true)
	mes := "2031-01"
	en1 := x.configurador([]int64{1})
	en2 := x.configurador([]int64{2})
	admin := x.persona([]int64{1}, "admin")
	sup := x.persona([]int64{1}, "supervisor")

	r := x.avisar(emp(1), 80, mes)
	if r.Avisos < 2 || r.CorreosPendientes != r.Avisos {
		t.Fatalf("avisos: %+v", r)
	}
	titulo80 := "El asistente de IA de Empresa 1 usó el 80 % de su tope del mes"
	for _, p := range []persona{en1, admin} {
		l := x.esperar(x.api(p, http.MethodGet, "/api/v1/yo/avisos", nil), http.StatusOK)
		d := l.cuerpo["data"].([]any)
		if len(d) != 1 {
			t.Fatalf("avisos de %s: %s", p.nombre, l.crudo)
		}
		a := d[0].(map[string]any)
		if a["titulo"] != titulo80 || a["enlace"] != "/administracion/asistente" || a["nivel"] != "alerta" || a["leido"] != false ||
			a["datos"].(map[string]any)["porcentaje"] != float64(80) {
			t.Fatalf("aviso: %v", a)
		}
	}
	// Ni el de otra empresa ni quien no tiene el permiso.
	for _, p := range []persona{en2, sup} {
		if d := x.esperar(x.api(p, http.MethodGet, "/api/v1/yo/avisos", nil), http.StatusOK).cuerpo["data"].([]any); len(d) != 0 {
			t.Fatalf("%s no debía tener avisos: %v", p.nombre, d)
		}
	}
	// Correo: mismo texto, el uso y el enlace al panel.
	x.mandarCorreos(false)
	c := x.correo.para(en1.correo)
	if len(c) != 1 || c[0].asunto != titulo80 || !strings.Contains(c[0].texto, "1,600,000 de 2,000,000 tokens") ||
		!strings.Contains(c[0].texto, "https://iris.prueba/administracion/asistente") {
		t.Fatalf("correo: %+v", c)
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE usuario_id = $1 AND estado = 'enviado' AND enviado_en IS NOT NULL`, en1.id); n != 1 {
		t.Fatalf("correo enviado: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'notificaciones.correo_enviado' AND (payload->>'usuario_id')::bigint = $1`, en1.id); n != 1 {
		t.Fatalf("notificaciones.correo_enviado: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'notificaciones.avisos_creados' AND payload->>'clave' = $1
		AND payload->'usuarios' @> to_jsonb($2::bigint)`, "asistente_tope:1:"+mes+":80", en1.id); n != 1 {
		t.Fatalf("notificaciones.avisos_creados: %d", n)
	}

	// El mismo umbral otra vez (evento repetido o recálculo del tope): nada nuevo.
	if r := x.avisar(emp(1), 80, mes); r.Avisos != 0 {
		t.Fatalf("repetido: %+v", r)
	}
	x.mandarCorreos(false)
	if c := x.correo.para(en1.correo); len(c) != 1 {
		t.Fatalf("correos repetidos: %d", len(c))
	}

	// 100 %: el segundo aviso, con su texto.
	x.avisar(emp(1), 100, mes)
	x.mandarCorreos(false)
	resumen := x.esperar(x.api(en1, http.MethodGet, "/api/v1/yo/avisos/resumen", nil), http.StatusOK)
	if resumen.cuerpo["noLeidos"] != float64(2) {
		t.Fatalf("resumen: %s", resumen.crudo)
	}
	d := x.esperar(x.api(en1, http.MethodGet, "/api/v1/yo/avisos?limit=1", nil), http.StatusOK)
	primero := d.cuerpo["data"].([]any)[0].(map[string]any)
	if primero["titulo"] != "El asistente de IA de Empresa 1 se pausó: llegó a su tope del mes" || primero["nivel"] != "peligro" || d.cuerpo["nextCursor"] == nil {
		t.Fatalf("aviso del 100 %%: %s", d.crudo)
	}
	if c := x.correo.para(en1.correo); len(c) != 2 || c[1].asunto != primero["titulo"] {
		t.Fatalf("correo del 100 %%: %+v", c)
	}
	// Leer: uno y luego todos.
	id := int64(primero["id"].(float64))
	leido := x.esperar(x.api(en1, http.MethodPost, fmt.Sprintf("/api/v1/yo/avisos/%d/leer", id), nil), http.StatusOK)
	if leido.cuerpo["leido"] != true || leido.cuerpo["leidoEn"] == nil {
		t.Fatalf("leer: %s", leido.crudo)
	}
	x.esperar(x.api(admin, http.MethodPost, fmt.Sprintf("/api/v1/yo/avisos/%d/leer", id), nil), http.StatusNotFound)
	if r := x.esperar(x.api(en1, http.MethodPost, "/api/v1/yo/avisos/leer-todos", nil), http.StatusOK); r.cuerpo["leidos"] != float64(1) {
		t.Fatalf("leer todos: %s", r.crudo)
	}
	if r := x.esperar(x.api(en1, http.MethodGet, "/api/v1/yo/avisos/resumen", nil), http.StatusOK); r.cuerpo["noLeidos"] != float64(0) {
		t.Fatalf("resumen tras leer: %s", r.crudo)
	}
	if d := x.esperar(x.api(en1, http.MethodGet, "/api/v1/yo/avisos?soloNoLeidos=true", nil), http.StatusOK).cuerpo["data"].([]any); len(d) != 0 {
		t.Fatalf("no leídos: %v", d)
	}
	// La campana es de la empresa activa: en la 2 no ve los de la 1.
	admin.empresa = 2
	if d := x.esperar(x.api(admin, http.MethodGet, "/api/v1/yo/avisos", nil), http.StatusOK).cuerpo["data"].([]any); len(d) != 0 {
		t.Fatalf("avisos de otra empresa: %v", d)
	}
}

// Sin IRIS_SMTP_HOST / IRIS_SMTP_REMITENTE: la campana sale igual y el correo queda omitido, sin error.
func TestAvisoTopeSinCorreoDelSistema(t *testing.T) {
	x := nuevoEntornoAvisos(t, false)
	p := x.configurador([]int64{1})
	r := x.avisar(emp(1), 80, "2031-02")
	if r.Avisos < 1 || r.CorreosPendientes != 0 || r.CorreosOmitidos != r.Avisos || len(x.correos) != 0 {
		t.Fatalf("sin correo: %+v %v", r, x.correos)
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE usuario_id = $1 AND estado = 'omitido' AND motivo = 'sin_correo_sistema'`, p.id); n != 1 {
		t.Fatalf("correo omitido registrado: %d", n)
	}
	if d := x.esperar(x.api(p, http.MethodGet, "/api/v1/yo/avisos", nil), http.StatusOK).cuerpo["data"].([]any); len(d) != 1 {
		t.Fatalf("campana: %v", d)
	}
	if ev := x.ultimoEvento("notificaciones.avisos_creados"); ev["correo"] != "omitido" {
		t.Fatalf("evento: %v", ev)
	}
}

// El SMTP falla: se reintenta (error) y en el último intento queda fallido con su evento.
func TestAvisoTopeCorreoFallido(t *testing.T) {
	x := nuevoEntornoAvisos(t, true)
	p := x.configurador([]int64{1})
	x.avisar(emp(1), 80, "2031-03")
	x.correo.fallar = true
	x.mu.Lock()
	ids := append([]int64(nil), x.correos...)
	x.mu.Unlock()
	var mio int64
	for _, id := range ids {
		if x.contar(`SELECT count(*) FROM notificaciones.correos WHERE id = $1 AND usuario_id = $2`, id, p.id) == 1 {
			mio = id
		}
	}
	if mio == 0 {
		t.Fatal("sin correo encolado")
	}
	if err := x.notif.EnviarCorreo(x.ctx, mio, false); err == nil {
		t.Fatal("un intento que no es el último devuelve error (River reintenta)")
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE id = $1 AND estado = 'pendiente' AND intentos = 1`, mio); n != 1 {
		t.Fatalf("pendiente tras el primer intento: %d", n)
	}
	if err := x.notif.EnviarCorreo(x.ctx, mio, true); err != nil {
		t.Fatal(err)
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE id = $1 AND estado = 'fallido' AND motivo IS NOT NULL`, mio); n != 1 {
		t.Fatalf("fallido: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'notificaciones.correo_fallido' AND (payload->>'correo_id')::bigint = $1`, mio); n != 1 {
		t.Fatalf("notificaciones.correo_fallido: %d", n)
	}
	// Repetir el job no cambia nada.
	if err := x.notif.EnviarCorreo(x.ctx, mio, true); err != nil {
		t.Fatal(err)
	}
}

// El suscriptor (relay del outbox): asistente.tope_alcanzado → avisos, una sola vez aunque llegue dos.
func TestAvisoTopeDesdeElOutbox(t *testing.T) {
	x := nuevoEntornoAvisos(t, false)
	p := x.configurador([]int64{1})
	x.notif.SuscribirAvisos(x.relay.Bus, x.bd)
	for range 2 {
		if err := nucleo.EnTx(x.ctx, func(ctx context.Context, tx pgx.Tx) error {
			_, err := integracion.Insertar(ctx, tx, "asistente.tope_alcanzado", emp(1), "", map[string]any{"empresa_id": 1, "porcentaje": 100,
				"mes": "2031-04", "tokens": 2000000, "tope": 2000000})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := x.relay.Ejecutar(x.ctx); err != nil {
		t.Fatal(err)
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND clave = 'asistente_tope:1:2031-04:100'`, p.id); n != 1 {
		t.Fatalf("avisos desde el outbox: %d", n)
	}
}
