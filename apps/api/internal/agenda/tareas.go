package agenda

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Tareas propias del técnico: algo suyo con hora y duración que no es una orden (recoger material,
// revisar una caja…), opcionalmente ligado a una orden o a un cliente de Argos. El técnico las
// agenda desde la app de campo, también sin señal (operación tarea.crear del lote, con el id de la
// operación como id de la tarea); el supervisor y despacho las ven en GET /agenda/tareas.
//
// Las tres acciones (crear, completar, anular) corren aquí, dentro de la transacción del caso de
// uso que las llama: el de la API (CrearTarea, CompletarTarea, AnularTarea) o el del lote de campo
// (que además reclama la operación). Una tarea que choca con una cita del técnico no se bloquea:
// la respuesta trae solapaCon para que la app lo muestre.

// Estados de una tarea.
const (
	TareaPendiente  = "pendiente"
	TareaCompletada = "completada"
	TareaAnulada    = "anulada"
)

// Avisos de completar y anular cuando la tarea ya estaba así (otro teléfono o el supervisor).
const (
	AvisoTareaYaCompletada = "tarea_ya_completada"
	AvisoTareaYaAnulada    = "tarea_ya_anulada"
)

// Límites de una tarea (espejo en @iris/sync, esquemaTarea).
const (
	TareaQueMax         = 200
	TareaDuracionMin    = 5
	TareaDuracionMax    = 480
	tareaAntiguedadMax  = 7 * 24 * time.Hour
	tareaAnticipacionMx = 366 * 24 * time.Hour
)

// Tarea es una tarea propia de un técnico.
type Tarea struct {
	ID           uuid.UUID  `json:"id" format:"uuid" doc:"UUID (v7) generado en el cliente"`
	EmpresaID    *int64     `json:"empresaId" nullable:"true"`
	TecnicoID    int64      `json:"tecnicoId" doc:"Usuario técnico dueño de la tarea"`
	CreadaPor    *int64     `json:"creadaPor" nullable:"true"`
	Que          string     `json:"que"`
	Inicio       time.Time  `json:"inicio"`
	Fin          time.Time  `json:"fin"`
	DuracionMin  int32      `json:"duracionMin"`
	OrdenID      *int64     `json:"ordenId" nullable:"true"`
	ClienteID    *int64     `json:"clienteId" nullable:"true"`
	Origen       string     `json:"origen" enum:"panel,campo,api"`
	Estado       string     `json:"estado" enum:"pendiente,completada,anulada"`
	CompletadaEn *time.Time `json:"completadaEn" nullable:"true"`
	AnuladaEn    *time.Time `json:"anuladaEn" nullable:"true"`
	Motivo       *string    `json:"motivo" nullable:"true" doc:"Por qué se anuló"`
	Version      int32      `json:"version"`
	CreadoEn     time.Time  `json:"creadoEn"`
}

// TareaAgenda es una tarea en la lista de agenda (con el nombre del técnico).
type TareaAgenda struct {
	Tarea
	TecnicoNombre string `json:"tecnicoNombre"`
}

func aTarea(f agendabd.AgendaTarea) Tarea {
	return Tarea{ID: f.ID, EmpresaID: f.EmpresaID, TecnicoID: f.TecnicoID, CreadaPor: f.CreadaPor, Que: f.Que, Inicio: f.Inicio,
		Fin: f.Fin, DuracionMin: f.DuracionMin, OrdenID: f.OrdenID, ClienteID: f.ClienteID, Origen: f.Origen, Estado: f.Estado,
		CompletadaEn: f.CompletadaEn, AnuladaEn: f.AnuladaEn, Motivo: f.Motivo, Version: f.Version, CreadoEn: f.CreatedAt}
}

// DatosTarea son los datos de una tarea (los mismos en la API y en la operación tarea.crear).
type DatosTarea struct {
	Que         string    `json:"que" minLength:"1" maxLength:"200" doc:"Qué vas a hacer"`
	Inicio      time.Time `json:"inicio" doc:"Cuándo (UTC); hasta 7 días atrás y 366 adelante"`
	DuracionMin int32     `json:"duracionMin" minimum:"5" maximum:"480" doc:"Minutos (la app ofrece 15, 30, 60 y 120)"`
	OrdenID     *int64    `json:"ordenId,omitempty" minimum:"1" doc:"Orden ligada (debes verla)"`
	ClienteID   *int64    `json:"clienteId,omitempty" minimum:"1" doc:"Cliente de Argos ligado (de la instantánea de la empresa); con ordenId, debe ser el de la orden"`
}

// EntradaCrearTarea crea una tarea propia (o de un técnico que supervisas).
type EntradaCrearTarea struct {
	ID        uuid.UUID `json:"id" format:"uuid" doc:"UUID (v7) generado en el cliente; la creación es idempotente por él"`
	TecnicoID *int64    `json:"tecnicoId,omitempty" minimum:"1" doc:"Usuario técnico; ausente = tú. Para otro hace falta agenda.ver con alcance sobre su cuadrilla"`
	DatosTarea
	// Origen: campo desde el lote; panel o api desde la API (según el canal).
	Origen string `json:"-"`
}

// RespuestaTarea es la tarea y las citas del técnico con que se cruza (no bloquea).
type RespuestaTarea struct {
	Tarea     Tarea             `json:"tarea"`
	SolapaCon []CitaEnConflicto `json:"solapaCon" doc:"Citas vivas del técnico que se cruzan con la tarea: la tarea se guardó igual; la app lo avisa"`
	// Aviso: tarea_ya_completada o tarea_ya_anulada (la acción ya estaba hecha: no cambió nada).
	Aviso *string `json:"aviso,omitempty" enum:"tarea_ya_completada,tarea_ya_anulada"`
	// Duplicada: el id ya era de esta tarea (reintento): no se volvió a crear.
	Duplicada bool `json:"duplicada,omitempty" doc:"El id ya era de esta tarea (reintento): se devuelve tal como está"`
}

// EntradaCompletarTarea marca la tarea como hecha.
type EntradaCompletarTarea struct {
	ID      uuid.UUID `json:"-"`
	Version int32     `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	// SinVersion: desde el lote (los conflictos se resuelven por la regla de contratos/sync.md §3).
	SinVersion bool      `json:"-"`
	En         time.Time `json:"-"`
}

// EntradaAnularTarea anula la tarea (queda en el historial).
type EntradaAnularTarea struct {
	ID         uuid.UUID `json:"-"`
	Version    int32     `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo     *string   `json:"motivo,omitempty" maxLength:"500"`
	SinVersion bool      `json:"-"`
	En         time.Time `json:"-"`
}

// EntradaListarTareas: tareas de los técnicos visibles en un rango.
type EntradaListarTareas struct {
	Desde           string `query:"desde" required:"true" format:"date-time" doc:"UTC (RFC 3339)"`
	Hasta           string `query:"hasta" required:"true" format:"date-time" doc:"UTC; hasta 31 días después de desde"`
	TecnicoID       int64  `query:"tecnicoId" minimum:"0" doc:"Usuario técnico"`
	IncluirAnuladas bool   `query:"incluirAnuladas" doc:"También las anuladas"`
	nucleo.ParamsPagina
}

// OrdenParaTarea es la orden ligada a una tarea, como la ve el actor.
type OrdenParaTarea struct {
	ID        int64
	Numero    string
	EmpresaID *int64
	ClienteID *int64
}

// OrdenesTareas valida, dentro de la transacción, la orden ligada a una tarea: existe, es de la
// empresa activa y el actor la ve (mismas reglas que GET /ordenes/{id}). Si no, 422 en ordenId.
// Lo implementa campo (agenda no importa campo); sin él, una tarea no se liga a órdenes.
type OrdenesTareas interface {
	OrdenParaTarea(ctx context.Context, t *auditoria.Tx, ordenID int64) (OrdenParaTarea, error)
}

// UsarOrdenes conecta la validación de órdenes de campo (campo.Nuevo la llama).
func (m *Modulo) UsarOrdenes(o OrdenesTareas) { m.ordenes = o }

func tareaNoEncontrada() *nucleo.Problema { return nucleo.NoEncontrado("La tarea no existe") }

func (m *Modulo) nuevoCrearTarea() *auditoria.Caso[EntradaCrearTarea, RespuestaTarea] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearTarea, RespuestaTarea]{
		Accion: "agenda.crear_tarea", Permiso: nucleo.PermAgendaCrearTarea,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearTarea) (RespuestaTarea, error) {
			if in.Origen == "" {
				in.Origen = "api"
				if contexto.CanalDe(ctx) == contexto.CanalPanel {
					in.Origen = "panel"
				}
			}
			return m.CrearTareaEnTx(ctx, t, in)
		},
	})
}

