package agenda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Rutas del día: propuesta (OSRM o línea recta), publicación, lectura frente a la real y la
// marca de «desactualizada» cuando una orden cambia de técnico o de hora (ReservarCita,
// QuitarCita). Las llamadas a OSRM se hacen fuera de la transacción (ProponerRuta).

// maxParadas por ruta (la matriz de OSRM es de hasta 101 × 101 con la salida).
const maxParadas = 100

// EntradaProponerRuta: POST /rutas/proponer.
type EntradaProponerRuta struct {
	TecnicoID int64   `json:"tecnicoId" minimum:"1" doc:"Usuario técnico (el tecnicoId de las órdenes)"`
	Fecha     string  `json:"fecha,omitempty" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"Día local (YYYY-MM-DD, America/Guatemala); hoy por defecto"`
	Version   *int32  `json:"version,omitempty" minimum:"1" doc:"Versión del borrador que se rehace (opcional); si cambió, 409"`
	OrdenIDs  []int64 `json:"ordenIds,omitempty" maxItems:"100" doc:"Orden manual: todas las órdenes programadas del técnico ese día en el orden de visita (las ya en camino, en sitio o terminadas van primero en el orden real y se pueden omitir). Ausente = optimizada"`

	ahora time.Time
	pre   *preRuta
}

// EntradaPublicarRuta: POST /rutas/{id}/publicar.
type EntradaPublicarRuta struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1" doc:"Versión leída del borrador; si cambió, 409"`
}

// EntradaVerRuta: GET /rutas/{id}.
type EntradaVerRuta struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaListarRutas: GET /rutas.
type EntradaListarRutas struct {
	Fecha               string `query:"fecha" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"Día local (YYYY-MM-DD); hoy por defecto"`
	TecnicoID           int64  `query:"tecnicoId" minimum:"0" doc:"Usuario técnico"`
	CuadrillaID         int64  `query:"cuadrillaId" minimum:"0" doc:"Cuadrilla del técnico"`
	Estado              string `query:"estado" enum:"borrador,publicada,reemplazada," doc:"Sin estado: borradores y publicadas"`
	IncluirReemplazadas bool   `query:"incluirReemplazadas" doc:"También las publicadas antes que la actual"`
}

// preRuta: lo calculado antes de la transacción (OSRM y el plan). La transacción lo usa solo si
// la entrada del plan no cambió (huella); si no, calcula en línea recta.
type preRuta struct {
	huella string
	matriz Matriz
	motor  string
	motivo *string
	plan   *plan
	trazo  [][2]float64
}

func rutaNoEncontrada() *nucleo.Problema { return nucleo.NoEncontrado("La ruta no existe") }

// fechaDe: el día local de t como fecha (medianoche UTC, para la columna date).
func fechaDe(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

// diaLocal: medianoche local de una fecha de la columna date.
func diaLocal(fecha time.Time, loc *time.Location) time.Time {
	return time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 0, 0, 0, 0, loc)
}

// --- Entrada del plan ------------------------------------------------------------------

// datosRuta: lo leído de la base para proponer.
type datosRuta struct {
	entrada planEntrada
	citas   []agendabd.CitasParaRutaRow
	ficha   *agendabd.AgendaTecnico
}

var estadosFijos = []string{"en_camino", "en_sitio", "pausada", "cerrada", "fallida"}

// leerDatosRuta arma la entrada del plan (la misma fuera y dentro de la transacción).
func (m *Modulo) leerDatosRuta(ctx context.Context, q *agendabd.Queries, empresa *int64, tecnico int64, dia, ahora time.Time, manual []int64) (datosRuta, error) {
	loc := m.cfg.ZonaHoraria
	d := datosRuta{}
	f, err := q.TecnicoPorUsuario(ctx, tecnico)
	switch {
	case err == nil:
		d.ficha = &f
	case !errors.Is(err, pgx.ErrNoRows):
		return d, err
	}
	finDia := dia.AddDate(0, 0, 1)
	d.citas, err = q.CitasParaRuta(ctx, agendabd.CitasParaRutaParams{TecnicoID: tecnico, Desde: dia.UTC(), Hasta: finDia.UTC(), EmpresaID: empresa})
	if err != nil {
		return d, err
	}
	if len(d.citas) > maxParadas {
		return d, nucleo.ReglaNegocio("demasiadas_paradas", fmt.Sprintf("Una ruta lleva hasta %d paradas", maxParadas))
	}
	e := planEntrada{}
	if len(manual) > 0 {
		e.Manual = manual
	}
	// Salida: inicio del turno (el primero del día) o la primera cita; fin del turno.
	var tramos []intervalo
	if d.ficha != nil {
		turnos, err := q.TurnosDeTecnicos(ctx, []int64{d.ficha.ID})
		if err != nil {
			return d, err
		}
		tramos = turnosDelDia(turnos, d.ficha.ID, dia)
	}
	for _, c := range d.citas {
		if e.SaleEn.IsZero() || c.Inicio.Before(e.SaleEn) {
			e.SaleEn = c.Inicio
		}
	}
	for _, tr := range tramos {
		if e.SaleEn.IsZero() || tr.ini.Before(e.SaleEn) {
			e.SaleEn = tr.ini
		}
		if e.FinTurno == nil || tr.fin.After(*e.FinTurno) {
			fin := tr.fin
			e.FinTurno = &fin
		}
	}
	if e.SaleEn.IsZero() {
		e.SaleEn = dia.Add(8 * time.Hour).UTC()
	}
	hoy := medianoche(ahora, loc).Equal(dia)
	if hoy {
		a := ahora
		e.Ahora = &a
	}
	for _, c := range d.citas {
		p := paradaPlan{OrdenID: c.OrdenID, Duracion: time.Duration(c.DuracionMin) * time.Minute, VentanaIni: c.VentanaInicio,
			VentanaFin: c.VentanaFin, CitaInicio: c.Inicio}
		if c.Lat != nil && c.Lon != nil {
			p.Punto = &Punto{Lat: *c.Lat, Lon: *c.Lon}
		}
		if slices.Contains(estadosFijos, c.Estado) {
			p.Fija, p.LlegadaReal = true, c.EnSitioEn
			if c.Estado == "cerrada" || c.Estado == "fallida" {
				p.SalidaReal = c.FinalizadaEn
			}
			e.Fijas = append(e.Fijas, p)
			continue
		}
		e.Libres = append(e.Libres, p)
	}
	// Las fijas en el orden real (llegada, salida a camino o cierre).
	clave := func(p paradaPlan) time.Time {
		for _, c := range d.citas {
			if c.OrdenID == p.OrdenID {
				for _, t := range []*time.Time{c.EnSitioEn, c.EnCaminoEn, c.FinalizadaEn} {
					if t != nil {
						return *t
					}
				}
				return c.Inicio
			}
		}
		return time.Time{}
	}
	slices.SortStableFunc(e.Fijas, func(a, b paradaPlan) int { return clave(a).Compare(clave(b)) })
	// De dónde sale: con paradas hechas, el plan parte de la base y la parte libre de la última
	// hecha; hoy sin paradas hechas, de la última posición (2 h como mucho); si no, de la base;
	// sin base, de la primera parada.
	e.OrigenSalida = "primera_parada"
	if d.ficha != nil && d.ficha.BaseLat != nil && d.ficha.BaseLon != nil {
		e.Salida, e.OrigenSalida = &Punto{Lat: *d.ficha.BaseLat, Lon: *d.ficha.BaseLon}, "base"
	}
	if hoy && len(e.Fijas) == 0 && ahora.After(e.SaleEn) {
		u, err := q.UltimaPosicionEnEmpresa(ctx, agendabd.UltimaPosicionEnEmpresaParams{TecnicoID: tecnico, EmpresaID: empresa})
		switch {
		case err == nil && ahora.Sub(u.RegistradaEn) <= 2*time.Hour:
			e.Salida, e.OrigenSalida = &Punto{Lat: u.Lat, Lon: u.Lon}, "posicion"
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return d, err
		}
	}
	if len(e.Fijas) > 0 {
		e.OrigenSalida = "ultima_parada"
	}
	d.entrada = e
	return d, nil
}

