package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Solicitudes de servicio del portal del cliente de Argos (contrato 0.9.0 §11.7, B10). En Iris una
// solicitud es un prospecto de ventas con origen portal_argos, del cliente de Argos ya conocido
// (cliente_existente_id): un asesor la revisa, cotiza y la convierte en orden. «Ganar» nunca crea
// otro cliente en Argos: una alta solo pide POST /servicios; cambio de plan, traslado y otro no
// llaman a Argos (una persona lo aplica allá hasta A34). El cliente ve solo estado_cliente (nunca el
// vendedor, las actividades, la factibilidad, la cotización ni el motivo de pérdida); cada cambio
// emite solicitud_servicio.estado_cambiado, que va a Argos (§6.6).

// OrigenPortalArgos: la pidió el cliente desde el portal de Argos.
const OrigenPortalArgos = "portal_argos"

// Tipos de solicitud (§11.7).
const (
	SolicitudAlta       = "alta"
	SolicitudCambioPlan = "cambio_plan"
	SolicitudTraslado   = "traslado"
	SolicitudOtro       = "otro"
)

// TopeSolicitudesDia: solicitudes por cliente y empresa en el último día (§11.2).
const TopeSolicitudesDia = 5

// EventoSolicitudEstado es el evento (y el webhook a Argos) del cambio de estado_cliente.
const EventoSolicitudEstado = "solicitud_servicio.estado_cambiado"

// SolicitudServicio es lo que ve el cliente (§11.7).
type SolicitudServicio struct {
	ID            int64     `json:"id"`
	Numero        string    `json:"numero" doc:"PR-000731"`
	Tipo          string    `json:"tipo" enum:"alta,cambio_plan,traslado,otro"`
	ServicioID    *int64    `json:"servicio_id" nullable:"true"`
	EstadoCliente string    `json:"estado_cliente" enum:"recibida,en_revision,cotizada,en_proceso,completada,no_procede"`
	Comentario    *string   `json:"comentario" nullable:"true"`
	CreadoEn      time.Time `json:"creado_en"`
	ActualizadoEn time.Time `json:"actualizado_en"`
}

func aSolicitud(f comercialbd.ComercialProspecto) SolicitudServicio {
	return SolicitudServicio{ID: f.ID, Numero: f.Numero, Tipo: deref(f.SolicitudTipo), ServicioID: f.SolicitudServicioID,
		EstadoCliente: deref(f.EstadoCliente), Comentario: f.SolicitudComentario, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt}
}

// CuerpoSolicitud es el JSON de POST …/solicitudes (§11.7).
type CuerpoSolicitud struct {
	Tipo             string  `json:"tipo" enum:"alta,cambio_plan,traslado,otro"`
	ServicioID       *int64  `json:"servicio_id,omitempty" nullable:"true" minimum:"1" doc:"Obligatorio en cambio_plan y traslado (422 servicio_requerido); no va en alta"`
	Direccion        *string `json:"direccion,omitempty" nullable:"true" maxLength:"300" doc:"Obligatoria en alta y traslado; en cambio_plan, la del servicio"`
	Coordenadas      *string `json:"coordenadas,omitempty" nullable:"true" maxLength:"60" doc:"\"lat,lng\""`
	PlanInteresID    *int64  `json:"plan_interes_id,omitempty" nullable:"true" minimum:"1"`
	Comentario       *string `json:"comentario,omitempty" nullable:"true" maxLength:"2000"`
	TelefonoContacto *string `json:"telefono_contacto,omitempty" nullable:"true" maxLength:"32" doc:"Regla de §3.2; si falta, el de la ficha"`
}

// EntradaCrearSolicitud: POST …/solicitudes.
type EntradaCrearSolicitud struct {
	Sesion argos.SesionPortal `json:"-"`
	CuerpoSolicitud
	ClaveIdempotencia string `json:"-"`
	lat, lon          *float64
	// planNoValidado: el plan que pidió el cliente cuando Argos no respondió (5xx) al validarlo: la
	// solicitud se guarda sin plan y con la nota «plan no validado».
	planNoValidado *int64
}

// MarshalJSON de la auditoría: sin dirección, comentario ni teléfono.
func (e EntradaCrearSolicitud) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"tipo": e.Tipo, "servicio_id": e.ServicioID, "plan_interes_id": e.PlanInteresID,
		"con_direccion": e.Direccion != nil, "con_coordenadas": e.Coordenadas != nil, "con_comentario": e.Comentario != nil,
		"con_telefono": e.TelefonoContacto != nil, "plan_no_validado": e.planNoValidado})
}

