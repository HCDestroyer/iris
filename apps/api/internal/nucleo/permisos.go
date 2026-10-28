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

// Empresas (multiempresa igual que Argos).
const (
	PermEmpresasCrear          Permiso = "empresas.crear"
	PermEmpresasEditar         Permiso = "empresas.editar"
	PermEmpresasTodas          Permiso = "empresas.todas"
	PermEmpresasVer            Permiso = "empresas.ver"
	PermUsuariosAsignarEmpresa Permiso = "usuarios.asignar_empresa"
	PermUsuariosQuitarEmpresa  Permiso = "usuarios.quitar_empresa"
)

// Conector Argos (lectura) e integraciones.
const (
	PermClientesEstadoCuenta    Permiso = "clientes.estado_cuenta"
	PermClientesVer             Permiso = "clientes.ver"
	PermIntegracionesActivar    Permiso = "integraciones.activar"
	PermIntegracionesConectar   Permiso = "integraciones.conectar"
	PermIntegracionesDesactivar Permiso = "integraciones.desactivar"
	PermIntegracionesVer        Permiso = "integraciones.ver"
)

// DefPermiso es una entrada del catálogo.
type DefPermiso struct {
	Codigo      Permiso
	Descripcion string
}

// Catalogo es la lista ordenada de todos los permisos.
var Catalogo = []DefPermiso{
	{PermClientesEstadoCuenta, "Ver el estado de cuenta resumido de un cliente de Argos (saldo y facturas vencidas)"},
	{PermClientesVer, "Buscar clientes de Argos y ver su ficha y servicios (lectura auditada)"},
	{PermEmpresasCrear, "Registrar a mano una empresa de Argos (id, nombre, NIT) cuando el conector no la trajo"},
	{PermEmpresasEditar, "Editar el nombre y el NIT de una empresa registrada"},
	{PermEmpresasTodas, "Operar en todas las empresas sin asignación (rol global); el resto solo en sus empresas asignadas"},
	{PermEmpresasVer, "Ver las empresas registradas"},
	{PermIntegracionesActivar, "Activar un conector con ArgosISP u Horus"},
	{PermIntegracionesConectar, "Conectar Iris con ArgosISP u Horus (enlace de conexión)"},
	{PermIntegracionesDesactivar, "Desactivar un conector con ArgosISP u Horus"},
	{PermIntegracionesVer, "Ver los conectores y su estado (nunca el token ni el secreto)"},
	{PermPermisosVer, "Ver el catálogo de permisos"},
	{PermRolesVer, "Ver roles y sus permisos"},
	{PermTokensCrear, "Crear tokens de API (iris_pat_) para un usuario"},
	{PermTokensRevocar, "Revocar tokens de API"},
	{PermTokensVer, "Ver tokens de API (nunca el valor)"},
	{PermUsuariosAsignarEmpresa, "Asignar a un usuario una empresa en que operar (solo empresas en que operas tú)"},
	{PermUsuariosCrear, "Crear usuarios y asignarles roles"},
	{PermUsuariosDesactivar, "Desactivar usuarios (no se borran)"},
	{PermUsuariosEditar, "Editar usuarios, su contraseña y sus roles"},
	{PermUsuariosQuitarEmpresa, "Quitar a un usuario una empresa (solo empresas en que operas tú)"},
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
		Permisos: []Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer, PermTokensVer,
			PermClientesVer, PermClientesEstadoCuenta, PermIntegracionesVer, PermEmpresasVer}},
	{Codigo: "despacho", Nombre: "Despacho", Descripcion: "Programa órdenes y rutas",
		Permisos: []Permiso{PermUsuariosVer,
			PermClientesVer}},
	{Codigo: "tecnico", Nombre: "Técnico", Descripcion: "Trabaja órdenes en campo",
		Permisos: []Permiso{PermClientesVer}},
	{Codigo: "soporte", Nombre: "Soporte", Descripcion: "Atiende tickets",
		Permisos: []Permiso{PermUsuariosVer,
			PermClientesVer, PermClientesEstadoCuenta}},
	{Codigo: "ventas", Nombre: "Ventas", Descripcion: "Seguimiento comercial",
		Permisos: []Permiso{PermClientesVer, PermClientesEstadoCuenta}},
	{Codigo: "lectura", Nombre: "Lectura", Descripcion: "Solo consulta",
		Permisos: []Permiso{PermUsuariosVer, PermRolesVer, PermPermisosVer,
			PermClientesVer}},
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
