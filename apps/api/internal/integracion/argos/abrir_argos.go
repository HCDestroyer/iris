package argos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Autologin Iris → Argos (contrato 0.7.0 §1.3, pedido B3): «Abrir en Argos» (con destino) y
// «Vincular mi usuario de Argos» (sin destino). Iris pide a Argos un ticket de un solo uso (60 s)
// con el token del conector y el id del usuario de Iris como usuario.id_externo: el mismo id que va
// en X-Iris-Actor (usuario:<id>) al aprobar o rechazar un comprobante (§7.7). El vínculo lo crea
// Argos cuando la persona abre el enlace y, la primera vez, confirma con su contraseña de Argos.
// Iris no sabe si el vínculo existe: lo descubre con el 403 actor_sin_vinculo de §7.7.
//
// La URL lleva el ticket: nunca se guarda (auditoría «***», evento sin URL, respuesta no repetible).

// TopeAbrirArgos es cuántos enlaces puede pedir una persona por minuto (contado en Postgres).
const TopeAbrirArgos = 10

const rutaTicketsArgos = "/integraciones/iris/sso/tickets"

// DestinoArgos es a dónde llega la persona en Argos.
type DestinoArgos struct {
	Tipo      string `json:"tipo" enum:"cliente,servicio"`
	IDExterno string `json:"idExterno" pattern:"^[1-9][0-9]{0,17}$" doc:"Id de Argos del cliente o del servicio"`
}

// EntradaAbrirArgos es POST /integraciones/argosisp/abrir.
type EntradaAbrirArgos struct {
	Destino *DestinoArgos `json:"destino,omitempty" doc:"Sin destino, la persona llega al inicio del panel de Argos: es «Vincular mi usuario de Argos»"`
}

// EnlaceArgos es el enlace de un solo uso para abrir Argos.
type EnlaceArgos struct {
	URL      string    `json:"url" doc:"https://<panel de Argos>/sso/entrar#<ticket>: se abre en otra pestaña. De un solo uso; no se guarda ni se repite"`
	ExpiraEn time.Time `json:"expiraEn" doc:"El ticket dura 60 s"`
}

// AbrirArgos pide el ticket a Argos (fuera de transacción) y deja la auditoría y el evento.
type AbrirArgos struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaAbrirArgos, previoAbrir]
	final  *auditoria.Caso[abrirAuditado, struct{}]
}

type previoAbrir struct {
	conector conector.Leido
	usuario  int64
	correo   string
	nombre   string
}

// abrirAuditado es lo que queda en la auditoría: nunca la URL ni el ticket.
type abrirAuditado struct {
	ConectorID  int64   `json:"conector_id"`
	ConDestino  bool    `json:"con_destino"`
	DestinoTipo *string `json:"destino_tipo,omitempty"`
	DestinoID   *string `json:"destino_id,omitempty"`
	usuario     int64
}

var idDestinoArgos = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

// conectorApagadoAbrir: 409 (no el 503 de las lecturas: aquí no hay datos locales con que seguir).
func conectorApagadoAbrir() *nucleo.Problema {
	return nucleo.Conflicto("conector_apagado", "No hay un conector con ArgosISP activo: pide a un administrador que lo conecte y lo active")
}

func soloPersonas(que string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "solo_personas", "Solo personas", que+" solo lo puede hacer una persona con su sesión del panel, no un token ni una integración")
}