// EntradaSolicitudes: GET …/solicitudes.
type EntradaSolicitudes struct {
	Sesion  argos.SesionPortal `json:"-"`
	Estados []string           `json:"estados,omitempty"`
	Cursor  string             `json:"-"`
	Limite  int                `json:"limit"`
}

// EntradaVerSolicitud: GET …/solicitudes/{solicitud_id}.
type EntradaVerSolicitud struct {
	Sesion      argos.SesionPortal `json:"-"`
	SolicitudID int64              `json:"solicitud_id"`
}

// exigirTitular: las solicitudes son solo del titular con sesión de documento y el cliente confirmado.
func exigirTitular(s argos.SesionPortal) error {
	if s.Contacto != nil {
		return nucleo.NuevoProblema(http.StatusForbidden, "solo_titular", "Solo el titular",
			"Las solicitudes de servicio las pide y las ve solo el titular")
	}
	if err := s.ExigirDocumento(); err != nil {
		return err
	}
	return s.ExigirClienteValidado()
}

func prepararSolicitud(t *auditoria.Tx, s argos.SesionPortal) {
	t.FijarEmpresa(s.EmpresaID)
	t.Anotar(map[string]any{"en_nombre_de": s.EnNombreDe()})
}

func noEncontradaSolicitud() *nucleo.Problema { return nucleo.NoEncontrado("La solicitud no existe") }

// EstadoClienteDe: lo que ve el cliente según el estado del prospecto (y si su orden terminó).
func EstadoClienteDe(estado string, tipo string, ordenID *int64, completada bool) string {
	switch estado {
	case EstadoNuevo:
		return "recibida"
	case EstadoContactado, EstadoFactibilidad:
		return "en_revision"
	case EstadoCotizado:
		return "cotizada"
	case EstadoContrato:
		return "en_proceso"
	case EstadoGanado:
		// En proceso hasta que termina: una alta, al cerrarse bien su orden; cambio de plan, traslado u
		// otro, al marcar hecha la tarea «Abrir en Argos» (o al cerrarse bien su orden, si el asesor abrió
		// una).
		if completada {
			return "completada"
		}
		return "en_proceso"
	case EstadoPerdido:
		return "no_procede"
	}
	return "en_revision"
}

// sincronizarEstadoCliente recalcula estado_cliente y emite el evento si cambió. Lo llaman
// cambioDeEstado (cualquier cambio del prospecto) y el cierre de su orden.
func sincronizarEstadoCliente(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, f comercialbd.ComercialProspecto, completada bool) error {
	if f.SolicitudTipo == nil {
		return nil
	}
	nuevo := EstadoClienteDe(f.Estado, *f.SolicitudTipo, f.OrdenID, completada || deref(f.TareaArgos) == "hecha")
	anterior := deref(f.EstadoCliente)
	if nuevo == anterior {
		return nil
	}
	if err := q.FijarEstadoCliente(ctx, comercialbd.FijarEstadoClienteParams{ID: f.ID, EstadoCliente: &nuevo}); err != nil {
		return err
	}
	t.RegistrarCambios("prospecto", f.ID, map[string]any{"estado_cliente": anterior}, map[string]any{"estado_cliente": nuevo})
	// Sin datos personales ni motivo: Argos lee lo que muestra con §11.7.
	return t.Emitir(EventoSolicitudEstado, map[string]any{"solicitud_id": f.ID, "numero": f.Numero, "empresa_id": f.EmpresaID,
		"cliente_id": f.ClienteExistenteID, "tipo": *f.SolicitudTipo, "servicio_id": f.SolicitudServicioID, "de": anterior, "a": nuevo})
}

