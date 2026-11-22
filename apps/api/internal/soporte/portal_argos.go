package soporte

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Tickets del portal del cliente de Argos (contrato 0.9.0 §11.3–§11.6, B8 y B13; §11.10, B28).
// La sesión (token_portal, empresa, cliente, contacto) ya viene resuelta por argos.SesionPortal;
// estos casos de uso exigen además el permiso portal_argos.* de la acción (solo lo tiene el
// token_portal) y auditan con en_nombre_de. El cliente ve lo mismo que en el portal de Iris: el
// ticket reducido y sus mensajes públicos, nunca notas internas, asignado, prioridad, SLA,
// historial ni datos de Horus. Lo que escribe queda como mensaje del cliente (canal portal_argos).

// OrigenPortalArgos: el ticket lo abrió el cliente desde el portal de Argos.
const OrigenPortalArgos = "portal_argos"

// CategoriaTicketArgos es la categoría del ticket en el portal de Argos.
type CategoriaTicketArgos struct {
	ID     int64  `json:"id"`
	Codigo string `json:"codigo"`
	Nombre string `json:"nombre"`
}

// TicketPortalArgos es el ticket del portal de Argos (§11.3, snake_case). La vista reducida (sesión
// ip, o un ticket del servicio que no abrió el contacto de la sesión) trae solo id, numero, estado,
// categoria, afectado_por_incidente y fechas.
type TicketPortalArgos struct {
	ID                   int64                 `json:"id"`
	Numero               string                `json:"numero" doc:"TK-000123"`
	Vista                string                `json:"vista" enum:"completa,reducida" doc:"reducida: sesión ip o ticket de otro en una sesión de contacto (sin asunto ni mensajes)"`
	Asunto               *string               `json:"asunto,omitempty" doc:"Solo en la vista completa"`
	Estado               string                `json:"estado" enum:"nuevo,abierto,en_espera,resuelto,cerrado"`
	Categoria            *CategoriaTicketArgos `json:"categoria" doc:"null si no tiene"`
	ServicioID           *int64                `json:"servicio_id,omitempty" nullable:"true" doc:"Solo en la vista completa"`
	Origen               *string               `json:"origen,omitempty" enum:"panel,portal,portal_argos,correo,telegram,horus,argos,api" doc:"Solo en la vista completa (informativo)"`
	AfectadoPorIncidente bool                  `json:"afectado_por_incidente" doc:"Ligado a una falla general de la red que sigue abierta"`
	PuedeConfirmar       *bool                 `json:"puede_confirmar,omitempty" doc:"Solo en la vista completa: está resuelto y se puede confirmar o responder"`
	AbiertoPorContacto   *string               `json:"abierto_por_contacto,omitempty" doc:"Vista del titular: nombre del contacto que lo abrió a cargo del servicio (§11.10; «Abierto por <nombre> (a cargo)»)"`
	ResueltoEn           *time.Time            `json:"resuelto_en" nullable:"true"`
	CerradoEn            *time.Time            `json:"cerrado_en" nullable:"true"`
	Version              *int32                `json:"version,omitempty" doc:"Solo en la vista completa"`
	CreadoEn             time.Time             `json:"creado_en"`
	ActualizadoEn        time.Time             `json:"actualizado_en"`
}

// AdjuntoMensajeArgos es un adjunto de un mensaje público.
type AdjuntoMensajeArgos struct {
	ID     uuid.UUID `json:"id"`
	Nombre string    `json:"nombre"`
	Mime   string    `json:"mime"`
	Tamano int64     `json:"tamano"`
}

// MensajePortalArgos es un mensaje público (§11.3).
type MensajePortalArgos struct {
	ID          int64                 `json:"id"`
	AutorTipo   string                `json:"autor_tipo" enum:"cliente,agente,sistema"`
	AutorNombre string                `json:"autor_nombre" doc:"agente: su nombre de pila; sistema: «Soporte»"`
	Cuerpo      string                `json:"cuerpo"`
	Adjuntos    []AdjuntoMensajeArgos `json:"adjuntos"`
	CreadoEn    time.Time             `json:"creado_en"`
}

// TicketYMensajeArgos es la respuesta de responder (§11.5).
type TicketYMensajeArgos struct {
	Ticket  TicketPortalArgos  `json:"ticket"`
	Mensaje MensajePortalArgos `json:"mensaje"`
}

// Entradas (el handler resuelve la sesión y la deja en Sesion) -------------------------------

// EntradaTicketsPortalArgos: GET …/tickets.
type EntradaTicketsPortalArgos struct {
	Sesion     argos.SesionPortal `json:"-"`
	Estados    []string           `json:"estados,omitempty"`
	ServicioID *int64             `json:"servicio_id,omitempty"`
	Cursor     string             `json:"-"`
	Limite     int                `json:"limit"`
}

