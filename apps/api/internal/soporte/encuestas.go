package soporte

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Encuesta de satisfacción (Fase 5). Se genera una por ticket al pasar a resuelto (salvo el
// maestro de un incidente de red, que no tiene cliente) y una por orden de instalación o visita
// cerrada con éxito (campo llama a GenerarEncuestaOrden en la transacción del cierre).
//
// Acceso por enlace <IRIS_BASE_URL>/encuesta#<token>, de un solo uso y que vence a los 7 días.
// En la base solo queda el SHA-256 del token y una semilla aleatoria: el token es
// HMAC-SHA256(clave, semilla) con una clave derivada de IRIS_CLAVE_CIFRADO, así soporte vuelve
// a ver el enlace (GET /encuestas) y el worker arma el correo sin que el token esté en la base.
// Sin clave el token es aleatorio y no se puede volver a mostrar (canal enlace, enlace null).
//
// Envío: si el ticket (el de la orden, si tiene) tiene correo de contacto, hay un buzón activo
// con SMTP de su empresa, clave e IRIS_BASE_URL, se encola el correo con el enlace (worker de
// correo: correos_salientes.encuesta_id); si no, el enlace queda en GET /encuestas para que
// soporte lo comparta. Sin Argos.
//
// Los accesos públicos siguen el patrón del canje del portal (nucleo/identidad/portal.go): sin
// límite por IP (detrás del proxy del panel todos comparten IP; el token de 256 bits no se
// adivina), los rechazos por token inválido no se auditan (se cuentan y van al log con
// muestreo) y un tope global de fallidos por ventana responde 429 demasiados_intentos.

// Valores por defecto de las encuestas.
const (
	VigenciaEncuesta     = 7 * 24 * time.Hour
	TopeFallidosEncuesta = 1000
	VentanaTopeEncuesta  = time.Minute
)

// ConfigEncuestas del módulo.
type ConfigEncuestas struct {
	// Claves derivadas de IRIS_CLAVE_CIFRADO (y la anterior durante una rotación), de
	// ClavesEncuestas. La primera firma los tokens nuevos; todas sirven para volver a mostrar un
	// enlace. Sin claves, los enlaces no se pueden volver a mostrar ni enviar por correo.
	Claves [][]byte
	// BaseURL: IRIS_BASE_URL (el enlace es <BaseURL>/encuesta#<token>). Vacía: el enlace es
	// relativo y no se envía por correo.
	BaseURL string
	// Vigencia del enlace (7 días por defecto).
	Vigencia time.Duration
	// TopeFallidos por VentanaTope (1000 por minuto por defecto): pasado, los accesos con un
	// token inválido responden 429.
	TopeFallidos int64
	VentanaTope  time.Duration
}

func (c ConfigEncuestas) vigencia() time.Duration {
	if c.Vigencia <= 0 {
		return VigenciaEncuesta
	}
	return c.Vigencia
}

// ClavesEncuestas deriva las claves de las encuestas de IRIS_CLAVE_CIFRADO y de la anterior
// (base64 de 32 bytes). Vacía = sin claves.
func ClavesEncuestas(actual, anterior string) ([][]byte, error) {
	var claves [][]byte
	for _, c := range []string{actual, anterior} {
		if c == "" {
			continue
		}
		crudo, err := base64.StdEncoding.DecodeString(c)
		if err != nil {
			crudo, err = base64.RawURLEncoding.DecodeString(c)
		}
		if err != nil || len(crudo) != 32 {
			return nil, errors.New("encuestas: IRIS_CLAVE_CIFRADO debe ser base64 de 32 bytes")
		}
		h := hmac.New(sha256.New, crudo)
		h.Write([]byte("iris/encuestas/v1"))
		claves = append(claves, h.Sum(nil))
	}
	return claves, nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func tokenDe(clave, semilla []byte) string {
	h := hmac.New(sha256.New, clave)
	h.Write(semilla)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// nuevoToken: con clave, HMAC de una semilla aleatoria; sin clave, 32 bytes aleatorios.
func (m *Modulo) nuevoToken() (token string, hash, semilla []byte, err error) {
	azar := make([]byte, 32)
	if _, err := rand.Read(azar); err != nil {
		return "", nil, nil, err
	}
	if claves := m.cfg.Encuestas.Claves; len(claves) > 0 {
		token = tokenDe(claves[0], azar)
		return token, hashToken(token), azar, nil
	}
	token = base64.RawURLEncoding.EncodeToString(azar)
	return token, hashToken(token), nil, nil
}

// tokenGuardado vuelve a derivar el token de una encuesta (con la clave que corresponda a su
// hash). false: sin semilla o sin una clave que coincida (rotada o ausente).
func (m *Modulo) tokenGuardado(e soportebd.SoporteEncuesta) (string, bool) {
	if e.Semilla == nil {
		return "", false
	}
	for _, c := range m.cfg.Encuestas.Claves {
		token := tokenDe(c, e.Semilla)
		if subtle.ConstantTimeCompare(hashToken(token), e.TokenHash) == 1 {
			return token, true
		}
	}
	return "", false
}

func (m *Modulo) urlEncuesta(token string) string {
	return strings.TrimRight(m.cfg.Encuestas.BaseURL, "/") + "/encuesta#" + token
}

// estadoEncuesta: pendiente, respondida o vencida (pendiente con expira_en pasado).
func estadoEncuesta(e soportebd.SoporteEncuesta, ahora time.Time) string {
	if e.Estado == "pendiente" && !ahora.Before(e.ExpiraEn) {
		return "vencida"
	}
	return e.Estado
}

// Generar ---------------------------------------------------------------------------------

// datosEncuesta: de qué es la encuesta y, si hay, el ticket con el correo de contacto.
type datosEncuesta struct {
	entidad    string
	ticketID   *int64
	ordenID    *int64
	numero     string
	empresa    *int64
	zona       *int64
	cuadrilla  *int64
	usuario    *int64
	categoria  *int64
	ticketMail *soportebd.SoporteTicket
}

// encuestaAuditada es lo que queda en auditoria.cambios (nunca el hash ni la semilla).
type encuestaAuditada struct {
	Entidad   string    `json:"entidad"`
	EntidadID int64     `json:"entidad_id"`
	Canal     string    `json:"canal"`
	ExpiraEn  time.Time `json:"expira_en"`
}

// generarEncuesta crea la encuesta (si la entidad no tenía) dentro de la transacción del caso
// que la origina: auditada en sus cambios y con encuesta.enviada en el outbox.
func (m *Modulo) generarEncuesta(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, d datosEncuesta) error {
	token, hash, semilla, err := m.nuevoToken()
	if err != nil {
		return err
	}
	_ = token // el token solo se entrega derivándolo (enlace o correo)
	ahora := m.ahora()
	canal, buzon, inReplyTo, destinatario, err := m.canalEncuesta(ctx, q, d)
	if err != nil {
		return err
	}
	e, err := q.CrearEncuesta(ctx, soportebd.CrearEncuestaParams{
		EmpresaID: d.empresa, Entidad: d.entidad, TicketID: d.ticketID, OrdenID: d.ordenID, Numero: d.numero,
		ZonaID: d.zona, CuadrillaID: d.cuadrilla, UsuarioID: d.usuario, CategoriaID: d.categoria,
		TokenHash: hash, Semilla: semilla, Canal: canal, ExpiraEn: ahora.Add(m.cfg.Encuestas.vigencia()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // ya tenía su encuesta (una por ticket y una por orden)
	}
	if err != nil {
		return err
	}
	entidadID := deref(e.TicketID)
	if e.OrdenID != nil {
		entidadID = *e.OrdenID
	}
	t.RegistrarCambios("encuesta", e.ID, nil, encuestaAuditada{Entidad: e.Entidad, EntidadID: entidadID, Canal: e.Canal, ExpiraEn: e.ExpiraEn})
	if canal == "correo" {
		_, dominio, _ := strings.Cut(buzon.Correo, "@")
		s, err := q.CrearCorreoEncuesta(ctx, soportebd.CrearCorreoEncuestaParams{
			BuzonID: buzon.ID, EmpresaID: d.ticketMail.EmpresaID, TicketID: d.ticketMail.ID, EncuestaID: &e.ID,
			MessageID: fmt.Sprintf("iris.encuesta.%s.%s@%s", strings.ToLower(d.numero), uuid.NewString(), dominio),
			InReplyTo: inReplyTo, Destinatario: destinatario,
			Asunto: correo.Recortar("["+d.ticketMail.Numero+"] ¿Cómo te atendimos?", 250),
		})
		if err != nil {
			return err
		}
		if m.cfg.Encolar != nil {
			if err := m.cfg.Encolar(ctx, t.Tx, ArgsEnviarCorreo{SalienteID: s.ID}); err != nil {
				return err
			}
		}
	}
	return t.Emitir("encuesta.enviada", map[string]any{
		"encuesta_id": e.ID, "entidad": e.Entidad, "entidad_id": entidadID, "canal": e.Canal,
	})
}

// canalEncuesta decide si sale por correo: contacto con correo, buzón activo con SMTP de la
// empresa del ticket, clave para derivar el enlace y URL pública.
func (m *Modulo) canalEncuesta(ctx context.Context, q *soportebd.Queries, d datosEncuesta) (string, soportebd.SoporteBuzon, *string, string, error) {
	var nada soportebd.SoporteBuzon
	tk := d.ticketMail
	if tk == nil || len(m.cfg.Encuestas.Claves) == 0 || m.cfg.Encuestas.BaseURL == "" {
		return "enlace", nada, nil, "", nil
	}
	contacto := normalizarCorreo(derefStr(tk.ContactoCorreo))
	if contacto == "" {
		return "enlace", nada, nil, "", nil
	}
	var preferido *int64
	var inReplyTo *string
	ult, err := q.UltimoCorreoEntranteDeTicket(ctx, soportebd.UltimoCorreoEntranteDeTicketParams{TicketID: &tk.ID, Contacto: contacto})
	switch {
	case err == nil:
		preferido, inReplyTo = &ult.BuzonID, &ult.MessageID
	case !errors.Is(err, pgx.ErrNoRows):
		return "", nada, nil, "", err
	}
	b, err := q.BuzonParaEncuesta(ctx, soportebd.BuzonParaEncuestaParams{EmpresaID: tk.EmpresaID, Preferido: preferido})
	if errors.Is(err, pgx.ErrNoRows) {
		return "enlace", nada, nil, "", nil
	}
	if err != nil {
		return "", nada, nil, "", err
	}
	if preferido == nil || b.ID != *preferido {
		inReplyTo = nil // otro buzón: no es el mismo hilo
	}
	return "correo", b, inReplyTo, contacto, nil
}

// generarEncuestaTicket: al resolver un ticket (no el maestro de un incidente).
func (m *Modulo) generarEncuestaTicket(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, f soportebd.SoporteTicket) error {
	if f.Maestro != nil {
		return nil
	}
	return m.generarEncuesta(ctx, t, q, datosEncuesta{
		entidad: "ticket", ticketID: &f.ID, numero: f.Numero, empresa: f.EmpresaID, zona: f.ZonaID,
		cuadrilla: f.CuadrillaID, usuario: f.AsignadoA, categoria: f.CategoriaID, ticketMail: &f,
	})
}

// EncuestaDeOrden son los datos de una orden cerrada con éxito (instalación o visita).
type EncuestaDeOrden struct {
	OrdenID     int64
	Numero      string
	EmpresaID   *int64
	TicketID    *int64
	ZonaID      *int64
	CuadrillaID *int64
	TecnicoID   *int64
}

// GenerarEncuestaOrden crea la encuesta de una orden dentro de la transacción del cierre (la
// llama campo). Si la orden viene de un ticket, el correo va a su contacto.
func (m *Modulo) GenerarEncuestaOrden(ctx context.Context, t *auditoria.Tx, o EncuestaDeOrden) error {
	q := soportebd.New(t)
	d := datosEncuesta{
		entidad: "orden", ordenID: &o.OrdenID, numero: o.Numero, empresa: o.EmpresaID, zona: o.ZonaID,
		cuadrilla: o.CuadrillaID, usuario: o.TecnicoID,
	}
	if o.TicketID != nil {
		tk, err := q.TicketPorID(ctx, *o.TicketID)
		if err != nil {
			return err
		}
		d.ticketMail = &tk
	}
	return m.generarEncuesta(ctx, t, q, d)
}

// textoCorreoEncuesta arma el cuerpo del correo de la encuesta. motivo no vacío = no se envía
// (sin enlace, respondida o vencida).
func (m *Modulo) textoCorreoEncuesta(ctx context.Context, q *soportebd.Queries, id int64, numeroTicket string) (texto, motivo string, err error) {
	e, err := q.EncuestaPorID(ctx, id)
	if err != nil {
		return "", "", err
	}
	if estado := estadoEncuesta(e, m.ahora()); estado != "pendiente" {
		return "", "encuesta " + estado + ": no se envía", nil
	}
	token, ok := m.tokenGuardado(e)
	if !ok || m.cfg.Encuestas.BaseURL == "" {
		return "", "enlace de la encuesta no disponible (clave de cifrado distinta o sin IRIS_BASE_URL)", nil
	}
	zona := zonaGuatemala()
	que := "el caso " + numeroTicket
	if e.Entidad == "orden" {
		que = "la visita " + e.Numero + " (caso " + numeroTicket + ")"
	}
	return fmt.Sprintf("Hola:\n\nGracias por contactarnos. ¿Cómo te atendimos en %s?\n\n"+
		"Califica la atención de 1 a 5 en este enlace (es personal, de un solo uso y vale hasta el %s):\n\n%s\n\n"+
		"Solo toma un minuto.", que, e.ExpiraEn.In(zona).Format("02/01/2006"), m.urlEncuesta(token)), "", nil
}

var zonaGT = sync.OnceValue(func() *time.Location {
	l, err := time.LoadLocation("America/Guatemala")
	if err != nil {
		return time.UTC
	}
	return l
})

func zonaGuatemala() *time.Location { return zonaGT() }

// Acceso público -----------------------------------------------------------------------------

var patronToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// EntradaEncuestaPublica: el token del enlace.
type EntradaEncuestaPublica struct {
	Token string `path:"token" json:"token" maxLength:"200" doc:"Lo que sigue a # en /encuesta#<token>"`
}

// EncuestaPublica es lo que ve el cliente: nada personal.
type EncuestaPublica struct {
	Entidad      string    `json:"entidad" enum:"ticket,orden"`
	Numero       string    `json:"numero" doc:"Número del ticket o de la orden"`
	Fecha        time.Time `json:"fecha" doc:"Cuándo se resolvió el ticket o se cerró la orden"`
	Empresa      *string   `json:"empresa" nullable:"true" doc:"Nombre de la empresa (null sin conector)"`
	Estado       string    `json:"estado" enum:"pendiente,respondida,vencida"`
	ExpiraEn     time.Time `json:"expiraEn"`
	Calificacion *int      `json:"calificacion,omitempty" minimum:"1" maximum:"5" doc:"Solo si ya se respondió"`
}

// EntradaResponderEncuesta es la entrada de encuestas.responder.
type EntradaResponderEncuesta struct {
	Token        string  `json:"token"`
	Calificacion int     `json:"calificacion"`
	Comentario   *string `json:"comentario" auditoria:"sensible"`
}

// CuerpoResponderEncuesta es el cuerpo de POST /encuestas/publica/{token}/responder.
type CuerpoResponderEncuesta struct {
	Calificacion int     `json:"calificacion" minimum:"1" maximum:"5" doc:"1 (muy mal) a 5 (muy bien)"`
	Comentario   *string `json:"comentario,omitempty" maxLength:"1000" doc:"Opcional"`
}

func encuestaNoEncontrada() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusNotFound, "encuesta_no_encontrada", "Encuesta no encontrada",
		"El enlace de la encuesta no existe o está incompleto")
}

func encuestaVencida() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusGone, "encuesta_vencida", "Encuesta vencida",
		"El enlace de la encuesta venció; ya no se puede responder")
}

func encuestaRespondida() *nucleo.Problema {
	return nucleo.Conflicto("encuesta_respondida", "La encuesta ya se respondió con otra calificación o comentario")
}

// topeFallidos cuenta accesos con un token inválido en una ventana fija (en memoria, por
// proceso): el freno global de abuso de los enlaces de encuesta.
type topeFallidos struct {
	mu      sync.Mutex
	fin     time.Time
	cuenta  int64
	avisado bool
}

// fallo cuenta uno y dice si la ventana superó el tope (y hasta cuándo).
func (v *topeFallidos) fallo(ahora time.Time, ventana time.Duration, tope int64) (time.Time, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !ahora.Before(v.fin) {
		v.fin, v.cuenta, v.avisado = ahora.Add(ventana), 0, false
	}
	v.cuenta++
	if v.cuenta > tope {
		if !v.avisado {
			v.avisado = true
			slog.Warn("soporte: tope global de enlaces de encuesta inválidos alcanzado", "tope", tope, "hasta", v.fin)
		}
		return v.fin, true
	}
	return time.Time{}, false
}

// rechazoToken: 404 o, pasado el tope global, 429.
func (m *Modulo) rechazoToken() error {
	tope, ventana := m.cfg.Encuestas.TopeFallidos, m.cfg.Encuestas.VentanaTope
	if tope <= 0 {
		tope = TopeFallidosEncuesta
	}
	if ventana <= 0 {
		ventana = VentanaTopeEncuesta
	}
	if hasta, excedido := m.topeEncuestas.fallo(time.Now(), ventana, tope); excedido {
		return nucleo.DemasiadosIntentos("demasiados_intentos",
			"Demasiados enlaces de encuesta inválidos; vuelve a intentar en un momento", time.Until(hasta))
	}
	return encuestaNoEncontrada()
}

// auditarRechazoEncuesta: los 404 y 429 de un token inválido no se auditan (cualquiera puede
// provocarlos sin límite); se cuentan y van al log con muestreo.
func (m *Modulo) auditarRechazoEncuesta(err error) bool {
	p, _ := nucleo.AProblema(err)
	if p.Codigo() != "encuesta_no_encontrada" && p.Status != http.StatusTooManyRequests {
		return true
	}
	if n := m.rechazosEncuesta.Add(1); n == 1 || n%100 == 0 {
		slog.Warn("soporte: accesos a encuestas rechazados sin auditar", "codigo", p.Codigo(), "total", n)
	}
	return false
}

// RechazosEncuestaSinAuditar devuelve cuántos accesos a encuestas se rechazaron sin auditar.
func (m *Modulo) RechazosEncuestaSinAuditar() int64 { return m.rechazosEncuesta.Load() }

func (m *Modulo) encuestaPublica(e soportebd.SoporteEncuesta, empresa *string) EncuestaPublica {
	p := EncuestaPublica{Entidad: e.Entidad, Numero: e.Numero, Fecha: e.CreatedAt, Empresa: empresa,
		Estado: estadoEncuesta(e, m.ahora()), ExpiraEn: e.ExpiraEn}
	if e.Calificacion != nil {
		p.Calificacion = ptr(int(*e.Calificacion))
	}
	return p
}

func (m *Modulo) nuevoVerEncuestaPublica() *auditoria.Caso[EntradaEncuestaPublica, EncuestaPublica] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEncuestaPublica, EncuestaPublica]{
		Accion: "encuestas.ver_publica", Acceso: auditoria.Publico,
		AuditarExito: soloLecturas[EncuestaPublica], AuditarError: m.auditarRechazoEncuesta,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEncuestaPublica) (EncuestaPublica, error) {
			if !patronToken.MatchString(in.Token) {
				return EncuestaPublica{}, m.rechazoToken()
			}
			f, err := soportebd.New(t).EncuestaPublica(ctx, hashToken(in.Token))
			if errors.Is(err, pgx.ErrNoRows) {
				return EncuestaPublica{}, m.rechazoToken()
			}
			if err != nil {
				return EncuestaPublica{}, err
			}
			return m.encuestaPublica(f.SoporteEncuesta, f.EmpresaNombre), nil
		},
	})
}

