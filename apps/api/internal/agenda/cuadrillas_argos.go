package agenda

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Bodega de cuadrilla (contrato Iris ↔ ArgosISP 0.11.0 §3.9, pedido B26). Iris es la fuente de la
// membresía: cada cuadrilla.creada / cuadrilla.actualizada lleva un solo empresa_id, la versión de la
// cuadrilla (Argos solo aplica una mayor), los miembros completos y el jefe. Una cuadrilla sin empresa
// (catálogo común) atiende todas: sale un evento por cada empresa registrada, con el id_externo solo
// de los miembros que operan en ella. La versión sube y el evento sale también cuando cambia un
// miembro: su ficha entra o sale de la cuadrilla, se activa o desactiva, cambia su vínculo I1 o se
// desactiva su usuario. Si el jefe deja de ser miembro con vínculo I1, la cuadrilla queda sin jefe.

// MiembroArgos es un miembro de la cuadrilla tal como viaja a Argos.
type MiembroArgos struct {
	UsuarioID int64   `json:"usuario_id"`
	IDExterno *string `json:"id_externo" nullable:"true" doc:"Usuario de Argos (vínculo I1); null sin vínculo. Solo van los miembros que operan en la empresa"`
}

// CuadrillaArgos es la cuadrilla para conciliar la bodega en Argos (GET
// /integraciones/argosisp/cuadrillas/{id}). Los nombres son los del contrato (snake_case).
type CuadrillaArgos struct {
	CuadrillaID   int64          `json:"cuadrilla_id"`
	EmpresaID     *int64         `json:"empresa_id" nullable:"true"`
	Version       int32          `json:"version" doc:"Versión para Argos: entera y creciente; solo aplica una mayor que la última"`
	Activa        bool           `json:"activa"`
	Miembros      []MiembroArgos `json:"miembros"`
	JefeUsuarioID *int64         `json:"jefe_usuario_id" nullable:"true"`
}

// miembrosParaArgos: miembros activos de la cuadrilla. Con empresa, solo los que operan en ella
// (asignada o empresas.todas global): a Argos de una empresa nunca llegan usuarios de otra (B-265-1).
// nil = sin empresa (modo sin conector, o la revisión interna del jefe): todos, con su vínculo.
func miembrosParaArgos(ctx context.Context, q *agendabd.Queries, cuadrilla int64, empresa *int64) ([]MiembroArgos, []agendabd.MiembrosDeCuadrillaRow, error) {
	fs, err := q.MiembrosDeCuadrilla(ctx, agendabd.MiembrosDeCuadrillaParams{CuadrillaID: &cuadrilla, EmpresaID: empresa})
	if err != nil {
		return nil, nil, err
	}
	salida := make([]MiembroArgos, 0, len(fs))
	for _, f := range fs {
		if !f.OperaEnEmpresa {
			continue
		}
		salida = append(salida, MiembroArgos{UsuarioID: f.UsuarioID, IDExterno: f.ArgosIDExterno})
	}
	return salida, fs, nil
}

// jefeEnMiembros: el jefe solo si está entre los miembros de esa empresa con vínculo I1; si no, null.
func jefeEnMiembros(jefe *int64, miembros []MiembroArgos) *int64 {
	if jefe == nil {
		return nil
	}
	for _, m := range miembros {
		if m.UsuarioID == *jefe && m.IDExterno != nil {
			return jefe
		}
	}
	return nil
}

// jefeValido: el usuario es miembro activo de la cuadrilla con vínculo I1.
func jefeValido(fs []agendabd.MiembrosDeCuadrillaRow, usuario int64) bool {
	return slices.ContainsFunc(fs, func(f agendabd.MiembrosDeCuadrillaRow) bool {
		return f.UsuarioID == usuario && f.ArgosIDExterno != nil
	})
}

// empresasDeCuadrilla: la suya o, sin empresa, todas las registradas (ninguna: modo sin conector,
// un solo evento con empresa null que no va a Argos).
func empresasDeCuadrilla(ctx context.Context, q *agendabd.Queries, c agendabd.AgendaCuadrilla) ([]*int64, error) {
	if c.EmpresaID != nil {
		return []*int64{c.EmpresaID}, nil
	}
	ids, err := q.EmpresasRegistradas(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []*int64{nil}, nil
	}
	salida := make([]*int64, 0, len(ids))
	for _, id := range ids {
		salida = append(salida, &id)
	}
	return salida, nil
}

