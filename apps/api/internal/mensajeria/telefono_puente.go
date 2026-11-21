package mensajeria

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// El puente del teléfono de Iris (contratos/voz.md §3.12, E4c): abre el WebSocket del navegador, la
// conexión al WSS de la Issabel y deja a sippuente reenviar con las reglas del servidor. La política
// (qué se puede marcar y transferir, y los estados de la llamada) son casos de uso con auditoría.

var (
	errReemplazada = errors.New("telefono: conexión reemplazada")
	errSinAcceso   = errors.New("telefono: sin acceso")
)

// conexionTelefono es lo que el puente necesita (con la credencial descifrada: nunca sale de aquí).
type conexionTelefono struct {
	id          uuid.UUID
	usuario     int64
	extensionID int64
	extension   string
	centralita  int64
	dispositivo string
	guardadoEn  time.Time
	cred        sippuente.Credencial
	opciones    sippuente.OpcionesWSS
}

type entradaConectar struct {
	Ticket string `json:"ticket"`
}

type entradaRevalidar struct {
	ConexionID  uuid.UUID `json:"conexion_id"`
	ExtensionID int64     `json:"extension_id"`
	GuardadoEn  time.Time `json:"-"`
}

type entradaCerrarConexion struct {
	ConexionID uuid.UUID `json:"conexion_id"`
	Motivo     string    `json:"motivo"`
}

type entradaInvitar struct {
	Usuario     string `json:"destino"`
	ExtensionID int64  `json:"extension_id"`
}

type entradaTransferir struct {
	Usuario     string     `json:"destino"`
	ExtensionID int64      `json:"extension_id"`
	LlamadaID   *uuid.UUID `json:"llamada_id"`
}

type entradaHechoPuente struct {
	LlamadaID uuid.UUID `json:"llamada_id"`
	Tipo      string    `json:"tipo"`
	Codigo    int       `json:"codigo"`
	Cancelada bool      `json:"cancelada"`
	Destino   string    `json:"destino,omitempty"`
}

type entradaCredenciales struct {
	ExtensionID int64 `json:"extension_id"`
}

func ticketNoValido() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "ticket_no_valido", "Ticket no válido",
		"El ticket del teléfono venció, ya se usó o no es de esta sesión: abre el teléfono otra vez")
}

