package mensajeria

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/equipos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Asistente de IA con los equipos del cliente (contrato argosisp.md 0.9.0 §12.3 y §12.4, pedido B14).
// Las acciones ver_equipo y cambiar_wifi vienen apagadas de fábrica; cada empresa las enciende en el
// perfil del asistente. El modelo:
//   - lee el estado del equipo (estado_del_equipo) sin serie, MAC, modelo, firmware ni datos de red;
//   - nunca pide ni recibe la clave Wi-Fi: con el cliente verificado en la conversación (datos del
//     titular, §7.5, de menos de 10 min), enlace_wifi crea un enlace de un solo uso (10 min, ligado a
//     la conversación, al servicio y a esa verificación) que el sistema pone en la marca {{enlace_wifi}};
//     el cliente escribe el SSID y la clave en un formulario de Iris y de ahí sale la llamada a Argos;
//   - una clave escrita en el chat se tapa (***) antes del modelo (RedactarParaIA) y del historial
//     (TaparClaveWifi al guardar el mensaje), y no se usa.

// Herramientas de la 0.9.0.
const (
	HerrEstadoEquipo = "estado_del_equipo"
	HerrEnlaceWifi   = "enlace_wifi"
)

// Acciones del perfil (apagadas de fábrica).
const (
	AccionVerEquipo   = "ver_equipo"
	AccionCambiarWifi = "cambiar_wifi"
)

// Los tokens de los enlaces públicos nunca salen en los logs HTTP (nucleo.EnmascararRuta).
func init() {
	nucleo.RegistrarRutaConSecreto(Prefijo + "/publico/wifi/{token}")
	nucleo.RegistrarRutaConSecreto(Prefijo + "/publico/manuales/{token}")
}

// Límite por IP del formulario público del enlace (un token es de 256 bits; esto frena el ruido).
const (
	MaxEnlaceWifiPorIP  = 20
	VentanaEnlaceWifiIP = 10 * time.Minute
)

// limiteIP cuenta las peticiones por IP en una ventana (solo en memoria, por proceso).
type limiteIP struct {
	mu   sync.Mutex
	vist map[string][]time.Time
}

func (l *limiteIP) permitir(ip string, ahora time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.vist == nil {
		l.vist = map[string][]time.Time{}
	}
	desde := ahora.Add(-VentanaEnlaceWifiIP)
	v := l.vist[ip][:0]
	for _, t := range l.vist[ip] {
		if t.After(desde) {
			v = append(v, t)
		}
	}
	if len(v) >= MaxEnlaceWifiPorIP {
		l.vist[ip] = v
		return false
	}
	l.vist[ip] = append(v, ahora)
	if len(l.vist) > 10000 {
		l.vist = map[string][]time.Time{}
	}
	return true
}

var limiteEnlaceWifi limiteIP

// VigenciaEnlaceWifi: el enlace dura 10 min; la verificación también (§12.4).
const VigenciaEnlaceWifi = 10 * time.Minute

// ArgosEquipo: el estado del equipo en Argos (§12.3). Opcional: sin él no hay estado_del_equipo.
type ArgosEquipo interface {
	LeerEquipo(ctx context.Context, servicio int64, actor string, asistente bool) (argos.EstadoEquipo, error)
}

var reMarcaEnlaceWifi = regexp.MustCompile(`\{\{\s*enlace_wifi\s*\}\}`)

// reClaveWifi: «clave/contraseña … wifi/red/internet … <clave>» o «wifi … clave … <clave>». Se tapa
// la palabra que sigue (8 a 63 caracteres sin espacios: lo que acepta WPA2).
var reClaveWifi = regexp.MustCompile(`(?i)\b(?:(?:clave|contrase(?:ñ|n)a|password|passwd|pass)\b[^.\n?!]{0,30}?\b(?:wi-?fi|red|internet|modem|módem|router)|(?:wi-?fi|red|internet)\b[^.\n?!]{0,30}?\b(?:clave|contrase(?:ñ|n)a|password|passwd|pass))\b(?:\s+(?:es|sea|será|sera|nueva|ahora|quiero|que|sea|pon|ponle|a))*\s*[:=]?\s*["'«]?(\S{8,63})`)

