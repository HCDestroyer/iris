package mensajeria

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Conexión con Meta de la instalación (contratos/conversaciones.md §15.1): una sola fila en
// mensajeria.conexion_meta (modo propia o compartida) o el entorno (modo saas). La usan los números
// conectados con QR (coexistencia). Los secretos (app secret, clave de enlace, verify token del
// webhook de la app) se guardan cifrados y nunca se devuelven ni se registran.

// Modos de la conexión.
const (
	ModoMetaSinConfigurar = "sin_configurar"
	ModoMetaPropia        = "propia"
	ModoMetaCompartida    = "compartida"
	ModoMetaSaaS          = "saas"
)

// ServicioCentralPorDefecto es la URL de Iris Cloud (modo compartida).
const ServicioCentralPorDefecto = "https://cloud.iris.kns.gt"

// CamposWebhookMeta son los campos del webhook a los que se suscribe la app en Meta.
var CamposWebhookMeta = []string{"messages", "smb_message_echoes", "history", "smb_app_state_sync", "account_update"}

// RutaWebhookApp es el webhook de la app de Meta (todos los números conectados con QR).
const RutaWebhookApp = Prefijo + "/meta/webhook"

// MetaEntorno es la conexión fija del entorno (Iris en la nube, IRIS_META_MODO=saas).
type MetaEntorno struct {
	// Modo: "" (la configura el administrador) o "saas".
	Modo         string
	AppID        string
	AppSecret    string
	ConfigID     string
	GraphVersion string
	VerifyToken  string
}

var (
	reDigitos      = regexp.MustCompile(`^[0-9]{1,30}$`)
	reVersionGraph = regexp.MustCompile(`^v[0-9]{1,2}\.[0-9]$`)
	reVersionFinal = regexp.MustCompile(`/v[0-9]{1,2}\.[0-9]$`)
)

// hostGraph: la URL de la Graph API sin la versión final (IRIS_MENSAJERIA_META_URL la trae).
func (m *Modulo) hostGraph() string { return reVersionFinal.ReplaceAllString(m.cfg.MetaURL, "") }

// versionGraphIris: la versión de la Graph API de Iris (la de IRIS_MENSAJERIA_META_URL; v21.0).
func (m *Modulo) versionGraphIris() string {
	if v := reVersionFinal.FindString(m.cfg.MetaURL); v != "" {
		return v[1:]
	}
	return "v21.0"
}

// secretosConexion es el JSON cifrado de la conexión.
type secretosConexion struct {
	AppSecret   string `json:"app_secret,omitempty"`
	ClaveEnlace string `json:"clave_enlace,omitempty"`
	VerifyToken string `json:"verify_token,omitempty"`
}

// DatosAsociadosConexion ata los secretos cifrados a la fila (también lo usa la rotación de la clave).
func DatosAsociadosConexion() []byte { return []byte("mensajeria.conexion_meta:1:secretos") }

func (m *Modulo) secretosDe(f mensajeriabd.MensajeriaConexionMeta) (secretosConexion, error) {
	if m.cfg.Cifrador == nil {
		return secretosConexion{}, errSinCifrado()
	}
	plano, err := m.cfg.Cifrador.Descifrar(f.SecretosCifrados, DatosAsociadosConexion())
	if err != nil {
		return secretosConexion{}, fmt.Errorf("conexión con Meta: descifrar secretos: %w", err)
	}
	defer clear(plano)
	var s secretosConexion
	return s, json.Unmarshal(plano, &s)
}

func (m *Modulo) cifrarSecretos(s secretosConexion) ([]byte, error) {
	if m.cfg.Cifrador == nil {
		return nil, errSinCifrado()
	}
	plano, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	defer clear(plano)
	return m.cfg.Cifrador.Cifrar(plano, DatosAsociadosConexion())
}

// conexionEfectiva es la conexión con la que se trabaja (la de la base o la del entorno).
type conexionEfectiva struct {
	modo         string
	appID        string
	configID     string
	graphVersion string
	appSecret    string
	verifyToken  string
	servicioURL  string
	fila         *mensajeriabd.MensajeriaConexionMeta
}

// completa: hay app ID, secreto y config ID (el QR está disponible).
func (c conexionEfectiva) completa() bool {
	return (c.modo == ModoMetaPropia || c.modo == ModoMetaSaaS) && c.appID != "" && c.appSecret != "" && c.configID != ""
}

func (m *Modulo) esSaaS() bool { return strings.EqualFold(m.cfg.Meta.Modo, ModoMetaSaaS) }

// conexion lee la conexión efectiva (con secretos: nunca salen de este proceso).
func (m *Modulo) conexion(ctx context.Context, q *mensajeriabd.Queries) (conexionEfectiva, error) {
	if m.esSaaS() {
		e := m.cfg.Meta
		v := e.GraphVersion
		if v == "" {
			v = m.versionGraphIris()
		}
		return conexionEfectiva{modo: ModoMetaSaaS, appID: e.AppID, configID: e.ConfigID, graphVersion: v, appSecret: e.AppSecret, verifyToken: e.VerifyToken}, nil
	}
	f, err := q.ConexionMeta(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return conexionEfectiva{modo: ModoMetaSinConfigurar, graphVersion: m.versionGraphIris()}, nil
	}
	if err != nil {
		return conexionEfectiva{}, err
	}
	return m.conexionDeFila(f)
}

