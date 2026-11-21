package agenda

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Vínculo de cada técnico con su usuario de Argos (contrato con ArgosISP 0.14.0 §6.6): Iris avisa a
// Argos con tecnico.argos_vinculado / tecnico.argos_desvinculado, uno por empresa en la que opera
// el usuario, cada uno con una version nueva (entera y creciente por técnico) y el conector del
// que salió el id_externo (solo a él se mandan; mensajeria lo quita del cuerpo). Argos concilia con
// GET …/tecnicos/:usuario_id.

// Eventos del vínculo.
const (
	EventoVinculado    = "tecnico.argos_vinculado"
	EventoDesvinculado = "tecnico.argos_desvinculado"
	// MotivoDesactivado: se desactivó el técnico o su usuario.
	MotivoDesactivado = "desactivado"
)

// emitirVinculo emite el evento del vínculo, uno por empresa del usuario (sin empresas, uno sin
// empresa, que no va a Argos), cada uno con su versión. extra: anterior_id_externo o motivo.
// Devuelve cuántos van a Argos (los que tienen empresa).
func emitirVinculo(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, f agendabd.AgendaTecnico, evento, idExterno string,
	extra map[string]any, carga *uuid.UUID) (int, error) {
	empresas, err := q.EmpresasDelUsuario(ctx, f.UsuarioID)
	if err != nil {
		return 0, err
	}
	return emitirVinculoEn(ctx, t, q, f, evento, idExterno, extra, carga, empresas)
}

// emitirVinculoEn: como emitirVinculo, solo para estas empresas (la asignada o la quitada).
func emitirVinculoEn(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, f agendabd.AgendaTecnico, evento, idExterno string,
	extra map[string]any, carga *uuid.UUID, empresas []int64) (int, error) {
	destinos := make([]*int64, 0, len(empresas))
	for i := range empresas {
		destinos = append(destinos, &empresas[i])
	}
	if len(destinos) == 0 {
		destinos = append(destinos, nil)
	}
	for _, empresa := range destinos {
		version, err := q.SubirVersionVinculo(ctx, f.ID)
		if err != nil {
			return 0, err
		}
		p := map[string]any{"tecnico_id": f.ID, "usuario_id": f.UsuarioID, "empresa_id": empresa, "version": version,
			"id_externo": idExterno, "conector_id": f.ArgosConectorID}
		for k, v := range extra {
			p[k] = v
		}
		if carga != nil {
			p["carga_id"] = carga.String()
		}
		if err := t.EmitirEnEmpresa(evento, empresa, p); err != nil {
			return 0, err
		}
	}
	if destinos[0] == nil {
		return 0, nil
	}
	return len(destinos), nil
}

// vinculoPorActivo: al activar o desactivar la ficha (o el usuario) de un técnico vinculado, Argos
// recibe el vínculo de nuevo o el desvinculado con motivo desactivado.
func vinculoPorActivo(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, f agendabd.AgendaTecnico, activo bool) error {
	if f.ArgosIDExterno == nil {
		return nil
	}
	evento, extra := EventoDesvinculado, map[string]any{"motivo": MotivoDesactivado}
	if activo {
		evento, extra = EventoVinculado, map[string]any{"anterior_id_externo": nil}
	}
	_, err := emitirVinculo(ctx, t, q, f, evento, *f.ArgosIDExterno, extra, nil)
	return err
}

// EntradaCargaVinculos: la carga inicial de vínculos de un conector (al informar 0.14.0).
type EntradaCargaVinculos struct {
	ConectorID int64     `json:"conector_id"`
	CargaID    uuid.UUID `json:"carga_id"`
}

func (m *Modulo) nuevoCargarVinculos() *auditoria.Caso[EntradaCargaVinculos, int] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCargaVinculos, int]{
		Accion: "agenda.cargar_vinculos_argos", Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCargaVinculos) (int, error) {
			if !esSistema(ctx) {
				return 0, nucleo.Prohibido("Solo el sistema carga los vínculos de Argos")
			}
			t.FijarEntidad("conector", in.ConectorID)
			q := agendabd.New(t)
			fichas, err := q.VinculosVigentesDeConector(ctx, &in.ConectorID)
			if err != nil {
				return 0, err
			}
			n := 0
			for _, f := range fichas {
				k, err := emitirVinculo(ctx, t, q, f, EventoVinculado, *f.ArgosIDExterno, map[string]any{"anterior_id_externo": nil}, &in.CargaID)
				if err != nil {
					return 0, err
				}
				n += k
			}
			t.Anotar(map[string]any{"tecnicos": len(fichas), "vinculados": n, "carga_id": in.CargaID})
			return n, nil
		},
	})
}

