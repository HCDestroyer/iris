package agenda

import (
	"context"
	"slices"
	"time"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaAsignables: técnicos que pueden tomar una orden de ese tipo en esa zona y día.
type EntradaAsignables struct {
	ZonaID      int64  `query:"zonaId" minimum:"0" doc:"Zona de la orden: técnicos que la atienden (cuadrilla) y la pueden ver (alcance)"`
	Fecha       string `query:"fecha" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"Día en hora de Guatemala (YYYY-MM-DD); hoy por defecto"`
	Tipo        string `query:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento," doc:"Tipo de orden: duración y capacidad por tipo"`
	CuadrillaID int64  `query:"cuadrillaId" minimum:"0"`
	Habilidad   string `query:"habilidad" maxLength:"40"`
	OrdenID     int64  `query:"ordenId" minimum:"0" doc:"Orden que se va a reprogramar: su cita actual no cuenta como ocupada"`
}

// Franja de tiempo [inicio, fin) en UTC.
type Franja struct {
	Inicio time.Time `json:"inicio"`
	Fin    time.Time `json:"fin"`
}

// Ocupacion: una cita del técnico ese día.
type Ocupacion struct {
	OrdenID int64     `json:"ordenId" doc:"0 si la orden es de otra empresa"`
	Numero  string    `json:"numero" doc:"Vacío si la orden es de otra empresa"`
	Estado  string    `json:"estado" doc:"Estado de la orden; «ocupado» si es de otra empresa"`
	Inicio  time.Time `json:"inicio"`
	Fin     time.Time `json:"fin"`
}

// UsoCapacidad: una línea de capacidad y lo ya usado ese día.
type UsoCapacidad struct {
	De            string  `json:"de" enum:"tecnico,cuadrilla"`
	Tipo          *string `json:"tipo" nullable:"true" doc:"null = todos los tipos"`
	Cupos         *int32  `json:"cupos" nullable:"true"`
	CuposUsados   int32   `json:"cuposUsados"`
	Minutos       *int32  `json:"minutos" nullable:"true"`
	MinutosUsados int32   `json:"minutosUsados"`
}

// Asignable: un técnico con su disponibilidad del día.
type Asignable struct {
	TecnicoID     int64          `json:"tecnicoId" doc:"Ficha de agenda"`
	UsuarioID     int64          `json:"usuarioId" doc:"Usuario: es el tecnicoId de POST /ordenes/{id}/programar"`
	Nombre        string         `json:"nombre"`
	CuadrillaID   *int64         `json:"cuadrillaId" nullable:"true"`
	Habilidades   []string       `json:"habilidades"`
	Disponible    bool           `json:"disponible" doc:"Tiene turno, no está ausente todo el día, le queda capacidad y un hueco del largo del tipo"`
	Motivo        *string        `json:"motivo" nullable:"true" enum:"sin_turno,ausente,capacidad_completa,sin_espacio" doc:"Por qué no está disponible"`
	Turnos        []Franja       `json:"turnos"`
	Ausencias     []Franja       `json:"ausencias"`
	Ocupado       []Ocupacion    `json:"ocupado" doc:"Citas del día (también las cerradas, que cuentan para la capacidad)"`
	Libres        []Franja       `json:"libres" doc:"Huecos dentro del turno sin citas vivas ni ausencias"`
	MinutosLibres int32          `json:"minutosLibres"`
	Capacidad     []UsoCapacidad `json:"capacidad"`
}

// RespuestaAsignables lista los técnicos, los disponibles primero.
type RespuestaAsignables struct {
	Fecha       string      `json:"fecha"`
	Tipo        *string     `json:"tipo" nullable:"true"`
	DuracionMin int32       `json:"duracionMin" doc:"Duración del tipo (0 sin tipo)"`
	Data        []Asignable `json:"data"`
}

// restar quita de las franjas los intervalos ocupados.
func restar(libres []intervalo, quitar []intervalo) []intervalo {
	for _, q := range quitar {
		siguiente := []intervalo{}
		for _, l := range libres {
			if !l.cruza(q) {
				siguiente = append(siguiente, l)
				continue
			}
			if l.ini.Before(q.ini) {
				siguiente = append(siguiente, intervalo{l.ini, q.ini})
			}
			if q.fin.Before(l.fin) {
				siguiente = append(siguiente, intervalo{q.fin, l.fin})
			}
		}
		libres = siguiente
	}
	return libres
}

func franjas(is []intervalo) []Franja {
	salida := make([]Franja, 0, len(is))
	for _, i := range is {
		salida = append(salida, Franja{i.ini, i.fin})
	}
	return salida
}

func (m *Modulo) nuevoAsignables() *auditoria.Caso[EntradaAsignables, RespuestaAsignables] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAsignables, RespuestaAsignables]{
		Accion: "agenda.ver", Permiso: nucleo.PermAgendaVer, AuditarExito: soloLecturas[RespuestaAsignables],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsignables) (RespuestaAsignables, error) {
			q := agendabd.New(t)
			dia, err := m.fechaLocal("fecha", in.Fecha)
			if err != nil {
				return RespuestaAsignables{}, err
			}
			tipos, err := codigosDeTipos(ctx, q)
			if err != nil {
				return RespuestaAsignables{}, err
			}
			salida := RespuestaAsignables{Fecha: dia.Format(time.DateOnly), Data: []Asignable{}}
			var tipo *int64
			if in.Tipo != "" {
				id, ok := tipos.id[in.Tipo]
				if !ok {
					return salida, campoInvalido("tipo", "Tipo de orden desconocido", "enum")
				}
				tipo, salida.Tipo, salida.DuracionMin = &id, &in.Tipo, tipos.duracion[id]
			}
			p := m.paramsTecnicos(ctx, nucleo.PermAgendaVer)
			p.Limite, p.Activo, p.ZonaID, p.CuadrillaID = 200, ptr(true), filtro(in.ZonaID), filtro(in.CuadrillaID)
			if in.Habilidad != "" {
				p.Habilidad = &in.Habilidad
			}
			filas, err := q.ListarTecnicos(ctx, p)
			if err != nil {
				return salida, err
			}
			candidatos := []agendabd.ListarTecnicosRow{}
			for _, f := range filas {
				ok, err := puedeVerZona(ctx, q, f.UsuarioID, filtro(in.ZonaID))
				if err != nil {
					return salida, err
				}
				if ok {
					candidatos = append(candidatos, f)
				}
			}
			if len(candidatos) == 0 {
				return salida, nil
			}
			fichas, usuarios, cuadrillas := []int64{}, []int64{}, []int64{}
			for _, f := range candidatos {
				fichas, usuarios = append(fichas, f.ID), append(usuarios, f.UsuarioID)
				if f.CuadrillaID != nil && !slices.Contains(cuadrillas, *f.CuadrillaID) {
					cuadrillas = append(cuadrillas, *f.CuadrillaID)
				}
			}
			finDia := dia.AddDate(0, 0, 1)
			turnos, err := q.TurnosDeTecnicos(ctx, fichas)
			if err != nil {
				return salida, err
			}
			aus, err := q.AusenciasEnRango(ctx, agendabd.AusenciasEnRangoParams{Tecnicos: fichas, Desde: dia, Hasta: finDia})
			if err != nil {
				return salida, err
			}
			ocup, err := q.OcupacionTecnicos(ctx, agendabd.OcupacionTecnicosParams{Tecnicos: usuarios, ExcluirOrden: in.OrdenID, Desde: dia, Hasta: finDia})
			if err != nil {
				return salida, err
			}
			caps, err := q.CapacidadesDeTecnicos(ctx, agendabd.CapacidadesDeTecnicosParams{Tecnicos: fichas, Cuadrillas: cuadrillas})
			if err != nil {
				return salida, err
			}
			// Uso de cada cuadrilla (todas sus fichas, no solo las candidatas).
			usoCuadrilla := map[int64]uso{}
			for _, c := range cuadrillas {
				us, err := q.UsuariosDeCuadrilla(ctx, &c)
				if err != nil {
					return salida, err
				}
				fs, err := q.OcupacionTecnicos(ctx, agendabd.OcupacionTecnicosParams{Tecnicos: us, ExcluirOrden: in.OrdenID, Desde: dia, Hasta: finDia})
				if err != nil {
					return salida, err
				}
				usoCuadrilla[c] = usoDe(fs)
			}
			_, empresa := filtroEmpresa(ctx)
			todas, _ := filtroEmpresa(ctx)
			duracion := time.Duration(salida.DuracionMin) * time.Minute
			for _, f := range candidatos {
				a := Asignable{TecnicoID: f.ID, UsuarioID: f.UsuarioID, Nombre: f.Nombre, CuadrillaID: f.CuadrillaID,
					Habilidades: f.Habilidades, Ocupado: []Ocupacion{}, Capacidad: []UsoCapacidad{}}
				if a.Habilidades == nil {
					a.Habilidades = []string{}
				}
				tramos := turnosDelDia(turnos, f.ID, dia)
				a.Turnos = franjas(tramos)
				ausentes := []intervalo{}
				for _, x := range aus {
					if x.TecnicoID == f.ID {
						ausentes = append(ausentes, intervalo{maxT(x.Inicio, dia), minT(x.Fin, finDia)})
					}
				}
				a.Ausencias = franjas(ausentes)
				propias := []agendabd.OcupacionTecnicosRow{}
				ocupado := []intervalo{}
				for _, o := range ocup {
					if o.TecnicoID != f.UsuarioID {
						continue
					}
					propias = append(propias, o)
					oc := Ocupacion{OrdenID: o.OrdenID, Numero: o.Numero, Estado: o.Estado, Inicio: o.Inicio, Fin: o.Fin}
					if !todas && !mismoID(o.EmpresaID, empresa) {
						// De otra empresa: solo que está ocupado y su horario.
						oc.OrdenID, oc.Numero, oc.Estado = 0, "", "ocupado"
					}
					a.Ocupado = append(a.Ocupado, oc)
					if o.Estado != "cerrada" {
						ocupado = append(ocupado, intervalo{o.Inicio, o.Fin})
					}
				}
				libres := restar(restar(tramos, ausentes), ocupado)
				a.Libres = franjas(libres)
				mayor := time.Duration(0)
				for _, l := range libres {
					d := l.fin.Sub(l.ini)
					a.MinutosLibres += minutosDe(d)
					mayor = max(mayor, d)
				}
				propio := usoDe(propias)
				completa := false
				for _, c := range caps {
					var u uso
					de := "tecnico"
					switch {
					case c.TecnicoID != nil && *c.TecnicoID == f.ID:
						u = propio
					case c.CuadrillaID != nil && f.CuadrillaID != nil && *c.CuadrillaID == *f.CuadrillaID:
						u, de = usoCuadrilla[*c.CuadrillaID], "cuadrilla"
					default:
						continue
					}
					cupos, minutos := u.de(c.TipoID)
					uc := aCapacidad(c, tipos)
					a.Capacidad = append(a.Capacidad, UsoCapacidad{De: de, Tipo: uc.Tipo, Cupos: c.Cupos, CuposUsados: cupos, Minutos: c.Minutos, MinutosUsados: minutos})
					if tipo == nil || c.TipoID == nil || *c.TipoID == *tipo {
						if (c.Cupos != nil && cupos >= *c.Cupos) || (c.Minutos != nil && minutos+salida.DuracionMin > *c.Minutos) {
							completa = true
						}
					}
				}
				var motivo string
				switch {
				case len(tramos) == 0:
					motivo = "sin_turno"
				case len(restar(tramos, ausentes)) == 0:
					motivo = "ausente"
				case completa:
					motivo = "capacidad_completa"
				case len(libres) == 0 || mayor < duracion:
					motivo = "sin_espacio"
				}
				a.Disponible = motivo == ""
				if motivo != "" {
					a.Motivo = &motivo
				}
				salida.Data = append(salida.Data, a)
			}
			slices.SortStableFunc(salida.Data, func(x, y Asignable) int {
				if x.Disponible != y.Disponible {
					if x.Disponible {
						return -1
					}
					return 1
				}
				return int(y.MinutosLibres - x.MinutosLibres)
			})
			return salida, nil
		},
	})
}

