package auditoria

import "github.com/HCDestroyer/iris/apps/api/internal/nucleo"

// accionesSinPermisoConReautenticacion: acciones sensibles que no piden un permiso sensible (las
// hace el propio usuario): su MFA, sus vínculos de autologin y pedir su enlace hacia Argos
// (integraciones.abrir_argos: con él se crea el vínculo que autoriza aprobar pagos; una sesión
// abierta desde Argos no debe bastar para encadenar otra hacia Argos).
var accionesSinPermisoConReautenticacion = map[string]bool{
	"mfa.configurar": true, "mfa.activar": true, "sso.vincular": true, "usuarios.borrar_vinculo_sso": true,
	"integraciones.abrir_argos": true,
}

// RequiereReautenticacion indica si la acción exige re-autenticarse en una sesión abierta por el
// autologin desde Argos (Argos o su token comprometidos no deben bastar para administrar Iris):
// toda acción con un permiso de nucleo.PermisosSensibles y las de accionesSinPermisoConReautenticacion.
func RequiereReautenticacion(accion string, permiso nucleo.Permiso) bool {
	return nucleo.EsSensible(permiso) || accionesSinPermisoConReautenticacion[accion]
}
