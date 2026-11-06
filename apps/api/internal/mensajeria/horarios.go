package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Horarios (contratos/conversaciones.md §10.1): entidad genérica que se asigna a un número, a una
// cola y, en la fase 2, a un asistente. Franjas por día de la semana en hora local de su zona y
// feriados que cierran el día o lo reemplazan. En la base todo es UTC; la zona solo convierte.
// `perfil` por franja es el punto de extensión de la fase 2 (permisos del asistente en esa franja).

// FranjaHorario es un tramo abierto de un día.
type FranjaHorario struct {
	Dias   []int   `json:"dias,omitempty" maxItems:"7" uniqueItems:"true" doc:"1 = lunes … 7 = domingo (en un feriado no va)"`
	Inicio string  `json:"inicio" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"HH:MM local"`
	Fin    string  `json:"fin" pattern:"^(([01][0-9]|2[0-3]):[0-5][0-9]|24:00)$" doc:"HH:MM local (mayor que inicio; 24:00 = fin del día)"`
	Perfil *string `json:"perfil,omitempty" maxLength:"60" nullable:"true" doc:"Fase 2: conjunto de permisos de un asistente en esta franja (se guarda; hoy nada lo usa)"`
}

// FeriadoHorario cierra un día o reemplaza sus franjas.
type FeriadoHorario struct {
	Fecha   string          `json:"fecha" format:"date" doc:"AAAA-MM-DD local"`
	Nombre  string          `json:"nombre" minLength:"1" maxLength:"80"`
	Franjas []FranjaHorario `json:"franjas" doc:"Vacío = cerrado todo el día"`
}

// Horario es un horario de atención.
type Horario struct {
	ID        int64            `json:"id"`
	EmpresaID *int64           `json:"empresaId" nullable:"true"`
	Nombre    string           `json:"nombre"`
	Zona      string           `json:"zona" doc:"IANA, America/Guatemala por defecto"`
	Franjas   []FranjaHorario  `json:"franjas"`
	Feriados  []FeriadoHorario `json:"feriados"`
	Activo    bool             `json:"activo"`
	Version   int32            `json:"version"`
}

// ListaHorarios es { data, nextCursor }.
type ListaHorarios struct {
	Data       []Horario `json:"data"`
	NextCursor *string   `json:"nextCursor" nullable:"true"`
}

func aHorario(f mensajeriabd.MensajeriaHorario) Horario {
	h := Horario{ID: f.ID, EmpresaID: f.EmpresaID, Nombre: f.Nombre, Zona: f.Zona, Activo: f.Activo, Version: f.Version,
		Franjas: []FranjaHorario{}, Feriados: []FeriadoHorario{}}
	_ = json.Unmarshal(f.Franjas, &h.Franjas)
	_ = json.Unmarshal(f.Feriados, &h.Feriados)
	return h
}

// Evaluación ----------------------------------------------------------------------------------------

type tramo struct {
	ini, fin int // minutos desde la medianoche local
	perfil   *string
}

type calendario struct {
	zona     *time.Location
	semanal  map[int][]tramo
	feriados map[string][]tramo
}

