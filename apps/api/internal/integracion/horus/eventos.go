package horus

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Eventos de Horus (§2). Llegan por webhook con estos nombres y, normalizados, salen por el
// outbox con el mismo nombre como eventos internos de Iris (soporte se suscribe a ellos).
const (
	EventoRouterEstadoCambiado  = "horus.router_estado_cambiado"
	EventoServicioInfraCambiado = "horus.servicio_infra_cambiado"
	EventoHallazgoAbierto       = "horus.hallazgo_abierto"
	EventoHallazgoResuelto      = "horus.hallazgo_resuelto"
)

// Estados de un router o servicio de infraestructura.
const (
	EstadoCaido  = "caido"
	EstadoArriba = "arriba"
)

// Payloads de los eventos internos (outbox). Las claves van en snake_case como el resto del
// catálogo (contratos/eventos.md). empresa_id también va en el sobre del evento.

// RouterEstadoCambiado es el payload de horus.router_estado_cambiado.
type RouterEstadoCambiado struct {
	RouterID   int64     `json:"router_id"`
	Nombre     string    `json:"nombre"`
	Estado     string    `json:"estado"`
	OcurridoEn time.Time `json:"ocurrido_en"`
	EmpresaID  *int64    `json:"empresa_id"`
	EntranteID int64     `json:"entrante_id"`
}

// ServicioInfraCambiado es el payload de horus.servicio_infra_cambiado.
type ServicioInfraCambiado struct {
	InfraID    string    `json:"infra_id"`
	Nombre     string    `json:"nombre"`
	Tipo       string    `json:"tipo"`
	Estado     string    `json:"estado"`
	RouterID   *int64    `json:"router_id"`
	OcurridoEn time.Time `json:"ocurrido_en"`
	EmpresaID  *int64    `json:"empresa_id"`
	EntranteID int64     `json:"entrante_id"`
}

// HallazgoAbierto es el payload de horus.hallazgo_abierto. cliente_id, servicio_id y
// empresa_id salen de Argos (IP → cliente); si no se pudo, van en null y motivo_sin_cliente
// dice por qué.
type HallazgoAbierto struct {
	HallazgoID       string    `json:"hallazgo_id"`
	Categoria        string    `json:"categoria"`
	Severidad        string    `json:"severidad"`
	IP               *string   `json:"ip"`
	RouterID         *int64    `json:"router_id"`
	Resumen          string    `json:"resumen"`
	URLHorus         *string   `json:"url_horus"`
	ClienteID        *int64    `json:"cliente_id"`
	ServicioID       *int64    `json:"servicio_id"`
	EmpresaID        *int64    `json:"empresa_id"`
	FuenteCliente    *string   `json:"fuente_cliente"`
	MotivoSinCliente *string   `json:"motivo_sin_cliente"`
	OcurridoEn       time.Time `json:"ocurrido_en"`
	EntranteID       int64     `json:"entrante_id"`
}

// HallazgoResuelto es el payload de horus.hallazgo_resuelto. Sin IP ni cliente (minimización):
// soporte lo vincula por hallazgo_id con el ticket que abrió horus.hallazgo_abierto.
type HallazgoResuelto struct {
	HallazgoID string    `json:"hallazgo_id"`
	Categoria  string    `json:"categoria"`
	Resumen    string    `json:"resumen"`
	OcurridoEn time.Time `json:"ocurrido_en"`
	EmpresaID  *int64    `json:"empresa_id"`
	EntranteID int64     `json:"entrante_id"`
}

// Motivos por los que un webhook de Horus se guarda como «ignorado» (sin evento interno).
const (
	notaEventoDesconocido = "evento_desconocido"
	notaPayloadInvalido   = "payload_invalido: "
)

// MotivoSinIP: el hallazgo no trae IP (no hay cliente que buscar).
const MotivoSinIP = "sin_ip"

// Lectura tolerante del payload ---------------------------------------------------------------

// datos es el payload normalizado (claves en snake_case) leído campo por campo: un campo con
// un tipo inesperado no invalida a los demás.
type datos map[string]json.RawMessage

func leerDatos(payload []byte) datos {
	var d datos
	if conector.Decodificar(payload, &d) != nil {
		return datos{}
	}
	return d
}

