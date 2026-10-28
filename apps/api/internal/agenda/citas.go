package agenda

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Reserva es lo que campo pide al programar, reprogramar o cambiar el técnico de una orden.
type Reserva struct {
	OrdenID     int64
	Numero      string
	EmpresaID   *int64
	TipoID      int64
	CuadrillaID *int64 // de la orden (para el evento)
	TecnicoID   int64  // usuario
	Inicio, Fin time.Time
	// Ventana de llegada acordada con el cliente. Sin ventana nueva: al reprogramar se conserva
	// la anterior si el nuevo inicio cae dentro; si no, la cita queda sin ventana.
	VentanaInicio, VentanaFin *time.Time
	// Origen: programar, reprogramar o asignar (otro técnico para la misma cita).
	Origen string
	Motivo *string
}

// Problemas de la reserva (422 con el motivo).
func reglaCita(codigo, detalle string) *nucleo.Problema { return nucleo.ReglaNegocio(codigo, detalle) }

// ReservarCita valida la cita (ficha activa, ventana, turno, ausencia, solape y capacidad del
// técnico y de su cuadrilla), la guarda y emite orden.programada, en la transacción de la acción
// de campo. Serializa las reservas del técnico (y de su cuadrilla) con un candado.
func (m *Modulo) ReservarCita(ctx context.Context, t *auditoria.Tx, r Reserva) (Cita, error) {
	q := agendabd.New(t)
	if err := q.CandadoAgendaTecnico(ctx, r.TecnicoID); err != nil {
		return Cita{}, err
	}
	ficha, err := q.TecnicoPorUsuario(ctx, r.TecnicoID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !ficha.Activo) {
		return Cita{}, nucleo.NuevoProblema(422, "tecnico_sin_agenda", "Técnico sin agenda",
			"El técnico no tiene ficha activa en la agenda (turnos y capacidad): dásela en Agenda › Técnicos",
			nucleo.ErrorCampo{Field: "tecnicoId", Message: "Sin ficha activa de técnico", Rule: "agenda"})
	}
	if err != nil {
		return Cita{}, err
	}
	if ficha.CuadrillaID != nil {
		if err := q.CandadoAgendaCuadrilla(ctx, *ficha.CuadrillaID); err != nil {
			return Cita{}, err
		}
	}
	previa, err := q.CitaDeOrden(ctx, r.OrdenID)
	hayPrevia := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Cita{}, err
	}
	inicio, fin := r.Inicio.UTC().Truncate(time.Microsecond), r.Fin.UTC().Truncate(time.Microsecond)
	vi, vf, err := ventanaDe(r, previa, hayPrevia, inicio)
	if err != nil {
		return Cita{}, err
	}
	if err := m.verificarDisponible(ctx, q, ficha, r, inicio, fin); err != nil {
		return Cita{}, err
	}
	c, err := q.GuardarCita(ctx, agendabd.GuardarCitaParams{OrdenID: r.OrdenID, EmpresaID: r.EmpresaID, TecnicoID: r.TecnicoID,
		TipoID: r.TipoID, Inicio: inicio, Fin: fin, VentanaInicio: vi, VentanaFin: vf})
	if err != nil {
		return Cita{}, err
	}
	if hayPrevia {
		t.RegistrarCambios("cita", c.ID, aCita(previa), aCita(c))
	} else {
		t.RegistrarCambios("cita", c.ID, nil, aCita(c))
	}
	cita := aCita(c)
	return cita, t.Emitir("orden.programada", map[string]any{
		"orden_id": r.OrdenID, "numero": r.Numero, "tecnico_id": r.TecnicoID, "cuadrilla_id": r.CuadrillaID,
		"tecnico_cuadrilla_id": ficha.CuadrillaID, "inicio": inicio, "fin": fin, "ventana_inicio": vi, "ventana_fin": vf,
		"reprogramada": r.Origen == "reprogramar", "origen": r.Origen, "motivo": r.Motivo,
	})
}

// QuitarCita libera la agenda de una orden que deja de estar programada (sin técnico,
// cancelada o reabierta). No falla si no tenía cita.
func QuitarCita(ctx context.Context, db agendabd.DBTX, ordenID int64) error {
	_, err := agendabd.New(db).QuitarCita(ctx, ordenID)
	return err
}

// CitasDeOrdenes: la cita de cada orden (para el paquete de campo y el detalle).
func CitasDeOrdenes(ctx context.Context, db agendabd.DBTX, ordenes []int64) (map[int64]Cita, error) {
	fs, err := agendabd.New(db).CitasDeOrdenes(ctx, ordenes)
	if err != nil {
		return nil, err
	}
	salida := make(map[int64]Cita, len(fs))
	for _, f := range fs {
		salida[f.OrdenID] = aCita(f)
	}
	return salida, nil
}