func mismoComentario(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (m *Modulo) nuevoResponderEncuesta() *auditoria.Caso[EntradaResponderEncuesta, EncuestaPublica] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResponderEncuesta, EncuestaPublica]{
		Accion: "encuestas.responder", Acceso: auditoria.Publico, AuditarError: m.auditarRechazoEncuesta,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResponderEncuesta) (EncuestaPublica, error) {
			if !patronToken.MatchString(in.Token) {
				return EncuestaPublica{}, m.rechazoToken()
			}
			if in.Calificacion < 1 || in.Calificacion > 5 {
				return EncuestaPublica{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "calificacion", Message: "Debe estar entre 1 y 5", Rule: "rango"})
			}
			comentario, err := textoOpcional("comentario", in.Comentario, 1000)
			if err != nil {
				return EncuestaPublica{}, err
			}
			q := soportebd.New(t)
			e, err := q.EncuestaParaResponder(ctx, hashToken(in.Token))
			if errors.Is(err, pgx.ErrNoRows) {
				return EncuestaPublica{}, m.rechazoToken()
			}
			if err != nil {
				return EncuestaPublica{}, err
			}
			t.FijarEntidad("encuesta", e.ID)
			t.FijarEmpresa(deref(e.EmpresaID))
			empresa, err := nombreEmpresa(ctx, t, e.EmpresaID)
			if err != nil {
				return EncuestaPublica{}, err
			}
			if e.Estado == "respondida" {
				// Un solo uso: la misma respuesta otra vez es idempotente; otra distinta, 409.
				if int(deref16(e.Calificacion)) == in.Calificacion && mismoComentario(e.Comentario, comentario) {
					t.Anotar(map[string]any{"repetida": true})
					return m.encuestaPublica(e, empresa), nil
				}
				return EncuestaPublica{}, encuestaRespondida()
			}
			ahora := m.ahora()
			if !ahora.Before(e.ExpiraEn) {
				return EncuestaPublica{}, encuestaVencida()
			}
			despues, err := q.ResponderEncuesta(ctx, soportebd.ResponderEncuestaParams{
				ID: e.ID, Calificacion: ptr(int16(in.Calificacion)), Comentario: comentario, Ahora: &ahora, //nolint:gosec // 1–5
			})
			if err != nil {
				return EncuestaPublica{}, err
			}
			t.RegistrarCambios("encuesta", e.ID, respuestaAuditada(e), respuestaAuditada(despues))
			entidadID := deref(e.TicketID)
			if e.OrdenID != nil {
				entidadID = *e.OrdenID
			}
			if err := t.Emitir("encuesta.respondida", map[string]any{
				"encuesta_id": e.ID, "entidad": e.Entidad, "entidad_id": entidadID,
				"calificacion": in.Calificacion, "alerta": in.Calificacion <= 2,
			}); err != nil {
				return EncuestaPublica{}, err
			}
			return m.encuestaPublica(despues, empresa), nil
		},
	})
}

