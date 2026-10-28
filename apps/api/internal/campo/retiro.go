package campo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Orden de retiro automática: argos.servicio_retirado (el núcleo lo emite tras confirmar en
// Argos que el servicio quedó retirado) → si el servicio tiene equipos en comodato según el
// conector y no hay una orden de retiro abierta para él, campo crea una (origen argos).

// EntradaRetiroArgos: el evento ya leído y el comodato del servicio (leído de Argos antes).
type EntradaRetiroArgos struct {
	EntranteID int64            `json:"entranteId"`
	EmpresaID  *int64           `json:"empresaId"`
	ServicioID int64            `json:"servicioId"`
	ClienteID  *int64           `json:"clienteId"`
	De         string           `json:"de"`
	Equipos    []EquipoComodato `json:"equipos"`
}

// servicioRetirado es el payload de argos.servicio_retirado (contratos/eventos.md).
type servicioRetirado struct {
	ServicioID json.Number  `json:"servicio_id"`
	ClienteID  *json.Number `json:"cliente_id"`
	De         string       `json:"de"`
	A          string       `json:"a"`
	EntranteID json.Number  `json:"entrante_id"`
}

// nuevoRetiroDesdeArgos: idempotente (candado por servicio, orden de retiro abierta y la
// Idempotency-Key del entrante). Devuelve nil si no hizo falta crearla (no deja auditoría).
func (m *Modulo) nuevoRetiroDesdeArgos() *auditoria.Caso[EntradaRetiroArgos, *Orden] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRetiroArgos, *Orden]{
		Accion: "ordenes.retiro_automatico", Permiso: nucleo.PermOrdenesCrear,
		AuditarExito: func(o *Orden) bool { return o != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRetiroArgos) (*Orden, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return nil, nucleo.Prohibido("La orden de retiro automática la crea el sistema")
			}
			q := campobd.New(t)
			if err := q.CandadoRetiroServicio(ctx, in.ServicioID); err != nil {
				return nil, err
			}
			abierta, err := q.RetiroAbiertoDeServicio(ctx, campobd.RetiroAbiertoDeServicioParams{ServicioID: &in.ServicioID, EmpresaID: in.EmpresaID})
			if err == nil {
				t.Anotar(map[string]any{"orden_abierta": abierta})
				return nil, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			equipos := make([]string, 0, len(in.Equipos))
			for _, e := range in.Equipos {
				d := e.Serie
				if e.Modelo != "" {
					d += " (" + e.Modelo + ")"
				}
				equipos = append(equipos, strings.TrimSpace(d))
			}
			descripcion := fmt.Sprintf("Retiro automático: el servicio %d se retiró en Argos (%s → retirado). Recuperar los equipos en comodato: %s.",
				in.ServicioID, in.De, strings.Join(equipos, ", "))
			if r := []rune(descripcion); len(r) > 4000 {
				descripcion = string(r[:3999]) + "…"
			}
			entrada := EntradaCrearOrden{Tipo: "retiro", EmpresaID: in.EmpresaID, ClienteID: in.ClienteID, ServicioID: &in.ServicioID,
				Descripcion: descripcion, ClaveIdempotencia: fmt.Sprintf("argos.servicio_retirado:%d", in.EntranteID), Origen: "argos"}
			// Sin instantánea con dirección (ni del servicio ni del cliente), la orden nace con una
			// dirección por confirmar: despacho la completa al programarla.
			if !tieneDireccion(ctx, q, in.ServicioID, in.ClienteID) {
				entrada.Direccion = ptr(fmt.Sprintf("Por confirmar (servicio %d de Argos)", in.ServicioID))
			}
			if in.ClienteID == nil {
				// La orden exige el cliente del servicio: sin él, la instantánea lo da.
				if s, err := q.ServicioArgos(ctx, in.ServicioID); err == nil {
					entrada.ClienteID = &s.ClienteID
				}
			}
			if entrada.ClienteID == nil {
				// Sin cliente no se puede ligar el servicio (la orden lo exige): queda en la
				// descripción.
				entrada.ServicioID = nil
			}
			o, err := m.CrearOrden.Ejecutar(ctx, entrada)
			if err != nil {
				return nil, err
			}
			t.FijarEntidad("orden", o.ID)
			t.FijarEmpresa(deref(o.EmpresaID))
			return &o, nil
		},
	})
}