// ventanaDe valida la ventana nueva o decide si se conserva la anterior.
func ventanaDe(r Reserva, previa agendabd.AgendaCita, hayPrevia bool, inicio time.Time) (*time.Time, *time.Time, error) {
	if (r.VentanaInicio == nil) != (r.VentanaFin == nil) {
		return nil, nil, campoInvalido("ventanaInicio", "ventanaInicio y ventanaFin van juntas", "ventana")
	}
	if r.VentanaInicio == nil {
		if hayPrevia && previa.VentanaInicio != nil && !inicio.Before(*previa.VentanaInicio) && !inicio.After(*previa.VentanaFin) {
			return previa.VentanaInicio, previa.VentanaFin, nil
		}
		return nil, nil, nil
	}
	vi, vf := r.VentanaInicio.UTC().Truncate(time.Microsecond), r.VentanaFin.UTC().Truncate(time.Microsecond)
	if !vf.After(vi) {
		return nil, nil, campoInvalido("ventanaFin", "El fin de la ventana debe ser posterior a su inicio", "ventana")
	}
	if vf.Sub(vi) > 24*time.Hour {
		return nil, nil, campoInvalido("ventanaFin", "La ventana dura hasta 24 horas", "ventana")
	}
	if inicio.Before(vi) || inicio.After(vf) {
		return nil, nil, reglaCita("fuera_de_ventana", "El inicio de la cita debe caer dentro de la ventana acordada con el cliente")
	}
	return &vi, &vf, nil
}

// intervalo [ini, fin).
type intervalo struct{ ini, fin time.Time }

func (a intervalo) cruza(b intervalo) bool { return a.ini.Before(b.fin) && b.ini.Before(a.fin) }

// turnosDelDia: los turnos del técnico ese día local, como intervalos UTC.
func turnosDelDia(turnos []agendabd.AgendaTurno, tecnico int64, dia time.Time) []intervalo {
	salida := []intervalo{}
	d := diaISO(dia)
	for _, tu := range turnos {
		if tu.TecnicoID == tecnico && tu.Dia == d {
			salida = append(salida, intervalo{dia.Add(duracionDe(tu.Inicio)).UTC(), dia.Add(duracionDe(tu.Fin)).UTC()})
		}
	}
	return salida
}

func hora(t time.Time, loc *time.Location) string { return t.In(loc).Format("15:04") }

// verificarDisponible: turno, ausencia, solape y capacidad.
func (m *Modulo) verificarDisponible(ctx context.Context, q *agendabd.Queries, ficha agendabd.AgendaTecnico, r Reserva, inicio, fin time.Time) error {
	loc := m.cfg.ZonaHoraria
	dia := medianoche(inicio, loc)
	if !medianoche(fin.Add(-time.Microsecond), loc).Equal(dia) {
		return reglaCita("fuera_de_turno", "La cita no puede cruzar la medianoche (hora de Guatemala)")
	}
	turnos, err := q.TurnosDeTecnicos(ctx, []int64{ficha.ID})
	if err != nil {
		return err
	}
	tramos := turnosDelDia(turnos, ficha.ID, dia)
	dentro := false
	for _, tr := range tramos {
		if !inicio.Before(tr.ini) && !fin.After(tr.fin) {
			dentro = true
		}
	}
	if !dentro {
		if len(tramos) == 0 {
			return reglaCita("fuera_de_turno", fmt.Sprintf("El técnico no tiene turno el %s", dia.Format(time.DateOnly)))
		}
		partes := make([]string, 0, len(tramos))
		for _, tr := range tramos {
			partes = append(partes, hora(tr.ini, loc)+"–"+hora(tr.fin, loc))
		}
		return reglaCita("fuera_de_turno", fmt.Sprintf("La cita (%s–%s) no cabe en un turno del técnico el %s: %s",
			hora(inicio, loc), hora(fin, loc), dia.Format(time.DateOnly), strings.Join(partes, ", ")))
	}
	aus, err := q.AusenciasEnRango(ctx, agendabd.AusenciasEnRangoParams{Tecnicos: []int64{ficha.ID}, Desde: inicio, Hasta: fin})
	if err != nil {
		return err
	}
	if len(aus) > 0 {
		a := aus[0]
		return reglaCita("tecnico_ausente", fmt.Sprintf("El técnico está ausente (%s) del %s al %s", a.Motivo,
			a.Inicio.In(loc).Format("2006-01-02 15:04"), a.Fin.In(loc).Format("2006-01-02 15:04")))
	}
	solapes, err := q.Solapes(ctx, agendabd.SolapesParams{TecnicoID: r.TecnicoID, OrdenID: r.OrdenID, Inicio: inicio, Fin: fin})
	if err != nil {
		return err
	}
	if len(solapes) > 0 {
		s := solapes[0]
		que := "otra cita del técnico"
		if mismoID(s.EmpresaID, r.EmpresaID) {
			que = "la orden " + s.Numero
		}
		return reglaCita("solape", fmt.Sprintf("Se cruza con %s (%s–%s)", que, hora(s.Inicio, loc), hora(s.Fin, loc)))
	}
	return m.verificarCapacidad(ctx, q, ficha, r, dia, fin.Sub(inicio))
}