// texto devuelve el primer valor no vacío (texto o número) entre las claves.
func (d datos) texto(claves ...string) string {
	for _, k := range claves {
		var t conector.Texto
		if crudo, ok := d[k]; ok && json.Unmarshal(crudo, &t) == nil && t != "" {
			return string(t)
		}
	}
	return ""
}

// entero devuelve el primer id entero positivo (número o texto numérico) entre las claves.
func (d datos) entero(claves ...string) *int64 {
	for _, k := range claves {
		s := d.texto(k)
		if s == "" {
			continue
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return &n
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 && f == float64(int64(f)) {
			n := int64(f)
			return &n
		}
	}
	return nil
}

// objeto devuelve un objeto anidado ({ router: { id, nombre } }).
func (d datos) objeto(clave string) datos {
	var o datos
	if crudo, ok := d[clave]; ok && json.Unmarshal(crudo, &o) == nil {
		return o
	}
	return datos{}
}

// recortar limpia y acota un texto (en runas).
func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// codigo normaliza una categoría, severidad o tipo: minúsculas, sin espacios a los lados.
func codigo(s string) string {
	return recortar(strings.ToLower(s), 100)
}

// estadoDe normaliza el estado de un router o servicio (también los nombres en inglés).
func estadoDe(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "caido", "caído", "down", "offline", "fuera_de_linea", "fuera de línea":
		return EstadoCaido
	case "arriba", "up", "online", "en_linea", "en línea", "restablecido":
		return EstadoArriba
	}
	return ""
}

// ipDe valida y normaliza la IP del hallazgo; "" si no viene, inválida = false.
func ipDe(s string) (ip string, valida bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return "", false
	}
	return a.Unmap().String(), true
}

// urlHorus acepta el enlace del hallazgo solo si apunta al mismo origen que el conector (su
// URL base o la pública): un webhook no puede hacer que el panel enlace a otro sitio.
func urlHorus(crudo string, origenes ...string) *string {
	crudo = strings.TrimSpace(crudo)
	if crudo == "" || len(crudo) > 2000 {
		return nil
	}
	u, err := url.Parse(crudo)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil
	}
	for _, o := range origenes {
		base, err := url.Parse(o)
		if err == nil && strings.EqualFold(base.Scheme, u.Scheme) && strings.EqualFold(base.Host, u.Host) {
			s := u.String()
			return &s
		}
	}
	return nil
}

// Normalización por evento ------------------------------------------------------------------

// normalizado es un webhook de Horus ya validado, listo para emitir (uno de los payloads).
type normalizado struct {
	evento string
	// payload es *RouterEstadoCambiado, *ServicioInfraCambiado, *HallazgoAbierto o
	// *HallazgoResuelto.
	payload any
	// resolverIP: el hallazgo trae una IP válida y hay que preguntar a Argos de quién es.
	resolverIP string
	// routerSinEmpresa: el router no trajo empresa_id; se busca en la instantánea de Argos.
	routerSinEmpresa *int64
}

