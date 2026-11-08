package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Canales (contrato 0.6.0 §6.1). Cada medio se habla con un Adaptador. Fase 1: WhatsApp Cloud API
// (Meta) y el simulado (desarrollo y CI). WhatsApp por QR y Telegram (fases siguientes) se suman
// como otro Adaptador: QR responde PermiteAvisos() = false (nunca avisos de Argos ni a contactos
// nuevos) y Telegram solo llega a quien ya inició el chat.

// Tipos de adaptador.
const (
	TipoWhatsAppCloud = "whatsapp_cloud"
	TipoSimulado      = "simulado"
	// Futuros (sin implementar): TipoWhatsAppQR = "whatsapp_qr", TipoTelegram = "telegram".
)

// Medios por los que sale un aviso.
const (
	CanalWhatsApp = "whatsapp"
	CanalTelegram = "telegram"
	CanalCorreo   = "correo"
)

// Mensaje es un aviso listo para salir por un canal.
type Mensaje struct {
	AvisoID   uuid.UUID
	Destino   string // E.164
	Plantilla Plantilla
	Variables map[string]string
}

// Resultado de un envío aceptado por el canal.
type Resultado struct {
	// ProveedorID: id del mensaje en el canal (wamid de Meta), para casar los estados de entrega.
	ProveedorID string
	// Entregado: el canal ya confirmó la entrega (el simulado); si no, llega por su webhook.
	Entregado bool
}

// ErrorCanal es un rechazo del canal. Temporal se reintenta; si no, el aviso falla con Motivo
// (numero_invalido o rechazado_por_canal). Pausar: el número debe pausarse (límite o calidad).
type ErrorCanal struct {
	Motivo   string
	Detalle  string
	Temporal bool
	Pausar   bool
}

func (e *ErrorCanal) Error() string { return "canal: " + e.Motivo + ": " + e.Detalle }

// Adaptador habla con un medio.
type Adaptador interface {
	Tipo() string
	Canal() string
	// PermiteAvisos: el canal puede mandar avisos (plantillas) a quien no escribió antes.
	PermiteAvisos() bool
	// Aprobada: la plantilla está aprobada en este canal.
	Aprobada(codigo string) bool
	// EnviarPlantilla manda el aviso. Un *ErrorCanal dice si se reintenta.
	EnviarPlantilla(ctx context.Context, m Mensaje) (Resultado, error)
	// EnviarConversacion manda un mensaje de una conversación con un cliente (texto, medio o
	// plantilla; contratos/conversaciones.md §8).
	EnviarConversacion(ctx context.Context, m MensajeSaliente) (Resultado, error)
}

// MensajeSaliente es un mensaje de una conversación listo para salir.
type MensajeSaliente struct {
	ID      uuid.UUID
	Destino string // E.164
	Texto   *string
	// Plantilla (con sus variables) en lugar de texto.
	Plantilla *Plantilla
	Variables map[string]string
	// Medio con Texto como pie.
	Medio *MedioSaliente
}

// MedioSaliente es un archivo del módulo de adjuntos.
type MedioSaliente struct {
	// Tipo: imagen, documento, audio o video.
	Tipo   string
	Mime   string
	Nombre string
	Datos  []byte
}

func (simulado) EnviarConversacion(_ context.Context, m MensajeSaliente) (Resultado, error) {
	if strings.HasSuffix(m.Destino, "0000") {
		return Resultado{}, &ErrorCanal{Motivo: MotivoNumeroInvalido, Detalle: "simulado: número inválido"}
	}
	return Resultado{ProveedorID: "sim-" + m.ID.String(), Entregado: true}, nil
}

// Simulado ----------------------------------------------------------------------------------

// simulado no sale a ningún lado: acepta y confirma la entrega al instante. Un destino que termina
// en 0000 responde numero_invalido y uno en 9999 falla temporalmente (para probar los dos caminos).
type simulado struct{}

func (simulado) Tipo() string         { return TipoSimulado }
func (simulado) Canal() string        { return CanalWhatsApp }
func (simulado) PermiteAvisos() bool  { return true }
func (simulado) Aprobada(string) bool { return true }
func (simulado) EnviarPlantilla(_ context.Context, m Mensaje) (Resultado, error) {
	switch {
	case strings.HasSuffix(m.Destino, "0000"):
		return Resultado{}, &ErrorCanal{Motivo: MotivoNumeroInvalido, Detalle: "simulado: número inválido"}
	case strings.HasSuffix(m.Destino, "9999"):
		return Resultado{}, &ErrorCanal{Motivo: MotivoRechazado, Detalle: "simulado: falla temporal", Temporal: true}
	}
	return Resultado{ProveedorID: "sim-" + m.AvisoID.String(), Entregado: true}, nil
}

