package mensajeria

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Acciones desde la conversación (contratos/conversaciones.md §6): reutilizan el caso de uso del
// módulo dueño en la misma transacción (un savepoint), y dejan un vínculo y un mensaje de sistema.

func noDisponible(que string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusServiceUnavailable, "servicio_no_disponible", "No disponible", que+" no está disponible en este servicio")
}

// vincular deja el vínculo, el mensaje de sistema y el evento de la conversación.
func (m *Modulo) vincular(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, actor int64, tipo, id, referencia, evento string) error {
	q := mensajeriabd.New(t)
	if err := q.InsertarVinculo(ctx, mensajeriabd.InsertarVinculoParams{ConversacionID: c.ID, Tipo: tipo, EntidadID: id, Referencia: referencia, CreadoPor: &actor}); err != nil {
		return err
	}
	if err := m.sistema(ctx, t, c, EventoSistema{Tipo: evento, ActorNombre: actorNombre(ctx, q, actor), Referencia: &referencia, EntidadID: &id}); err != nil {
		return err
	}
	return t.Emitir("conversacion.vinculo_creado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "tipo": tipo,
		"entidad_id": id, "referencia": referencia, "actor_id": actor})
}

// accionConAsegurar: verifica que ve la conversación, asegura la instantánea del cliente de la
// conversación fuera de transacción y corre la acción final.
func (m *Modulo) asegurarCliente(ctx context.Context, previo *auditoria.Caso[EntradaConversacion, convLeida], id int64, servicio *int64) (convLeida, error) {
	l, err := previo.Ejecutar(ctx, EntradaConversacion{ID: id})
	if err != nil {
		return l, err
	}
	if m.cfg.Argos != nil && l.conv.ClienteID != nil {
		if servicio == nil {
			servicio = l.conv.ServicioID
		}
		if err := m.cfg.Argos.AsegurarInstantanea(ctx, *l.conv.ClienteID, servicio); err != nil {
			return l, err
		}
	}
	return l, nil
}

// Ticket ------------------------------------------------------------------------------------------

// EntradaTicketConv es POST /conversaciones/{id}/crear-ticket.
type EntradaTicketConv struct {
	ID          int64  `json:"-"`
	Asunto      string `json:"asunto" minLength:"3" maxLength:"200"`
	Descripcion string `json:"descripcion,omitempty" maxLength:"20000" doc:"Primer mensaje público del ticket"`
	CategoriaID *int64 `json:"categoriaId,omitempty" minimum:"1"`
	Prioridad   string `json:"prioridad,omitempty" enum:"baja,media,alta,critica"`
	ServicioID  *int64 `json:"servicioId,omitempty" minimum:"1" doc:"Por defecto, el de la conversación"`
	ZonaID      *int64 `json:"zonaId,omitempty" minimum:"1"`
	Clave       string `json:"-"`
}

// VinculoCreado es la respuesta de las acciones: lo creado y la conversación al día.
type VinculoCreado struct {
	Vinculo      VinculoConversacion        `json:"vinculo"`
	Conversacion ConversacionClienteDetalle `json:"conversacion"`
}

// CrearTicketConv crea un ticket ligado a la conversación.
type CrearTicketConv struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaConversacion, convLeida]
	final  *auditoria.Caso[EntradaTicketConv, VinculoCreado]
}

func nuevoCrearTicketConv(m *Modulo) *CrearTicketConv {
	k := &CrearTicketConv{m: m, previo: nuevoLeerConvPara("conversaciones.crear_ticket", nucleo.PermTicketsCrear)}
	k.final = auditoria.Nuevo(auditoria.Caso[EntradaTicketConv, VinculoCreado]{
		Accion: "conversaciones.crear_ticket", Permiso: nucleo.PermTicketsCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTicketConv) (VinculoCreado, error) {
			var v VinculoConversacion
			d, err := m.accionConv(ctx, t, in.ID, 0, func(a alcanceConv, _ *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if m.cfg.Soporte == nil {
					return noDisponible("Crear tickets")
				}
				e := soporte.EntradaCrearTicket{ClienteID: c.ClienteID, ServicioID: c.ServicioID, ZonaID: in.ZonaID, CategoriaID: in.CategoriaID,
					Prioridad: in.Prioridad, Asunto: in.Asunto, Descripcion: in.Descripcion, ClaveIdempotencia: in.Clave}
				if in.ServicioID != nil {
					e.ServicioID = in.ServicioID
				}
				if c.ClienteID == nil {
					nombre := c.Numero
					if c.PerfilNombre != nil {
						nombre = *c.PerfilNombre
					}
					e.Contacto = &soporte.EntradaContacto{Nombre: &nombre, Telefono: &c.Numero}
				}
				tk, err := m.cfg.Soporte.CrearTicket.Caso.Ejecutar(ctx, e)
				if err != nil {
					return err
				}
				v = VinculoConversacion{Tipo: "ticket", ID: strconv.FormatInt(tk.ID, 10), Referencia: tk.Numero, CreadoEn: m.ahora()}
				return m.vincular(ctx, t, c, a.usuario, "ticket", v.ID, tk.Numero, "ticket_creado")
			})
			return VinculoCreado{Vinculo: v, Conversacion: d}, err
		},
	})
	return k
}