func (m *Modulo) conexionDeFila(f mensajeriabd.MensajeriaConexionMeta) (conexionEfectiva, error) {
	s, err := m.secretosDe(f)
	if err != nil {
		return conexionEfectiva{}, err
	}
	c := conexionEfectiva{modo: f.Modo, appSecret: s.AppSecret, verifyToken: s.VerifyToken, fila: &f, graphVersion: m.versionGraphIris(),
		servicioURL: ServicioCentralPorDefecto}
	if f.AppID != nil {
		c.appID = *f.AppID
	}
	if f.ConfigID != nil {
		c.configID = *f.ConfigID
	}
	if f.GraphVersion != nil {
		c.graphVersion = *f.GraphVersion
	}
	if f.ServicioUrl != nil {
		c.servicioURL = *f.ServicioUrl
	}
	return c, nil
}

// Respuesta -----------------------------------------------------------------------------------------

// ComprobacionMeta es una línea de «Probar conexión».
type ComprobacionMeta struct {
	Clave   string `json:"clave" enum:"app,configuracion,webhook,tech_provider"`
	Estado  string `json:"estado" enum:"ok,error,advertencia,sin_verificar"`
	Detalle string `json:"detalle" doc:"En español; nunca lleva secretos ni el mensaje de Meta"`
}

// ConexionMeta es la conexión con Meta de la instalación (nunca sus secretos).
type ConexionMeta struct {
	Modo                  string             `json:"modo" enum:"sin_configurar,propia,compartida,saas"`
	Gestionada            bool               `json:"gestionada" doc:"true en modo saas: solo lectura"`
	AppID                 *string            `json:"appId" nullable:"true"`
	ConfigID              *string            `json:"configId" nullable:"true" doc:"Configuración de Facebook Login para empresas (WhatsApp Embedded Signup)"`
	GraphVersion          string             `json:"graphVersion" doc:"Versión efectiva de la Graph API"`
	GraphVersionPropia    *string            `json:"graphVersionPropia" nullable:"true" doc:"La guardada; null = la de Iris"`
	AppSecretGuardadoEn   *time.Time         `json:"appSecretGuardadoEn" nullable:"true" doc:"Cuándo se guardó el app secret (nunca el valor)"`
	ServicioURL           string             `json:"servicioUrl" doc:"Servicio central (modo compartida)"`
	ClaveEnlaceGuardadaEn *time.Time         `json:"claveEnlaceGuardadaEn" nullable:"true" doc:"Cuándo se guardó la clave de enlace (nunca el valor)"`
	WebhookURL            string             `json:"webhookUrl" doc:"URL del webhook de la app para pegar en Meta"`
	WebhookURLCompleta    bool               `json:"webhookUrlCompleta"`
	VerifyTokenGuardadoEn *time.Time         `json:"verifyTokenGuardadoEn" nullable:"true"`
	WebhookVerificadoEn   *time.Time         `json:"webhookVerificadoEn" nullable:"true"`
	UltimoWebhookEn       *time.Time         `json:"ultimoWebhookEn" nullable:"true" doc:"Último webhook firmado en la URL de la app (precisión de 1 minuto)"`
	CamposWebhook         []string           `json:"camposWebhook" doc:"Campos del webhook a suscribir en Meta"`
	Estado                string             `json:"estado" enum:"sin_configurar,sin_probar,lista,error,sin_servicio_central,gestionada"`
	QRDisponible          bool               `json:"qrDisponible"`
	MotivoQR              *string            `json:"motivoQr" nullable:"true" enum:"sin_configurar,servicio_central_no_disponible"`
	UltimaPruebaEn        *time.Time         `json:"ultimaPruebaEn" nullable:"true"`
	Comprobaciones        []ComprobacionMeta `json:"comprobaciones"`
	Version               int32              `json:"version"`
	ActualizadoEn         *time.Time         `json:"actualizadoEn" nullable:"true"`
	// VerifyTokenGenerado solo sale en la respuesta de actualizar cuando Iris lo generó.
	VerifyTokenGenerado *string `json:"verifyTokenGenerado,omitempty" doc:"Verify token del webhook de la app: solo en esta respuesta, una vez"`
}

