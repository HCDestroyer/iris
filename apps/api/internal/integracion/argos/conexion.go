package argos

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Enlace de conexión (§1.3, igual que Argos ↔ Horus): https://<A>/conectar#<codigo>, válido
// 15 min y de un solo uso; el canje es servidor a servidor. Hay dos sentidos:
//
//   - enlace_remoto: un administrador de Argos genera el enlace y lo pega en Iris
//     (POST /integraciones/argosisp/conectar). Iris llama al canje de Argos
//     (POST /api/v1/integraciones/iris/conexion/canjear) con el código, su URL, la URL del
//     webhook y el secreto que genera (el receptor genera el secreto), y recibe el token atk_….
//   - enlace_local: un administrador de Iris genera el enlace (POST
//     /integraciones/argosisp/conexion/enlaces) y lo pega en Argos. Argos llama al canje de Iris
//     (POST /api/v1/integraciones/argosisp/conexion/canjear) con el código, su URL y el token
//     atk_…, y recibe la URL del webhook y el secreto.
//
// En los dos casos el conector nace apagado hasta que un administrador lo activa.

const prefijoToken = "atk_"

var codigoValido = regexp.MustCompile(`^[A-Za-z0-9_-]{16,200}$`)

// validarBase normaliza el origen de Argos: https (o http con PermitirHTTP), con host, sin
// usuario, consulta ni fragmento. Conserva un prefijo de ruta (Argos detrás de /argos).
func (m *Modulo) validarBase(crudo, campo string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(crudo))
	invalido := func(msg string) error {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: msg, Rule: "url"})
	}
	if err != nil || u.Host == "" {
		return "", invalido("Debe ser una URL absoluta (https://…)")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !m.cfg.PermitirHTTP) {
		return "", invalido("Debe usar https")
	}
	if u.User != nil || u.RawQuery != "" {
		return "", invalido("No puede llevar usuario ni consulta")
	}
	// Una IP interna escrita tal cual se rechaza aquí; un nombre que resuelve a una se corta al
	// conectar (red.go).
	if ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil && !DestinoPermitido(ip, m.cfg.RedesPermitidas) {
		return "", invalido("Apunta a una red interna; si es intencional, agrégala a IRIS_ARGOS_REDES_PERMITIDAS")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/"), nil
}

// parsearEnlace separa https://<argos>[/prefijo]/conectar#<codigo> en base y código.
func (m *Modulo) parsearEnlace(enlace string) (base, codigo string, err error) {
	invalido := nucleo.Validacion(nucleo.ErrorCampo{Field: "enlace", Message: "Debe ser https://<argos>/conectar#<código>", Rule: "enlace"})
	u, err := url.Parse(strings.TrimSpace(enlace))
	if err != nil || !codigoValido.MatchString(u.Fragment) || !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/conectar") {
		return "", "", invalido
	}
	codigo = u.Fragment
	u.Fragment = ""
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/conectar")
	u.RawPath = ""
	base, err = m.validarBase(u.String(), "enlace")
	return base, codigo, err
}

func (m *Modulo) exigirConfiguracion() error {
	if m.cfg.Cifrador == nil {
		return sinCifrado()
	}
	if m.cfg.BaseURL == "" {
		return nucleo.ReglaNegocio("url_publica_no_configurada", "Falta IRIS_BASE_URL: Argos no tendría a dónde enviar los webhooks")
	}
	return nil
}

// insertarConector reserva el id, cifra token y secreto atados a la fila e inserta (apagado).
func (m *Modulo) insertarConector(ctx context.Context, t *auditoria.Tx, id int64, origen, base, token, secreto string, remotoID *string, empresas []int64) (argosbd.IntegracionConectore, error) {
	q := argosbd.New(t)
	tokenCifrado, err := m.cifrar(id, "token", token)
	if err != nil {
		return argosbd.IntegracionConectore{}, err
	}
	secretoCifrado, err := m.cifrar(id, "secreto_webhook", secreto)
	if err != nil {
		return argosbd.IntegracionConectore{}, err
	}
	var creadoPor *int64
	if a := t.Actor(); a.EsUsuario() {
		creadoPor = &a.ID
	}
	if empresas == nil {
		empresas = []int64{}
	}
	f, err := q.InsertarConector(ctx, argosbd.InsertarConectorParams{
		ID: id, Sistema: Sistema, BaseUrl: base, Origen: origen, TokenCifrado: tokenCifrado,
		SecretoWebhookCifrado: secretoCifrado, RemotoID: remotoID, Empresas: empresas, CreadoPor: creadoPor,
	})
	if err != nil {
		return f, err
	}
	// Las empresas que cubre el token quedan registradas (multiempresa): un administrador les
	// pone nombre y las asigna a los usuarios.
	for _, e := range empresas {
		if e <= 0 {
			continue
		}
		if err := q.RegistrarEmpresaArgos(ctx, argosbd.RegistrarEmpresaArgosParams{ID: e, Nombre: "Empresa " + itoa(e)}); err != nil {
			return f, err
		}
	}
	t.FijarEmpresa(0)
	t.RegistrarCambios("conector", f.ID, nil, m.aConector(f))
	return f, t.Emitir("conector.creado", map[string]any{"conector_id": f.ID, "sistema": f.Sistema, "origen": f.Origen})
}

