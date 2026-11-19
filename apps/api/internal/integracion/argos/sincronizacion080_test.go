package argos_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

// Contrato argosisp.md 0.8.0 §1.5 y §2.5 (B7, lado Iris): primera sincronización automática al
// activar, con progreso por recurso, reanudable desde el último cursor y al ritmo de Argos; solo
// clientes con servicio no retirado (excluir_retirados=true) con Argos ≥ 0.8.0; informe del avance
// con PUT /integraciones/iris/sincronizacion (409 sincronizacion_vieja se descarta). Con un Argos
// anterior todo sigue como en 0.7.1. Las pruebas no dependen de argos.ContratoIris: el falso
// responde siempre su propio contrato (contratoMax), sea cual sea el que informe Iris.

// sinc080 es el estado del Argos falso para §2.5.
type sinc080 struct {
	// rutaInforme: PUT …/sincronizacion existe (Argos ≥ 1.18.0).
	rutaInforme bool
	informes    []map[string]any
	guardado    map[string]any
	// consultas: la query de cada GET /clientes y GET /servicios de las listas completas.
	consultas []string
	// fallarServicios: GET /servicios con cursor responde 503 (Argos se cae a mitad de la lista).
	fallarServicios bool
	bancos          []map[string]any
	// cursorEnorme: GET /planes responde un cursor de 600 caracteres (revisión de seguridad).
	cursorEnorme bool
	cabeceras    []string // X-Iris-Actor de las lecturas de cuentas y bancos
}

func (a *argosFalso) rutas080(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/v1/integraciones/iris/sincronizacion", a.conToken(func(w http.ResponseWriter, r *http.Request) {
		if !a.sinc080.rutaInforme {
			problema(w, 404, "urn:argosisp:problema:no_encontrado", "Ruta no encontrada")
			return
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		a.sinc080.informes = append(a.sinc080.informes, p)
		if g := a.sinc080.guardado; g != nil {
			id, gid := p["sincronizacion_id"].(string), g["sincronizacion_id"].(string)
			if id < gid || (id == gid && p["actualizada_en"].(string) < g["actualizada_en"].(string)) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:argosisp:problema:sincronizacion_vieja", "title": "Conflict", "status": 409})
				return
			}
		}
		a.sinc080.guardado = p
		escribir(w, map[string]any{"sincronizacion_id": p["sincronizacion_id"], "estado": p["estado"], "actualizada_en": p["actualizada_en"]})
	}))
	mux.HandleFunc("GET /api/v1/bancos", a.conToken(func(w http.ResponseWriter, r *http.Request) {
		a.sinc080.cabeceras = append(a.sinc080.cabeceras, r.Header.Get("X-Iris-Actor"))
		escribir(w, a.sinc080.bancos)
	}))
}

func (a *argosFalso) anotarConsulta(r *http.Request) {
	a.sinc080.consultas = append(a.sinc080.consultas, r.URL.Path+"?"+r.URL.RawQuery)
}

// excluido: con excluir_retirados=true, un cliente sin ningún servicio que no esté retirado.
func (a *argosFalso) soloRetirados(cliente int64) bool {
	for _, s := range a.servicios[cliente] {
		if s["estado"] != "retirado" {
			return false
		}
	}
	return true
}

// paginarPorID: de a 2, por id ascendente, con cursor = último id.
func paginarPorID(w http.ResponseWriter, r *http.Request, filas []map[string]any) {
	slices.SortFunc(filas, func(x, y map[string]any) int { return int(enteroDe(x["id"]) - enteroDe(y["id"])) })
	desde, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	pagina := []map[string]any{}
	for _, f := range filas {
		if enteroDe(f["id"]) > desde && len(pagina) < 2 {
			pagina = append(pagina, f)
		}
	}
	var siguiente any
	if len(pagina) == 2 && enteroDe(pagina[1]["id"]) < enteroDe(filas[len(filas)-1]["id"]) {
		siguiente = strconv.FormatInt(enteroDe(pagina[1]["id"]), 10)
	}
	escribir(w, map[string]any{"data": pagina, "nextCursor": siguiente})
}

