package soporte

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Portal del cliente (/soporte): casos de uso con Acceso auditoria.Cliente. El cliente y la
// empresa salen siempre de la sesión del portal (contexto.ActorDe(ctx).ClienteID/EmpresaID),
// nunca del cuerpo. Solo ve sus tickets (cliente_id y empresa_id de la sesión; lo demás, 404)
// y solo sus mensajes públicos: nunca notas internas, historial, asignación, SLA ni datos de
// Horus. La lógica común con el panel vive en funciones internas (crearTicket,
// reactivarPorCliente, tomar, cerrar), porque un caso Cliente no puede anidar uno ConPermiso.

// TicketPortal es lo que el cliente ve de su ticket.
type TicketPortal struct {
	ID                   int64      `json:"id"`
	Numero               string     `json:"numero" doc:"Número legible (TK-000123)"`
	Asunto               string     `json:"asunto"`
	Estado               string     `json:"estado" enum:"nuevo,abierto,en_espera,resuelto,cerrado"`
	CategoriaID          *int64     `json:"categoriaId" nullable:"true"`
	Categoria            *string    `json:"categoria" nullable:"true" doc:"Nombre de la categoría"`
	ServicioID           *int64     `json:"servicioId" nullable:"true" doc:"Servicio en Argos"`
	Origen               string     `json:"origen" enum:"panel,portal,correo,telegram,horus,argos,api"`
	AfectadoPorIncidente bool       `json:"afectadoPorIncidente" doc:"Vinculado a una falla general de la red (incidente maestro) que sigue abierta"`
	PuedeConfirmar       bool       `json:"puedeConfirmar" doc:"Resuelto: el cliente puede confirmar la solución (lo cierra) o responder (lo reabre)"`
	ResueltoEn           *time.Time `json:"resueltoEn" nullable:"true"`
	CerradoEn            *time.Time `json:"cerradoEn" nullable:"true"`
	Version              int32      `json:"version"`
	CreadoEn             time.Time  `json:"creadoEn"`
	ActualizadoEn        time.Time  `json:"actualizadoEn"`
}

func aTicketPortal(f soportebd.SoporteTicket, categoria *string, padreAbierto bool) TicketPortal {
	return TicketPortal{
		ID: f.ID, Numero: f.Numero, Asunto: f.Asunto, Estado: f.Estado, CategoriaID: f.CategoriaID, Categoria: categoria,
		ServicioID: f.ServicioID, Origen: f.Origen, AfectadoPorIncidente: padreAbierto,
		PuedeConfirmar: f.Estado == EstadoResuelto, ResueltoEn: f.ResueltoEn, CerradoEn: f.CerradoEn,
		Version: f.Version, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt,
	}
}

// ticketPortal agrega el nombre de la categoría y si el incidente maestro sigue abierto.
func ticketPortal(ctx context.Context, q *soportebd.Queries, f soportebd.SoporteTicket) (TicketPortal, error) {
	d, err := q.DatosTicketPortal(ctx, f.ID)
	if err != nil {
		return TicketPortal{}, err
	}
	return aTicketPortal(f, d.CategoriaNombre, d.PadreAbierto), nil
}

// MensajePortal es un mensaje público de la conversación, como lo ve el cliente.
type MensajePortal struct {
	ID          int64       `json:"id"`
	AutorTipo   string      `json:"autorTipo" enum:"cliente,agente,sistema" doc:"cliente = el propio cliente (portal o correo); agente = soporte; sistema = aviso automático"`
	AutorNombre string      `json:"autorNombre"`
	Cuerpo      string      `json:"cuerpo"`
	Adjuntos    []uuid.UUID `json:"adjuntos"`
	CreadoEn    time.Time   `json:"creadoEn"`
}

func aMensajePortal(m soportebd.SoporteTicketMensaje) MensajePortal {
	autor := m.AutorTipo
	if autor == "usuario" {
		autor = "agente"
	}
	nombre := m.AutorNombre
	if autor == "sistema" {
		nombre = "Soporte"
	}
	adj := m.Adjuntos
	if adj == nil {
		adj = []uuid.UUID{}
	}
	return MensajePortal{ID: m.ID, AutorTipo: autor, AutorNombre: nombre, Cuerpo: m.Cuerpo, Adjuntos: adj, CreadoEn: m.CreatedAt}
}

