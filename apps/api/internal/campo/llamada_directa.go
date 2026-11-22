package campo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// llamada.directa desde el lote (contratos/sync.md §2; voz.md §3.12, E4c): «Llamar desde mi celular»
// sin señal. El teléfono abre `tel:` al momento con el número de la orden guardada y encola la
// operación; al volver la señal el servidor anota la llamada en la orden con la hora real. La fila y
// el evento (`llamada.directa_registrada`) son de la mensajería (LlamadasDirectas); aquí se reclama
// la operación (idempotencia por su id, que es también el id de la llamada), se verifica que la
// orden sea del técnico y que la hora quepa, y se guarda el resultado, en la misma transacción. No
// cambia la orden (ni su versión).

// OpLlamadaDirecta es el tipo de la operación.
const OpLlamadaDirecta = "llamada.directa"

// toleranciaHoraLlamada: la hora de la llamada puede estar hasta 5 min en el futuro o antes de la
// asignación (relojes de teléfono), como recibidoEn de traslado.recibir.
const toleranciaHoraLlamada = 5 * time.Minute

// DatosLlamadaDirectaLote son los datos de llamada.directa.
type DatosLlamadaDirectaLote struct {
	Telefono *string `json:"telefono,omitempty" enum:"contacto,movil,principal" doc:"El número de la orden que eligió el técnico (de fábrica, el primero que exista: contacto en sitio, móvil, principal). El número lo pone el servidor, nunca el teléfono"`
}

// EntradaLlamadaDirectaSync es lo que la mensajería necesita para anotar la llamada.
type EntradaLlamadaDirectaSync struct {
	// ID: el de la operación (uuid v7 del teléfono), que es también el de la llamada.
	ID        uuid.UUID
	Orden     campobd.CampoOrden
	Telefono  *string
	UsuarioID int64
	// En: la hora de la llamada, corregida con el desfase del teléfono.
	En time.Time
}

// LlamadasDirectas anota una llamada directa en la transacción de la operación (la mensajería:
// mensajeria.LlamadasDirectas).
type LlamadasDirectas interface {
	RegistrarDirectaEnTx(ctx context.Context, t *auditoria.Tx, in EntradaLlamadaDirectaSync) (uuid.UUID, error)
}

type entradaLlamadaDirectaLote struct {
	sync    *OpSync
	ordenID int64
	datos   DatosLlamadaDirectaLote
	// enTelefono: la hora del teléfono corregida con el desfase, sin acotar (para validarla).
	enTelefono *time.Time
}

