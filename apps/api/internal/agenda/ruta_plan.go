package agenda

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// Plan de la ruta del día (puro: sin base de datos ni red). Las paradas fijas (ya en camino, en
// sitio o terminadas) van primero en el orden real; las libres se ordenan para llegar dentro de
// la ventana acordada con el cliente y, después, con el menor tiempo de viaje y espera.
//
// Simulación: se sale de la salida (base, posición o la primera parada) a la hora de salida; en
// cada parada se llega tras el viaje, se espera si se llega antes de la ventana y se trabaja la
// duración del tipo de orden. Llegar después del fin de la ventana es retraso. Una parada fija
// con hora real (en sitio, terminada) ancla el reloj en esa hora.

// paradaPlan es una parada a ordenar.
type paradaPlan struct {
	OrdenID    int64
	Punto      *Punto // nil = sin coordenadas (viaje 0, va al final en modo optimizado)
	Duracion   time.Duration
	VentanaIni *time.Time
	VentanaFin *time.Time
	CitaInicio time.Time
	Fija       bool
	// Hora real de llegada y de salida (paradas fijas).
	LlegadaReal *time.Time
	SalidaReal  *time.Time
}

// planEntrada es lo necesario para proponer la ruta.
type planEntrada struct {
	Salida       *Punto
	OrigenSalida string
	SaleEn       time.Time  // hora de salida (inicio del turno o la primera cita)
	Ahora        *time.Time // hoy: la parte libre no empieza antes de ahora
	FinTurno     *time.Time
	Fijas        []paradaPlan // en el orden real
	Libres       []paradaPlan // en el orden de la cita
	Manual       []int64      // orden pedido para las libres (modo manual)
}

// puntos de la matriz: la salida (si hay) y las paradas con coordenadas. indice[orden] = fila.
func (e planEntrada) puntos() ([]Punto, map[int64]int, int) {
	ps := []Punto{}
	salida := -1
	if e.Salida != nil {
		salida = 0
		ps = append(ps, *e.Salida)
	}
	indice := map[int64]int{}
	for _, p := range slices.Concat(e.Fijas, e.Libres) {
		if p.Punto != nil {
			indice[p.OrdenID] = len(ps)
			ps = append(ps, *p.Punto)
		}
	}
	return ps, indice, salida
}

// huella de los puntos de la matriz (salida y paradas con coordenadas, en orden): si cambian
// entre la lectura de OSRM (fuera de la transacción) y la transacción, la matriz ya no sirve.
func (e planEntrada) huella() string {
	ps, indice, _ := e.puntos()
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "%.6f,%.6f;", p.Lat, p.Lon)
	}
	ids := make([]int64, 0, len(indice))
	for id := range indice {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		fmt.Fprintf(&b, "%d:%d;", id, indice[id])
	}
	return b.String()
}

// tramo calculado de una parada.
type tramo struct {
	Parada    paradaPlan
	Llegada   time.Time
	Salida    time.Time
	ViajeSeg  int32
	Metros    int32
	EsperaSeg int32
	Retraso   int32
}

// plan es la ruta calculada.
type plan struct {
	Tramos    []tramo
	SaleEn    time.Time
	TerminaEn *time.Time
	Metros    int32
	ViajeSeg  int32
	EsperaSeg int32
	Retraso   int32 // segundos de retraso en total
	Secuencia []Punto
}

// reloj de la simulación.
type estadoSim struct {
	fila  int // fila de la matriz donde está (-1: sin posición conocida)
	hora  time.Time
	costo float64
}

func segundos(d float64) int32 {
	if d <= 0 || math.IsNaN(d) {
		return 0
	}
	if d > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(math.Round(d))
}

