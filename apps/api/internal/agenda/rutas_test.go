package agenda_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// OSRM falso: puntos con nombre y una matriz fija de minutos (simétrica). /route devuelve los
// puntos pedidos y un punto intermedio (para reconocer el trazo por calles).
type osrmFalso struct {
	srv     *httptest.Server
	caido   atomic.Bool
	tablas  atomic.Int32
	trazos  atomic.Int32
	nombres map[string]string
	minutos map[[2]string]float64
}

var (
	puntoBase = [2]float64{14.600000, -90.520000}
	puntoA    = [2]float64{14.610000, -90.510000}
	puntoC    = [2]float64{14.620000, -90.500000}
	puntoD    = [2]float64{14.630000, -90.490000}
)

func clave(lat, lon float64) string { return fmt.Sprintf("%.6f,%.6f", lon, lat) }

func nuevoOSRMFalso(t *testing.T) *osrmFalso {
	o := &osrmFalso{nombres: map[string]string{
		clave(puntoBase[0], puntoBase[1]): "B", clave(puntoA[0], puntoA[1]): "A",
		clave(puntoC[0], puntoC[1]): "C", clave(puntoD[0], puntoD[1]): "D",
	}, minutos: map[[2]string]float64{
		{"B", "A"}: 10, {"B", "C"}: 20, {"B", "D"}: 30, {"A", "C"}: 10, {"A", "D"}: 20, {"C", "D"}: 10,
	}}
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o.caido.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		partes := strings.Split(r.URL.Path, "/")
		coords := strings.Split(partes[len(partes)-1], ";")
		switch {
		case strings.HasPrefix(r.URL.Path, "/table/v1/driving/"):
			o.tablas.Add(1)
			n := len(coords)
			dur, dist := make([][]float64, n), make([][]float64, n)
			for i := range n {
				dur[i], dist[i] = make([]float64, n), make([]float64, n)
				for j := range n {
					a, b := o.nombres[coords[i]], o.nombres[coords[j]]
					m, ok := o.minutos[[2]string{a, b}]
					if !ok {
						m = o.minutos[[2]string{b, a}]
					}
					dur[i][j], dist[i][j] = m*60, m*400
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "durations": dur, "distances": dist})
		case strings.HasPrefix(r.URL.Path, "/route/v1/driving/"):
			o.trazos.Add(1)
			cs := [][2]float64{}
			for i, c := range coords {
				var lon, lat float64
				_, _ = fmt.Sscanf(c, "%f,%f", &lon, &lat)
				if i > 0 {
					cs = append(cs, [2]float64{lon - 0.001, lat})
				}
				cs = append(cs, [2]float64{lon, lat})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "routes": []any{map[string]any{
				"geometry": map[string]any{"type": "LineString", "coordinates": cs}}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func entornoConOSRM(t *testing.T, o *osrmFalso) entorno {
	r := &reloj{t: gt(2, 9, 0)}
	var enr agenda.Enrutador
	if o != nil {
		cli, err := agenda.NuevoOSRM(o.srv.URL, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		enr = cli
	}
	ag := agenda.Nuevo(agenda.Config{Reloj: r.ahora, Enrutador: enr})
	cmp := campo.Nuevo(campo.Config{Reloj: r.ahora, Adjuntos: campo.SinAdjuntosObligatorios{}, Agenda: ag})
	return entorno{cmp: cmp, ag: ag, reloj: r}
}

// escenarioRuta: técnico con base en B y tres visitas el jueves en A, C y D (D con ventana).
type escenarioRuta struct {
	e               entorno
	cs, cd          context.Context
	tec, desp       int64
	zona, cuadrilla int64
	oA, oC, oD      campo.Orden
}

func nuevoEscenarioRuta(t *testing.T, o *osrmFalso) escenarioRuta {
	t.Helper()
	e := entornoConOSRM(t, o)
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	desp := usuarioCon(t, rol("despacho"))
	cd := como(t, desp, "despacho")
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona rutas " + pruebas.Unico()}))(t)
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla rutas " + pruebas.Unico(), ZonaIDs: []int64{z.ID}}))(t)
	tec := usuarioCon(t, rol("tecnico"))
	f := fichaCon(t, e, cs, tec, &c.ID)
	deber(e.ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, CuadrillaID: &c.ID,
		BodegaID: ptr(int64(9)), BaseLat: ptr(puntoBase[0]), BaseLon: ptr(puntoBase[1])}))(t)
	nueva := func(p [2]float64) campo.Orden {
		return deber(e.cmp.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("Casa"), ZonaID: &z.ID,
			Lat: ptr(p[0]), Lon: ptr(p[1])}))(t)
	}
	// Programadas por hora: C 10:00, A 13:00, D 15:00 (ventana 14:30–15:30).
	oC := deber(programar(e, cd, nueva(puntoC), tec, gt(3, 10, 0)))(t)
	oA := deber(programar(e, cd, nueva(puntoA), tec, gt(3, 13, 0)))(t)
	d := nueva(puntoD)
	oD := deber(e.cmp.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: d.ID, Version: d.Version, TecnicoID: &tec,
		Inicio: gt(3, 15, 0), VentanaInicio: ptr(gt(3, 14, 30)), VentanaFin: ptr(gt(3, 15, 30))}))(t)
	return escenarioRuta{e: e, cs: cs, cd: cd, tec: tec, desp: desp, zona: z.ID, cuadrilla: c.ID, oA: oA, oC: oC, oD: oD}
}