// CrearTareaEnTx crea la tarea en la transacción del caso de uso que llama (API o lote).
func (m *Modulo) CrearTareaEnTx(ctx context.Context, t *auditoria.Tx, in EntradaCrearTarea) (RespuestaTarea, error) {
	q := agendabd.New(t)
	actor := contexto.ActorDe(ctx)
	t.FijarEntidad("tarea", in.ID.String())
	if in.ID == uuid.Nil {
		return RespuestaTarea{}, campoObligatorio("id")
	}
	que, err := texto("que", in.Que, 1, TareaQueMax)
	if err != nil {
		return RespuestaTarea{}, err
	}
	if in.DuracionMin < TareaDuracionMin || in.DuracionMin > TareaDuracionMax {
		return RespuestaTarea{}, campoInvalido("duracionMin", fmt.Sprintf("Entre %d y %d minutos", TareaDuracionMin, TareaDuracionMax), "rango")
	}
	ahora := m.ahora()
	inicio := in.Inicio.UTC().Truncate(time.Microsecond)
	if inicio.Before(ahora.Add(-tareaAntiguedadMax)) || inicio.After(ahora.Add(tareaAnticipacionMx)) {
		return RespuestaTarea{}, campoInvalido("inicio", "Entre 7 días atrás y 366 días adelante", "rango")
	}
	fin := inicio.Add(time.Duration(in.DuracionMin) * time.Minute)
	empresa, err := empresaDeAlta(ctx)
	if err != nil {
		return RespuestaTarea{}, err
	}
	// De quién es: el actor o un técnico que supervisa.
	var tecnico int64
	switch {
	case in.TecnicoID == nil || (actor.EsUsuario() && *in.TecnicoID == actor.ID):
		if !actor.EsUsuario() {
			return RespuestaTarea{}, campoObligatorio("tecnicoId")
		}
		tecnico = actor.ID
	default:
		tecnico = *in.TecnicoID
		if err := m.exigirTecnicoDeTarea(ctx, t, q, tecnico); err != nil {
			return RespuestaTarea{}, err
		}
	}
	if empresa != nil {
		t.FijarEmpresa(*empresa)
	}
	// Orden o cliente ligados.
	cliente := in.ClienteID
	if in.OrdenID != nil {
		if m.ordenes == nil {
			return RespuestaTarea{}, campoInvalido("ordenId", "Las tareas no se pueden ligar a órdenes aquí", "orden")
		}
		o, err := m.ordenes.OrdenParaTarea(ctx, t, *in.OrdenID)
		if err != nil {
			return RespuestaTarea{}, err
		}
		if cliente != nil && !mismoID(cliente, o.ClienteID) {
			return RespuestaTarea{}, campoInvalido("clienteId", "No es el cliente de la orden", "cliente")
		}
		cliente = o.ClienteID
	} else if cliente != nil {
		if empresa == nil {
			return RespuestaTarea{}, campoInvalido("clienteId", "Sin conector con Argos no hay clientes que ligar", "cliente")
		}
		ok, err := q.ClienteArgosEnEmpresa(ctx, agendabd.ClienteArgosEnEmpresaParams{ID: *cliente, EmpresaID: *empresa})
		if err != nil {
			return RespuestaTarea{}, err
		}
		if !ok {
			return RespuestaTarea{}, campoInvalido("clienteId", "El cliente no está en la instantánea de Argos de la empresa", "cliente")
		}
	}
	f, err := q.CrearTarea(ctx, agendabd.CrearTareaParams{ID: in.ID, EmpresaID: empresa, TecnicoID: tecnico, CreadaPor: actorUsuario(t),
		Que: que, Inicio: inicio, DuracionMin: in.DuracionMin, Fin: fin, OrdenID: in.OrdenID, ClienteID: cliente, Origen: in.Origen})
	if errors.Is(err, pgx.ErrNoRows) {
		// El id ya existe: un reintento del mismo autor devuelve la tarea; otro, 409.
		previa, err := q.TareaPorID(ctx, in.ID)
		if err != nil {
			return RespuestaTarea{}, err
		}
		if !mismoID(previa.CreadaPor, actorUsuario(t)) || previa.TecnicoID != tecnico {
			return RespuestaTarea{}, nucleo.Conflicto("tarea_ajena", "Ese id ya es de otra tarea; genera uno nuevo")
		}
		r, err := m.respuestaTarea(ctx, q, previa)
		r.Duplicada = true
		return r, err
	}
	if err != nil {
		return RespuestaTarea{}, err
	}
	t.RegistrarCambios("tarea", f.ID.String(), nil, f)
	r, err := m.respuestaTarea(ctx, q, f)
	if err != nil {
		return r, err
	}
	if len(r.SolapaCon) > 0 {
		t.Anotar(map[string]any{"solapa_con": len(r.SolapaCon)})
	}
	return r, t.Emitir("tarea.creada", map[string]any{"tarea_id": f.ID, "empresa_id": f.EmpresaID, "tecnico_id": f.TecnicoID,
		"creada_por": f.CreadaPor, "inicio": f.Inicio, "fin": f.Fin, "duracion_min": f.DuracionMin, "orden_id": f.OrdenID,
		"cliente_id": f.ClienteID, "origen": f.Origen, "solapa_con": len(r.SolapaCon)})
}

