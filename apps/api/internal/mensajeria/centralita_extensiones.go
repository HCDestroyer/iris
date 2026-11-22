package mensajeria

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Extensiones de la centralita (contratos/voz.md §3.12): a quién le sale la ventana cuando suena.

// Extension es la extensión de un usuario.
type Extension struct {
	ID            int64  `json:"id"`
	Extension     string `json:"extension"`
	UsuarioID     int64  `json:"usuarioId"`
	UsuarioNombre string `json:"usuarioNombre"`
	// E4c: navegador contesta y llama en el teléfono de Iris.
	Tipo          string     `json:"tipo" enum:"escritorio,navegador"`
	SipUsuario    *string    `json:"sipUsuario" nullable:"true" doc:"Usuario SIP del endpoint (navegador)"`
	SipGuardadoEn *time.Time `json:"sipGuardadoEn" nullable:"true" doc:"Cuándo se guardó la contraseña SIP (nunca se devuelve)"`
	SipProbadoEn  *time.Time `json:"sipProbadoEn" nullable:"true" doc:"La última vez que «Probar registro» pasó"`
	// SipRechazadoEn: la Issabel rechazó la contraseña guardada (se borra cuando una prueba pasa o al guardar otra).
	SipRechazadoEn *time.Time `json:"sipRechazadoEn" nullable:"true" doc:"La última vez que la Issabel rechazó la contraseña guardada; con ella no se vuelve a probar hasta escribir otra"`
}

// ListaExtensiones es GET /centralitas/{id}/extensiones.
type ListaExtensiones struct {
	Data []Extension `json:"data"`
}

// EntradaAsignarExtension es POST /centralitas/{id}/extensiones/asignar.
type EntradaAsignarExtension struct {
	CentralitaID int64  `json:"-"`
	UsuarioID    int64  `json:"usuarioId" minimum:"1"`
	Extension    string `json:"extension" pattern:"^[0-9]{2,8}$" doc:"De 2 a 8 dígitos, como aparece en Issabel"`
}

// EntradaQuitarExtension es POST /centralitas/{id}/extensiones/{extensionId}/quitar.
type EntradaQuitarExtension struct {
	CentralitaID int64 `json:"-"`
	ExtensionID  int64 `json:"-"`
}

