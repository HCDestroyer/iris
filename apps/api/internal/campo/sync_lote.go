package campo

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/inventario"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// POST /sync/lote (contratos/sync.md §2 y §3). Claves en camelCase, como el resto de la API.
//
// Cada operación corre en su propia transacción con el caso de uso de su acción (la misma que
// el panel: auditoría, eventos y reglas), así que un rechazo no deshace las anteriores. Se
// aplican en el orden recibido; un rechazo bloquea las siguientes de la misma orden, salvo
// orden_reasignada y orden_cancelada, que se evalúan operación por operación (tras una
// cancelación, las notas y los adjuntos siguen entrando como evidencia).

// OperacionLote es una operación del teléfono.
type OperacionLote struct {
	ID          string         `json:"id" format:"uuid" doc:"uuid v7 generado en el teléfono; idempotencia"`
	Tipo        string         `json:"tipo" enum:"orden.iniciar_camino,orden.llegar,checklist.marcar,nota.agregar,material.usar,equipo.instalar,equipo.retirar,firma.capturar,adjunto.registrar,orden.cerrar,orden.fallar,orden.pausar,orden.reanudar,orden.agregar_formulario,orden.quitar_formulario,traslado.recibir"`
	OrdenID     int64          `json:"ordenId,omitempty" minimum:"1" doc:"Obligatorio salvo en traslado.recibir (no es de una orden)"`
	VersionBase *int32         `json:"versionBase,omitempty" doc:"Versión de la orden que veía el teléfono (se guarda; los conflictos se resuelven por las reglas de §3, no por versión)"`
	OcurridoEn  *time.Time     `json:"ocurridoEn,omitempty" doc:"Hora del teléfono, con el mismo reloj que enviadoEn; el servidor la corrige con el desfase"`
	Datos       map[string]any `json:"datos,omitempty" doc:"Los mismos campos (camelCase) que el cuerpo de la acción del panel, sin version"`
}

// EntradaLote es el cuerpo de POST /sync/lote.
type EntradaLote struct {
	DispositivoID string          `json:"dispositivoId" minLength:"1" maxLength:"100"`
	Plataforma    string          `json:"plataforma,omitempty" maxLength:"40" doc:"pwa, android, ios…"`
	EnviadoEn     *time.Time      `json:"enviadoEn,omitempty" doc:"Hora del teléfono al enviar (el mismo reloj que ocurridoEn): desfase = hora del servidor − enviadoEn"`
	Cursor        string          `json:"cursor,omitempty" maxLength:"200" doc:"Cursor de cambios que tiene el teléfono (se devuelve igual)"`
	Operaciones   []OperacionLote `json:"operaciones" maxItems:"200"`
}

// EstadoOrden es el estado de la orden después de la operación (o el actual, en un rechazo).
type EstadoOrden struct {
	Version int32  `json:"version"`
	Estado  string `json:"estado" enum:"pendiente,programada,en_camino,en_sitio,pausada,cerrada,fallida,cancelada"`
}

// ResultadoOperacion es el resultado de una operación, en el mismo orden que llegaron.
type ResultadoOperacion struct {
	ID     string           `json:"id"`
	Estado string           `json:"estado" enum:"aplicada,duplicada,rechazada"`
	Error  *nucleo.Problema `json:"error,omitempty" doc:"problem+json del rechazo; el código va en type (urn:iris:problema:<codigo>)"`
	Aviso  *string          `json:"aviso,omitempty" enum:"checklist_no_vigente,existencia_insuficiente,version_anterior,pausa_ajustada"`
	Orden  *EstadoOrden     `json:"orden,omitempty" doc:"Versión y estado de la orden (también en los rechazos, si el técnico la ve o la tuvo)"`
	// Recepcion: traslado.recibir aplicada (contrato 0.5.0; I1).
	Recepcion *inventario.EstadoRecepcion `json:"recepcion,omitempty" doc:"traslado.recibir: la recepción encolada hacia Argos (su estado sigue en GET /campo/cambios, recepciones[])"`
}

// RespuestaLote es la respuesta de POST /sync/lote.
type RespuestaLote struct {
	Resultados []ResultadoOperacion `json:"resultados"`
	Cursor     string               `json:"cursor" doc:"Cursor de cambios para seguir con GET /campo/cambios: el que mandó el teléfono o, sin él, el de antes de aplicar el lote (nunca salta un cambio). No avanza con las operaciones del lote: sus efectos ya vienen en resultados[].orden"`
	ServidorEn time.Time            `json:"servidorEn"`
	DesfaseMs  int64                `json:"desfaseMs" doc:"Hora del servidor − enviadoEn (0 sin enviadoEn)"`
}

