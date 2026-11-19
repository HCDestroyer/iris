package campo_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.8.0 §2.4 (revisión de seguridad): la red y la ubicación del servicio se
// recortan en el servidor también en el paquete y los cambios de la app de campo, y en la copia de
// Argos de GET /ordenes/{id} y GET /tickets/{id}.
func TestHTTPServiciosRecorteCampoYDetalle(t *testing.T) {
	base := nuevoServidor(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	const cliente, servicio = int64(7951), int64(79510)
	exec(`DELETE FROM integracion.argos_servicios WHERE cliente_id = $1`, cliente)
	exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, codigo, direccion, sincronizado_en)
		VALUES ($1, 1, 'Rosa Pérez', 'C-7951', 'Barrio El Centro', now()) ON CONFLICT (id) DO UPDATE SET direccion = excluded.direccion`, cliente)
	exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, router_nombre, ip, direccion, lat, lon, precio_mensual, moneda, sincronizado_en)
		VALUES ($1, 1, $2, 'S-7951', 'Fibra 50', 'activo', 'Chitá', 'NODO-SECRETO', '100.64.79.10', 'CALLE-SECRETA 7', 14.6, -90.5, 250.00, 'GTQ', now())`, servicio, cliente)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM integracion.argos_copias WHERE cliente_id = $1`, cliente)
		_, _ = pool.Exec(ctx, `DELETE FROM integracion.argos_servicios WHERE cliente_id = $1`, cliente)
	})

	// Un rol propio con lo del técnico menos servicios.ver_*.
	rol := "tecnico_sin_red_" + pruebas.Unico()
	exec(`WITH r AS (INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, 'Técnico sin red') RETURNING id)
		INSERT INTO nucleo.roles_permisos (rol_id, permiso)
		SELECT r.id, rp.permiso FROM r, nucleo.roles_permisos rp JOIN nucleo.roles b ON b.id = rp.rol_id
		WHERE b.codigo = 'tecnico' AND rp.permiso NOT LIKE 'servicios.%'`, rol)

	d := entrar(t, base, "despacho")
	tec := entrar(t, base, "tecnico")
	sinRed := entrar(t, base, rol)
	conAgenda(t, sinRed.id)
	crear := func(tecnico int64) int64 {
		t.Helper()
		r := d.pedir(http.MethodPost, "/api/v1/ordenes", map[string]any{"tipo": "visita", "direccion": "Zona 1", "zonaId": 3}, "Idempotency-Key", "orden-"+pruebas.Unico())
		if r.status != http.StatusCreated {
			t.Fatalf("crear: %d %s", r.status, r.crudo)
		}
		id := int64(r.cuerpo["id"].(float64))
		exec(`UPDATE campo.ordenes SET cliente_id = $2, servicio_id = $3 WHERE id = $1`, id, cliente, servicio)
		if r := d.pedir(http.MethodPost, fmt.Sprintf("/api/v1/ordenes/%d/programar", id), map[string]any{"version": r.cuerpo["version"], "tecnicoId": tecnico,
			"inicio": "2026-10-28T16:00:00Z"}); r.status != http.StatusOK {
			t.Fatalf("programar: %d %s", r.status, r.crudo)
		}
		return id
	}
	a := crear(tec.id)
	b := crear(sinRed.id)

	servicioDelPaquete := func(c *clienteHTTP, orden int64) (map[string]any, string) {
		t.Helper()
		p := c.pedir(http.MethodGet, "/api/v1/campo/paquete?fecha=2026-10-28", nil)
		if p.status != http.StatusOK {
			t.Fatalf("paquete: %d %s", p.status, p.crudo)
		}
		for _, o := range p.cuerpo["ordenes"].([]any) {
			m := o.(map[string]any)
			if int64(m["id"].(float64)) == orden {
				s, _ := m["servicio"].(map[string]any)
				return s, p.cuerpo["cursor"].(string)
			}
		}
		t.Fatalf("la orden %d no está en el paquete: %s", orden, p.crudo)
		return nil, ""
	}
	// El técnico (servicios.ver_*), en su orden: la red y la ubicación, con plan, zona y precio.
	s, _ := servicioDelPaquete(tec, a)
	if s["ip"] != "100.64.79.10" || s["router"] != "NODO-SECRETO" || s["direccion"] != "CALLE-SECRETA 7" || s["zona"] != "Chitá" || s["precioMensual"] != "250.00" {
		t.Fatalf("paquete del técnico: %v", s)
	}
	// Sin servicios.ver_*: el servidor omite.
	s, cursor := servicioDelPaquete(sinRed, b)
	for _, campo := range []string{"ip", "router", "direccion", "lat", "lon"} {
		if _, ok := s[campo]; ok {
			t.Fatalf("paquete sin servicios.ver_*: no debía venir %q: %v", campo, s)
		}
	}
	if s["plan"] != "Fibra 50" || s["precioMensual"] != "250.00" {
		t.Fatalf("plan y precio sí: %v", s)
	}
	// Los cambios, igual.
	l := sinRed.pedir(http.MethodPost, "/api/v1/sync/lote", map[string]any{"dispositivoId": "pwa-sr", "plataforma": "pwa", "operaciones": []map[string]any{
		{"id": opID(), "tipo": "orden.iniciar_camino", "ordenId": b, "versionBase": 2, "ocurridoEn": "2026-10-28T15:30:00Z"},
	}})
	if l.status != http.StatusOK {
		t.Fatalf("lote: %d %s", l.status, l.crudo)
	}
	c := sinRed.pedir(http.MethodGet, "/api/v1/campo/cambios?cursor="+cursor, nil)
	if c.status != http.StatusOK || len(c.cuerpo["cambios"].([]any)) == 0 {
		t.Fatalf("cambios: %d %s", c.status, c.crudo)
	}
	for _, x := range []string{"100.64.79.10", "NODO-SECRETO", "CALLE-SECRETA"} {
		if strings.Contains(string(c.crudo), x) {
			t.Fatalf("los cambios traen %q: %s", x, c.crudo)
		}
	}

	// GET /ordenes/{id} y GET /tickets/{id}: la copia de Argos recortada.
	copia := `{"cliente": {"id": 7951, "codigo": "C-7951", "nombre": "Rosa Pérez", "direccion": "Barrio El Centro", "lat": null, "lon": null, "zonaNombre": "Chitá", "estado": "activo"},
		"servicio": {"id": 79510, "planNombre": "Fibra 50", "estado": "activo", "direccion": "CALLE-SECRETA 7", "lat": 14.6, "lon": -90.5, "ip": "100.64.79.10", "routerNombre": "NODO-SECRETO", "zonaNombre": "Chitá"},
		"direccion": "CALLE-SECRETA 7", "completa": true, "anonimizada": false}`
	exec(`INSERT INTO integracion.argos_copias (entidad, entidad_id, empresa_id, cliente_id, servicio_id, datos, sincronizado_en) VALUES ('orden', $1, NULL, $2, $3, $4::jsonb, now())`, a, cliente, servicio, copia)
	r := d.pedir(http.MethodGet, fmt.Sprintf("/api/v1/ordenes/%d", a), nil)
	if r.status != http.StatusOK {
		t.Fatalf("detalle: %d %s", r.status, r.crudo)
	}
	da, _ := r.cuerpo["datosArgos"].(map[string]any)
	if da == nil {
		t.Fatalf("detalle sin datosArgos: %s", r.crudo)
	}
	// Despacho: ve la ubicación, no la red.
	sv := da["servicio"].(map[string]any)
	if _, ok := sv["ip"]; ok || sv["routerNombre"] != nil || sv["direccion"] != "CALLE-SECRETA 7" {
		t.Fatalf("copia para despacho: %v", sv)
	}
	// Lectura: ni red ni ubicación; la dirección que aplica vuelve a ser la del cliente.
	lec := entrar(t, base, "lectura")
	r = lec.pedir(http.MethodGet, fmt.Sprintf("/api/v1/ordenes/%d", a), nil)
	if r.status != http.StatusOK || strings.Contains(string(r.crudo), "100.64.79.10") || strings.Contains(string(r.crudo), "NODO-SECRETO") {
		t.Fatalf("detalle para lectura: %d %s", r.status, r.crudo)
	}
	if da := r.cuerpo["datosArgos"].(map[string]any); da["direccion"] != "Barrio El Centro" {
		t.Fatalf("lectura: la dirección que aplica debía ser la del cliente: %v", da)
	}

	tk := d.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Sin internet"})
	if tk.status != http.StatusCreated {
		t.Fatalf("ticket: %d %s", tk.status, tk.crudo)
	}
	ticket := int64(tk.cuerpo["id"].(float64))
	exec(`INSERT INTO integracion.argos_copias (entidad, entidad_id, empresa_id, cliente_id, servicio_id, datos, sincronizado_en) VALUES ('ticket', $1, NULL, $2, $3, $4::jsonb, now())`, ticket, cliente, servicio, copia)
	r = lec.pedir(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d", ticket), nil)
	if r.status != http.StatusOK {
		t.Fatalf("ticket para lectura: %d %s", r.status, r.crudo)
	}
	for _, x := range []string{"100.64.79.10", "NODO-SECRETO", "CALLE-SECRETA"} {
		if strings.Contains(string(r.crudo), x) {
			t.Fatalf("GET /tickets/{id} trae %q a lectura: %s", x, r.crudo)
		}
	}
	if r = d.pedir(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d", ticket), nil); r.status != http.StatusOK || !strings.Contains(string(r.crudo), "CALLE-SECRETA") || strings.Contains(string(r.crudo), "100.64.79.10") {
		t.Fatalf("ticket para despacho (ubicación sí, red no): %d %s", r.status, r.crudo)
	}
}
