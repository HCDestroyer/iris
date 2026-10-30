package conector

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Enlace de conexión (contrato Argos ↔ Horus §1.4, reutilizado con Argos y Horus):
// https://<A>/conectar#<codigo>, válido 15 min y de un solo uso; el canje es servidor a
// servidor. Hay dos sentidos:
//
//   - enlace_remoto: un administrador del otro sistema genera el enlace y lo pega en Iris
//     (POST /integraciones/<sistema>/conectar). Iris llama al canje del otro sistema
//     (Sistema.RutaCanje) con el código, su URL, la URL del webhook y el secreto que genera (el
//     receptor genera el secreto), y recibe el token.
//   - enlace_local: un administrador de Iris genera el enlace (POST
//     /integraciones/<sistema>/conexion/enlaces) y lo pega en el otro sistema, que llama al
//     canje de Iris (POST /api/v1/integraciones/<sistema>/conexion/canjear) con el código, su
//     URL y su token, y recibe la URL del webhook y el secreto.
//
// En los dos casos el conector nace apagado hasta que un administrador lo activa.

var codigoValido = regexp.MustCompile(`^[A-Za-z0-9_-]{16,200}$`)

func esSinFilas(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// ValidarOrigen normaliza un origen: https (o http con PermitirHTTP), con host, sin usuario,
// consulta ni fragmento. Conserva un prefijo de ruta (el sistema detrás de /argos).
func (n *Nucleo) validarOrigen(crudo, campo string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(crudo))
	invalido := func(msg string) error {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: msg, Rule: "url"})
	}
	if err != nil || u.Host == "" {
		return nil, invalido("Debe ser una URL absoluta (https://…)")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !n.cfg.PermitirHTTP) {
		return nil, invalido("Debe usar https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, invalido("No puede llevar usuario, consulta ni fragmento")
	}
	return u, nil
}

func origenDe(u *url.URL) string {
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/")
}

// validarBase es validarOrigen más la protección contra SSRF para una IP escrita tal cual (un
// nombre que resuelve a una IP interna se corta al conectar, red.go).
func (c *Conexion) validarBase(crudo, campo string) (string, error) {
	u, err := c.n.validarOrigen(crudo, campo)
	if err != nil {
		return "", err
	}
	if ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil && !DestinoPermitido(ip, c.s.RedesPermitidas) {
		return "", nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Rule: "url",
			Message: "Apunta a una red interna; si es intencional, agrégala a " + c.s.Remoto.VariableRedes})
	}
	return origenDe(u), nil
}

// parsearEnlace separa https://<sistema>[/prefijo]/conectar#<codigo> en base y código.
func (c *Conexion) parsearEnlace(enlace string) (base, codigo string, err error) {
	invalido := nucleo.Validacion(nucleo.ErrorCampo{Field: "enlace", Message: "Debe ser https://<" + strings.ToLower(c.s.Remoto.Nombre) + ">/conectar#<código>", Rule: "enlace"})
	u, err := url.Parse(strings.TrimSpace(enlace))
	if err != nil || !codigoValido.MatchString(u.Fragment) || !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/conectar") {
		return "", "", invalido
	}
	codigo = u.Fragment
	u.Fragment = ""
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/conectar")
	u.RawPath = ""
	base, err = c.validarBase(u.String(), "enlace")
	return base, codigo, err
}

func (c *Conexion) exigirConfiguracion() error {
	if c.n.cfg.Cifrador == nil {
		return SinCifrado()
	}
	if c.n.cfg.BaseURL == "" {
		return nucleo.ReglaNegocio("url_publica_no_configurada", "Falta IRIS_BASE_URL: "+c.s.Remoto.Nombre+" no tendría a dónde enviar los webhooks")
	}
	return nil
}

// insertarConector cifra token y secreto atados a la fila (id reservado) e inserta (apagado).
func (c *Conexion) insertarConector(ctx context.Context, t *auditoria.Tx, id int64, origen, base, token, secreto string, remotoID *string, empresas []int64) (conectorbd.IntegracionConectore, error) {
	lista := make([]EmpresaCanje, 0, len(empresas))
	for _, e := range empresas {
		lista = append(lista, EmpresaCanje{ID: e})
	}
	return c.insertarConectorCon(ctx, t, id, origen, base, token, secreto, remotoID, lista, nil, nil)
}

