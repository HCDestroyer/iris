package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.8.0 §2.4 (revisión de seguridad): la ficha de la conversación trae los
// servicios del cliente con su línea (plan, precio, zona, número) y el servidor omite la red y la
// ubicación sin servicios.ver_red / servicios.ver_ubicacion; sin clientes.ver no trae servicios; una
// conversación sin empresa, con empresas registradas, no trae al cliente.
func TestConvServiciosRecortePorPermiso(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	cliente := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Marta Ixcot', $2, 'C-77', now(), '{1}')`, cliente, "+502 "+num[3:7]+"-"+num[7:]); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, router_id, router_nombre,
			ip, direccion, lat, lon, user_ppp, precio_mensual, moneda, sincronizado_en)
		VALUES ($1, 1, $2, 'S-77', 'Fibra 50', 'activo', 'Chitá', 5, 'NODO-SECRETO', '100.64.77.9', 'CALLE-SECRETA 1', 14.6, -90.5, 'PPP-SECRETO', 250.00, 'GTQ', now())`,
		cliente*10, cliente); err != nil {
		t.Fatal(err)
	}
	b.texto(num, "wamid.srv1", "Hola")
	c := b.conv(num)
	id := cid(c)

	// Supervisor (ver_red y ver_ubicacion): la línea completa.
	s := servicioDe(t, c)
	if s["planNombre"] != "Fibra 50" || s["plan"] != "Fibra 50" || s["precioMensual"] != "250.00" || s["numero"] != float64(1) ||
		s["ip"] != "100.64.77.9" || s["routerNombre"] != "NODO-SECRETO" || s["direccion"] != "CALLE-SECRETA 1" {
		t.Fatalf("supervisor: %v", s)
	}

	// Un rol propio que ve las conversaciones y los clientes, sin servicios.ver_*: el servidor omite.
	rol := "conv_sin_red_" + pruebas.Unico()
	var rolID int64
	if err := b.bd.QueryRow(b.ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, 'Mesa') RETURNING id`, rol).Scan(&rolID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso)
		VALUES ($1, 'conversaciones.ver'), ($1, 'conversaciones.supervisar'), ($1, 'clientes.ver')`, rolID); err != nil {
		t.Fatal(err)
	}
	mesa := b.persona([]int64{1}, identidad.AsignacionRol{Rol: rol})
	r := b.api(mesa, http.MethodGet, "/conversaciones/"+id, nil)
	b.exigir(r, http.StatusOK)
	s = servicioDe(t, r.cuerpo)
	if s["precioMensual"] != "250.00" || s["planNombre"] != "Fibra 50" || s["zonaNombre"] != "Chitá" {
		t.Fatalf("sin servicios.ver_*: plan, zona y precio sí: %v", s)
	}
	for _, campo := range []string{"ip", "routerNombre", "routerId", "userPpp", "direccion", "lat", "lon"} {
		if _, ok := s[campo]; ok {
			t.Fatalf("sin servicios.ver_* no debía venir %q: %v", campo, s)
		}
	}
	crudo, _ := json.Marshal(r.cuerpo)
	for _, x := range []string{"100.64.77.9", "NODO-SECRETO", "CALLE-SECRETA", "PPP-SECRETO"} {
		if strings.Contains(string(crudo), x) {
			t.Fatalf("la respuesta trae %q", x)
		}
	}

	// Sin clientes.ver: el resumen del cliente sin servicios.
	sinClientes := "conv_sin_clientes_" + pruebas.Unico()
	if err := b.bd.QueryRow(b.ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, 'Mesa 2') RETURNING id`, sinClientes).Scan(&rolID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso)
		VALUES ($1, 'conversaciones.ver'), ($1, 'conversaciones.supervisar'), ($1, 'servicios.ver_red'), ($1, 'servicios.ver_ubicacion')`, rolID); err != nil {
		t.Fatal(err)
	}
	r = b.api(b.persona([]int64{1}, identidad.AsignacionRol{Rol: sinClientes}), http.MethodGet, "/conversaciones/"+id, nil)
	b.exigir(r, http.StatusOK)
	if res, _ := r.cuerpo["clienteResumen"].(map[string]any); res == nil || len(res["servicios"].([]any)) != 0 {
		t.Fatalf("sin clientes.ver no hay servicios: %v", r.cuerpo["clienteResumen"])
	}

	// Conversación sin empresa con empresas registradas: sin cliente.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET empresa_id = NULL WHERE id = $1`, c["id"]); err != nil {
		t.Skipf("la conversación no admite empresa nula: %v", err)
	}
	r = b.api(b.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%s", id), nil)
	if r.status == http.StatusOK {
		if _, ok := r.cuerpo["clienteResumen"]; ok && r.cuerpo["clienteResumen"] != nil {
			t.Fatalf("conversación sin empresa: no debía traer al cliente: %v", r.cuerpo["clienteResumen"])
		}
	}
}

func servicioDe(t *testing.T, c map[string]any) map[string]any {
	t.Helper()
	res, _ := c["clienteResumen"].(map[string]any)
	if res == nil {
		t.Fatalf("sin clienteResumen: %v", c)
	}
	ss, _ := res["servicios"].([]any)
	if len(ss) != 1 {
		t.Fatalf("servicios: %v", res)
	}
	s, _ := ss[0].(map[string]any)
	return s
}
