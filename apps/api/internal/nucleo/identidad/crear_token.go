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
	UsuarioID int64            `json:"usuarioId,omitempty" minimum:"1" doc:"Dueño del token (por defecto, quien lo crea). Para Argos u Horus, su usuario de integración"`
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
		Accion:  "tokens.crear",
		Permiso: nucleo.PermTokensCrear,
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
			if !u.Activo {
				return TokenCreado{}, nucleo.ReglaNegocio("usuario_inactivo", "El usuario está desactivado")
			}
			// Nadie da más de lo que tiene: lo que el token podrá hacer (permisos del dueño ∩
			// límite) debe estar en los permisos sin límite de quien lo crea.
			if !actor.EsUsuario() || actor.ID != dueno {
				efectivos, err := cargarPermisos(ctx, q, dueno, limite)
				if err != nil {
					return TokenCreado{}, err
				}
				quien := contexto.PermisosDe(ctx)
				for _, p := range efectivos.Lista() {
					if !quien.TieneGlobal(p) {
						return TokenCreado{}, nucleo.Prohibido("El token tendría el permiso " + p + ", que no tienes sin límite de alcance; limita sus permisos")
					}
				}
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
				Permisos: limite, ExpiraEn: in.ExpiraEn, CreadoPor: creadoPor,
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
