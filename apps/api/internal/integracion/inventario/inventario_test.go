package inventario_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
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

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/inventario"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const tokenArgos = "atk_prueba_inventario_0123456789"

// recibida es un POST …/recibir que llegó al Argos falso.
type recibida struct {
	Ruta, Clave, Empresa string
	Cuerpo               map[string]any
}

// argosFalso: canje, GET /inventario/mi-bodega y POST /inventario/traslados/:id/recibir (Argos 1.6.0).
type argosFalso struct {
	srv       *httptest.Server
	mu        sync.Mutex
	recibidas []recibida
	respuesta map[string][2]string // traslado → status, type del problema ("" = 201)
	bodegas   int
}

func nuevoArgosFalso(t *testing.T) *argosFalso {
	a := &argosFalso{respuesta: map[string][2]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/canjear", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + tokenArgos + `","empresas":[1],"version_argos":"1.6.0","contrato":"0.5.0"}`))
	})
	mux.HandleFunc("GET /api/v1/inventario/mi-bodega", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.bodegas++
		a.mu.Unlock()
		if r.URL.Query().Get("id_externo") != "u-77" || r.Header.Get("X-Empresa-Id") != "1" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"type":"urn:argosisp:problema:tecnico_sin_bodega","status":404,"detail":"Sin bodega"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bodega":{"id":70,"nombre":"Bodega Ana","empresa_id":1},
			"existencias":[{"producto_id":5,"nombre":"Cable UTP","cantidad":"120.5","unidad":"m","modalidad":"consumible_gasto"},
				{"producto":{"id":6,"nombre":"ONT"},"cantidad":2,"modalidad":"comodato"}],
			"unidades":[{"id":900,"producto_id":6,"serie":"ZTE-1","mac":"AA:BB:CC:DD:EE:FF","estado":"disponible"}],
			"trasladosPendientes":[{"id":31}],
			"kardex":[{"id":1,"tipo":"traslado_entrada","producto_id":5,"cantidad":"100","fecha":"2026-10-30T15:00:00Z","referencia":"T-31"}]}`))
	})
	mux.HandleFunc("POST /api/v1/inventario/traslados/{id}/recibir", func(w http.ResponseWriter, r *http.Request) {
		crudo, _ := io.ReadAll(r.Body)
		var c map[string]any
		_ = json.Unmarshal(crudo, &c)
		a.mu.Lock()
		a.recibidas = append(a.recibidas, recibida{Ruta: r.URL.Path, Clave: r.Header.Get("Idempotency-Key"), Empresa: r.Header.Get("X-Empresa-Id"), Cuerpo: c})
		resp, ok := a.respuesta[r.PathValue("id")]
		a.mu.Unlock()
		if ok {
			status, _ := strconv.Atoi(resp[0])
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:argosisp:problema:" + resp[1], "status": status, "detail": resp[1]})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id": 1, "estado": "recibido"}`))
	})
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

type entorno struct {
	t      *testing.T
	pool   *pgxpool.Pool
	argos  *argosFalso
	inv    *inventario.Modulo
	cmp    *campo.Modulo
	pend   *pendientes.Modulo
	disco  *adjuntos.Disco
	adj    *adjuntos.Modulo
	reloj  time.Time
	tec    int64
	ctxTec context.Context
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	for _, sql := range []string{
		"UPDATE integracion.conectores SET estado = 'apagado'",
		"DELETE FROM integracion.pendientes WHERE operacion = 'recepcion'",
		"INSERT INTO nucleo.empresas (id, nombre) VALUES (1, 'Empresa 1') ON CONFLICT (id) DO NOTHING",
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
		Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true, Timeout: 2 * time.Second, Origenes: []string{fa.srv.URL},
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	})
	am.Nucleo().FijarTokens(pruebas.TokensFalsos{})
	sis := pruebas.ContextoSistema(pool)
	c, err := am.Conectar.Ejecutar(sis, argos.EntradaConectar{Enlace: fa.srv.URL + "/conectar#codigo-de-prueba-123"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := am.ActivarConector.Ejecutar(sis, argos.EntradaCambioConector{ID: c.ID, Version: c.Version}); err != nil {
		t.Fatal(err)
	}
	disco, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &entorno{t: t, pool: pool, argos: fa, disco: disco, reloj: time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC)}
	reloj := func() time.Time { return e.reloj }
	inventario.RegistrarAdjuntos()
	e.inv = inventario.Nuevo(inventario.Config{Argos: am, Almacen: disco, Reloj: reloj})
	e.cmp = campo.Nuevo(campo.Config{Recepciones: e.inv, Reloj: reloj, Adjuntos: campo.SinAdjuntosObligatorios{}})
	e.pend = pendientes.Nuevo(pendientes.Config{Argos: am, Reloj: reloj})
	e.adj = adjuntos.Nuevo(adjuntos.Config{Almacen: disco})
	e.tec = e.usuario()
	e.vincular(e.tec, "u-77")
	e.ctxTec = e.como(e.tec)
	return e
}

func (e *entorno) usuario() int64 {
	e.t.Helper()
	u, err := identidad.Nuevo(identidad.Config{}).CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Ana Técnica", Correo: pruebas.Correo("tec"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		e.t.Fatal(err)
	}
	return u.ID
}

func (e *entorno) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entorno) vincular(usuario int64, externo string) {
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = $1`, externo)
	e.exec(`INSERT INTO agenda.tecnicos (usuario_id, argos_id_externo, argos_correo, argos_vinculado_en) VALUES ($1, $2, 'ana@argos.gt', now())`, usuario, externo)
}

