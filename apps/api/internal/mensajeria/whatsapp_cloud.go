package mensajeria

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// WhatsApp Cloud API (Meta). Envío de plantillas con POST /{phone_number_id}/messages y token del
// sistema de la empresa (Bearer); estados de entrega (sent, delivered, read, failed) y mensajes del
// cliente por el webhook de la app, firmado con su app secret (X-Hub-Signature-256). El token y el
// app secret nunca salen en errores ni logs.

type whatsappCloud struct {
	m             *Modulo
	phoneNumberID string
	token         string
	plantillas    map[string]PlantillaMeta
}

func (w *whatsappCloud) Tipo() string        { return TipoWhatsAppCloud }
func (w *whatsappCloud) Canal() string       { return CanalWhatsApp }
func (w *whatsappCloud) PermiteAvisos() bool { return true }
func (w *whatsappCloud) Aprobada(codigo string) bool {
	_, ok := w.plantillas[codigo]
	return ok
}

type metaParametro struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type metaComponente struct {
	Type       string          `json:"type"`
	Parameters []metaParametro `json:"parameters"`
}

type metaPlantilla struct {
	Name     string `json:"name"`
	Language struct {
		Code string `json:"code"`
	} `json:"language"`
	Components []metaComponente `json:"components,omitempty"`
}

type metaEnvio struct {
	MessagingProduct string        `json:"messaging_product"`
	RecipientType    string        `json:"recipient_type"`
	To               string        `json:"to"`
	Type             string        `json:"type"`
	Template         metaPlantilla `json:"template"`
}

type metaRespuesta struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	Error *metaError `json:"error"`
}

type metaError struct {
	Code    int    `json:"code"`
	Subcode int    `json:"error_subcode"`
	Type    string `json:"type"`
	Title   string `json:"error_user_title"`
	Message string `json:"message"`
}