// EntradaDispositivo registra el teléfono al empezar un lote.
type EntradaDispositivo struct {
	DispositivoID string     `json:"dispositivoId"`
	Plataforma    string     `json:"plataforma"`
	EnviadoEn     *time.Time `json:"enviadoEn"`
	Operaciones   int        `json:"operaciones"`
	ids           []uuid.UUID
	ordenes       []int64
}

// InicioLote es lo que el lote necesita antes de aplicar.
type InicioLote struct {
	DesfaseMs int64
	Cursor    int64
	Previas   map[uuid.UUID]ResultadoOperacion
	Ordenes   map[int64]infoOrden // las del lote que el actor ve
}

// infoOrden: lo que el lote necesita para leer de Argos antes de aplicar.
type infoOrden struct {
	empresa  int64
	bodega   *int64
	servicio *int64
}

// EntradaRechazo guarda una operación rechazada.
type EntradaRechazo struct {
	Op      OpSync
	OrdenID int64
	Error   *nucleo.Problema
}

func (m *Modulo) nuevoRegistrarDispositivo() *auditoria.Caso[EntradaDispositivo, InicioLote] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDispositivo, InicioLote]{
		Accion:  "campo.sincronizar",
		Permiso: nucleo.PermCampoSincronizar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDispositivo) (InicioLote, error) {
			q := campobd.New(t)
			actor := contexto.ActorDe(ctx)
			if !actor.EsUsuario() {
				return InicioLote{}, nucleo.Prohibido("El lote de campo es de un usuario")
			}
			ahora := m.ahora()
			var desfase int64
			if in.EnviadoEn != nil {
				desfase = ahora.Sub(*in.EnviadoEn).Milliseconds()
			}
			t.FijarEntidad("dispositivo", in.DispositivoID)
			_, err := q.GuardarDispositivo(ctx, campobd.GuardarDispositivoParams{
				ID: in.DispositivoID, UsuarioID: actor.ID, Plataforma: in.Plataforma, UltimoContacto: ahora, DesfaseMs: desfase,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return InicioLote{}, nucleo.Conflicto("dispositivo_ajeno", "Ese dispositivoId está registrado a otro usuario; genera uno propio")
			}
			if err != nil {
				return InicioLote{}, err
			}
			t.Anotar(map[string]any{"desfase_ms": desfase})
			cursor, err := q.UltimoCambio(ctx, actor.ID)
			if err != nil {
				return InicioLote{}, err
			}
			inicio := InicioLote{DesfaseMs: desfase, Cursor: cursor, Previas: map[uuid.UUID]ResultadoOperacion{},
				Ordenes: map[int64]infoOrden{}}
			previas, err := q.OperacionesPorIDs(ctx, noNulo(in.ids))
			if err != nil {
				return InicioLote{}, err
			}
			for _, p := range previas {
				if p.Estado == "aplicada" && p.UsuarioID == actor.ID {
					inicio.Previas[p.ID] = resultadoGuardado(p)
				}
			}
			ordenes, err := q.OrdenesPorIDs(ctx, noNulo(in.ordenes))
			if err != nil {
				return InicioLote{}, err
			}
			for _, f := range ordenes {
				if enEmpresa(ctx, f) && visible(ctx, f) {
					inicio.Ordenes[f.ID] = infoOrden{empresa: deref(f.EmpresaID), bodega: f.BodegaID, servicio: f.ServicioID}
				}
			}
			return inicio, nil
		},
	})
}

