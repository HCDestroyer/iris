package identidad

import (
	"context"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// EntradaCrearUsuario es la entrada de usuarios.crear.
type EntradaCrearUsuario struct {
	Nombre     string          `json:"nombre" minLength:"1" maxLength:"120"`
	Correo     string          `json:"correo" format:"email" maxLength:"254"`
	Contrasena string          `json:"contrasena,omitempty" maxLength:"200" doc:"Obligatoria para personas (mínimo 12 caracteres); las integraciones no tienen contraseña"`
	Tipo       string          `json:"tipo,omitempty" enum:"persona,integracion" default:"persona"`
	Roles      []AsignacionRol `json:"roles,omitempty" maxItems:"50"`
	Empresas   []int64         `json:"empresas,omitempty" maxItems:"50" doc:"Empresas en que operará (solo empresas en que operas tú). Obligatoria (al menos una) si no tienes empresas.todas y hay empresas registradas: si no, nadie más que un admin global podría administrarlo"`
}

func nuevoCrearUsuario() *auditoria.Caso[EntradaCrearUsuario, Usuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearUsuario, Usuario]{
		Accion:     "usuarios.crear",
		Permiso:    nucleo.PermUsuariosCrear,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearUsuario) (Usuario, error) {
			q := identidadbd.New(t)
			nombre, correo, err := normalizarNombreCorreo(in.Nombre, in.Correo)
			if err != nil {
				return Usuario{}, err
			}
			tipo := in.Tipo
			if tipo == "" {
				tipo = "persona"
			}
			hash, err := hashSegunTipo(tipo, in.Contrasena)
			if err != nil {
				return Usuario{}, err
			}
			roles, err := resolverRoles(ctx, q, contexto.PermisosDe(ctx), "roles", in.Roles)
			if err != nil {
				return Usuario{}, err
			}

			empresas, err := empresasNuevoUsuario(ctx, q, in.Empresas)
			if err != nil {
				return Usuario{}, err
			}

			if _, err := q.UsuarioPorCorreo(ctx, correo); err == nil {
				return Usuario{}, nucleo.Conflicto("correo_en_uso", "Ya existe un usuario con el correo "+correo)
			}
			u, err := q.CrearUsuario(ctx, identidadbd.CrearUsuarioParams{Nombre: nombre, Correo: correo, Tipo: tipo, ContrasenaHash: hash})
			if err != nil {
				return Usuario{}, err
			}
			if err := asignarRoles(ctx, q, u.ID, roles); err != nil {
				return Usuario{}, err
			}
			asignadas := asignaciones(roles)
			t.RegistrarCambios("usuario", u.ID, nil, auditable(u, asignadas))
			if err := t.Emitir("usuario.creado", map[string]any{"usuario_id": u.ID, "tipo": u.Tipo}); err != nil {
				return Usuario{}, err
			}
			if len(empresas) > 0 {
				var por *int64
				if a := t.Actor(); a.EsUsuario() {
					por = &a.ID
				}
				for _, e := range empresas {
					if _, err := q.AsignarEmpresa(ctx, identidadbd.AsignarEmpresaParams{UsuarioID: u.ID, EmpresaID: e, AsignadoPor: por}); err != nil {
						return Usuario{}, err
					}
					if err := t.Emitir("usuario.empresa_asignada", map[string]any{"usuario_id": u.ID, "empresa_id": e}); err != nil {
						return Usuario{}, err
					}
				}
				t.RegistrarCambios("usuario", u.ID, map[string]any{"empresas": []int64{}}, map[string]any{"empresas": empresas})
			}
			return aUsuario(u, asignadas, empresas), nil
		},
	})
}

func normalizarNombreCorreo(nombre, correo string) (string, string, error) {
	nombre = strings.TrimSpace(nombre)
	correo = strings.ToLower(strings.TrimSpace(correo))
	var errores []nucleo.ErrorCampo
	if nombre == "" || utf8.RuneCountInString(nombre) > 120 {
		errores = append(errores, nucleo.ErrorCampo{Field: "nombre", Message: "Nombre obligatorio (máximo 120 caracteres)", Rule: "requerido"})
	}
	if dir, err := mail.ParseAddress(correo); err != nil || dir.Address != correo {
		errores = append(errores, nucleo.ErrorCampo{Field: "correo", Message: "Correo inválido", Rule: "email"})
	}
	if len(errores) > 0 {
		return "", "", nucleo.Validacion(errores...)
	}
	return nombre, correo, nil
}

func hashSegunTipo(tipo, contrasena string) (*string, error) {
	if tipo == "integracion" {
		if contrasena != "" {
			return nil, nucleo.Validacion(nucleo.ErrorCampo{Field: "contrasena", Message: "Las integraciones entran solo con token", Rule: "prohibido"})
		}
		return nil, nil
	}
	return hashValidado(contrasena)
}

func hashValidado(contrasena string) (*string, error) {
	if utf8.RuneCountInString(contrasena) < LongitudMinimaContrasena {
		return nil, nucleo.Validacion(nucleo.ErrorCampo{Field: "contrasena", Message: "Mínimo 12 caracteres", Rule: "minLength"})
	}
	h, err := HashContrasena(contrasena)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// empresasNuevoUsuario valida las empresas de un usuario nuevo: sin repetir, registradas y en
// que opera quien lo crea. Sin empresas.todas global (y con empresas registradas) debe venir al
// menos una: un usuario sin empresas solo lo administra un admin global (exigirEmpresasDe).
func empresasNuevoUsuario(ctx context.Context, q *identidadbd.Queries, ids []int64) ([]int64, error) {
	salida := make([]int64, 0, len(ids))
	for i, id := range ids {
		if slices.Contains(salida, id) {
			return nil, nucleo.Validacion(nucleo.ErrorCampo{Field: fmt.Sprintf("empresas[%d]", i), Message: "Empresa repetida", Rule: "unico"})
		}
		if err := exigirEmpresaPropia(ctx, q, id); err != nil {
			return nil, err
		}
		salida = append(salida, id)
	}
	slices.Sort(salida)
	if len(salida) == 0 {
		a, err := alcanceDelActor(ctx, q)
		if err != nil {
			return nil, err
		}
		if !a.todas {
			return nil, nucleo.Validacion(nucleo.ErrorCampo{Field: "empresas", Message: "Indica al menos una empresa en que operará", Rule: "requerido"})
		}
	}
	return salida, nil
}