func (m *Modulo) aConexion(c conexionEfectiva) ConexionMeta {
	out := ConexionMeta{Modo: c.modo, GraphVersion: c.graphVersion, ServicioURL: ServicioCentralPorDefecto, CamposWebhook: slices.Clone(CamposWebhookMeta),
		Comprobaciones: []ComprobacionMeta{}}
	out.WebhookURL, out.WebhookURLCompleta = m.urlPublica(RutaWebhookApp)
	if c.appID != "" {
		out.AppID = texto(c.appID)
	}
	if c.configID != "" {
		out.ConfigID = texto(c.configID)
	}
	if f := c.fila; f != nil {
		out.GraphVersionPropia, out.AppSecretGuardadoEn, out.ClaveEnlaceGuardadaEn = f.GraphVersion, f.AppSecretGuardadoEn, f.ClaveEnlaceGuardadaEn
		out.VerifyTokenGuardadoEn, out.WebhookVerificadoEn, out.UltimoWebhookEn = f.VerifyTokenGuardadoEn, f.WebhookVerificadoEn, f.UltimoWebhookEn
		out.UltimaPruebaEn, out.Version = f.UltimaPruebaEn, f.Version
		actualizado := f.UpdatedAt
		out.ActualizadoEn = &actualizado
		if f.ServicioUrl != nil {
			out.ServicioURL = *f.ServicioUrl
		}
		_ = json.Unmarshal(f.Comprobaciones, &out.Comprobaciones)
		if out.Comprobaciones == nil {
			out.Comprobaciones = []ComprobacionMeta{}
		}
	}
	out.QRDisponible = c.completa()
	switch {
	case c.modo == ModoMetaSaaS:
		out.Gestionada, out.Estado = true, "gestionada"
	case c.modo == ModoMetaSinConfigurar:
		out.Estado = "sin_configurar"
	case c.modo == ModoMetaCompartida:
		out.Estado = "sin_servicio_central"
	case c.fila != nil && c.fila.UltimoEstadoPrueba != nil:
		out.Estado = *c.fila.UltimoEstadoPrueba
	default:
		out.Estado = "sin_probar"
	}
	if !out.QRDisponible {
		motivo := "sin_configurar"
		if c.modo == ModoMetaCompartida {
			motivo = "servicio_central_no_disponible"
		}
		out.MotivoQR = &motivo
	}
	return out
}

// urlPublica: la URL completa (sobre IRIS_BASE_URL) o, sin ella, solo la ruta.
func (m *Modulo) urlPublica(ruta string) (string, bool) {
	base := strings.TrimRight(strings.TrimSpace(m.cfg.BaseURL), "/")
	if base == "" {
		return ruta, false
	}
	return base + ruta, true
}

// Casos de uso -------------------------------------------------------------------------------------

func soloPersonaConexion(ctx context.Context) error {
	if actor := contexto.ActorDe(ctx); actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
		return nucleo.NoEncontrado("La conexión con Meta no existe")
	}
	return nil
}

func nuevoVerConexionMeta(m *Modulo) *auditoria.Caso[struct{}, ConexionMeta] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ConexionMeta]{
		Accion: "mensajeria.ver_conexion_meta", Permiso: nucleo.PermMensajeriaConexionMeta, SinEmpresa: true,
		AuditarExito: sinAuditar[ConexionMeta],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ConexionMeta, error) {
			if err := soloPersonaConexion(ctx); err != nil {
				return ConexionMeta{}, err
			}
			c, err := m.conexion(ctx, mensajeriabd.New(t))
			if err != nil {
				return ConexionMeta{}, err
			}
			return m.aConexion(c), nil
		},
	})
}

// EntradaConexionMeta es POST /conexion-meta/actualizar.
type EntradaConexionMeta struct {
	Version            int32   `json:"version" minimum:"0" doc:"0 la primera vez"`
	Modo               string  `json:"modo" enum:"propia,compartida"`
	AppID              *string `json:"appId,omitempty" nullable:"true" pattern:"^[0-9]{1,30}$"`
	AppSecret          string  `json:"appSecret,omitempty" maxLength:"256" auditoria:"sensible" doc:"Solo escritura; vacío = no cambia"`
	ConfigID           *string `json:"configId,omitempty" nullable:"true" pattern:"^[0-9]{1,30}$"`
	GraphVersion       *string `json:"graphVersion,omitempty" nullable:"true" pattern:"^v[0-9]{1,2}\\.[0-9]$" doc:"null = la de Iris"`
	ServicioURL        *string `json:"servicioUrl,omitempty" nullable:"true" maxLength:"300" doc:"https; null = la por defecto"`
	ClaveEnlace        string  `json:"claveEnlace,omitempty" maxLength:"512" auditoria:"sensible" doc:"Solo escritura; vacío = no cambia"`
	GenerarVerifyToken bool    `json:"generarVerifyToken,omitempty" doc:"Genera otro verify token para el webhook de la app"`
}

// conexionAuditable: lo que queda en auditoria.cambios (los secretos como ***).
type conexionAuditable struct {
	Modo         string  `json:"modo"`
	AppID        *string `json:"app_id"`
	ConfigID     *string `json:"config_id"`
	GraphVersion *string `json:"graph_version"`
	ServicioURL  *string `json:"servicio_url"`
	AppSecret    string  `json:"app_secret" auditoria:"sensible"`
	ClaveEnlace  string  `json:"clave_enlace" auditoria:"sensible"`
	VerifyToken  string  `json:"verify_token" auditoria:"sensible"`
}