// EntradaTicketPortalArgos: GET …/tickets/{ticket_id} y …/mensajes.
type EntradaTicketPortalArgos struct {
	Sesion   argos.SesionPortal `json:"-"`
	TicketID int64              `json:"ticket_id"`
	Cursor   string             `json:"-"`
	Limite   int                `json:"limit,omitempty"`
}

// EntradaAdjuntoPortalArgos: GET …/tickets/{ticket_id}/adjuntos/{adjunto_id}.
type EntradaAdjuntoPortalArgos struct {
	Sesion    argos.SesionPortal `json:"-"`
	TicketID  int64              `json:"ticket_id"`
	AdjuntoID uuid.UUID          `json:"adjunto_id"`
}

// AdjuntoDescargado es el archivo con su tipo real y su nombre saneado.
type AdjuntoDescargado struct {
	Nombre string
	Mime   string
	Datos  []byte
}

// CuerpoCrearTicketArgos es el JSON de POST …/tickets (§11.4).
type CuerpoCrearTicketArgos struct {
	Asunto      string `json:"asunto" minLength:"3" maxLength:"200"`
	Descripcion string `json:"descripcion" minLength:"1" maxLength:"20000" doc:"Primer mensaje público del cliente"`
	CategoriaID *int64 `json:"categoria_id,omitempty" nullable:"true" minimum:"1" doc:"Una de GET …/categorias"`
	ServicioID  *int64 `json:"servicio_id,omitempty" nullable:"true" minimum:"1" doc:"Servicio del cliente (obligatorio con un contacto, §11.10)"`
}

// CuerpoResponderArgos es el JSON de POST …/responder (§11.5).
type CuerpoResponderArgos struct {
	Cuerpo  string `json:"cuerpo" minLength:"1" maxLength:"20000"`
	Version *int32 `json:"version,omitempty" nullable:"true" minimum:"1" doc:"Si viene y cambió, 409 conflicto_version"`
}

// CuerpoConfirmarArgos es el JSON de POST …/confirmar-solucion (§11.5).
type CuerpoConfirmarArgos struct {
	Version int32 `json:"version" minimum:"1"`
}

// EntradaCrearTicketArgos: POST …/tickets.
type EntradaCrearTicketArgos struct {
	Sesion argos.SesionPortal `json:"-"`
	CuerpoCrearTicketArgos
	Adjuntos          int    `json:"adjuntos" doc:"Cuántos archivos trae (los bytes no se auditan)"`
	ClaveIdempotencia string `json:"-"`
	archivos          []archivoPortal
}

// EntradaResponderArgos: POST …/responder.
type EntradaResponderArgos struct {
	Sesion   argos.SesionPortal `json:"-"`
	TicketID int64              `json:"ticket_id"`
	CuerpoResponderArgos
	Adjuntos          int    `json:"adjuntos"`
	ClaveIdempotencia string `json:"-"`
	archivos          []archivoPortal
}

// EntradaConfirmarArgos: POST …/confirmar-solucion.
type EntradaConfirmarArgos struct {
	Sesion   argos.SesionPortal `json:"-"`
	TicketID int64              `json:"ticket_id"`
	CuerpoConfirmarArgos
}

// Vistas -------------------------------------------------------------------------------

// vistaCompleta: el titular con sesión de documento ve todo; la sesión ip, nada más que la lista
// reducida; un contacto, completos solo los que abrió él (§11.10).
func vistaCompleta(s argos.SesionPortal, f soportebd.SoporteTicket) bool {
	if s.EsIP() {
		return false
	}
	if s.Contacto != nil {
		return f.CreadoPorContacto != nil && *f.CreadoPorContacto == s.Contacto.ID
	}
	return true
}

// visibleEnPortal: el ticket es del cliente y de la empresa de la sesión y, con un contacto, de uno
// de sus servicios (los que no tienen servicio nunca se le muestran).
func visibleEnPortal(s argos.SesionPortal, f soportebd.SoporteTicket) bool {
	if f.ClienteID == nil || *f.ClienteID != s.ClienteID || f.EmpresaID == nil || *f.EmpresaID != s.EmpresaID {
		return false
	}
	if s.Contacto != nil {
		return f.ServicioID != nil && s.Contacto.CubreServicio(*f.ServicioID)
	}
	return true
}