// --- OSRM fuera de la transacción --------------------------------------------------------

// prepararRuta consulta OSRM (si está configurado), calcula el plan y pide su trazo. Todo fuera
// de la transacción: dentro solo se reutiliza si la entrada no cambió.
func (m *Modulo) prepararRuta(ctx context.Context, e planEntrada) *preRuta {
	puntos, _, _ := e.puntos()
	pre := &preRuta{huella: e.huellaPlan()}
	switch {
	case m.cfg.Enrutador == nil:
		pre.matriz, pre.motor, pre.motivo = MatrizEnLineaRecta(puntos), "haversine", ptr("sin_osrm")
	case len(puntos) < 2:
		pre.matriz, pre.motor = MatrizEnLineaRecta(puntos), "osrm"
	default:
		mt, err := m.cfg.Enrutador.Matriz(ctx, puntos)
		if err != nil {
			pre.matriz, pre.motor, pre.motivo = MatrizEnLineaRecta(puntos), "haversine", ptr("osrm_no_disponible")
			break
		}
		pre.matriz, pre.motor = mt, "osrm"
	}
	p, err := planificar(ctx, e, pre.matriz)
	if err != nil {
		return pre // orden manual inválido o petición cancelada: la transacción lo vuelve a ver
	}
	pre.plan = &p
	if pre.motor == "osrm" && len(p.Secuencia) >= 2 {
		if trazo, err := m.cfg.Enrutador.Trazo(ctx, p.Secuencia); err == nil && len(trazo) >= 2 {
			pre.trazo = trazo
		}
	}
	return pre
}

// ProponerRuta propone la ruta. Fuera de la transacción, y solo si el actor puede proponer la
// ruta de ese técnico (alcance sobre él y sobre todas sus paradas), lee las paradas, consulta
// OSRM y calcula el plan; luego corre el caso de uso rutas.proponer, que vuelve a leer y
// verificar todo y reutiliza el plan si la entrada no cambió (si cambió, línea recta).
func (m *Modulo) ProponerRuta(ctx context.Context, in EntradaProponerRuta) (RutaDetalle, error) {
	in.ahora = m.ahora()
	in.pre = nil
	if pre, ok := m.prefase(ctx, in); ok {
		in.pre = pre
	}
	return m.Proponer.Ejecutar(ctx, in)
}

// prefase: lo de ProponerRuta antes de la transacción. Cualquier duda (sin permiso, fuera de
// alcance, error de lectura) la deja sin hacer: la transacción da el error que corresponde.
func (m *Modulo) prefase(ctx context.Context, in EntradaProponerRuta) (*preRuta, bool) {
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermRutasProponer)) {
		return nil, false
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nil, false
	}
	todas, empresa := filtroEmpresa(ctx)
	dia, err := m.fechaLocalEn("fecha", in.Fecha, in.ahora)
	if err != nil || todas || (empresa != nil && *empresa == 0) {
		return nil, false
	}
	q := agendabd.New(bd)
	ficha, err := m.tecnicoDeRuta(ctx, bd, q, in.TecnicoID, nucleo.PermRutasProponer)
	if err != nil {
		return nil, false
	}
	d, err := m.leerDatosRuta(ctx, q, empresa, in.TecnicoID, dia, in.ahora, in.OrdenIDs)
	if err != nil || len(d.citas) == 0 {
		return nil, false
	}
	a := alcanceDe(ctx, nucleo.PermRutasProponer)
	for _, c := range d.citas {
		if !paradaVisible(a, c.ZonaID, c.CuadrillaID, ficha.CuadrillaID) {
			return nil, false
		}
	}
	return m.prepararRuta(ctx, d.entrada), true
}

// fechaLocalEn: como fechaLocal, con la hora dada.
func (m *Modulo) fechaLocalEn(campo, fecha string, ahora time.Time) (time.Time, error) {
	if fecha == "" {
		return medianoche(ahora, m.cfg.ZonaHoraria), nil
	}
	return m.fechaLocal(campo, fecha)
}

