package agenda

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Pruebas del plan (puro) y del cliente de OSRM contra un servidor falso. No tocan la base.

var ocho = time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC) // 08:00 en Guatemala

func en(min int) *time.Time { t := ocho.Add(time.Duration(min) * time.Minute); return &t }

// matriz simétrica en minutos.
func matrizMin(n int, pares map[[2]int]float64) Matriz {
	m := Matriz{Segundos: make([][]float64, n), Metros: make([][]float64, n)}
	for i := range n {
		m.Segundos[i], m.Metros[i] = make([]float64, n), make([]float64, n)
	}
	for k, v := range pares {
		m.Segundos[k[0]][k[1]], m.Segundos[k[1]][k[0]] = v*60, v*60
		m.Metros[k[0]][k[1]], m.Metros[k[1]][k[0]] = v*500, v*500
	}
	return m
}

func ids(p plan) []int64 {
	s := []int64{}
	for _, t := range p.Tramos {
		s = append(s, t.Parada.OrdenID)
	}
	return s
}

func parada(id int64, lat float64, dur int) paradaPlan {
	return paradaPlan{OrdenID: id, Punto: &Punto{Lat: lat, Lon: -90.5}, Duracion: time.Duration(dur) * time.Minute, CitaInicio: ocho}
}

func TestPlanRespetaLaVentana(t *testing.T) {
	// Salida (0) y paradas 1, 2, 3 en línea: lo más corto es 1 → 2 → 3, pero la 3 tiene ventana
	// de 08:00 a 08:45 y hay que ir primero ahí.
	base := Punto{Lat: 14.6, Lon: -90.5}
	e := planEntrada{Salida: &base, OrigenSalida: "base", SaleEn: ocho,
		Libres: []paradaPlan{parada(1, 14.61, 30), parada(2, 14.62, 30), parada(3, 14.63, 30)}}
	e.Libres[2].VentanaIni, e.Libres[2].VentanaFin = en(0), en(45)
	m := matrizMin(4, map[[2]int]float64{{0, 1}: 10, {0, 2}: 20, {0, 3}: 30, {1, 2}: 10, {1, 3}: 20, {2, 3}: 10})
	p, err := planificar(e, m)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(p); fmt.Sprint(got) != "[3 2 1]" {
		t.Fatalf("orden con ventana: %v", got)
	}
	if p.Retraso != 0 || !p.Tramos[0].Llegada.Equal(*en(30)) || p.Tramos[0].ViajeSeg != 1800 {
		t.Fatalf("llegada a la de ventana: %+v", p.Tramos[0])
	}
	// Sin la ventana, la más corta.
	e.Libres[2].VentanaIni, e.Libres[2].VentanaFin = nil, nil
	p, _ = planificar(e, m)
	if got := ids(p); fmt.Sprint(got) != "[1 2 3]" {
		t.Fatalf("sin ventana: %v", got)
	}
	if p.ViajeSeg != 1800 || p.Metros != 15000 || !p.TerminaEn.Equal(*en(30 + 90)) {
		t.Fatalf("totales: viaje %d, metros %d, termina %v", p.ViajeSeg, p.Metros, p.TerminaEn)
	}
	// Llegar antes de la ventana espera; llegar después es retraso.
	e.Libres[0].VentanaIni, e.Libres[0].VentanaFin = en(60), en(90)
	p, _ = planificar(e, m)
	for _, tr := range p.Tramos {
		if tr.Parada.OrdenID == 1 && (tr.Llegada.Before(*en(10)) || tr.Retraso != 0) {
			t.Fatalf("ventana de la 1: %+v", tr)
		}
	}
	imposible := planEntrada{Salida: &base, SaleEn: ocho, Libres: []paradaPlan{parada(1, 14.61, 30)}}
	imposible.Libres[0].VentanaIni, imposible.Libres[0].VentanaFin = en(-60), en(-30)
	p, _ = planificar(imposible, matrizMin(2, map[[2]int]float64{{0, 1}: 10}))
	if p.Retraso != 40*60 || p.Tramos[0].Retraso != 2400 {
		t.Fatalf("retraso: %+v", p.Tramos[0])
	}
}

