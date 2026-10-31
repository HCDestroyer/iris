package argos_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato Iris ↔ ArgosISP 0.5.0 (Argos 1.6.0): versión y contrato, técnicos de Argos y webhooks
// nuevos.

// §3.3 «Versión y compatibilidad»: Iris manda contrato 0.5.0 en el canje y guarda version_argos y
// contrato; el conector expone la capacidad inventario_por_tecnico. Al activar se encola el informe
// del contrato (PUT …/contrato y GET …/estado).
func TestContratoEnCanjeYVersionArgos16(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	admin, id, _ := e.conectado()
	var canje map[string]any
	e.argos.con(func() { canje = e.argos.canjes[len(e.argos.canjes)-1] })
	if canje["contrato"] != "0.5.0" {
		t.Fatalf("el canje debía mandar contrato 0.5.0: %v", canje["contrato"])
	}
	if n := e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND version_remota = '1.6.0' AND contrato_remoto = '0.5.0'
		AND contrato_informado = '0.5.0' AND version_consultada_en IS NOT NULL`, id); n != 1 {
		t.Fatal("el conector debía guardar la versión del canje")
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato' AND (args->>'conector_id')::bigint = $1`, id); n != 1 {
		t.Fatalf("activar debía encolar el informe del contrato: %d", n)
	}
	l := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores", nil)
	con := conectorDe(t, l, id)
	if con["versionRemota"] != "1.6.0" || con["contratoInformado"] != "0.5.0" || fmt.Sprint(con["capacidades"]) != "[inventario_por_tecnico]" {
		t.Fatalf("conector en la API: %v", con)
	}
	// El job: PUT …/contrato (idempotente) y GET …/estado. Sin cambios: no audita ni emite.
	ctx := pruebas.ContextoSistema(e.pool)
	r, err := e.mod.InformarContrato.Ejecutar(ctx, id)
	if err != nil || r.Cambio {
		t.Fatalf("informar: %+v %v", r, err)
	}
	if e.argos.contratos != 1 {
		t.Fatalf("PUT contrato: %d", e.argos.contratos)
	}
}

// Argos 1.5.x: no informa versión y responde 404 a las rutas nuevas. Iris no falla, queda sin la
// capacidad (bodega_id) y la revisión periódica encola el informe; cuando Argos pasa a 1.6.0, el
// job guarda la versión y emite conector.actualizado.
func TestArgos15SinVersionYLuegoActualizado(t *testing.T) {
	e := nuevoEntorno(t)
	admin, id, _ := e.conectado()
	l := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores", nil)
	con := conectorDe(t, l, id)
	if con["versionRemota"] != nil || fmt.Sprint(con["capacidades"]) != "[]" {
		t.Fatalf("Argos 1.5: %v", con)
	}
	ctx := pruebas.ContextoSistema(e.pool)
	r, err := e.mod.InformarContrato.Ejecutar(ctx, id)
	if err != nil || r.Cambio || r.ContratoInformado != nil {
		t.Fatalf("con Argos 1.5 no es error: %+v %v", r, err)
	}
	// El selector de técnicos dice por qué no.
	admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos", nil).problema(t, 422, "argos_sin_inventario_tecnico")

	antes := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato' AND (args->>'conector_id')::bigint = $1`, id)
	if err := e.mod.RevisarContrato(ctx); err != nil {
		t.Fatal(err)
	}
	// La activación ya lo había encolado en el mismo minuto (único por minuto): no se duplica.
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato' AND (args->>'conector_id')::bigint = $1`, id); n != antes || n < 1 {
		t.Fatalf("revisar: %d → %d", antes, n)
	}

	e.argos.con(func() { e.argos.version = "1.6.0" })
	r, err = e.mod.InformarContrato.Ejecutar(ctx, id)
	if err != nil || !r.Cambio || r.VersionRemota == nil || *r.VersionRemota != "1.6.0" || r.ContratoInformado == nil || *r.ContratoInformado != "0.5.0" {
		t.Fatalf("Argos actualizado: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.actualizado' AND (payload->>'conector_id')::bigint = $1
		AND payload->'campos' ? 'version_remota' AND payload->'campos' ? 'contrato_informado'`, id); n != 1 {
		t.Fatalf("conector.actualizado: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.informar_contrato' AND resultado = 'ok' AND entidad_id = $1`, fmt.Sprint(id)); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
}