func (m *Modulo) nuevosCasosPuente(k *casosTelefono) {
	k.conectar = auditoria.Nuevo(auditoria.Caso[entradaConectar, conexionTelefono]{
		Accion: "telefono.conectar", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaConectar) (conexionTelefono, error) {
			if err := soloSesion(ctx); err != nil {
				return conexionTelefono{}, err
			}
			q := mensajeriabd.New(t)
			ahora := m.ahora()
			h := sha256.Sum256([]byte(in.Ticket))
			tk, err := q.UsarTicketTelefono(ctx, mensajeriabd.UsarTicketTelefonoParams{Hash: h[:], Ahora: ahora})
			if errors.Is(err, pgx.ErrNoRows) {
				return conexionTelefono{}, ticketNoValido()
			}
			if err != nil {
				return conexionTelefono{}, err
			}
			actor := contexto.ActorDe(ctx)
			if tk.UsuarioID != actor.ID || tk.SesionID != contexto.SesionDe(ctx).ID || derefEmpresa(tk.EmpresaID) != empresaDeCtx(ctx) {
				return conexionTelefono{}, ticketNoValido()
			}
			x, motivo, err := m.telefonoDisponible(ctx, q)
			if err != nil {
				return conexionTelefono{}, err
			}
			if motivo != "" {
				return conexionTelefono{}, problemaTelefono(motivo)
			}
			if x.ID != tk.ExtensionID || x.SipGuardadoEn == nil {
				return conexionTelefono{}, ticketNoValido()
			}
			if err := m.validarHostCentralita(x.Host); err != nil {
				return conexionTelefono{}, err
			}
			secreto, err := m.descifrarSIP(x.ID, x.SipSecretoCifrado)
			if err != nil {
				return conexionTelefono{}, problemaTelefono(motivoSinCredencial)
			}
			id, err := uuid.NewV7()
			if err != nil {
				return conexionTelefono{}, err
			}
			// La conexión nueva reemplaza a la anterior de la persona (la cierra su réplica en el latido).
			if err := q.TomarConexionTelefono(ctx, mensajeriabd.TomarConexionTelefonoParams{ID: id, UsuarioID: actor.ID, EmpresaID: tk.EmpresaID,
				ExtensionID: x.ID, Dispositivo: tk.Dispositivo, Replica: m.idProceso, Ahora: ahora}); err != nil {
				return conexionTelefono{}, err
			}
			t.FijarEntidad("centralita_extension", x.ID)
			t.Anotar(map[string]any{"dispositivo": tk.Dispositivo, "extension": x.Extension, "conexion_id": id})
			return conexionTelefono{id: id, usuario: actor.ID, extensionID: x.ID, extension: x.Extension, centralita: x.CentralitaID, dispositivo: tk.Dispositivo,
				guardadoEn: *x.SipGuardadoEn, cred: sippuente.Credencial{Usuario: *x.SipUsuario, Secreto: secreto, Dominio: x.Dominio},
				opciones: m.opcionesWSS(x.Host, x.WssPuerto, x.WssRuta, *x.WssHuella)}, nil
		},
	})
	k.revalidar = auditoria.Nuevo(auditoria.Caso[entradaRevalidar, struct{}]{
		Accion: "telefono.latido", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[struct{}],
		AuditarError: func(error) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRevalidar) (struct{}, error) {
			q := mensajeriabd.New(t)
			if _, err := q.LatidoConexionTelefono(ctx, mensajeriabd.LatidoConexionTelefonoParams{ID: in.ConexionID, Ahora: m.ahora()}); errors.Is(err, pgx.ErrNoRows) {
				return struct{}{}, errReemplazada
			} else if err != nil {
				return struct{}{}, err
			}
			x, motivo, err := m.telefonoDisponible(ctx, q)
			if err != nil {
				return struct{}{}, err
			}
			if motivo != "" || x.ID != in.ExtensionID || x.SipGuardadoEn == nil || !x.SipGuardadoEn.Equal(in.GuardadoEn) {
				return struct{}{}, errSinAcceso
			}
			return struct{}{}, nil
		},
	})
	// credenciales: la Issabel rechazó la contraseña guardada en una conexión del puente. La extensión
	// queda sin «Probar registro» (no se reabre el teléfono hasta que pase otra prueba) y el puente corta.
	k.credenciales = auditoria.Nuevo(auditoria.Caso[entradaCredenciales, struct{}]{
		Accion: "telefono.credenciales_rechazadas", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCredenciales) (struct{}, error) {
			q := mensajeriabd.New(t)
			x, err := q.TelefonoDeUsuario(ctx, mensajeriabd.TelefonoDeUsuarioParams{Usuario: contexto.ActorDe(ctx).ID, Empresa: empresaDeCtx(ctx)})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && x.ID != in.ExtensionID) {
				return struct{}{}, nil
			}
			if err != nil {
				return struct{}{}, err
			}
			if err := q.MarcarRegistroRechazado(ctx, mensajeriabd.MarcarRegistroRechazadoParams{ID: x.ID, Ahora: m.ahora()}); err != nil {
				return struct{}{}, err
			}
			t.FijarEntidad("centralita_extension", x.ID)
			return struct{}{}, t.Emitir("extension.registro_probado", map[string]any{"centralita_id": x.CentralitaID, "empresa_id": x.EmpresaID,
				"extension_id": x.ID, "usuario_id": contexto.ActorDe(ctx).ID, "estado": "credenciales", "origen": "puente"})
		},
	})
	k.cerrarConexio = auditoria.Nuevo(auditoria.Caso[entradaCerrarConexion, struct{}]{
		Accion: "telefono.desconectar", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCerrarConexion) (struct{}, error) {
			t.Anotar(map[string]any{"motivo": in.Motivo})
			return struct{}{}, mensajeriabd.New(t).SoltarConexionTelefono(ctx, in.ConexionID)
		},
	})
	k.invitar = auditoria.Nuevo(auditoria.Caso[entradaInvitar, sippuente.Destino]{
		Accion: "telefono.invitar", Permiso: nucleo.PermCentralitaLlamar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaInvitar) (sippuente.Destino, error) {
			q := mensajeriabd.New(t)
			x, motivo, err := m.telefonoDisponible(ctx, q)
			if err != nil {
				return sippuente.Destino{}, err
			}
			if motivo != "" || x.ID != in.ExtensionID {
				return sippuente.Destino{}, problemaTelefono(motivoSinExtension)
			}
			ahora := m.ahora()
			if resto, ok := strings.CutPrefix(in.Usuario, "iris-"); ok {
				id, err := uuid.Parse(resto)
				if err != nil {
					return sippuente.Destino{}, k.destinoNoPermitido()
				}
				l, err := q.LlamadaPuenteParaInvitar(ctx, mensajeriabd.LlamadaPuenteParaInvitarParams{ID: id, UsuarioID: contexto.ActorDe(ctx).ID,
					Desde: ahora.Add(-vidaMarcacion)})
				if errors.Is(err, pgx.ErrNoRows) {
					return sippuente.Destino{}, k.destinoNoPermitido()
				}
				if err != nil {
					return sippuente.Destino{}, err
				}
				nacional := nacionalGT(l.Numero)
				if nacional == "" || l.Extension == nil || *l.Extension != x.Extension {
					return sippuente.Destino{}, k.destinoNoPermitido()
				}
				if err := q.MarcarInviteLlamada(ctx, mensajeriabd.MarcarInviteLlamadaParams{ID: l.ID, Ahora: ahora}); err != nil {
					return sippuente.Destino{}, err
				}
				t.FijarEntidad("centralita_llamada", l.ID)
				return sippuente.Destino{Usuario: x.PrefijoSalida + nacional, LlamadaID: &l.ID}, nil
			}
			if err := m.numeroInterno(ctx, q, x, in.Usuario); err != nil {
				return sippuente.Destino{}, k.destinoNoPermitido()
			}
			t.FijarEntidad("centralita_extension", x.ID)
			t.Anotar(map[string]any{"interna": true})
			return sippuente.Destino{Usuario: in.Usuario}, nil
		},
	})
	k.transferir = auditoria.Nuevo(auditoria.Caso[entradaTransferir, string]{
		Accion: "telefono.transferir", Permiso: nucleo.PermCentralitaTransferir,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaTransferir) (string, error) {
			q := mensajeriabd.New(t)
			x, motivo, err := m.telefonoDisponible(ctx, q)
			if err != nil {
				return "", err
			}
			if motivo != "" || x.ID != in.ExtensionID {
				return "", problemaTelefono(motivoSinExtension)
			}
			if err := m.numeroInterno(ctx, q, x, in.Usuario); err != nil {
				return "", k.destinoNoPermitido()
			}
			// Solo valida: llamada.transferida sale cuando la Issabel acepta el REFER (202, hecho
			// «transferida»).
			t.FijarEntidad("centralita_extension", x.ID)
			if in.LlamadaID != nil {
				t.Anotar(map[string]any{"llamada_id": *in.LlamadaID})
			}
			return in.Usuario, nil
		},
	})
	k.hecho = auditoria.Nuevo(auditoria.Caso[entradaHechoPuente, bool]{
		Accion: "telefono.estado", Acceso: auditoria.Autenticado,
		AuditarExito: func(cambio bool) bool { return cambio },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaHechoPuente) (bool, error) {
			q := mensajeriabd.New(t)
			l, err := q.LlamadaPuenteParaActualizar(ctx, in.LlamadaID)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if l.UsuarioID == nil || *l.UsuarioID != contexto.ActorDe(ctx).ID {
				return false, nil
			}
			t.FijarEmpresa(derefEmpresa(l.EmpresaID))
			t.FijarEntidad("centralita_llamada", l.ID)
			ahora := m.ahora()
			switch in.Tipo {
			case sippuente.HechoContestada:
				if l.Estado != LlamadaMarcando {
					return false, nil
				}
				l.Estado, l.ContestadaEn, l.AtendidaPor = LlamadaEnCurso, &ahora, texto("persona")
				if l, err = guardarLlamadaCentralita(ctx, q, l); err != nil {
					return false, err
				}
				return true, t.Emitir("llamada.contestada", map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "origen": "centralita",
					"centralita_id": l.CentralitaID, "atendida_por": "persona", "usuario_id": l.UsuarioID, "espera_seg": max(int(ahora.Sub(l.SonoEn).Seconds()), 0)})
			case sippuente.HechoColgada:
				return m.cerrarLlamadaCentralita(ctx, t, q, l, ahora, nil)
			case sippuente.HechoTransferida:
				if l.Estado != LlamadaEnCurso || l.CentralitaID == nil {
					return false, nil
				}
				usuario := contexto.ActorDe(ctx).ID
				p := map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "usuario_id": usuario, "de": usuario,
					"a_usuario_id": nil, "a_grupo_id": nil, "a_cola_id": nil, "origen": "centralita"}
				if e, err := q.ExtensionActivaPorNumero(ctx, mensajeriabd.ExtensionActivaPorNumeroParams{CentralitaID: *l.CentralitaID, Extension: in.Destino}); err == nil {
					p["a_usuario_id"] = e.UsuarioID
				} else if g, err := q.GrupoActivoPorNumero(ctx, mensajeriabd.GrupoActivoPorNumeroParams{CentralitaID: *l.CentralitaID, Numero: in.Destino}); err == nil {
					p["a_grupo_id"] = g.ID
				}
				return true, t.Emitir("llamada.transferida", p)
			case sippuente.HechoFallo:
				if l.Estado != LlamadaMarcando {
					return false, nil
				}
				motivo, estado, evento := "error_medio", LlamadaFallida, "llamada.fallida"
				switch {
				case in.Cancelada:
					motivo, estado, evento = "agente_colgo", LlamadaPerdida, "llamada.perdida"
				case in.Codigo == 408 || in.Codigo == 480 || in.Codigo == 486 || in.Codigo == 487 || in.Codigo == 600 || in.Codigo == 603:
					motivo, estado, evento = "nadie_contesto", LlamadaPerdida, "llamada.perdida"
				}
				l.Estado, l.MotivoFin, l.TerminadaEn = estado, &motivo, &ahora
				if l, err = guardarLlamadaCentralita(ctx, q, l); err != nil {
					return false, err
				}
				p := map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "origen": "centralita", "centralita_id": l.CentralitaID}
				if evento == "llamada.perdida" {
					p["direccion"], p["cola_id"], p["sono_seg"], p["devolucion_id"], p["usuarios"] = "saliente", nil, max(int(ahora.Sub(l.SonoEn).Seconds()), 0), nil, []int64{}
				} else {
					p["motivo"], p["etapa"] = motivo, "puente"
				}
				return true, t.Emitir(evento, p)
			}
			return false, nil
		},
	})
}