// Ejecutar crea el ticket.
func (k *CrearTicketConv) Ejecutar(ctx context.Context, in EntradaTicketConv) (VinculoCreado, error) {
	if _, err := k.m.asegurarCliente(ctx, k.previo, in.ID, in.ServicioID); err != nil {
		return VinculoCreado{}, err
	}
	return k.final.Ejecutar(ctx, in)
}

// Orden ----------------------------------------------------------------------------------------------

// EntradaOrdenConv es POST /conversaciones/{id}/crear-orden.
type EntradaOrdenConv struct {
	ID          int64   `json:"-"`
	Tipo        string  `json:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento"`
	Descripcion string  `json:"descripcion,omitempty" maxLength:"4000"`
	TicketID    *int64  `json:"ticketId,omitempty" minimum:"1" doc:"Ticket de origen (p. ej. el creado desde la conversación)"`
	ServicioID  *int64  `json:"servicioId,omitempty" minimum:"1" doc:"Por defecto, el de la conversación"`
	Direccion   *string `json:"direccion,omitempty" maxLength:"300" doc:"Obligatoria si no sale de la instantánea de Argos"`
	ZonaID      *int64  `json:"zonaId,omitempty" minimum:"1"`
	CuadrillaID *int64  `json:"cuadrillaId,omitempty" minimum:"1"`
	Clave       string  `json:"-"`
}

// CrearOrdenConv crea una orden ligada a la conversación.
type CrearOrdenConv struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaConversacion, convLeida]
	final  *auditoria.Caso[EntradaOrdenConv, VinculoCreado]
}

func nuevoCrearOrdenConv(m *Modulo) *CrearOrdenConv {
	k := &CrearOrdenConv{m: m, previo: nuevoLeerConvPara("conversaciones.crear_orden", nucleo.PermOrdenesCrear)}
	k.final = auditoria.Nuevo(auditoria.Caso[EntradaOrdenConv, VinculoCreado]{
		Accion: "conversaciones.crear_orden", Permiso: nucleo.PermOrdenesCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaOrdenConv) (VinculoCreado, error) {
			var v VinculoConversacion
			d, err := m.accionConv(ctx, t, in.ID, 0, func(a alcanceConv, _ *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if m.cfg.Campo == nil {
					return noDisponible("Crear órdenes")
				}
				e := campo.EntradaCrearOrden{Tipo: in.Tipo, TicketID: in.TicketID, Descripcion: in.Descripcion, Direccion: in.Direccion,
					ZonaID: in.ZonaID, CuadrillaID: in.CuadrillaID, ClaveIdempotencia: in.Clave}
				if in.TicketID == nil {
					e.ClienteID, e.ServicioID = c.ClienteID, c.ServicioID
					if in.ServicioID != nil {
						e.ServicioID = in.ServicioID
					}
					nombre := c.Numero
					if c.PerfilNombre != nil {
						nombre = *c.PerfilNombre
					}
					e.Contacto = &campo.EntradaContactoOrden{Nombre: &nombre, Telefono: &c.Numero}
				}
				o, err := m.cfg.Campo.CrearOrden.Caso.Ejecutar(ctx, e)
				if err != nil {
					return err
				}
				v = VinculoConversacion{Tipo: "orden", ID: strconv.FormatInt(o.ID, 10), Referencia: o.Numero, CreadoEn: m.ahora()}
				return m.vincular(ctx, t, c, a.usuario, "orden", v.ID, o.Numero, "orden_creada")
			})
			return VinculoCreado{Vinculo: v, Conversacion: d}, err
		},
	})
	return k
}

// Ejecutar crea la orden.
func (k *CrearOrdenConv) Ejecutar(ctx context.Context, in EntradaOrdenConv) (VinculoCreado, error) {
	if _, err := k.m.asegurarCliente(ctx, k.previo, in.ID, in.ServicioID); err != nil {
		return VinculoCreado{}, err
	}
	return k.final.Ejecutar(ctx, in)
}

// Prospecto -------------------------------------------------------------------------------------------

// EntradaProspectoConv es POST /conversaciones/{id}/crear-prospecto (la entrada de POST /prospectos).
type EntradaProspectoConv struct {
	ID int64 `json:"-"`
	comercial.EntradaCrearProspecto
}