// emitirCuadrilla emite cuadrilla.creada o cuadrilla.actualizada, uno por empresa, con la versión,
// los miembros completos y el jefe (más los campos propios de cada evento).
func emitirCuadrilla(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, c agendabd.AgendaCuadrilla, evento string, extra map[string]any) error {
	empresas, err := empresasDeCuadrilla(ctx, q, c)
	if err != nil {
		return err
	}
	for _, empresa := range empresas {
		miembros, _, err := miembrosParaArgos(ctx, q, c.ID, empresa)
		if err != nil {
			return err
		}
		p := map[string]any{
			"cuadrilla_id": c.ID, "empresa_id": empresa, "version": c.VersionArgos, "nombre": c.Nombre,
			"activa": c.Activa, "miembros": miembros, "jefe_usuario_id": jefeEnMiembros(c.JefeUsuarioID, miembros),
		}
		for k, v := range extra {
			p[k] = v
		}
		if err := t.EmitirEnEmpresa(evento, empresa, p); err != nil {
			return err
		}
	}
	return nil
}

// revisarCuadrillas: cambió un miembro de estas cuadrillas. Cada una (en orden de id, para no
// cruzar candados) sube su versión para Argos (la del panel no), pierde al jefe si ya no es miembro con vínculo I1 y emite
// cuadrilla.actualizada con sus miembros completos.
func revisarCuadrillas(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, ids []*int64) error {
	unicas := []int64{}
	for _, id := range ids {
		if id != nil && !slices.Contains(unicas, *id) {
			unicas = append(unicas, *id)
		}
	}
	slices.Sort(unicas)
	for _, id := range unicas {
		c, err := q.CuadrillaPorIDParaActualizar(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		_, fs, err := miembrosParaArgos(ctx, q, id, nil)
		if err != nil {
			return err
		}
		campos := []string{"miembros"}
		jefe := c.JefeUsuarioID
		if jefe != nil && !jefeValido(fs, *jefe) {
			jefe = nil
			campos = append(campos, "jefe")
		}
		f, err := q.RevisarCuadrilla(ctx, agendabd.RevisarCuadrillaParams{ID: id, JefeUsuarioID: jefe})
		if err != nil {
			return err
		}
		if !mismoID(c.JefeUsuarioID, f.JefeUsuarioID) {
			t.RegistrarCambios("cuadrilla", id, map[string]any{"jefe_usuario_id": c.JefeUsuarioID}, map[string]any{"jefe_usuario_id": f.JefeUsuarioID})
		}
		if err := emitirCuadrilla(ctx, t, q, f, "cuadrilla.actualizada", map[string]any{"campos": campos}); err != nil {
			return err
		}
	}
	return nil
}

// EntradaAsignarJefe asigna (o quita, con null) el jefe de la cuadrilla.
type EntradaAsignarJefe struct {
	ID            int64  `json:"-"`
	Version       int32  `json:"version" minimum:"1" doc:"Versión leída de la cuadrilla; si cambió, 409"`
	JefeUsuarioID *int64 `json:"jefeUsuarioId" nullable:"true" doc:"Usuario miembro con vínculo I1 con Argos; null = la cuadrilla queda sin jefe"`
}

func (m *Modulo) nuevoAsignarJefe() *auditoria.Caso[EntradaAsignarJefe, Cuadrilla] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAsignarJefe, Cuadrilla]{
		Accion: "cuadrillas.asignar_jefe", Permiso: nucleo.PermCuadrillasAsignarJefe,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsignarJefe) (Cuadrilla, error) {
			q := agendabd.New(t)
			antes, err := tomarCuadrilla(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return Cuadrilla{}, err
			}
			if ok, err := alcanceDe(ctx, nucleo.PermCuadrillasAsignarJefe).cubreCuadrilla(ctx, q, &antes.ID); err != nil || !ok {
				if err != nil {
					return Cuadrilla{}, err
				}
				return Cuadrilla{}, fueraDeAlcance("Tu permiso cuadrillas.asignar_jefe no cubre esta cuadrilla")
			}
			if in.JefeUsuarioID != nil {
				// Nadie se asigna a sí mismo (§10.6 d): el jefe firma por el material de todos.
				if a := t.Actor(); a.EsUsuario() && a.ID == *in.JefeUsuarioID {
					return Cuadrilla{}, nucleo.NuevoProblema(http.StatusForbidden, "autoasignacion", "No puedes asignarte",
						"Nadie puede asignarse a sí mismo como jefe de cuadrilla: pídeselo a otro supervisor o a un administrador")
				}
				_, fs, err := miembrosParaArgos(ctx, q, antes.ID, nil)
				if err != nil {
					return Cuadrilla{}, err
				}
				if !jefeValido(fs, *in.JefeUsuarioID) {
					return Cuadrilla{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "jefe_no_miembro", "El jefe debe ser miembro",
						"El jefe debe ser un técnico activo de la cuadrilla con su ficha vinculada a Argos")
				}
			}
			if mismoID(antes.JefeUsuarioID, in.JefeUsuarioID) {
				return Cuadrilla{}, nucleo.ReglaNegocio("sin_cambios", "La cuadrilla ya tiene ese jefe")
			}
			f, err := q.FijarJefeCuadrilla(ctx, agendabd.FijarJefeCuadrillaParams{ID: antes.ID, Version: antes.Version, JefeUsuarioID: in.JefeUsuarioID})
			if err != nil {
				return Cuadrilla{}, nucleo.VerificarVersion(err, "cuadrilla", antes.ID)
			}
			t.RegistrarCambios("cuadrilla", f.ID, map[string]any{"jefe_usuario_id": antes.JefeUsuarioID}, map[string]any{"jefe_usuario_id": f.JefeUsuarioID})
			if err := emitirCuadrilla(ctx, t, q, f, "cuadrilla.actualizada", map[string]any{"campos": []string{"jefe"}}); err != nil {
				return Cuadrilla{}, err
			}
			return cuadrillaDe(ctx, q, f)
		},
	})
}

