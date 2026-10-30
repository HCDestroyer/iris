package auditoria

import "strings"

// accionesConReautenticacion: en una sesión abierta por el autologin desde Argos (Argos o su
// token comprometidos no deben bastar para tomar la cuenta), estas acciones exigen confirmar la
// contraseña antes (identidad: POST /api/v1/sesion/reautenticar): tokens, conectores, gestión de
// usuarios, roles y vínculos de autologin, y el MFA propio.
var accionesConReautenticacion = map[string]bool{
	"tokens.crear": true, "tokens.revocar": true,
	"integraciones.conectar": true, "integraciones.activar": true, "integraciones.desactivar": true,
	"integraciones.generar_enlace": true, "integraciones.configurar_url": true,
	"usuarios.crear": true, "usuarios.editar": true, "usuarios.desactivar": true, "usuarios.reiniciar_mfa": true,
	"usuarios.asignar_empresa": true, "usuarios.quitar_empresa": true,
	"usuarios.vincular_sso": true, "usuarios.borrar_vinculo_sso": true, "sso.vincular": true,
	"mfa.configurar": true, "mfa.activar": true,
}

// RequiereReautenticacion indica si la acción exige re-autenticarse en una sesión sso.
func RequiereReautenticacion(accion string) bool {
	if accionesConReautenticacion[accion] {
		return true
	}
	return strings.HasPrefix(accion, "roles.") && accion != "roles.ver"
}
