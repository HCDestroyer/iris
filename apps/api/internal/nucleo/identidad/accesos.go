package identidad

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Historial de accesos (nucleo.accesos): quién entró, cuándo, por qué canal, desde dónde y con
// qué resultado. Lo escriben el inicio de sesión, el MFA, el canje del autologin y el primer uso
// de cada hora de un token. Los fallos se guardan en la transacción de auditoría del error (que
// sí se confirma), con el usuario solo si existe: el correo no se guarda, así la tabla no sirve
// para enumerar cuentas, y la respuesta del login es la misma exista o no.

// Canales de un acceso (nucleo.accesos.canal).
const (
	CanalAccesoPanel       = "panel"
	CanalAccesoCampoPWA    = "campo_pwa"
	CanalAccesoCampoNativa = "campo_nativa"
	CanalAccesoSSO         = "sso"
	CanalAccesoToken       = "token"
)

// Resultados de un acceso (nucleo.accesos.resultado).
const (
	AccesoExito             = "exito"
	AccesoContrasenaErronea = "contrasena_erronea"
	AccesoMFAFallido        = "mfa_fallido"
	AccesoBloqueado         = "bloqueado"
	AccesoInactivo          = "inactivo"
)

// CabeceraApp identifica la app que hace la petición: `campo/<versión> (<plataforma>)`, p. ej.
// `campo/1.4.0 (android)` o `campo/0.9.2 (pwa)`. La mandan la PWA de campo y la app nativa
// (packages/sync y apps/movil).
const CabeceraApp = "X-Iris-App"

// CabeceraPendientes: operaciones que la app tiene sin enviar en su cola local (opcional).
const CabeceraPendientes = "X-Iris-Pendientes"

// ClienteHTTP es lo que se sabe de quien hace la petición (lo deja el Middleware en el contexto).
type ClienteHTTP struct {
	IP        string
	UserAgent string
	// Canal declarado con X-Iris-Canal (solo «campo» cuenta).
	Canal string
	// App es X-Iris-App ya validada (vacía si no vino o no tiene el formato).
	App        string
	AppNombre  string
	Version    string
	Plataforma string
	// Pendientes informados por la app (X-Iris-Pendientes); nil si no vino.
	Pendientes *int32
}

type claveCliente struct{}

var reApp = regexp.MustCompile(`^([a-z][a-z0-9_-]{0,39})/([0-9A-Za-z][0-9A-Za-z.+_-]{0,39})(?: \(([a-z0-9_-]{1,40})\))?$`)

// LeerApp interpreta X-Iris-App. ok = false si no tiene el formato `app/versión (plataforma)`.
func LeerApp(valor string) (app, version, plataforma string, ok bool) {
	m := reApp.FindStringSubmatch(strings.TrimSpace(valor))
	if m == nil {
		return "", "", "", false
	}
	plataforma = m[3]
	if plataforma == "" {
		plataforma = "desconocida"
	}
	return m[1], m[2], plataforma, true
}

func clienteDePeticion(r *http.Request) ClienteHTTP {
	c := ClienteHTTP{IP: nucleo.HostDe(r.RemoteAddr), UserAgent: recortar(r.Header.Get("User-Agent"), 400), Canal: r.Header.Get(CabeceraCanal)}
	if app, version, plataforma, ok := LeerApp(r.Header.Get(CabeceraApp)); ok {
		c.AppNombre, c.Version, c.Plataforma = app, version, plataforma
		c.App = app + "/" + version + " (" + plataforma + ")"
	}
	if p := strings.TrimSpace(r.Header.Get(CabeceraPendientes)); p != "" {
		var n int32
		valido := len(p) <= 6
		for _, d := range p {
			if d < '0' || d > '9' {
				valido = false
				break
			}
			n = n*10 + (d - '0')
		}
		if valido {
			c.Pendientes = &n
		}
	}
	return c
}

func conClienteHTTP(ctx context.Context, c ClienteHTTP) context.Context {
	return context.WithValue(ctx, claveCliente{}, c)
}

