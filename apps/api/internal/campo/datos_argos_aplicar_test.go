package campo_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Regresión (#81, encontrada por I3): OrdenDetalle declaraba su propio DatosArgos (la copia,
// `datosArgos`) y tapaba Orden.DatosArgos (`datosArgosCambiados`): GET /ordenes/{id} y su OpenAPI
// deben llevar los dos.
func TestHTTPDetalleConDatosArgosCambiados(t *testing.T) {
	base := nuevoServidor(t)
	d := entrar(t, base, "despacho")
	r := d.pedir(http.MethodPost, "/api/v1/ordenes", map[string]any{"tipo": "visita", "direccion": "Zona 1", "zonaId": 3})
	if r.status != http.StatusCreated {
		t.Fatalf("crear: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE campo.ordenes SET datos_argos_cambiados_en = now(),
		datos_argos_campos = '{servicio.direccion,servicio.referencia}' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	r = d.pedir(http.MethodGet, "/api/v1/ordenes/"+strconv.FormatInt(id, 10), nil)
	marca, _ := r.cuerpo["datosArgosCambiados"].(map[string]any)
	if r.status != http.StatusOK || marca == nil || len(marca["campos"].([]any)) != 2 || marca["en"] == nil {
		t.Fatalf("GET /ordenes/{id} sin datosArgosCambiados: %d %s", r.status, r.crudo)
	}
	o := d.pedir(http.MethodGet, "/api/v1/openapi.json", nil)
	esquemas, _ := o.cuerpo["components"].(map[string]any)["schemas"].(map[string]any)
	props, _ := esquemas["OrdenDetalle"].(map[string]any)["properties"].(map[string]any)
	if props["datosArgosCambiados"] == nil || props["datosArgos"] == nil {
		t.Fatalf("OpenAPI de OrdenDetalle sin datosArgosCambiados o datosArgos: %v", keys(props))
	}
}

func keys(m map[string]any) []string {
	salida := []string{}
	for k := range m {
		salida = append(salida, k)
	}
	return salida
}

// Aplicar los datos nuevos de Argos (dirección, coordenadas, referencia, contacto) a la orden abierta
// o descartar el aviso: quitan la marca, suben la versión, auditan el diff y emiten su evento.
func TestAplicarYDescartarDatosArgos(t *testing.T) {
	registrarEmpresas(t, 65)
	e := nuevoEntorno()
	d := despacho(t)
	asignarEmpresa(t, d, 65)
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoUsuarioEnEmpresa(pool, d, 65, permisosDe("despacho")...)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, movil, direccion, lat, lon,
			referencia, nit_cui, tipo_documento_fiscal, sincronizado_en, version_datos)
		VALUES (7911, 65, '{65}', 'Lucía Pop', 'C-007911', '2233-4455', '5512 3344', 'Casa de Lucía', 14.6, -90.5,
			'Portón negro', '12345678', 'nit', now(), 2)`)
	exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, ip, direccion, lat, lon, referencia,
			contacto_nombre, contacto_telefono, estado, sincronizado_en)
		VALUES (79110, 65, 7911, 'Fibra 100', '100.64.2.9', 'Lote 9', 14.7, -90.6, 'Frente a la escuela', '', '', 'activo', now())`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE cliente_id = 7911")
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id = 7911")
	})
	o := deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: "visita", ClienteID: ptr(int64(7911)), ServicioID: ptr(int64(79110))}))(t)
	if o.Referencia == nil || *o.Referencia != "Frente a la escuela" || o.Direccion != "Lote 9" {
		t.Fatalf("la orden copia la referencia al crearse: %+v", o)
	}

	// Sin aviso no hay nada que aplicar.
	_, err := e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: o.Version, AvisoEn: o.CreadoEn})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "sin_aviso_argos" {
		t.Fatalf("sin aviso: %v", err)
	}

	// Argos cambia el servicio (y el contacto del cliente) y avisa: la orden queda marcada.
	exec(`UPDATE integracion.argos_servicios SET direccion = 'Lote 10', lat = 14.8, lon = -90.7, referencia = 'Casa azul',
		contacto_nombre = 'Marta', contacto_telefono = '5599 0000' WHERE id = 79110`)
	deber(e.m.MarcarDatosArgos.Ejecutar(pruebas.ContextoSistema(pool), campo.EntradaDatosArgos{
		Entidad: "servicio", ServicioID: ptr(int64(79110)), Campos: []string{"servicio.direccion", "servicio.contacto_telefono"}, EntranteID: 9}))(t)
	v := deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	if v.DatosArgosCambiados == nil {
		t.Fatal("marcada")
	}
	leido := v.DatosArgosCambiados.En
	// Un aviso repetido no cambia la marca; uno con un campo nuevo (después de que despacho leyó), sí:
	// atender con lo leído da 409 aviso_argos_cambiado y no se pierde.
	deber(e.m.MarcarDatosArgos.Ejecutar(pruebas.ContextoSistema(pool), campo.EntradaDatosArgos{
		Entidad: "servicio", ServicioID: ptr(int64(79110)), Campos: []string{"servicio.direccion"}, EntranteID: 9}))(t)
	if r := deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t).Orden; !r.DatosArgosCambiados.En.Equal(leido) {
		t.Fatal("aviso repetido: la marca no cambia")
	}
	deber(e.m.MarcarDatosArgos.Ejecutar(pruebas.ContextoSistema(pool), campo.EntradaDatosArgos{
		Entidad: "servicio", ServicioID: ptr(int64(79110)), Campos: []string{"servicio.coordenadas"}, EntranteID: 12}))(t)
	for _, err := range []error{
		func() error {
			_, err := e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: v.Version, AvisoEn: leido})
			return err
		}(),
		func() error {
			_, err := e.m.DescartarDatosArgos.Ejecutar(ctx, campo.EntradaDescartarDatosArgos{ID: o.ID, Version: v.Version, AvisoEn: leido})
			return err
		}(),
	} {
		if p, _ := nucleo.AProblema(err); p == nil || p.Status != 409 || p.Codigo() != "aviso_argos_cambiado" {
			t.Fatalf("aviso nuevo sin ver: %v", err)
		}
	}
	v = deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	if !v.DatosArgosCambiados.En.After(leido) || len(v.DatosArgosCambiados.Campos) != 3 {
		t.Fatalf("la marca nueva sigue: %+v", v.DatosArgosCambiados)
	}

	// Versión vieja: 409.
	_, err = e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: v.Version - 1, AvisoEn: v.DatosArgosCambiados.En})
	if p, _ := nucleo.AProblema(err); p == nil || p.Status != 409 {
		t.Fatalf("versión vieja: %v", err)
	}
	// Sin el permiso: 403.
	if _, err := e.m.AplicarDatosArgos.Ejecutar(pruebas.ContextoUsuarioEnEmpresa(pool, d, 65, nucleo.PermOrdenesVer),
		campo.EntradaAplicarDatosArgos{ID: o.ID, Version: v.Version, AvisoEn: v.DatosArgosCambiados.En}); err == nil {
		t.Fatal("sin ordenes.aplicar_datos_argos no se aplica")
	}

	// Solo dirección y contacto: las coordenadas y la referencia se quedan, y del aviso queda solo
	// servicio.coordenadas (lo no aplicado sigue avisado, con el mismo en).
	a := deber(e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: v.Version,
		AvisoEn: v.DatosArgosCambiados.En, Campos: []string{"contacto", "direccion"}}))(t)
	if a.Direccion != "Lote 10" || *a.Lat != 14.7 || *a.Referencia != "Frente a la escuela" || a.Contacto.Nombre == nil || *a.Contacto.Nombre != "Marta" ||
		a.Contacto.Telefono == nil || *a.Contacto.Telefono != "+502 5599 0000" || a.Version != v.Version+1 ||
		a.DatosArgosCambiados == nil || fmt.Sprint(a.DatosArgosCambiados.Campos) != "[servicio.coordenadas]" || !a.DatosArgosCambiados.En.Equal(v.DatosArgosCambiados.En) {
		t.Fatalf("aplicado: %+v (contacto %v %v)", a, deref(a.Contacto.Nombre), deref(a.Contacto.Telefono))
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.datos_argos_aplicados' AND (payload->>'orden_id')::bigint = $1
		AND payload->'cambiados' = '["direccion","contacto"]'::jsonb AND payload->'campos' = '["contacto","direccion"]'::jsonb
		AND payload->'campos_argos' = '["servicio.contacto_telefono","servicio.coordenadas","servicio.direccion"]'::jsonb
		AND payload->'campos_restantes' = '["servicio.coordenadas"]'::jsonb`, o.ID); n != 1 {
		t.Fatalf("evento orden.datos_argos_aplicados: %d", n)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id
		WHERE a.accion = 'ordenes.aplicar_datos_argos' AND c.entidad = 'orden' AND c.entidad_id = $1
		AND c.campo IN ('direccion', 'contacto_nombre', 'contacto_telefono', 'datos_argos_campos')`, strconv.FormatInt(o.ID, 10)); n != 4 {
		t.Fatalf("diff por campo en la auditoría: %d", n)
	}
	// Otro aviso: todo (sin campos) aplica coordenadas y referencia y quita la marca entera.
	deber(e.m.MarcarDatosArgos.Ejecutar(pruebas.ContextoSistema(pool), campo.EntradaDatosArgos{
		Entidad: "servicio", ServicioID: ptr(int64(79110)), Campos: []string{"servicio.referencia"}, EntranteID: 10}))(t)
	a = deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	todo := deber(e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: a.Version, AvisoEn: a.DatosArgosCambiados.En}))(t)
	if *todo.Lat != 14.8 || *todo.Lon != -90.7 || *todo.Referencia != "Casa azul" || todo.DatosArgosCambiados != nil {
		t.Fatalf("todo: %+v", todo)
	}
	// Sin aviso ya no se aplica de nuevo.
	_, err = e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: todo.Version, AvisoEn: a.DatosArgosCambiados.En})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "sin_aviso_argos" {
		t.Fatalf("aviso ya atendido: %v", err)
	}

	// Descartar: solo quita la marca.
	exec(`UPDATE integracion.argos_servicios SET direccion = 'Lote 11' WHERE id = 79110`)
	deber(e.m.MarcarDatosArgos.Ejecutar(pruebas.ContextoSistema(pool), campo.EntradaDatosArgos{
		Entidad: "servicio", ServicioID: ptr(int64(79110)), Campos: []string{"servicio.direccion"}, EntranteID: 11}))(t)
	marcada := deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	ds := deber(e.m.DescartarDatosArgos.Ejecutar(ctx, campo.EntradaDescartarDatosArgos{ID: o.ID, Version: todo.Version,
		AvisoEn: marcada.DatosArgosCambiados.En, Motivo: ptr("El técnico ya va en camino a la dirección anterior")}))(t)
	if ds.Direccion != "Lote 10" || ds.DatosArgosCambiados != nil || ds.Version != todo.Version+1 {
		t.Fatalf("descartado: %+v", ds)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.datos_argos_descartados' AND (payload->>'orden_id')::bigint = $1
		AND payload->>'motivo' = 'El técnico ya va en camino a la dirección anterior'`, o.ID); n != 1 {
		t.Fatalf("evento orden.datos_argos_descartados: %d", n)
	}

	// Una orden cerrada (o cancelada) no se toca.
	c := deber(e.m.CancelarOrden.Ejecutar(ctx, campo.EntradaMotivo{ID: o.ID, Version: ds.Version, Motivo: "Duplicada"}))(t)
	exec(`UPDATE campo.ordenes SET datos_argos_cambiados_en = now() WHERE id = $1`, o.ID)
	_, err = e.m.DescartarDatosArgos.Ejecutar(ctx, campo.EntradaDescartarDatosArgos{ID: o.ID, Version: c.Version, AvisoEn: c.CreadoEn})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "transicion_invalida" {
		t.Fatalf("cancelada: %v", err)
	}
}

// La instantánea tiene que seguir siendo de la orden: un servicio que Argos movió a otro cliente (u
// otra empresa) no se aplica; se usa la del cliente si es de la empresa; si nada coincide, 422
// instantanea_no_coincide.
func TestAplicarDatosArgosInstantaneaNoCoincide(t *testing.T) {
	registrarEmpresas(t, 66)
	e := nuevoEntorno()
	d := despacho(t)
	asignarEmpresa(t, d, 66)
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoUsuarioEnEmpresa(pool, d, 66, permisosDe("despacho")...)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, movil, direccion, lat, lon,
			referencia, nit_cui, tipo_documento_fiscal, sincronizado_en, version_datos)
		VALUES (8011, 66, '{66}', 'Ana Coc', 'C-008011', '', '5512 0000', 'Casa de Ana', 14.1, -90.1, 'Esquina', '1', 'nit', now(), 1),
		       (8012, 66, '{66}', 'Otro', 'C-008012', '', '', 'Casa de otro', 14.2, -90.2, 'Otra', '2', 'nit', now(), 1)`)
	exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, ip, direccion, lat, lon, referencia,
			estado, sincronizado_en)
		VALUES (80110, 66, 8011, 'Fibra', '100.64.3.1', 'Lote 1', 14.3, -90.3, 'Junto al río', 'activo', now())`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE id = 80110")
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id IN (8011, 8012)")
	})
	o := deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: "visita", ClienteID: ptr(int64(8011)), ServicioID: ptr(int64(80110))}))(t)
	marcar := func() campo.Orden {
		t.Helper()
		exec(`UPDATE campo.ordenes SET datos_argos_cambiados_en = clock_timestamp(), datos_argos_campos = '{servicio.direccion}' WHERE id = $1`, o.ID)
		return deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	}

	// Argos movió el servicio a otro cliente: se ignora y se usa la del cliente de la orden.
	exec(`UPDATE integracion.argos_servicios SET cliente_id = 8012, direccion = 'Lote ajeno' WHERE id = 80110`)
	v := marcar()
	a := deber(e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: v.Version, AvisoEn: v.DatosArgosCambiados.En}))(t)
	if a.Direccion != "Casa de Ana" || *a.Lat != 14.1 || *a.Referencia != "Esquina" {
		t.Fatalf("con la instantánea del cliente: %+v", a)
	}

	// Y el cliente ya es de otra empresa: nada coincide.
	exec(`INSERT INTO nucleo.empresas (id, nombre) VALUES (67, 'Empresa 67') ON CONFLICT (id) DO NOTHING`)
	exec(`UPDATE integracion.argos_clientes SET empresa_id = 67, empresas = '{67}' WHERE id = 8011`)
	v = marcar()
	_, err := e.m.AplicarDatosArgos.Ejecutar(ctx, campo.EntradaAplicarDatosArgos{ID: o.ID, Version: v.Version, AvisoEn: v.DatosArgosCambiados.En})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "instantanea_no_coincide" {
		t.Fatalf("instantánea de otra empresa: %v", err)
	}
	if r := deber(e.m.VerOrden.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t).Orden; r.Direccion != "Casa de Ana" || r.DatosArgosCambiados == nil {
		t.Fatalf("la orden no cambia: %+v", r)
	}
}