func minutosHM(s string) (int, error) {
	if s == "24:00" {
		return 24 * 60, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

func tramosDe(fs []FranjaHorario, campo string) ([]tramo, error) {
	var out []tramo
	for i, f := range fs {
		ini, err1 := minutosHM(f.Inicio)
		fin, err2 := minutosHM(f.Fin)
		if err1 != nil || err2 != nil || fin <= ini {
			return nil, campoConv(fmt.Sprintf("%s[%d]", campo, i), "Inicio y fin HH:MM con fin mayor que inicio (una franja que cruza la medianoche se escribe como dos)", "franja")
		}
		out = append(out, tramo{ini: ini, fin: fin, perfil: f.Perfil})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ini < out[b].ini })
	for i := 1; i < len(out); i++ {
		if out[i].ini < out[i-1].fin {
			return nil, campoConv(campo, "Las franjas de un mismo día no pueden solaparse", "solapada")
		}
	}
	return out, nil
}

// calendarioDe valida y arma el calendario de un horario.
func calendarioDe(zona string, franjas []FranjaHorario, feriados []FeriadoHorario) (*calendario, error) {
	loc, err := time.LoadLocation(zona)
	if err != nil {
		return nil, campoConv("zona", "Zona IANA desconocida", "zona")
	}
	c := &calendario{zona: loc, semanal: map[int][]tramo{}, feriados: map[string][]tramo{}}
	porDia := map[int][]FranjaHorario{}
	for i, f := range franjas {
		if len(f.Dias) == 0 {
			return nil, campoConv(fmt.Sprintf("franjas[%d].dias", i), "Indica los días (1 = lunes … 7 = domingo)", "required")
		}
		for _, d := range f.Dias {
			if d < 1 || d > 7 {
				return nil, campoConv(fmt.Sprintf("franjas[%d].dias", i), "Días del 1 (lunes) al 7 (domingo)", "dia")
			}
			porDia[d] = append(porDia[d], f)
		}
	}
	for d, fs := range porDia {
		ts, err := tramosDe(fs, "franjas")
		if err != nil {
			return nil, err
		}
		c.semanal[d] = ts
	}
	for i, f := range feriados {
		if _, err := time.Parse("2006-01-02", f.Fecha); err != nil {
			return nil, campoConv(fmt.Sprintf("feriados[%d].fecha", i), "Fecha AAAA-MM-DD", "date")
		}
		if _, ok := c.feriados[f.Fecha]; ok {
			return nil, campoConv(fmt.Sprintf("feriados[%d].fecha", i), "Fecha repetida", "unica")
		}
		ts, err := tramosDe(f.Franjas, fmt.Sprintf("feriados[%d].franjas", i))
		if err != nil {
			return nil, err
		}
		if ts == nil {
			ts = []tramo{}
		}
		c.feriados[f.Fecha] = ts
	}
	return c, nil
}

func calendarioDeFila(f mensajeriabd.MensajeriaHorario) (*calendario, error) {
	h := aHorario(f)
	return calendarioDe(h.Zona, h.Franjas, h.Feriados)
}

// tramosDelDia: las franjas de esa fecha local (un feriado las reemplaza).
func (c *calendario) tramosDelDia(dia time.Time) []tramo {
	if ts, ok := c.feriados[dia.Format("2006-01-02")]; ok {
		return ts
	}
	d := int(dia.Weekday())
	if d == 0 {
		d = 7
	}
	return c.semanal[d]
}

// estado: si está abierto en t, la franja y el próximo cambio (cierre si está abierto; apertura si
// está cerrado; cero si no vuelve a abrir en un año).
func (c *calendario) estado(t time.Time) (bool, *tramo, time.Time) {
	local := t.In(c.zona)
	dia := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, c.zona)
	ahora := local.Hour()*60 + local.Minute()
	instante := func(d time.Time, m int) time.Time { return d.Add(time.Duration(m) * time.Minute).UTC() }
	for _, tr := range c.tramosDelDia(dia) {
		if ahora >= tr.ini && ahora < tr.fin {
			tt := tr
			fin := instante(dia, tr.fin)
			// Una franja que termina a las 24:00 y sigue al día siguiente desde las 00:00: el cierre es después.
			for d := 1; d < 8 && tt.fin == 24*60; d++ {
				sig := dia.AddDate(0, 0, d)
				ts := c.tramosDelDia(sig)
				if len(ts) == 0 || ts[0].ini != 0 {
					break
				}
				fin = instante(sig, ts[0].fin)
				if ts[0].fin != 24*60 {
					break
				}
			}
			return true, &tt, fin
		}
	}
	for d := 0; d <= 370; d++ {
		sig := dia.AddDate(0, 0, d)
		for _, tr := range c.tramosDelDia(sig) {
			if d == 0 && tr.ini <= ahora {
				continue
			}
			return false, nil, instante(sig, tr.ini)
		}
	}
	return false, nil, time.Time{}
}

// Casos -------------------------------------------------------------------------------------------