// respuestaTarea arma la respuesta con las citas vivas del técnico que se cruzan (de otra empresa,
// solo el horario).
func (m *Modulo) respuestaTarea(ctx context.Context, q *agendabd.Queries, f agendabd.AgendaTarea) (RespuestaTarea, error) {
	salida := RespuestaTarea{Tarea: aTarea(f), SolapaCon: []CitaEnConflicto{}}
	if f.Estado == TareaAnulada {
		return salida, nil
	}
	choques, err := q.Solapes(ctx, agendabd.SolapesParams{TecnicoID: f.TecnicoID, OrdenID: deref(f.OrdenID), Inicio: f.Inicio, Fin: f.Fin})
	if err != nil {
		return salida, err
	}
	todas, empresa := filtroEmpresa(ctx)
	for _, c := range choques {
		cc := CitaEnConflicto{OrdenID: c.OrdenID, Numero: c.Numero, Inicio: c.Inicio, Fin: c.Fin}
		if !todas && !mismoID(c.EmpresaID, empresa) {
			cc.OrdenID, cc.Numero = 0, ""
		}
		salida.SolapaCon = append(salida.SolapaCon, cc)
	}
	return salida, nil
}

// exigirTecnicoDeTarea: crear una tarea para otro exige que sea una persona activa de la empresa
// activa y que agenda.ver y agenda.crear_tarea cubran su cuadrilla (por su ficha; sin ficha, solo
// con alcance global).
func (m *Modulo) exigirTecnicoDeTarea(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, tecnico int64) error {
	invalido := campoInvalido("tecnicoId", "El técnico no existe o no lo supervisas", "tecnico")
	u, err := q.UsuarioParaTecnico(ctx, tecnico)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!u.Activo || u.Tipo != "persona")) {
		return invalido
	}
	if err != nil {
		return err
	}
	if todas, empresa := filtroEmpresa(ctx); !todas {
		en, err := identidad.UsuarioEnEmpresa(ctx, t, tecnico, deref(empresa))
		if err != nil {
			return err
		}
		if !en {
			return invalido
		}
	}
	cuadrilla, err := cuadrillaDeUsuario(ctx, q, tecnico)
	if err != nil {
		return err
	}
	if ok, err := alcanceDe(ctx, nucleo.PermAgendaVer).cubreCuadrilla(ctx, q, cuadrilla); err != nil || !ok {
		if err != nil {
			return err
		}
		return invalido
	}
	if ok, err := alcanceDe(ctx, nucleo.PermAgendaCrearTarea).cubreCuadrilla(ctx, q, cuadrilla); err != nil || !ok {
		if err != nil {
			return err
		}
		return fueraDeAlcance("Tu permiso agenda.crear_tarea no cubre la cuadrilla de este técnico")
	}
	return nil
}