func aTicketArgos(s argos.SesionPortal, f soportebd.SoporteTicket, catNombre, catCodigo *string, padreAbierto bool, contactoNombre *string) TicketPortalArgos {
	v := TicketPortalArgos{ID: f.ID, Numero: f.Numero, Vista: "reducida", Estado: f.Estado, AfectadoPorIncidente: padreAbierto,
		ResueltoEn: f.ResueltoEn, CerradoEn: f.CerradoEn, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt}
	if f.CategoriaID != nil && catNombre != nil {
		v.Categoria = &CategoriaTicketArgos{ID: *f.CategoriaID, Nombre: *catNombre, Codigo: valorTexto(catCodigo)}
	}
	if !vistaCompleta(s, f) {
		return v
	}
	puede := f.Estado == EstadoResuelto
	v.Vista, v.Asunto, v.ServicioID, v.Origen, v.PuedeConfirmar, v.Version = "completa", &f.Asunto, f.ServicioID, &f.Origen, &puede, &f.Version
	if s.Contacto == nil && f.CreadoPorContacto != nil {
		nombre := "un contacto"
		if contactoNombre != nil && strings.TrimSpace(*contactoNombre) != "" {
			nombre = *contactoNombre
		}
		v.AbiertoPorContacto = &nombre
	}
	return v
}

