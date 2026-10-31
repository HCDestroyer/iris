package campo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Ticket y orden de la mano: crear la orden de un ticket ya asignada (y programada) en un paso,
// e igualar la asignación de la orden y su ticket cuando difieren. Las dos son acciones
// compuestas: corren los casos de uso de siempre (ordenes.crear, ordenes.asignar,
// ordenes.programar, tickets.asignar) anidados en su transacción, cada uno con su permiso, su
// auditoría y sus eventos (orden.creada, orden.asignada, orden.programada, ticket.asignado).
// Nada se iguala en silencio: siempre lo pide alguien.

// EntradaCrearOrdenDeTicket es la entrada de tickets.crear_orden (POST /tickets/{id}/crear-orden).
type EntradaCrearOrdenDeTicket struct {
	TicketID      int64      `json:"-"`
	Tipo          string     `json:"tipo,omitempty" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento" doc:"visita por defecto"`
	PlantillaID   *int64     `json:"plantillaId,omitempty" minimum:"1" doc:"Otra plantilla del mismo tipo; por defecto, la del tipo"`
	Descripcion   string     `json:"descripcion,omitempty" maxLength:"4000" doc:"Trabajo a realizar; vacío = el asunto del ticket"`
	Direccion     *string    `json:"direccion,omitempty" maxLength:"300" doc:"Vacía = la del servicio o el cliente del ticket en Argos"`
	TecnicoID     *int64     `json:"tecnicoId,omitempty" minimum:"1" doc:"Técnico (usuario). Técnico, cuadrilla o ambos: al menos uno (422 asignacion_requerida)"`
	CuadrillaID   *int64     `json:"cuadrillaId,omitempty" minimum:"1" doc:"Cuadrilla (catálogo de agenda). Ausente = ninguna (no se hereda la del ticket: el panel la propone)"`
	BodegaID      *int64     `json:"bodegaId,omitempty" minimum:"1" doc:"Bodega del técnico en Argos"`
	Inicio        *time.Time `json:"inicio,omitempty" doc:"Con fecha, la orden queda programada para el técnico (exige tecnicoId; mismas validaciones de agenda que programar)"`
	Fin           *time.Time `json:"fin,omitempty" doc:"Fin estimado; por defecto, inicio + la duración del tipo"`
	VentanaInicio *time.Time `json:"ventanaInicio,omitempty" doc:"Ventana de llegada acordada con el cliente (va con ventanaFin)"`
	VentanaFin    *time.Time `json:"ventanaFin,omitempty"`

	// ClaveIdempotencia es la Idempotency-Key de la petición (la pone el handler).
	ClaveIdempotencia string `json:"-"`
}

// asignacionRequerida: ni técnico ni cuadrilla.
func asignacionRequerida() *nucleo.Problema {
	return nucleo.Validacion(
		nucleo.ErrorCampo{Field: "tecnicoId", Message: "Elige un técnico o una cuadrilla", Rule: "asignacion_requerida"},
		nucleo.ErrorCampo{Field: "cuadrillaId", Message: "Elige un técnico o una cuadrilla", Rule: "asignacion_requerida"},
	)
}