func (m *Modulo) nuevoCrearSolicitud() *auditoria.Caso[EntradaCrearSolicitud, SolicitudServicio] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearSolicitud, SolicitudServicio]{
		Accion: "portal_argos.solicitar_servicio", Permiso: nucleo.PermPortalArgosSolicitarServicio,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearSolicitud) (SolicitudServicio, error) {
			s := in.Sesion
			prepararSolicitud(t, s)
			if err := exigirTitular(s); err != nil {
				return SolicitudServicio{}, err
			}
			q := comercialbd.New(t)
			idem := conIdempotencia(ctx, claveSolicitud(s, in.ClaveIdempotencia))
			if idem != nil {
				if f, err := q.SolicitudPorIdempotencia(ctx, idem); err == nil {
					if f.ClienteExistenteID == nil || *f.ClienteExistenteID != s.ClienteID || f.EmpresaID == nil || *f.EmpresaID != s.EmpresaID {
						return SolicitudServicio{}, noEncontradaSolicitud()
					}
					t.FijarEntidad("prospecto", f.ID)
					t.Anotar(map[string]any{"repetida": true})
					return aSolicitud(f), nil
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return SolicitudServicio{}, err
				}
			}
			ficha, err := q.FichaClienteSolicitud(ctx, s.ClienteID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return SolicitudServicio{}, err
			}
			d := comercialbd.CrearSolicitudPortalParams{EmpresaID: &s.EmpresaID, ClienteID: &s.ClienteID, SolicitudTipo: &in.Tipo,
				Idempotencia: idem, Lat: in.lat, Lon: in.lon, PlanInteresID: in.PlanInteresID}
			// El plan pedido queda fijo (revisión M1): plan_interes_id se puede editar después.
			d.SolicitudPlanID = in.PlanInteresID
			if in.planNoValidado != nil {
				d.SolicitudPlanID, d.SolicitudPlanNoValidado = in.planNoValidado, true
				d.Notas = "Plan no validado: el cliente pidió el plan " + strconv.FormatInt(*in.planNoValidado, 10) +
					" de Argos y Argos no respondió al validarlo; confírmalo al cotizar."
				t.Anotar(map[string]any{"plan_no_validado": *in.planNoValidado})
			}
			d.Nombre = strings.TrimSpace(ficha.Nombre)
			if d.Nombre == "" {
				d.Nombre = "Cliente " + strconv.FormatInt(s.ClienteID, 10)
			}
			if c := strings.TrimSpace(ficha.Correo); c != "" {
				d.Correo = &c
			}
			d.ZonaID = ficha.ZonaID
			// Teléfono: el que escribió el cliente (regla de §3.2) o el de la ficha.
			tel := in.TelefonoContacto
			if tel == nil || strings.TrimSpace(*tel) == "" {
				for _, v := range []string{ficha.Movil, ficha.Telefono} {
					if v = strings.TrimSpace(v); v != "" {
						tel = &v
						break
					}
				}
				if tel != nil {
					if n, err := telefonoArgos(tel); err == nil {
						tel = n
					}
				}
			} else if tel, err = telefonoArgos(tel); err != nil {
				return SolicitudServicio{}, campoInvalido("telefono_contacto", "Teléfono no válido (regla de §3.2)", "telefono")
			}
			d.Telefono = tel
			if in.ServicioID != nil {
				srv, err := q.ServicioDeClienteSolicitud(ctx, comercialbd.ServicioDeClienteSolicitudParams{ID: *in.ServicioID, ClienteID: s.ClienteID, EmpresaID: s.EmpresaID})
				if errors.Is(err, pgx.ErrNoRows) {
					return SolicitudServicio{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "servicio_id", Message: "El servicio no es del cliente", Rule: "servicio"})
				}
				if err != nil {
					return SolicitudServicio{}, err
				}
				d.SolicitudServicioID = &srv.ID
				if in.Tipo == SolicitudCambioPlan {
					// Cambio de plan: la dirección y la ubicación del servicio.
					d.Direccion, d.Lat, d.Lon = srv.Direccion, srv.Lat, srv.Lon
				}
				if srv.ZonaID != nil {
					d.ZonaID = srv.ZonaID
				}
			}
			if in.Direccion != nil && strings.TrimSpace(*in.Direccion) != "" && in.Tipo != SolicitudCambioPlan {
				if d.Direccion, err = texto("direccion", *in.Direccion, 1, 300); err != nil {
					return SolicitudServicio{}, err
				}
			}
			if d.Direccion == "" {
				d.Direccion = strings.TrimSpace(ficha.Direccion)
			}
			if d.Direccion == "" {
				d.Direccion = "(sin dirección)"
			}
			if in.Comentario != nil && strings.TrimSpace(*in.Comentario) != "" {
				c, err := texto("comentario", *in.Comentario, 1, 2000)
				if err != nil {
					return SolicitudServicio{}, err
				}
				d.SolicitudComentario = &c
			}
			// Una abierta del mismo tipo y servicio: 409 con su id.
			if err := q.BloquearRegistrosArgos(ctx, s.EmpresaID); err != nil {
				return SolicitudServicio{}, err
			}
			abierta, err := q.SolicitudAbierta(ctx, comercialbd.SolicitudAbiertaParams{EmpresaID: s.EmpresaID, ClienteID: s.ClienteID,
				SolicitudTipo: in.Tipo, ServicioID: valorID(d.SolicitudServicioID)})
			switch {
			case err == nil:
				p := nucleo.Conflicto("solicitud_abierta", "Ya hay una solicitud abierta de ese tipo para ese servicio")
				p.SolicitudID = &abierta
				return SolicitudServicio{}, p
			case !errors.Is(err, pgx.ErrNoRows):
				return SolicitudServicio{}, err
			}
			n, err := q.SolicitudesUltimoDia(ctx, comercialbd.SolicitudesUltimoDiaParams{EmpresaID: s.EmpresaID, ClienteID: s.ClienteID})
			if err != nil {
				return SolicitudServicio{}, err
			}
			if n >= TopeSolicitudesDia {
				espera := 24 * time.Hour
				if desde, err := q.PrimeraSolicitudUltimoDia(ctx, comercialbd.PrimeraSolicitudUltimoDiaParams{EmpresaID: s.EmpresaID, ClienteID: s.ClienteID}); err == nil {
					espera = time.Until(desde.Add(24 * time.Hour))
				}
				return SolicitudServicio{}, nucleo.DemasiadosIntentos("demasiadas_solicitudes", "El cliente llegó al tope de solicitudes del día", espera)
			}
			if d.ZonaID != nil {
				if err := validarZona(ctx, t, d.ZonaID, &s.EmpresaID); err != nil {
					// La zona de la ficha puede no estar en el catálogo de agenda: sin zona (sin vendedor por zona).
					d.ZonaID = nil
				} else if d.VendedorID, err = vendedorDeZona(ctx, t, q, *d.ZonaID, s.EmpresaID); err != nil {
					return SolicitudServicio{}, err
				}
			}
			f, err := q.CrearSolicitudPortal(ctx, d)
			if err != nil {
				return SolicitudServicio{}, err
			}
			t.FijarEntidad("prospecto", f.ID)
			t.RegistrarCambios("prospecto", f.ID, nil, f)
			if err := historial(ctx, t, q, f.ID, "creado", nil, &f.Estado, nil, map[string]any{"origen": f.Origen, "solicitud": in.Tipo, "plan_no_validado": in.planNoValidado}); err != nil {
				return SolicitudServicio{}, err
			}
			if err := t.Emitir("prospecto.creado", map[string]any{"prospecto_id": f.ID, "numero": f.Numero, "empresa_id": f.EmpresaID,
				"origen": f.Origen, "zona_id": f.ZonaID, "vendedor_id": f.VendedorID, "solicitud_tipo": in.Tipo}); err != nil {
				return SolicitudServicio{}, err
			}
			return aSolicitud(f), nil
		},
	})
}