// Conectar con un enlace de Argos ---------------------------------------------------------

// EntradaConectar es la entrada de integraciones.conectar.
type EntradaConectar struct {
	Enlace string `json:"enlace" minLength:"10" maxLength:"2000" auditoria:"sensible" doc:"Enlace de conexión generado en Argos: https://<argos>/conectar#<código> (15 min, un solo uso)"`
}

type entradaGuardarConector struct {
	Base       string `json:"base_url"`
	ConectorID int64  `json:"conector_id"`
	secreto    string
	respuesta  RespuestaCanje
	errArgos   error
}

// Conectar canjea un enlace generado en Argos (sentido enlace_remoto). La llamada a Argos va
// fuera de transacción, entre un caso que reserva el id y otro que guarda el conector.
//
// Riesgo: si Argos canjea el código pero Iris no logra guardar el conector (caída entre las
// dos fases), el código queda usado y hay que generar otro enlace en Argos.
type Conectar struct {
	m        *Modulo
	reservar *auditoria.Caso[EntradaConectar, int64]
	guardar  *auditoria.Caso[entradaGuardarConector, Conector]
}

func nuevoConectar(m *Modulo) *Conectar {
	c := &Conectar{m: m}
	c.reservar = auditoria.Nuevo(auditoria.Caso[EntradaConectar, int64]{
		Accion: "integraciones.conectar", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		AuditarExito: soloLecturas[int64],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConectar) (int64, error) {
			if err := m.exigirConfiguracion(); err != nil {
				return 0, err
			}
			if _, _, err := m.parsearEnlace(in.Enlace); err != nil {
				return 0, err
			}
			return argosbd.New(t).SiguienteIDConector(ctx)
		},
	})
	c.guardar = auditoria.Nuevo(auditoria.Caso[entradaGuardarConector, Conector]{
		Accion: "integraciones.conectar", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarConector) (Conector, error) {
			t.FijarEntidad("conector", in.ConectorID)
			if in.errArgos != nil {
				return Conector{}, Problema(in.errArgos)
			}
			token := strings.TrimSpace(in.respuesta.Token)
			if !strings.HasPrefix(token, prefijoToken) || len(token) < len(prefijoToken)+8 {
				return Conector{}, nucleo.NuevoProblema(http.StatusBadGateway, "argos_rechazo", "Argos rechazó la petición",
					"El canje de Argos no devolvió un token atk_…")
			}
			f, err := m.insertarConector(ctx, t, in.ConectorID, "enlace_remoto", in.Base, token, in.secreto,
				in.respuesta.IDConexion(), in.respuesta.IDsEmpresas())
			if err != nil {
				return Conector{}, err
			}
			return m.aConector(f), nil
		},
	})
	return c
}

// Ejecutar valida el enlace, reserva el id, canjea en Argos y guarda el conector apagado.
func (c *Conectar) Ejecutar(ctx context.Context, in EntradaConectar) (Conector, error) {
	id, err := c.reservar.Ejecutar(ctx, in)
	if err != nil {
		return Conector{}, err
	}
	base, codigo, _ := c.m.parsearEnlace(in.Enlace)
	secreto, err := nuevoSecreto()
	if err != nil {
		return Conector{}, err
	}
	g := entradaGuardarConector{Base: base, ConectorID: id, secreto: secreto}
	cli := NuevoCliente(base, "", OpcionesCliente{HTTP: c.m.http, Timeout: c.m.cfg.Timeout, UserAgent: "Iris/" + c.m.cfg.Version})
	g.respuesta, g.errArgos = cli.Canjear(ctx, PeticionCanje{
		Codigo: codigo, Sistema: "iris", URL: c.m.cfg.BaseURL, WebhookURL: c.m.urlWebhook(id), WebhookSecreto: secreto,
	})
	return c.guardar.Ejecutar(ctx, g)
}

// Generar un enlace para Argos -------------------------------------------------------------

// EnlaceGenerado es el enlace que se pega en Argos.
type EnlaceGenerado struct {
	Enlace   string    `json:"enlace" doc:"https://<iris>/conectar#<código>; se muestra una sola vez"`
	ExpiraEn time.Time `json:"expiraEn"`
}

