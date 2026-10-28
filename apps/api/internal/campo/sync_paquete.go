package campo

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Paquete del día y cambios por cursor (contratos/sync.md §1). Claves en camelCase, como el
// resto de la API.

// Motivos de una sección del paquete sin datos de Argos.
const (
	MotivoSinConector       = "sin_conector"
	MotivoArgosNoDisponible = "argos_no_disponible"
	MotivoSinServicio       = "sin_servicio"
	MotivoSinBodega         = "sin_bodega"
)

// ClienteCampo es la instantánea del cliente de Argos (integracion.argos_clientes).
type ClienteCampo struct {
	ID             int64     `json:"id"`
	Codigo         string    `json:"codigo"`
	Nombre         string    `json:"nombre"`
	Telefono       string    `json:"telefono"`
	Direccion      string    `json:"direccion"`
	Lat            *float64  `json:"lat" nullable:"true"`
	Lon            *float64  `json:"lon" nullable:"true"`
	Estado         string    `json:"estado"`
	SincronizadoEn time.Time `json:"sincronizadoEn"`
}

// ServicioCampo es la instantánea del servicio de Argos (integracion.argos_servicios).
type ServicioCampo struct {
	ID             int64     `json:"id"`
	Codigo         string    `json:"codigo"`
	Plan           string    `json:"plan"`
	Router         string    `json:"router"`
	IP             string    `json:"ip"`
	Direccion      string    `json:"direccion"`
	Lat            *float64  `json:"lat" nullable:"true"`
	Lon            *float64  `json:"lon" nullable:"true"`
	Estado         string    `json:"estado"`
	SincronizadoEn time.Time `json:"sincronizadoEn"`
}

// Comodato: equipos de Argos asignados al servicio. disponible = false (y motivo) si no se
// pudieron leer: el teléfono no debe suponer «sin equipos».
type Comodato struct {
	Disponible bool             `json:"disponible"`
	Motivo     *string          `json:"motivo" nullable:"true" enum:"sin_conector,argos_no_disponible,sin_servicio"`
	Equipos    []EquipoComodato `json:"equipos"`
}

// Existencias de las bodegas de las órdenes del paquete.
type Existencias struct {
	Disponible bool                `json:"disponible"`
	Motivo     *string             `json:"motivo" nullable:"true" enum:"sin_conector,argos_no_disponible,sin_bodega"`
	Bodegas    []ExistenciasBodega `json:"bodegas"`
}

// ExistenciasBodega son las existencias de una bodega.
type ExistenciasBodega struct {
	BodegaID int64        `json:"bodegaId"`
	Lineas   []Existencia `json:"lineas"`
}

// RutaCampo es la ruta publicada del día (agenda): el orden de visita y la llegada estimada.
// Solo trae las órdenes del paquete (las del técnico, vivas): las cerradas o reasignadas salen.
type RutaCampo struct {
	ID       int64   `json:"id"`
	OrdenIDs []int64 `json:"ordenIds" doc:"Órdenes en el orden de visita"`
	Version  int32   `json:"version,omitempty" doc:"Versión de la ruta (cambia al publicar otra o al quedar desactualizada)"`
	Fecha    string  `json:"fecha,omitempty" doc:"Día de la ruta (YYYY-MM-DD, America/Guatemala)"`
	// Desactualizada: una orden cambió de técnico o de hora después de publicarla (despacho debe
	// publicar otra). Mostrarlo; las órdenes nuevas del día van después de las de la ruta.
	Desactualizada bool          `json:"desactualizada,omitempty" doc:"Una orden cambió de técnico o de hora después de publicarla: despacho publicará otra"`
	Degradada      bool          `json:"degradada,omitempty" doc:"Tiempos en línea recta (OSRM no estaba disponible al proponerla)"`
	PublicadaEn    *time.Time    `json:"publicadaEn,omitempty"`
	Paradas        []ParadaCampo `json:"paradas,omitempty" doc:"Las de ordenIds, con la llegada estimada"`
}

// ParadaCampo: una parada de la ruta publicada.
type ParadaCampo struct {
	OrdenID         int64     `json:"ordenId"`
	Posicion        int16     `json:"posicion" doc:"Posición en la ruta publicada (1 = primera; puede saltar números si una orden salió)"`
	LlegadaEstimada time.Time `json:"llegadaEstimada"`
	SalidaEstimada  time.Time `json:"salidaEstimada"`
	FueraDeVentana  bool      `json:"fueraDeVentana" doc:"Se estima llegar después de la ventana acordada con el cliente"`
}