func valorID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// claveSolicitud: la clave por credencial y cliente (§11.2).
func claveSolicitud(s argos.SesionPortal, clave string) string {
	if clave == "" {
		return ""
	}
	return "portal_argos:" + strconv.FormatInt(s.EmpresaID, 10) + ":" + strconv.FormatInt(s.ClienteID, 10) + ":" + clave
}

// validarCuerpoSolicitud: las reglas de forma de §11.7, antes de abrir la transacción.
func (m *Modulo) validarCuerpoSolicitud(ctx context.Context, in *EntradaCrearSolicitud) error {
	switch in.Tipo {
	case SolicitudAlta, SolicitudCambioPlan, SolicitudTraslado, SolicitudOtro:
	default:
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "tipo", Message: "alta, cambio_plan, traslado u otro", Rule: "enum"})
	}
	if in.Tipo == SolicitudAlta && in.ServicioID != nil {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "servicio_id", Message: "Una alta no lleva servicio", Rule: "servicio"})
	}
	if (in.Tipo == SolicitudCambioPlan || in.Tipo == SolicitudTraslado) && in.ServicioID == nil {
		return nucleo.ReglaNegocio("servicio_requerido", "Un cambio de plan o un traslado indica el servicio")
	}
	if in.Tipo == SolicitudAlta || in.Tipo == SolicitudTraslado {
		if in.Direccion == nil || strings.TrimSpace(*in.Direccion) == "" {
			return nucleo.Validacion(nucleo.ErrorCampo{Field: "direccion", Message: "Es obligatoria en una alta o un traslado", Rule: "required"})
		}
		if _, err := texto("direccion", *in.Direccion, 1, 300); err != nil {
			return err
		}
	}
	if in.Comentario != nil {
		if _, err := texto("comentario", *in.Comentario, 0, 2000); err != nil {
			return err
		}
	}
	var err error
	if in.lat, in.lon, err = coordenadasArgos(in.Coordenadas); err != nil {
		return err
	}
	if in.PlanInteresID != nil && m.cfg.Planes != nil {
		ps, err := m.cfg.Planes.Planes(ctx, nil, "")
		var pr *nucleo.Problema
		switch {
		case errors.As(err, &pr) && pr.Status >= 500:
			// Argos no responde: la solicitud se guarda sin plan y con la nota «plan no validado» (el
			// asesor lo revisa al cotizar); nunca un plan sin validar.
			in.planNoValidado, in.PlanInteresID = in.PlanInteresID, nil
		case err != nil:
			return err
		case !slices.ContainsFunc(ps, func(p PlanComercial) bool { return p.ID == *in.PlanInteresID }):
			return nucleo.Validacion(nucleo.ErrorCampo{Field: "plan_interes_id", Message: "El plan no es de los de Argos", Rule: "plan"})
		}
	}
	return nil
}