// EntradaHorario crea o actualiza un horario.
type EntradaHorario struct {
	ID       int64            `json:"-"`
	Version  int32            `json:"version,omitempty" minimum:"0" doc:"Obligatoria al actualizar"`
	Nombre   string           `json:"nombre" minLength:"1" maxLength:"80"`
	Zona     string           `json:"zona,omitempty" maxLength:"60" doc:"America/Guatemala por defecto"`
	Franjas  []FranjaHorario  `json:"franjas" maxItems:"50"`
	Feriados []FeriadoHorario `json:"feriados,omitempty" maxItems:"100"`
	Activo   *bool            `json:"activo,omitempty" doc:"Al actualizar (por defecto sigue igual)"`
}

// EstadoHorario dice si está abierto.
type EstadoHorario struct {
	Abierto       bool           `json:"abierto"`
	Franja        *FranjaHorario `json:"franja,omitempty" doc:"La franja abierta (con su perfil)"`
	ProximoCambio *time.Time     `json:"proximoCambio" nullable:"true" doc:"Cierre si está abierto; apertura si está cerrado"`
}

// EntradaEstadoHorario es GET /horarios/{id}/estado.
type EntradaEstadoHorario struct {
	ID int64     `path:"id" minimum:"1"`
	En time.Time `query:"en" doc:"Instante a evaluar (RFC 3339; por defecto, ahora)"`
}

// EntradaHorarioID es un horario por id.
type EntradaHorarioID struct {
	ID int64 `path:"id" minimum:"1"`
}

func (m *Modulo) horarioDeEmpresa(ctx context.Context, q *mensajeriabd.Queries, empresa int64, id *int64) error {
	if id == nil {
		return nil
	}
	if _, err := q.HorarioPorID(ctx, mensajeriabd.HorarioPorIDParams{ID: *id, Empresa: empresa}); errors.Is(err, pgx.ErrNoRows) {
		return campoConv("horarioId", "No es un horario de la empresa", "horario")
	} else if err != nil {
		return err
	}
	return nil
}

func nuevoListarHorarios(*Modulo) *auditoria.Caso[struct{}, ListaHorarios] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ListaHorarios]{
		Accion: "conversaciones.listar_horarios", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ListaHorarios],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ListaHorarios, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaHorarios{}, err
			}
			fs, err := mensajeriabd.New(t).ListarHorarios(ctx, a.empresa)
			out := ListaHorarios{Data: make([]Horario, 0, len(fs))}
			for _, f := range fs {
				out.Data = append(out.Data, aHorario(f))
			}
			return out, err
		},
	})
}

func nuevoVerHorario(*Modulo) *auditoria.Caso[EntradaHorarioID, Horario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaHorarioID, Horario]{
		Accion: "conversaciones.ver_horario", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[Horario],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaHorarioID) (Horario, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return Horario{}, err
			}
			f, err := mensajeriabd.New(t).HorarioPorID(ctx, mensajeriabd.HorarioPorIDParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return Horario{}, nucleo.NoEncontrado("El horario no existe")
			}
			return aHorario(f), err
		},
	})
}

func nuevoEstadoHorario(m *Modulo) *auditoria.Caso[EntradaEstadoHorario, EstadoHorario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEstadoHorario, EstadoHorario]{
		Accion: "conversaciones.estado_horario", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[EstadoHorario],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEstadoHorario) (EstadoHorario, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return EstadoHorario{}, err
			}
			f, err := mensajeriabd.New(t).HorarioPorID(ctx, mensajeriabd.HorarioPorIDParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return EstadoHorario{}, nucleo.NoEncontrado("El horario no existe")
			}
			if err != nil {
				return EstadoHorario{}, err
			}
			c, err := calendarioDeFila(f)
			if err != nil {
				return EstadoHorario{}, err
			}
			en := m.ahora()
			if !in.En.IsZero() {
				en = in.En
			}
			abierto, tr, prox := c.estado(en)
			out := EstadoHorario{Abierto: abierto}
			if !prox.IsZero() {
				out.ProximoCambio = &prox
			}
			if tr != nil {
				hm := func(n int) string { return fmt.Sprintf("%02d:%02d", n/60, n%60) }
				out.Franja = &FranjaHorario{Inicio: hm(tr.ini), Fin: hm(tr.fin), Perfil: tr.perfil}
			}
			return out, nil
		},
	})
}

