package soporte

import (
	"context"
	"errors"
	"fmt"
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
	EmpresaID     *int64           `json:"empresaId,omitempty" minimum:"1" doc:"Empresa en Argos. El ticket es de la empresa activa: otra distinta da 422 empresa_no_coincide; sin conector (sin empresas registradas) se guarda null y no se puede fijar. Solo el sistema la elige aquí (obligatoria si hay empresas)"`
	ClienteID     *int64           `json:"clienteId,omitempty" minimum:"1" doc:"Cliente en Argos"`
	ServicioID    *int64           `json:"servicioId,omitempty" minimum:"1" doc:"Servicio en Argos (exige clienteId)"`
	ZonaID        *int64           `json:"zonaId,omitempty" minimum:"1" doc:"Zona (Argos); define el alcance"`
	Contacto      *EntradaContacto `json:"contacto,omitempty"`
	CategoriaID   *int64           `json:"categoriaId,omitempty" minimum:"1"`
	Prioridad     string           `json:"prioridad,omitempty" enum:"baja,media,alta,critica" doc:"media por defecto"`
	Origen        string           `json:"origen,omitempty" enum:"panel,portal,correo,telegram,horus,argos,api" doc:"Por defecto, según el canal. correo, telegram, horus y argos solo para integraciones o el sistema; portal solo por el canal portal (si no, 422 origen_no_permitido)"`
	Asunto        string           `json:"asunto" minLength:"3" maxLength:"200"`
	Descripcion   string           `json:"descripcion,omitempty" maxLength:"20000" doc:"Primer mensaje público del ticket"`
	TicketPadreID *int64           `json:"ticketPadreId,omitempty" minimum:"1" doc:"Incidente maestro al que se vincula"`

	// ClaveIdempotencia es la Idempotency-Key de la petición (la pone el handler): si la
	// misma operación ya creó un ticket, se devuelve ese en vez de crear otro.
	ClaveIdempotencia string `json:"-"`
}

// origenesDeIntegracion son los orígenes que solo declara una integración o el sistema.
var origenesDeIntegracion = []string{"correo", "telegram", "horus", "argos"}

// validarOrigen impide que una persona se haga pasar por un canal automático.
func validarOrigen(ctx context.Context, origen string) error {
	switch {
	case slices.Contains(origenesDeIntegracion, origen) && !esSistemaOIntegracion(ctx):
		return nucleo.ReglaNegocio("origen_no_permitido", "El origen "+origen+" solo lo declara una integración o el sistema")
	case origen == "portal" && contexto.CanalDe(ctx) != contexto.CanalPortal:
		return nucleo.ReglaNegocio("origen_no_permitido", "El origen portal solo vale desde el portal")
	}
	return nil
}

// empresaDelTicket: el ticket es de la empresa que resolvió el middleware de I1.
//   - Con empresa activa (persona, token o integración): esa; el cuerpo solo puede repetirla
//     (422 empresa_no_coincide). Una integración opera en las empresas de su usuario dueño.
//   - Sin conector (no hay empresas registradas): null; el cuerpo no puede fijar una (422).
//   - Actor sistema (sin empresa resuelta): sin empresas registradas, null; con empresas, la del
//     cuerpo, obligatoria (422 empresa_requerida) y registrada (422 empresa_no_registrada).
func empresaDelTicket(ctx context.Context, q *soportebd.Queries, pedida *int64) (*int64, error) {
	noCoincide := func(detalle string) error { return nucleo.ReglaNegocio("empresa_no_coincide", detalle) }
	todas, id := filtroEmpresa(ctx)
	if !todas {
		if id != nil && *id == 0 {
			return nil, nucleo.EmpresaRequerida() // no llega: auditoria.Caso corta antes
		}
		if pedida != nil && !mismoID(pedida, id) {
			if id == nil {
				return nil, noCoincide("Sin conector con Argos los tickets se guardan sin empresa")
			}
			return nil, noCoincide("La empresa del ticket debe ser la empresa activa")
		}
		return id, nil
	}
	hay, err := q.HayEmpresas(ctx)
	if err != nil {
		return nil, err
	}
	if !hay {
		if pedida != nil {
			return nil, noCoincide("Sin conector con Argos los tickets se guardan sin empresa")
		}
		return nil, nil
	}
	if pedida == nil {
		return nil, nucleo.ReglaNegocio("empresa_requerida", "Indica la empresa del ticket")
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
			f, err := m.crearTicket(ctx, t, in, opcionesCrear{})
			if err != nil {
				return Ticket{}, err
			}
			return aTicket(f, m.ahora()), nil
		},
	})
}

// opcionesCrear son los datos que solo fijan los casos internos (Horus) al crear.
type opcionesCrear struct {
	// incidente: maestro de un router o servicio de infraestructura, o hallazgo de Horus.
	incidente *datosIncidente
	// nota: nota interna inicial (del sistema).
	nota string
}

type datosIncidente struct {
	maestro    *string
	routerID   *int64
	infraID    *string
	nombre     *string
	hallazgoID *string
	urlHorus   *string
}

