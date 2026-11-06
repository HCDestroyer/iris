package mensajeria

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Bandeja 1.1 (contratos/conversaciones.md §14.1 y §14.5): supervisores por cola, equipo de la
// cola, disponibilidad de los agentes y los ajustes de la empresa.

// Estados y orígenes de la disponibilidad.
const (
	Disponible   = "disponible"
	NoDisponible = "no_disponible"

	OrigenDispManual       = "manual"
	OrigenDispCierreSesion = "cierre_sesion"
	OrigenDispInactividad  = "inactividad"
	OrigenDispHorario      = "horario"
)

// MinutosInactividadPorDefecto: sin ajuste de la empresa, pasa a no_disponible a los 15 min.
const MinutosInactividadPorDefecto = 15

// maxDisponiblesEspera: cuántos disponibles trae `espera` (el total va aparte).
const maxDisponiblesEspera = 5

// Supervisores por cola -------------------------------------------------------------------------

// EntradaSupervisoresCola es POST /colas/{id}/supervisores.
type EntradaSupervisoresCola struct {
	ID          int64   `json:"-"`
	Version     int32   `json:"version" minimum:"1"`
	UsuarioIDs  []int64 `json:"usuarioIds" maxItems:"20" uniqueItems:"true" doc:"Personas que supervisan la cola (ven, asignan y pasan todo lo de la cola)"`
	AsistenteIA bool    `json:"asistenteIa,omitempty" doc:"Fase 3: un asistente de IA como supervisor (hoy 422 no_disponible_aun)"`
}

func noDisponibleAun(detalle string) *nucleo.Problema {
	return reglaConv("no_disponible_aun", detalle)
}

func nuevoSupervisoresCola(m *Modulo) *auditoria.Caso[EntradaSupervisoresCola, ColaConversaciones] {
	return auditoria.Nuevo(auditoria.Caso[EntradaSupervisoresCola, ColaConversaciones]{
		Accion: "canales.supervisores_cola", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSupervisoresCola) (ColaConversaciones, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ColaConversaciones{}, err
			}
			if in.AsistenteIA {
				return ColaConversaciones{}, noDisponibleAun("Un asistente de IA como supervisor de la cola llegará en una fase siguiente")
			}
			antes, err := m.colaParaActualizar(ctx, t, a, in.ID)
			if err != nil {
				return ColaConversaciones{}, err
			}
			if antes.Version != in.Version {
				return ColaConversaciones{}, nucleo.ConflictoVersion("cola", in.ID)
			}
			q := mensajeriabd.New(t)
			previos, err := colasConMiembros(ctx, q, []mensajeriabd.MensajeriaCola{antes})
			if err != nil {
				return ColaConversaciones{}, err
			}
			for _, u := range in.UsuarioIDs {
				if _, _, err := usuarioAtiende(ctx, t, a.empresa, u); err != nil {
					return ColaConversaciones{}, err
				}
			}
			if err := q.BorrarSupervisoresCola(ctx, in.ID); err != nil {
				return ColaConversaciones{}, err
			}
			for _, u := range in.UsuarioIDs {
				if err := q.InsertarSupervisorCola(ctx, mensajeriabd.InsertarSupervisorColaParams{ColaID: in.ID, UsuarioID: &u}); err != nil {
					return ColaConversaciones{}, err
				}
			}
			f, err := q.TocarCola(ctx, in.ID)
			if err != nil {
				return ColaConversaciones{}, err
			}
			cs, err := colasConMiembros(ctx, q, []mensajeriabd.MensajeriaCola{f})
			if err != nil {
				return ColaConversaciones{}, err
			}
			t.RegistrarCambios("cola", f.ID, map[string]any{"supervisores": idsDe(previos[0].Supervisores)}, map[string]any{"supervisores": idsDe(cs[0].Supervisores)})
			return cs[0], t.Emitir("cola.actualizada", map[string]any{"cola_id": f.ID, "empresa_id": f.EmpresaID, "campos": []string{"supervisores"}, "activa": f.Activa})
		},
	})
}

func idsDe(us []UsuarioRef) []int64 {
	out := []int64{}
	for _, u := range us {
		out = append(out, u.ID)
	}
	return out
}

