package argos

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Confirmación de un traslado con Argos (contrato 0.5.0 §3.3, GET /inventario/traslados/:id): la
// recepción de un traslado que Iris aún no conoce (el webhook no llegó) o sin técnico se confirma
// leyéndolo de Argos antes de decidir.

// Resultados de ConfirmarTraslado que no son «guardado».
var (
	// ErrTrasladoNoEncontrado: Argos respondió 404 (o lo que devolvió es de una empresa fuera del
	// alcance del conector): definitivo.
	ErrTrasladoNoEncontrado = errors.New("argos: traslado no encontrado")
	// ErrTrasladoNoConfirmado: Argos no respondió (caída, 5xx, 429), se agotó la cuota de lecturas o
	// respondió algo que no se puede guardar: se reintenta más tarde.
	ErrTrasladoNoConfirmado = errors.New("argos: traslado sin confirmar")
)

// claveTraslado: los traslados comparten la caché negativa y el singleflight de la ficha con ids
// negativos (los clientes son positivos), sin chocar.
func claveTraslado(id int64) int64 { return -id }

// ConfirmarTraslado lee el traslado de Argos (fuera de transacción) y lo guarda en argos_traslados
// con la misma validación que el webhook (empresa en el alcance del conector, líneas acotadas), sin
// pisar un estado final. Protegido como la ficha: una sola lectura en curso por (conector,
// traslado), cuota de verificación (del usuario, del conector y de la empresa) y caché negativa
// corta (404: NegativoNoExisteTTL; caída: NegativoCaidaTTL). empresa: la activa (X-Empresa-Id).
func (m *Modulo) ConfirmarTraslado(ctx context.Context, id, empresa int64) error {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	if err != nil {
		return err
	}
	ahora := time.Now()
	if n, ok := m.refrescos.negativoDe(c.ID, claveTraslado(id), ahora); ok {
		if n.noExiste {
			return ErrTrasladoNoEncontrado
		}
		return ErrTrasladoNoConfirmado
	}
	cob := cobro{uso: usoVerificar, usuario: contexto.ActorDe(ctx).ID, empresa: empresa}
	clave := "traslado:" + strconv.FormatInt(c.ID, 10) + ":" + strconv.FormatInt(id, 10)
	v, err, _ := m.refrescos.vuelo.Do(clave, func() (any, error) {
		if !m.refrescos.tomarCuota(c.ID, cob, ahora) {
			return nil, errCuota
		}
		var crudo json.RawMessage
		err := m.clienteDe(c).Traslado(context.WithoutCancel(ctx), id, empresa, &crudo)
		switch {
		case err == nil:
			m.refrescos.olvidarNegativo(c.ID, claveTraslado(id))
		case EsNoEncontrado(err):
			m.refrescos.recordarNegativo(c.ID, claveTraslado(id), true, ahora)
		case EsCaido(err):
			m.refrescos.recordarNegativo(c.ID, claveTraslado(id), false, ahora)
		}
		return crudo, err
	})
	switch {
	case err == nil:
	case EsNoEncontrado(err):
		return ErrTrasladoNoEncontrado
	default:
		return ErrTrasladoNoConfirmado
	}
	crudo, _ := v.(json.RawMessage)
	var d payload050
	if conector.Decodificar(crudo, &d) != nil {
		return ErrTrasladoNoConfirmado
	}
	if d.TrasladoID == nil {
		d.TrasladoID = d.ID
	}
	if d.TrasladoID == nil || *d.TrasladoID != id {
		return ErrTrasladoNoConfirmado
	}
	estado, ok := estadosTraslado[strings.ToLower(strings.TrimSpace(d.Estado))]
	if !ok {
		return ErrTrasladoNoConfirmado
	}
	p, motivo := trasladoDeDatos(d, estado, nil, c.IntegracionConectore)
	switch {
	case motivo == "empresa_fuera_de_alcance":
		return ErrTrasladoNoEncontrado
	case p == nil:
		return ErrTrasladoNoConfirmado
	}
	_, err = m.guardarTrasladoLeido.Ejecutar(ctx, *p)
	return err
}

// estadosTraslado: el estado que devuelve Argos → el de argos_traslados.
var estadosTraslado = map[string]string{"enviado": "enviado", "recibido": "recibido", "cancelado": "cancelado"}

func nuevoGuardarTrasladoLeido() *auditoria.Caso[argosbd.GuardarTrasladoParams, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[argosbd.GuardarTrasladoParams, struct{}]{
		Accion: "integraciones.confirmar_traslado", Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, p argosbd.GuardarTrasladoParams) (struct{}, error) {
			t.FijarEntidad("traslado", p.ID)
			err := aplicarTraslado(ctx, t, argosbd.New(t), p)
			if errors.Is(err, errSinCambios) {
				return struct{}{}, nil
			}
			return struct{}{}, err
		},
	})
}
