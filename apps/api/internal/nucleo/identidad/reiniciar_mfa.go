package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaReiniciarMFA es la entrada de usuarios.reiniciar_mfa: un administrador borra el TOTP
// de otro usuario (perdió el teléfono, o tras rotar IRIS_CLAVE_CIFRADO sin la anterior) y cierra
// sus sesiones. El usuario vuelve a entrar con su contraseña y configura el MFA de nuevo.
type EntradaReiniciarMFA struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  string `json:"motivo" minLength:"1" maxLength:"500" doc:"Por qué se reinicia (queda en la auditoría y en el evento)"`
}

func nuevoReiniciarMFA() *auditoria.Caso[EntradaReiniciarMFA, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReiniciarMFA, Usuario]{
		Accion:     "usuarios.reiniciar_mfa",
		Permiso:    nucleo.PermUsuariosReiniciarMFA,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReiniciarMFA) (Usuario, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.ID)
			// El propio MFA no se reinicia por esta vía: quien lo pide no podría probar que es él
			// (con sesión o token propios se saltaría su segundo factor). Lo hace otro admin.
			if actor := contexto.ActorDe(ctx); actor.EsUsuario() && actor.ID == in.ID {
				return Usuario{}, nucleo.ReglaNegocio("reinicio_mfa_propio", "No puedes reiniciar tu propio MFA; pídeselo a otro administrador")
			}
			antes, err := q.UsuarioPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "usuario", in.ID); err != nil {
				return Usuario{}, err
			}
			// Mismo alcance que editar o desactivar: empresas del objetivo y sus permisos sin límite.
			if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.ID, "reiniciar el MFA de este usuario"); err != nil {
				return Usuario{}, err
			}
			if antes.TotpSecreto == nil && !antes.TotpActivo {
				return Usuario{}, nucleo.ReglaNegocio("mfa_no_configurado", "El usuario no tiene MFA configurado")
			}
			despues, err := q.ReiniciarMFA(ctx, identidadbd.ReiniciarMFAParams{ID: in.ID, Version: in.Version})
			if err != nil {
				return Usuario{}, nucleo.VerificarVersion(err, "usuario", in.ID)
			}
			cerradas, err := q.RevocarSesionesDeUsuario(ctx, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			roles, err := rolesDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			t.RegistrarCambios("usuario", in.ID, auditable(antes, roles[in.ID]), auditable(despues, roles[in.ID]))
			t.Anotar(map[string]any{"mfa_activo_antes": antes.TotpActivo, "sesiones_cerradas": cerradas})
			if err := t.Emitir("usuario.mfa_reiniciado", map[string]any{
				"usuario_id": in.ID, "motivo": in.Motivo, "sesiones_cerradas": cerradas,
			}); err != nil {
				return Usuario{}, err
			}
			empresas, err := empresasDe(ctx, q, in.ID)
			if err != nil {
				return Usuario{}, err
			}
			return aUsuario(despues, roles[in.ID], empresas[in.ID]), nil
		},
	})
}