func (m *Modulo) nuevoRegistrarRechazo() *auditoria.Caso[EntradaRechazo, *EstadoOrden] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRechazo, *EstadoOrden]{
		Accion:       "campo.rechazo",
		Permiso:      nucleo.PermCampoSincronizar,
		AuditarExito: soloLecturas[*EstadoOrden], // la acción rechazada ya auditó su error
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRechazo) (*EstadoOrden, error) {
			q := campobd.New(t)
			actor := contexto.ActorDe(ctx)
			var estado *EstadoOrden
			f, err := q.OrdenPorID(ctx, in.OrdenID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return nil, err
			case enEmpresa(ctx, f):
				tuvo := visible(ctx, f)
				if !tuvo {
					if tuvo, err = q.TuvoOrden(ctx, campobd.TuvoOrdenParams{TecnicoID: actor.ID, OrdenID: f.ID}); err != nil {
						return nil, err
					}
				}
				if tuvo {
					estado = &EstadoOrden{Version: f.Version, Estado: f.Estado}
				}
			}
			res := ResultadoOperacion{ID: in.Op.ID.String(), Estado: "rechazada", Error: in.Error, Orden: estado}
			crudo, err := json.Marshal(res)
			if err != nil {
				return nil, err
			}
			var ordenID *int64
			if in.OrdenID > 0 {
				ordenID = &in.OrdenID
			}
			return estado, q.GuardarRechazo(ctx, campobd.GuardarRechazoParams{
				ID: in.Op.ID, DispositivoID: in.Op.DispositivoID, UsuarioID: actor.ID, OrdenID: ordenID, Tipo: in.Op.Tipo,
				Resultado: crudo, VersionBase: in.Op.VersionBase, OcurridoEnTelefono: in.Op.OcurridoEnTelefono,
				OcurridoEn: &in.Op.OcurridoEn,
			})
		},
	})
}

// codigosNoBloqueantes: rechazos que se evalúan operación por operación.
var codigosNoBloqueantes = map[string]bool{"orden_reasignada": true, "orden_cancelada": true}

// codigosReintentables: el choque con otra transacción (409 concurrencia: deadlock o
// serialización que Postgres cortó) no es un rechazo de la operación. No se guarda (el mismo id
// se puede volver a mandar), y lo que sigue de esa orden en el lote tampoco se aplica (se
// responde igual): el teléfono las deja en su cola y reintenta con espera (contratos/sync.md §3).
var codigosReintentables = map[string]bool{"concurrencia": true}

// reintentable: la respuesta «vuelve a mandarla» de una operación, sin guardarla.
func reintentable(id string, p *nucleo.Problema) ResultadoOperacion {
	return ResultadoOperacion{ID: id, Estado: "rechazada", Error: p}
}

// Lote aplica un lote de operaciones del teléfono (canal campo).
func (m *Modulo) Lote(ctx context.Context, in EntradaLote) (RespuestaLote, error) {
	ctx = contexto.ConCanal(ctx, contexto.CanalCampo)
	if err := sinNulos(struct {
		DispositivoID string `json:"dispositivoId"`
		Plataforma    string `json:"plataforma"`
		Cursor        string `json:"cursor"`
	}{in.DispositivoID, in.Plataforma, in.Cursor}); err != nil {
		return RespuestaLote{}, err
	}
	ids := make([]uuid.UUID, len(in.Operaciones))
	ordenes := []int64{}
	for i, op := range in.Operaciones {
		id, err := uuid.Parse(op.ID)
		if err != nil {
			return RespuestaLote{}, campoInvalido("operaciones["+itoa(i)+"].id", "Debe ser un uuid", "uuid")
		}
		ids[i] = id
		if op.OrdenID > 0 {
			ordenes = append(ordenes, op.OrdenID)
		}
	}
	inicio, err := m.RegistrarDispositivo.Ejecutar(ctx, EntradaDispositivo{
		DispositivoID: in.DispositivoID, Plataforma: in.Plataforma, EnviadoEn: in.EnviadoEn,
		Operaciones: len(in.Operaciones), ids: ids, ordenes: ordenes,
	})
	if err != nil {
		return RespuestaLote{}, err
	}
	ahora := m.ahora()
	resp := RespuestaLote{Resultados: make([]ResultadoOperacion, 0, len(in.Operaciones)), ServidorEn: ahora,
		DesfaseMs: inicio.DesfaseMs, Cursor: in.Cursor}
	if resp.Cursor == "" {
		resp.Cursor = cursorDe(inicio.Cursor)
	}
	existencias, modalidades := m.existenciasDelLote(ctx, in.Operaciones, inicio.Ordenes)
	comodatos := m.comodatosDelLote(ctx, in.Operaciones, inicio.Ordenes)

	enEsteLote := map[uuid.UUID]ResultadoOperacion{}
	bloqueadas := map[int64]string{}        // orden → operación rechazada que la bloquea
	choques := map[int64]*nucleo.Problema{} // orden → concurrencia: lo que sigue se reintenta
	for i, op := range in.Operaciones {
		id := ids[i]
		if previa, ok := enEsteLote[id]; ok {
			// Repetida en el mismo lote: el mismo resultado; una rechazada sigue rechazada.
			if previa.Estado != "rechazada" {
				previa.Estado = "duplicada"
			}
			resp.Resultados = append(resp.Resultados, previa)
			continue
		}
		if previa, ok := inicio.Previas[id]; ok {
			previa.Estado = "duplicada"
			resp.Resultados = append(resp.Resultados, previa)
			enEsteLote[id] = previa
			continue
		}
		s := &OpSync{ID: id, Tipo: op.Tipo, DispositivoID: in.DispositivoID, VersionBase: op.VersionBase,
			OcurridoEnTelefono: op.OcurridoEn, OcurridoEn: corregir(op.OcurridoEn, inicio.DesfaseMs, ahora)}
		var res ResultadoOperacion
		// Grupo: la orden o, en traslado.recibir, el traslado (clave negativa: no choca con órdenes y
		// no bloquea ninguna orden).
		grupo := grupoDe(op)
		if choque, ok := choques[grupo]; ok {
			res = reintentable(op.ID, choque)
		} else if bloqueo, ok := bloqueadas[grupo]; ok {
			res = m.rechazar(ctx, s, op.OrdenID, nucleo.Conflicto("bloqueada_por_rechazo",
				"Una operación anterior de "+queBloquea(op)+" ("+bloqueo+") fue rechazada; resuélvela primero"))
		} else if op.Tipo == OpTrasladoRecibir {
			res = m.aplicarTraslado(ctx, s, op)
		} else if op.OrdenID <= 0 {
			res = m.rechazar(ctx, s, 0, nucleo.Validacion(nucleo.ErrorCampo{Field: "ordenId", Message: "Obligatorio en " + op.Tipo, Rule: "required"}))
		} else {
			s.Existencia, s.Modalidad, s.pre = existencias[i], modalidades[i], comodatos[i]
			res = m.aplicar(ctx, s, op)
		}
		if res.Estado == "rechazada" && res.Error != nil && codigosReintentables[res.Error.Codigo()] {
			if _, ya := choques[grupo]; !ya {
				choques[grupo] = res.Error
			}
		} else if res.Estado == "rechazada" && res.Error != nil && !codigosNoBloqueantes[res.Error.Codigo()] {
			if _, ya := bloqueadas[grupo]; !ya {
				bloqueadas[grupo] = op.ID
			}
		}
		enEsteLote[id] = res
		resp.Resultados = append(resp.Resultados, res)
	}
	return resp, nil
}