type respuestaEncuesta struct {
	Estado       string  `json:"estado"`
	Calificacion *int16  `json:"calificacion"`
	Comentario   *string `json:"comentario" auditoria:"sensible"`
}

func respuestaAuditada(e soportebd.SoporteEncuesta) respuestaEncuesta {
	return respuestaEncuesta{Estado: e.Estado, Calificacion: e.Calificacion, Comentario: e.Comentario}
}

func deref16(p *int16) int16 {
	if p == nil {
		return 0
	}
	return *p
}

func nombreEmpresa(ctx context.Context, t *auditoria.Tx, id *int64) (*string, error) {
	if id == nil {
		return nil, nil
	}
	var nombre string
	err := t.QueryRow(ctx, "SELECT nombre FROM nucleo.empresas WHERE id = $1", *id).Scan(&nombre)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &nombre, nil
}

// Panel: lista -------------------------------------------------------------------------------

// EntradaListarEncuestas: filtros de GET /encuestas.
type EntradaListarEncuestas struct {
	nucleo.ParamsPagina
	Desde        string  `query:"desde" format:"date" doc:"Creadas desde este día (YYYY-MM-DD, hora de Guatemala)"`
	Hasta        string  `query:"hasta" format:"date" doc:"Creadas hasta este día, incluido"`
	Calificacion []int32 `query:"calificacion" minimum:"1" maximum:"5" doc:"Una o varias (calificacion=1,2)"`
	Entidad      string  `query:"entidad" enum:"ticket,orden"`
	Estado       string  `query:"estado" enum:"pendiente,respondida,vencida"`
	TicketID     int64   `query:"ticketId" minimum:"0" doc:"La del ticket (detalle del ticket)"`
	OrdenID      int64   `query:"ordenId" minimum:"0" doc:"La de la orden (detalle de la orden)"`
}