// --- Alcance -----------------------------------------------------------------------------

// tecnicoDeRuta: la ficha del técnico (usuario) visible para rutas.ver en la empresa activa y
// con el alcance del permiso de la acción sobre su cuadrilla. Falla cerrado: sin ficha, o con
// una cuadrilla que el alcance no cubre, 404 (o 403 si se ve pero la acción no alcanza).
func (m *Modulo) tecnicoDeRuta(ctx context.Context, db agendabd.DBTX, q *agendabd.Queries, tecnico int64, permiso nucleo.Permiso) (agendabd.AgendaTecnico, error) {
	noEsta := nucleo.NoEncontrado("El técnico no existe o no tiene ficha de agenda")
	f, err := q.TecnicoPorUsuario(ctx, tecnico)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEsta
	}
	if err != nil {
		return f, err
	}
	todas, empresa := filtroEmpresa(ctx)
	if !todas {
		en, err := identidad.UsuarioEnEmpresa(ctx, db, tecnico, deref(empresa))
		if err != nil {
			return f, err
		}
		if !en {
			return f, noEsta
		}
	}
	if ok, err := alcanceDe(ctx, nucleo.PermRutasVer).cubreCuadrilla(ctx, q, f.CuadrillaID); err != nil || !ok {
		if err != nil {
			return f, err
		}
		return f, noEsta
	}
	if permiso != nucleo.PermRutasVer {
		if ok, err := alcanceDe(ctx, permiso).cubreCuadrilla(ctx, q, f.CuadrillaID); err != nil || !ok {
			if err != nil {
				return f, err
			}
			return f, fueraDeAlcance(fmt.Sprintf("Tu permiso %s no cubre la cuadrilla de este técnico", permiso))
		}
	}
	return f, nil
}

// paradaVisible: el alcance ve la orden (global, su zona, su cuadrilla o la cuadrilla del
// técnico en el alcance por cuadrilla).
func paradaVisible(a alcance, zona, cuadrillaOrden, cuadrillaTecnico *int64) bool {
	return a.global ||
		(zona != nil && slices.Contains(a.zonas, *zona)) ||
		(cuadrillaOrden != nil && slices.Contains(a.cuadrillas, *cuadrillaOrden)) ||
		(cuadrillaTecnico != nil && slices.Contains(a.cuadrillas, *cuadrillaTecnico))
}