func cuadrillaDeUsuario(ctx context.Context, q *agendabd.Queries, usuario int64) (*int64, error) {
	f, err := q.TecnicoPorUsuario(ctx, usuario)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f.CuadrillaID, nil
}

// tomarTarea lee la tarea con candado: de la empresa activa y del actor (su dueño) o de un técnico
// que supervisa (agenda.ver y el permiso de la acción sobre su cuadrilla). Si no la ve, 404.
func (m *Modulo) tomarTarea(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, id uuid.UUID, permiso nucleo.Permiso, version *int32) (agendabd.AgendaTarea, error) {
	t.FijarEntidad("tarea", id.String())
	f, err := q.TareaPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, tareaNoEncontrada()
	}
	if err != nil {
		return f, err
	}
	todas, empresa := filtroEmpresa(ctx)
	if !todas && !mismoID(f.EmpresaID, empresa) {
		return f, tareaNoEncontrada()
	}
	actor := contexto.ActorDe(ctx)
	if (!actor.EsUsuario() || actor.ID != f.TecnicoID) && !esSistema(ctx) {
		cuadrilla, err := cuadrillaDeUsuario(ctx, q, f.TecnicoID)
		if err != nil {
			return f, err
		}
		if ok, err := alcanceDe(ctx, nucleo.PermAgendaVer).cubreCuadrilla(ctx, q, cuadrilla); err != nil || !ok {
			if err != nil {
				return f, err
			}
			return f, tareaNoEncontrada()
		}
		if ok, err := alcanceDe(ctx, permiso).cubreCuadrilla(ctx, q, cuadrilla); err != nil || !ok {
			if err != nil {
				return f, err
			}
			return f, fueraDeAlcance(fmt.Sprintf("Tu permiso %s no cubre la cuadrilla de este técnico", permiso))
		}
	}
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	if version != nil {
		if err := nucleo.ExigirVersion(*version, f.Version, "tarea", id.String()); err != nil {
			return f, err
		}
	}
	return f, nil
}