// TicketPortalConMensaje es el ticket tras el mensaje del cliente.
type TicketPortalConMensaje struct {
	Ticket  TicketPortal  `json:"ticket"`
	Mensaje MensajePortal `json:"mensaje"`
}

// CategoriaPortal es una categoría que el cliente puede elegir.
type CategoriaPortal struct {
	ID     int64  `json:"id"`
	Codigo string `json:"codigo"`
	Nombre string `json:"nombre"`
}

// Entradas -----------------------------------------------------------------------------

// EntradaListarTicketsPortal: tickets del cliente, más recientes primero.
type EntradaListarTicketsPortal struct {
	nucleo.ParamsPagina
	Estado []string `query:"estado" enum:"nuevo,abierto,en_espera,resuelto,cerrado" doc:"Uno o varios estados"`
}

// EntradaVerTicketPortal: un ticket del cliente.
type EntradaVerTicketPortal struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaListarMensajesPortal: mensajes públicos del ticket, más antiguos primero.
type EntradaListarMensajesPortal struct {
	nucleo.ParamsPagina
	ID int64 `path:"id" minimum:"1"`
}

// EntradaCrearTicketPortal: el cliente abre un ticket. Cliente y empresa salen de la sesión.
type EntradaCrearTicketPortal struct {
	Asunto      string `json:"asunto" minLength:"3" maxLength:"200"`
	Descripcion string `json:"descripcion" minLength:"1" maxLength:"20000" doc:"Primer mensaje del cliente"`
	CategoriaID *int64 `json:"categoriaId,omitempty" minimum:"1" doc:"Una de GET /portal/categorias"`
	ServicioID  *int64 `json:"servicioId,omitempty" minimum:"1" doc:"Servicio del cliente en Argos (debe ser suyo según la instantánea)"`

	ClaveIdempotencia string `json:"-"`
}

// EntradaResponderTicketPortal: mensaje del cliente.
type EntradaResponderTicketPortal struct {
	ID      int64  `json:"-"`
	Version *int32 `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si se envía y cambió, 409"`
	Cuerpo  string `json:"cuerpo" minLength:"1" maxLength:"20000"`

	ClaveIdempotencia string `json:"-"`
}

// EntradaConfirmarSolucion: el cliente confirma que el ticket resuelto quedó bien (lo cierra).
type EntradaConfirmarSolucion struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
}

// Casos de uso -------------------------------------------------------------------------