func auditableConexion(f *mensajeriabd.MensajeriaConexionMeta, appSecret, claveEnlace, verifyToken string) any {
	if f == nil {
		return nil
	}
	return conexionAuditable{Modo: f.Modo, AppID: f.AppID, ConfigID: f.ConfigID, GraphVersion: f.GraphVersion, ServicioURL: f.ServicioUrl,
		AppSecret: appSecret, ClaveEnlace: claveEnlace, VerifyToken: verifyToken}
}

func conexionGestionada() *nucleo.Problema {
	return reglaConv("conexion_gestionada", "En Iris en la nube la conexión con Meta la administra Horus")
}

func nuevoActualizarConexionMeta(m *Modulo) *auditoria.Caso[EntradaConexionMeta, ConexionMeta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConexionMeta, ConexionMeta]{
		Accion: "mensajeria.conexion_meta", Permiso: nucleo.PermMensajeriaConexionMeta, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConexionMeta) (ConexionMeta, error) {
			if err := soloPersonaConexion(ctx); err != nil {
				return ConexionMeta{}, err
			}
			if m.esSaaS() {
				return ConexionMeta{}, conexionGestionada()
			}
			q := mensajeriabd.New(t)
			var antes *mensajeriabd.MensajeriaConexionMeta
			sec := secretosConexion{}
			f, err := q.ConexionMetaParaActualizar(ctx)
			switch {
			case err == nil:
				antes = &f
				if sec, err = m.secretosDe(f); err != nil {
					return ConexionMeta{}, err
				}
			case !errors.Is(err, pgx.ErrNoRows):
				return ConexionMeta{}, err
			}
			version := int32(0)
			if antes != nil {
				version = antes.Version
			}
			if in.Version != version {
				return ConexionMeta{}, nucleo.ConflictoVersion("conexion_meta", 1)
			}
			if m.cfg.Cifrador == nil {
				return ConexionMeta{}, errSinCifrado()
			}
			ahora := m.ahora()
			p := mensajeriabd.GuardarConexionMetaParams{Modo: in.Modo, ActualizadoPor: usuarioDe(ctx), Comprobaciones: []byte("[]")}
			if antes != nil {
				p.AppSecretGuardadoEn, p.ClaveEnlaceGuardadaEn, p.VerifyTokenGuardadoEn = antes.AppSecretGuardadoEn, antes.ClaveEnlaceGuardadaEn, antes.VerifyTokenGuardadoEn
				p.WebhookVerificadoEn, p.UltimaPruebaEn, p.UltimoEstadoPrueba, p.Comprobaciones = antes.WebhookVerificadoEn, antes.UltimaPruebaEn, antes.UltimoEstadoPrueba, antes.Comprobaciones
				// Lo del otro modo se conserva (cambiar de modo y volver no obliga a reescribirlo).
				p.AppID, p.ConfigID, p.GraphVersion, p.ServicioUrl = antes.AppID, antes.ConfigID, antes.GraphVersion, antes.ServicioUrl
			}
			cambioApp, cambioSecretos := false, false
			marca := map[string]string{"app_secret": "sin_cambio", "clave_enlace": "sin_cambio", "verify_token": "sin_cambio"}
			switch in.Modo {
			case ModoMetaPropia:
				if in.AppID == nil || !reDigitos.MatchString(*in.AppID) {
					return ConexionMeta{}, campoConv("appId", "Obligatorio: el app ID de tu app de Meta (solo dígitos)", "required")
				}
				if in.ConfigID == nil || !reDigitos.MatchString(*in.ConfigID) {
					return ConexionMeta{}, campoConv("configId", "Obligatorio: el ID de la configuración de Facebook Login para empresas (solo dígitos)", "required")
				}
				if in.AppSecret != "" {
					if len(in.AppSecret) < 16 {
						return ConexionMeta{}, campoConv("appSecret", "Demasiado corto (mínimo 16)", "minLength")
					}
					sec.AppSecret, marca["app_secret"], cambioSecretos = in.AppSecret, "***", true
					p.AppSecretGuardadoEn = &ahora
				}
				if sec.AppSecret == "" {
					return ConexionMeta{}, campoConv("appSecret", "Obligatorio la primera vez", "required")
				}
				if in.GraphVersion != nil && *in.GraphVersion != "" && !reVersionGraph.MatchString(*in.GraphVersion) {
					return ConexionMeta{}, campoConv("graphVersion", "Formato vNN.N (p. ej. v21.0)", "pattern")
				}
				cambioApp = p.AppID == nil || *p.AppID != *in.AppID || p.ConfigID == nil || *p.ConfigID != *in.ConfigID || cambioSecretos
				p.AppID, p.ConfigID = in.AppID, in.ConfigID
				p.GraphVersion = nil
				if in.GraphVersion != nil && *in.GraphVersion != "" {
					p.GraphVersion = in.GraphVersion
				}
			case ModoMetaCompartida:
				if in.ServicioURL != nil && *in.ServicioURL != "" {
					u, err := url.Parse(*in.ServicioURL)
					if err != nil || u.Scheme != "https" || u.Host == "" {
						return ConexionMeta{}, campoConv("servicioUrl", "Debe ser una URL https", "url")
					}
					s := strings.TrimRight(*in.ServicioURL, "/")
					p.ServicioUrl = &s
				} else {
					p.ServicioUrl = nil
				}
				if in.ClaveEnlace != "" {
					if len(in.ClaveEnlace) < 16 {
						return ConexionMeta{}, campoConv("claveEnlace", "Demasiado corta (mínimo 16)", "minLength")
					}
					sec.ClaveEnlace, marca["clave_enlace"] = in.ClaveEnlace, "***"
					p.ClaveEnlaceGuardadaEn = &ahora
				}
				if sec.ClaveEnlace == "" {
					return ConexionMeta{}, campoConv("claveEnlace", "Obligatoria la primera vez", "required")
				}
			default:
				return ConexionMeta{}, campoConv("modo", "propia o compartida", "enum")
			}
			var generado *string
			if sec.VerifyToken == "" || in.GenerarVerifyToken {
				vt, err := generarVerifyToken()
				if err != nil {
					return ConexionMeta{}, err
				}
				sec.VerifyToken, generado, marca["verify_token"] = vt, &vt, "***"
				p.VerifyTokenGuardadoEn, p.WebhookVerificadoEn = &ahora, nil
			}
			if cambioApp || (antes != nil && antes.Modo != in.Modo) {
				p.UltimaPruebaEn, p.UltimoEstadoPrueba, p.Comprobaciones = nil, nil, []byte("[]")
			}
			if p.SecretosCifrados, err = m.cifrarSecretos(sec); err != nil {
				return ConexionMeta{}, err
			}
			nueva, err := q.GuardarConexionMeta(ctx, p)
			if err != nil {
				return ConexionMeta{}, err
			}
			t.FijarEntidad("conexion_meta", 1)
			t.FijarEmpresa(0)
			campos := t.RegistrarCambios("conexion_meta", int64(1), auditableConexion(antes, "sin_cambio", "sin_cambio", "sin_cambio"),
				auditableConexion(&nueva, marca["app_secret"], marca["clave_enlace"], marca["verify_token"]))
			if campos == nil {
				campos = []string{}
			}
			anterior := ModoMetaSinConfigurar
			if antes != nil {
				anterior = antes.Modo
			}
			c, err := m.conexionDeFila(nueva)
			if err != nil {
				return ConexionMeta{}, err
			}
			out := m.aConexion(c)
			out.VerifyTokenGenerado = generado
			return out, t.Emitir("meta.conexion_cambiada", map[string]any{"modo": nueva.Modo, "anterior": anterior, "campos": campos})
		},
	})
}

