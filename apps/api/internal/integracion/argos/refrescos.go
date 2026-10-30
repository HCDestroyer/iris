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
//     (singleflight). Solo el líder (el que llama a Argos) cobra cuota;
//   - toda lectura que llama a Argos cobra la cuota del usuario (CuotaUsuario por minuto), también
//     las de una instantánea vencida o inexistente;
//   - las lecturas a pedido cobran además una cuota propia por conector y por (conector, empresa),
//     separada según el uso: «Refrescar» (CuotaRefrescar*) y la verificación de altas de tickets y
//     órdenes (CuotaVerificar*, más holgada), para que gastar una no bloquee la otra ni a las
//     demás empresas;
//   - tras una caída de Argos al leer un cliente con instantánea (fallo_en), durante
//     EsperaTrasFallo no se vuelve a llamar; sin instantánea, un 404 o una caída quedan en una caché
//     negativa en memoria (NegativoTTL) que respetan la ficha, «Refrescar» y la verificación.
const (
	CuotaUsuario               = 30
	CuotaRefrescarConector     = 60
	CuotaRefrescarEmpresa      = 20
	CuotaVerificarConector     = 300
	CuotaVerificarEmpresa      = 100
	EsperaTrasFallo            = time.Minute
	NegativoTTL                = time.Minute
	maxEntradasEnMemoria       = 10000
	usoRefrescar           uso = "refrescar"
	usoVerificar           uso = "verificar"
)

// uso de una lectura a pedido ("" = lectura normal: solo la cuota del usuario).
type uso string

// errCuota: la lectura no se hizo porque se agotó una cuota.
var errCuota = errors.New("argos: cuota de lecturas agotada")

// cobro: a quién se cobra una lectura.
type cobro struct {
	uso     uso
	usuario int64 // 0 = sin usuario (sistema)
	empresa int64 // 0 = sin empresa activa
}

type ventanaCuota struct {
	inicio time.Time
	n      int
}

// negativo: Argos dijo 404 o no respondió para un cliente sin instantánea.
type negativo struct {
	noExiste bool
	hasta    time.Time
}

type claveCliente struct{ conector, cliente int64 }

type refrescos struct {
	vuelo     singleflight.Group
	mu        sync.Mutex
	cuotas    map[string]ventanaCuota
	negativos map[claveCliente]negativo
}

func nuevosRefrescos() *refrescos {
	return &refrescos{cuotas: map[string]ventanaCuota{}, negativos: map[claveCliente]negativo{}}
}

// tomarCuota descuenta una lectura de todas las cuotas que le tocan; false (sin descontar nada) si
// alguna ya se agotó en este minuto.
func (r *refrescos) tomarCuota(conectorID int64, c cobro, ahora time.Time) bool {
	type limite struct {
		clave string
		tope  int
	}
	con := strconv.FormatInt(conectorID, 10)
	var limites []limite
	if c.usuario != 0 {
		limites = append(limites, limite{"u:" + strconv.FormatInt(c.usuario, 10), CuotaUsuario})
	}
	switch c.uso {
	case usoRefrescar:
		limites = append(limites, limite{"r:" + con, CuotaRefrescarConector})
		if c.empresa != 0 {
			limites = append(limites, limite{"r:" + con + ":" + strconv.FormatInt(c.empresa, 10), CuotaRefrescarEmpresa})
		}
	case usoVerificar:
		limites = append(limites, limite{"v:" + con, CuotaVerificarConector})
		if c.empresa != 0 {
			limites = append(limites, limite{"v:" + con + ":" + strconv.FormatInt(c.empresa, 10), CuotaVerificarEmpresa})
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cuotas) > maxEntradasEnMemoria {
		for k, v := range r.cuotas { // se van las ventanas ya vencidas, nunca las vigentes
			if ahora.Sub(v.inicio) >= time.Minute {
				delete(r.cuotas, k)
			}
		}
	}
	ventanas := make([]ventanaCuota, len(limites))
	for i, l := range limites {
		v := r.cuotas[l.clave]
		if ahora.Sub(v.inicio) >= time.Minute {
			v = ventanaCuota{inicio: ahora}
		}
		if v.n >= l.tope {
			return false
		}
		ventanas[i] = v
	}
	for i, l := range limites {
		ventanas[i].n++
		r.cuotas[l.clave] = ventanas[i]
	}
	return true
}

// negativoDe devuelve el resultado negativo vigente de un cliente (ok = false si no hay).
func (r *refrescos) negativoDe(conectorID, clienteID int64, ahora time.Time) (negativo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.negativos[claveCliente{conectorID, clienteID}]
	if !ok || !ahora.Before(n.hasta) {
		delete(r.negativos, claveCliente{conectorID, clienteID})
		return negativo{}, false
	}
	return n, true
}

// recordarNegativo guarda un 404 (noExiste) o una caída de un cliente sin instantánea.
func (r *refrescos) recordarNegativo(conectorID, clienteID int64, noExiste bool, ahora time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.negativos) > maxEntradasEnMemoria {
		for k, v := range r.negativos {
			if !ahora.Before(v.hasta) {
				delete(r.negativos, k)
			}
		}
	}
	r.negativos[claveCliente{conectorID, clienteID}] = negativo{noExiste: noExiste, hasta: ahora.Add(NegativoTTL)}
}

// olvidarNegativo: una lectura buena borra el resultado negativo.
func (r *refrescos) olvidarNegativo(conectorID, clienteID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.negativos, claveCliente{conectorID, clienteID})
}

// leerUnaVez: una sola lectura de la ficha en curso por (conector, cliente, uso). El líder (el
// único que llama a Argos) cobra antes de llamar; agotada una cuota, todos reciben errCuota. La
// lectura corre sin la cancelación del primero que la pidió (los demás también la esperan); el
// cliente HTTP tiene su propio timeout. Un 404 o una caída quedan en la caché negativa si sinFila.
func (r *refrescos) leerUnaVez(ctx context.Context, conectorID, clienteID int64, c cobro, sinFila bool, leer func(context.Context) (fichaArgos, error)) (fichaArgos, error) {
	clave := strconv.FormatInt(conectorID, 10) + ":" + strconv.FormatInt(clienteID, 10) + ":" + string(c.uso)
	v, err, _ := r.vuelo.Do(clave, func() (any, error) {
		ahora := time.Now()
		if !r.tomarCuota(conectorID, c, ahora) {
			return fichaArgos{}, errCuota
		}
		f, err := leer(context.WithoutCancel(ctx))
		switch {
		case err == nil:
			r.olvidarNegativo(conectorID, clienteID)
		case EsNoEncontrado(err):
			r.recordarNegativo(conectorID, clienteID, true, ahora)
		case EsCaido(err) && sinFila:
			r.recordarNegativo(conectorID, clienteID, false, ahora)
		}
		return f, err
	})
	if err != nil {
		return fichaArgos{}, err
	}
	return v.(fichaArgos), nil
}
