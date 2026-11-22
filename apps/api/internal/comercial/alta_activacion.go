package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Alta pendiente hasta activar (contrato argosisp.md 0.10.0 §3.4.8, pedido B17). Con el servicio
// creado en Argos y la orden de instalación creada, el prospecto queda «ganado, alta pendiente»
// (esperando_activacion), ligado al cliente y al servicio de Argos. Pasa a completa cuando la
// activación de la instalación (iris:instalacion:<orden>) queda hecha en Argos. Si en la
// instalación cambia el titular, servicio.titular_cambiado lo pasa al cliente nuevo.

// AltaEsperandoActivacion: el servicio existe en Argos, falta activarlo.
const AltaEsperandoActivacion = "esperando_activacion"

// claveActivacion es la Idempotency-Key de la activación de la orden (pendientes, cierre_orden).
func claveActivacion(orden int64) string { return "iris:instalacion:" + strconv.FormatInt(orden, 10) }

// EntradaAltaActivada: la activación de la orden quedó hecha en Argos.
type EntradaAltaActivada struct {
	OrdenID int64 `json:"orden_id"`
}

// ResultadoAlta: los prospectos que cambiaron.
type ResultadoAlta struct {
	Prospectos []int64 `json:"prospectos"`
}

func (m *Modulo) nuevoAltaActivada() *auditoria.Caso[EntradaAltaActivada, ResultadoAlta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAltaActivada, ResultadoAlta]{
		Accion: "prospectos.alta_activada", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoAlta) bool { return len(r.Prospectos) > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAltaActivada) (ResultadoAlta, error) {
			r := ResultadoAlta{Prospectos: []int64{}}
			if err := exigirSistema(ctx); err != nil {
				return r, err
			}
			q := comercialbd.New(t)
			ids, err := q.ProspectosEsperandoActivacion(ctx, &in.OrdenID)
			if err != nil {
				return r, err
			}
			for _, id := range ids {
				antes, err := tomar(ctx, t, q, id, nucleo.PermProspectosGanar, nil)
				if err != nil {
					return r, err
				}
				if deref(antes.AltaEstado) != AltaEsperandoActivacion {
					continue
				}
				d := antes
				d.AltaEstado = ptr(AltaCompleta)
				f, err := guardar(ctx, t, q, antes, d)
				if err != nil {
					return r, err
				}
				if err := historial(ctx, t, q, f.ID, "alta", ptr(AltaEsperandoActivacion), f.AltaEstado, nil, map[string]any{
					"cliente_id": f.ClienteArgosID, "servicio_id": f.ServicioArgosID, "orden_id": in.OrdenID,
				}); err != nil {
					return r, err
				}
				if err := t.Emitir("prospecto.alta_actualizada", map[string]any{
					"prospecto_id": f.ID, "estado": AltaCompleta, "cliente_id": f.ClienteArgosID, "servicio_id": f.ServicioArgosID, "orden_id": in.OrdenID,
				}); err != nil {
					return r, err
				}
				r.Prospectos = append(r.Prospectos, f.ID)
			}
			return r, nil
		},
	})
}

// EntradaTitularCambiado es el payload de argos.titular_cambiado (servicio.titular_cambiado de
// Argos, confirmado por el conector).
type EntradaTitularCambiado struct {
	ServicioID         int64  `json:"servicio_id"`
	DeClienteID        *int64 `json:"de_cliente_id"`
	AClienteID         int64  `json:"a_cliente_id"`
	DeClienteEliminado bool   `json:"de_cliente_eliminado"`
	EntranteID         int64  `json:"entrante_id"`
	// TipoTitular: persona o empresa (contrato 0.14.1; ausente con un Argos anterior).
	TipoTitular *string `json:"tipo_titular,omitempty"`
}

