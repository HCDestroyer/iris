package argos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
)

// Webhooks del contrato 0.5.0 (§2.3): Argos ≥ 1.6.0 solo los manda a conectores con contrato ≥ 0.5.0
// (Iris lo informa en el canje y con PUT /integraciones/iris/contrato).
const (
	EventoServicioDatosCambiados = "servicio.datos_cambiados"
	EventoCatalogoCambiado       = "catalogo.cambiado"
	EventoTrasladoEnviado        = "inventario.traslado_enviado"
	EventoTrasladoRecibido       = "inventario.traslado_recibido"
	EventoTrasladoCancelado      = "inventario.traslado_cancelado"
)

// Eventos internos (outbox) que emite Iris con ellos.
const (
	// EventoDatosCambiados: cambiaron en Argos datos de ubicación o contacto que copian las órdenes
	// (servicio.datos_cambiados, o cliente.actualizado con esos campos). Campo marca sus órdenes
	// abiertas «dato de Argos cambió» (no las pisa).
	EventoDatosCambiados = "argos.datos_cambiados"
	// EventoTrasladoCambiado: una orden de traslado hacia la bodega de un técnico se envió, recibió o
	// canceló (aviso para la app de campo).
	EventoTrasladoCambiado = "argos.traslado_cambiado"
)

// camposOrden: los campos de Argos que una orden copia al crearse (dirección, coordenadas, referencia
// y contacto); un cambio en ellos marca las órdenes abiertas.
var camposOrden = []string{"direccion", "coordenadas", "referencia", "contacto_nombre", "contacto_telefono", "telefono", "movil", "nombre"}

// camposQueCopianLasOrdenes: los de campos que copian las órdenes, con prefijo (cliente.direccion).
func camposQueCopianLasOrdenes(entidad string, campos []string) []string {
	salida := []string{}
	for _, c := range campos {
		if slices.Contains(camposOrden, c) {
			salida = append(salida, entidad+"."+c)
		}
	}
	slices.Sort(salida)
	return slices.Compact(salida)
}

// payload050 son los campos de los webhooks 0.5.0 que no están en payloadEntrante.
type payload050 struct {
	EmpresaID *int64   `json:"empresa_id"`
	Campos    []string `json:"campos"`
	// catalogo.cambiado
	Catalogo string `json:"catalogo"`
	ID       *int64 `json:"id"`
	// inventario.traslado_*
	TrasladoID      *int64          `json:"traslado_id"`
	BodegaDestinoID *int64          `json:"bodega_destino_id"`
	Tecnico         *RefTecnicoArgo `json:"tecnico"`
	Estado          string          `json:"estado"`
	Lineas          json.RawMessage `json:"lineas"`
	EnviadoEn       *time.Time      `json:"enviado_en"`
	RecibidoEn      *time.Time      `json:"recibido_en"`
	RecibidoPor     json.RawMessage `json:"recibido_por"`
	MetodoRecepcion string          `json:"metodo_recepcion"`
	Origen          string          `json:"origen"`
	CanceladoEn     *time.Time      `json:"cancelado_en"`
	Motivo          string          `json:"motivo"`
}

// RefTecnicoArgo es el técnico de un webhook de traslado ({ id_externo, correo } o null).
type RefTecnicoArgo struct {
	IDExterno IDExterno `json:"id_externo"`
	Correo    string    `json:"correo"`
}

func leerPayload050(crudo []byte) payload050 {
	var p payload050
	_ = conector.Decodificar(crudo, &p)
	return p
}

// cambioCatalogo: lo que se leyó de Argos para catalogo.cambiado (nil = no cambia).
type cambioCatalogo struct {
	Catalogo string   `json:"catalogo"`
	ID       int64    `json:"id"`
	Nombre   *string  `json:"nombre,omitempty"`
	Activo   *bool    `json:"activo,omitempty"`
	Lat      *float64 `json:"lat,omitempty"`
	Lon      *float64 `json:"lon,omitempty"`
	conCoord bool
}