// numeroInterno: dígitos de una extensión activa (no la propia) o de un grupo activo de la centralita,
// de 2 a 6 cifras.
func (m *Modulo) numeroInterno(ctx context.Context, q *mensajeriabd.Queries, x mensajeriabd.TelefonoDeUsuarioRow, numero string) error {
	if !numeroInternoValido.MatchString(numero) || numero == x.Extension {
		return errSinAcceso
	}
	if _, err := q.ExtensionActivaPorNumero(ctx, mensajeriabd.ExtensionActivaPorNumeroParams{CentralitaID: x.CentralitaID, Extension: numero}); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := q.GrupoActivoPorNumero(ctx, mensajeriabd.GrupoActivoPorNumeroParams{CentralitaID: x.CentralitaID, Numero: numero}); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return errSinAcceso
}

// politicaTelefono es la Politica de sippuente: llama a los casos de uso con el contexto de la
// persona al día (el latido lo renueva con los permisos de ahora).
type politicaTelefono struct {
	m   *Modulo
	cx  conexionTelefono
	ctx atomic.Pointer[context.Context]
}

func (p *politicaTelefono) actual() context.Context { return *p.ctx.Load() }

func (p *politicaTelefono) Invitar(_ context.Context, usuario string) (sippuente.Destino, error) {
	return p.m.Centralita.Telefono.invitar.Ejecutar(p.actual(), entradaInvitar{Usuario: usuario, ExtensionID: p.cx.extensionID})
}