func ordenesDe(r agenda.RutaDetalle) []int64 {
	ids := []int64{}
	for _, p := range r.Paradas {
		ids = append(ids, p.OrdenID)
	}
	return ids
}

const jueves = "2026-10-29"

func TestProponerYPublicarRutaConOSRM(t *testing.T) {
	o := nuevoOSRMFalso(t)
	s := nuevoEscenarioRuta(t, o)
	e := s.e

	r := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	// B → A → C → D: el menor viaje que llega a D dentro de su ventana (espera hasta 14:30).
	if got := ordenesDe(r); fmt.Sprint(got) != fmt.Sprint([]int64{s.oA.ID, s.oC.ID, s.oD.ID}) {
		t.Fatalf("orden: %v (A %d, C %d, D %d)", got, s.oA.ID, s.oC.ID, s.oD.ID)
	}
	if r.Motor != "osrm" || r.Degradada || r.MotivoMotor != nil || r.Estado != "borrador" || r.Modo != "optimizada" ||
		r.OrigenSalida != "base" || r.Fecha != jueves || r.TotalParadas != 3 || r.FueraDeVentana != 0 {
		t.Fatalf("ruta: %+v", r.Ruta)
	}
	if !r.SaleEn.Equal(gt(3, 8, 0)) || !r.Paradas[0].LlegadaEstimada.Equal(gt(3, 8, 10)) ||
		!r.Paradas[1].LlegadaEstimada.Equal(gt(3, 9, 20)) || !r.Paradas[2].LlegadaEstimada.Equal(gt(3, 10, 30)) ||
		r.Paradas[2].EsperaSeg != 4*3600 || !r.Paradas[2].SalidaEstimada.Equal(gt(3, 15, 30)) {
		t.Fatalf("horas: %+v", r.Paradas)
	}
	if r.ViajeSeg != 30*60 || r.DistanciaM != 30*400 || r.TerminaEn == nil || !r.TerminaEn.Equal(gt(3, 15, 30)) || r.ExcedeTurno {
		t.Fatalf("totales: %+v", r.Ruta)
	}
	if r.Paradas[0].DuracionMin != 60 || !r.Paradas[2].Vigente || r.Paradas[2].VentanaInicio == nil || r.Paradas[0].Oculta {
		t.Fatalf("paradas: %+v", r.Paradas)
	}
	// El trazo es el de OSRM (con los puntos intermedios del falso).
	coords, _ := r.Geometria["coordinates"].([]any)
	if r.Geometria["type"] != "LineString" || len(coords) != 7 || o.tablas.Load() != 1 || o.trazos.Load() != 1 {
		t.Fatalf("geometría: %v (tablas %d, trazos %d)", r.Geometria, o.tablas.Load(), o.trazos.Load())
	}
	// Fuera de alcance: 404 sin llegar a OSRM (el alcance se mira antes de la fase previa).
	otra := deber(e.ag.CrearCuadrilla.Ejecutar(s.cs, agenda.EntradaCrearCuadrilla{Nombre: "Ajena OSRM " + pruebas.Unico()}))(t)
	ajeno := conAlcance(t, s.desp, map[string][]contexto.Alcance{"rutas.ver": {{CuadrillaID: otra.ID}}, "rutas.proponer": {{CuadrillaID: otra.ID}}})
	_, err := e.ag.ProponerRuta(ajeno, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves})
	problema(t, err, 404, "no_encontrado")
	if o.tablas.Load() != 1 || o.trazos.Load() != 1 {
		t.Fatalf("OSRM consultado fuera de alcance: tablas %d, trazos %d", o.tablas.Load(), o.trazos.Load())
	}
	p := ultimoEventoRuta(t, "ruta.propuesta", r.ID)
	if p["motor"] != "osrm" || p["degradada"] != false || len(p["ordenes"].([]any)) != 3 || p["fecha"] != jueves {
		t.Fatalf("ruta.propuesta: %+v", p)
	}

	// Manual: el orden pedido. Rehace el mismo borrador (versión).
	_, err = e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves, Version: ptr(r.Version + 5)})
	problema(t, err, 409, "conflicto_version")
	m := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves, Version: &r.Version,
		OrdenIDs: []int64{s.oD.ID, s.oC.ID, s.oA.ID}}))(t)
	if m.ID != r.ID || m.Modo != "manual" || fmt.Sprint(ordenesDe(m)) != fmt.Sprint([]int64{s.oD.ID, s.oC.ID, s.oA.ID}) {
		t.Fatalf("manual: %+v %v", m.Ruta, ordenesDe(m))
	}
	_, err = e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves, OrdenIDs: []int64{s.oD.ID}})
	if pr := problema(t, err, 422, "validacion"); pr.Errors[0].Field != "ordenIds" {
		t.Fatalf("manual incompleto: %+v", pr)
	}
	r = deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)

	// Publicar: el técnico la recibe en el paquete, con sus órdenes en el orden de la ruta.
	_, err = e.ag.Publicar.Ejecutar(s.cd, agenda.EntradaPublicarRuta{ID: r.ID, Version: r.Version - 1})
	problema(t, err, 409, "conflicto_version")
	pub := deber(e.ag.Publicar.Ejecutar(s.cd, agenda.EntradaPublicarRuta{ID: r.ID, Version: r.Version}))(t)
	if pub.Estado != "publicada" || pub.PublicadaEn == nil || pub.PublicadaPor == nil || *pub.PublicadaPor != s.desp {
		t.Fatalf("publicada: %+v", pub.Ruta)
	}
	ev := ultimoEventoRuta(t, "ruta.publicada", r.ID)
	if ev["tecnico_id"] != float64(s.tec) || ev["fecha"] != jueves || len(ev["ordenes"].([]any)) != 3 || ev["reemplaza_id"] != nil {
		t.Fatalf("ruta.publicada: %+v", ev)
	}
	_, err = e.ag.Publicar.Ejecutar(s.cd, agenda.EntradaPublicarRuta{ID: r.ID, Version: pub.Version})
	problema(t, err, 422, "transicion_invalida")
	ct := como(t, s.tec, "tecnico")
	paq := deber(e.cmp.LeerPaquete.Ejecutar(ct, campo.EntradaPaquete{Fecha: jueves}))(t)
	if paq.Ruta == nil || paq.Ruta.ID != r.ID || fmt.Sprint(paq.Ruta.OrdenIDs) != fmt.Sprint([]int64{s.oA.ID, s.oC.ID, s.oD.ID}) ||
		len(paq.Ruta.Paradas) != 3 || paq.Ruta.Desactualizada || paq.Ruta.Degradada {
		t.Fatalf("ruta del paquete: %+v", paq.Ruta)
	}
	if len(paq.Ordenes) != 3 || paq.Ordenes[0].ID != s.oA.ID || paq.Ordenes[1].ID != s.oC.ID || paq.Ordenes[2].ID != s.oD.ID {
		t.Fatalf("órdenes del paquete en orden de ruta: %d %d %d", paq.Ordenes[0].ID, paq.Ordenes[1].ID, paq.Ordenes[2].ID)
	}
	lista := deber(e.ag.ListarRutas.Ejecutar(s.cd, agenda.EntradaListarRutas{Fecha: jueves, TecnicoID: s.tec}))(t)
	if len(lista.Data) != 1 || lista.Data[0].ID != r.ID || lista.Data[0].TotalParadas != 3 || lista.Data[0].TecnicoNombre == "" {
		t.Fatalf("lista: %+v", lista.Data)
	}

	// Reasignación en caliente: C pasa al viernes. La publicada queda marcada (no se borra) y
	// el paquete del técnico lo dice; publicar otra la deja reemplazada.
	oC := deber(e.cmp.ReprogramarOrden.Ejecutar(s.cd, campo.EntradaProgramarOrden{ID: s.oC.ID, Version: s.oC.Version,
		Inicio: gt(4, 10, 0), Motivo: ptr("Cliente pidió el viernes")}))(t)
	ver := deber(e.ag.VerRuta.Ejecutar(s.cd, agenda.EntradaVerRuta{ID: r.ID}))(t)
	if !ver.Desactualizada || ver.MotivoDesactualizada == nil || !strings.Contains(*ver.MotivoDesactualizada, oC.Numero) ||
		ver.Estado != "publicada" || ver.Paradas[1].Vigente || !ver.Paradas[0].Vigente {
		t.Fatalf("desactualizada: %+v %+v", ver.Ruta, ver.Paradas)
	}
	des := ultimoEventoRuta(t, "ruta.desactualizada", r.ID)
	if des["orden_id"] != float64(s.oC.ID) || des["estado"] != "publicada" {
		t.Fatalf("ruta.desactualizada: %+v", des)
	}
	paq = deber(e.cmp.LeerPaquete.Ejecutar(ct, campo.EntradaPaquete{Fecha: jueves}))(t)
	if paq.Ruta == nil || !paq.Ruta.Desactualizada || fmt.Sprint(paq.Ruta.OrdenIDs) != fmt.Sprint([]int64{s.oA.ID, s.oD.ID}) {
		t.Fatalf("paquete con la ruta desactualizada: %+v", paq.Ruta)
	}
	// Otra orden que entra al jueves también la marca (una sola vez: ya estaba).
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'ruta.desactualizada' AND (payload->>'ruta_id')::bigint = $1", r.ID); n != 1 {
		t.Fatalf("eventos de desactualizada: %d", n)
	}
	r2 := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	if r2.ID == r.ID || r2.TotalParadas != 2 || r2.Desactualizada {
		t.Fatalf("nueva propuesta: %+v", r2.Ruta)
	}
	pub2 := deber(e.ag.Publicar.Ejecutar(s.cd, agenda.EntradaPublicarRuta{ID: r2.ID, Version: r2.Version}))(t)
	vieja := deber(e.ag.VerRuta.Ejecutar(s.cd, agenda.EntradaVerRuta{ID: r.ID}))(t)
	if vieja.Estado != "reemplazada" || vieja.ReemplazadaPorID == nil || *vieja.ReemplazadaPorID != pub2.ID || len(vieja.Paradas) != 3 {
		t.Fatalf("reemplazada: %+v", vieja.Ruta)
	}
	if ev := ultimoEventoRuta(t, "ruta.publicada", r2.ID); ev["reemplaza_id"] != float64(r.ID) {
		t.Fatalf("reemplaza: %+v", ev)
	}
	// Un borrador que se desactualiza no se publica.
	r3 := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	deber(e.cmp.ReprogramarOrden.Ejecutar(s.cd, campo.EntradaProgramarOrden{ID: oC.ID, Version: oC.Version,
		Inicio: gt(3, 11, 0), Motivo: ptr("Al final sí el jueves")}))(t)
	r3 = deber(e.ag.VerRuta.Ejecutar(s.cd, agenda.EntradaVerRuta{ID: r3.ID}))(t)
	_, err = e.ag.Publicar.Ejecutar(s.cd, agenda.EntradaPublicarRuta{ID: r3.ID, Version: r3.Version})
	problema(t, err, 422, "ruta_desactualizada")
	if n := contar(t, "SELECT count(*) FROM agenda.rutas WHERE tecnico_id = $1", s.tec); n != 3 {
		t.Fatalf("ninguna ruta se borra: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'rutas.publicar' AND resultado = 'ok' AND entidad_id IN ($1::bigint::text, $2::bigint::text)", r.ID, r2.ID); n != 2 {
		t.Fatalf("auditoría de publicar: %d", n)
	}
}

