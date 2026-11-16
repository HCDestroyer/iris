package agenda

import (
	"context"
	"slices"
	"time"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Huecos ofrecibles: las ventanas en las que una visita cabe en la agenda de la zona. Lo usa el
// asistente de IA de mensajería para ofrecer y agendar una visita (contratos/conversaciones.md
// §17.10) sin ver la agenda: la salida son solo ventanas, nunca técnicos, cuadrillas ni cuántos hay
// libres. Las reglas son las de ReservarCita (turnos, ausencias, citas vivas de cualquier empresa y
// capacidad diaria) más las tareas pendientes del técnico.

// BloqueHora es un bloque de atención en hora local (HH:MM), p. ej. 08:00–12:00.
type BloqueHora struct {
	Inicio string `json:"inicio"`
	Fin    string `json:"fin"`
}

// EntradaHuecos pide ventanas ofrecibles.
type EntradaHuecos struct {
	// EmpresaID: la empresa de la orden (0 = sin conector).
	EmpresaID int64
	// ZonaID: la zona del servicio o del cliente. 0 = sin zona (nadie la cubre).
	ZonaID int64
	// Tipo: código del tipo de orden (visita, mantenimiento…): fija la duración.
	Tipo string
	// Desde y Hasta: primer y último día (hora local), los dos incluidos.
	Desde, Hasta time.Time
	// NoAntesDe: ninguna ventana empieza antes (anticipación mínima).
	NoAntesDe time.Time
	// Bloques de atención del día. Una ventana es un bloque de un día.
	Bloques []BloqueHora
	// Max: cuántas ventanas como mucho (las más próximas).
	Max int
	// ExcluirOrden: una orden cuya cita no cuenta como ocupación (la que se va a reprogramar).
	ExcluirOrden int64
}

// Hueco es una ventana ofrecible.
type Hueco struct {
	Inicio time.Time `json:"inicio"`
	Fin    time.Time `json:"fin"`
}

// Huecos es la respuesta.
type Huecos struct {
	Data []Hueco
	// SinCobertura: ningún técnico activo atiende esa zona en la empresa (no es «no hay espacio»).
	SinCobertura bool
}

// TecnicoElegido: quién atiende una ventana y a qué hora empieza su cita.
type TecnicoElegido struct {
	UsuarioID   int64
	Inicio, Fin time.Time
}

// ErrVentanaNoDisponible: ya no cabe nadie en esa ventana.
func ErrVentanaNoDisponible() *nucleo.Problema {
	return nucleo.ReglaNegocio("ventana_no_disponible", "Esa ventana ya no tiene espacio en la agenda")
}

// libreDeTecnico: los tramos libres de un técnico un día y lo cargado que está.
type libreDeTecnico struct {
	usuario int64
	libres  []intervalo
	usados  int32
}

// HuecosOfrecibles devuelve las ventanas (bloque de un día) en las que algún técnico que atiende
// la zona tiene un tramo libre del tamaño del tipo de orden. Solo lectura.
func (m *Modulo) HuecosOfrecibles(ctx context.Context, db agendabd.DBTX, in EntradaHuecos) (Huecos, error) {
	q := agendabd.New(db)
	loc := m.cfg.ZonaHoraria
	out := Huecos{Data: []Hueco{}}
	if in.Max <= 0 {
		in.Max = 3
	}
	hubo := false
	for dia := medianoche(in.Desde, loc); !dia.After(medianoche(in.Hasta, loc)) && len(out.Data) < in.Max; dia = dia.AddDate(0, 0, 1) {
		libres, dur, cubre, err := m.libresDelDia(ctx, q, in.EmpresaID, in.ZonaID, in.Tipo, dia, in.ExcluirOrden)
		if err != nil {
			return out, err
		}
		hubo = hubo || cubre
		for _, b := range in.Bloques {
			v, ok := ventanaDelBloque(b, dia, loc)
			if !ok || v.ini.Before(in.NoAntesDe) {
				continue
			}
			if _, cabe := primerTramo(libres, v, dur); cabe && len(out.Data) < in.Max {
				out.Data = append(out.Data, Hueco{Inicio: v.ini, Fin: v.fin})
			}
		}
	}
	out.SinCobertura = !hubo
	return out, nil
}

// ElegirTecnicoEnVentana elige, entre los técnicos que atienden la zona y todavía caben en la
// ventana, al menos cargado ese día, y la hora de inicio de su cita (el primer tramo libre dentro de
// la ventana). No reserva: la reserva (con sus candados y sus reglas) la hace ReservarCita al
// programar; si otro la ganó, esa falla y quien llama vuelve a ofrecer.
func (m *Modulo) ElegirTecnicoEnVentana(ctx context.Context, db agendabd.DBTX, in EntradaHuecos, ventana Hueco) (TecnicoElegido, error) {
	q := agendabd.New(db)
	loc := m.cfg.ZonaHoraria
	libres, dur, _, err := m.libresDelDia(ctx, q, in.EmpresaID, in.ZonaID, in.Tipo, medianoche(ventana.Inicio, loc), in.ExcluirOrden)
	if err != nil {
		return TecnicoElegido{}, err
	}
	slices.SortStableFunc(libres, func(a, b libreDeTecnico) int { return int(a.usados - b.usados) })
	v := intervalo{ventana.Inicio, ventana.Fin}
	for _, l := range libres {
		if ini, ok := primerTramo([]libreDeTecnico{l}, v, dur); ok {
			return TecnicoElegido{UsuarioID: l.usuario, Inicio: ini, Fin: ini.Add(dur)}, nil
		}
	}
	return TecnicoElegido{}, ErrVentanaNoDisponible()
}

// ventanaDelBloque: el bloque de ese día como intervalo (false si el bloque no es válido).
func ventanaDelBloque(b BloqueHora, dia time.Time, loc *time.Location) (intervalo, bool) {
	ini, err1 := time.ParseInLocation("15:04", b.Inicio, loc)
	fin, err2 := time.ParseInLocation("15:04", b.Fin, loc)
	if err1 != nil || err2 != nil || !fin.After(ini) {
		return intervalo{}, false
	}
	d := dia.In(loc)
	en := func(h time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), h.Hour(), h.Minute(), 0, 0, loc)
	}
	return intervalo{en(ini), en(fin)}, true
}

