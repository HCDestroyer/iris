package auditoria

import "github.com/HCDestroyer/iris/apps/api/internal/nucleo"

// accionesSinPermisoConReautenticacion: acciones sensibles que no piden un permiso (las hace el
// propio usuario): su MFA y sus vínculos de autologin.
var accionesSinPermisoConReautenticacion = map[string]bool{
	"mfa.configurar": true, "mfa.activar": true, "sso.vincular": true, "usuarios.borrar_vinculo_sso": true,
}

// RequiereReautenticacion indica si la acción exige re-autenticarse en una sesión abierta por el
// autologin desde Argos (Argos o su token comprometidos no deben bastar para administrar Iris):
// toda acción con un permiso de nucleo.PermisosSensibles y las de accionesSinPermisoConReautenticacion.
func RequiereReautenticacion(accion string, permiso nucleo.Permiso) bool {
	return nucleo.EsSensible(permiso) || accionesSinPermisoConReautenticacion[accion]
}