// insertarConectorCon es insertarConector con el iris_pat_ que Iris entregó en el canje (o nil) y
// lo que informó el canje de Argos (0.3.1): empresas con nombre y nit, y alcance_global (o nil).
func (c *Conexion) insertarConectorCon(ctx context.Context, t *auditoria.Tx, id int64, origen, base, token, secreto string, remotoID *string, lista []EmpresaCanje, alcanceGlobal *bool, entregado *int64) (conectorbd.IntegracionConectore, error) {
	q := conectorbd.New(t)
	tokenCifrado, err := c.n.cifrar(id, "token", token)
	if err != nil {
		return conectorbd.IntegracionConectore{}, err
	}
	secretoCifrado, err := c.n.cifrar(id, "secreto_webhook", secreto)
	if err != nil {
		return conectorbd.IntegracionConectore{}, err
	}
	var creadoPor *int64
	if a := t.Actor(); a.EsUsuario() {
		creadoPor = &a.ID
	}
	if !c.s.RegistrarEmpresas {
		lista, alcanceGlobal = nil, nil
	}
	empresas := make([]int64, 0, len(lista))
	for _, e := range lista {
		if e.ID > 0 && !slices.Contains(empresas, e.ID) {
			empresas = append(empresas, e.ID)
		}
	}
	f, err := q.InsertarConector(ctx, conectorbd.InsertarConectorParams{
		ID: id, Sistema: c.s.Nombre, BaseUrl: base, Origen: origen, TokenCifrado: tokenCifrado,
		SecretoWebhookCifrado: secretoCifrado, RemotoID: remotoID, Empresas: empresas, CreadoPor: creadoPor,
		TokenEntregadoID: entregado, AlcanceGlobal: alcanceGlobal,
	})
	if err != nil {
		return f, err
	}
	// Las empresas que cubre el token quedan registradas (multiempresa): un administrador les
	// pone nombre y las asigna a los usuarios.
	registradas := map[int64]bool{}
	for _, e := range lista {
		if e.ID <= 0 || registradas[e.ID] {
			continue
		}
		registradas[e.ID] = true
		nombre := e.Nombre
		if nombre == "" {
			nombre = "Empresa " + itoa(e.ID)
		}
		if err := q.RegistrarEmpresaConector(ctx, conectorbd.RegistrarEmpresaConectorParams{ID: e.ID, Nombre: nombre, Nit: e.NIT}); err != nil {
			return f, err
		}
	}
	t.FijarEmpresa(0)
	t.RegistrarCambios("conector", f.ID, nil, c.n.AConector(f))
	return f, t.Emitir("conector.creado", map[string]any{"conector_id": f.ID, "sistema": f.Sistema, "origen": f.Origen})
}

// Conectar con un enlace del otro sistema --------------------------------------------------

type entradaGuardarConector struct {
	Base       string `json:"base_url"`
	ConectorID int64  `json:"conector_id"`
	// tokenEntregado: id del iris_pat_ entregado al otro sistema (queda en la fila y la auditoría).
	tokenEntregado int64
	secreto        string
	respuesta      RespuestaCanje
	errRemoto      error
}

// Conectar canjea un enlace generado en el otro sistema (sentido enlace_remoto). La llamada va
// fuera de transacción, entre un caso que reserva el id y otro que guarda el conector.
//
// Seguridad: Iris entrega un iris_pat_ al origen del enlace, así que solo acepta orígenes de
// Sistema.Origenes (o el de un conector que ya existe), exige empresas.todas global y el token nace
// con vencimiento (VigenciaTokenCanje): solo queda sin vencimiento al activar el conector. Si el
// canje no se completa (rechazo, respuesta inválida, error o pánico), se revoca.
//
// Riesgo: si el otro sistema canjea el código pero Iris no logra guardar el conector (caída
// entre las dos fases), el código queda usado y hay que generar otro enlace.
type Conectar struct {
	c        *Conexion
	reservar *auditoria.Caso[EntradaConectar, int64]
	guardar  *auditoria.Caso[entradaGuardarConector, Conector]
}

