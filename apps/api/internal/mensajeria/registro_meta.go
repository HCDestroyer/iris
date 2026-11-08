package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Registro de un número con QR (Embedded Signup con coexistencia, contratos/conversaciones.md
// §15.2). RegistroMeta es lo que Iris necesita de Meta para conectar y sincronizar un número; lo
// implementa la app propia (Graph API con el app secret de la instalación) y, en el modo
// compartida, el servicio central «Iris Cloud» (aún no existe: responde siempre
// servicio_central_no_disponible). Ningún método devuelve en un error el código, el token ni el
// mensaje de Meta.

// RegistroMeta habla con Meta (o con el servicio central) para el registro con QR.
type RegistroMeta interface {
	// CanjearCodigo cambia el código de Embedded Signup (vive ~30 s) por el token del negocio.
	CanjearCodigo(ctx context.Context, codigo string) (string, error)
	// ConsultarNumero lee el número con el token del negocio y comprueba que es de la WABA.
	ConsultarNumero(ctx context.Context, token, wabaID, phoneNumberID string) (NumeroRegistrado, error)
	// SuscribirWABA suscribe la app a los webhooks de la WABA (alterno: URL propia del número).
	SuscribirWABA(ctx context.Context, token, wabaID string, alterno *WebhookAlterno) error
	// PedirSincronizacion pide a Meta los contactos (smb_app_state_sync) o el historial (history).
	PedirSincronizacion(ctx context.Context, token, phoneNumberID, tipo string) (string, error)
}

// NumeroRegistrado es lo que Meta dice del número.
type NumeroRegistrado struct {
	Numero       string // E.164
	NombreMeta   string
	EnAppNegocio *bool // is_on_biz_app (nil = Meta no lo dijo)
}

// WebhookAlterno es la URL de webhook propia de un número (override_callback_uri).
type WebhookAlterno struct {
	URL         string
	VerifyToken string
}

// Tipos de sincronización de smb_app_data.
const (
	SyncContactos = "smb_app_state_sync"
	SyncHistorial = "history"
)

// Códigos de error del registro con QR.
const (
	ErrQRCodigoVencido        = "codigo_vencido"
	ErrQRAppNoAprobada        = "app_no_aprobada"
	ErrQRNumeroNoElegible     = "numero_no_elegible"
	ErrQRErrorMeta            = "error_meta"
	ErrQRServicioNoDisponible = "servicio_central_no_disponible"
	detalleServicioCentral    = "El servicio central de Horus (Iris Cloud) todavía no está disponible: usa tu propia app de Meta o conecta el número con los datos de la API"
	detalleNumeroNoElegible   = "Meta dice que este número no se puede conectar con QR (debe estar en la app WhatsApp Business 2.24.17 o más reciente y no estar en otra plataforma)"
	detalleAppNoAprobada      = "La app de Meta de esta instalación no tiene permiso para el registro con QR (debe ser Tech Provider aprobado con acceso avanzado)"
	detalleCodigoVencido      = "El código de Meta venció o ya se usó: vuelve a abrir la ventana de Meta"
	tiempoLlamadaRegistroMeta = 15 * time.Second
	limiteRespuestaRegistroQR = 64 << 10
)

// ErrorRegistro es un rechazo de Meta (o del servicio central) traducido a un código de Iris.
type ErrorRegistro struct {
	Codigo  string
	Detalle string
}

func (e *ErrorRegistro) Error() string { return "registro QR: " + e.Codigo + ": " + e.Detalle }

// errorGraphQR es el error de la Graph API (solo números y textos cortos que se pueden mostrar).
type errorGraphQR struct {
	Code      int    `json:"code"`
	Subcode   int    `json:"error_subcode"`
	Type      string `json:"type"`
	FBTraceID string `json:"fbtrace_id"`
}

var reCortoSeguro = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,40}$`)

// detalleErrorMeta arma «Código N · Tipo · fbtrace X» sin copiar el mensaje de Meta.
func detalleErrorMeta(status int, e *errorGraphQR) string {
	d := fmt.Sprintf("Meta respondió con un error (HTTP %d", status)
	if e != nil {
		d += fmt.Sprintf(" · Código %d", e.Code)
		if reCortoSeguro.MatchString(e.Type) {
			d += " · " + e.Type
		}
		if reCortoSeguro.MatchString(e.FBTraceID) {
			d += " · fbtrace " + e.FBTraceID
		}
	}
	return d + "). Puede ser algo pasajero: espera un minuto y vuelve a intentar"
}

// registroServicioCentral es el modo compartida: hasta que exista Iris Cloud, nada se puede hacer.
type registroServicioCentral struct{}

func (registroServicioCentral) CanjearCodigo(context.Context, string) (string, error) {
	return "", &ErrorRegistro{Codigo: ErrQRServicioNoDisponible, Detalle: detalleServicioCentral}
}

func (registroServicioCentral) ConsultarNumero(context.Context, string, string, string) (NumeroRegistrado, error) {
	return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRServicioNoDisponible, Detalle: detalleServicioCentral}
}

func (registroServicioCentral) SuscribirWABA(context.Context, string, string, *WebhookAlterno) error {
	return &ErrorRegistro{Codigo: ErrQRServicioNoDisponible, Detalle: detalleServicioCentral}
}

func (registroServicioCentral) PedirSincronizacion(context.Context, string, string, string) (string, error) {
	return "", &ErrorRegistro{Codigo: ErrQRServicioNoDisponible, Detalle: detalleServicioCentral}
}

// registroGraph es la app propia de la instalación (o la del entorno en saas).
type registroGraph struct {
	m         *Modulo
	base      string // host + versión
	appID     string
	appSecret string
}

// registroDe elige la implementación según el modo de la conexión.
func (m *Modulo) registroDe(c conexionEfectiva) RegistroMeta {
	if c.modo == ModoMetaCompartida {
		if m.cfg.ServicioCentral != nil {
			return m.cfg.ServicioCentral
		}
		return registroServicioCentral{}
	}
	return &registroGraph{m: m, base: m.hostGraph() + "/" + c.graphVersion, appID: c.appID, appSecret: c.appSecret}
}

// pedir llama a la Graph API. El error de red nunca se devuelve (su texto lleva la URL).
func (r *registroGraph) pedir(ctx context.Context, metodo, ruta string, token string, cuerpo any) (int, []byte, error) {
	ctx, cancelar := context.WithTimeout(ctx, tiempoLlamadaRegistroMeta)
	defer cancelar()
	var b io.Reader
	if cuerpo != nil {
		crudo, err := json.Marshal(cuerpo)
		if err != nil {
			return 0, nil, err
		}
		b = bytes.NewReader(crudo)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, r.base+ruta, b)
	if err != nil {
		return 0, nil, &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: "No se pudo armar la llamada a Meta"}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Iris/"+r.m.cfg.Version)
	resp, err := r.m.cfg.HTTPMeta.Do(req)
	if err != nil {
		return 0, nil, &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: "Meta no respondió (tiempo agotado o sin conexión); espera un minuto y vuelve a intentar"}
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, limiteRespuestaRegistroQR))
	return resp.StatusCode, leido, nil
}

// pedirForm hace un POST application/x-www-form-urlencoded (sin token en la cabecera).
func (r *registroGraph) pedirForm(ctx context.Context, ruta string, form url.Values) (int, []byte, error) {
	ctx, cancelar := context.WithTimeout(ctx, tiempoLlamadaRegistroMeta)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+ruta, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: "No se pudo armar la llamada a Meta"}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Iris/"+r.m.cfg.Version)
	resp, err := r.m.cfg.HTTPMeta.Do(req)
	if err != nil {
		return 0, nil, &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: "Meta no respondió (tiempo agotado o sin conexión); espera un minuto y vuelve a intentar"}
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, limiteRespuestaRegistroQR))
	return resp.StatusCode, leido, nil
}

func errorDe(cuerpo []byte) *errorGraphQR {
	var r struct {
		Error *errorGraphQR `json:"error"`
	}
	if json.Unmarshal(cuerpo, &r) != nil {
		return nil
	}
	return r.Error
}

func codigoDe(e *errorGraphQR) (int, int) {
	if e == nil {
		return 0, 0
	}
	return e.Code, e.Subcode
}

// sinPermisoApp: los códigos de Meta de «la app no puede» (permisos, capacidad, secreto).
func sinPermisoApp(status, codigo int) bool {
	return status == http.StatusForbidden || codigo == 3 || codigo == 10 || codigo == 101 || (codigo >= 200 && codigo <= 299)
}

func (r *registroGraph) CanjearCodigo(ctx context.Context, codigo string) (string, error) {
	// POST con cuerpo form: el secreto y el código nunca van en la URL (logs de proxies).
	form := url.Values{"client_id": {r.appID}, "client_secret": {r.appSecret}, "code": {codigo}}
	status, cuerpo, err := r.pedirForm(ctx, "/oauth/access_token", form)
	if err != nil {
		return "", err
	}
	var ok struct {
		AccessToken string `json:"access_token"`
	}
	if status/100 == 2 && json.Unmarshal(cuerpo, &ok) == nil && ok.AccessToken != "" {
		return ok.AccessToken, nil
	}
	e := errorDe(cuerpo)
	c, _ := codigoDe(e)
	switch {
	case sinPermisoApp(status, c):
		return "", &ErrorRegistro{Codigo: ErrQRAppNoAprobada, Detalle: detalleAppNoAprobada}
	case c == 100 || c == 190 || (status == http.StatusBadRequest && e == nil):
		return "", &ErrorRegistro{Codigo: ErrQRCodigoVencido, Detalle: detalleCodigoVencido}
	}
	return "", &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: detalleErrorMeta(status, e)}
}

func (r *registroGraph) ConsultarNumero(ctx context.Context, token, wabaID, phoneNumberID string) (NumeroRegistrado, error) {
	// El número debe ser de la WABA que dijo la ventana de Meta (GET /{waba_id}/phone_numbers).
	status, cuerpo, err := r.pedir(ctx, http.MethodGet, "/"+url.PathEscape(wabaID)+"/phone_numbers?fields=id&limit=100", token, nil)
	if err != nil {
		return NumeroRegistrado{}, err
	}
	var lista struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if status/100 != 2 || json.Unmarshal(cuerpo, &lista) != nil {
		e := errorDe(cuerpo)
		c, _ := codigoDe(e)
		if sinPermisoApp(status, c) {
			return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRAppNoAprobada, Detalle: detalleAppNoAprobada}
		}
		return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: detalleErrorMeta(status, e)}
	}
	deLaWABA := false
	for _, n := range lista.Data {
		deLaWABA = deLaWABA || n.ID == phoneNumberID
	}
	if !deLaWABA {
		return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRNumeroNoElegible, Detalle: "Ese número no es de la cuenta de WhatsApp Business que se conectó en la ventana de Meta"}
	}
	status, cuerpo, err = r.pedir(ctx, http.MethodGet, "/"+url.PathEscape(phoneNumberID)+"?fields=display_phone_number,verified_name,is_on_biz_app,platform_type", token, nil)
	if err != nil {
		return NumeroRegistrado{}, err
	}
	var n struct {
		ID                 string `json:"id"`
		DisplayPhoneNumber string `json:"display_phone_number"`
		VerifiedName       string `json:"verified_name"`
		IsOnBizApp         *bool  `json:"is_on_biz_app"`
	}
	if status/100 == 2 && json.Unmarshal(cuerpo, &n) == nil {
		digitos := recortar(soloDigitos(n.DisplayPhoneNumber), 15)
		if n.IsOnBizApp != nil && !*n.IsOnBizApp {
			return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRNumeroNoElegible, Detalle: detalleNumeroNoElegible}
		}
		if n.ID != "" && n.ID != phoneNumberID {
			return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRNumeroNoElegible, Detalle: "Meta devolvió otro número para ese phone number ID"}
		}
		if len(digitos) < 8 {
			return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: "Meta no devolvió el número de teléfono de este phone number ID"}
		}
		return NumeroRegistrado{Numero: "+" + digitos, NombreMeta: recortar(strings.TrimSpace(n.VerifiedName), 120), EnAppNegocio: n.IsOnBizApp}, nil
	}
	e := errorDe(cuerpo)
	c, sub := codigoDe(e)
	switch {
	case c == 100 && sub == 33:
		return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRNumeroNoElegible, Detalle: "Meta no da acceso a ese número con el permiso que se otorgó"}
	case sinPermisoApp(status, c):
		return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRAppNoAprobada, Detalle: detalleAppNoAprobada}
	}
	return NumeroRegistrado{}, &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: detalleErrorMeta(status, e)}
}

func (r *registroGraph) SuscribirWABA(ctx context.Context, token, wabaID string, alterno *WebhookAlterno) error {
	var cuerpo any
	if alterno != nil {
		cuerpo = map[string]string{"override_callback_uri": alterno.URL, "verify_token": alterno.VerifyToken}
	}
	status, crudo, err := r.pedir(ctx, http.MethodPost, "/"+url.PathEscape(wabaID)+"/subscribed_apps", token, cuerpo)
	if err != nil {
		return err
	}
	var ok struct {
		Success bool `json:"success"`
	}
	if status/100 == 2 && json.Unmarshal(crudo, &ok) == nil && ok.Success {
		return nil
	}
	e := errorDe(crudo)
	c, _ := codigoDe(e)
	if sinPermisoApp(status, c) {
		return &ErrorRegistro{Codigo: ErrQRAppNoAprobada, Detalle: detalleAppNoAprobada}
	}
	return &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: detalleErrorMeta(status, e)}
}

func (r *registroGraph) PedirSincronizacion(ctx context.Context, token, phoneNumberID, tipo string) (string, error) {
	status, crudo, err := r.pedir(ctx, http.MethodPost, "/"+url.PathEscape(phoneNumberID)+"/smb_app_data", token,
		map[string]string{"messaging_product": "whatsapp", "sync_type": tipo})
	if err != nil {
		return "", err
	}
	var ok struct {
		RequestID string `json:"request_id"`
	}
	if status/100 == 2 && json.Unmarshal(crudo, &ok) == nil {
		return recortar(ok.RequestID, 120), nil
	}
	e := errorDe(crudo)
	c, _ := codigoDe(e)
	if sinPermisoApp(status, c) {
		return "", &ErrorRegistro{Codigo: ErrQRAppNoAprobada, Detalle: detalleAppNoAprobada}
	}
	return "", &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: detalleErrorMeta(status, e)}
}
