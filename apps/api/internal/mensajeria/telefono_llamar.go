package mensajeria

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Mi teléfono, abrirlo, llamar y la llamada directa (contratos/voz.md §3.12, E4c).

const (
	// vidaTicketTelefono: el ticket para abrir el WebSocket.
	vidaTicketTelefono = 30 * time.Second
	// vidaMarcacion: cuánto espera un destino de llamar su INVITE.
	vidaMarcacion = 60 * time.Second
	// RutaTelefonoSIP es el WebSocket del teléfono (subprotocolo sip).
	RutaTelefonoSIP = "/api/v1/mensajeria/telefono/sip"
)

// Motivos por los que el teléfono no está disponible.
const (
	motivoSinCentralita      = "sin_centralita"
	motivoSinExtension       = "sin_extension"
	motivoExtensionEscritorio = "extension_escritorio"
	motivoSinCredencial      = "sin_credencial"
	motivoTelefonoApagado    = "telefono_apagado"
	motivoTURN               = "turn_no_configurado"
)

var detallesTelefono = map[string]string{
	motivoSinCentralita:       "Tu empresa no tiene una centralita conectada",
	motivoSinExtension:        "No tienes una extensión: pide que te asignen una en Administración",
	motivoExtensionEscritorio: "Tu extensión es de un teléfono de escritorio, no del teléfono de Iris",
	motivoSinCredencial:       "Tu extensión no tiene la contraseña SIP guardada",
	motivoTelefonoApagado:     "El teléfono de Iris está apagado en tu empresa",
	motivoTURN:                "La instalación no tiene el relevo de audio (TURN)",
}

func problemaTelefono(motivo string) *nucleo.Problema {
	if motivo == motivoTURN {
		return nucleo.NuevoProblema(http.StatusServiceUnavailable, motivo, "Teléfono no disponible", detallesTelefono[motivo])
	}
	return nucleo.ReglaNegocio(motivo, detallesTelefono[motivo])
}