func (e *entorno) como(usuario int64) context.Context {
	return contexto.ConCanal(pruebas.ContextoUsuarioEnEmpresa(e.pool, usuario, 1, nucleo.PermCampoSincronizar, nucleo.PermCampoRecibirTraslado,
		nucleo.PermCampoVerBodega, nucleo.PermAdjuntosSubir, nucleo.PermAdjuntosVer), contexto.CanalCampo)
}

func pngDe(t *testing.T, ancho, alto int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, ancho, alto))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// firma registra la firma del traslado (POST /adjuntos, entidad traslado) y, si completa, la sube.
func (e *entorno) firma(traslado int64, contenido []byte, completa bool) uuid.UUID {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7())
	suma := sha256.Sum256(contenido)
	if _, err := e.adj.Registrar.Ejecutar(e.ctxTec, adjuntos.EntradaRegistrar{ID: id, Entidad: "traslado", EntidadID: strconv.FormatInt(traslado, 10),
		Tipo: "firma", Mime: "image/png", Tamano: int64(len(contenido)), SHA256: hex.EncodeToString(suma[:])}); err != nil {
		e.t.Fatalf("registrar firma: %v", err)
	}
	if completa {
		e.completar(id, contenido)
	}
	return id
}

func (e *entorno) completar(id uuid.UUID, contenido []byte) {
	e.t.Helper()
	var clave string
	if err := e.pool.QueryRow(context.Background(), `SELECT clave FROM nucleo.adjuntos WHERE id = $1`, id).Scan(&clave); err != nil {
		e.t.Fatal(err)
	}
	suma := sha256.Sum256(contenido)
	if err := e.disco.Guardar(context.Background(), clave, bytes.NewReader(contenido), int64(len(contenido)), "image/png", hex.EncodeToString(suma[:])); err != nil {
		e.t.Fatal(err)
	}
	e.exec(`UPDATE nucleo.adjuntos SET estado = 'completo', recibido = tamano, completado_en = now() WHERE id = $1`, id)
}

func (e *entorno) enviado(traslado int64, externo string) {
	e.exec(`DELETE FROM integracion.argos_traslados WHERE id = $1`, traslado)
	e.exec(`INSERT INTO integracion.argos_traslados (id, empresa_id, bodega_destino_id, tecnico_id_externo, estado, lineas, enviado_en)
		VALUES ($1, 1, 70, $2, 'enviado', '[{"linea_id": 1, "producto_id": 5, "cantidad": "10", "series": []}]', '2026-10-30T15:00:00Z')`, traslado, externo)
}

func (e *entorno) recibir(traslado int64, firma uuid.UUID, id ...uuid.UUID) campo.ResultadoOperacion {
	e.t.Helper()
	op := uuid.Must(uuid.NewV7())
	if len(id) > 0 {
		op = id[0]
	}
	r, err := e.cmp.Lote(e.ctxTec, campo.EntradaLote{DispositivoID: "tel-" + strconv.FormatInt(e.tec, 10), Operaciones: []campo.OperacionLote{{
		ID: op.String(), Tipo: "traslado.recibir", OcurridoEn: &e.reloj,
		Datos: map[string]any{"trasladoId": traslado, "firmaAdjuntoId": firma.String(), "recibidoEn": e.reloj.Add(-time.Minute).Format(time.RFC3339),
			"lineas": []any{map[string]any{"lineaId": 1, "cantidadRecibida": 9, "seriesFaltantes": []any{}}}},
	}}})
	if err != nil {
		e.t.Fatal(err)
	}
	return r.Resultados[0]
}

func (e *entorno) pendiente(traslado int64) pendientes.Registro {
	e.t.Helper()
	p, err := pendientes.PorClave(context.Background(), e.pool, inventario.ClaveRecepcion(traslado))
	if err != nil {
		e.t.Fatalf("pendiente de %d: %v", traslado, err)
	}
	return p
}

