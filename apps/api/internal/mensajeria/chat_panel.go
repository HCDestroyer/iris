package mensajeria

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Chat del equipo en el panel (contratos/chat.md §2.1–§2.3 y §3.2): menciones, contadores,
// miembros de un hilo de orden (y agregar), el hilo de una orden sin abrirlo y «Nuevo mensaje».

// maxMenciones por mensaje.
const maxMenciones = 20

// Motivos de MiembroChat (contratos/chat.md §2.2).
const (
	MotivoDirecta     = "directa"
	MotivoTecnico     = "tecnico"
	MotivoDespacho    = "despacho"
	MotivoAgregado    = "agregado"
	MotivoSupervision = "supervision"
)

func conversacionCerrada() *nucleo.Problema {
	return nucleo.ReglaNegocio("conversacion_cerrada", "La orden está cerrada, fallida o cancelada: su chat es de solo lectura")
}

// normalizarMenciones quita los repetidos y ordena (el hash del contenido no depende del orden).
func normalizarMenciones(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// validarMenciones: cada mencionado ve la conversación y no es el autor (422 mencion_invalida con
// el campo de cada uno). menciones ya viene normalizada.
func validarMenciones(menciones []int64, miembros []mensajeriabd.MiembrosChatRow, autor int64) error {
	var errs []nucleo.ErrorCampo
	for i, u := range menciones {
		switch {
		case u == autor:
			errs = append(errs, nucleo.ErrorCampo{Field: fmt.Sprintf("menciones[%d]", i), Message: "No puedes mencionarte", Rule: "mencion_invalida"})
		case !slices.ContainsFunc(miembros, func(m mensajeriabd.MiembrosChatRow) bool { return m.UsuarioID == u }):
			errs = append(errs, nucleo.ErrorCampo{Field: fmt.Sprintf("menciones[%d]", i),
				Message: fmt.Sprintf("El usuario %d no está en esta conversación", u), Rule: "mencion_invalida"})
		}
	}
	if len(errs) > 0 {
		return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "mencion_invalida", "Mención inválida",
			"Solo puedes mencionar a quienes están en esta conversación", errs...)
	}
	return nil
}

// patronBusqueda arma el patrón ILIKE de q (escapa % _ y \). Vacío: sin filtro.
func patronBusqueda(q string) *string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	p := "%" + r.Replace(q) + "%"
	return &p
}

// Resumen -------------------------------------------------------------------------------------------

// ContadoresChat son los no leídos de un grupo de conversaciones.
type ContadoresChat struct {
	NoLeidos               int64 `json:"no_leidos" doc:"Mensajes de otros, sin eliminar, después de tu lectura"`
	Menciones              int64 `json:"menciones" doc:"De esos, los que te mencionan"`
	ConversacionesNoLeidas int64 `json:"conversaciones_no_leidas" doc:"Conversaciones con al menos un no leído"`
	RequiereAccion         int64 `json:"requiere_accion" doc:"No leídos de las directas + menciones sin leer en cuadrillas y órdenes: el número que se muestra"`
}

// ResumenChat son los contadores del ícono del encabezado y de las pestañas (contratos/chat.md §2.1).
type ResumenChat struct {
	ContadoresChat
	PorTipo PorTipoChat `json:"por_tipo"`
}

// PorTipoChat son los contadores por tipo de conversación.
type PorTipoChat struct {
	Directa   ContadoresChat `json:"directa"`
	Cuadrilla ContadoresChat `json:"cuadrilla"`
	Orden     ContadoresChat `json:"orden"`
}

// EntradaResumenChat es GET /chat/resumen.
type EntradaResumenChat struct{}