// telefonoDisponible: la extensión del usuario y, si no se puede usar el teléfono, por qué.
func (m *Modulo) telefonoDisponible(ctx context.Context, q *mensajeriabd.Queries) (mensajeriabd.TelefonoDeUsuarioRow, string, error) {
	empresa := empresaDeCtx(ctx)
	x, err := q.TelefonoDeUsuario(ctx, mensajeriabd.TelefonoDeUsuarioParams{Usuario: contexto.ActorDe(ctx).ID, Empresa: empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := q.CentralitaDeEmpresa(ctx, empresa); errors.Is(err, pgx.ErrNoRows) {
			return x, motivoSinCentralita, nil
		} else if err != nil {
			return x, "", err
		}
		return x, motivoSinExtension, nil
	}
	if err != nil {
		return x, "", err
	}
	switch {
	case x.Tipo != "navegador":
		return x, motivoExtensionEscritorio, nil
	case x.SipSecretoCifrado == nil || x.SipUsuario == nil:
		return x, motivoSinCredencial, nil
	case !x.TelefonoActivo || x.WssHuella == nil:
		return x, motivoTelefonoApagado, nil
	case !m.cfg.TURN.Configurado():
		return x, motivoTURN, nil
	}
	return x, "", nil
}

// soloSesion: una persona con su sesión (cookie del panel o de la app); un token de API o una
// integración reciben 404.
func soloSesion(ctx context.Context) error {
	if err := soloPersonas(ctx); err != nil || contexto.SesionDe(ctx).ID == 0 {
		return nucleo.NoEncontrado("El teléfono no existe")
	}
	return nil
}

// Mi teléfono -------------------------------------------------------------------------------------------

// PuedeTelefono: lo que la persona puede hacer con su teléfono.
type PuedeTelefono struct {
	Llamar     bool `json:"llamar"`
	Transferir bool `json:"transferir"`
	Recibir    bool `json:"recibir"`
}

// LimiteTelefono: salientes externas de hoy (hora de Guatemala).
type LimiteTelefono struct {
	PorDia    int32 `json:"porDia"`
	UsadasHoy int32 `json:"usadasHoy"`
}

// TelefonoMio es GET /telefono.
type TelefonoMio struct {
	Disponible   bool           `json:"disponible"`
	Motivo       *string        `json:"motivo" nullable:"true" enum:"sin_centralita,sin_extension,extension_escritorio,sin_credencial,telefono_apagado,turn_no_configurado"`
	Extension    *string        `json:"extension" nullable:"true"`
	Dispositivos []string       `json:"dispositivos" doc:"Dónde se puede usar: panel (llama y recibe) y app (primera etapa, solo llama)"`
	Puede        PuedeTelefono  `json:"puede"`
	Limite       LimiteTelefono `json:"limite"`
}

// EntradaAbrirTelefono es POST /telefono/abrir.
type EntradaAbrirTelefono struct {
	Dispositivo string `json:"dispositivo" enum:"panel,app" doc:"panel llama y recibe; app (primera etapa) solo llama"`
}

// SIPTelefono: la identidad SIP de la extensión (sin contraseña).
type SIPTelefono struct {
	URI     string `json:"uri"`
	Usuario string `json:"usuario"`
	Dominio string `json:"dominio"`
	Nombre  string `json:"nombre"`
}

// SesionTelefono es la respuesta de abrir: con qué conectarse. Nunca trae la contraseña SIP.
type SesionTelefono struct {
	WS          string        `json:"ws" doc:"Ruta del WebSocket (subprotocolo sip) con el ticket de un solo uso"`
	TicketVence time.Time     `json:"ticketVence"`
	SIP         SIPTelefono   `json:"sip"`
	ICE         []ServidorICE `json:"ice" doc:"Servidores TURN con credencial efímera"`
	PoliticaIce string        `json:"politicaIce" enum:"relay" doc:"El audio siempre va por el relevo de Iris"`
	IceVence    time.Time     `json:"iceVence"`
}

// Llamar ------------------------------------------------------------------------------------------------

// EntradaLlamarTelefono es POST /telefono/llamar: uno de clienteId, ticketId, ordenId, usuarioId o numero.
type EntradaLlamarTelefono struct {
	ClienteID *int64  `json:"clienteId,omitempty" minimum:"1"`
	TicketID  *int64  `json:"ticketId,omitempty" minimum:"1"`
	OrdenID   *int64  `json:"ordenId,omitempty" minimum:"1"`
	UsuarioID *int64  `json:"usuarioId,omitempty" minimum:"1" doc:"Un compañero: su extensión"`
	Numero    *string `json:"numero,omitempty" maxLength:"20" doc:"Teclado: un número de Guatemala o una extensión o grupo del equipo"`
	Telefono  *string `json:"telefono,omitempty" enum:"principal,movil,contacto" doc:"Con clienteId, ticketId u ordenId: cuál de sus teléfonos"`
}

// MarshalJSON: a la auditoría nunca va el número del teclado.
func (e EntradaLlamarTelefono) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"cliente_id": e.ClienteID, "ticket_id": e.TicketID, "orden_id": e.OrdenID, "usuario_id": e.UsuarioID,
		"teclado": e.Numero != nil, "telefono": e.Telefono})
}

// MarcacionTelefono es la respuesta de llamar.
type MarcacionTelefono struct {
	Destino string    `json:"destino" doc:"La URI SIP que el teléfono marca (el puente solo deja pasar esta)"`
	Llamada *Llamada  `json:"llamada" nullable:"true" doc:"La llamada en marcando (null en una interna)"`
	Vence   time.Time `json:"vence" doc:"Hasta cuándo vale el destino"`
}

// EntradaLlamadaDirecta es POST /telefono/llamada-directa.
type EntradaLlamadaDirecta struct {
	OrdenID int64 `json:"ordenId" minimum:"1"`
}

// LlamadaDirecta: la llamada anotada y el número para abrir tel:.
type LlamadaDirecta struct {
	Llamada  Llamada `json:"llamada"`
	Telefono string  `json:"telefono" doc:"E.164 para abrir tel: en el celular"`
}

// PersonaDestino y DestinosTransferencia son GET /telefono/transferir/destinos.
type PersonaDestino struct {
	UsuarioID int64  `json:"usuarioId"`
	Nombre    string `json:"nombre"`
	Extension string `json:"extension"`
}

// DestinosTransferencia: personas y grupos de la empresa.
type DestinosTransferencia struct {
	Personas []PersonaDestino `json:"personas"`
	Grupos   []GrupoDestino   `json:"grupos"`
}

// GrupoDestino es un grupo activo.
type GrupoDestino struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Numero string `json:"numero"`
}