// normalizar valida el payload del entrante. nota ≠ "" = se ignora (evento desconocido o
// payload inválido) y no sale evento interno.
func normalizar(e conector.Entrante, origenes ...string) (*normalizado, string) {
	d := leerDatos(e.Payload)
	ocurrido := e.RecibidoEn.UTC()
	if e.OcurridoEn != nil {
		ocurrido = e.OcurridoEn.UTC()
	}
	empresa := d.entero("empresa_id")
	router := d.objeto("router")
	routerID := d.entero("router_id")
	if routerID == nil {
		routerID = router.entero("id")
	}
	switch e.Evento {
	case EventoRouterEstadoCambiado:
		estado := estadoDe(d.texto("estado"))
		switch {
		case routerID == nil:
			return nil, notaPayloadInvalido + "falta router_id"
		case estado == "":
			return nil, notaPayloadInvalido + "estado debe ser caido o arriba"
		}
		p := &RouterEstadoCambiado{
			RouterID: *routerID, Nombre: recortar(primero(d.texto("nombre", "router_nombre"), router.texto("nombre")), 200),
			Estado: estado, OcurridoEn: ocurrido, EmpresaID: empresa, EntranteID: e.ID,
		}
		n := &normalizado{evento: e.Evento, payload: p}
		if empresa == nil {
			n.routerSinEmpresa = routerID
		}
		return n, ""
	case EventoServicioInfraCambiado:
		infra := d.objeto("servicio")
		id := recortar(primero(d.texto("infra_id", "servicio_infra_id", "id"), infra.texto("id")), 200)
		estado := estadoDe(d.texto("estado"))
		switch {
		case id == "":
			return nil, notaPayloadInvalido + "falta infra_id"
		case estado == "":
			return nil, notaPayloadInvalido + "estado debe ser caido o arriba"
		}
		p := &ServicioInfraCambiado{
			InfraID: id, Nombre: recortar(primero(d.texto("nombre"), infra.texto("nombre")), 200),
			Tipo: codigo(primero(d.texto("tipo"), infra.texto("tipo"))), Estado: estado, RouterID: routerID,
			OcurridoEn: ocurrido, EmpresaID: empresa, EntranteID: e.ID,
		}
		n := &normalizado{evento: e.Evento, payload: p}
		if empresa == nil && routerID != nil {
			n.routerSinEmpresa = routerID
		}
		return n, ""
	case EventoHallazgoAbierto, EventoHallazgoResuelto:
		hallazgo := d.objeto("hallazgo")
		id := recortar(primero(d.texto("hallazgo_id", "id"), hallazgo.texto("id")), 200)
		categoria := codigo(primero(d.texto("categoria"), hallazgo.texto("categoria")))
		ip, valida := ipDe(primero(d.texto("ip"), hallazgo.texto("ip")))
		resumen := recortar(primero(d.texto("resumen", "titulo", "descripcion"), hallazgo.texto("resumen")), 500)
		var ipPtr *string
		if ip != "" {
			ipPtr = &ip
		}
		if id == "" {
			return nil, notaPayloadInvalido + "falta hallazgo_id"
		}
		if e.Evento == EventoHallazgoResuelto {
			return &normalizado{evento: e.Evento, payload: &HallazgoResuelto{
				HallazgoID: id, Categoria: categoria, Resumen: resumen, OcurridoEn: ocurrido, EmpresaID: empresa, EntranteID: e.ID,
			}}, ""
		}
		if categoria == "" {
			return nil, notaPayloadInvalido + "falta categoria"
		}
		p := &HallazgoAbierto{
			HallazgoID: id, Categoria: categoria, Severidad: codigo(primero(d.texto("severidad"), hallazgo.texto("severidad"))),
			IP: ipPtr, RouterID: routerID, Resumen: resumen, URLHorus: urlHorus(primero(d.texto("url", "url_horus"), hallazgo.texto("url")), origenes...),
			EmpresaID: empresa, OcurridoEn: ocurrido, EntranteID: e.ID,
		}
		n := &normalizado{evento: e.Evento, payload: p}
		switch {
		case !valida:
			motivo := "ip_invalida"
			p.MotivoSinCliente = &motivo
		case ip == "":
			motivo := MotivoSinIP
			p.MotivoSinCliente = &motivo
		default:
			n.resolverIP = ip
		}
		return n, ""
	}
	return nil, notaEventoDesconocido
}

func primero(valores ...string) string {
	for _, v := range valores {
		if v != "" {
			return v
		}
	}
	return ""
}

// empresaDe devuelve la empresa del payload normalizado (la del sobre del evento).
func (n *normalizado) empresaDe() *int64 {
	switch p := n.payload.(type) {
	case *RouterEstadoCambiado:
		return p.EmpresaID
	case *ServicioInfraCambiado:
		return p.EmpresaID
	case *HallazgoAbierto:
		return p.EmpresaID
	case *HallazgoResuelto:
		return p.EmpresaID
	}
	return nil
}

// fijarEmpresaRouter completa la empresa de un evento de router con la de la instantánea.
func (n *normalizado) fijarEmpresaRouter(empresa *int64) {
	switch p := n.payload.(type) {
	case *RouterEstadoCambiado:
		p.EmpresaID = empresa
	case *ServicioInfraCambiado:
		p.EmpresaID = empresa
	}
}
