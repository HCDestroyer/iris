package argos

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Protección de Argos frente a lecturas repetidas de la ficha (revisión de seguridad, PR #70):
//
//   - una sola lectura en curso por (conector, cliente): las demás peticiones esperan su resultado
//     (singleflight);
//   - las lecturas a pedido («Refrescar» y la verificación antes de crear un ticket u orden) tienen
//     cuota por conector (CuotaConector por minuto, compartida por todas las empresas) y por usuario
//     (CuotaUsuario por minuto). Solo se cobra cuando de verdad se llama a Argos, y solo el líder del
//     singleflight; pasada la cuota, no se llama (errCuota);
//   - tras una caída de Argos al leer un cliente (fallo_en en la instantánea), durante
//     EsperaTrasFallo no se vuelve a llamar.
const (
	CuotaConector   = 60
	CuotaUsuario    = 10
	EsperaTrasFallo = time.Minute
)

// errCuota: la lectura a pedido no se hizo porque se agotó la cuota.
var errCuota = errors.New("argos: cuota de lecturas a pedido agotada")

type refrescos struct {
	vuelo    singleflight.Group
	mu       sync.Mutex
	conector map[int64]ventanaCuota
	usuario  map[int64]ventanaCuota
}

type ventanaCuota struct {
	inicio time.Time
	n      int
}

func nuevosRefrescos() *refrescos {
	return &refrescos{conector: map[int64]ventanaCuota{}, usuario: map[int64]ventanaCuota{}}
}

func vigente(v ventanaCuota, ahora time.Time) ventanaCuota {
	if ahora.Sub(v.inicio) >= time.Minute {
		return ventanaCuota{inicio: ahora}
	}
	return v
}

// tomarCuota descuenta una lectura a pedido del conector y del usuario (0 = sin usuario: solo el
// conector); false si alguna de las dos ya se agotó en este minuto (entonces no descuenta nada).
func (r *refrescos) tomarCuota(conectorID, usuarioID int64, ahora time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := vigente(r.conector[conectorID], ahora)
	u := vigente(r.usuario[usuarioID], ahora)
	if c.n >= CuotaConector || (usuarioID != 0 && u.n >= CuotaUsuario) {
		return false
	}
	c.n++
	r.conector[conectorID] = c
	if usuarioID != 0 {
		if len(r.usuario) > 10000 { // tope de memoria
			r.usuario = map[int64]ventanaCuota{}
		}
		u.n++
		r.usuario[usuarioID] = u
	}
	return true
}

// leerUnaVez: una sola lectura de la ficha en curso por (conector, cliente). Con cobrar, el líder
// (el único que llama a Argos) descuenta la cuota antes de llamar; agotada, todos reciben
// errCuota. La lectura corre sin la cancelación del primero que la pidió (los demás también la
// esperan); el cliente HTTP tiene su propio timeout.
func (r *refrescos) leerUnaVez(ctx context.Context, conectorID, clienteID, usuarioID int64, cobrar bool, leer func(context.Context) (fichaArgos, error)) (fichaArgos, error) {
	clave := strconv.FormatInt(conectorID, 10) + ":" + strconv.FormatInt(clienteID, 10)
	if cobrar {
		clave += ":a_pedido"
	}
	v, err, _ := r.vuelo.Do(clave, func() (any, error) {
		if cobrar && !r.tomarCuota(conectorID, usuarioID, time.Now()) {
			return fichaArgos{}, errCuota
		}
		return leer(context.WithoutCancel(ctx))
	})
	if err != nil {
		return fichaArgos{}, err
	}
	return v.(fichaArgos), nil
}