// Probar la conexión ---------------------------------------------------------------------------------

// ProbarConexionMeta: lee la conexión (un caso), consulta a Meta fuera de la transacción y guarda el
// resultado (otro caso, auditado, con meta.conexion_probada).
type ProbarConexionMeta struct {
	m       *Modulo
	leer    *auditoria.Caso[struct{}, conexionAProbar]
	guardar *auditoria.Caso[resultadoConexion, ConexionMeta]
}

type conexionAProbar struct {
	c       conexionEfectiva
	version int32
}

type resultadoConexion struct {
	Version        int32              `json:"version"`
	Estado         string             `json:"estado"`
	Comprobaciones []ComprobacionMeta `json:"comprobaciones"`
}

func nuevoProbarConexionMeta(m *Modulo) *ProbarConexionMeta {
	p := &ProbarConexionMeta{m: m}
	p.leer = auditoria.Nuevo(auditoria.Caso[struct{}, conexionAProbar]{
		Accion: "mensajeria.probar_conexion_meta", Permiso: nucleo.PermMensajeriaConexionMeta, SinEmpresa: true,
		AuditarExito: func(conexionAProbar) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (conexionAProbar, error) {
			if err := soloPersonaConexion(ctx); err != nil {
				return conexionAProbar{}, err
			}
			c, err := m.conexion(ctx, mensajeriabd.New(t))
			if err != nil {
				return conexionAProbar{}, err
			}
			if c.modo == ModoMetaSinConfigurar || (c.modo != ModoMetaCompartida && !c.completa()) {
				return conexionAProbar{}, sinConexionMeta()
			}
			out := conexionAProbar{c: c}
			if c.fila != nil {
				out.version = c.fila.Version
			}
			return out, nil
		},
	})
	p.guardar = auditoria.Nuevo(auditoria.Caso[resultadoConexion, ConexionMeta]{
		Accion: "mensajeria.probar_conexion_meta", Permiso: nucleo.PermMensajeriaConexionMeta, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoConexion) (ConexionMeta, error) {
			q := mensajeriabd.New(t)
			t.FijarEntidad("conexion_meta", 1)
			t.FijarEmpresa(0)
			c, err := m.conexion(ctx, q)
			if err != nil {
				return ConexionMeta{}, err
			}
			if c.modo == ModoMetaSaaS || c.modo == ModoMetaCompartida {
				// Nada que guardar: el saas no tiene fila y la compartida no se prueba todavía.
				out := m.aConexion(c)
				ahora := m.ahora()
				out.UltimaPruebaEn, out.Comprobaciones = &ahora, in.Comprobaciones
				if c.modo == ModoMetaSaaS && in.Estado == "error" {
					out.Estado = "error"
				}
				return out, t.Emitir("meta.conexion_probada", map[string]any{"modo": c.modo, "estado": in.Estado})
			}
			if c.fila == nil || c.fila.Version != in.Version {
				return ConexionMeta{}, nucleo.Conflicto("conflicto_version", "La conexión cambió mientras se probaba; vuelve a probar")
			}
			crudo, err := json.Marshal(in.Comprobaciones)
			if err != nil {
				return ConexionMeta{}, err
			}
			ahora := m.ahora()
			f, err := q.RegistrarPruebaConexionMeta(ctx, mensajeriabd.RegistrarPruebaConexionMetaParams{ProbadoEn: &ahora, Estado: &in.Estado, Comprobaciones: crudo})
			if err != nil {
				return ConexionMeta{}, err
			}
			c2, err := m.conexionDeFila(f)
			if err != nil {
				return ConexionMeta{}, err
			}
			return m.aConexion(c2), t.Emitir("meta.conexion_probada", map[string]any{"modo": f.Modo, "estado": in.Estado})
		},
	})
	return p
}