// leerCatalogo relee de Argos el elemento del catálogo (fuera de transacción): zona en GET /zonas,
// router con GET /routers/:id, plan en GET /planes. Si ya no aparece y el aviso es de `activo`, queda
// inactivo. omitido explica por qué no se aplica nada.
func (p *ProcesarEntrante) leerCatalogo(ctx context.Context, q conector.DBTX, e conector.Entrante) (*cambioCatalogo, string, error) {
	d := leerPayload050(e.Payload)
	if d.ID == nil || *d.ID <= 0 {
		return nil, "catalogo_sin_id", nil
	}
	c := &cambioCatalogo{Catalogo: d.Catalogo, ID: *d.ID}
	inactivo := func() (*cambioCatalogo, string, error) {
		if slices.Contains(d.Campos, "activo") {
			f := false
			c.Activo = &f
			return c, "", nil
		}
		return nil, "catalogo_no_encontrado", nil
	}
	cli, err := p.clienteArgos(ctx, q, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	switch d.Catalogo {
	case "zona":
		zonas, err := cli.Zonas(ctx)
		if err != nil {
			return nil, "", err
		}
		for _, z := range zonas {
			if z.ID == c.ID {
				c.Nombre, c.Activo = textoNoVacio(z.Nombre), activoDe(z.Activo, z.Estado)
				return c, "", nil
			}
		}
		return inactivo()
	case "router":
		r, err := cli.ObtenerRouter(ctx, c.ID)
		if conector.EsNoEncontradoConfirmado(err) {
			// Solo un 404 problem+json no_encontrado confirma que ya no está (§2.3).
			return inactivo()
		}
		if err != nil {
			return nil, "", err
		}
		c.Nombre, c.Activo = textoNoVacio(r.Nombre), activoDe(r.Activo, r.Estado)
		if slices.Contains(d.Campos, "coordenadas") {
			c.Lat, c.Lon = r.LatLon()
			c.conCoord = true
		}
		return c, "", nil
	case "plan":
		planes, err := cli.Planes(ctx)
		if err != nil {
			return nil, "", err
		}
		for _, pl := range planes {
			if pl.ID == c.ID {
				c.Nombre = textoNoVacio(pl.Nombre)
				return c, "", nil
			}
		}
		return nil, "catalogo_no_encontrado", nil
	}
	return nil, "catalogo_desconocido", nil
}

func textoNoVacio(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// activoDe: activo (si vino) o, si no, el estado («inactivo»/«inactiva» = false). Sin datos, true.
func activoDe(activo *bool, estado string) *bool {
	if activo != nil {
		return activo
	}
	v := !strings.HasPrefix(strings.ToLower(strings.TrimSpace(estado)), "inactiv")
	return &v
}

// aplicarCatalogo actualiza la instantánea (zonas, routers), los nombres copiados en servicios y
// clientes, y la zona espejo de agenda (nombre y activa).
func aplicarCatalogo(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, c cambioCatalogo) error {
	var n int64
	var err error
	switch c.Catalogo {
	case "zona":
		if n, err = q.CambiarZonaArgos(ctx, argosbd.CambiarZonaArgosParams{ID: c.ID, Nombre: c.Nombre, Activo: c.Activo}); err != nil {
			return err
		}
		if c.Nombre != nil {
			m, err := q.CambiarZonaCopiada(ctx, argosbd.CambiarZonaCopiadaParams{Nombre: *c.Nombre, ZonaID: c.ID})
			if err != nil {
				return err
			}
			n += m
		}
		if err := cambiarZonaAgenda(ctx, t, q, c); err != nil {
			return err
		}
	case "router":
		if n, err = q.CambiarRouterArgos(ctx, argosbd.CambiarRouterArgosParams{ID: c.ID, Nombre: c.Nombre, Activo: c.Activo,
			ConCoordenadas: c.conCoord, Lat: c.Lat, Lon: c.Lon}); err != nil {
			return err
		}
		if c.Nombre != nil {
			m, err := q.CambiarRouterCopiado(ctx, argosbd.CambiarRouterCopiadoParams{Nombre: *c.Nombre, RouterID: c.ID})
			if err != nil {
				return err
			}
			n += m
		}
	case "plan":
		if c.Nombre != nil {
			if n, err = q.CambiarPlanArgos(ctx, argosbd.CambiarPlanArgosParams{Nombre: *c.Nombre, ID: &c.ID}); err != nil {
				return err
			}
		}
	}
	t.Anotar(map[string]any{"catalogo": c.Catalogo, "catalogo_id": c.ID, "filas": n})
	return nil
}

// trasladoDe arma el traslado del webhook (nil si no trae traslado_id).
func trasladoDe(e conector.Entrante, con conectorbd.IntegracionConectore) (*argosbd.GuardarTrasladoParams, string) {
	d := leerPayload050(e.Payload)
	if d.TrasladoID == nil || *d.TrasladoID <= 0 {
		return nil, "traslado_sin_id"
	}
	if d.EmpresaID != nil && !empresaEnAlcance(con, *d.EmpresaID) {
		return nil, "empresa_fuera_de_alcance"
	}
	lineas, ok := lineasTraslado(d.Lineas)
	if !ok {
		return nil, "traslado_lineas_invalidas"
	}
	// Fuera de los valores conocidos no se guarda (el estado del traslado sí: lo necesita la app).
	metodo, _ := enumOpcional(d.MetodoRecepcion, metodosRecepcion)
	origen, _ := enumOpcional(d.Origen, origenesRecepcion)
	estado := map[string]string{EventoTrasladoEnviado: "enviado", EventoTrasladoRecibido: "recibido", EventoTrasladoCancelado: "cancelado"}[e.Evento]
	entrante := e.ID
	p := &argosbd.GuardarTrasladoParams{
		ID: *d.TrasladoID, EmpresaID: d.EmpresaID, BodegaDestinoID: d.BodegaDestinoID, Estado: estado, Lineas: lineas,
		EnviadoEn: d.EnviadoEn, RecibidoEn: d.RecibidoEn, CanceladoEn: d.CanceladoEn, EntranteID: &entrante,
		MetodoRecepcion: metodo, OrigenRecepcion: origen, Motivo: recortarPtr(d.Motivo, 500),
		RecibidoPor: recibidoPor(d.RecibidoPor),
	}
	if d.Tecnico != nil {
		p.TecnicoIDExterno = recortarPtr(string(d.Tecnico.IDExterno), 100)
		p.TecnicoCorreo = recortarPtr(d.Tecnico.Correo, 320)
	}
	return p, ""
}

// Valores de la recepción que Iris acepta en el webhook (contrato 0.5.0 §2.3 y §3.3).
var (
	metodosRecepcion  = []string{"firma_digital", "manual", "panel"}
	origenesRecepcion = []string{"iris", "panel"}
)

// maxLineasTraslado acota lo que se guarda de un webhook.
const maxLineasTraslado = 200

// enumOpcional: vacío = nil; si no, debe ser uno de validos.
func enumOpcional(v string, validos []string) (*string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, true
	}
	if len(v) > 40 || !slices.Contains(validos, v) {
		return nil, false
	}
	return &v, true
}

// empresaEnAlcance: la empresa está en el alcance que informó el canje del conector (alcance global o
// sin informar: cualquiera; si no, una de sus empresas).
func empresaEnAlcance(con conectorbd.IntegracionConectore, empresa int64) bool {
	if con.AlcanceGlobal != nil && *con.AlcanceGlobal {
		return true
	}
	if con.AlcanceGlobal == nil && len(con.Empresas) == 0 {
		return true
	}
	return slices.Contains(con.Empresas, empresa)
}

// lineasTraslado valida y normaliza lineas[]: a lo más 200, cada una { linea_id?, producto_id (entero >
// 0), cantidad (número ≥ 0), series[] (textos de hasta 100, a lo más 1000) }. Ausente = [].
func lineasTraslado(crudo json.RawMessage) ([]byte, bool) {
	crudo = bytes.TrimSpace(crudo)
	if len(crudo) == 0 || string(crudo) == "null" {
		return []byte("[]"), true
	}
	var lineas []struct {
		ID         *int64          `json:"id"`
		LineaID    *int64          `json:"linea_id"`
		ProductoID int64           `json:"producto_id"`
		Cantidad   conector.Numero `json:"cantidad"`
		Series     []string        `json:"series"`
	}
	if conector.Decodificar(crudo, &lineas) != nil || len(lineas) > maxLineasTraslado {
		return nil, false
	}
	salida := make([]map[string]any, 0, len(lineas))
	for _, l := range lineas {
		cant := l.Cantidad.Float()
		if l.ProductoID <= 0 || cant == nil || *cant < 0 || len(l.Series) > 1000 {
			return nil, false
		}
		series := make([]string, 0, len(l.Series))
		for _, s := range l.Series {
			if s = strings.TrimSpace(s); s == "" || len(s) > 100 {
				return nil, false
			}
			series = append(series, s)
		}
		linea := l.LineaID
		if linea == nil {
			linea = l.ID
		}
		m := map[string]any{"producto_id": l.ProductoID, "cantidad": *cant, "series": series}
		if linea != nil && *linea > 0 {
			m["linea_id"] = *linea
		}
		salida = append(salida, m)
	}
	b, err := json.Marshal(salida)
	return b, err == nil
}

// recibidoPor: el técnico ({ id_externo, correo }) o el texto que mande Argos.
func recibidoPor(crudo json.RawMessage) *string {
	crudo = bytes.TrimSpace(crudo)
	if len(crudo) == 0 || string(crudo) == "null" {
		return nil
	}
	var r RefTecnicoArgo
	if crudo[0] == '{' && json.Unmarshal(crudo, &r) == nil {
		if r.IDExterno != "" {
			return recortarPtr(string(r.IDExterno), 200)
		}
		return recortarPtr(r.Correo, 200)
	}
	var s string
	if json.Unmarshal(crudo, &s) == nil {
		return recortarPtr(s, 200)
	}
	return recortarPtr(string(crudo), 200)
}

func recortarPtr(s string, n int) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return &s
}

