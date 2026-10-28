package campo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// EntradaContactoOrden son los datos de contacto en sitio.
type EntradaContactoOrden struct {
	Nombre   *string `json:"nombre,omitempty" maxLength:"120"`
	Telefono *string `json:"telefono,omitempty" maxLength:"40"`
}

// EntradaCrearOrden es la entrada de ordenes.crear. Cliente, servicio, zona y bodega son ids de
// Argos; sin conector se captura la dirección a mano.
type EntradaCrearOrden struct {
	Tipo        string                `json:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento"`
	PlantillaID *int64                `json:"plantillaId,omitempty" minimum:"1" doc:"Otra plantilla del mismo tipo (p. ej. instalación inalámbrica); por defecto, la del tipo"`
	EmpresaID   *int64                `json:"empresaId,omitempty" minimum:"1" doc:"Empresa en Argos. La orden es de la empresa activa (otra: 422 empresa_no_coincide); solo el sistema la elige aquí"`
	TicketID    *int64                `json:"ticketId,omitempty" minimum:"1" doc:"Ticket de origen: la orden hereda su empresa, cliente, servicio, zona, cuadrilla y contacto, y el ticket recibe una nota interna. Exige ver el ticket (tickets.ver)"`
	ProspectoID *int64                `json:"prospectoId,omitempty" minimum:"1" doc:"Prospecto de origen (fase 4)"`
	ClienteID   *int64                `json:"clienteId,omitempty" minimum:"1" doc:"Cliente en Argos"`
	ServicioID  *int64                `json:"servicioId,omitempty" minimum:"1" doc:"Servicio en Argos (exige clienteId)"`
	ZonaID      *int64                `json:"zonaId,omitempty" minimum:"1" doc:"Zona (Argos); define el alcance. Por defecto, la del servicio o el cliente en la instantánea"`
	CuadrillaID *int64                `json:"cuadrillaId,omitempty" minimum:"1"`
	BodegaID    *int64                `json:"bodegaId,omitempty" minimum:"1" doc:"Bodega del técnico en Argos"`
	Direccion   *string               `json:"direccion,omitempty" maxLength:"300" doc:"Obligatoria si no sale de la instantánea de Argos del servicio o el cliente"`
	Lat         *float64              `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon         *float64              `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	Contacto    *EntradaContactoOrden `json:"contacto,omitempty"`
	Descripcion string                `json:"descripcion,omitempty" maxLength:"4000" doc:"Trabajo a realizar"`

	// ClaveIdempotencia es la Idempotency-Key de la petición (la pone el handler): si la misma
	// operación ya creó una orden, se devuelve esa.
	ClaveIdempotencia string `json:"-"`
}

// conIdempotencia identifica la operación por la credencial y su Idempotency-Key. nil sin
// clave.
func conIdempotencia(ctx context.Context, clave string) *string {
	if clave == "" {
		return nil
	}
	a := contexto.ActorDe(ctx)
	credencial := fmt.Sprintf("%s:%d", a.Tipo, a.ID)
	if a.TokenID != 0 {
		credencial = fmt.Sprintf("token:%d", a.TokenID)
	}
	v := credencial + "|" + clave
	return &v
}

// empresaDeLaOrden: la empresa que resolvió el middleware de I1 (mismas reglas que un ticket).
//   - Con empresa activa: esa; el cuerpo solo puede repetirla (422 empresa_no_coincide).
//   - Sin conector: null; el cuerpo no puede fijar una.
//   - Actor sistema: sin empresas registradas, null; con empresas, la pedida, obligatoria y
//     registrada.
func empresaDeLaOrden(ctx context.Context, q *campobd.Queries, pedida *int64) (*int64, error) {
	noCoincide := func(detalle string) error { return nucleo.ReglaNegocio("empresa_no_coincide", detalle) }
	todas, id := filtroEmpresa(ctx)
	if !todas {
		if id != nil && *id == 0 {
			return nil, nucleo.EmpresaRequerida()
		}
		if pedida != nil && !mismoID(pedida, id) {
			if id == nil {
				return nil, noCoincide("Sin conector con Argos las órdenes se guardan sin empresa")
			}
			return nil, noCoincide("La empresa de la orden debe ser la empresa activa")
		}
		return id, nil
	}
	hay, err := q.HayEmpresas(ctx)
	if err != nil {
		return nil, err
	}
	if !hay {
		if pedida != nil {
			return nil, noCoincide("Sin conector con Argos las órdenes se guardan sin empresa")
		}
		return nil, nil
	}
	if pedida == nil {
		return nil, nucleo.ReglaNegocio("empresa_requerida", "Indica la empresa de la orden")
	}
	existe, err := q.EmpresaRegistrada(ctx, *pedida)
	if err != nil {
		return nil, err
	}
	if !existe {
		return nil, nucleo.ReglaNegocio("empresa_no_registrada", "La empresa no está registrada en Iris")
	}
	return pedida, nil
}

