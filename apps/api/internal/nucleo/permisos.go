package nucleo

import (
	"slices"

	"github.com/danielgtaylor/huma/v2"
)

// Permiso es el nombre de una acción del ACL (`modulo.verbo`), CLAUDE.md §8.
//
// Este archivo es el ÚNICO catálogo: `iris migrar` lo sincroniza con nucleo.permisos y arma
// los roles base, y el OpenAPI lo publica como el enum del esquema `Permiso`. Un caso de uso
// que exige un permiso que no está aquí no arranca (auditoria.Nuevo entra en pánico).
//
// Para agregar permisos (I2: tickets.*, ordenes.*…): una constante, una línea en Catalogo y,
// si corresponde, en RolesBase; luego `make openapi`.
type Permiso string

const (
	PermUsuariosVer        Permiso = "usuarios.ver"
	PermUsuariosCrear      Permiso = "usuarios.crear"
	PermUsuariosEditar     Permiso = "usuarios.editar"
	PermUsuariosDesactivar Permiso = "usuarios.desactivar"
	PermRolesVer           Permiso = "roles.ver"
	PermPermisosVer        Permiso = "permisos.ver"
	PermTokensVer          Permiso = "tokens.ver"
	PermTokensCrear        Permiso = "tokens.crear"
	PermTokensRevocar      Permiso = "tokens.revocar" //nolint:gosec // nombre de permiso, no una credencial
)

// DefPermiso es una entrada del catálogo.
type DefPermiso struct {
	Codigo      Permiso
	Descripcion string
}

// Catalogo es la lista ordenada de todos los permisos.
var Catalogo = []DefPermiso{
	{PermPermisosVer, "Ver el catálogo de permisos"},
	{PermRolesVer, "Ver roles y sus permisos"},
	{PermTokensCrear, "Crear tokens de API (iris_pat_) para un usuario"},
	{PermTokensRevocar, "Revocar tokens de API"},
	{PermTokensVer, "Ver tokens de API (nunca el valor)"},
	{PermUsuariosCrear, "Crear usuarios y asignarles roles"},
	{PermUsuariosDesactivar, "Desactivar usuarios (no se borran)"},
	{PermUsuariosEditar, "Editar usuarios, su contraseña y sus roles"},
	{PermUsuariosVer, "Ver y buscar usuarios"},
}

// DefRol es un rol base. Permisos nil = todos los del catálogo (admin).
type DefRol struct {
	Codigo      string
	Nombre      string
	Descripcion string
	Todos       bool
	Permisos    []Permiso
}

// RolesBase son los roles que crea y mantiene el seed (CLAUDE.md §8). Sus permisos se
// sincronizan en cada `iris migrar`: lo que no está aquí se les quita.
var RolesBase = []DefRol{
	{Codigo: "admin", Nombre: "Administrador", Descripcion: "Todos los permisos", Todos: true},
	{Codigo: "supervisor", Nombre: "Supervisor", Descripcion: "Supervisa soporte, campo y despacho",
		Permisos: []Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer, PermTokensVer}},
	{Codigo: "despacho", Nombre: "Despacho", Descripcion: "Programa órdenes y rutas",
		Permisos: []Permiso{PermUsuariosVer}},
	{Codigo: "tecnico", Nombre: "Técnico", Descripcion: "Trabaja órdenes en campo"},
	{Codigo: "soporte", Nombre: "Soporte", Descripcion: "Atiende tickets",
		Permisos: []Permiso{PermUsuariosVer}},
	{Codigo: "ventas", Nombre: "Ventas", Descripcion: "Seguimiento comercial"},
	{Codigo: "lectura", Nombre: "Lectura", Descripcion: "Solo consulta",
		Permisos: []Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer}},
}

// PermisosDeRol devuelve los permisos efectivos de un rol base.
func (r DefRol) PermisosDeRol() []Permiso {
	if r.Todos {
		return CodigosPermisos()
	}
	return r.Permisos
}

// CodigosPermisos devuelve los códigos del catálogo.
func CodigosPermisos() []Permiso {
	salida := make([]Permiso, 0, len(Catalogo))
	for _, p := range Catalogo {
		salida = append(salida, p.Codigo)
	}
	return salida
}

// EsPermiso indica si el código está en el catálogo.
func EsPermiso(codigo string) bool {
	return slices.ContainsFunc(Catalogo, func(p DefPermiso) bool { return string(p.Codigo) == codigo })
}

// DescripcionPermiso devuelve la descripción de un permiso del catálogo.
func DescripcionPermiso(codigo Permiso) string {
	for _, p := range Catalogo {
		if p.Codigo == codigo {
			return p.Descripcion
		}
	}
	return ""
}

// Schema publica Permiso como componente con enum en el OpenAPI (huma.SchemaProvider).
func (Permiso) Schema(r huma.Registry) *huma.Schema {
	const nombre = "Permiso"
	if _, ok := r.Map()[nombre]; !ok {
		enum := make([]any, 0, len(Catalogo))
		for _, p := range CodigosPermisos() {
			enum = append(enum, string(p))
		}
		r.Map()[nombre] = &huma.Schema{
			Type:        huma.TypeString,
			Title:       nombre,
			Description: "Permiso del ACL: nombre de la acción (`modulo.verbo`). Catálogo en internal/nucleo/permisos.go.",
			Enum:        enum,
		}
	}
	return &huma.Schema{Ref: "#/components/schemas/" + nombre}
}