func nuevoConectar(c *Conexion) *Conectar {
	k := &Conectar{c: c}
	k.reservar = auditoria.Nuevo(auditoria.Caso[EntradaConectar, int64]{
		Accion: "integraciones.conectar", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		AuditarExito: soloLecturas[int64],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConectar) (int64, error) {
			if err := c.exigirConfiguracion(); err != nil {
				return 0, err
			}
			base, _, err := c.parsearEnlace(in.Enlace)
			if err != nil {
				return 0, err
			}
			if c.n.tokensFijados() == nil {
				return 0, errSinTokens
			}
			// El token que se entrega es de un usuario de integración con todas las empresas: solo
			// lo emite quien opera en todas.
			if !contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas)) {
				return 0, nucleo.Prohibido("Conectar con " + c.s.Remoto.Nombre + " exige operar en todas las empresas (empresas.todas sin límite de alcance)")
			}
			q := conectorbd.New(t)
			if err := c.exigirOrigen(ctx, q, base); err != nil {
				return 0, err
			}
			return q.SiguienteIDConector(ctx)
		},
	})
	k.guardar = auditoria.Nuevo(auditoria.Caso[entradaGuardarConector, Conector]{
		Accion: "integraciones.conectar", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarConector) (Conector, error) {
			t.FijarEntidad("conector", in.ConectorID)
			t.Anotar(map[string]any{"sistema": c.s.Nombre, "token_entregado_id": in.tokenEntregado})
			if in.errRemoto != nil {
				return Conector{}, Problema(in.errRemoto)
			}
			token := strings.TrimSpace(in.respuesta.Token)
			if !strings.HasPrefix(token, c.s.PrefijoToken) || len(token) < len(c.s.PrefijoToken)+8 {
				return Conector{}, Rechazo(c.s.Remoto, "El canje de "+c.s.Remoto.Nombre+" no devolvió un token "+c.s.PrefijoToken+"…")
			}
			remotoID, err := in.respuesta.ConectorIDValido()
			if err != nil {
				return Conector{}, Rechazo(c.s.Remoto, "El canje de "+c.s.Remoto.Nombre+" devolvió un conector_id inválido")
			}
			var entregado *int64
			if in.tokenEntregado != 0 {
				entregado = &in.tokenEntregado
			}
			f, err := c.insertarConectorCon(ctx, t, in.ConectorID, "enlace_remoto", in.Base, token, in.secreto,
				remotoID, in.respuesta.EmpresasCanje(), in.respuesta.AlcanceGlobal, entregado)
			if err != nil {
				return Conector{}, err
			}
			// Los tokens de intentos anteriores sin activar ya no sirven. El del conector activo se
			// revoca al activar este (la integración no se corta mientras tanto).
			anteriores, err := conectorbd.New(t).TokensEntregadosDeInactivos(ctx, conectorbd.TokensEntregadosDeInactivosParams{Sistema: c.s.Nombre, Excepto: f.ID})
			if err != nil {
				return Conector{}, err
			}
			for _, id := range anteriores {
				c.n.revocarEntregado(ctx, t.Tx, id, "conector_reemplazado")
			}
			if len(anteriores) > 0 {
				t.Anotar(map[string]any{"tokens_revocados": anteriores})
			}
			return c.n.AConector(f), nil
		},
	})
	return k
}

// Ejecutar valida el enlace, reserva el id, emite el token, canjea en el otro sistema y guarda el
// conector apagado.
func (k *Conectar) Ejecutar(ctx context.Context, in EntradaConectar) (con Conector, err error) {
	id, err := k.reservar.Ejecutar(ctx, in)
	if err != nil {
		return Conector{}, err
	}
	base, codigo, _ := k.c.parsearEnlace(in.Enlace)
	secreto, err := NuevoSecreto()
	if err != nil {
		return Conector{}, err
	}
	// El iris_pat_ con el que el otro sistema leerá de Iris (§1.2): va en la petición, nace con
	// vencimiento y se revoca si el canje no termina bien (también ante un pánico).
	tokenID, valor, err := k.c.n.tokensFijados().EmitirTokenCanje(ctx, k.c.s.Nombre, time.Now().Add(VigenciaTokenCanje))
	if err != nil {
		return Conector{}, err
	}
	ok := false
	defer func() {
		if !ok {
			k.c.n.revocarEntregado(context.WithoutCancel(ctx), nil, tokenID, "canje_no_completado")
		}
	}()
	g := entradaGuardarConector{Base: base, ConectorID: id, secreto: secreto, tokenEntregado: tokenID}
	cli := k.c.clienteHacia(base, "")
	// Sin reintentos (el código es de un solo uso) y sin token.
	g.errRemoto = cli.Pedir(ctx, http.MethodPost, k.c.s.RutaCanje, nil, PeticionCanje{
		Codigo: codigo, Sistema: "iris", BaseURL: k.c.n.cfg.BaseURL, Token: valor,
		Webhook: WebhookCanje{URL: k.c.n.URLWebhook(k.c.s.Nombre, id), Secreto: secreto},
	}, false, &Recurso{Destino: &g.respuesta})
	antesDeGuardar()
	con, err = k.guardar.Ejecutar(ctx, g)
	ok = err == nil
	return con, err
}