func versionDe(v int32, sin bool) *int32 {
	if sin {
		return nil
	}
	return &v
}

func (m *Modulo) nuevoCompletarTarea() *auditoria.Caso[EntradaCompletarTarea, RespuestaTarea] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCompletarTarea, RespuestaTarea]{
		Accion: "agenda.completar_tarea", Permiso: nucleo.PermAgendaCompletarTarea,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCompletarTarea) (RespuestaTarea, error) {
			return m.CompletarTareaEnTx(ctx, t, in)
		},
	})
}

// CompletarTareaEnTx marca la tarea como hecha en la transacción del caso de uso que llama.
// Ya completada → sin cambios y aviso tarea_ya_completada; anulada → 409 tarea_anulada.
func (m *Modulo) CompletarTareaEnTx(ctx context.Context, t *auditoria.Tx, in EntradaCompletarTarea) (RespuestaTarea, error) {
	q := agendabd.New(t)
	antes, err := m.tomarTarea(ctx, t, q, in.ID, nucleo.PermAgendaCompletarTarea, versionDe(in.Version, in.SinVersion))
	if err != nil {
		return RespuestaTarea{}, err
	}
	switch antes.Estado {
	case TareaCompletada:
		r, err := m.respuestaTarea(ctx, q, antes)
		r.Aviso = ptr(AvisoTareaYaCompletada)
		return r, err
	case TareaAnulada:
		return RespuestaTarea{}, nucleo.Conflicto("tarea_anulada", "La tarea está anulada; no se puede marcar como hecha")
	}
	cuando := m.cuandoTarea(in.En, antes)
	f, err := q.CompletarTarea(ctx, agendabd.CompletarTareaParams{ID: antes.ID, Version: antes.Version, CompletadaEn: &cuando, CompletadaPor: actorUsuario(t)})
	if err != nil {
		return RespuestaTarea{}, nucleo.VerificarVersion(err, "tarea", antes.ID.String())
	}
	t.RegistrarCambios("tarea", f.ID.String(), antes, f)
	r, err := m.respuestaTarea(ctx, q, f)
	if err != nil {
		return r, err
	}
	return r, t.Emitir("tarea.completada", map[string]any{"tarea_id": f.ID, "empresa_id": f.EmpresaID, "tecnico_id": f.TecnicoID,
		"completada_por": f.CompletadaPor, "completada_en": f.CompletadaEn})
}

