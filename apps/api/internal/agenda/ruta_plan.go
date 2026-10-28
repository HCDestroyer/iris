package agenda

import (
	"context"
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

// huellaPlan resume toda la entrada del plan (puntos, horas, ventanas, paradas fijas, salida,
// orden manual): si es igual, el plan calculado fuera de la transacción sirve tal cual.
func (e planEntrada) huellaPlan() string {
	var b strings.Builder
	b.WriteString(e.huella())
	tm := func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return fmt.Sprint(t.UnixMicro())
	}
	fmt.Fprintf(&b, "|%s|%d|%s|%s|%v|", e.OrigenSalida, e.SaleEn.UnixMicro(), tm(e.Ahora), tm(e.FinTurno), e.Manual)
	for _, p := range slices.Concat(e.Fijas, e.Libres) {
		fmt.Fprintf(&b, "%d:%d:%s:%s:%d:%v:%s:%s;", p.OrdenID, p.Duracion, tm(p.VentanaIni), tm(p.VentanaFin),
			p.CitaInicio.UnixMicro(), p.Fija, tm(p.LlegadaReal), tm(p.SalidaReal))
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
func planificar(ctx context.Context, e planEntrada, m Matriz) (plan, error) {
	_, indice, salida := e.puntos()
	s := estadoSim{fila: salida, hora: e.SaleEn}
	tramos := []tramo{}
	s, tramos = simular(m, indice, s, e.Fijas, tramos)
	if e.Ahora != nil && s.hora.Before(*e.Ahora) {
		s.hora = *e.Ahora
	}
	libres, err := ordenarLibres(ctx, e, m, indice, s)
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
func ordenarLibres(ctx context.Context, e planEntrada, m Matriz, indice map[int64]int, s estadoSim) ([]paradaPlan, error) {
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
	orden, err := mejorOrden(ctx, conPunto, m, indice, s)
	if err != nil {
		return nil, err
	}
	return append(orden, sinPunto...), nil
}

// maxExacto: hasta este número de paradas se prueban todas las permutaciones (8! = 40 320).
const maxExacto = 8

// Límites de la búsqueda local (con 100 paradas, unos cientos de ms como mucho): presupuesto de
// tiempo para todas las semillas, vueltas por semilla y, por encima de soloDosOpt paradas, solo
// 2-opt (la reubicación es la parte cara). Agotado el presupuesto queda el mejor orden hallado.
const (
	presupuestoBusqueda = 250 * time.Millisecond
	soloDosOpt          = 30
)

// mejorOrden busca el orden de menor costo: exacto hasta maxExacto paradas; si no, semillas (por
// fin de ventana, por cita y vecino más cercano) mejoradas con búsqueda local acotada. Devuelve
// ctx.Err() si la petición se canceló.
func mejorOrden(ctx context.Context, ps []paradaPlan, m Matriz, indice map[int64]int, s estadoSim) ([]paradaPlan, error) {
	if len(ps) <= 1 {
		return ps, nil
	}
	costo := func(sec []paradaPlan) float64 {
		fin, _ := simular(m, indice, s, sec, nil)
		return costoFinal(fin, s.hora)
	}
	if len(ps) <= maxExacto {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mejor, mejorCosto := slices.Clone(ps), costo(ps)
		permutar(slices.Clone(ps), func(sec []paradaPlan) {
			if c := costo(sec); c < mejorCosto-1e-9 {
				mejor, mejorCosto = slices.Clone(sec), c
			}
		})
		return mejor, nil
	}
	limite := time.Now().Add(presupuestoBusqueda)
	semillas := [][]paradaPlan{porVentana(ps), vecinoMasCercano(ps, m, indice, s.fila)}
	if len(ps) <= soloDosOpt {
		semillas = append(semillas, slices.Clone(ps))
	}
	var mejor []paradaPlan
	mejorCosto := math.Inf(1)
	for _, sem := range semillas {
		sec, err := mejorar(ctx, sem, costo, limite)
		if err != nil {
			return nil, err
		}
		if c := costo(sec); c < mejorCosto-1e-9 {
			mejor, mejorCosto = sec, c
		}
	}
	return mejor, nil
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

// mejorar: búsqueda local (reubicar una parada, hasta soloDosOpt paradas, y 2-opt) hasta que
// nada mejore, maxVueltas o el límite de tiempo. Revisa ctx en cada fila.
func mejorar(ctx context.Context, sec []paradaPlan, costo func([]paradaPlan) float64, limite time.Time) ([]paradaPlan, error) {
	actual := slices.Clone(sec)
	n := len(actual)
	c := costo(actual)
	prueba := make([]paradaPlan, n)
	maxVueltas := 20
	if n > soloDosOpt {
		maxVueltas = 4
	}
	agotado := func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		return time.Now().After(limite), nil
	}
	for vuelta := 0; vuelta < maxVueltas; vuelta++ {
		mejoro := false
		if n <= soloDosOpt {
			for i := 0; i < n; i++ {
				if fin, err := agotado(); fin {
					return actual, err
				}
				for j := 0; j < n; j++ {
					if i == j {
						continue
					}
					copy(prueba, actual)
					p := prueba[i]
					if i < j {
						copy(prueba[i:j], prueba[i+1:j+1])
					} else {
						copy(prueba[j+1:i+1], prueba[j:i])
					}
					prueba[j] = p
					if pc := costo(prueba); pc < c-1e-9 {
						copy(actual, prueba)
						c, mejoro = pc, true
					}
				}
			}
		}
		for i := 0; i < n-1; i++ {
			if fin, err := agotado(); fin {
				return actual, err
			}
			for j := i + 1; j < n; j++ {
				copy(prueba, actual)
				slices.Reverse(prueba[i : j+1])
				if pc := costo(prueba); pc < c-1e-9 {
					copy(actual, prueba)
					c, mejoro = pc, true
				}
			}
		}
		if !mejoro {
			break
		}
	}
	return actual, nil
}