// ClienteHTTPDe devuelve lo que el Middleware dejó de la petición (vacío fuera de HTTP).
func ClienteHTTPDe(ctx context.Context) ClienteHTTP {
	c, _ := ctx.Value(claveCliente{}).(ClienteHTTP)
	return c
}

// canalDeAcceso: sso si la sesión la abrió el autologin; la app de campo nativa (android, ios) o
// PWA por X-Iris-App o X-Iris-Canal: campo; si no, el panel.
func canalDeAcceso(c ClienteHTTP, origen string) string {
	switch {
	case origen == contexto.OrigenSesionSSO:
		return CanalAccesoSSO
	case c.AppNombre == "campo" && (c.Plataforma == "android" || c.Plataforma == "ios"):
		return CanalAccesoCampoNativa
	case c.AppNombre == "campo" || contexto.Canal(c.Canal) == contexto.CanalCampo:
		return CanalAccesoCampoPWA
	}
	return CanalAccesoPanel
}

var nombresPlataforma = map[string]string{"android": "Android", "ios": "iOS", "pwa": "PWA", "desconocida": "plataforma desconocida"}

// ResumirDispositivo arma una etiqueta corta y legible: la app y su versión si mandó X-Iris-App;
// si no, navegador y sistema del User-Agent («Chrome · Android»).
func ResumirDispositivo(c ClienteHTTP) string {
	if c.App != "" {
		p := nombresPlataforma[c.Plataforma]
		if p == "" {
			p = c.Plataforma
		}
		nombre := c.AppNombre
		if nombre == "campo" {
			nombre = "App de campo"
		}
		return nombre + " " + c.Version + " · " + p
	}
	return resumirUserAgent(c.UserAgent)
}

func resumirUserAgent(ua string) string {
	if ua == "" {
		return ""
	}
	navegador := ""
	switch {
	case strings.Contains(ua, "Edg/"):
		navegador = "Edge"
	case strings.Contains(ua, "OPR/"):
		navegador = "Opera"
	case strings.Contains(ua, "Firefox/"):
		navegador = "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		navegador = "Chrome"
	case strings.Contains(ua, "Safari/"):
		navegador = "Safari"
	case strings.HasPrefix(ua, "okhttp") || strings.Contains(ua, "Expo") || strings.Contains(ua, "Dalvik"):
		navegador = "App nativa"
	case strings.HasPrefix(ua, "curl/") || strings.HasPrefix(ua, "Go-http-client") || strings.HasPrefix(ua, "python"):
		navegador = "Script"
	}
	so := ""
	switch {
	case strings.Contains(ua, "Android"):
		so = "Android"
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad"):
		so = "iOS"
	case strings.Contains(ua, "Windows"):
		so = "Windows"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		so = "macOS"
	case strings.Contains(ua, "CrOS"):
		so = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		so = "Linux"
	}
	switch {
	case navegador != "" && so != "":
		return navegador + " · " + so
	case navegador != "":
		return navegador
	case so != "":
		return so
	}
	return recortar(ua, 60)
}

// registrarAcceso escribe una fila de nucleo.accesos con lo que se sabe de la petición.
func registrarAcceso(ctx context.Context, q *identidadbd.Queries, usuarioID *int64, resultado, origen string, sesionID *int64) error {
	c := ClienteHTTPDe(ctx)
	var app *string
	if c.App != "" {
		app = &c.App
	}
	return q.RegistrarAcceso(ctx, identidadbd.RegistrarAccesoParams{
		UsuarioID: usuarioID, Canal: canalDeAcceso(c, origen), Resultado: resultado, Ip: recortar(c.IP, 64),
		UserAgent: c.UserAgent, Dispositivo: ResumirDispositivo(c), App: app, SesionID: sesionID,
		RequestID: contexto.RequestIDDe(ctx),
	})
}

// accesoFallido registra el intento fallido en la transacción de auditoría del error (la de la
// acción se revierte). usuarioID nil = el correo no existe.
func accesoFallido(t *auditoria.Tx, usuarioID *int64, resultado, origen string) {
	t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
		return registrarAcceso(ctx, identidadbd.New(tx), usuarioID, resultado, origen, nil)
	})
}