func sinConexionMeta() *nucleo.Problema {
	return reglaConv("conexion_meta_sin_configurar", "Falta configurar la conexión con Meta (app ID, app secret e ID de configuración)")
}

// Ejecutar prueba la conexión: lee, consulta a Meta y guarda.
func (p *ProbarConexionMeta) Ejecutar(ctx context.Context) (ConexionMeta, error) {
	l, err := p.leer.Ejecutar(ctx, struct{}{})
	if err != nil {
		return ConexionMeta{}, err
	}
	r := resultadoConexion{Version: l.version}
	if l.c.modo == ModoMetaCompartida {
		r.Estado = "sin_servicio_central"
		r.Comprobaciones = []ComprobacionMeta{{Clave: "app", Estado: "sin_verificar",
			Detalle: "El servicio central de Horus todavía no está disponible: no hay nada que probar"}}
		return p.guardar.Ejecutar(ctx, r)
	}
	r.Comprobaciones = []ComprobacionMeta{p.m.probarApp(ctx, l.c),
		{Clave: "configuracion", Estado: "sin_verificar", Detalle: "Iris revisa solo el formato del ID; Meta no publica una lectura de la configuración. Si no es de tipo «WhatsApp Embedded Signup», la ventana de Meta lo dice"}}
	if l.c.fila != nil && l.c.fila.WebhookVerificadoEn != nil {
		r.Comprobaciones = append(r.Comprobaciones, ComprobacionMeta{Clave: "webhook", Estado: "ok",
			Detalle: "Meta verificó el webhook de la app el " + l.c.fila.WebhookVerificadoEn.In(p.m.cfg.Zona).Format("02/01/2006 15:04")})
	} else {
		r.Comprobaciones = append(r.Comprobaciones, ComprobacionMeta{Clave: "webhook", Estado: "advertencia",
			Detalle: "Meta todavía no verificó el webhook de la app: pega la URL y el token de verificación en tu app de Meta"})
	}
	r.Comprobaciones = append(r.Comprobaciones, ComprobacionMeta{Clave: "tech_provider", Estado: "sin_verificar",
		Detalle: "Iris no puede saber si tu app ya es Tech Provider con acceso avanzado a whatsapp_business_messaging y whatsapp_business_management: si no lo es, el QR falla al final"})
	r.Estado = "lista"
	if r.Comprobaciones[0].Estado == "error" {
		r.Estado = "error"
	}
	return p.guardar.Ejecutar(ctx, r)
}