func (m *Modulo) nuevoAnularTarea() *auditoria.Caso[EntradaAnularTarea, RespuestaTarea] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAnularTarea, RespuestaTarea]{
		Accion: "agenda.anular_tarea", Permiso: nucleo.PermAgendaAnularTarea,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAnularTarea) (RespuestaTarea, error) {
			return m.AnularTareaEnTx(ctx, t, in)
		},
	})
}

// AnularTareaEnTx anula la tarea (borrado lógico) en la transacción del caso de uso que llama.
// Ya anulada → sin cambios y aviso tarea_ya_anulada; completada → 409 tarea_completada.
func (m *Modulo) AnularTareaEnTx(ctx context.Context, t *auditoria.Tx, in EntradaAnularTarea) (RespuestaTarea, error) {
	q := agendabd.New(t)
	antes, err := m.tomarTarea(ctx, t, q, in.ID, nucleo.PermAgendaAnularTarea, versionDe(in.Version, in.SinVersion))
	if err != nil {
		return RespuestaTarea{}, err
	}
	var motivo *string
	if in.Motivo != nil {
		v, err := texto("motivo", *in.Motivo, 0, 500)
		if err != nil {
			return RespuestaTarea{}, err
		}
		if v != "" {
			motivo = &v
		}
	}
	switch antes.Estado {
	case TareaAnulada:
		r, err := m.respuestaTarea(ctx, q, antes)
		r.Aviso = ptr(AvisoTareaYaAnulada)
		return r, err
	case TareaCompletada:
		return RespuestaTarea{}, nucleo.Conflicto("tarea_completada", "La tarea ya está hecha; no se puede anular")
	}
	cuando := m.cuandoTarea(in.En, antes)
	f, err := q.AnularTarea(ctx, agendabd.AnularTareaParams{ID: antes.ID, Version: antes.Version, AnuladaEn: &cuando, AnuladaPor: actorUsuario(t), Motivo: motivo})
	if err != nil {
		return RespuestaTarea{}, nucleo.VerificarVersion(err, "tarea", antes.ID.String())
	}
	t.RegistrarCambios("tarea", f.ID.String(), antes, f)
	return RespuestaTarea{Tarea: aTarea(f), SolapaCon: []CitaEnConflicto{}}, t.Emitir("tarea.anulada", map[string]any{"tarea_id": f.ID,
		"empresa_id": f.EmpresaID, "tecnico_id": f.TecnicoID, "anulada_por": f.AnuladaPor, "motivo": f.Motivo})
}

// cuandoTarea: la hora del hecho (la del teléfono corregida, desde el lote) o la del servidor;
// nunca antes de crear la tarea ni después de ahora.
func (m *Modulo) cuandoTarea(en time.Time, f agendabd.AgendaTarea) time.Time {
	ahora := m.ahora()
	if en.IsZero() || en.After(ahora) {
		return ahora
	}
	en = en.UTC().Truncate(time.Microsecond)
	if en.Before(f.CreatedAt) {
		return f.CreatedAt.UTC().Truncate(time.Microsecond)
	}
	return en
}

