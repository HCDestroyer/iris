package conector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
)

// Tipos que expone la API de Iris (camelCase). El OpenAPI se genera de aquí.

// Conector con Argos u Horus. Nunca lleva el token ni el secreto del webhook.
type Conector struct {
	ID              int64      `json:"id"`
	Sistema         string     `json:"sistema" enum:"argosisp,horus"`
	BaseURL         string     `json:"baseUrl" doc:"Origen del otro sistema"`
	URLPublica      string     `json:"urlPublica" doc:"Origen de la interfaz del otro sistema para los enlaces «Ver en Argos/Horus»: la configurada o, si no hay, baseUrl"`
	Origen          string     `json:"origen" enum:"enlace_remoto,enlace_local" doc:"enlace_remoto: el enlace lo generó el otro sistema y lo canjeó Iris; enlace_local: lo generó Iris y lo canjeó el otro"`
	Estado          string     `json:"estado" enum:"apagado,activo"`
	RemotoID        *string    `json:"remotoId" nullable:"true" doc:"Id de la conexión en el otro sistema, si lo informó"`
	Empresas        []int64    `json:"empresas" doc:"Empresas de Argos que cubre el token (vacío si el otro sistema no lo informó)"`
	AlcanceGlobal   *bool      `json:"alcanceGlobal,omitempty" doc:"Lo que informó el canje de Argos (0.3.1): true = Iris ve también las empresas que se creen después en Argos; false = solo las de empresas (vacía = ninguna). Ausente si no lo informó (Argos anterior a 0.3.1, Horus o un enlace generado en Iris)"`
	WebhookURL      string     `json:"webhookUrl" doc:"URL a la que el otro sistema envía los webhooks"`
	ActivadoEn      *time.Time `json:"activadoEn" nullable:"true"`
	UltimoWebhookEn *time.Time `json:"ultimoWebhookEn" nullable:"true"`
	Version         int32      `json:"version"`
	CreadoEn        time.Time  `json:"creadoEn"`
	ActualizadoEn   time.Time  `json:"actualizadoEn"`
}

// AConector arma el tipo de la API desde la fila.
func (n *Nucleo) AConector(f conectorbd.IntegracionConectore) Conector {
	empresas := f.Empresas
	if empresas == nil {
		empresas = []int64{}
	}
	return Conector{
		ID: f.ID, Sistema: f.Sistema, BaseURL: f.BaseUrl, URLPublica: urlPublica(f), Origen: f.Origen, Estado: f.Estado,
		RemotoID: f.RemotoID, Empresas: empresas, AlcanceGlobal: f.AlcanceGlobal, WebhookURL: n.URLWebhook(f.Sistema, f.ID), ActivadoEn: f.ActivadoEn,
		UltimoWebhookEn: f.UltimoWebhookEn, Version: f.Version, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt,
	}
}

func urlPublica(f conectorbd.IntegracionConectore) string {
	if f.UrlPublica != nil && *f.UrlPublica != "" {
		return *f.UrlPublica
	}
	return f.BaseUrl
}

// URLWebhook es la URL a la que el sistema envía sus webhooks.
func (n *Nucleo) URLWebhook(sistema string, id int64) string {
	return fmt.Sprintf("%s/api/v1/integraciones/%s/webhook/%d", n.cfg.BaseURL, sistema, id)
}

// EntradaConectar es la entrada de integraciones.conectar (enlace del otro sistema).
type EntradaConectar struct {
	Enlace string `json:"enlace" minLength:"10" maxLength:"2000" auditoria:"sensible" doc:"Enlace de conexión generado en el otro sistema: https://<sistema>/conectar#<código> (15 min, un solo uso)"`
}

// EnlaceGenerado es el enlace que se pega en el otro sistema.
type EnlaceGenerado struct {
	Enlace   string    `json:"enlace" doc:"https://<iris>/conectar#<código>; se muestra una sola vez"`
	ExpiraEn time.Time `json:"expiraEn"`
}