func nuevoAbrirArgos(m *Modulo) *AbrirArgos {
	k := &AbrirArgos{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaAbrirArgos, previoAbrir]{
		Accion: "integraciones.abrir_argos", Permiso: nucleo.PermIntegracionesAbrirArgos, SinEmpresa: true,
		AuditarExito: func(previoAbrir) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAbrirArgos) (previoAbrir, error) {
			actor := t.Actor()
			if actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
				return previoAbrir{}, soloPersonas("Abrir Argos con tu usuario")
			}
			if in.Destino != nil && ((in.Destino.Tipo != "cliente" && in.Destino.Tipo != "servicio") || !idDestinoArgos.MatchString(in.Destino.IDExterno)) {
				return previoAbrir{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "destino", Message: "Destino inválido (cliente o servicio, con su id de Argos)", Rule: "destino"})
			}
			c, err := m.conectorActivo(ctx, t)
			var p *nucleo.Problema
			if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
				return previoAbrir{}, conectorApagadoAbrir()
			}
			if err != nil {
				return previoAbrir{}, err
			}
			t.FijarEntidad("conector", c.ID)
			if !SoportaAprobarComprobantes(c.ContratoRemoto) {
				return previoAbrir{}, SinAprobacion()
			}
			espera, err := tomarCuotaAbrir(ctx, t, actor.ID)
			if err != nil {
				return previoAbrir{}, err
			}
			if espera > 0 {
				return previoAbrir{}, nucleo.DemasiadosIntentos("demasiados_intentos",
					"Pediste demasiados enlaces a Argos en el último minuto; espera un momento", espera)
			}
			r := previoAbrir{conector: c, usuario: actor.ID}
			err = t.QueryRow(ctx, `SELECT correo, nombre FROM nucleo.usuarios WHERE id = $1`, actor.ID).Scan(&r.correo, &r.nombre)
			if errors.Is(err, pgx.ErrNoRows) {
				return previoAbrir{}, nucleo.NoAutenticado("")
			}
			return r, err
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[abrirAuditado, struct{}]{
		Accion: "integraciones.abrir_argos", Permiso: nucleo.PermIntegracionesAbrirArgos, SinEmpresa: true,
		Logica: func(_ context.Context, t *auditoria.Tx, in abrirAuditado) (struct{}, error) {
			t.FijarEntidad("conector", in.ConectorID)
			t.FijarEmpresa(0)
			// La URL lleva el ticket: en la auditoría queda enmascarada.
			t.Anotar(map[string]any{"url": auditoria.Mascara})
			return struct{}{}, t.Emitir("usuario.argos_abierto", map[string]any{"usuario_id": in.usuario, "conector_id": in.ConectorID, "con_destino": in.ConDestino})
		},
	})
	return k
}

// tomarCuotaAbrir cuenta un pedido en la ventana de un minuto del usuario
// (integracion.abrir_argos_cuotas: en la base, vale con varias réplicas de la API; la fila serializa
// los pedidos simultáneos). Pasado el tope devuelve cuánto falta para la siguiente ventana (0 = puede
// seguir); el pedido rechazado no cuenta (la transacción del caso se revierte).
func tomarCuotaAbrir(ctx context.Context, q conector.DBTX, usuario int64) (time.Duration, error) {
	var pedidos int
	var restante float64
	err := q.QueryRow(ctx, `INSERT INTO integracion.abrir_argos_cuotas AS c (usuario_id, inicio, pedidos) VALUES ($1, clock_timestamp(), 1)
		ON CONFLICT (usuario_id) DO UPDATE SET
			inicio = CASE WHEN c.inicio <= clock_timestamp() - interval '1 minute' THEN clock_timestamp() ELSE c.inicio END,
			pedidos = CASE WHEN c.inicio <= clock_timestamp() - interval '1 minute' THEN 1 ELSE c.pedidos + 1 END
		RETURNING pedidos, extract(epoch FROM (inicio + interval '1 minute' - clock_timestamp()))::float8`, usuario).Scan(&pedidos, &restante)
	if err != nil {
		return 0, err
	}
	if pedidos <= TopeAbrirArgos {
		return 0, nil
	}
	return max(time.Duration(restante*float64(time.Second)), time.Second), nil
}

type ticketArgos struct {
	URL      string `json:"url"`
	ExpiraEn string `json:"expira_en"`
}

// Ejecutar devuelve el enlace de un solo uso.
func (k *AbrirArgos) Ejecutar(ctx context.Context, in EntradaAbrirArgos) (EnlaceArgos, error) {
	// La respuesta lleva el ticket: el middleware de idempotencia no la guarda ni la repite.
	nucleo.NoRepetible(ctx)
	pre, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return EnlaceArgos{}, err
	}
	// usuario.id_externo: el id del usuario de Iris en texto, el mismo de X-Iris-Actor (§7.1).
	usuario := map[string]any{"id_externo": strconv.FormatInt(pre.usuario, 10), "correo": pre.correo, "nombre": pre.nombre}
	cuerpo := map[string]any{"usuario": usuario}
	auditado := abrirAuditado{ConectorID: pre.conector.ID, usuario: pre.usuario}
	if in.Destino != nil {
		cuerpo["destino"] = map[string]any{"tipo": in.Destino.Tipo, "id_externo": in.Destino.IDExterno}
		auditado.ConDestino, auditado.DestinoTipo, auditado.DestinoID = true, &in.Destino.Tipo, &in.Destino.IDExterno
	}
	var r ticketArgos
	if err := k.m.clienteDe(pre.conector).Pedir(ctx, http.MethodPost, rutaTicketsArgos, nil, cuerpo, false, &conector.Recurso{Destino: &r}); err != nil {
		return EnlaceArgos{}, problemaAbrir(err)
	}
	enlace, err := k.validar(pre.conector, r)
	if err != nil {
		return EnlaceArgos{}, err
	}
	if _, err := k.final.Ejecutar(ctx, auditado); err != nil {
		return EnlaceArgos{}, err
	}
	return enlace, nil
}