// tomarRuta: la ruta de la empresa activa, visible (rutas.ver) y con el alcance del permiso.
func (m *Modulo) tomarRuta(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, id int64, permiso nucleo.Permiso, paraActualizar bool) (agendabd.AgendaRuta, agendabd.AgendaTecnico, error) {
	t.FijarEntidad("ruta", id)
	var (
		r   agendabd.AgendaRuta
		err error
	)
	if paraActualizar {
		r, err = q.RutaPorIDParaActualizar(ctx, id)
	} else {
		r, err = q.RutaPorID(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return r, agendabd.AgendaTecnico{}, rutaNoEncontrada()
	}
	if err != nil {
		return r, agendabd.AgendaTecnico{}, err
	}
	todas, empresa := filtroEmpresa(ctx)
	if !todas && !mismoID(r.EmpresaID, empresa) {
		return r, agendabd.AgendaTecnico{}, rutaNoEncontrada()
	}
	if r.EmpresaID != nil {
		t.FijarEmpresa(*r.EmpresaID)
	}
	f, err := m.tecnicoDeRuta(ctx, t, q, r.TecnicoID, permiso)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Status == 404 {
		return r, f, rutaNoEncontrada()
	}
	return r, f, err
}

// --- Casos de uso ------------------------------------------------------------------------

func (m *Modulo) nuevoProponerRuta() *auditoria.Caso[EntradaProponerRuta, RutaDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaProponerRuta, RutaDetalle]{
		Accion: "rutas.proponer", Permiso: nucleo.PermRutasProponer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProponerRuta) (RutaDetalle, error) {
			q := agendabd.New(t)
			ahora := in.ahora
			if ahora.IsZero() {
				ahora = m.ahora()
			}
			dia, err := m.fechaLocalEn("fecha", in.Fecha, ahora)
			if err != nil {
				return RutaDetalle{}, err
			}
			todas, empresa := filtroEmpresa(ctx)
			if todas || (empresa != nil && *empresa == 0) {
				return RutaDetalle{}, nucleo.EmpresaRequerida()
			}
			if empresa != nil {
				t.FijarEmpresa(*empresa)
			}
			ficha, err := m.tecnicoDeRuta(ctx, t, q, in.TecnicoID, nucleo.PermRutasProponer)
			if err != nil {
				return RutaDetalle{}, err
			}
			fecha := fechaDe(dia, m.cfg.ZonaHoraria)
			if err := q.CandadoRuta(ctx, agendabd.CandadoRutaParams{TecnicoID: in.TecnicoID, Fecha: fecha}); err != nil {
				return RutaDetalle{}, err
			}
			d, err := m.leerDatosRuta(ctx, q, empresa, in.TecnicoID, dia, ahora, in.OrdenIDs)
			if err != nil {
				return RutaDetalle{}, err
			}
			if len(d.citas) == 0 {
				return RutaDetalle{}, nucleo.ReglaNegocio("ruta_vacia", "El técnico no tiene citas ese día en esta empresa: programa sus órdenes primero")
			}
			a := alcanceDe(ctx, nucleo.PermRutasProponer)
			for _, c := range d.citas {
				if !paradaVisible(a, c.ZonaID, c.CuadrillaID, ficha.CuadrillaID) {
					return RutaDetalle{}, fueraDeAlcance("La ruta del técnico tiene órdenes fuera de tu alcance: la propone quien las ve todas")
				}
			}
			// El plan calculado antes de la transacción, si la entrada no cambió; si no, en línea
			// recta (la búsqueda está acotada en tiempo y revisa ctx).
			var (
				p      plan
				motor  = "haversine"
				motivo = ptr("sin_osrm")
				trazo  [][2]float64
			)
			switch {
			case in.pre != nil && in.pre.plan != nil && in.pre.huella == d.entrada.huellaPlan():
				p, motor, motivo, trazo = *in.pre.plan, in.pre.motor, in.pre.motivo, in.pre.trazo
			case in.pre != nil && in.pre.huella == d.entrada.huellaPlan():
				motor, motivo = in.pre.motor, in.pre.motivo
			case in.pre != nil:
				motivo = ptr("datos_cambiaron")
			case m.cfg.Enrutador != nil:
				motivo = ptr("osrm_no_disponible")
			}
			if p.Tramos == nil {
				puntos, _, _ := d.entrada.puntos()
				matriz := MatrizEnLineaRecta(puntos)
				if motor == "osrm" {
					matriz = in.pre.matriz
				}
				if p, err = planificar(ctx, d.entrada, matriz); err != nil {
					return RutaDetalle{}, err
				}
			}
			var geometria []byte
			if len(p.Secuencia) >= 2 {
				coords := make([][2]float64, 0, len(p.Secuencia))
				for _, s := range p.Secuencia {
					coords = append(coords, [2]float64{s.Lon, s.Lat})
				}
				if motor == "osrm" && len(trazo) >= 2 {
					coords = trazo
				}
				if geometria, err = json.Marshal(map[string]any{"type": "LineString", "coordinates": coords}); err != nil {
					return RutaDetalle{}, err
				}
			}
			modo := "optimizada"
			if len(in.OrdenIDs) > 0 {
				modo = "manual"
			}
			var salidaLat, salidaLon *float64
			if s := salidaLibre(d.entrada); s != nil {
				salidaLat, salidaLon = &s.Lat, &s.Lon
			}
			datos := agendabd.CrearRutaParams{EmpresaID: empresa, TecnicoID: in.TecnicoID, Fecha: fecha, Modo: modo, Motor: motor,
				MotivoMotor: motivo, OrigenSalida: d.entrada.OrigenSalida, SalidaLat: salidaLat, SalidaLon: salidaLon,
				SaleEn: d.entrada.SaleEn, TerminaEn: p.TerminaEn, FinTurno: d.entrada.FinTurno, DistanciaM: p.Metros,
				ViajeSeg: p.ViajeSeg, EsperaSeg: p.EsperaSeg, Geometria: geometria, PropuestaPor: actorUsuario(t), PropuestaEn: ahora}
			previa, err := q.RutaVigente(ctx, agendabd.RutaVigenteParams{TecnicoID: in.TecnicoID, Fecha: fecha, Estado: "borrador", EmpresaID: empresa})
			hayPrevia := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return RutaDetalle{}, err
			}
			if in.Version != nil {
				if !hayPrevia {
					return RutaDetalle{}, nucleo.ConflictoVersion("ruta", 0)
				}
				if err := nucleo.ExigirVersion(*in.Version, previa.Version, "ruta", previa.ID); err != nil {
					return RutaDetalle{}, err
				}
			}
			var r agendabd.AgendaRuta
			if hayPrevia {
				if err := q.QuitarParadasDeBorrador(ctx, previa.ID); err != nil {
					return RutaDetalle{}, err
				}
				r, err = q.RehacerBorrador(ctx, agendabd.RehacerBorradorParams{ID: previa.ID, Modo: datos.Modo, Motor: datos.Motor,
					MotivoMotor: datos.MotivoMotor, OrigenSalida: datos.OrigenSalida, SalidaLat: datos.SalidaLat, SalidaLon: datos.SalidaLon,
					SaleEn: datos.SaleEn, TerminaEn: datos.TerminaEn, FinTurno: datos.FinTurno, DistanciaM: datos.DistanciaM,
					ViajeSeg: datos.ViajeSeg, EsperaSeg: datos.EsperaSeg, Geometria: datos.Geometria, PropuestaPor: datos.PropuestaPor,
					PropuestaEn: datos.PropuestaEn})
			} else {
				r, err = q.CrearRuta(ctx, datos)
			}
			if err != nil {
				return RutaDetalle{}, err
			}
			ids, err := guardarParadas(ctx, q, r.ID, p)
			if err != nil {
				return RutaDetalle{}, err
			}
			if hayPrevia {
				t.RegistrarCambios("ruta", r.ID, resumenCambio(previa), resumenCambio(r))
			} else {
				t.RegistrarCambios("ruta", r.ID, nil, resumenCambio(r))
			}
			t.Anotar(map[string]any{"ordenes": ids, "motor": motor})
			if err := t.Emitir("ruta.propuesta", map[string]any{
				"ruta_id": r.ID, "tecnico_id": r.TecnicoID, "fecha": fechaTexto(r.Fecha), "ordenes": ids, "modo": modo,
				"motor": motor, "degradada": motor != "osrm", "motivo_motor": motivo,
			}); err != nil {
				return RutaDetalle{}, err
			}
			return m.detalleRuta(ctx, q, r, ficha)
		},
	})
}

// salidaLibre: el punto desde el que arranca la parte que se ordena.
func salidaLibre(e planEntrada) *Punto {
	if len(e.Fijas) == 0 {
		return e.Salida
	}
	for i := len(e.Fijas) - 1; i >= 0; i-- {
		if e.Fijas[i].Punto != nil {
			return e.Fijas[i].Punto
		}
	}
	return e.Salida
}

// resumenCambio: lo que se audita de una ruta (sin la geometría).
func resumenCambio(r agendabd.AgendaRuta) map[string]any {
	return map[string]any{"estado": r.Estado, "modo": r.Modo, "motor": r.Motor, "motivo_motor": r.MotivoMotor,
		"origen_salida": r.OrigenSalida, "sale_en": r.SaleEn, "termina_en": r.TerminaEn, "distancia_m": r.DistanciaM,
		"viaje_seg": r.ViajeSeg, "desactualizada": r.Desactualizada, "publicada_en": r.PublicadaEn,
		"reemplazada_por_id": r.ReemplazadaPorID}
}