func ultimoEventoRuta(t *testing.T, evento string, ruta int64) map[string]any {
	t.Helper()
	var crudo []byte
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT payload FROM integracion.outbox WHERE evento = $1 AND (payload->>'ruta_id')::bigint = $2 ORDER BY id DESC LIMIT 1",
		evento, ruta).Scan(&crudo); err != nil {
		t.Fatalf("%s de la ruta %d: %v", evento, ruta, err)
	}
	var p map[string]any
	_ = json.Unmarshal(crudo, &p)
	return p
}

// OSRM caído o sin configurar: la ruta sale igual, por ventana y línea recta, y lo dice.
func TestRutaDegradadaSinOSRM(t *testing.T) {
	o := nuevoOSRMFalso(t)
	o.caido.Store(true)
	s := nuevoEscenarioRuta(t, o)
	r := deber(s.e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	if r.Motor != "haversine" || !r.Degradada || r.MotivoMotor == nil || *r.MotivoMotor != "osrm_no_disponible" {
		t.Fatalf("degradada: %+v", r.Ruta)
	}
	if got := ordenesDe(r); fmt.Sprint(got) != fmt.Sprint([]int64{s.oA.ID, s.oC.ID, s.oD.ID}) || r.FueraDeVentana != 0 {
		t.Fatalf("orden por distancia en línea recta: %v", got)
	}
	coords, _ := r.Geometria["coordinates"].([]any)
	if len(coords) != 4 || r.DistanciaM <= 0 {
		t.Fatalf("trazo recto: %v, %d m", r.Geometria, r.DistanciaM)
	}
	if ev := ultimoEventoRuta(t, "ruta.propuesta", r.ID); ev["degradada"] != true || ev["motivo_motor"] != "osrm_no_disponible" {
		t.Fatalf("evento: %+v", ev)
	}
	// Sin OSRM configurado.
	s2 := nuevoEscenarioRuta(t, nil)
	r2 := deber(s2.e.ag.ProponerRuta(s2.cd, agenda.EntradaProponerRuta{TecnicoID: s2.tec, Fecha: jueves}))(t)
	if r2.Motor != "haversine" || r2.MotivoMotor == nil || *r2.MotivoMotor != "sin_osrm" {
		t.Fatalf("sin OSRM: %+v", r2.Ruta)
	}
	// Sin citas ese día: 422.
	_, err := s2.e.ag.ProponerRuta(s2.cd, agenda.EntradaProponerRuta{TecnicoID: s2.tec, Fecha: "2026-10-30"})
	problema(t, err, 422, "ruta_vacia")
}

// Ruta propuesta frente a la real: horas de llegada y recorrido (campo.posiciones).
func TestRutaPropuestaFrenteALaReal(t *testing.T) {
	s := nuevoEscenarioRuta(t, nuevoOSRMFalso(t))
	e := s.e
	r := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	r = deber(e.ag.Publicar.Ejecutar(s.cd, agenda.EntradaPublicarRuta{ID: r.ID, Version: r.Version}))(t)

	// Jueves: sale 08:05 hacia A y llega 08:12 (estimada 08:10).
	ct := como(t, s.tec, "tecnico")
	e.reloj.mu.Lock()
	e.reloj.t = gt(3, 8, 5)
	e.reloj.mu.Unlock()
	a := deber(e.cmp.IniciarCamino.Ejecutar(ct, campo.EntradaPosicion{ID: s.oA.ID, Version: s.oA.Version}))(t)
	e.reloj.mu.Lock()
	e.reloj.t = gt(3, 8, 12)
	e.reloj.mu.Unlock()
	deber(e.cmp.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: a.ID, Version: a.Version}))(t)
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(context.Background(), "SELECT campo.asegurar_particion_posiciones($1)", gt(3, 8, 0)); err != nil {
		t.Fatal(err)
	}
	for i, p := range [][2]float64{puntoBase, {14.605, -90.515}, puntoA} {
		if _, err := pool.Exec(context.Background(), "INSERT INTO campo.posiciones (tecnico_id, lat, lon, registrada_en) VALUES ($1, $2, $3, $4)",
			s.tec, p[0], p[1], gt(3, 8, 5+3*i)); err != nil {
			t.Fatal(err)
		}
	}
	cal := deber(e.ag.Calendario.Ejecutar(s.cd, agenda.EntradaCalendario{Desde: gt(3, 0, 0).Format(time.RFC3339),
		Hasta: gt(4, 0, 0).Format(time.RFC3339), TecnicoID: s.tec}))(t)
	for _, c := range cal.Citas {
		if (c.OrdenID == s.oA.ID) != (c.EnSitioEn != nil && c.EnSitioEn.Equal(gt(3, 8, 12))) {
			t.Fatalf("enSitioEn en el calendario: %+v", c)
		}
	}
	ver := deber(e.ag.VerRuta.Ejecutar(s.cd, agenda.EntradaVerRuta{ID: r.ID}))(t)
	pa := ver.Paradas[0]
	if pa.OrdenID != s.oA.ID || pa.Estado != "en_sitio" || pa.LlegadaReal == nil || !pa.LlegadaReal.Equal(gt(3, 8, 12)) ||
		pa.DesvioSeg == nil || *pa.DesvioSeg != 120 || pa.EnCaminoEn == nil {
		t.Fatalf("llegada real: %+v", pa)
	}
	if !ver.Real.Disponible || ver.Real.TotalPuntos != 3 || len(ver.Real.Puntos) != 3 || ver.Real.DistanciaM < 1000 ||
		fmt.Sprint(ver.Real.OrdenVisitado) != fmt.Sprint([]int64{s.oA.ID}) {
		t.Fatalf("recorrido real: %+v", ver.Real)
	}
	// Sin campo.ver_posiciones (rol lectura) no se ve el recorrido, pero sí las horas.
	lec := como(t, usuarioCon(t, rol("lectura")), "lectura")
	ver = deber(e.ag.VerRuta.Ejecutar(lec, agenda.EntradaVerRuta{ID: r.ID}))(t)
	if ver.Real.Disponible || ver.Real.Motivo == nil || *ver.Real.Motivo != "sin_permiso" || len(ver.Real.Puntos) != 0 {
		t.Fatalf("sin permiso de posiciones: %+v", ver.Real)
	}
	// Hoy, con A en sitio: al volver a proponer A queda fija primero y lo demás sale desde A.
	r2 := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	if !r2.Paradas[0].Fija || r2.Paradas[0].OrdenID != s.oA.ID || r2.OrigenSalida != "ultima_parada" ||
		r2.SalidaLat == nil || *r2.SalidaLat != puntoA[0] || !r2.Paradas[0].LlegadaEstimada.Equal(gt(3, 8, 12)) {
		t.Fatalf("propuesta con una parada hecha: %+v %+v", r2.Ruta, r2.Paradas[0])
	}
}