// atiendeCola: miembro o supervisor de la cola.
func atiendeCola(ctx context.Context, q *mensajeriabd.Queries, cola, usuario int64) (bool, error) {
	return q.AtiendeCola(ctx, mensajeriabd.AtiendeColaParams{ColaID: cola, UsuarioID: usuario})
}

// supervisaColaDe: el usuario supervisa la cola de la conversación (false sin cola).
func supervisaColaDe(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion, usuario int64) (bool, error) {
	if c.ColaID == nil {
		return false, nil
	}
	return q.EsSupervisorCola(ctx, mensajeriabd.EsSupervisorColaParams{ColaID: *c.ColaID, UsuarioID: &usuario})
}

// Equipo y disponibles ----------------------------------------------------------------------------

// disponibilidades: estado por usuario en la empresa (sin fila: no_disponible).
func disponibilidades(ctx context.Context, q *mensajeriabd.Queries, empresa int64, usuarios []int64) (map[int64]DisponibilidadAgente, error) {
	out := map[int64]DisponibilidadAgente{}
	if len(usuarios) == 0 {
		return out, nil
	}
	fs, err := q.DisponibilidadDeUsuarios(ctx, mensajeriabd.DisponibilidadDeUsuariosParams{Empresa: empresa, Usuarios: usuarios})
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		d := f.Desde
		out[f.UsuarioID] = DisponibilidadAgente{Estado: f.Estado, Origen: f.Origen, Desde: &d}
	}
	return out, nil
}

func dispDe(m map[int64]DisponibilidadAgente, id int64) DisponibilidadAgente {
	if d, ok := m[id]; ok {
		return d
	}
	return DisponibilidadAgente{Estado: NoDisponible, Origen: OrigenDispManual}
}