// EntradaCuadrillaArgos: GET /integraciones/argosisp/cuadrillas/{id}?empresa_id=.
type EntradaCuadrillaArgos struct {
	ID        int64 `path:"id"`
	EmpresaID int64 `query:"empresa_id" required:"true" minimum:"1" doc:"Empresa de la bodega de cuadrilla (una por empresa)"`
}

func cuadrillaArgosNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La cuadrilla no existe")
}

// empresaEnAlcance: el actor (el token de Argos) opera en esa empresa.
func empresaEnAlcance(ctx context.Context, empresa int64) bool {
	e := contexto.EmpresasDe(ctx)
	if e.Estado == contexto.EmpresaNoResuelta && !contexto.ActorDe(ctx).EsUsuario() {
		return true // sistema (pruebas, consola)
	}
	return e.Todas || slices.Contains(e.Disponibles, empresa)
}

func (m *Modulo) nuevoCuadrillaArgos() *auditoria.Caso[EntradaCuadrillaArgos, CuadrillaArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCuadrillaArgos, CuadrillaArgos]{
		Accion: "cuadrillas.leer_integracion", Permiso: nucleo.PermCuadrillasLeerIntegracion, SinEmpresa: true,
		// Se lee para conciliar; no deja fila por lectura (como las demás lecturas de agenda).
		AuditarExito: soloLecturas[CuadrillaArgos],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCuadrillaArgos) (CuadrillaArgos, error) {
			t.FijarEntidad("cuadrilla", in.ID)
			// Fuera del alcance del conector, la misma respuesta que una que no existe.
			if !empresaEnAlcance(ctx, in.EmpresaID) {
				return CuadrillaArgos{}, cuadrillaArgosNoEncontrada()
			}
			q := agendabd.New(t)
			c, err := q.CuadrillaPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return CuadrillaArgos{}, cuadrillaArgosNoEncontrada()
			}
			if err != nil {
				return CuadrillaArgos{}, err
			}
			if c.EmpresaID != nil && *c.EmpresaID != in.EmpresaID {
				return CuadrillaArgos{}, cuadrillaArgosNoEncontrada()
			}
			if c.EmpresaID == nil {
				// Sin empresa: atiende las registradas; una que no existe no tiene bodega.
				empresas, err := q.EmpresasRegistradas(ctx)
				if err != nil {
					return CuadrillaArgos{}, err
				}
				if !slices.Contains(empresas, in.EmpresaID) {
					return CuadrillaArgos{}, cuadrillaArgosNoEncontrada()
				}
			}
			t.FijarEmpresa(in.EmpresaID)
			miembros, _, err := miembrosParaArgos(ctx, q, c.ID, &in.EmpresaID)
			if err != nil {
				return CuadrillaArgos{}, err
			}
			return CuadrillaArgos{CuadrillaID: c.ID, EmpresaID: &in.EmpresaID, Version: c.VersionArgos, Activa: c.Activa,
				Miembros: miembros, JefeUsuarioID: jefeEnMiembros(c.JefeUsuarioID, miembros)}, nil
		},
	})
}