// CrearSolicitud: POST …/solicitudes. Valida la forma (y el plan con Argos) fuera de la transacción.
func (m *Modulo) CrearSolicitud(ctx context.Context, in EntradaCrearSolicitud) (SolicitudServicio, error) {
	if err := exigirTitular(in.Sesion); err != nil {
		// Se audita como rechazo del caso (sin tocar Argos).
		return m.crearSolicitud.Ejecutar(ctx, in)
	}
	if err := m.validarCuerpoSolicitud(ctx, &in); err != nil {
		return SolicitudServicio{}, err
	}
	return m.crearSolicitud.Ejecutar(ctx, in)
}

func (m *Modulo) nuevoListarSolicitudes() *auditoria.Caso[EntradaSolicitudes, nucleo.Pagina[SolicitudServicio]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaSolicitudes, nucleo.Pagina[SolicitudServicio]]{
		Accion: "portal_argos.ver_solicitudes", Permiso: nucleo.PermPortalArgosVerSolicitudes,
		AuditarExito: soloLecturas[nucleo.Pagina[SolicitudServicio]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSolicitudes) (nucleo.Pagina[SolicitudServicio], error) {
			s := in.Sesion
			prepararSolicitud(t, s)
			var cero nucleo.Pagina[SolicitudServicio]
			if err := exigirTitular(s); err != nil {
				return cero, err
			}
			for _, e := range in.Estados {
				if !slices.Contains([]string{"recibida", "en_revision", "cotizada", "en_proceso", "completada", "no_procede"}, e) {
					return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "estado", Message: "Estado desconocido", Rule: "enum"})
				}
			}
			pagina := nucleo.ParamsPagina{Cursor: in.Cursor, Limit: in.Limite}
			var antesDe int64
			hay, err := nucleo.DecodificarCursor(in.Cursor, &antesDe)
			if err != nil {
				return cero, err
			}
			estados := in.Estados
			if estados == nil {
				estados = []string{}
			}
			p := comercialbd.ListarSolicitudesClienteParams{EmpresaID: s.EmpresaID, ClienteID: s.ClienteID, Estados: estados,
				Limite: int32(pagina.Limite() + 1)} //nolint:gosec // ≤ 201
			if hay {
				p.AntesDe = &antesDe
			}
			filas, err := comercialbd.New(t).ListarSolicitudesCliente(ctx, p)
			if err != nil {
				return cero, err
			}
			return nucleo.Paginar(filas, pagina.Limite(), aSolicitud,
				func(f comercialbd.ComercialProspecto) []any { return []any{f.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoVerSolicitud() *auditoria.Caso[EntradaVerSolicitud, SolicitudServicio] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerSolicitud, SolicitudServicio]{
		Accion: "portal_argos.ver_solicitudes", Permiso: nucleo.PermPortalArgosVerSolicitudes,
		AuditarExito: soloLecturas[SolicitudServicio],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerSolicitud) (SolicitudServicio, error) {
			s := in.Sesion
			prepararSolicitud(t, s)
			if err := exigirTitular(s); err != nil {
				return SolicitudServicio{}, err
			}
			t.FijarEntidad("prospecto", in.SolicitudID)
			f, err := comercialbd.New(t).ProspectoPorID(ctx, in.SolicitudID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && (f.SolicitudTipo == nil || f.ClienteExistenteID == nil ||
				*f.ClienteExistenteID != s.ClienteID || f.EmpresaID == nil || *f.EmpresaID != s.EmpresaID)) {
				return SolicitudServicio{}, noEncontradaSolicitud()
			}
			if err != nil {
				return SolicitudServicio{}, err
			}
			return aSolicitud(f), nil
		},
	})
}