func nuevoResumenChat(m *Modulo) *auditoria.Caso[EntradaResumenChat, ResumenChat] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResumenChat, ResumenChat]{
		Accion: "chat.resumen", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[ResumenChat],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ EntradaResumenChat) (ResumenChat, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return ResumenChat{}, err
			}
			filas, err := mensajeriabd.New(t).ResumenChat(ctx, mensajeriabd.ResumenChatParams{Usuario: a.usuario, Empresa: a.empresa, Todas: a.todas,
				SupGlobal: a.global, SupZonas: a.zonas, SupCuadrillas: a.cuadrillas, OrdGlobal: a.ordGlobal, OrdZonas: a.ordZonas, OrdCuadrillas: a.ordCuadrillas})
			if err != nil {
				return ResumenChat{}, err
			}
			var r ResumenChat
			for _, f := range filas {
				c := ContadoresChat{NoLeidos: f.NoLeidos, Menciones: f.Menciones, ConversacionesNoLeidas: f.ConversacionesNoLeidas, RequiereAccion: f.Menciones}
				switch f.Tipo {
				case TipoDirecta:
					c.RequiereAccion = f.NoLeidos
					r.PorTipo.Directa = c
				case TipoCuadrilla:
					r.PorTipo.Cuadrilla = c
				case TipoOrden:
					r.PorTipo.Orden = c
				}
				r.NoLeidos += c.NoLeidos
				r.Menciones += c.Menciones
				r.ConversacionesNoLeidas += c.ConversacionesNoLeidas
				r.RequiereAccion += c.RequiereAccion
			}
			return r, nil
		},
	})
}

// Miembros ------------------------------------------------------------------------------------------

// MiembroChat es alguien que ve la conversación y por qué.
type MiembroChat struct {
	UsuarioID int64  `json:"usuario_id"`
	Nombre    string `json:"nombre"`
	Motivo    string `json:"motivo" enum:"directa,tecnico,despacho,agregado,supervision" doc:"Por qué está (con varios, el primero de esta lista)"`
}

// MiembrosChat es la lista de miembros (acotada: sin paginar).
type MiembrosChat struct {
	Data []MiembroChat `json:"data"`
	// Nuevo: POST agregó a alguien (201).
	Nuevo bool `json:"-"`
}

func aMiembros(ms []mensajeriabd.MiembrosChatRow) []MiembroChat {
	out := make([]MiembroChat, 0, len(ms))
	for _, mb := range ms {
		out = append(out, MiembroChat{UsuarioID: mb.UsuarioID, Nombre: mb.Nombre, Motivo: mb.Motivo})
	}
	return out
}

func nuevoListarMiembros(m *Modulo) *auditoria.Caso[EntradaConversacion, MiembrosChat] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, MiembrosChat]{
		Accion: "chat.listar_miembros", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[MiembrosChat],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (MiembrosChat, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return MiembrosChat{}, err
			}
			c, err := m.conversacionVisible(ctx, mensajeriabd.New(t), a, in.ID, false)
			if err != nil {
				return MiembrosChat{}, err
			}
			ms, err := m.miembrosDe(ctx, t, c, nil)
			return MiembrosChat{Data: aMiembros(ms)}, err
		},
	})
}

// EntradaAgregarMiembro es POST /chat/conversaciones/{id}/miembros.
type EntradaAgregarMiembro struct {
	ConversacionID int64 `json:"conversacion_id"`
	UsuarioID      int64 `json:"usuario_id"`
}

func nuevoAgregarMiembro(m *Modulo) *auditoria.Caso[EntradaAgregarMiembro, MiembrosChat] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAgregarMiembro, MiembrosChat]{
		Accion: "chat.agregar_miembro", Permiso: nucleo.PermChatSupervisar,
		AuditarExito: func(r MiembrosChat) bool { return r.Nuevo },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAgregarMiembro) (MiembrosChat, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return MiembrosChat{}, err
			}
			q := mensajeriabd.New(t)
			// FOR UPDATE: dos agregados a la vez quedan en fila (la lista y el evento salen exactos).
			c, err := m.conversacionVisible(ctx, q, a, in.ConversacionID, true)
			if err != nil {
				return MiembrosChat{}, err
			}
			t.FijarEntidad("chat_conversacion", c.ID)
			if c.EmpresaID != nil {
				t.FijarEmpresa(*c.EmpresaID)
			} else {
				t.FijarEmpresa(0)
			}
			if c.Tipo != TipoOrden || c.OrdenID == nil {
				return MiembrosChat{}, nucleo.ReglaNegocio("tipo_no_admite_miembros", "Solo se agregan personas al chat de una orden")
			}
			o, err := campobd.New(t).OrdenPorID(ctx, *c.OrdenID)
			if err != nil {
				return MiembrosChat{}, err
			}
			if !a.cubre(o.ZonaID, o.CuadrillaID) {
				return MiembrosChat{}, nucleo.NuevoProblema(http.StatusForbidden, "fuera_de_alcance", "Fuera de tu alcance",
					"Tu permiso chat.supervisar no cubre esta orden (su zona o su cuadrilla)")
			}
			if ordenFinalizada(o.Estado) {
				return MiembrosChat{}, conversacionCerrada()
			}
			if err := usuarioParaChat(ctx, t, in.UsuarioID, a.empresa); err != nil {
				return MiembrosChat{}, err
			}
			ms, err := m.miembrosDe(ctx, t, c, &o)
			if err != nil {
				return MiembrosChat{}, err
			}
			if slices.ContainsFunc(ms, func(mb mensajeriabd.MiembrosChatRow) bool { return mb.UsuarioID == in.UsuarioID }) {
				return MiembrosChat{Data: aMiembros(ms)}, nil
			}
			ahora := m.ahora()
			if _, err := q.AgregarMiembroChat(ctx, mensajeriabd.AgregarMiembroChatParams{ConversacionID: c.ID, UsuarioID: in.UsuarioID,
				AgregadoPor: &a.usuario, Ahora: ahora}); err != nil {
				return MiembrosChat{}, err
			}
			if err := t.Emitir("chat.miembro_agregado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "orden_id": o.ID,
				"usuario_id": in.UsuarioID, "agregado_por": a.usuario}); err != nil {
				return MiembrosChat{}, err
			}
			// Al agregado le aparece el hilo en su feed (y a los demás se les actualiza).
			if err := emitirEventoChat(ctx, t, c, EventoConversacionActualizada, nil, &a.usuario, ahora); err != nil {
				return MiembrosChat{}, err
			}
			if ms, err = m.miembrosDe(ctx, t, c, &o); err != nil {
				return MiembrosChat{}, err
			}
			return MiembrosChat{Data: aMiembros(ms), Nuevo: true}, nil
		},
	})
}

