package argos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
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
			m.avisarConfirmacion(ctx, id, "no_encontrado_cache", nil)
			return ErrTrasladoNoEncontrado
		}
		m.avisarConfirmacion(ctx, id, "sin_respuesta_cache", nil)
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
		m.avisarConfirmacion(ctx, id, "no_encontrado", nil)
		return ErrTrasladoNoEncontrado
	case errors.Is(err, errCuota):
		m.avisarConfirmacion(ctx, id, "cuota", nil)
		return ErrTrasladoNoConfirmado
	default:
		m.avisarConfirmacion(ctx, id, "sin_respuesta", err)
		return ErrTrasladoNoConfirmado
	}
	crudo, _ := v.(json.RawMessage)
	p, motivo := trasladoDeDetalle(crudo, id, c.IntegracionConectore)
	switch {
	case motivo == "empresa_fuera_de_alcance":
		m.avisarConfirmacion(ctx, id, "fuera_de_alcance", nil)
		return ErrTrasladoNoEncontrado
	case p == nil:
		m.avisarConfirmacion(ctx, id, "respuesta_invalida:"+motivo, nil)
		return ErrTrasladoNoConfirmado
	}
	_, err = m.guardarTrasladoLeido.Ejecutar(ctx, *p)
	return err
}

// estadosDetalle: el estado de OrdenTrasladoDetalle (distinto del webhook) → el de argos_traslados.
var estadosDetalle = map[string]string{
	"en_transito": "enviado", "recibida": "recibido", "recibida_con_diferencias": "recibido", "cancelada": "cancelado",
}

// ordenTrasladoDetalle es la respuesta de GET /inventario/traslados/:id (Argos 1.6.0, contrato
// 0.5.1): objeto directo. Solo lo que Iris guarda; lo demás se ignora.
type ordenTrasladoDetalle struct {
	ID                *int64         `json:"id"`
	EmpresaID         *int64         `json:"empresa_id"`
	Estado            string         `json:"estado"`
	BodegaDestinoID   *int64         `json:"bodega_destino_id"`
	BodegaDestinoTipo string         `json:"bodega_destino_tipo"`
	UsuarioDestinoID  *IDExterno     `json:"usuario_destino_id"`
	Motivo            *string        `json:"motivo"`
	EnviadoEn         *time.Time     `json:"enviado_en"`
	MetodoRecepcion   *string        `json:"metodo_recepcion"`
	RecibidoPor       *IDExterno     `json:"recibido_por"`
	RecibidoEn        *time.Time     `json:"recibido_en"`
	CanceladoEn       *time.Time     `json:"cancelado_en"`
	MotivoCancelacion *string        `json:"motivo_cancelacion"`
	Lineas            []lineaDetalle `json:"lineas"`
}

type lineaDetalle struct {
	ID         *int64          `json:"id"`
	ProductoID int64           `json:"producto_id"`
	Cantidad   conector.Numero `json:"cantidad"`
	Unidades   []struct {
		Serie *string `json:"serie"`
	} `json:"unidades"`
}

// trasladoDeDetalle normaliza OrdenTrasladoDetalle al modelo del webhook (líneas { linea_id,
// producto_id, cantidad, series[] } con los mismos topes) y lo valida igual (trasladoDeDatos). El
// técnico es usuario_destino_id (su id_externo) solo con bodega_destino_tipo = tecnico; sin él, el
// traslado queda sin técnico. nil + motivo si la forma no es la esperada.
func trasladoDeDetalle(crudo []byte, id int64, con conectorbd.IntegracionConectore) (*argosbd.GuardarTrasladoParams, string) {
	var d ordenTrasladoDetalle
	if conector.Decodificar(crudo, &d) != nil {
		return nil, "json"
	}
	if d.ID == nil || *d.ID != id {
		return nil, "id"
	}
	estado, ok := estadosDetalle[strings.TrimSpace(d.Estado)]
	if !ok {
		return nil, "estado"
	}
	if len(d.Lineas) > maxLineasTraslado {
		return nil, "lineas"
	}
	lineas := make([]map[string]any, 0, len(d.Lineas))
	for _, l := range d.Lineas {
		cant := l.Cantidad.Float()
		if cant == nil || math.IsNaN(*cant) || math.IsInf(*cant, 0) {
			return nil, "lineas"
		}
		series := []string{}
		for _, u := range l.Unidades {
			if u.Serie != nil && strings.TrimSpace(*u.Serie) != "" {
				series = append(series, *u.Serie)
			}
		}
		linea := map[string]any{"producto_id": l.ProductoID, "cantidad": *cant, "series": series}
		if l.ID != nil {
			linea["linea_id"] = *l.ID
		}
		lineas = append(lineas, linea)
	}
	crudoLineas, err := json.Marshal(lineas)
	if err != nil {
		return nil, "lineas"
	}
	p := payload050{TrasladoID: d.ID, BodegaDestinoID: d.BodegaDestinoID, Lineas: crudoLineas,
		EnviadoEn: d.EnviadoEn, RecibidoEn: d.RecibidoEn, CanceladoEn: d.CanceladoEn}
	p.EmpresaID = d.EmpresaID
	if d.MetodoRecepcion != nil {
		p.MetodoRecepcion = *d.MetodoRecepcion
	}
	if d.RecibidoPor != nil && *d.RecibidoPor != "" {
		p.RecibidoPor, _ = json.Marshal(string(*d.RecibidoPor))
	}
	switch {
	case d.MotivoCancelacion != nil:
		p.Motivo = *d.MotivoCancelacion
	case d.Motivo != nil:
		p.Motivo = *d.Motivo
	}
	if d.BodegaDestinoTipo == "tecnico" && d.UsuarioDestinoID != nil && *d.UsuarioDestinoID != "" {
		p.Tecnico = &RefTecnicoArgo{IDExterno: *d.UsuarioDestinoID}
	}
	return trasladoDeDatos(p, estado, nil, con)
}

// avisarConfirmacion deja en el log (estructurado, con el actor) una confirmación que no guardó nada:
// 404, Argos sin responder, cuota o respuesta inválida (la fila de auditoría la deja quien la pidió si
// termina en error).
func (m *Modulo) avisarConfirmacion(ctx context.Context, id int64, resultado string, err error) {
	a := contexto.ActorDe(ctx)
	attrs := []any{"traslado_id", id, "resultado", resultado, "actor_tipo", a.Tipo, "actor_id", a.ID, "empresa", contexto.EmpresaDe(ctx)}
	if err != nil {
		attrs = append(attrs, "err", err.Error())
	}
	slog.WarnContext(ctx, "argos: traslado sin confirmar", attrs...)
}

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