func trasladoNuevo() int64 { return 700000 + time.Now().UnixNano()%100000 }

// traslado.recibir: idempotente por operación; espera la firma; arma el cuerpo PNG + sha256 con
// X-Empresa-Id e Idempotency-Key iris:recepcion:<id>; 409 traslado_ya_recibido cuenta como hecho;
// los rechazos de negocio llegan a cambios.
func TestRecepcionDeTraslado(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	tr := trasladoNuevo()
	e.enviado(tr, "u-77")
	contenido := pngDe(t, 300, 100)
	firma := e.firma(tr, contenido, false)

	op := uuid.Must(uuid.NewV7())
	r := e.recibir(tr, firma, op)
	if r.Estado != "aplicada" || r.Recepcion == nil || r.Recepcion.Estado != "esperando_firma" {
		t.Fatalf("recibir: %+v %+v", r, r.Error)
	}
	if d := e.recibir(tr, firma, op); d.Estado != "duplicada" || d.Recepcion == nil {
		t.Fatalf("misma operación: %+v", d)
	}
	// Otra operación del mismo técnico para el mismo traslado: la misma recepción.
	if otra := e.recibir(tr, firma); otra.Estado != "aplicada" || otra.Recepcion.PendienteID != r.Recepcion.PendienteID {
		t.Fatalf("otra operación del mismo traslado: %+v", otra)
	}
	// La firma no terminó de subir: sigue esperando y la cola no la toma.
	if res, err := e.inv.ArmarFirma(ctx, r.Recepcion.PendienteID); err != nil || res.Resultado != "esperando" {
		t.Fatalf("esperando: %+v %v", res, err)
	}
	if n, _ := e.pend.Procesar(ctx); n != 0 {
		t.Fatal("no sale hacia Argos sin la firma")
	}
	e.completar(firma, contenido)
	if res, err := e.inv.ArmarFirma(ctx, r.Recepcion.PendienteID); err != nil || res.Resultado != "lista" {
		t.Fatalf("lista: %+v %v", res, err)
	}
	p := e.pendiente(tr)
	var cuerpo map[string]any
	_ = json.Unmarshal(p.Payload, &cuerpo)
	suma := sha256.Sum256(contenido)
	fi := cuerpo["firma"].(map[string]any)
	if p.Estado != "pendiente" || fi["sha256"] != hex.EncodeToString(suma[:]) || fi["png_base64"] != base64.StdEncoding.EncodeToString(contenido) ||
		cuerpo["firma_adjunto_id"] != nil || cuerpo["metodo_recepcion"] != "firma_digital" || cuerpo["recibido_por"].(map[string]any)["id_externo"] != "u-77" {
		t.Fatalf("cuerpo: %v", cuerpo)
	}
	if _, err := e.pend.Procesar(ctx); err != nil {
		t.Fatal(err)
	}
	e.argos.mu.Lock()
	ultima := e.argos.recibidas[len(e.argos.recibidas)-1]
	e.argos.mu.Unlock()
	if ultima.Clave != inventario.ClaveRecepcion(tr) || ultima.Empresa != "1" || ultima.Ruta != "/api/v1/inventario/traslados/"+strconv.FormatInt(tr, 10)+"/recibir" ||
		len(ultima.Cuerpo["lineas"].([]any)) != 1 {
		t.Fatalf("llamada a Argos: %+v", ultima)
	}
	if p := e.pendiente(tr); p.Estado != "hecho" {
		t.Fatalf("hecho: %+v", p)
	}

	// 409 traslado_ya_recibido (lo recibió el panel de Argos): hecho, conciliado.
	tr2 := tr + 1
	e.enviado(tr2, "u-77")
	e.argos.mu.Lock()
	e.argos.respuesta[strconv.FormatInt(tr2, 10)] = [2]string{"409", "traslado_ya_recibido"}
	e.argos.mu.Unlock()
	e.recibir(tr2, e.firma(tr2, contenido, true))
	if _, err := e.inv.BarrerFirmas(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pend.Procesar(ctx); err != nil {
		t.Fatal(err)
	}
	if p := e.pendiente(tr2); p.Estado != "hecho" || p.CodigoError == nil || *p.CodigoError != "traslado_ya_recibido" {
		t.Fatalf("ya recibido: %+v", p)
	}

	// 409 traslado_cancelado en Argos: rechazada, llega al teléfono por cambios.
	tr3 := tr + 2
	e.argos.mu.Lock()
	e.argos.respuesta[strconv.FormatInt(tr3, 10)] = [2]string{"409", "traslado_cancelado"}
	e.argos.mu.Unlock()
	e.enviado(tr3, "u-77")
	e.recibir(tr3, e.firma(tr3, contenido, true)) // Iris no lo sabe cancelado: Argos decide
	_, _ = e.inv.BarrerFirmas(ctx)
	_, _ = e.pend.Procesar(ctx)
	// Firma de 10×10 px: firma_invalida sin llamar a Argos.
	tr4 := tr + 3
	e.enviado(tr4, "u-77")
	e.recibir(tr4, e.firma(tr4, pngDe(t, 10, 10), true))
	antes := len(e.argos.recibidas)
	_, _ = e.inv.BarrerFirmas(ctx)
	_, _ = e.pend.Procesar(ctx)
	if len(e.argos.recibidas) != antes {
		t.Fatal("una firma inválida no sale hacia Argos")
	}
	// Otra operación con una firma buena rehace la recepción rechazada.
	if rehecha := e.recibir(tr4, e.firma(tr4, contenido, true)); rehecha.Recepcion == nil || rehecha.Recepcion.Estado != "esperando_firma" {
		t.Fatalf("rehacer: %+v", rehecha)
	}
	_, _ = e.inv.BarrerFirmas(ctx)
	if p := e.pendiente(tr4); p.Estado != "pendiente" {
		t.Fatalf("rehecha y lista: %+v", p)
	}
	e.exec(`UPDATE integracion.pendientes SET estado = 'fallido_negocio', codigo_error = 'firma_invalida' WHERE idempotency_key = $1`, inventario.ClaveRecepcion(tr4))
	c, err := e.cmp.Cambios(e.ctxTec, campo.EntradaCambios{})
	if err != nil {
		t.Fatal(err)
	}
	estados := map[int64]string{}
	for _, rc := range c.Recepciones {
		estados[rc.TrasladoID] = rc.Estado
		if rc.Estado == "rechazada" {
			estados[rc.TrasladoID] += ":" + *rc.Codigo
		}
	}
	if estados[tr] != "recibida" || estados[tr2] != "recibida" || estados[tr3] != "rechazada:traslado_cancelado" || estados[tr4] != "rechazada:firma_invalida" {
		t.Fatalf("recepciones en cambios: %v", estados)
	}
}

// Reglas de conflicto (contratos/sync.md §3): traslado cancelado o de otro técnico según Iris, sin
// vínculo, y un rechazo bloquea las siguientes del mismo traslado pero no otra.
func TestRecepcionRechazos(t *testing.T) {
	e := nuevoEntorno(t)
	contenido := pngDe(t, 300, 100)
	tr := trasladoNuevo()
	e.enviado(tr, "u-77")
	e.exec(`UPDATE integracion.argos_traslados SET estado = 'cancelado' WHERE id = $1`, tr)
	firma := e.firma(tr, contenido, true)
	r, err := e.cmp.Lote(e.ctxTec, campo.EntradaLote{DispositivoID: "tel-r-" + strconv.FormatInt(e.tec, 10), Operaciones: []campo.OperacionLote{
		{ID: uuid.Must(uuid.NewV7()).String(), Tipo: "traslado.recibir", Datos: map[string]any{"trasladoId": tr, "firmaAdjuntoId": firma.String(), "recibidoEn": e.reloj.Format(time.RFC3339)}},
		{ID: uuid.Must(uuid.NewV7()).String(), Tipo: "traslado.recibir", Datos: map[string]any{"trasladoId": tr, "firmaAdjuntoId": firma.String(), "recibidoEn": e.reloj.Format(time.RFC3339)}},
		{ID: uuid.Must(uuid.NewV7()).String(), Tipo: "orden.llegar"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Resultados[0].Error == nil || r.Resultados[0].Error.Codigo() != "traslado_cancelado" ||
		r.Resultados[1].Error == nil || r.Resultados[1].Error.Codigo() != "bloqueada_por_rechazo" {
		t.Fatalf("cancelado y bloqueada: %+v / %+v", r.Resultados[0].Error, r.Resultados[1].Error)
	}
	if r.Resultados[2].Error == nil || r.Resultados[2].Error.Codigo() != "validacion" || !strings.Contains(r.Resultados[2].Error.Errors[0].Field, "ordenId") {
		t.Fatalf("una operación de orden sin ordenId: %+v", r.Resultados[2].Error)
	}
	// De otro técnico.
	tr2 := tr + 1
	e.enviado(tr2, "u-otro")
	suma := sha256.Sum256(contenido)
	_, err = e.adj.Registrar.Ejecutar(e.ctxTec, adjuntos.EntradaRegistrar{ID: uuid.Must(uuid.NewV7()), Entidad: "traslado", EntidadID: strconv.FormatInt(tr2, 10),
		Tipo: "firma", Mime: "image/png", Tamano: int64(len(contenido)), SHA256: hex.EncodeToString(suma[:])})
	if p, _ := nucleo.AProblema(err); p == nil || p.Status != 404 {
		t.Fatalf("la firma de un traslado ajeno no se registra: %v", err)
	}
	if res := e.recibir(tr2, uuid.Must(uuid.NewV7())); res.Error == nil || res.Error.Codigo() != "traslado_de_otro_tecnico" {
		t.Fatalf("traslado de otro técnico: %+v", res.Error)
	}
	// Sin vínculo.
	otro := e.usuario()
	e.exec(`INSERT INTO agenda.tecnicos (usuario_id) VALUES ($1)`, otro)
	sinVinculo := e.como(otro)
	tr3 := tr + 2
	res, err := e.cmp.Lote(sinVinculo, campo.EntradaLote{DispositivoID: "tel-sv-" + strconv.FormatInt(otro, 10), Operaciones: []campo.OperacionLote{
		{ID: uuid.Must(uuid.NewV7()).String(), Tipo: "traslado.recibir", Datos: map[string]any{"trasladoId": tr3, "firmaAdjuntoId": uuid.Must(uuid.NewV7()).String(), "recibidoEn": e.reloj.Format(time.RFC3339)}},
	}})
	if err != nil || res.Resultados[0].Error == nil || res.Resultados[0].Error.Codigo() != "tecnico_sin_vincular" {
		t.Fatalf("sin vínculo: %+v %v", res.Resultados, err)
	}
}

// Mi bodega (con caché) y la lista de traslados con el estado de la recepción.
func TestMiBodegaYTraslados(t *testing.T) {
	e := nuevoEntorno(t)
	b, err := e.inv.MiBodega.Ejecutar(e.ctxTec)
	if err != nil {
		t.Fatal(err)
	}
	if b.Bodega.ID != 70 || len(b.Existencias) != 2 || b.Existencias[0].Cantidad != 120.5 || b.Existencias[1].Modalidad != "comodato" ||
		b.Existencias[1].ProductoID != 6 || len(b.Unidades) != 1 || len(b.TrasladosPendientes) != 1 || len(b.Kardex) != 1 {
		t.Fatalf("mi bodega: %+v", b)
	}
	if _, err := e.inv.MiBodega.Ejecutar(e.ctxTec); err != nil || e.argos.bodegas != 1 {
		t.Fatalf("caché: %d lecturas %v", e.argos.bodegas, err)
	}
	// Sin vínculo: 422.
	otro := e.usuario()
	_, err = e.inv.MiBodega.Ejecutar(e.como(otro))
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "tecnico_sin_vincular" {
		t.Fatalf("sin vínculo: %v", err)
	}
	tr := trasladoNuevo()
	e.enviado(tr, "u-77")
	e.recibir(tr, e.firma(tr, pngDe(t, 300, 100), false))
	l, err := e.inv.Traslados.Ejecutar(e.ctxTec, inventario.EntradaTraslados{Estado: "enviado"})
	if err != nil {
		t.Fatal(err)
	}
	var encontrado *inventario.Traslado
	for i := range l.Data {
		if l.Data[i].ID == tr {
			encontrado = &l.Data[i]
		}
	}
	if encontrado == nil || len(encontrado.Lineas) != 1 || encontrado.Lineas[0].LineaID != 1 || encontrado.Recepcion == nil || encontrado.Recepcion.Estado != "esperando_firma" {
		t.Fatalf("traslados: %+v", l.Data)
	}
}

// B5 de la revisión: Argos rechaza al técnico (tecnico_no_encontrado); el reintento a mano tras
// vincularlo de nuevo corrige recibido_por, y la firma (png_base64) nunca queda en la auditoría.
func TestCorreccionDeRecepcionNoAuditaLaFirma(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	contenido := pngDe(t, 300, 100)
	tr := trasladoNuevo()
	e.enviado(tr, "u-77")
	e.argos.mu.Lock()
	e.argos.respuesta[strconv.FormatInt(tr, 10)] = [2]string{"422", "tecnico_no_encontrado"}
	e.argos.mu.Unlock()
	e.recibir(tr, e.firma(tr, contenido, true))
	_, _ = e.inv.BarrerFirmas(ctx)
	_, _ = e.pend.Procesar(ctx)
	p := e.pendiente(tr)
	if p.Estado != "fallido_negocio" || p.CodigoError == nil || *p.CodigoError != "tecnico_no_encontrado" {
		t.Fatalf("rechazado por el técnico: %+v", p)
	}
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = 'u-78'`)
	e.exec(`UPDATE agenda.tecnicos SET argos_id_externo = 'u-78' WHERE usuario_id = $1`, e.tec)
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, e.tec, 1, nucleo.PermIntegracionesReintentar)
	f, err := pendientes.PorID(context.Background(), e.pool, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var version int32
	if err := e.pool.QueryRow(context.Background(), `SELECT version FROM integracion.pendientes WHERE id = $1`, f.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	r, err := e.pend.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: p.ID, Version: version})
	if err != nil || r.Estado != "pendiente" || r.Payload["recibido_por"].(map[string]any)["id_externo"] != "u-78" || r.Payload["firma"] == nil {
		t.Fatalf("corregido: %+v %v", r, err)
	}
	firma := base64.StdEncoding.EncodeToString(contenido)
	// La API de pendientes nunca muestra la imagen (LOW 1 de la segunda revisión): ni en el
	// reintento, ni en la lista de la entidad, ni en payloadConflicto. El sha256 sí.
	if f, _ := r.Payload["firma"].(map[string]any); f["png_base64"] != "***" || f["sha256"] == "***" || f["sha256"] == nil {
		t.Fatalf("firma oculta en el reintento: %+v", r.Payload["firma"])
	}
	e.exec(`UPDATE integracion.pendientes SET conflicto = 'cierre_repetido_con_otros_datos', conflicto_en = now(),
		conflicto_payload = payload WHERE id = $1`, p.ID)
	lista, err := pendientes.DeEntidad(context.Background(), e.pool, "traslado", strconv.FormatInt(tr, 10))
	if err != nil || len(lista) != 1 {
		t.Fatalf("pendientes del traslado: %+v %v", lista, err)
	}
	crudo, _ := json.Marshal(lista)
	if strings.Contains(string(crudo), firma[:60]) || strings.Count(string(crudo), `"png_base64":"***"`) != 2 {
		t.Fatalf("la firma sale en la API de pendientes: %s", crudo)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM auditoria.cambios WHERE coalesce(antes::text, '') || coalesce(despues::text, '') LIKE '%' || $1 || '%') +
		(SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%' OR coalesce(error::text, '') LIKE '%' || $1 || '%')`,
		firma[:60]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("la firma quedó en la auditoría (%d filas)", n)
	}
}

// Revisión de seguridad de la PR #83 (HIGH): nadie bloquea la recepción de otro tomando la clave
// iris:recepcion:<id>. Un traslado que Iris no conoce (o sin técnico) no se encola ni admite firmas
// (traslado_desconocido, reintentable); el destinatario real toma la recepción que registró otro
// que ya no lo es (o que Argos rechazó); nadie ve la firma de un traslado ajeno.
func TestNadieBloqueaLaRecepcionDeOtro(t *testing.T) {
	e := nuevoEntorno(t)
	contenido := pngDe(t, 300, 100)
	suma := sha256.Sum256(contenido)
	intruso := e.usuario()
	e.vincular(intruso, "u-88")
	ctxIntruso := e.como(intruso)
	reciboDe := func(ctx context.Context, usuario, traslado int64, firma uuid.UUID) campo.ResultadoOperacion {
		t.Helper()
		r, err := e.cmp.Lote(ctx, campo.EntradaLote{DispositivoID: "tel-" + strconv.FormatInt(usuario, 10), Operaciones: []campo.OperacionLote{{
			ID: uuid.Must(uuid.NewV7()).String(), Tipo: "traslado.recibir", OcurridoEn: &e.reloj,
			Datos: map[string]any{"trasladoId": traslado, "firmaAdjuntoId": firma.String(), "recibidoEn": e.reloj.Add(-time.Minute).Format(time.RFC3339)},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return r.Resultados[0]
	}
	registrarFirma := func(ctx context.Context, traslado int64) (uuid.UUID, error) {
		id := uuid.Must(uuid.NewV7())
		_, err := e.adj.Registrar.Ejecutar(ctx, adjuntos.EntradaRegistrar{ID: id, Entidad: "traslado", EntidadID: strconv.FormatInt(traslado, 10),
			Tipo: "firma", Mime: "image/png", Tamano: int64(len(contenido)), SHA256: hex.EncodeToString(suma[:])})
		return id, err
	}
	es404 := func(err error) bool {
		p, _ := nucleo.AProblema(err)
		return p != nil && p.Status == 404
	}

	// 1. Un id futuro que Iris no conoce: ni firma ni recepción; nada queda encolado.
	futuro := trasladoNuevo() + 50000
	if _, err := registrarFirma(ctxIntruso, futuro); !es404(err) {
		t.Fatalf("la firma de un traslado desconocido no se registra: %v", err)
	}
	r := reciboDe(ctxIntruso, intruso, futuro, uuid.Must(uuid.NewV7()))
	if r.Error == nil || r.Error.Codigo() != inventario.CodigoTrasladoDesconocido {
		t.Fatalf("traslado desconocido: %+v", r)
	}
	if _, err := pendientes.PorClave(context.Background(), e.pool, inventario.ClaveRecepcion(futuro)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("la clave no se toma: %v", err)
	}
	// Conocido pero sin técnico: igual.
	e.enviado(futuro, "u-77")
	e.exec(`UPDATE integracion.argos_traslados SET tecnico_id_externo = NULL WHERE id = $1`, futuro)
	if _, err := registrarFirma(ctxIntruso, futuro); !es404(err) {
		t.Fatalf("sin técnico no admite firmas: %v", err)
	}
	if r := reciboDe(ctxIntruso, intruso, futuro, uuid.Must(uuid.NewV7())); r.Error == nil || r.Error.Codigo() != inventario.CodigoTrasladoDesconocido {
		t.Fatalf("sin técnico: %+v", r)
	}
	// Llega el webhook: es de B (u-77). El intruso no puede; B sí.
	e.enviado(futuro, "u-77")
	if r := reciboDe(ctxIntruso, intruso, futuro, uuid.Must(uuid.NewV7())); r.Error == nil || r.Error.Codigo() != "traslado_de_otro_tecnico" {
		t.Fatalf("de otro técnico: %+v", r)
	}
	if r := e.recibir(futuro, e.firma(futuro, contenido, true)); r.Estado != "aplicada" || r.Recepcion == nil {
		t.Fatalf("el destinatario recibe: %+v %+v", r, r.Error)
	}
	// La firma de B no la ve el intruso.
	if _, err := e.adj.Listar.Ejecutar(ctxIntruso, adjuntos.EntradaListar{Entidad: "traslado", EntidadID: strconv.FormatInt(futuro, 10)}); !es404(err) {
		t.Fatalf("nadie ve la firma de un traslado ajeno: %v", err)
	}

	// 2. El traslado era del intruso y Argos lo reasignó a B: B toma la recepción que el intruso dejó
	// esperando la firma (auditado), con su cuerpo y como dueño.
	tr := trasladoNuevo()
	e.enviado(tr, "u-88")
	fi, err := registrarFirma(ctxIntruso, tr)
	if err != nil {
		t.Fatal(err)
	}
	if r := reciboDe(ctxIntruso, intruso, tr, fi); r.Estado != "aplicada" {
		t.Fatalf("el intruso recibe lo suyo: %+v %+v", r, r.Error)
	}
	if p := e.pendiente(tr); p.TecnicoUsuarioID == nil || *p.TecnicoUsuarioID != intruso {
		t.Fatalf("dueño: %+v", p)
	}
	// Mientras sea del intruso, B no puede.
	if _, err := registrarFirma(e.ctxTec, tr); !es404(err) {
		t.Fatalf("B no registra firmas de un traslado ajeno: %v", err)
	}
	e.enviado(tr, "u-77")
	tomada := e.recibir(tr, e.firma(tr, contenido, true))
	if tomada.Estado != "aplicada" || tomada.Recepcion == nil || tomada.Recepcion.Estado != "esperando_firma" {
		t.Fatalf("B toma la recepción: %+v %+v", tomada, tomada.Error)
	}
	p := e.pendiente(tr)
	var cuerpo map[string]any
	_ = json.Unmarshal(p.Payload, &cuerpo)
	if p.TecnicoUsuarioID == nil || *p.TecnicoUsuarioID != e.tec || cuerpo["recibido_por"].(map[string]any)["id_externo"] != "u-77" {
		t.Fatalf("B es el dueño y va su vínculo: %+v %v", p, cuerpo)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM auditoria.cambios WHERE entidad = 'pendiente' AND entidad_id = $1
		AND campo = 'tecnico_usuario_id' AND despues #>> '{}' = $2`, strconv.FormatInt(p.ID, 10), strconv.FormatInt(e.tec, 10)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("el cambio de dueño se audita: %d %v", n, err)
	}
	// La firma del intruso ya no la ve él.
	if _, err := e.adj.Listar.Ejecutar(ctxIntruso, adjuntos.EntradaListar{Entidad: "traslado", EntidadID: strconv.FormatInt(tr, 10)}); !es404(err) {
		t.Fatalf("el intruso ya no ve las firmas: %v", err)
	}

	// 3. Argos rechazó la del intruso (fallido_negocio) y el traslado pasó a B: B la rehace.
	tr2 := tr + 1
	e.enviado(tr2, "u-88")
	fi2, err := registrarFirma(ctxIntruso, tr2)
	if err != nil {
		t.Fatal(err)
	}
	reciboDe(ctxIntruso, intruso, tr2, fi2)
	e.exec(`UPDATE integracion.pendientes SET estado = 'fallido_negocio', ultimo_error = 'traslado_de_otro_tecnico', codigo_error = 'traslado_de_otro_tecnico'
		WHERE idempotency_key = $1`, inventario.ClaveRecepcion(tr2))
	e.enviado(tr2, "u-77")
	if r := e.recibir(tr2, e.firma(tr2, contenido, true)); r.Estado != "aplicada" || r.Recepcion.Estado != "esperando_firma" {
		t.Fatalf("B rehace tras el rechazo: %+v %+v", r, r.Error)
	}
	// Ya hecha por otro: 409 recepcion_existente (no se pisa).
	tr3 := tr + 2
	e.enviado(tr3, "u-88")
	fi3, err := registrarFirma(ctxIntruso, tr3)
	if err != nil {
		t.Fatal(err)
	}
	reciboDe(ctxIntruso, intruso, tr3, fi3)
	e.exec(`UPDATE integracion.pendientes SET estado = 'hecho', ultimo_error = NULL, hecho_en = now() WHERE idempotency_key = $1`, inventario.ClaveRecepcion(tr3))
	e.enviado(tr3, "u-77")
	if r := e.recibir(tr3, e.firma(tr3, contenido, true)); r.Error == nil || r.Error.Codigo() != "recepcion_existente" {
		t.Fatalf("hecha por otro: %+v", r)
	}
}

// LOW de la revisión: un armado viejo (leyó el pendiente antes de que se rehiciera) no pisa la
// recepción; un PNG dañado se rechaza local; series de más de 100 caracteres y recibidoEn anterior
// al envío se rechazan.
func TestArmadoViejoPNGDanadoYValidaciones(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	contenido := pngDe(t, 300, 100)
	tr := trasladoNuevo()
	e.enviado(tr, "u-77")
	r := e.recibir(tr, e.firma(tr, contenido, true))
	if r.Recepcion == nil {
		t.Fatalf("recibir: %+v", r.Error)
	}
	// Otro proceso cambia el pendiente (sube la versión) sin cambiar el estado: el armado que ya leyó
	// la versión anterior no guarda.
	viejo := e.pendiente(tr)
	e.exec(`UPDATE integracion.pendientes SET version = version + 1 WHERE id = $1`, viejo.ID)
	if ok, err := e.inv.GuardarArmadoConVersion(ctx, viejo.ID, viejo.Version); err != nil || ok {
		t.Fatalf("armado viejo no pisa: %v %v", ok, err)
	}
	if res, err := e.inv.ArmarFirma(ctx, viejo.ID); err != nil || res.Resultado != "lista" {
		t.Fatalf("armado con la versión actual: %+v %v", res, err)
	}

	// PNG dañado: cabecera válida, datos rotos.
	danado := append([]byte(nil), contenido[:len(contenido)/2]...)
	tr2 := tr + 1
	e.enviado(tr2, "u-77")
	e.recibir(tr2, e.firma(tr2, danado, true))
	if res, err := e.inv.ArmarFirma(ctx, e.pendiente(tr2).ID); err != nil || res.Resultado != "invalida" {
		t.Fatalf("PNG dañado: %+v %v", res, err)
	}
	if p := e.pendiente(tr2); p.CodigoError == nil || *p.CodigoError != "firma_invalida" || !strings.Contains(*p.UltimoError, "dañado") {
		t.Fatalf("firma_invalida: %+v", p)
	}

	tr3 := tr + 2
	e.enviado(tr3, "u-77")
	firma := e.firma(tr3, contenido, true)
	lote := func(datos map[string]any) *nucleo.Problema {
		t.Helper()
		res, err := e.cmp.Lote(e.ctxTec, campo.EntradaLote{DispositivoID: "tel-v", Operaciones: []campo.OperacionLote{{
			ID: uuid.Must(uuid.NewV7()).String(), Tipo: "traslado.recibir", OcurridoEn: &e.reloj, Datos: datos}}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Resultados[0].Error
	}
	larga := strings.Repeat("S", 101)
	if p := lote(map[string]any{"trasladoId": tr3, "firmaAdjuntoId": firma.String(), "recibidoEn": e.reloj.Format(time.RFC3339),
		"lineas": []any{map[string]any{"lineaId": 1, "cantidadRecibida": 9, "seriesFaltantes": []any{larga}}}}); p == nil || p.Codigo() != "validacion" {
		t.Fatalf("serie de 101: %+v", p)
	}
	// Enviado a las 15:00: recibido a las 14:50 no (más de 5 min antes); a las 14:56 sí.
	if p := lote(map[string]any{"trasladoId": tr3, "firmaAdjuntoId": firma.String(), "recibidoEn": "2026-10-30T14:50:00Z"}); p == nil || p.Codigo() != "validacion" ||
		p.Errors[0].Field != "recibidoEn" {
		t.Fatalf("recibido antes del envío: %+v", p)
	}
	if p := lote(map[string]any{"trasladoId": tr3, "firmaAdjuntoId": firma.String(), "recibidoEn": "2026-10-30T14:56:00Z"}); p != nil {
		t.Fatalf("dentro de la tolerancia: %+v", p)
	}
}