// Alcance: falla cerrado. Despacho de otra cuadrilla no ve ni propone; una parada fuera del
// alcance por zona sale oculta y no deja proponer.
func TestRutaAlcance(t *testing.T) {
	s := nuevoEscenarioRuta(t, nil)
	e := s.e
	r := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	otra := deber(e.ag.CrearCuadrilla.Ejecutar(s.cs, agenda.EntradaCrearCuadrilla{Nombre: "Otra rutas " + pruebas.Unico()}))(t)
	ajeno := conAlcance(t, s.desp, map[string][]contexto.Alcance{
		"rutas.ver": {{CuadrillaID: otra.ID}}, "rutas.proponer": {{CuadrillaID: otra.ID}}, "rutas.publicar": {{CuadrillaID: otra.ID}},
	})
	_, err := e.ag.ProponerRuta(ajeno, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves})
	problema(t, err, 404, "no_encontrado")
	_, err = e.ag.VerRuta.Ejecutar(ajeno, agenda.EntradaVerRuta{ID: r.ID})
	problema(t, err, 404, "no_encontrado")
	_, err = e.ag.Publicar.Ejecutar(ajeno, agenda.EntradaPublicarRuta{ID: r.ID, Version: r.Version})
	problema(t, err, 404, "no_encontrado")
	if l := deber(e.ag.ListarRutas.Ejecutar(ajeno, agenda.EntradaListarRutas{Fecha: jueves}))(t); len(l.Data) != 0 {
		t.Fatalf("lista ajena: %+v", l.Data)
	}
	// Ve la ruta (cuadrilla) pero no puede publicar: 403.
	soloVer := conAlcance(t, s.desp, map[string][]contexto.Alcance{
		"rutas.ver": {{CuadrillaID: s.cuadrilla}}, "rutas.publicar": {{CuadrillaID: otra.ID}},
	})
	_, err = e.ag.Publicar.Ejecutar(soloVer, agenda.EntradaPublicarRuta{ID: r.ID, Version: r.Version})
	problema(t, err, 403, "fuera_de_alcance")
	// Alcance por zona: la cuadrilla del técnico atiende la zona, pero una orden de otra zona
	// sale oculta y la ruta no se puede proponer con ese alcance.
	z2 := deber(e.ag.CrearZona.Ejecutar(s.cs, agenda.EntradaCrearZona{Nombre: "Zona ajena " + pruebas.Unico()}))(t)
	c := deber(e.ag.ListarCuadrillas.Ejecutar(s.cs, agenda.EntradaListarCatalogo{}))(t)
	for _, cu := range c.Data {
		if cu.ID == s.cuadrilla {
			deber(e.ag.EditarCuadrilla.Ejecutar(s.cs, agenda.EntradaEditarCuadrilla{ID: cu.ID, Version: cu.Version, Nombre: cu.Nombre, ZonaIDs: []int64{s.zona, z2.ID}}))(t)
		}
	}
	fuera := deber(e.cmp.CrearOrden.Ejecutar(s.cd, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("Lejos"), ZonaID: &z2.ID,
		Lat: ptr(puntoD[0]), Lon: ptr(puntoD[1])}))(t)
	deber(programar(e, s.cd, fuera, s.tec, gt(3, 16, 0)))(t)
	porZona := conAlcance(t, s.desp, map[string][]contexto.Alcance{
		"rutas.ver": {{ZonaID: s.zona}}, "rutas.proponer": {{ZonaID: s.zona}},
	})
	_, err = e.ag.ProponerRuta(porZona, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves})
	problema(t, err, 403, "fuera_de_alcance")
	r = deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: jueves}))(t)
	ver := deber(e.ag.VerRuta.Ejecutar(porZona, agenda.EntradaVerRuta{ID: r.ID}))(t)
	ocultas := 0
	for i, p := range ver.Paradas {
		if p.Oculta {
			ocultas++
			if (p != agenda.ParadaRuta{Posicion: p.Posicion, Oculta: true}) {
				t.Fatalf("parada oculta con datos (solo la posición): %+v", p)
			}
			if i+1 < len(ver.Paradas) && (ver.Paradas[i+1].ViajeSeg != 0 || ver.Paradas[i+1].DistanciaM != 0) {
				t.Fatalf("el viaje desde la oculta la ubica: %+v", ver.Paradas[i+1])
			}
		}
	}
	if ocultas != 1 || len(ver.Paradas) != 4 {
		t.Fatalf("ocultas: %d de %d", ocultas, len(ver.Paradas))
	}
	// El trazo pasa por la parada oculta: no sale. Quien la ve, sí lo recibe.
	if ver.Geometria != nil {
		t.Fatalf("geometría con una parada oculta: %v", ver.Geometria)
	}
	if todo := deber(e.ag.VerRuta.Ejecutar(s.cd, agenda.EntradaVerRuta{ID: r.ID}))(t); todo.Geometria == nil {
		t.Fatal("con alcance global sí hay geometría")
	}
}

