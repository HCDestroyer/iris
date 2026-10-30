package campo_test

import (
	"context"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Datos de Argos coherentes (pedido 5 del PM): la orden y el ticket toman la dirección de
// instalación y el teléfono del servicio que corresponde (en Argos el teléfono es del cliente: el
// móvil válido primero, en formato +502) y conservan su copia aunque Argos cambie después.
func TestDatosArgosDeTicketYOrden(t *testing.T) {
	registrarEmpresas(t, 64)
	e := nuevoEntorno()
	d := despacho(t)
	asignarEmpresa(t, d, 64)
	ctx := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), d, 64, append(permisosDe("despacho"), permisosDe("soporte")...)...)
	pool := pruebas.Pool(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, movil, direccion, lat, lon,
			nit_cui, tipo_documento_fiscal, sincronizado_en, version_datos)
		VALUES (7811, 64, '{64}', 'Rosa Ajú', 'C-007811', '2233-4455', '5512 3344', 'Casa de Rosa, zona 3', 14.6, -90.5,
			'12345678', 'nit', now(), 2)`)
	exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, ip, direccion, lat, lon, estado, sincronizado_en)
		VALUES (78110, 64, 7811, 'Fibra 100', '100.64.1.9', 'Lote 9, aldea Las Flores', 14.7, -90.6, 'activo', now()),
		       (78111, 64, 7811, 'Fibra 50', '100.64.1.10', '', null, null, 'activo', now())`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE cliente_id = 7811")
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id = 7811")
	})

	// Ticket sin contacto capturado: guarda la copia de Argos y el detalle la devuelve.
	sop := soporte.Nuevo(soporte.Config{})
	tk := deber(sop.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Sin señal", ClienteID: ptr(int64(7811)), ServicioID: ptr(int64(78110))}))(t)
	vt := deber(sop.VerTicket.Ejecutar(ctx, soporte.EntradaVerTicket{ID: tk.ID}))(t)
	if c := vt.DatosArgos; c == nil || c.Direccion != "Lote 9, aldea Las Flores" || c.Telefono == nil || c.Telefono.Mostrar != "+502 5512 3344" ||
		c.Servicio == nil || c.Servicio.IP != "100.64.1.9" || c.Cliente.Direccion != "Casa de Rosa, zona 3" || c.Cliente.Nombre != "Rosa Ajú" {
		t.Fatalf("copia del ticket: %+v", vt.DatosArgos)
	}

	// Orden desde ese ticket (sin contacto): dirección y coordenadas de instalación del servicio y
	// el teléfono principal del cliente; antes quedaba sin teléfono.
	o := deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk.ID}))(t)
	if o.Direccion != "Lote 9, aldea Las Flores" || o.Lat == nil || *o.Lat != 14.7 || o.Contacto.Nombre == nil || *o.Contacto.Nombre != "Rosa Ajú" ||
		o.Contacto.Telefono == nil || *o.Contacto.Telefono != "+502 5512 3344" {
		t.Fatalf("orden desde el ticket: %+v (contacto %v %v)", o, deref(o.Contacto.Nombre), deref(o.Contacto.Telefono))
	}
	// Un servicio sin dirección propia: aplica la del cliente.
	o2 := deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: "visita", ClienteID: ptr(int64(7811)), ServicioID: ptr(int64(78111))}))(t)
	if o2.Direccion != "Casa de Rosa, zona 3" || o2.Lat == nil || *o2.Lat != 14.6 {
		t.Fatalf("servicio sin dirección de instalación: %+v", o2)
	}
	// Contacto indicado a mano: se respeta, sin completar el teléfono del cliente.
	o3 := deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: "visita", ClienteID: ptr(int64(7811)), ServicioID: ptr(int64(78110)),
		Contacto: &campo.EntradaContactoOrden{Nombre: ptr("Pedro, vecino")}}))(t)
	if o3.Contacto.Telefono != nil || *o3.Contacto.Nombre != "Pedro, vecino" {
		t.Fatalf("contacto a mano: %v %v", deref(o3.Contacto.Nombre), deref(o3.Contacto.Telefono))
	}

	// Argos cambia la dirección y el teléfono: la orden y su copia conservan los de cuando se creó.
	exec(`UPDATE integracion.argos_servicios SET direccion = 'Lote 10' WHERE id = 78110`)
	exec(`UPDATE integracion.argos_clientes SET movil = '', telefono = '7777-0000' WHERE id = 7811`)
	vo := deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t)
	if vo.Direccion != "Lote 9, aldea Las Flores" || vo.DatosArgos == nil || vo.DatosArgos.Direccion != "Lote 9, aldea Las Flores" ||
		vo.DatosArgos.Telefono == nil || vo.DatosArgos.Telefono.Mostrar != "+502 5512 3344" || vo.DatosArgos.Servicio.PlanNombre != "Fibra 100" {
		t.Fatalf("copia de la orden: %+v", vo.DatosArgos)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.argos_copias WHERE cliente_id = 7811"); n != 4 {
		t.Fatalf("una copia por ticket y por orden: %d", n)
	}
	// Sin clientes.ver la copia no sale (datos personales, como la ficha).
	sinFicha := pruebas.ContextoUsuarioEnEmpresa(pool, d, 64, nucleo.PermOrdenesVer, nucleo.PermTicketsVer)
	if v := deber(e.m.VerOrden.Ejecutar(sinFicha, campo.EntradaVerOrden{ID: o.ID}))(t); v.DatosArgos != nil {
		t.Fatal("sin clientes.ver no se entrega la copia de la orden")
	}
	if v := deber(sop.VerTicket.Ejecutar(sinFicha, soporte.EntradaVerTicket{ID: tk.ID}))(t); v.DatosArgos != nil {
		t.Fatal("sin clientes.ver no se entrega la copia del ticket")
	}
	// Una orden sin cliente de Argos no tiene copia.
	o4 := deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("Calle sin cliente")}))(t)
	if v := deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o4.ID}))(t); v.DatosArgos != nil {
		t.Fatalf("sin cliente no hay copia: %+v", v.DatosArgos)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// B2: CrearOrden asegura la instantánea del cliente y del servicio en Argos antes de su
// transacción (para usuarios con el permiso; nunca para el sistema) y un fallo corta la orden.
func TestCrearOrdenAseguraInstantanea(t *testing.T) {
	var llamadas []int64
	falla := error(nil)
	e := nuevoEntorno(func(c *campo.Config) {
		c.AsegurarArgos = func(_ context.Context, cliente int64, servicio *int64) error {
			llamadas = append(llamadas, cliente)
			return falla
		}
	})
	d := despacho(t)
	cd := como(t, d, "despacho")
	falla = nucleo.ReglaNegocio("argos_no_disponible", "Argos no respondió")
	_, err := e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ClienteID: ptr(int64(8801)), ServicioID: ptr(int64(88011))})
	if err != falla || len(llamadas) != 1 || llamadas[0] != 8801 {
		t.Fatalf("debía asegurar y cortar: %v %v", err, llamadas)
	}
	// Sin el permiso, no se consulta Argos (el caso responde 403).
	sinPermiso := pruebas.ContextoUsuario(pruebas.Pool(t), d, nucleo.PermOrdenesVer)
	_, err = e.m.CrearOrden.Ejecutar(sinPermiso, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ClienteID: ptr(int64(8801))})
	problema(t, err, 403, "prohibido")
	// El sistema (retiro, alta comercial) no consulta.
	falla = nil
	deber(e.m.CrearOrden.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ClienteID: ptr(int64(8801))}))(t)
	if len(llamadas) != 1 {
		t.Fatalf("sin permiso o como sistema no debía consultar: %v", llamadas)
	}
}
