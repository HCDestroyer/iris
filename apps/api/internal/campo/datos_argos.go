package campo

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// «Dato de Argos cambió» (contrato Iris ↔ ArgosISP 0.5.0; I1): cuando Argos avisa que cambió la
// dirección, las coordenadas, la referencia o el contacto de un servicio (servicio.datos_cambiados) o
// de un cliente (cliente.actualizado), el conector emite argos.datos_cambiados. Las órdenes abiertas
// conservan lo que copiaron al crearse (el técnico puede ir en camino): solo quedan marcadas con
// cuándo y qué cambió, para que despacho lo revise y aplique. Emite orden.datos_argos_cambiados por
// orden marcada.

// DatosArgosCambiados es la marca de la orden.
type DatosArgosCambiados struct {
	En     time.Time `json:"en" doc:"Último aviso de Argos"`
	Campos []string  `json:"campos" doc:"Campos que cambiaron en Argos desde que se copió la orden (servicio.direccion, cliente.referencia, servicio.contacto_telefono…), acumulados"`
}

func datosArgosDe(f campobd.CampoOrden) *DatosArgosCambiados {
	if f.DatosArgosCambiadosEn == nil {
		return nil
	}
	campos := f.DatosArgosCampos
	if campos == nil {
		campos = []string{}
	}
	return &DatosArgosCambiados{En: *f.DatosArgosCambiadosEn, Campos: campos}
}

// EntradaDatosArgos es el payload de argos.datos_cambiados.
type EntradaDatosArgos struct {
	Entidad    string   `json:"entidad"`
	ClienteID  *int64   `json:"cliente_id"`
	ServicioID *int64   `json:"servicio_id"`
	Campos     []string `json:"campos"`
	EntranteID int64    `json:"entrante_id"`
}

// ResultadoDatosArgos: las órdenes marcadas.
type ResultadoDatosArgos struct {
	Ordenes []int64 `json:"ordenes"`
}

func (m *Modulo) nuevoMarcarDatosArgos() *auditoria.Caso[EntradaDatosArgos, ResultadoDatosArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDatosArgos, ResultadoDatosArgos]{
		Accion: "ordenes.marcar_datos_argos", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoDatosArgos) bool { return len(r.Ordenes) > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDatosArgos) (ResultadoDatosArgos, error) {
			r := ResultadoDatosArgos{Ordenes: []int64{}}
			campos := slices.Clone(in.Campos)
			slices.Sort(campos)
			campos = slices.Compact(campos)
			if len(campos) == 0 || (in.ClienteID == nil && in.ServicioID == nil) {
				return r, nil
			}
			p := campobd.MarcarDatosArgosParams{CambiadoEn: ptr(m.ahora()), Campos: campos}
			if in.Entidad == "servicio" {
				p.ServicioID = in.ServicioID
			} else {
				// Un cambio del cliente: todas sus órdenes abiertas (las que tienen servicio copian la
				// dirección del servicio, pero el contacto y la referencia del cliente también cuentan).
				p.ClienteID = in.ClienteID
			}
			filas, err := campobd.New(t).MarcarDatosArgos(ctx, p)
			if err != nil {
				return r, err
			}
			for _, f := range filas {
				r.Ordenes = append(r.Ordenes, f.ID)
				if f.EmpresaID != nil {
					t.FijarEmpresa(*f.EmpresaID)
				} else {
					t.FijarEmpresa(0)
				}
				if err := t.Emitir("orden.datos_argos_cambiados", map[string]any{
					"orden_id": f.ID, "numero": f.Numero, "entidad": in.Entidad, "campos": campos, "entrante_id": in.EntranteID,
				}); err != nil {
					return r, err
				}
			}
			t.FijarEmpresa(0)
			t.Anotar(map[string]any{"ordenes": r.Ordenes, "campos": campos, "entrante_id": in.EntranteID})
			return r, nil
		},
	})
}

// SuscribirDatosArgos conecta argos.datos_cambiados (en cmd/worker). Idempotente: marcar dos veces
// deja la misma marca (los campos se acumulan sin repetir).
func (m *Modulo) SuscribirDatosArgos(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("campo.datos_argos", argos.EventoDatosCambiados, func(ctx context.Context, e integracion.Evento) error {
		var in EntradaDatosArgos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			log.ErrorContext(ctx, "campo: argos.datos_cambiados ilegible", "evento", e.ID, "err", err)
			return nil
		}
		_, err := m.MarcarDatosArgos.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}
