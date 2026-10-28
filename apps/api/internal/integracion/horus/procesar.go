package horus

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Procesar un webhook de Horus (job) ----------------------------------------------------------
//
// El job no crea tickets (eso es de soporte, I2): valida y normaliza el payload (eventos.go) y
// lo publica como evento interno por el outbox con el mismo nombre. Para un hallazgo con IP,
// antes de la transacción pregunta a Argos de quién es la IP, solo dentro de la empresa del
// hallazgo (la del payload o, si no viene, la de su router en la instantánea de Argos; sin
// empresa no se resuelve: motivo_sin_cliente = empresa_desconocida) (argos.ResolverIPInterno: A8 con
// respaldo a GET /ip/{ip} y a la instantánea) y agrega cliente_id, servicio_id y empresa_id (o
// null con motivo_sin_cliente). Si el router no trae empresa_id, la toma de la instantánea de
// routers de Argos. Un payload inválido o un evento desconocido se guarda como «ignorado» con
// la nota en integracion.entrantes.error (no se reintenta). Es idempotente: un entrante
// procesado no se vuelve a emitir.

// ResultadoEntrante dice qué se hizo con un webhook.
type ResultadoEntrante struct {
	EntranteID int64  `json:"entrante_id"`
	Resultado  string `json:"resultado"` // aplicado | ignorado | ya_procesado
	Evento     string `json:"evento,omitempty"`
}

type entradaAplicar struct {
	EntranteID int64 `json:"entrante_id"`
	norm       *normalizado
	nota       string
}

// ProcesarEntrante aplica un webhook de Horus guardado.
type ProcesarEntrante struct {
	m       *Modulo
	aplicar *auditoria.Caso[entradaAplicar, ResultadoEntrante]
}

func nuevoProcesarEntrante(m *Modulo) *ProcesarEntrante {
	p := &ProcesarEntrante{m: m}
	p.aplicar = auditoria.Nuevo(auditoria.Caso[entradaAplicar, ResultadoEntrante]{
		Accion: "integraciones.procesar_webhook", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoEntrante) bool { return r.Resultado != "ya_procesado" },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaAplicar) (ResultadoEntrante, error) {
			var causa string
			r, err := aplicarEn(ctx, t, in, &causa)
			if err != nil {
				causa = err.Error()
			}
			return r, err
		},
	})
	return p
}

func aplicarEn(ctx context.Context, t *auditoria.Tx, in entradaAplicar, causa *string) (ResultadoEntrante, error) {
	r := ResultadoEntrante{EntranteID: in.EntranteID}
	e, procesado, err := conector.TomarEntrante(ctx, t, in.EntranteID, causa)
	if err != nil {
		return r, err
	}
	if procesado {
		r.Resultado = "ya_procesado"
		return r, nil
	}
	t.Anotar(map[string]any{"sistema": Sistema})
	if in.norm == nil {
		r.Resultado = "ignorado"
		t.Anotar(map[string]any{"nota": in.nota})
		return r, conector.MarcarEntrante(ctx, t, e.ID, r.Resultado, &in.nota)
	}
	if empresa := in.norm.empresaDe(); empresa != nil {
		t.FijarEmpresa(*empresa)
	} else {
		t.FijarEmpresa(0)
	}
	if h, ok := in.norm.payload.(*HallazgoAbierto); ok {
		t.Anotar(map[string]any{"cliente_id": h.ClienteID, "servicio_id": h.ServicioID, "motivo_sin_cliente": h.MotivoSinCliente})
	}
	if err := t.Emitir(in.norm.evento, in.norm.payload); err != nil {
		return r, err
	}
	r.Resultado, r.Evento = "aplicado", in.norm.evento
	return r, conector.MarcarEntrante(ctx, t, e.ID, r.Resultado, nil)
}

// Ejecutar procesa el entrante. ctx necesita la base (nucleo.ConBD) y un actor (el worker usa
// el actor sistema).
func (p *ProcesarEntrante) Ejecutar(ctx context.Context, entranteID int64) (ResultadoEntrante, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoEntrante{}, nucleo.ErrSinBD
	}
	e, err := conector.LeerEntrante(ctx, bd, entranteID)
	if err != nil {
		return ResultadoEntrante{}, err
	}
	if e.ProcesadoEn != nil {
		return ResultadoEntrante{EntranteID: e.ID, Resultado: "ya_procesado"}, nil
	}
	f, err := p.m.conexion.Fila(ctx, bd, e.ConectorID)
	if err != nil {
		return ResultadoEntrante{}, err
	}
	in := entradaAplicar{EntranteID: e.ID}
	in.norm, in.nota = normalizar(e, f.BaseUrl, conector.URLPublica(f))
	if in.norm != nil {
		if err := p.enriquecer(ctx, in.norm); err != nil {
			return ResultadoEntrante{}, err
		}
	}
	return p.aplicar.Ejecutar(ctx, in)
}

// enriquecer completa lo que Horus no sabe: el cliente de la IP (Argos) y la empresa del router
// (instantánea de Argos). Fuera de transacción: la resolución puede llamar a Argos.
func (p *ProcesarEntrante) enriquecer(ctx context.Context, n *normalizado) error {
	if n.routerSinEmpresa != nil {
		empresa, err := p.m.argos.EmpresaDeRouter(ctx, *n.routerSinEmpresa)
		if err != nil {
			return err
		}
		n.fijarEmpresaRouter(empresa)
	}
	h, ok := n.payload.(*HallazgoAbierto)
	if !ok || n.resolverIP == "" {
		return nil
	}
	if h.EmpresaID == nil {
		// Sin empresa no se resuelve: buscar la IP en todas las empresas podría dar el cliente
		// de otra (IPs CGNAT o privadas repetidas).
		motivo := MotivoEmpresaDesconocida
		h.MotivoSinCliente = &motivo
		return nil
	}
	res, err := p.m.argos.ResolverIPInterno(ctx, n.resolverIP, *h.EmpresaID)
	if err != nil {
		return err
	}
	if !res.Resuelta() {
		motivo := res.Motivo
		h.MotivoSinCliente = &motivo
		return nil
	}
	fuente := res.Fuente
	h.ClienteID, h.ServicioID, h.FuenteCliente = res.ClienteID, res.ServicioID, &fuente
	if res.EmpresaID != nil {
		h.EmpresaID = res.EmpresaID
	}
	return nil
}

// Motivos de motivo_sin_cliente (además de MotivoSinIP e ip_invalida).
const (
	MotivoConectorArgosApagado = argos.MotivoConectorApagado
	MotivoIPSinCliente         = argos.MotivoIPSinCliente
	MotivoArgosNoDisponible    = argos.MotivoArgosNoDisponible
	MotivoArgosRechazo         = argos.MotivoArgosRechazo
	MotivoOtraEmpresa          = argos.MotivoOtraEmpresa
)