func nuevoCrearProspectoConv(m *Modulo) *auditoria.Caso[EntradaProspectoConv, VinculoCreado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaProspectoConv, VinculoCreado]{
		Accion: "conversaciones.crear_prospecto", Permiso: nucleo.PermProspectosCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProspectoConv) (VinculoCreado, error) {
			var v VinculoConversacion
			d, err := m.accionConv(ctx, t, in.ID, 0, func(a alcanceConv, _ *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if m.cfg.Comercial == nil {
					return noDisponible("Crear prospectos")
				}
				e := in.EntradaCrearProspecto
				if e.Telefono == nil {
					e.Telefono = &c.Numero
				}
				if e.Origen == "" {
					e.Origen = "otro"
				}
				if e.OrigenDetalle == nil {
					e.OrigenDetalle = nombrePtr("WhatsApp")
				}
				p, err := m.cfg.Comercial.CrearProspecto.Ejecutar(ctx, e)
				if err != nil {
					return err
				}
				c.ProspectoID = &p.ID
				if c.Asociacion == nil {
					c.Asociacion = nombrePtr("manual")
				}
				v = VinculoConversacion{Tipo: "prospecto", ID: strconv.FormatInt(p.ID, 10), Referencia: p.Numero, CreadoEn: m.ahora()}
				return m.vincular(ctx, t, c, a.usuario, "prospecto", v.ID, p.Numero, "prospecto_creado")
			})
			return VinculoCreado{Vinculo: v, Conversacion: d}, err
		},
	})
}

// Boletas (contrato con Argos §7.2) -------------------------------------------------------------------

// EntradaBoleta es POST /conversaciones/{id}/boletas.
type EntradaBoleta struct {
	ID               int64     `json:"-"`
	AdjuntoID        uuid.UUID `json:"adjuntoId" doc:"Imagen o PDF de esta conversación (la que mandó el cliente)"`
	ServicioID       *int64    `json:"servicioId,omitempty" minimum:"1"`
	Metodo           string    `json:"metodo" enum:"deposito,transferencia,ach,remesa"`
	CuentaBancariaID int64     `json:"cuentaBancariaId" minimum:"1" doc:"Cuenta de la empresa (GET /cuentas-bancarias de Argos)"`
	BancoOrigenID    *int64    `json:"bancoOrigenId,omitempty" minimum:"1" doc:"Obligatorio con transferencia y ach"`
	Referencia       string    `json:"referencia" minLength:"1" maxLength:"60"`
	Monto            string    `json:"monto" pattern:"^[0-9]{1,10}\\.[0-9]{2}$" doc:"Decimal con 2 decimales, en GTQ"`
	FechaDeposito    string    `json:"fechaDeposito" format:"date" doc:"AAAA-MM-DD, no futura"`
	Nota             *string   `json:"nota,omitempty" maxLength:"500"`
}

// Boleta es una boleta mandada (o por mandar) a Argos.
type Boleta struct {
	ID            uuid.UUID `json:"id"`
	Estado        string    `json:"estado" enum:"por_enviar,registrada,fallida,aprobada,rechazada"`
	ComprobanteID *int64    `json:"comprobanteId" nullable:"true" doc:"Id del comprobante en Argos"`
	Detalle       *string   `json:"detalle" nullable:"true"`
	Referencia    string    `json:"referencia"`
	Monto         string    `json:"monto"`
	CreadaEn      time.Time `json:"creadaEn"`
}

func aBoleta(f mensajeriabd.MensajeriaBoleta) Boleta {
	monto := ""
	if v, err := f.Monto.Value(); err == nil && v != nil {
		monto, _ = v.(string)
	}
	return Boleta{ID: f.ID, Estado: f.Estado, ComprobanteID: f.ComprobanteID, Detalle: f.Detalle, Referencia: f.Referencia, Monto: monto, CreadaEn: f.CreatedAt}
}