func nuevoGenerarEnlace(m *Modulo) *auditoria.Caso[struct{}, EnlaceGenerado] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, EnlaceGenerado]{
		Accion: "integraciones.generar_enlace", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (EnlaceGenerado, error) {
			if err := m.exigirConfiguracion(); err != nil {
				return EnlaceGenerado{}, err
			}
			codigo, err := nuevoSecreto()
			if err != nil {
				return EnlaceGenerado{}, err
			}
			var creadoPor *int64
			if a := t.Actor(); a.EsUsuario() {
				creadoPor = &a.ID
			}
			f, err := argosbd.New(t).InsertarCodigoConexion(ctx, argosbd.InsertarCodigoConexionParams{
				Sistema: Sistema, CodigoHash: hash256(codigo), ExpiraEn: time.Now().Add(m.cfg.VigenciaEnlace), CreadoPor: creadoPor,
			})
			if err != nil {
				return EnlaceGenerado{}, err
			}
			t.FijarEntidad("codigo_conexion", f.ID)
			t.FijarEmpresa(0)
			return EnlaceGenerado{Enlace: m.cfg.BaseURL + "/conectar#" + codigo, ExpiraEn: f.ExpiraEn}, nil
		},
	})
}

// Canje que llama Argos --------------------------------------------------------------------

// EntradaCanjear es lo que Argos envía al canjear un enlace generado en Iris.
type EntradaCanjear struct {
	Codigo     string  `json:"codigo" minLength:"16" maxLength:"200" auditoria:"sensible" doc:"Código del enlace (lo que sigue a #)"`
	Sistema    string  `json:"sistema" enum:"argosisp" doc:"Sistema que canjea"`
	URL        string  `json:"url" minLength:"8" maxLength:"500" doc:"Origen de Argos (https://…), sin /api/v1"`
	Token      string  `json:"token" minLength:"12" maxLength:"500" doc:"Token atk_… del usuario técnico «Iris» en Argos (§1.1)"`
	ConexionID string  `json:"conexionId,omitempty" maxLength:"100" doc:"Id de la conexión en Argos"`
	Empresas   []int64 `json:"empresas,omitempty" maxItems:"1000" doc:"Empresas de Argos que cubre el token"`
}

// ConexionCanjeada es la respuesta del canje: Argos guarda la URL y el secreto del webhook.
type ConexionCanjeada struct {
	ConectorID     int64  `json:"conectorId"`
	Sistema        string `json:"sistema" enum:"iris"`
	WebhookURL     string `json:"webhookUrl" doc:"URL a la que Argos envía los webhooks firmados (§2.3)"`
	WebhookSecreto string `json:"webhookSecreto" doc:"Secreto para HMAC-SHA256 de X-Webhook-Signature; solo se entrega aquí"`
	Estado         string `json:"estado" enum:"apagado" doc:"El conector queda apagado hasta que un administrador de Iris lo active"`
}

func nuevoCanjearConexion(m *Modulo) *auditoria.Caso[EntradaCanjear, ConexionCanjeada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCanjear, ConexionCanjeada]{
		Accion: "integraciones.canjear", Acceso: auditoria.Publico,
		// Un código inválido es un 401 anónimo: no se audita (se cuenta y va al log con muestreo).
		AuditarError: m.auditarRechazo("integraciones.canjear"),
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanjear) (ConexionCanjeada, error) {
			if err := m.exigirConfiguracion(); err != nil {
				return ConexionCanjeada{}, err
			}
			q := argosbd.New(t)
			codigo, err := q.TomarCodigoConexion(ctx, argosbd.TomarCodigoConexionParams{CodigoHash: hash256(in.Codigo), Sistema: Sistema})
			if errors.Is(err, pgx.ErrNoRows) {
				return ConexionCanjeada{}, nucleo.NuevoProblema(http.StatusUnauthorized, "codigo_invalido", "Código inválido",
					"El código de conexión no existe, venció o ya se usó")
			}
			if err != nil {
				return ConexionCanjeada{}, err
			}
			base, err := m.validarBase(in.URL, "url")
			if err != nil {
				return ConexionCanjeada{}, err
			}
			token := strings.TrimSpace(in.Token)
			if !strings.HasPrefix(token, prefijoToken) {
				return ConexionCanjeada{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "token", Message: "Debe ser un token atk_… de Argos", Rule: "prefijo"})
			}
			id, err := q.SiguienteIDConector(ctx)
			if err != nil {
				return ConexionCanjeada{}, err
			}
			secreto, err := nuevoSecreto()
			if err != nil {
				return ConexionCanjeada{}, err
			}
			var remoto *string
			if in.ConexionID != "" {
				remoto = &in.ConexionID
			}
			f, err := m.insertarConector(ctx, t, id, "enlace_local", base, token, secreto, remoto, in.Empresas)
			if err != nil {
				return ConexionCanjeada{}, err
			}
			if err := q.VincularCodigoConexion(ctx, argosbd.VincularCodigoConexionParams{ConectorID: &f.ID, ID: codigo.ID}); err != nil {
				return ConexionCanjeada{}, err
			}
			t.IdentificarActor(actorArgos(f.ID))
			return ConexionCanjeada{ConectorID: f.ID, Sistema: "iris", WebhookURL: m.urlWebhook(f.ID), WebhookSecreto: secreto, Estado: f.Estado}, nil
		},
	})
}