func (m *Modulo) nuevoListarTicketsPortal() *auditoria.Caso[EntradaListarTicketsPortal, nucleo.Pagina[TicketPortal]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarTicketsPortal, nucleo.Pagina[TicketPortal]]{
		Accion:       "portal.ver_tickets",
		Acceso:       auditoria.Cliente,
		AuditarExito: soloLecturas[nucleo.Pagina[TicketPortal]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarTicketsPortal) (nucleo.Pagina[TicketPortal], error) {
			a := contexto.ActorDe(ctx)
			var antesDe int64
			hay, err := nucleo.DecodificarCursor(in.Cursor, &antesDe)
			if err != nil {
				return nucleo.Pagina[TicketPortal]{}, err
			}
			p := soportebd.ListarTicketsClienteParams{
				ClienteID: a.ClienteID, EmpresaID: a.EmpresaID, Estados: noNulo(in.Estado),
				Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			}
			if hay {
				p.AntesDe = &antesDe
			}
			filas, err := soportebd.New(t).ListarTicketsCliente(ctx, p)
			if err != nil {
				return nucleo.Pagina[TicketPortal]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(),
				func(f soportebd.ListarTicketsClienteRow) TicketPortal {
					return aTicketPortal(f.SoporteTicket, f.CategoriaNombre, f.PadreAbierto)
				},
				func(f soportebd.ListarTicketsClienteRow) []any { return []any{f.SoporteTicket.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoVerTicketPortal() *auditoria.Caso[EntradaVerTicketPortal, TicketPortal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerTicketPortal, TicketPortal]{
		Accion:       "portal.ver_ticket",
		Acceso:       auditoria.Cliente,
		AuditarExito: soloLecturas[TicketPortal],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerTicketPortal) (TicketPortal, error) {
			t.FijarEntidad("ticket", in.ID)
			q := soportebd.New(t)
			f, err := leer(ctx, q, in.ID)
			if err != nil {
				return TicketPortal{}, err
			}
			return ticketPortal(ctx, q, f)
		},
	})
}

func (m *Modulo) nuevoListarMensajesPortal() *auditoria.Caso[EntradaListarMensajesPortal, nucleo.Pagina[MensajePortal]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarMensajesPortal, nucleo.Pagina[MensajePortal]]{
		Accion:       "portal.ver_ticket",
		Acceso:       auditoria.Cliente,
		AuditarExito: soloLecturas[nucleo.Pagina[MensajePortal]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarMensajesPortal) (nucleo.Pagina[MensajePortal], error) {
			t.FijarEntidad("ticket", in.ID)
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[MensajePortal]{}, err
			}
			q := soportebd.New(t)
			if _, err := leer(ctx, q, in.ID); err != nil {
				return nucleo.Pagina[MensajePortal]{}, err
			}
			filas, err := q.ListarMensajesPublicos(ctx, soportebd.ListarMensajesPublicosParams{
				TicketID: in.ID, DespuesDe: despues, Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			})
			if err != nil {
				return nucleo.Pagina[MensajePortal]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aMensajePortal,
				func(f soportebd.SoporteTicketMensaje) []any { return []any{f.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoCrearTicketPortal() *auditoria.Caso[EntradaCrearTicketPortal, TicketPortal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearTicketPortal, TicketPortal]{
		Accion: "portal.crear_ticket",
		Acceso: auditoria.Cliente,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearTicketPortal) (TicketPortal, error) {
			a := contexto.ActorDe(ctx)
			q := soportebd.New(t)
			descripcion, err := texto("descripcion", in.Descripcion, 1, 20000)
			if err != nil {
				return TicketPortal{}, err
			}
			if in.CategoriaID != nil {
				c, err := q.CategoriaPorID(ctx, *in.CategoriaID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!c.VisiblePortal || !c.Activa ||
					(c.EmpresaID != nil && *c.EmpresaID != a.EmpresaID))) {
					return TicketPortal{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "categoriaId", Message: "Elige una categoría de la lista", Rule: "categoria"})
				}
				if err != nil {
					return TicketPortal{}, err
				}
			}
			if err := m.topeTickets(ctx, q, a, in.ClaveIdempotencia); err != nil {
				return TicketPortal{}, err
			}
			entrada := EntradaCrearTicket{
				ClienteID: &a.ClienteID, CategoriaID: in.CategoriaID, Prioridad: "media", Origen: "portal",
				Asunto: in.Asunto, Descripcion: descripcion, ClaveIdempotencia: in.ClaveIdempotencia,
			}
			// Contacto y zona desde la ficha del cliente en la instantánea de Argos (si existe).
			ficha, err := q.ClienteDeArgos(ctx, soportebd.ClienteDeArgosParams{ID: a.ClienteID, EmpresaID: a.EmpresaID})
			switch {
			case err == nil:
				entrada.ZonaID = ficha.ZonaID
				entrada.Contacto = &EntradaContacto{Nombre: textoONil(recortarRunas(ficha.Nombre, 120)), Telefono: textoONil(recortarRunas(ficha.Telefono, 40))}
			case errors.Is(err, pgx.ErrNoRows):
				if a.Nombre != "" {
					entrada.Contacto = &EntradaContacto{Nombre: textoONil(recortarRunas(a.Nombre, 120))}
				}
			default:
				return TicketPortal{}, err
			}
			if in.ServicioID != nil {
				s, err := q.ServicioDeCliente(ctx, soportebd.ServicioDeClienteParams{ID: *in.ServicioID, ClienteID: a.ClienteID, EmpresaID: a.EmpresaID})
				if errors.Is(err, pgx.ErrNoRows) {
					return TicketPortal{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "servicioId", Message: "El servicio no es tuyo o no lo conocemos", Rule: "servicio"})
				}
				if err != nil {
					return TicketPortal{}, err
				}
				entrada.ServicioID = &s.ID
				if s.ZonaID != nil {
					entrada.ZonaID = s.ZonaID
				}
			}
			f, err := m.crearTicket(ctx, t, entrada, opcionesCrear{})
			if err != nil {
				return TicketPortal{}, err
			}
			return ticketPortal(ctx, q, f)
		},
	})
}

