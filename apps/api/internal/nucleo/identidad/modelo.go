package identidad

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Tipos que expone la API (camelCase). El OpenAPI se genera de aquí.

// AsignacionRol es un rol de un usuario con su alcance opcional (zona o cuadrilla, no ambas).
type AsignacionRol struct {
	Rol         string `json:"rol" minLength:"1" maxLength:"60" doc:"Código del rol (admin, supervisor, despacho, tecnico, soporte, ventas, lectura…)"`
	ZonaID      *int64 `json:"zonaId,omitempty" minimum:"1" doc:"Limita el rol a una zona"`
	CuadrillaID *int64 `json:"cuadrillaId,omitempty" minimum:"1" doc:"Limita el rol a una cuadrilla"`
}

func (a AsignacionRol) clave() string {
	k := a.Rol
	if a.ZonaID != nil {
		k += "@zona:" + strconv.FormatInt(*a.ZonaID, 10)
	}
	if a.CuadrillaID != nil {
		k += "@cuadrilla:" + strconv.FormatInt(*a.CuadrillaID, 10)
	}
	return k
}

// Usuario es una persona o una integración (Argos, Horus) que entra a Iris.
type Usuario struct {
	ID        int64  `json:"id"`
	Nombre    string `json:"nombre"`
	Correo    string `json:"correo"`
	Tipo      string `json:"tipo" enum:"persona,integracion"`
	Activo    bool   `json:"activo"`
	MFAActivo bool   `json:"mfaActivo" doc:"Tiene TOTP activo"`
	// CambioContrasenaRequerido: tiene una contraseña temporal (reinicio de MFA por un admin).
	CambioContrasenaRequerido bool            `json:"cambioContrasenaRequerido,omitempty" doc:"Solo presente (true) si tiene una contraseña temporal: hasta cambiarla (POST /api/v1/yo/contrasena) su sesión solo puede cambiarla, ver /yo o cerrar sesión (403 cambio_contrasena_requerido)"`
	Roles                     []AsignacionRol `json:"roles"`
	Empresas                  []int64         `json:"empresas" doc:"Empresas de Argos asignadas al usuario (ids). Con el permiso empresas.todas opera además en todas"`
	Version                   int32           `json:"version"`
	CreadoEn                  time.Time       `json:"creadoEn"`
	ActualizadoEn             time.Time       `json:"actualizadoEn"`
}

// Rol con sus permisos.
type Rol struct {
	ID          int64            `json:"id"`
	Codigo      string           `json:"codigo"`
	Nombre      string           `json:"nombre"`
	Descripcion string           `json:"descripcion"`
	Sistema     bool             `json:"sistema" doc:"Rol base: lo mantiene el seed"`
	Permisos    []nucleo.Permiso `json:"permisos"`
	Version     int32            `json:"version"`
}

// PermisoInfo es una entrada del catálogo.
type PermisoInfo struct {
	Codigo      nucleo.Permiso `json:"codigo"`
	Descripcion string         `json:"descripcion"`
}

// TokenAPI es un token sin su valor (el valor solo se ve al crearlo).
type TokenAPI struct {
	ID         int64            `json:"id"`
	UsuarioID  int64            `json:"usuarioId"`
	Nombre     string           `json:"nombre"`
	Prefijo    string           `json:"prefijo" doc:"Inicio del token para reconocerlo (iris_pat_XXXX)"`
	Permisos   []nucleo.Permiso `json:"permisos" nullable:"true" doc:"null = los del usuario; si no, la intersección"`
	ExpiraEn   *time.Time       `json:"expiraEn" nullable:"true"`
	UltimoUso  *time.Time       `json:"ultimoUso" nullable:"true"`
	RevocadoEn *time.Time       `json:"revocadoEn" nullable:"true"`
	CreadoPor  *int64           `json:"creadoPor" nullable:"true"`
	CreadoEn   time.Time        `json:"creadoEn"`
}

func aUsuario(u identidadbd.NucleoUsuario, roles []AsignacionRol, empresas []int64) Usuario {
	if roles == nil {
		roles = []AsignacionRol{}
	}
	if empresas == nil {
		empresas = []int64{}
	}
	return Usuario{
		ID: u.ID, Nombre: u.Nombre, Correo: u.Correo, Tipo: u.Tipo, Activo: u.Activo,
		MFAActivo: u.TotpActivo, CambioContrasenaRequerido: u.DebeCambiarContrasena, Roles: roles, Empresas: empresas, Version: u.Version, CreadoEn: u.CreatedAt, ActualizadoEn: u.UpdatedAt,
	}
}

// empresasDe carga las empresas asignadas a varios usuarios.
func empresasDe(ctx context.Context, q *identidadbd.Queries, ids ...int64) (map[int64][]int64, error) {
	filas, err := q.EmpresasDeUsuarios(ctx, ids)
	if err != nil {
		return nil, err
	}
	salida := map[int64][]int64{}
	for _, f := range filas {
		salida[f.UsuarioID] = append(salida[f.UsuarioID], f.EmpresaID)
	}
	return salida, nil
}

func aTokenAPI(t identidadbd.NucleoTokensApi) TokenAPI {
	var permisos []nucleo.Permiso
	if t.Permisos != nil {
		permisos = make([]nucleo.Permiso, 0, len(t.Permisos))
		for _, p := range t.Permisos {
			permisos = append(permisos, nucleo.Permiso(p))
		}
	}
	return TokenAPI{
		ID: t.ID, UsuarioID: t.UsuarioID, Nombre: t.Nombre, Prefijo: t.Prefijo, Permisos: permisos,
		ExpiraEn: t.ExpiraEn, UltimoUso: t.UltimoUso, RevocadoEn: t.RevocadoEn, CreadoPor: t.CreadoPor, CreadoEn: t.CreatedAt,
	}
}

// rolesDe carga los roles de varios usuarios.
func rolesDe(ctx context.Context, q *identidadbd.Queries, ids ...int64) (map[int64][]AsignacionRol, error) {
	filas, err := q.RolesDeUsuarios(ctx, ids)
	if err != nil {
		return nil, err
	}
	salida := map[int64][]AsignacionRol{}
	for _, f := range filas {
		salida[f.UsuarioID] = append(salida[f.UsuarioID], AsignacionRol{Rol: f.Codigo, ZonaID: f.ZonaID, CuadrillaID: f.CuadrillaID})
	}
	return salida, nil
}

// usuarioAuditable es la vista de un usuario que se compara en auditoria.cambios.
type usuarioAuditable struct {
	Nombre         string   `json:"nombre"`
	Correo         string   `json:"correo"`
	Tipo           string   `json:"tipo"`
	Activo         bool     `json:"activo"`
	ContrasenaHash *string  `json:"contrasena_hash"`
	TotpActivo     bool     `json:"totp_activo"`
	DebeCambiar    bool     `json:"cambio_requerido"`
	Roles          []string `json:"roles"`
}

func auditable(u identidadbd.NucleoUsuario, roles []AsignacionRol) usuarioAuditable {
	claves := make([]string, 0, len(roles))
	for _, r := range roles {
		claves = append(claves, r.clave())
	}
	slices.Sort(claves)
	return usuarioAuditable{
		Nombre: u.Nombre, Correo: u.Correo, Tipo: u.Tipo, Activo: u.Activo,
		ContrasenaHash: u.ContrasenaHash, TotpActivo: u.TotpActivo, DebeCambiar: u.DebeCambiarContrasena, Roles: claves,
	}
}
