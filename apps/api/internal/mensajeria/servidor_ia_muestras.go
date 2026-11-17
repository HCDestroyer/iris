package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
)

// Muestras de /metricas, series del tablero y episodios de aviso (contratos/voz.md §4.12).

// vidaLecturaIA: una muestra más vieja que esto ya no es «de ahora».
const vidaLecturaIA = 45 * time.Second

// muestraGuardada es lo que queda en servidor_ia_muestras.datos: la lectura ya validada.
type muestraGuardada struct {
	EncendidoSeg int64           `json:"encendidoSeg"`
	Servidor     ServidorTablero `json:"servidor"`
}

func enRango(v *float64, lo, hi float64) bool { return v == nil || (*v >= lo && *v <= hi) }

func ptrF32(v float64) *float32 {
	f := float32(v)
	return &f
}

// muestraDe revalida TODO lo que devuelve /metricas (el servidor no es de confianza): esquema,
// longitudes, nombres y rangos. Si algo no cuadra se descarta la lectura entera (cuenta como
// fallida): nunca se guarda ni se muestra a medias.
func muestraDe(mt voz.Metricas) (muestraGuardada, mensajeriabd.InsertarMuestraIAParams, bool) {
	var p mensajeriabd.InsertarMuestraIAParams
	g := muestraGuardada{EncendidoSeg: mt.EncendidoSeg, Servidor: ServidorTablero{GPUs: GPUsTablero{Estado: "omitido", Lista: []GPUTablero{}}, Modelos: []ModeloTablero{}}}
	megas := func(v int64) bool { return v >= 0 && v <= 1<<32 }
	if mt.EncendidoSeg < 0 || mt.EncendidoSeg > 100*365*86400 || len(mt.GPUs.Lista) > 64 || len(mt.Modelos) > 256 ||
		mt.CPU.Nucleos < 0 || mt.CPU.Nucleos > 4096 || !enRango(mt.CPU.UsoPct, 0, 100) ||
		!megas(mt.Memoria.UsadoMb) || !megas(mt.Memoria.TotalMb) || mt.Memoria.UsadoMb > mt.Memoria.TotalMb ||
		!megas(mt.Disco.UsadoMb) || !megas(mt.Disco.TotalMb) || mt.Disco.UsadoMb > mt.Disco.TotalMb ||
		mt.LLM.EnCurso < 0 || mt.LLM.EnCurso > 1024 || mt.LLM.MaxSimultaneas < 0 || mt.LLM.MaxSimultaneas > 64 {
		return g, p, false
	}
	s := &g.Servidor
	s.CPU = CPUTablero{Nucleos: mt.CPU.Nucleos, UsoPct: mt.CPU.UsoPct}
	s.Memoria = UsoRecurso{UsadoMb: mt.Memoria.UsadoMb, TotalMb: mt.Memoria.TotalMb}
	if mt.Disco.TotalMb > 0 {
		s.Disco = &DiscoTablero{UsadoMb: mt.Disco.UsadoMb, TotalMb: mt.Disco.TotalMb}
		p.DiscoPct = ptrF32(100 * float64(mt.Disco.UsadoMb) / float64(mt.Disco.TotalMb))
	}
	s.LLM = OcupacionIA{EnCurso: int64(mt.LLM.EnCurso), MaxSimultaneas: int32(mt.LLM.MaxSimultaneas)} //nolint:gosec // acotado arriba
	enCurso := int32(mt.LLM.EnCurso)                                                                  //nolint:gosec // acotado arriba
	p.EnCurso = &enCurso
	var usada, total, uso, temp float64
	var conUso, conTemp int
	for _, x := range mt.GPUs.Lista {
		if x.Indice < 0 || x.Indice > 1024 || !voz.ReNombreGPU.MatchString(x.Nombre) || !enRango(x.UsoPct, 0, 100) ||
			!enRango(x.VRAMUsadaMb, 0, 1<<22) || !enRango(x.VRAMTotalMb, 0, 1<<22) || !enRango(x.TemperaturaC, 0, 150) || !enRango(x.PotenciaW, 0, 5000) {
			return g, p, false
		}
		s.GPUs.Lista = append(s.GPUs.Lista, GPUTablero{Indice: x.Indice, Nombre: x.Nombre, UsoPct: x.UsoPct, VRAMUsadaMb: x.VRAMUsadaMb,
			VRAMTotalMb: x.VRAMTotalMb, TemperaturaC: x.TemperaturaC, PotenciaW: x.PotenciaW})
		if x.VRAMUsadaMb != nil && x.VRAMTotalMb != nil {
			usada, total = usada+*x.VRAMUsadaMb, total+*x.VRAMTotalMb
		}
		if x.UsoPct != nil {
			uso, conUso = max(uso, *x.UsoPct), conUso+1
		}
		if x.TemperaturaC != nil {
			temp, conTemp = max(temp, *x.TemperaturaC), conTemp+1
		}
	}
	if len(s.GPUs.Lista) > 0 {
		s.GPUs.Estado = "ok"
		if total > 0 && usada <= total {
			p.VramPct = ptrF32(100 * usada / total)
		}
		if conUso > 0 {
			p.UsoPct = ptrF32(uso)
		}
		if conTemp > 0 {
			p.TemperaturaC = ptrF32(temp)
		}
	}
	for _, x := range mt.Modelos {
		if !voz.ReNombreModelo.MatchString(x.ID) || x.TamanoMb < 0 || x.TamanoMb > 1<<24 || x.VRAMMb < 0 || x.VRAMMb > 1<<22 {
			return g, p, false
		}
		s.Modelos = append(s.Modelos, ModeloTablero{ID: x.ID, TamanoMb: x.TamanoMb, Permitido: x.Permitido, Cargado: x.Cargado, VRAMMb: x.VRAMMb, CargadoHasta: x.CargadoHasta})
	}
	return g, p, true
}