// Cierre de la orden de una solicitud: completada si la orden se cerró bien.

type entradaOrdenSolicitud struct {
	OrdenID   int64  `json:"orden_id"`
	Resultado string `json:"resultado"`
}

func (m *Modulo) nuevoOrdenDeSolicitud() *auditoria.Caso[entradaOrdenSolicitud, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[entradaOrdenSolicitud, struct{}]{
		Accion: "comercial.solicitud_orden_cerrada", Acceso: auditoria.Autenticado,
		AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaOrdenSolicitud) (struct{}, error) {
			q := comercialbd.New(t)
			f, err := q.SolicitudDeOrden(ctx, &in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return struct{}{}, nil
			}
			if err != nil {
				return struct{}{}, err
			}
			t.FijarEntidad("prospecto", f.ID)
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			return struct{}{}, sincronizarEstadoCliente(ctx, t, q, f, in.Resultado == "exitosa")
		},
	})
}

// SuscribirSolicitudes: al cerrarse la orden de una solicitud, el cliente la ve completada.
func (m *Modulo) SuscribirSolicitudes(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("comercial.solicitud_orden", "orden.cerrada", func(ctx context.Context, e integracion.Evento) error {
		var p entradaOrdenSolicitud
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.OrdenID == 0 {
			return nil
		}
		_, err := m.ordenDeSolicitud.Ejecutar(nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), bd), p)
		return err
	})
}

// Tarea «Abrir en Argos» (§11.7 y §10.4 punto 10) --------------------------------------------

// EntradaCompletarTareaArgos: una persona aplicó en Argos el cambio de plan, el traslado u otro.
type EntradaCompletarTareaArgos struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

func (m *Modulo) nuevoCompletarTareaArgos() *auditoria.Caso[EntradaCompletarTareaArgos, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCompletarTareaArgos, Prospecto]{
		Accion: "prospectos.completar_tarea_argos", Permiso: nucleo.PermProspectosCompletarTareaArgos,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCompletarTareaArgos) (Prospecto, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermProspectosCompletarTareaArgos, &in.Version)
			if err != nil {
				return Prospecto{}, err
			}
			if deref(antes.TareaArgos) != "pendiente" {
				return Prospecto{}, nucleo.ReglaNegocio("sin_tarea_argos", "El prospecto no tiene una tarea «Abrir en Argos» pendiente")
			}
			ahora := m.ahora()
			f, err := q.FijarTareaArgos(ctx, comercialbd.FijarTareaArgosParams{ID: antes.ID, TareaArgos: ptr("hecha"), HechaEn: &ahora, HechaPor: actorUsuario(t)})
			if err != nil {
				return Prospecto{}, err
			}
			t.RegistrarCambios("prospecto", f.ID, map[string]any{"tarea_argos": "pendiente"}, map[string]any{"tarea_argos": "hecha"})
			if err := historial(ctx, t, q, f.ID, "tarea_argos", ptr("pendiente"), ptr("hecha"), nil, nil); err != nil {
				return Prospecto{}, err
			}
			if err := t.Emitir("prospecto.tarea_argos_completada", map[string]any{"prospecto_id": f.ID, "empresa_id": f.EmpresaID,
				"solicitud_tipo": f.SolicitudTipo}); err != nil {
				return Prospecto{}, err
			}
			if err := sincronizarEstadoCliente(ctx, t, q, f, false); err != nil {
				return Prospecto{}, err
			}
			f, err = q.ProspectoPorID(ctx, f.ID)
			return aProspecto(f), err
		},
	})
}