func nuevoRegistrarBoleta(m *Modulo) *auditoria.Caso[EntradaBoleta, Boleta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaBoleta, Boleta]{
		Accion: "conversaciones.registrar_boleta", Permiso: nucleo.PermConversacionesRegistrarBoleta,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaBoleta) (Boleta, error) {
			if m.cfg.Argos == nil {
				return Boleta{}, noDisponible("Registrar boletas en Argos")
			}
			fecha, err := time.Parse("2006-01-02", in.FechaDeposito)
			if err != nil {
				return Boleta{}, campoConv("fechaDeposito", "Fecha inválida (AAAA-MM-DD)", "date")
			}
			if fecha.After(m.ahora().In(m.cfg.Zona)) {
				return Boleta{}, campoConv("fechaDeposito", "La fecha del depósito no puede ser futura", "no_futura")
			}
			if (in.Metodo == "transferencia" || in.Metodo == "ach") && in.BancoOrigenID == nil {
				return Boleta{}, campoConv("bancoOrigenId", "Obligatorio con transferencia y ach", "required")
			}
			var monto pgtype.Numeric
			if err := monto.Scan(in.Monto); err != nil {
				return Boleta{}, campoConv("monto", "Monto inválido", "monto")
			}
			var b Boleta
			_, err = m.accionConv(ctx, t, in.ID, 0, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if c.ClienteID == nil {
					return reglaConv("sin_cliente", "Asocia la conversación a un cliente de Argos antes de registrar su boleta")
				}
				servicio := c.ServicioID
				if in.ServicioID != nil {
					servicio = in.ServicioID
				}
				if err := clienteEnEmpresa(ctx, q, empresaDeConv(*c), *c.ClienteID, servicio); err != nil {
					return err
				}
				adj, err := adjuntos.DeEntidad(ctx, t, in.AdjuntoID, EntidadAdjuntosConversacion, strconv.FormatInt(c.ID, 10))
				if err != nil || adj.Estado != adjuntos.EstadoCompleto || adj.Tamano > 5<<20 ||
					(!strings.HasPrefix(adj.Mime, "image/") && adj.Mime != "application/pdf") {
					return reglaConv("adjunto_no_listo", "La boleta debe ser una imagen o un PDF completo de esta conversación, de hasta 5 MB")
				}
				id, err := uuid.NewV7()
				if err != nil {
					return err
				}
				f, err := q.InsertarBoleta(ctx, mensajeriabd.InsertarBoletaParams{ID: id, EmpresaID: c.EmpresaID, ConversacionID: c.ID, ClienteID: *c.ClienteID,
					ServicioID: servicio, AdjuntoID: in.AdjuntoID, Metodo: in.Metodo, CuentaBancariaID: in.CuentaBancariaID, BancoOrigenID: in.BancoOrigenID,
					Referencia: in.Referencia, Monto: monto, FechaDeposito: pgtype.Date{Time: fecha, Valid: true}, Nota: in.Nota,
					RegistradaPor: &a.usuario, ProximoIntento: m.ahora()})
				if err != nil {
					return err
				}
				b = aBoleta(f)
				t.RegistrarCambios("boleta", f.ID, nil, map[string]any{"cliente_id": f.ClienteID, "servicio_id": f.ServicioID, "metodo": f.Metodo,
					"referencia": f.Referencia, "monto": in.Monto, "adjunto_id": f.AdjuntoID})
				if err := m.vincular(ctx, t, c, a.usuario, "boleta", f.ID.String(), "Boleta "+f.Referencia, "boleta_registrada"); err != nil {
					return err
				}
				return t.Emitir("boleta.registrada", map[string]any{"boleta_id": f.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID,
					"cliente_id": f.ClienteID, "servicio_id": f.ServicioID})
			})
			return b, err
		},
	})
}

// Despacho de boletas: el reloj del proceso las manda a Argos (POST /comprobantes multipart) con
// reintentos a 1 min, 5 min, 30 min, 2 h y 12 h; un rechazo de negocio la deja fallida.

type boletaPorEnviar struct {
	ID    uuid.UUID `json:"boleta_id"`
	nada  bool
	f     mensajeriabd.MensajeriaBoleta
	datos []byte
	mime  string
	a     argos.AtencionIris
}

type resultadoBoleta struct {
	ID            uuid.UUID `json:"boleta_id"`
	ComprobanteID *int64    `json:"comprobante_id,omitempty"`
	Error         string    `json:"error,omitempty"`
	Temporal      bool      `json:"temporal,omitempty"`
}

func nuevoPrepararBoleta(m *Modulo) *auditoria.Caso[uuid.UUID, boletaPorEnviar] {
	return auditoria.Nuevo(auditoria.Caso[uuid.UUID, boletaPorEnviar]{
		Accion: "mensajeria.preparar_boleta", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[boletaPorEnviar],
		Logica: func(ctx context.Context, t *auditoria.Tx, id uuid.UUID) (boletaPorEnviar, error) {
			q := mensajeriabd.New(t)
			f, err := q.BoletaParaEnviar(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return boletaPorEnviar{nada: true}, nil
			}
			if err != nil {
				return boletaPorEnviar{}, err
			}
			if f.Estado != "por_enviar" {
				return boletaPorEnviar{nada: true}, nil
			}
			// Reservada 5 min: otro despacho no la toma mientras se manda.
			if _, err := q.ResultadoBoleta(ctx, mensajeriabd.ResultadoBoletaParams{ID: id, Estado: f.Estado, ComprobanteID: f.ComprobanteID,
				Detalle: f.Detalle, ProximoIntento: m.ahora().Add(5 * time.Minute)}); err != nil {
				return boletaPorEnviar{}, err
			}
			datos, adj, err := adjuntos.Contenido(ctx, m.cfg.Almacen, t, f.AdjuntoID, EntidadAdjuntosConversacion, strconv.FormatInt(f.ConversacionID, 10), 5<<20)
			if err != nil {
				return boletaPorEnviar{}, err
			}
			empresa := int64(0)
			if f.EmpresaID != nil {
				empresa = *f.EmpresaID
			}
			usuario := int64(0)
			if f.RegistradaPor != nil {
				usuario = *f.RegistradaPor
			}
			return boletaPorEnviar{ID: id, f: f, datos: datos, mime: adj.Mime,
				a: atencionDe(alcanceConv{usuario: usuario, empresa: empresa}, f.ConversacionID)}, nil
		},
	})
}

