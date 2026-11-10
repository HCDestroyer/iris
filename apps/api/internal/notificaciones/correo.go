package notificaciones

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Correo saliente del sistema (IRIS_SMTP_*): avisos a usuarios de Iris, no respuestas a clientes
// (esas salen por el SMTP de cada buzón de soporte). Sin IRIS_SMTP_HOST ni IRIS_SMTP_REMITENTE no hay
// correo del sistema: los correos quedan «omitido» con motivo sin_correo_sistema, sin error.

// EmisorCorreo manda un correo de texto plano.
type EmisorCorreo interface {
	Enviar(ctx context.Context, para mail.Address, asunto, texto, messageID string) error
	// Dominio para el Message-ID (el del remitente).
	Dominio() string
}

// SMTPSistema manda por el SMTP del sistema con TLS obligatorio (implícito o STARTTLS) y la
// protección de red de los buzones (IRIS_CORREO_REDES_PERMITIDAS).
type SMTPSistema struct {
	Servidor  correo.Servidor
	Remitente mail.Address
	Red       correo.Opciones
}

// Enviar implementa EmisorCorreo.
func (s *SMTPSistema) Enviar(ctx context.Context, para mail.Address, asunto, texto, messageID string) error {
	return correo.Enviar(ctx, s.Red, s.Servidor, correo.Saliente{De: s.Remitente, Para: para, Asunto: asunto, Texto: texto,
		MessageID: messageID, Fecha: time.Now()})
}

// Dominio implementa EmisorCorreo.
func (s *SMTPSistema) Dominio() string {
	if _, d, ok := strings.Cut(s.Remitente.Address, "@"); ok && d != "" {
		return d
	}
	return "iris"
}

// CorreoDesdeEntorno lee IRIS_SMTP_HOST, IRIS_SMTP_PORT, IRIS_SMTP_USUARIO, IRIS_SMTP_CONTRASENA,
// IRIS_SMTP_TLS (starttls por defecto, o tls) e IRIS_SMTP_REMITENTE. Sin host o sin remitente: nil
// (sin correo del sistema), sin error. Con un valor que no sirve: nil y el motivo (para el log).
func CorreoDesdeEntorno(red correo.Opciones) (EmisorCorreo, error) {
	host := strings.TrimSpace(os.Getenv("IRIS_SMTP_HOST"))
	remitente := strings.TrimSpace(os.Getenv("IRIS_SMTP_REMITENTE"))
	if host == "" || remitente == "" {
		return nil, nil
	}
	de, err := mail.ParseAddress(remitente)
	if err != nil {
		return nil, fmt.Errorf("IRIS_SMTP_REMITENTE no es una dirección de correo: %w", err)
	}
	puerto := 587
	if v := strings.TrimSpace(os.Getenv("IRIS_SMTP_PORT")); v != "" {
		if puerto, err = strconv.Atoi(v); err != nil || puerto < 1 || puerto > 65535 {
			return nil, fmt.Errorf("IRIS_SMTP_PORT inválido: %q", v)
		}
	}
	tls := strings.TrimSpace(strings.ToLower(os.Getenv("IRIS_SMTP_TLS")))
	if tls == "" {
		tls = correo.TLSStartTLS
	}
	if tls != correo.TLSStartTLS && tls != correo.TLSImplicito {
		return nil, fmt.Errorf("IRIS_SMTP_TLS inválido: %q (starttls o tls)", tls)
	}
	return &SMTPSistema{Remitente: *de, Red: red, Servidor: correo.Servidor{Host: host, Puerto: puerto, TLS: tls,
		Usuario: os.Getenv("IRIS_SMTP_USUARIO"), Contrasena: os.Getenv("IRIS_SMTP_CONTRASENA")}}, nil
}

// ArgsEnviarCorreo manda un correo del sistema. Único por correo.
type ArgsEnviarCorreo struct {
	CorreoID int64 `json:"correo_id"`
}

// Kind implementa river.JobArgs.
func (ArgsEnviarCorreo) Kind() string { return "notificaciones.enviar_correo" }