// reClaveSuelta: con cambiar_wifi encendido, también «clave|contraseña … <8 a 63 caracteres>» sin
// nombrar el Wi-Fi (el cliente contesta a «¿qué clave quieres?»).
var reClaveSuelta = regexp.MustCompile(`(?i)\b(?:clave|contrase(?:ñ|n)a|password|passwd|pass)\b(?:\s+(?:es|sea|será|sera|nueva|ahora|quiero|que|pon|ponle|a|de|mi))*\s*[:=]?\s*["'«]?(\S{8,63})`)

// TaparClaveWifi tapa una clave Wi-Fi escrita en el chat (§12.4): el mensaje guardado nunca la lleva.
func TaparClaveWifi(s string) string { return taparCon(s, reClaveWifi) }

// TaparClaveWifiAmplio: TaparClaveWifi y, además, cualquier «clave: …» de 8 a 63 caracteres.
func TaparClaveWifiAmplio(s string) string { return taparCon(taparCon(s, reClaveWifi), reClaveSuelta) }

func taparCon(s string, re *regexp.Regexp) string {
	idx := re.FindAllStringSubmatchIndex(s, -1)
	if len(idx) == 0 {
		return s
	}
	var b strings.Builder
	ult := 0
	for _, g := range idx {
		b.WriteString(s[ult:g[2]])
		b.WriteString(auditoria.Mascara)
		ult = g[3]
	}
	b.WriteString(s[ult:])
	return b.String()
}

// herramientasEquipos agrega las del bloque E si su acción está encendida (herramientasDe).
func (m *Modulo) herramientasEquipos(t *turnoIA) []HerramientaIA {
	var hs []HerramientaIA
	if t.clienteID == 0 || t.servicioID == nil || m.cfg.Argos == nil {
		return nil
	}
	if _, ok := m.cfg.Argos.(ArgosEquipo); ok && t.acciones[AccionVerEquipo] {
		hs = append(hs, HerramientaIA{Nombre: HerrEstadoEquipo,
			Descripcion: "Lee si el equipo del cliente (módem u ONT) está en línea y su señal. No devuelve la serie, la MAC ni datos de red."})
	}
	if t.acciones[AccionCambiarWifi] {
		hs = append(hs, HerramientaIA{Nombre: HerrEnlaceWifi,
			Descripcion: "Para cambiar el nombre o la clave del Wi-Fi: crea un enlace de un solo uso (10 minutos) donde el cliente los escribe. Exige la identidad verificada con verificar_identidad. Nunca pidas ni repitas la clave: escribe la marca {{enlace_wifi}} donde va el enlace y el sistema la pone."})
	}
	return hs
}

// herrEstadoEquipo: §12.3 con X-Iris-Actor: asistente_ia; al modelo solo en línea, señal y último contacto.
func (m *Modulo) herrEstadoEquipo(ctx context.Context, t *turnoIA, hecha accionHecha) (resHerramienta, accionHecha) {
	ae, ok := m.cfg.Argos.(ArgosEquipo)
	if !ok || t.servicioID == nil {
		return rechazo("la conversación no tiene un servicio con equipo."), hecha
	}
	if !t.verificado {
		return rechazo("primero verifica la identidad del cliente (verificar_identidad)."), hecha
	}
	e, err := ae.LeerEquipo(ctx, *t.servicioID, equipos.ActorAsistente, true)
	if err != nil {
		hecha.Resultado = "fallo"
		return resHerramienta{contenido: "No se pudo leer el equipo ahora (Argos o el equipo no respondieron). Ofrece intentarlo más tarde o pasar a una persona."}, hecha
	}
	hecha.Resultado, hecha.Entidad = "ok", "servicio"
	var b strings.Builder
	if e.EnLinea {
		b.WriteString("El equipo está en línea.")
	} else {
		b.WriteString("El equipo no está en línea.")
	}
	if v, ok := e.Senal["rx_dbm"]; ok {
		b.WriteString(" Señal óptica recibida: " + formatoFloat(v) + " dBm.")
	}
	if v, ok := e.Senal["rssi_dbm"]; ok {
		b.WriteString(" Señal inalámbrica: " + formatoFloat(v) + " dBm.")
	}
	if e.UltimoContacto != nil {
		b.WriteString(" Último contacto: " + e.UltimoContacto.In(zonaPlantillas).Format("02/01 15:04") + ".")
	}
	return resHerramienta{contenido: b.String()}, hecha
}

