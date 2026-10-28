package soporte

import (
	"context"
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // zonas horarias embebidas: la imagen distroless no trae /usr/share/zoneinfo

	"github.com/jackc/pgx/v5/pgtype"

	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Reloj del SLA. Un SLA sin calendario corre 24/7 (Calendario nil); con calendario, solo cuenta
// el tiempo dentro de los tramos laborales de cada día, fuera de los feriados. Todo se calcula
// con la hora local de la zona del calendario y se guarda en UTC.

// Tramo es un horario laboral de un día de la semana (ISO: 1 = lunes … 7 = domingo), con
// inicio y fin medidos desde la medianoche local.
type Tramo struct {
	Dia    int
	Inicio time.Duration
	Fin    time.Duration
}

// Feriado es un día no laboral. Desde nil = todo el día; si no, deja de ser laboral desde esa
// hora local (24 y 31 de diciembre desde las 12:00).
type Feriado struct {
	Anio  int
	Mes   time.Month
	Dia   int
	Desde *time.Duration
}

type fecha struct {
	anio int
	mes  time.Month
	dia  int
}

// Calendario es un horario laboral con feriados. nil = 24/7.
type Calendario struct {
	zona     *time.Location
	tramos   [8][]Tramo // por día ISO (índice 0 sin uso)
	feriados map[fecha]Feriado
}

// maxDias acota la búsqueda de tiempo laboral (diez años): un calendario válido siempre
// encuentra tramos mucho antes.
const maxDias = 3660

// NuevoCalendario arma un calendario. Exige al menos un tramo válido.
func NuevoCalendario(zona string, tramos []Tramo, feriados []Feriado) (*Calendario, error) {
	loc, err := time.LoadLocation(zona)
	if err != nil {
		return nil, fmt.Errorf("calendario: zona horaria %q: %w", zona, err)
	}
	c := &Calendario{zona: loc, feriados: map[fecha]Feriado{}}
	for _, t := range tramos {
		if t.Dia < 1 || t.Dia > 7 || t.Inicio < 0 || t.Fin > 24*time.Hour || t.Inicio >= t.Fin {
			return nil, fmt.Errorf("calendario: tramo inválido %+v", t)
		}
		c.tramos[t.Dia] = append(c.tramos[t.Dia], t)
	}
	hay := false
	for _, ts := range c.tramos {
		hay = hay || len(ts) > 0
	}
	if !hay {
		return nil, errors.New("calendario: sin tramos laborales")
	}
	for _, f := range feriados {
		c.feriados[fecha{f.Anio, f.Mes, f.Dia}] = f
	}
	return c, nil
}

// Zona devuelve la zona horaria del calendario (UTC si es 24/7).
func (c *Calendario) Zona() *time.Location {
	if c == nil {
		return time.UTC
	}
	return c.zona
}

// intervalos devuelve los intervalos laborales [inicio, fin) del día local que empieza en dia.
func (c *Calendario) intervalos(dia time.Time) [][2]time.Time {
	y, m, d := dia.Date()
	fer, esFeriado := c.feriados[fecha{y, m, d}]
	if esFeriado && fer.Desde == nil {
		return nil
	}
	iso := int(dia.Weekday())
	if iso == 0 {
		iso = 7
	}
	var salida [][2]time.Time
	for _, t := range c.tramos[iso] {
		fin := t.Fin
		if esFeriado && *fer.Desde < fin {
			fin = *fer.Desde
		}
		if t.Inicio >= fin {
			continue
		}
		salida = append(salida, [2]time.Time{c.aHora(y, m, d, t.Inicio), c.aHora(y, m, d, fin)})
	}
	return salida
}

func (c *Calendario) aHora(y int, m time.Month, d int, desde time.Duration) time.Time {
	h := int(desde / time.Hour)
	mi := int(desde % time.Hour / time.Minute)
	s := int(desde % time.Minute / time.Second)
	return time.Date(y, m, d, h, mi, s, 0, c.zona)
}

func (c *Calendario) medianoche(t time.Time) time.Time {
	l := t.In(c.zona)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, c.zona)
}

// Sumar devuelve el instante en que se cumplen d de tiempo laboral contados desde desde. Si
// desde cae fuera del horario, el reloj arranca en el siguiente tramo.
func (c *Calendario) Sumar(desde time.Time, d time.Duration) time.Time {
	if c == nil {
		return desde.Add(d).UTC()
	}
	if d <= 0 {
		return desde.UTC()
	}
	restante := d
	dia := c.medianoche(desde)
	for range maxDias {
		for _, iv := range c.intervalos(dia) {
			ini := iv[0]
			if ini.Before(desde) {
				ini = desde
			}
			if !ini.Before(iv[1]) {
				continue
			}
			disponible := iv[1].Sub(ini)
			if restante <= disponible {
				return ini.Add(restante).UTC()
			}
			restante -= disponible
		}
		dia = dia.AddDate(0, 0, 1)
	}
	return desde.Add(d).UTC() // inalcanzable con un calendario válido
}

// Entre devuelve el tiempo laboral entre a y b (0 si b no es posterior).
func (c *Calendario) Entre(a, b time.Time) time.Duration {
	if !b.After(a) {
		return 0
	}
	if c == nil {
		return b.Sub(a)
	}
	var total time.Duration
	dia := c.medianoche(a)
	for range maxDias {
		if !dia.Before(b) {
			break
		}
		for _, iv := range c.intervalos(dia) {
			ini, fin := iv[0], iv[1]
			if ini.Before(a) {
				ini = a
			}
			if fin.After(b) {
				fin = b
			}
			if fin.After(ini) {
				total += fin.Sub(ini)
			}
		}
		dia = dia.AddDate(0, 0, 1)
	}
	return total
}