// nuevoCrearOrdenDeTicket: tickets.crear_orden. Crea la orden ligada al ticket (hereda cliente,
// servicio, zona, dirección y contacto; el ticket recibe la nota de siempre), la asigna al
// técnico y/o cuadrilla elegidos y, con fecha, la programa. Todo o nada: si la agenda rechaza la
// cita (fuera de turno, solape…), no queda orden.
func (m *Modulo) nuevoCrearOrdenDeTicket() *auditoria.Caso[EntradaCrearOrdenDeTicket, Orden] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearOrdenDeTicket, Orden]{
		Accion:  "tickets.crear_orden",
		Permiso: nucleo.PermTicketsCrearOrden,
		Logica: sinDeadlockEn(func(ctx context.Context, t *auditoria.Tx, in EntradaCrearOrdenDeTicket) (Orden, error) {
			if err := sinNulos(in); err != nil {
				return Orden{}, err
			}
			t.FijarEntidad("ticket", in.TicketID)
			q := campobd.New(t)

			// La misma petición repetida (clave reclamada tras una caída) devuelve su orden tal
			// cual quedó: la asignación y la cita ya se hicieron en aquella transacción.
			if idem := conIdempotencia(ctx, in.ClaveIdempotencia); idem != nil {
				previa, err := q.OrdenPorIdempotencia(ctx, idem)
				if err == nil {
					if err := verificarAcceso(ctx, nucleo.PermOrdenesVer, previa); err != nil {
						return Orden{}, err
					}
					// La clave ya creó la orden de otro ticket: no es la misma petición.
					if previa.TicketID == nil || *previa.TicketID != in.TicketID {
						return Orden{}, nucleo.ReglaNegocio("idempotencia_reutilizada",
							"Esa Idempotency-Key ya se usó para crear la orden "+previa.Numero+" de otro ticket")
					}
					t.FijarEmpresa(deref(previa.EmpresaID))
					t.Anotar(map[string]any{"repetida": true, "orden_id": previa.ID})
					tipo, err := tipoDe(ctx, q, previa)
					if err != nil {
						return Orden{}, err
					}
					return aOrden(previa, tipo.Codigo), nil
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return Orden{}, err
				}
			}

			if in.TecnicoID == nil && in.CuadrillaID == nil {
				return Orden{}, asignacionRequerida()
			}
			if in.Inicio == nil && (in.Fin != nil || in.VentanaInicio != nil || in.VentanaFin != nil) {
				return Orden{}, campoObligatorio("inicio")
			}
			if in.Inicio != nil && in.TecnicoID == nil {
				return Orden{}, campoInvalido("tecnicoId", "Para programar la visita elige el técnico", "required")
			}
			// El ticket: visible para el actor (si no, el mismo 404 de soporte) y no cerrado.
			tk, err := soporte.TicketParaOrden(ctx, t, in.TicketID)
			if err != nil {
				return Orden{}, err
			}
			t.FijarEmpresa(deref(tk.EmpresaID))
			tipo := in.Tipo
			if tipo == "" {
				tipo = "visita"
			}
			descripcion := strings.TrimSpace(in.Descripcion)
			if descripcion == "" {
				descripcion = tk.Asunto
			}

			o, err := m.CrearOrden.Ejecutar(ctx, EntradaCrearOrden{
				Tipo: tipo, PlantillaID: in.PlantillaID, TicketID: &tk.ID, CuadrillaID: in.CuadrillaID, BodegaID: in.BodegaID,
				Direccion: in.Direccion, Descripcion: descripcion, ClaveIdempotencia: in.ClaveIdempotencia,
				sinCuadrillaDelTicket: true,
			})
			if err != nil {
				return Orden{}, err
			}
			if in.TecnicoID != nil {
				// ordenes.asignar de siempre: valida al técnico y emite orden.asignada.
				if o, err = m.AsignarOrden.Ejecutar(ctx, EntradaAsignarOrden{
					ID: o.ID, Version: o.Version, TecnicoID: in.TecnicoID, CuadrillaID: o.CuadrillaID,
				}); err != nil {
					return Orden{}, err
				}
			} else {
				// Solo cuadrilla: ordenes.asignar no corre (no cambia nada), pero la asignación se
				// exige igual, con su alcance (403 si no la cubre).
				if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesAsignar)) {
					return Orden{}, nucleo.Prohibido("Falta el permiso ordenes.asignar")
				}
				f, err := q.OrdenPorIDParaActualizar(ctx, o.ID)
				if err != nil {
					return Orden{}, err
				}
				if err := verificarAcceso(ctx, nucleo.PermOrdenesAsignar, f); err != nil {
					return Orden{}, err
				}
				if err := asignadaAlCrear(ctx, t, q, o, m.ahora()); err != nil {
					return Orden{}, err
				}
			}
			if in.Inicio != nil {
				if o, err = m.ProgramarOrden.Ejecutar(ctx, EntradaProgramarOrden{
					ID: o.ID, Version: o.Version, Inicio: *in.Inicio, Fin: in.Fin,
					VentanaInicio: in.VentanaInicio, VentanaFin: in.VentanaFin,
				}); err != nil {
					return Orden{}, err
				}
			}
			t.Anotar(map[string]any{"orden_id": o.ID, "numero": o.Numero, "programada": in.Inicio != nil})
			return o, nil
		}),
	})
}

// asignadaAlCrear: la orden nació solo con cuadrilla (sin técnico). ordenes.asignar no aplica
// (no cambia nada), pero la asignación se registra y se avisa igual: historial y orden.asignada.
func asignadaAlCrear(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, o Orden, ahora time.Time) error {
	if err := historial(ctx, t, q, o.ID, "asignada", nil, nil, nil, map[string]any{
		"tecnico_id": nil, "cuadrilla_id": o.CuadrillaID, "bodega_id": o.BodegaID,
		"tecnico_anterior": nil, "cuadrilla_anterior": nil,
	}, ahora); err != nil {
		return err
	}
	return t.Emitir("orden.asignada", map[string]any{
		"orden_id": o.ID, "tecnico_id": nil, "cuadrilla_id": o.CuadrillaID, "tecnico_anterior_id": nil,
	})
}

// Sentidos de ordenes.igualar_asignacion.
const (
	IgualarDesdeTicket = "ticket" // la orden toma el responsable y la cuadrilla del ticket
	IgualarDesdeOrden  = "orden"  // el ticket toma el técnico y la cuadrilla de la orden
)

// EntradaIgualarAsignacion es la entrada de ordenes.igualar_asignacion. Lleva las dos
// versiones: lo que se copia es lo que el usuario vio (si una cambió, 409).
type EntradaIgualarAsignacion struct {
	ID            int64  `json:"-"`
	Desde         string `json:"desde" enum:"ticket,orden" doc:"ticket = la orden toma el responsable y la cuadrilla del ticket (ordenes.asignar); orden = el ticket toma el técnico y la cuadrilla de la orden (tickets.asignar)"`
	Version       int32  `json:"version" minimum:"1" doc:"Versión leída de la orden; si cambió, 409"`
	VersionTicket int32  `json:"versionTicket" minimum:"1" doc:"Versión leída del ticket; si cambió, 409"`
}