func formatoFloat(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// herrEnlaceWifi crea el enlace de un solo uso (§12.4). Exige una verificación que coincidió en esta
// conversación, para el cliente asociado, de menos de 10 min y sin otro enlace.
func (m *Modulo) herrEnlaceWifi(ctx context.Context, t *turnoIA, hecha accionHecha) (resHerramienta, accionHecha) {
	if t.servicioID == nil || t.clienteID == 0 {
		return rechazo("la conversación no tiene un cliente y un servicio asociados."), hecha
	}
	if strings.TrimSpace(m.cfg.BaseURL) == "" {
		hecha.Resultado = "fallo"
		return resHerramienta{fallo: true}, hecha
	}
	ahora := m.ahora()
	var url string
	err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := mensajeriabd.New(tx)
		v, err := q.VerificacionParaWifi(ctx, mensajeriabd.VerificacionParaWifiParams{ConversacionID: t.conv, ClienteID: t.clienteID,
			Desde: ahora.Add(-VigenciaEnlaceWifi)})
		if err != nil {
			return err
		}
		crudo := make([]byte, 32)
		if _, err := rand.Read(crudo); err != nil {
			return err
		}
		token := base64.RawURLEncoding.EncodeToString(crudo)
		huella := sha256.Sum256([]byte(token))
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if m.cfg.Cifrador == nil {
			return errors.New("sin IRIS_CLAVE_CIFRADO")
		}
		cifrado, err := m.cfg.Cifrador.Cifrar([]byte(token), datosEnlace(id))
		if err != nil {
			return err
		}
		if err := q.PurgarTokensEnlaceWifi(ctx, ahora); err != nil {
			return err
		}
		if _, err := q.CrearEnlaceWifi(ctx, mensajeriabd.CrearEnlaceWifiParams{ID: id, Huella: huella[:], EmpresaID: &t.empresa,
			ConversacionID: t.conv, ClienteID: t.clienteID, ServicioID: *t.servicioID, VerificacionID: v.ID,
			Metodo: equipos.MetodoDatosTitular, VerificadoEn: v.CreadaEn, VenceEn: ahora.Add(VigenciaEnlaceWifi), TokenCifrado: cifrado}); err != nil {
			return err
		}
		// El mensaje guardado (historial, bandeja, modelo) lleva el enlace tapado; el token se pone solo
		// al mandarlo a Meta (conTokenEnlace).
		url = strings.TrimRight(m.cfg.BaseURL, "/") + EnlaceWifiTapado
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return rechazo("primero verifica la identidad del cliente con verificar_identidad (vale 10 minutos y para un solo cambio)."), hecha
	}
	if err != nil {
		hecha.Resultado = "fallo"
		return resHerramienta{fallo: true}, hecha
	}
	t.enlaceWifi = &url
	hecha.Resultado, hecha.Entidad = "ok", "servicio"
	return resHerramienta{contenido: "Enlace listo (vence en 10 minutos, un solo uso). Escribe la marca {{enlace_wifi}} donde va el enlace; el cliente pone ahí el nombre y la clave nuevos. Nunca le pidas la clave por el chat."}, hecha
}

// conEnlaceWifi pone el enlace en la marca (solo si este turno lo creó; una sola vez).
func conEnlaceWifi(t *turnoIA, texto string) (string, bool) {
	n := len(reMarcaEnlaceWifi.FindAllStringIndex(texto, -1))
	if n == 0 {
		return texto, true
	}
	if n > 1 || t.enlaceWifi == nil {
		return "", false
	}
	return reMarcaEnlaceWifi.ReplaceAllLiteralString(texto, *t.enlaceWifi), true
}

// --- Formulario público del enlace ---------------------------------------------------------------

// EntradaWifiPublico es lo que manda el formulario del enlace (sin sesión: el token es la credencial).
type EntradaWifiPublico struct {
	Token string  `json:"-"`
	SSID  *string `json:"ssid,omitempty" minLength:"1" maxLength:"32" auditoria:"sensible" doc:"Nombre nuevo del Wi-Fi"`
	Clave *string `json:"clave,omitempty" minLength:"8" maxLength:"63" auditoria:"sensible" doc:"Clave nueva (8 a 63). Nunca se devuelve ni queda en Iris"`
}

