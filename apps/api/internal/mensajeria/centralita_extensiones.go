package mensajeria

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Extensiones de la centralita (contratos/voz.md §3.12): a quién le sale la ventana cuando suena.

// Extension es la extensión de un usuario.
type Extension struct {
	ID            int64  `json:"id"`
	Extension     string `json:"extension"`
	UsuarioID     int64  `json:"usuarioId"`
	UsuarioNombre string `json:"usuarioNombre"`
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
				out.Data = append(out.Data, Extension{ID: f.ID, Extension: f.Extension, UsuarioID: f.UsuarioID, UsuarioNombre: f.UsuarioNombre})
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
					return Extension{ID: x.ID, Extension: x.Extension, UsuarioID: x.UsuarioID, UsuarioNombre: u.Nombre}, nil
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
			return Extension{ID: x.ID, Extension: x.Extension, UsuarioID: x.UsuarioID, UsuarioNombre: u.Nombre},
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
			t.FijarEntidad("centralita_extension", x.ID)
			t.RegistrarCambios("centralita_extension", x.ID, map[string]any{"activa": true}, map[string]any{"activa": false})
			if err := notificarMotor(ctx, q, c.ID); err != nil {
				return Extension{}, err
			}
			return Extension{ID: x.ID, Extension: x.Extension, UsuarioID: x.UsuarioID},
				t.Emitir("extension.quitada", map[string]any{"centralita_id": c.ID, "empresa_id": c.EmpresaID, "extension_id": x.ID, "usuario_id": x.UsuarioID})
		},
	})
	return listar, asignar, quitar
}