// guardarMuestra deja la muestra de este latido (buena o fallida) y abre o cierra los episodios de
// aviso. Corre dentro de la transacción del latido.
func (m *Modulo) guardarMuestra(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaVozMotor, mt *voz.Metricas) error {
	ahora := m.ahora()
	p := mensajeriabd.InsertarMuestraIAParams{MotorID: f.ID, TomadaEn: ahora}
	if mt != nil {
		if g, escalares, ok := muestraDe(*mt); ok {
			datos, err := json.Marshal(g)
			if err != nil {
				return err
			}
			p = escalares
			p.MotorID, p.TomadaEn, p.Ok, p.Datos = f.ID, ahora, true, datos
		}
	}
	if err := q.InsertarMuestraIA(ctx, p); err != nil {
		return err
	}
	// Episodios: se abren una vez y se cierran solos al volver a la normalidad.
	episodio := func(tipo string, activo bool, valor, umbral float64, conValor bool) error {
		if !activo {
			_, err := q.CerrarAvisoIA(ctx, mensajeriabd.CerrarAvisoIAParams{MotorID: f.ID, Tipo: tipo, Hasta: &ahora})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil || tipo == "caido" || tipo == "incompatible" {
				return err
			}
			return t.EmitirEnEmpresa("servidor_ia.umbral_normalizado", nil, map[string]any{"motor_id": f.ID, "tipo": tipo, "valor": valor, "umbral": umbral})
		}
		ab := mensajeriabd.AbrirAvisoIAParams{MotorID: f.ID, Tipo: tipo, Desde: ahora}
		if conValor {
			ab.Valor, ab.Umbral = ptrF32(valor), ptrF32(umbral)
		}
		_, err := q.AbrirAvisoIA(ctx, ab)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // ya estaba abierto
		}
		if err != nil || !conValor {
			return err // caído e incompatible avisan con voz.motor_estado_cambiado
		}
		return t.EmitirEnEmpresa("servidor_ia.umbral_superado", nil, map[string]any{"motor_id": f.ID, "tipo": tipo, "valor": valor, "umbral": umbral})
	}
	if err := episodio("caido", f.Estado == MotorNoDisponible, 0, 0, false); err != nil {
		return err
	}
	if err := episodio("incompatible", f.Estado == MotorIncompatible, 0, 0, false); err != nil {
		return err
	}
	if !p.Ok {
		return nil // sin lectura no se decide nada de los umbrales (ni se abren ni se cierran)
	}
	// «Durante N minutos»: toda la ventana por encima, con al menos la mitad de las muestras esperadas.
	sostenido := func(minutos int32) (mensajeriabd.VentanaMuestrasIARow, int64, error) {
		v, err := q.VentanaMuestrasIA(ctx, mensajeriabd.VentanaMuestrasIAParams{MotorID: f.ID, Desde: ahora.Add(-time.Duration(minutos) * time.Minute)})
		return v, int64(minutos) * 3, err
	}
	v, minimo, err := sostenido(f.VramMinutos)
	if err != nil {
		return err
	}
	if p.VramPct != nil {
		if err := episodio("vram", v.ConVram >= minimo && v.VramMin >= float64(f.VramPct), float64(*p.VramPct), float64(f.VramPct), true); err != nil {
			return err
		}
	}
	if v, minimo, err = sostenido(f.TemperaturaMinutos); err != nil {
		return err
	}
	if p.TemperaturaC != nil {
		if err := episodio("temperatura", v.ConTemperatura >= minimo && v.TemperaturaMin >= float64(f.TemperaturaC), float64(*p.TemperaturaC), float64(f.TemperaturaC), true); err != nil {
			return err
		}
	}
	if p.DiscoPct != nil {
		if err := episodio("disco", float64(*p.DiscoPct) >= float64(f.DiscoPct), float64(*p.DiscoPct), float64(f.DiscoPct), true); err != nil {
			return err
		}
	}
	rechazos, err := q.RechazosIADesde(ctx, mensajeriabd.RechazosIADesdeParams{MotorID: f.ID, Desde: ahora.Add(-time.Duration(f.SaturacionMinutos) * time.Minute).Truncate(time.Minute)})
	if err != nil {
		return err
	}
	return episodio("saturacion", rechazos >= int64(f.SaturacionRechazos), float64(rechazos), float64(f.SaturacionRechazos), true)
}