// registrarUsoToken deja una fila por token y hora (no una por petición). No falla la petición.
func registrarUsoToken(ctx context.Context, q *identidadbd.Queries, usuarioID, tokenID int64) {
	c := ClienteHTTPDe(ctx)
	var app *string
	if c.App != "" {
		app = &c.App
	}
	if _, err := q.RegistrarAccesoToken(ctx, identidadbd.RegistrarAccesoTokenParams{
		UsuarioID: &usuarioID, Ip: recortar(c.IP, 64), UserAgent: c.UserAgent, Dispositivo: ResumirDispositivo(c),
		App: app, TokenID: tokenID, RequestID: contexto.RequestIDDe(ctx),
	}); err != nil {
		slog.WarnContext(ctx, "identidad: registrar uso de token en el historial de accesos", "err", err)
	}
}

// Listar ---------------------------------------------------------------------------------

// EntradaListarAccesos son los filtros del historial.
type EntradaListarAccesos struct {
	nucleo.ParamsPagina
	UsuarioID int64     `query:"usuarioId" minimum:"0" doc:"Solo los accesos de este usuario"`
	Desde     time.Time `query:"desde" doc:"Desde esta hora (incluida, RFC 3339)"`
	Hasta     time.Time `query:"hasta" doc:"Hasta esta hora (excluida, RFC 3339)"`
	Canal     string    `query:"canal" enum:"panel,campo_pwa,campo_nativa,sso,token" doc:"Canal: panel, app de campo PWA o nativa, autologin desde Argos o token"`
	Resultado string    `query:"resultado" enum:"exito,contrasena_erronea,mfa_fallido,bloqueado,inactivo"`
}

// Acceso es una fila del historial.
type Acceso struct {
	ID            int64     `json:"id"`
	UsuarioID     *int64    `json:"usuarioId" nullable:"true" doc:"null: un correo que no existe (no se guarda cuál)"`
	UsuarioNombre *string   `json:"usuarioNombre" nullable:"true"`
	UsuarioCorreo *string   `json:"usuarioCorreo" nullable:"true"`
	Canal         string    `json:"canal" enum:"panel,campo_pwa,campo_nativa,sso,token"`
	Resultado     string    `json:"resultado" enum:"exito,contrasena_erronea,mfa_fallido,bloqueado,inactivo"`
	IP            string    `json:"ip"`
	Dispositivo   string    `json:"dispositivo" doc:"Navegador y sistema, o la app y su versión"`
	App           *string   `json:"app" nullable:"true" doc:"X-Iris-App tal cual"`
	SesionID      *int64    `json:"sesionId" nullable:"true"`
	TokenID       *int64    `json:"tokenId" nullable:"true"`
	CreadoEn      time.Time `json:"creadoEn"`
}