func valorTexto(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func ticketArgos(ctx context.Context, q *soportebd.Queries, s argos.SesionPortal, f soportebd.SoporteTicket) (TicketPortalArgos, error) {
	d, err := q.DatosTicketPortal(ctx, f.ID)
	if err != nil {
		return TicketPortalArgos{}, err
	}
	return aTicketArgos(s, f, d.CategoriaNombre, d.CategoriaCodigo, d.PadreAbierto, d.ContactoNombre), nil
}

// nombreDePila: el agente se muestra solo con su nombre de pila (§11.3).
func nombreDePila(n string) string {
	if c := strings.Fields(n); len(c) > 0 {
		return c[0]
	}
	return "Soporte"
}

func mensajesArgos(ctx context.Context, q *soportebd.Queries, filas []soportebd.SoporteTicketMensaje) ([]MensajePortalArgos, error) {
	var ids []uuid.UUID
	for _, m := range filas {
		ids = append(ids, m.Adjuntos...)
	}
	meta := map[uuid.UUID]AdjuntoMensajeArgos{}
	if len(ids) > 0 {
		as, err := q.AdjuntosPorIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, a := range as {
			nombre := "adjunto"
			if a.Nombre != nil {
				nombre = *a.Nombre
			}
			meta[a.ID] = AdjuntoMensajeArgos{ID: a.ID, Nombre: nombre, Mime: a.Mime, Tamano: a.Tamano}
		}
	}
	salida := make([]MensajePortalArgos, 0, len(filas))
	for _, m := range filas {
		salida = append(salida, mensajeArgos(m, meta))
	}
	return salida, nil
}

func mensajeArgos(m soportebd.SoporteTicketMensaje, meta map[uuid.UUID]AdjuntoMensajeArgos) MensajePortalArgos {
	autor, nombre := m.AutorTipo, m.AutorNombre
	switch autor {
	case "usuario":
		autor, nombre = "agente", nombreDePila(m.AutorNombre)
	case "sistema":
		nombre = "Soporte"
	}
	adj := []AdjuntoMensajeArgos{}
	for _, id := range m.Adjuntos {
		if a, ok := meta[id]; ok {
			adj = append(adj, a)
		}
	}
	return MensajePortalArgos{ID: m.ID, AutorTipo: autor, AutorNombre: nombre, Cuerpo: m.Cuerpo, Adjuntos: adj, CreadoEn: m.CreatedAt}
}

// leerEnPortal lee el ticket (con candado si paraActualizar) y lo oculta (404) si no es visible
// para la sesión.
func leerEnPortal(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, s argos.SesionPortal, id int64, paraActualizar bool) (soportebd.SoporteTicket, error) {
	t.FijarEntidad("ticket", id)
	var f soportebd.SoporteTicket
	var err error
	if paraActualizar {
		f, err = q.TicketPorIDParaActualizar(ctx, id)
	} else {
		f, err = q.TicketPorID(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !visibleEnPortal(s, f)) {
		return f, noEncontrado()
	}
	return f, err
}

// exigirCompleto: mensajes, adjuntos y escrituras solo sobre un ticket que la sesión ve completo.
func exigirCompleto(s argos.SesionPortal, f soportebd.SoporteTicket, escritura bool) error {
	if vistaCompleta(s, f) {
		return nil
	}
	if escritura && s.Contacto != nil {
		return argos.PermisoDeContacto("El contacto solo escribe en los tickets que abrió")
	}
	return noEncontrado()
}

// preparar: la entrada común de los casos (en_nombre_de en la auditoría, empresa, nombre del actor).
func prepararPortalArgos(t *auditoria.Tx, s argos.SesionPortal) {
	t.FijarEmpresa(s.EmpresaID)
	t.Anotar(map[string]any{"en_nombre_de": s.EnNombreDe()})
}

// claveDelCliente: la Idempotency-Key se guarda por credencial y cliente (§11.2).
func claveDelCliente(s argos.SesionPortal, clave string) string {
	if clave == "" {
		return ""
	}
	return fmt.Sprintf("portal_argos:%d:%d:%s", s.EmpresaID, s.ClienteID, clave)
}

// Casos de uso ----------------------------------------------------------------------------

func nuevoCategoriasPortalArgos() *auditoria.Caso[argos.SesionPortal, nucleo.Pagina[CategoriaPortal]] {
	return auditoria.Nuevo(auditoria.Caso[argos.SesionPortal, nucleo.Pagina[CategoriaPortal]]{
		Accion: "portal_argos.ver_tickets", Permiso: nucleo.PermPortalArgosVerTickets,
		AuditarExito: soloLecturas[nucleo.Pagina[CategoriaPortal]],
		Logica: func(ctx context.Context, t *auditoria.Tx, s argos.SesionPortal) (nucleo.Pagina[CategoriaPortal], error) {
			prepararPortalArgos(t, s)
			filas, err := soportebd.New(t).ListarCategoriasPortal(ctx, s.EmpresaID)
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

func (m *Modulo) nuevoTicketsPortalArgos() *auditoria.Caso[EntradaTicketsPortalArgos, nucleo.Pagina[TicketPortalArgos]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTicketsPortalArgos, nucleo.Pagina[TicketPortalArgos]]{
		Accion: "portal_argos.ver_tickets", Permiso: nucleo.PermPortalArgosVerTickets,
		AuditarExito: soloLecturas[nucleo.Pagina[TicketPortalArgos]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTicketsPortalArgos) (nucleo.Pagina[TicketPortalArgos], error) {
			s := in.Sesion
			prepararPortalArgos(t, s)
			var cero nucleo.Pagina[TicketPortalArgos]
			if err := s.ExigirClienteValidado(); err != nil {
				return cero, err
			}
			if s.Contacto != nil {
				// §11.10: con un contacto, el servicio es obligatorio y debe ser suyo.
				if in.ServicioID == nil {
					return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "servicio_id", Message: "Con un contacto, indica el servicio", Rule: "required"})
				}
				if !s.Contacto.CubreServicio(*in.ServicioID) {
					return cero, argos.ServicioFueraDelContacto()
				}
			}
			for _, e := range in.Estados {
				switch e {
				case EstadoNuevo, EstadoAbierto, EstadoEnEspera, EstadoResuelto, EstadoCerrado:
				default:
					return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "estado", Message: "Estado desconocido", Rule: "enum"})
				}
			}
			pagina := nucleo.ParamsPagina{Cursor: in.Cursor, Limit: in.Limite}
			var antesDe int64
			hay, err := nucleo.DecodificarCursor(in.Cursor, &antesDe)
			if err != nil {
				return cero, err
			}
			p := soportebd.ListarTicketsClienteParams{ClienteID: s.ClienteID, EmpresaID: s.EmpresaID, Estados: noNulo(in.Estados),
				ServicioID: in.ServicioID, Limite: int32(pagina.Limite() + 1)} //nolint:gosec // ≤ 201
			if hay {
				p.AntesDe = &antesDe
			}
			filas, err := soportebd.New(t).ListarTicketsCliente(ctx, p)
			if err != nil {
				return cero, err
			}
			return nucleo.Paginar(filas, pagina.Limite(),
				func(f soportebd.ListarTicketsClienteRow) TicketPortalArgos {
					return aTicketArgos(s, f.SoporteTicket, f.CategoriaNombre, f.CategoriaCodigo, f.PadreAbierto, f.ContactoNombre)
				},
				func(f soportebd.ListarTicketsClienteRow) []any { return []any{f.SoporteTicket.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoVerTicketPortalArgos() *auditoria.Caso[EntradaTicketPortalArgos, TicketPortalArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTicketPortalArgos, TicketPortalArgos]{
		Accion: "portal_argos.ver_tickets", Permiso: nucleo.PermPortalArgosVerTickets,
		AuditarExito: soloLecturas[TicketPortalArgos],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTicketPortalArgos) (TicketPortalArgos, error) {
			s := in.Sesion
			prepararPortalArgos(t, s)
			if err := s.ExigirDocumento(); err != nil {
				return TicketPortalArgos{}, err
			}
			if err := s.ExigirClienteValidado(); err != nil {
				return TicketPortalArgos{}, err
			}
			q := soportebd.New(t)
			f, err := leerEnPortal(ctx, t, q, s, in.TicketID, false)
			if err != nil {
				return TicketPortalArgos{}, err
			}
			return ticketArgos(ctx, q, s, f)
		},
	})
}

func (m *Modulo) nuevoMensajesPortalArgos() *auditoria.Caso[EntradaTicketPortalArgos, nucleo.Pagina[MensajePortalArgos]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTicketPortalArgos, nucleo.Pagina[MensajePortalArgos]]{
		Accion: "portal_argos.ver_tickets", Permiso: nucleo.PermPortalArgosVerTickets,
		AuditarExito: soloLecturas[nucleo.Pagina[MensajePortalArgos]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTicketPortalArgos) (nucleo.Pagina[MensajePortalArgos], error) {
			s := in.Sesion
			prepararPortalArgos(t, s)
			var cero nucleo.Pagina[MensajePortalArgos]
			if err := s.ExigirDocumento(); err != nil {
				return cero, err
			}
			if err := s.ExigirClienteValidado(); err != nil {
				return cero, err
			}
			q := soportebd.New(t)
			f, err := leerEnPortal(ctx, t, q, s, in.TicketID, false)
			if err != nil {
				return cero, err
			}
			if err := exigirCompleto(s, f, false); err != nil {
				return cero, err
			}
			pagina := nucleo.ParamsPagina{Cursor: in.Cursor, Limit: in.Limite}
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return cero, err
			}
			filas, err := q.ListarMensajesPublicos(ctx, soportebd.ListarMensajesPublicosParams{
				TicketID: f.ID, DespuesDe: despues, Limite: int32(pagina.Limite() + 1), //nolint:gosec // ≤ 201
			})
			if err != nil {
				return cero, err
			}
			corte := filas
			if len(corte) > pagina.Limite() {
				corte = corte[:pagina.Limite()]
			}
			msjs, err := mensajesArgos(ctx, q, corte)
			if err != nil {
				return cero, err
			}
			i := 0
			return nucleo.Paginar(filas, pagina.Limite(),
				func(soportebd.SoporteTicketMensaje) MensajePortalArgos { m := msjs[i]; i++; return m },
				func(f soportebd.SoporteTicketMensaje) []any { return []any{f.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoAdjuntoPortalArgos() *auditoria.Caso[EntradaAdjuntoPortalArgos, AdjuntoDescargado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAdjuntoPortalArgos, AdjuntoDescargado]{
		Accion: "portal_argos.ver_tickets", Permiso: nucleo.PermPortalArgosVerTickets,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAdjuntoPortalArgos) (AdjuntoDescargado, error) {
			s := in.Sesion
			prepararPortalArgos(t, s)
			if err := s.ExigirDocumento(); err != nil {
				return AdjuntoDescargado{}, err
			}
			if err := s.ExigirClienteValidado(); err != nil {
				return AdjuntoDescargado{}, err
			}
			q := soportebd.New(t)
			f, err := leerEnPortal(ctx, t, q, s, in.TicketID, false)
			if err != nil {
				return AdjuntoDescargado{}, err
			}
			if err := exigirCompleto(s, f, false); err != nil {
				return AdjuntoDescargado{}, err
			}
			t.Anotar(map[string]any{"adjunto_id": in.AdjuntoID})
			a, err := q.AdjuntoDeMensajePublico(ctx, soportebd.AdjuntoDeMensajePublicoParams{AdjuntoID: in.AdjuntoID, TicketID: f.ID})
			if errors.Is(err, pgx.ErrNoRows) {
				return AdjuntoDescargado{}, nucleo.NoEncontrado("El adjunto no existe")
			}
			if err != nil {
				return AdjuntoDescargado{}, err
			}
			datos, _, err := adjuntos.Contenido(ctx, m.cfg.Almacen, t, a.ID, a.Entidad, a.EntidadID, MaxTamanoAdjuntoPortal*2)
			if err != nil {
				return AdjuntoDescargado{}, err
			}
			nombre := "adjunto"
			if a.Nombre != nil {
				nombre = *a.Nombre
			}
			return AdjuntoDescargado{Nombre: nombre, Mime: a.Mime, Datos: datos}, nil
		},
	})
}

// guardarArchivos guarda los adjuntos del mensaje (entidad ticket) y devuelve sus ids.
func (m *Modulo) guardarArchivos(ctx context.Context, t *auditoria.Tx, f soportebd.SoporteTicket, archivos []archivoPortal) ([]uuid.UUID, error) {
	if len(archivos) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(archivos))
	for _, a := range archivos {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		nombre := a.Nombre
		if _, err := adjuntos.GuardarCompleto(ctx, t, m.cfg.Almacen, id, adjuntos.EntradaCompleto{EmpresaID: f.EmpresaID,
			Entidad: "ticket", EntidadID: fmt.Sprint(f.ID), Tipo: a.Tipo, Mime: a.Mime, Datos: a.Datos, Nombre: &nombre}); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// exigirArchivos: con una sesión ip no hay adjuntos (§11.2) y sin almacenamiento no se aceptan.
func (m *Modulo) exigirArchivos(s argos.SesionPortal, n int) error {
	if n == 0 {
		return nil
	}
	if s.EsIP() {
		return nucleo.ReglaNegocio("adjuntos_no_permitidos", "Con una sesión abierta por la IP no se envían adjuntos")
	}
	if m.cfg.Almacen == nil {
		return nucleo.NoDisponible("Iris no tiene almacenamiento de adjuntos configurado")
	}
	return nil
}

// mensajeDelCliente: el mensaje que escribió el cliente (o su contacto) desde el portal de Argos.
func mensajeDelCliente(ctx context.Context, q *soportebd.Queries, s argos.SesionPortal, f soportebd.SoporteTicket, nombre, cuerpo string, idem *string, adj []uuid.UUID) (soportebd.SoporteTicketMensaje, error) {
	p := soportebd.CrearMensajeParams{TicketID: f.ID, EmpresaID: f.EmpresaID, Publico: true, AutorTipo: "cliente",
		AutorNombre: nombre, Canal: OrigenPortalArgos, Cuerpo: cuerpo, Idempotencia: idem, Adjuntos: adj}
	if s.Contacto != nil {
		p.ContactoID = &s.Contacto.ID
		p.AutorNombre = s.Contacto.Nombre
	}
	return q.CrearMensaje(ctx, p)
}

// topesPortalArgos: los del cliente (compartidos con /soporte) y, con un contacto, los suyos.
func (m *Modulo) topesTicketsArgos(ctx context.Context, q *soportebd.Queries, s argos.SesionPortal, clave string) error {
	if err := m.topeTicketsDe(ctx, q, s.EmpresaID, s.ClienteID, clave); err != nil {
		return err
	}
	if s.Contacto == nil {
		return nil
	}
	c, err := q.ConteoTicketsContacto(ctx, soportebd.ConteoTicketsContactoParams{ContactoID: s.Contacto.ID, EmpresaID: s.EmpresaID})
	if err != nil {
		return err
	}
	abiertos, porHora, _ := m.cfg.Portal.valores()
	if c.Abiertos >= abiertos || c.UltimaHora >= porHora {
		return demasiados("demasiados_tickets", "El contacto llegó al tope de tickets; escribe en uno abierto o intenta más tarde")
	}
	return nil
}

func (m *Modulo) topesMensajesArgos(ctx context.Context, q *soportebd.Queries, s argos.SesionPortal) error {
	if err := m.topeMensajesDe(ctx, q, s.EmpresaID, s.ClienteID); err != nil {
		return err
	}
	if s.Contacto == nil {
		return nil
	}
	n, err := q.MensajesContactoUltimoMinuto(ctx, soportebd.MensajesContactoUltimoMinutoParams{ContactoID: s.Contacto.ID, EmpresaID: s.EmpresaID})
	if err != nil {
		return err
	}
	if _, _, porMinuto := m.cfg.Portal.valores(); n >= porMinuto {
		return demasiados("demasiados_mensajes", "Enviaste demasiados mensajes en el último minuto; espera un momento")
	}
	return nil
}

func (m *Modulo) nuevoCrearTicketPortalArgos() *auditoria.Caso[EntradaCrearTicketArgos, TicketPortalArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearTicketArgos, TicketPortalArgos]{
		Accion: "portal_argos.crear_ticket", Permiso: nucleo.PermPortalArgosCrearTicket,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearTicketArgos) (TicketPortalArgos, error) {
			s := in.Sesion
			prepararPortalArgos(t, s)
			var cero TicketPortalArgos
			q := soportebd.New(t)
			if err := m.exigirArchivos(s, len(in.archivos)); err != nil {
				return cero, err
			}
			descripcion, err := texto("descripcion", in.Descripcion, 1, 20000)
			if err != nil {
				return cero, err
			}
			if s.Contacto != nil {
				if !s.Contacto.Reportar {
					return cero, argos.PermisoDeContacto("El contacto no tiene permiso de reportar")
				}
				if in.ServicioID == nil {
					return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "servicio_id", Message: "Con un contacto, indica el servicio", Rule: "required"})
				}
				if !s.Contacto.CubreServicio(*in.ServicioID) {
					return cero, argos.ServicioFueraDelContacto()
				}
			}
			if in.CategoriaID != nil {
				c, err := q.CategoriaPorID(ctx, *in.CategoriaID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!c.VisiblePortal || !c.Activa ||
					(c.EmpresaID != nil && *c.EmpresaID != s.EmpresaID))) {
					return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "categoria_id", Message: "Elige una categoría de la lista", Rule: "categoria"})
				}
				if err != nil {
					return cero, err
				}
			}
			clave := claveDelCliente(s, in.ClaveIdempotencia)
			if err := m.topesTicketsArgos(ctx, q, s, clave); err != nil {
				return cero, err
			}
			entrada := EntradaCrearTicket{ClienteID: &s.ClienteID, CategoriaID: in.CategoriaID, Prioridad: "media", Origen: OrigenPortalArgos,
				Asunto: in.Asunto, ClaveIdempotencia: clave}
			// Contacto y zona desde la ficha de Argos (si Iris la tiene); nunca el teléfono de un contacto.
			ficha, err := q.ClienteDeArgos(ctx, soportebd.ClienteDeArgosParams{ID: s.ClienteID, EmpresaID: s.EmpresaID})
			nombre := ""
			switch {
			case err == nil:
				nombre = recortarRunas(ficha.Nombre, 120)
				entrada.ZonaID = ficha.ZonaID
				entrada.Contacto = &EntradaContacto{Nombre: textoONil(nombre), Telefono: textoONil(recortarRunas(ficha.Telefono, 40))}
			case !errors.Is(err, pgx.ErrNoRows):
				return cero, err
			}
			if in.ServicioID != nil {
				srv, err := q.ServicioDeCliente(ctx, soportebd.ServicioDeClienteParams{ID: *in.ServicioID, ClienteID: s.ClienteID, EmpresaID: s.EmpresaID})
				if errors.Is(err, pgx.ErrNoRows) {
					return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "servicio_id", Message: "El servicio no es del cliente", Rule: "servicio"})
				}
				if err != nil {
					return cero, err
				}
				entrada.ServicioID = &srv.ID
				if srv.ZonaID != nil {
					entrada.ZonaID = srv.ZonaID
				}
			}
			if entrada.ZonaID, err = agenda.ZonaDeArgos(ctx, t, entrada.ZonaID, &s.EmpresaID); err != nil {
				return cero, err
			}
			repetida := false
			f, err := m.crearTicket(ctx, t, entrada, opcionesCrear{portal: &s, repetida: &repetida})
			if err != nil {
				return cero, err
			}
			if !repetida {
				adj, err := m.guardarArchivos(ctx, t, f, in.archivos)
				if err != nil {
					return cero, err
				}
				if _, err := mensajeDelCliente(ctx, q, s, f, nombre, descripcion, nil, adj); err != nil {
					return cero, err
				}
				var revision *string
				if !s.ClienteValidado {
					revision = ptr("pendiente")
				}
				var contacto *int64
				if s.Contacto != nil {
					contacto = &s.Contacto.ID
				}
				if contacto != nil || revision != nil {
					if err := q.MarcarTicketPortalArgos(ctx, soportebd.MarcarTicketPortalArgosParams{ID: f.ID, ContactoID: contacto, ClienteRevision: revision}); err != nil {
						return cero, err
					}
					f.CreadoPorContacto, f.ClienteRevision = contacto, revision
				}
			}
			return ticketArgos(ctx, q, s, f)
		},
	})
}