func conectorDe(t *testing.T, r respuesta, id int64) map[string]any {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("conectores: %d %s", r.status, r.crudo)
	}
	for _, c := range r.cuerpo["data"].([]any) {
		m := c.(map[string]any)
		if int64(m["id"].(float64)) == id {
			return m
		}
	}
	t.Fatalf("no está el conector %d", id)
	return nil
}

// GET /integraciones/argosisp/tecnicos: proxy paginado de GET /inventario/tecnicos con agenda.vincular_argos;
// marca los ya vinculados y si tienen bodega en la empresa activa.
func TestSelectorTecnicosArgos(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() {
		e.argos.version = "1.6.0"
		e.argos.tecnicos = []map[string]any{
			{"id_externo": "u-7", "correo": "ana@argos.gt", "nombre": "Ana", "bodegas": []any{map[string]any{"id": 70, "empresa_id": 1, "activa": true}}},
			{"idExterno": 8, "correo": "beto@argos.gt", "nombre": "Beto", "bodegas": []any{map[string]any{"id": 80, "empresa_id": 2, "activa": true}}},
			{"id_externo": "u-9", "correo": "caro@argos.gt", "nombre": "Caro", "bodegas": []any{map[string]any{"id": 90, "empresa_id": 1, "activa": false}}},
		}
	})
	admin, _, _ := e.conectado()
	e.usuario("supervisor").pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos", nil).problema(t, 403, "prohibido")
	// Un técnico de Iris ya vinculado a u-7.
	usuario := e.contar(`SELECT id FROM nucleo.usuarios ORDER BY id DESC LIMIT 1`)
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = 'u-7' OR usuario_id = $1`, usuario)
	e.exec(`INSERT INTO agenda.tecnicos (usuario_id, argos_id_externo, argos_vinculado_en) VALUES ($1, 'u-7', now())`, usuario)

	p1 := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?limit=2", nil, "X-Iris-Empresa", "1")
	if p1.status != 200 {
		t.Fatalf("selector: %d %s", p1.status, p1.crudo)
	}
	data := p1.cuerpo["data"].([]any)
	ana, beto := data[0].(map[string]any), data[1].(map[string]any)
	if ana["idExterno"] != "u-7" || ana["enEmpresa"] != true || ana["vinculadoA"] == nil {
		t.Fatalf("ana: %v", ana)
	}
	if beto["idExterno"] != "8" || beto["vinculadoA"] != nil {
		t.Fatalf("beto (id numérico, libre): %v", beto)
	}
	if p1.cuerpo["nextCursor"] != "2" {
		t.Fatalf("cursor: %v", p1.cuerpo["nextCursor"])
	}
	p2 := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?cursor=2", nil)
	if p2.status != 200 || len(p2.cuerpo["data"].([]any)) != 1 || p2.cuerpo["nextCursor"] != nil {
		t.Fatalf("página 2: %s", p2.crudo)
	}
	// BuscarTecnico recorre las páginas.
	x, err := e.mod.BuscarTecnico(pruebas.ContextoSistema(e.pool), "u-9")
	if err != nil || x.Correo != "caro@argos.gt" {
		t.Fatalf("buscar: %+v %v", x, err)
	}
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = 'u-7'`)
}