// Encuesta es una encuesta en el panel.
type Encuesta struct {
	ID           int64      `json:"id"`
	Entidad      string     `json:"entidad" enum:"ticket,orden"`
	TicketID     *int64     `json:"ticketId" nullable:"true"`
	OrdenID      *int64     `json:"ordenId" nullable:"true"`
	Numero       string     `json:"numero" doc:"Número del ticket o de la orden"`
	Canal        string     `json:"canal" enum:"correo,enlace" doc:"correo: se encoló el correo al contacto; enlace: soporte comparte el enlace"`
	Estado       string     `json:"estado" enum:"pendiente,respondida,vencida"`
	ExpiraEn     time.Time  `json:"expiraEn"`
	Calificacion *int       `json:"calificacion" nullable:"true" minimum:"1" maximum:"5"`
	Comentario   *string    `json:"comentario" nullable:"true"`
	RespondidaEn *time.Time `json:"respondidaEn" nullable:"true"`
	Alerta       bool       `json:"alerta" doc:"Calificación 1 o 2: resaltar"`
	Enlace       *string    `json:"enlace" nullable:"true" doc:"<IRIS_BASE_URL>/encuesta#<token> mientras esté pendiente; null si respondida, vencida o sin clave para derivarlo (enlaceDisponible)"`
	// EnlaceDisponible: false si la encuesta está pendiente pero el enlace no se puede volver a
	// mostrar (sin IRIS_CLAVE_CIFRADO o con la clave rotada).
	EnlaceDisponible bool      `json:"enlaceDisponible"`
	CreadaEn         time.Time `json:"creadaEn"`
}