// crearTicket es la lógica de tickets.crear, compartida con el portal (actor cliente) y con los
// tickets que abre Horus (actor sistema): valida, calcula el SLA, vincula al incidente maestro
// abierto del router del cliente (ver vincularMaestro), crea, audita y emite ticket.creado.
func (m *Modulo) crearTicket(ctx context.Context, t *auditoria.Tx, in EntradaCrearTicket, op opcionesCrear) (soportebd.SoporteTicket, error) {
	q := soportebd.New(t)
	ahora := m.ahora()
	var cero soportebd.SoporteTicket

	// La misma operación repetida (clave reclamada tras una caída) devuelve su ticket.
	idem := conIdempotencia(ctx, in.ClaveIdempotencia)
	if idem != nil {
		previo, err := q.TicketPorIdempotencia(ctx, idem)
		if err == nil {
			t.FijarEntidad("ticket", previo.ID)
			if err := verificarAcceso(ctx, nucleo.PermTicketsVer, previo); err != nil {
				return cero, err
			}
			t.FijarEmpresa(deref(previo.EmpresaID))
			t.Anotar(map[string]any{"repetida": true})
			return previo, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return cero, err
		}
	}

	asunto, err := texto("asunto", in.Asunto, 3, 200)
	if err != nil {
		return cero, err
	}
	descripcion := strings.TrimSpace(in.Descripcion)
	prioridad := in.Prioridad
	if prioridad == "" {
		prioridad = "media"
	}
	if !slices.Contains(Prioridades, prioridad) {
		return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "prioridad", Message: "Prioridad desconocida", Rule: "enum"})
	}
	origen := in.Origen
	if origen == "" {
		origen = origenPorCanal(contexto.CanalDe(ctx))
	}
	if !slices.Contains(Origenes, origen) {
		return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "origen", Message: "Origen desconocido", Rule: "enum"})
	}
	if err := validarOrigen(ctx, origen); err != nil {
		return cero, err
	}
	if in.ServicioID != nil && in.ClienteID == nil {
		return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "clienteId", Message: "Un servicio exige su cliente", Rule: "required"})
	}
	var contacto EntradaContacto
	if in.Contacto != nil {
		if contacto.Nombre, err = textoOpcional("contacto.nombre", in.Contacto.Nombre, 120); err != nil {
			return cero, err
		}
		if contacto.Telefono, err = textoOpcional("contacto.telefono", in.Contacto.Telefono, 40); err != nil {
			return cero, err
		}
		if contacto.Correo, err = textoOpcional("contacto.correo", in.Contacto.Correo, 254); err != nil {
			return cero, err
		}
		if contacto.Correo != nil {
			c := strings.ToLower(*contacto.Correo)
			contacto.Correo = &c
		}
	}

	empresa, err := empresaDelTicket(ctx, q, in.EmpresaID)
	if err != nil {
		return cero, err
	}
	t.FijarEmpresa(deref(empresa))

	// Al crear no hay cuadrilla: un alcance limitado se juzga por la zona. El actor cliente no
	// tiene permisos de usuario: su alcance es su propio cliente (lo fija el portal).
	if contexto.ActorDe(ctx).Tipo != contexto.ActorCliente {
		if global, zonas, _ := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermTicketsCrear)); !global &&
			(in.ZonaID == nil || !slices.Contains(zonas, *in.ZonaID)) {
			return cero, fueraDeAlcance("Tu permiso tickets.crear se limita a otras zonas; indica una zona tuya")
		}
	}

	// Categoría → SLA (el de la categoría o el por defecto) → objetivos de la prioridad.
	var categoria *soportebd.SoporteCategoria
	if in.CategoriaID != nil {
		c, err := q.CategoriaPorID(ctx, *in.CategoriaID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!c.Activa || (c.EmpresaID != nil && deref(c.EmpresaID) != deref(empresa)))) {
			return cero, nucleo.Validacion(nucleo.ErrorCampo{Field: "categoriaId", Message: "La categoría no existe o está inactiva", Rule: "categoria"})
		}
		if err != nil {
			return cero, err
		}
		categoria = &c
	}
	sla, err := slaPara(ctx, q, categoria, empresa)
	if err != nil {
		return cero, err
	}
	padreID := in.TicketPadreID
	if padreID != nil {
		// FOR SHARE: el maestro no se cierra hasta el COMMIT. Si el actor no lo puede ver,
		// el mismo 422 (no revela que existe).
		padre, err := q.TicketPorIDCompartido(ctx, *padreID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (padre.Estado == EstadoCerrado ||
			!mismoID(padre.EmpresaID, empresa) || verificarAcceso(ctx, nucleo.PermTicketsVer, padre) != nil)) {
			return cero, nucleo.ReglaNegocio("ticket_padre_invalido", "El incidente maestro no existe, está cerrado o es de otra empresa")
		}
		if err != nil {
			return cero, err
		}
	}
	var maestro *soportebd.SoporteTicket
	if padreID == nil && op.incidente == nil && origen != "horus" {
		if maestro, err = vincularMaestro(ctx, q, empresa, in.ClienteID, in.ServicioID); err != nil {
			return cero, err
		}
		if maestro != nil {
			padreID = &maestro.ID
		}
	}

	nuevo := soportebd.SoporteTicket{SlaInicio: ahora, Estado: EstadoNuevo}
	var cal *Calendario
	if sla != nil {
		nuevo.SlaID, nuevo.CalendarioID = &sla.ID, sla.CalendarioID
		obj, err := q.ObjetivoSLA(ctx, soportebd.ObjetivoSLAParams{SlaID: sla.ID, Prioridad: prioridad})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return cero, err
		}
		if err == nil {
			nuevo.SlaRespuestaMin, nuevo.SlaResolucionMin = &obj.RespuestaMin, &obj.ResolucionMin
		}
		if cal, err = cargarCalendario(ctx, q, sla.CalendarioID); err != nil {
			return cero, err
		}
	}
	aplicarSLA(&nuevo, cal)

	var catID *int64
	if categoria != nil {
		catID = &categoria.ID
	}
	inc := datosIncidente{}
	if op.incidente != nil {
		inc = *op.incidente
	}
	f, err := q.CrearTicket(ctx, soportebd.CrearTicketParams{
		EmpresaID: empresa, ClienteID: in.ClienteID, ServicioID: in.ServicioID, ZonaID: in.ZonaID,
		ContactoNombre: contacto.Nombre, ContactoTelefono: contacto.Telefono, ContactoCorreo: contacto.Correo,
		CategoriaID: catID, Prioridad: prioridad, Origen: origen, Asunto: asunto, TicketPadreID: padreID,
		SlaID: nuevo.SlaID, CalendarioID: nuevo.CalendarioID, SlaInicio: ahora,
		SlaRespuestaMin: nuevo.SlaRespuestaMin, SlaResolucionMin: nuevo.SlaResolucionMin,
		RiesgoRespuesta: nuevo.RiesgoRespuesta, VenceRespuesta: nuevo.VenceRespuesta,
		RiesgoResolucion: nuevo.RiesgoResolucion, VenceResolucion: nuevo.VenceResolucion, Idempotencia: idem,
		Maestro: inc.maestro, RouterID: inc.routerID, InfraID: inc.infraID, IncidenteNombre: inc.nombre,
		HallazgoID: inc.hallazgoID, UrlHorus: inc.urlHorus,
	})
	if err != nil {
		return cero, err
	}
	t.FijarEntidad("ticket", f.ID)
	t.RegistrarCambios("ticket", f.ID, nil, f)
	if descripcion != "" {
		if _, err := mensajeIdempotente(ctx, t, q, f, true, descripcion, nil); err != nil {
			return cero, err
		}
	}
	if op.nota != "" {
		if _, err := mensajeSistema(ctx, q, f, false, op.nota); err != nil {
			return cero, err
		}
	}
	if maestro != nil {
		nota := fmt.Sprintf("Vinculado automáticamente al incidente maestro %s (%s): el servicio del cliente está en ese router según la instantánea de Argos.",
			maestro.Numero, maestro.Asunto)
		if _, err := mensajeSistema(ctx, q, f, false, nota); err != nil {
			return cero, err
		}
		t.Anotar(map[string]any{"vinculado_a": maestro.ID})
	}
	if err := historial(ctx, t, q, f, "creado", nil, &f.Estado, nil, map[string]any{"origen": origen, "prioridad": prioridad}); err != nil {
		return cero, err
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
		return cero, err
	}
	return f, nil
}