func nuevoListarAccesos() *auditoria.Caso[EntradaListarAccesos, nucleo.Pagina[Acceso]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarAccesos, nucleo.Pagina[Acceso]]{
		Accion:       "accesos.ver",
		Permiso:      nucleo.PermAccesosVer,
		SinEmpresa:   true, // administración: no es operativa
		AuditarExito: soloLecturas[nucleo.Pagina[Acceso]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarAccesos) (nucleo.Pagina[Acceso], error) {
			var antes int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &antes); err != nil {
				return nucleo.Pagina[Acceso]{}, err
			}
			if !in.Desde.IsZero() && !in.Hasta.IsZero() && !in.Hasta.After(in.Desde) {
				return nucleo.Pagina[Acceso]{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "hasta", Message: "Debe ser posterior a desde", Rule: "rango"})
			}
			q := identidadbd.New(t)
			if in.UsuarioID != 0 {
				if _, err := q.UsuarioPorID(ctx, in.UsuarioID); err != nil {
					return nucleo.Pagina[Acceso]{}, err // sin filas → 404
				}
				if err := exigirVisible(ctx, q, in.UsuarioID, "ver los accesos de este usuario"); err != nil {
					return nucleo.Pagina[Acceso]{}, err
				}
			}
			a, err := alcanceDelActor(ctx, q)
			if err != nil {
				return nucleo.Pagina[Acceso]{}, err
			}
			p := identidadbd.ListarAccesosParams{AntesDe: antes, Canales: []string{}, Resultados: []string{},
				Todas: a.todas, Yo: contexto.ActorDe(ctx).ID, Empresas: lista(a.propias), Limite: int32(in.Limite() + 1)} //nolint:gosec // ≤ 201
			if in.UsuarioID != 0 {
				p.UsuarioID = &in.UsuarioID
			}
			if !in.Desde.IsZero() {
				p.Desde = &in.Desde
			}
			if !in.Hasta.IsZero() {
				p.Hasta = &in.Hasta
			}
			if in.Canal != "" {
				p.Canales = []string{in.Canal}
			}
			if in.Resultado != "" {
				p.Resultados = []string{in.Resultado}
			}
			filas, err := q.ListarAccesos(ctx, p)
			if err != nil {
				return nucleo.Pagina[Acceso]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), func(f identidadbd.ListarAccesosRow) Acceso {
				return Acceso{ID: f.ID, UsuarioID: f.UsuarioID, UsuarioNombre: f.UsuarioNombre, UsuarioCorreo: f.UsuarioCorreo,
					Canal: f.Canal, Resultado: f.Resultado, IP: f.Ip, Dispositivo: f.Dispositivo, App: f.App,
					SesionID: f.SesionID, TokenID: f.TokenID, CreadoEn: f.CreatedAt}
			}, func(f identidadbd.ListarAccesosRow) []any { return []any{f.ID} }), nil
		},
	})
}

// Sesiones activas -----------------------------------------------------------------------

// EntradaSesionesUsuario: el usuario cuyas sesiones se listan.
type EntradaSesionesUsuario struct {
	UsuarioID int64 `path:"id" minimum:"1"`
}

// SesionActiva es una sesión abierta (no vencida ni cerrada) de un usuario.
type SesionActiva struct {
	ID          int64     `json:"id"`
	Canal       string    `json:"canal" enum:"panel,campo_pwa,campo_nativa,sso"`
	Origen      string    `json:"origen" enum:"password,sso"`
	IP          string    `json:"ip"`
	Dispositivo string    `json:"dispositivo"`
	App         *string   `json:"app" nullable:"true"`
	CreadaEn    time.Time `json:"creadaEn"`
	UltimoUso   time.Time `json:"ultimoUso"`
	ExpiraEn    time.Time `json:"expiraEn"`
	Actual      bool      `json:"actual" doc:"Es la sesión de quien consulta"`
}