func (w *whatsappCloud) EnviarPlantilla(ctx context.Context, msg Mensaje) (Resultado, error) {
	pm, ok := w.plantillas[msg.Plantilla.Codigo]
	if !ok {
		return Resultado{}, &ErrorCanal{Motivo: MotivoRechazado, Detalle: "plantilla no aprobada en el canal"}
	}
	cuerpo := metaEnvio{MessagingProduct: "whatsapp", RecipientType: "individual", To: strings.TrimPrefix(msg.Destino, "+"), Type: "template"}
	cuerpo.Template.Name, cuerpo.Template.Language.Code = pm.Nombre, pm.Idioma
	if params := msg.Plantilla.Parametros(msg.Variables); len(params) > 0 {
		c := metaComponente{Type: "body"}
		for _, p := range params {
			c.Parameters = append(c.Parameters, metaParametro{Type: "text", Text: p})
		}
		cuerpo.Template.Components = []metaComponente{c}
	}
	crudo, err := json.Marshal(cuerpo)
	if err != nil {
		return Resultado{}, err
	}
	ctx, cancelar := context.WithTimeout(ctx, 15*time.Second)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.m.cfg.MetaURL+"/"+w.phoneNumberID+"/messages", bytes.NewReader(crudo))
	if err != nil {
		return Resultado{}, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Iris/"+w.m.cfg.Version)
	resp, err := w.m.cfg.HTTPMeta.Do(req)
	if err != nil {
		// Sin el texto del error (podría llevar la URL); se reintenta.
		return Resultado{}, &ErrorCanal{Motivo: MotivoRechazado, Detalle: "meta: sin respuesta", Temporal: true}
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var r metaRespuesta
	_ = json.Unmarshal(leido, &r)
	if resp.StatusCode/100 == 2 && len(r.Messages) > 0 && r.Messages[0].ID != "" {
		return Resultado{ProveedorID: r.Messages[0].ID}, nil
	}
	return Resultado{}, clasificarErrorMeta(resp.StatusCode, r.Error)
}

// EnviarConversacion manda texto (dentro de la ventana de 24 h), un medio (lo sube primero a
// /{phone_number_id}/media) o una plantilla.
func (w *whatsappCloud) EnviarConversacion(ctx context.Context, msg MensajeSaliente) (Resultado, error) {
	if msg.Plantilla != nil {
		return w.EnviarPlantilla(ctx, Mensaje{AvisoID: msg.ID, Destino: msg.Destino, Plantilla: *msg.Plantilla, Variables: msg.Variables})
	}
	cuerpo := map[string]any{"messaging_product": "whatsapp", "recipient_type": "individual", "to": strings.TrimPrefix(msg.Destino, "+")}
	switch {
	case msg.Medio != nil:
		id, err := w.subirMedio(ctx, *msg.Medio)
		if err != nil {
			return Resultado{}, err
		}
		tipo := map[string]string{"imagen": "image", "audio": "audio", "video": "video"}[msg.Medio.Tipo]
		if tipo == "" {
			tipo = "document"
		}
		medio := map[string]any{"id": id}
		if msg.Texto != nil && tipo != "audio" {
			medio["caption"] = *msg.Texto
		}
		if tipo == "document" {
			medio["filename"] = msg.Medio.Nombre
		}
		cuerpo["type"], cuerpo[tipo] = tipo, medio
	case msg.Texto != nil:
		cuerpo["type"] = "text"
		cuerpo["text"] = map[string]any{"body": *msg.Texto, "preview_url": false}
	default:
		return Resultado{}, &ErrorCanal{Motivo: MotivoRechazado, Detalle: "mensaje vacío"}
	}
	crudo, err := json.Marshal(cuerpo)
	if err != nil {
		return Resultado{}, err
	}
	status, leido, err := w.postMeta(ctx, "/messages", "application/json", bytes.NewReader(crudo))
	if err != nil {
		return Resultado{}, err
	}
	var r metaRespuesta
	_ = json.Unmarshal(leido, &r)
	if status/100 == 2 && len(r.Messages) > 0 && r.Messages[0].ID != "" {
		return Resultado{ProveedorID: r.Messages[0].ID}, nil
	}
	return Resultado{}, clasificarErrorMeta(status, r.Error)
}

// subirMedio sube el archivo a Meta y devuelve su id de medio.
func (w *whatsappCloud) subirMedio(ctx context.Context, md MedioSaliente) (string, error) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("messaging_product", "whatsapp")
	_ = mw.WriteField("type", md.Mime)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, md.Nombre))
	h.Set("Content-Type", md.Mime)
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(md.Datos); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	status, leido, err := w.postMeta(ctx, "/media", mw.FormDataContentType(), &b)
	if err != nil {
		return "", err
	}
	var r struct {
		ID    string     `json:"id"`
		Error *metaError `json:"error"`
	}
	_ = json.Unmarshal(leido, &r)
	if status/100 == 2 && r.ID != "" {
		return r.ID, nil
	}
	return "", clasificarErrorMeta(status, r.Error)
}

// postMeta hace un POST a /{phone_number_id}<ruta> con el token del canal.
func (w *whatsappCloud) postMeta(ctx context.Context, ruta, tipo string, cuerpo io.Reader) (int, []byte, error) {
	ctx, cancelar := context.WithTimeout(ctx, 30*time.Second)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.m.cfg.MetaURL+"/"+w.phoneNumberID+ruta, cuerpo)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", tipo)
	req.Header.Set("User-Agent", "Iris/"+w.m.cfg.Version)
	resp, err := w.m.cfg.HTTPMeta.Do(req)
	if err != nil {
		return 0, nil, &ErrorCanal{Motivo: MotivoRechazado, Detalle: "meta: sin respuesta", Temporal: true}
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, leido, nil
}

// clasificarErrorMeta traduce un error de la Cloud API
// (https://developers.facebook.com/docs/whatsapp/cloud-api/support/error-codes).
func clasificarErrorMeta(status int, e *metaError) *ErrorCanal {
	codigo := 0
	titulo := ""
	if e != nil {
		codigo, titulo = e.Code, e.Title
		if titulo == "" {
			titulo = e.Type
		}
	}
	detalle := recortar(fmt.Sprintf("meta %d: %d %s", status, codigo, titulo), 200)
	switch {
	case codigo == 131048 || codigo == 131056 || codigo == 368:
		// Límite de spam, demasiados mensajes al mismo número o restricción de la cuenta: se pausa.
		return &ErrorCanal{Motivo: MotivoRechazado, Detalle: detalle, Temporal: true, Pausar: true}
	case status == http.StatusTooManyRequests || codigo == 4 || codigo == 80007 || codigo == 130429 || codigo == 131016 || status >= 500 || status == 0:
		return &ErrorCanal{Motivo: MotivoRechazado, Detalle: detalle, Temporal: true}
	case codigo == 131047:
		// Más de 24 h desde el último mensaje del cliente: solo plantillas.
		return &ErrorCanal{Motivo: MotivoFueraDeVentana, Detalle: detalle}
	case codigo == 131026 || codigo == 131030 || codigo == 131021 || codigo == 133010:
		return &ErrorCanal{Motivo: MotivoNumeroInvalido, Detalle: detalle}
	}
	return &ErrorCanal{Motivo: MotivoRechazado, Detalle: detalle}
}