// nacionalGT: los 8 dígitos de un número de Guatemala (fijo o móvil: empieza por 2 a 7), o "".
func nacionalGT(crudo string) string {
	s := strings.TrimSpace(crudo)
	for _, c := range s {
		if !(c >= '0' && c <= '9') && !strings.ContainsRune("+ -()", c) {
			return ""
		}
	}
	d := soloDigitos(s)
	if strings.HasPrefix(s, "+") && !strings.HasPrefix(d, "502") {
		return ""
	}
	if len(d) == 11 && strings.HasPrefix(d, "502") {
		d = d[3:]
	}
	if len(d) != 8 || d[0] < '2' || d[0] > '7' {
		return ""
	}
	return d
}

func (k *casosTelefono) destinoNoPermitido() *nucleo.Problema {
	return nucleo.ReglaNegocio("destino_no_permitido", "Solo números de Guatemala (8 dígitos) o extensiones y grupos de tu empresa")
}

type numeroResuelto struct {
	nacional   string
	clienteID  *int64
	servicioID *int64
	ticketID   *int64
	ordenID    *int64
}

// elegirTelefono: el teléfono pedido (o el primero que exista en el orden de fábrica).
func elegirTelefono(pedido *string, orden []string, tels map[string]string) (string, error) {
	if pedido != nil {
		orden = []string{*pedido}
	}
	for _, t := range orden {
		if v := strings.TrimSpace(tels[t]); v != "" {
			return v, nil
		}
	}
	return "", nucleo.ReglaNegocio("sin_contacto", "No tiene un teléfono de ese tipo")
}

func (m *Modulo) telefonosCliente(ctx context.Context, q *mensajeriabd.Queries, cliente int64, servicio *int64) (map[string]string, error) {
	c, err := q.ClienteInstantaneaEnEmpresa(ctx, mensajeriabd.ClienteInstantaneaEnEmpresaParams{ID: cliente, Empresa: empresaDeCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nucleo.NoEncontrado("El cliente no existe")
	}
	if err != nil {
		return nil, err
	}
	tels := map[string]string{"principal": c.Telefono, "movil": c.Movil}
	ss, err := q.ServiciosInstantanea(ctx, cliente)
	if err != nil {
		return nil, err
	}
	for _, s := range ss {
		if (servicio == nil || s.ID == *servicio) && strings.TrimSpace(s.ContactoTelefono) != "" {
			tels["contacto"] = s.ContactoTelefono
			break
		}
	}
	return tels, nil
}

// verOrden: la orden si quien pide la ve (ordenes.ver, o ordenes.ver_propias si es suya); si no, 404.
func (m *Modulo) verOrden(ctx context.Context, id int64) (campo.OrdenDetalle, error) {
	if m.cfg.Campo == nil {
		return campo.OrdenDetalle{}, noDisponible("Las órdenes")
	}
	if contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesVer)) {
		return m.cfg.Campo.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: id})
	}
	if contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesVerPropias)) {
		return m.cfg.Campo.VerOrdenPropia.Ejecutar(ctx, campo.EntradaVerOrden{ID: id})
	}
	return campo.OrdenDetalle{}, nucleo.NoEncontrado("La orden no existe")
}

// numeroDeOrden: contacto en sitio de la orden, móvil o principal del cliente (o el pedido).
func (m *Modulo) numeroDeOrden(ctx context.Context, q *mensajeriabd.Queries, id int64, pedido *string) (campo.OrdenDetalle, string, error) {
	o, err := m.verOrden(ctx, id)
	if err != nil {
		return o, "", err
	}
	tels := map[string]string{}
	if o.ClienteID != nil {
		if tels, err = m.telefonosCliente(ctx, q, *o.ClienteID, o.ServicioID); err != nil {
			var p *nucleo.Problema
			if !errors.As(err, &p) {
				return o, "", err
			}
			tels = map[string]string{}
		}
	}
	if o.Contacto.Telefono != nil && strings.TrimSpace(*o.Contacto.Telefono) != "" {
		tels["contacto"] = *o.Contacto.Telefono
	}
	n, err := elegirTelefono(pedido, []string{"contacto", "movil", "principal"}, tels)
	return o, n, err
}