// EntradaCanjear es lo que el otro sistema envía al canjear un enlace generado en Iris.
type EntradaCanjear struct {
	Codigo     string  `json:"codigo" minLength:"16" maxLength:"200" auditoria:"sensible" doc:"Código del enlace (lo que sigue a #)"`
	Sistema    string  `json:"sistema" enum:"argosisp,horus" doc:"Sistema que canjea; debe ser el de la ruta"`
	URL        string  `json:"url" minLength:"8" maxLength:"500" doc:"Origen del otro sistema (https://…), sin /api/v1"`
	Token      string  `json:"token" minLength:"12" maxLength:"500" doc:"Token del otro sistema para Iris: atk_… del usuario técnico «Iris» en Argos, hf_pat_… en Horus"`
	ConexionID string  `json:"conexionId,omitempty" maxLength:"100" doc:"Id de la conexión en el otro sistema"`
	Empresas   []int64 `json:"empresas,omitempty" maxItems:"1000" doc:"Empresas de Argos que cubre el token (solo Argos)"`
}

// ConexionCanjeada es la respuesta del canje: el otro sistema guarda la URL y el secreto del
// webhook.
type ConexionCanjeada struct {
	ConectorID     int64  `json:"conectorId"`
	Sistema        string `json:"sistema" enum:"iris"`
	WebhookURL     string `json:"webhookUrl" doc:"URL a la que el otro sistema envía los webhooks firmados"`
	WebhookSecreto string `json:"webhookSecreto" doc:"Secreto para HMAC-SHA256 de X-Webhook-Signature; solo se entrega aquí"`
	Estado         string `json:"estado" enum:"apagado" doc:"El conector queda apagado hasta que un administrador de Iris lo active"`
}

// EntradaCambioConector es la entrada de integraciones.activar / integraciones.desactivar.
type EntradaCambioConector struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión que leíste (409 si cambió)"`
	Motivo  string `json:"motivo,omitempty" maxLength:"500"`
}

// EntradaURLPublica es la entrada de integraciones.configurar_url.
type EntradaURLPublica struct {
	ID         int64   `json:"-"`
	Version    int32   `json:"version" minimum:"1" doc:"Versión que leíste (409 si cambió)"`
	URLPublica *string `json:"urlPublica" nullable:"true" maxLength:"500" doc:"Origen de la interfaz del otro sistema (https://…); null = la URL base del conector"`
}

// EnlaceExterno es el origen de la interfaz de un sistema con conector activo.
type EnlaceExterno struct {
	ConectorID int64  `json:"conectorId"`
	URLPublica string `json:"urlPublica" doc:"Origen de la interfaz del sistema (https://…), sin barra final"`
}

// EnlacesExternos son los orígenes para los enlaces «Ver en Argos» / «Ver en Horus» del panel.
// Un sistema sin conector activo no aparece (no se muestra el enlace).
type EnlacesExternos struct {
	Argos *EnlaceExterno `json:"argosisp,omitempty" doc:"ArgosISP; ausente sin conector activo"`
	Horus *EnlaceExterno `json:"horus,omitempty" doc:"Horus Flow; ausente sin conector activo"`
}

// PeticionCanje es el cuerpo del canje en el otro sistema (contrato Argos ↔ Horus §1.4, al que
// remite §1.3 de los contratos de Iris): el origen de la API de Iris, el token iris_pat_ que Iris
// emite ahora para que el otro sistema la lea (§1.2) y el receptor de webhooks de Iris con el
// secreto que Iris generó.
type PeticionCanje struct {
	Codigo  string       `json:"codigo"`
	Sistema string       `json:"sistema"`
	BaseURL string       `json:"base_url"`
	Token   string       `json:"token"`
	Webhook WebhookCanje `json:"webhook"`
}