// probarApp valida el app ID y el app secret con GET /{app_id}?fields=id,name y el token de la app
// ({app_id}|{app_secret}). El detalle nunca copia el mensaje de Meta.
func (m *Modulo) probarApp(ctx context.Context, c conexionEfectiva) ComprobacionMeta {
	ctx, cancelar := context.WithTimeout(ctx, 15*time.Second)
	defer cancelar()
	// El token de la app ({app_id}|{app_secret}) va en la cabecera Authorization, nunca en la URL.
	destino := m.hostGraph() + "/" + c.graphVersion + "/" + url.PathEscape(c.appID) + "?fields=id,name"
	falla := func(d string) ComprobacionMeta {
		return ComprobacionMeta{Clave: "app", Estado: "error", Detalle: recortar(d, 300)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, destino, nil)
	if err != nil {
		return falla("No se pudo armar la consulta a Meta")
	}
	req.Header.Set("Authorization", "Bearer "+c.appID+"|"+c.appSecret)
	req.Header.Set("User-Agent", "Iris/"+m.cfg.Version)
	resp, err := m.cfg.HTTPMeta.Do(req)
	if err != nil {
		// Sin el texto del error.
		return falla("Meta no respondió (tiempo agotado o sin conexión); vuelve a probar en unos minutos")
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var r struct {
		ID    string        `json:"id"`
		Name  string        `json:"name"`
		Error *errorGraphQR `json:"error"`
	}
	legible := json.Unmarshal(leido, &r) == nil
	if resp.StatusCode/100 == 2 && legible && r.ID == c.appID {
		d := "Meta aceptó el app ID y el app secret"
		if n := strings.TrimSpace(r.Name); n != "" {
			d += ": app «" + recortar(n, 80) + "»"
		}
		return ComprobacionMeta{Clave: "app", Estado: "ok", Detalle: d}
	}
	codigo := 0
	if r.Error != nil {
		codigo = r.Error.Code
	}
	if resp.StatusCode/100 == 2 {
		return falla("Meta respondió con otra app para este ID")
	}
	if codigo == 190 || codigo == 101 || resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		return falla(fmt.Sprintf("Meta no aceptó el app ID o el app secret (HTTP %d, código %d)", resp.StatusCode, codigo))
	}
	return falla(fmt.Sprintf("Meta respondió con un error (HTTP %d, código %d)", resp.StatusCode, codigo))
}

// Datos para abrir la ventana de Meta (GET /conexion-meta/registro) ----------------------------------

// RegistroQR es lo que el panel necesita para abrir Embedded Signup (nunca el secreto).
type RegistroQR struct {
	Disponible         bool    `json:"disponible"`
	Motivo             *string `json:"motivo" nullable:"true" enum:"sin_configurar,servicio_central_no_disponible"`
	Modo               string  `json:"modo" enum:"sin_configurar,propia,compartida,saas"`
	AppID              *string `json:"appId" nullable:"true"`
	ConfigID           *string `json:"configId" nullable:"true"`
	GraphVersion       string  `json:"graphVersion"`
	FeatureType        string  `json:"featureType" doc:"extras.featureType de FB.login (coexistencia)"`
	SessionInfoVersion string  `json:"sessionInfoVersion" doc:"extras.sessionInfoVersion de FB.login"`
}

func nuevoRegistroQR(m *Modulo) *auditoria.Caso[struct{}, RegistroQR] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, RegistroQR]{
		Accion: "mensajeria.registro_qr", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true, AuditarExito: sinAuditar[RegistroQR],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (RegistroQR, error) {
			if err := soloPersonaConexion(ctx); err != nil {
				return RegistroQR{}, err
			}
			c, err := m.conexion(ctx, mensajeriabd.New(t))
			if err != nil {
				return RegistroQR{}, err
			}
			v := m.aConexion(c)
			out := RegistroQR{Disponible: v.QRDisponible, Motivo: v.MotivoQR, Modo: c.modo, GraphVersion: c.graphVersion,
				FeatureType: "whatsapp_business_app_onboarding", SessionInfoVersion: "3"}
			if out.Disponible {
				out.AppID, out.ConfigID = v.AppID, v.ConfigID
			}
			return out, nil
		},
	})
}

// Webhook de la app (POST/GET /meta/webhook) ----------------------------------------------------------

// EntradaMetaApp es el POST del webhook de la app.
type EntradaMetaApp struct {
	Firma  string `json:"-"`
	Cuerpo []byte `json:"-"`
}

// MarshalJSON: lo auditado es el tamaño, nunca el cuerpo.
func (e EntradaMetaApp) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"bytes": len(e.Cuerpo)})
}

// secretoWebhookApp: el app secret con que Meta firma el webhook de la app ("" = sin conexión).
func (m *Modulo) secretoWebhookApp(ctx context.Context, q *mensajeriabd.Queries) (conexionEfectiva, error) {
	c, err := m.conexion(ctx, q)
	if err != nil {
		return c, err
	}
	if c.modo != ModoMetaPropia && c.modo != ModoMetaSaaS {
		c.appSecret, c.verifyToken = "", ""
	}
	return c, nil
}