// Ajustes de las solicitudes por empresa (§10.4 punto 10) --------------------------------------

// AjustesSolicitudes son los ajustes de las solicitudes del portal de Argos de la empresa activa.
type AjustesSolicitudes struct {
	EmpresaID               int64 `json:"empresaId"`
	CambioPlanExigeContrato bool  `json:"cambioPlanExigeContrato" doc:"Ganar un cambio de plan exige el contrato firmado (de fábrica, false: basta la cotización aceptada)"`
	Version                 int32 `json:"version" doc:"0 = nunca se guardaron (valores de fábrica)"`
}

// EntradaAjustesSolicitudes cambia los ajustes de la empresa activa.
type EntradaAjustesSolicitudes struct {
	CambioPlanExigeContrato bool  `json:"cambioPlanExigeContrato"`
	Version                 int32 `json:"version" minimum:"0" doc:"La leída (0 si nunca se guardaron)"`
}

// ajustesSolicitudes lee los de la empresa (los de fábrica si no hay fila).
func ajustesSolicitudes(ctx context.Context, q *comercialbd.Queries, empresa int64) (AjustesSolicitudes, error) {
	f, err := q.AjustesSolicitudes(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return AjustesSolicitudes{EmpresaID: empresa}, nil
	}
	if err != nil {
		return AjustesSolicitudes{}, err
	}
	return AjustesSolicitudes{EmpresaID: empresa, CambioPlanExigeContrato: f.CambioPlanExigeContrato, Version: f.Version}, nil
}

func empresaDeAjustes(ctx context.Context) (int64, error) {
	_, id := filtroEmpresa(ctx)
	if id == nil || *id == 0 {
		return 0, nucleo.EmpresaRequerida()
	}
	return *id, nil
}

func (m *Modulo) nuevoVerAjustesSolicitudes() *auditoria.Caso[struct{}, AjustesSolicitudes] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesSolicitudes]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[AjustesSolicitudes],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesSolicitudes, error) {
			empresa, err := empresaDeAjustes(ctx)
			if err != nil {
				return AjustesSolicitudes{}, err
			}
			return ajustesSolicitudes(ctx, comercialbd.New(t), empresa)
		},
	})
}

func (m *Modulo) nuevoCambiarAjustesSolicitudes() *auditoria.Caso[EntradaAjustesSolicitudes, AjustesSolicitudes] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAjustesSolicitudes, AjustesSolicitudes]{
		Accion: "prospectos.configurar_solicitudes", Permiso: nucleo.PermProspectosConfigurarSolicitudes,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAjustesSolicitudes) (AjustesSolicitudes, error) {
			empresa, err := empresaDeAjustes(ctx)
			if err != nil {
				return AjustesSolicitudes{}, err
			}
			t.FijarEmpresa(empresa)
			t.FijarEntidad("ajustes_solicitudes", empresa)
			q := comercialbd.New(t)
			antes, err := ajustesSolicitudes(ctx, q, empresa)
			if err != nil {
				return AjustesSolicitudes{}, err
			}
			if in.Version != antes.Version {
				return AjustesSolicitudes{}, nucleo.ConflictoVersion("ajustes_solicitudes", empresa)
			}
			f, err := q.GuardarAjustesSolicitudes(ctx, comercialbd.GuardarAjustesSolicitudesParams{EmpresaID: empresa,
				CambioPlanExigeContrato: in.CambioPlanExigeContrato, Version: in.Version})
			if errors.Is(err, pgx.ErrNoRows) {
				return AjustesSolicitudes{}, nucleo.ConflictoVersion("ajustes_solicitudes", empresa)
			}
			if err != nil {
				return AjustesSolicitudes{}, err
			}
			despues := AjustesSolicitudes{EmpresaID: empresa, CambioPlanExigeContrato: f.CambioPlanExigeContrato, Version: f.Version}
			t.RegistrarCambios("ajustes_solicitudes", empresa, antes, despues)
			return despues, t.Emitir("solicitudes.ajustes_cambiados", map[string]any{"empresa_id": empresa,
				"cambio_plan_exige_contrato": f.CambioPlanExigeContrato, "version": f.Version})
		},
	})
}