func TestPlanFijasManualYSinCoordenadas(t *testing.T) {
	base := Punto{Lat: 14.6, Lon: -90.5}
	hecha := parada(9, 14.65, 30)
	hecha.Fija, hecha.LlegadaReal, hecha.SalidaReal = true, en(50), en(70)
	sinPunto := paradaPlan{OrdenID: 7, Duracion: 20 * time.Minute, CitaInicio: ocho}
	e := planEntrada{Salida: &base, SaleEn: ocho, Fijas: []paradaPlan{hecha},
		Libres: []paradaPlan{sinPunto, parada(1, 14.61, 30), parada(2, 14.62, 30)}}
	m := matrizMin(4, map[[2]int]float64{{0, 1}: 10, {0, 2}: 20, {0, 3}: 5, {1, 2}: 5, {1, 3}: 15, {2, 3}: 10})
	p, err := planificar(e, m)
	if err != nil {
		t.Fatal(err)
	}
	// La fija primero (con su hora real); la sin coordenadas al final; las otras, desde la fija
	// (fila 1): 1 → 2 (5 + 10 min) es mejor que 2 → 1 (15 + 10).
	if got := ids(p); fmt.Sprint(got) != "[9 1 2 7]" {
		t.Fatalf("orden: %v", got)
	}
	if !p.Tramos[0].Llegada.Equal(*en(50)) || !p.Tramos[1].Llegada.Equal(*en(75)) {
		t.Fatalf("horas: %v %v", p.Tramos[0].Llegada, p.Tramos[1].Llegada)
	}
	if p.Tramos[3].ViajeSeg != 0 || !p.Tramos[3].Llegada.Equal(p.Tramos[2].Salida) {
		t.Fatalf("sin coordenadas: %+v", p.Tramos[3])
	}
	// Hoy, la parte libre no empieza antes de ahora.
	ahora := *en(180)
	e.Ahora = &ahora
	p, _ = planificar(e, m)
	if !p.Tramos[1].Llegada.Equal(ahora.Add(5 * time.Minute)) {
		t.Fatalf("desde ahora: %v", p.Tramos[1].Llegada)
	}
	// Manual: se respeta; la fija puede venir o no; faltar o sobrar una es 422.
	e.Ahora = nil
	e.Manual = []int64{9, 2, 7, 1}
	p, err = planificar(e, m)
	if err != nil || fmt.Sprint(ids(p)) != "[9 2 7 1]" {
		t.Fatalf("manual: %v %v", ids(p), err)
	}
	for _, mal := range [][]int64{{2, 1}, {2, 7, 1, 1}, {2, 7, 1, 55}} {
		e.Manual = mal
		if _, err := planificar(e, m); err == nil {
			t.Fatalf("manual inválido %v", mal)
		}
	}
}

func TestPlanConMuchasParadas(t *testing.T) {
	// 12 paradas en una línea, desordenadas: la búsqueda local las recorre en orden.
	n := 12
	base := Punto{Lat: 14.6, Lon: -90.5}
	e := planEntrada{Salida: &base, SaleEn: ocho}
	for i := range n {
		id := int64((i*7)%n + 1)
		e.Libres = append(e.Libres, parada(id, 14.6+float64(id)/100, 15))
	}
	ps, _, _ := e.puntos()
	p, err := planificar(e, MatrizEnLineaRecta(ps))
	if err != nil {
		t.Fatal(err)
	}
	for i, tr := range p.Tramos {
		if tr.Parada.OrdenID != int64(i+1) {
			t.Fatalf("orden con %d paradas: %v", n, ids(p))
		}
	}
}

func TestHaversineYLineaRecta(t *testing.T) {
	d := Haversine(Punto{Lat: 14, Lon: -90}, Punto{Lat: 15, Lon: -90})
	if math.Abs(d-111195) > 100 {
		t.Fatalf("un grado de latitud: %.0f m", d)
	}
	m := MatrizEnLineaRecta([]Punto{{14, -90}, {15, -90}})
	if math.Abs(m.Metros[0][1]-d*FactorRodeo) > 1 || math.Abs(m.Segundos[0][1]-m.Metros[0][1]/(30000.0/3600)) > 1 {
		t.Fatalf("línea recta: %+v", m)
	}
}

func TestClienteOSRM(t *testing.T) {
	var pedidas []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidas = append(pedidas, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case strings.HasPrefix(r.URL.Path, "/table/v1/driving/"):
			_, _ = w.Write([]byte(`{"code":"Ok","durations":[[0,100],[null,0]],"distances":[[0,900],[null,0]]}`))
		case strings.HasPrefix(r.URL.Path, "/route/v1/driving/"):
			_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"geometry":{"type":"LineString","coordinates":[[-90.5,14.6],[-90.49,14.61]]}}]}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"InvalidUrl","message":"x"}`))
		}
	}))
	defer srv.Close()
	o, err := NuevoOSRM(srv.URL+"/", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ps := []Punto{{14.6, -90.5}, {14.61, -90.49}}
	m, err := o.Matriz(context.Background(), ps)
	if err != nil {
		t.Fatal(err)
	}
	if m.Segundos[0][1] != 100 || m.Metros[0][1] != 900 || m.Segundos[1][0] <= 0 {
		t.Fatalf("matriz (null en línea recta): %+v", m)
	}
	if pedidas[0] != "/table/v1/driving/-90.500000,14.600000;-90.490000,14.610000?annotations=duration,distance" {
		t.Fatalf("petición: %s", pedidas[0])
	}
	tr, err := o.Trazo(context.Background(), ps)
	if err != nil || len(tr) != 2 || tr[1][0] != -90.49 {
		t.Fatalf("trazo: %v %v", tr, err)
	}
	// Respuesta de error, cuerpo ilegible o servidor caído: ErrOSRM.
	malo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html>"))
	}))
	o2, _ := NuevoOSRM(malo.URL, time.Second)
	if _, err := o2.Matriz(context.Background(), ps); !errors.Is(err, ErrOSRM) {
		t.Fatalf("503: %v", err)
	}
	malo.Close()
	if _, err := o2.Matriz(context.Background(), ps); !errors.Is(err, ErrOSRM) {
		t.Fatalf("caído: %v", err)
	}
	if _, err := NuevoOSRM("ftp://x", 0); err == nil {
		t.Fatal("URL inválida")
	}
	t.Setenv("IRIS_OSRM_URL", "")
	if enr, err := OSRMDesdeEntorno(); enr != nil || err != nil {
		t.Fatalf("sin URL: %v %v", enr, err)
	}
	t.Setenv("IRIS_OSRM_URL", srv.URL)
	t.Setenv("IRIS_OSRM_ESPERA", "nada")
	if _, err := OSRMDesdeEntorno(); err == nil {
		t.Fatal("espera inválida")
	}
}
