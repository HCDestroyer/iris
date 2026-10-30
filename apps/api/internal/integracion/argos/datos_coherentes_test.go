package argos_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
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
		c.Telefono == nil || c.Telefono.Mostrar != "+502 5512 3344" || c.Cliente.Estado != "suspendido" || !c.Completa {
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
	leida, err := argos.CopiaDe(ctx, e.pool, argos.CopiaTicket, 900001, &empresa)
	if err != nil || leida == nil || leida.Direccion != "Lote 5, aldea El Durazno" || leida.Telefono.Mostrar != "+502 5512 3344" ||
		leida.CopiadoEn.IsZero() || leida.SincronizadoEn.IsZero() {
		t.Fatalf("copia conservada: %+v %v", leida, err)
	}
	// Sin instantánea del cliente no hay copia.
	if c, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaTicket, 900002, 999999, nil, nil, ahora); err != nil || c != nil {
		t.Fatalf("sin instantánea: %+v %v", c, err)
	}
	if c, err := argos.CopiaDe(ctx, e.pool, argos.CopiaTicket, 900002, &empresa); err != nil || c != nil {
		t.Fatalf("sin copia: %+v %v", c, err)
	}
	// La copia no lleva documento fiscal, correo ni empresas (solo lo operativo).
	if n := e.contar(`SELECT count(*) FROM integracion.argos_copias WHERE entidad_id = 900001
		AND (datos::text LIKE '%12345678%' OR datos::text LIKE '%hilda@%' OR datos->'cliente' ? 'empresas' OR datos->'cliente' ? 'nitCui')`); n != 0 {
		t.Fatal("la copia guarda datos personales que no hacen falta")
	}
	// Leída desde otra empresa, no sale.
	otra := int64(2)
	if c, err := argos.CopiaDe(ctx, e.pool, argos.CopiaTicket, 900001, &otra); err != nil || c != nil {
		t.Fatalf("copia leída desde otra empresa: %+v %v", c, err)
	}
	// Ni se copia un cliente que la instantánea pone en otra empresa.
	if c, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaTicket, 900003, 457, &srv, &otra, ahora); err != nil || c != nil {
		t.Fatalf("copia de un cliente de otra empresa: %+v %v", c, err)
	}
	// De una fila anterior a los datos coherentes, la copia queda marcada incompleta.
	e.exec(`UPDATE integracion.argos_clientes SET version_datos = 1 WHERE id = 457`)
	if c, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaOrden, 900004, 457, &srv, &empresa, ahora); err != nil || c == nil || c.Completa {
		t.Fatalf("copia de una fila vieja: %+v %v", c, err)
	}
	// Argos elimina al cliente: sus copias se anonimizan (solo ids).
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours' WHERE id = 457`)
	e.argos.con(func() { delete(e.argos.clientes, 457) })
	e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/457", nil).problema(t, 404, "no_encontrado")
	an, err := argos.CopiaDe(ctx, e.pool, argos.CopiaTicket, 900001, &empresa)
	if err != nil || an == nil || !an.Anonimizada || an.Cliente.Nombre != "" || an.Direccion != "" || an.Telefono != nil || an.Servicio != nil || an.Cliente.ID != 457 {
		t.Fatalf("copia anonimizada: %+v %v", an, err)
	}
	e.exec(`DELETE FROM integracion.argos_copias WHERE entidad_id IN (900001, 900002, 900003, 900004)`)
}

