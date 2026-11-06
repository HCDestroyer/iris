package notificaciones

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cliente mínimo de Firebase Cloud Messaging HTTP v1 con la cuenta de servicio (OAuth 2.0, flujo
// JWT bearer de RFC 7523): sin dependencias nuevas. El token de acceso se guarda en memoria hasta
// un minuto antes de vencer. Nunca registra el token del dispositivo ni el contenido.

// URLFCM es la API de FCM; AlcanceFCM, el permiso OAuth que pide.
const (
	URLFCM     = "https://fcm.googleapis.com"
	AlcanceFCM = "https://www.googleapis.com/auth/firebase.messaging"
)

// CuentaServicio son los campos que se usan del JSON de la cuenta de servicio de Google.
type CuentaServicio struct {
	Tipo         string `json:"type"`
	ProyectoID   string `json:"project_id"`
	ClavePrivada string `json:"private_key"`
	ClaveID      string `json:"private_key_id"`
	Correo       string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// LeerCuentaServicio lee y valida el JSON de la cuenta de servicio.
func LeerCuentaServicio(ruta string) (CuentaServicio, *rsa.PrivateKey, error) {
	b, err := os.ReadFile(ruta) //nolint:gosec // ruta de configuración del operador (IRIS_FCM_CREDENCIALES)
	if err != nil {
		return CuentaServicio{}, nil, fmt.Errorf("no se puede leer el archivo: %w", errors.Unwrap(err))
	}
	return ParsearCuentaServicio(b)
}

// ParsearCuentaServicio valida el JSON de la cuenta de servicio y su clave RSA.
func ParsearCuentaServicio(b []byte) (CuentaServicio, *rsa.PrivateKey, error) {
	var c CuentaServicio
	if err := json.Unmarshal(b, &c); err != nil {
		return c, nil, errors.New("el archivo no es un JSON de cuenta de servicio")
	}
	if c.Tipo != "service_account" || c.Correo == "" || c.ClavePrivada == "" {
		return c, nil, errors.New("el JSON no es de una cuenta de servicio (type, client_email, private_key)")
	}
	if c.TokenURI == "" {
		c.TokenURI = "https://oauth2.googleapis.com/token"
	}
	bloque, _ := pem.Decode([]byte(c.ClavePrivada))
	if bloque == nil {
		return c, nil, errors.New("private_key no es PEM")
	}
	var clave any
	var err error
	if clave, err = x509.ParsePKCS8PrivateKey(bloque.Bytes); err != nil {
		if clave, err = x509.ParsePKCS1PrivateKey(bloque.Bytes); err != nil {
			return c, nil, errors.New("private_key no es una clave RSA válida")
		}
	}
	rsaClave, ok := clave.(*rsa.PrivateKey)
	if !ok {
		return c, nil, errors.New("private_key no es RSA")
	}
	return c, rsaClave, nil
}

// MensajeFCM es el cuerpo de messages:send (solo datos; contratos/push.md §4).
type MensajeFCM struct {
	Token   string            `json:"token"`
	Data    map[string]string `json:"data"`
	Android AndroidFCM        `json:"android"`
}

// AndroidFCM son las opciones de Android del mensaje.
type AndroidFCM struct {
	Prioridad   string `json:"priority"`
	TTL         string `json:"ttl"`
	CollapseKey string `json:"collapse_key,omitempty"`
}

// Emisor manda un mensaje a FCM y devuelve el name del mensaje (projects/…/messages/…).
type Emisor interface {
	Enviar(ctx context.Context, m MensajeFCM) (string, error)
}

// ErrorFCM es la respuesta de error de FCM (o de la red).
type ErrorFCM struct {
	Status     int    // 0 = sin respuesta (red)
	Codigo     string // errorCode de FcmError (UNREGISTERED, …) o el status de Google
	RetryAfter time.Duration
	causa      error
}

func (e *ErrorFCM) Error() string {
	if e.Status == 0 {
		return "fcm: sin respuesta: " + e.causa.Error()
	}
	return fmt.Sprintf("fcm: %d %s", e.Status, e.Codigo)
}

// Reintentable: sin respuesta, 429 o 5xx.
func (e *ErrorFCM) Reintentable() bool {
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// TokenInvalido: FCM dice que el token del dispositivo no sirve; hay que quitarlo.
func (e *ErrorFCM) TokenInvalido() bool {
	if strings.HasPrefix(e.Codigo, "OAUTH") {
		return false // falló el token de acceso del servidor, no el del dispositivo
	}
	switch e.Codigo {
	case "UNREGISTERED", "INVALID_ARGUMENT", "SENDER_ID_MISMATCH":
		return true
	}
	return e.Status == http.StatusNotFound
}

// ClienteFCM implementa Emisor contra FCM HTTP v1.
type ClienteFCM struct {
	proyecto string
	base     string
	cuenta   CuentaServicio
	clave    *rsa.PrivateKey
	http     *http.Client
	ahora    func() time.Time

	mu     sync.Mutex
	acceso string
	vence  time.Time
}

// OpcionesFCM arma un ClienteFCM. URL y HTTP solo cambian en pruebas.
type OpcionesFCM struct {
	Proyecto string
	Cuenta   CuentaServicio
	Clave    *rsa.PrivateKey
	URL      string
	HTTP     *http.Client
	Ahora    func() time.Time
}

// NuevoClienteFCM arma el cliente.
func NuevoClienteFCM(o OpcionesFCM) *ClienteFCM {
	if o.URL == "" {
		o.URL = URLFCM
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	if o.Ahora == nil {
		o.Ahora = time.Now
	}
	return &ClienteFCM{proyecto: o.Proyecto, base: strings.TrimRight(o.URL, "/"), cuenta: o.Cuenta, clave: o.Clave, http: o.HTTP, ahora: o.Ahora}
}

// Enviar manda el mensaje. Con 401 renueva el token de acceso una vez.
func (c *ClienteFCM) Enviar(ctx context.Context, m MensajeFCM) (string, error) {
	for intento := 0; ; intento++ {
		acceso, err := c.tokenAcceso(ctx)
		if err != nil {
			return "", err
		}
		nombre, err := c.enviarCon(ctx, acceso, m)
		var e *ErrorFCM
		if intento == 0 && errors.As(err, &e) && e.Status == http.StatusUnauthorized {
			c.olvidarAcceso()
			continue
		}
		return nombre, err
	}
}

func (c *ClienteFCM) enviarCon(ctx context.Context, acceso string, m MensajeFCM) (string, error) {
	cuerpo, err := json.Marshal(map[string]any{"message": m})
	if err != nil {
		return "", err
	}
	u := c.base + "/v1/projects/" + url.PathEscape(c.proyecto) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(cuerpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+acceso)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	res, err := c.http.Do(req)
	if err != nil {
		return "", &ErrorFCM{causa: errorSinURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return "", errorDeRespuesta(res, b)
	}
	var ok struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(b, &ok)
	return ok.Name, nil
}

// errorDeRespuesta lee el error de Google: { error: { code, status, details: [{ errorCode }] } }.
func errorDeRespuesta(res *http.Response, b []byte) *ErrorFCM {
	e := &ErrorFCM{Status: res.StatusCode}
	var cuerpo struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				Tipo      string `json:"@type"`
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &cuerpo) == nil {
		e.Codigo = cuerpo.Error.Status
		for _, d := range cuerpo.Error.Details {
			if d.ErrorCode != "" {
				e.Codigo = d.ErrorCode
				break
			}
		}
	}
	if s := res.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			e.RetryAfter = time.Duration(n) * time.Second
		}
	}
	return e
}

// errorSinURL quita la URL del error de red (la de FCM no es secreta, pero así el log es corto).
func errorSinURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func (c *ClienteFCM) olvidarAcceso() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acceso, c.vence = "", time.Time{}
}

// tokenAcceso devuelve el token OAuth guardado o pide uno nuevo con un JWT firmado (RS256).
func (c *ClienteFCM) tokenAcceso(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ahora := c.ahora()
	if c.acceso != "" && ahora.Before(c.vence) {
		return c.acceso, nil
	}
	jwt, err := c.firmar(ahora)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {jwt}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cuenta.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return "", &ErrorFCM{causa: fmt.Errorf("token OAuth: %w", errorSinURL(err))}
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		// 4xx del servidor de tokens: credenciales mal (no se arregla reintentando); 5xx sí.
		e := &ErrorFCM{Status: res.StatusCode, Codigo: "OAUTH"}
		if res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
			e.Codigo = "OAUTH_RECHAZADO"
		}
		return "", e
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &t); err != nil || t.AccessToken == "" {
		return "", &ErrorFCM{Status: http.StatusBadGateway, Codigo: "OAUTH"}
	}
	if t.ExpiresIn <= 0 {
		t.ExpiresIn = 3600
	}
	c.acceso = t.AccessToken
	c.vence = ahora.Add(time.Duration(t.ExpiresIn)*time.Second - time.Minute)
	return c.acceso, nil
}

func (c *ClienteFCM) firmar(ahora time.Time) (string, error) {
	cab := map[string]string{"alg": "RS256", "typ": "JWT"}
	if c.cuenta.ClaveID != "" {
		cab["kid"] = c.cuenta.ClaveID
	}
	reclamos := map[string]any{
		"iss": c.cuenta.Correo, "scope": AlcanceFCM, "aud": c.cuenta.TokenURI,
		"iat": ahora.Unix(), "exp": ahora.Add(time.Hour).Unix(),
	}
	a, _ := json.Marshal(cab)
	b, _ := json.Marshal(reclamos)
	firmado := base64.RawURLEncoding.EncodeToString(a) + "." + base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(firmado))
	firma, err := rsa.SignPKCS1v15(nil, c.clave, crypto.SHA256, h[:])
	if err != nil {
		return "", fmt.Errorf("fcm: firmar el JWT: %w", err)
	}
	return firmado + "." + base64.RawURLEncoding.EncodeToString(firma), nil
}
