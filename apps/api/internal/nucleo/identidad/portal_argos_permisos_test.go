package identidad_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// problemaDe exige un *nucleo.Problema con ese status y, si se da, ese código (sufijo del type).
func problemaDe(t *testing.T, err error, status int, codigo string) {
	t.Helper()
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != status || (codigo != "" && p.Type != "urn:iris:problema:"+codigo) {
		t.Fatalf("esperaba %d %s, llegó %v", status, codigo, err)
	}
}

// Revisión B1 (a): un rol a medida no puede llevar portal_argos.*.
func TestRolAMedidaSinPermisosPortalArgos(t *testing.T) {
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	_, err := mod.CrearRol.Ejecutar(ctx, identidad.EntradaCrearRol{Codigo: "portal_mano", Nombre: "Portal a mano",
		Permisos: []nucleo.Permiso{nucleo.PermTicketsVer, nucleo.PermPortalArgosVerTickets}})
	problemaDe(t, err, http.StatusUnprocessableEntity, "validacion")
}

// Revisión B1 (b): integracion_argos e integracion_horus no se dan a una persona.
func TestRolesDeIntegracionSoloAIntegraciones(t *testing.T) {
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	u, err := mod.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{Nombre: "Persona", Correo: pruebas.Correo("persona"),
		Contrasena: "contrasena-larga-1", Roles: []identidad.AsignacionRol{{Rol: "soporte"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mod.AsignarRolUsuario.Ejecutar(ctx, identidad.EntradaRolUsuario{UsuarioID: u.ID,
		AsignacionRol: identidad.AsignacionRol{Rol: "integracion_argos"}})
	problemaDe(t, err, http.StatusUnprocessableEntity, "validacion")
	_, err = mod.EditarUsuario.Ejecutar(ctx, identidad.EntradaEditarUsuario{ID: u.ID, Version: u.Version,
		Roles: &[]identidad.AsignacionRol{{Rol: "soporte"}, {Rol: "integracion_horus"}}})
	problemaDe(t, err, http.StatusUnprocessableEntity, "validacion")
}

// Revisión B3: un token creado por la API no lleva portal_argos.* (422 permiso_solo_token_portal).
func TestTokenDeLaAPISinPermisosPortalArgos(t *testing.T) {
	pool := pruebas.Pool(t)
	u, err := mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pool), identidad.EntradaCrearUsuario{Nombre: "Con token",
		Correo: pruebas.Correo("tok"), Contrasena: "contrasena-larga-1", Roles: []identidad.AsignacionRol{{Rol: "admin"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := pruebas.ContextoUsuario(pool, u.ID, nucleo.PermTokensCrear)
	_, err = mod.CrearToken.Ejecutar(ctx, identidad.EntradaCrearToken{Nombre: "portal",
		Permisos: []nucleo.Permiso{nucleo.PermTicketsVer, nucleo.PermPortalArgosCrearTicket}})
	problemaDe(t, err, http.StatusUnprocessableEntity, "permiso_solo_token_portal")
}

// todosMenosPortal: los permisos de un admin tal como los deja la sesión del panel (sin portal_argos.*).
func todosMenosPortal() contexto.Permisos {
	mapa := map[string][]contexto.Alcance{}
	for _, p := range nucleo.CodigosPermisos() {
		if !strings.HasPrefix(string(p), "portal_argos.") {
			mapa[string(p)] = []contexto.Alcance{{}}
		}
	}
	return contexto.NuevosPermisos(mapa)
}

// rolConPortal crea a mano en la base (CrearRol ya no lo deja) un rol a medida con un permiso
// portal_argos.*: la exención solo vale para los roles de sistema de integración.
func rolConPortal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	id := contexto.NuevoRequestID()
	codigo := "portal_mano_" + id[len(id)-8:]
	var rol int64
	if err := pool.QueryRow(ctx, `INSERT INTO nucleo.roles (codigo, nombre, descripcion, sistema) VALUES ($1, $1, '', false) RETURNING id`, codigo).Scan(&rol); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, 'portal_argos.ver_tickets')`, rol); err != nil {
		t.Fatal(err)
	}
	return codigo
}

func integracionArgos(t *testing.T, ctx context.Context) identidad.Usuario {
	t.Helper()
	u, err := mod.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{Nombre: "Argos", Correo: pruebas.Correo("argos"), Tipo: "integracion",
		Roles: []identidad.AsignacionRol{{Rol: "integracion_argos"}}})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// Revisión B1 (c): resolverRoles y exigirSuperior solo ignoran portal_argos.* de los roles de sistema
// de integración.
func TestPortalArgosExentoSoloEnRolesDeIntegracion(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	q := identidadbd.New(pool)
	quien := todosMenosPortal()
	if err := identidad.ResolverRolesPrueba(ctx, q, quien, []identidad.AsignacionRol{{Rol: "integracion_argos"}}); err != nil {
		t.Fatalf("integracion_argos: %v", err)
	}
	aMano := rolConPortal(t, ctx, pool)
	problemaDe(t, identidad.ResolverRolesPrueba(ctx, q, quien, []identidad.AsignacionRol{{Rol: aMano}}), http.StatusForbidden, "")

	integ := integracionArgos(t, ctx)
	persona, err := mod.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{Nombre: "Persona", Correo: pruebas.Correo("pmano"),
		Contrasena: "contrasena-larga-1", Roles: []identidad.AsignacionRol{{Rol: aMano}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := identidad.ExigirSuperiorPrueba(ctx, q, quien, integ.ID); err != nil {
		t.Fatalf("usuario de integración: %v", err)
	}
	problemaDe(t, identidad.ExigirSuperiorPrueba(ctx, q, quien, persona.ID), http.StatusForbidden, "")
}

// Revisión B2: un token derivado (token_portal) solo sale del canje y solo con portal_argos.*.
func TestPuedeEmitirTokenPortal(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	q := identidadbd.New(pool)
	integ := integracionArgos(t, ctx)
	dueno, err := q.UsuarioPorID(ctx, integ.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor := contexto.Actor{Tipo: contexto.ActorIntegracion, ID: integ.ID, Nombre: "Argos"}
	base := contexto.ConActor(ctx, actor, todosMenosPortal())
	portal := []string{}
	for _, p := range nucleo.PermisosPortalArgos() {
		portal = append(portal, string(p))
	}
	hijo := identidad.ConTokenPadrePrueba(base, 1)
	problemaDe(t, identidad.PuedeEmitirPrueba(hijo, q, actor, dueno, portal), http.StatusForbidden, "")
	canje := identidad.ConTokenPadrePrueba(identidad.ConTokenDeCanjePrueba(base), 1)
	if err := identidad.PuedeEmitirPrueba(canje, q, actor, dueno, portal); err != nil {
		t.Fatalf("token_portal en el canje: %v", err)
	}
	problemaDe(t, identidad.PuedeEmitirPrueba(canje, q, actor, dueno, append(portal, "tickets.ver")), http.StatusForbidden, "")
	// Sin padre, portal_argos.* no cuentan: el token los pierde al autenticarse.
	if err := identidad.PuedeEmitirPrueba(base, q, actor, dueno, nil); err != nil {
		t.Fatalf("token sin padre: %v", err)
	}
}