func nuevoListarSesionesUsuario() *auditoria.Caso[EntradaSesionesUsuario, nucleo.Pagina[SesionActiva]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaSesionesUsuario, nucleo.Pagina[SesionActiva]]{
		Accion:       "accesos.ver_sesiones",
		Permiso:      nucleo.PermAccesosVer,
		SinEmpresa:   true,
		AuditarExito: soloLecturas[nucleo.Pagina[SesionActiva]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSesionesUsuario) (nucleo.Pagina[SesionActiva], error) {
			q := identidadbd.New(t)
			if _, err := q.UsuarioPorID(ctx, in.UsuarioID); err != nil {
				return nucleo.Pagina[SesionActiva]{}, err // sin filas → 404
			}
			if err := exigirVisible(ctx, q, in.UsuarioID, "ver las sesiones de este usuario"); err != nil {
				return nucleo.Pagina[SesionActiva]{}, err
			}
			filas, err := q.SesionesActivasDeUsuario(ctx, in.UsuarioID)
			if err != nil {
				return nucleo.Pagina[SesionActiva]{}, err
			}
			actual := contexto.SesionDe(ctx).ID
			salida := make([]SesionActiva, 0, len(filas))
			for _, s := range filas {
				salida = append(salida, aSesionActiva(s, actual))
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

func aSesionActiva(s identidadbd.NucleoSesione, actual int64) SesionActiva {
	c := ClienteHTTP{UserAgent: s.UserAgent}
	if s.App != nil {
		if app, version, plataforma, ok := LeerApp(*s.App); ok {
			c.App, c.AppNombre, c.Version, c.Plataforma = *s.App, app, version, plataforma
		}
	}
	return SesionActiva{ID: s.ID, Canal: s.Canal, Origen: s.Origen, IP: s.Ip, Dispositivo: ResumirDispositivo(c), App: s.App,
		CreadaEn: s.CreatedAt, UltimoUso: s.UltimoUso, ExpiraEn: s.ExpiraEn, Actual: s.ID == actual}
}

// Cerrar sesiones ------------------------------------------------------------------------

// EntradaCerrarSesionUsuario cierra una sesión de un usuario.
type EntradaCerrarSesionUsuario struct {
	UsuarioID int64  `json:"-"`
	SesionID  int64  `json:"-"`
	Motivo    string `json:"motivo,omitempty" maxLength:"200" doc:"Por qué se cierra (queda en la auditoría y en el evento)"`
}

// EntradaCerrarSesionesUsuario cierra todas las sesiones de un usuario.
type EntradaCerrarSesionesUsuario struct {
	UsuarioID int64  `json:"-"`
	Motivo    string `json:"motivo,omitempty" maxLength:"200" doc:"Por qué se cierran (queda en la auditoría y en el evento)"`
}

// SesionesCerradas dice cuántas sesiones se cerraron.
type SesionesCerradas struct {
	UsuarioID int64   `json:"usuarioId"`
	Cerradas  int     `json:"cerradas"`
	Sesiones  []int64 `json:"sesiones" doc:"Ids de las sesiones cerradas"`
}

// exigirCerrarSesiones: el usuario debe estar en el alcance (404 si no) y, salvo el propio, quien
// actúa debe superarlo (cerrar las sesiones de un admin global no lo hace un admin de zona).
func exigirCerrarSesiones(ctx context.Context, q *identidadbd.Queries, objetivo int64) error {
	if _, err := q.UsuarioPorID(ctx, objetivo); err != nil {
		return err
	}
	if err := exigirVisible(ctx, q, objetivo, "cerrar las sesiones de este usuario"); err != nil {
		return err
	}
	if a := contexto.ActorDe(ctx); a.Tipo == contexto.ActorUsuario && a.ID == objetivo {
		return nil
	}
	return exigirSuperior(ctx, q, contexto.PermisosDe(ctx), objetivo, "cerrar las sesiones de este usuario")
}

func nuevoCerrarSesionUsuario() *auditoria.Caso[EntradaCerrarSesionUsuario, SesionesCerradas] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCerrarSesionUsuario, SesionesCerradas]{
		Accion:     "usuarios.cerrar_sesion",
		Permiso:    nucleo.PermUsuariosCerrarSesiones,
		SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCerrarSesionUsuario) (SesionesCerradas, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			if err := exigirCerrarSesiones(ctx, q, in.UsuarioID); err != nil {
				return SesionesCerradas{}, err
			}
			n, err := q.RevocarSesionDeUsuario(ctx, identidadbd.RevocarSesionDeUsuarioParams{ID: in.SesionID, UsuarioID: in.UsuarioID})
			if err != nil {
				return SesionesCerradas{}, err
			}
			if n == 0 {
				return SesionesCerradas{}, nucleo.NoEncontrado("La sesión no existe, ya está cerrada o venció")
			}
			salida := SesionesCerradas{UsuarioID: in.UsuarioID, Cerradas: 1, Sesiones: []int64{in.SesionID}}
			return salida, emitirSesionesCerradas(t, salida, in.Motivo, false)
		},
	})
}