func (m *Modulo) nuevoResponderPortalArgos() *auditoria.Caso[EntradaResponderArgos, TicketYMensajeArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResponderArgos, TicketYMensajeArgos]{
		Accion: "portal_argos.responder_ticket", Permiso: nucleo.PermPortalArgosResponderTicket,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResponderArgos) (TicketYMensajeArgos, error) {
			s := in.Sesion
			prepararPortalArgos(t, s)
			var cero TicketYMensajeArgos
			if err := s.ExigirDocumento(); err != nil {
				return cero, err
			}
			if err := s.ExigirClienteValidado(); err != nil {
				return cero, err
			}
			if err := m.exigirArchivos(s, len(in.archivos)); err != nil {
				return cero, err
			}
			cuerpo, err := texto("cuerpo", in.Cuerpo, 1, 20000)
			if err != nil {
				return cero, err
			}
			q := soportebd.New(t)
			idem := conIdempotencia(ctx, claveDelCliente(s, in.ClaveIdempotencia))
			if idem != nil {
				msj, err := q.MensajePorIdempotencia(ctx, soportebd.MensajePorIdempotenciaParams{TicketID: in.TicketID, Idempotencia: idem})
				if err == nil {
					f, err := leerEnPortal(ctx, t, q, s, in.TicketID, false)
					if err != nil {
						return cero, err
					}
					t.Anotar(map[string]any{"repetida": true, "mensaje_id": msj.ID})
					return respuestaArgos(ctx, q, s, f, msj)
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return cero, err
				}
			}
			antes, err := leerEnPortal(ctx, t, q, s, in.TicketID, true)
			if err != nil {
				return cero, err
			}
			if err := exigirCompleto(s, antes, true); err != nil {
				return cero, err
			}
			if s.Contacto != nil && !s.Contacto.Reportar {
				return cero, argos.PermisoDeContacto("El contacto no tiene permiso de reportar")
			}
			if in.Version != nil {
				if err := nucleo.ExigirVersion(*in.Version, antes.Version, "ticket", antes.ID); err != nil {
					return cero, err
				}
			}
			if err := m.topesMensajesArgos(ctx, q, s); err != nil {
				return cero, err
			}
			if err := exigirEstado(antes, "responder", EstadoNuevo, EstadoAbierto, EstadoEnEspera, EstadoResuelto); err != nil {
				return cero, err
			}
			f, motivo, err := m.reactivarPorCliente(ctx, t, q, antes, "el portal de Argos")
			if err != nil {
				return cero, err
			}
			adj, err := m.guardarArchivos(ctx, t, f, in.archivos)
			if err != nil {
				return cero, err
			}
			nombre := ""
			if ficha, err := q.ClienteDeArgos(ctx, soportebd.ClienteDeArgosParams{ID: s.ClienteID, EmpresaID: s.EmpresaID}); err == nil {
				nombre = recortarRunas(ficha.Nombre, 120)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return cero, err
			}
			msj, err := mensajeDelCliente(ctx, q, s, f, nombre, cuerpo, idem, adj)
			if err != nil {
				return cero, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, motivo); err != nil {
				return cero, err
			}
			if err := t.Emitir("ticket.mensaje_cliente", map[string]any{"ticket_id": f.ID, "mensaje_id": msj.ID, "canal": OrigenPortalArgos}); err != nil {
				return cero, err
			}
			return respuestaArgos(ctx, q, s, f, msj)
		},
	})
}

