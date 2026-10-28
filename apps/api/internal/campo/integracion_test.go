package campo_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Integración con los módulos de I1: adjuntos (nucleo/adjuntos), inventario y pendientes
// hacia Argos (integracion/argos, integracion/pendientes), contra un Argos falso.

// escritura que recibió el Argos falso.
type escritura struct {
	Ruta, Clave string
	Cuerpo      map[string]any
}

// argosCampo: Argos falso con el canje, las lecturas de inventario y las escrituras de §3.
type argosCampo struct {
	srv        *httptest.Server
	mu         sync.Mutex
	escrituras []escritura
}

func nuevoArgosCampo(t *testing.T) *argosCampo {
	a := &argosCampo{}
	json200 := func(w http.ResponseWriter, v string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/canjear", func(w http.ResponseWriter, _ *http.Request) {
		json200(w, `{"token":"atk_prueba_campo","empresas":[1]}`)
	})
	mux.HandleFunc("GET /api/v1/servicios/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "9901":
			json200(w, `{"id":9901,"empresa_id":1,"cliente_id":990,"estado":"activo"}`)
		case "9902":
			json200(w, `{"id":9902,"empresa_id":2,"cliente_id":991,"estado":"activo"}`)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("GET /api/v1/servicios/{id}/equipos", func(w http.ResponseWriter, _ *http.Request) {
		json200(w, `{"data":[{"unidad_id":5,"serie":"ZTE1","mac":"AA:BB:CC:00:00:01","modelo":"F660"}]}`)
	})
	mux.HandleFunc("GET /api/v1/bodegas/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "9":
			json200(w, `{"id":9,"empresa_id":1}`)
		case "8":
			json200(w, `{"id":8,"empresa_id":2}`)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("GET /api/v1/inventario/existencias", func(w http.ResponseWriter, _ *http.Request) {
		json200(w, `{"data":[{"producto_id":77,"descripcion":"Cable drop","cantidad":"5","unidad":"m","bodega_id":9}]}`)
	})
	mux.HandleFunc("POST /api/v1/", func(w http.ResponseWriter, r *http.Request) {
		var c map[string]any
		_ = json.NewDecoder(r.Body).Decode(&c)
		a.mu.Lock()
		a.escrituras = append(a.escrituras, escritura{Ruta: strings.TrimPrefix(r.URL.Path, "/api/v1"), Clave: r.Header.Get("Idempotency-Key"), Cuerpo: c})
		a.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *argosCampo) claves() map[string]escritura {
	a.mu.Lock()
	defer a.mu.Unlock()
	salida := map[string]escritura{}
	for _, e := range a.escrituras {
		salida[e.Clave] = e
	}
	return salida
}

type entornoI1 struct {
	t     *testing.T
	am    *argos.Modulo
	falso *argosCampo
	m     *campo.Modulo
	pend  *pendientes.Modulo
	bus   *integracion.Bus
	base  string
	conID int64
	ver   int32
}

func sistema(t *testing.T) context.Context {
	return nucleo.ConBD(contexto.Sistema(context.Background(), contexto.CanalWorker), pruebas.Pool(t))
}

// nuevoEntornoI1: conector Argos (aún sin conectar), pendientes suscrito y la API con adjuntos y
// campo reales. Al terminar borra conectores, pendientes y empresas (las demás pruebas del
// paquete corren sin conector).
func nuevoEntornoI1(t *testing.T) *entornoI1 {
	t.Helper()
	campo.RegistrarAdjuntos()
	pool := pruebas.Pool(t)
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	e := &entornoI1{t: t, falso: nuevoArgosCampo(t), bus: integracion.NuevoBus()}
	e.am = argos.Nuevo(argos.Config{
		Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true, Timeout: 2 * time.Second,
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	})
	e.pend = pendientes.Nuevo(pendientes.Config{Argos: e.am})
	e.pend.Suscribir(e.bus)
	e.m = campo.Nuevo(campo.Config{Inventario: campo.InventarioDeArgos(e.am)})
	almacen, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){e.m.Registrar, adjuntos.Nuevo(adjuntos.Config{Almacen: almacen}).RegistrarRutas},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	e.base = srv.URL
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM integracion.pendientes", "DELETE FROM integracion.conectores",
			"DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas"} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	return e
}

// conectar canjea el enlace del Argos falso (registra la empresa 1) y activa el conector.
func (e *entornoI1) conectar() {
	e.t.Helper()
	c, err := e.am.Conectar.Ejecutar(sistema(e.t), argos.EntradaConectar{Enlace: e.falso.srv.URL + "/conectar#codigo-de-prueba-campo"})
	if err != nil {
		e.t.Fatal(err)
	}
	a, err := e.am.ActivarConector.Ejecutar(sistema(e.t), argos.EntradaCambioConector{ID: c.ID, Version: c.Version})
	if err != nil {
		e.t.Fatal(err)
	}
	e.conID, e.ver = a.ID, a.Version
}

func (e *entornoI1) apagar() {
	e.t.Helper()
	if _, err := e.am.DesactivarConector.Ejecutar(sistema(e.t), argos.EntradaCambioConector{ID: e.conID, Version: e.ver, Motivo: "prueba"}); err != nil {
		e.t.Fatal(err)
	}
}

// relay publica el outbox al bus (el suscriptor de pendientes encola).
func (e *entornoI1) relay() {
	e.t.Helper()
	r := &integracion.Relay{BD: pruebas.Pool(e.t), Bus: e.bus, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := r.Ejecutar(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entornoI1) pendientesDe(orden int64) map[string]string {
	e.t.Helper()
	filas, err := pruebas.Pool(e.t).Query(context.Background(),
		"SELECT idempotency_key, estado FROM integracion.pendientes WHERE entidad = 'orden' AND entidad_id = $1", strconv.FormatInt(orden, 10))
	if err != nil {
		e.t.Fatal(err)
	}
	salida := map[string]string{}
	for filas.Next() {
		var k, s string
		if err := filas.Scan(&k, &s); err != nil {
			e.t.Fatal(err)
		}
		salida[k] = s
	}
	return salida
}

func pngChico(t *testing.T) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// altaAdjunto registra un adjunto de la orden por HTTP (POST /adjuntos).
func altaAdjunto(c *clienteHTTP, orden int64, tipo, claveChecklist string, datos []byte) (uuid.UUID, respuesta) {
	id := uuid.Must(uuid.NewV7())
	h := sha256.Sum256(datos)
	cuerpo := map[string]any{"id": id.String(), "entidad": "orden", "entidadId": strconv.FormatInt(orden, 10), "tipo": tipo,
		"mime": "image/png", "tamano": len(datos), "sha256": hex.EncodeToString(h[:])}
	if claveChecklist != "" {
		cuerpo["claveChecklist"] = claveChecklist
	}
	return id, c.pedir(http.MethodPost, "/api/v1/adjuntos", cuerpo)
}

// subir manda el archivo completo por tus (PATCH).
func subir(t *testing.T, c *clienteHTTP, id uuid.UUID, datos []byte) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, c.base+adjuntos.RutaSubida(id), bytes.NewReader(datos))
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Content-Type", adjuntos.TipoParte)
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("X-CSRF-Token", c.csrf)
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("subida tus: %d", res.StatusCode)
	}
}