// vincularMaestro busca el incidente maestro abierto (de Horus) del router del cliente, según
// la instantánea de Argos (integracion.argos_servicios.router_id): el del servicio indicado o,
// sin servicio, el de los servicios del cliente si todos los que tienen maestro abierto están
// en un mismo router. Sin cliente, sin instantánea o con varios routers posibles, ninguno. Si
// hay maestro de router y de servicio de infraestructura del mismo router, gana el del router.
// El maestro queda FOR SHARE (no se cierra hasta el COMMIT).
func vincularMaestro(ctx context.Context, q *soportebd.Queries, empresa, cliente, servicio *int64) (*soportebd.SoporteTicket, error) {
	if cliente == nil {
		return nil, nil
	}
	candidatos, err := q.MaestrosParaVincular(ctx, soportebd.MaestrosParaVincularParams{EmpresaID: empresa, ServicioID: servicio, ClienteID: cliente})
	if err != nil || len(candidatos) == 0 {
		return nil, err
	}
	for _, c := range candidatos[1:] {
		if !mismoID(c.RouterID, candidatos[0].RouterID) {
			return nil, nil // varios routers: no se adivina
		}
	}
	m, err := q.TicketPorIDCompartido(ctx, candidatos[0].ID)
	if err != nil {
		return nil, err
	}
	if m.Estado != EstadoNuevo && m.Estado != EstadoAbierto && m.Estado != EstadoEnEspera {
		return nil, nil
	}
	return &m, nil
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