func guardarParadas(ctx context.Context, q *agendabd.Queries, ruta int64, p plan) ([]int64, error) {
	type fila struct {
		Posicion        int16      `json:"posicion"`
		OrdenID         int64      `json:"orden_id"`
		Lat             *float64   `json:"lat"`
		Lon             *float64   `json:"lon"`
		DuracionMin     int32      `json:"duracion_min"`
		VentanaInicio   *time.Time `json:"ventana_inicio"`
		VentanaFin      *time.Time `json:"ventana_fin"`
		CitaInicio      time.Time  `json:"cita_inicio"`
		Fija            bool       `json:"fija"`
		LlegadaEstimada time.Time  `json:"llegada_estimada"`
		SalidaEstimada  time.Time  `json:"salida_estimada"`
		ViajeSeg        int32      `json:"viaje_seg"`
		DistanciaM      int32      `json:"distancia_m"`
		EsperaSeg       int32      `json:"espera_seg"`
		RetrasoSeg      int32      `json:"retraso_seg"`
	}
	filas := make([]fila, 0, len(p.Tramos))
	ids := make([]int64, 0, len(p.Tramos))
	for i, tr := range p.Tramos {
		pp := tr.Parada
		f := fila{Posicion: int16(i + 1), OrdenID: pp.OrdenID, DuracionMin: minutosDe(pp.Duracion), VentanaInicio: pp.VentanaIni, //nolint:gosec // ≤ 100 paradas
			VentanaFin: pp.VentanaFin, CitaInicio: pp.CitaInicio, Fija: pp.Fija, LlegadaEstimada: tr.Llegada,
			SalidaEstimada: tr.Salida, ViajeSeg: tr.ViajeSeg, DistanciaM: tr.Metros, EsperaSeg: tr.EsperaSeg, RetrasoSeg: tr.Retraso}
		if f.DuracionMin <= 0 {
			f.DuracionMin = 1
		}
		if pp.Punto != nil {
			f.Lat, f.Lon = &pp.Punto.Lat, &pp.Punto.Lon
		}
		filas = append(filas, f)
		ids = append(ids, pp.OrdenID)
	}
	crudo, err := json.Marshal(filas)
	if err != nil {
		return nil, err
	}
	return ids, q.CrearParadas(ctx, agendabd.CrearParadasParams{RutaID: ruta, Paradas: crudo})
}

func (m *Modulo) nuevoPublicarRuta() *auditoria.Caso[EntradaPublicarRuta, RutaDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPublicarRuta, RutaDetalle]{
		Accion: "rutas.publicar", Permiso: nucleo.PermRutasPublicar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPublicarRuta) (RutaDetalle, error) {
			q := agendabd.New(t)
			ahora := m.ahora()
			r, ficha, err := m.tomarRuta(ctx, t, q, in.ID, nucleo.PermRutasPublicar, false)
			if err != nil {
				return RutaDetalle{}, err
			}
			if err := q.CandadoRuta(ctx, agendabd.CandadoRutaParams{TecnicoID: r.TecnicoID, Fecha: r.Fecha}); err != nil {
				return RutaDetalle{}, err
			}
			if r, err = q.RutaPorIDParaActualizar(ctx, in.ID); err != nil {
				return RutaDetalle{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, r.Version, "ruta", r.ID); err != nil {
				return RutaDetalle{}, err
			}
			if r.Estado != "borrador" {
				return RutaDetalle{}, nucleo.ReglaNegocio("transicion_invalida", "Solo se publica un borrador: propón la ruta de nuevo")
			}
			if r.Desactualizada {
				return RutaDetalle{}, nucleo.ReglaNegocio("ruta_desactualizada",
					"La ruta cambió después de proponerla ("+deTexto(r.MotivoDesactualizada)+"): propónla de nuevo antes de publicar")
			}
			paradas, err := q.ParadasDeRutas(ctx, []int64{r.ID})
			if err != nil {
				return RutaDetalle{}, err
			}
			// Las paradas siguen siendo las citas del técnico ese día (defensa: la marca de
			// desactualizada ya lo cubre).
			citas, err := q.CitasParaRuta(ctx, agendabd.CitasParaRutaParams{TecnicoID: r.TecnicoID,
				Desde: diaLocal(r.Fecha, m.cfg.ZonaHoraria).UTC(), Hasta: diaLocal(r.Fecha, m.cfg.ZonaHoraria).AddDate(0, 0, 1).UTC(), EmpresaID: r.EmpresaID})
			if err != nil {
				return RutaDetalle{}, err
			}
			actuales, enRuta := []int64{}, []int64{}
			for _, c := range citas {
				actuales = append(actuales, c.OrdenID)
			}
			a := alcanceDe(ctx, nucleo.PermRutasPublicar)
			for _, p := range paradas {
				enRuta = append(enRuta, p.OrdenID)
				if !paradaVisible(a, p.ZonaID, p.CuadrillaID, ficha.CuadrillaID) {
					return RutaDetalle{}, fueraDeAlcance("La ruta tiene órdenes fuera de tu alcance: la publica quien las ve todas")
				}
			}
			slices.Sort(actuales)
			ordenadas := slices.Clone(enRuta)
			slices.Sort(ordenadas)
			if !slices.Equal(actuales, ordenadas) {
				return RutaDetalle{}, nucleo.ReglaNegocio("ruta_desactualizada", "Las citas del técnico ese día cambiaron: propón la ruta de nuevo antes de publicar")
			}
			anterior, err := q.RutaVigente(ctx, agendabd.RutaVigenteParams{TecnicoID: r.TecnicoID, Fecha: r.Fecha, Estado: "publicada", EmpresaID: r.EmpresaID})
			hayAnterior := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return RutaDetalle{}, err
			}
			var reemplaza *int64
			if hayAnterior {
				// Nunca se borra: queda reemplazada (historial) y apunta a la nueva.
				nueva, err := q.ReemplazarRuta(ctx, agendabd.ReemplazarRutaParams{ID: anterior.ID, ReemplazadaEn: &ahora, ReemplazadaPorID: &r.ID})
				if err != nil {
					return RutaDetalle{}, err
				}
				t.RegistrarCambios("ruta", anterior.ID, resumenCambio(anterior), resumenCambio(nueva))
				reemplaza = &anterior.ID
			}
			antes := r
			if r, err = q.PublicarRuta(ctx, agendabd.PublicarRutaParams{ID: r.ID, PublicadaPor: actorUsuario(t), PublicadaEn: &ahora}); err != nil {
				return RutaDetalle{}, err
			}
			t.RegistrarCambios("ruta", r.ID, resumenCambio(antes), resumenCambio(r))
			if err := t.Emitir("ruta.publicada", map[string]any{
				"ruta_id": r.ID, "tecnico_id": r.TecnicoID, "fecha": fechaTexto(r.Fecha), "ordenes": enRuta,
				"reemplaza_id": reemplaza, "motor": r.Motor, "degradada": r.Motor != "osrm", "version": r.Version,
			}); err != nil {
				return RutaDetalle{}, err
			}
			return m.detalleRuta(ctx, q, r, ficha)
		},
	})
}