// equipoDeCola: supervisores de la cola y el equipo (miembros y supervisores) con su disponibilidad.
func equipoDeCola(ctx context.Context, q *mensajeriabd.Queries, empresa int64, cola *int64) ([]UsuarioRef, []MiembroEquipo, error) {
	sups, equipo := []UsuarioRef{}, []MiembroEquipo{}
	if cola == nil {
		return sups, equipo, nil
	}
	ms, err := q.MiembrosDeColas(ctx, []int64{*cola})
	if err != nil {
		return nil, nil, err
	}
	ss, err := q.SupervisoresDeColas(ctx, []int64{*cola})
	if err != nil {
		return nil, nil, err
	}
	ids := []int64{}
	for _, x := range ms {
		ids = append(ids, x.ID)
	}
	for _, x := range ss {
		ids = append(ids, x.ID)
	}
	disp, err := disponibilidades(ctx, q, empresa, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, x := range ms {
		equipo = append(equipo, MiembroEquipo{ID: x.ID, Nombre: x.Nombre, Rol: "miembro", Disponible: dispDe(disp, x.ID).Estado == Disponible})
	}
	for _, x := range ss {
		sups = append(sups, UsuarioRef{ID: x.ID, Nombre: x.Nombre})
		if !slices.ContainsFunc(equipo, func(e MiembroEquipo) bool { return e.ID == x.ID }) {
			equipo = append(equipo, MiembroEquipo{ID: x.ID, Nombre: x.Nombre, Rol: "supervisor", Disponible: dispDe(disp, x.ID).Estado == Disponible})
		}
	}
	return sups, equipo, nil
}

// disponiblesDe: los disponibles que atienden la cola (sin cola, los de la empresa).
func disponiblesDe(ctx context.Context, q *mensajeriabd.Queries, empresa int64, cola *int64) ([]UsuarioRef, error) {
	fs, err := q.DisponiblesDeCola(ctx, mensajeriabd.DisponiblesDeColaParams{Empresa: empresa, ColaID: cola})
	if err != nil {
		return nil, err
	}
	out := make([]UsuarioRef, 0, len(fs))
	for _, f := range fs {
		out = append(out, UsuarioRef{ID: f.ID, Nombre: f.Nombre})
	}
	return out, nil
}

// Disponibilidad ---------------------------------------------------------------------------------

// DisponibilidadAgente es el estado de un agente.
type DisponibilidadAgente struct {
	Estado string     `json:"estado" enum:"disponible,no_disponible"`
	Origen string     `json:"origen" enum:"manual,cierre_sesion,inactividad,horario" doc:"Qué la dejó así"`
	Desde  *time.Time `json:"desde" nullable:"true"`
}

// MiDisponibilidad es GET /conversaciones/disponibilidad.
type MiDisponibilidad struct {
	Estado             string     `json:"estado" enum:"disponible,no_disponible"`
	Origen             string     `json:"origen" enum:"manual,cierre_sesion,inactividad,horario"`
	Desde              *time.Time `json:"desde" nullable:"true"`
	HorarioHasta       *time.Time `json:"horarioHasta" nullable:"true" doc:"Cierre de tu horario de trabajo: a esa hora pasas a no_disponible"`
	MinutosInactividad int32      `json:"minutosInactividad" doc:"Sin actividad en estos minutos pasas a no_disponible"`
	Version            int32      `json:"version" doc:"0 si nunca la cambiaste"`
}

// EntradaDisponibilidad es POST /conversaciones/disponibilidad.
type EntradaDisponibilidad struct {
	Estado  string `json:"estado" enum:"disponible,no_disponible"`
	Version int32  `json:"version" minimum:"0" doc:"La de GET /conversaciones/disponibilidad (0 la primera vez)"`
}

// latidoDisponibilidad: el resultado del latido (sin auditar).
type latidoDisponibilidad struct {
	MiDisponibilidad
}

func (m *Modulo) minutosInactividad(ctx context.Context, q *mensajeriabd.Queries, empresa int64) (int32, error) {
	f, err := q.AjustesBandeja(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return MinutosInactividadPorDefecto, nil
	}
	if err != nil {
		return 0, err
	}
	return f.MinutosInactividad, nil
}

func (m *Modulo) miDisponibilidad(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, f *mensajeriabd.MensajeriaDisponibilidad) (MiDisponibilidad, error) {
	min, err := m.minutosInactividad(ctx, q, a.empresa)
	if err != nil {
		return MiDisponibilidad{}, err
	}
	if f == nil {
		return MiDisponibilidad{Estado: NoDisponible, Origen: OrigenDispManual, MinutosInactividad: min}, nil
	}
	d := f.Desde
	return MiDisponibilidad{Estado: f.Estado, Origen: f.Origen, Desde: &d, HorarioHasta: f.HorarioHasta, MinutosInactividad: min, Version: f.Version}, nil
}

// horarioHastaDe: el cierre más tardío de las franjas abiertas ahora en los horarios de las colas
// del agente (nil: fuera de horario, sin horario o siempre abierto). con = false si no se puede
// saber (no cambia lo anotado).
func (m *Modulo) horarioHastaDe(ctx context.Context, q *mensajeriabd.Queries, empresa, usuario int64) (*time.Time, bool, error) {
	siempre, err := q.AgenteSiempreEnHorario(ctx, mensajeriabd.AgenteSiempreEnHorarioParams{Empresa: empresa, UsuarioID: usuario})
	if err != nil {
		return nil, false, err
	}
	if siempre {
		return nil, true, nil
	}
	hs, err := q.HorariosDeAgente(ctx, mensajeriabd.HorariosDeAgenteParams{Empresa: empresa, UsuarioID: usuario})
	if err != nil {
		return nil, false, err
	}
	ahora := m.ahora()
	var hasta *time.Time
	for _, h := range hs {
		cal, err := calendarioDeFila(h)
		if err != nil || cal == nil {
			continue
		}
		if abierto, _, fin := cal.estado(ahora); abierto && (hasta == nil || fin.After(*hasta)) {
			f := fin
			hasta = &f
		}
	}
	// Fuera de horario: nada que anotar (queda como estaba).
	return hasta, hasta != nil, nil
}

func nuevoVerDisponibilidad(m *Modulo) *auditoria.Caso[struct{}, MiDisponibilidad] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, MiDisponibilidad]{
		Accion: "conversaciones.ver_disponibilidad", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[MiDisponibilidad],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (MiDisponibilidad, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return MiDisponibilidad{}, err
			}
			q := mensajeriabd.New(t)
			f, err := q.DisponibilidadDe(ctx, mensajeriabd.DisponibilidadDeParams{UsuarioID: a.usuario, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return m.miDisponibilidad(ctx, q, a, nil)
			}
			if err != nil {
				return MiDisponibilidad{}, err
			}
			return m.miDisponibilidad(ctx, q, a, &f)
		},
	})
}

func sesionDe(ctx context.Context) *int64 {
	if s := contexto.SesionDe(ctx); s.ID != 0 {
		id := s.ID
		return &id
	}
	return nil
}

