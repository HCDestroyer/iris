package identidad

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaCrearToken es la entrada de tokens.crear.
type EntradaCrearToken struct {
	UsuarioID int64            `json:"usuarioId,omitempty" minimum:"1" doc:"Dueño del token (por defecto, quien lo crea). Otro usuario solo si es de tipo integración (Argos, Horus); para una persona, 422 token_para_persona"`
	Nombre    string           `json:"nombre" minLength:"1" maxLength:"120"`
	Permisos  []nucleo.Permiso `json:"permisos,omitempty" maxItems:"200" doc:"Límite de permisos; sin él, los del usuario"`
	ExpiraEn  *time.Time       `json:"expiraEn,omitempty" doc:"Vencimiento (opcional)"`
}

// TokenCreado lleva el valor del token: solo se ve esta vez (se guarda su SHA-256).
type TokenCreado struct {
	Valor string   `json:"valor" doc:"iris_pat_…; guárdalo ahora, no se vuelve a mostrar"`
	Token TokenAPI `json:"token"`
}

func nuevoCrearToken() *auditoria.Caso[EntradaCrearToken, TokenCreado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearToken, TokenCreado]{
		Accion:     "tokens.crear",
		Permiso:    nucleo.PermTokensCrear,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearToken) (TokenCreado, error) {
			actor := contexto.ActorDe(ctx)
			dueno := in.UsuarioID
			if dueno == 0 {
				var err error
				if dueno, err = usuarioDelActor(actor); err != nil {
					return TokenCreado{}, err
				}
			}
			nombre := strings.TrimSpace(in.Nombre)
			if nombre == "" {
				return TokenCreado{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "nombre", Message: "Nombre obligatorio", Rule: "requerido"})
			}
			if in.ExpiraEn != nil && !in.ExpiraEn.After(time.Now()) {
				return TokenCreado{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "expiraEn", Message: "Debe ser una fecha futura", Rule: "futuro"})
			}
			var limite []string
			if in.Permisos != nil {
				limite = make([]string, 0, len(in.Permisos))
				for i, p := range in.Permisos {
					if !nucleo.EsPermiso(string(p)) {
						return TokenCreado{}, nucleo.Validacion(nucleo.ErrorCampo{Field: fmt.Sprintf("permisos[%d]", i), Message: "Permiso desconocido", Rule: "enum"})
					}
					if !slices.Contains(limite, string(p)) {
						limite = append(limite, string(p))
					}
				}
				slices.Sort(limite)
			}

			q := identidadbd.New(t)
			u, err := q.UsuarioPorID(ctx, dueno)
			if err != nil {
				return TokenCreado{}, err
			}
			// Para otro usuario fuera de tu alcance de empresas: 404, como si no existiera (no se
			// revela si es persona, integración o está inactivo).
			if !actor.EsUsuario() || actor.ID != u.ID {
				if err := exigirVisible(ctx, q, u.ID, "crear tokens de este usuario"); err != nil {
					return TokenCreado{}, err
				}
			}
			if !u.Activo {
				return TokenCreado{}, nucleo.ReglaNegocio("usuario_inactivo", "El usuario está desactivado")
			}
			if err := puedeEmitir(ctx, q, actor, u, limite); err != nil {
				return TokenCreado{}, err
			}
			expira := in.ExpiraEn
			var via *string
			if conector, err := esDeConector(ctx, q, u.ID); err != nil {
				return TokenCreado{}, err
			} else if conector && !esTokenDeCanje(ctx) && (!actor.EsUsuario() || actor.ID != u.ID) {
				// Vía de recuperación del conector (contrato con Argos 0.8.0 §1.5, S5): queda marcada, vence
				// (30 días por defecto, 90 como máximo) y se revoca al confirmarse un canje nuevo.
				if expira == nil {
					v := time.Now().Add(VigenciaRecuperacion)
					expira = &v
				} else if expira.After(time.Now().Add(VigenciaMaximaRecuperacion)) {
					return TokenCreado{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "expiraEn",
						Message: "El token de recuperación de un conector vence como mucho en 90 días", Rule: "maximo"})
				}
				r := ViaRecuperacion
				via = &r
				t.Anotar(map[string]any{"via": ViaRecuperacion})
			}

			valor, err := NuevoTokenAPI()
			if err != nil {
				return TokenCreado{}, err
			}
			var creadoPor *int64
			if actor.EsUsuario() {
				creadoPor = &actor.ID
			}
			fila, err := q.CrearToken(ctx, identidadbd.CrearTokenParams{
				UsuarioID: dueno, Nombre: nombre, Prefijo: prefijoVisible(valor), Hash: Hash256(valor),
				Permisos: limite, ExpiraEn: expira, CreadoPor: creadoPor, Via: via,
			})
			if err != nil {
				return TokenCreado{}, err
			}
			token := aTokenAPI(fila)
			t.RegistrarCambios("token", fila.ID, nil, token)
			if err := t.Emitir("token.creado", map[string]any{"token_id": fila.ID, "usuario_id": dueno}); err != nil {
				return TokenCreado{}, err
			}
			return TokenCreado{Valor: valor, Token: token}, nil
		},
	})
}