func (e EntradaIgualarAsignacion) ordenID() int64 { return e.ID }

// AsignacionTicketOrden es el resultado: la orden y su ticket ya igualados.
type AsignacionTicketOrden struct {
	Orden  Orden          `json:"orden"`
	Ticket soporte.Ticket `json:"ticket"`
}

// nuevoIgualarAsignacion: ordenes.igualar_asignacion. Copia técnico y cuadrilla de un lado al
// otro con el caso de uso del lado que cambia (ordenes.asignar o tickets.asignar, con sus
// validaciones, permisos y eventos). Sin nada que copiar: 422 ticket_sin_asignar u
// orden_sin_asignar; ya iguales: 422 sin_cambios.
func (m *Modulo) nuevoIgualarAsignacion() *auditoria.Caso[EntradaIgualarAsignacion, AsignacionTicketOrden] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIgualarAsignacion, AsignacionTicketOrden]{
		Accion:  "ordenes.igualar_asignacion",
		Permiso: nucleo.PermOrdenesIgualarAsignacion,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIgualarAsignacion) (AsignacionTicketOrden, error) {
			r, err := m.igualar(ctx, t, in)
			return r, sinDeadlock(err)
		},
	})
}

// sinDeadlock: red de seguridad. Orden y ticket se bloquean siempre en el mismo orden (orden,
// después ticket, con FOR UPDATE), pero si Postgres corta un deadlock o una serialización la
// respuesta es 409 concurrencia (reintentable), nunca 500.
func sinDeadlock(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "40P01" || pg.Code == "40001") {
		return nucleo.Conflicto("concurrencia", "Otra operación cambió la orden o el ticket a la vez; vuelve a intentar")
	}
	return err
}

// sinDeadlockEn envuelve la lógica de un caso con sinDeadlock.
func sinDeadlockEn[In, Out any](logica func(context.Context, *auditoria.Tx, In) (Out, error)) func(context.Context, *auditoria.Tx, In) (Out, error) {
	return func(ctx context.Context, t *auditoria.Tx, in In) (Out, error) {
		out, err := logica(ctx, t, in)
		return out, sinDeadlock(err)
	}
}

func (m *Modulo) igualar(ctx context.Context, t *auditoria.Tx, in EntradaIgualarAsignacion) (AsignacionTicketOrden, error) {
	var r AsignacionTicketOrden
	q := campobd.New(t)
	f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesVer, &in.Version, nil)
	if err != nil {
		return r, err
	}
	if f.TicketID == nil {
		return r, nucleo.ReglaNegocio("sin_ticket", "La orden no viene de un ticket")
	}
	tk, err := soporte.TicketParaOrden(ctx, t, *f.TicketID)
	if err != nil {
		return r, err
	}
	if err := nucleo.ExigirVersion(in.VersionTicket, tk.Version, "ticket", tk.ID); err != nil {
		return r, err
	}
	if mismoID(f.TecnicoID, tk.AsignadoA) && mismoID(f.CuadrillaID, tk.CuadrillaID) {
		return r, nucleo.ReglaNegocio("sin_cambios", "La orden y el ticket ya tienen la misma asignación")
	}
	t.Anotar(map[string]any{"desde": in.Desde, "ticket_id": tk.ID})
	switch in.Desde {
	case IgualarDesdeTicket:
		if tk.AsignadoA == nil && tk.CuadrillaID == nil {
			return r, nucleo.ReglaNegocio("ticket_sin_asignar", fmt.Sprintf("El ticket %s no tiene responsable ni cuadrilla", tk.Numero))
		}
		if r.Orden, err = m.AsignarOrden.Ejecutar(ctx, EntradaAsignarOrden{
			ID: f.ID, Version: f.Version, TecnicoID: tk.AsignadoA, CuadrillaID: tk.CuadrillaID,
		}); err != nil {
			return r, err
		}
		r.Ticket, err = m.cfg.Soporte.VerTicket.Ejecutar(ctx, soporte.EntradaVerTicket{ID: tk.ID})
		return r, err
	case IgualarDesdeOrden:
		if f.TecnicoID == nil && f.CuadrillaID == nil {
			return r, nucleo.ReglaNegocio("orden_sin_asignar", fmt.Sprintf("La orden %s no tiene técnico ni cuadrilla", f.Numero))
		}
		if r.Ticket, err = m.cfg.Soporte.AsignarTicket.Ejecutar(ctx, soporte.EntradaAsignarTicket{
			ID: tk.ID, Version: tk.Version, UsuarioID: f.TecnicoID, CuadrillaID: f.CuadrillaID,
		}); err != nil {
			return r, err
		}
		tipo, err := tipoDe(ctx, q, f)
		if err != nil {
			return r, err
		}
		r.Orden = aOrden(f, tipo.Codigo)
		return r, nil
	}
	return r, campoInvalido("desde", "Indica ticket u orden", "enum")
}