// antiguedadMaxima: una operación no puede fechar su hecho más atrás (la app caduca lo no
// sincronizado a los 7 días, contratos/sync.md §1).
const antiguedadMaxima = 7 * 24 * time.Hour

// corregir lleva la hora del teléfono al reloj del servidor (más el desfase medido en este
// lote), acotada a [ahora − 7 días, ahora]. Sin hora, la del servidor. La acción la acota
// además a la hora del estado de la orden (cuandoEn).
func corregir(t *time.Time, desfaseMs int64, ahora time.Time) time.Time {
	if t == nil {
		return ahora
	}
	c := t.Add(time.Duration(desfaseMs) * time.Millisecond).UTC().Truncate(time.Microsecond)
	if piso := ahora.Add(-antiguedadMaxima); c.Before(piso) {
		return piso
	}
	if c.After(ahora) {
		return ahora
	}
	return c
}

// aplicar corre la operación con el caso de uso de su acción.
func (m *Modulo) aplicar(ctx context.Context, s *OpSync, op OperacionLote) ResultadoOperacion {
	// NUL antes del caso de uso: Postgres no lo guarda (ni en la auditoría).
	if err := sinNulos(op.Datos); err != nil {
		p, _ := nucleo.AProblema(err)
		return m.rechazar(ctx, s, op.OrdenID, p)
	}
	o, err := m.despachar(ctx, s, op)
	var d *duplicada
	switch {
	case errors.As(err, &d):
		r := d.resultado
		r.Estado = "duplicada"
		return r
	case err != nil:
		p, _ := nucleo.AProblema(err)
		if codigosReintentables[p.Codigo()] {
			return reintentable(s.ID.String(), p)
		}
		return m.rechazar(ctx, s, op.OrdenID, p)
	}
	return ResultadoOperacion{ID: s.ID.String(), Estado: "aplicada", Aviso: o.Aviso,
		Orden: &EstadoOrden{Version: o.Version, Estado: o.Estado}}
}

// rechazar guarda el rechazo (se puede reevaluar con el mismo id) y lo devuelve con el estado
// actual de la orden.
func (m *Modulo) rechazar(ctx context.Context, s *OpSync, ordenID int64, p *nucleo.Problema) ResultadoOperacion {
	res := ResultadoOperacion{ID: s.ID.String(), Estado: "rechazada", Error: p}
	estado, err := m.RegistrarRechazo.Ejecutar(ctx, EntradaRechazo{Op: *s, OrdenID: ordenID, Error: p})
	if err == nil {
		res.Orden = estado
	}
	return res
}

