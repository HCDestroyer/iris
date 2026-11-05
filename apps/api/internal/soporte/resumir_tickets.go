package soporte

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// DiasResumen: días de la serie del resumen (hoy y los 6 anteriores).
const DiasResumen = 7

// MaxUrgentes: tickets con el SLA en riesgo o vencido que trae el resumen.
const MaxUrgentes = 5

// zonaResumen: los días de la serie son de Guatemala (fechas en UTC en la base).
var zonaResumen = func() *time.Location {
	loc, err := time.LoadLocation("America/Guatemala")
	if err != nil {
		panic("soporte: zona horaria America/Guatemala: " + err.Error())
	}
	return loc
}()

// EntradaResumirTickets no lleva filtros: el resumen es de la bandeja que el actor puede ver.
type EntradaResumirTickets struct{}

// ResumenTickets: contadores de la bandeja, los tickets más urgentes y la serie de 7 días. Lo
// usan el Inicio del panel, las pestañas de vistas y la barra lateral. Mismo alcance y empresa
// que GET /tickets: nunca cuenta lo que el actor no puede ver.
type ResumenTickets struct {
	Activos     int64        `json:"activos" doc:"Tickets nuevos, abiertos o en espera"`
	Mios        int64        `json:"mios" doc:"Activos asignados al actor (0 si no es un usuario)"`
	SinAsignar  int64        `json:"sinAsignar" doc:"Activos sin usuario asignado"`
	EnEspera    int64        `json:"enEspera" doc:"En espera (su SLA de resolución está en pausa)"`
	SLAEnRiesgo int64        `json:"slaEnRiesgo" doc:"Nuevos o abiertos con el SLA de resolución en riesgo (aún sin vencer)"`
	SLAVencidos int64        `json:"slaVencidos" doc:"Nuevos o abiertos con el SLA de resolución vencido"`
	SLAVenceHoy int64        `json:"slaVenceHoy" doc:"Nuevos o abiertos cuyo SLA de resolución vence antes del fin de hoy (America/Guatemala), sin contar los ya vencidos"`
	Urgentes    []Ticket     `json:"urgentes" doc:"Hasta 5 tickets nuevos o abiertos con el SLA de resolución en riesgo o vencido; el que vence antes primero"`
	Dias        []DiaTickets `json:"dias" doc:"Hoy y los 6 días anteriores (America/Guatemala), del más antiguo a hoy"`
	CalculadoEn time.Time    `json:"calculadoEn"`
}

// DiaTickets: actividad de la bandeja en un día local.
type DiaTickets struct {
	Dia             string `json:"dia" format:"date" doc:"Día local (YYYY-MM-DD, America/Guatemala)"`
	Creados         int64  `json:"creados"`
	Resueltos       int64  `json:"resueltos" doc:"Resueltos ese día (sin los fusionados, como el reporte de SLA)"`
	ResueltosConSLA int64  `json:"resueltosConSla" doc:"De los resueltos, los que tenían SLA de resolución"`
	ResueltosEnSLA  int64  `json:"resueltosEnSla" doc:"De los resueltos con SLA, los resueltos dentro del plazo"`
	Vencidos        int64  `json:"vencidos" doc:"Tickets cuyo plazo de resolución cayó ese día, ya pasó y no se resolvieron a tiempo"`
}

func (m *Modulo) nuevoResumirTickets() *auditoria.Caso[EntradaResumirTickets, ResumenTickets] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResumirTickets, ResumenTickets]{
		Accion:       "tickets.ver",
		Permiso:      nucleo.PermTicketsVer,
		AuditarExito: soloLecturas[ResumenTickets],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ EntradaResumirTickets) (ResumenTickets, error) {
			ahora := m.ahora()
			local := ahora.In(zonaResumen)
			hoy := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zonaResumen)
			primero := hoy.AddDate(0, 0, -(DiasResumen - 1))
			finHoy := hoy.AddDate(0, 0, 1)

			global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermTicketsVer))
			todas, empresa := filtroEmpresa(ctx)
			var actor *int64
			if a := contexto.ActorDe(ctx); a.EsUsuario() {
				actor = &a.ID
			}
			q := soportebd.New(t)

			c, err := q.ResumenTickets(ctx, soportebd.ResumenTicketsParams{
				ActorID: actor, Ahora: ahora, FinHoy: finHoy.UTC(), TodasEmpresas: todas, EmpresaID: empresa,
				AlcanceGlobal: global, Zonas: noNulo(zonas), Cuadrillas: noNulo(cuadrillas),
			})
			if err != nil {
				return ResumenTickets{}, err
			}
			urgentes, err := q.TicketsUrgentes(ctx, soportebd.TicketsUrgentesParams{
				Ahora: ahora, TodasEmpresas: todas, EmpresaID: empresa, AlcanceGlobal: global,
				Zonas: noNulo(zonas), Cuadrillas: noNulo(cuadrillas), ActorID: actor, Limite: MaxUrgentes,
			})
			if err != nil {
				return ResumenTickets{}, err
			}
			serie, err := q.SerieTicketsPorDia(ctx, soportebd.SerieTicketsPorDiaParams{
				Ahora: ahora, PrimerDia: fechaLocal(primero), UltimoDia: fechaLocal(hoy), Desde: primero.UTC(),
				TodasEmpresas: todas, EmpresaID: empresa, AlcanceGlobal: global,
				Zonas: noNulo(zonas), Cuadrillas: noNulo(cuadrillas), ActorID: actor,
			})
			if err != nil {
				return ResumenTickets{}, err
			}

			r := ResumenTickets{
				Activos: c.Activos, Mios: c.Mios, SinAsignar: c.SinAsignar, EnEspera: c.EnEspera,
				SLAEnRiesgo: c.SlaEnRiesgo, SLAVencidos: c.SlaVencidos, SLAVenceHoy: c.SlaVenceHoy,
				Urgentes: make([]Ticket, 0, len(urgentes)), Dias: make([]DiaTickets, 0, len(serie)), CalculadoEn: ahora,
			}
			for _, f := range urgentes {
				r.Urgentes = append(r.Urgentes, aTicket(f, ahora))
			}
			for _, d := range serie {
				r.Dias = append(r.Dias, DiaTickets{
					Dia: d.Dia.Time.Format(time.DateOnly), Creados: d.Creados, Resueltos: d.Resueltos,
					ResueltosConSLA: d.ResueltosConSla, ResueltosEnSLA: d.ResueltosEnSla, Vencidos: d.Vencidos,
				})
			}
			return r, nil
		},
	})
}

// fechaLocal: el día (año, mes y día de la fecha local) como date de Postgres.
func fechaLocal(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
