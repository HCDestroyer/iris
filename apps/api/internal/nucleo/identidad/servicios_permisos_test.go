package identidad_test

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.8.0 §2.4 y §10.2 puntos 4 y 5: quién ve la red y la ubicación del
// servicio en los roles base (los sincroniza `iris migrar`).
func TestServiciosPermisosRolesBase(t *testing.T) {
	ctx := context.Background()
	pool := pruebas.Pool(t)
	tiene := func(rol, permiso string) bool {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM nucleo.roles_permisos rp JOIN nucleo.roles r ON r.id = rp.rol_id
			WHERE r.codigo = $1 AND rp.permiso = $2`, rol, permiso).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	red := []string{"admin", "supervisor", "soporte", "tecnico"}
	ubicacion := []string{"admin", "supervisor", "despacho", "soporte", "ventas", "tecnico"}
	for _, rol := range []string{"admin", "supervisor", "despacho", "tecnico", "soporte", "ventas", "lectura", "caja", "integracion_argos", "integracion_horus"} {
		if got, want := tiene(rol, "servicios.ver_red"), slices.Contains(red, rol); got != want {
			t.Errorf("%s: servicios.ver_red = %v, se esperaba %v", rol, got, want)
		}
		if got, want := tiene(rol, "servicios.ver_ubicacion"), slices.Contains(ubicacion, rol); got != want {
			t.Errorf("%s: servicios.ver_ubicacion = %v, se esperaba %v", rol, got, want)
		}
	}
}

// Los roles propios que veían el servicio con clientes.ver reciben los dos permisos al migrar, una
// vez, con su fila de auditoría; los demás no cambian.
func TestServiciosPermisosRolesPropiosMigracion(t *testing.T) {
	ctx := context.Background()
	crudo, err := os.ReadFile("../../../migraciones/20261119024424_integracion_argos_servicio_datos_080.sql")
	if err != nil {
		t.Fatal(err)
	}
	texto := string(crudo)
	ini := strings.Index(texto, "INSERT INTO nucleo.permisos")
	fin := strings.Index(texto, "-- +goose Down")
	if ini < 0 || fin < ini {
		t.Fatal("no se encontró la parte de permisos de la migración")
	}
	tx, err := pruebas.Pool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sufijo := pruebas.Unico()
	var conClientes, sinClientes int64
	if err := tx.QueryRow(ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, 'Mesa de ayuda') RETURNING id`, "mesa_"+sufijo).Scan(&conClientes); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, 'Bodega') RETURNING id`, "bodega_"+sufijo).Scan(&sinClientes); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, 'clientes.ver'), ($2, 'tickets.ver')`, conClientes, sinClientes); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, texto[ini:fin]); err != nil {
		t.Fatal(err)
	}
	permisos := func(rol int64) []string {
		filas, err := tx.Query(ctx, `SELECT permiso FROM nucleo.roles_permisos WHERE rol_id = $1 ORDER BY permiso`, rol)
		if err != nil {
			t.Fatal(err)
		}
		defer filas.Close()
		var salida []string
		for filas.Next() {
			var p string
			if err := filas.Scan(&p); err != nil {
				t.Fatal(err)
			}
			salida = append(salida, p)
		}
		return salida
	}
	if got := permisos(conClientes); !slices.Equal(got, []string{"clientes.ver", "servicios.ver_red", "servicios.ver_ubicacion"}) {
		t.Fatalf("rol propio con clientes.ver: %v", got)
	}
	if got := permisos(sinClientes); !slices.Equal(got, []string{"tickets.ver"}) {
		t.Fatalf("rol propio sin clientes.ver no debía cambiar: %v", got)
	}
	var version, auditadas int
	if err := tx.QueryRow(ctx, `SELECT version FROM nucleo.roles WHERE id = $1`, conClientes).Scan(&version); err != nil || version != 2 {
		t.Fatalf("la versión del rol debía subir: %d %v", version, err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'roles.permisos_agregados_por_migracion'
		AND entidad = 'rol' AND entidad_id = $1 AND entrada->'permisos' = '["servicios.ver_red", "servicios.ver_ubicacion"]'::jsonb`, itoa(conClientes)).Scan(&auditadas); err != nil || auditadas != 1 {
		t.Fatalf("auditoría de la migración: %d %v", auditadas, err)
	}
	// Una segunda pasada no duplica nada ni audita de nuevo.
	if _, err := tx.Exec(ctx, texto[ini:fin]); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'roles.permisos_agregados_por_migracion' AND entidad_id = $1`, itoa(conClientes)).Scan(&auditadas); err != nil || auditadas != 1 {
		t.Fatalf("la segunda pasada no debía auditar: %d %v", auditadas, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