func (p *politicaTelefono) Transferir(_ context.Context, usuario string, llamada *uuid.UUID) (string, error) {
	return p.m.Centralita.Telefono.transferir.Ejecutar(p.actual(), entradaTransferir{Usuario: usuario, ExtensionID: p.cx.extensionID, LlamadaID: llamada})
}

func (p *politicaTelefono) Entrante(_ context.Context, numero string) *uuid.UUID {
	ctx := p.actual()
	d := soloDigitos(numero)
	bd := nucleo.BDDe(ctx)
	if len(d) < 8 || bd == nil {
		return nil
	}
	id, err := mensajeriabd.New(bd).LlamadaEntranteDeUsuario(ctx, mensajeriabd.LlamadaEntranteDeUsuarioParams{UsuarioID: p.cx.usuario,
		Desde: p.m.ahora().Add(-30 * time.Second), Digitos: d})
	if err != nil {
		return nil
	}
	return &id
}

func (p *politicaTelefono) Hecho(ctx context.Context, h sippuente.HechoLlamada) {
	actual := p.actual()
	hctx, cancelar := context.WithTimeout(context.WithoutCancel(actual), 10*time.Second)
	defer cancelar()
	_ = ctx
	if _, err := p.m.Centralita.Telefono.hecho.Ejecutar(hctx, entradaHechoPuente{LlamadaID: h.LlamadaID, Tipo: h.Tipo, Codigo: h.Codigo, Cancelada: h.Cancelada,
		Destino: h.Destino}); err != nil {
		slog.Default().WarnContext(hctx, "telefono: no se pudo guardar el estado de la llamada", "llamada", h.LlamadaID, "tipo", h.Tipo, "err", err)
	}
}