// uso del día: citas y minutos (por tipo).
type uso struct {
	cupos   map[int64]int32 // tipo → citas
	minutos map[int64]int32
}

func (u uso) de(tipo *int64) (int32, int32) {
	var c, mi int32
	for t, n := range u.cupos {
		if tipo == nil || *tipo == t {
			c += n
			mi += u.minutos[t]
		}
	}
	return c, mi
}

func usoDe(filas []agendabd.OcupacionTecnicosRow) uso {
	u := uso{cupos: map[int64]int32{}, minutos: map[int64]int32{}}
	for _, f := range filas {
		u.cupos[f.TipoID]++
		u.minutos[f.TipoID] += minutosDe(f.Fin.Sub(f.Inicio))
	}
	return u
}

// excede: con la cita nueva (tipo, minutos) se pasa de alguna línea de capacidad.
func excede(caps []agendabd.AgendaCapacidad, u uso, tipo int64, minutos int32, tipos tiposOrden) (string, bool) {
	for _, c := range caps {
		if c.TipoID != nil && *c.TipoID != tipo {
			continue
		}
		cupos, usados := u.de(c.TipoID)
		que := "en total"
		if c.TipoID != nil {
			que = "de " + tipos.codigo[*c.TipoID]
		}
		if c.Cupos != nil && cupos+1 > *c.Cupos {
			return fmt.Sprintf("ya tiene %d de %d citas %s ese día", cupos, *c.Cupos, que), true
		}
		if c.Minutos != nil && usados+minutos > *c.Minutos {
			return fmt.Sprintf("ya tiene %d de %d minutos %s ese día", usados, *c.Minutos, que), true
		}
	}
	return "", false
}

func (m *Modulo) verificarCapacidad(ctx context.Context, q *agendabd.Queries, ficha agendabd.AgendaTecnico, r Reserva, dia time.Time, dur time.Duration) error {
	caps, err := q.CapacidadesDeTecnico(ctx, &ficha.ID)
	if err != nil {
		return err
	}
	var capsCuadrilla []agendabd.AgendaCapacidad
	if ficha.CuadrillaID != nil {
		if capsCuadrilla, err = q.CapacidadesDeCuadrilla(ctx, ficha.CuadrillaID); err != nil {
			return err
		}
	}
	if len(caps) == 0 && len(capsCuadrilla) == 0 {
		return nil
	}
	tipos, err := codigosDeTipos(ctx, q)
	if err != nil {
		return err
	}
	minutos := minutosDe(dur)
	finDia := dia.AddDate(0, 0, 1)
	if len(caps) > 0 {
		filas, err := q.OcupacionTecnicos(ctx, agendabd.OcupacionTecnicosParams{Tecnicos: []int64{r.TecnicoID}, ExcluirOrden: r.OrdenID, Desde: dia, Hasta: finDia})
		if err != nil {
			return err
		}
		if detalle, ok := excede(caps, usoDe(filas), r.TipoID, minutos, tipos); ok {
			return reglaCita("capacidad_excedida", "El técnico "+detalle)
		}
	}
	if len(capsCuadrilla) > 0 {
		usuarios, err := q.UsuariosDeCuadrilla(ctx, ficha.CuadrillaID)
		if err != nil {
			return err
		}
		filas, err := q.OcupacionTecnicos(ctx, agendabd.OcupacionTecnicosParams{Tecnicos: usuarios, ExcluirOrden: r.OrdenID, Desde: dia, Hasta: finDia})
		if err != nil {
			return err
		}
		if detalle, ok := excede(capsCuadrilla, usoDe(filas), r.TipoID, minutos, tipos); ok {
			return reglaCita("capacidad_excedida", "La cuadrilla "+detalle)
		}
	}
	return nil
}

// minutosDe: una duración en minutos (las citas duran menos de un día; acotado por si acaso).
func minutosDe(d time.Duration) int32 {
	m := int64(d / time.Minute)
	switch {
	case m > math.MaxInt32:
		return math.MaxInt32
	case m < 0:
		return 0
	}
	return int32(m)
}