func (m *Modulo) aEncuesta(e soportebd.SoporteEncuesta, ahora time.Time) Encuesta {
	r := Encuesta{
		ID: e.ID, Entidad: e.Entidad, TicketID: e.TicketID, OrdenID: e.OrdenID, Numero: e.Numero, Canal: e.Canal,
		Estado: estadoEncuesta(e, ahora), ExpiraEn: e.ExpiraEn, Comentario: e.Comentario, RespondidaEn: e.RespondidaEn,
		CreadaEn: e.CreatedAt,
	}
	if e.Calificacion != nil {
		r.Calificacion = ptr(int(*e.Calificacion))
		r.Alerta = *e.Calificacion <= 2
	}
	if r.Estado == "pendiente" {
		if token, ok := m.tokenGuardado(e); ok {
			r.Enlace, r.EnlaceDisponible = ptr(m.urlEncuesta(token)), true
		}
	}
	return r
}

func (m *Modulo) nuevoListarEncuestas() *auditoria.Caso[EntradaListarEncuestas, nucleo.Pagina[Encuesta]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarEncuestas, nucleo.Pagina[Encuesta]]{
		Accion:       "encuestas.ver",
		Permiso:      nucleo.PermEncuestasVer,
		AuditarExito: soloLecturas[nucleo.Pagina[Encuesta]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarEncuestas) (nucleo.Pagina[Encuesta], error) {
			var antesDe int64
			hay, err := nucleo.DecodificarCursor(in.Cursor, &antesDe)
			if err != nil {
				return nucleo.Pagina[Encuesta]{}, err
			}
			desde, hasta, err := diasGuatemala(in.Desde, in.Hasta)
			if err != nil {
				return nucleo.Pagina[Encuesta]{}, err
			}
			global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermEncuestasVer))
			todas, empresa := filtroEmpresa(ctx)
			ahora := m.ahora()
			p := soportebd.ListarEncuestasParams{
				TodasEmpresas: todas, EmpresaID: empresa, AlcanceGlobal: global, Zonas: noNulo(zonas), Cuadrillas: noNulo(cuadrillas),
				Desde: desde, Hasta: hasta, TicketID: filtro(in.TicketID), OrdenID: filtro(in.OrdenID),
				Calificaciones: noNulo(in.Calificacion), Ahora: ahora,
				Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			}
			if hay {
				p.AntesDe = &antesDe
			}
			if in.Entidad != "" {
				p.Entidad = &in.Entidad
			}
			if in.Estado != "" {
				p.Estado = &in.Estado
			}
			filas, err := soportebd.New(t).ListarEncuestas(ctx, p)
			if err != nil {
				return nucleo.Pagina[Encuesta]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(),
				func(f soportebd.SoporteEncuesta) Encuesta { return m.aEncuesta(f, ahora) },
				func(f soportebd.SoporteEncuesta) []any { return []any{f.ID} }), nil
		},
	})
}

// diasGuatemala convierte días opcionales (YYYY-MM-DD, hora de Guatemala) en [desde, hasta+1).
func diasGuatemala(desde, hasta string) (*time.Time, *time.Time, error) {
	var d, h *time.Time
	for _, x := range []struct {
		campo, valor string
		destino      **time.Time
		dias         int
	}{{"desde", desde, &d, 0}, {"hasta", hasta, &h, 1}} {
		if x.valor == "" {
			continue
		}
		v, err := time.ParseInLocation(time.DateOnly, x.valor, zonaGuatemala())
		if err != nil {
			return nil, nil, nucleo.Validacion(nucleo.ErrorCampo{Field: x.campo, Message: "Fecha inválida (YYYY-MM-DD)", Rule: "date"})
		}
		v = v.AddDate(0, 0, x.dias).UTC()
		*x.destino = &v
	}
	if d != nil && h != nil && !h.After(*d) {
		return nil, nil, nucleo.Validacion(nucleo.ErrorCampo{Field: "hasta", Message: "Debe ser igual o posterior a desde", Rule: "rango"})
	}
	return d, h, nil
}