func nuevoResultadoBoleta(m *Modulo) *auditoria.Caso[resultadoBoleta, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[resultadoBoleta, struct{}]{
		Accion: "mensajeria.enviar_boleta", Acceso: auditoria.Autenticado,
		AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoBoleta) (struct{}, error) {
			q := mensajeriabd.New(t)
			f, err := q.BoletaParaEnviar(ctx, in.ID)
			if err != nil {
				return struct{}{}, err
			}
			if f.Estado != "por_enviar" {
				return struct{}{}, nil
			}
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			t.FijarEntidad("boleta", f.ID)
			ahora := m.ahora()
			p := mensajeriabd.ResultadoBoletaParams{ID: f.ID, Estado: "registrada", ComprobanteID: in.ComprobanteID, ProximoIntento: ahora}
			switch {
			case in.ComprobanteID != nil:
			case in.Temporal && int(f.Intentos) < len(EsperasWebhook):
				p.Estado, p.Detalle, p.ProximoIntento = "por_enviar", nombrePtr(recortar(in.Error, 300)), ahora.Add(EsperasWebhook[f.Intentos])
				_, err := q.ResultadoBoleta(ctx, p)
				return struct{}{}, err
			default:
				p.Estado, p.Detalle = "fallida", nombrePtr(recortar(in.Error, 300))
			}
			g, err := q.ResultadoBoleta(ctx, p)
			if err != nil {
				return struct{}{}, err
			}
			t.RegistrarCambios("boleta", g.ID, map[string]any{"estado": f.Estado}, map[string]any{"estado": g.Estado, "comprobante_id": g.ComprobanteID})
			c, err := q.ConversacionClienteParaActualizar(ctx, g.ConversacionID)
			if err != nil {
				return struct{}{}, err
			}
			ev := EventoSistema{Tipo: "boleta_registrada", Referencia: nombrePtr("Boleta " + g.Referencia), EntidadID: nombrePtr(g.ID.String())}
			evento := "boleta.registrada_en_argos"
			if g.Estado == "fallida" {
				ev.Tipo, ev.Detalle, evento = "boleta_fallida", g.Detalle, "boleta.fallida"
			}
			if err := m.sistema(ctx, t, &c, ev); err != nil {
				return struct{}{}, err
			}
			if _, err := guardarConv(ctx, q, c, false); err != nil {
				return struct{}{}, err
			}
			if g.Estado == "registrada" {
				return struct{}{}, t.Emitir(evento, map[string]any{"boleta_id": g.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID, "comprobante_id": g.ComprobanteID})
			}
			return struct{}{}, t.Emitir(evento, map[string]any{"boleta_id": g.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID, "detalle": g.Detalle})
		},
	})
}

// despacharBoletas manda las boletas vencidas (hasta 50 por corrida).
func (m *Modulo) despacharBoletas(ctx context.Context, bd nucleo.BD) error {
	if m.cfg.Argos == nil {
		return nil
	}
	ids, err := mensajeriabd.New(bd).BoletasVencidas(ctx, m.ahora())
	if err != nil {
		return err
	}
	var primero error
	for _, id := range ids {
		if err := m.enviarBoleta(ctx, id); err != nil && primero == nil {
			primero = err
		}
	}
	return primero
}

