package argos_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// sembrarComoArgos14 siembra un cliente y su servicio con la forma exacta de Argos 1.4.0
// (core/esquemas.ts: Cliente y Servicio): sin estado de cliente, email, nit_cui sin guiones,
// teléfono y móvil como texto libre, coordenadas "lat,lng", y el servicio sin código, con la IP en
// ipv4, sin zona_id (zona_nombre es la de su cliente) y con su dirección de instalación.
func (e *entorno) sembrarComoArgos14(id int64) {
	e.argos.con(func() {
		e.argos.clientes[id] = map[string]any{
			"id": id, "codigo": "C-000455", "codigo_externo": nil, "nombre": "Hilda Pérez", "tipo": "persona",
			"tipo_documento_fiscal": "nit", "nit_cui": "12345678", "nombre_fiscal": nil, "email": "hilda@ejemplo.gt",
			"telefono": "2233-4455 / 2233-4456", "movil": "5512 3344", "direccion": "3a calle 4-10 zona 1, Mixco",
			"coordenadas": "14.63491234567,-90.50690000001", "zona_id": 3, "canal_preferido": "correo",
		}
		e.argos.servicios[id] = []map[string]any{
			{
				"id": id * 10, "cliente_id": id, "empresa_id": 1, "router_id": 4, "plan_id": 8, "estado": "suspendido",
				"tipo_conexion": "pppoe", "user_ppp": "hilda.perez", "direccion": "Lote 5, aldea El Durazno",
				"coordenadas": "14.7000001, -90.6", "ipv4": "100.64.10.5", "cliente_nombre": "Hilda Pérez",
				"plan_nombre": "Fibra 100", "router_nombre": "RT-Norte", "zona_nombre": "Norte",
			},
			{
				"id": id*10 + 1, "cliente_id": id, "empresa_id": 1, "router_id": 4, "plan_id": 9, "estado": "activo",
				"tipo_conexion": "ip_estatica", "user_ppp": nil, "direccion": nil, "coordenadas": nil, "ipv4": "100.64.10.6",
				"cliente_nombre": "Hilda Pérez", "plan_nombre": "Fibra 50", "router_nombre": "RT-Norte", "zona_nombre": "Norte",
			},
		}
		e.argos.estados[id] = map[string]any{"saldo": "0.00", "moneda": "GTQ", "facturas_vencidas": 0}
	})
}

func mapa(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("no es un objeto: %#v", v)
	}
	return m
}

// La ficha expone todo lo que Argos 1.4.0 manda del cliente y del servicio, con la dirección del
// cliente separada de la de instalación, los teléfonos normalizados sin perder el original, el NIT
// para mostrar, las coordenadas a 6 decimales, la IP (ipv4), la zona del cliente y el estado
// resumido (Argos no tiene estado de cliente).
func TestFichaDatosCoherentesArgos14(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(455)
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en)
		VALUES (3, 1, 'Norte', now()) ON CONFLICT (id) DO UPDATE SET nombre = 'Norte'`); err != nil {
		t.Fatal(err)
	}
	f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/455", nil)
	if f.status != 200 {
		t.Fatalf("ficha: %d %s", f.status, f.crudo)
	}
	cli := mapa(t, f.cuerpo["cliente"])
	if cli["codigo"] != "C-000455" || cli["tipo"] != "persona" || cli["nitCui"] != "12345678" || cli["nitCuiMostrar"] != "1234567-8" ||
		cli["correo"] != "hilda@ejemplo.gt" || cli["movil"] != "5512 3344" || cli["telefono"] != "2233-4455 / 2233-4456" ||
		cli["direccion"] != "3a calle 4-10 zona 1, Mixco" || cli["zonaId"] != float64(3) || cli["zonaNombre"] != "Norte" ||
		cli["lat"] != 14.634912 || cli["lon"] != -90.5069 || cli["estado"] != "suspendido" || cli["sincronizadoEn"] == nil {
		t.Fatalf("cliente: %s", f.crudo)
	}
	tels := cli["telefonos"].([]any)
	if len(tels) != 3 {
		t.Fatalf("teléfonos (dos del campo teléfono y el móvil): %v", tels)
	}
	movil := mapa(t, tels[2])
	if movil["tipo"] != "movil" || movil["original"] != "5512 3344" || movil["e164"] != "+50255123344" || movil["mostrar"] != "+502 5512 3344" {
		t.Fatalf("móvil: %v", movil)
	}
	if fijo := mapa(t, tels[1]); fijo["original"] != "2233-4456" || fijo["mostrar"] != "+502 2233 4456" {
		t.Fatalf("segundo teléfono: %v", fijo)
	}
	servicios := f.cuerpo["servicios"].([]any)
	if len(servicios) != 2 {
		t.Fatalf("servicios: %s", f.crudo)
	}
	s := mapa(t, servicios[0])
	if s["ip"] != "100.64.10.5" || s["direccion"] != "Lote 5, aldea El Durazno" || s["lat"] != 14.7 || s["lon"] != -90.6 ||
		s["userPpp"] != "hilda.perez" || s["tipoConexion"] != "pppoe" || s["zonaId"] != float64(3) || s["zonaNombre"] != "Norte" ||
		s["planNombre"] != "Fibra 100" || s["routerNombre"] != "RT-Norte" || s["estado"] != "suspendido" || s["sincronizadoEn"] == nil {
		t.Fatalf("servicio con dirección de instalación: %v", s)
	}
	// Sin dirección propia: vacía (aplica la del cliente; Iris no la inventa).
	if s2 := mapa(t, servicios[1]); s2["direccion"] != "" || s2["lat"] != nil || s2["ip"] != "100.64.10.6" || s2["userPpp"] != "" {
		t.Fatalf("servicio sin dirección propia: %v", s2)
	}
	// La IP (ipv4) queda en la instantánea: la resolución local por IP (Argos caído) la usa.
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE ip = '100.64.10.5' AND user_ppp = 'hilda.perez'`); n != 1 {
		t.Fatal("la IP del servicio no quedó en la instantánea")
	}
}