// antesDeGuardar es un punto de prueba (pánico entre el canje y el guardado); no hace nada.
var antesDeGuardar = func() {}

// exigirOrigen: el origen (esquema://host[:puerto]) de un enlace debe estar en Sistema.Origenes o
// ser el del conector activo del sistema. La lista es obligatoria (IRIS_CONECTORES_PERMITIR_HTTP solo
// permite http, no cualquier origen), así un enlace de phishing no se lleva el token del canje.
func (c *Conexion) exigirOrigen(ctx context.Context, q *conectorbd.Queries, base string) error {
	origen, err := nucleo.NormalizarOrigen(base)
	if err != nil {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "enlace", Message: "Origen inválido", Rule: "url"})
	}
	if slices.Contains(c.s.Origenes, origen) {
		return nil
	}
	activo, err := q.ExisteConectorConBase(ctx, conectorbd.ExisteConectorConBaseParams{Sistema: c.s.Nombre, BaseUrl: base})
	if err != nil {
		return err
	}
	if activo {
		return nil
	}
	if len(c.s.Origenes) == 0 {
		return nucleo.ReglaNegocio("origenes_no_configurados", "Falta "+c.s.Remoto.VariableOrigenes+": configura el origen de "+
			c.s.Remoto.Nombre+" (https://host[:puerto]) antes de conectar; Iris solo entrega su token a orígenes conocidos")
	}
	return nucleo.ReglaNegocio("origen_no_permitido", "El enlace es de "+origen+", que no está en "+c.s.Remoto.VariableOrigenes+
		": si de verdad es "+c.s.Remoto.Nombre+", agrégalo a la lista")
}

// Previsualizar un enlace (sin llamar afuera) ------------------------------------------------

// Previsualizacion es lo que el panel muestra antes de canjear: a qué origen irá el token.
type Previsualizacion struct {
	Sistema           string  `json:"sistema"`
	Origen            string  `json:"origen" doc:"esquema://host[:puerto] del enlace: adónde irá el token de Iris"`
	Permitido         bool    `json:"permitido" doc:"El origen está en la lista o es el del conector activo"`
	Motivo            *string `json:"motivo" nullable:"true" doc:"Si no se permite: origenes_no_configurados u origen_no_permitido"`
	ConectorExistente bool    `json:"conectorExistente" doc:"El conector activo de este sistema tiene ese origen"`
}

func nuevoPrevisualizar(c *Conexion) *auditoria.Caso[EntradaConectar, Previsualizacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConectar, Previsualizacion]{
		Accion: "integraciones.previsualizar", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		AuditarExito: soloLecturas[Previsualizacion],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConectar) (Previsualizacion, error) {
			base, _, err := c.parsearEnlace(in.Enlace)
			if err != nil {
				return Previsualizacion{}, err
			}
			origen, err := nucleo.NormalizarOrigen(base)
			if err != nil {
				return Previsualizacion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "enlace", Message: "Origen inválido", Rule: "url"})
			}
			q := conectorbd.New(t)
			p := Previsualizacion{Sistema: c.s.Nombre, Origen: origen, Permitido: true}
			if p.ConectorExistente, err = q.ExisteConectorConBase(ctx, conectorbd.ExisteConectorConBaseParams{Sistema: c.s.Nombre, BaseUrl: base}); err != nil {
				return p, err
			}
			if err := c.exigirOrigen(ctx, q, base); err != nil {
				pr, _ := nucleo.AProblema(err)
				if pr == nil || pr.Status >= 500 {
					return p, err
				}
				motivo := pr.Codigo()
				p.Permitido, p.Motivo = false, &motivo
			}
			return p, nil
		},
	})
}

