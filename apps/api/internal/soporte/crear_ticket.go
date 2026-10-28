package soporte

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaCrearTicket es la entrada de tickets.crear. Cliente, servicio y zona son ids de Argos
// (opcionales: sin conector se capturan los datos de contacto).
type EntradaCrearTicket struct {
	EmpresaID     *int64           `json:"empresaId,omitempty" minimum:"1" doc:"Empresa en Argos; por defecto, la activa"`
	ClienteID     *int64           `json:"clienteId,omitempty" minimum:"1" doc:"Cliente en Argos"`
	ServicioID    *int64           `json:"servicioId,omitempty" minimum:"1" doc:"Servicio en Argos (exige clienteId)"`
	ZonaID        *int64           `json:"zonaId,omitempty" minimum:"1" doc:"Zona (Argos); define el alcance"`
	Contacto      *EntradaContacto `json:"contacto,omitempty"`
	CategoriaID   *int64           `json:"categoriaId,omitempty" minimum:"1"`
	Prioridad     string           `json:"prioridad,omitempty" enum:"baja,media,alta,critica" doc:"media por defecto"`
	Origen        string           `json:"origen,omitempty" enum:"panel,portal,correo,telegram,horus,argos,api" doc:"Por defecto, según el canal"`
	Asunto        string           `json:"asunto" minLength:"3" maxLength:"200"`
	Descripcion   string           `json:"descripcion,omitempty" maxLength:"20000" doc:"Primer mensaje público del ticket"`
	TicketPadreID *int64           `json:"ticketPadreId,omitempty" minimum:"1" doc:"Incidente maestro al que se vincula"`
}

func origenPorCanal(c contexto.Canal) string {
	switch c {
	case contexto.CanalPortal:
		return "portal"
	case contexto.CanalAPI, contexto.CanalIntegracion:
		return "api"
	}
	return "panel"
}