// paso simula una parada desde el estado.
func paso(m Matriz, indice map[int64]int, s estadoSim, p paradaPlan) (estadoSim, tramo) {
	t := tramo{Parada: p}
	fila := s.fila
	viaje := 0.0
	if p.Punto != nil {
		destino := indice[p.OrdenID]
		if s.fila >= 0 {
			viaje = m.Segundos[s.fila][destino]
			t.Metros = segundos(m.Metros[s.fila][destino])
		}
		fila = destino
	}
	t.ViajeSeg = segundos(viaje)
	llegada := s.hora.Add(time.Duration(t.ViajeSeg) * time.Second)
	if p.LlegadaReal != nil {
		llegada = *p.LlegadaReal
	}
	inicio := llegada
	if p.LlegadaReal == nil && p.VentanaIni != nil && llegada.Before(*p.VentanaIni) {
		inicio = *p.VentanaIni
		t.EsperaSeg = segundos(p.VentanaIni.Sub(llegada).Seconds())
	}
	if p.VentanaFin != nil && llegada.After(*p.VentanaFin) {
		t.Retraso = segundos(llegada.Sub(*p.VentanaFin).Seconds())
	}
	salida := inicio.Add(p.Duracion)
	if p.SalidaReal != nil {
		salida = *p.SalidaReal
	}
	t.Llegada, t.Salida = llegada, salida
	// Costo: el retraso pesa 100 veces más que el tiempo; luego la hora de terminar (costoFinal)
	// y, a igual hora, el menor viaje.
	costo := s.costo + 100*float64(t.Retraso) + 0.1*float64(t.ViajeSeg)
	return estadoSim{fila: fila, hora: salida, costo: costo}, t
}

// simular recorre la secuencia; devuelve el costo (retraso ponderado + hora de terminar).
func simular(m Matriz, indice map[int64]int, s estadoSim, secuencia []paradaPlan, tramos []tramo) (estadoSim, []tramo) {
	for _, p := range secuencia {
		var t tramo
		s, t = paso(m, indice, s, p)
		if tramos != nil {
			tramos = append(tramos, t)
		}
	}
	return s, tramos
}

func costoFinal(s estadoSim, base time.Time) float64 {
	return s.costo + s.hora.Sub(base).Seconds()
}

// planificar arma la ruta con la matriz de los puntos de e (e.puntos()).
func planificar(e planEntrada, m Matriz) (plan, error) {
	_, indice, salida := e.puntos()
	s := estadoSim{fila: salida, hora: e.SaleEn}
	tramos := []tramo{}
	s, tramos = simular(m, indice, s, e.Fijas, tramos)
	if e.Ahora != nil && s.hora.Before(*e.Ahora) {
		s.hora = *e.Ahora
	}
	libres, err := ordenarLibres(e, m, indice, s)
	if err != nil {
		return plan{}, err
	}
	_, tramos = simular(m, indice, s, libres, tramos)
	p := plan{Tramos: tramos, SaleEn: e.SaleEn}
	if e.Salida != nil {
		p.Secuencia = append(p.Secuencia, *e.Salida)
	}
	for _, t := range tramos {
		p.Metros += t.Metros
		p.ViajeSeg += t.ViajeSeg
		p.EsperaSeg += t.EsperaSeg
		p.Retraso += t.Retraso
		if t.Parada.Punto != nil {
			p.Secuencia = append(p.Secuencia, *t.Parada.Punto)
		}
	}
	if n := len(tramos); n > 0 {
		p.TerminaEn = &tramos[n-1].Salida
	}
	return p, nil
}

// ordenarLibres: el orden manual (validado) o el de menor costo.
func ordenarLibres(e planEntrada, m Matriz, indice map[int64]int, s estadoSim) ([]paradaPlan, error) {
	if e.Manual != nil {
		porID := map[int64]paradaPlan{}
		for _, p := range e.Libres {
			porID[p.OrdenID] = p
		}
		fijas := map[int64]bool{}
		for _, p := range e.Fijas {
			fijas[p.OrdenID] = true
		}
		salida := make([]paradaPlan, 0, len(e.Libres))
		vistas := map[int64]bool{}
		for _, id := range e.Manual {
			if fijas[id] {
				continue // ya en camino, en sitio o terminada: va en el orden real
			}
			p, ok := porID[id]
			if !ok || vistas[id] {
				return nil, campoInvalido("ordenIds", fmt.Sprintf("La orden %d no es una parada pendiente de esta ruta (o está repetida)", id), "ordenIds")
			}
			vistas[id] = true
			salida = append(salida, p)
		}
		if len(salida) != len(e.Libres) {
			return nil, campoInvalido("ordenIds", "Faltan paradas: van todas las órdenes programadas del técnico ese día", "ordenIds")
		}
		return salida, nil
	}
	conPunto, sinPunto := []paradaPlan{}, []paradaPlan{}
	for _, p := range e.Libres {
		if p.Punto != nil {
			conPunto = append(conPunto, p)
		} else {
			sinPunto = append(sinPunto, p)
		}
	}
	return append(mejorOrden(conPunto, m, indice, s), sinPunto...), nil
}