// FirmaMetaValida compara X-Hub-Signature-256 (sha256=<hex(HMAC-SHA256(app_secret, cuerpo))>) en
// tiempo constante.
func FirmaMetaValida(appSecret, cabecera string, cuerpo []byte) bool {
	valor, ok := strings.CutPrefix(strings.TrimSpace(cabecera), "sha256=")
	if !ok || appSecret == "" {
		return false
	}
	recibida, err := hex.DecodeString(valor)
	if err != nil {
		return false
	}
	h := hmac.New(sha256.New, []byte(appSecret))
	h.Write(cuerpo)
	return hmac.Equal(recibida, h.Sum(nil))
}

// FirmarMeta calcula X-Hub-Signature-256 (pruebas y documentación del formato).
func FirmarMeta(appSecret string, cuerpo []byte) string {
	h := hmac.New(sha256.New, []byte(appSecret))
	h.Write(cuerpo)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// Webhook de Meta --------------------------------------------------------------------------------

type metaWebhook struct {
	Object string `json:"object"`
	Entry  []struct {
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Statuses []metaEstado   `json:"statuses"`
				Messages []metaMensaje  `json:"messages"`
				Contacts []metaContacto `json:"contacts"`
				// phone_number_quality_update
				Event string `json:"event"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

type metaEstado struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Errors    []struct {
		Code  int    `json:"code"`
		Title string `json:"title"`
	} `json:"errors"`
}

type metaMensaje struct {
	From      string `json:"from"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text      struct {
		Body string `json:"body"`
	} `json:"text"`
	// Mensaje al que responde (p. ej. un aviso).
	Context *struct {
		ID string `json:"id"`
	} `json:"context"`
	Image    *metaMedio `json:"image"`
	Document *metaMedio `json:"document"`
	Audio    *metaMedio `json:"audio"`
	Video    *metaMedio `json:"video"`
	Sticker  *metaMedio `json:"sticker"`
	Location *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name"`
		Address   string  `json:"address"`
	} `json:"location"`
	Button *struct {
		Text string `json:"text"`
	} `json:"button"`
	Interactive *struct {
		ButtonReply *struct {
			Title string `json:"title"`
		} `json:"button_reply"`
		ListReply *struct {
			Title string `json:"title"`
		} `json:"list_reply"`
	} `json:"interactive"`
	Contacts []struct {
		Name struct {
			FormattedName string `json:"formatted_name"`
		} `json:"name"`
	} `json:"contacts"`
}

// metaMedio es un archivo que mandó el cliente (se descarga con su id).
type metaMedio struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	Caption  string `json:"caption"`
	Filename string `json:"filename"`
}

// metaContacto es el perfil de quien escribe.
type metaContacto struct {
	WaID    string `json:"wa_id"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}

// instanteMeta: segundos Unix como texto; el reloj del servidor si no se entiende.
func instanteMeta(s string, por time.Time) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return por
	}
	return time.Unix(n, 0).UTC()
}

// motivoFallaMeta: el motivo del contrato para un estado failed.
func motivoFallaMeta(e metaEstado) (motivo, detalle string) {
	if len(e.Errors) == 0 {
		return MotivoRechazado, "meta: failed"
	}
	err := e.Errors[0]
	c := clasificarErrorMeta(http.StatusBadRequest, &metaError{Code: err.Code, Title: err.Title})
	if c.Motivo == MotivoNumeroInvalido {
		return MotivoNumeroInvalido, c.Detalle
	}
	return MotivoRechazado, recortar(fmt.Sprintf("meta: %d %s", err.Code, err.Title), 200)
}

func recortar(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