// Resolución ---------------------------------------------------------------------------------

// credenciales de un canal de Meta (JSON cifrado en mensajeria.canales.credenciales_cifradas).
type credenciales struct {
	Token       string `json:"token"`
	AppSecret   string `json:"app_secret"`
	VerifyToken string `json:"verify_token"`
}

// PlantillaMeta es una plantilla aprobada en Meta para un código de Iris.
type PlantillaMeta struct {
	Nombre string `json:"nombre" minLength:"1" maxLength:"512" pattern:"^[a-z0-9_]+$" doc:"Nombre de la plantilla aprobada en Meta"`
	Idioma string `json:"idioma" minLength:"2" maxLength:"10" pattern:"^[a-z]{2,3}(_[A-Z]{2})?$" doc:"Código de idioma de la plantilla en Meta (es, es_MX…)"`
}

// DatosAsociadosCanal ata las credenciales cifradas a su fila (también lo usa la rotación de la clave).
func DatosAsociadosCanal(id int64) []byte {
	return fmt.Appendf(nil, "mensajeria.canal:%d:credenciales", id)
}

// resuelto: el canal por el que saldría un aviso de una empresa.
type resuelto struct {
	a         Adaptador
	canalID   *int64
	remitente string
	fila      *mensajeriabd.MensajeriaCanal
}

// resolver elige el canal de la empresa para el medio: el número de avisos de la empresa (activo)
// o, sin él y con Simulado, el simulado. nil = ningún canal (canal_no_disponible).
func (m *Modulo) resolver(ctx context.Context, q *mensajeriabd.Queries, empresa int64, medio string) (*resuelto, error) {
	f, err := q.CanalAvisosDeEmpresa(ctx, mensajeriabd.CanalAvisosDeEmpresaParams{EmpresaID: empresa, Canal: medio})
	if errors.Is(err, pgx.ErrNoRows) {
		// Un número prestado que la empresa marcó como su número de avisos (§15.3).
		f, err = q.CanalAvisosPrestado(ctx, mensajeriabd.CanalAvisosPrestadoParams{EmpresaID: empresa, Canal: medio})
	}
	switch {
	case err == nil:
		return m.resueltoDe(f)
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	case m.cfg.Simulado && medio == CanalWhatsApp:
		return &resuelto{a: simulado{}, remitente: "simulado:" + strconv.FormatInt(empresa, 10)}, nil
	}
	return nil, nil
}

// resolverPorID: el canal de un aviso ya encolado (nil id = simulado). nil si ya no está activo.
func (m *Modulo) resolverPorID(ctx context.Context, q *mensajeriabd.Queries, id *int64, empresa int64) (*resuelto, error) {
	if id == nil {
		if !m.cfg.Simulado {
			return nil, nil
		}
		return &resuelto{a: simulado{}, remitente: "simulado:" + strconv.FormatInt(empresa, 10)}, nil
	}
	f, err := q.CanalPorID(ctx, *id)
	if err != nil {
		return nil, err
	}
	if !f.Activo || f.DesconectadoEn != nil {
		return nil, nil
	}
	// Un número de otra empresa solo con su préstamo activo y el uso avisos (§15.3).
	if derefEmpresa(f.EmpresaID) != empresa {
		u, err := usoDe(ctx, q, f, empresa)
		if err != nil || !u.activo || !slices.Contains(u.usos, "avisos") {
			return nil, err
		}
	}
	return m.resueltoDe(f)
}

func (m *Modulo) resueltoDe(f mensajeriabd.MensajeriaCanal) (*resuelto, error) {
	cr, err := m.credencialesDe(f)
	if err != nil {
		return nil, err
	}
	plantillas := map[string]PlantillaMeta{}
	if err := json.Unmarshal(f.Plantillas, &plantillas); err != nil {
		return nil, err
	}
	id := f.ID
	a := &whatsappCloud{m: m, phoneNumberID: f.PhoneNumberID, token: cr.Token, plantillas: plantillas}
	return &resuelto{a: a, canalID: &id, remitente: "canal:" + strconv.FormatInt(f.ID, 10), fila: &f}, nil
}

func (m *Modulo) credencialesDe(f mensajeriabd.MensajeriaCanal) (credenciales, error) {
	if m.cfg.Cifrador == nil {
		return credenciales{}, errSinCifrado()
	}
	plano, err := m.cfg.Cifrador.Descifrar(f.CredencialesCifradas, DatosAsociadosCanal(f.ID))
	if err != nil {
		return credenciales{}, fmt.Errorf("canal %d: descifrar credenciales: %w", f.ID, err)
	}
	defer clear(plano)
	var c credenciales
	return c, json.Unmarshal(plano, &c)
}
