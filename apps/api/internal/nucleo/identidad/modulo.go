// Package identidad: usuarios (argon2id), sesiones del panel (cookie + CSRF por doble envío),
// TOTP opcional, roles y permisos con alcance, y tokens de API iris_pat_.
//
// Cada acción es un caso de uso de internal/auditoria; los handlers (rutas.go) solo validan y
// llaman. La resolución de credenciales de cada petición vive en middleware.go.
package identidad

import (
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Config del módulo.
type Config struct {
	// DuracionSesion es la vida absoluta de una sesión (12 h por defecto).
	DuracionSesion time.Duration
	// CookieSegura pone Secure en las cookies (true salvo desarrollo sin HTTPS).
	CookieSegura bool
	// Cifrador para el secreto TOTP. nil = MFA no disponible (configurar responde 422).
	Cifrador *nucleo.Cifrador
	// EmisorTOTP aparece en la app autenticadora ("Iris" por defecto).
	EmisorTOTP string
	// LoginMaxFallos: inicios de sesión fallidos seguidos (por correo, dentro de LoginVentana)
	// que bloquean ese correo durante LoginBloqueo. 10, 15 min y 15 min por defecto.
	LoginMaxFallos int32
	LoginVentana   time.Duration
	LoginBloqueo   time.Duration
	// MFAMaxFallos: códigos TOTP inválidos que revocan la sesión pendiente (5 por defecto).
	MFAMaxFallos int32
}

// Modulo reúne los casos de uso de identidad.
type Modulo struct {
	cfg Config

	CrearUsuario      *auditoria.Caso[EntradaCrearUsuario, Usuario]
	EditarUsuario     *auditoria.Caso[EntradaEditarUsuario, Usuario]
	DesactivarUsuario *auditoria.Caso[EntradaDesactivarUsuario, Usuario]
	ListarUsuarios    *auditoria.Caso[EntradaListarUsuarios, nucleo.Pagina[Usuario]]

	IniciarSesion *auditoria.Caso[EntradaIniciarSesion, SesionIniciada]
	VerificarMFA  *auditoria.Caso[EntradaVerificarMFA, SesionIniciada]
	CerrarSesion  *auditoria.Caso[struct{}, struct{}]
	ObtenerYo     *auditoria.Caso[struct{}, Yo]
	ConfigurarMFA *auditoria.Caso[struct{}, MFAConfigurado]
	ActivarMFA    *auditoria.Caso[EntradaActivarMFA, Usuario]

	ListarRoles    *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Rol]]
	ListarPermisos *auditoria.Caso[struct{}, nucleo.Pagina[PermisoInfo]]

	ListarEmpresas        *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Empresa]]
	CrearEmpresa          *auditoria.Caso[EntradaCrearEmpresa, Empresa]
	EditarEmpresa         *auditoria.Caso[EntradaEditarEmpresa, Empresa]
	AsignarEmpresaUsuario *auditoria.Caso[EntradaEmpresaUsuario, Usuario]
	QuitarEmpresaUsuario  *auditoria.Caso[EntradaEmpresaUsuario, Usuario]

	CrearToken   *auditoria.Caso[EntradaCrearToken, TokenCreado]
	RevocarToken *auditoria.Caso[EntradaRevocarToken, TokenAPI]
	ListarTokens *auditoria.Caso[EntradaListarTokens, nucleo.Pagina[TokenAPI]]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.DuracionSesion <= 0 {
		cfg.DuracionSesion = 12 * time.Hour
	}
	if cfg.EmisorTOTP == "" {
		cfg.EmisorTOTP = "Iris"
	}
	if cfg.LoginMaxFallos <= 0 {
		cfg.LoginMaxFallos = 10
	}
	if cfg.LoginVentana <= 0 {
		cfg.LoginVentana = 15 * time.Minute
	}
	if cfg.LoginBloqueo <= 0 {
		cfg.LoginBloqueo = 15 * time.Minute
	}
	if cfg.MFAMaxFallos <= 0 {
		cfg.MFAMaxFallos = 5
	}
	m := &Modulo{cfg: cfg}
	m.CrearUsuario = nuevoCrearUsuario()
	m.EditarUsuario = nuevoEditarUsuario()
	m.DesactivarUsuario = nuevoDesactivarUsuario()
	m.ListarUsuarios = nuevoListarUsuarios()
	m.IniciarSesion = nuevoIniciarSesion(cfg)
	m.VerificarMFA = nuevoVerificarMFA(cfg)
	m.CerrarSesion = nuevoCerrarSesion()
	m.ObtenerYo = nuevoObtenerYo()
	m.ConfigurarMFA = nuevoConfigurarMFA(cfg)
	m.ActivarMFA = nuevoActivarMFA(cfg)
	m.ListarRoles = nuevoListarRoles()
	m.ListarPermisos = nuevoListarPermisos()
	m.ListarEmpresas = nuevoListarEmpresas()
	m.CrearEmpresa = nuevoCrearEmpresa()
	m.EditarEmpresa = nuevoEditarEmpresa()
	m.AsignarEmpresaUsuario = nuevoCambiarEmpresaUsuario(true)
	m.QuitarEmpresaUsuario = nuevoCambiarEmpresaUsuario(false)
	m.CrearToken = nuevoCrearToken()
	m.RevocarToken = nuevoRevocarToken()
	m.ListarTokens = nuevoListarTokens()
	return m
}

// Config devuelve la configuración (para el middleware y las cookies).
func (m *Modulo) Config() Config { return m.cfg }

// soloLecturas: las consultas no dejan fila de auditoría al terminar bien (sí al fallar).
func soloLecturas[Out any](Out) bool { return false }

// usuarioDelActor devuelve el id de usuario del actor o 401.
func usuarioDelActor(a contexto.Actor) (int64, error) {
	if !a.EsUsuario() {
		return 0, nucleo.NoAutenticado("Esta acción requiere un usuario")
	}
	return a.ID, nil
}
