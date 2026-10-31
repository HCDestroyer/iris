package agenda

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Vínculo del técnico con su usuario de Argos (contrato Iris ↔ ArgosISP 0.5.0 §3.3; lo escribió I1
// sobre la ficha de agenda, coordinado en .claude/integration/i1.md). Con Argos ≥ 1.6.0 los consumos,
// comodatos, devoluciones y recepciones mandan `tecnico: { id_externo }` y Argos usa la bodega activa
// de ese usuario en la empresa de la operación. No depende del vínculo de SSO (se borra al reiniciar
// el MFA o cambiar el correo).

// VinculoArgos es el usuario de Argos vinculado a la ficha.
type VinculoArgos struct {
	IDExterno   string    `json:"idExterno" doc:"Id del usuario en Argos (va como tecnico.id_externo)"`
	Correo      string    `json:"correo" doc:"Correo del usuario en Argos al vincular"`
	Nombre      string    `json:"nombre" doc:"Nombre del usuario en Argos al vincular"`
	VinculadoEn time.Time `json:"vinculadoEn"`
}

func vinculoDe(f agendabd.AgendaTecnico) *VinculoArgos {
	if f.ArgosIDExterno == nil || f.ArgosVinculadoEn == nil {
		return nil
	}
	v := &VinculoArgos{IDExterno: *f.ArgosIDExterno, VinculadoEn: *f.ArgosVinculadoEn}
	if f.ArgosCorreo != nil {
		v.Correo = *f.ArgosCorreo
	}
	if f.ArgosNombre != nil {
		v.Nombre = *f.ArgosNombre
	}
	return v
}

// BodegaArgosVerificada es una bodega técnico del usuario de Argos.
type BodegaArgosVerificada struct {
	ID        int64
	EmpresaID int64
	Activa    bool
}

// TecnicoArgosVerificado es el usuario de Argos tal como lo devolvió GET /inventario/tecnicos.
type TecnicoArgosVerificado struct {
	IDExterno string
	Correo    string
	Nombre    string
	Bodegas   []BodegaArgosVerificada
}

// TecnicosArgos verifica un usuario de Argos (lo implementa el conector, I1). Se llama fuera de
// transacción. Los errores ya son *nucleo.Problema (422 argos_sin_inventario_tecnico, 422
// tecnico_no_encontrado, 503 conector_apagado, 503 argos_no_disponible…).
type TecnicosArgos interface {
	BuscarTecnico(ctx context.Context, idExterno string) (TecnicoArgosVerificado, error)
}

// TecnicosDeArgos adapta el conector de Argos.
func TecnicosDeArgos(a *argos.Modulo) TecnicosArgos { return tecnicosArgos{a: a} }

type tecnicosArgos struct{ a *argos.Modulo }

func (t tecnicosArgos) BuscarTecnico(ctx context.Context, idExterno string) (TecnicoArgosVerificado, error) {
	x, err := t.a.BuscarTecnico(ctx, idExterno)
	switch {
	case errors.Is(err, argos.ErrSinInventarioTecnico):
		return TecnicoArgosVerificado{}, argos.SinInventarioTecnico()
	case errors.Is(err, argos.ErrTecnicoNoEncontrado):
		return TecnicoArgosVerificado{}, nucleo.ReglaNegocio("tecnico_no_encontrado",
			"Argos no tiene un técnico con bodega con ese id (GET /inventario/tecnicos)")
	case err != nil:
		return TecnicoArgosVerificado{}, argos.Problema(err)
	}
	v := TecnicoArgosVerificado{IDExterno: string(x.IDExterno), Correo: x.Correo, Nombre: x.Nombre}
	for _, b := range x.Bodegas {
		v.Bodegas = append(v.Bodegas, BodegaArgosVerificada{ID: b.ID, EmpresaID: b.EmpresaID, Activa: b.EsActiva()})
	}
	return v, nil
}

// EntradaVincularArgos vincula la ficha con un usuario de Argos.
type EntradaVincularArgos struct {
	ID        int64  `json:"-"`
	Version   int32  `json:"version" minimum:"1" doc:"Versión leída de la ficha; si cambió, 409"`
	IDExterno string `json:"idExterno" minLength:"1" maxLength:"100" doc:"idExterno de GET /integraciones/argosisp/tecnicos"`
}

// EntradaDesvincularArgos quita el vínculo.
type EntradaDesvincularArgos struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída de la ficha; si cambió, 409"`
	Motivo  string `json:"motivo,omitempty" maxLength:"500"`
}

type entradaGuardarVinculo struct {
	EntradaVincularArgos
	Correo string `json:"correo"`
	Nombre string `json:"nombre"`
}

// VincularArgos: permiso y ficha (fase corta), verificación en Argos fuera de transacción y vínculo
// en un caso de uso (auditado, tecnico.argos_vinculado).
type VincularArgos struct {
	m       *Modulo
	previo  *auditoria.Caso[EntradaVincularArgos, struct{}]
	guardar *auditoria.Caso[entradaGuardarVinculo, TecnicoDetalle]
}

func vinculoTecnicoInvalido() *nucleo.Problema {
	return nucleo.ReglaNegocio("tecnico_sin_bodega", "El usuario de Argos no tiene bodega técnico activa en la empresa activa")
}

