package argos_test

import (
	"context"
	"net/http"
	"testing"
)

// Pruebas de regresión de la revisión de seguridad del conector (rama agente/i1-argos).

func (e *entorno) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// sembrarClienteEn es sembrarCliente en otra empresa (0 = Argos no informa empresa).
func (e *entorno) sembrarClienteEn(id, empresa int64, nombre string) {
	e.sembrarCliente(id, nombre)
	e.argos.con(func() {
		e.argos.clientes[id]["empresaId"] = empresa
		for _, s := range e.argos.servicios[id] {
			s["empresa_id"] = empresa
		}
	})
}

// Hallazgo 1 (ALTO): un usuario solo ve clientes de su empresa activa en la búsqueda, la ficha
// y resolver IP, también desde las instantáneas; sin empresa no lee nada.
func TestMultiempresaConector(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.exec(`INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2')`)
	e.sembrarClienteEn(61, 1, "Ana Norte")
	e.sembrarClienteEn(62, 2, "Ana Sur")
	e.sembrarClienteEn(63, 0, "Ana Sin Empresa")
	e.argos.con(func() {
		e.argos.ips["10.0.0.61"] = map[string]any{"cliente_id": 61, "servicio_id": 610, "empresa_id": 1}
		e.argos.ips["10.0.0.62"] = map[string]any{"cliente_id": 62, "servicio_id": 620, "empresa_id": 2}
		e.argos.ips["10.0.0.63"] = map[string]any{"cliente_id": 63, "servicio_id": 630}
	})
	uno := e.usuarioEn([]int64{1}, "soporte")

	// Búsqueda: solo la empresa activa; lo que llega sin empresa se descarta.
	b := uno.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil)
	if b.status != 200 || len(b.cuerpo["data"].([]any)) != 1 || b.cuerpo["data"].([]any)[0].(map[string]any)["id"] != float64(61) {
		t.Fatalf("búsqueda multiempresa: %d %s", b.status, b.crudo)
	}
	// Ficha de otra empresa: 404 y sin pedir su estado de cuenta.
	uno.pedir(http.MethodGet, "/api/v1/clientes/62", nil).problema(t, 404, "no_encontrado")
	uno.pedir(http.MethodGet, "/api/v1/clientes/63", nil).problema(t, 404, "no_encontrado")
	if n := e.argos.llamadasA("/api/v1/clientes/62/estado-cuenta"); n != 0 {
		t.Fatalf("pidió el estado de cuenta de un cliente ajeno (%d veces)", n)
	}
	if f := uno.pedir(http.MethodGet, "/api/v1/clientes/61", nil); f.status != 200 || f.cuerpo["estadoCuenta"] == nil {
		t.Fatalf("ficha propia: %d %s", f.status, f.crudo)
	}
	// Resolver IP: la de otra empresa (o sin empresa) es «IP sin cliente».
	uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.62", nil).problema(t, 404, "no_encontrado")
	uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.63", nil).problema(t, 404, "no_encontrado")
	if r := uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.61", nil); r.status != 200 || r.cuerpo["clienteId"] != float64(61) {
		t.Fatalf("resolver IP propia: %d %s", r.status, r.crudo)
	}

	// Con dos empresas hay que elegir; con la cabecera, la otra empresa sí se ve.
	dos := e.usuarioEn([]int64{1, 2}, "soporte")
	dos.pedir(http.MethodGet, "/api/v1/clientes/62", nil).problema(t, 422, "empresa_requerida")
	if f := dos.pedir(http.MethodGet, "/api/v1/clientes/62", nil, "X-Iris-Empresa", "2"); f.status != 200 {
		t.Fatalf("ficha con X-Iris-Empresa: %d %s", f.status, f.crudo)
	}
	dos.pedir(http.MethodGet, "/api/v1/clientes/62", nil, "X-Iris-Empresa", "3").problema(t, 403, "empresa_no_permitida")

	// Con Argos caído, las instantáneas también se filtran (la de 62 quedó guardada).
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 62`); n != 1 {
		t.Fatalf("instantánea de 62: %d", n)
	}
	e.argos.con(func() { e.argos.caido = true })
	bl := uno.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil)
	if bl.status != 200 || bl.cuerpo["stale"] != true || len(bl.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("búsqueda local multiempresa: %d %s", bl.status, bl.crudo)
	}
	uno.pedir(http.MethodGet, "/api/v1/clientes/62", nil).problema(t, 404, "no_encontrado")
	if r := uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.62", nil); r.status == 200 {
		t.Fatalf("resolver IP desde la instantánea de otra empresa: %s", r.crudo)
	}
	e.argos.con(func() { e.argos.caido = false })

	// Sin empresas asignadas (antes veía todo): nada.
	nadie := e.usuarioEn(nil, "tecnico")
	nadie.pedir(http.MethodGet, "/api/v1/clientes/61", nil).problema(t, 403, "sin_empresa")
	nadie.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil).problema(t, 403, "sin_empresa")
	nadie.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.61", nil).problema(t, 403, "sin_empresa")

	// Conector activo sin empresas registradas: no hay cómo filtrar, 422.
	e.exec(`DELETE FROM nucleo.usuarios_empresas`)
	e.exec(`DELETE FROM nucleo.empresas`)
	nadie.pedir(http.MethodGet, "/api/v1/clientes/61", nil).problema(t, 422, "empresas_no_registradas")
}