// OrdenCampo es una orden con lo que el técnico necesita sin señal.
type OrdenCampo struct {
	OrdenDetalle
	Cliente  *ClienteCampo  `json:"cliente,omitempty" doc:"Instantánea de Argos; ausente si Iris no la tiene"`
	Servicio *ServicioCampo `json:"servicio,omitempty" doc:"Instantánea de Argos; ausente si Iris no la tiene"`
	Comodato Comodato       `json:"comodato"`
}

// Paquete del día del técnico.
type Paquete struct {
	Fecha       string       `json:"fecha" doc:"Día del paquete (YYYY-MM-DD, America/Guatemala)"`
	ServidorEn  time.Time    `json:"servidorEn" doc:"Hora del servidor al armar el paquete"`
	Cursor      string       `json:"cursor" doc:"Cursor para GET /campo/cambios (tomado antes de leer: puede repetir algo, nunca saltarlo)"`
	Ordenes     []OrdenCampo `json:"ordenes" doc:"Asignadas al técnico: pendientes, programadas hasta ese día (también atrasadas), en camino y en sitio"`
	Existencias Existencias  `json:"existencias"`
	Ruta        *RutaCampo   `json:"ruta,omitempty" doc:"Ruta publicada del día (ausente si no hay): ordenes viene en su orden y las que no están en ella, después"`
}

// EntradaPaquete es la entrada de GET /campo/paquete.
type EntradaPaquete struct {
	Fecha string `query:"fecha" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"YYYY-MM-DD (America/Guatemala); hoy por defecto"`
}