func tieneDireccion(ctx context.Context, q *campobd.Queries, servicio int64, cliente *int64) bool {
	if s, err := q.ServicioArgos(ctx, servicio); err == nil && strings.TrimSpace(s.Direccion) != "" {
		return true
	}
	if cliente != nil {
		if c, err := q.ClienteArgos(ctx, *cliente); err == nil && strings.TrimSpace(c.Direccion) != "" {
			return true
		}
	}
	return false
}

// AlServicioRetirado procesa argos.servicio_retirado (actor sistema del relay): lee el comodato
// de Argos fuera de la transacción y, si hay equipos, pide la orden de retiro. Sin conector no
// se sabe el comodato: no crea nada (log). Argos caído: error (el relay reintenta). Una regla de
// negocio (422) no se reintenta: queda en el log.
func (m *Modulo) AlServicioRetirado(ctx context.Context, e integracion.Evento, log *slog.Logger) error {
	var p servicioRetirado
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		log.WarnContext(ctx, "campo: argos.servicio_retirado ilegible", "evento", e.ID, "error", err)
		return nil
	}
	servicio, err := p.ServicioID.Int64()
	if err != nil || servicio <= 0 {
		log.WarnContext(ctx, "campo: argos.servicio_retirado sin servicio_id", "evento", e.ID)
		return nil
	}
	in := EntradaRetiroArgos{EmpresaID: e.EmpresaID, ServicioID: servicio, De: p.De}
	in.EntranteID, _ = p.EntranteID.Int64()
	if p.ClienteID != nil {
		if c, err := p.ClienteID.Int64(); err == nil && c > 0 {
			in.ClienteID = &c
		}
	}
	if m.cfg.Inventario == nil {
		log.InfoContext(ctx, "campo: servicio retirado sin conector de inventario; no se crea orden de retiro", "servicio", servicio)
		return nil
	}
	equipos, err := m.cfg.Inventario.EquiposEnComodato(ctx, deref(e.EmpresaID), servicio)
	switch {
	case errors.Is(err, ErrSinConector):
		log.InfoContext(ctx, "campo: servicio retirado con el conector apagado; no se crea orden de retiro", "servicio", servicio)
		return nil
	case errors.Is(err, argos.ErrOtraEmpresa):
		log.WarnContext(ctx, "campo: el servicio retirado es de otra empresa según Argos", "servicio", servicio)
		return nil
	case err != nil:
		return err
	}
	if len(equipos) == 0 {
		return nil
	}
	in.Equipos = equipos
	o, err := m.RetiroDesdeArgos.Ejecutar(ctx, in)
	var pr *nucleo.Problema
	if errors.As(err, &pr) && pr.Status < 500 {
		log.WarnContext(ctx, "campo: no se pudo crear la orden de retiro", "servicio", servicio, "codigo", pr.Codigo(), "detalle", pr.Detail)
		return nil
	}
	if err != nil {
		return err
	}
	if o != nil {
		log.InfoContext(ctx, "campo: orden de retiro automática", "orden", o.Numero, "servicio", servicio)
	}
	return nil
}

// SuscribirRetiros conecta argos.servicio_retirado (en cmd/worker).
func (m *Modulo) SuscribirRetiros(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("campo.retiro", argos.EventoServicioRetirado, func(ctx context.Context, e integracion.Evento) error {
		return m.AlServicioRetirado(nucleo.ConBD(ctx, bd), e, log)
	})
}