func (m *Modulo) centralitaVisible(ctx context.Context, q *mensajeriabd.Queries, id int64) (mensajeriabd.MensajeriaCentralita, error) {
	f, err := q.CentralitaPorID(ctx, mensajeriabd.CentralitaPorIDParams{ID: id, Empresa: empresaDeCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		return f, centralitaNoEncontrada()
	}
	return f, err
}

func nuevosCasosExtensiones(m *Modulo) (*auditoria.Caso[EntradaVerCentralita, ListaExtensiones], *auditoria.Caso[EntradaAsignarExtension, Extension],
	*auditoria.Caso[EntradaQuitarExtension, Extension]) {
	listar := auditoria.Nuevo(auditoria.Caso[EntradaVerCentralita, ListaExtensiones]{
		Accion: "centralita.extensiones", Permiso: nucleo.PermCentralitaAsignarExtension, AuditarExito: sinAuditar[ListaExtensiones],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerCentralita) (ListaExtensiones, error) {
			q := mensajeriabd.New(t)
			if _, err := m.centralitaVisible(ctx, q, in.ID); err != nil {
				return ListaExtensiones{}, err
			}
			fs, err := q.ExtensionesDeCentralita(ctx, in.ID)
			if err != nil {
				return ListaExtensiones{}, err
			}
			out := ListaExtensiones{Data: make([]Extension, 0, len(fs))}
			for _, f := range fs {
				out.Data = append(out.Data, Extension{ID: f.ID, Extension: f.Extension, UsuarioID: f.UsuarioID, UsuarioNombre: f.UsuarioNombre, Tipo: f.Tipo,
					SipUsuario: f.SipUsuario, SipGuardadoEn: f.SipGuardadoEn, SipProbadoEn: f.SipProbadoEn, SipRechazadoEn: f.SipRechazadoEn})
			}
			return out, nil
		},
	})
	asignar := auditoria.Nuevo(auditoria.Caso[EntradaAsignarExtension, Extension]{
		Accion: "centralita.asignar_extension",
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsignarExtension) (Extension, error) {
			if !extensionValida.MatchString(in.Extension) {
				return Extension{}, campoCentralita("extension", "De 2 a 8 dígitos")
			}
			q := mensajeriabd.New(t)
			c, err := m.centralitaVisible(ctx, q, in.CentralitaID)
			if err != nil {
				return Extension{}, err
			}
			// Bloquea la centralita: dos asignaciones a la vez no se cruzan.
			if _, err := q.CentralitaParaActualizar(ctx, mensajeriabd.CentralitaParaActualizarParams{ID: c.ID, Empresa: empresaDeCtx(ctx)}); err != nil {
				return Extension{}, err
			}
			u, err := q.UsuarioDeEmpresaActivo(ctx, mensajeriabd.UsuarioDeEmpresaActivoParams{Usuario: in.UsuarioID, Empresa: derefEmpresa(c.EmpresaID)})
			if errors.Is(err, pgx.ErrNoRows) {
				return Extension{}, nucleo.ReglaNegocio("usuario_no_valido", "Esa persona no existe, no está activa o no es de esta empresa")
			}
			if err != nil {
				return Extension{}, err
			}
			if x, err := q.ExtensionActivaPorNumero(ctx, mensajeriabd.ExtensionActivaPorNumeroParams{CentralitaID: c.ID, Extension: in.Extension}); err == nil {
				if x.UsuarioID == in.UsuarioID {
					return aExtension(x, u.Nombre), nil
				}
				return Extension{}, nucleo.Conflicto("extension_ocupada", "Esa extensión ya es de otra persona: quítasela primero")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return Extension{}, err
			}
			if _, err := q.ExtensionActivaDeUsuario(ctx, mensajeriabd.ExtensionActivaDeUsuarioParams{CentralitaID: c.ID, UsuarioID: in.UsuarioID}); err == nil {
				return Extension{}, nucleo.Conflicto("usuario_con_extension", "Esa persona ya tiene una extensión: quítasela primero")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return Extension{}, err
			}
			x, err := q.InsertarExtension(ctx, mensajeriabd.InsertarExtensionParams{EmpresaID: c.EmpresaID, CentralitaID: c.ID, Extension: in.Extension, UsuarioID: in.UsuarioID})
			if esUnica(err) {
				return Extension{}, nucleo.Conflicto("extension_ocupada", "Esa extensión o esa persona ya tienen asignación")
			}
			if err != nil {
				return Extension{}, err
			}
			t.FijarEntidad("centralita_extension", x.ID)
			t.RegistrarCambios("centralita_extension", x.ID, nil, map[string]any{"extension": x.Extension, "usuario_id": x.UsuarioID})
			if err := notificarMotor(ctx, q, c.ID); err != nil {
				return Extension{}, err
			}
			return aExtension(x, u.Nombre),
				t.Emitir("extension.asignada", map[string]any{"centralita_id": c.ID, "empresa_id": c.EmpresaID, "extension_id": x.ID, "usuario_id": x.UsuarioID})
		},
	})
	quitar := auditoria.Nuevo(auditoria.Caso[EntradaQuitarExtension, Extension]{
		Accion: "centralita.quitar_extension", Permiso: nucleo.PermCentralitaAsignarExtension,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaQuitarExtension) (Extension, error) {
			q := mensajeriabd.New(t)
			c, err := m.centralitaVisible(ctx, q, in.CentralitaID)
			if err != nil {
				return Extension{}, err
			}
			x, err := q.ExtensionPorID(ctx, mensajeriabd.ExtensionPorIDParams{ID: in.ExtensionID, CentralitaID: c.ID})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !x.Activa) {
				return Extension{}, nucleo.NoEncontrado("La extensión no existe")
			}
			if err != nil {
				return Extension{}, err
			}
			if _, err := q.QuitarExtension(ctx, mensajeriabd.QuitarExtensionParams{ID: x.ID, Ahora: m.ahora()}); err != nil {
				return Extension{}, err
			}
			// La credencial SIP del navegador se borra con la extensión (el puente corta en su latido).
			if err := q.BorrarCredencialExtension(ctx, x.ID); err != nil {
				return Extension{}, err
			}
			t.FijarEntidad("centralita_extension", x.ID)
			t.RegistrarCambios("centralita_extension", x.ID, map[string]any{"activa": true}, map[string]any{"activa": false})
			if err := notificarMotor(ctx, q, c.ID); err != nil {
				return Extension{}, err
			}
			return Extension{ID: x.ID, Extension: x.Extension, UsuarioID: x.UsuarioID, Tipo: "escritorio"},
				t.Emitir("extension.quitada", map[string]any{"centralita_id": c.ID, "empresa_id": c.EmpresaID, "extension_id": x.ID, "usuario_id": x.UsuarioID})
		},
	})
	return listar, asignar, quitar
}

// MiExtension es GET /centralita/mi-extension: la extensión del usuario (sin permiso: es suya).
type MiExtension struct {
	Extension    *string `json:"extension" nullable:"true" doc:"Null si no tiene una extensión activa en la centralita activa de la empresa"`
	CentralitaID *int64  `json:"centralitaId" nullable:"true"`
	Estado       *string `json:"estado" nullable:"true" enum:"conectada,desconectada,error,desactivada" doc:"Estado de la conexión con la centralita"`
	Tipo         *string `json:"tipo" nullable:"true" enum:"escritorio,navegador" doc:"navegador: contesta y llama en el teléfono de Iris (E4c)"`
}

func nuevoMiExtension(*Modulo) *auditoria.Caso[struct{}, MiExtension] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, MiExtension]{
		Accion: "centralita.mi_extension", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[MiExtension],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (MiExtension, error) {
			actor := contexto.ActorDe(ctx)
			if actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
				return MiExtension{}, nil
			}
			f, err := mensajeriabd.New(t).MiExtension(ctx, mensajeriabd.MiExtensionParams{Usuario: actor.ID, Empresa: empresaDeCtx(ctx)})
			if errors.Is(err, pgx.ErrNoRows) {
				return MiExtension{}, nil
			}
			if err != nil {
				return MiExtension{}, err
			}
			return MiExtension{Extension: &f.Extension, CentralitaID: &f.CentralitaID, Estado: &f.Estado, Tipo: &f.Tipo}, nil
		},
	})
}