func (m *Modulo) nuevoLlamadaDirectaLote() *auditoria.Caso[entradaLlamadaDirectaLote, ResultadoOperacion] {
	return auditoria.Nuevo(auditoria.Caso[entradaLlamadaDirectaLote, ResultadoOperacion]{
		Accion: "telefono.llamada_directa", Permiso: nucleo.PermCentralitaLlamar,
		AuditarError: func(err error) bool {
			var d *duplicada
			return !errors.As(err, &d)
		},
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaLlamadaDirectaLote) (ResultadoOperacion, error) {
			if m.cfg.LlamadasDirectas == nil {
				return ResultadoOperacion{}, nucleo.NuevoProblema(http.StatusServiceUnavailable, "no_disponible", "No disponible",
					"Las llamadas directas no están conectadas en este servidor")
			}
			q := campobd.New(t)
			t.FijarEntidad("orden", in.ordenID)
			f, err := q.OrdenPorID(ctx, in.ordenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ResultadoOperacion{}, noEncontrada()
			}
			if err != nil {
				return ResultadoOperacion{}, err
			}
			if !enEmpresa(ctx, f) {
				return ResultadoOperacion{}, noEncontrada()
			}
			t.FijarEmpresa(deref(f.EmpresaID))
			// Primero la idempotencia: la misma operación repetida devuelve su resultado aunque la
			// orden haya cambiado después (si algo de lo que sigue falla, la transacción la suelta).
			if err := reclamar(ctx, q, in.sync, &f.ID); err != nil {
				return ResultadoOperacion{}, err
			}
			// Solo la orden asignada al técnico: si ya no es suya pero lo fue, orden_reasignada.
			if !propia(ctx, f) {
				tuvo, err := q.TuvoOrden(ctx, campobd.TuvoOrdenParams{TecnicoID: contexto.ActorDe(ctx).ID, OrdenID: f.ID})
				if err != nil {
					return ResultadoOperacion{}, err
				}
				if tuvo {
					return ResultadoOperacion{}, ordenReasignada()
				}
				return ResultadoOperacion{}, noEncontrada()
			}
			if err := validarHoraLlamada(in.enTelefono, f.AsignadaEn, m.ahora()); err != nil {
				return ResultadoOperacion{}, err
			}
			t.Anotar(map[string]any{"operacion_id": in.sync.ID, "dispositivo_id": in.sync.DispositivoID, "tipo": in.sync.Tipo,
				"ocurrido_en": in.sync.OcurridoEn, "ocurrido_en_telefono": in.sync.OcurridoEnTelefono})
			llamada, err := m.cfg.LlamadasDirectas.RegistrarDirectaEnTx(ctx, t, EntradaLlamadaDirectaSync{ID: in.sync.ID, Orden: f,
				Telefono: in.datos.Telefono, UsuarioID: contexto.ActorDe(ctx).ID, En: in.sync.OcurridoEn})
			if err != nil {
				return ResultadoOperacion{}, sinDeadlock(err)
			}
			res := ResultadoOperacion{ID: in.sync.ID.String(), Estado: "aplicada", LlamadaID: &llamada,
				Orden: &EstadoOrden{Version: f.Version, Estado: f.Estado}}
			crudo, err := json.Marshal(res)
			if err != nil {
				return res, err
			}
			return res, q.FijarResultadoOperacion(ctx, campobd.FijarResultadoOperacionParams{ID: in.sync.ID, Resultado: crudo})
		},
	})
}

// validarHoraLlamada: la hora es obligatoria, no futura y no anterior a la asignación (con la
// tolerancia de los relojes de teléfono).
func validarHoraLlamada(en, asignadaEn *time.Time, ahora time.Time) error {
	if en == nil {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "ocurridoEn", Message: "Obligatorio en llamada.directa: la hora de la llamada", Rule: "required"})
	}
	if en.After(ahora.Add(toleranciaHoraLlamada)) {
		return nucleo.ReglaNegocio("hora_invalida", "La hora de la llamada está en el futuro: revisa la hora del teléfono")
	}
	if asignadaEn != nil && en.Before(asignadaEn.Add(-toleranciaHoraLlamada)) {
		return nucleo.ReglaNegocio("hora_invalida", "La hora de la llamada es anterior a la asignación de la orden")
	}
	if en.Before(ahora.Add(-antiguedadMaxima)) {
		return nucleo.ReglaNegocio("hora_invalida", "La llamada es de hace más de 7 días")
	}
	return nil
}

// aplicarLlamadaDirecta corre llamada.directa con su caso de uso.
func (m *Modulo) aplicarLlamadaDirecta(ctx context.Context, s *OpSync, op OperacionLote, desfaseMs int64) ResultadoOperacion {
	if err := sinNulos(op.Datos); err != nil {
		p, _ := nucleo.AProblema(err)
		return m.rechazar(ctx, s, op.OrdenID, p)
	}
	d, err := datosDe[DatosLlamadaDirectaLote](op)
	if err == nil {
		var en *time.Time
		if op.OcurridoEn != nil {
			c := op.OcurridoEn.Add(time.Duration(desfaseMs) * time.Millisecond).UTC()
			en = &c
		}
		var r ResultadoOperacion
		r, err = m.LlamadaDirectaLote.Ejecutar(ctx, entradaLlamadaDirectaLote{sync: s, ordenID: op.OrdenID, datos: d, enTelefono: en})
		if err == nil {
			return r
		}
	}
	var dup *duplicada
	if errors.As(err, &dup) {
		r := dup.resultado
		r.Estado = "duplicada"
		return r
	}
	p, _ := nucleo.AProblema(err)
	if codigosReintentables[p.Codigo()] {
		return reintentable(s.ID.String(), p)
	}
	return m.rechazar(ctx, s, op.OrdenID, p)
}