// EntradaRevisarUsuario: cambió el usuario de un miembro (desactivado, o se le asignó o quitó una
// empresa): sus cuadrillas avisan a Argos.
type EntradaRevisarUsuario struct {
	UsuarioID int64 `json:"usuario_id"`
}

// ResultadoRevisarUsuario son las cuadrillas que se revisaron.
type ResultadoRevisarUsuario struct {
	Cuadrillas []int64 `json:"cuadrillas"`
}

func (m *Modulo) nuevoRevisarUsuario() *auditoria.Caso[EntradaRevisarUsuario, ResultadoRevisarUsuario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRevisarUsuario, ResultadoRevisarUsuario]{
		Accion: "agenda.revisar_cuadrillas_usuario", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoRevisarUsuario) bool { return len(r.Cuadrillas) > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRevisarUsuario) (ResultadoRevisarUsuario, error) {
			if !esSistema(ctx) {
				return ResultadoRevisarUsuario{}, nucleo.Prohibido("Solo el sistema revisa las cuadrillas de un usuario")
			}
			t.FijarEntidad("usuario", in.UsuarioID)
			q := agendabd.New(t)
			ids := []*int64{}
			f, err := q.TecnicoPorUsuario(ctx, in.UsuarioID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return ResultadoRevisarUsuario{}, err
			default:
				ids = append(ids, f.CuadrillaID)
			}
			jefe, err := q.CuadrillasConJefe(ctx, &in.UsuarioID)
			if err != nil {
				return ResultadoRevisarUsuario{}, err
			}
			for _, id := range jefe {
				ids = append(ids, &id)
			}
			r := ResultadoRevisarUsuario{Cuadrillas: []int64{}}
			for _, id := range ids {
				if id != nil && !slices.Contains(r.Cuadrillas, *id) {
					r.Cuadrillas = append(r.Cuadrillas, *id)
				}
			}
			return r, revisarCuadrillas(ctx, t, q, ids)
		},
	})
}

// EventosUsuarioCuadrilla son los eventos del usuario que cambian lo que se manda de una cuadrilla.
var EventosUsuarioCuadrilla = []string{"usuario.desactivado", "usuario.empresa_asignada", "usuario.empresa_quitada"}

// SuscribirCuadrillas registra en el bus del relay (cmd/worker) la revisión de las cuadrillas de un
// usuario que cambió. Un evento repetido solo sube otra vez la versión, con los mismos miembros.
func (m *Modulo) SuscribirCuadrillas(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	for _, ev := range EventosUsuarioCuadrilla {
		bus.Suscribir("agenda.cuadrillas_usuario", ev, func(ctx context.Context, e integracion.Evento) error {
			var in EntradaRevisarUsuario
			if err := json.Unmarshal(e.Payload, &in); err != nil || in.UsuarioID == 0 {
				log.ErrorContext(ctx, "agenda: evento de usuario ilegible", "evento", e.ID, "nombre", e.Evento)
				return nil
			}
			_, err := m.RevisarUsuario.Ejecutar(nucleo.ConBD(ctx, bd), in)
			return err
		})
	}
}