// puedeEmitir aplica «nadie da más de lo que tiene» a un token nuevo del usuario dueno con el
// límite dado:
//
//   - para sí mismo con sesión (actor usuario): sin más reglas; el token nunca supera a su dueño;
//   - para sí mismo desde un token o una integración: lo que podrá hacer el token nuevo (permisos
//     del dueño ∩ límite) debe estar en los permisos del token que lo crea, con el mismo alcance;
//     y si ese token tiene límite, el nuevo también (sin límite crecería con los roles del dueño);
//   - para otro usuario: solo si es de tipo integración (un token de una persona actuaría a su
//     nombre) y quien lo crea tiene sin límite de alcance cada permiso actual de ese usuario.
func puedeEmitir(ctx context.Context, q *identidadbd.Queries, actor contexto.Actor, dueno identidadbd.NucleoUsuario, limite []string) error {
	quien := contexto.PermisosDe(ctx)
	propio := actor.EsUsuario() && actor.ID == dueno.ID
	switch {
	case propio && actor.Tipo == contexto.ActorUsuario:
		return nil
	case propio:
		if actor.TokenID != 0 {
			padre, err := q.TokenPorID(ctx, actor.TokenID)
			if err != nil {
				return err
			}
			if padre.Permisos != nil && limite == nil {
				return nucleo.Prohibido("Un token creado con un token limitado debe llevar permisos (subconjunto de los del token)")
			}
		}
		efectivos, err := cargarPermisos(ctx, q, dueno.ID, limite)
		if err != nil {
			return err
		}
		if falta, ok := quien.Cubre(efectivos); !ok {
			return nucleo.Prohibido("El token tendría el permiso " + falta + ", que el token con el que lo creas no tiene; limita sus permisos")
		}
		return nil
	default:
		if dueno.Tipo != "integracion" {
			return nucleo.ReglaNegocio("token_para_persona", "Solo se crean tokens para otro usuario si es de tipo integración; cada persona crea los suyos")
		}
		// Contrato con Argos 0.8.0 §1.5 (S5) y §10.1 punto 1: el token de un usuario de integración
		// creado a mano (para pegarlo en Argos u Horus) es la vía de RECUPERACIÓN del conector: solo un
		// administrador (integraciones.conectar sin límite de alcance), auditada y marcada así. Lo normal
		// es la conexión de un paso, donde el token nunca se muestra.
		conector, err := esDeConector(ctx, q, dueno.ID)
		if err != nil {
			return err
		}
		if conector && (!quien.TieneGlobal(string(nucleo.PermIntegracionesConectar)) || !quien.TieneGlobal(string(nucleo.PermEmpresasTodas))) {
			return nucleo.Prohibido("El token de un usuario de integración es la vía de recuperación del conector: solo lo crea un administrador (integraciones.conectar y empresas.todas, sin límite de alcance)")
		}
		return exigirSuperior(ctx, q, quien, dueno.ID, "crear tokens de este usuario")
	}
}

// esDeConector: el usuario de integración es el de un conector (rol integracion_argos o
// integracion_horus). Sus tokens a mano son la vía de recuperación (contrato con Argos 0.8.0 §1.5).
func esDeConector(ctx context.Context, q *identidadbd.Queries, usuarioID int64) (bool, error) {
	roles, err := q.RolesDeUsuarios(ctx, []int64{usuarioID})
	if err != nil {
		return false, err
	}
	for _, r := range roles {
		if r.Codigo == "integracion_argos" || r.Codigo == "integracion_horus" {
			return true, nil
		}
	}
	return false, nil
}

// Vía de recuperación del conector (contrato con Argos 0.8.0 §1.5, S5).
const (
	ViaRecuperacion            = "recuperacion"
	VigenciaRecuperacion       = 30 * 24 * time.Hour
	VigenciaMaximaRecuperacion = 90 * 24 * time.Hour
)
