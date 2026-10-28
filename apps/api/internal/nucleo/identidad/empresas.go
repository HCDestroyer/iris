package identidad

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Empresas registradas (instantánea de Argos) y membresía usuario ↔ empresa. Ver
// empresa_activa.go para cómo se elige la empresa de cada petición.

// Empresa es una empresa de Argos registrada en Iris.
type Empresa struct {
	ID             int64      `json:"id" doc:"Id de la empresa en Argos"`
	Nombre         string     `json:"nombre"`
	NIT            string     `json:"nit"`
	Origen         string     `json:"origen" enum:"argos,manual" doc:"argos: la trajo el conector; manual: la registró un administrador"`
	SincronizadoEn *time.Time `json:"sincronizadoEn" nullable:"true"`
	Version        int32      `json:"version"`
	CreadoEn       time.Time  `json:"creadoEn"`
	ActualizadoEn  time.Time  `json:"actualizadoEn"`
}

// EmpresaResumen es lo que necesita el selector de empresa del panel.
type EmpresaResumen struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

func aEmpresa(e identidadbd.NucleoEmpresa) Empresa {
	return Empresa{ID: e.ID, Nombre: e.Nombre, NIT: e.Nit, Origen: e.Origen, SincronizadoEn: e.SincronizadoEn,
		Version: e.Version, CreadoEn: e.CreatedAt, ActualizadoEn: e.UpdatedAt}
}

// empresaAuditable es la vista de una empresa que se compara en auditoria.cambios.
type empresaAuditable struct {
	Nombre string `json:"nombre"`
	NIT    string `json:"nit"`
}

// Listar -----------------------------------------------------------------------------------

func nuevoListarEmpresas() *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Empresa]] {
	return auditoria.Nuevo(auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Empresa]]{
		Accion:       "empresas.ver",
		Permiso:      nucleo.PermEmpresasVer,
		SinEmpresa:   true, // administración: no es operativa
		AuditarExito: soloLecturas[nucleo.Pagina[Empresa]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in nucleo.ParamsPagina) (nucleo.Pagina[Empresa], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Empresa]{}, err
			}
			filas, err := identidadbd.New(t).ListarEmpresas(ctx, identidadbd.ListarEmpresasParams{DespuesDe: despues, Limite: int32(in.Limite() + 1)}) //nolint:gosec // ≤ 201
			if err != nil {
				return nucleo.Pagina[Empresa]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aEmpresa,
				func(e identidadbd.NucleoEmpresa) []any { return []any{e.ID} }), nil
		},
	})
}

// Crear y editar ---------------------------------------------------------------------------

// EntradaCrearEmpresa registra a mano una empresa de Argos.
type EntradaCrearEmpresa struct {
	ID     int64  `json:"id" minimum:"1" doc:"Id de la empresa en Argos"`
	Nombre string `json:"nombre" minLength:"1" maxLength:"200"`
	NIT    string `json:"nit,omitempty" maxLength:"30"`
}