// EntradaCambios es la entrada de GET /campo/cambios.
type EntradaCambios struct {
	Cursor string `query:"cursor" maxLength:"200" doc:"El del paquete o el de la respuesta anterior; vacío = desde el principio"`
	Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"100"`
}

// CambioOrden: una orden que cambió desde el cursor. orden = null si ya no es del técnico
// (reasignada): el teléfono la quita.
type CambioOrden struct {
	OrdenID int64       `json:"ordenId"`
	Tipo    string      `json:"tipo" enum:"nueva,actualizada,reprogramada,reasignada,cancelada"`
	Orden   *OrdenCampo `json:"orden,omitempty" doc:"Ausente si ya no es del técnico (reasignada): quitarla del teléfono"`
}

// Cambios desde un cursor.
type Cambios struct {
	Cambios []CambioOrden `json:"cambios" doc:"Una entrada por orden (el último cambio), en orden"`
	Cursor  string        `json:"cursor" doc:"Siguiente cursor"`
	HayMas  bool          `json:"hayMas" doc:"Quedan cambios: pedir otra vez con el cursor"`
	Ruta    *RutaCampo    `json:"ruta,omitempty" doc:"Ruta publicada de hoy (America/Guatemala) tal como está ahora; ausente si no hay. Reemplaza la del paquete si es del mismo día"`
}

func cursorDe(id int64) string { return nucleo.CodificarCursor(id) }

func (m *Modulo) nuevoLeerPaquete() *auditoria.Caso[EntradaPaquete, Paquete] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPaquete, Paquete]{
		Accion:  "campo.paquete",
		Permiso: nucleo.PermCampoSincronizar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPaquete) (Paquete, error) {
			q := campobd.New(t)
			actor := contexto.ActorDe(ctx)
			if !actor.EsUsuario() {
				return Paquete{}, nucleo.Prohibido("El paquete es de un técnico")
			}
			ahora := m.ahora()
			dia := ahora.In(m.cfg.ZonaHoraria)
			if in.Fecha != "" {
				d, err := time.ParseInLocation("2006-01-02", in.Fecha, m.cfg.ZonaHoraria)
				if err != nil {
					return Paquete{}, campoInvalido("fecha", "Fecha inválida (YYYY-MM-DD)", "fecha")
				}
				dia = d
			}
			inicioDia := time.Date(dia.Year(), dia.Month(), dia.Day(), 0, 0, 0, 0, m.cfg.ZonaHoraria)
			hasta := inicioDia.AddDate(0, 0, 1).UTC()
			// El cursor antes de leer: un cambio que entre en medio se repite en /cambios.
			ultimo, err := q.UltimoCambio(ctx, actor.ID)
			if err != nil {
				return Paquete{}, err
			}
			todas, empresa := filtroEmpresa(ctx)
			filas, err := q.OrdenesDelPaquete(ctx, campobd.OrdenesDelPaqueteParams{
				TecnicoID: &actor.ID, TodasEmpresas: todas, EmpresaID: empresa, Hasta: &hasta,
			})
			if err != nil {
				return Paquete{}, err
			}
			ordenes, err := ordenesCampo(ctx, q, filas)
			if err != nil {
				return Paquete{}, err
			}
			ids := make([]int64, 0, len(ordenes))
			for _, o := range ordenes {
				ids = append(ids, o.ID)
			}
			ruta, err := m.rutaCampo(ctx, t, actor.ID, inicioDia, ids)
			if err != nil {
				return Paquete{}, err
			}
			if ruta != nil {
				ordenarPorRuta(ordenes, ruta.OrdenIDs)
				ids = ids[:0]
				for _, o := range ordenes {
					ids = append(ids, o.ID)
				}
			}
			t.Anotar(map[string]any{"fecha": inicioDia.Format("2006-01-02"), "ordenes": ids})
			return Paquete{
				Fecha: inicioDia.Format("2006-01-02"), ServidorEn: ahora, Cursor: cursorDe(ultimo), Ordenes: ordenes,
				Existencias: Existencias{Bodegas: []ExistenciasBodega{}}, Ruta: ruta,
			}, nil
		},
	})
}

func (m *Modulo) nuevoLeerCambios() *auditoria.Caso[EntradaCambios, Cambios] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambios, Cambios]{
		Accion:       "campo.cambios",
		Permiso:      nucleo.PermCampoSincronizar,
		AuditarExito: func(c Cambios) bool { return len(c.Cambios) > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambios) (Cambios, error) {
			q := campobd.New(t)
			actor := contexto.ActorDe(ctx)
			if !actor.EsUsuario() {
				return Cambios{}, nucleo.Prohibido("Los cambios son de un técnico")
			}
			var desde int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &desde); err != nil {
				return Cambios{}, err
			}
			limite := in.Limit
			if limite <= 0 || limite > 200 {
				limite = 100
			}
			filas, err := q.CambiosDesde(ctx, campobd.CambiosDesdeParams{
				TecnicoID: actor.ID, DespuesDe: desde, Limite: int32(limite + 1), //nolint:gosec // ≤ 201
			})
			if err != nil {
				return Cambios{}, err
			}
			salida := Cambios{Cambios: []CambioOrden{}, Cursor: cursorDe(desde)}
			if len(filas) > limite {
				filas, salida.HayMas = filas[:limite], true
			}
			if len(filas) == 0 {
				return salida, m.rutaDeHoyEnCambios(ctx, t, q, &salida)
			}
			salida.Cursor = cursorDe(filas[len(filas)-1].ID)
			// Una entrada por orden: el último cambio, en el orden en que ocurrió.
			ultimo := map[int64]int{}
			for i, f := range filas {
				ultimo[f.OrdenID] = i
			}
			ids := []int64{}
			for i, f := range filas {
				if ultimo[f.OrdenID] == i {
					ids = append(ids, f.OrdenID)
				}
			}
			actuales, err := q.OrdenesPorIDs(ctx, ids)
			if err != nil {
				return Cambios{}, err
			}
			suyas := []campobd.CampoOrden{}
			for _, f := range actuales {
				if propia(ctx, f) && enEmpresa(ctx, f) {
					suyas = append(suyas, f)
				}
			}
			completas, err := ordenesCampo(ctx, q, suyas)
			if err != nil {
				return Cambios{}, err
			}
			porID := map[int64]*OrdenCampo{}
			for i := range completas {
				porID[completas[i].ID] = &completas[i]
			}
			esDeOtraEmpresa := map[int64]bool{}
			for _, f := range actuales {
				if !enEmpresa(ctx, f) {
					esDeOtraEmpresa[f.ID] = true
				}
			}
			for i, f := range filas {
				if ultimo[f.OrdenID] != i || esDeOtraEmpresa[f.OrdenID] {
					continue
				}
				c := CambioOrden{OrdenID: f.OrdenID, Tipo: f.Tipo, Orden: porID[f.OrdenID]}
				if c.Orden == nil {
					c.Tipo = "reasignada"
				} else if c.Tipo == "reasignada" {
					c.Tipo = "nueva" // se la quitaron y se la devolvieron
				}
				salida.Cambios = append(salida.Cambios, c)
			}
			t.Anotar(map[string]any{"ordenes": ids})
			return salida, m.rutaDeHoyEnCambios(ctx, t, q, &salida)
		},
	})
}

// rutaDeHoyEnCambios agrega a los cambios la ruta publicada de hoy con las órdenes vivas del
// técnico en la empresa activa.
func (m *Modulo) rutaDeHoyEnCambios(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, c *Cambios) error {
	actor := contexto.ActorDe(ctx)
	hoy := m.ahora().In(m.cfg.ZonaHoraria)
	dia := time.Date(hoy.Year(), hoy.Month(), hoy.Day(), 0, 0, 0, 0, m.cfg.ZonaHoraria)
	todas, empresa := filtroEmpresa(ctx)
	filas, err := q.OrdenesDelPaquete(ctx, campobd.OrdenesDelPaqueteParams{TecnicoID: &actor.ID, TodasEmpresas: todas,
		EmpresaID: empresa, Hasta: ptr(dia.AddDate(0, 0, 1).UTC())})
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(filas))
	for _, f := range filas {
		ids = append(ids, f.ID)
	}
	c.Ruta, err = m.rutaCampo(ctx, t, actor.ID, dia, ids)
	return err
}

// rutaCampo: la ruta publicada del técnico ese día en la empresa activa, solo con las órdenes
// dadas (las del paquete). nil si no hay ruta o ninguna de sus órdenes sigue en el paquete.
func (m *Modulo) rutaCampo(ctx context.Context, t *auditoria.Tx, tecnico int64, dia time.Time, ordenes []int64) (*RutaCampo, error) {
	todas, empresa := filtroEmpresa(ctx)
	if todas || (empresa != nil && *empresa == 0) {
		return nil, nil
	}
	r, err := m.cfg.Agenda.RutaPublicadaDelTecnico(ctx, t, empresa, tecnico, dia)
	if err != nil || r == nil {
		return nil, err
	}
	salida := &RutaCampo{ID: r.ID, OrdenIDs: []int64{}, Version: r.Version, Fecha: r.Fecha, Desactualizada: r.Desactualizada,
		Degradada: r.Motor != "osrm", Paradas: []ParadaCampo{}}
	if !r.PublicadaEn.IsZero() {
		salida.PublicadaEn = &r.PublicadaEn
	}
	for _, p := range r.Paradas {
		if !slices.Contains(ordenes, p.OrdenID) {
			continue
		}
		salida.OrdenIDs = append(salida.OrdenIDs, p.OrdenID)
		salida.Paradas = append(salida.Paradas, ParadaCampo{OrdenID: p.OrdenID, Posicion: p.Posicion,
			LlegadaEstimada: p.LlegadaEstimada, SalidaEstimada: p.SalidaEstimada, FueraDeVentana: p.FueraDeVentana})
	}
	if len(salida.OrdenIDs) == 0 {
		return nil, nil
	}
	return salida, nil
}

// ordenarPorRuta: primero las de la ruta en su orden; las demás después, en el orden que traían.
func ordenarPorRuta(ordenes []OrdenCampo, ruta []int64) {
	posicion := make(map[int64]int, len(ruta))
	for i, id := range ruta {
		posicion[id] = i
	}
	slices.SortStableFunc(ordenes, func(a, b OrdenCampo) int {
		pa, ea := posicion[a.ID]
		pb, eb := posicion[b.ID]
		switch {
		case ea && eb:
			return pa - pb
		case ea:
			return -1
		case eb:
			return 1
		}
		return 0
	})
}

// ordenesCampo arma las órdenes con sus instantáneas de Argos (sin el comodato: se lee fuera
// de la transacción, completarInventario).
func ordenesCampo(ctx context.Context, q *campobd.Queries, filas []campobd.CampoOrden) ([]OrdenCampo, error) {
	det, err := detalles(ctx, q, filas)
	if err != nil {
		return nil, err
	}
	clientes, servicios := []int64{}, []int64{}
	for _, f := range filas {
		if f.ClienteID != nil {
			clientes = append(clientes, *f.ClienteID)
		}
		if f.ServicioID != nil {
			servicios = append(servicios, *f.ServicioID)
		}
	}
	cs, err := q.ClientesArgos(ctx, clientes)
	if err != nil {
		return nil, err
	}
	ss, err := q.ServiciosArgos(ctx, servicios)
	if err != nil {
		return nil, err
	}
	salida := make([]OrdenCampo, 0, len(det))
	for _, d := range det {
		o := OrdenCampo{OrdenDetalle: d, Comodato: Comodato{Equipos: []EquipoComodato{}}}
		for _, c := range cs {
			// Solo la instantánea de la misma empresa que la orden.
			if d.ClienteID != nil && c.ID == *d.ClienteID && (d.EmpresaID == nil || *d.EmpresaID == c.EmpresaID) {
				o.Cliente = &ClienteCampo{ID: c.ID, Codigo: c.Codigo, Nombre: c.Nombre, Telefono: c.Telefono,
					Direccion: c.Direccion, Lat: c.Lat, Lon: c.Lon, Estado: c.Estado, SincronizadoEn: c.SincronizadoEn}
			}
		}
		for _, s := range ss {
			if d.ServicioID != nil && s.ID == *d.ServicioID && (d.EmpresaID == nil || *d.EmpresaID == s.EmpresaID) {
				o.Servicio = &ServicioCampo{ID: s.ID, Codigo: s.Codigo, Plan: s.PlanNombre, Router: s.RouterNombre, IP: s.Ip,
					Direccion: s.Direccion, Lat: s.Lat, Lon: s.Lon, Estado: s.Estado, SincronizadoEn: s.SincronizadoEn}
			}
		}
		salida = append(salida, o)
	}
	return salida, nil
}

// completarInventario lee de Argos (fuera de toda transacción) el comodato de cada orden y las
// existencias de sus bodegas. Sin conector o con Argos caído, disponible = false con el motivo.
func (m *Modulo) completarInventario(ctx context.Context, ordenes []*OrdenCampo, existencias *Existencias) {
	sinConector := ptr(MotivoSinConector)
	bodegas := []int64{}
	empresaDe := map[int64]int64{}
	for _, o := range ordenes {
		if o.BodegaID != nil {
			if _, ok := empresaDe[*o.BodegaID]; !ok {
				bodegas = append(bodegas, *o.BodegaID)
				empresaDe[*o.BodegaID] = deref(o.EmpresaID)
			}
		}
		switch {
		case m.cfg.Inventario == nil:
			o.Comodato.Motivo = sinConector
		case o.ServicioID == nil:
			o.Comodato.Motivo = ptr(MotivoSinServicio)
		default:
			eq, err := m.cfg.Inventario.EquiposEnComodato(ctx, deref(o.EmpresaID), *o.ServicioID)
			if err != nil {
				o.Comodato.Motivo = motivoInventario(err)
				continue
			}
			o.Comodato.Disponible, o.Comodato.Equipos = true, noNulo(eq)
		}
	}
	if existencias == nil {
		return
	}
	switch {
	case m.cfg.Inventario == nil:
		existencias.Motivo = sinConector
		return
	case len(bodegas) == 0:
		existencias.Motivo = ptr(MotivoSinBodega)
		return
	}
	for _, b := range bodegas {
		lineas, err := m.cfg.Inventario.Existencias(ctx, empresaDe[b], b)
		if err != nil {
			existencias.Motivo, existencias.Bodegas = motivoInventario(err), []ExistenciasBodega{}
			return
		}
		existencias.Bodegas = append(existencias.Bodegas, ExistenciasBodega{BodegaID: b, Lineas: noNulo(lineas)})
	}
	existencias.Disponible = true
}

// Paquete arma el paquete del día: el caso de uso (transacción y auditoría) y luego, fuera de
// la transacción, el inventario de Argos.
func (m *Modulo) Paquete(ctx context.Context, in EntradaPaquete) (Paquete, error) {
	p, err := m.LeerPaquete.Ejecutar(ctx, in)
	if err != nil {
		return p, err
	}
	refs := make([]*OrdenCampo, 0, len(p.Ordenes))
	for i := range p.Ordenes {
		refs = append(refs, &p.Ordenes[i])
	}
	m.completarInventario(ctx, refs, &p.Existencias)
	return p, nil
}

// Cambios devuelve los cambios desde el cursor, con el comodato de las órdenes.
func (m *Modulo) Cambios(ctx context.Context, in EntradaCambios) (Cambios, error) {
	c, err := m.LeerCambios.Ejecutar(ctx, in)
	if err != nil {
		return c, err
	}
	refs := []*OrdenCampo{}
	for i := range c.Cambios {
		if c.Cambios[i].Orden != nil {
			refs = append(refs, c.Cambios[i].Orden)
		}
	}
	m.completarInventario(ctx, refs, nil)
	return c, nil
}

// motivoInventario: sin conector activo (apagado) o Argos no disponible.
func motivoInventario(err error) *string {
	if errors.Is(err, ErrSinConector) {
		return ptr(MotivoSinConector)
	}
	return ptr(MotivoArgosNoDisponible)
}