// validar revisa lo que respondió Argos: una URL http(s) a /sso/entrar con el ticket en el
// fragmento y en el host del panel de Argos que Iris conoce: la URL pública configurada a mano, la
// del canje (panel_url) o, si el conector no tiene ninguna, la de su API (base_url). Nunca un host libre: lo que no cumple no se le
// entrega al navegador.
func (k *AbrirArgos) validar(c conector.Leido, r ticketArgos) (EnlaceArgos, error) {
	inesperada := conector.Rechazo(Remoto, "Argos respondió un enlace de autologin que Iris no reconoce")
	u, err := url.Parse(strings.TrimSpace(r.URL))
	if err != nil || u.Host == "" || u.User != nil || u.Fragment == "" || !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/sso/entrar") {
		return EnlaceArgos{}, inesperada
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !k.m.cfg.PermitirHTTP) {
		return EnlaceArgos{}, inesperada
	}
	// El mismo origen que el panel recibe en GET /integraciones/enlaces (argosisp.urlPublica).
	conocido := conector.URLPublicaDe(c.IntegracionConectore)
	if panel, err := url.Parse(strings.TrimSpace(conocido)); err != nil || panel.Host == "" || !strings.EqualFold(panel.Host, u.Host) {
		return EnlaceArgos{}, inesperada
	}
	expira, err := time.Parse(time.RFC3339, r.ExpiraEn)
	if err != nil {
		expira = time.Now().Add(time.Minute)
	}
	return EnlaceArgos{URL: u.String(), ExpiraEn: expira.UTC()}, nil
}

// problemaAbrir traduce los errores de Argos al pedir el ticket.
func problemaAbrir(err error) error {
	var er *ErrorArgos
	if !errors.As(err, &er) {
		return err
	}
	switch {
	case er.Status == http.StatusConflict && er.Codigo == "iris_apagado":
		return IrisApagado()
	case er.Caido():
		return NoDisponible()
	}
	return Problema(err)
}

// IrisApagado: Argos tiene apagado (o desconectado) su conector con Iris.
func IrisApagado() *nucleo.Problema {
	return nucleo.Conflicto("iris_apagado", "El conector con Iris está apagado en Argos: pide a un administrador de Argos que lo active")
}

// NoDisponible: Argos no respondió una acción que la persona espera al momento (502, con
// «Reintentar»: nada quedó a medias y la misma petición se puede repetir).
func NoDisponible() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusBadGateway, "argos_no_disponible", "Argos no disponible",
		"Argos no respondió; vuelve a intentar en unos segundos")
}

func (m *Modulo) registrarAbrirArgos(api huma.API) {
	op := conector.Operacion(conector.EtiquetaIntegraciones, "abrirArgos", http.MethodPost, "/api/v1/integraciones/argosisp/abrir",
		"Abrir Argos con tu usuario («Abrir en Argos» y «Vincular mi usuario de Argos»)", nucleo.PermIntegracionesAbrirArgos, http.StatusOK,
		http.StatusConflict, http.StatusTooManyRequests, http.StatusBadGateway)
	op.Description += " Autologin Iris → Argos (contrato con Argos 0.7.0 §1.3). Solo una persona con su sesión del panel (un token o una integración: 403 `solo_personas`). " +
		"Devuelve un enlace de **un solo uso** que dura 60 s: el panel lo abre en otra pestaña enseguida. Con `destino` llega a ese cliente o servicio; " +
		"sin `destino` llega al inicio de Argos: es «Vincular mi usuario de Argos» (la primera vez Argos pide el correo y la contraseña de Argos y crea el vínculo que autoriza aprobar y rechazar boletas). " +
		"Iris no sabe si el vínculo existe: el panel lo descubre con el 409 `argos_sin_vinculo` al aprobar o rechazar. " +
		"Acepta `Idempotency-Key`, pero la respuesta no se guarda: repetir la clave responde 409 `idempotencia_no_repetible` (pide otro enlace con otra clave). " +
		"409 `conector_apagado` (sin conector con Argos activo) e `iris_apagado` (Argos tiene apagado su conector con Iris); 422 `argos_sin_aprobacion` (Argos no informa contrato ≥ 0.7.0); " +
		"429 `demasiados_intentos` con `Retry-After` (10 por minuto por persona); 403 `reautenticacion_requerida` en una sesión abierta por autologin sin re-autenticar; 502 `argos_no_disponible` (Argos no respondió: «Reintentar»)."
	huma.Register(api, op, func(ctx context.Context, in *struct {
		nucleo.ConIdempotencia
		Body EntradaAbrirArgos
	}) (*conector.Cuerpo[EnlaceArgos], error) {
		return conector.Responder(m.AbrirArgos.Ejecutar(ctx, in.Body))
	})
}
