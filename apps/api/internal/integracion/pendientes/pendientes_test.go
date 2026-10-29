package pendientes_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const tokenArgos = "atk_prueba_pendientes_0123456789"

// llamada es una escritura que recibió el Argos falso.
type llamada struct {
	Ruta   string
	Clave  string
	Token  string
	Cuerpo map[string]any
	Crudo  []byte
}

// argosFalso responde el canje y registra las escrituras de §3. respuestas fija el status por
// ruta (por defecto 201).
type argosFalso struct {
	srv        *httptest.Server
	mu         sync.Mutex
	llamadas   []llamada
	respuestas map[string][]int // ruta → statuses en orden (el último se repite)
	detalle    string
	cuerpos    map[string]string // ruta → cuerpo del 2xx (por defecto {"ok":true})
	tipos      map[string]string // ruta → type del problem+json (por defecto urn:argos:sin_existencias)
}

func nuevoArgosFalso(t *testing.T) *argosFalso {
	a := &argosFalso{respuestas: map[string][]int{}, cuerpos: map[string]string{}, tipos: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/canjear", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + tokenArgos + `","empresas":[1]}`))
	})
	mux.HandleFunc("POST /api/v1/", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		crudo, _ := io.ReadAll(r.Body)
		var c map[string]any
		_ = json.Unmarshal(crudo, &c)
		ruta := strings.TrimPrefix(r.URL.Path, "/api/v1")
		a.llamadas = append(a.llamadas, llamada{Ruta: ruta, Clave: r.Header.Get("Idempotency-Key"), Token: r.Header.Get("Authorization"), Cuerpo: c, Crudo: crudo})
		status := 201
		if lista := a.respuestas[ruta]; len(lista) > 0 {
			status = lista[0]
			if len(lista) > 1 {
				a.respuestas[ruta] = lista[1:]
			}
		}
		if status >= 400 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(status)
			tipo, ok := a.tipos[ruta]
			if !ok {
				tipo = "urn:argos:sin_existencias"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"type": tipo, "title": "Error", "status": status, "detail": a.detalle})
			return
		}
		cuerpo, ok := a.cuerpos[ruta]
		if !ok {
			cuerpo = `{"ok":true}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(cuerpo))
	})
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *argosFalso) responder(ruta string, statuses ...int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.respuestas[ruta] = statuses
}

func (a *argosFalso) copia() []llamada {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]llamada(nil), a.llamadas...)
}

