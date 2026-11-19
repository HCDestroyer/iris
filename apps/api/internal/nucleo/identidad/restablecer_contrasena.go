package identidad

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaRestablecerContrasena es la entrada de usuarios.restablecer_contrasena: un administrador
// le pone a otra persona una contraseña temporal (la olvidó o hay que cortar un acceso), cierra
// todas sus sesiones y se la entrega fuera de banda. No toca el MFA; los tokens, solo si se pide.
type EntradaRestablecerContrasena struct {
	ID                 int64  `json:"-"`
	Version            int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo             string `json:"motivo" minLength:"1" maxLength:"500" doc:"Por qué se restablece (queda en la auditoría y en el evento)"`
	ContrasenaTemporal string `json:"contrasenaTemporal" maxLength:"200" auditoria:"sensible" doc:"Contraseña temporal (mínimo 12 caracteres, distinta de la actual) que el administrador entrega al usuario fuera de banda. Al entrar con ella, el usuario debe cambiarla antes de cualquier otra cosa. Nunca se audita ni va en eventos"`
	RevocarTokens      *bool  `json:"revocarTokens,omitempty" doc:"Revoca también sus tokens iris_pat_ (false por omisión), menos los que usa un conector activo"`
}

// ContrasenaRestablecida es la respuesta de usuarios.restablecer_contrasena: el usuario
// actualizado más los contadores del evento usuario.contrasena_restablecida.
type ContrasenaRestablecida struct {
	Usuario
	SesionesCerradas  int64 `json:"sesionesCerradas" minimum:"0" doc:"Sesiones del usuario que se cerraron"`
	TokensRevocados   int   `json:"tokensRevocados" minimum:"0" doc:"Tokens iris_pat_ revocados (0 salvo revocarTokens: true)"`
	TokensConservados int   `json:"tokensConservados" minimum:"0" doc:"Tokens que no se revocaron porque los usa un conector activo de Argos u Horus"`
}

func nuevoRestablecerContrasena() *auditoria.Caso[EntradaRestablecerContrasena, ContrasenaRestablecida] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRestablecerContrasena, ContrasenaRestablecida]{
		Accion:     "usuarios.restablecer_contrasena",
		Permiso:    nucleo.PermUsuariosRestablecerContrasena,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRestablecerContrasena) (ContrasenaRestablecida, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.ID)
			// La propia se cambia con la actual («Cambiar mi contraseña»), no por esta vía.
			if actor := contexto.ActorDe(ctx); actor.EsUsuario() && actor.ID == in.ID {
				return ContrasenaRestablecida{}, nucleo.ReglaNegocio("restablecer_propia", "No puedes restablecer tu propia contraseña; usa «Cambiar mi contraseña»")
			}
			antes, err := adminSobreOtro(ctx, q, in.ID, in.Version, "restablecer la contraseña de este usuario")
			if err != nil {
				return ContrasenaRestablecida{}, err
			}
			if antes.Tipo == "integracion" {
				return ContrasenaRestablecida{}, nucleo.ReglaNegocio("usuario_integracion", "Las integraciones entran solo con token; no tienen contraseña")
			}
			if !antes.Activo {
				return ContrasenaRestablecida{}, nucleo.ReglaNegocio("usuario_inactivo", "El usuario está desactivado")
			}
			hash, err := hashTemporal(in.ContrasenaTemporal, antes.ContrasenaHash)
			if err != nil {
				return ContrasenaRestablecida{}, err
			}
			despues, err := q.RestablecerContrasena(ctx, identidadbd.RestablecerContrasenaParams{ID: in.ID, Version: in.Version, ContrasenaHash: &hash})
			if err != nil {
				return ContrasenaRestablecida{}, nucleo.VerificarVersion(err, "usuario", in.ID)
			}
			cerradas, err := q.RevocarSesionesDeUsuario(ctx, in.ID)
			if err != nil {
				return ContrasenaRestablecida{}, err
			}
			revocados, conservados := []int64{}, int32(0)
			if in.RevocarTokens != nil && *in.RevocarTokens {
				if revocados, conservados, err = revocarTokensDe(ctx, t, q, in.ID, "contrasena_restablecida"); err != nil {
					return ContrasenaRestablecida{}, err
				}
			}
			roles, err := rolesDe(ctx, q, in.ID)
			if err != nil {
				return ContrasenaRestablecida{}, err
			}
			t.RegistrarCambios("usuario", in.ID, auditable(antes, roles[in.ID]), auditable(despues, roles[in.ID]))
			t.Anotar(map[string]any{"sesiones_cerradas": cerradas, "tokens_revocados": revocados, "tokens_conservados": conservados})
			if err := t.Emitir("usuario.contrasena_restablecida", map[string]any{
				"usuario_id": in.ID, "motivo": in.Motivo, "sesiones_cerradas": cerradas, "tokens_revocados": len(revocados), "tokens_conservados": conservados,
			}); err != nil {
				return ContrasenaRestablecida{}, err
			}
			empresas, err := empresasDe(ctx, q, in.ID)
			if err != nil {
				return ContrasenaRestablecida{}, err
			}
			return ContrasenaRestablecida{
				Usuario:          aUsuario(despues, roles[in.ID], empresas[in.ID]),
				SesionesCerradas: cerradas,
				TokensRevocados:  len(revocados), TokensConservados: int(conservados),
			}, nil
		},
	})
}

// adminSobreOtro bloquea al usuario objetivo y comprueba lo que comparten las acciones de un
// administrador sobre otro (reiniciar MFA, restablecer contraseña): fuera del alcance de
// empresas, 404 como si no existiera (ni su versión se revela); luego la versión (409) y que el
// actor tenga sin límite los permisos del objetivo (403).
func adminSobreOtro(ctx context.Context, q *identidadbd.Queries, id int64, version int32, que string) (identidadbd.NucleoUsuario, error) {
	antes, err := q.UsuarioPorIDParaActualizar(ctx, id)
	if err != nil {
		return antes, err
	}
	if err := exigirVisible(ctx, q, id, que); err != nil {
		return antes, err
	}
	if err := nucleo.ExigirVersion(version, antes.Version, "usuario", id); err != nil {
		return antes, err
	}
	if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), id, que); err != nil {
		return antes, err
	}
	return antes, nil
}

// revocarTokensDe revoca los tokens iris_pat_ del usuario menos los que usa un conector activo
// (revocarlos cortaría la integración) y emite token.revocado por cada uno. Devuelve los
// revocados y cuántos se conservaron.
func revocarTokensDe(ctx context.Context, t *auditoria.Tx, q *identidadbd.Queries, id int64, motivo string) ([]int64, int32, error) {
	if err := q.BloquearTokensDeUsuario(ctx, id); err != nil {
		return nil, 0, err
	}
	revocados, err := q.RevocarTokensDeUsuario(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	conservados, err := q.TokensDeUsuarioEnUsoPorConector(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	for _, tok := range revocados {
		if err := t.Emitir("token.revocado", map[string]any{"token_id": tok, "usuario_id": id, "motivo": motivo}); err != nil {
			return nil, 0, err
		}
	}
	return revocados, conservados, nil
}