// usuarioParaChat: persona activa, que opera en la empresa y con chat.ver (si no, 422
// usuario_no_disponible).
func usuarioParaChat(ctx context.Context, t *auditoria.Tx, id int64, empresa *int64) error {
	u, err := mensajeriabd.New(t).UsuarioChat(ctx, id)
	if esSinFilas(err) {
		return usuarioNoDisponible()
	}
	if err != nil {
		return err
	}
	if u.Tipo != "persona" || !u.Activo {
		return usuarioNoDisponible()
	}
	ib := identidadbd.New(t)
	if empresa != nil {
		en, err := ib.UsuarioEnEmpresa(ctx, identidadbd.UsuarioEnEmpresaParams{UsuarioID: u.ID, EmpresaID: *empresa})
		if err != nil {
			return err
		}
		if !en {
			return usuarioNoDisponible()
		}
	}
	perms, err := ib.PermisosDeUsuario(ctx, u.ID)
	if err != nil {
		return err
	}
	for _, p := range perms {
		if p.Permiso == string(nucleo.PermChatVer) {
			return nil
		}
	}
	return usuarioNoDisponible()
}

// Hilo de una orden (sin abrirlo) ------------------------------------------------------------------

// EntradaConversacionDeOrden es GET /chat/ordenes/{orden_id}/conversacion.
type EntradaConversacionDeOrden struct {
	OrdenID int64 `path:"orden_id" minimum:"1"`
}

func nuevoConversacionDeOrden(m *Modulo) *auditoria.Caso[EntradaConversacionDeOrden, ConversacionChatDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacionDeOrden, ConversacionChatDetalle]{
		Accion: "chat.ver_conversacion_orden", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[ConversacionChatDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacionDeOrden) (ConversacionChatDetalle, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return ConversacionChatDetalle{}, err
			}
			noEsta := nucleo.NoEncontrado("La orden no tiene chat o no puedes verlo (ábrelo con POST /chat/ordenes/{orden_id}/abrir)")
			q := mensajeriabd.New(t)
			id := in.OrdenID
			c, err := q.ConversacionDeOrden(ctx, &id)
			if esSinFilas(err) {
				return ConversacionChatDetalle{}, noEsta
			}
			if err != nil {
				return ConversacionChatDetalle{}, err
			}
			if _, err := m.conversacionVisible(ctx, q, a, c.ID, false); err != nil {
				return ConversacionChatDetalle{}, noEsta
			}
			return detalle(ctx, q, a.usuario, c.ID)
		},
	})
}

// Nuevo mensaje -------------------------------------------------------------------------------------

// PersonaChat es alguien con quien abrir una directa.
type PersonaChat struct {
	ID             int64  `json:"id"`
	Nombre         string `json:"nombre"`
	ConversacionID *int64 `json:"conversacion_id" nullable:"true" doc:"La directa que ya tienen (o null)"`
}