// Las filas guardadas antes de esta versión (version_datos = 1) se releen aunque sean recientes,
// y ?refrescar=true relee salvo que la instantánea sea de hace menos de 30 s.
func TestFichaVersionDatosYRefrescar(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(456)
	lectura := e.usuario("lectura")
	if f := lectura.pedir(http.MethodGet, "/api/v1/clientes/456", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	llamadas := func() int { return e.argos.llamadasA("/api/v1/clientes/456") }
	antes := llamadas()
	lectura.pedir(http.MethodGet, "/api/v1/clientes/456?refrescar=true", nil)
	if llamadas() != antes {
		t.Fatal("refrescar con una instantánea de hace menos de 30 s no debía llamar a Argos")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 456 AND version_datos = $1`, argos.VersionDatos); n != 1 {
		t.Fatal("la fila nueva lleva la versión actual de los datos")
	}
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '1 minute' WHERE id = 456`)
	e.argos.con(func() { e.argos.clientes[456]["movil"] = "4444-1111" })
	lectura.pedir(http.MethodGet, "/api/v1/clientes/456", nil)
	if llamadas() != antes {
		t.Fatal("sin refrescar, una instantánea de 1 minuto no se relee")
	}
	r := lectura.pedir(http.MethodGet, "/api/v1/clientes/456?refrescar=true", nil)
	if llamadas() != antes+1 || mapa(t, r.cuerpo["cliente"])["movil"] != "4444-1111" {
		t.Fatalf("refrescar: %d llamadas, %s", llamadas()-antes, r.crudo)
	}
	// Una fila de antes de los datos coherentes se relee aunque sea reciente.
	e.exec(`UPDATE integracion.argos_clientes SET version_datos = 1, sincronizado_en = now() WHERE id = 456`)
	lectura.pedir(http.MethodGet, "/api/v1/clientes/456", nil)
	if llamadas() != antes+2 {
		t.Fatal("una fila de una versión anterior de los datos debía releerse")
	}
	// Refrescar con Argos caído: la última instantánea, marcada stale.
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '1 minute' WHERE id = 456`)
	e.argos.con(func() { e.argos.caido = true })
	if s := lectura.pedir(http.MethodGet, "/api/v1/clientes/456?refrescar=true", nil); s.status != 200 || s.cuerpo["stale"] != true {
		t.Fatalf("refrescar con Argos caído: %d %s", s.status, s.crudo)
	}
}

// Copias: lo que la instantánea tiene al crear un ticket o una orden se conserva tal cual; la
// dirección que aplica es la de instalación del servicio (o la del cliente si no tiene) y el
// teléfono, el principal (móvil válido primero).
func TestCopiaDeDatos(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(457)
	if f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/457", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	ctx := pruebas.ContextoSistema(e.pool)
	ahora := time.Now()
	srv := int64(4570)
	empresa := int64(1)
	c, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaTicket, 900001, 457, &srv, &empresa, ahora)
	if err != nil || c == nil {
		t.Fatalf("copia: %v %v", c, err)
	}
	if c.Direccion != "Lote 5, aldea El Durazno" || c.Servicio == nil || c.Servicio.IP != "100.64.10.5" ||
		c.Telefono == nil || c.Telefono.Mostrar != "+502 5512 3344" || c.Cliente.NitCuiMostrar != "1234567-8" || c.Cliente.Estado != "suspendido" {
		t.Fatalf("copia con servicio: %+v", c)
	}
	sinDir := int64(4571)
	c2, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaOrden, 900001, 457, &sinDir, &empresa, ahora)
	if err != nil || c2.Direccion != "3a calle 4-10 zona 1, Mixco" {
		t.Fatalf("sin dirección de instalación aplica la del cliente: %+v %v", c2, err)
	}
	// Se conserva: cambia Argos (y la instantánea) y la copia queda igual; un segundo CopiarDatos
	// para la misma entidad no la pisa.
	e.exec(`UPDATE integracion.argos_clientes SET movil = '', telefono = '7777-0000' WHERE id = 457`)
	e.exec(`UPDATE integracion.argos_servicios SET direccion = 'Otra' WHERE id = 4570`)
	if _, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaTicket, 900001, 457, &srv, &empresa, ahora.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	leida, err := argos.CopiaDe(ctx, e.pool, argos.CopiaTicket, 900001)
	if err != nil || leida == nil || leida.Direccion != "Lote 5, aldea El Durazno" || leida.Telefono.Mostrar != "+502 5512 3344" ||
		leida.CopiadoEn.IsZero() || leida.SincronizadoEn.IsZero() {
		t.Fatalf("copia conservada: %+v %v", leida, err)
	}
	// Sin instantánea del cliente no hay copia.
	if c, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaTicket, 900002, 999999, nil, nil, ahora); err != nil || c != nil {
		t.Fatalf("sin instantánea: %+v %v", c, err)
	}
	if c, err := argos.CopiaDe(ctx, e.pool, argos.CopiaTicket, 900002); err != nil || c != nil {
		t.Fatalf("sin copia: %+v %v", c, err)
	}
	e.exec(`DELETE FROM integracion.argos_copias WHERE entidad_id IN (900001, 900002)`)
}