func deTexto(s *string) string {
	if s == nil {
		return "sin detalle"
	}
	return *s
}

func (m *Modulo) nuevoVerRuta() *auditoria.Caso[EntradaVerRuta, RutaDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerRuta, RutaDetalle]{
		Accion: "rutas.ver", Permiso: nucleo.PermRutasVer, AuditarExito: soloLecturas[RutaDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerRuta) (RutaDetalle, error) {
			q := agendabd.New(t)
			r, ficha, err := m.tomarRuta(ctx, t, q, in.ID, nucleo.PermRutasVer, false)
			if err != nil {
				return RutaDetalle{}, err
			}
			return m.detalleRuta(ctx, q, r, ficha)
		},
	})
}

func (m *Modulo) nuevoListarRutas() *auditoria.Caso[EntradaListarRutas, nucleo.Pagina[Ruta]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarRutas, nucleo.Pagina[Ruta]]{
		Accion: "rutas.ver", Permiso: nucleo.PermRutasVer, AuditarExito: soloLecturas[nucleo.Pagina[Ruta]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarRutas) (nucleo.Pagina[Ruta], error) {
			q := agendabd.New(t)
			salida := nucleo.Pagina[Ruta]{Data: []Ruta{}}
			dia, err := m.fechaLocal("fecha", in.Fecha)
			if err != nil {
				return salida, err
			}
			todas, empresa := filtroEmpresa(ctx)
			var estado *string
			if in.Estado != "" {
				estado = &in.Estado
			}
			filas, err := q.ListarRutas(ctx, agendabd.ListarRutasParams{Fecha: fechaDe(dia, m.cfg.ZonaHoraria), TodasEmpresas: todas,
				EmpresaID: empresa, TecnicoID: filtro(in.TecnicoID), CuadrillaID: filtro(in.CuadrillaID),
				IncluirReemplazadas: in.IncluirReemplazadas || in.Estado == "reemplazada", Estado: estado})
			if err != nil {
				return salida, err
			}
			a := alcanceDe(ctx, nucleo.PermRutasVer)
			cubre := map[int64]bool{}
			for _, f := range filas {
				ok, visto := cubre[f.TecnicoID]
				if !visto {
					if ok, err = a.cubreCuadrilla(ctx, q, f.TecnicoCuadrillaID); err != nil {
						return salida, err
					}
					if ok && !todas {
						if ok, err = identidad.UsuarioEnEmpresa(ctx, t, f.TecnicoID, deref(empresa)); err != nil {
							return salida, err
						}
					}
					cubre[f.TecnicoID] = ok
				}
				if !ok {
					continue
				}
				r := agendabd.AgendaRuta{ID: f.ID, EmpresaID: f.EmpresaID, TecnicoID: f.TecnicoID, Fecha: f.Fecha, Estado: f.Estado,
					Modo: f.Modo, Motor: f.Motor, MotivoMotor: f.MotivoMotor, OrigenSalida: f.OrigenSalida, SalidaLat: f.SalidaLat,
					SalidaLon: f.SalidaLon, SaleEn: f.SaleEn, TerminaEn: f.TerminaEn, FinTurno: f.FinTurno, DistanciaM: f.DistanciaM,
					ViajeSeg: f.ViajeSeg, EsperaSeg: f.EsperaSeg, Desactualizada: f.Desactualizada, DesactualizadaEn: f.DesactualizadaEn,
					MotivoDesactualizada: f.MotivoDesactualizada, PropuestaPor: f.PropuestaPor, PropuestaEn: f.PropuestaEn,
					PublicadaPor: f.PublicadaPor, PublicadaEn: f.PublicadaEn, ReemplazadaEn: f.ReemplazadaEn,
					ReemplazadaPorID: f.ReemplazadaPorID, Version: f.Version}
				ruta := aRuta(r, f.TecnicoNombre, f.Paradas, f.FueraDeVentana)
				if err := m.ocultarPosicion(ctx, q, &ruta, f.TecnicoCuadrillaID); err != nil {
					return salida, err
				}
				salida.Data = append(salida.Data, ruta)
			}
			return salida, nil
		},
	})
}

