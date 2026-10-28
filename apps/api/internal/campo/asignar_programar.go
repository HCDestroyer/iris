package campo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// EntradaAsignarOrden es la entrada de ordenes.asignar. Sin técnico, la orden queda sin
// técnico (y vuelve a pendiente si estaba programada o en camino).
type EntradaAsignarOrden struct {
	ID          int64  `json:"-"`
	Version     int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	TecnicoID   *int64 `json:"tecnicoId,omitempty" minimum:"1" doc:"Usuario técnico; ausente = ninguno"`
	CuadrillaID *int64 `json:"cuadrillaId,omitempty" minimum:"1" doc:"Cuadrilla; ausente = ninguna"`
	BodegaID    *int64 `json:"bodegaId,omitempty" minimum:"1" doc:"Bodega del técnico en Argos (hasta que agenda la tenga); ausente = se conserva"`
}

// EntradaProgramarOrden es la entrada de ordenes.programar y ordenes.reprogramar. La agenda
// (fase 3) agregará la cita con ventana; por ahora, técnico y fecha.
type EntradaProgramarOrden struct {
	ID        int64      `json:"-"`
	Version   int32      `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	TecnicoID *int64     `json:"tecnicoId,omitempty" minimum:"1" doc:"Técnico; obligatorio si la orden no tiene. Otro distinto la reasigna"`
	BodegaID  *int64     `json:"bodegaId,omitempty" minimum:"1" doc:"Bodega del técnico en Argos; ausente = se conserva"`
	Inicio    time.Time  `json:"inicio" doc:"Inicio de la visita (UTC)"`
	Fin       *time.Time `json:"fin,omitempty" doc:"Fin estimado; por defecto, inicio + la duración del tipo"`
	Motivo    *string    `json:"motivo,omitempty" maxLength:"500" doc:"Obligatorio al reprogramar"`
}

func (m *Modulo) nuevoAsignarOrden() *auditoria.Caso[EntradaAsignarOrden, Orden] {
	return accionOrden("ordenes.asignar", nucleo.PermOrdenesAsignar, sinLote[EntradaAsignarOrden],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaAsignarOrden) (campobd.CampoOrden, string, error) {
			ahora := m.ahora()
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesAsignar, &in.Version, nil)
			if err != nil {
				return antes, "", err
			}
			if err := exigirEstado(antes, "asignar", EstadoPendiente, EstadoProgramada, EstadoEnCamino); err != nil {
				return antes, "", err
			}
			bodega := antes.BodegaID
			if in.BodegaID != nil {
				bodega = in.BodegaID
			}
			if mismoID(antes.TecnicoID, in.TecnicoID) && mismoID(antes.CuadrillaID, in.CuadrillaID) && mismoID(antes.BodegaID, bodega) {
				return antes, "", nucleo.ReglaNegocio("sin_cambios", "La orden ya tiene esa asignación")
			}
			despues := antes
			despues.TecnicoID, despues.CuadrillaID, despues.BodegaID = in.TecnicoID, in.CuadrillaID, bodega
			if in.TecnicoID != nil {
				if err := validarTecnico(ctx, t, q, *in.TecnicoID, despues); err != nil {
					return antes, "", err
				}
			}
			if !mismoID(antes.TecnicoID, in.TecnicoID) {
				despues.AsignadaEn = nil
				if in.TecnicoID != nil {
					despues.AsignadaEn = &ahora
				}
				switch {
				case in.TecnicoID == nil && antes.Estado != EstadoPendiente:
					// Sin técnico no hay programación que sostener.
					despues.Estado, despues.EnCaminoEn = EstadoPendiente, nil
				case antes.Estado == EstadoEnCamino:
					// El nuevo técnico todavía no salió.
					despues.Estado, despues.EnCaminoEn = EstadoProgramada, nil
				}
			}
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			if err := historial(ctx, t, q, f.ID, "asignada", nil, nil, nil, map[string]any{
				"tecnico_id": f.TecnicoID, "cuadrilla_id": f.CuadrillaID, "bodega_id": f.BodegaID,
				"tecnico_anterior": antes.TecnicoID, "cuadrilla_anterior": antes.CuadrillaID,
			}, ahora); err != nil {
				return f, "", err
			}
			if err := t.Emitir("orden.asignada", map[string]any{
				"orden_id": f.ID, "tecnico_id": f.TecnicoID, "cuadrilla_id": f.CuadrillaID,
				"tecnico_anterior_id": antes.TecnicoID,
			}); err != nil {
				return f, "", err
			}
			return f, "", cambioDeEstado(ctx, t, q, antes, f, ptr("asignación"), nil, ahora)
		})
}

// nuevoProgramarOrden arma ordenes.programar (pendiente → programada) u ordenes.reprogramar
// (programada, en camino o fallida → programada, con motivo).
func (m *Modulo) nuevoProgramarOrden(re bool) *auditoria.Caso[EntradaProgramarOrden, Orden] {
	accion, permiso, verbo := "ordenes.programar", nucleo.PermOrdenesProgramar, "programar"
	desde := []string{EstadoPendiente}
	if re {
		accion, permiso, verbo = "ordenes.reprogramar", nucleo.PermOrdenesReprogramar, "reprogramar"
		desde = []string{EstadoProgramada, EstadoEnCamino, EstadoFallida}
	}
	return accionOrden(accion, permiso, sinLote[EntradaProgramarOrden],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaProgramarOrden) (campobd.CampoOrden, string, error) {
			ahora := m.ahora()
			antes, err := tomar(ctx, t, q, in.ID, permiso, &in.Version, nil)
			if err != nil {
				return antes, "", err
			}
			if err := exigirEstado(antes, verbo, desde...); err != nil {
				return antes, "", err
			}
			motivo, err := textoOpcional("motivo", in.Motivo, 500)
			if err != nil {
				return antes, "", err
			}
			if re && motivo == nil {
				return antes, "", campoObligatorio("motivo")
			}
			tecnico := antes.TecnicoID
			if in.TecnicoID != nil {
				tecnico = in.TecnicoID
			}
			if tecnico == nil {
				return antes, "", campoObligatorio("tecnicoId")
			}
			tipo, err := tipoDe(ctx, q, antes)
			if err != nil {
				return antes, "", err
			}
			inicio := in.Inicio.UTC().Truncate(time.Microsecond)
			fin := inicio.Add(time.Duration(tipo.DuracionMin) * time.Minute)
			if in.Fin != nil {
				fin = in.Fin.UTC().Truncate(time.Microsecond)
			}
			if !fin.After(inicio) {
				return antes, "", campoInvalido("fin", "El fin debe ser posterior al inicio", "rango")
			}
			despues := antes
			despues.TecnicoID, despues.ProgramadaInicio, despues.ProgramadaFin = tecnico, &inicio, &fin
			if in.BodegaID != nil {
				despues.BodegaID = in.BodegaID
			}
			if !mismoID(antes.TecnicoID, tecnico) {
				if err := validarTecnico(ctx, t, q, *tecnico, despues); err != nil {
					return antes, "", err
				}
				despues.AsignadaEn = &ahora
			}
			if antes.Estado == EstadoFallida {
				if err := invalidarFirma(ctx, t, q, antes.ID, ahora); err != nil {
					return antes, "", err
				}
			}
			despues.Estado = EstadoProgramada
			despues.EnCaminoEn, despues.EnSitioEn, despues.FinalizadaEn = nil, nil, nil
			despues.Resultado, despues.MotivoFalla = nil, nil
			if re {
				despues.Motivo = motivo
			}
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			if err := historial(ctx, t, q, f.ID, "programada", nil, nil, motivo, map[string]any{
				"tecnico_id": f.TecnicoID, "inicio": inicio, "fin": fin, "reprogramada": re,
				"inicio_anterior": antes.ProgramadaInicio, "tecnico_anterior": antes.TecnicoID,
			}, ahora); err != nil {
				return f, "", err
			}
			if !mismoID(antes.TecnicoID, f.TecnicoID) {
				if err := t.Emitir("orden.asignada", map[string]any{
					"orden_id": f.ID, "tecnico_id": f.TecnicoID, "cuadrilla_id": f.CuadrillaID,
					"tecnico_anterior_id": antes.TecnicoID,
				}); err != nil {
					return f, "", err
				}
			}
			// orden.programada es del catálogo de agenda; mientras no exista (fase 3) lo emite campo.
			if err := t.Emitir("orden.programada", map[string]any{
				"orden_id": f.ID, "tecnico_id": f.TecnicoID, "cuadrilla_id": f.CuadrillaID,
				"inicio": inicio, "fin": fin, "reprogramada": re, "motivo": motivo,
			}); err != nil {
				return f, "", err
			}
			return f, "", cambioDeEstado(ctx, t, q, antes, f, motivo, nil, ahora)
		})
}

// sinLote: la acción solo se usa desde el panel o la API.
func sinLote[In any](In) *OpSync { return nil }

func (e EntradaAsignarOrden) ordenID() int64   { return e.ID }
func (e EntradaProgramarOrden) ordenID() int64 { return e.ID }

// validarTecnico: el técnico es una persona activa que ve la orden (ordenes.ver_propias u
// ordenes.ver con alcance global, de su zona o de su cuadrilla) y opera en su empresa.
func validarTecnico(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, tecnicoID int64, f campobd.CampoOrden) error {
	invalido := func(detalle string) error { return campoInvalido("tecnicoId", detalle, "tecnico") }
	u, err := q.UsuarioAsignable(ctx, tecnicoID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !u.Activo) {
		return invalido("El usuario no existe o está inactivo")
	}
	if err != nil {
		return err
	}
	if u.Tipo != "persona" {
		return invalido("Solo se asigna a personas, no a integraciones")
	}
	puede := false
	for _, permiso := range []nucleo.Permiso{nucleo.PermOrdenesVerPropias, nucleo.PermOrdenesVer} {
		alcances, err := q.AlcancesDePermisoDeUsuario(ctx, campobd.AlcancesDePermisoDeUsuarioParams{
			UsuarioID: tecnicoID, Permiso: string(permiso),
		})
		if err != nil {
			return err
		}
		for _, a := range alcances {
			if (a.ZonaID == nil && a.CuadrillaID == nil) ||
				(a.ZonaID != nil && f.ZonaID != nil && *a.ZonaID == *f.ZonaID) ||
				(a.CuadrillaID != nil && f.CuadrillaID != nil && *a.CuadrillaID == *f.CuadrillaID) {
				puede = true
			}
		}
	}
	if !puede {
		return invalido("El usuario no puede ver esta orden (ordenes.ver_propias u ordenes.ver con alcance sobre su zona o cuadrilla)")
	}
	enLaEmpresa, err := identidad.UsuarioEnEmpresa(ctx, t, tecnicoID, deref(f.EmpresaID))
	if err != nil {
		return err
	}
	if !enLaEmpresa {
		return nucleo.ReglaNegocio("tecnico_fuera_de_empresa", "El técnico no opera en la empresa de la orden")
	}
	return nil
}