func nuevoRecibirMetaApp(m *Modulo) *auditoria.Caso[EntradaMetaApp, MetaRecibido] {
	return auditoria.Nuevo(auditoria.Caso[EntradaMetaApp, MetaRecibido]{
		Accion: "mensajeria.recibir_webhook_app", Acceso: auditoria.Publico, AuditarError: noAuditar401,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaMetaApp) (MetaRecibido, error) {
			q := mensajeriabd.New(t)
			c, err := m.secretoWebhookApp(ctx, q)
			if err != nil {
				return MetaRecibido{}, err
			}
			if c.appSecret == "" || !FirmaMetaValida(c.appSecret, in.Firma, in.Cuerpo) {
				return MetaRecibido{}, firmaMetaInvalida()
			}
			var w struct {
				Object string            `json:"object"`
				Entry  []json.RawMessage `json:"entry"`
			}
			if err := json.Unmarshal(in.Cuerpo, &w); err != nil {
				return MetaRecibido{}, nucleo.SolicitudInvalida("El cuerpo no es JSON")
			}
			t.FijarEmpresa(0)
			ahora := m.ahora()
			if c.fila != nil {
				if err := q.MarcarConexionWebhookRecibido(ctx, ahora); err != nil {
					return MetaRecibido{}, err
				}
			}
			// Cada change va al número QR de su phone_number_id (o al de su WABA en account_update):
			// un entry con cambios de varios números se separa por número.
			repartidos, descartados, duplicados := 0, 0, 0
			for _, crudo := range w.Entry {
				var e struct {
					ID      string            `json:"id"`
					Changes []json.RawMessage `json:"changes"`
				}
				if json.Unmarshal(crudo, &e) != nil {
					descartados++
					continue
				}
				porCanal := map[int64][]json.RawMessage{}
				orden := []int64{}
				for _, ch := range e.Changes {
					canal, err := m.canalDeCambio(ctx, q, e.ID, ch)
					if err != nil {
						return MetaRecibido{}, err
					}
					if canal == nil {
						descartados++
						continue
					}
					if _, ok := porCanal[canal.ID]; !ok {
						orden = append(orden, canal.ID)
					}
					porCanal[canal.ID] = append(porCanal[canal.ID], ch)
				}
				for _, canal := range orden {
					cuerpo, err := json.Marshal(map[string]any{"object": w.Object, "entry": []any{map[string]any{"id": e.ID, "changes": porCanal[canal]}}})
					if err != nil {
						return MetaRecibido{}, err
					}
					id, nuevo, err := m.guardarEntrante(ctx, t, q, canal, cuerpo)
					if err != nil {
						return MetaRecibido{}, err
					}
					if !nuevo {
						duplicados++
						continue
					}
					repartidos++
					if err := m.encolar(ctx, t.Tx, ArgsProcesarEntrante{EntranteID: id}); err != nil {
						return MetaRecibido{}, err
					}
				}
			}
			t.Anotar(map[string]any{"repartidos": repartidos, "descartados": descartados, "duplicados": duplicados})
			return MetaRecibido{Duplicado: repartidos == 0 && duplicados > 0}, nil
		},
	})
}

// canalDeCambio: el número QR al que va un change del webhook de la app (nil = de nadie). Un
// account_update va al primero de su WABA (se aplica a todos los números de la WABA al procesarlo).
func (m *Modulo) canalDeCambio(ctx context.Context, q *mensajeriabd.Queries, entrada string, crudo json.RawMessage) (*mensajeriabd.MensajeriaCanal, error) {
	var ch struct {
		Field string `json:"field"`
		Value struct {
			Metadata struct {
				PhoneNumberID string `json:"phone_number_id"`
			} `json:"metadata"`
			WabaInfo *struct {
				WabaID string `json:"waba_id"`
			} `json:"waba_info"`
		} `json:"value"`
	}
	if json.Unmarshal(crudo, &ch) != nil {
		return nil, nil
	}
	if pid := ch.Value.Metadata.PhoneNumberID; ch.Field != "account_update" {
		if !reDigitos.MatchString(pid) {
			return nil, nil
		}
		fs, err := q.CanalesQRPorPhoneNumber(ctx, pid)
		if err != nil || len(fs) == 0 {
			return nil, err
		}
		return &fs[0], nil
	}
	waba := entrada
	if ch.Value.WabaInfo != nil && reDigitos.MatchString(ch.Value.WabaInfo.WabaID) {
		waba = ch.Value.WabaInfo.WabaID
	}
	if !reDigitos.MatchString(waba) {
		return nil, nil
	}
	fs, err := q.CanalesQRPorWaba(ctx, waba)
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	return &fs[0], nil
}

// EntradaVerificarMetaApp es el GET de verificación del webhook de la app.
type EntradaVerificarMetaApp struct {
	Modo      string `json:"modo"`
	Token     string `json:"-"`
	Challenge string `json:"-"`
}

func nuevoVerificarMetaApp(m *Modulo) *auditoria.Caso[EntradaVerificarMetaApp, string] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerificarMetaApp, string]{
		Accion: "mensajeria.verificar_webhook_app", Acceso: auditoria.Publico, AuditarError: noAuditar401,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerificarMetaApp) (string, error) {
			prohibido := nucleo.NuevoProblema(http.StatusForbidden, "verificacion_invalida", "Verificación inválida", "hub.verify_token no corresponde a la conexión")
			q := mensajeriabd.New(t)
			c, err := m.secretoWebhookApp(ctx, q)
			if err != nil {
				return "", err
			}
			if in.Modo != "subscribe" || c.verifyToken == "" || subtle.ConstantTimeCompare([]byte(in.Token), []byte(c.verifyToken)) != 1 {
				return "", prohibido
			}
			t.FijarEntidad("conexion_meta", 1)
			t.FijarEmpresa(0)
			if c.fila != nil {
				ahora := m.ahora()
				if err := q.MarcarConexionWebhookVerificado(ctx, &ahora); err != nil {
					return "", err
				}
			}
			return in.Challenge, nil
		},
	})
}
