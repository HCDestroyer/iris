package argos

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Verificación en Argos de los clientes que devuelve la búsqueda y que Iris no conoce: se leen sus
// servicios (GET /servicios?cliente_id=&empresas=todas) para saber sus empresas. Para no
// amplificar una búsqueda en muchas llamadas a Argos: a lo sumo TopeVerificaciones por búsqueda
// (los demás quedan fuera), CuposPorConector a la vez por conector (en todo el proceso) y una
// caché de VigenciaVerificacion por cliente.
const (
	TopeVerificaciones   = 20
	CuposPorConector     = 10
	VigenciaVerificacion = 5 * time.Minute
	// TopeServiciosBusqueda: con ?servicios=true, a lo sumo estos clientes por búsqueda leen sus
	// servicios (los demás quedan con servicios null); el resto sale de la caché.
	TopeServiciosBusqueda = 8
)

// verificada: lo leído de un cliente en Argos (empresas y servicios), en caché por
// VigenciaVerificacion con la misma clave (conector, cliente).
type verificada struct {
	leido leidoEnArgos
	vence time.Time
}

type verificaciones struct {
	mu    sync.Mutex
	cache map[[2]int64]verificada // conector, cliente
	cupos map[int64]chan struct{} // por conector
}

func nuevasVerificaciones() *verificaciones {
	return &verificaciones{cache: map[[2]int64]verificada{}, cupos: map[int64]chan struct{}{}}
}

func (v *verificaciones) leer(conectorID, clienteID int64) (infoEmpresas, bool) {
	l, ok := v.leerTodo(conectorID, clienteID)
	return l.info, ok
}

// leerTodo: empresas y servicios en caché del cliente, si no vencieron.
func (v *verificaciones) leerTodo(conectorID, clienteID int64) (leidoEnArgos, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	r, ok := v.cache[[2]int64{conectorID, clienteID}]
	if !ok || time.Now().After(r.vence) {
		delete(v.cache, [2]int64{conectorID, clienteID})
		return leidoEnArgos{}, false
	}
	return r.leido, true
}

func (v *verificaciones) guardar(conectorID, clienteID int64, l leidoEnArgos) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.cache) > 10000 { // tope de memoria: se vacía (vuelve a verificar)
		v.cache = map[[2]int64]verificada{}
	}
	v.cache[[2]int64{conectorID, clienteID}] = verificada{leido: l, vence: time.Now().Add(VigenciaVerificacion)}
}

func (v *verificaciones) cupo(conectorID int64) chan struct{} {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.cupos[conectorID]
	if !ok {
		c = make(chan struct{}, CuposPorConector)
		v.cupos[conectorID] = c
	}
	return c
}

// leidoEnArgos: los servicios de un cliente leídos en Argos y las empresas que se deducen.
type leidoEnArgos struct {
	info      infoEmpresas
	servicios []ServicioArgos
	// leidoEn: cuándo se pidieron a Argos (los de la caché conservan su hora).
	leidoEn time.Time
}

// verificarEnArgos lee las empresas de hasta TopeVerificaciones clientes en Argos. Falla cerrado:
// un error de Argos (o el tope) deja al cliente fuera del resultado (no aparece en el mapa).
func (m *Modulo) verificarEnArgos(ctx context.Context, conectorID int64, cli *Cliente, clientes []ClienteArgos) map[int64]infoEmpresas {
	leidos := m.leerServiciosEnArgos(ctx, conectorID, cli, clientes)
	salida := make(map[int64]infoEmpresas, len(leidos))
	for id, l := range leidos {
		salida[id] = l.info
	}
	return salida
}