// Multiempresa: una ruta lleva solo las citas de su empresa, no se ve desde otra y la marca de
// desactualizada no cruza empresas.
func TestRutaPorEmpresa(t *testing.T) {
	e := entornoConOSRM(t, nil)
	registrarEmpresas(t, 61, 62)
	adm, desp, tec := usuarioCon(t, rol("admin")), usuarioCon(t, rol("despacho")), usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, desp, 61, 62)
	asignarEmpresa(t, tec, 61, 62)
	a61, d61, d62 := enEmpresa(t, adm, 61, "admin"), enEmpresa(t, desp, 61, "despacho"), enEmpresa(t, desp, 62, "despacho")
	z61 := deber(e.ag.CrearZona.Ejecutar(a61, agenda.EntradaCrearZona{Nombre: "Z61 " + pruebas.Unico()}))(t)
	z62 := deber(e.ag.CrearZona.Ejecutar(enEmpresa(t, adm, 62, "admin"), agenda.EntradaCrearZona{Nombre: "Z62 " + pruebas.Unico()}))(t)
	fichaCon(t, e, a61, tec, nil)
	nueva := func(ctx context.Context, z int64, p [2]float64, hora int) campo.Orden {
		o := deber(e.cmp.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ZonaID: &z, Lat: ptr(p[0]), Lon: ptr(p[1])}))(t)
		return deber(programar(e, ctx, o, tec, gt(3, hora, 0)))(t)
	}
	o61 := nueva(d61, z61.ID, puntoA, 9)
	o62 := nueva(d62, z62.ID, puntoC, 11)
	r61 := deber(e.ag.ProponerRuta(d61, agenda.EntradaProponerRuta{TecnicoID: tec, Fecha: jueves}))(t)
	r62 := deber(e.ag.ProponerRuta(d62, agenda.EntradaProponerRuta{TecnicoID: tec, Fecha: jueves}))(t)
	if fmt.Sprint(ordenesDe(r61)) != fmt.Sprint([]int64{o61.ID}) || fmt.Sprint(ordenesDe(r62)) != fmt.Sprint([]int64{o62.ID}) ||
		r61.EmpresaID == nil || *r61.EmpresaID != 61 {
		t.Fatalf("paradas por empresa: %v %v", ordenesDe(r61), ordenesDe(r62))
	}
	_, err := e.ag.VerRuta.Ejecutar(d62, agenda.EntradaVerRuta{ID: r61.ID})
	problema(t, err, 404, "no_encontrado")
	_, err = e.ag.Publicar.Ejecutar(d62, agenda.EntradaPublicarRuta{ID: r61.ID, Version: r61.Version})
	problema(t, err, 404, "no_encontrado")
	for _, r := range deber(e.ag.ListarRutas.Ejecutar(d62, agenda.EntradaListarRutas{Fecha: jueves}))(t).Data {
		if r.ID == r61.ID {
			t.Fatal("la lista de la 62 trae la ruta de la 61")
		}
	}
	// Mover la orden de la 62 marca solo la ruta de la 62.
	deber(e.cmp.ReprogramarOrden.Ejecutar(d62, campo.EntradaProgramarOrden{ID: o62.ID, Version: o62.Version, Inicio: gt(3, 14, 0), Motivo: ptr("Más tarde")}))(t)
	if v := deber(e.ag.VerRuta.Ejecutar(d61, agenda.EntradaVerRuta{ID: r61.ID}))(t); v.Desactualizada {
		t.Fatal("la ruta de la 61 no se toca")
	}
	if v := deber(e.ag.VerRuta.Ejecutar(d62, agenda.EntradaVerRuta{ID: r62.ID}))(t); !v.Desactualizada {
		t.Fatal("la ruta de la 62 queda desactualizada")
	}
	// El paquete de la 61 trae solo su ruta.
	deber(e.ag.Publicar.Ejecutar(d61, agenda.EntradaPublicarRuta{ID: r61.ID, Version: r61.Version}))(t)
	paq := deber(e.cmp.LeerPaquete.Ejecutar(enEmpresa(t, tec, 61, "tecnico"), campo.EntradaPaquete{Fecha: jueves}))(t)
	if paq.Ruta == nil || paq.Ruta.ID != r61.ID || fmt.Sprint(paq.Ruta.OrdenIDs) != fmt.Sprint([]int64{o61.ID}) {
		t.Fatalf("paquete de la 61: %+v", paq.Ruta)
	}
	if paq = deber(e.cmp.LeerPaquete.Ejecutar(enEmpresa(t, tec, 62, "tecnico"), campo.EntradaPaquete{Fecha: jueves}))(t); paq.Ruta != nil {
		t.Fatalf("la 62 no tiene ruta publicada: %+v", paq.Ruta)
	}
}

