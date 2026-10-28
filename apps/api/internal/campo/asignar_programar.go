package campo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
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
	CuadrillaID *int64 `json:"cuadrillaId,omitempty" minimum:"1" doc:"Cuadrilla (catálogo de agenda: activa y de la empresa de la orden); ausente = ninguna"`
	BodegaID    *int64 `json:"bodegaId,omitempty" minimum:"1" doc:"Bodega del técnico en Argos; ausente = se conserva"`
}

// EntradaProgramarOrden es la entrada de ordenes.programar y ordenes.reprogramar: la cita
// (técnico, inicio y fin) y la ventana de llegada acordada con el cliente. La agenda la valida
// (turno, ausencia, solape y capacidad: 422 fuera_de_turno, tecnico_ausente, solape,
// capacidad_excedida, fuera_de_ventana, tecnico_sin_agenda).
type EntradaProgramarOrden struct {
	ID            int64      `json:"-"`
	Version       int32      `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	TecnicoID     *int64     `json:"tecnicoId,omitempty" minimum:"1" doc:"Técnico (usuario); obligatorio si la orden no tiene. Otro distinto la reasigna"`
	BodegaID      *int64     `json:"bodegaId,omitempty" minimum:"1" doc:"Bodega del técnico en Argos; ausente = la de su ficha de agenda o se conserva"`
	Inicio        time.Time  `json:"inicio" doc:"Inicio de la visita (UTC)"`
	Fin           *time.Time `json:"fin,omitempty" doc:"Fin estimado; por defecto, inicio + la duración del tipo"`
	VentanaInicio *time.Time `json:"ventanaInicio,omitempty" doc:"Ventana de llegada acordada con el cliente (va con ventanaFin); el inicio debe caer dentro. Al reprogramar sin ventana se conserva la anterior si el nuevo inicio cae en ella"`
	VentanaFin    *time.Time `json:"ventanaFin,omitempty" doc:"Fin de la ventana (hasta 24 h después de su inicio)"`
	Motivo        *string    `json:"motivo,omitempty" maxLength:"500" doc:"Obligatorio al reprogramar"`
}

func (m *Modulo) nuevoAsignarOrden() *auditoria.Caso[EntradaAsignarOrden, Orden] {
	return accionOrden("ordenes.asignar", nucleo.PermOrdenesAsignar, sinLote[EntradaAsignarOrden],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaAsignarOrden) (campobd.CampoOrden, string, error) {
			ahora := m.ahora()
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesAsignar, &in.Version, nil)
			if err != nil {
				return antes, "", err
			}
			if err := noPausada(antes, "reasignarla (o reprográmala)"); err != nil {
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
			if in.CuadrillaID != nil && !mismoID(antes.CuadrillaID, in.CuadrillaID) {
				if err := agenda.ValidarCuadrilla(ctx, t, "cuadrillaId", *in.CuadrillaID, antes.EmpresaID); err != nil {
					return antes, "", err
				}
			}
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
					// Sin técnico no hay programación que sostener: se libera la cita.
					despues.Estado, despues.EnCaminoEn = EstadoPendiente, nil
					if err := m.cfg.Agenda.QuitarCita(ctx, t, antes.ID, fmt.Sprintf("La orden %s quedó sin técnico", antes.Numero)); err != nil {
						return antes, "", err
					}
				case antes.Estado == EstadoEnCamino:
					// El nuevo técnico todavía no salió.
					despues.Estado, despues.EnCaminoEn = EstadoProgramada, nil
				}
				// La misma cita pasa al nuevo técnico: la agenda valida su turno, ausencias,
				// solapes y capacidad, y emite orden.programada.
				if in.TecnicoID != nil && despues.Estado == EstadoProgramada && antes.ProgramadaInicio != nil && antes.ProgramadaFin != nil {
					if _, err := m.cfg.Agenda.ReservarCita(ctx, t, agenda.Reserva{OrdenID: antes.ID, Numero: antes.Numero,
						EmpresaID: antes.EmpresaID, TipoID: antes.TipoID, CuadrillaID: despues.CuadrillaID, TecnicoID: *in.TecnicoID,
						Inicio: *antes.ProgramadaInicio, Fin: *antes.ProgramadaFin, Origen: "asignar"}); err != nil {
						return antes, "", err
					}
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
// (programada, en camino, pausada o fallida → programada, con motivo; desde pausada cierra la
// pausa).
func (m *Modulo) nuevoProgramarOrden(re bool) *auditoria.Caso[EntradaProgramarOrden, Orden] {
	accion, permiso, verbo := "ordenes.programar", nucleo.PermOrdenesProgramar, "programar"
	desde := []string{EstadoPendiente}
	if re {
		accion, permiso, verbo = "ordenes.reprogramar", nucleo.PermOrdenesReprogramar, "reprogramar"
		desde = []string{EstadoProgramada, EstadoEnCamino, EstadoPausada, EstadoFallida}
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
				if in.BodegaID == nil {
					// La bodega de la ficha de agenda del nuevo técnico (si la tiene).
					if b, err := q.BodegaDeTecnico(ctx, *tecnico); err == nil && b != nil {
						despues.BodegaID = b
					} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
						return antes, "", err
					}
				}
			}
			// La agenda valida la cita (ficha, turno, ausencia, solape, capacidad y ventana), la
			// guarda y emite orden.programada (catálogo: lo emite agenda).
			cita, err := m.cfg.Agenda.ReservarCita(ctx, t, agenda.Reserva{OrdenID: antes.ID, Numero: antes.Numero,
				EmpresaID: antes.EmpresaID, TipoID: antes.TipoID, CuadrillaID: antes.CuadrillaID, TecnicoID: *tecnico,
				Inicio: inicio, Fin: fin, VentanaInicio: in.VentanaInicio, VentanaFin: in.VentanaFin, Origen: verbo, Motivo: motivo})
			if err != nil {
				return antes, "", err
			}
			if antes.Estado == EstadoFallida {
				if err := invalidarFirma(ctx, t, q, antes.ID, ahora); err != nil {
					return antes, "", err
				}
			}
			if antes.Estado == EstadoPausada {
				if _, err := cerrarPausa(ctx, t, q, &despues, ahora, CierreReprogramada, nil); err != nil {
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
				"ventana_inicio": cita.VentanaInicio, "ventana_fin": cita.VentanaFin,
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