func (m *Modulo) enviarBoleta(ctx context.Context, id uuid.UUID) error {
	p, err := m.prepararBoleta.Ejecutar(ctx, id)
	if err != nil || p.nada {
		return err
	}
	f := p.f
	monto := ""
	if v, err := f.Monto.Value(); err == nil && v != nil {
		monto, _ = v.(string)
	}
	ext := ".jpg"
	switch p.mime {
	case "application/pdf":
		ext = ".pdf"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	}
	in := argos.EntradaComprobante{ClienteID: f.ClienteID, ServicioID: f.ServicioID, Metodo: f.Metodo, CuentaBancariaID: f.CuentaBancariaID,
		BancoOrigenID: f.BancoOrigenID, Referencia: f.Referencia, Monto: monto, FechaDeposito: f.FechaDeposito.Time.Format("2006-01-02"),
		Archivo: conector.Archivo{Campo: "archivo", Nombre: "boleta-" + f.ID.String() + ext, Mime: p.mime, Datos: p.datos}, Nota: f.Nota,
		Clave: "iris:boleta:" + f.ID.String()}
	r, errArgos := m.cfg.Argos.RegistrarComprobante(ctx, p.a, in)
	res := resultadoBoleta{ID: id}
	if errArgos == nil {
		res.ComprobanteID = &r.ID
	} else {
		res.Error, res.Temporal = resumenErrorArgos(errArgos)
	}
	_, err = m.resultadoBoleta.Ejecutar(context.WithoutCancel(ctx), res)
	return err
}

// resumenErrorArgos: el detalle seguro para guardar y si se reintenta (caído, sin conector).
func resumenErrorArgos(err error) (string, bool) {
	var er *conector.ErrorRemoto
	if errors.As(err, &er) {
		if er.Caido() {
			return "Argos no respondió", true
		}
		if er.Status == http.StatusUnauthorized || er.Status == http.StatusForbidden {
			return "Argos rechazó la credencial del conector", true
		}
		d := "Argos respondió " + strconv.Itoa(er.Status)
		if er.Tipo != "" {
			partes := strings.Split(er.Tipo, ":")
			d += " " + recortar(partes[len(partes)-1], 60)
		}
		return d, false
	}
	var p *nucleo.Problema
	if errors.As(err, &p) {
		return p.Codigo(), p.Status >= 500
	}
	return "error inesperado", true
}

// Promesas (contrato con Argos §7.4) -------------------------------------------------------------------

// EntradaSimularPromesa es POST /conversaciones/{id}/promesas/simular.
type EntradaSimularPromesa struct {
	ID         int64   `json:"-"`
	ServicioID int64   `json:"servicioId" minimum:"1"`
	VenceEl    *string `json:"venceEl,omitempty" format:"date" doc:"Día prometido (AAAA-MM-DD)"`
}

// SimulacionPromesaConv es la política de Argos para la promesa (sin crear nada).
type SimulacionPromesaConv struct {
	Resultado string          `json:"resultado" doc:"aprobada, rechazada o pendiente_aprobacion"`
	Regla     *string         `json:"regla" nullable:"true"`
	Motivo    *string         `json:"motivo" nullable:"true"`
	Monto     json.RawMessage `json:"monto" doc:"Lo vencido que cubriría"`
	Limites   json.RawMessage `json:"limites" doc:"plazo_max_dias, fecha_maxima, max_por_periodo, en_periodo, deuda_vencida…"`
}

// EntradaPromesa es POST /conversaciones/{id}/promesas.
type EntradaPromesa struct {
	ID         int64  `json:"-"`
	ServicioID int64  `json:"servicioId" minimum:"1"`
	VenceEl    string `json:"venceEl" format:"date" doc:"Día prometido (AAAA-MM-DD); Iris manda el final de ese día en hora de Guatemala"`
	Clave      string `json:"-"`
}

// PromesaConv es la promesa que pidió Iris y lo que decidió Argos.
type PromesaConv struct {
	ID             uuid.UUID `json:"id"`
	Estado         string    `json:"estado" enum:"activa,rechazada,pendiente_aprobacion"`
	PromesaArgosID *int64    `json:"promesaArgosId" nullable:"true"`
	Monto          *string   `json:"monto" nullable:"true"`
	Regla          *string   `json:"regla" nullable:"true"`
	Motivo         *string   `json:"motivo" nullable:"true"`
	VenceEl        string    `json:"venceEl"`
	CreadaEn       time.Time `json:"creadaEn"`
	Repetida       bool      `json:"-"`
}

func aPromesa(f mensajeriabd.MensajeriaPromesa) PromesaConv {
	return PromesaConv{ID: f.ID, Estado: f.Estado, PromesaArgosID: f.PromesaArgosID, Monto: f.Monto, Regla: f.Regla, Motivo: f.Motivo,
		VenceEl: f.VenceEl.Time.Format("2006-01-02"), CreadaEn: f.CreatedAt}
}

// Promesas reúne simular y crear.
type Promesas struct {
	m       *Modulo
	previo  *auditoria.Caso[entradaPreviaPromesa, previaPromesa]
	final   *auditoria.Caso[entradaFinalPromesa, PromesaConv]
	simular *auditoria.Caso[EntradaConversacion, convLeida]
}

