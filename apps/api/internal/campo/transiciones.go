package campo

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// EntradaPosicion es la entrada de ordenes.iniciar_camino y ordenes.llegar: la posición GPS
// (opcional) queda en el historial como evidencia con hora.
type EntradaPosicion struct {
	ID        int64    `json:"-"`
	Version   int32    `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Lat       *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon       *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	Precision *float64 `json:"precision,omitempty" minimum:"0" doc:"Metros"`
	Sync      *OpSync  `json:"-"`
}

// EntradaFallarOrden es la entrada de ordenes.fallar.
type EntradaFallarOrden struct {
	ID          int64    `json:"-"`
	Version     int32    `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	MotivoFalla string   `json:"motivoFalla" enum:"cliente_ausente,sin_acceso,sin_cobertura,falta_material,equipo_defectuoso,clima,cliente_rechaza,otro"`
	Detalle     string   `json:"detalle,omitempty" maxLength:"2000" doc:"Obligatorio con motivo otro"`
	Lat         *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon         *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	Sync        *OpSync  `json:"-"`
}

// EntradaMotivo es la entrada de ordenes.cancelar y ordenes.reabrir.
type EntradaMotivo struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  string `json:"motivo" minLength:"3" maxLength:"500"`
}

func (e EntradaPosicion) ordenID() int64    { return e.ID }
func (e EntradaFallarOrden) ordenID() int64 { return e.ID }
func (e EntradaMotivo) ordenID() int64      { return e.ID }

func syncPosicion(e EntradaPosicion) *OpSync  { return e.Sync }
func syncFallar(e EntradaFallarOrden) *OpSync { return e.Sync }

func (m *Modulo) nuevoIniciarCamino() *auditoria.Caso[EntradaPosicion, Orden] {
	return accionOrden("ordenes.iniciar_camino", nucleo.PermOrdenesIniciarCamino, syncPosicion,
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaPosicion) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesIniciarCamino, versionDe(in.Version, in.Sync), in.Sync)
			if err != nil {
				return antes, "", err
			}
			cuando := m.cuandoEn(in.Sync, antes)
			if err := exigirEstado(antes, "iniciar el camino de", EstadoProgramada); err != nil {
				return antes, "", err
			}
			if err := validarCoordenadas(in.Lat, in.Lon); err != nil {
				return antes, "", err
			}
			despues := antes
			despues.Estado, despues.EnCaminoEn = EstadoEnCamino, &cuando
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			return f, "", cambioDeEstado(ctx, t, q, antes, f, nil, gps(in.Lat, in.Lon, in.Precision, cuando), cuando)
		})
}

func (m *Modulo) nuevoLlegar() *auditoria.Caso[EntradaPosicion, Orden] {
	return accionOrden("ordenes.llegar", nucleo.PermOrdenesLlegar, syncPosicion,
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaPosicion) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesLlegar, versionDe(in.Version, in.Sync), in.Sync)
			if err != nil {
				return antes, "", err
			}
			cuando := m.cuandoEn(in.Sync, antes)
			// Desde programada también: el técnico pudo no marcar «en camino».
			if err := exigirEstado(antes, "marcar la llegada de", EstadoProgramada, EstadoEnCamino); err != nil {
				return antes, "", err
			}
			if err := validarCoordenadas(in.Lat, in.Lon); err != nil {
				return antes, "", err
			}
			despues := antes
			despues.Estado, despues.EnSitioEn = EstadoEnSitio, &cuando
			if despues.EnCaminoEn == nil {
				despues.EnCaminoEn = &cuando
			}
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			return f, "", cambioDeEstado(ctx, t, q, antes, f, nil, gps(in.Lat, in.Lon, in.Precision, cuando), cuando)
		})
}

func (m *Modulo) nuevoFallarOrden() *auditoria.Caso[EntradaFallarOrden, Orden] {
	return accionOrden("ordenes.fallar", nucleo.PermOrdenesFallar, syncFallar,
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaFallarOrden) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesFallar, versionDe(in.Version, in.Sync), in.Sync)
			if err != nil {
				return antes, "", err
			}
			cuando := m.cuandoEn(in.Sync, antes)
			if err := exigirEstado(antes, "marcar como fallida", EstadoProgramada, EstadoEnCamino, EstadoEnSitio); err != nil {
				return antes, "", err
			}
			if !slices.Contains(MotivosFalla, in.MotivoFalla) {
				return antes, "", campoInvalido("motivoFalla", "Motivo de falla desconocido", "enum")
			}
			detalle, err := textoOpcional("detalle", &in.Detalle, 2000)
			if err != nil {
				return antes, "", err
			}
			if in.MotivoFalla == "otro" && detalle == nil {
				return antes, "", campoObligatorio("detalle")
			}
			if err := validarCoordenadas(in.Lat, in.Lon); err != nil {
				return antes, "", err
			}
			despues := antes
			despues.Estado, despues.FinalizadaEn = EstadoFallida, &cuando
			despues.Resultado, despues.MotivoFalla, despues.Motivo = ptr("fallida"), &in.MotivoFalla, detalle
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			motivo := in.MotivoFalla
			if detalle != nil {
				motivo += ": " + *detalle
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, &motivo, gps(in.Lat, in.Lon, nil, cuando), cuando); err != nil {
				return f, "", err
			}
			return f, "", notaEnTicket(ctx, t, f, fmt.Sprintf("La orden %s falló (%s).", f.Numero, motivo))
		})
}