// Generar un enlace para el otro sistema ----------------------------------------------------

func nuevoGenerarEnlace(c *Conexion) *auditoria.Caso[struct{}, EnlaceGenerado] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, EnlaceGenerado]{
		Accion: "integraciones.generar_enlace", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (EnlaceGenerado, error) {
			if err := c.exigirConfiguracion(); err != nil {
				return EnlaceGenerado{}, err
			}
			codigo, err := NuevoSecreto()
			if err != nil {
				return EnlaceGenerado{}, err
			}
			var creadoPor *int64
			if a := t.Actor(); a.EsUsuario() {
				creadoPor = &a.ID
			}
			f, err := conectorbd.New(t).InsertarCodigoConexion(ctx, conectorbd.InsertarCodigoConexionParams{
				Sistema: c.s.Nombre, CodigoHash: Hash256(codigo), ExpiraEn: time.Now().Add(c.n.cfg.VigenciaEnlace), CreadoPor: creadoPor,
			})
			if err != nil {
				return EnlaceGenerado{}, err
			}
			t.FijarEntidad("codigo_conexion", f.ID)
			t.FijarEmpresa(0)
			t.Anotar(map[string]any{"sistema": c.s.Nombre})
			return EnlaceGenerado{Enlace: c.n.cfg.BaseURL + "/conectar#" + codigo, ExpiraEn: f.ExpiraEn}, nil
		},
	})
}

// Canje que llama el otro sistema ------------------------------------------------------------

func nuevoCanjearConexion(c *Conexion) *auditoria.Caso[EntradaCanjear, ConexionCanjeada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCanjear, ConexionCanjeada]{
		Accion: "integraciones.canjear", Acceso: auditoria.Publico,
		// Un código inválido es un 401 anónimo: no se audita (se cuenta y va al log con muestreo).
		AuditarError: c.n.auditarRechazo("integraciones.canjear"),
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanjear) (ConexionCanjeada, error) {
			if err := c.exigirConfiguracion(); err != nil {
				return ConexionCanjeada{}, err
			}
			q := conectorbd.New(t)
			codigo, err := q.TomarCodigoConexion(ctx, conectorbd.TomarCodigoConexionParams{CodigoHash: Hash256(in.Codigo), Sistema: c.s.Nombre})
			if esSinFilas(err) {
				return ConexionCanjeada{}, nucleo.NuevoProblema(http.StatusUnauthorized, "codigo_invalido", "Código inválido",
					"El código de conexión no existe, venció o ya se usó")
			}
			if err != nil {
				return ConexionCanjeada{}, err
			}
			if in.Sistema != c.s.Nombre {
				return ConexionCanjeada{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "sistema", Message: "Debe ser " + c.s.Nombre, Rule: "igual"})
			}
			base, err := c.validarBase(in.URL, "url")
			if err != nil {
				return ConexionCanjeada{}, err
			}
			if err := c.exigirOrigen(ctx, q, base); err != nil {
				return ConexionCanjeada{}, err
			}
			token := strings.TrimSpace(in.Token)
			if !strings.HasPrefix(token, c.s.PrefijoToken) {
				return ConexionCanjeada{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "token",
					Message: "Debe ser un token " + c.s.PrefijoToken + "… de " + c.s.Remoto.Nombre, Rule: "prefijo"})
			}
			id, err := q.SiguienteIDConector(ctx)
			if err != nil {
				return ConexionCanjeada{}, err
			}
			secreto, err := NuevoSecreto()
			if err != nil {
				return ConexionCanjeada{}, err
			}
			var remoto *string
			if in.ConexionID != "" {
				remoto = &in.ConexionID
			}
			f, err := c.insertarConector(ctx, t, id, "enlace_local", base, token, secreto, remoto, in.Empresas)
			if err != nil {
				return ConexionCanjeada{}, err
			}
			if err := q.VincularCodigoConexion(ctx, conectorbd.VincularCodigoConexionParams{ConectorID: &f.ID, ID: codigo.ID}); err != nil {
				return ConexionCanjeada{}, err
			}
			t.IdentificarActor(ActorDe(c.s.Nombre, f.ID))
			return ConexionCanjeada{ConectorID: f.ID, Sistema: "iris", WebhookURL: c.n.URLWebhook(c.s.Nombre, f.ID), WebhookSecreto: secreto, Estado: f.Estado}, nil
		},
	})
}