// CargarVinculos emite un tecnico.argos_vinculado por cada vínculo vigente que salió del conector
// (uno por empresa del técnico), marcado con la carga; devuelve cuántos van a Argos. Corre en la
// transacción de quien lo llama (argos, al informar 0.14.0).
func (m *Modulo) CargarVinculos(ctx context.Context, conectorID int64, carga uuid.UUID) (int, error) {
	return m.cargarVinculos.Ejecutar(ctx, EntradaCargaVinculos{ConectorID: conectorID, CargaID: carga})
}

// EntradaTecnicoArgos: GET /integraciones/argosisp/tecnicos/{usuario_id}?empresa_id=.
type EntradaTecnicoArgos struct {
	UsuarioID int64 `path:"usuario_id" minimum:"1"`
	EmpresaID int64 `query:"empresa_id" required:"true" minimum:"1" doc:"Empresa del vínculo (una por empresa)"`
}

// TecnicoArgos es el vínculo del técnico para conciliar (0.14.0 §6.6).
type TecnicoArgos struct {
	TecnicoID int64   `json:"tecnico_id"`
	UsuarioID int64   `json:"usuario_id"`
	EmpresaID int64   `json:"empresa_id"`
	IDExterno *string `json:"id_externo" nullable:"true" doc:"El usuario de Argos vinculado (null: desvinculado)"`
	Version   int32   `json:"version" doc:"La del último tecnico.argos_*: Argos aplica solo una mayor"`
	Activo    bool    `json:"activo" doc:"false equivale a desvinculado"`
}

func tecnicoArgosNoEncontrado() *nucleo.Problema {
	return nucleo.NoEncontrado("El técnico no existe")
}

func (m *Modulo) nuevoTecnicoArgos() *auditoria.Caso[EntradaTecnicoArgos, TecnicoArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTecnicoArgos, TecnicoArgos]{
		Accion: "tecnicos.leer_integracion", Permiso: nucleo.PermCuadrillasLeerIntegracion, SinEmpresa: true,
		AuditarExito: soloLecturas[TecnicoArgos],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTecnicoArgos) (TecnicoArgos, error) {
			t.FijarEntidad("usuario", in.UsuarioID)
			// Fuera del alcance, sin ficha o con un vínculo que no salió de este conector: el mismo 404.
			if !empresaEnAlcance(ctx, in.EmpresaID) {
				return TecnicoArgos{}, tecnicoArgosNoEncontrado()
			}
			q := agendabd.New(t)
			actor := contexto.ActorDe(ctx)
			conector, err := q.ConectorDeTokenArgos(ctx, &actor.TokenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return TecnicoArgos{}, tecnicoArgosNoEncontrado()
			}
			if err != nil {
				return TecnicoArgos{}, err
			}
			f, err := q.TecnicoPorUsuario(ctx, in.UsuarioID)
			if errors.Is(err, pgx.ErrNoRows) {
				return TecnicoArgos{}, tecnicoArgosNoEncontrado()
			}
			if err != nil {
				return TecnicoArgos{}, err
			}
			if f.ArgosConectorID == nil || *f.ArgosConectorID != conector {
				return TecnicoArgos{}, tecnicoArgosNoEncontrado()
			}
			empresas, err := q.EmpresasDelUsuario(ctx, f.UsuarioID)
			if err != nil {
				return TecnicoArgos{}, err
			}
			opera := false
			for _, e := range empresas {
				opera = opera || e == in.EmpresaID
			}
			if !opera {
				return TecnicoArgos{}, tecnicoArgosNoEncontrado()
			}
			usuarioActivo, err := q.UsuarioActivo(ctx, f.UsuarioID)
			if err != nil {
				return TecnicoArgos{}, err
			}
			t.FijarEmpresa(in.EmpresaID)
			return TecnicoArgos{TecnicoID: f.ID, UsuarioID: f.UsuarioID, EmpresaID: in.EmpresaID, IDExterno: f.ArgosIDExterno,
				Version: f.VersionVinculo, Activo: f.ArgosIDExterno != nil && f.Activo && usuarioActivo}, nil
		},
	})
}