func nuevoFijarDisponibilidad(m *Modulo) *auditoria.Caso[EntradaDisponibilidad, MiDisponibilidad] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDisponibilidad, MiDisponibilidad]{
		Accion: "conversaciones.disponibilidad", Permiso: nucleo.PermConversacionesVer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDisponibilidad) (MiDisponibilidad, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return MiDisponibilidad{}, err
			}
			if in.Estado != Disponible && in.Estado != NoDisponible {
				return MiDisponibilidad{}, campoConv("estado", "disponible o no_disponible", "enum")
			}
			t.FijarEmpresa(a.empresa)
			q := mensajeriabd.New(t)
			ahora := m.ahora()
			var hasta *time.Time
			if in.Estado == Disponible {
				if hasta, _, err = m.horarioHastaDe(ctx, q, a.empresa, a.usuario); err != nil {
					return MiDisponibilidad{}, err
				}
			}
			f, err := q.DisponibilidadParaActualizar(ctx, mensajeriabd.DisponibilidadParaActualizarParams{UsuarioID: a.usuario, Empresa: a.empresa})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if in.Version != 0 {
					return MiDisponibilidad{}, nucleo.ConflictoVersion("disponibilidad", a.usuario)
				}
				f, err = q.InsertarDisponibilidad(ctx, mensajeriabd.InsertarDisponibilidadParams{UsuarioID: a.usuario, EmpresaID: a.empresaPtr(),
					Estado: in.Estado, Origen: OrigenDispManual, Desde: ahora, UltimaActividad: ahora, SesionID: sesionDe(ctx), HorarioHasta: hasta})
				if errors.Is(err, pgx.ErrNoRows) {
					return MiDisponibilidad{}, nucleo.ConflictoVersion("disponibilidad", a.usuario)
				}
				if err != nil {
					return MiDisponibilidad{}, err
				}
				t.FijarEntidad("disponibilidad", f.ID)
				t.RegistrarCambios("disponibilidad", f.ID, nil, map[string]any{"estado": f.Estado, "origen": f.Origen})
				if err := emitirDisponibilidad(t, f, NoDisponible); err != nil {
					return MiDisponibilidad{}, err
				}
				return m.miDisponibilidad(ctx, q, a, &f)
			case err != nil:
				return MiDisponibilidad{}, err
			}
			t.FijarEntidad("disponibilidad", f.ID)
			if f.Version != in.Version {
				return MiDisponibilidad{}, nucleo.ConflictoVersion("disponibilidad", a.usuario)
			}
			antes := f
			if f.Estado == in.Estado {
				// Sin cambio de estado: cuenta como actividad (y renueva la sesión y el horario).
				if f, err = q.LatidoDisponibilidad(ctx, mensajeriabd.LatidoDisponibilidadParams{ID: f.ID, Ahora: ahora, SesionID: sesionDe(ctx),
					ConHorario: in.Estado == Disponible, HorarioHasta: hasta}); err != nil {
					return MiDisponibilidad{}, err
				}
				return m.miDisponibilidad(ctx, q, a, &f)
			}
			if f, err = q.FijarDisponibilidad(ctx, mensajeriabd.FijarDisponibilidadParams{ID: f.ID, Estado: in.Estado, Origen: OrigenDispManual, Desde: ahora,
				UltimaActividad: ahora, SesionID: sesionDe(ctx), HorarioHasta: hasta}); err != nil {
				return MiDisponibilidad{}, err
			}
			t.RegistrarCambios("disponibilidad", f.ID, map[string]any{"estado": antes.Estado, "origen": antes.Origen}, map[string]any{"estado": f.Estado, "origen": f.Origen})
			if err := emitirDisponibilidad(t, f, antes.Estado); err != nil {
				return MiDisponibilidad{}, err
			}
			return m.miDisponibilidad(ctx, q, a, &f)
		},
	})
}

func emitirDisponibilidad(t *auditoria.Tx, f mensajeriabd.MensajeriaDisponibilidad, anterior string) error {
	return t.Emitir("agente.disponibilidad_cambiada", map[string]any{"usuario_id": f.UsuarioID, "empresa_id": f.EmpresaID, "estado": f.Estado,
		"origen": f.Origen, "anterior": anterior, "desde": f.Desde})
}