// heredar toma del ticket lo que la entrada no trae; si trae algo distinto, 422.
func heredar(campo string, propio, delTicket *int64) (*int64, error) {
	if propio == nil {
		return delTicket, nil
	}
	if delTicket != nil && *propio != *delTicket {
		return nil, nucleo.ReglaNegocio("no_coincide_con_ticket", "El "+campo+" no es el del ticket de origen")
	}
	return propio, nil
}

func (m *Modulo) nuevoCrearOrden() *auditoria.Caso[EntradaCrearOrden, Orden] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearOrden, Orden]{
		Accion:  "ordenes.crear",
		Permiso: nucleo.PermOrdenesCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearOrden) (Orden, error) {
			if err := sinNulos(in); err != nil {
				return Orden{}, err
			}
			q := campobd.New(t)

			// La misma operación repetida (clave reclamada tras una caída) devuelve su orden.
			idem := conIdempotencia(ctx, in.ClaveIdempotencia)
			if idem != nil {
				previa, err := q.OrdenPorIdempotencia(ctx, idem)
				if err == nil {
					t.FijarEntidad("orden", previa.ID)
					if err := verificarAcceso(ctx, nucleo.PermOrdenesVer, previa); err != nil {
						return Orden{}, err
					}
					t.FijarEmpresa(deref(previa.EmpresaID))
					t.Anotar(map[string]any{"repetida": true})
					return aOrden(previa, in.Tipo), nil
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return Orden{}, err
				}
			}

			tipo, err := q.TipoPorCodigo(ctx, in.Tipo)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !tipo.Activo) {
				return Orden{}, campoInvalido("tipo", "El tipo de orden no existe o está inactivo", "tipo")
			}
			if err != nil {
				return Orden{}, err
			}
			plantilla := tipo.PlantillaID
			if in.PlantillaID != nil {
				p, err := q.PlantillaPorID(ctx, *in.PlantillaID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!p.Activa || p.TipoCodigo != tipo.Codigo)) {
					return Orden{}, campoInvalido("plantillaId", "La plantilla no existe, está inactiva o es de otro tipo", "plantilla")
				}
				if err != nil {
					return Orden{}, err
				}
				plantilla = &p.ID
			}

			datos := in
			pedida := in.EmpresaID
			var ticket *soporte.TicketDeOrden
			if in.TicketID != nil {
				tk, err := soporte.TicketParaOrden(ctx, t, *in.TicketID)
				var p *nucleo.Problema
				if errors.As(err, &p) && p.Codigo() == "no_encontrado" {
					return Orden{}, nucleo.ReglaNegocio("ticket_invalido", "El ticket no existe o no lo puedes ver")
				}
				if err != nil {
					return Orden{}, err
				}
				ticket = &tk
				if datos.ClienteID, err = heredar("cliente", in.ClienteID, tk.ClienteID); err != nil {
					return Orden{}, err
				}
				if datos.ServicioID, err = heredar("servicio", in.ServicioID, tk.ServicioID); err != nil {
					return Orden{}, err
				}
				if datos.ZonaID == nil {
					datos.ZonaID = tk.ZonaID
				}
				if datos.CuadrillaID == nil {
					datos.CuadrillaID = tk.CuadrillaID
				}
				if pedida == nil && !contexto.ActorDe(ctx).EsUsuario() {
					pedida = tk.EmpresaID
				}
				if datos.Contacto == nil {
					datos.Contacto = &EntradaContactoOrden{Nombre: tk.ContactoNombre, Telefono: tk.ContactoTelefono}
				}
			}
			if datos.ServicioID != nil && datos.ClienteID == nil {
				return Orden{}, campoInvalido("clienteId", "Un servicio exige su cliente", "required")
			}
			empresa, err := empresaDeLaOrden(ctx, q, pedida)
			if err != nil {
				return Orden{}, err
			}
			if ticket != nil && !mismoID(ticket.EmpresaID, empresa) {
				return Orden{}, nucleo.ReglaNegocio("empresa_no_coincide", "El ticket es de otra empresa")
			}
			t.FijarEmpresa(deref(empresa))
			if err := completarDesdeArgos(ctx, q, &datos, empresa); err != nil {
				return Orden{}, err
			}

			direccion, err := textoOpcional("direccion", datos.Direccion, 300)
			if err != nil {
				return Orden{}, err
			}
			if direccion == nil {
				return Orden{}, campoInvalido("direccion", "Indica la dirección (no hay instantánea de Argos de donde tomarla)", "required")
			}
			if err := validarCoordenadas(datos.Lat, datos.Lon); err != nil {
				return Orden{}, err
			}
			var contacto EntradaContactoOrden
			if datos.Contacto != nil {
				if contacto.Nombre, err = textoOpcional("contacto.nombre", datos.Contacto.Nombre, 120); err != nil {
					return Orden{}, err
				}
				if contacto.Telefono, err = textoOpcional("contacto.telefono", datos.Contacto.Telefono, 40); err != nil {
					return Orden{}, err
				}
			}

			// Con alcance limitado, la orden debe caer en una zona o cuadrilla propia.
			if global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermOrdenesCrear)); !global &&
				(datos.ZonaID == nil || !slices.Contains(zonas, *datos.ZonaID)) &&
				(datos.CuadrillaID == nil || !slices.Contains(cuadrillas, *datos.CuadrillaID)) {
				return Orden{}, fueraDeAlcance("Tu permiso ordenes.crear se limita a otras zonas o cuadrillas; indica una tuya")
			}

			f, err := q.CrearOrden(ctx, campobd.CrearOrdenParams{
				EmpresaID: empresa, TipoID: tipo.ID, PlantillaID: plantilla, TicketID: in.TicketID, ProspectoID: in.ProspectoID,
				ClienteID: datos.ClienteID, ServicioID: datos.ServicioID, ZonaID: datos.ZonaID, CuadrillaID: datos.CuadrillaID,
				BodegaID: datos.BodegaID, Direccion: *direccion, Lat: datos.Lat, Lon: datos.Lon,
				ContactoNombre: contacto.Nombre, ContactoTelefono: contacto.Telefono,
				Descripcion: strings.TrimSpace(in.Descripcion), CreadoPor: actorUsuario(t), Idempotencia: idem,
			})
			if err != nil {
				return Orden{}, err
			}
			t.FijarEntidad("orden", f.ID)
			t.RegistrarCambios("orden", f.ID, nil, f)
			if err := historial(ctx, t, q, f.ID, "creada", nil, &f.Estado, nil, map[string]any{"tipo": tipo.Codigo, "ticket_id": f.TicketID}, m.ahora()); err != nil {
				return Orden{}, err
			}
			if err := t.Emitir("orden.creada", map[string]any{
				"orden_id": f.ID, "numero": f.Numero, "tipo": tipo.Codigo, "empresa_id": f.EmpresaID,
				"ticket_id": f.TicketID, "prospecto_id": f.ProspectoID, "cliente_id": f.ClienteID,
				"servicio_id": f.ServicioID, "zona_id": f.ZonaID,
			}); err != nil {
				return Orden{}, err
			}
			if ticket != nil {
				nota := fmt.Sprintf("Se creó la orden de trabajo %s (%s) desde este ticket.", f.Numero, tipo.Nombre)
				if err := soporte.NotaDeOrden(ctx, t, ticket.ID, nota); err != nil {
					return Orden{}, err
				}
			}
			return aOrden(f, tipo.Codigo), nil
		},
	})
}