// cargarCalendario lee el calendario de la base (nil id = 24/7).
func cargarCalendario(ctx context.Context, q *soportebd.Queries, id *int64) (*Calendario, error) {
	if id == nil {
		return nil, nil
	}
	cal, err := q.CalendarioPorID(ctx, *id)
	if err != nil {
		return nil, fmt.Errorf("calendario %d: %w", *id, err)
	}
	filasT, err := q.TramosDeCalendario(ctx, *id)
	if err != nil {
		return nil, err
	}
	filasF, err := q.FeriadosDeCalendario(ctx, *id)
	if err != nil {
		return nil, err
	}
	tramos := make([]Tramo, 0, len(filasT))
	for _, t := range filasT {
		tramos = append(tramos, Tramo{Dia: int(t.DiaSemana), Inicio: duracionDe(t.Inicio), Fin: finDe(t.Fin)})
	}
	feriados := make([]Feriado, 0, len(filasF))
	for _, f := range filasF {
		y, m, d := f.Fecha.Time.Date()
		fer := Feriado{Anio: y, Mes: m, Dia: d}
		if f.Desde.Valid {
			desde := duracionDe(f.Desde)
			fer.Desde = &desde
		}
		feriados = append(feriados, fer)
	}
	return NuevoCalendario(cal.ZonaHoraria, tramos, feriados)
}

func duracionDe(t pgtype.Time) time.Duration {
	return time.Duration(t.Microseconds) * time.Microsecond
}

// finDe: el fin de un tramo «hasta 24:00» se guarda como 23:59:59.999999; se redondea a 24 h.
func finDe(t pgtype.Time) time.Duration {
	d := duracionDe(t)
	if d >= 24*time.Hour-time.Second {
		return 24 * time.Hour
	}
	return d
}

// Relojes del ticket ----------------------------------------------------------------

func minutos(m int32) time.Duration { return time.Duration(m) * time.Minute }

// riesgo es el 80 % del objetivo.
func riesgo(objetivo time.Duration) time.Duration { return objetivo * 8 / 10 }

// aplicarSLA recalcula los vencimientos del ticket según sus objetivos, la pausa acumulada y
// el estado. La respuesta no se pausa; la resolución no corre en espera (null) y, fuera de
// espera, vence en sla_inicio + objetivo + pausa (en tiempo laboral).
func aplicarSLA(f *soportebd.SoporteTicket, cal *Calendario) {
	f.RiesgoRespuesta, f.VenceRespuesta = nil, nil
	if f.SlaRespuestaMin != nil {
		obj := minutos(*f.SlaRespuestaMin)
		r, v := cal.Sumar(f.SlaInicio, riesgo(obj)), cal.Sumar(f.SlaInicio, obj)
		f.RiesgoRespuesta, f.VenceRespuesta = &r, &v
	}
	f.RiesgoResolucion, f.VenceResolucion = nil, nil
	if f.SlaResolucionMin != nil && f.Estado != EstadoEnEspera {
		obj := minutos(*f.SlaResolucionMin)
		pausa := time.Duration(f.PausaSeg) * time.Second
		r, v := cal.Sumar(f.SlaInicio, riesgo(obj)+pausa), cal.Sumar(f.SlaInicio, obj+pausa)
		f.RiesgoResolucion, f.VenceResolucion = &r, &v
	}
}

// acumularPausa suma a pausa_seg el tiempo laboral desde desde hasta ahora.
func acumularPausa(f *soportebd.SoporteTicket, cal *Calendario, desde *time.Time, ahora time.Time) {
	if desde == nil {
		return
	}
	f.PausaSeg += int64(cal.Entre(*desde, ahora) / time.Second)
}

// salirDeEspera cierra la pausa en curso (si el ticket estaba en espera).
func salirDeEspera(f *soportebd.SoporteTicket, cal *Calendario, ahora time.Time) {
	if f.Estado != EstadoEnEspera {
		return
	}
	acumularPausa(f, cal, f.EnEsperaDesde, ahora)
	f.EnEsperaDesde = nil
}

// RelojSLA es el estado de un reloj (respuesta o resolución) para mostrar.
type RelojSLA struct {
	ObjetivoMin *int32     `json:"objetivoMin" nullable:"true" doc:"Objetivo en minutos (laborales si el SLA tiene calendario); null = sin SLA"`
	RiesgoEn    *time.Time `json:"riesgoEn" nullable:"true" doc:"80 % del objetivo"`
	VenceEn     *time.Time `json:"venceEn" nullable:"true" doc:"Vencimiento; null = sin SLA o en pausa"`
	CumplidoEn  *time.Time `json:"cumplidoEn" nullable:"true" doc:"Primera respuesta o resolución"`
	Estado      string     `json:"estado" enum:"sin_sla,en_tiempo,en_riesgo,vencido,cumplido,incumplido,pausado"`
}

func reloj(objetivo *int32, riesgoEn, venceEn, cumplidoEn *time.Time, pausado bool, ref time.Time) RelojSLA {
	r := RelojSLA{ObjetivoMin: objetivo, RiesgoEn: riesgoEn, VenceEn: venceEn, CumplidoEn: cumplidoEn}
	switch {
	case objetivo == nil:
		r.Estado = "sin_sla"
	case cumplidoEn != nil && venceEn != nil && cumplidoEn.After(*venceEn):
		r.Estado = "incumplido"
	case cumplidoEn != nil:
		r.Estado = "cumplido"
	case pausado:
		r.Estado = "pausado"
	case venceEn == nil:
		r.Estado = "sin_sla"
	case !ref.Before(*venceEn):
		r.Estado = "vencido"
	case riesgoEn != nil && !ref.Before(*riesgoEn):
		r.Estado = "en_riesgo"
	default:
		r.Estado = "en_tiempo"
	}
	return r
}
