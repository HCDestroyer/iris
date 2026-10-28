package agenda

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaRegistrarAusencia registra una ausencia del técnico.
type EntradaRegistrarAusencia struct {
	ID      int64  `json:"-"`
	Inicio  string `json:"inicio" format:"date-time" doc:"UTC (RFC 3339)"`
	Fin     string `json:"fin" format:"date-time" doc:"Posterior al inicio; hasta 366 días"`
	Motivo  string `json:"motivo" enum:"vacaciones,enfermedad,capacitacion,permiso,otro"`
	Detalle string `json:"detalle,omitempty" maxLength:"500"`
}

// EntradaAnularAusencia anula una ausencia (queda en el historial).
type EntradaAnularAusencia struct {
	TecnicoID  int64   `json:"-"`
	AusenciaID int64   `json:"-"`
	Version    int32   `json:"version" minimum:"1" doc:"Versión leída de la ausencia; si cambió, 409"`
	Motivo     *string `json:"motivo,omitempty" maxLength:"500"`
}

// CitaEnConflicto: una cita viva del técnico que cae en la ausencia (hay que reprogramarla).
type CitaEnConflicto struct {
	OrdenID int64     `json:"ordenId" doc:"0 si la orden es de otra empresa (solo el horario)"`
	Numero  string    `json:"numero" doc:"Vacío si la orden es de otra empresa"`
	Inicio  time.Time `json:"inicio"`
	Fin     time.Time `json:"fin"`
}

// RespuestaAusencia: la ausencia y las citas que quedan en conflicto (no se mueven solas).
type RespuestaAusencia struct {
	Ausencia         Ausencia          `json:"ausencia"`
	CitasEnConflicto []CitaEnConflicto `json:"citasEnConflicto" doc:"Citas vivas del técnico dentro de la ausencia: reprográmalas"`
}

func (m *Modulo) nuevoRegistrarAusencia() *auditoria.Caso[EntradaRegistrarAusencia, RespuestaAusencia] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRegistrarAusencia, RespuestaAusencia]{
		Accion: "agenda.registrar_ausencia", Permiso: nucleo.PermAgendaRegistrarAusencia,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrarAusencia) (RespuestaAusencia, error) {
			q := agendabd.New(t)
			ficha, err := m.tomarTecnico(ctx, t, q, in.ID, nucleo.PermAgendaRegistrarAusencia, nil)
			if err != nil {
				return RespuestaAusencia{}, err
			}
			inicio, err := instante("inicio", in.Inicio)
			if err != nil {
				return RespuestaAusencia{}, err
			}
			fin, err := instante("fin", in.Fin)
			if err != nil {
				return RespuestaAusencia{}, err
			}
			if !fin.After(inicio) {
				return RespuestaAusencia{}, campoInvalido("fin", "El fin debe ser posterior al inicio", "rango")
			}
			if fin.Sub(inicio) > 366*24*time.Hour {
				return RespuestaAusencia{}, campoInvalido("fin", "Una ausencia dura hasta 366 días", "rango")
			}
			detalle, err := texto("detalle", in.Detalle, 0, 500)
			if err != nil {
				return RespuestaAusencia{}, err
			}
			f, err := q.CrearAusencia(ctx, agendabd.CrearAusenciaParams{TecnicoID: ficha.ID, Inicio: inicio, Fin: fin, Motivo: in.Motivo,
				Detalle: detalle, CreadaPor: actorUsuario(t)})
			if err != nil {
				return RespuestaAusencia{}, err
			}
			t.FijarEntidad("ausencia", f.ID)
			t.RegistrarCambios("ausencia", f.ID, nil, f)
			conflictos, err := q.Solapes(ctx, agendabd.SolapesParams{TecnicoID: ficha.UsuarioID, OrdenID: 0, Inicio: inicio, Fin: fin})
			if err != nil {
				return RespuestaAusencia{}, err
			}
			salida := RespuestaAusencia{Ausencia: aAusencia(f), CitasEnConflicto: []CitaEnConflicto{}}
			_, empresa := filtroEmpresa(ctx)
			for _, c := range conflictos {
				cc := CitaEnConflicto{OrdenID: c.OrdenID, Numero: c.Numero, Inicio: c.Inicio, Fin: c.Fin}
				if todas, _ := filtroEmpresa(ctx); !todas && !mismoID(c.EmpresaID, empresa) {
					cc.OrdenID, cc.Numero = 0, "" // de otra empresa: solo el horario
				}
				salida.CitasEnConflicto = append(salida.CitasEnConflicto, cc)
			}
			if len(salida.CitasEnConflicto) > 0 {
				t.Anotar(map[string]any{"citas_en_conflicto": len(salida.CitasEnConflicto)})
			}
			return salida, t.Emitir("tecnico.ausencia_registrada", map[string]any{"tecnico_id": ficha.ID, "usuario_id": ficha.UsuarioID,
				"ausencia_id": f.ID, "inicio": f.Inicio, "fin": f.Fin, "motivo": f.Motivo, "citas_en_conflicto": len(conflictos)})
		},
	})
}

func (m *Modulo) nuevoAnularAusencia() *auditoria.Caso[EntradaAnularAusencia, Ausencia] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAnularAusencia, Ausencia]{
		Accion: "agenda.anular_ausencia", Permiso: nucleo.PermAgendaAnularAusencia,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAnularAusencia) (Ausencia, error) {
			q := agendabd.New(t)
			ficha, err := m.tomarTecnico(ctx, t, q, in.TecnicoID, nucleo.PermAgendaAnularAusencia, nil)
			if err != nil {
				return Ausencia{}, err
			}
			t.FijarEntidad("ausencia", in.AusenciaID)
			antes, err := q.AusenciaPorIDParaActualizar(ctx, in.AusenciaID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && antes.TecnicoID != ficha.ID) {
				return Ausencia{}, nucleo.NoEncontrado("La ausencia no existe")
			}
			if err != nil {
				return Ausencia{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "ausencia", antes.ID); err != nil {
				return Ausencia{}, err
			}
			if antes.AnuladaEn != nil {
				return Ausencia{}, nucleo.ReglaNegocio("sin_cambios", "La ausencia ya está anulada")
			}
			if in.Motivo != nil {
				if _, err := texto("motivo", *in.Motivo, 0, 500); err != nil {
					return Ausencia{}, err
				}
			}
			ahora := m.ahora()
			f, err := q.AnularAusencia(ctx, agendabd.AnularAusenciaParams{ID: antes.ID, Version: antes.Version, AnuladaEn: &ahora, AnuladaPor: actorUsuario(t)})
			if err != nil {
				return Ausencia{}, nucleo.VerificarVersion(err, "ausencia", antes.ID)
			}
			t.RegistrarCambios("ausencia", f.ID, antes, f)
			return aAusencia(f), t.Emitir("tecnico.ausencia_anulada", map[string]any{"tecnico_id": ficha.ID, "usuario_id": ficha.UsuarioID,
				"ausencia_id": f.ID, "motivo": in.Motivo})
		},
	})
}

// instante lee una fecha y hora RFC 3339 (a UTC, en microsegundos).
func instante(campo, v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, campoInvalido(campo, "Fecha y hora RFC 3339", "fecha")
	}
	return t.UTC().Truncate(time.Microsecond), nil
}