func textoONil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (m *Modulo) nuevoResponderTicketPortal() *auditoria.Caso[EntradaResponderTicketPortal, TicketPortalConMensaje] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResponderTicketPortal, TicketPortalConMensaje]{
		Accion: "portal.responder_ticket",
		Acceso: auditoria.Cliente,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResponderTicketPortal) (TicketPortalConMensaje, error) {
			q := soportebd.New(t)
			cuerpo, err := texto("cuerpo", in.Cuerpo, 1, 20000)
			if err != nil {
				return TicketPortalConMensaje{}, err
			}
			t.FijarEntidad("ticket", in.ID)
			// La misma operación repetida (clave reclamada) devuelve su mensaje sin repetir nada.
			idem := conIdempotencia(ctx, in.ClaveIdempotencia)
			if idem != nil {
				msj, err := q.MensajePorIdempotencia(ctx, soportebd.MensajePorIdempotenciaParams{TicketID: in.ID, Idempotencia: idem})
				if err == nil {
					f, err := leer(ctx, q, in.ID)
					if err != nil {
						return TicketPortalConMensaje{}, err
					}
					t.FijarEmpresa(deref(f.EmpresaID))
					t.Anotar(map[string]any{"repetida": true, "mensaje_id": msj.ID})
					tp, err := ticketPortal(ctx, q, f)
					return TicketPortalConMensaje{Ticket: tp, Mensaje: aMensajePortal(msj)}, err
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return TicketPortalConMensaje{}, err
				}
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsVer, in.Version)
			if err != nil {
				return TicketPortalConMensaje{}, err
			}
			if err := m.topeMensajes(ctx, q, contexto.ActorDe(ctx)); err != nil {
				return TicketPortalConMensaje{}, err
			}
			if err := exigirEstado(antes, "responder", EstadoNuevo, EstadoAbierto, EstadoEnEspera, EstadoResuelto); err != nil {
				return TicketPortalConMensaje{}, err
			}
			f, motivo, err := m.reactivarPorCliente(ctx, t, q, antes, "el portal")
			if err != nil {
				return TicketPortalConMensaje{}, err
			}
			msj, err := mensajeIdempotente(ctx, t, q, f, true, cuerpo, idem)
			if err != nil {
				return TicketPortalConMensaje{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, motivo); err != nil {
				return TicketPortalConMensaje{}, err
			}
			if err := t.Emitir("ticket.mensaje_cliente", map[string]any{"ticket_id": f.ID, "mensaje_id": msj.ID, "canal": "portal"}); err != nil {
				return TicketPortalConMensaje{}, err
			}
			tp, err := ticketPortal(ctx, q, f)
			return TicketPortalConMensaje{Ticket: tp, Mensaje: aMensajePortal(msj)}, err
		},
	})
}

func (m *Modulo) nuevoConfirmarSolucionPortal() *auditoria.Caso[EntradaConfirmarSolucion, TicketPortal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfirmarSolucion, TicketPortal]{
		Accion: "portal.confirmar_solucion",
		Acceso: auditoria.Cliente,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfirmarSolucion) (TicketPortal, error) {
			q := soportebd.New(t)
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsVer, &in.Version)
			if err != nil {
				return TicketPortal{}, err
			}
			if err := exigirEstado(antes, "confirmar la solución de", EstadoResuelto); err != nil {
				return TicketPortal{}, err
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return TicketPortal{}, err
			}
			f, err := guardar(ctx, t, q, antes, cerrar(antes, cal, m.ahora()), cal)
			if err != nil {
				return TicketPortal{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, ptr("El cliente confirmó la solución")); err != nil {
				return TicketPortal{}, err
			}
			return ticketPortal(ctx, q, f)
		},
	})
}