func (a *argosFalso) listaClientes(w http.ResponseWriter, r *http.Request) {
	a.anotarConsulta(r)
	excluir := r.URL.Query().Get("excluir_retirados") == "true"
	filas := []map[string]any{}
	for id, c := range a.clientes {
		if excluir && a.soloRetirados(id) {
			continue
		}
		filas = append(filas, c)
	}
	paginarPorID(w, r, filas)
}

func (a *argosFalso) listaServicios(w http.ResponseWriter, r *http.Request) {
	a.anotarConsulta(r)
	if a.sinc080.fallarServicios && r.URL.Query().Get("cursor") != "" {
		problema(w, 503, "urn:argosisp:problema:no_disponible", "Argos no disponible")
		return
	}
	excluir := r.URL.Query().Get("excluir_retirados") == "true"
	filas := []map[string]any{}
	for _, lista := range a.servicios {
		for _, s := range lista {
			if excluir && s["estado"] == "retirado" {
				continue
			}
			filas = append(filas, s)
		}
	}
	paginarPorID(w, r, filas)
}

// sembrarSinc: Argos con zonas, routers, planes, cuentas, bancos, tres clientes con servicios (uno
// solo con servicios retirados) y un cliente sin nada más.
func (e *entorno) sembrarSinc(version, contrato string) {
	e.argos.con(func() {
		e.argos.version, e.argos.contratoMax = version, contrato
		e.argos.sinc080.rutaInforme = conector.VersionAlMenos(&contrato, "0.8.0")
		e.argos.zonas = []map[string]any{{"id": 3, "nombre": "Chitá", "empresa_id": 1}, {"id": 4, "nombre": "Centro", "empresa_id": 1}}
		e.argos.routers = []map[string]any{{"id": 5, "nombre": "Nodo Centro", "empresa_id": 1}, {"id": 6, "nombre": "Nodo Sur", "empresa_id": 1},
			{"id": 7, "nombre": "Nodo Norte", "empresa_id": 1}}
		e.argos.planes = []map[string]any{{"id": 12, "nombre": "Fibra 50"}, {"id": 13, "nombre": "Fibra 100"}, {"id": 14, "nombre": "Fibra 200"}}
		e.argos.sinc080.bancos = []map[string]any{{"id": 1, "nombre": "Banco Industrial"}, {"id": 2, "nombre": "Banrural"}}
		e.argos.rutaExtra = func(w http.ResponseWriter, r *http.Request) {
			e.argos.sinc080.cabeceras = append(e.argos.sinc080.cabeceras, r.Header.Get("X-Iris-Actor"))
			// La forma real de Argos (§7.8): número, moneda y activa siempre vienen.
			escribir(w, []map[string]any{{"id": 1, "alias": "Monetaria BI", "banco_nombre": "Banco Industrial", "empresa_id": 1, "numero": "000-123456-7",
				"moneda": "GTQ", "activa": true, "titular": "Internet del Norte, S.A.", "tipo": "monetaria"}})
		}
		for _, c := range []int64{701, 702, 703, 704} {
			e.argos.clientes[c] = map[string]any{"id": c, "codigo": fmt.Sprintf("C-%d", c), "nombre": fmt.Sprintf("Cliente %d", c), "zona_id": 3}
		}
		e.argos.servicios[701] = []map[string]any{{"id": 7010, "cliente_id": 701, "empresa_id": int64(1), "estado": "activo", "plan_nombre": "Fibra 50",
			"mac": "AA:BB:CC:DD:EE:FF", "costo_override": "1.00"}}
		e.argos.servicios[702] = []map[string]any{{"id": 7020, "cliente_id": 702, "empresa_id": int64(1), "estado": "suspendido"},
			{"id": 7021, "cliente_id": 702, "empresa_id": int64(1), "estado": "retirado"}}
		e.argos.servicios[703] = []map[string]any{{"id": 7030, "cliente_id": 703, "empresa_id": int64(1), "estado": "retirado"}}
		e.argos.servicios[704] = []map[string]any{{"id": 7040, "cliente_id": 704, "empresa_id": int64(1), "estado": "activo"},
			{"id": 7041, "cliente_id": 704, "empresa_id": int64(1), "estado": "activo"}}
	})
}