// maxExacto: hasta este número de paradas se prueban todas las permutaciones (8! = 40 320).
const maxExacto = 8

// mejorOrden busca el orden de menor costo: exacto hasta maxExacto paradas; si no, varias
// semillas (por fin de ventana, por cita y vecino más cercano) mejoradas con reubicación y 2-opt.
func mejorOrden(ps []paradaPlan, m Matriz, indice map[int64]int, s estadoSim) []paradaPlan {
	if len(ps) <= 1 {
		return ps
	}
	costo := func(sec []paradaPlan) float64 {
		fin, _ := simular(m, indice, s, sec, nil)
		return costoFinal(fin, s.hora)
	}
	if len(ps) <= maxExacto {
		mejor, mejorCosto := slices.Clone(ps), costo(ps)
		permutar(slices.Clone(ps), func(sec []paradaPlan) {
			if c := costo(sec); c < mejorCosto-1e-9 {
				mejor, mejorCosto = slices.Clone(sec), c
			}
		})
		return mejor
	}
	semillas := [][]paradaPlan{porVentana(ps), slices.Clone(ps), vecinoMasCercano(ps, m, indice, s.fila)}
	var mejor []paradaPlan
	mejorCosto := math.Inf(1)
	for _, sem := range semillas {
		sec := mejorar(sem, costo)
		if c := costo(sec); c < mejorCosto-1e-9 {
			mejor, mejorCosto = sec, c
		}
	}
	return mejor
}

// permutar recorre todas las permutaciones (algoritmo de Heap).
func permutar(a []paradaPlan, visitar func([]paradaPlan)) {
	n := len(a)
	c := make([]int, n)
	visitar(a)
	for i := 0; i < n; {
		if c[i] < i {
			if i%2 == 0 {
				a[0], a[i] = a[i], a[0]
			} else {
				a[c[i]], a[i] = a[i], a[c[i]]
			}
			visitar(a)
			c[i]++
			i = 0
		} else {
			c[i] = 0
			i++
		}
	}
}

// porVentana: primero las que vencen antes (fin de ventana), luego las sin ventana por cita.
func porVentana(ps []paradaPlan) []paradaPlan {
	sec := slices.Clone(ps)
	slices.SortStableFunc(sec, func(a, b paradaPlan) int {
		switch {
		case a.VentanaFin != nil && b.VentanaFin != nil:
			return a.VentanaFin.Compare(*b.VentanaFin)
		case a.VentanaFin != nil:
			return -1
		case b.VentanaFin != nil:
			return 1
		}
		return a.CitaInicio.Compare(b.CitaInicio)
	})
	return sec
}

// vecinoMasCercano: siempre a la parada más cercana desde donde se está.
func vecinoMasCercano(ps []paradaPlan, m Matriz, indice map[int64]int, desde int) []paradaPlan {
	resto := slices.Clone(ps)
	sec := make([]paradaPlan, 0, len(ps))
	fila := desde
	for len(resto) > 0 {
		k := 0
		if fila >= 0 {
			for i := range resto {
				if m.Segundos[fila][indice[resto[i].OrdenID]] < m.Segundos[fila][indice[resto[k].OrdenID]] {
					k = i
				}
			}
		}
		sec = append(sec, resto[k])
		fila = indice[resto[k].OrdenID]
		resto = slices.Delete(resto, k, k+1)
	}
	return sec
}

// mejorar: búsqueda local (reubicar una parada y 2-opt) hasta que nada mejore.
func mejorar(sec []paradaPlan, costo func([]paradaPlan) float64) []paradaPlan {
	actual := slices.Clone(sec)
	c := costo(actual)
	for vuelta := 0; vuelta < 100; vuelta++ {
		mejoro := false
		n := len(actual)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i == j {
					continue
				}
				prueba := slices.Clone(actual)
				p := prueba[i]
				prueba = slices.Delete(prueba, i, i+1)
				prueba = slices.Insert(prueba, j, p)
				if pc := costo(prueba); pc < c-1e-9 {
					actual, c, mejoro = prueba, pc, true
				}
			}
		}
		for i := 0; i < n-1; i++ {
			for j := i + 1; j < n; j++ {
				prueba := slices.Clone(actual)
				slices.Reverse(prueba[i : j+1])
				if pc := costo(prueba); pc < c-1e-9 {
					actual, c, mejoro = prueba, pc, true
				}
			}
		}
		if !mejoro {
			break
		}
	}
	return actual
}