func (m *Modulo) nuevoCrearTicket() *auditoria.Caso[EntradaCrearTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearTicket, Ticket]{
		Accion:  "tickets.crear",
		Permiso: nucleo.PermTicketsCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearTicket) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()

			asunto, err := texto("asunto", in.Asunto, 3, 200)
			if err != nil {
				return Ticket{}, err
			}
			descripcion := strings.TrimSpace(in.Descripcion)
			prioridad := in.Prioridad
			if prioridad == "" {
				prioridad = "media"
			}
			if !slices.Contains(Prioridades, prioridad) {
				return Ticket{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "prioridad", Message: "Prioridad desconocida", Rule: "enum"})
			}
			origen := in.Origen
			if origen == "" {
				origen = origenPorCanal(contexto.CanalDe(ctx))
			}
			if !slices.Contains(Origenes, origen) {
				return Ticket{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "origen", Message: "Origen desconocido", Rule: "enum"})
			}
			if in.ServicioID != nil && in.ClienteID == nil {
				return Ticket{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "clienteId", Message: "Un servicio exige su cliente", Rule: "required"})
			}
			var contacto EntradaContacto
			if in.Contacto != nil {
				if contacto.Nombre, err = textoOpcional("contacto.nombre", in.Contacto.Nombre, 120); err != nil {
					return Ticket{}, err
				}
				if contacto.Telefono, err = textoOpcional("contacto.telefono", in.Contacto.Telefono, 40); err != nil {
					return Ticket{}, err
				}
				if contacto.Correo, err = textoOpcional("contacto.correo", in.Contacto.Correo, 254); err != nil {
					return Ticket{}, err
				}
				if contacto.Correo != nil {
					c := strings.ToLower(*contacto.Correo)
					contacto.Correo = &c
				}
			}

			// Empresa: la activa; otra distinta no se puede elegir.
			empresa := in.EmpresaID
			if activa := contexto.EmpresaDe(ctx); activa != 0 {
				if empresa != nil && *empresa != activa {
					return Ticket{}, nucleo.Prohibido("No puedes crear tickets de otra empresa")
				}
				empresa = &activa
			}
			t.FijarEmpresa(deref(empresa))

			// Al crear no hay cuadrilla: un alcance limitado se juzga por la zona.
			if global, zonas, _ := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermTicketsCrear)); !global &&
				(in.ZonaID == nil || !slices.Contains(zonas, *in.ZonaID)) {
				return Ticket{}, fueraDeAlcance("Tu permiso tickets.crear se limita a otras zonas; indica una zona tuya")
			}

			// Categoría → SLA (el de la categoría o el por defecto) → objetivos de la prioridad.
			var categoria *soportebd.SoporteCategoria
			if in.CategoriaID != nil {
				c, err := q.CategoriaPorID(ctx, *in.CategoriaID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!c.Activa || (c.EmpresaID != nil && deref(c.EmpresaID) != deref(empresa)))) {
					return Ticket{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "categoriaId", Message: "La categoría no existe o está inactiva", Rule: "categoria"})
				}
				if err != nil {
					return Ticket{}, err
				}
				categoria = &c
			}
			sla, err := slaPara(ctx, q, categoria, empresa)
			if err != nil {
				return Ticket{}, err
			}
			if in.TicketPadreID != nil {
				padre, err := q.TicketPorID(ctx, *in.TicketPadreID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && (padre.Estado == EstadoCerrado || deref(padre.EmpresaID) != deref(empresa) || !enEmpresa(ctx, padre))) {
					return Ticket{}, nucleo.ReglaNegocio("ticket_padre_invalido", "El incidente maestro no existe, está cerrado o es de otra empresa")
				}
				if err != nil {
					return Ticket{}, err
				}
			}

			nuevo := soportebd.SoporteTicket{SlaInicio: ahora, Estado: EstadoNuevo}
			var cal *Calendario
			if sla != nil {
				nuevo.SlaID, nuevo.CalendarioID = &sla.ID, sla.CalendarioID
				obj, err := q.ObjetivoSLA(ctx, soportebd.ObjetivoSLAParams{SlaID: sla.ID, Prioridad: prioridad})
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return Ticket{}, err
				}
				if err == nil {
					nuevo.SlaRespuestaMin, nuevo.SlaResolucionMin = &obj.RespuestaMin, &obj.ResolucionMin
				}
				if cal, err = cargarCalendario(ctx, q, sla.CalendarioID); err != nil {
					return Ticket{}, err
				}
			}
			aplicarSLA(&nuevo, cal)

			var catID *int64
			if categoria != nil {
				catID = &categoria.ID
			}
			f, err := q.CrearTicket(ctx, soportebd.CrearTicketParams{
				EmpresaID: empresa, ClienteID: in.ClienteID, ServicioID: in.ServicioID, ZonaID: in.ZonaID,
				ContactoNombre: contacto.Nombre, ContactoTelefono: contacto.Telefono, ContactoCorreo: contacto.Correo,
				CategoriaID: catID, Prioridad: prioridad, Origen: origen, Asunto: asunto, TicketPadreID: in.TicketPadreID,
				SlaID: nuevo.SlaID, CalendarioID: nuevo.CalendarioID, SlaInicio: ahora,
				SlaRespuestaMin: nuevo.SlaRespuestaMin, SlaResolucionMin: nuevo.SlaResolucionMin,
				RiesgoRespuesta: nuevo.RiesgoRespuesta, VenceRespuesta: nuevo.VenceRespuesta,
				RiesgoResolucion: nuevo.RiesgoResolucion, VenceResolucion: nuevo.VenceResolucion,
			})
			if err != nil {
				return Ticket{}, err
			}
			t.RegistrarCambios("ticket", f.ID, nil, f)
			if descripcion != "" {
				if _, err := mensaje(ctx, t, q, f, true, descripcion); err != nil {
					return Ticket{}, err
				}
			}
			if err := historial(ctx, t, q, f, "creado", nil, &f.Estado, nil, map[string]any{"origen": origen, "prioridad": prioridad}); err != nil {
				return Ticket{}, err
			}
			var codigoCategoria *string
			if categoria != nil {
				codigoCategoria = &categoria.Codigo
			}
			if err := t.Emitir("ticket.creado", map[string]any{
				"ticket_id": f.ID, "numero": f.Numero, "empresa_id": f.EmpresaID, "cliente_id": f.ClienteID,
				"servicio_id": f.ServicioID, "categoria_id": f.CategoriaID, "categoria": codigoCategoria,
				"prioridad": f.Prioridad, "origen": f.Origen, "ticket_padre_id": f.TicketPadreID,
			}); err != nil {
				return Ticket{}, err
			}
			return aTicket(f, ahora), nil
		},
	})
}

// slaPara elige el SLA activo de la categoría o, si no tiene, el por defecto (nil si no hay).
func slaPara(ctx context.Context, q *soportebd.Queries, categoria *soportebd.SoporteCategoria, empresa *int64) (*soportebd.SoporteSla, error) {
	if categoria != nil && categoria.SlaID != nil {
		s, err := q.SLAPorID(ctx, *categoria.SlaID)
		if err != nil {
			return nil, err
		}
		if s.Activo {
			return &s, nil
		}
	}
	s, err := q.SLAPorDefecto(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