type entradaPreviaPromesa struct {
	ID         int64  `json:"conversacion_id"`
	ServicioID int64  `json:"servicio_id"`
	Clave      string `json:"-"`
	hash       []byte
}

type previaPromesa struct {
	repetida *PromesaConv
	a        alcanceConv
	conv     mensajeriabd.MensajeriaConversacion
}

type entradaFinalPromesa struct {
	EntradaPromesa
	id    uuid.UUID
	hash  []byte
	r     argos.PromesaArgos
	vence time.Time
}

// MarshalJSON: lo auditado.
func (e entradaFinalPromesa) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"conversacion_id": e.ID, "servicio_id": e.ServicioID, "vence_el": e.VenceEl, "promesa_id": e.id,
		"estado_argos": e.r.Estado, "promesa_argos_id": e.r.ID})
}

func nuevasPromesas(m *Modulo) *Promesas {
	k := &Promesas{m: m, simular: nuevoLeerConvPara("conversaciones.simular_promesa", nucleo.PermConversacionesRegistrarPromesa)}
	k.previo = auditoria.Nuevo(auditoria.Caso[entradaPreviaPromesa, previaPromesa]{
		Accion: "conversaciones.registrar_promesa", Permiso: nucleo.PermConversacionesRegistrarPromesa, AuditarExito: sinAuditar[previaPromesa],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaPreviaPromesa) (previaPromesa, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return previaPromesa{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return previaPromesa{}, err
			}
			if c.ClienteID == nil {
				return previaPromesa{}, reglaConv("sin_cliente", "Asocia la conversación a un cliente de Argos antes de pedir una promesa")
			}
			if in.Clave != "" {
				f, err := q.PromesaPorClave(ctx, mensajeriabd.PromesaPorClaveParams{SolicitadaPor: &a.usuario, IdempotencyKey: &in.Clave})
				switch {
				case err == nil:
					if string(f.HashPeticion) != string(in.hash) {
						return previaPromesa{}, idempotenciaReutilizada()
					}
					p := aPromesa(f)
					p.Repetida = true
					return previaPromesa{repetida: &p}, nil
				case !errors.Is(err, pgx.ErrNoRows):
					return previaPromesa{}, err
				}
			}
			return previaPromesa{a: a, conv: c}, nil
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[entradaFinalPromesa, PromesaConv]{
		Accion: "conversaciones.registrar_promesa", Permiso: nucleo.PermConversacionesRegistrarPromesa,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFinalPromesa) (PromesaConv, error) {
			var p PromesaConv
			_, err := m.accionConv(ctx, t, in.ID, 0, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				estado := in.r.Estado
				if estado != "activa" && estado != "rechazada" && estado != "pendiente_aprobacion" {
					estado = "pendiente_aprobacion"
				}
				params := mensajeriabd.InsertarPromesaParams{ID: in.id, EmpresaID: c.EmpresaID, ConversacionID: c.ID, ClienteID: *c.ClienteID,
					ServicioID: in.ServicioID, VenceEl: pgtype.Date{Time: in.vence, Valid: true}, Estado: estado, SolicitadaPor: &a.usuario, HashPeticion: in.hash}
				if in.r.ID != 0 {
					params.PromesaArgosID = &in.r.ID
				}
				if len(in.r.Monto) > 0 && string(in.r.Monto) != "null" {
					params.Monto = nombrePtr(strings.Trim(string(in.r.Monto), `"`))
				}
				if in.r.ResultadoEvaluacion != nil {
					params.Regla, params.Motivo = in.r.ResultadoEvaluacion.Regla, in.r.ResultadoEvaluacion.Motivo
					if params.Motivo != nil {
						params.Motivo = nombrePtr(recortar(*params.Motivo, 300))
					}
				}
				if in.Clave != "" {
					params.IdempotencyKey = &in.Clave
				}
				f, err := q.InsertarPromesa(ctx, params)
				if errors.Is(err, pgx.ErrNoRows) {
					return nucleo.Conflicto("idempotencia_en_curso", "Una petición con esta clave sigue en curso; vuelve a intentar")
				}
				if err != nil {
					return err
				}
				p = aPromesa(f)
				t.RegistrarCambios("promesa", f.ID, nil, map[string]any{"servicio_id": f.ServicioID, "vence_el": p.VenceEl, "estado": f.Estado,
					"promesa_argos_id": f.PromesaArgosID})
				ref := "Promesa " + p.VenceEl
				if err := m.vincular(ctx, t, c, a.usuario, "promesa", f.ID.String(), ref, "promesa_creada"); err != nil {
					return err
				}
				return t.Emitir("promesa.solicitada", map[string]any{"promesa_id": f.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID,
					"cliente_id": f.ClienteID, "servicio_id": f.ServicioID, "estado": f.Estado, "promesa_argos_id": f.PromesaArgosID})
			})
			return p, err
		},
	})
	return k
}