func nuevoCerrarSesionesUsuario() *auditoria.Caso[EntradaCerrarSesionesUsuario, SesionesCerradas] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCerrarSesionesUsuario, SesionesCerradas]{
		Accion:     "usuarios.cerrar_sesiones",
		Permiso:    nucleo.PermUsuariosCerrarSesiones,
		SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCerrarSesionesUsuario) (SesionesCerradas, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			if err := exigirCerrarSesiones(ctx, q, in.UsuarioID); err != nil {
				return SesionesCerradas{}, err
			}
			// Las del propio actor: todas menos la que está usando.
			var salvo int64
			if a := contexto.ActorDe(ctx); a.Tipo == contexto.ActorUsuario && a.ID == in.UsuarioID {
				salvo = contexto.SesionDe(ctx).ID
			}
			ids, err := q.RevocarSesionesDeUsuarioSalvo(ctx, identidadbd.RevocarSesionesDeUsuarioSalvoParams{UsuarioID: in.UsuarioID, Salvo: salvo})
			if err != nil {
				return SesionesCerradas{}, err
			}
			if ids == nil {
				ids = []int64{}
			}
			salida := SesionesCerradas{UsuarioID: in.UsuarioID, Cerradas: len(ids), Sesiones: ids}
			if len(ids) == 0 {
				return salida, nil // nada que cerrar: sin evento
			}
			return salida, emitirSesionesCerradas(t, salida, in.Motivo, true)
		},
	})
}

func emitirSesionesCerradas(t *auditoria.Tx, s SesionesCerradas, motivo string, todas bool) error {
	var m *string
	if motivo = strings.TrimSpace(motivo); motivo != "" {
		m = &motivo
	}
	return t.Emitir("usuario.sesiones_cerradas", map[string]any{
		"usuario_id": s.UsuarioID, "sesiones": s.Sesiones, "todas": todas, "motivo": m,
	})
}

// Purga ----------------------------------------------------------------------------------

// RetencionAccesosPorDefecto: el historial de accesos se guarda 180 días (IRIS_ACCESOS_RETENCION_DIAS).
const RetencionAccesosPorDefecto = 180 * 24 * time.Hour

// retencionMinimaAccesos: nunca se purga menos de 7 días (un valor mal puesto no borra todo).
const retencionMinimaAccesos = 7 * 24 * time.Hour

// PurgarAccesos borra, por lotes, los accesos más viejos que retencion. Devuelve cuántos borró.
func PurgarAccesos(ctx context.Context, bd nucleo.BD, retencion time.Duration, lote int32) (int64, error) {
	if retencion <= 0 {
		retencion = RetencionAccesosPorDefecto
	}
	segundos := int64(max(retencion, retencionMinimaAccesos) / time.Second)
	q := identidadbd.New(bd)
	return porLotes(ctx, lote, func(lote int32) (int64, error) {
		return q.PurgarAccesos(ctx, identidadbd.PurgarAccesosParams{AntiguedadSeg: segundos, Lote: lote})
	})
}

// ArgsPurgaAccesos es el job periódico de la purga del historial de accesos.
type ArgsPurgaAccesos struct{}

// Kind implementa river.JobArgs.
func (ArgsPurgaAccesos) Kind() string { return "nucleo.accesos_purga" }

// InsertOpts: único por media hora.
func (ArgsPurgaAccesos) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Minute}}
}

type trabajadorPurgaAccesos struct {
	river.WorkerDefaults[ArgsPurgaAccesos]
	bd        nucleo.BD
	retencion time.Duration
}

func (w *trabajadorPurgaAccesos) Work(ctx context.Context, _ *river.Job[ArgsPurgaAccesos]) error {
	n, err := PurgarAccesos(ctx, w.bd, w.retencion, LotePurga)
	if n > 0 {
		slog.InfoContext(ctx, "identidad: historial de accesos vencido purgado", "filas", n)
	}
	return err
}

// RegistrarPurgaAccesos agrega el job al worker (retencion 0 = RetencionAccesosPorDefecto).
func RegistrarPurgaAccesos(workers *river.Workers, bd nucleo.BD, retencion time.Duration) {
	river.AddWorker(workers, &trabajadorPurgaAccesos{bd: bd, retencion: retencion})
}

// TrabajoPeriodicoPurgaAccesos purga cada intervalo (1 h por defecto) y al arrancar.
func TrabajoPeriodicoPurgaAccesos(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Hour
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsPurgaAccesos{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