// Activar, desactivar, listar (por id, cualquier sistema) ------------------------------------

func nuevoActivarConector(n *Nucleo) *auditoria.Caso[EntradaCambioConector, Conector] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambioConector, Conector]{
		Accion: "integraciones.activar", Permiso: nucleo.PermIntegracionesActivar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambioConector) (Conector, error) {
			q := conectorbd.New(t)
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
			if _, err := n.Leer(antes); err != nil {
				return Conector{}, err
			}
			// El token que Iris entregó en el canje deja de vencer (si ya venció o se revocó: 422,
			// hay que volver a conectar).
			if antes.TokenEntregadoID != nil {
				tokens := n.tokensFijados()
				if tokens == nil {
					return Conector{}, errSinTokens
				}
				if err := tokens.ConfirmarTokenCanje(ctx, *antes.TokenEntregadoID); err != nil {
					return Conector{}, err
				}
				t.Anotar(map[string]any{"token_entregado_confirmado": *antes.TokenEntregadoID})
			}
			// Reconectar: el conector activo del sistema (si hay) queda reemplazado en esta misma
			// transacción, así la integración no se corta entre el canje y la activación. Su token, y
			// los de otros intentos sin activar, se revocan ahora (no al canjear).
			if otro, err := q.ConectorActivo(ctx, antes.Sistema); err == nil {
				apagado, err := q.CambiarEstadoConector(ctx, conectorbd.CambiarEstadoConectorParams{Estado: "apagado", ID: otro.ID, Version: otro.Version})
				if err := nucleo.VerificarVersion(err, "conector", otro.ID); err != nil {
					return Conector{}, err
				}
				t.RegistrarCambios("conector", otro.ID, n.AConector(otro), n.AConector(apagado))
				if err := t.Emitir("conector.desactivado", map[string]any{
					"conector_id": otro.ID, "sistema": otro.Sistema, "motivo": "reemplazado por el conector " + itoa(in.ID),
				}); err != nil {
					return Conector{}, err
				}
				t.Anotar(map[string]any{"reemplaza_a": otro.ID})
			} else if !esSinFilas(err) {
				return Conector{}, err
			}
			otros, err := q.TokensEntregadosDeOtros(ctx, conectorbd.TokensEntregadosDeOtrosParams{Sistema: antes.Sistema, Excepto: in.ID})
			if err != nil {
				return Conector{}, err
			}
			for _, id := range otros {
				n.revocarEntregado(ctx, t.Tx, id, "conector_reemplazado")
			}
			if len(otros) > 0 {
				t.Anotar(map[string]any{"tokens_revocados": otros})
			}
			var actor *int64
			if a := t.Actor(); a.EsUsuario() {
				actor = &a.ID
			}
			despues, err := q.CambiarEstadoConector(ctx, conectorbd.CambiarEstadoConectorParams{Estado: "activo", ActorID: actor, ID: in.ID, Version: in.Version})
			if err := nucleo.VerificarVersion(err, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			t.RegistrarCambios("conector", in.ID, n.AConector(antes), n.AConector(despues))
			if err := t.Emitir("conector.activado", map[string]any{"conector_id": in.ID, "sistema": despues.Sistema}); err != nil {
				return Conector{}, err
			}
			// Trabajo del sistema al activar (Argos: primera carga de zonas y routers), en la
			// misma transacción.
			if c := n.sistema(despues.Sistema); c != nil && c.s.AlActivar != nil {
				if err := c.s.AlActivar(ctx, t.Tx); err != nil {
					return Conector{}, err
				}
			}
			return n.AConector(despues), nil
		},
	})
}