// EnlaceWifiPublico es GET del enlace: si sirve y hasta cuándo (sin datos del cliente).
type EnlaceWifiPublico struct {
	Vigente bool       `json:"vigente"`
	VenceEn *time.Time `json:"venceEn,omitempty"`
}

// WifiPedido es la respuesta del formulario.
type WifiPedido struct {
	Estado string `json:"estado" enum:"en_proceso" doc:"Argos lo aplica en el equipo; el cliente recibe la confirmación por sus contactos"`
}

func enlaceVencido() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusGone, "enlace_vencido", "Enlace vencido", "Este enlace ya se usó o venció. Pide otro por el chat").
		ConCabecera("Cache-Control", "no-store").ConCabecera("Referrer-Policy", "no-referrer")
}

// noAuditarVencido: un 410 de un token inexistente lo provoca cualquiera; no deja fila.
func noAuditarVencido(err error) bool {
	var p *nucleo.Problema
	return !errors.As(err, &p) || p.Codigo() != "enlace_vencido"
}

// salidaEnlace: las respuestas del enlace no se guardan ni pasan el token como Referer.
type salidaEnlace[T any] struct {
	CacheControl   string `header:"Cache-Control"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           T
}

func conCabecerasEnlace[T any](v T) *salidaEnlace[T] {
	return &salidaEnlace[T]{CacheControl: "no-store", ReferrerPolicy: "no-referrer", Body: v}
}

// entradaToken: el token de la ruta y la IP de quien llama (límite por IP).
type entradaToken struct {
	Token string `path:"token" minLength:"40" maxLength:"60"`
	ip    string
}

// Resolve toma la IP remota (huma.Resolver).
func (e *entradaToken) Resolve(ctx huma.Context) []error {
	e.ip = ctx.RemoteAddr()
	if h, _, ok := strings.Cut(e.ip, ":"); ok && !strings.Contains(h, "[") {
		e.ip = h
	}
	return nil
}

// entradaUsarEnlace: el token, la IP y lo que escribió el cliente.
type entradaUsarEnlace struct {
	Token string `path:"token" minLength:"40" maxLength:"60"`
	Body  EntradaWifiPublico
	ip    string
}

// Resolve toma la IP remota (huma.Resolver).
func (e *entradaUsarEnlace) Resolve(ctx huma.Context) []error {
	var t entradaToken
	t.Resolve(ctx)
	e.ip = t.ip
	return nil
}

func demasiadasPeticiones() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusTooManyRequests, "demasiadas_peticiones", "Demasiadas peticiones", "Espera unos minutos").
		ConCabecera("Retry-After", "600").ConCabecera("Cache-Control", "no-store")
}

func huellaToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func (m *Modulo) nuevoVerEnlaceWifi() *auditoria.Caso[string, EnlaceWifiPublico] {
	return auditoria.Nuevo(auditoria.Caso[string, EnlaceWifiPublico]{
		Accion: "servicios.ver_enlace_wifi", Acceso: auditoria.Publico, AuditarExito: func(EnlaceWifiPublico) bool { return false },
		AuditarError: noAuditarVencido,
		Logica: func(ctx context.Context, t *auditoria.Tx, token string) (EnlaceWifiPublico, error) {
			t.FijarEmpresa(0)
			e, err := mensajeriabd.New(t).EnlaceWifiVigente(ctx, mensajeriabd.EnlaceWifiVigenteParams{Huella: huellaToken(token), Ahora: m.ahora()})
			if errors.Is(err, pgx.ErrNoRows) {
				return EnlaceWifiPublico{}, nil
			}
			if err != nil {
				return EnlaceWifiPublico{}, err
			}
			return EnlaceWifiPublico{Vigente: true, VenceEn: &e.VenceEn}, nil
		},
	})
}

// nuevoUsarEnlaceWifi: el formulario manda el cambio. El enlace se consume (un solo uso, aunque Argos
// falle después: el cambio queda en la cola), solo si sigue vigente y la conversación sigue con el
// mismo cliente; la llamada sale con X-Iris-Actor: asistente_ia, X-Iris-Conversacion y la verificación.
func (m *Modulo) nuevoUsarEnlaceWifi() *auditoria.Caso[EntradaWifiPublico, WifiPedido] {
	return auditoria.Nuevo(auditoria.Caso[EntradaWifiPublico, WifiPedido]{
		Accion: "servicios.cambiar_wifi", Acceso: auditoria.Publico, AuditarError: noAuditarVencido,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaWifiPublico) (WifiPedido, error) {
			t.FijarEmpresa(0)
			if in.SSID == nil && in.Clave == nil {
				return WifiPedido{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "ssid", Message: "Escribe el nombre o la clave nueva", Rule: "required"})
			}
			if in.SSID != nil {
				if err := equipos.ValidarSSID("ssid", *in.SSID); err != nil {
					return WifiPedido{}, err
				}
			}
			if in.Clave != nil {
				if err := equipos.ValidarClave("clave", *in.Clave); err != nil {
					return WifiPedido{}, err
				}
			}
			q := mensajeriabd.New(t)
			ahora := m.ahora()
			e, err := q.UsarEnlaceWifi(ctx, mensajeriabd.UsarEnlaceWifiParams{Huella: huellaToken(in.Token), Ahora: &ahora})
			if errors.Is(err, pgx.ErrNoRows) {
				return WifiPedido{}, enlaceVencido()
			}
			if err != nil {
				return WifiPedido{}, err
			}
			t.FijarEntidad("conversacion", e.ConversacionID)
			if e.EmpresaID != nil {
				t.FijarEmpresa(*e.EmpresaID)
			}
			// La verificación caduca si cambió el cliente asociado a la conversación.
			cliente, err := q.ClienteDeConversacion(ctx, e.ConversacionID)
			if err != nil {
				return WifiPedido{}, err
			}
			if cliente == nil || *cliente != e.ClienteID || ahora.Sub(e.VerificadoEn) > VigenciaEnlaceWifi {
				return WifiPedido{}, enlaceVencido()
			}
			c, err := equipos.Encolar(ctx, t, m.cfg.Cifrador, equipos.Pedido{
				EmpresaID: e.EmpresaID, ServicioID: e.ServicioID, ClienteID: e.ClienteID, OrigenTipo: equipos.OrigenConversacion,
				OrigenID: e.ConversacionID, Actor: equipos.ActorAsistente, Metodo: e.Metodo, VerificadoEn: e.VerificadoEn,
				VerificacionID: &e.VerificacionID, EnlaceID: &e.ID, RevalidarServicio: true,
				SSID: in.SSID, Clave: in.Clave, Ahora: ahora, Zona: zonaPlantillas,
			}, pendientes.Encolador(m.cfg.Encolar))
			if err != nil {
				return WifiPedido{}, err
			}
			if err := q.FijarCambioEnlaceWifi(ctx, mensajeriabd.FijarCambioEnlaceWifiParams{ID: e.ID, CambioID: pgtype.UUID{Bytes: c.ID, Valid: true}}); err != nil {
				return WifiPedido{}, err
			}
			t.Anotar(map[string]any{"enlace_wifi": e.ID, "cambio_id": c.ID})
			return WifiPedido{Estado: equipos.EstadoEnProceso}, nil
		},
	})
}

func (m *Modulo) registrarEnlaceWifi(api huma.API) {
	ver := op("verEnlaceWifi", http.MethodGet, "/publico/wifi/{token}", "Enlace de un solo uso para cambiar el Wi-Fi", "", http.StatusOK)
	ver.Security = nil
	ver.Description = "Sin sesión: el token es la credencial (256 bits; Iris guarda solo su huella). Dice si el enlace sigue vigente (10 min, un solo uso)."
	huma.Register(api, ver, func(ctx context.Context, in *entradaToken) (*salidaEnlace[EnlaceWifiPublico], error) {
		if !limiteEnlaceWifi.permitir(in.ip, time.Now()) {
			return nil, demasiadasPeticiones()
		}
		r, err := m.VerEnlaceWifi.Ejecutar(ctx, in.Token)
		if err != nil {
			return nil, err
		}
		return conCabecerasEnlace(r), nil
	})
	usar := op("usarEnlaceWifi", http.MethodPost, "/publico/wifi/{token}", "Cambiar el Wi-Fi con el enlace", "", http.StatusAccepted, http.StatusGone,
		http.StatusTooManyRequests)
	usar.Security = nil
	usar.Description = "El cliente escribe el nombre o la clave nuevos (al menos uno). Consume el enlace (un solo uso) y manda el cambio a Argos " +
		"(POST /servicios/:id/wifi con X-Iris-Actor: asistente_ia y la verificación de la conversación). 410 `enlace_vencido` (usado, vencido o el " +
		"cliente de la conversación cambió); 429 `demasiados_cambios_wifi`. La clave nunca se guarda ni se devuelve."
	huma.Register(api, usar, func(ctx context.Context, in *entradaUsarEnlace) (*salidaEnlace[WifiPedido], error) {
		if !limiteEnlaceWifi.permitir(in.ip, time.Now()) {
			return nil, demasiadasPeticiones()
		}
		e := in.Body
		e.Token = in.Token
		r, err := m.UsarEnlaceWifi.Ejecutar(contexto.ConCanal(ctx, contexto.CanalPortal), e)
		if err != nil {
			return nil, err
		}
		return conCabecerasEnlace(r), nil
	})
}

// EnlaceWifiTapado: cómo queda el enlace en el mensaje guardado.
const EnlaceWifiTapado = "/wifi/" + auditoria.Mascara

func datosEnlace(id uuid.UUID) []byte { return []byte("enlace_wifi:" + id.String()) }

// conTokenEnlace pone el token del último enlace vigente de la conversación en el texto que sale a
// Meta (nunca en el mensaje guardado). Sin enlace vigente, el texto sale tapado.
func (m *Modulo) conTokenEnlace(ctx context.Context, q *mensajeriabd.Queries, conv int64, texto *string, ahora time.Time) (*string, error) {
	if texto == nil || !strings.Contains(*texto, EnlaceWifiTapado) || m.cfg.Cifrador == nil {
		return texto, nil
	}
	f, err := q.TokenEnlaceWifiParaEnviar(ctx, mensajeriabd.TokenEnlaceWifiParaEnviarParams{ConversacionID: conv, Ahora: ahora})
	if errors.Is(err, pgx.ErrNoRows) {
		return texto, nil
	}
	if err != nil {
		return nil, err
	}
	token, err := m.cfg.Cifrador.Descifrar(f.TokenCifrado, datosEnlace(f.ID))
	if err != nil {
		return nil, err
	}
	v := strings.Replace(*texto, EnlaceWifiTapado, "/wifi/"+string(token), 1)
	return &v, nil
}

// tapadorWifi para el payload crudo de Meta (taparEntranteMetaCon).
func tapadorWifi(amplio bool) func(string) (string, bool) {
	return func(s string) (string, bool) {
		t := TaparClaveWifi(s)
		if amplio {
			t = TaparClaveWifiAmplio(s)
		}
		return t, t != s
	}
}

// wifiEncendido: algún perfil del asistente de la empresa tiene cambiar_wifi.
func wifiEncendido(ctx context.Context, q *mensajeriabd.Queries, empresa *int64) bool {
	if empresa == nil {
		return false
	}
	aj, err := leerAjustesAsistente(ctx, q, *empresa)
	if err != nil || aj == nil {
		return false
	}
	for _, p := range perfilesDe(aj.Perfiles) {
		if p.Acciones.CambiarWifi {
			return true
		}
	}
	return false
}

// taparWifiEntrante tapa la clave en el texto del mensaje que llega y, si tapó algo, también en el
// payload crudo de Meta (mensajeria.entrantes).
func taparWifiEntrante(ctx context.Context, q *mensajeriabd.Queries, canal int64, wamid string, empresa *int64, texto *string) (*string, error) {
	if texto == nil {
		return nil, nil
	}
	amplio := wifiEncendido(ctx, q, empresa)
	t, cambio := tapadorWifi(amplio)(*texto)
	if !cambio {
		return texto, nil
	}
	return &t, taparEntranteMetaCon(ctx, q, canal, wamid, tapadorWifi(amplio))
}