// B2 (revisión de #73): antes de crear un ticket u orden con servicio, la instantánea se asegura
// en Argos; un servicio que no es del cliente es 422 y, con Argos caído, 503 (nunca se acepta un
// servicio sin verificar).
func TestAsegurarInstantanea(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	srv := func(id int64) *int64 { return &id }
	// Sin conector activo: nada que verificar.
	if err := e.mod.AsegurarInstantanea(ctx, 460, srv(4600)); err != nil {
		t.Fatalf("sin conector: %v", err)
	}
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(460)
	e.sembrarComoArgos14(461)
	if err := e.mod.AsegurarInstantanea(ctx, 460, srv(4600)); err != nil {
		t.Fatalf("servicio del cliente: %v", err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 4600 AND cliente_id = 460`); n != 1 {
		t.Fatal("la instantánea no quedó guardada")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.sincronizar' AND entidad_id = '460'`); n != 1 {
		t.Fatal("la lectura no quedó auditada")
	}
	antes := e.argos.llamadasA("/api/v1/clientes/460")
	if err := e.mod.AsegurarInstantanea(ctx, 460, srv(4601)); err != nil || e.argos.llamadasA("/api/v1/clientes/460") != antes {
		t.Fatalf("con la instantánea no debía llamar a Argos: %v", err)
	}
	// Servicio de otro cliente con la instantánea recién leída: se valida localmente, sin Argos,
	// y da el 422 único (el mismo que «no existe» y «de otra empresa»).
	antes = e.argos.llamadasA("/api/v1/clientes/460")
	if err := e.mod.AsegurarInstantanea(ctx, 460, srv(4610)); !esProblemaStatus(err, 422, "empresa_no_coincide") ||
		e.argos.llamadasA("/api/v1/clientes/460") != antes {
		t.Fatalf("servicio de otro cliente: %v", err)
	}
	// Con la instantánea de hace más de 30 s, se relee una vez y el servicio sigue sin ser suyo.
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '1 minute' WHERE id = 460`)
	if err := e.mod.AsegurarInstantanea(ctx, 460, srv(4610)); !esProblemaStatus(err, 422, "empresa_no_coincide") ||
		e.argos.llamadasA("/api/v1/clientes/460") != antes+1 {
		t.Fatalf("servicio de otro cliente tras releer: %v", err)
	}
	// Cliente que Argos no tiene: el mismo 422.
	if err := e.mod.AsegurarInstantanea(ctx, 999999, srv(1)); !esProblemaStatus(err, 422, "empresa_no_coincide") {
		t.Fatalf("cliente inexistente: %v", err)
	}
	// Cliente de otra empresa (la activa es la 2): mismo 422 y no entra a la instantánea.
	e.sembrarComoArgos14(465)
	otra := pruebas.ContextoUsuarioEnEmpresa(e.pool, 999, 2)
	if err := e.mod.AsegurarInstantanea(otra, 465, srv(4650)); !esProblemaStatus(err, 422, "empresa_no_coincide") {
		t.Fatalf("cliente de otra empresa: %v", err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 465`); n != 0 {
		t.Fatal("un cliente de otra empresa no debía guardarse")
	}
	// Argos eliminó al cliente: se borra la instantánea y se anonimizan sus copias.
	empresa := int64(1)
	if _, err := argos.CopiarDatos(ctx, e.pool, argos.CopiaTicket, 900010, 460, srv(4600), &empresa, time.Now()); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '1 minute' WHERE id = 460`)
	e.argos.con(func() { delete(e.argos.clientes, 460) })
	if err := e.mod.AsegurarInstantanea(ctx, 460, srv(4699)); !esProblemaStatus(err, 422, "empresa_no_coincide") {
		t.Fatalf("cliente eliminado en Argos: %v", err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 460`); n != 0 {
		t.Fatal("la instantánea del cliente eliminado debía borrarse")
	}
	if c, err := argos.CopiaDe(ctx, e.pool, argos.CopiaTicket, 900010, &empresa); err != nil || c == nil || !c.Anonimizada {
		t.Fatalf("copia anonimizada: %+v %v", c, err)
	}
	e.exec(`DELETE FROM integracion.argos_copias WHERE entidad_id = 900010`)
	// Argos caído: con servicio, 503; solo con cliente, se sigue.
	e.sembrarComoArgos14(462)
	e.argos.con(func() { e.argos.caido = true })
	if err := e.mod.AsegurarInstantanea(ctx, 462, srv(4620)); !esProblemaStatus(err, 503, "argos_no_disponible") {
		t.Fatalf("Argos caído con servicio: %v", err)
	}
	if err := e.mod.AsegurarInstantanea(ctx, 462, nil); err != nil {
		t.Fatalf("Argos caído sin servicio: %v", err)
	}
}