func horarioAuditable(h Horario) map[string]any {
	return map[string]any{"nombre": h.Nombre, "zona": h.Zona, "franjas": h.Franjas, "feriados": h.Feriados, "activo": h.Activo}
}

func nuevoGuardarHorario(_ *Modulo, crear bool) *auditoria.Caso[EntradaHorario, Horario] {
	accion := "canales.actualizar_horario"
	if crear {
		accion = "canales.crear_horario"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaHorario, Horario]{
		Accion: accion, Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaHorario) (Horario, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return Horario{}, err
			}
			if in.Zona == "" {
				in.Zona = "America/Guatemala"
			}
			if in.Franjas == nil {
				in.Franjas = []FranjaHorario{}
			}
			if in.Feriados == nil {
				in.Feriados = []FeriadoHorario{}
			}
			for i := range in.Feriados {
				if in.Feriados[i].Franjas == nil {
					in.Feriados[i].Franjas = []FranjaHorario{}
				}
			}
			if _, err := calendarioDe(in.Zona, in.Franjas, in.Feriados); err != nil {
				return Horario{}, err
			}
			franjas, _ := json.Marshal(in.Franjas)
			feriados, _ := json.Marshal(in.Feriados)
			q := mensajeriabd.New(t)
			t.FijarEmpresa(a.empresa)
			dup := func(err error) error { return conflictoNombre(err, "un horario") }
			if crear {
				f, err := q.InsertarHorario(ctx, mensajeriabd.InsertarHorarioParams{EmpresaID: a.empresaPtr(), Nombre: strings.TrimSpace(in.Nombre), Zona: in.Zona,
					Franjas: franjas, Feriados: feriados})
				if err != nil {
					return Horario{}, dup(err)
				}
				h := aHorario(f)
				t.RegistrarCambios("horario", f.ID, nil, horarioAuditable(h))
				return h, t.Emitir("horario.creado", map[string]any{"horario_id": f.ID, "empresa_id": f.EmpresaID, "nombre": f.Nombre})
			}
			antes, err := q.HorarioParaActualizar(ctx, mensajeriabd.HorarioParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return Horario{}, nucleo.NoEncontrado("El horario no existe")
			}
			if err != nil {
				return Horario{}, err
			}
			if antes.Version != in.Version {
				return Horario{}, nucleo.ConflictoVersion("horario", in.ID)
			}
			activo := antes.Activo
			if in.Activo != nil {
				activo = *in.Activo
			}
			f, err := q.ActualizarHorario(ctx, mensajeriabd.ActualizarHorarioParams{ID: in.ID, Nombre: strings.TrimSpace(in.Nombre), Zona: in.Zona,
				Franjas: franjas, Feriados: feriados, Activo: activo})
			if err != nil {
				return Horario{}, dup(err)
			}
			h := aHorario(f)
			campos := t.RegistrarCambios("horario", f.ID, horarioAuditable(aHorario(antes)), horarioAuditable(h))
			if campos == nil {
				campos = []string{}
			}
			return h, t.Emitir("horario.actualizado", map[string]any{"horario_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activo": f.Activo})
		},
	})
}

// calendarioEfectivo: el horario de la cola o, si no tiene, el del número (nil = siempre abierto).
func calendarioEfectivo(ctx context.Context, q *mensajeriabd.Queries, colaHorario *int64, canal int64) (*calendario, error) {
	var f mensajeriabd.MensajeriaHorario
	var err error
	switch {
	case colaHorario != nil:
		f, err = q.HorarioPorIDSinEmpresa(ctx, *colaHorario)
	default:
		var ca mensajeriabd.MensajeriaCanal
		if ca, err = q.CanalPorID(ctx, canal); err != nil {
			return nil, err
		}
		if ca.HorarioID == nil {
			return nil, nil
		}
		f, err = q.HorarioPorIDSinEmpresa(ctx, *ca.HorarioID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || !f.Activo {
		return nil, err
	}
	return calendarioDeFila(f)
}