// InsertOpts: hasta 6 intentos, único por argumentos.
func (ArgsEnviarCorreo) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: MaxIntentosCorreo, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// MaxIntentosCorreo de un correo del sistema.
const MaxIntentosCorreo = 6

// correoListo es el correo armado (fuera de la transacción).
type correoListo struct {
	ID        int64
	Para      mail.Address
	Asunto    string
	Texto     string `json:"-"`
	MessageID string
}

// ResultadoCorreo es lo que respondió el SMTP.
type ResultadoCorreo struct {
	CorreoID int64  `json:"correo_id"`
	Error    string `json:"error,omitempty"`
	Ultimo   bool   `json:"ultimo"`
}

func (m *Modulo) nuevoPrepararCorreo() *auditoria.Caso[int64, *correoListo] {
	return auditoria.Nuevo(auditoria.Caso[int64, *correoListo]{
		Accion: "notificaciones.preparar_correo", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(*correoListo) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (*correoListo, error) {
			if err := soloSistema(ctx); err != nil {
				return nil, err
			}
			c, err := notificacionesbd.New(t).CorreoParaEnviar(ctx, id)
			if esSinFilas(err) || (err == nil && c.Estado != "pendiente") {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			para, err := mail.ParseAddress(c.Para)
			if err != nil {
				return &correoListo{ID: c.ID}, nil // lo cierra como fallido
			}
			dominio := "iris"
			if m.cfg.Correo != nil {
				dominio = m.cfg.Correo.Dominio()
			}
			return &correoListo{ID: c.ID, Para: *para, Asunto: c.Asunto, Texto: m.textoCorreo(c),
				MessageID: fmt.Sprintf("iris.aviso.%d.%s@%s", c.ID, uuid.NewString()[:8], dominio)}, nil
		},
	})
}

// textoCorreo arma el cuerpo (texto plano) del correo según su tipo.
func (m *Modulo) textoCorreo(c notificacionesbd.NotificacionesCorreo) string {
	var d struct {
		Porcentaje int    `json:"porcentaje"`
		Mes        string `json:"mes"`
		Tokens     int64  `json:"tokens"`
		Tope       int64  `json:"tope"`
		Empresa    string `json:"empresa"`
	}
	_ = json.Unmarshal(c.Datos, &d)
	var b strings.Builder
	b.WriteString(c.Asunto + ".\n\n")
	fmt.Fprintf(&b, "Uso de %s: %s de %s tokens.\n", d.Mes, miles(d.Tokens), miles(d.Tope))
	if d.Porcentaje >= 100 {
		b.WriteString("Las sugerencias y los resúmenes quedan en pausa hasta el mes siguiente o hasta que suban el tope.\n")
	} else {
		b.WriteString("Al llegar al 100 % el asistente se pausa hasta el mes siguiente o hasta que suban el tope.\n")
	}
	fmt.Fprintf(&b, "\nRevisa el uso y el tope en %s%s\n", strings.TrimRight(m.cfg.BaseURL, "/"), EnlaceAsistente)
	fmt.Fprintf(&b, "\nEste aviso lo envía Iris a quienes administran el asistente de IA de %s.\n", d.Empresa)
	return b.String()
}

// miles: 1500000 → «1,500,000».
func miles(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func (m *Modulo) nuevoCerrarCorreo() *auditoria.Caso[ResultadoCorreo, bool] {
	return auditoria.Nuevo(auditoria.Caso[ResultadoCorreo, bool]{
		Accion: "notificaciones.enviar_correo", Acceso: auditoria.Autenticado, SinEmpresa: true,
		// Solo el resultado final (enviado o fallido) deja fila; un reintento no.
		AuditarExito: func(final bool) bool { return final },
		Logica: func(ctx context.Context, t *auditoria.Tx, in ResultadoCorreo) (bool, error) {
			if err := soloSistema(ctx); err != nil {
				return false, err
			}
			q := notificacionesbd.New(t)
			c, err := q.CorreoParaEnviar(ctx, in.CorreoID)
			if esSinFilas(err) || (err == nil && c.Estado != "pendiente") {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			t.FijarEntidad("correo_sistema", c.ID)
			if c.EmpresaID != nil {
				t.FijarEmpresa(*c.EmpresaID)
			} else {
				t.FijarEmpresa(0)
			}
			if in.Error != "" && !in.Ultimo {
				return false, q.ReintentoCorreo(ctx, notificacionesbd.ReintentoCorreoParams{ID: c.ID, Motivo: ptr(recortarMotivo(in.Error))})
			}
			estado, evento := "enviado", "notificaciones.correo_enviado"
			var motivo *string
			if in.Error != "" {
				estado, evento, motivo = "fallido", "notificaciones.correo_fallido", ptr(recortarMotivo(in.Error))
			}
			if _, err := q.CerrarCorreo(ctx, notificacionesbd.CerrarCorreoParams{ID: c.ID, Estado: estado, Motivo: motivo, Ahora: m.ahora()}); err != nil {
				return false, err
			}
			t.Anotar(map[string]any{"estado": estado, "tipo": c.Tipo, "usuario_id": c.UsuarioID})
			return true, t.Emitir(evento, map[string]any{"correo_id": c.ID, "usuario_id": c.UsuarioID, "tipo": c.Tipo, "clave": c.Clave, "motivo": motivo})
		},
	})
}

func recortarMotivo(s string) string {
	if r := []rune(s); len(r) > 300 {
		return string(r[:299]) + "…"
	}
	return s
}

// EnviarCorreo prepara (transacción), manda por SMTP (fuera) y cierra (otra transacción). Un error del
// SMTP antes del último intento devuelve error (River reintenta).
func (m *Modulo) EnviarCorreo(ctx context.Context, id int64, ultimo bool) error {
	c, err := m.prepararCorreo.Ejecutar(ctx, id)
	if err != nil || c == nil {
		return err
	}
	var errEnvio error
	switch {
	case c.Para.Address == "":
		errEnvio, ultimo = fmt.Errorf("el correo del usuario no es válido"), true
	case m.cfg.Correo == nil:
		errEnvio, ultimo = fmt.Errorf("no hay correo saliente del sistema configurado"), true
	default:
		errEnvio = m.cfg.Correo.Enviar(ctx, c.Para, c.Asunto, c.Texto, c.MessageID)
	}
	in := ResultadoCorreo{CorreoID: id, Ultimo: ultimo}
	if errEnvio != nil {
		in.Error = resumenErrorCorreo(errEnvio)
	}
	if _, err := m.cerrarCorreo.Ejecutar(context.WithoutCancel(ctx), in); err != nil {
		return err
	}
	if errEnvio != nil && !ultimo {
		return errEnvio
	}
	return nil
}

// resumenErrorCorreo: el resumen del error de SMTP (sin credenciales ni la respuesta cruda).
func resumenErrorCorreo(err error) string {
	if e := correo.Resumir(err); e != nil {
		return e.Codigo + ": " + e.Detalle
	}
	return err.Error()
}

// TrabajadorCorreo es el worker de River de un correo del sistema.
type TrabajadorCorreo struct {
	river.WorkerDefaults[ArgsEnviarCorreo]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorCorreo) Work(ctx context.Context, job *river.Job[ArgsEnviarCorreo]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.BD)
	return w.Modulo.EnviarCorreo(ctx, job.Args.CorreoID, job.Attempt >= job.MaxAttempts)
}

// NextRetry: la misma espera exponencial del push.
func (w *TrabajadorCorreo) NextRetry(job *river.Job[ArgsEnviarCorreo]) time.Time {
	return time.Now().Add(EsperaReintento(job.Attempt))
}

// Timeout del job.
func (w *TrabajadorCorreo) Timeout(*river.Job[ArgsEnviarCorreo]) time.Duration {
	return 2 * time.Minute
}

func soloSistema(ctx context.Context) error {
	if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
		return nucleo.Prohibido("Solo el sistema avisa y manda correos del sistema")
	}
	return nil
}