func (m *Modulo) nuevoVincularArgos() *VincularArgos {
	v := &VincularArgos{m: m}
	v.previo = auditoria.Nuevo(auditoria.Caso[EntradaVincularArgos, struct{}]{
		Accion: "agenda.vincular_argos", Permiso: nucleo.PermAgendaVincularArgos, AuditarExito: soloLecturas[struct{}],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVincularArgos) (struct{}, error) {
			_, err := m.tomarTecnico(ctx, t, agendabd.New(t), in.ID, nucleo.PermAgendaVincularArgos, &in.Version)
			if err == nil && m.cfg.Argos == nil {
				err = argos.ConectorApagado()
			}
			return struct{}{}, err
		},
	})
	v.guardar = auditoria.Nuevo(auditoria.Caso[entradaGuardarVinculo, TecnicoDetalle]{
		Accion: "agenda.vincular_argos", Permiso: nucleo.PermAgendaVincularArgos,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarVinculo) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			antes, err := m.tomarTecnico(ctx, t, q, in.ID, nucleo.PermAgendaVincularArgos, &in.Version)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if antes.ArgosIDExterno != nil && *antes.ArgosIDExterno == in.IDExterno {
				return TecnicoDetalle{}, nucleo.ReglaNegocio("sin_cambios", "La ficha ya está vinculada a ese usuario de Argos")
			}
			f, err := q.VincularArgosTecnico(ctx, agendabd.VincularArgosTecnicoParams{
				IDExterno: &in.IDExterno, Correo: textoNulo(in.Correo, 320), Nombre: textoNulo(in.Nombre, 200),
				VinculadoEn: ptr(m.ahora()), VinculadoPor: actorUsuario(t), ID: antes.ID, Version: antes.Version,
			})
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return TecnicoDetalle{}, nucleo.NuevoProblema(http.StatusConflict, "argos_vinculado_a_otro", "Usuario de Argos ya vinculado",
					"Ese usuario de Argos ya está vinculado a otra ficha de técnico: desvincúlala primero")
			}
			if err != nil {
				return TecnicoDetalle{}, nucleo.VerificarVersion(err, "tecnico", antes.ID)
			}
			t.RegistrarCambios("tecnico", f.ID, vinculoAuditable(antes), vinculoAuditable(f))
			if err := t.Emitir("tecnico.argos_vinculado", map[string]any{
				"tecnico_id": f.ID, "usuario_id": f.UsuarioID, "id_externo": in.IDExterno, "anterior_id_externo": antes.ArgosIDExterno,
			}); err != nil {
				return TecnicoDetalle{}, err
			}
			return m.detalleDe(ctx, q, f)
		},
	})
	return v
}

// vinculoAuditable: lo que se audita del vínculo (el correo de Argos es dato personal: solo si cambió).
func vinculoAuditable(f agendabd.AgendaTecnico) map[string]any {
	return map[string]any{"argos_id_externo": f.ArgosIDExterno, "argos_correo": f.ArgosCorreo, "argos_vinculado_en": f.ArgosVinculadoEn}
}

func textoNulo(s string, maximo int) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if r := []rune(s); len(r) > maximo {
		s = string(r[:maximo])
	}
	return &s
}

// Ejecutar vincula la ficha con el usuario de Argos, que debe existir en GET /inventario/tecnicos
// con bodega técnico activa en la empresa activa (sin empresa activa: en alguna).
func (v *VincularArgos) Ejecutar(ctx context.Context, in EntradaVincularArgos) (TecnicoDetalle, error) {
	in.IDExterno = strings.TrimSpace(in.IDExterno)
	if in.IDExterno == "" {
		return TecnicoDetalle{}, campoObligatorio("idExterno")
	}
	if _, err := v.previo.Ejecutar(ctx, in); err != nil {
		return TecnicoDetalle{}, err
	}
	x, err := v.m.cfg.Argos.BuscarTecnico(ctx, in.IDExterno)
	if err != nil {
		return TecnicoDetalle{}, err
	}
	empresa := contexto.EmpresaDe(ctx)
	conBodega := false
	for _, b := range x.Bodegas {
		if b.Activa && (empresa == 0 || b.EmpresaID == empresa) {
			conBodega = true
		}
	}
	if !conBodega {
		return TecnicoDetalle{}, vinculoTecnicoInvalido()
	}
	return v.guardar.Ejecutar(ctx, entradaGuardarVinculo{EntradaVincularArgos: in, Correo: x.Correo, Nombre: x.Nombre})
}

func (m *Modulo) nuevoDesvincularArgos() *auditoria.Caso[EntradaDesvincularArgos, TecnicoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDesvincularArgos, TecnicoDetalle]{
		Accion: "agenda.desvincular_argos", Permiso: nucleo.PermAgendaDesvincularArgos,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDesvincularArgos) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			antes, err := m.tomarTecnico(ctx, t, q, in.ID, nucleo.PermAgendaDesvincularArgos, &in.Version)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if antes.ArgosIDExterno == nil {
				return TecnicoDetalle{}, nucleo.ReglaNegocio("sin_cambios", "La ficha no está vinculada a un usuario de Argos")
			}
			f, err := q.DesvincularArgosTecnico(ctx, agendabd.DesvincularArgosTecnicoParams{ID: antes.ID, Version: antes.Version})
			if err != nil {
				return TecnicoDetalle{}, nucleo.VerificarVersion(err, "tecnico", antes.ID)
			}
			t.RegistrarCambios("tecnico", f.ID, vinculoAuditable(antes), vinculoAuditable(f))
			var motivo *string
			if s := strings.TrimSpace(in.Motivo); s != "" {
				motivo = &s
			}
			if err := t.Emitir("tecnico.argos_desvinculado", map[string]any{
				"tecnico_id": f.ID, "usuario_id": f.UsuarioID, "id_externo": *antes.ArgosIDExterno, "motivo": motivo,
			}); err != nil {
				return TecnicoDetalle{}, err
			}
			return m.detalleDe(ctx, q, f)
		},
	})
}