func respuestaArgos(ctx context.Context, q *soportebd.Queries, s argos.SesionPortal, f soportebd.SoporteTicket, msj soportebd.SoporteTicketMensaje) (TicketYMensajeArgos, error) {
	tk, err := ticketArgos(ctx, q, s, f)
	if err != nil {
		return TicketYMensajeArgos{}, err
	}
	ms, err := mensajesArgos(ctx, q, []soportebd.SoporteTicketMensaje{msj})
	if err != nil {
		return TicketYMensajeArgos{}, err
	}
	return TicketYMensajeArgos{Ticket: tk, Mensaje: ms[0]}, nil
}

func (m *Modulo) nuevoConfirmarPortalArgos() *auditoria.Caso[EntradaConfirmarArgos, TicketPortalArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfirmarArgos, TicketPortalArgos]{
		Accion: "portal_argos.confirmar_solucion", Permiso: nucleo.PermPortalArgosConfirmarSolucion,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfirmarArgos) (TicketPortalArgos, error) {
			s := in.Sesion
			prepararPortalArgos(t, s)
			var cero TicketPortalArgos
			if err := s.ExigirDocumento(); err != nil {
				return cero, err
			}
			if err := s.ExigirClienteValidado(); err != nil {
				return cero, err
			}
			q := soportebd.New(t)
			antes, err := leerEnPortal(ctx, t, q, s, in.TicketID, true)
			if err != nil {
				return cero, err
			}
			if err := exigirCompleto(s, antes, true); err != nil {
				return cero, err
			}
			if s.Contacto != nil && !s.Contacto.Reportar {
				return cero, argos.PermisoDeContacto("El contacto no tiene permiso de reportar")
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "ticket", antes.ID); err != nil {
				return cero, err
			}
			if err := exigirEstado(antes, "confirmar la solución de", EstadoResuelto); err != nil {
				return cero, err
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return cero, err
			}
			f, err := guardar(ctx, t, q, antes, cerrar(antes, cal, m.ahora()), cal)
			if err != nil {
				return cero, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, ptr("El cliente confirmó la solución en el portal de Argos")); err != nil {
				return cero, err
			}
			return ticketArgos(ctx, q, s, f)
		},
	})
}

// contextoPortalArgos: el actor queda con el nombre de §11.1 («Argos (portal) · cliente <id>»), que
// va al historial del ticket y a la auditoría.
func contextoPortalArgos(ctx context.Context, s argos.SesionPortal) context.Context {
	a := contexto.ActorDe(ctx)
	a.Nombre = s.NombreActor()
	return contexto.ConActor(ctx, a, contexto.PermisosDe(ctx))
}
