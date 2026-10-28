package comercial

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaActividad registra una llamada, visita, nota o mensaje.
type EntradaActividad struct {
	ProspectoID   int64      `json:"-"`
	Tipo          string     `json:"tipo" enum:"llamada,visita,nota,mensaje"`
	Descripcion   string     `json:"descripcion" minLength:"1" maxLength:"4000"`
	Resultado     *string    `json:"resultado,omitempty" maxLength:"500" doc:"Contestó, no contestó, interesado…"`
	OcurridaEn    *time.Time `json:"ocurridaEn,omitempty" doc:"Cuándo pasó (por defecto ahora; no en el futuro)"`
	ProximaAccion *string    `json:"proximaAccion,omitempty" maxLength:"500" doc:"Qué sigue (llamar de nuevo, visita…)"`
	ProximaFecha  *time.Time `json:"proximaFecha,omitempty" doc:"Cuándo; exige proximaAccion"`

	ClaveIdempotencia string `json:"-"`
}

// EntradaListarActividades: actividades de un prospecto (más viejas primero).
type EntradaListarActividades struct {
	nucleo.ParamsPagina
	ID int64 `path:"id" minimum:"1"`
}

func (m *Modulo) nuevoRegistrarActividad() *auditoria.Caso[EntradaActividad, Actividad] {
	return auditoria.Nuevo(auditoria.Caso[EntradaActividad, Actividad]{
		Accion: "prospectos.registrar_actividad", Permiso: nucleo.PermProspectosRegistrarActividad,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActividad) (Actividad, error) {
			q := comercialbd.New(t)
			idem := conIdempotencia(ctx, in.ClaveIdempotencia)
			p, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermProspectosRegistrarActividad, nil)
			if err != nil {
				return Actividad{}, err
			}
			if idem != nil {
				previa, err := q.ActividadPorIdempotencia(ctx, idem)
				if err == nil && previa.ProspectoID == p.ID {
					t.Anotar(map[string]any{"repetida": true})
					return aActividad(previa), nil
				}
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return Actividad{}, err
				}
			}
			d := comercialbd.CrearActividadParams{EmpresaID: p.EmpresaID, ProspectoID: p.ID, Tipo: in.Tipo,
				ProximaFecha: in.ProximaFecha, AutorID: actorUsuario(t), Idempotencia: idem}
			if d.Descripcion, err = texto("descripcion", in.Descripcion, 1, 4000); err != nil {
				return Actividad{}, err
			}
			if d.Resultado, err = textoOpcional("resultado", in.Resultado, 500); err != nil {
				return Actividad{}, err
			}
			if d.ProximaAccion, err = textoOpcional("proximaAccion", in.ProximaAccion, 500); err != nil {
				return Actividad{}, err
			}
			if d.ProximaFecha != nil && d.ProximaAccion == nil {
				return Actividad{}, campoInvalido("proximaAccion", "Indica la próxima acción de esa fecha", "required")
			}
			ahora := m.ahora()
			d.OcurridaEn = ahora
			if in.OcurridaEn != nil {
				if in.OcurridaEn.After(ahora.Add(5 * time.Minute)) {
					return Actividad{}, campoInvalido("ocurridaEn", "No puede ser en el futuro", "fecha")
				}
				d.OcurridaEn = in.OcurridaEn.UTC()
			}
			f, err := q.CrearActividad(ctx, d)
			if err != nil {
				return Actividad{}, err
			}
			t.RegistrarCambios("actividad", f.ID, nil, f)
			return aActividad(f), t.Emitir("prospecto.actividad_registrada", map[string]any{
				"prospecto_id": p.ID, "actividad_id": f.ID, "tipo": f.Tipo, "proxima_fecha": f.ProximaFecha,
			})
		},
	})
}

func (m *Modulo) nuevoListarActividades() *auditoria.Caso[EntradaListarActividades, nucleo.Pagina[Actividad]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarActividades, nucleo.Pagina[Actividad]]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[nucleo.Pagina[Actividad]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarActividades) (nucleo.Pagina[Actividad], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Actividad]{}, err
			}
			q := comercialbd.New(t)
			if _, err := leer(ctx, t, q, in.ID); err != nil {
				return nucleo.Pagina[Actividad]{}, err
			}
			filas, err := q.ListarActividades(ctx, comercialbd.ListarActividadesParams{
				ProspectoID: in.ID, DespuesDe: despues, Limite: int32(in.Limite() + 1), //nolint:gosec // acotado a 201
			})
			if err != nil {
				return nucleo.Pagina[Actividad]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aActividad, func(f comercialbd.ComercialActividad) []any { return []any{f.ID} }), nil
		},
	})
}