func (p *politicaTelefono) Credenciales(ctx context.Context) {
	hctx, cancelar := context.WithTimeout(context.WithoutCancel(p.actual()), 10*time.Second)
	defer cancelar()
	_ = ctx
	if _, err := p.m.Centralita.Telefono.credenciales.Ejecutar(hctx, entradaCredenciales{ExtensionID: p.cx.extensionID}); err != nil {
		slog.Default().WarnContext(hctx, "telefono: no se pudo anotar la credencial rechazada", "err", err)
	}
}

// origenPermitido: si el navegador manda Origin, tiene que ser el de IRIS_BASE_URL (sin Origin es la
// app nativa; la protección contra CSWSH es además el ticket, que solo se obtiene con CSRF).
func (m *Modulo) origenPermitido(origen string) bool {
	if origen == "" {
		return true
	}
	base, err := url.Parse(strings.TrimSpace(m.cfg.BaseURL))
	if err != nil || base.Host == "" {
		return m.cfg.BaseURL == ""
	}
	o, err := url.Parse(origen)
	return err == nil && strings.EqualFold(o.Scheme, base.Scheme) && strings.EqualFold(o.Host, base.Host)
}

// prepararTelefono: el ticket, la conexión a la Issabel y lo que el puente necesita, antes del
// upgrade (así los errores son RFC 9457).
func (m *Modulo) prepararTelefono(ctx context.Context, ticket, origen string) (*telefonoPreparado, error) {
	if !m.origenPermitido(origen) {
		return nil, nucleo.NuevoProblema(http.StatusForbidden, "origen_no_permitido", "Origen no permitido", "El teléfono solo se abre desde el panel de Iris")
	}
	cx, err := m.Centralita.Telefono.conectar.Ejecutar(ctx, entradaConectar{Ticket: ticket})
	if err != nil {
		return nil, err
	}
	cen, _, err := sippuente.Conectar(ctx, cx.opciones)
	if err != nil {
		_, _ = m.Centralita.Telefono.cerrarConexio.Ejecutar(context.WithoutCancel(ctx), entradaCerrarConexion{ConexionID: cx.id, Motivo: "centralita_no_responde"})
		return nil, nucleo.NuevoProblema(http.StatusServiceUnavailable, "centralita_no_responde", "La centralita no responde",
			"Iris no pudo conectarse al teléfono de la centralita. Inténtalo de nuevo en un momento").ConCabecera("Retry-After", "10")
	}
	return &telefonoPreparado{cx: cx, cen: cen}, nil
}

type telefonoPreparado struct {
	cx  conexionTelefono
	cen *websocket.Conn
}