// Esquemas de los datos de cada operación: los de la entrada de su acción (los del OpenAPI), para
// que el lote valide lo mismo que el panel (maxLength, minimum, enum, required, sin campos
// desconocidos).
var (
	esquemasUna   sync.Once
	registroDatos huma.Registry
	esquemasDatos map[reflect.Type]*huma.Schema
)

func esquemaDatos(t reflect.Type) (huma.Registry, *huma.Schema) {
	esquemasUna.Do(func() {
		registroDatos = huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
		esquemasDatos = map[reflect.Type]*huma.Schema{}
		for _, v := range []any{EntradaPosicion{}, EntradaMarcarChecklist{}, EntradaAgregarNota{}, EntradaUsarMaterial{},
			EntradaEquipo{}, EntradaCapturarFirma{}, EntradaRegistrarAdjunto{}, EntradaCerrarOrden{}, EntradaFallarOrden{},
			EntradaPausar{}, EntradaReanudar{}, EntradaAgregarFormularioCampo{}, EntradaQuitarFormularioCampo{},
			inventario.EntradaRecibirTraslado{}} {
			tt := reflect.TypeOf(v)
			esquemasDatos[tt] = registroDatos.Schema(tt, true, "")
		}
	})
	return registroDatos, esquemasDatos[t]
}

// datosDe valida los datos de la operación con el esquema de la entrada de su acción y los
// decodifica en ella. version no viaja en el lote (no se exige).
func datosDe[T any](op OperacionLote) (T, error) {
	var v T
	crudo, err := json.Marshal(op.Datos)
	if err != nil {
		return v, err
	}
	var datos map[string]any
	if err := json.Unmarshal(crudo, &datos); err != nil || datos == nil {
		datos = map[string]any{}
	}
	if _, ok := datos["version"]; !ok {
		if f, ok := reflect.TypeOf(v).FieldByName("Version"); ok && f.Type.Kind() == reflect.Int32 {
			datos["version"] = 1 // obligatoria en el panel; el lote no la usa
		}
	}
	reg, esquema := esquemaDatos(reflect.TypeOf(v))
	if esquema != nil {
		res := &huma.ValidateResult{}
		huma.Validate(reg, esquema, huma.NewPathBuffer([]byte(""), 0), huma.ModeWriteToServer, datos, res)
		if len(res.Errors) > 0 {
			errores := make([]nucleo.ErrorCampo, 0, len(res.Errors))
			for _, e := range res.Errors {
				campo, msj := "datos", e.Error()
				var d *huma.ErrorDetail
				if errors.As(e, &d) {
					campo, msj = "datos."+d.Location, d.Message
				}
				errores = append(errores, nucleo.ErrorCampo{Field: campo, Message: msj, Rule: "esquema"})
			}
			return v, nucleo.Validacion(errores...)
		}
	}
	crudo, _ = json.Marshal(datos)
	if err := json.Unmarshal(crudo, &v); err != nil {
		return v, campoInvalido("datos", "Datos inválidos para "+op.Tipo+": "+err.Error(), "datos")
	}
	return v, nil
}