type entorno struct {
	t     *testing.T
	pool  *pgxpool.Pool
	argos *argosFalso
	am    *argos.Modulo
	m     *pendientes.Modulo
	bus   *integracion.Bus
	reloj time.Time
	conID int64
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	for _, sql := range []string{
		"UPDATE integracion.conectores SET estado = 'apagado'",
		"DELETE FROM integracion.pendientes",
		"UPDATE integracion.outbox SET publicado_en = now() WHERE publicado_en IS NULL",
		"INSERT INTO nucleo.empresas (id, nombre) VALUES (1, 'Empresa 1'), (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING",
	} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	fa := nuevoArgosFalso(t)
	am := argos.Nuevo(argos.Config{
		Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true, Timeout: 2 * time.Second,
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	})
	// El canje entrega un iris_pat_ (contrato §1.2); aquí uno fijo: el Argos falso no lo valida.
	am.Nucleo().FijarEmisorToken(conector.EmisorDesde(func(context.Context, string) (int64, string, error) {
		return 1, "iris_pat_" + strings.Repeat("p", 32), nil
	}, nil))
	e := &entorno{t: t, pool: pool, argos: fa, am: am, bus: integracion.NuevoBus(),
		reloj: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	e.m = pendientes.Nuevo(pendientes.Config{Argos: am, Reloj: func() time.Time { return e.reloj }})
	e.m.Suscribir(e.bus)
	ctx := e.ctx()
	c, err := am.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: fa.srv.URL + "/conectar#codigo-de-prueba-123"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := am.ActivarConector.Ejecutar(ctx, argos.EntradaCambioConector{ID: c.ID, Version: c.Version}); err != nil {
		t.Fatal(err)
	}
	e.conID = c.ID
	return e
}

func (e *entorno) ctx() context.Context {
	return nucleo.ConBD(contexto.Sistema(context.Background(), contexto.CanalWorker), e.pool)
}

// cerrar emite orden.cerrada por el outbox (empresa 1) y corre el relay (el suscriptor encola).
func (e *entorno) cerrar(payload map[string]any) {
	e.t.Helper()
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		empresa := int64(1)
		_, err := integracion.Insertar(ctx, tx, "orden.cerrada", &empresa, "req-prueba", payload)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	relay := &integracion.Relay{BD: e.pool, Bus: e.bus, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := relay.Ejecutar(ctx); err != nil {
		e.t.Fatal(err)
	}
	var fallidos int
	_ = e.pool.QueryRow(ctx, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.cerrada' AND publicado_en IS NULL").Scan(&fallidos)
	if fallidos != 0 {
		e.t.Fatal("el suscriptor de orden.cerrada falló")
	}
}

func (e *entorno) procesar() int {
	e.t.Helper()
	n, err := e.m.Procesar(e.ctx())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

type fila struct {
	Operacion, Clave, Estado, Error string
	Intentos                        int
	Proximo                         time.Time
	Incompleto                      bool
	ID                              int64
	Version                         int32
}

func (e *entorno) pendientes(orden string) map[string]fila {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT id, operacion, idempotency_key, estado, coalesce(ultimo_error, ''), intentos,
		proximo_intento, datos_incompletos, version FROM integracion.pendientes WHERE entidad_id = $1`, orden)
	if err != nil {
		e.t.Fatal(err)
	}
	salida := map[string]fila{}
	for rows.Next() {
		var f fila
		if err := rows.Scan(&f.ID, &f.Operacion, &f.Clave, &f.Estado, &f.Error, &f.Intentos, &f.Proximo, &f.Incompleto, &f.Version); err != nil {
			e.t.Fatal(err)
		}
		salida[f.Clave] = f
	}
	return salida
}

func (e *entorno) eventos(evento, orden string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>'entidad_id' = $2 OR payload->>'orden_id' = $2)", evento, orden).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func nuevaOrden() string { return uuid.Must(uuid.NewV7()).String() }

// Pruebas ----------------------------------------------------------------------------------

func TestInstalacionExitosaEncolaYEjecutaConIdempotencyKey(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	payload := map[string]any{
		"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "empresa_id": 1,
		"cliente_id": 10, "servicio_id": "20", "tecnico_id": 7, "bodega_id": 3,
		"equipos":    []any{map[string]any{"accion": "instalar", "unidad_id": 55, "serie": "ZTE123", "mac": "AA:BB"}, map[string]any{"accion": "instalar", "serie": "ONT-9"}},
		"materiales": []any{map[string]any{"producto_id": 4, "cantidad": 25.5}, map[string]any{"producto_id": 6, "cantidad": "2"}},
		"cargo":      map[string]any{"producto_id": 8, "descripcion": "Instalación"},
	}
	e.cerrar(payload)
	ps := e.pendientes(orden)
	claves := []string{"iris:instalacion:" + orden, "iris:comodato:" + orden + ":ZTE123", "iris:comodato:" + orden + ":ONT-9",
		"iris:consumo:" + orden, "iris:cargo:" + orden}
	for _, k := range claves {
		if ps[k].Estado != "pendiente" {
			t.Fatalf("falta %s: %+v", k, ps)
		}
	}
	if len(ps) != len(claves) {
		t.Fatalf("pendientes de más: %+v", ps)
	}
	// Evento repetido (el relay puede entregar dos veces): no duplica.
	e.cerrar(payload)
	if len(e.pendientes(orden)) != len(claves) {
		t.Fatal("un evento repetido duplicó pendientes")
	}

	if n := e.procesar(); n != len(claves) {
		t.Fatalf("procesó %d", n)
	}
	porClave := map[string]llamada{}
	for _, l := range e.argos.copia() {
		if l.Token != "Bearer "+tokenArgos {
			t.Fatalf("sin token: %+v", l)
		}
		porClave[l.Clave] = l
	}
	if l := porClave["iris:instalacion:"+orden]; l.Ruta != "/servicios/20/activar-instalacion" || l.Cuerpo["origen_tipo"] != "iris_orden" || l.Cuerpo["origen_id"] != orden {
		t.Fatalf("activar: %+v", l)
	}
	if l := porClave["iris:comodato:"+orden+":ZTE123"]; l.Ruta != "/servicios/20/equipos" || l.Cuerpo["unidad_id"] != float64(55) {
		t.Fatalf("comodato por unidad: %+v", l)
	}
	if l := porClave["iris:comodato:"+orden+":ONT-9"]; l.Cuerpo["serie"] != "ONT-9" || l.Cuerpo["unidad_id"] != nil {
		t.Fatalf("comodato por serie: %+v", l)
	}
	c := porClave["iris:consumo:"+orden]
	lineas, _ := c.Cuerpo["lineas"].([]any)
	if c.Ruta != "/inventario/consumos" || c.Cuerpo["bodega_id"] != float64(3) || c.Cuerpo["servicio_id"] != float64(20) || len(lineas) != 2 ||
		c.Cuerpo["origen_tipo"] != "iris_orden" || lineas[0].(map[string]any)["cantidad"] != 25.5 {
		t.Fatalf("consumo: %+v", c)
	}
	if l := porClave["iris:cargo:"+orden]; l.Ruta != "/facturas/cargo-unico" || l.Cuerpo["cliente_id"] != float64(10) || l.Cuerpo["producto_id"] != float64(8) {
		t.Fatalf("cargo: %+v", l)
	}
	for _, f := range e.pendientes(orden) {
		if f.Estado != "hecho" || f.Intentos != 1 {
			t.Fatalf("debía quedar hecho: %+v", f)
		}
	}
	if n := e.eventos("integracion.pendiente_hecho", orden); n != len(claves) {
		t.Fatalf("pendiente_hecho: %d", n)
	}
	// Nada más que hacer.
	if n := e.procesar(); n != 0 {
		t.Fatalf("procesó de nuevo %d", n)
	}
}

func TestReintentosConEsperaDelContratoYAgotado(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	e.argos.responder("/servicios/20/activar-instalacion", 503)
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": 20})
	clave := "iris:instalacion:" + orden
	inicio := e.reloj
	esperas := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}
	for i, espera := range esperas {
		if n := e.procesar(); n != 1 {
			t.Fatalf("intento %d: procesó %d", i+1, n)
		}
		f := e.pendientes(orden)[clave]
		if f.Estado != "pendiente" || f.Intentos != i+1 || !f.Proximo.Equal(e.reloj.Add(espera)) || !strings.Contains(f.Error, "HTTP 503") {
			t.Fatalf("intento %d: %+v (esperaba próximo %s)", i+1, f, e.reloj.Add(espera))
		}
		// Antes de la espera no se intenta.
		e.reloj = e.reloj.Add(espera - time.Second)
		if n := e.procesar(); n != 0 {
			t.Fatalf("intentó antes de tiempo (%d)", i+1)
		}
		e.reloj = e.reloj.Add(time.Second)
	}
	if n := e.procesar(); n != 1 {
		t.Fatal("sexto intento")
	}
	f := e.pendientes(orden)[clave]
	if f.Estado != "agotado" || f.Intentos != 6 {
		t.Fatalf("debía agotarse: %+v", f)
	}
	if !e.reloj.Equal(inicio.Add(time.Minute + 5*time.Minute + 30*time.Minute + 2*time.Hour + 12*time.Hour)) {
		t.Fatal("reloj")
	}
	if n := e.eventos("integracion.pendiente_agotado", orden); n != 1 {
		t.Fatalf("alerta de agotado: %d", n)
	}
	if n := len(e.argos.copia()); n != 6 {
		t.Fatalf("llamadas a Argos: %d", n)
	}
	for _, l := range e.argos.copia() {
		if l.Clave != clave {
			t.Fatalf("la clave debe ser siempre la misma: %s", l.Clave)
		}
	}
	e.reloj = e.reloj.Add(48 * time.Hour)
	if n := e.procesar(); n != 0 {
		t.Fatal("un agotado no se reintenta solo")
	}
}

func TestErrorDeNegocioNoSeReintentaYNoGuardaCredenciales(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	e.argos.detalle = "Existencia insuficiente en la bodega 3 (token atk_filtrado_secreto)"
	e.argos.responder("/inventario/consumos", 422)
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "visita", "resultado": "exitosa", "servicio_id": 20, "bodega_id": 3,
		"materiales": []any{map[string]any{"producto_id": 4, "cantidad": 300}}})
	e.procesar()
	f := e.pendientes(orden)["iris:consumo:"+orden]
	if f.Estado != "fallido_negocio" || f.Intentos != 1 || !strings.Contains(f.Error, "HTTP 422 urn:argos:sin_existencias: Existencia insuficiente") ||
		strings.Contains(f.Error, "atk_filtrado_secreto") {
		t.Fatalf("fallido_negocio: %+v", f)
	}
	if n := e.eventos("integracion.pendiente_fallido", orden); n != 1 {
		t.Fatalf("aviso al supervisor: %d", n)
	}
	e.reloj = e.reloj.Add(24 * time.Hour)
	if n := e.procesar(); n != 0 {
		t.Fatal("un 4xx de negocio no se reintenta")
	}
	var detalle string
	_ = e.pool.QueryRow(context.Background(), "SELECT payload::text FROM integracion.outbox WHERE evento = 'integracion.pendiente_fallido' AND payload->>'entidad_id' = $1", orden).Scan(&detalle)
	if strings.Contains(detalle, "atk_filtrado") || !strings.Contains(detalle, "Existencia insuficiente") {
		t.Fatalf("evento: %s", detalle)
	}
	// 401/403 de Argos no son de negocio: se reintentan.
	orden2 := nuevaOrden()
	e.argos.responder("/servicios/21/retirar", 403, 201)
	e.cerrar(map[string]any{"orden_id": orden2, "tipo": "retiro", "resultado": "exitosa", "servicio_id": 21, "retirar_servicio": true})
	e.procesar()
	if f := e.pendientes(orden2)["iris:retiro:"+orden2]; f.Estado != "pendiente" || f.Intentos != 1 {
		t.Fatalf("403 se reintenta: %+v", f)
	}
	e.reloj = e.reloj.Add(time.Minute)
	e.procesar()
	if f := e.pendientes(orden2)["iris:retiro:"+orden2]; f.Estado != "hecho" {
		t.Fatalf("retiro: %+v", f)
	}
}

func TestRetiroYCambioDeEquipoConDependencia(t *testing.T) {
	e := nuevoEntorno(t)
	retiro := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": retiro, "tipo": "retiro", "resultado": "exitosa", "servicio_id": 30, "bodega_id": 3, "retirar_servicio": true,
		"equipos": []any{
			map[string]any{"accion": "retirar", "unidad_id": 71, "serie": "A1", "condicion": "bueno"},
			map[string]any{"accion": "retirar", "unidad_id": 72, "serie": "A2", "condicion": "perdido"},
		}})
	e.procesar()
	porClave := map[string]llamada{}
	for _, l := range e.argos.copia() {
		porClave[l.Clave] = l
	}
	if l := porClave["iris:devolucion:"+retiro+":71"]; l.Ruta != "/servicios/30/equipos/71/devolver" || l.Cuerpo["bodega_id"] != float64(3) || l.Cuerpo["condicion"] != "bueno" {
		t.Fatalf("devolución: %+v", l)
	}
	if l := porClave["iris:perdida:"+retiro+":72"]; l.Ruta != "/servicios/30/equipos/72/perdida" {
		t.Fatalf("pérdida: %+v", l)
	}
	if l := porClave["iris:retiro:"+retiro]; l.Ruta != "/servicios/30/retirar" {
		t.Fatalf("retiro del servicio: %+v", l)
	}

	// Cambio de equipo: la asignación espera a la devolución.
	cambio := nuevaOrden()
	e.argos.responder("/servicios/40/equipos/81/devolver", 503, 201)
	e.cerrar(map[string]any{"orden_id": cambio, "tipo": "cambio_equipo", "resultado": "exitosa", "servicio_id": 40, "bodega_id": 3,
		"equipos": []any{
			map[string]any{"accion": "instalar", "serie": "NUEVO-1"},
			map[string]any{"accion": "retirar", "unidad_id": 81, "serie": "VIEJO-1", "condicion": "danado"},
		}})
	antes := len(e.argos.copia())
	e.procesar()
	nuevas := e.argos.copia()[antes:]
	if len(nuevas) != 1 || nuevas[0].Clave != "iris:devolucion:"+cambio+":81" {
		t.Fatalf("con la devolución fallida, la asignación no debe llamarse: %+v", nuevas)
	}
	ps := e.pendientes(cambio)
	asig := ps["iris:comodato:"+cambio+":NUEVO-1"]
	if asig.Estado != "pendiente" || asig.Intentos != 0 {
		t.Fatalf("asignación esperando: %+v", asig)
	}
	e.reloj = e.reloj.Add(time.Minute)
	e.procesar()
	nuevas = e.argos.copia()[antes:]
	if len(nuevas) != 3 || nuevas[1].Clave != "iris:devolucion:"+cambio+":81" || nuevas[2].Clave != "iris:comodato:"+cambio+":NUEVO-1" {
		t.Fatalf("orden devolución → asignación: %+v", nuevas)
	}
	if f := e.pendientes(cambio)["iris:comodato:"+cambio+":NUEVO-1"]; f.Estado != "hecho" {
		t.Fatalf("asignación: %+v", f)
	}
}

func TestConectorApagadoEncolaYSinConectorOmite(t *testing.T) {
	e := nuevoEntorno(t)
	// Conector registrado pero apagado: se encola igual y el worker reintenta.
	if _, err := e.pool.Exec(context.Background(), "UPDATE integracion.conectores SET estado = 'apagado'"); err != nil {
		t.Fatal(err)
	}
	orden := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": 20})
	if e.procesar() != 1 || len(e.argos.copia()) != 0 {
		t.Fatal("con el conector apagado no se llama a Argos")
	}
	if f := e.pendientes(orden)["iris:instalacion:"+orden]; f.Estado != "pendiente" || f.Intentos != 1 || !strings.Contains(f.Error, "sin conector Argos activo") {
		t.Fatalf("apagado: %+v", f)
	}
	if n := e.eventos("integracion.pendiente_omitido", orden); n != 0 {
		t.Fatal("con conector registrado no se omite")
	}
	// Al activarlo, el siguiente intento pasa.
	if _, err := e.pool.Exec(context.Background(), "UPDATE integracion.conectores SET estado = 'activo' WHERE id = $1", e.conID); err != nil {
		t.Fatal(err)
	}
	e.reloj = e.reloj.Add(time.Minute)
	e.procesar()
	if f := e.pendientes(orden)["iris:instalacion:"+orden]; f.Estado != "hecho" {
		t.Fatalf("tras activar: %+v", f)
	}

	// Nunca hubo conector Argos (modo sin conector): se omite.
	if _, err := e.pool.Exec(context.Background(), "UPDATE integracion.conectores SET estado = 'apagado', sistema = 'horus' WHERE sistema = 'argosisp'"); err != nil {
		t.Fatal(err)
	}
	sin := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": sin, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": 20})
	if len(e.pendientes(sin)) != 0 {
		t.Fatal("sin conector no se encola")
	}
	if n := e.eventos("integracion.pendiente_omitido", sin); n != 1 {
		t.Fatalf("pendiente_omitido: %d", n)
	}
	// Instalación fallida sin materiales: nada que hacer, ni evento.
	otra := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": otra, "tipo": "instalacion", "resultado": "fallida", "servicio_id": 20})
	if n := e.eventos("integracion.pendiente_omitido", otra); n != 0 {
		t.Fatal("sin acciones no hay omitido")
	}
}

func TestCierreRepetidoCorrigeIncompletosYMarcaConflictos(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	// Primer cierre sin servicio_id: incompleto. Consumo con servicio y bodega del primer cierre…
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "bodega_id": 3,
		"materiales": []any{map[string]any{"producto_id": 4, "cantidad": 2}}})
	ps := e.pendientes(orden)
	if f := ps["iris:instalacion:"+orden]; f.Estado != "fallido_negocio" || !f.Incompleto {
		t.Fatalf("incompleto: %+v", f)
	}
	// Se reabre y se cierra de nuevo con el servicio: el incompleto se corrige y se ejecuta.
	segundo := map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": 20, "bodega_id": 3,
		"materiales": []any{map[string]any{"producto_id": 4, "cantidad": 2}}}
	e.cerrar(segundo)
	if f := e.pendientes(orden)["iris:instalacion:"+orden]; f.Estado != "pendiente" || f.Incompleto || f.Error != "" {
		t.Fatalf("corregido: %+v", f)
	}
	if n := e.eventos("integracion.pendiente_corregido", orden); n != 2 { // activar y consumo (este también era incompleto)
		t.Fatalf("pendiente_corregido: %d", n)
	}
	e.procesar()
	var activar llamada
	for _, l := range e.argos.copia() {
		if l.Clave == "iris:instalacion:"+orden {
			activar = l
		}
	}
	if activar.Ruta != "/servicios/20/activar-instalacion" {
		t.Fatalf("la corrección usa la ruta nueva: %+v", activar)
	}
	llamadas := len(e.argos.copia())

	// Tercer cierre con otra cantidad: el consumo ya está hecho; no se pisa, se marca conflicto.
	tercero := map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": 20, "bodega_id": 3,
		"materiales": []any{map[string]any{"producto_id": 4, "cantidad": 5}}}
	e.cerrar(tercero)
	e.cerrar(tercero) // repetido: un solo aviso
	var conflicto, cantidad string
	if err := e.pool.QueryRow(context.Background(), `SELECT estado || ':' || coalesce(conflicto, ''), conflicto_payload->'lineas'->0->>'cantidad'
		FROM integracion.pendientes WHERE idempotency_key = $1`, "iris:consumo:"+orden).Scan(&conflicto, &cantidad); err != nil {
		t.Fatal(err)
	}
	if conflicto != "hecho:cierre_repetido_con_otros_datos" || cantidad != "5" {
		t.Fatalf("conflicto: %s %s", conflicto, cantidad)
	}
	if n := e.eventos("integracion.pendiente_conflicto", orden); n != 1 {
		t.Fatalf("pendiente_conflicto: %d", n)
	}
	e.reloj = e.reloj.Add(time.Hour)
	if e.procesar() != 0 || len(e.argos.copia()) != llamadas {
		t.Fatal("un conflicto no vuelve a llamar a Argos")
	}
	// Mismo cierre que el segundo para el activar (hecho, mismo payload): sin conflicto.
	if f := e.pendientes(orden)["iris:instalacion:"+orden]; f.Estado != "hecho" {
		t.Fatalf("activar: %+v", f)
	}
	ident := identidad.Nuevo(identidad.Config{})
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Supervisor", Correo: pruebas.Correo("sup"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		t.Fatal(err)
	}
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, u.ID, 1, nucleo.PermIntegracionesVerPendientes)
	pag, err := e.m.Listar.Ejecutar(sup, pendientes.EntradaListar{Entidad: "orden", EntidadID: orden, Conflicto: true})
	if err != nil || len(pag.Data) != 1 || pag.Data[0].Conflicto == nil || pag.Data[0].PayloadConflicto == nil {
		t.Fatalf("lista de conflictos: %+v %v", pag, err)
	}
}

func TestDependenciaFallidaBloqueaYSeDesbloquea(t *testing.T) {
	e := nuevoEntorno(t)
	ident := identidad.Nuevo(identidad.Config{})
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Supervisor", Correo: pruebas.Correo("sup"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		t.Fatal(err)
	}
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, u.ID, 1, nucleo.PermIntegracionesReintentar)
	cambio := nuevaOrden()
	e.argos.responder("/servicios/40/equipos/81/devolver", 422, 201)
	e.cerrar(map[string]any{"orden_id": cambio, "tipo": "cambio_equipo", "resultado": "exitosa", "servicio_id": 40, "bodega_id": 3,
		"equipos": []any{
			map[string]any{"accion": "instalar", "serie": "NUEVO-1"},
			map[string]any{"accion": "retirar", "unidad_id": 81, "condicion": "danado"},
		}})
	e.procesar()
	ps := e.pendientes(cambio)
	dev, asig := ps["iris:devolucion:"+cambio+":81"], ps["iris:comodato:"+cambio+":NUEVO-1"]
	if dev.Estado != "fallido_negocio" || asig.Estado != "bloqueado" || asig.Error != "depende_de_"+strconv.FormatInt(dev.ID, 10)+"_fallido" {
		t.Fatalf("bloqueo: %+v %+v", dev, asig)
	}
	if n := e.eventos("integracion.pendiente_bloqueado", cambio); n != 1 {
		t.Fatalf("pendiente_bloqueado: %d", n)
	}
	_, err = e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: asig.ID, Version: asig.Version})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "pendiente_no_reintentable" {
		t.Fatalf("un bloqueado se destraba por su dependencia: %v", err)
	}
	// Se corrige en Argos y se reintenta la devolución: hecha → la asignación se desbloquea y corre.
	if _, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: dev.ID, Version: dev.Version}); err != nil {
		t.Fatal(err)
	}
	e.procesar()
	ps = e.pendientes(cambio)
	if ps["iris:devolucion:"+cambio+":81"].Estado != "hecho" || ps["iris:comodato:"+cambio+":NUEVO-1"].Estado != "hecho" {
		t.Fatalf("desbloqueo: %+v", ps)
	}
	if n := e.eventos("integracion.pendiente_desbloqueado", cambio); n != 1 {
		t.Fatalf("pendiente_desbloqueado: %d", n)
	}

	// Dependencia que nace incompleta (retiro sin unidad_id): la asignación nace bloqueada.
	otro := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": otro, "tipo": "cambio_equipo", "resultado": "exitosa", "servicio_id": 40, "bodega_id": 3,
		"equipos": []any{map[string]any{"accion": "retirar", "serie": "SIN-UNIDAD", "condicion": "bueno"}, map[string]any{"accion": "instalar", "serie": "N2"}}})
	if f := e.pendientes(otro)["iris:comodato:"+otro+":N2"]; f.Estado != "bloqueado" {
		t.Fatalf("asignación tras devolución incompleta: %+v", f)
	}
}

func TestCantidadYMontoNoPositivos(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "visita", "resultado": "exitosa", "servicio_id": 20, "cliente_id": 10, "bodega_id": 3,
		"materiales": []any{map[string]any{"producto_id": 4, "cantidad": 0}},
		"cargo":      map[string]any{"monto": "-5"}})
	ps := e.pendientes(orden)
	if f := ps["iris:consumo:"+orden]; f.Estado != "fallido_negocio" || !f.Incompleto || !strings.Contains(f.Error, "cantidad_no_positiva") {
		t.Fatalf("cantidad: %+v", f)
	}
	if f := ps["iris:cargo:"+orden]; f.Estado != "fallido_negocio" || !f.Incompleto || !strings.Contains(f.Error, "monto_no_positivo") {
		t.Fatalf("monto: %+v", f)
	}
}

func TestDatosIncompletosYReintentoManual(t *testing.T) {
	e := nuevoEntorno(t)
	ident := identidad.Nuevo(identidad.Config{})
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Supervisor", Correo: pruebas.Correo("sup"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		t.Fatal(err)
	}
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, u.ID, 1, nucleo.PermIntegracionesReintentar, nucleo.PermIntegracionesVerPendientes)
	otraEmpresa := pruebas.ContextoUsuarioEnEmpresa(e.pool, u.ID, 2, nucleo.PermIntegracionesReintentar, nucleo.PermIntegracionesVerPendientes)

	// Sin servicio_id: nace fallido_negocio con motivo claro y no se reintenta.
	orden := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa"})
	f := e.pendientes(orden)["iris:instalacion:"+orden]
	if f.Estado != "fallido_negocio" || !f.Incompleto || !strings.Contains(f.Error, "sin servicio_id") {
		t.Fatalf("incompleto: %+v", f)
	}
	if e.procesar() != 0 || len(e.argos.copia()) != 0 {
		t.Fatal("un incompleto no llama a Argos")
	}
	_, err = e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: f.ID, Version: f.Version})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "pendiente_incompleto" {
		t.Fatalf("reintentar incompleto: %v", err)
	}

	// Agotado → reintento manual (auditado, con evento) → hecho.
	orden2 := nuevaOrden()
	e.argos.responder("/servicios/20/retirar", 500, 500, 500, 500, 500, 500, 201)
	e.cerrar(map[string]any{"orden_id": orden2, "tipo": "retiro", "resultado": "exitosa", "servicio_id": 20, "retirar_servicio": true})
	for range 6 {
		e.procesar()
		e.reloj = e.reloj.Add(13 * time.Hour)
	}
	f = e.pendientes(orden2)["iris:retiro:"+orden2]
	if f.Estado != "agotado" {
		t.Fatalf("agotado: %+v", f)
	}
	_, err = e.m.Reintentar.Ejecutar(otraEmpresa, pendientes.EntradaReintentar{ID: f.ID, Version: f.Version})
	if p, _ := nucleo.AProblema(err); p == nil || p.Status != 404 {
		t.Fatalf("otra empresa: %v", err)
	}
	_, err = e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: f.ID, Version: f.Version + 1})
	if p, _ := nucleo.AProblema(err); p == nil || p.Status != 409 {
		t.Fatalf("versión: %v", err)
	}
	p, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: f.ID, Version: f.Version})
	if err != nil || p.Estado != "pendiente" || p.Intentos != 0 {
		t.Fatalf("reintentar: %+v %v", p, err)
	}
	if n := e.eventos("integracion.pendiente_reintentado", orden2); n != 1 {
		t.Fatalf("evento reintentado: %d", n)
	}
	var auditadas int
	_ = e.pool.QueryRow(context.Background(), "SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.reintentar' AND resultado = 'ok' AND entidad_id = $1",
		strconv.FormatInt(f.ID, 10)).Scan(&auditadas)
	if auditadas != 1 {
		t.Fatalf("auditoría: %d", auditadas)
	}
	e.procesar()
	if f := e.pendientes(orden2)["iris:retiro:"+orden2]; f.Estado != "hecho" {
		t.Fatalf("tras reintentar: %+v", f)
	}
	_, err = e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: f.ID, Version: f.Version + 2})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "pendiente_no_reintentable" {
		t.Fatalf("un hecho no se reintenta: %v", err)
	}

	// Lista para el panel: filtrada por empresa activa, entidad y estado.
	pag, err := e.m.Listar.Ejecutar(sup, pendientes.EntradaListar{Entidad: "orden", EntidadID: orden})
	if err != nil || len(pag.Data) != 1 || pag.Data[0].IdempotencyKey != "iris:instalacion:"+orden || !pag.Data[0].DatosIncompletos {
		t.Fatalf("lista: %+v %v", pag, err)
	}
	pag, _ = e.m.Listar.Ejecutar(sup, pendientes.EntradaListar{Estado: "hecho"})
	if len(pag.Data) == 0 {
		t.Fatal("lista por estado")
	}
	// estadoDistintoDe=hecho: lo que aún no llegó a Argos, en una lectura.
	pag, err = e.m.Listar.Ejecutar(sup, pendientes.EntradaListar{Entidad: "orden", EstadoDistintoDe: "hecho"})
	if err != nil || len(pag.Data) == 0 {
		t.Fatalf("lista sin hechos: %+v %v", pag, err)
	}
	for _, p := range pag.Data {
		if p.Estado == "hecho" {
			t.Fatalf("estadoDistintoDe=hecho trajo un hecho: %+v", p)
		}
	}
	if pag, _ = e.m.Listar.Ejecutar(sup, pendientes.EntradaListar{Estado: "hecho", EstadoDistintoDe: "hecho"}); len(pag.Data) != 0 {
		t.Fatalf("estado y estadoDistintoDe se combinan: %+v", pag.Data)
	}
	pag, _ = e.m.Listar.Ejecutar(otraEmpresa, pendientes.EntradaListar{})
	if len(pag.Data) != 0 {
		t.Fatalf("otra empresa no ve: %+v", pag.Data)
	}
	if ps, _ := pendientes.DeEntidad(context.Background(), e.pool, "orden", orden); !pendientes.ConPendienteEnArgos(ps) {
		t.Fatal("la orden incompleta queda «cerrada con pendiente en Argos»")
	}
}

func TestEnCursoAbandonadoSeReclama(t *testing.T) {
	e := nuevoEntorno(t)
	orden := nuevaOrden()
	e.cerrar(map[string]any{"orden_id": orden, "tipo": "retiro", "resultado": "exitosa", "servicio_id": 20, "retirar_servicio": true})
	// Un worker lo tomó y cayó.
	if _, err := e.pool.Exec(context.Background(), "UPDATE integracion.pendientes SET estado = 'en_curso', tomado_en = $1 WHERE entidad_id = $2",
		e.reloj, orden); err != nil {
		t.Fatal(err)
	}
	if e.procesar() != 0 {
		t.Fatal("recién tomado no se reclama")
	}
	e.reloj = e.reloj.Add(16 * time.Minute)
	if e.procesar() != 1 {
		t.Fatal("abandonado se reclama")
	}
	if f := e.pendientes(orden)["iris:retiro:"+orden]; f.Estado != "hecho" {
		t.Fatalf("%+v", f)
	}
}