// Activar y desactivar ---------------------------------------------------------------------

// EntradaCambioConector es la entrada de integraciones.activar / integraciones.desactivar.
type EntradaCambioConector struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión que leíste (409 si cambió)"`
	Motivo  string `json:"motivo,omitempty" maxLength:"500"`
}

func nuevoActivarConector(m *Modulo) *auditoria.Caso[EntradaCambioConector, Conector] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambioConector, Conector]{
		Accion: "integraciones.activar", Permiso: nucleo.PermIntegracionesActivar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambioConector) (Conector, error) {
			q := argosbd.New(t)
			t.FijarEntidad("conector", in.ID)
			t.FijarEmpresa(0)
			antes, err := q.ConectorPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Conector{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			if antes.Estado == "activo" {
				return Conector{}, nucleo.ReglaNegocio("conector_activo", "El conector ya está activo")
			}
			if otro, err := q.ConectorActivo(ctx, antes.Sistema); err == nil {
				return Conector{}, nucleo.Conflicto("otro_conector_activo",
					"Ya hay un conector activo con "+antes.Sistema+" (id "+itoa(otro.ID)+"); desactívalo primero")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return Conector{}, err
			}
			if _, err := m.leerConector(antes); err != nil {
				return Conector{}, err
			}
			var actor *int64
			if a := t.Actor(); a.EsUsuario() {
				actor = &a.ID
			}
			despues, err := q.CambiarEstadoConector(ctx, argosbd.CambiarEstadoConectorParams{Estado: "activo", ActorID: actor, ID: in.ID, Version: in.Version})
			if err := nucleo.VerificarVersion(err, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			t.RegistrarCambios("conector", in.ID, m.aConector(antes), m.aConector(despues))
			if err := t.Emitir("conector.activado", map[string]any{"conector_id": in.ID, "sistema": despues.Sistema}); err != nil {
				return Conector{}, err
			}
			// Primera carga de zonas y routers, en la misma transacción.
			if m.cfg.Encolar != nil {
				if err := m.cfg.Encolar(ctx, t.Tx, ArgsSincronizarCatalogos{}); err != nil {
					return Conector{}, err
				}
			}
			return m.aConector(despues), nil
		},
	})
}

func nuevoDesactivarConector(m *Modulo) *auditoria.Caso[EntradaCambioConector, Conector] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambioConector, Conector]{
		Accion: "integraciones.desactivar", Permiso: nucleo.PermIntegracionesDesactivar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambioConector) (Conector, error) {
			q := argosbd.New(t)
			t.FijarEntidad("conector", in.ID)
			t.FijarEmpresa(0)
			antes, err := q.ConectorPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Conector{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			if antes.Estado != "activo" {
				return Conector{}, nucleo.ReglaNegocio("conector_inactivo", "El conector ya está apagado")
			}
			despues, err := q.CambiarEstadoConector(ctx, argosbd.CambiarEstadoConectorParams{Estado: "apagado", ID: in.ID, Version: in.Version})
			if err := nucleo.VerificarVersion(err, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			t.RegistrarCambios("conector", in.ID, m.aConector(antes), m.aConector(despues))
			return m.aConector(despues), t.Emitir("conector.desactivado", map[string]any{
				"conector_id": in.ID, "sistema": despues.Sistema, "motivo": in.Motivo,
			})
		},
	})
}

func nuevoListarConectores(m *Modulo) *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Conector]] {
	return auditoria.Nuevo(auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Conector]]{
		Accion: "integraciones.ver", Permiso: nucleo.PermIntegracionesVer, SinEmpresa: true,
		AuditarExito: soloLecturas[nucleo.Pagina[Conector]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in nucleo.ParamsPagina) (nucleo.Pagina[Conector], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Conector]{}, err
			}
			filas, err := argosbd.New(t).ListarConectores(ctx, argosbd.ListarConectoresParams{DespuesDe: despues, Limite: int32(in.Limite() + 1)}) //nolint:gosec // ≤ 201
			if err != nil {
				return nucleo.Pagina[Conector]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), m.aConector,
				func(f argosbd.IntegracionConectore) []any { return []any{f.ID} }), nil
		},
	})
}