func nuevoListarCategoriasPortal() *auditoria.Caso[struct{}, nucleo.Pagina[CategoriaPortal]] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, nucleo.Pagina[CategoriaPortal]]{
		Accion:       "portal.ver_categorias",
		Acceso:       auditoria.Cliente,
		AuditarExito: soloLecturas[nucleo.Pagina[CategoriaPortal]],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (nucleo.Pagina[CategoriaPortal], error) {
			filas, err := soportebd.New(t).ListarCategoriasPortal(ctx, contexto.ActorDe(ctx).EmpresaID)
			if err != nil {
				return nucleo.Pagina[CategoriaPortal]{}, err
			}
			salida := make([]CategoriaPortal, 0, len(filas))
			for _, c := range filas {
				salida = append(salida, CategoriaPortal{ID: c.ID, Codigo: c.Codigo, Nombre: c.Nombre})
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

// Topes anti spam del portal (Config.Portal). Se cuentan en la base, por cliente y empresa de la
// sesión, con un advisory lock por cliente para que dos peticiones simultáneas no pasen las dos.

// LimitesPortal son los topes del portal. Cero = el valor por defecto.
type LimitesPortal struct {
	// TicketsAbiertos: tickets del portal del cliente en nuevo, abierto o en espera (10).
	TicketsAbiertos int
	// TicketsPorHora: tickets creados desde el portal en la última hora (20).
	TicketsPorHora int
	// MensajesPorMinuto: mensajes del cliente por el portal en el último minuto, en todos sus
	// tickets (10).
	MensajesPorMinuto int
}

func (l LimitesPortal) valores() (abiertos, porHora, porMinuto int64) {
	v := func(n, def int) int64 {
		if n <= 0 {
			return int64(def)
		}
		return int64(n)
	}
	return v(l.TicketsAbiertos, 10), v(l.TicketsPorHora, 20), v(l.MensajesPorMinuto, 10)
}

func demasiados(codigo, detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusTooManyRequests, codigo, "Demasiadas solicitudes", detalle)
}

// topeTickets: 429 demasiados_tickets si el cliente ya tiene el máximo de tickets del portal
// abiertos o creó el máximo en la última hora. Una creación repetida con la misma
// Idempotency-Key no cuenta.
func (m *Modulo) topeTickets(ctx context.Context, q *soportebd.Queries, a contexto.Actor, clave string) error {
	if idem := conIdempotencia(ctx, clave); idem != nil {
		if _, err := q.TicketPorIdempotencia(ctx, idem); err == nil {
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if err := q.BloquearIncidente(ctx, fmt.Sprintf("soporte.portal:%d:%d", a.EmpresaID, a.ClienteID)); err != nil {
		return err
	}
	c, err := q.ConteoTicketsPortal(ctx, soportebd.ConteoTicketsPortalParams{ClienteID: a.ClienteID, EmpresaID: a.EmpresaID})
	if err != nil {
		return err
	}
	abiertos, porHora, _ := m.cfg.Portal.valores()
	switch {
	case c.Abiertos >= abiertos:
		return demasiados("demasiados_tickets", fmt.Sprintf("Ya tienes %d tickets abiertos; escribe en uno de ellos o espera a que los atendamos", c.Abiertos))
	case c.UltimaHora >= porHora:
		return demasiados("demasiados_tickets", "Abriste demasiados tickets en la última hora; intenta más tarde")
	}
	return nil
}

// topeMensajes: 429 demasiados_mensajes si el cliente escribió el máximo de mensajes por el
// portal en el último minuto.
func (m *Modulo) topeMensajes(ctx context.Context, q *soportebd.Queries, a contexto.Actor) error {
	if err := q.BloquearIncidente(ctx, fmt.Sprintf("soporte.portal:%d:%d", a.EmpresaID, a.ClienteID)); err != nil {
		return err
	}
	n, err := q.MensajesPortalUltimoMinuto(ctx, soportebd.MensajesPortalUltimoMinutoParams{ClienteID: a.ClienteID, EmpresaID: a.EmpresaID})
	if err != nil {
		return err
	}
	if _, _, porMinuto := m.cfg.Portal.valores(); n >= porMinuto {
		return demasiados("demasiados_mensajes", "Enviaste demasiados mensajes en el último minuto; espera un momento")
	}
	return nil
}
