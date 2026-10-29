package argos

import (
	"context"
	"errors"
	"net/netip"
	"strings"

	"github.com/jackc/pgx/v5"

	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Resolución de IP para uso interno (jobs de otros conectores, p. ej. los hallazgos de Horus):
// la misma lógica que GET /clientes/resolver-ip (A8 con respaldo a GET /ip/{ip}, instantánea si
// Argos no responde), sin HTTP ni permisos de usuario. Quien la llama audita lo que resolvió en
// su propio caso de uso.

// Motivos por los que no se resolvió el cliente de una IP.
const (
	MotivoIPInvalida        = "ip_invalida"
	MotivoConectorApagado   = "conector_argos_apagado"
	MotivoIPSinCliente      = "ip_sin_cliente"
	MotivoArgosNoDisponible = "argos_no_disponible"
	MotivoArgosRechazo      = "argos_rechazo"
	MotivoOtraEmpresa       = "otra_empresa"
)

// ResolucionIP es el dueño de una IP según Argos. Sin cliente, Motivo dice por qué.
type ResolucionIP struct {
	IP         string
	ClienteID  *int64
	ServicioID *int64
	EmpresaID  *int64
	// Fuente: argos | instantanea ("" si no se resolvió).
	Fuente string
	Motivo string
}

// Resuelta indica si se encontró el cliente o el servicio.
func (r ResolucionIP) Resuelta() bool { return r.Motivo == "" }

// ResolverIPInterno pregunta a Argos de quién es una IP. empresaID (≠ 0) acota la búsqueda: lo
// de otra empresa no cuenta (MotivoOtraEmpresa). Solo devuelve error si falla la base; lo
// demás (conector apagado, Argos caído sin instantánea, IP sin cliente) va en Motivo. ctx
// necesita la base (nucleo.ConBD).
func (m *Modulo) ResolverIPInterno(ctx context.Context, ip string, empresaID int64) (ResolucionIP, error) {
	r := ResolucionIP{IP: strings.TrimSpace(ip)}
	dir, err := netip.ParseAddr(r.IP)
	if err != nil {
		r.Motivo = MotivoIPInvalida
		return r, nil
	}
	r.IP = dir.Unmap().String()
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return r, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
		r.Motivo = MotivoConectorApagado
		return r, nil
	}
	if err != nil {
		return r, err
	}
	res, err := m.clienteDe(c).ResolverIP(ctx, r.IP, empresaID)
	switch {
	case err == nil:
		r.ClienteID, r.ServicioID, r.EmpresaID = res.Ids()
		r.Fuente = "argos"
		if r.ClienteID == nil && r.ServicioID == nil {
			r = ResolucionIP{IP: r.IP, Motivo: MotivoIPSinCliente}
		}
	case EsNoEncontrado(err):
		r.Motivo = MotivoIPSinCliente
	case EsCaido(err):
		s, err := argosbd.New(bd).ServicioPorIP(ctx, argosbd.ServicioPorIPParams{Ip: r.IP, EmpresaID: empresaID})
		if errors.Is(err, pgx.ErrNoRows) {
			r.Motivo = MotivoArgosNoDisponible
			return r, nil
		}
		if err != nil {
			return r, err
		}
		r.ClienteID, r.ServicioID, r.EmpresaID, r.Fuente = &s.ClienteID, &s.ID, &s.EmpresaID, "instantanea"
	default:
		r.Motivo = MotivoArgosRechazo
	}
	if r.Resuelta() && empresaID != 0 && (r.EmpresaID == nil || *r.EmpresaID != empresaID) {
		return ResolucionIP{IP: r.IP, Motivo: MotivoOtraEmpresa}, nil
	}
	return r, nil
}

// EmpresaDeRouter devuelve la empresa de un router según la instantánea de Argos (nil si no
// está). ctx necesita la base.
func (m *Modulo) EmpresaDeRouter(ctx context.Context, routerID int64) (*int64, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nil, nucleo.ErrSinBD
	}
	f, err := argosbd.New(bd).RouterPorID(ctx, routerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f.EmpresaID, nil
}

// ClienteValidado es lo que Argos dice de un cliente (para validar el acceso al portal).
// Validado = false si no se pudo preguntar (Motivo: conector_argos_apagado,
// argos_no_disponible, argos_rechazo, sin_empresa_en_argos).
type ClienteValidado struct {
	Validado bool
	Existe   bool
	// Empresas: las de sus servicios (el cliente de Argos no tiene empresa); ConServicios: tiene
	// alguno (vacía y sin servicios = lo ven todas).
	Empresas     []int64
	ConServicios bool
	Motivo       string
}

// ValidarCliente lee el cliente en Argos (GET /clientes/:id), sin HTTP de Iris ni permisos de
// usuario: quien lo llama audita el resultado. Solo devuelve error si falla la base.
func (m *Modulo) ValidarCliente(ctx context.Context, clienteID int64) (ClienteValidado, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ClienteValidado{}, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
		return ClienteValidado{Motivo: MotivoConectorApagado}, nil
	}
	if err != nil {
		return ClienteValidado{}, err
	}
	cli := m.clienteDe(c)
	cl, err := cli.ObtenerCliente(ctx, clienteID)
	var servicios []ServicioArgos
	if err == nil {
		servicios, err = cli.ServiciosDeCliente(ctx, clienteID)
	}
	switch {
	case err == nil:
		return ClienteValidado{Validado: true, Existe: true, Empresas: EmpresasDelCliente(cl, servicios), ConServicios: len(servicios) > 0}, nil
	case EsNoEncontrado(err):
		return ClienteValidado{Validado: true}, nil
	case EsCaido(err):
		return ClienteValidado{Motivo: MotivoArgosNoDisponible}, nil
	}
	return ClienteValidado{Motivo: MotivoArgosRechazo}, nil
}