func TestIntegracionAdjuntosInventarioYPendientes(t *testing.T) {
	e := nuevoEntornoI1(t)
	pool := pruebas.Pool(t)
	if n := contar(t, "SELECT count(*) FROM integracion.conectores"); n != 0 {
		t.Fatalf("la prueba empieza sin conector: %d", n)
	}

	// 1. Sin conector (nunca se conectó Argos): cerrar una instalación no encola nada.
	d0, t0 := despacho(t), tecnico(t)
	sinAdj := campo.Nuevo(campo.Config{Adjuntos: campo.SinAdjuntosObligatorios{}})
	cd0, ct0 := como(t, d0, "despacho"), como(t, t0, "tecnico")
	o0 := deber(sinAdj.CrearOrden.Ejecutar(cd0, campo.EntradaCrearOrden{Tipo: "instalacion", Direccion: ptr("Zona 1"),
		ClienteID: ptr(int64(990)), ServicioID: ptr(int64(9901))}))(t)
	o0 = deber(sinAdj.ProgramarOrden.Ejecutar(cd0, campo.EntradaProgramarOrden{ID: o0.ID, Version: o0.Version, TecnicoID: &t0, BodegaID: ptr(int64(9)), Inicio: time.Now()}))(t)
	r := deber(sinAdj.Lote(ct0, campo.EntradaLote{DispositivoID: "tel-sin-" + pruebas.Unico(), Operaciones: trabajoDeInstalacion(o0.ID)}))(t)
	if final := r.Resultados[len(r.Resultados)-1]; final.Estado != "aplicada" {
		t.Fatalf("cierre sin conector: %+v", final)
	}
	e.relay()
	if p := e.pendientesDe(o0.ID); len(p) != 0 {
		t.Fatalf("sin conector no se encola: %v", p)
	}

	// 2. Con conector: empresa 1, técnico y despacho de esa empresa.
	e.conectar()
	d, tecA, tecB := despacho(t), tecnico(t), tecnico(t)
	for _, u := range []int64{d, tecA, tecB} {
		asignarEmpresa(t, u, 1)
	}
	cd := pruebas.ContextoUsuarioEnEmpresa(pool, d, 1, permisosDe("despacho")...)
	ctA := pruebas.ContextoUsuarioEnEmpresa(pool, tecA, 1, permisosDe("tecnico")...)
	o := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "instalacion", Direccion: ptr("Zona 10"),
		ClienteID: ptr(int64(990)), ServicioID: ptr(int64(9901))}))(t)
	o = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tecA,
		BodegaID: ptr(int64(9)), Inicio: time.Now()}))(t)

	// Paquete con el inventario de Argos (comodato del servicio y existencias de la bodega 9).
	p := deber(e.m.Paquete(ctA, campo.EntradaPaquete{}))(t)
	var oc *campo.OrdenCampo
	for i := range p.Ordenes {
		if p.Ordenes[i].ID == o.ID {
			oc = &p.Ordenes[i]
		}
	}
	if oc == nil || !oc.Comodato.Disponible || len(oc.Comodato.Equipos) != 1 || oc.Comodato.Equipos[0].UnidadID != 5 ||
		!p.Existencias.Disponible || p.Existencias.Bodegas[0].Lineas[0].Cantidad != 5 {
		t.Fatalf("paquete con Argos: %+v %+v", oc, p.Existencias)
	}

	// Adjuntos por HTTP: el técnico de la orden da de alta; otro técnico, 404.
	cA, cB := entrarComo(t, e.base, tecA), entrarComo(t, e.base, tecB)
	foto, firmaPNG := pngChico(t), pngChico(t)
	idFoto, rA := altaAdjunto(cA, o.ID, "foto", "foto_onu", foto)
	if rA.status != http.StatusCreated {
		t.Fatalf("alta del técnico de la orden: %d %s", rA.status, rA.crudo)
	}
	_, rB := altaAdjunto(cB, o.ID, "foto", "foto_onu", foto)
	rB.problema(t, http.StatusNotFound, "no_encontrado")
	idFirma, rF := altaAdjunto(cA, o.ID, "firma", "", firmaPNG)
	if rF.status != http.StatusCreated {
		t.Fatalf("alta de la firma: %d %s", rF.status, rF.crudo)
	}

	// Trabajo por el lote; la foto del checklist es el adjunto dado de alta (aún sin subir).
	ops := trabajoDeInstalacion(o.ID)
	ops[7] = op(campo.OpAdjuntoRegistrar, o.ID, map[string]any{"adjuntoId": idFoto.String(), "tipo": "foto"})
	ops[8] = op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "foto_onu", "valor": idFoto.String()})
	ops[12] = op(campo.OpFirmaCapturar, o.ID, map[string]any{"nombre": "Ana", "svg": "<svg><path d='M0 0'/></svg>", "adjuntoId": idFirma.String()})
	ajeno := op(campo.OpAdjuntoRegistrar, o.ID, map[string]any{"adjuntoId": uuid.Must(uuid.NewV7()).String(), "tipo": "foto"})
	cerrar := ops[13]
	r = deber(e.m.Lote(ctA, campo.EntradaLote{DispositivoID: "tel-a-" + pruebas.Unico(), Operaciones: append(ops[:13:13], cerrar)}))(t)
	if est := r.Resultados[10]; est.Aviso == nil || *est.Aviso != campo.AvisoExistenciaInsuficiente {
		t.Fatalf("7 m de cable con 5 en la bodega: %+v", est)
	}
	fin := r.Resultados[13]
	if fin.Error == nil || fin.Error.Codigo() != "faltan_adjuntos" {
		t.Fatalf("cerrar sin subir: %+v", fin)
	}
	campos := []string{}
	for _, c := range fin.Error.Errors {
		campos = append(campos, c.Field)
	}
	if strings.Join(campos, ",") != "adjuntos.foto:foto_onu,adjuntos.firma" {
		t.Fatalf("faltantes: %v", campos)
	}

	ra := deber(e.m.Lote(ctA, campo.EntradaLote{DispositivoID: "tel-a-1-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{ajeno}}))(t)
	esperar(t, ra, "rechazada:no_encontrado") // un adjunto que no es de la orden

	// Subidas completas (tus, sha256 verificado): el mismo cierre pasa.
	subir(t, cA, idFoto, foto)
	subir(t, cA, idFirma, firmaPNG)
	r = deber(e.m.Lote(ctA, campo.EntradaLote{DispositivoID: "tel-a-2-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{cerrar}}))(t)
	if r.Resultados[0].Estado != "aplicada" || r.Resultados[0].Orden.Estado != "cerrada" {
		t.Fatalf("cierre con las subidas completas: %+v", r.Resultados[0])
	}

	// orden.cerrada → suscriptor de I1 → pendientes con las claves del contrato → Argos falso.
	e.relay()
	orden := strconv.FormatInt(o.ID, 10)
	pend := e.pendientesDe(o.ID)
	for _, k := range []string{"iris:instalacion:" + orden, "iris:comodato:" + orden + ":HWTC1234", "iris:consumo:" + orden} {
		if _, ok := pend[k]; !ok {
			t.Fatalf("falta el pendiente %s: %v", k, pend)
		}
	}
	if len(pend) != 3 {
		t.Fatalf("pendientes: %v", pend)
	}
	for range 3 {
		if _, err := e.pend.Procesar(sistema(t)); err != nil {
			t.Fatal(err)
		}
	}
	w := e.falso.claves()
	if x, ok := w["iris:instalacion:"+orden]; !ok || x.Ruta != "/servicios/9901/activar-instalacion" {
		t.Fatalf("activar instalación: %+v", w)
	}
	if x, ok := w["iris:comodato:"+orden+":HWTC1234"]; !ok || x.Ruta != "/servicios/9901/equipos" || x.Cuerpo["serie"] != "HWTC1234" {
		t.Fatalf("comodato: %+v", x)
	}
	x, ok := w["iris:consumo:"+orden]
	if !ok || x.Ruta != "/inventario/consumos" || x.Cuerpo["bodega_id"] != 9.0 || x.Cuerpo["origen_tipo"] != "iris_orden" {
		t.Fatalf("consumo: %+v", x)
	}
	if l := x.Cuerpo["lineas"].([]any)[0].(map[string]any); l["producto_id"] != 77.0 || l["cantidad"] != 7.0 {
		t.Fatalf("líneas del consumo: %v", l)
	}

	// Retiro: lo retirado se valida contra el comodato de Argos; con el conector apagado, no.
	ret := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "retiro", Direccion: ptr("Zona 10"),
		ClienteID: ptr(int64(990)), ServicioID: ptr(int64(9901))}))(t)
	ret = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: ret.ID, Version: ret.Version, TecnicoID: &tecA, BodegaID: ptr(int64(9)), Inicio: time.Now()}))(t)
	r = deber(e.m.Lote(ctA, campo.EntradaLote{DispositivoID: "tel-a-3-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{
		op(campo.OpLlegar, ret.ID, nil),
		op(campo.OpEquipoRetirar, ret.ID, map[string]any{"serie": "OTRA", "condicion": "buena"}),
	}}))(t)
	esperar(t, r, "aplicada", "rechazada:equipo_no_en_comodato")
	r = deber(e.m.Lote(ctA, campo.EntradaLote{DispositivoID: "tel-a-3b-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{
		op(campo.OpEquipoRetirar, ret.ID, map[string]any{"unidadId": 5, "condicion": "buena"}),
	}}))(t)
	esperar(t, r, "aplicada")
	// Otra bodega: despacho la indica si es de la empresa en Argos.
	_, err := e.m.UsarMaterialPanel(cd, campo.EntradaUsarMaterial{ID: ret.ID, ProductoID: 77, Cantidad: 1, BodegaID: ptr(int64(8))})
	problema(t, err, 422, "bodega_invalida")

	e.apagar()
	r = deber(e.m.Lote(ctA, campo.EntradaLote{DispositivoID: "tel-a-4-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{
		op(campo.OpEquipoRetirar, ret.ID, map[string]any{"serie": "CUALQUIERA", "condicion": "danada"}),
	}}))(t)
	esperar(t, r, "aplicada")
	if n := contar(t, "SELECT count(*) FROM campo.orden_equipos WHERE orden_id = $1 AND validado", ret.ID); n != 1 {
		t.Fatalf("solo el verificado con el conector activo: %d", n)
	}
	pa := deber(e.m.Paquete(ctA, campo.EntradaPaquete{}))(t)
	if pa.Existencias.Disponible || pa.Existencias.Motivo == nil || *pa.Existencias.Motivo != campo.MotivoSinConector {
		t.Fatalf("conector apagado: inventario no disponible: %+v", pa.Existencias)
	}
}

// entrarComo inicia sesión por HTTP con un usuario de usuarioCon (contraseña conocida).
func entrarComo(t *testing.T, base string, id int64) *clienteHTTP {
	t.Helper()
	var correo string
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT correo FROM nucleo.usuarios WHERE id = $1", id).Scan(&correo); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &clienteHTTP{t: t, base: base, http: &http.Client{Jar: jar}, id: id}
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": "contrasena-larga-1"})
	if r.status != http.StatusOK {
		t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	return c
}