// nuevoLatido: actividad del panel cada ~60 s. Nunca audita ni emite: solo anota la actividad, la
// sesión y el cierre del horario. Sin fila (nunca se puso disponible) no crea nada.
func nuevoLatido(m *Modulo) *auditoria.Caso[struct{}, latidoDisponibilidad] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, latidoDisponibilidad]{
		Accion: "conversaciones.latido", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[latidoDisponibilidad],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (latidoDisponibilidad, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return latidoDisponibilidad{}, err
			}
			q := mensajeriabd.New(t)
			f, err := q.DisponibilidadDe(ctx, mensajeriabd.DisponibilidadDeParams{UsuarioID: a.usuario, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				d, err := m.miDisponibilidad(ctx, q, a, nil)
				return latidoDisponibilidad{d}, err
			}
			if err != nil {
				return latidoDisponibilidad{}, err
			}
			p := mensajeriabd.LatidoDisponibilidadParams{ID: f.ID, Ahora: m.ahora(), SesionID: sesionDe(ctx)}
			if f.Estado == Disponible {
				if p.HorarioHasta, p.ConHorario, err = m.horarioHastaDe(ctx, q, a.empresa, a.usuario); err != nil {
					return latidoDisponibilidad{}, err
				}
			}
			if f, err = q.LatidoDisponibilidad(ctx, p); err != nil {
				return latidoDisponibilidad{}, err
			}
			d, err := m.miDisponibilidad(ctx, q, a, &f)
			return latidoDisponibilidad{d}, err
		},
	})
}

// tocarActividad: una acción de la bandeja cuenta como actividad (sin auditar aparte).
func (m *Modulo) tocarActividad(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv) error {
	return q.TocarActividad(ctx, mensajeriabd.TocarActividadParams{UsuarioID: a.usuario, Empresa: a.empresa, Ahora: m.ahora()})
}

// nuevoVencerDisponibilidad (mantenimiento, cada minuto): un agente que debe pasar a
// no_disponible (cierre de sesión, horario o inactividad). Bloquea la fila y vuelve a calcular el
// motivo con ella bloqueada: dos barridos a la vez no repiten la transición. Cada transición real
// deja una fila de auditoría (actor sistema) y su evento; los latidos nunca.
func nuevoVencerDisponibilidad(m *Modulo) *auditoria.Caso[int64, bool] {
	return auditoria.Nuevo(auditoria.Caso[int64, bool]{
		Accion: "mensajeria.vencer_disponibilidad", Acceso: auditoria.Autenticado, AuditarExito: func(ok bool) bool { return ok },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (bool, error) {
			if m.fallarFila != nil {
				if err := m.fallarFila(id); err != nil {
					return false, err
				}
			}
			q := mensajeriabd.New(t)
			ahora := m.ahora()
			f, err := q.DisponibilidadPorID(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			motivo, err := q.MotivoVencerDisponibilidad(ctx, mensajeriabd.MotivoVencerDisponibilidadParams{ID: id, Ahora: ahora})
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			e := int64(0)
			if f.EmpresaID != nil {
				e = *f.EmpresaID
			}
			t.FijarEmpresa(e)
			t.FijarEntidad("disponibilidad", f.ID)
			antes := f
			if f, err = q.FijarDisponibilidad(ctx, mensajeriabd.FijarDisponibilidadParams{ID: f.ID, Estado: NoDisponible, Origen: motivo, Desde: ahora,
				UltimaActividad: f.UltimaActividad, SesionID: f.SesionID}); err != nil {
				return false, err
			}
			t.RegistrarCambios("disponibilidad", f.ID, map[string]any{"estado": antes.Estado, "origen": antes.Origen},
				map[string]any{"estado": f.Estado, "origen": f.Origen})
			t.Anotar(map[string]any{"usuario_id": f.UsuarioID, "motivo": motivo})
			return true, emitirDisponibilidad(t, f, antes.Estado)
		},
	})
}

// vencerDisponibilidades corre el barrido: un caso por agente. Un error en uno queda en el log y
// sigue con el siguiente; devuelve las transiciones y el primer error (el mantenimiento solo lo
// registra).
func (m *Modulo) vencerDisponibilidades(ctx context.Context, bd mensajeriabd.DBTX) (int, error) {
	fs, err := mensajeriabd.New(bd).DisponiblesPorVencer(ctx, m.ahora())
	if err != nil {
		slog.ErrorContext(ctx, "mensajeria: leer disponibilidades por vencer", "err", err)
		return 0, err
	}
	n := 0
	var primero error
	for _, x := range fs {
		ok, err := m.vencerDisp.Ejecutar(ctx, x.ID)
		if err != nil {
			slog.ErrorContext(ctx, "mensajeria: vencer disponibilidad", "disponibilidad_id", x.ID, "err", err)
			if primero == nil {
				primero = err
			}
			continue
		}
		if ok {
			n++
		}
	}
	return n, primero
}

// Ajustes de la bandeja --------------------------------------------------------------------------

// AjustesBandeja son los ajustes de la empresa.
type AjustesBandeja struct {
	MinutosInactividad int32 `json:"minutosInactividad" doc:"Sin actividad en estos minutos, el agente pasa a no_disponible (15 por defecto)"`
	Version            int32 `json:"version" doc:"0 si nunca se cambiaron"`
}

// EntradaAjustesBandeja es POST /conversaciones/ajustes.
type EntradaAjustesBandeja struct {
	Version            int32 `json:"version" minimum:"0"`
	MinutosInactividad int32 `json:"minutosInactividad" minimum:"1" maximum:"480"`
}

func nuevoVerAjustes(*Modulo) *auditoria.Caso[struct{}, AjustesBandeja] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesBandeja]{
		Accion: "conversaciones.ver_ajustes", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[AjustesBandeja],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesBandeja, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return AjustesBandeja{}, err
			}
			f, err := mensajeriabd.New(t).AjustesBandeja(ctx, a.empresa)
			if errors.Is(err, pgx.ErrNoRows) {
				return AjustesBandeja{MinutosInactividad: MinutosInactividadPorDefecto}, nil
			}
			return AjustesBandeja{MinutosInactividad: f.MinutosInactividad, Version: f.Version}, err
		},
	})
}