// detalleRuta arma la ruta con sus paradas (lo estimado y lo real) y el recorrido real.
func (m *Modulo) detalleRuta(ctx context.Context, q *agendabd.Queries, r agendabd.AgendaRuta, ficha agendabd.AgendaTecnico) (RutaDetalle, error) {
	filas, err := q.ParadasDeRutas(ctx, []int64{r.ID})
	if err != nil {
		return RutaDetalle{}, err
	}
	dia := diaLocal(r.Fecha, m.cfg.ZonaHoraria)
	citas, err := q.CitasParaRuta(ctx, agendabd.CitasParaRutaParams{TecnicoID: r.TecnicoID, Desde: dia.UTC(),
		Hasta: dia.AddDate(0, 0, 1).UTC(), EmpresaID: r.EmpresaID})
	if err != nil {
		return RutaDetalle{}, err
	}
	vigentes := map[int64]bool{}
	for _, c := range citas {
		vigentes[c.OrdenID] = true
	}
	nombre := ""
	if us, err := q.UsuariosPorIDs(ctx, []int64{r.TecnicoID}); err == nil && len(us) == 1 {
		nombre = us[0].Nombre
	} else if err != nil {
		return RutaDetalle{}, err
	}
	a := alcanceDe(ctx, nucleo.PermRutasVer)
	d := RutaDetalle{Geometria: geometriaDe(r.Geometria), Paradas: make([]ParadaRuta, 0, len(filas)),
		Real: RecorridoReal{Puntos: []PuntoReal{}, OrdenVisitado: []int64{}}}
	var fuera int32
	visitadas := []ParadaRuta{}
	hayOcultas, anteriorOculta := false, false
	for _, f := range filas {
		p := ParadaRuta{Posicion: f.Posicion, OrdenID: f.OrdenID, Numero: f.Numero, Tipo: f.TipoCodigo, Estado: f.Estado,
			Vigente: vigentes[f.OrdenID], Direccion: f.Direccion, Lat: f.Lat, Lon: f.Lon, SinCoordenadas: f.Lat == nil,
			DuracionMin: f.DuracionMin, VentanaInicio: f.VentanaInicio, VentanaFin: f.VentanaFin, CitaInicio: f.CitaInicio,
			Fija: f.Fija, LlegadaEstimada: &f.LlegadaEstimada, SalidaEstimada: &f.SalidaEstimada, ViajeSeg: f.ViajeSeg,
			DistanciaM: f.DistanciaM, EsperaSeg: f.EsperaSeg, RetrasoSeg: f.RetrasoSeg, FueraDeVentana: f.RetrasoSeg > 0}
		// Lo real solo mientras la orden siga en este técnico.
		if f.OrdenTecnicoID != nil && *f.OrdenTecnicoID == r.TecnicoID {
			p.EnCaminoEn, p.LlegadaReal, p.FinalizadaEn = f.EnCaminoEn, f.EnSitioEn, f.FinalizadaEn
			if f.EnSitioEn != nil {
				desvio := segundosFirmados(f.EnSitioEn.Sub(f.LlegadaEstimada))
				p.DesvioSeg = &desvio
			}
		}
		if !paradaVisible(a, f.ZonaID, f.CuadrillaID, ficha.CuadrillaID) {
			// Solo la posición en la secuencia: ni la orden, ni el estado, ni las horas, ni el
			// lugar, ni el viaje (que permitirían ubicarla); el viaje de la siguiente, tampoco.
			p = ParadaRuta{Posicion: f.Posicion, Oculta: true}
			hayOcultas = true
		} else if anteriorOculta {
			p.ViajeSeg, p.DistanciaM = 0, 0
		}
		anteriorOculta = p.Oculta
		if p.FueraDeVentana {
			fuera++
		}
		if p.LlegadaReal != nil {
			visitadas = append(visitadas, p)
		}
		d.Paradas = append(d.Paradas, p)
	}
	d.Ruta = aRuta(r, nombre, int32(len(filas)), fuera) //nolint:gosec // ≤ 100 paradas
	if hayOcultas {
		d.Geometria = nil // el trazo pasa por las paradas ocultas
	}
	if err := m.ocultarPosicion(ctx, q, &d.Ruta, ficha.CuadrillaID); err != nil {
		return RutaDetalle{}, err
	}
	if d.SalidaLat == nil && r.OrigenSalida == "posicion" {
		d.Geometria = nil // empieza en la posición del técnico
	}
	slices.SortStableFunc(visitadas, func(x, y ParadaRuta) int { return x.LlegadaReal.Compare(*y.LlegadaReal) })
	for _, v := range visitadas {
		d.Real.OrdenVisitado = append(d.Real.OrdenVisitado, v.OrdenID)
	}
	return d, m.recorridoReal(ctx, q, r, dia, &d.Real)
}

func segundosFirmados(d time.Duration) int32 {
	s := int64(d / time.Second)
	return int32(max(min(s, math.MaxInt32), math.MinInt32)) //nolint:gosec // acotado arriba
}

// puedeVerPosiciones: el actor tiene campo.ver_posiciones con alcance sobre la cuadrilla del
// técnico (la misma regla que GET /campo/posiciones/ultimas).
func puedeVerPosiciones(ctx context.Context, q *agendabd.Queries, cuadrilla *int64) (bool, error) {
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermCampoVerPosiciones)) {
		return false, nil
	}
	return alcanceDe(ctx, nucleo.PermCampoVerPosiciones).cubreCuadrilla(ctx, q, cuadrilla)
}

// ocultarPosicion: si la ruta sale de la última posición del técnico, solo la ve quien puede ver
// posiciones; a los demás, la salida sin coordenadas.
func (m *Modulo) ocultarPosicion(ctx context.Context, q *agendabd.Queries, r *Ruta, cuadrilla *int64) error {
	if r.OrigenSalida != "posicion" || r.SalidaLat == nil {
		return nil
	}
	ok, err := puedeVerPosiciones(ctx, q, cuadrilla)
	if err != nil || ok {
		return err
	}
	r.SalidaLat, r.SalidaLon = nil, nil
	return nil
}

// maxPuntosReales: el recorrido se muestrea a este número de puntos.
const maxPuntosReales = 1000