func (m *Modulo) nuevoCancelarOrden() *auditoria.Caso[EntradaMotivo, Orden] {
	return accionOrden("ordenes.cancelar", nucleo.PermOrdenesCancelar, sinLote[EntradaMotivo],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaMotivo) (campobd.CampoOrden, string, error) {
			ahora := m.ahora()
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesCancelar, &in.Version, nil)
			if err != nil {
				return antes, "", err
			}
			if err := exigirEstado(antes, "cancelar", EstadoPendiente, EstadoProgramada, EstadoEnCamino, EstadoEnSitio); err != nil {
				return antes, "", err
			}
			motivo, err := texto("motivo", in.Motivo, 3, 500)
			if err != nil {
				return antes, "", err
			}
			despues := antes
			despues.Estado, despues.FinalizadaEn, despues.Resultado, despues.Motivo = EstadoCancelada, &ahora, ptr("cancelada"), &motivo
			// La cita deja de ocupar la agenda del técnico.
			if err := agenda.QuitarCita(ctx, t, antes.ID); err != nil {
				return antes, "", err
			}
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, &motivo, nil, ahora); err != nil {
				return f, "", err
			}
			return f, "", notaEnTicket(ctx, t, f, fmt.Sprintf("La orden %s se canceló: %s", f.Numero, motivo))
		})
}

// nuevoReabrirOrden: fallida o cancelada → pendiente (conserva el técnico; hay que volver a
// programarla). Una cerrada no se reabre: ya tuvo efectos en Argos; se crea otra orden.
func (m *Modulo) nuevoReabrirOrden() *auditoria.Caso[EntradaMotivo, Orden] {
	return accionOrden("ordenes.reabrir", nucleo.PermOrdenesReabrir, sinLote[EntradaMotivo],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaMotivo) (campobd.CampoOrden, string, error) {
			ahora := m.ahora()
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesReabrir, &in.Version, nil)
			if err != nil {
				return antes, "", err
			}
			if err := exigirEstado(antes, "reabrir", EstadoFallida, EstadoCancelada); err != nil {
				return antes, "", err
			}
			motivo, err := texto("motivo", in.Motivo, 3, 500)
			if err != nil {
				return antes, "", err
			}
			// La firma del intento anterior no vale para el próximo cierre.
			if err := invalidarFirma(ctx, t, q, antes.ID, ahora); err != nil {
				return antes, "", err
			}
			despues := antes
			despues.Estado, despues.FinalizadaEn, despues.Resultado, despues.MotivoFalla = EstadoPendiente, nil, nil, nil
			despues.EnCaminoEn, despues.EnSitioEn, despues.Motivo = nil, nil, &motivo
			// Vuelve a pendiente: hay que programarla de nuevo (otra cita).
			if err := agenda.QuitarCita(ctx, t, antes.ID); err != nil {
				return antes, "", err
			}
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			return f, "", cambioDeEstado(ctx, t, q, antes, f, &motivo, nil, ahora)
		})
}

// notaEnTicket deja una nota interna en el ticket de origen (si lo hay).
func notaEnTicket(ctx context.Context, t *auditoria.Tx, f campobd.CampoOrden, cuerpo string) error {
	if f.TicketID == nil {
		return nil
	}
	return soporte.NotaDeOrden(ctx, t, *f.TicketID, cuerpo)
}

// invalidarFirma: al reabrir o reprogramar una orden fallida, la firma del intento anterior deja
// de contar (el nuevo cierre pide otra; queda guardada como evidencia).
func invalidarFirma(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, ordenID int64, ahora time.Time) error {
	n, err := q.InvalidarFirma(ctx, campobd.InvalidarFirmaParams{OrdenID: ordenID, InvalidadaEn: &ahora})
	if err == nil && n > 0 {
		t.Anotar(map[string]any{"firma_invalidada": true})
	}
	return err
}