// leerServiciosEnArgos lee los servicios (y con ellos las empresas) de los clientes: los que están
// en la caché (VigenciaVerificacion) salen de ella y solo los demás se piden a Argos, a lo sumo
// TopeVerificaciones y con los cupos por conector. Cada lectura respeta la protección de Argos de
// refrescos.go: se cobra a la cuota de verificación (usoVerificar: la del usuario y la del
// conector y la empresa) y no se pide un cliente en la caché negativa o en espera tras una caída
// (fallo_en). Un cliente que no se lee (cuota, espera o error) no queda en el mapa.
func (m *Modulo) leerServiciosEnArgos(ctx context.Context, conectorID int64, cli *Cliente, clientes []ClienteArgos) map[int64]leidoEnArgos {
	salida := make(map[int64]leidoEnArgos, len(clientes))
	pendientes := make([]ClienteArgos, 0, len(clientes))
	for _, c := range clientes {
		if l, ok := m.verificacion.leerTodo(conectorID, c.ID); ok {
			salida[c.ID] = l
		} else {
			pendientes = append(pendientes, c)
		}
	}
	if len(pendientes) > TopeVerificaciones {
		pendientes = pendientes[:TopeVerificaciones]
	}
	pendientes, conFila := m.lecturasPermitidas(ctx, conectorID, pendientes)
	var mu sync.Mutex
	var wg sync.WaitGroup
	cupo := m.verificacion.cupo(conectorID)
	for _, c := range pendientes {
		wg.Add(1)
		go func(c ClienteArgos) {
			defer wg.Done()
			select {
			case cupo <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-cupo }()
			leidoEn := time.Now().UTC()
			servicios, completa, err := cli.ServiciosDeClienteCompletos(ctx, c.ID)
			switch {
			case err == nil:
				m.refrescos.olvidarNegativo(conectorID, c.ID)
			case EsNoEncontrado(err):
				m.refrescos.recordarNegativo(conectorID, c.ID, true, leidoEn)
				return
			case EsCaido(err) && !conFila[c.ID]:
				m.refrescos.recordarNegativo(conectorID, c.ID, false, leidoEn)
				return
			default:
				return
			}
			info, err := infoDeServicios(ctx, c, servicios, completa)
			if err != nil {
				return
			}
			l := leidoEnArgos{info: info, servicios: servicios, leidoEn: leidoEn}
			m.verificacion.guardar(conectorID, c.ID, l)
			mu.Lock()
			salida[c.ID] = l
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	return salida
}

// lecturasPermitidas: de los clientes por leer, los que se pueden pedir a Argos ahora (sin
// resultado negativo vigente, sin fallo_en reciente y con cuota de verificación, que se cobra
// aquí), y cuáles tienen instantánea (para recordar una caída solo de los que no la tienen).
func (m *Modulo) lecturasPermitidas(ctx context.Context, conectorID int64, clientes []ClienteArgos) ([]ClienteArgos, map[int64]bool) {
	conFila := make(map[int64]bool, len(clientes))
	bd := nucleo.BDDe(ctx)
	if bd == nil || len(clientes) == 0 {
		return nil, conFila
	}
	q := argosbd.New(bd)
	ahora := time.Now()
	cob := cobro{uso: usoVerificar, usuario: contexto.ActorDe(ctx).ID, empresa: contexto.EmpresaDe(ctx)}
	salida := make([]ClienteArgos, 0, len(clientes))
	for _, c := range clientes {
		var fila *argosbd.IntegracionArgosCliente
		f, err := q.ClientePorID(ctx, c.ID)
		switch {
		case err == nil:
			fila = &f
			conFila[c.ID] = true
		case !errors.Is(err, pgx.ErrNoRows):
			continue
		}
		if enEspera(fila, ahora) {
			continue
		}
		if _, negativo, err := m.refrescos.vigenteNegativo(ctx, q, conectorID, c.ID, ahora); err != nil || negativo {
			continue
		}
		if !m.refrescos.tomarCuota(conectorID, cob, ahora) {
			// Agotada: ni este ni los siguientes (fallan cerrado: sin servicios ni verificación).
			break
		}
		salida = append(salida, c)
	}
	return salida, conFila
}

// infoDeServicios: las empresas de los servicios y, con la lista completa vacía, la pista de un
// alta desde Iris (empresasYPista) o «sin servicios».
func infoDeServicios(ctx context.Context, c ClienteArgos, servicios []ServicioArgos, completa bool) (infoEmpresas, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return infoEmpresas{}, nucleo.ErrSinBD
	}
	empresas, sin, err := empresasYPista(ctx, argosbd.New(bd), c, servicios, completa)
	return infoEmpresas{empresas: empresas, sinServicios: sin}, err
}