func (e *entorno) limpiarSinc() {
	e.exec("DELETE FROM integracion.argos_sincronizaciones")
	e.exec("DELETE FROM integracion.argos_certificados")
	e.exec("DELETE FROM river.river_job WHERE kind IN ('integracion.argos_primera_sincronizacion', 'integracion.argos_informar_sincronizacion')")
}

// sincronizar corre el job de la primera sincronización como lo haría el worker.
func (e *entorno) sincronizar(conectorID int64) (argos.ResultadoPrimeraSincronizacion, error) {
	e.t.Helper()
	return e.mod.PrimeraSincronizacion.Ejecutar(pruebas.ContextoSistema(e.pool), conectorID, false)
}

// informar corre, en orden, los jobs de informe encolados y devuelve cómo terminó cada uno.
func (e *entorno) informar() []string {
	e.t.Helper()
	filas, err := e.pool.Query(context.Background(), `SELECT args FROM river.river_job WHERE kind = 'integracion.argos_informar_sincronizacion' AND state = 'available' ORDER BY id`)
	if err != nil {
		e.t.Fatal(err)
	}
	var lista []argos.ArgsInformarSincronizacion
	for filas.Next() {
		var b []byte
		var a argos.ArgsInformarSincronizacion
		if err := filas.Scan(&b); err != nil {
			e.t.Fatal(err)
		}
		_ = json.Unmarshal(b, &a)
		lista = append(lista, a)
	}
	filas.Close()
	e.exec("UPDATE river.river_job SET state = 'completed', finalized_at = now() WHERE kind = 'integracion.argos_informar_sincronizacion' AND state = 'available'")
	estados := []string{}
	for _, a := range lista {
		r, err := e.mod.InformarSincronizacion.Ejecutar(pruebas.ContextoSistema(e.pool), a)
		if err != nil && r.Estado != "error" {
			e.t.Fatalf("informe: %v", err)
		}
		estados = append(estados, r.Estado)
	}
	return estados
}

func etapa(s argos.Sincronizacion, recurso string) argos.EtapaSincronizacion {
	for _, x := range s.Etapas {
		if x.Recurso == recurso {
			return x
		}
	}
	return argos.EtapaSincronizacion{}
}

