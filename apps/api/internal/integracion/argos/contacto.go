package argos

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// ContactoArgos es lo que la mensajería (contrato 0.6.0 §6.2) necesita de un cliente de Argos: sus
// teléfonos y correo de la instantánea (§2.2), sus empresas (las de sus servicios) y la empresa de
// cada servicio. Nunca viene de quien pide el aviso: así ningún aviso sale a otro contacto.
type ContactoArgos struct {
	// Existe: hay instantánea del cliente (leída o refrescada).
	Existe       bool
	Empresas     []int64
	SinServicios bool
	Telefono     string
	Movil        string
	Correo       string
	// Servicios: id del servicio → empresa.
	Servicios map[int64]int64
	// Stale: Argos no respondió y se usó la instantánea vencida.
	Stale          bool
	SincronizadoEn time.Time
}

// EnEmpresa indica si el cliente es de la empresa (alguno de sus servicios lo es, o no tiene
// servicios), con la misma regla que las lecturas del panel.
func (c ContactoArgos) EnEmpresa(empresa int64) bool {
	return enEmpresa(c.Empresas, c.SinServicios, empresa)
}

// ContactoCliente devuelve el contacto del cliente desde la instantánea y la refresca de Argos si
// no existe o tiene más de VigenciaInstantanea (24 h, §2.2). Sin conector activo se usa lo que haya
// (Existe = false si no hay nada). Si Argos no responde se usa la instantánea vencida (Stale) y, sin
// ninguna, 503 argos_no_disponible. Un cliente que Argos ya no tiene (404) borra la instantánea y
// vuelve con Existe = false. La lectura pasa por la cuota y la protección de la ficha y se audita
// (clientes.sincronizar) con el actor del contexto.
func (m *Modulo) ContactoCliente(ctx context.Context, clienteID int64) (ContactoArgos, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ContactoArgos{}, nucleo.ErrSinBD
	}
	q := argosbd.New(bd)
	var previa *argosbd.IntegracionArgosCliente
	if f, err := q.ClientePorID(ctx, clienteID); err == nil {
		previa = &f
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ContactoArgos{}, err
	}
	if previa != nil && !debeRefrescar(previa, false, m.cfg.VigenciaInstantanea) {
		return contactoDe(ctx, q, *previa, false)
	}
	c, err := m.conectorActivo(ctx, bd)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
		if previa == nil {
			return ContactoArgos{}, nil
		}
		return contactoDe(ctx, q, *previa, true)
	}
	if err != nil {
		return ContactoArgos{}, err
	}
	ahora := time.Now()
	if enEspera(previa, ahora) {
		return contactoDe(ctx, q, *previa, true)
	}
	cob := cobro{uso: usoVerificar, usuario: contexto.ActorDe(ctx).ID}
	ficha, err := m.refrescos.leerUnaVez(ctx, c.ID, clienteID, cob, previa == nil, func(cx context.Context) (fichaArgos, error) {
		return leerFichaArgos(cx, bd, m.clienteDe(c), clienteID)
	})
	switch {
	case err == nil:
		if _, err := m.guardarFichaInterna.Ejecutar(ctx, entradaGuardarFicha{ClienteID: clienteID, ficha: &ficha}); err != nil {
			return ContactoArgos{}, err
		}
	case EsNoEncontrado(err):
		if previa != nil {
			if _, err := m.guardarFichaInterna.Ejecutar(ctx, entradaGuardarFicha{ClienteID: clienteID, borrar: true}); err != nil {
				return ContactoArgos{}, err
			}
		}
		return ContactoArgos{}, nil
	case errors.Is(err, errCuota) && previa != nil, EsCaido(err) && previa != nil:
		if EsCaido(err) {
			if _, err := m.guardarFichaInterna.Ejecutar(ctx, entradaGuardarFicha{ClienteID: clienteID, fallo: true}); err != nil {
				return ContactoArgos{}, err
			}
		}
		return contactoDe(ctx, q, *previa, true)
	case errors.Is(err, errCuota):
		return ContactoArgos{}, errCuotaArgos()
	default:
		return ContactoArgos{}, Problema(err)
	}
	f, err := q.ClientePorID(ctx, clienteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContactoArgos{}, nil
	}
	if err != nil {
		return ContactoArgos{}, err
	}
	return contactoDe(ctx, q, f, false)
}

func contactoDe(ctx context.Context, q *argosbd.Queries, f argosbd.IntegracionArgosCliente, stale bool) (ContactoArgos, error) {
	servicios, err := q.ServiciosDeCliente(ctx, f.ID)
	if err != nil {
		return ContactoArgos{}, err
	}
	c := ContactoArgos{
		Existe: true, Empresas: empresasDeFila(f), SinServicios: f.SinServicios, Telefono: f.Telefono, Movil: f.Movil,
		Correo: f.Correo, Servicios: make(map[int64]int64, len(servicios)), Stale: stale, SincronizadoEn: f.SincronizadoEn,
	}
	for _, s := range servicios {
		c.Servicios[s.ID] = s.EmpresaID
	}
	return c, nil
}