// Una ruta que sale de la última posición del técnico no la muestra a quien no puede ver
// posiciones (rol lectura): ni la salida ni el trazo, en la lista ni en el detalle.
func TestRutaPosicionDeSalidaSoloConPermiso(t *testing.T) {
	s := nuevoEscenarioRuta(t, nil)
	e := s.e
	// Hoy (miércoles 09:00), con una cita a las 11:00 y una posición de hace 10 minutos.
	o := deber(e.cmp.CrearOrden.Ejecutar(s.cd, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("Hoy"), ZonaID: &s.zona,
		Lat: ptr(puntoC[0]), Lon: ptr(puntoC[1])}))(t)
	deber(programar(e, s.cd, o, s.tec, gt(2, 11, 0)))(t)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO campo.posiciones_ultimas
		(tecnico_id, empresa_id, lat, lon, registrada_en, recibida_en) VALUES ($1, NULL, 14.611111, -90.511111, $2, $2)`,
		s.tec, gt(2, 8, 50)); err != nil {
		t.Fatal(err)
	}
	r := deber(e.ag.ProponerRuta(s.cd, agenda.EntradaProponerRuta{TecnicoID: s.tec, Fecha: "2026-10-28"}))(t)
	if r.OrigenSalida != "posicion" || r.SalidaLat == nil || *r.SalidaLat != 14.611111 || r.Geometria == nil {
		t.Fatalf("despacho (con campo.ver_posiciones) ve la salida: %+v", r.Ruta)
	}
	lec := como(t, usuarioCon(t, rol("lectura")), "lectura")
	v := deber(e.ag.VerRuta.Ejecutar(lec, agenda.EntradaVerRuta{ID: r.ID}))(t)
	if v.OrigenSalida != "posicion" || v.SalidaLat != nil || v.SalidaLon != nil || v.Geometria != nil {
		t.Fatalf("lectura no ve la posición: %+v %v", v.Ruta, v.Geometria)
	}
	for _, l := range deber(e.ag.ListarRutas.Ejecutar(lec, agenda.EntradaListarRutas{Fecha: "2026-10-28", TecnicoID: s.tec}))(t).Data {
		if l.SalidaLat != nil || l.SalidaLon != nil {
			t.Fatalf("lista para lectura: %+v", l)
		}
	}
	if l := deber(e.ag.ListarRutas.Ejecutar(s.cd, agenda.EntradaListarRutas{Fecha: "2026-10-28", TecnicoID: s.tec}))(t); len(l.Data) != 1 || l.Data[0].SalidaLat == nil {
		t.Fatalf("lista para despacho: %+v", l.Data)
	}
}

// El paquete trae el turno del día del técnico, sin sus ausencias (pedido de I4).
func TestPaqueteConTurnoDelDia(t *testing.T) {
	s := nuevoEscenarioRuta(t, nil)
	e := s.e
	ct := como(t, s.tec, "tecnico")
	paq := deber(e.cmp.LeerPaquete.Ejecutar(ct, campo.EntradaPaquete{Fecha: jueves}))(t)
	if paq.Turno == nil || !paq.Turno.Inicio.Equal(gt(3, 8, 0)) || !paq.Turno.Fin.Equal(gt(3, 17, 0)) || len(paq.Turno.Tramos) != 1 {
		t.Fatalf("turno del jueves: %+v", paq.Turno)
	}
	f := deber(e.ag.ListarTecnicos.Ejecutar(s.cs, agenda.EntradaListarTecnicos{UsuarioID: s.tec}))(t)
	deber(e.ag.RegistrarAusencia.Ejecutar(s.cs, agenda.EntradaRegistrarAusencia{ID: f.Data[0].ID, Inicio: gt(3, 8, 0).Format(time.RFC3339),
		Fin: gt(3, 12, 0).Format(time.RFC3339), Motivo: "permiso"}))(t)
	paq = deber(e.cmp.LeerPaquete.Ejecutar(ct, campo.EntradaPaquete{Fecha: jueves}))(t)
	if paq.Turno == nil || !paq.Turno.Inicio.Equal(gt(3, 12, 0)) || !paq.Turno.Fin.Equal(gt(3, 17, 0)) {
		t.Fatalf("turno sin la ausencia: %+v", paq.Turno)
	}
	if paq = deber(e.cmp.LeerPaquete.Ejecutar(ct, campo.EntradaPaquete{Fecha: "2026-10-31"}))(t); paq.Turno != nil {
		t.Fatalf("el sábado no trabaja: %+v", paq.Turno)
	}
}