// EntradaPersonasChat es GET /chat/personas.
type EntradaPersonasChat struct {
	nucleo.ParamsPagina
	Q string `query:"q" maxLength:"100" doc:"Parte del nombre (sin distinguir mayúsculas)"`
}

func nuevoPersonasChat(m *Modulo) *auditoria.Caso[EntradaPersonasChat, nucleo.Pagina[PersonaChat]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPersonasChat, nucleo.Pagina[PersonaChat]]{
		Accion: "chat.listar_personas", Permiso: nucleo.PermChatEnviar, AuditarExito: sinAuditar[nucleo.Pagina[PersonaChat]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPersonasChat) (nucleo.Pagina[PersonaChat], error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return nucleo.Pagina[PersonaChat]{}, err
			}
			var nombre string
			var ultimo int64
			con, err := nucleo.DecodificarCursor(in.Cursor, &nombre, &ultimo)
			if err != nil {
				return nucleo.Pagina[PersonaChat]{}, err
			}
			limite := in.Limite()
			fs, err := mensajeriabd.New(t).PersonasChat(ctx, mensajeriabd.PersonasChatParams{Empresa: a.empresa, Usuario: a.usuario, Patron: patronBusqueda(in.Q),
				ConCursor: con, CursorNombre: nombre, CursorID: ultimo, Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
			if err != nil {
				return nucleo.Pagina[PersonaChat]{}, err
			}
			hayMas := len(fs) > limite
			if hayMas {
				fs = fs[:limite]
			}
			p := nucleo.Pagina[PersonaChat]{Data: make([]PersonaChat, 0, len(fs))}
			for _, f := range fs {
				v := PersonaChat{ID: f.ID, Nombre: f.Nombre}
				if f.ConversacionID != 0 {
					id := f.ConversacionID
					v.ConversacionID = &id
				}
				p.Data = append(p.Data, v)
			}
			if hayMas && len(fs) > 0 {
				u := fs[len(fs)-1]
				cur := nucleo.CodificarCursor(u.Nombre, u.ID)
				p.NextCursor = &cur
			}
			return p, nil
		},
	})
}

// CuadrillaChat es una cuadrilla cuyo chat puedes abrir.
type CuadrillaChat struct {
	ID             int64    `json:"id"`
	Nombre         string   `json:"nombre"`
	Tecnicos       int64    `json:"tecnicos" doc:"Técnicos activos"`
	Zonas          []string `json:"zonas" doc:"Nombres de las zonas que atiende"`
	ConversacionID *int64   `json:"conversacion_id" nullable:"true" doc:"Su chat en la empresa, si ya existe («Ya existe»)"`
}

// CuadrillasChat es la lista (acotada: sin paginar).
type CuadrillasChat struct {
	Data []CuadrillaChat `json:"data"`
}

// EntradaCuadrillasChat es GET /chat/cuadrillas.
type EntradaCuadrillasChat struct {
	Q string `query:"q" maxLength:"100" doc:"Parte del nombre (sin distinguir mayúsculas)"`
}

func nuevoCuadrillasChat(m *Modulo) *auditoria.Caso[EntradaCuadrillasChat, CuadrillasChat] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCuadrillasChat, CuadrillasChat]{
		Accion: "chat.listar_cuadrillas", Permiso: nucleo.PermChatEnviar, AuditarExito: sinAuditar[CuadrillasChat],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCuadrillasChat) (CuadrillasChat, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return CuadrillasChat{}, err
			}
			fs, err := mensajeriabd.New(t).CuadrillasChat(ctx, mensajeriabd.CuadrillasChatParams{Empresa: a.empresa, Patron: patronBusqueda(in.Q),
				SupGlobal: a.global, SupCuadrillas: a.cuadrillas, Usuario: a.usuario, SupZonas: a.zonas})
			if err != nil {
				return CuadrillasChat{}, err
			}
			r := CuadrillasChat{Data: make([]CuadrillaChat, 0, len(fs))}
			for _, f := range fs {
				v := CuadrillaChat{ID: f.ID, Nombre: f.Nombre, Tecnicos: f.Tecnicos, Zonas: f.Zonas}
				if v.Zonas == nil {
					v.Zonas = []string{}
				}
				if f.ConversacionID != 0 {
					id := f.ConversacionID
					v.ConversacionID = &id
				}
				r.Data = append(r.Data, v)
			}
			return r, nil
		},
	})
}