func maxT(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// puedeVerZona: el usuario ve órdenes de la zona (ordenes.ver_propias u ordenes.ver, global o
// con esa zona). Sin zona, basta tener alguno de los dos. Es la regla con que campo valida al
// técnico al programar.
func puedeVerZona(ctx context.Context, q *agendabd.Queries, usuario int64, zona *int64) (bool, error) {
	alc, err := q.AlcancesDePermisoDeUsuario(ctx, agendabd.AlcancesDePermisoDeUsuarioParams{UsuarioID: usuario,
		Permisos: []string{string(nucleo.PermOrdenesVerPropias), string(nucleo.PermOrdenesVer)}})
	if err != nil {
		return false, err
	}
	for _, a := range alc {
		if zona == nil || (a.ZonaID == nil && a.CuadrillaID == nil) || (a.ZonaID != nil && *a.ZonaID == *zona) {
			return true, nil
		}
	}
	return false, nil
}

// EntradaCalendario: GET /agenda.
type EntradaCalendario struct {
	Desde       string `query:"desde" required:"true" format:"date-time" doc:"UTC (RFC 3339)"`
	Hasta       string `query:"hasta" required:"true" format:"date-time" doc:"UTC; hasta 31 días después de desde"`
	TecnicoID   int64  `query:"tecnicoId" minimum:"0" doc:"Usuario técnico"`
	CuadrillaID int64  `query:"cuadrillaId" minimum:"0" doc:"Cuadrilla del técnico o de la orden"`
	ZonaID      int64  `query:"zonaId" minimum:"0" doc:"Zona de la orden"`
}

// CitaAgenda: una cita del calendario con su orden.
type CitaAgenda struct {
	OrdenID       int64      `json:"ordenId"`
	Numero        string     `json:"numero"`
	Tipo          string     `json:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento"`
	Estado        string     `json:"estado" enum:"pendiente,programada,en_camino,en_sitio,cerrada,fallida,cancelada"`
	OrdenVersion  int32      `json:"ordenVersion" doc:"Versión de la orden (para reprogramar)"`
	TecnicoID     int64      `json:"tecnicoId" doc:"Usuario técnico"`
	Inicio        time.Time  `json:"inicio"`
	Fin           time.Time  `json:"fin"`
	VentanaInicio *time.Time `json:"ventanaInicio" nullable:"true"`
	VentanaFin    *time.Time `json:"ventanaFin" nullable:"true"`
	ZonaID        *int64     `json:"zonaId" nullable:"true"`
	CuadrillaID   *int64     `json:"cuadrillaId" nullable:"true" doc:"Cuadrilla de la orden"`
	Direccion     string     `json:"direccion"`
	Lat           *float64   `json:"lat" nullable:"true"`
	Lon           *float64   `json:"lon" nullable:"true"`
	ClienteID     *int64     `json:"clienteId" nullable:"true"`
	ServicioID    *int64     `json:"servicioId" nullable:"true"`
}

// AusenciaAgenda: una ausencia en el rango.
type AusenciaAgenda struct {
	ID        int64     `json:"id"`
	TecnicoID int64     `json:"tecnicoId" doc:"Usuario técnico"`
	FichaID   int64     `json:"fichaId"`
	Inicio    time.Time `json:"inicio"`
	Fin       time.Time `json:"fin"`
	Motivo    string    `json:"motivo"`
}

// Calendario de despacho.
type Calendario struct {
	Desde     time.Time        `json:"desde"`
	Hasta     time.Time        `json:"hasta"`
	Citas     []CitaAgenda     `json:"citas"`
	Ausencias []AusenciaAgenda `json:"ausencias"`
}

func (m *Modulo) nuevoCalendario() *auditoria.Caso[EntradaCalendario, Calendario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCalendario, Calendario]{
		Accion: "agenda.ver", Permiso: nucleo.PermAgendaVer, AuditarExito: soloLecturas[Calendario],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCalendario) (Calendario, error) {
			desde, err := instante("desde", in.Desde)
			if err != nil {
				return Calendario{}, err
			}
			hasta, err := instante("hasta", in.Hasta)
			if err != nil {
				return Calendario{}, err
			}
			if !hasta.After(desde) || hasta.Sub(desde) > 31*24*time.Hour {
				return Calendario{}, campoInvalido("hasta", "hasta debe ser posterior a desde y como mucho 31 días después", "rango")
			}
			q := agendabd.New(t)
			todas, empresa := filtroEmpresa(ctx)
			a := alcanceDe(ctx, nucleo.PermAgendaVer)
			filas, err := q.AgendaCitas(ctx, agendabd.AgendaCitasParams{Desde: desde, Hasta: hasta, TodasEmpresas: todas, EmpresaID: empresa,
				TecnicoID: filtro(in.TecnicoID), CuadrillaID: filtro(in.CuadrillaID), ZonaID: filtro(in.ZonaID),
				AlcanceGlobal: a.global, Zonas: a.zonas, Cuadrillas: a.cuadrillas})
			if err != nil {
				return Calendario{}, err
			}
			salida := Calendario{Desde: desde, Hasta: hasta, Citas: []CitaAgenda{}, Ausencias: []AusenciaAgenda{}}
			for _, f := range filas {
				salida.Citas = append(salida.Citas, CitaAgenda{OrdenID: f.OrdenID, Numero: f.Numero, Tipo: f.TipoCodigo, Estado: f.Estado,
					OrdenVersion: f.OrdenVersion, TecnicoID: f.TecnicoID, Inicio: f.Inicio, Fin: f.Fin, VentanaInicio: f.VentanaInicio,
					VentanaFin: f.VentanaFin, ZonaID: f.ZonaID, CuadrillaID: f.OrdenCuadrillaID, Direccion: f.Direccion, Lat: f.Lat,
					Lon: f.Lon, ClienteID: f.ClienteID, ServicioID: f.ServicioID})
			}
			// Ausencias de los técnicos visibles (con los mismos filtros de técnico y cuadrilla).
			p := m.paramsTecnicos(ctx, nucleo.PermAgendaVer)
			p.CuadrillaID, p.UsuarioID, p.ZonaID = filtro(in.CuadrillaID), filtro(in.TecnicoID), filtro(in.ZonaID)
			tecs, err := q.ListarTecnicos(ctx, p)
			if err != nil {
				return Calendario{}, err
			}
			fichas, usuario := []int64{}, map[int64]int64{}
			for _, tc := range tecs {
				fichas = append(fichas, tc.ID)
				usuario[tc.ID] = tc.UsuarioID
			}
			aus, err := q.AusenciasEnRango(ctx, agendabd.AusenciasEnRangoParams{Tecnicos: fichas, Desde: desde, Hasta: hasta})
			if err != nil {
				return Calendario{}, err
			}
			for _, x := range aus {
				salida.Ausencias = append(salida.Ausencias, AusenciaAgenda{ID: x.ID, TecnicoID: usuario[x.TecnicoID], FichaID: x.TecnicoID,
					Inicio: x.Inicio, Fin: x.Fin, Motivo: x.Motivo})
			}
			return salida, nil
		},
	})
}
