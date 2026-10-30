package argos

import (
	"context"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Protección de Argos frente a lecturas repetidas de la ficha (revisión de seguridad, PR #70):
//
//   - una sola lectura en curso por (conector, cliente): las demás peticiones esperan su resultado
//     (singleflight);
//   - «Refrescar» (?refrescar=true) tiene cuota por conector: CuotaRefrescos por minuto en el
//     proceso; pasada la cuota, la ficha se lee como sin refrescar;
//   - tras una caída de Argos al leer un cliente (fallo_en en la instantánea), durante
//     EsperaTrasFallo no se vuelve a llamar: se sirve la instantánea marcada stale.
const (
	CuotaRefrescos  = 60
	EsperaTrasFallo = time.Minute
)

type refrescos struct {
	vuelo singleflight.Group
	mu    sync.Mutex
	// ventana de la cuota por conector: inicio y cuántos van.
	cuota map[int64]ventanaCuota
}

type ventanaCuota struct {
	inicio time.Time
	n      int
}

func nuevosRefrescos() *refrescos { return &refrescos{cuota: map[int64]ventanaCuota{}} }

// tomarCuota descuenta un «Refrescar» del conector; false si ya se agotó en este minuto.
func (r *refrescos) tomarCuota(conectorID int64, ahora time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.cuota[conectorID]
	if ahora.Sub(v.inicio) >= time.Minute {
		v = ventanaCuota{inicio: ahora}
	}
	if v.n >= CuotaRefrescos {
		return false
	}
	v.n++
	r.cuota[conectorID] = v
	return true
}

// leerUnaVez: una sola lectura de la ficha en curso por (conector, cliente). La lectura corre sin
// la cancelación del primero que la pidió (los demás también la esperan); el cliente HTTP tiene su
// propio timeout.
func (r *refrescos) leerUnaVez(ctx context.Context, conectorID, clienteID int64, leer func(context.Context) (fichaArgos, error)) (fichaArgos, error) {
	clave := strconv.FormatInt(conectorID, 10) + ":" + strconv.FormatInt(clienteID, 10)
	v, err, _ := r.vuelo.Do(clave, func() (any, error) {
		return leer(context.WithoutCancel(ctx))
	})
	if err != nil {
		return fichaArgos{}, err
	}
	return v.(fichaArgos), nil
}