// atenderTelefono hace el upgrade del navegador y corre el puente con su latido.
func (m *Modulo) atenderTelefono(ctx context.Context, w http.ResponseWriter, r *http.Request, tp *telefonoPreparado, log *slog.Logger) {
	cx := tp.cx
	defer func() {
		_, _ = m.Centralita.Telefono.cerrarConexio.Ejecutar(context.WithoutCancel(ctx), entradaCerrarConexion{ConexionID: cx.id, Motivo: "fin"})
	}()
	// Una conexión larga: net/http borra los plazos del servidor al secuestrarla (Accept); la limitan el
	// latido y la vida máxima. El Origin ya se revisó contra IRIS_BASE_URL (prepararTelefono).
	nav, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{sippuente.Subprotocolo}, InsecureSkipVerify: true})
	if err != nil {
		_ = tp.cen.CloseNow()
		return
	}
	if nav.Subprotocol() != sippuente.Subprotocolo {
		_ = nav.Close(websocket.StatusPolicyViolation, "sip")
		_ = tp.cen.CloseNow()
		return
	}
	pol := &politicaTelefono{m: m, cx: cx}
	pol.ctx.Store(&ctx)
	puente := sippuente.Nuevo(sippuente.Opciones{Credencial: cx.cred, Registrar: cx.dispositivo == "panel", Politica: pol, Log: log, Relevo: m.cfg.TURN.RelayIP},
		sippuente.NuevoLadoWS(nav, sippuente.MaxNavegador), sippuente.NuevoLadoWS(tp.cen, sippuente.MaxCentralita))
	vigia, cancelar := context.WithCancel(ctx)
	defer cancelar()
	go m.latidoTelefono(vigia, pol, puente)
	if err := puente.Correr(ctx); err != nil && !errors.Is(err, sippuente.ErrCortado) {
		log.DebugContext(ctx, "telefono: el puente terminó", "err", err)
	}
}

// latidoTelefono revisa cada TelefonoLatido la sesión, los permisos, la extensión y que la conexión
// siga siendo la de la persona; si algo falla, corta (BYE a lo establecido).
func (m *Modulo) latidoTelefono(ctx context.Context, pol *politicaTelefono, p *sippuente.Puente) {
	// Cada vistazo (2 s como mucho) mira solo si otra pestaña tomó el teléfono; cada TelefonoLatido,
	// todo lo demás.
	vistazo := min(2*time.Second, m.cfg.TelefonoLatido)
	tick := time.NewTicker(vistazo)
	defer tick.Stop()
	fin := time.NewTimer(m.cfg.TelefonoVidaMax)
	defer fin.Stop()
	ultimo := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-fin.C:
			p.Cortar(sippuente.CierreNormal, "vida_maxima")
			return
		case <-tick.C:
		}
		bd := nucleo.BDDe(ctx)
		if bd == nil {
			p.Cortar(sippuente.CierreSinAcceso, "sin_acceso")
			return
		}
		if time.Since(ultimo) < m.cfg.TelefonoLatido {
			vigente, err := mensajeriabd.New(bd).ConexionTelefonoVigente(ctx, pol.cx.id)
			if err == nil && !vigente {
				p.Cortar(sippuente.CierreReemplazada, "reemplazada")
				return
			}
			continue
		}
		ultimo = time.Now()
		nctx, err := identidad.Revalidar(ctx, bd)
		if err != nil {
			var pr *nucleo.Problema
			if errors.As(err, &pr) {
				p.Cortar(sippuente.CierreSinAcceso, "sin_acceso")
				return
			}
			continue // la base no respondió: se reintenta en el siguiente latido
		}
		_, err = m.Centralita.Telefono.revalidar.Ejecutar(nctx, entradaRevalidar{ConexionID: pol.cx.id, ExtensionID: pol.cx.extensionID, GuardadoEn: pol.cx.guardadoEn})
		switch {
		case errors.Is(err, errReemplazada):
			p.Cortar(sippuente.CierreReemplazada, "reemplazada")
			return
		case errors.Is(err, errSinAcceso):
			p.Cortar(sippuente.CierreSinAcceso, "sin_acceso")
			return
		case err != nil:
			var pr *nucleo.Problema
			if errors.As(err, &pr) {
				p.Cortar(sippuente.CierreSinAcceso, "sin_acceso")
				return
			}
			continue
		}
		pol.ctx.Store(&nctx)
	}
}