// finDelDia: el final del día local (23:59:59) en UTC.
func finDelDia(dia string, zona *time.Location) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", dia, zona)
	if err != nil {
		return time.Time{}, time.Time{}, campoConv("venceEl", "Fecha inválida (AAAA-MM-DD)", "date")
	}
	return d, d.Add(24*time.Hour - time.Second).UTC(), nil
}

// Simular consulta la política de Argos (sin crear nada).
func (k *Promesas) Simular(ctx context.Context, in EntradaSimularPromesa) (SimulacionPromesaConv, error) {
	l, err := k.simular.Ejecutar(ctx, EntradaConversacion{ID: in.ID})
	if err != nil {
		return SimulacionPromesaConv{}, err
	}
	if k.m.cfg.Argos == nil {
		return SimulacionPromesaConv{}, noDisponible("Simular promesas en Argos")
	}
	if l.conv.ClienteID == nil {
		return SimulacionPromesaConv{}, reglaConv("sin_cliente", "Asocia la conversación a un cliente de Argos")
	}
	var vence *time.Time
	if in.VenceEl != nil {
		_, v, err := finDelDia(*in.VenceEl, k.m.cfg.Zona)
		if err != nil {
			return SimulacionPromesaConv{}, err
		}
		vence = &v
	}
	r, err := k.m.cfg.Argos.SimularPromesa(ctx, atencionDe(l.alcance, l.ID), in.ServicioID, vence)
	if err != nil {
		return SimulacionPromesaConv{}, argos.Problema(err)
	}
	return SimulacionPromesaConv{Resultado: r.Resultado, Regla: r.Regla, Motivo: r.Motivo, Monto: r.Monto, Limites: r.Limites}, nil
}

// Crear pide la promesa a Argos (fuera de transacción) y guarda lo que decidió.
func (k *Promesas) Crear(ctx context.Context, in EntradaPromesa) (PromesaConv, error) {
	if k.m.cfg.Argos == nil {
		return PromesaConv{}, noDisponible("Pedir promesas a Argos")
	}
	dia, vence, err := finDelDia(in.VenceEl, k.m.cfg.Zona)
	if err != nil {
		return PromesaConv{}, err
	}
	b, _ := json.Marshal(in)
	h := sha256.Sum256(append(b, []byte(strconv.FormatInt(in.ID, 10))...))
	pre, err := k.previo.Ejecutar(ctx, entradaPreviaPromesa{ID: in.ID, ServicioID: in.ServicioID, Clave: in.Clave, hash: h[:]})
	if err != nil {
		return PromesaConv{}, err
	}
	if pre.repetida != nil {
		return *pre.repetida, nil
	}
	if k.m.cfg.Argos != nil {
		if err := k.m.cfg.Argos.AsegurarInstantanea(ctx, *pre.conv.ClienteID, &in.ServicioID); err != nil {
			return PromesaConv{}, err
		}
	}
	if err := clienteEnEmpresa(ctx, mensajeriabd.New(nucleo.BDDe(ctx)), pre.a.empresa, *pre.conv.ClienteID, &in.ServicioID); err != nil {
		return PromesaConv{}, err
	}
	// Con la misma Idempotency-Key, el mismo id (y la misma clave hacia Argos) en cada reintento.
	id := uuid.Must(uuid.NewV7())
	if in.Clave != "" {
		id = uuid.NewSHA1(uuid.NameSpaceOID, []byte("iris:promesa:"+strconv.FormatInt(pre.a.usuario, 10)+":"+in.Clave))
	}
	r, err := k.m.cfg.Argos.CrearPromesa(ctx, atencionDe(pre.a, in.ID), in.ServicioID, vence, "iris:promesa:"+id.String())
	if err != nil {
		var er *conector.ErrorRemoto
		if errors.As(err, &er) && er.Status == http.StatusConflict && strings.HasSuffix(er.Tipo, "promesa_abierta") {
			return PromesaConv{}, nucleo.Conflicto("promesa_abierta", "El servicio ya tiene una promesa activa o pendiente en Argos")
		}
		if errors.As(err, &er) && er.Status == http.StatusUnprocessableEntity {
			return PromesaConv{}, reglaConv("promesa_no_permitida", "Argos no acepta la promesa para este servicio (servicio retirado, sin plantilla o fecha no futura)")
		}
		return PromesaConv{}, argos.Problema(err)
	}
	return k.final.Ejecutar(ctx, entradaFinalPromesa{EntradaPromesa: in, id: id, hash: h[:], r: r, vence: dia})
}