func TestPrimeraSincronizacion080(t *testing.T) {
	e := nuevoEntorno(t)
	e.limpiarSinc()
	e.sembrarSinc("1.18.0", "0.8.0")
	admin, id, _ := e.conectado()

	// Activar encola el job (único por conector: encolarlo otra vez no lo duplica).
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_primera_sincronizacion' AND args->>'conector_id' = $1`,
		strconv.FormatInt(id, 10)); n != 1 {
		t.Fatalf("activar debía encolar la primera sincronización: %d", n)
	}
	cola, err := integracion.NuevoClienteInsercion(e.pool, migraciones.EsquemaRiver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := cola.Insert(context.Background(), argos.ArgsPrimeraSincronizacion{ConectorID: id}, nil); err != nil || !r.UniqueSkippedAsDuplicate {
		t.Fatalf("el job debía ser único por conector: %v %+v", err, r)
	}

	var contratosAntes int
	e.argos.con(func() { contratosAntes = e.argos.contratos })
	r, err := e.sincronizar(id)
	if err != nil {
		t.Fatal(err)
	}
	var contratosDespues int
	var consultas []string
	var cabeceras []string
	e.argos.con(func() {
		contratosDespues = e.argos.contratos
		consultas = slices.Clone(e.argos.sinc080.consultas)
		cabeceras = slices.Clone(e.argos.sinc080.cabeceras)
	})
	// Antes de leer, la llamada autenticada que confirma el canje (PUT …/contrato).
	if contratosDespues <= contratosAntes {
		t.Fatal("la sincronización debía empezar con PUT /integraciones/iris/contrato")
	}
	if r.Estado != argos.SincCompleta || !r.ExcluirRetirados || r.Origen != "activacion" {
		t.Fatalf("sincronización: %+v", r.Sincronizacion)
	}
	if v, _ := uuid.Parse(r.ID.String()); v.Version() != 7 {
		t.Fatalf("sincronizacion_id debe ser UUID v7: %s", r.ID)
	}
	for recurso, esperado := range map[string]int32{"empresas": 1, "zonas": 2, "planes": 3, "routers": 3, "cuentas_bancarias": 1, "bancos": 2, "clientes": 3, "servicios": 4} {
		if x := etapa(r.Sincronizacion, recurso); x.Estado != argos.SincCompleta || x.Procesados != esperado || x.Error != nil {
			t.Fatalf("etapa %s: %+v (esperaba %d)", recurso, x, esperado)
		}
	}
	// Solo clientes con un servicio no retirado, con limit=200 y por cursor; sin cabeceras de Iris.
	for _, c := range consultas {
		if !strings.Contains(c, "excluir_retirados=true") || !strings.Contains(c, "limit=200") {
			t.Fatalf("lista sin excluir_retirados o sin limit=200: %s", c)
		}
	}
	if len(cabeceras) != 2 || cabeceras[0] != "" || cabeceras[1] != "" {
		t.Fatalf("cuentas y bancos se leen sin X-Iris-Actor: %v", cabeceras)
	}
	// B5: el paso de cuentas llena la instantánea con titular y tipo (FijarCargasBancarias).
	if n := e.contar(`SELECT count(*) FROM integracion.argos_cuentas_bancarias WHERE id = 1 AND titular = 'Internet del Norte, S.A.' AND tipo = 'monetaria' AND activa`); n != 1 {
		t.Fatal("la primera sincronización llena la instantánea de cuentas")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id IN (701, 702, 703, 704)`); n != 3 {
		t.Fatalf("clientes en la instantánea: %d (el 703 solo tiene retirados)", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id IN (701, 702, 704) AND empresas = '{1}' AND empresa_id = 1`); n != 3 {
		t.Fatalf("las empresas del cliente salen de sus servicios: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id IN (7010, 7020, 7040, 7041)`); n != 4 {
		t.Fatalf("servicios: %d", n)
	}
	// Lista blanca: lo de fuera no se guarda.
	if s := e.consultarTexto(`SELECT row_to_json(s)::text FROM integracion.argos_servicios s WHERE id = 7010`); strings.Contains(s, "AA:BB:CC") || strings.Contains(s, "1.00") {
		t.Fatalf("la instantánea guardó campos fuera de la lista blanca: %s", s)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_zonas WHERE id IN (3, 4)`) + e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id IN (5, 6, 7)`); n != 5 {
		t.Fatalf("zonas y routers: %d", n)
	}
	// Auditoría y eventos.
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.sincronizacion_iniciada' AND payload->>'sincronizacion_id' = $1`, r.ID.String()); n != 1 {
		t.Fatalf("argos.sincronizacion_iniciada: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.sincronizacion_terminada' AND payload->>'resultado' = 'completa' AND jsonb_array_length(payload->'recursos') = 8`); n != 1 {
		t.Fatalf("argos.sincronizacion_terminada: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.sincronizar_avance' AND entidad_id = $1`, r.ID.String()); n == 0 || n > 17 {
		t.Fatalf("el avance se audita por cambio de etapa, no por página: %d", n)
	}

	// Informe a Argos: uno por cambio de etapa o de estado; los viejos se omiten, el último llega.
	estados := e.informar()
	if !slices.Contains(estados, "informado") {
		t.Fatalf("informes: %v", estados)
	}
	var informes []map[string]any
	var guardado map[string]any
	e.argos.con(func() { informes, guardado = slices.Clone(e.argos.sinc080.informes), e.argos.sinc080.guardado })
	if len(informes) != 1 || guardado["estado"] != "completa" || guardado["sincronizacion_id"] != r.ID.String() {
		t.Fatalf("Argos debía recibir solo el último estado: %d informes, %v", len(informes), guardado)
	}
	etapas, _ := guardado["etapas"].([]any)
	if len(etapas) != 8 {
		t.Fatalf("etapas del informe: %v", guardado["etapas"])
	}
	if s := fmt.Sprint(guardado); strings.Contains(s, "Cliente 70") {
		t.Fatalf("el informe no lleva datos de clientes: %s", s)
	}

	// Pantalla de estado: lo que Iris sabe, sin secretos.
	est := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/estado", nil)
	if est.status != 200 || strings.Contains(string(est.crudo), tokenArgos) {
		t.Fatalf("estado: %d %s", est.status, est.crudo)
	}
	sinc, _ := est.cuerpo["sincronizacion"].(map[string]any)
	if sinc["estado"] != "completa" || sinc["informeEstado"] != "informado" || est.cuerpo["contratoArgos"] != "0.8.0" {
		t.Fatalf("estado: %s", est.crudo)
	}
	if act, _ := est.cuerpo["activacion"].(map[string]any); act == nil || act["usuarioId"] == nil {
		t.Fatalf("quién activó: %s", est.crudo)
	}
	e.usuario("lectura").pedir(http.MethodGet, "/api/v1/integraciones/argosisp/estado", nil).problema(t, 403, "prohibido")

	// Ya terminada: el job no la repite. Volver a lanzarla (manual) crea otra con un id mayor.
	if r2, err := e.sincronizar(id); err != nil || !r2.Omitido {
		t.Fatalf("una terminada no se repite: %v %+v", err, r2)
	}
	e.usuario("lectura").pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/sincronizar", id), nil).problema(t, 403, "prohibido")
	// Recién terminada completa: 409 sincronizacion_reciente (10 minutos).
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/sincronizar", id), nil).problema(t, 409, "sincronizacion_reciente")
	// Si Argos cambió de contrato desde entonces, sí se puede volver a lanzar.
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.8.1' WHERE id = $1`, id)
	otra := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/sincronizar", id), nil)
	if otra.status != 202 || otra.cuerpo["estado"] != "en_curso" || otra.cuerpo["origen"] != "manual" || otra.cuerpo["id"].(string) <= r.ID.String() {
		t.Fatalf("volver a lanzar: %d %s", otra.status, otra.crudo)
	}
	r3, err := e.sincronizar(id)
	if err != nil || r3.Estado != argos.SincCompleta || r3.ID.String() != otra.cuerpo["id"] {
		t.Fatalf("segunda: %v %+v", err, r3)
	}
	// Idempotente: releer no duplica filas.
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id IN (7010, 7020, 7040, 7041)`); n != 4 {
		t.Fatalf("servicios tras la segunda: %d", n)
	}

	// 409 sincronizacion_vieja: Argos ya tiene uno más nuevo; Iris lo descarta sin reintentar.
	e.argos.con(func() {
		e.argos.sinc080.guardado = map[string]any{"sincronizacion_id": "ffffffff-ffff-7fff-bfff-ffffffffffff", "actualizada_en": "2099-01-01T00:00:00.000Z"}
	})
	if estados := e.informar(); !slices.Contains(estados, "descartado") {
		t.Fatalf("409 sincronizacion_vieja: %v", estados)
	}
	if s := e.consultarTexto(`SELECT informe_estado FROM integracion.argos_sincronizaciones WHERE id = $1`, r3.ID); s != "descartado" {
		t.Fatalf("informe_estado: %s", s)
	}
}

func TestPrimeraSincronizacionArgosAnterior(t *testing.T) {
	e := nuevoEntorno(t)
	e.limpiarSinc()
	e.sembrarSinc("1.17.0", "0.7.1")
	_, id, _ := e.conectado()
	r, err := e.sincronizar(id)
	if err != nil {
		t.Fatal(err)
	}
	// Listas completas (sin excluir_retirados), sin cuentas ni bancos y sin informe (§4.1).
	if r.Estado != argos.SincConErrores || r.ExcluirRetirados {
		t.Fatalf("sincronización: %+v", r.Sincronizacion)
	}
	for _, recurso := range []string{"cuentas_bancarias", "bancos"} {
		if x := etapa(r.Sincronizacion, recurso); x.Estado != argos.SincConErrores || x.Error == nil || *x.Error != argos.MotivoArgosAnterior080 {
			t.Fatalf("%s con Argos anterior: %+v", recurso, x)
		}
	}
	if x := etapa(r.Sincronizacion, "clientes"); x.Estado != argos.SincCompleta || x.Procesados != 4 {
		t.Fatalf("clientes con la lista completa: %+v", x)
	}
	var consultas []string
	var cabeceras []string
	e.argos.con(func() {
		consultas, cabeceras = slices.Clone(e.argos.sinc080.consultas), slices.Clone(e.argos.sinc080.cabeceras)
	})
	for _, c := range consultas {
		if strings.Contains(c, "excluir_retirados") {
			t.Fatalf("con Argos anterior a 0.8.0 no se manda excluir_retirados: %s", c)
		}
	}
	if len(cabeceras) != 0 {
		t.Fatal("con Argos anterior no se leen cuentas ni bancos")
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_informar_sincronizacion'`); n != 0 {
		t.Fatalf("con Argos anterior no se informa el avance: %d", n)
	}
	if s := e.consultarTexto(`SELECT informe_estado FROM integracion.argos_sincronizaciones WHERE id = $1`, r.ID); s != "pendiente" {
		t.Fatalf("informe_estado: %s", s)
	}
	// La pantalla dice qué queda apagado por la versión.
	est := e.usuario("admin").pedir(http.MethodGet, "/api/v1/integraciones/argosisp/estado", nil)
	if !strings.Contains(string(est.crudo), `"informe_sincronizacion"`) || !strings.Contains(string(est.crudo), `"primera_sincronizacion_completa"`) {
		t.Fatalf("funciones apagadas: %s", est.crudo)
	}
}

func TestPrimeraSincronizacionReanudable(t *testing.T) {
	e := nuevoEntorno(t)
	e.limpiarSinc()
	e.sembrarSinc("1.18.0", "0.8.0")
	_, id, _ := e.conectado()
	// Una lápida de servicio.eliminado: la lista nunca la revive.
	e.exec(`INSERT INTO integracion.argos_eliminados (tipo, id, empresa_id, cliente_id, conector_id, eliminado_en, confirmado_en)
		VALUES ('servicio', 7041, 1, 704, $1, now(), now())`, id)
	e.argos.con(func() { e.argos.sinc080.fallarServicios = true })
	if _, err := e.sincronizar(id); err == nil {
		t.Fatal("con Argos caído a mitad de la lista, el job debía pedir otro intento")
	}
	var cursor *string
	var estado, errEtapa string
	if err := e.pool.QueryRow(context.Background(), `SELECT e.cursor, e.estado, coalesce(e.error, '') FROM integracion.argos_sincronizacion_etapas e
		JOIN integracion.argos_sincronizaciones s ON s.id = e.sincronizacion_id WHERE s.conector_id = $1 AND e.recurso = 'servicios'`, id).Scan(&cursor, &estado, &errEtapa); err != nil {
		t.Fatal(err)
	}
	if cursor == nil || estado != "en_curso" || !strings.Contains(errEtapa, "Argos no respondió") {
		t.Fatalf("tras el corte: cursor %v, estado %s, error %q", cursor, estado, errEtapa)
	}
	e.argos.con(func() {
		e.argos.sinc080.fallarServicios = false
		e.argos.sinc080.consultas = nil
	})
	r, err := e.sincronizar(id)
	if err != nil || r.Estado != argos.SincCompleta {
		t.Fatalf("reanudar: %v %+v", err, r)
	}
	var consultas []string
	e.argos.con(func() { consultas = slices.Clone(e.argos.sinc080.consultas) })
	for _, c := range consultas {
		if strings.HasPrefix(c, "/api/v1/clientes") || !strings.Contains(c, "cursor="+*cursor) {
			t.Fatalf("reanudar debía seguir desde el cursor %s: %v", *cursor, consultas)
		}
	}
	if x := etapa(r.Sincronizacion, "servicios"); x.Procesados != 3 || x.Omitidos != 1 || x.Error != nil {
		t.Fatalf("servicios (uno con lápida): %+v", x)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = 7041`); n != 0 {
		t.Fatal("la lista revivió un servicio con lápida")
	}
}

// Certificado TLS de Argos (§1.5): registrar y avisar, nunca bloquear.
func TestCertificadoArgos(t *testing.T) {
	// El cliente anota el certificado de la API que respondió por HTTPS.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { escribir(w, map[string]any{"ok": true}) }))
	defer srv.Close()
	cli := conector.NuevoCliente(argos.Remoto, srv.URL, "atk_x", conector.OpcionesCliente{HTTP: srv.Client()})
	ctx, visto := conector.ConCapturaTLS(context.Background())
	if err := cli.Pedir(ctx, http.MethodGet, "/integraciones/iris/estado", nil, nil, true, nil); err != nil {
		t.Fatal(err)
	}
	v := visto()
	if v == nil || len(v.HuellaSHA256) != 64 || v.Host == "" || v.VenceEn.IsZero() {
		t.Fatalf("certificado visto: %+v", v)
	}

	e := nuevoEntorno(t)
	e.limpiarSinc()
	e.sembrarSinc("1.18.0", "0.8.0")
	admin, id, _ := e.conectado()
	ctxS := pruebas.ContextoSistema(e.pool)
	a := conector.CertificadoVisto{HuellaSHA256: strings.Repeat("a", 64), Emisor: "CN=R11,O=Let's Encrypt", Sujeto: "CN=argos.prueba", Host: "argos.prueba", VenceEn: time.Now().Add(60 * 24 * time.Hour)}
	if r, err := e.mod.RegistrarCertificado.Ejecutar(ctxS, argos.EntradaCertificado{ConectorID: id, Visto: a}); err != nil || !r.Nuevo || r.Cambiado {
		t.Fatalf("primero: %v %+v", err, r)
	}
	if r, err := e.mod.RegistrarCertificado.Ejecutar(ctxS, argos.EntradaCertificado{ConectorID: id, Visto: a}); err != nil || r.Nuevo {
		t.Fatalf("el mismo otra vez no es nuevo: %v %+v", err, r)
	}
	b := a
	b.HuellaSHA256 = strings.Repeat("b", 64)
	if r, err := e.mod.RegistrarCertificado.Ejecutar(ctxS, argos.EntradaCertificado{ConectorID: id, Visto: b}); err != nil || !r.Cambiado {
		t.Fatalf("cambio: %v %+v", err, r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.certificado_cambiado' AND payload->>'huella_anterior' = $1`, a.HuellaSHA256); n != 1 {
		t.Fatalf("argos.certificado_cambiado: %d", n)
	}
	// No marca el conector ni detiene nada: sigue activo y las lecturas siguen.
	if s := e.consultarTexto(`SELECT estado FROM integracion.conectores WHERE id = $1`, id); s != "activo" {
		t.Fatalf("el conector cambió de estado: %s", s)
	}
	if _, err := e.sincronizar(id); err != nil {
		t.Fatalf("la sincronización debía seguir: %v", err)
	}
	// A → B → A: volver a uno anterior también es un cambio y avisa.
	if r, err := e.mod.RegistrarCertificado.Ejecutar(ctxS, argos.EntradaCertificado{ConectorID: id, Visto: a}); err != nil || !r.Cambiado {
		t.Fatalf("A → B → A: %v %+v", err, r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'argos.certificado_cambiado' AND payload->>'huella_anterior' = $1`, b.HuellaSHA256); n != 1 {
		t.Fatalf("argos.certificado_cambiado de B a A: %d", n)
	}
	if r, err := e.mod.RegistrarCertificado.Ejecutar(ctxS, argos.EntradaCertificado{ConectorID: id, Visto: b}); err != nil || !r.Cambiado {
		t.Fatalf("A → B: %v %+v", err, r)
	}
	est := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/estado", nil)
	cert, _ := est.cuerpo["certificado"].(map[string]any)
	ant, _ := cert["anterior"].(map[string]any)
	if cert["cambiado"] != true || cert["huellaSha256"] != b.HuellaSHA256 || ant["huellaSha256"] != a.HuellaSHA256 {
		t.Fatalf("certificado en el estado: %s", est.crudo)
	}
}

// Revisión de seguridad de B7: lo que Argos mande fuera del alcance del canje no se guarda (cuenta como
// omitido), y un cursor enorme o una página más grande que el límite son una respuesta ilegible.
func TestPrimeraSincronizacionDefensas(t *testing.T) {
	e := nuevoEntorno(t)
	e.limpiarSinc()
	e.sembrarSinc("1.18.0", "0.8.0")
	e.argos.con(func() {
		// El canje informa solo la empresa 1 (alcance_global: false).
		falso := false
		e.argos.alcanceGlobal = &falso
		e.argos.zonas = append(e.argos.zonas, map[string]any{"id": 9, "nombre": "Ajena", "empresa_id": 2})
		e.argos.routers = append(e.argos.routers, map[string]any{"id": 9, "nombre": "Router ajeno", "empresa_id": 2})
		e.argos.servicios[704] = append(e.argos.servicios[704], map[string]any{"id": 7049, "cliente_id": 704, "empresa_id": int64(2), "estado": "activo"})
		// Sin empresa: no se atribuye a nadie y se omite.
		e.argos.servicios[704] = append(e.argos.servicios[704], map[string]any{"id": 7048, "cliente_id": 704, "empresa_id": int64(0), "estado": "activo"})
	})
	_, id, _ := e.conectado()
	r, err := e.sincronizar(id)
	if err != nil || r.Estado != argos.SincCompleta {
		t.Fatalf("sincronizar: %v %+v", err, r)
	}
	if x := etapa(r.Sincronizacion, "zonas"); x.Procesados != 2 || x.Omitidos != 1 {
		t.Fatalf("zonas: %+v", x)
	}
	if x := etapa(r.Sincronizacion, "routers"); x.Procesados != 3 || x.Omitidos != 1 {
		t.Fatalf("routers: %+v", x)
	}
	if x := etapa(r.Sincronizacion, "servicios"); x.Procesados != 4 || x.Omitidos != 2 {
		t.Fatalf("servicios: %+v", x)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_zonas WHERE id = 9`) + e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 9`) +
		e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id IN (7048, 7049)`); n != 0 {
		t.Fatalf("se guardaron filas fuera del alcance: %d", n)
	}

	// Cursor de más de 512 caracteres: la etapa queda con errores (ilegible), sin guardarlo.
	e.limpiarSinc()
	e.argos.con(func() {
		e.argos.planes = []map[string]any{{"id": 1}, {"id": 2}, {"id": 3}}
		e.argos.sinc080.cursorEnorme = true
	})
	e.exec(`DELETE FROM river.river_job WHERE kind = 'integracion.argos_primera_sincronizacion'`)
	admin := e.usuario("admin")
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/sincronizar", id), nil); r.status != 202 {
		t.Fatalf("sincronizar: %d %s", r.status, r.crudo)
	}
	r, err = e.sincronizar(id)
	if err != nil {
		t.Fatal(err)
	}
	if x := etapa(r.Sincronizacion, "planes"); x.Estado != argos.SincConErrores || x.Error == nil || !strings.Contains(*x.Error, "no pudo leer") {
		t.Fatalf("cursor enorme: %+v", x)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_sincronizacion_etapas WHERE length(cursor) > 512`); n != 0 {
		t.Fatal("se guardó un cursor enorme")
	}
}