// mantenerMuestras agrega por hora y aplica la retención (cada pocos minutos basta).
func (m *Modulo) mantenerMuestras(ctx context.Context, q *mensajeriabd.Queries) error {
	ahora := m.ahora()
	if ahora.Minute()%5 != 0 || ahora.Second() >= 10 {
		return nil
	}
	hora := ahora.Truncate(time.Hour)
	if err := q.AgregarMuestrasHoraIA(ctx, mensajeriabd.AgregarMuestrasHoraIAParams{Desde: hora.Add(-2 * time.Hour), Hasta: hora.Add(time.Hour)}); err != nil {
		return err
	}
	for _, borrar := range []func(context.Context, time.Time) (int64, error){q.BorrarMuestrasIA, q.BorrarMuestrasHoraIA, q.BorrarUsoIA, q.BorrarAvisosIA} {
		if _, err := borrar(ctx, ahora); err != nil {
			return err
		}
	}
	return nil
}

// p95DeCubetas: el límite superior de la cubeta donde cae el percentil 95.
func p95DeCubetas(cubetas []int64) *float64 {
	var total int64
	for _, c := range cubetas {
		total += c
	}
	if total == 0 {
		return nil
	}
	objetivo, suma := (total*95+99)/100, int64(0)
	for i, c := range cubetas {
		suma += c
		if suma >= objetivo {
			v := float64(cubetasLatencia[min(i, len(cubetasLatencia)-1)])
			return &v
		}
	}
	return nil
}