// Webhooks 0.5.0 (§2.3).
func TestWebhooks050(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	_, id, secreto := e.conectado()
	lectura := e.usuario("lectura")
	e.sembrarCliente(81, "Gabriela")
	if f := lectura.pedir(http.MethodGet, "/api/v1/clientes/81", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}

	// servicio.datos_cambiados: refresca referencia y contacto en sitio; avisa a campo (las órdenes
	// abiertas se marcan, no se pisan).
	e.argos.con(func() {
		s := e.argos.servicios[81][0]
		s["referencia"], s["contacto_nombre"], s["contacto_telefono"] = "Portón negro", "Luis", "+502 5512-3344"
	})
	r := e.procesarWebhook(id, secreto, "servicio.datos_cambiados", map[string]any{"servicio_id": 810, "empresa_id": 1, "empresas": []int{1},
		"cliente_id": 81, "campos": []string{"referencia", "contacto_telefono"}})
	if r.Resultado != "aplicado" {
		t.Fatalf("datos_cambiados: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810 AND referencia = 'Portón negro'
		AND contacto_nombre = 'Luis' AND contacto_telefono = '+502 5512-3344'`); n != 1 {
		t.Fatal("no se refrescó el contacto del servicio")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.datos_cambiados' AND empresa_id = 1
		AND payload->>'entidad' = 'servicio' AND (payload->>'servicio_id')::bigint = 810
		AND payload->'campos' = '["servicio.contacto_telefono", "servicio.referencia"]'::jsonb`); n != 1 {
		t.Fatalf("argos.datos_cambiados: %d", n)
	}
	ficha := lectura.pedir(http.MethodGet, "/api/v1/clientes/81", nil)
	srv := ficha.cuerpo["servicios"].([]any)[0].(map[string]any)
	if srv["referencia"] != "Portón negro" || srv["contactoNombre"] != "Luis" || len(srv["contactoTelefonos"].([]any)) != 1 {
		t.Fatalf("ficha: %v", srv)
	}

	// cliente.actualizado con email y referencia (0.5.0).
	e.argos.con(func() { e.argos.clientes[81]["referencia"] = "Frente a la iglesia" })
	if r := e.procesarWebhook(id, secreto, "cliente.actualizado", map[string]any{"cliente_id": 81, "empresa_id": 1, "campos": []string{"email", "referencia"}}); r.Resultado != "aplicado" {
		t.Fatalf("cliente.actualizado: %+v", r)
	}
	if e.consultarTexto(`SELECT referencia FROM integracion.argos_clientes WHERE id = 81`) != "Frente a la iglesia" {
		t.Fatal("no se guardó la referencia del cliente")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.datos_cambiados' AND payload->>'entidad' = 'cliente'
		AND payload->'campos' = '["cliente.referencia"]'::jsonb`); n != 1 {
		t.Fatalf("datos del cliente: %d", n)
	}

	// catalogo.cambiado: zona renombrada (instantánea, nombres copiados y espejo de agenda).
	e.exec(`INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en) VALUES (3, 1, 'Centro', now()) ON CONFLICT (id) DO UPDATE SET nombre = 'Centro', activo = true`)
	e.exec(`INSERT INTO agenda.zonas (id, empresa_id, origen, nombre) VALUES (3, 1, 'argos', 'Centro') ON CONFLICT (id) DO UPDATE SET nombre = 'Centro', origen = 'argos', activa = true`)
	e.argos.con(func() { e.argos.zonas = []map[string]any{{"id": 3, "empresa_id": 1, "nombre": "Centro Histórico"}} })
	if r := e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "zona", "id": 3, "empresa_id": 1, "campos": []string{"nombre"}}); r.Resultado != "aplicado" {
		t.Fatalf("zona: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'zona.actualizada' AND (payload->>'zona_id')::bigint = 3
		AND payload->'campos' = '["nombre"]'::jsonb AND (payload->>'activa')::boolean`); n != 1 {
		t.Fatalf("zona.actualizada (B4): %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_zonas WHERE id = 3 AND nombre = 'Centro Histórico' AND activo`) +
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810 AND zona_nombre = 'Centro Histórico'`) +
		e.contar(`SELECT count(*) FROM agenda.zonas WHERE id = 3 AND nombre = 'Centro Histórico' AND activa`); n != 3 {
		t.Fatalf("zona renombrada: %d de 3", n)
	}
	// Zona desactivada (ya no aparece en la lista): inactiva en la instantánea y en agenda.
	e.argos.con(func() { e.argos.zonas = nil })
	e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "zona", "id": 3, "empresa_id": 1, "campos": []string{"activo"}})
	if n := e.contar(`SELECT count(*) FROM integracion.argos_zonas WHERE id = 3 AND NOT activo`) + e.contar(`SELECT count(*) FROM agenda.zonas WHERE id = 3 AND NOT activa`); n != 2 {
		t.Fatalf("zona desactivada: %d de 2", n)
	}
	// B3: un 404 que no es el problem+json no_encontrado de Argos (un proxy) no desactiva el router.
	e.exec(`INSERT INTO integracion.argos_routers (id, empresa_id, nombre, sincronizado_en) VALUES (5, 1, 'RT-5', now()) ON CONFLICT (id) DO UPDATE SET activo = true`)
	e.argos.con(func() { e.argos.proxy404["/api/v1/routers/5"] = true })
	ent := e.entranteDe(e.enviar(id, secreto, "wh-r5-"+pruebas.Unico(), "catalogo.cambiado", map[string]any{"catalogo": "router", "id": 5, "campos": []string{"activo"}}, time.Now()))
	if _, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err == nil {
		t.Fatal("un 404 no confirmado se reintenta")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 5 AND activo`); n != 1 {
		t.Fatal("el router sigue activo")
	}
	// Router renombrado; plan renombrado (en los servicios).
	e.exec(`INSERT INTO integracion.argos_routers (id, empresa_id, nombre, sincronizado_en) VALUES (4, 1, 'RT-Centro', now()) ON CONFLICT (id) DO NOTHING`)
	e.argos.con(func() {
		e.argos.routers = []map[string]any{{"id": 4, "empresa_id": 1, "nombre": "RT-Centro-2", "activo": false, "coordenadas": "14.5,-90.5"}}
		e.argos.planes = []map[string]any{{"id": 8, "nombre": "Fibra 100 Plus"}}
	})
	e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "router", "id": 4, "empresa_id": 1, "campos": []string{"nombre", "activo", "coordenadas"}})
	e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "plan", "id": 8, "empresa_id": nil, "campos": []string{"nombre"}})
	if n := e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 4 AND nombre = 'RT-Centro-2' AND NOT activo AND lat = 14.5`) +
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 810 AND router_nombre = 'RT-Centro-2' AND plan_nombre = 'Fibra 100 Plus'`); n != 2 {
		t.Fatalf("router y plan: %d de 2", n)
	}
	// Catálogo desconocido: ignorado.
	r = e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "bodega", "id": 1, "campos": []string{"nombre"}})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "catalogo_desconocido" {
		t.Fatalf("catálogo desconocido: %+v", r)
	}
}