// recorridoReal lee campo.posiciones del técnico ese día en la empresa de la ruta. Exige ver
// posiciones (campo.ver_posiciones con alcance sobre el técnico): sin él, disponible = false.
func (m *Modulo) recorridoReal(ctx context.Context, q *agendabd.Queries, r agendabd.AgendaRuta, dia time.Time, real *RecorridoReal) error {
	ficha, err := q.TecnicoPorUsuario(ctx, r.TecnicoID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	ok, err := puedeVerPosiciones(ctx, q, ficha.CuadrillaID)
	if err != nil {
		return err
	}
	if !ok {
		real.Motivo = ptr("sin_permiso")
		return nil
	}
	ps, err := q.PosicionesDelDia(ctx, agendabd.PosicionesDelDiaParams{TecnicoID: r.TecnicoID, Desde: dia.UTC(),
		Hasta: dia.AddDate(0, 0, 1).UTC(), EmpresaID: r.EmpresaID})
	if err != nil {
		return err
	}
	if len(ps) == 0 {
		real.Motivo = ptr("sin_posiciones")
		return nil
	}
	real.Disponible = true
	real.TotalPuntos = int32(len(ps)) //nolint:gosec // ≤ 20000
	real.PrimeraEn, real.UltimaEn = &ps[0].RegistradaEn, &ps[len(ps)-1].RegistradaEn
	metros := 0.0
	for i := 1; i < len(ps); i++ {
		metros += Haversine(Punto{ps[i-1].Lat, ps[i-1].Lon}, Punto{ps[i].Lat, ps[i].Lon})
	}
	real.DistanciaM = segundos(metros)
	paso := 1
	if len(ps) > maxPuntosReales {
		paso = (len(ps) + maxPuntosReales - 1) / maxPuntosReales
	}
	for i := 0; i < len(ps); i += paso {
		real.Puntos = append(real.Puntos, PuntoReal{Lat: ps[i].Lat, Lon: ps[i].Lon, RegistradaEn: ps[i].RegistradaEn})
	}
	if ultimo := ps[len(ps)-1]; !real.Puntos[len(real.Puntos)-1].RegistradaEn.Equal(ultimo.RegistradaEn) {
		real.Puntos = append(real.Puntos, PuntoReal{Lat: ultimo.Lat, Lon: ultimo.Lon, RegistradaEn: ultimo.RegistradaEn})
	}
	return nil
}

// --- Reasignación en caliente ------------------------------------------------------------

// marcarRutas marca para recalcular el borrador y la publicada del técnico en el día local de
// inicio (en la empresa) y emite ruta.desactualizada por cada una que no lo estaba. Nunca borra
// una ruta: la publicada sigue vigente para el técnico hasta que despacho publique otra.
func (m *Modulo) marcarRutas(ctx context.Context, t *auditoria.Tx, empresa *int64, tecnico int64, inicio time.Time, ordenID int64, motivo string) error {
	q := agendabd.New(t)
	fecha := fechaDe(inicio, m.cfg.ZonaHoraria)
	marcadas, err := q.MarcarRutasDesactualizadas(ctx, agendabd.MarcarRutasDesactualizadasParams{Ahora: ptr(m.ahora()), Motivo: &motivo,
		TecnicoID: tecnico, Fecha: fecha, EmpresaID: empresa})
	if err != nil {
		return err
	}
	for _, r := range marcadas {
		if err := t.Emitir("ruta.desactualizada", map[string]any{
			"ruta_id": r.ID, "tecnico_id": r.TecnicoID, "fecha": fechaTexto(r.Fecha), "estado": r.Estado,
			"orden_id": ordenID, "motivo": motivo,
		}); err != nil {
			return err
		}
	}
	return nil
}

// RutaPublicadaDelTecnico: la ruta publicada del técnico ese día (dia: medianoche local) en la
// empresa (null = sin conector), para el paquete de la app de campo. nil si no hay.
func (m *Modulo) RutaPublicadaDelTecnico(ctx context.Context, db agendabd.DBTX, empresa *int64, tecnico int64, dia time.Time) (*RutaDelTecnico, error) {
	q := agendabd.New(db)
	r, err := q.RutaPublicadaDe(ctx, agendabd.RutaPublicadaDeParams{TecnicoID: tecnico, Fecha: fechaDe(dia, m.cfg.ZonaHoraria), EmpresaID: empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	filas, err := q.ParadasDeRutas(ctx, []int64{r.ID})
	if err != nil {
		return nil, err
	}
	salida := &RutaDelTecnico{ID: r.ID, Version: r.Version, Fecha: fechaTexto(r.Fecha), Desactualizada: r.Desactualizada,
		Motor: r.Motor, Paradas: make([]ParadaDelTecnico, 0, len(filas))}
	if r.PublicadaEn != nil {
		salida.PublicadaEn = *r.PublicadaEn
	}
	for _, f := range filas {
		salida.Paradas = append(salida.Paradas, ParadaDelTecnico{OrdenID: f.OrdenID, Posicion: f.Posicion,
			LlegadaEstimada: f.LlegadaEstimada, SalidaEstimada: f.SalidaEstimada, FueraDeVentana: f.RetrasoSeg > 0})
	}
	return salida, nil
}

// TurnoDelTecnico: el turno de un día (para la app de campo).
type TurnoDelTecnico struct {
	Inicio time.Time
	Fin    time.Time
	Tramos []Franja
}

// TurnoDelDia: los turnos del técnico (usuario) ese día local (dia: medianoche local) menos sus
// ausencias; nil si no trabaja ese día (sin ficha activa, sin turno o ausente todo el turno).
func (m *Modulo) TurnoDelDia(ctx context.Context, db agendabd.DBTX, usuario int64, dia time.Time) (*TurnoDelTecnico, error) {
	q := agendabd.New(db)
	f, err := q.TecnicoPorUsuario(ctx, usuario)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !f.Activo) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	turnos, err := q.TurnosDeTecnicos(ctx, []int64{f.ID})
	if err != nil {
		return nil, err
	}
	tramos := turnosDelDia(turnos, f.ID, dia)
	aus, err := q.AusenciasEnRango(ctx, agendabd.AusenciasEnRangoParams{Tecnicos: []int64{f.ID}, Desde: dia.UTC(), Hasta: dia.AddDate(0, 0, 1).UTC()})
	if err != nil {
		return nil, err
	}
	quitar := make([]intervalo, 0, len(aus))
	for _, a := range aus {
		quitar = append(quitar, intervalo{a.Inicio, a.Fin})
	}
	libres := restar(tramos, quitar)
	if len(libres) == 0 {
		return nil, nil
	}
	slices.SortFunc(libres, func(a, b intervalo) int { return a.ini.Compare(b.ini) })
	t := &TurnoDelTecnico{Inicio: libres[0].ini, Fin: libres[0].fin, Tramos: franjas(libres)}
	for _, l := range libres {
		t.Fin = maxT(t.Fin, l.fin)
	}
	return t, nil
}