func (m *Modulo) nuevoListarTareas() *auditoria.Caso[EntradaListarTareas, nucleo.Pagina[TareaAgenda]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarTareas, nucleo.Pagina[TareaAgenda]]{
		Accion: "agenda.ver", Permiso: nucleo.PermAgendaVer, AuditarExito: soloLecturas[nucleo.Pagina[TareaAgenda]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarTareas) (nucleo.Pagina[TareaAgenda], error) {
			vacia := nucleo.Pagina[TareaAgenda]{Data: []TareaAgenda{}}
			desde, err := instante("desde", in.Desde)
			if err != nil {
				return vacia, err
			}
			hasta, err := instante("hasta", in.Hasta)
			if err != nil {
				return vacia, err
			}
			if !hasta.After(desde) || hasta.Sub(desde) > 31*24*time.Hour {
				return vacia, campoInvalido("hasta", "hasta debe ser posterior a desde y como mucho 31 días después", "rango")
			}
			var cursorInicio time.Time
			var cursorID uuid.UUID
			conCursor, err := nucleo.DecodificarCursor(in.Cursor, &cursorInicio, &cursorID)
			if err != nil {
				return vacia, err
			}
			q := agendabd.New(t)
			todas, empresa := filtroEmpresa(ctx)
			p := agendabd.ListarTareasParams{TodasEmpresas: todas, EmpresaID: empresa, Desde: desde, Hasta: hasta,
				IncluirAnuladas: in.IncluirAnuladas, ConCursor: conCursor, CursorInicio: cursorInicio, CursorID: cursorID,
				Tecnicos: []int64{}, Limite: int32(in.Limite() + 1)} //nolint:gosec // ≤ 201
			a := alcanceDe(ctx, nucleo.PermAgendaVer)
			switch {
			case a.global && in.TecnicoID == 0:
				p.TodosTecnicos = true
			case a.global:
				p.Tecnicos = []int64{in.TecnicoID}
			default:
				// Con alcance: los técnicos de las cuadrillas que cubre (por su ficha).
				pt := m.paramsTecnicos(ctx, nucleo.PermAgendaVer)
				pt.UsuarioID = filtro(in.TecnicoID)
				tecs, err := q.ListarTecnicos(ctx, pt)
				if err != nil {
					return vacia, err
				}
				for _, tc := range tecs {
					p.Tecnicos = append(p.Tecnicos, tc.UsuarioID)
				}
			}
			filas, err := q.ListarTareas(ctx, p)
			if err != nil {
				return vacia, err
			}
			ids := []int64{}
			for _, f := range filas {
				ids = append(ids, f.TecnicoID)
			}
			nombres := map[int64]string{}
			us, err := q.UsuariosPorIDs(ctx, ids)
			if err != nil {
				return vacia, err
			}
			for _, u := range us {
				nombres[u.ID] = u.Nombre
			}
			return nucleo.Paginar(filas, in.Limite(), func(f agendabd.AgendaTarea) TareaAgenda {
				return TareaAgenda{Tarea: aTarea(f), TecnicoNombre: nombres[f.TecnicoID]}
			}, func(f agendabd.AgendaTarea) []any { return []any{f.Inicio, f.ID} }), nil
		},
	})
}

// TareasDelTecnico: las tareas vivas del técnico en la empresa activa que empiezan antes de hasta
// (desde `desde` y las pendientes atrasadas), para el paquete y los cambios de la app de campo.
func (m *Modulo) TareasDelTecnico(ctx context.Context, t *auditoria.Tx, tecnico int64, desde, hasta time.Time) ([]Tarea, error) {
	todas, empresa := filtroEmpresa(ctx)
	filas, err := agendabd.New(t).TareasDelTecnico(ctx, agendabd.TareasDelTecnicoParams{TecnicoID: tecnico, TodasEmpresas: todas,
		EmpresaID: empresa, Desde: desde.UTC(), Hasta: hasta.UTC()})
	if err != nil {
		return nil, err
	}
	salida := make([]Tarea, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, aTarea(f))
	}
	return salida, nil
}

// TareaOrdenInvalida: la orden ligada no existe o no la ves (422, sin revelar cuál).
func TareaOrdenInvalida() *nucleo.Problema {
	return campoInvalido("ordenId", "La orden no existe o no la ves", "orden")
}