// inventario.traslado_*: modelo de lectura por técnico; un estado final no vuelve a enviado.
func TestWebhooksTraslados(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	_, id, secreto := e.conectado()
	traslado := int64(900000) + int64(e.contar(`SELECT (random() * 90000)::int`))
	e.exec(`DELETE FROM integracion.argos_traslados WHERE id = $1`, traslado)
	enviado := map[string]any{"traslado_id": traslado, "empresa_id": 1, "bodega_destino_id": 70, "tecnico": map[string]any{"id_externo": "u-7", "correo": "ana@argos.gt"},
		"estado": "enviado", "lineas": []any{map[string]any{"producto_id": 5, "cantidad": "10", "series": []any{}}}, "enviado_en": "2026-10-30T15:00:00Z"}
	if r := e.procesarWebhook(id, secreto, "inventario.traslado_enviado", enviado); r.Resultado != "aplicado" {
		t.Fatalf("enviado: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_traslados WHERE id = $1 AND estado = 'enviado' AND tecnico_id_externo = 'u-7'
		AND jsonb_array_length(lineas) = 1 AND empresa_id = 1`, traslado); n != 1 {
		t.Fatal("traslado enviado mal guardado")
	}
	recibido := map[string]any{"traslado_id": traslado, "empresa_id": 1, "estado": "recibido", "recibido_en": "2026-10-30T16:00:00Z",
		"recibido_por": map[string]any{"id_externo": "u-7"}, "metodo_recepcion": "firma_digital", "origen": "iris"}
	e.procesarWebhook(id, secreto, "inventario.traslado_recibido", recibido)
	// Un enviado atrasado (reintento de Argos con otro id) no lo reabre.
	e.procesarWebhook(id, secreto, "inventario.traslado_enviado", enviado)
	if n := e.contar(`SELECT count(*) FROM integracion.argos_traslados WHERE id = $1 AND estado = 'recibido' AND recibido_por = 'u-7'
		AND origen_recepcion = 'iris' AND jsonb_array_length(lineas) = 1`, traslado); n != 1 {
		t.Fatal("el traslado recibido no debía volver a enviado")
	}
	// El enviado atrasado no toca nada ni avisa (B1).
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.traslado_cambiado' AND (payload->>'traslado_id')::bigint = $1`, traslado); n != 2 {
		t.Fatalf("argos.traslado_cambiado: %d", n)
	}
	// B2: líneas inválidas o empresa fuera del alcance del conector: ignorado, sin guardar.
	malo := map[string]any{"traslado_id": traslado + 1, "empresa_id": 1, "estado": "enviado", "lineas": []any{map[string]any{"producto_id": "x", "cantidad": 1}}}
	if r := e.procesarWebhook(id, secreto, "inventario.traslado_enviado", malo); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "traslado_lineas_invalidas" {
		t.Fatalf("líneas inválidas: %+v", r)
	}
	ajeno := map[string]any{"traslado_id": traslado + 2, "empresa_id": 2, "estado": "enviado", "lineas": []any{}, "metodo_recepcion": strings.Repeat("x", 50)}
	if r := e.procesarWebhook(id, secreto, "inventario.traslado_enviado", ajeno); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "empresa_fuera_de_alcance" {
		t.Fatalf("empresa fuera de alcance: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_traslados WHERE id IN ($1, $2)`, traslado+1, traslado+2); n != 0 {
		t.Fatal("no se guardan")
	}
	// Sin traslado_id: ignorado.
	r := e.procesarWebhook(id, secreto, "inventario.traslado_cancelado", map[string]any{"empresa_id": 1})
	if r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "traslado_sin_id" {
		t.Fatalf("sin id: %+v", r)
	}
}

// MEDIUM 3 de la revisión: Argos vuelve de 1.6.0 a 1.5.x. GET …/estado responde 404: la versión y el
// contrato guardados se borran (Iris vuelve a bodega_id) y la revisión periódica relee siempre.
func TestArgosVueltaAtras(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	admin, id, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	e.exec(`DELETE FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato'`)
	if err := e.mod.RevisarContrato(ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato' AND (args->>'conector_id')::bigint = $1`, id); n != 1 {
		t.Fatalf("la revisión relee aunque esté al día: %d", n)
	}
	e.argos.con(func() { e.argos.version = "" })
	r, err := e.mod.InformarContrato.Ejecutar(ctx, id)
	if err != nil || !r.Cambio || r.VersionRemota != nil || r.ContratoRemoto != nil || r.ContratoInformado != nil {
		t.Fatalf("vuelta atrás: %+v %v", r, err)
	}
	if ok, _ := e.mod.InventarioPorTecnico(ctx, e.pool); ok {
		t.Fatal("sin versión, la cola vuelve a bodega_id")
	}
	if con := conectorDe(t, admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores", nil), id); fmt.Sprint(con["capacidades"]) != "[]" {
		t.Fatalf("capacidades: %v", con["capacidades"])
	}
}

// MEDIUM 1 de la revisión: sin empresas.todas, el selector solo muestra a los usuarios de Argos con
// bodega en la empresa activa (y solo esas bodegas), y vinculadoA solo si la ficha es de esa empresa.
func TestSelectorTecnicosArgosPorEmpresa(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() {
		e.argos.version = "1.6.0"
		e.argos.tecnicos = []map[string]any{
			{"id_externo": "u-e1", "correo": "uno@argos.gt", "nombre": "Uno", "bodegas": []any{
				map[string]any{"id": 10, "empresa_id": 1, "activa": true}, map[string]any{"id": 20, "empresa_id": 2, "activa": true}}},
			{"id_externo": "u-e2", "correo": "dos@argos.gt", "nombre": "Dos", "bodegas": []any{map[string]any{"id": 21, "empresa_id": 2, "activa": true}}},
		}
	})
	_, _, _ = e.conectado()
	e.exec("INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING")
	// u-e1 vinculado a un técnico que solo opera en la empresa 2.
	var tec int64
	if err := e.pool.QueryRow(context.Background(), `SELECT id FROM nucleo.usuarios ORDER BY id DESC LIMIT 1`).Scan(&tec); err != nil {
		t.Fatal(err)
	}
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo IN ('u-e1', 'u-e2') OR usuario_id = $1`, tec)
	e.exec(`DELETE FROM nucleo.usuarios_empresas WHERE usuario_id = $1`, tec)
	e.exec(`INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, 2)`, tec)
	e.exec(`INSERT INTO agenda.tecnicos (usuario_id, argos_id_externo, argos_vinculado_en) VALUES ($1, 'u-e1', now())`, tec)
	leer := func(ctx context.Context) []argos.TecnicoDeArgos {
		t.Helper()
		var todos []argos.TecnicoDeArgos
		cursor := ""
		for {
			p, err := e.mod.ListarTecnicosArgos.Ejecutar(ctx, argos.EntradaTecnicosArgos{Cursor: cursor, Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			todos = append(todos, p.Data...)
			if p.NextCursor == nil {
				return todos
			}
			cursor = *p.NextCursor
		}
	}
	actor := e.contar(`SELECT id FROM nucleo.usuarios ORDER BY id LIMIT 1`)
	enUno := leer(pruebas.ContextoUsuarioEnEmpresa(e.pool, int64(actor), 1, nucleo.PermAgendaVincularArgos))
	if len(enUno) != 1 || enUno[0].IDExterno != "u-e1" || len(enUno[0].Bodegas) != 1 || enUno[0].Bodegas[0].EmpresaID != 1 || enUno[0].VinculadoA != nil {
		t.Fatalf("empresa 1 sin empresas.todas: %+v", enUno)
	}
	global := leer(pruebas.ContextoUsuarioEnEmpresa(e.pool, int64(actor), 1, nucleo.PermAgendaVincularArgos, nucleo.PermEmpresasTodas))
	if len(global) != 2 || len(global[0].Bodegas) != 2 || global[0].VinculadoA == nil {
		t.Fatalf("con empresas.todas: %+v", global)
	}
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = 'u-e1'`)
}

// B6: datos_cambiados de un servicio que Argos ya eliminó no avisa a campo.
func TestDatosCambiadosDeServicioEliminado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	_, id, secreto := e.conectado()
	e.exec(`DELETE FROM integracion.argos_eliminados WHERE tipo = 'servicio' AND id = 9191`)
	e.exec(`INSERT INTO integracion.argos_eliminados (tipo, id, conector_id, eliminado_en, confirmado_en) VALUES ('servicio', 9191, $1, now(), now())`, id)
	r := e.procesarWebhook(id, secreto, "servicio.datos_cambiados", map[string]any{"servicio_id": 9191, "empresa_id": 1, "cliente_id": 919, "campos": []string{"referencia"}})
	if r.Resultado != "ignorado" {
		t.Fatalf("eliminado: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.datos_cambiados' AND (payload->>'servicio_id')::bigint = 9191`); n != 0 {
		t.Fatal("no se avisa de un servicio eliminado")
	}
}