// EntradaEditarEmpresa cambia nombre y NIT.
type EntradaEditarEmpresa struct {
	ID      int64  `json:"-"`
	Nombre  string `json:"nombre" minLength:"1" maxLength:"200"`
	NIT     string `json:"nit,omitempty" maxLength:"30"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
}

func nombreEmpresa(nombre string) (string, error) {
	nombre = strings.TrimSpace(nombre)
	if nombre == "" {
		return "", nucleo.Validacion(nucleo.ErrorCampo{Field: "nombre", Message: "Obligatorio", Rule: "requerido"})
	}
	return nombre, nil
}

func nuevoCrearEmpresa() *auditoria.Caso[EntradaCrearEmpresa, Empresa] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearEmpresa, Empresa]{
		Accion:     "empresas.crear",
		Permiso:    nucleo.PermEmpresasCrear,
		SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearEmpresa) (Empresa, error) {
			nombre, err := nombreEmpresa(in.Nombre)
			if err != nil {
				return Empresa{}, err
			}
			e, err := identidadbd.New(t).CrearEmpresa(ctx, identidadbd.CrearEmpresaParams{ID: in.ID, Nombre: nombre, Nit: strings.TrimSpace(in.NIT)})
			if err != nil {
				return Empresa{}, err // unique → 409
			}
			t.FijarEmpresa(e.ID)
			t.RegistrarCambios("empresa", e.ID, nil, empresaAuditable{Nombre: e.Nombre, NIT: e.Nit})
			return aEmpresa(e), t.Emitir("empresa.creada", map[string]any{"empresa_id": e.ID, "origen": e.Origen})
		},
	})
}

func nuevoEditarEmpresa() *auditoria.Caso[EntradaEditarEmpresa, Empresa] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarEmpresa, Empresa]{
		Accion:     "empresas.editar",
		Permiso:    nucleo.PermEmpresasEditar,
		SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarEmpresa) (Empresa, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("empresa", in.ID)
			t.FijarEmpresa(in.ID)
			nombre, err := nombreEmpresa(in.Nombre)
			if err != nil {
				return Empresa{}, err
			}
			antes, err := q.EmpresaPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Empresa{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "empresa", in.ID); err != nil {
				return Empresa{}, err
			}
			despues, err := q.EditarEmpresa(ctx, identidadbd.EditarEmpresaParams{ID: in.ID, Version: in.Version, Nombre: nombre, Nit: strings.TrimSpace(in.NIT)})
			if err := nucleo.VerificarVersion(err, "empresa", in.ID); err != nil {
				return Empresa{}, err
			}
			campos := t.RegistrarCambios("empresa", in.ID, empresaAuditable{Nombre: antes.Nombre, NIT: antes.Nit},
				empresaAuditable{Nombre: despues.Nombre, NIT: despues.Nit})
			if campos == nil {
				campos = []string{}
			}
			return aEmpresa(despues), t.Emitir("empresa.actualizada", map[string]any{"empresa_id": in.ID, "campos": campos})
		},
	})
}

// Membresía --------------------------------------------------------------------------------

// EntradaEmpresaUsuario asigna o quita una empresa a un usuario.
type EntradaEmpresaUsuario struct {
	UsuarioID int64 `json:"-"`
	EmpresaID int64 `json:"empresaId" minimum:"1" doc:"Id de la empresa (debe estar registrada)"`
}

// exigirEmpresaPropia: nadie da (ni quita) acceso a una empresa en que no opera.
func exigirEmpresaPropia(ctx context.Context, q *identidadbd.Queries, empresaID int64) error {
	if _, err := q.EmpresaPorID(ctx, empresaID); err != nil {
		return err // 404
	}
	actor := contexto.ActorDe(ctx)
	propias, err := q.EmpresasAccesibles(ctx, identidadbd.EmpresasAccesiblesParams{
		Todas: contexto.PermisosDe(ctx).Tiene(string(nucleo.PermEmpresasTodas)), UsuarioID: actor.ID,
	})
	if err != nil {
		return err
	}
	if !slices.Contains(propias, empresaID) {
		return nucleo.Prohibido("No operas en esa empresa: no puedes asignarla ni quitarla")
	}
	return nil
}

func nuevoCambiarEmpresaUsuario(asignar bool) *auditoria.Caso[EntradaEmpresaUsuario, Usuario] {
	accion, permiso, evento := "usuarios.asignar_empresa", nucleo.PermUsuariosAsignarEmpresa, "usuario.empresa_asignada"
	if !asignar {
		accion, permiso, evento = "usuarios.quitar_empresa", nucleo.PermUsuariosQuitarEmpresa, "usuario.empresa_quitada"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaEmpresaUsuario, Usuario]{
		Accion:     accion,
		Permiso:    permiso,
		SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEmpresaUsuario) (Usuario, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("usuario", in.UsuarioID)
			t.FijarEmpresa(in.EmpresaID)
			u, err := q.UsuarioPorIDParaActualizar(ctx, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			if err := exigirEmpresaPropia(ctx, q, in.EmpresaID); err != nil {
				return Usuario{}, err
			}
			antes, err := empresasDe(ctx, q, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			var n int64
			if asignar {
				var por *int64
				if a := t.Actor(); a.EsUsuario() {
					por = &a.ID
				}
				n, err = q.AsignarEmpresa(ctx, identidadbd.AsignarEmpresaParams{UsuarioID: in.UsuarioID, EmpresaID: in.EmpresaID, AsignadoPor: por})
			} else {
				// Quitar acceso es como quitar roles: no a alguien con más permisos que tú.
				if err := exigirSuperior(ctx, q, contexto.PermisosDe(ctx), in.UsuarioID, "quitarle una empresa a este usuario"); err != nil {
					return Usuario{}, err
				}
				n, err = q.QuitarEmpresa(ctx, identidadbd.QuitarEmpresaParams{UsuarioID: in.UsuarioID, EmpresaID: in.EmpresaID})
			}
			if err != nil {
				return Usuario{}, err
			}
			if n == 0 {
				if asignar {
					return Usuario{}, nucleo.ReglaNegocio("empresa_ya_asignada", "El usuario ya tiene esa empresa")
				}
				return Usuario{}, nucleo.ReglaNegocio("empresa_no_asignada", "El usuario no tiene esa empresa")
			}
			despues, err := empresasDe(ctx, q, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			t.RegistrarCambios("usuario", in.UsuarioID, map[string]any{"empresas": lista(antes[in.UsuarioID])},
				map[string]any{"empresas": lista(despues[in.UsuarioID])})
			if err := t.Emitir(evento, map[string]any{"usuario_id": in.UsuarioID, "empresa_id": in.EmpresaID}); err != nil {
				return Usuario{}, err
			}
			roles, err := rolesDe(ctx, q, in.UsuarioID)
			if err != nil {
				return Usuario{}, err
			}
			return aUsuario(u, roles[in.UsuarioID], despues[in.UsuarioID]), nil
		},
	})
}

func lista(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// empresasResumen devuelve id y nombre de las empresas dadas (para /yo).
func empresasResumen(ctx context.Context, q *identidadbd.Queries, ids []int64) ([]EmpresaResumen, error) {
	salida := []EmpresaResumen{}
	if len(ids) == 0 {
		return salida, nil
	}
	filas, err := q.EmpresasPorIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, f := range filas {
		salida = append(salida, EmpresaResumen{ID: f.ID, Nombre: f.Nombre})
	}
	return salida, nil
}