func (m *Modulo) nuevosCasosTelefonoPersona(k *casosTelefono) {
	k.Mio = auditoria.Nuevo(auditoria.Caso[struct{}, TelefonoMio]{
		Accion: "telefono.ver", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[TelefonoMio],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (TelefonoMio, error) {
			if err := soloSesion(ctx); err != nil {
				return TelefonoMio{}, err
			}
			q := mensajeriabd.New(t)
			x, motivo, err := m.telefonoDisponible(ctx, q)
			if err != nil {
				return TelefonoMio{}, err
			}
			p := contexto.PermisosDe(ctx)
			out := TelefonoMio{Disponible: motivo == "", Dispositivos: []string{}, Puede: PuedeTelefono{Llamar: p.Tiene(string(nucleo.PermCentralitaLlamar)),
				Transferir: p.Tiene(string(nucleo.PermCentralitaTransferir))}}
			if motivo != "" {
				out.Motivo = &motivo
			} else {
				out.Dispositivos = []string{"panel", "app"}
				out.Puede.Recibir = true
			}
			if motivo != motivoSinCentralita && motivo != motivoSinExtension {
				e := x.Extension
				out.Extension = &e
				out.Limite.PorDia = x.LlamadasPorUsuarioDia
				n, err := q.ContarLlamadasPuenteDia(ctx, mensajeriabd.ContarLlamadasPuenteDiaParams{UsuarioID: contexto.ActorDe(ctx).ID, Desde: inicioDia(m.ahora())})
				if err != nil {
					return TelefonoMio{}, err
				}
				out.Limite.UsadasHoy = n
			}
			if !out.Disponible {
				out.Puede = PuedeTelefono{}
			}
			return out, nil
		},
	})
	k.Abrir = auditoria.Nuevo(auditoria.Caso[EntradaAbrirTelefono, SesionTelefono]{
		Accion: "telefono.abrir", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAbrirTelefono) (SesionTelefono, error) {
			if err := soloSesion(ctx); err != nil {
				return SesionTelefono{}, err
			}
			if in.Dispositivo != "panel" && in.Dispositivo != "app" {
				return SesionTelefono{}, campoCentralita("dispositivo", "panel o app")
			}
			q := mensajeriabd.New(t)
			x, motivo, err := m.telefonoDisponible(ctx, q)
			if err != nil {
				return SesionTelefono{}, err
			}
			if motivo != "" {
				return SesionTelefono{}, problemaTelefono(motivo)
			}
			ahora := m.ahora()
			if err := q.BorrarTicketsTelefono(ctx, ahora.Add(-time.Hour)); err != nil {
				return SesionTelefono{}, err
			}
			crudo := make([]byte, 32)
			if _, err := rand.Read(crudo); err != nil {
				return SesionTelefono{}, err
			}
			ticket := base64.RawURLEncoding.EncodeToString(crudo)
			h := sha256.Sum256([]byte(ticket))
			actor := contexto.ActorDe(ctx)
			vence := ahora.Add(vidaTicketTelefono)
			if err := q.InsertarTicketTelefono(ctx, mensajeriabd.InsertarTicketTelefonoParams{Hash: h[:], UsuarioID: actor.ID, EmpresaID: ptrEmpresa(empresaDeCtx(ctx)),
				ExtensionID: x.ID, Dispositivo: in.Dispositivo, SesionID: contexto.SesionDe(ctx).ID, ExpiraEn: vence}); err != nil {
				return SesionTelefono{}, err
			}
			t.FijarEntidad("centralita_extension", x.ID)
			nombre := ""
			if u, err := q.UsuarioDeEmpresaActivo(ctx, mensajeriabd.UsuarioDeEmpresaActivoParams{Usuario: actor.ID, Empresa: 0}); err == nil {
				nombre = u.Nombre
			}
			ice, iceVence := m.cfg.TURN.credencial(actor.ID, ahora)
			return SesionTelefono{WS: RutaTelefonoSIP + "?ticket=" + ticket, TicketVence: vence,
				SIP:         SIPTelefono{URI: "sip:" + *x.SipUsuario + "@" + x.Dominio, Usuario: *x.SipUsuario, Dominio: x.Dominio, Nombre: nombre},
				ICE:         []ServidorICE{ice}, PoliticaIce: "relay", IceVence: iceVence}, nil
		},
	})
	k.Llamar = auditoria.Nuevo(auditoria.Caso[EntradaLlamarTelefono, MarcacionTelefono]{
		Accion: "telefono.llamar", Permiso: nucleo.PermCentralitaLlamar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamarTelefono) (MarcacionTelefono, error) {
			if err := soloSesion(ctx); err != nil {
				return MarcacionTelefono{}, err
			}
			n := 0
			for _, v := range []bool{in.ClienteID != nil, in.TicketID != nil, in.OrdenID != nil, in.UsuarioID != nil, in.Numero != nil} {
				if v {
					n++
				}
			}
			if n != 1 {
				return MarcacionTelefono{}, campoCentralita("clienteId", "Indica uno: clienteId, ticketId, ordenId, usuarioId o numero")
			}
			q := mensajeriabd.New(t)
			x, motivo, err := m.telefonoDisponible(ctx, q)
			if err != nil {
				return MarcacionTelefono{}, err
			}
			if motivo != "" {
				return MarcacionTelefono{}, problemaTelefono(motivo)
			}
			ahora := m.ahora()
			if err := m.cerrarMarcacionesVencidas(ctx, t, q, ahora); err != nil {
				return MarcacionTelefono{}, err
			}
			usuario := contexto.ActorDe(ctx).ID
			interna := func(numero string) (MarcacionTelefono, error) {
				if numero == x.Extension {
					return MarcacionTelefono{}, k.destinoNoPermitido()
				}
				t.FijarEntidad("centralita_extension", x.ID)
				t.Anotar(map[string]any{"interna": true, "extension": x.Extension})
				return MarcacionTelefono{Destino: "sip:" + numero + "@" + x.Dominio, Vence: ahora.Add(vidaMarcacion)}, nil
			}
			var r numeroResuelto
			switch {
			case in.UsuarioID != nil:
				e, err := q.ExtensionActivaDeUsuario(ctx, mensajeriabd.ExtensionActivaDeUsuarioParams{CentralitaID: x.CentralitaID, UsuarioID: *in.UsuarioID})
				if errors.Is(err, pgx.ErrNoRows) {
					return MarcacionTelefono{}, nucleo.ReglaNegocio("sin_extension", "Esa persona no tiene una extensión")
				}
				if err != nil {
					return MarcacionTelefono{}, err
				}
				return interna(e.Extension)
			case in.Numero != nil:
				d := soloDigitos(*in.Numero)
				if len(d) >= 2 && len(d) <= 8 && d == strings.TrimSpace(*in.Numero) {
					if _, err := q.ExtensionActivaPorNumero(ctx, mensajeriabd.ExtensionActivaPorNumeroParams{CentralitaID: x.CentralitaID, Extension: d}); err == nil {
						return interna(d)
					} else if !errors.Is(err, pgx.ErrNoRows) {
						return MarcacionTelefono{}, err
					}
					if _, err := q.GrupoActivoPorNumero(ctx, mensajeriabd.GrupoActivoPorNumeroParams{CentralitaID: x.CentralitaID, Numero: d}); err == nil {
						return interna(d)
					} else if !errors.Is(err, pgx.ErrNoRows) {
						return MarcacionTelefono{}, err
					}
				}
				if r.nacional = nacionalGT(*in.Numero); r.nacional == "" {
					return MarcacionTelefono{}, k.destinoNoPermitido()
				}
				// La regla de una sola coincidencia (§3.4 del chat) para el cliente del teclado.
				cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: "502" + r.nacional, Empresa: empresaDeCtx(ctx)})
				if err != nil {
					return MarcacionTelefono{}, err
				}
				if len(cs) == 1 {
					r.clienteID = &cs[0].ID
				}
			case in.ClienteID != nil:
				if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermClientesVer)) {
					return MarcacionTelefono{}, nucleo.NoEncontrado("El cliente no existe")
				}
				tels, err := m.telefonosCliente(ctx, q, *in.ClienteID, nil)
				if err != nil {
					return MarcacionTelefono{}, err
				}
				num, err := elegirTelefono(in.Telefono, []string{"movil", "principal", "contacto"}, tels)
				if err != nil {
					return MarcacionTelefono{}, err
				}
				r.nacional, r.clienteID = nacionalGT(num), in.ClienteID
			case in.TicketID != nil:
				if m.cfg.Soporte == nil {
					return MarcacionTelefono{}, noDisponible("Los tickets")
				}
				tk, err := m.cfg.Soporte.VerTicket.Ejecutar(ctx, soporte.EntradaVerTicket{ID: *in.TicketID})
				if err != nil {
					return MarcacionTelefono{}, err
				}
				if tk.ClienteID == nil {
					return MarcacionTelefono{}, nucleo.ReglaNegocio("sin_contacto", "El ticket no tiene cliente")
				}
				tels, err := m.telefonosCliente(ctx, q, *tk.ClienteID, tk.ServicioID)
				if err != nil {
					return MarcacionTelefono{}, err
				}
				num, err := elegirTelefono(in.Telefono, []string{"movil", "principal", "contacto"}, tels)
				if err != nil {
					return MarcacionTelefono{}, err
				}
				r.nacional, r.clienteID, r.servicioID, r.ticketID = nacionalGT(num), tk.ClienteID, tk.ServicioID, &tk.ID
			case in.OrdenID != nil:
				o, num, err := m.numeroDeOrden(ctx, q, *in.OrdenID, in.Telefono)
				if err != nil {
					return MarcacionTelefono{}, err
				}
				r.nacional, r.clienteID, r.servicioID, r.ordenID = nacionalGT(num), o.ClienteID, o.ServicioID, &o.ID
			}
			if r.nacional == "" {
				return MarcacionTelefono{}, k.destinoNoPermitido()
			}
			// Límites: por persona y día, y simultáneas de la empresa.
			hoy, err := q.ContarLlamadasPuenteDia(ctx, mensajeriabd.ContarLlamadasPuenteDiaParams{UsuarioID: usuario, Desde: inicioDia(ahora)})
			if err != nil {
				return MarcacionTelefono{}, err
			}
			if hoy >= x.LlamadasPorUsuarioDia {
				return MarcacionTelefono{}, nucleo.ReglaNegocio("limite_llamadas", "Llegaste al límite de llamadas de hoy")
			}
			if err := q.CandadoChat(ctx, "telefono:simultaneas:"+strconv.FormatInt(empresaDeCtx(ctx), 10)); err != nil {
				return MarcacionTelefono{}, err
			}
			vivas, err := q.ContarLlamadasPuenteVivas(ctx, mensajeriabd.ContarLlamadasPuenteVivasParams{Empresa: empresaDeCtx(ctx), HaceMinuto: ahora.Add(-vidaMarcacion)})
			if err != nil {
				return MarcacionTelefono{}, err
			}
			if vivas >= x.LlamadasSimultaneas {
				return MarcacionTelefono{}, nucleo.Conflicto("llamadas_simultaneas", "Tu empresa tiene todas sus líneas ocupadas: intenta en un momento")
			}
			id, err := uuid.NewV7()
			if err != nil {
				return MarcacionTelefono{}, err
			}
			p := mensajeriabd.InsertarLlamadaPuenteParams{ID: id, IDExterno: "iris:" + id.String(), EmpresaID: ptrEmpresa(empresaDeCtx(ctx)), CentralitaID: x.CentralitaID,
				Numero: "+502" + r.nacional, ClienteID: r.clienteID, ServicioID: r.servicioID, UsuarioID: usuario, Extension: x.Extension, SonoEn: ahora,
				SuenaHasta: ahora.Add(CentralitaSonandoMax), TicketID: r.ticketID, OrdenID: r.ordenID}
			if r.clienteID != nil {
				p.Asociacion, p.Coincidencias = texto("manual"), 1
				if in.Numero != nil {
					p.Asociacion = texto("automatica")
				}
			}
			l, err := q.InsertarLlamadaPuente(ctx, p)
			if err != nil {
				return MarcacionTelefono{}, err
			}
			t.FijarEntidad("centralita_llamada", l.ID)
			t.Anotar(map[string]any{"extension": x.Extension, "cliente_id": r.clienteID, "ticket_id": r.ticketID, "orden_id": r.ordenID})
			if err := t.Emitir("llamada.saliente_iniciada", map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "canal_id": nil,
				"centralita_id": x.CentralitaID, "usuario_id": usuario, "devolucion_id": nil, "origen": "centralita", "puente": true, "orden_id": r.ordenID,
				"ticket_id": r.ticketID}); err != nil {
				return MarcacionTelefono{}, err
			}
			v, err := m.unaLlamada(ctx, q, l, false)
			if err != nil {
				return MarcacionTelefono{}, err
			}
			return MarcacionTelefono{Destino: "sip:iris-" + l.ID.String() + "@" + x.Dominio, Llamada: &v, Vence: ahora.Add(vidaMarcacion)}, nil
		},
	})
	k.Directa = auditoria.Nuevo(auditoria.Caso[EntradaLlamadaDirecta, LlamadaDirecta]{
		Accion: "telefono.llamada_directa", Permiso: nucleo.PermCentralitaLlamar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamadaDirecta) (LlamadaDirecta, error) {
			if err := soloSesion(ctx); err != nil {
				return LlamadaDirecta{}, err
			}
			q := mensajeriabd.New(t)
			o, num, err := m.numeroDeOrden(ctx, q, in.OrdenID, nil)
			if err != nil {
				return LlamadaDirecta{}, err
			}
			d := soloDigitos(num)
			if len(d) < 8 || len(d) > 15 {
				return LlamadaDirecta{}, nucleo.ReglaNegocio("sin_contacto", "La orden no tiene un teléfono válido")
			}
			e164 := "+" + d
			if g := nacionalGT(num); g != "" {
				e164 = "+502" + g
			}
			id, err := uuid.NewV7()
			if err != nil {
				return LlamadaDirecta{}, err
			}
			usuario := contexto.ActorDe(ctx).ID
			l, err := q.InsertarLlamadaDirecta(ctx, mensajeriabd.InsertarLlamadaDirectaParams{ID: id, IDExterno: "celular:" + id.String(), EmpresaID: o.EmpresaID,
				Numero: e164, ClienteID: o.ClienteID, ServicioID: o.ServicioID, UsuarioID: usuario, Ahora: m.ahora(), OrdenID: o.ID})
			if err != nil {
				return LlamadaDirecta{}, err
			}
			t.FijarEntidad("centralita_llamada", l.ID)
			if err := t.Emitir("llamada.directa_registrada", map[string]any{"llamada_id": l.ID, "empresa_id": l.EmpresaID, "orden_id": o.ID,
				"cliente_id": o.ClienteID, "servicio_id": o.ServicioID, "usuario_id": usuario, "origen": "celular"}); err != nil {
				return LlamadaDirecta{}, err
			}
			v, err := m.unaLlamada(ctx, q, l, false)
			if err != nil {
				return LlamadaDirecta{}, err
			}
			return LlamadaDirecta{Llamada: v, Telefono: e164}, nil
		},
	})
	k.Destinos = auditoria.Nuevo(auditoria.Caso[struct{}, DestinosTransferencia]{
		Accion: "telefono.destinos", Permiso: nucleo.PermCentralitaTransferir, AuditarExito: sinAuditar[DestinosTransferencia],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (DestinosTransferencia, error) {
			if err := soloSesion(ctx); err != nil {
				return DestinosTransferencia{}, err
			}
			q := mensajeriabd.New(t)
			out := DestinosTransferencia{Personas: []PersonaDestino{}, Grupos: []GrupoDestino{}}
			c, err := q.CentralitaDeEmpresa(ctx, empresaDeCtx(ctx))
			if errors.Is(err, pgx.ErrNoRows) {
				return out, nil
			}
			if err != nil {
				return out, err
			}
			xs, err := q.ExtensionesDeCentralita(ctx, c.ID)
			if err != nil {
				return out, err
			}
			yo := contexto.ActorDe(ctx).ID
			for _, x := range xs {
				if x.UsuarioID != yo {
					out.Personas = append(out.Personas, PersonaDestino{UsuarioID: x.UsuarioID, Nombre: x.UsuarioNombre, Extension: x.Extension})
				}
			}
			gs, err := q.GruposDeCentralita(ctx, c.ID)
			if err != nil {
				return out, err
			}
			for _, g := range gs {
				if g.Activo {
					out.Grupos = append(out.Grupos, GrupoDestino{ID: g.ID, Nombre: g.Nombre, Numero: g.Numero})
				}
			}
			return out, nil
		},
	})
}

// cerrarMarcacionesVencidas: las marcaciones de la persona que nunca mandaron su INVITE quedan
// fallidas (y cuentan para el límite del día).
func (m *Modulo) cerrarMarcacionesVencidas(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, ahora time.Time) error {
	ls, err := q.MarcacionesVencidas(ctx, mensajeriabd.MarcacionesVencidasParams{UsuarioID: contexto.ActorDe(ctx).ID, Antes: ahora.Add(-vidaMarcacion)})
	if err != nil {
		return err
	}
	for _, l := range ls {
		l.Estado, l.MotivoFin, l.TerminadaEn = LlamadaFallida, texto("error_medio"), &ahora
		if _, err := guardarLlamadaCentralita(ctx, q, l); err != nil {
			return err
		}
		if err := t.Emitir("llamada.fallida", map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "origen": "centralita",
			"motivo": "error_medio", "etapa": "marcacion"}); err != nil {
			return err
		}
	}
	return nil
}
