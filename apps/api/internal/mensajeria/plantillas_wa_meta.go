package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Graph API de plantillas (WhatsApp Business Management API, versión de IRIS_MENSAJERIA_META_URL):
//
//	GET    /{waba_id}/message_templates?fields=…&limit=…&after=…  leer (paginado por cursor)
//	POST   /{waba_id}/message_templates                           crear {name, language, category, parameter_format, components}
//	POST   /{template_id}                                         editar {components} (Meta la vuelve a revisar)
//	DELETE /{waba_id}/message_templates?name=…&hsm_id=…           borrar una (por id) o todas las de un nombre
//	POST   /{template_id}/unpause                                 reanudar una pausada
//	GET    /me/permissions                                        ¿el token tiene whatsapp_business_management?
//	POST   /{app_id}/uploads y POST /{upload_id}                   carga reanudable (archivo de ejemplo del encabezado)
//
// El token nunca sale en errores ni registros; de los errores de Meta solo se guardan los números
// y, recortado, el mensaje para personas (error_user_msg).

// CuentaGraph es una cuenta de WhatsApp con el token de uno de sus números.
type CuentaGraph struct {
	WabaID string
	Token  string
	// AppID de la app de Meta (solo para la carga reanudable; vacío si no se conoce).
	AppID string
}

// ErrMetaPlantilla es un rechazo o una falta de respuesta de Meta.
type ErrMetaPlantilla struct {
	Status    int
	Codigo    int
	Subcodigo int
	// SinRespuesta: red, plazo o 5xx (no se sabe si Meta la aplicó).
	SinRespuesta bool
	// Mensaje para personas de Meta (solo error_user_msg, recortado; nunca message, que es técnico).
	Mensaje string
}

func (e *ErrMetaPlantilla) Error() string {
	if e.SinRespuesta {
		return "meta: sin respuesta"
	}
	return fmt.Sprintf("meta: %d (código %d/%d)", e.Status, e.Codigo, e.Subcodigo)
}

// PlantillaMetaLeida es una plantilla tal como la devuelve Meta.
type PlantillaMetaLeida struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Language         string          `json:"language"`
	Status           string          `json:"status"`
	Category         string          `json:"category"`
	PreviousCategory string          `json:"previous_category"`
	ParameterFormat  string          `json:"parameter_format"`
	RejectedReason   string          `json:"rejected_reason"`
	QualityScore     json.RawMessage `json:"quality_score"`
	Components       json.RawMessage `json:"components"`
}

// Calidad: quality_score llega como objeto {score} (o, en versiones viejas, como texto).
func (p PlantillaMetaLeida) Calidad() string {
	var o struct {
		Score string `json:"score"`
	}
	var s string
	switch {
	case json.Unmarshal(p.QualityScore, &o) == nil && o.Score != "":
		s = o.Score
	case json.Unmarshal(p.QualityScore, &s) == nil:
	}
	switch s = strings.ToUpper(s); s {
	case "GREEN", "YELLOW", "RED":
		return s
	}
	return "UNKNOWN"
}

// RespuestaCrearMeta es lo que responde Meta al crear.
type RespuestaCrearMeta struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Category string `json:"category"`
}

// GraphPlantillas es la parte de la Graph API que usan las plantillas.
type GraphPlantillas interface {
	Listar(ctx context.Context, c CuentaGraph) ([]PlantillaMetaLeida, error)
	Crear(ctx context.Context, c CuentaGraph, cuerpo map[string]any) (RespuestaCrearMeta, error)
	Editar(ctx context.Context, c CuentaGraph, metaID string, cuerpo map[string]any) error
	Borrar(ctx context.Context, c CuentaGraph, nombre, metaID string) error
	Reanudar(ctx context.Context, c CuentaGraph, metaID string) error
	// Permiso: leido = Meta respondió la lista de permisos; tiene = whatsapp_business_management concedido.
	Permiso(ctx context.Context, c CuentaGraph) (tiene, leido bool)
	SubirArchivo(ctx context.Context, c CuentaGraph, nombre, mime string, datos []byte) (string, error)
	// VerificarNumero: el phone_number_id es de la WABA (GET /{waba}/phone_numbers con el token).
	VerificarNumero(ctx context.Context, c CuentaGraph, phoneNumberID string) (bool, error)
}