// primerTramo: el inicio del primer tramo libre de alguien que cabe (dur) dentro de la ventana.
func primerTramo(libres []libreDeTecnico, v intervalo, dur time.Duration) (time.Time, bool) {
	var mejor time.Time
	ok := false
	for _, l := range libres {
		for _, t := range l.libres {
			ini, fin := maxT(t.ini, v.ini), minT(t.fin, v.fin)
			if fin.Sub(ini) >= dur && (!ok || ini.Before(mejor)) {
				mejor, ok = ini, true
			}
		}
	}
	return mejor, ok
}

// libresDelDia: por cada técnico activo que atiende la zona en la empresa y tiene capacidad ese día,
// sus tramos libres (turno menos ausencias, citas vivas y tareas pendientes). cubre: hay al menos un
// técnico que atiende la zona (aunque no tenga espacio).
func (m *Modulo) libresDelDia(ctx context.Context, q *agendabd.Queries, empresa, zona int64, tipo string, dia time.Time, excluir int64) ([]libreDeTecnico, time.Duration, bool, error) {
	tipos, err := codigosDeTipos(ctx, q)
	if err != nil {
		return nil, 0, false, err
	}
	tipoID, ok := tipos.id[tipo]
	if !ok {
		return nil, 0, false, campoInvalido("tipo", "Tipo de orden desconocido", "enum")
	}
	durMin := tipos.duracion[tipoID]
	dur := time.Duration(durMin) * time.Minute
	// Sin zona nadie la cubre: una cuadrilla «sin zonas» atiende todas, pero no una orden sin zona.
	if zona <= 0 {
		return nil, dur, false, nil
	}
	p := agendabd.ListarTecnicosParams{EmpresaID: filtro(empresa), AlcanceGlobal: true, Activo: ptr(true), ZonaID: filtro(zona), Limite: 200}
	filas, err := q.ListarTecnicos(ctx, p)
	if err != nil {
		return nil, dur, false, err
	}
	candidatos := []agendabd.ListarTecnicosRow{}
	for _, f := range filas {
		puede, err := puedeVerZona(ctx, q, f.UsuarioID, filtro(zona))
		if err != nil {
			return nil, dur, false, err
		}
		if puede {
			candidatos = append(candidatos, f)
		}
	}
	if len(candidatos) == 0 {
		return nil, dur, false, nil
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
		return nil, dur, true, err
	}
	aus, err := q.AusenciasEnRango(ctx, agendabd.AusenciasEnRangoParams{Tecnicos: fichas, Desde: dia, Hasta: finDia})
	if err != nil {
		return nil, dur, true, err
	}
	ocup, err := q.OcupacionTecnicos(ctx, agendabd.OcupacionTecnicosParams{Tecnicos: usuarios, ExcluirOrden: excluir, Desde: dia, Hasta: finDia})
	if err != nil {
		return nil, dur, true, err
	}
	tareas, err := q.TareasDeTecnicosEnRango(ctx, agendabd.TareasDeTecnicosEnRangoParams{Tecnicos: usuarios, Desde: dia, Hasta: finDia})
	if err != nil {
		return nil, dur, true, err
	}
	caps, err := q.CapacidadesDeTecnicos(ctx, agendabd.CapacidadesDeTecnicosParams{Tecnicos: fichas, Cuadrillas: cuadrillas})
	if err != nil {
		return nil, dur, true, err
	}
	usoCuadrilla := map[int64]uso{}
	for _, c := range cuadrillas {
		us, err := q.UsuariosDeCuadrilla(ctx, &c)
		if err != nil {
			return nil, dur, true, err
		}
		fs, err := q.OcupacionTecnicos(ctx, agendabd.OcupacionTecnicosParams{Tecnicos: us, ExcluirOrden: excluir, Desde: dia, Hasta: finDia})
		if err != nil {
			return nil, dur, true, err
		}
		usoCuadrilla[c] = usoDe(fs)
	}
	out := []libreDeTecnico{}
	for _, f := range candidatos {
		tramos := turnosDelDia(turnos, f.ID, dia)
		quitar := []intervalo{}
		for _, x := range aus {
			if x.TecnicoID == f.ID {
				quitar = append(quitar, intervalo{maxT(x.Inicio, dia), minT(x.Fin, finDia)})
			}
		}
		propias := []agendabd.OcupacionTecnicosRow{}
		for _, o := range ocup {
			if o.TecnicoID != f.UsuarioID {
				continue
			}
			propias = append(propias, o)
			if o.Estado != "cerrada" {
				quitar = append(quitar, intervalo{o.Inicio, o.Fin})
			}
		}
		for _, x := range tareas {
			if x.TecnicoID == f.UsuarioID {
				quitar = append(quitar, intervalo{x.Inicio, x.Fin})
			}
		}
		slices.SortFunc(quitar, func(a, b intervalo) int { return a.ini.Compare(b.ini) })
		propio := usoDe(propias)
		completa := false
		for _, c := range caps {
			var u uso
			switch {
			case c.TecnicoID != nil && *c.TecnicoID == f.ID:
				u = propio
			case c.CuadrillaID != nil && f.CuadrillaID != nil && *c.CuadrillaID == *f.CuadrillaID:
				u = usoCuadrilla[*c.CuadrillaID]
			default:
				continue
			}
			if c.TipoID != nil && *c.TipoID != tipoID {
				continue
			}
			cupos, minutos := u.de(c.TipoID)
			if (c.Cupos != nil && cupos >= *c.Cupos) || (c.Minutos != nil && minutos+durMin > *c.Minutos) {
				completa = true
			}
		}
		if completa {
			continue
		}
		libres := tramos
		for _, x := range quitar {
			libres = restar(libres, []intervalo{x})
		}
		_, usados := propio.de(nil)
		out = append(out, libreDeTecnico{usuario: f.UsuarioID, libres: libres, usados: usados})
	}
	return out, dur, true, nil
}