// completarDesdeArgos valida el cliente y el servicio contra la instantánea de Argos (si Iris
// la tiene: misma empresa, servicio del cliente) y completa lo que falte (dirección,
// coordenadas, zona y contacto). Sin instantánea no se puede validar: se aceptan los ids.
func completarDesdeArgos(ctx context.Context, q *campobd.Queries, in *EntradaCrearOrden, empresa *int64) error {
	otraEmpresa := func(e int64) bool { return empresa != nil && e != *empresa }
	if in.ServicioID != nil {
		s, err := q.ServicioArgos(ctx, *in.ServicioID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			if otraEmpresa(s.EmpresaID) {
				return nucleo.ReglaNegocio("empresa_no_coincide", "El servicio es de otra empresa en Argos")
			}
			if s.ClienteID != deref(in.ClienteID) {
				return campoInvalido("servicioId", "El servicio no es de ese cliente según Argos", "servicio")
			}
			if in.Direccion == nil && s.Direccion != "" {
				in.Direccion = &s.Direccion
			}
			if in.Lat == nil && s.Lat != nil && s.Lon != nil {
				in.Lat, in.Lon = s.Lat, s.Lon
			}
			if in.ZonaID == nil {
				in.ZonaID = s.ZonaID
			}
		}
	}
	if in.ClienteID != nil {
		c, err := q.ClienteArgos(ctx, *in.ClienteID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			if otraEmpresa(c.EmpresaID) {
				return nucleo.ReglaNegocio("empresa_no_coincide", "El cliente es de otra empresa en Argos")
			}
			if in.Direccion == nil && c.Direccion != "" {
				in.Direccion = &c.Direccion
			}
			if in.Lat == nil && c.Lat != nil && c.Lon != nil {
				in.Lat, in.Lon = c.Lat, c.Lon
			}
			if in.ZonaID == nil {
				in.ZonaID = c.ZonaID
			}
			if in.Contacto == nil {
				in.Contacto = &EntradaContactoOrden{Nombre: &c.Nombre}
				if c.Telefono != "" {
					in.Contacto.Telefono = &c.Telefono
				}
			}
		}
	}
	return nil
}