// errSinCambios: el webhook no cambió nada (resultado «ignorado»).
var errSinCambios = errors.New("argos: sin cambios")

// aplicarTraslado guarda el traslado (un estado final no vuelve a enviado) y avisa.
func aplicarTraslado(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, p argosbd.GuardarTrasladoParams) error {
	f, err := q.GuardarTraslado(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		// Ya estaba en un estado final distinto: no se toca ni se avisa.
		t.Anotar(map[string]any{"traslado_id": p.ID, "omitido": "traslado_en_estado_final"})
		return errSinCambios
	}
	if err != nil {
		return err
	}
	var usuario *int64
	if f.TecnicoIDExterno != nil || f.TecnicoCorreo != nil {
		u, err := q.UsuarioDeVinculoArgos(ctx, argosbd.UsuarioDeVinculoArgosParams{IDExterno: deref(f.TecnicoIDExterno), Correo: deref(f.TecnicoCorreo)})
		if err == nil {
			usuario = &u
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	t.Anotar(map[string]any{"traslado_id": f.ID, "estado": f.Estado})
	return t.Emitir(EventoTrasladoCambiado, map[string]any{
		"traslado_id": f.ID, "estado": f.Estado, "empresa_id": f.EmpresaID, "usuario_id": usuario, "entrante_id": p.EntranteID,
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// emitirDatosCambiados: el evento interno para que campo marque sus órdenes abiertas.
func emitirDatosCambiados(t *auditoria.Tx, e conector.Entrante, entidad string, clienteID, servicioID *int64) error {
	d := leerPayload050(e.Payload)
	campos := camposQueCopianLasOrdenes(entidad, d.Campos)
	if len(campos) == 0 || (clienteID == nil && servicioID == nil) {
		return nil
	}
	if d.EmpresaID != nil {
		t.FijarEmpresa(*d.EmpresaID)
	}
	return t.Emitir(EventoDatosCambiados, map[string]any{
		"entidad": entidad, "cliente_id": clienteID, "servicio_id": servicioID, "empresa_id": d.EmpresaID, "campos": campos,
		"entrante_id": e.ID,
	})
}

// cambiarZonaAgenda aplica el nombre y activa de Argos a la zona espejo de agenda (si existe y cambió),
// auditado y con zona.actualizada { zona_id, campos[], activa }, como agenda.editar_zona.
func cambiarZonaAgenda(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, c cambioCatalogo) error {
	antes, err := q.ZonaAgendaArgos(ctx, c.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	despues, err := q.CambiarZonaAgenda(ctx, argosbd.CambiarZonaAgendaParams{ID: c.ID, Nombre: c.Nombre, Activo: c.Activo})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	campos := []string{}
	if antes.Nombre != despues.Nombre {
		campos = append(campos, "nombre")
	}
	if antes.Activa != despues.Activa {
		campos = append(campos, "activa")
	}
	t.RegistrarCambios("zona", despues.ID, map[string]any{"nombre": antes.Nombre, "activa": antes.Activa},
		map[string]any{"nombre": despues.Nombre, "activa": despues.Activa})
	empresa := int64(0)
	if despues.EmpresaID != nil {
		empresa = *despues.EmpresaID
	}
	t.FijarEmpresa(empresa)
	defer t.FijarEmpresa(0)
	return t.Emitir("zona.actualizada", map[string]any{"zona_id": despues.ID, "campos": campos, "activa": despues.Activa})
}