func nuevoDesactivarConector(n *Nucleo) *auditoria.Caso[EntradaCambioConector, Conector] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambioConector, Conector]{
		Accion: "integraciones.desactivar", Permiso: nucleo.PermIntegracionesDesactivar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambioConector) (Conector, error) {
			q := conectorbd.New(t)
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
			despues, err := q.CambiarEstadoConector(ctx, conectorbd.CambiarEstadoConectorParams{Estado: "apagado", ID: in.ID, Version: in.Version})
			if err := nucleo.VerificarVersion(err, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			t.RegistrarCambios("conector", in.ID, n.AConector(antes), n.AConector(despues))
			// Apagado, el otro sistema ya no lee de Iris con el token del canje: se revoca (volver
			// a activarlo exige conectar de nuevo).
			if antes.TokenEntregadoID != nil {
				n.revocarEntregado(ctx, t.Tx, *antes.TokenEntregadoID, "conector_desactivado")
				t.Anotar(map[string]any{"token_entregado_revocado": *antes.TokenEntregadoID})
			}
			return n.AConector(despues), t.Emitir("conector.desactivado", map[string]any{
				"conector_id": in.ID, "sistema": despues.Sistema, "motivo": in.Motivo,
			})
		},
	})
}

func nuevoListarConectores(n *Nucleo) *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Conector]] {
	return auditoria.Nuevo(auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Conector]]{
		Accion: "integraciones.ver", Permiso: nucleo.PermIntegracionesVer, SinEmpresa: true,
		AuditarExito: soloLecturas[nucleo.Pagina[Conector]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in nucleo.ParamsPagina) (nucleo.Pagina[Conector], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Conector]{}, err
			}
			filas, err := conectorbd.New(t).ListarConectores(ctx, conectorbd.ListarConectoresParams{DespuesDe: despues, Limite: int32(in.Limite() + 1)}) //nolint:gosec // ≤ 201
			if err != nil {
				return nucleo.Pagina[Conector]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), n.AConector,
				func(f conectorbd.IntegracionConectore) []any { return []any{f.ID} }), nil
		},
	})
}

// URL pública y enlaces «Ver en …» -------------------------------------------------------

func nuevoCambiarURLPublica(n *Nucleo) *auditoria.Caso[EntradaURLPublica, Conector] {
	return auditoria.Nuevo(auditoria.Caso[EntradaURLPublica, Conector]{
		Accion: "integraciones.configurar_url", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaURLPublica) (Conector, error) {
			q := conectorbd.New(t)
			t.FijarEntidad("conector", in.ID)
			t.FijarEmpresa(0)
			var nueva *string
			if in.URLPublica != nil && strings.TrimSpace(*in.URLPublica) != "" {
				// Solo se muestra como enlace en el panel (Iris no la llama): basta con que sea un
				// origen http(s) limpio; nada de javascript: ni credenciales.
				u, err := n.validarOrigen(*in.URLPublica, "urlPublica")
				if err != nil {
					return Conector{}, err
				}
				o := origenDe(u)
				nueva = &o
			}
			antes, err := q.ConectorPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Conector{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			despues, err := q.CambiarURLPublicaConector(ctx, conectorbd.CambiarURLPublicaConectorParams{UrlPublica: nueva, ID: in.ID, Version: in.Version})
			if err := nucleo.VerificarVersion(err, "conector", in.ID); err != nil {
				return Conector{}, err
			}
			campos := t.RegistrarCambios("conector", in.ID, n.AConector(antes), n.AConector(despues))
			if len(campos) == 0 {
				return n.AConector(despues), nil
			}
			return n.AConector(despues), t.Emitir("conector.actualizado", map[string]any{
				"conector_id": in.ID, "sistema": despues.Sistema, "campos": campos,
			})
		},
	})
}

func nuevoObtenerEnlaces(n *Nucleo) *auditoria.Caso[struct{}, EnlacesExternos] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, EnlacesExternos]{
		// Mismo permiso que ver los datos de Argos (los enlaces llevan a sus fichas): un token o
		// usuario sin clientes.ver no conoce los orígenes de Argos ni de Horus. No es operativa.
		Accion: "integraciones.enlaces", Permiso: nucleo.PermClientesVer, SinEmpresa: true,
		AuditarExito: soloLecturas[EnlacesExternos],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (EnlacesExternos, error) {
			filas, err := conectorbd.New(t).ConectoresActivos(ctx)
			if err != nil {
				return EnlacesExternos{}, err
			}
			var e EnlacesExternos
			for _, f := range filas {
				enlace := &EnlaceExterno{ConectorID: f.ID, URLPublica: urlPublica(f)}
				switch f.Sistema {
				case "argosisp":
					e.Argos = enlace
				case "horus":
					e.Horus = enlace
				}
			}
			return e, nil
		},
	})
}