// Cuota de lecturas a pedido por usuario: pasada, 429 y sin llamar a Argos.
func TestAsegurarInstantaneaCuota(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	ctx := pruebas.ContextoUsuarioEnEmpresa(e.pool, 991, 1)
	s := int64(1)
	for i := range argos.CuotaUsuario {
		if err := e.mod.AsegurarInstantanea(ctx, int64(800000+i), &s); !esProblemaStatus(err, 422, "empresa_no_coincide") {
			t.Fatalf("lectura %d: %v", i, err)
		}
	}
	if err := e.mod.AsegurarInstantanea(ctx, 800099, &s); !esProblemaStatus(err, 429, "cuota_argos") || e.argos.llamadasA("/api/v1/clientes/800099") != 0 {
		t.Fatalf("pasada la cuota: %v", err)
	}
	// Otro usuario tiene la suya.
	if err := e.mod.AsegurarInstantanea(pruebas.ContextoUsuarioEnEmpresa(e.pool, 992, 1), 800099, &s); !esProblemaStatus(err, 422, "empresa_no_coincide") {
		t.Fatalf("otro usuario: %v", err)
	}
}

func esProblemaStatus(err error, status int, codigo string) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Status == status && p.Codigo() == codigo
}

// MEDIUM-1: tras una caída de Argos no se vuelve a llamar durante EsperaTrasFallo (stale), y
// «Refrescar» de un cliente que la instantánea pone en otra empresa no llama a Argos.
func TestFichaEsperaTrasFalloYEmpresa(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(463)
	lectura := e.usuario("lectura")
	if f := lectura.pedir(http.MethodGet, "/api/v1/clientes/463", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	llamadas := func() int { return e.argos.llamadasA("/api/v1/clientes/463") }
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours' WHERE id = 463`)
	e.argos.con(func() { e.argos.caido = true })
	if s := lectura.pedir(http.MethodGet, "/api/v1/clientes/463", nil); s.status != 200 || s.cuerpo["stale"] != true {
		t.Fatalf("caído: %s", s.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 463 AND fallo_en IS NOT NULL`); n != 1 {
		t.Fatal("no quedó marcado el fallo")
	}
	e.argos.con(func() { e.argos.caido = false })
	antes := llamadas()
	for range 3 {
		if s := lectura.pedir(http.MethodGet, "/api/v1/clientes/463?refrescar=true", nil); s.status != 200 || s.cuerpo["stale"] != true {
			t.Fatalf("en espera: %s", s.crudo)
		}
	}
	if llamadas() != antes {
		t.Fatal("en espera tras el fallo no debía llamar a Argos")
	}
	e.exec(`UPDATE integracion.argos_clientes SET fallo_en = now() - interval '2 minutes' WHERE id = 463`)
	if s := lectura.pedir(http.MethodGet, "/api/v1/clientes/463", nil); s.status != 200 || s.cuerpo["stale"] != false || llamadas() != antes+1 {
		t.Fatalf("pasada la espera: %s", s.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 463 AND fallo_en IS NULL`); n != 1 {
		t.Fatal("una lectura buena limpia el fallo")
	}
	// La instantánea pone al cliente en otra empresa: «Refrescar» no llama; 404.
	e.exec(`UPDATE integracion.argos_clientes SET empresas = '{2}', empresa_id = 2, sin_servicios = false,
		sincronizado_en = now() - interval '1 minute' WHERE id = 463`)
	antes = llamadas()
	lectura.pedir(http.MethodGet, "/api/v1/clientes/463?refrescar=true", nil).problema(t, 404, "no_encontrado")
	if llamadas() != antes {
		t.Fatal("refrescar un cliente de otra empresa no debía llamar a Argos")
	}
}

// LOW-5: una búsqueda por teléfono o NIT queda enmascarada en la auditoría.
func TestBusquedaEnmascaradaEnAuditoria(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	if b := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes?q=5512-3344", nil); b.status != 200 {
		t.Fatalf("buscar: %s", b.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.buscar' AND entrada->>'q' = '****-**44'`); n != 1 {
		t.Fatal("la búsqueda por teléfono no quedó enmascarada")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.buscar' AND entrada::text LIKE '%5512%'`); n != 0 {
		t.Fatal("el teléfono buscado quedó en la auditoría")
	}
}

// LOW-2: un servicio retirado no resuelve su IP desde la instantánea (la IP ya puede ser de otro).
func TestServicioPorIPSinRetirados(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(464)
	if f := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/464", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	ctx := context.Background()
	q := argosbd.New(e.pool)
	if _, err := q.ServicioPorIP(ctx, argosbd.ServicioPorIPParams{Ip: "100.64.10.5", EmpresaID: 1}); err != nil {
		t.Fatalf("activo: %v", err)
	}
	e.exec(`UPDATE integracion.argos_servicios SET estado = 'retirado' WHERE id = 4640`)
	if _, err := q.ServicioPorIP(ctx, argosbd.ServicioPorIPParams{Ip: "100.64.10.5", EmpresaID: 1}); err == nil {
		t.Fatal("un servicio retirado no debía resolver la IP")
	}
}

// «Refrescar»: solo se cobra la cuota cuando se llama a Argos; pasada la cuota del usuario, la
// ficha dice refrescoOmitido «cuota» (y otro usuario sigue pudiendo); una instantánea de hace
// menos de 30 s da «reciente» sin cobrar; una vencida se lee aunque la cuota esté agotada.
func TestRefrescarCuotaYOmitido(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.sembrarComoArgos14(466)
	yo := e.usuario("lectura")
	if f := yo.pedir(http.MethodGet, "/api/v1/clientes/466", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	llamadas := func() int { return e.argos.llamadasA("/api/v1/clientes/466") }
	// Recién leída: no se llama ni se cobra.
	for range argos.CuotaUsuario + 2 {
		if r := yo.pedir(http.MethodGet, "/api/v1/clientes/466?refrescar=true", nil); r.cuerpo["refrescoOmitido"] != "reciente" {
			t.Fatalf("reciente: %s", r.crudo)
		}
	}
	antes := llamadas()
	for i := range argos.CuotaUsuario {
		e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '1 minute' WHERE id = 466`)
		if r := yo.pedir(http.MethodGet, "/api/v1/clientes/466?refrescar=true", nil); r.cuerpo["refrescoOmitido"] != nil {
			t.Fatalf("refresco %d: %s", i, r.crudo)
		}
	}
	if llamadas() != antes+argos.CuotaUsuario {
		t.Fatalf("cada refresco dentro de la cuota llama a Argos: %d", llamadas()-antes)
	}
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '1 minute' WHERE id = 466`)
	if r := yo.pedir(http.MethodGet, "/api/v1/clientes/466?refrescar=true", nil); r.status != 200 || r.cuerpo["refrescoOmitido"] != "cuota" || r.cuerpo["stale"] != false {
		t.Fatalf("pasada la cuota: %s", r.crudo)
	}
	if llamadas() != antes+argos.CuotaUsuario {
		t.Fatal("pasada la cuota no debía llamar a Argos")
	}
	if r := e.usuario("lectura").pedir(http.MethodGet, "/api/v1/clientes/466?refrescar=true", nil); r.cuerpo["refrescoOmitido"] != nil {
		t.Fatalf("otro usuario tiene su cuota: %s", r.crudo)
	}
	// Vencida (24 h): se lee aunque la cuota del usuario esté agotada.
	e.exec(`UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours' WHERE id = 466`)
	antes = llamadas()
	if r := yo.pedir(http.MethodGet, "/api/v1/clientes/466", nil); r.status != 200 || llamadas() != antes+1 {
		t.Fatalf("vencida: %s", r.crudo)
	}
}