// seriesIA arma los puntos del rango: uso de GPU y VRAM de las muestras (solo para quien ve todo) y
// peticiones y tiempos del uso (de todas las empresas o de una). Un valor null = sin dato.
func (m *Modulo) seriesIA(ctx context.Context, q *mensajeriabd.Queries, motor int64, rango string, empresa int64, conServidor bool) ([]PuntoSerieIA, time.Time, error) {
	ahora := m.ahora()
	paso, desde := int64(60), ahora.Add(-time.Hour)
	switch rango {
	case "ahora":
		paso, desde = 10, ahora.Add(-10*time.Minute)
	case "24h":
		desde = ahora.Add(-24 * time.Hour)
	case "7d":
		paso, desde = 3600, ahora.Add(-7*24*time.Hour)
	}
	desde = time.Unix(desde.Unix()/paso*paso, 0).UTC()
	type gpu struct{ uso, vram *float64 }
	lecturas := map[int64]gpu{}
	if conServidor && paso < 3600 {
		filas, err := q.SerieMuestrasIA(ctx, mensajeriabd.SerieMuestrasIAParams{MotorID: motor, Desde: desde, Paso: int32(paso)}) //nolint:gosec // 10 o 60
		if err != nil {
			return nil, desde, err
		}
		for _, f := range filas {
			var g gpu
			if f.ConUso > 0 {
				g.uso = &f.UsoPct
			}
			if f.ConVram > 0 {
				g.vram = &f.VramPct
			}
			lecturas[f.T] = g
		}
	} else if conServidor {
		filas, err := q.SerieMuestrasHoraIA(ctx, mensajeriabd.SerieMuestrasHoraIAParams{MotorID: motor, Desde: desde})
		if err != nil {
			return nil, desde, err
		}
		for _, f := range filas {
			var g gpu
			if f.UsoPct != nil {
				v := float64(*f.UsoPct)
				g.uso = &v
			}
			if f.VramPct != nil {
				v := float64(*f.VramPct)
				g.vram = &v
			}
			lecturas[f.Hora.Unix()] = g
		}
	}
	// El uso se guarda por minuto: en «ahora» (paso de 10 s) cada punto toma el de su minuto.
	pasoUso := max(paso, 60)
	type uso struct {
		peticiones, latencia int64
		cubetas              []int64
	}
	usos := map[int64]*uso{}
	filas, err := q.UsoIAFilas(ctx, mensajeriabd.UsoIAFilasParams{MotorID: motor, Desde: time.Unix(desde.Unix()/pasoUso*pasoUso, 0).UTC(), EmpresaID: empresa})
	if err != nil {
		return nil, desde, err
	}
	for _, f := range filas {
		k := f.Hora.Unix() / pasoUso * pasoUso
		u := usos[k]
		if u == nil {
			u = &uso{cubetas: make([]int64, len(cubetasLatencia)+1)}
			usos[k] = u
		}
		u.peticiones, u.latencia = u.peticiones+f.Peticiones, u.latencia+f.LatenciaMsSuma
		for i, c := range f.LatenciaCubetas {
			if i < len(u.cubetas) {
				u.cubetas[i] += c
			}
		}
	}
	puntos := []PuntoSerieIA{}
	for t := desde.Unix(); t <= ahora.Unix(); t += paso {
		p := PuntoSerieIA{T: time.Unix(t, 0).UTC()}
		if g, ok := lecturas[t]; ok {
			p.UsoPct, p.VRAMPct = g.uso, g.vram
		}
		if u := usos[t/pasoUso*pasoUso]; u != nil {
			porMin := float64(u.peticiones) / (float64(pasoUso) / 60)
			p.PeticionesMin = &porMin
			if u.peticiones > 0 {
				media := float64(u.latencia) / float64(u.peticiones)
				p.LatenciaMediaMs, p.LatenciaP95Ms = &media, p95DeCubetas(u.cubetas)
			}
		}
		puntos = append(puntos, p)
	}
	return puntos, desde, nil
}