func nuevoFijarAjustes(*Modulo) *auditoria.Caso[EntradaAjustesBandeja, AjustesBandeja] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAjustesBandeja, AjustesBandeja]{
		Accion: "canales.ajustes_bandeja", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAjustesBandeja) (AjustesBandeja, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return AjustesBandeja{}, err
			}
			if in.MinutosInactividad < 1 || in.MinutosInactividad > 480 {
				return AjustesBandeja{}, campoConv("minutosInactividad", "Entre 1 y 480 minutos", "rango")
			}
			t.FijarEmpresa(a.empresa)
			q := mensajeriabd.New(t)
			f, err := q.AjustesBandejaParaActualizar(ctx, a.empresa)
			anterior := int32(MinutosInactividadPorDefecto)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if in.Version != 0 {
					return AjustesBandeja{}, nucleo.ConflictoVersion("ajustes_bandeja", a.empresa)
				}
				if f, err = q.InsertarAjustesBandeja(ctx, mensajeriabd.InsertarAjustesBandejaParams{EmpresaID: a.empresaPtr(), MinutosInactividad: in.MinutosInactividad}); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return AjustesBandeja{}, nucleo.ConflictoVersion("ajustes_bandeja", a.empresa)
					}
					return AjustesBandeja{}, err
				}
			case err != nil:
				return AjustesBandeja{}, err
			default:
				if f.Version != in.Version {
					return AjustesBandeja{}, nucleo.ConflictoVersion("ajustes_bandeja", a.empresa)
				}
				anterior = f.MinutosInactividad
				if f, err = q.ActualizarAjustesBandeja(ctx, mensajeriabd.ActualizarAjustesBandejaParams{ID: f.ID, MinutosInactividad: in.MinutosInactividad}); err != nil {
					return AjustesBandeja{}, err
				}
			}
			t.FijarEntidad("ajustes_bandeja", f.ID)
			campos := t.RegistrarCambios("ajustes_bandeja", f.ID, map[string]any{"minutos_inactividad": anterior}, map[string]any{"minutos_inactividad": f.MinutosInactividad})
			if campos == nil {
				campos = []string{}
			}
			return AjustesBandeja{MinutosInactividad: f.MinutosInactividad, Version: f.Version},
				t.Emitir("conversaciones.ajustes_cambiados", map[string]any{"empresa_id": f.EmpresaID, "campos": campos, "minutos_inactividad": f.MinutosInactividad})
		},
	})
}