func (m *Modulo) nuevoMoverTitular() *auditoria.Caso[EntradaTitularCambiado, ResultadoAlta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTitularCambiado, ResultadoAlta]{
		Accion: "prospectos.titular_cambiado", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoAlta) bool { return len(r.Prospectos) > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTitularCambiado) (ResultadoAlta, error) {
			r := ResultadoAlta{Prospectos: []int64{}}
			if err := exigirSistema(ctx); err != nil {
				return r, err
			}
			if in.ServicioID <= 0 || in.AClienteID <= 0 {
				return r, nil
			}
			q := comercialbd.New(t)
			// Solo si la instantánea sigue diciendo que el servicio es de a_cliente_id (MEDIA 2 de
			// #280): un aviso atrasado (A→B después de B→C) no devuelve el prospecto a B.
			actual, err := q.TitularDeServicioArgos(ctx, in.ServicioID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && actual != in.AClienteID) {
				t.Anotar(map[string]any{"titular": "no_coincide_instantanea", "entrante_id": in.EntranteID})
				return r, nil
			}
			if err != nil {
				return r, err
			}
			ids, err := q.ProspectosDeServicioArgos(ctx, &in.ServicioID)
			if err != nil {
				return r, err
			}
			for _, id := range ids {
				antes, err := tomar(ctx, t, q, id, nucleo.PermProspectosGanar, nil)
				if err != nil {
					return r, err
				}
				// Idempotente: ya está en el titular nuevo (el webhook puede llegar dos veces).
				if antes.ClienteArgosID != nil && *antes.ClienteArgosID == in.AClienteID {
					continue
				}
				d := antes
				d.ClienteArgosID = &in.AClienteID
				f, err := guardar(ctx, t, q, antes, d)
				if err != nil {
					return r, err
				}
				if err := historial(ctx, t, q, f.ID, "titular", nil, nil, nil, map[string]any{
					"de_cliente_id": antes.ClienteArgosID, "a_cliente_id": in.AClienteID, "servicio_id": in.ServicioID,
					"de_cliente_eliminado": in.DeClienteEliminado, "tipo_titular": in.TipoTitular, "entrante_id": in.EntranteID,
				}); err != nil {
					return r, err
				}
				if err := t.Emitir("prospecto.alta_actualizada", map[string]any{
					"prospecto_id": f.ID, "estado": deref(f.AltaEstado), "cliente_id": in.AClienteID, "servicio_id": in.ServicioID, "orden_id": f.OrdenID,
				}); err != nil {
					return r, err
				}
				r.Prospectos = append(r.Prospectos, f.ID)
			}
			return r, nil
		},
	})
}

// ActivarAlta: la activación de la orden quedó hecha (actor sistema; ctx con la base).
func (m *Modulo) ActivarAlta(ctx context.Context, orden int64) (ResultadoAlta, error) {
	return m.altaActivada.Ejecutar(ctx, EntradaAltaActivada{OrdenID: orden})
}

// MoverTitular pasa los prospectos del servicio al titular nuevo (actor sistema).
func (m *Modulo) MoverTitular(ctx context.Context, in EntradaTitularCambiado) (ResultadoAlta, error) {
	return m.moverTitular.Ejecutar(ctx, in)
}

// activacionHecha: en el barrido, si la activación de la orden ya está hecha, completa el alta.
func (m *Modulo) activacionHecha(ctx context.Context, bd nucleo.BD, p comercialbd.ComercialProspecto) error {
	if p.OrdenID == nil {
		return nil
	}
	pend, err := pendientes.PorClave(ctx, bd, claveActivacion(*p.OrdenID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || pend.Estado != pendientes.EstadoHecho {
		return err
	}
	_, err = m.ActivarAlta(ctx, *p.OrdenID)
	return err
}

// eventoPendienteOrden: lo que usa comercial de integracion.pendiente_hecho de una orden.
type eventoPendienteOrden struct {
	Operacion string `json:"operacion"`
	Entidad   string `json:"entidad"`
	EntidadID string `json:"entidad_id"`
	Clave     string `json:"idempotency_key"`
}

// SuscribirActivacion: la activación de una instalación hecha en Argos completa el alta; el
// cambio de titular en Argos pasa el prospecto al cliente nuevo (cmd/worker). Idempotentes.
func (m *Modulo) SuscribirActivacion(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("comercial.alta_activada", pendientes.EventoHecho, func(ctx context.Context, e integracion.Evento) error {
		var p eventoPendienteOrden
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Entidad != "orden" || p.Operacion != pendientes.OpActivarInstalacion {
			return nil
		}
		orden, err := strconv.ParseInt(p.EntidadID, 10, 64)
		if err != nil || orden <= 0 {
			return nil
		}
		_, err = m.ActivarAlta(nucleo.ConBD(ctx, bd), orden)
		return err
	})
	bus.Suscribir("comercial.titular_cambiado", argos.EventoTitularCambiado, func(ctx context.Context, e integracion.Evento) error {
		var in EntradaTitularCambiado
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			log.ErrorContext(ctx, "comercial: argos.titular_cambiado ilegible", "evento", e.ID, "err", err)
			return nil
		}
		_, err := m.MoverTitular(nucleo.ConBD(ctx, bd), in)
		return err
	})
}