// WebhookCanje es un receptor de webhooks y su secreto HMAC, en el canje.
type WebhookCanje struct {
	URL     string `json:"url"`
	Secreto string `json:"secreto"`
}

// RespuestaCanje es lo que devuelve el otro sistema al canjear (§1.4): su token para Iris, su
// receptor de webhooks (Iris aún no envía eventos: no se guarda) y el id del conector allá.
// conexion_id y empresas son opcionales (versiones anteriores del supuesto). Argos 0.3.1 agrega
// alcance_global y empresas[] como { id, nombre, nit } (§1.3).
type RespuestaCanje struct {
	Token         string            `json:"token"`
	Webhook       *WebhookCanje     `json:"webhook"`
	ConectorID    json.RawMessage   `json:"conector_id"`
	ConexionID    json.RawMessage   `json:"conexion_id"`
	Empresas      []json.RawMessage `json:"empresas"`
	AlcanceGlobal *bool             `json:"alcance_global"`
}

// EmpresaCanje es una empresa del alcance que informa el canje: nombre comercial y NIT sin
// guiones (vacíos si no vinieron).
type EmpresaCanje struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre,omitempty"`
	NIT    string `json:"nit,omitempty"`
}

var (
	idRemotoTexto  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	idRemotoNumero = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
)

// ConectorIDValido devuelve el id del conector en el otro sistema (conector_id o, en versiones
// anteriores, conexion_id) como texto: un entero positivo o un texto de hasta 64 caracteres de
// [A-Za-z0-9_-]. nil si no vino. Cualquier otra cosa es un error (el canje falla y el token
// entregado se revoca).
func (r RespuestaCanje) ConectorIDValido() (*string, error) {
	crudo := bytes.TrimSpace(r.ConectorID)
	if len(crudo) == 0 || string(crudo) == "null" {
		crudo = bytes.TrimSpace(r.ConexionID)
	}
	if len(crudo) == 0 || string(crudo) == "null" {
		return nil, nil
	}
	var texto string
	if crudo[0] == '"' {
		if err := json.Unmarshal(crudo, &texto); err != nil || !idRemotoTexto.MatchString(texto) {
			return nil, errors.New("conector_id inválido")
		}
		return &texto, nil
	}
	texto = string(crudo)
	if !idRemotoNumero.MatchString(texto) {
		return nil, errors.New("conector_id inválido")
	}
	return &texto, nil
}

// EmpresasCanje acepta [1, 2] o [{ id: 1, nombre, nit }, …] (Argos 0.3.1; nit puede ser null).
// Ids no positivos se descartan; nombre y nit se recortan (200 y 30 caracteres, como
// nucleo.empresas).
func (r RespuestaCanje) EmpresasCanje() []EmpresaCanje {
	salida := []EmpresaCanje{}
	for _, c := range r.Empresas {
		var n int64
		if json.Unmarshal(c, &n) == nil {
			if n > 0 {
				salida = append(salida, EmpresaCanje{ID: n})
			}
			continue
		}
		var ref struct {
			ID     *int64  `json:"id"`
			Nombre *string `json:"nombre"`
			NIT    *string `json:"nit"`
		}
		if json.Unmarshal(c, &ref) != nil || ref.ID == nil || *ref.ID <= 0 {
			continue
		}
		e := EmpresaCanje{ID: *ref.ID}
		if ref.Nombre != nil {
			e.Nombre = recortar(strings.TrimSpace(*ref.Nombre), 200)
		}
		if ref.NIT != nil {
			e.NIT = recortar(strings.TrimSpace(*ref.NIT), 30)
		}
		salida = append(salida, e)
	}
	return salida
}

// IDsEmpresas son los ids de EmpresasCanje.
func (r RespuestaCanje) IDsEmpresas() []int64 {
	salida := []int64{}
	for _, e := range r.EmpresasCanje() {
		salida = append(salida, e.ID)
	}
	return salida
}

func recortar(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