// despachar traduce la operación a su caso de uso.
func (m *Modulo) despachar(ctx context.Context, s *OpSync, op OperacionLote) (Orden, error) {
	switch op.Tipo {
	case OpIniciarCamino, OpLlegar:
		e, err := datosDe[EntradaPosicion](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		if op.Tipo == OpLlegar {
			return m.Llegar.Ejecutar(ctx, e)
		}
		return m.IniciarCamino.Ejecutar(ctx, e)
	case OpChecklistMarcar:
		e, err := datosDe[EntradaMarcarChecklist](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Version, e.Sync = op.OrdenID, nil, s
		return m.MarcarChecklist.Ejecutar(ctx, e)
	case OpNotaAgregar:
		e, err := datosDe[EntradaAgregarNota](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Version, e.Sync = op.OrdenID, nil, s
		return m.AgregarNota.Ejecutar(ctx, e)
	case OpMaterialUsar:
		e, err := datosDe[EntradaUsarMaterial](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Version, e.Sync = op.OrdenID, nil, s
		return m.UsarMaterial.Ejecutar(ctx, e)
	case OpEquipoInstalar, OpEquipoRetirar:
		e, err := datosDe[EntradaEquipo](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Version, e.Sync = op.OrdenID, nil, s
		if op.Tipo == OpEquipoRetirar {
			e.pre = s.pre
			return m.RetirarEquipo.Ejecutar(ctx, e)
		}
		return m.InstalarEquipo.Ejecutar(ctx, e)
	case OpFirmaCapturar:
		e, err := datosDe[EntradaCapturarFirma](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Version, e.Sync = op.OrdenID, nil, s
		return m.CapturarFirma.Ejecutar(ctx, e)
	case OpAdjuntoRegistrar:
		e, err := datosDe[EntradaRegistrarAdjunto](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Version, e.Sync = op.OrdenID, nil, s
		return m.RegistrarAdjunto.Ejecutar(ctx, e)
	case OpCerrar:
		e, err := datosDe[EntradaCerrarOrden](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.CerrarOrden.Ejecutar(ctx, e)
	case OpFallar:
		e, err := datosDe[EntradaFallarOrden](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.FallarOrden.Ejecutar(ctx, e)
	case OpPausar:
		e, err := datosDe[EntradaPausar](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.PausarOrden.Ejecutar(ctx, e)
	case OpReanudar:
		e, err := datosDe[EntradaReanudar](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.ReanudarOrden.Ejecutar(ctx, e)
	case OpAgregarForm:
		e, err := datosDe[EntradaAgregarFormularioCampo](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.AgregarFormularioCampo.Ejecutar(ctx, e)
	case OpQuitarForm:
		e, err := datosDe[EntradaQuitarFormularioCampo](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.QuitarFormularioCampo.Ejecutar(ctx, e)
	}
	return Orden{}, campoInvalido("tipo", "Tipo de operación desconocido", "enum")
}

// existenciasDelLote lee de Argos, fuera de toda transacción, la existencia de cada producto
// que usan las operaciones material.usar (una lectura por bodega) y su modalidad (Argos ≥ 1.6.0).
// nil / vacía = no se sabe.
func (m *Modulo) existenciasDelLote(ctx context.Context, ops []OperacionLote, ordenes map[int64]infoOrden) ([]*float64, []string) {
	salida := make([]*float64, len(ops))
	modalidades := make([]string, len(ops))
	if m.cfg.Inventario == nil {
		return salida, modalidades
	}
	leidas := map[[2]int64]map[int64]float64{}
	modalidadDe := map[[2]int64]map[int64]string{}
	for i, op := range ops {
		if op.Tipo != OpMaterialUsar {
			continue
		}
		e, err := datosDe[EntradaUsarMaterial](op)
		if err != nil {
			continue
		}
		info, ok := ordenes[op.OrdenID]
		if !ok || info.bodega == nil {
			continue // la orden no es visible o no tiene bodega (siempre la de la orden)
		}
		eb := [2]int64{info.empresa, *info.bodega}
		lineas, ok := leidas[eb]
		if !ok {
			lista, err := m.cfg.Inventario.Existencias(ctx, eb[0], eb[1])
			if err != nil {
				leidas[eb] = nil
				continue
			}
			lineas = map[int64]float64{}
			modalidadDe[eb] = map[int64]string{}
			for _, l := range lista {
				lineas[l.ProductoID] += l.Cantidad
				if l.Modalidad != "" {
					modalidadDe[eb][l.ProductoID] = l.Modalidad
				}
			}
			leidas[eb] = lineas
		}
		if lineas == nil {
			continue
		}
		cantidad := lineas[e.ProductoID]
		salida[i] = &cantidad
		modalidades[i] = modalidadDe[eb][e.ProductoID]
	}
	return salida, modalidades
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// comodatosDelLote lee de Argos, fuera de toda transacción, el comodato del servicio de cada
// orden con equipo.retirar (una lectura por servicio). nil = no se sabe (sin conector o caído).
func (m *Modulo) comodatosDelLote(ctx context.Context, ops []OperacionLote, ordenes map[int64]infoOrden) []*prelectura {
	salida := make([]*prelectura, len(ops))
	leidos := map[int64]*prelectura{}
	for i, op := range ops {
		info, ok := ordenes[op.OrdenID]
		if op.Tipo != OpEquipoRetirar || !ok || info.servicio == nil {
			continue
		}
		p, ya := leidos[*info.servicio]
		if !ya {
			p = m.comodatoDe(ctx, info.empresa, info.servicio)
			leidos[*info.servicio] = p
		}
		salida[i] = p
	}
	return salida
}