// PlazoMetaPlantillas: lo que se espera a Meta por petición (el panel dice «Meta no contestó en 15 s»).
const PlazoMetaPlantillas = 15 * time.Second

// MaxPaginasPlantillas: tope de páginas al leer una cuenta (100 por página; Meta admite 6 000).
const MaxPaginasPlantillas = 80

type graphPlantillasHTTP struct{ m *Modulo }

func (m *Modulo) graphPlantillas() GraphPlantillas {
	if m.cfg.Plantillas != nil {
		return m.cfg.Plantillas
	}
	return graphPlantillasHTTP{m: m}
}

func (g graphPlantillasHTTP) pedir(ctx context.Context, metodo, ruta string, consulta url.Values, cuerpo any, token string) ([]byte, error) {
	ctx, cancelar := context.WithTimeout(ctx, PlazoMetaPlantillas)
	defer cancelar()
	var rd io.Reader
	if cuerpo != nil {
		b, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := g.m.cfg.MetaURL + ruta
	if len(consulta) > 0 {
		u += "?" + consulta.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, metodo, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Iris/"+g.m.cfg.Version)
	return g.hacer(req)
}

func (g graphPlantillasHTTP) hacer(req *http.Request) ([]byte, error) {
	resp, err := g.m.cfg.HTTPMeta.Do(req)
	if err != nil {
		return nil, &ErrMetaPlantilla{SinRespuesta: true}
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return leido, nil
	}
	e := &ErrMetaPlantilla{Status: resp.StatusCode, SinRespuesta: resp.StatusCode >= 500}
	var r struct {
		Error struct {
			Message  string `json:"message"`
			Code     int    `json:"code"`
			Subcode  int    `json:"error_subcode"`
			UserMsg  string `json:"error_user_msg"`
			UserTitl string `json:"error_user_title"`
		} `json:"error"`
	}
	if json.Unmarshal(leido, &r) == nil {
		e.Codigo, e.Subcodigo = r.Error.Code, r.Error.Subcode
		e.Mensaje = recortar(limpiarTextoMeta(r.Error.UserMsg), 300)
	}
	return nil, e
}

// limpiarTextoMeta quita de un mensaje de Meta lo que parezca un token (por si lo repite).
func limpiarTextoMeta(s string) string {
	campos := strings.Fields(s)
	for i, c := range campos {
		if len(c) > 40 && !strings.ContainsAny(c, " ") && strings.IndexFunc(c, func(r rune) bool { return r == '.' || r == ',' }) < 0 {
			campos[i] = "***"
		}
	}
	return strings.Join(campos, " ")
}

const camposPlantillasMeta = "id,name,language,status,category,previous_category,parameter_format,rejected_reason,quality_score,components"
const camposPlantillasMetaMinimos = "id,name,language,status,category,components"

func (g graphPlantillasHTTP) Listar(ctx context.Context, c CuentaGraph) ([]PlantillaMetaLeida, error) {
	campos := camposPlantillasMeta
	out := []PlantillaMetaLeida{}
	despues := ""
	for pagina := 0; pagina < MaxPaginasPlantillas; pagina++ {
		q := url.Values{"fields": {campos}, "limit": {"100"}}
		if despues != "" {
			q.Set("after", despues)
		}
		b, err := g.pedir(ctx, http.MethodGet, "/"+url.PathEscape(c.WabaID)+"/message_templates", q, nil, c.Token)
		var em *ErrMetaPlantilla
		if errors.As(err, &em) && em.Codigo == 100 && pagina == 0 && campos == camposPlantillasMeta {
			// Una versión de la Graph API que no conoce algún campo opcional: se leen los básicos.
			campos = camposPlantillasMetaMinimos
			pagina--
			continue
		}
		if err != nil {
			return nil, err
		}
		var r struct {
			Data   []PlantillaMetaLeida `json:"data"`
			Paging struct {
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
				Next string `json:"next"`
			} `json:"paging"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, &ErrMetaPlantilla{SinRespuesta: true}
		}
		out = append(out, r.Data...)
		// Se sigue con el cursor (nunca con la URL next que manda Meta).
		if r.Paging.Next == "" || r.Paging.Cursors.After == "" || len(r.Data) == 0 {
			return out, nil
		}
		despues = r.Paging.Cursors.After
	}
	return out, nil
}

func (g graphPlantillasHTTP) Crear(ctx context.Context, c CuentaGraph, cuerpo map[string]any) (RespuestaCrearMeta, error) {
	b, err := g.pedir(ctx, http.MethodPost, "/"+url.PathEscape(c.WabaID)+"/message_templates", nil, cuerpo, c.Token)
	if err != nil {
		return RespuestaCrearMeta{}, err
	}
	var r RespuestaCrearMeta
	if json.Unmarshal(b, &r) != nil || r.ID == "" {
		return RespuestaCrearMeta{}, &ErrMetaPlantilla{SinRespuesta: true}
	}
	return r, nil
}

func (g graphPlantillasHTTP) Editar(ctx context.Context, c CuentaGraph, metaID string, cuerpo map[string]any) error {
	_, err := g.pedir(ctx, http.MethodPost, "/"+url.PathEscape(metaID), nil, cuerpo, c.Token)
	return err
}

func (g graphPlantillasHTTP) Borrar(ctx context.Context, c CuentaGraph, nombre, metaID string) error {
	q := url.Values{"name": {nombre}}
	if metaID != "" {
		q.Set("hsm_id", metaID)
	}
	_, err := g.pedir(ctx, http.MethodDelete, "/"+url.PathEscape(c.WabaID)+"/message_templates", q, nil, c.Token)
	return err
}

func (g graphPlantillasHTTP) Reanudar(ctx context.Context, c CuentaGraph, metaID string) error {
	_, err := g.pedir(ctx, http.MethodPost, "/"+url.PathEscape(metaID)+"/unpause", nil, nil, c.Token)
	return err
}

func (g graphPlantillasHTTP) Permiso(ctx context.Context, c CuentaGraph) (bool, bool) {
	b, err := g.pedir(ctx, http.MethodGet, "/me/permissions", nil, nil, c.Token)
	if err != nil {
		return false, false
	}
	var r struct {
		Data []struct {
			Permission string `json:"permission"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &r) != nil || len(r.Data) == 0 {
		return false, false
	}
	for _, p := range r.Data {
		if p.Permission == "whatsapp_business_management" && strings.EqualFold(p.Status, "granted") {
			return true, true
		}
	}
	return false, true
}

// SubirArchivo usa la carga reanudable de Meta (una sola parte) y devuelve el handle.
func (g graphPlantillasHTTP) SubirArchivo(ctx context.Context, c CuentaGraph, nombre, mime string, datos []byte) (string, error) {
	if !reDigitos.MatchString(c.AppID) {
		return "", &ErrMetaPlantilla{Status: http.StatusBadRequest}
	}
	q := url.Values{"file_name": {nombre}, "file_length": {strconv.Itoa(len(datos))}, "file_type": {mime}}
	b, err := g.pedir(ctx, http.MethodPost, "/"+url.PathEscape(c.AppID)+"/uploads", q, nil, c.Token)
	if err != nil {
		return "", err
	}
	var s struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(b, &s) != nil || !strings.HasPrefix(s.ID, "upload:") {
		return "", &ErrMetaPlantilla{SinRespuesta: true}
	}
	ctx, cancelar := context.WithTimeout(ctx, 2*PlazoMetaPlantillas)
	defer cancelar()
	if strings.ContainsAny(strings.TrimPrefix(s.ID, "upload:"), "/?#") {
		return "", &ErrMetaPlantilla{SinRespuesta: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.m.cfg.MetaURL+"/"+s.ID, bytes.NewReader(datos))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "OAuth "+c.Token)
	req.Header.Set("file_offset", "0")
	req.Header.Set("User-Agent", "Iris/"+g.m.cfg.Version)
	b, err = g.hacer(req)
	if err != nil {
		return "", err
	}
	var h struct {
		H string `json:"h"`
	}
	if json.Unmarshal(b, &h) != nil || h.H == "" {
		return "", &ErrMetaPlantilla{SinRespuesta: true}
	}
	return h.H, nil
}

func (g graphPlantillasHTTP) VerificarNumero(ctx context.Context, c CuentaGraph, phoneNumberID string) (bool, error) {
	if !reDigitos.MatchString(c.WabaID) {
		return false, nil
	}
	b, err := g.pedir(ctx, http.MethodGet, "/"+url.PathEscape(c.WabaID)+"/phone_numbers", url.Values{"fields": {"id"}, "limit": {"100"}}, nil, c.Token)
	if err != nil {
		return false, err
	}
	var r struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &r) != nil {
		return false, &ErrMetaPlantilla{SinRespuesta: true}
	}
	for _, n := range r.Data {
		if n.ID == phoneNumberID {
			return true, nil
		}
	}
	return false, nil
}

// Iris → Meta ----------------------------------------------------------------------------------------

var tiposEncabezadoMeta = map[string]string{"texto": "TEXT", "imagen": "IMAGE", "video": "VIDEO", "documento": "DOCUMENT", "ubicacion": "LOCATION"}

func ejemploDe(d DefinicionPWA, nombre string) string {
	for _, v := range d.Variables {
		if v.Nombre == nombre {
			return v.Ejemplo
		}
	}
	return ""
}

// componentesAMeta convierte la definición de Iris en los components de Meta. handle: el del
// archivo de ejemplo del encabezado (si lleva).
func componentesAMeta(categoria, formato string, d DefinicionPWA, handle string) []map[string]any {
	out := []map[string]any{}
	if categoria == CategoriaAutenticacion {
		cuerpo := map[string]any{"type": "BODY"}
		if d.Autenticacion != nil {
			cuerpo["add_security_recommendation"] = d.Autenticacion.AvisoSeguridad
		}
		out = append(out, cuerpo)
		if d.Autenticacion != nil && d.Autenticacion.CaducaMinutos != nil {
			out = append(out, map[string]any{"type": "FOOTER", "code_expiration_minutes": *d.Autenticacion.CaducaMinutos})
		}
		texto := "Copiar código"
		for _, b := range d.Botones {
			if b.Tipo == "copiar_codigo" && b.Texto != "" {
				texto = b.Texto
			}
		}
		return append(out, map[string]any{"type": "BUTTONS", "buttons": []map[string]any{{"type": "OTP", "otp_type": "COPY_CODE", "text": texto}}})
	}
	nombrados := formato == "NAMED"
	ejemplos := func(clave string, vs []string) map[string]any {
		if len(vs) == 0 {
			return nil
		}
		if nombrados {
			ps := []map[string]any{}
			for _, n := range vs {
				ps = append(ps, map[string]any{"param_name": n, "example": ejemploDe(d, n)})
			}
			return map[string]any{clave + "_named_params": ps}
		}
		ex := []string{}
		for _, n := range vs {
			ex = append(ex, ejemploDe(d, n))
		}
		if clave == "body_text" {
			return map[string]any{clave: [][]string{ex}}
		}
		return map[string]any{clave: ex}
	}
	if e := d.Encabezado; e != nil {
		h := map[string]any{"type": "HEADER", "format": tiposEncabezadoMeta[e.Tipo]}
		switch e.Tipo {
		case "texto":
			h["text"] = derefTexto(e.Texto)
			if ex := ejemplos("header_text", variablesDeTexto(derefTexto(e.Texto))); ex != nil {
				h["example"] = ex
			}
		case "imagen", "video", "documento":
			if handle != "" {
				h["example"] = map[string]any{"header_handle": []string{handle}}
			}
		}
		out = append(out, h)
	}
	cuerpo := map[string]any{"type": "BODY", "text": d.Cuerpo}
	if ex := ejemplos("body_text", variablesDeTexto(d.Cuerpo)); ex != nil {
		cuerpo["example"] = ex
	}
	out = append(out, cuerpo)
	if d.Pie != nil && *d.Pie != "" {
		out = append(out, map[string]any{"type": "FOOTER", "text": *d.Pie})
	}
	if len(d.Botones) > 0 {
		bs := []map[string]any{}
		for _, b := range d.Botones {
			switch b.Tipo {
			case "respuesta":
				bs = append(bs, map[string]any{"type": "QUICK_REPLY", "text": b.Texto})
			case "url":
				u := derefTexto(b.URL)
				x := map[string]any{"type": "URL", "text": b.Texto}
				if vs := variablesDeTexto(u); len(vs) == 1 {
					base := reVarFinalURLPW.ReplaceAllString(u, "")
					x["url"] = base + "{{1}}"
					x["example"] = []string{base + derefTexto(b.Ejemplo)}
				} else {
					x["url"] = u
				}
				bs = append(bs, x)
			case "telefono":
				bs = append(bs, map[string]any{"type": "PHONE_NUMBER", "text": b.Texto, "phone_number": derefTexto(b.Telefono)})
			case "copiar_codigo":
				bs = append(bs, map[string]any{"type": "COPY_CODE", "example": derefTexto(b.Ejemplo)})
			}
		}
		out = append(out, map[string]any{"type": "BUTTONS", "buttons": bs})
	}
	return out
}

// cuerpoCrearMeta es el POST de creación.
func cuerpoCrearMeta(nombre, idioma, categoria, formato string, d DefinicionPWA, handle string) map[string]any {
	c := map[string]any{"name": nombre, "language": idioma, "category": categoria, "components": componentesAMeta(categoria, formato, d, handle)}
	if categoria != CategoriaAutenticacion {
		c["parameter_format"] = strings.ToLower(formato)
	}
	return c
}

// Meta → Iris ----------------------------------------------------------------------------------------

type componenteMeta struct {
	Type                      string          `json:"type"`
	Format                    string          `json:"format"`
	Text                      string          `json:"text"`
	Example                   json.RawMessage `json:"example"`
	AddSecurityRecommendation *bool           `json:"add_security_recommendation"`
	CodeExpirationMinutes     *int            `json:"code_expiration_minutes"`
	Buttons                   []struct {
		Type        string          `json:"type"`
		Text        string          `json:"text"`
		URL         string          `json:"url"`
		PhoneNumber string          `json:"phone_number"`
		Example     json.RawMessage `json:"example"`
		OTPType     string          `json:"otp_type"`
	} `json:"buttons"`
}

type ejemploMeta struct {
	HeaderText      []string   `json:"header_text"`
	BodyText        [][]string `json:"body_text"`
	HeaderTextNamed []struct {
		Nombre  string `json:"param_name"`
		Ejemplo string `json:"example"`
	} `json:"header_text_named_params"`
	BodyTextNamed []struct {
		Nombre  string `json:"param_name"`
		Ejemplo string `json:"example"`
	} `json:"body_text_named_params"`
}

// definicionDeMeta lee los components de Meta. previas: las variables que ya tenía (se conserva qué
// dato de Iris va en cada una).
func definicionDeMeta(crudo json.RawMessage, previas []VariablePWA) DefinicionPWA {
	d := DefinicionPWA{Botones: []BotonPWA{}, Variables: []VariablePWA{}}
	var cs []componenteMeta
	if json.Unmarshal(crudo, &cs) != nil {
		return d
	}
	ejemplos := map[string]string{}
	agregar := func(vs []string, ex []string) {
		for i, n := range vs {
			if i < len(ex) {
				if _, ya := ejemplos[n]; !ya {
					ejemplos[n] = ex[i]
				}
			}
		}
	}
	orden := []string{}
	for _, c := range cs {
		var ex ejemploMeta
		_ = json.Unmarshal(c.Example, &ex)
		switch strings.ToUpper(c.Type) {
		case "HEADER":
			tipo := map[string]string{"TEXT": "texto", "IMAGE": "imagen", "VIDEO": "video", "DOCUMENT": "documento", "LOCATION": "ubicacion"}[strings.ToUpper(c.Format)]
			if tipo == "" {
				continue
			}
			e := &EncabezadoPWA{Tipo: tipo}
			if tipo == "texto" {
				e.Texto = ptr(c.Text)
				vs := variablesDeTexto(c.Text)
				orden = append(orden, vs...)
				agregar(vs, ex.HeaderText)
				for _, n := range ex.HeaderTextNamed {
					ejemplos[n.Nombre] = n.Ejemplo
				}
			}
			d.Encabezado = e
		case "BODY":
			d.Cuerpo = c.Text
			vs := variablesDeTexto(c.Text)
			orden = append(orden, vs...)
			if len(ex.BodyText) > 0 {
				agregar(vs, ex.BodyText[0])
			}
			for _, n := range ex.BodyTextNamed {
				ejemplos[n.Nombre] = n.Ejemplo
			}
			if c.AddSecurityRecommendation != nil {
				if d.Autenticacion == nil {
					d.Autenticacion = &AutenticacionPWA{}
				}
				d.Autenticacion.AvisoSeguridad = *c.AddSecurityRecommendation
			}
		case "FOOTER":
			if c.CodeExpirationMinutes != nil {
				if d.Autenticacion == nil {
					d.Autenticacion = &AutenticacionPWA{}
				}
				d.Autenticacion.CaducaMinutos = c.CodeExpirationMinutes
			}
			if c.Text != "" {
				d.Pie = ptr(c.Text)
			}
		case "BUTTONS":
			for _, b := range c.Buttons {
				var exs []string
				var exs1 string
				if json.Unmarshal(b.Example, &exs) != nil {
					_ = json.Unmarshal(b.Example, &exs1)
				} else if len(exs) > 0 {
					exs1 = exs[0]
				}
				switch strings.ToUpper(b.Type) {
				case "QUICK_REPLY":
					d.Botones = append(d.Botones, BotonPWA{Tipo: "respuesta", Texto: b.Text})
				case "URL":
					x := BotonPWA{Tipo: "url", Texto: b.Text, URL: ptr(b.URL)}
					if vs := variablesDeTexto(b.URL); len(vs) == 1 {
						base := reVarFinalURLPW.ReplaceAllString(b.URL, "")
						x.Ejemplo = ptr(strings.TrimPrefix(exs1, base))
					}
					d.Botones = append(d.Botones, x)
				case "PHONE_NUMBER":
					d.Botones = append(d.Botones, BotonPWA{Tipo: "telefono", Texto: b.Text, Telefono: ptr(b.PhoneNumber)})
				case "COPY_CODE", "OTP":
					d.Botones = append(d.Botones, BotonPWA{Tipo: "copiar_codigo", Texto: b.Text, Ejemplo: ptr(exs1)})
				}
			}
		}
	}
	datoPrevio := map[string]*string{}
	for _, v := range previas {
		datoPrevio[v.Nombre] = v.Dato
	}
	vistos := map[string]bool{}
	for _, n := range orden {
		if vistos[n] {
			continue
		}
		vistos[n] = true
		v := VariablePWA{Nombre: n, Ejemplo: ejemplos[n], Dato: datoPrevio[n]}
		if v.Dato == nil {
			if _, ok := DatoPorClave(n); ok {
				v.Dato = ptr(n)
			}
		}
		d.Variables = append(d.Variables, v)
	}
	return d
}
