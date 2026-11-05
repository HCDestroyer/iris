package mensajeria

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Casos de uso del chat (contratos/chat.md). Lecturas sin fila de auditoría (AuditarExito); se
// auditan abrir una conversación nueva, enviar y eliminar (con el id, nunca el texto).

func sinAuditar[T any](T) bool { return false }

// casosChat reúne los casos del chat.
type casosChat struct {
	ListarConversaciones *auditoria.Caso[EntradaListarConversaciones, nucleo.Pagina[ConversacionChat]]
	VerConversacion      *auditoria.Caso[EntradaConversacion, ConversacionChatDetalle]
	AbrirDirecta         *auditoria.Caso[EntradaAbrirDirecta, ConversacionChatAbierta]
	AbrirCuadrilla       *auditoria.Caso[EntradaAbrirCuadrilla, ConversacionChatAbierta]
	AbrirOrden           *auditoria.Caso[EntradaAbrirOrden, ConversacionChatAbierta]
	EnviarMensaje        *auditoria.Caso[EntradaEnviarMensaje, MensajeEnviado]
	EliminarMensaje      *auditoria.Caso[EntradaEliminarMensaje, MensajeEliminado]
	MarcarLeido          *auditoria.Caso[EntradaMarcarLeido, LecturaPropia]
	ListarMensajes       *auditoria.Caso[EntradaListarMensajes, nucleo.Pagina[MensajeChat]]
	Cambios              *auditoria.Caso[EntradaCambios, CambiosChat]
	abrirFlujo           *auditoria.Caso[entradaFlujo, flujo]
}

func nuevosCasosChat(m *Modulo) casosChat {
	return casosChat{
		ListarConversaciones: nuevoListarConversaciones(m),
		VerConversacion:      nuevoVerConversacion(m),
		AbrirDirecta:         nuevoAbrirDirecta(m),
		AbrirCuadrilla:       nuevoAbrirCuadrilla(m),
		AbrirOrden:           nuevoAbrirOrden(m),
		EnviarMensaje:        nuevoEnviarMensaje(m),
		EliminarMensaje:      nuevoEliminarMensaje(m),
		MarcarLeido:          nuevoMarcarLeido(m),
		ListarMensajes:       nuevoListarMensajes(m),
		Cambios:              nuevoCambios(m),
		abrirFlujo:           nuevoAbrirFlujo(m),
	}
}

// Listar y ver --------------------------------------------------------------------------------------

// EntradaListarConversaciones es GET /chat/conversaciones.
type EntradaListarConversaciones struct {
	nucleo.ParamsPagina
	Tipo string `query:"tipo" enum:"directa,cuadrilla,orden" doc:"Solo de ese tipo"`
}

func nuevoListarConversaciones(m *Modulo) *auditoria.Caso[EntradaListarConversaciones, nucleo.Pagina[ConversacionChat]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarConversaciones, nucleo.Pagina[ConversacionChat]]{
		Accion: "chat.listar_conversaciones", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[nucleo.Pagina[ConversacionChat]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarConversaciones) (nucleo.Pagina[ConversacionChat], error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return nucleo.Pagina[ConversacionChat]{}, err
			}
			var actividad string
			var ultimo int64
			con, err := nucleo.DecodificarCursor(in.Cursor, &actividad, &ultimo)
			if err != nil {
				return nucleo.Pagina[ConversacionChat]{}, err
			}
			var cursorAct time.Time
			if con {
				if cursorAct, err = time.Parse(time.RFC3339Nano, actividad); err != nil {
					return nucleo.Pagina[ConversacionChat]{}, campoChat("cursor", "Cursor inválido", "cursor")
				}
			}
			var tipo *string
			if in.Tipo != "" {
				tipo = &in.Tipo
			}
			limite := in.Limite()
			q := mensajeriabd.New(t)
			filas, err := q.ConversacionesVisibles(ctx, mensajeriabd.ConversacionesVisiblesParams{Usuario: a.usuario, Empresa: a.empresa, Todas: a.todas,
				SupGlobal: a.global, SupZonas: a.zonas, SupCuadrillas: a.cuadrillas, Tipo: tipo, ConCursor: con, CursorActividad: cursorAct,
				CursorID: ultimo, Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
			if err != nil {
				return nucleo.Pagina[ConversacionChat]{}, err
			}
			hayMas := len(filas) > limite
			if hayMas {
				filas = filas[:limite]
			}
			ids := make([]int64, 0, len(filas))
			for _, f := range filas {
				ids = append(ids, f.ID)
			}
			cs, err := resumir(ctx, q, a.usuario, ids)
			if err != nil {
				return nucleo.Pagina[ConversacionChat]{}, err
			}
			p := nucleo.Pagina[ConversacionChat]{Data: cs}
			if hayMas && len(filas) > 0 {
				u := filas[len(filas)-1]
				c := nucleo.CodificarCursor(u.UltimaActividad.UTC().Format(time.RFC3339Nano), u.ID)
				p.NextCursor = &c
			}
			return p, nil
		},
	})
}

// EntradaConversacion es una conversación por id.
type EntradaConversacion struct {
	ID int64 `path:"id" minimum:"1"`
}

func nuevoVerConversacion(m *Modulo) *auditoria.Caso[EntradaConversacion, ConversacionChatDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, ConversacionChatDetalle]{
		Accion: "chat.ver_conversacion", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[ConversacionChatDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (ConversacionChatDetalle, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return ConversacionChatDetalle{}, err
			}
			q := mensajeriabd.New(t)
			if _, err := m.conversacionVisible(ctx, q, a, in.ID, false); err != nil {
				return ConversacionChatDetalle{}, err
			}
			return detalle(ctx, q, a.usuario, in.ID)
		},
	})
}

func detalle(ctx context.Context, q *mensajeriabd.Queries, usuario, id int64) (ConversacionChatDetalle, error) {
	c, err := resumirUna(ctx, q, usuario, id)
	if err != nil {
		return ConversacionChatDetalle{}, err
	}
	ls, err := q.LecturasDeConversacion(ctx, id)
	if err != nil {
		return ConversacionChatDetalle{}, err
	}
	d := ConversacionChatDetalle{ConversacionChat: c, Lecturas: make([]LecturaMiembroChat, 0, len(ls))}
	for _, l := range ls {
		d.Lecturas = append(d.Lecturas, LecturaMiembroChat{UsuarioID: l.UsuarioID, Nombre: l.Nombre, HastaMensajeID: l.HastaMensajeID, HastaSeq: l.HastaSeq, LeidoEn: l.LeidoEn})
	}
	return d, nil
}

// Abrir ---------------------------------------------------------------------------------------------

// ConversacionChatAbierta es la respuesta de abrir (201 si se creó o te uniste, 200 si ya estaba).
type ConversacionChatAbierta struct {
	ConversacionChatDetalle
	Nueva bool `json:"-"`
}

// EntradaAbrirDirecta es POST /chat/directas.
type EntradaAbrirDirecta struct {
	UsuarioID int64 `json:"usuario_id" minimum:"1" doc:"Con quién (un usuario persona, activo, de la empresa activa y con chat.ver)"`
}

// EntradaAbrirCuadrilla es POST /chat/cuadrillas/{cuadrilla_id}/abrir.
type EntradaAbrirCuadrilla struct {
	CuadrillaID int64 `json:"cuadrilla_id"`
}

// EntradaAbrirOrden es POST /chat/ordenes/{orden_id}/abrir.
type EntradaAbrirOrden struct {
	OrdenID int64 `json:"orden_id"`
}

func usuarioNoDisponible() *nucleo.Problema {
	return nucleo.ReglaNegocio("usuario_no_disponible", "Ese usuario no existe, no está activo, no opera en esta empresa o no usa el chat")
}

// guardarConversacion crea la conversación (o lee la que otra petición creó a la vez) y, si es
// nueva, emite chat.conversacion_creada. Los miembros explícitos se agregan aparte.
func (m *Modulo) guardarConversacion(ctx context.Context, t *auditoria.Tx, p mensajeriabd.InsertarConversacionParams,
	existente func() (mensajeriabd.MensajeriaChatConversacion, error)) (mensajeriabd.MensajeriaChatConversacion, bool, error) {
	q := mensajeriabd.New(t)
	c, err := q.InsertarConversacion(ctx, p)
	if esSinFilas(err) {
		c, err = existente()
		return c, false, err
	}
	if err != nil {
		return c, false, err
	}
	t.FijarEntidad("chat_conversacion", c.ID)
	if c.EmpresaID != nil {
		t.FijarEmpresa(*c.EmpresaID)
	} else {
		t.FijarEmpresa(0)
	}
	return c, true, nil
}

func (m *Modulo) emitirCreada(t *auditoria.Tx, c mensajeriabd.MensajeriaChatConversacion, miembros []int64) error {
	if miembros == nil {
		miembros = []int64{}
	}
	return t.Emitir("chat.conversacion_creada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "tipo": c.Tipo,
		"cuadrilla_id": c.CuadrillaID, "orden_id": c.OrdenID, "creada_por": c.CreadaPor, "miembros": miembros})
}

func (m *Modulo) abierta(ctx context.Context, t *auditoria.Tx, a alcanceChat, c mensajeriabd.MensajeriaChatConversacion, nueva bool) (ConversacionChatAbierta, error) {
	q := mensajeriabd.New(t)
	if nueva {
		if err := emitirEventoChat(ctx, t, c, EventoConversacionActualizada, nil, &a.usuario, m.ahora()); err != nil {
			return ConversacionChatAbierta{}, err
		}
	}
	d, err := detalle(ctx, q, a.usuario, c.ID)
	return ConversacionChatAbierta{ConversacionChatDetalle: d, Nueva: nueva}, err
}

func nuevoAbrirDirecta(m *Modulo) *auditoria.Caso[EntradaAbrirDirecta, ConversacionChatAbierta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAbrirDirecta, ConversacionChatAbierta]{
		Accion: "chat.abrir_directa", Permiso: nucleo.PermChatEnviar,
		AuditarExito: func(r ConversacionChatAbierta) bool { return r.Nueva },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAbrirDirecta) (ConversacionChatAbierta, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			if in.UsuarioID == a.usuario {
				return ConversacionChatAbierta{}, campoChat("usuario_id", "No puedes abrir una conversación contigo", "otro_usuario")
			}
			q := mensajeriabd.New(t)
			otro, err := q.UsuarioChat(ctx, in.UsuarioID)
			if esSinFilas(err) {
				return ConversacionChatAbierta{}, usuarioNoDisponible()
			}
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			if otro.Tipo != "persona" || !otro.Activo {
				return ConversacionChatAbierta{}, usuarioNoDisponible()
			}
			if a.empresa != nil {
				en, err := identidadbd.New(t).UsuarioEnEmpresa(ctx, identidadbd.UsuarioEnEmpresaParams{UsuarioID: otro.ID, EmpresaID: *a.empresa})
				if err != nil {
					return ConversacionChatAbierta{}, err
				}
				if !en {
					return ConversacionChatAbierta{}, usuarioNoDisponible()
				}
			}
			perms, err := identidadbd.New(t).PermisosDeUsuario(ctx, otro.ID)
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			usaChat := false
			for _, p := range perms {
				usaChat = usaChat || p.Permiso == string(nucleo.PermChatVer)
			}
			if !usaChat {
				return ConversacionChatAbierta{}, usuarioNoDisponible()
			}
			clave := claveDirecta(a.usuario, otro.ID)
			c, nueva, err := m.guardarConversacion(ctx, t, mensajeriabd.InsertarConversacionParams{EmpresaID: a.empresa, Tipo: TipoDirecta,
				ClaveDirecta: &clave, CreadaPor: &a.usuario, Ahora: m.ahora()}, func() (mensajeriabd.MensajeriaChatConversacion, error) {
				return q.ConversacionDirecta(ctx, mensajeriabd.ConversacionDirectaParams{Clave: &clave, Empresa: a.empresa})
			})
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			if nueva {
				for _, u := range []int64{a.usuario, otro.ID} {
					if _, err := q.InsertarMiembro(ctx, mensajeriabd.InsertarMiembroParams{ConversacionID: c.ID, UsuarioID: u, Origen: "directa", Ahora: m.ahora()}); err != nil {
						return ConversacionChatAbierta{}, err
					}
				}
				if err := m.emitirCreada(t, c, []int64{a.usuario, otro.ID}); err != nil {
					return ConversacionChatAbierta{}, err
				}
			}
			return m.abierta(ctx, t, a, c, nueva)
		},
	})
}

func nuevoAbrirCuadrilla(m *Modulo) *auditoria.Caso[EntradaAbrirCuadrilla, ConversacionChatAbierta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAbrirCuadrilla, ConversacionChatAbierta]{
		Accion: "chat.abrir_cuadrilla", Permiso: nucleo.PermChatEnviar,
		AuditarExito: func(r ConversacionChatAbierta) bool { return r.Nueva },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAbrirCuadrilla) (ConversacionChatAbierta, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			noEsTuya := nucleo.NoEncontrado("La cuadrilla no existe o no eres de ella ni la supervisas")
			ag := agendabd.New(t)
			cu, err := ag.CuadrillaPorID(ctx, in.CuadrillaID)
			if esSinFilas(err) {
				return ConversacionChatAbierta{}, noEsTuya
			}
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			// Una cuadrilla sin empresa (catálogo común) tiene una conversación por empresa.
			if !cu.Activa || (cu.EmpresaID != nil && !a.empresaVisible(cu.EmpresaID)) {
				return ConversacionChatAbierta{}, noEsTuya
			}
			propia := false
			if tec, err := ag.TecnicoPorUsuario(ctx, a.usuario); err == nil {
				propia = tec.Activo && tec.CuadrillaID != nil && *tec.CuadrillaID == cu.ID
			} else if !esSinFilas(err) {
				return ConversacionChatAbierta{}, err
			}
			if !propia && !a.global && !contiene(a.cuadrillas, cu.ID) {
				zs, err := ag.ZonasDeCuadrillas(ctx, []int64{cu.ID})
				if err != nil {
					return ConversacionChatAbierta{}, err
				}
				cubre := false
				for _, z := range zs {
					cubre = cubre || contiene(a.zonas, z.ZonaID)
				}
				if !cubre {
					return ConversacionChatAbierta{}, noEsTuya
				}
			}
			q := mensajeriabd.New(t)
			id := cu.ID
			c, nueva, err := m.guardarConversacion(ctx, t, mensajeriabd.InsertarConversacionParams{EmpresaID: a.empresa, Tipo: TipoCuadrilla,
				CuadrillaID: &id, CreadaPor: &a.usuario, Ahora: m.ahora()}, func() (mensajeriabd.MensajeriaChatConversacion, error) {
				return q.ConversacionDeCuadrilla(ctx, mensajeriabd.ConversacionDeCuadrillaParams{CuadrillaID: &id, Empresa: a.empresa})
			})
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			if nueva {
				if err := m.emitirCreada(t, c, nil); err != nil {
					return ConversacionChatAbierta{}, err
				}
			}
			return m.abierta(ctx, t, a, c, nueva)
		},
	})
}

// cubreOrdenes: el actor ve la orden con ordenes.ver (despacho): global, su zona o su cuadrilla.
func cubreOrdenes(ctx context.Context, o campobd.CampoOrden) bool {
	p := contexto.PermisosDe(ctx)
	if !p.Tiene(string(nucleo.PermOrdenesVer)) {
		return false
	}
	g, z, c := p.Alcances(string(nucleo.PermOrdenesVer))
	return g || (o.ZonaID != nil && contiene(z, *o.ZonaID)) || (o.CuadrillaID != nil && contiene(c, *o.CuadrillaID))
}

func nuevoAbrirOrden(m *Modulo) *auditoria.Caso[EntradaAbrirOrden, ConversacionChatAbierta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAbrirOrden, ConversacionChatAbierta]{
		Accion: "chat.abrir_orden", Permiso: nucleo.PermChatEnviar,
		AuditarExito: func(r ConversacionChatAbierta) bool { return r.Nueva },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAbrirOrden) (ConversacionChatAbierta, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			noEsTuya := nucleo.NoEncontrado("La orden no existe o no puedes verla")
			o, err := campobd.New(t).OrdenPorID(ctx, in.OrdenID)
			if esSinFilas(err) {
				return ConversacionChatAbierta{}, noEsTuya
			}
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			if !a.empresaVisible(o.EmpresaID) {
				return ConversacionChatAbierta{}, noEsTuya
			}
			tecnico := o.TecnicoID != nil && *o.TecnicoID == a.usuario
			supervisa := a.cubre(o.ZonaID, o.CuadrillaID)
			despacho := !tecnico && !supervisa && cubreOrdenes(ctx, o)
			if !tecnico && !supervisa && !despacho {
				return ConversacionChatAbierta{}, noEsTuya
			}
			q := mensajeriabd.New(t)
			id := o.ID
			c, nueva, err := m.guardarConversacion(ctx, t, mensajeriabd.InsertarConversacionParams{EmpresaID: o.EmpresaID, Tipo: TipoOrden,
				OrdenID: &id, CreadaPor: &a.usuario, Ahora: m.ahora()}, func() (mensajeriabd.MensajeriaChatConversacion, error) {
				return q.ConversacionDeOrden(ctx, &id)
			})
			if err != nil {
				return ConversacionChatAbierta{}, err
			}
			var miembros []int64
			unido := false
			if despacho {
				// Despacho se une al hilo: queda como miembro aunque la orden cambie de zona o técnico.
				n, err := q.InsertarMiembro(ctx, mensajeriabd.InsertarMiembroParams{ConversacionID: c.ID, UsuarioID: a.usuario, Origen: "unido", Ahora: m.ahora()})
				if err != nil {
					return ConversacionChatAbierta{}, err
				}
				unido = n > 0
				miembros = []int64{a.usuario}
			}
			if nueva {
				if err := m.emitirCreada(t, c, miembros); err != nil {
					return ConversacionChatAbierta{}, err
				}
			} else if unido {
				t.FijarEntidad("chat_conversacion", c.ID)
				if err := emitirEventoChat(ctx, t, c, EventoConversacionActualizada, nil, &a.usuario, m.ahora()); err != nil {
					return ConversacionChatAbierta{}, err
				}
			}
			r, err := m.abierta(ctx, t, a, c, nueva)
			r.Nueva = nueva || unido
			return r, err
		},
	})
}

// Enviar ------------------------------------------------------------------------------------------

// EntradaEnviarMensaje es POST /chat/conversaciones/{id}/mensajes. Lo auditado es solo el id, la
// conversación y qué trae (nunca el texto).
type EntradaEnviarMensaje struct {
	ConversacionID int64          `json:"conversacion_id"`
	ID             uuid.UUID      `json:"id"`
	Texto          *string        `json:"-"`
	AdjuntoID      *uuid.UUID     `json:"adjunto_id,omitempty"`
	Ubicacion      *UbicacionChat `json:"-"`
	CreadoEn       *time.Time     `json:"creado_en,omitempty"`
	ConTexto       bool           `json:"con_texto"`
	ConUbicacion   bool           `json:"con_ubicacion"`
}

// MensajeEnviado es la respuesta de enviar (201 nuevo, 200 reenvío del mismo id).
type MensajeEnviado struct {
	MensajeChat
	Repetido bool `json:"-"`
}

func demasiadosMensajes(espera time.Duration, limite int) *nucleo.Problema {
	return nucleo.DemasiadosIntentos("demasiados_mensajes", fmt.Sprintf("Máximo %d mensajes por minuto: espera un momento", limite), espera)
}

func nuevoEnviarMensaje(m *Modulo) *auditoria.Caso[EntradaEnviarMensaje, MensajeEnviado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEnviarMensaje, MensajeEnviado]{
		Accion: "chat.enviar", Permiso: nucleo.PermChatEnviar,
		// Un reenvío del mismo id (cola sin conexión) no deja otra fila.
		AuditarExito: func(r MensajeEnviado) bool { return !r.Repetido },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEnviarMensaje) (MensajeEnviado, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return MensajeEnviado{}, err
			}
			ahora := m.ahora()
			if in.ID.Version() != 7 {
				return MensajeEnviado{}, campoChat("id", "Debe ser un uuid v7 generado por el cliente", "uuid_v7")
			}
			var texto *string
			if in.Texto != nil {
				if strings.ContainsRune(*in.Texto, 0) {
					return MensajeEnviado{}, campoChat("texto", "El texto no puede contener el carácter NUL", "texto")
				}
				if v := strings.TrimSpace(*in.Texto); v != "" {
					if n := len([]rune(v)); n > MaxTextoChat {
						return MensajeEnviado{}, campoChat("texto", fmt.Sprintf("Máximo %d caracteres", MaxTextoChat), "length")
					}
					texto = &v
				}
			}
			if texto == nil && in.AdjuntoID == nil && in.Ubicacion == nil {
				return MensajeEnviado{}, nucleo.ReglaNegocio("mensaje_vacio", "El mensaje necesita texto, una foto o una ubicación")
			}
			if in.CreadoEn != nil && in.CreadoEn.After(ahora.Add(5*time.Minute)) {
				return MensajeEnviado{}, campoChat("creado_en", "No puede ser futuro", "pasado")
			}
			q := mensajeriabd.New(t)
			c, err := m.conversacionVisible(ctx, q, a, in.ConversacionID, false)
			if err != nil {
				return MensajeEnviado{}, err
			}
			t.FijarEntidad("chat_mensaje", in.ID)
			if c.EmpresaID != nil {
				t.FijarEmpresa(*c.EmpresaID)
			} else {
				t.FijarEmpresa(0)
			}
			hash := hashContenido(c.ID, texto, in.AdjuntoID, in.Ubicacion)
			// Los envíos del mismo autor van en fila: el reenvío y el límite por minuto son exactos.
			if err := q.CandadoChat(ctx, fmt.Sprintf("chat:autor:%d", a.usuario)); err != nil {
				return MensajeEnviado{}, err
			}
			if previo, err := q.MensajePorID(ctx, in.ID); err == nil {
				if previo.AutorID != a.usuario || string(previo.HashContenido) != string(hash) {
					return MensajeEnviado{}, nucleo.Conflicto("mensaje_id_reutilizado", "Ese id de mensaje ya se usó con otro contenido: genera un uuid v7 nuevo")
				}
				msj, err := mensajeVisto(ctx, q, previo.ID)
				return MensajeEnviado{MensajeChat: msj, Repetido: true}, err
			} else if !esSinFilas(err) {
				return MensajeEnviado{}, err
			}
			recientes, err := q.EnviosRecientes(ctx, mensajeriabd.EnviosRecientesParams{AutorID: a.usuario, Desde: ahora.Add(-time.Minute)})
			if err != nil {
				return MensajeEnviado{}, err
			}
			if recientes.Total >= int64(m.cfg.ChatPorMinuto) {
				return MensajeEnviado{}, demasiadosMensajes(recientes.Primero.Add(time.Minute).Sub(ahora), m.cfg.ChatPorMinuto)
			}
			if in.AdjuntoID != nil {
				adj, err := adjuntos.DeEntidad(ctx, t, *in.AdjuntoID, EntidadAdjuntosChat, itoa(c.ID))
				if err != nil {
					return MensajeEnviado{}, nucleo.ReglaNegocio("adjunto_invalido", "La foto no está registrada en esta conversación (POST /api/v1/adjuntos con entidad chat_conversacion)")
				}
				switch {
				case adj.SubidoPor == nil || *adj.SubidoPor != a.usuario:
					return MensajeEnviado{}, nucleo.ReglaNegocio("adjunto_invalido", "Solo puedes enviar fotos que subiste tú")
				case adj.Tipo != "foto":
					return MensajeEnviado{}, nucleo.ReglaNegocio("adjunto_no_es_foto", "El adjunto de un mensaje debe ser una foto")
				case adj.Estado == adjuntos.EstadoRechazado:
					return MensajeEnviado{}, nucleo.ReglaNegocio("adjunto_rechazado", "La foto fue rechazada al subirla: vuelve a subirla")
				}
			}
			seq, err := q.AvanzarConversacion(ctx, mensajeriabd.AvanzarConversacionParams{MensajeID: in.ID, Ahora: ahora, ID: c.ID})
			if err != nil {
				return MensajeEnviado{}, err
			}
			p := mensajeriabd.InsertarMensajeParams{ID: in.ID, ConversacionID: c.ID, EmpresaID: c.EmpresaID, Seq: seq, AutorID: a.usuario,
				Texto: texto, AdjuntoID: pgUUID(in.AdjuntoID), CreadoEn: in.CreadoEn, EnviadoEn: ahora, HashContenido: hash}
			if in.CreadoEn != nil {
				u := in.CreadoEn.UTC()
				p.CreadoEn = &u
			}
			if in.Ubicacion != nil {
				p.Lat, p.Lon = &in.Ubicacion.Lat, &in.Ubicacion.Lon
			}
			f, err := q.InsertarMensaje(ctx, p)
			if err != nil {
				return MensajeEnviado{}, err
			}
			destinatarios, err := m.destinatarios(ctx, t, c, a.usuario)
			if err != nil {
				return MensajeEnviado{}, err
			}
			contenido := map[string]bool{"texto": texto != nil, "foto": in.AdjuntoID != nil, "ubicacion": in.Ubicacion != nil}
			if err := t.Emitir("chat.mensaje_enviado", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
				"tipo": c.Tipo, "cuadrilla_id": c.CuadrillaID, "orden_id": c.OrdenID, "autor_id": a.usuario, "seq": seq, "contenido": contenido,
				"enviado_en": ahora, "destinatarios": destinatarios}); err != nil {
				return MensajeEnviado{}, err
			}
			if err := emitirEventoChat(ctx, t, c, EventoMensajeNuevo, &f.ID, &a.usuario, ahora); err != nil {
				return MensajeEnviado{}, err
			}
			msj, err := mensajeVisto(ctx, q, f.ID)
			return MensajeEnviado{MensajeChat: msj}, err
		},
	})
}

func mensajeVisto(ctx context.Context, q *mensajeriabd.Queries, id uuid.UUID) (MensajeChat, error) {
	fs, err := q.MensajesPorIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return MensajeChat{}, err
	}
	if len(fs) == 0 {
		return MensajeChat{}, nucleo.NoEncontrado("")
	}
	return aMensaje(fs[0].MensajeriaChatMensaje, fs[0].AutorNombre, fs[0].AdjuntoEstado), nil
}

// destinatarios: a quién avisar de un mensaje (para el emisor de push, fuera de esta fase).
func (m *Modulo) destinatarios(ctx context.Context, t *auditoria.Tx, c mensajeriabd.MensajeriaChatConversacion, autor int64) ([]int64, error) {
	p := mensajeriabd.DestinatariosChatParams{AutorID: autor, Empresa: c.EmpresaID, ConversacionID: c.ID, Tipo: c.Tipo,
		CuadrillaID: c.CuadrillaID, OrdenID: c.OrdenID, AlcanceZonas: []int64{}}
	switch c.Tipo {
	case TipoCuadrilla:
		p.AlcanceCuadrilla = c.CuadrillaID
		zs, err := agendabd.New(t).ZonasDeCuadrillas(ctx, []int64{*c.CuadrillaID})
		if err != nil {
			return nil, err
		}
		for _, z := range zs {
			p.AlcanceZonas = append(p.AlcanceZonas, z.ZonaID)
		}
	case TipoOrden:
		o, err := campobd.New(t).OrdenPorID(ctx, *c.OrdenID)
		if err != nil {
			return nil, err
		}
		p.AlcanceCuadrilla = o.CuadrillaID
		if o.ZonaID != nil {
			p.AlcanceZonas = append(p.AlcanceZonas, *o.ZonaID)
		}
	}
	ds, err := mensajeriabd.New(t).DestinatariosChat(ctx, p)
	if ds == nil {
		ds = []int64{}
	}
	return ds, err
}

// Eliminar ----------------------------------------------------------------------------------------

// EntradaEliminarMensaje es POST /chat/mensajes/{id}/eliminar.
type EntradaEliminarMensaje struct {
	ID uuid.UUID `json:"mensaje_id"`
}

// MensajeEliminado es la lápida (Repetido: ya estaba eliminado).
type MensajeEliminado struct {
	MensajeChat
	Repetido bool `json:"-"`
}

func nuevoEliminarMensaje(m *Modulo) *auditoria.Caso[EntradaEliminarMensaje, MensajeEliminado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEliminarMensaje, MensajeEliminado]{
		Accion: "chat.eliminar", Permiso: nucleo.PermChatEnviar,
		AuditarExito: func(r MensajeEliminado) bool { return !r.Repetido },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEliminarMensaje) (MensajeEliminado, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return MensajeEliminado{}, err
			}
			q := mensajeriabd.New(t)
			noEsta := nucleo.NoEncontrado("El mensaje no existe o no lo puedes ver")
			f, err := q.MensajeParaActualizar(ctx, in.ID)
			if esSinFilas(err) {
				return MensajeEliminado{}, noEsta
			}
			if err != nil {
				return MensajeEliminado{}, err
			}
			c, err := m.conversacionVisible(ctx, q, a, f.ConversacionID, false)
			if err != nil {
				return MensajeEliminado{}, noEsta
			}
			t.FijarEntidad("chat_mensaje", f.ID)
			if c.EmpresaID != nil {
				t.FijarEmpresa(*c.EmpresaID)
			} else {
				t.FijarEmpresa(0)
			}
			if f.AutorID != a.usuario {
				return MensajeEliminado{}, nucleo.NuevoProblema(http.StatusForbidden, "no_es_autor", "No es tu mensaje", "Solo su autor puede eliminar un mensaje")
			}
			if f.EliminadoEn != nil {
				msj, err := mensajeVisto(ctx, q, f.ID)
				return MensajeEliminado{MensajeChat: msj, Repetido: true}, err
			}
			ahora := m.ahora()
			if ahora.Sub(f.EnviadoEn) > m.cfg.ChatPlazoEliminar {
				return MensajeEliminado{}, nucleo.ReglaNegocio("plazo_vencido",
					fmt.Sprintf("Solo se puede eliminar un mensaje hasta %d minutos después de enviarlo", int(m.cfg.ChatPlazoEliminar/time.Minute)))
			}
			if _, err := q.EliminarMensaje(ctx, mensajeriabd.EliminarMensajeParams{Ahora: &ahora, ID: f.ID}); err != nil {
				return MensajeEliminado{}, err
			}
			if err := t.Emitir("chat.mensaje_eliminado", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
				"autor_id": f.AutorID, "eliminado_en": ahora}); err != nil {
				return MensajeEliminado{}, err
			}
			if err := emitirEventoChat(ctx, t, c, EventoMensajeEliminado, &f.ID, &a.usuario, ahora); err != nil {
				return MensajeEliminado{}, err
			}
			msj, err := mensajeVisto(ctx, q, f.ID)
			return MensajeEliminado{MensajeChat: msj}, err
		},
	})
}

// Leer --------------------------------------------------------------------------------------------

// EntradaMarcarLeido es POST /chat/conversaciones/{id}/leer.
type EntradaMarcarLeido struct {
	ConversacionID int64     `json:"conversacion_id"`
	HastaMensajeID uuid.UUID `json:"hasta_mensaje_id"`
}

func nuevoMarcarLeido(m *Modulo) *auditoria.Caso[EntradaMarcarLeido, LecturaPropia] {
	return auditoria.Nuevo(auditoria.Caso[EntradaMarcarLeido, LecturaPropia]{
		// Frecuente y sin efecto de negocio: no deja fila de auditoría (CLAUDE.md §9 audita acciones).
		Accion: "chat.leer", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[LecturaPropia],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaMarcarLeido) (LecturaPropia, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return LecturaPropia{}, err
			}
			q := mensajeriabd.New(t)
			c, err := m.conversacionVisible(ctx, q, a, in.ConversacionID, false)
			if err != nil {
				return LecturaPropia{}, err
			}
			f, err := q.MensajePorID(ctx, in.HastaMensajeID)
			if esSinFilas(err) || (err == nil && f.ConversacionID != c.ID) {
				return LecturaPropia{}, campoChat("hasta_mensaje_id", "El mensaje no es de esta conversación (¿aún no llegó al servidor?)", "mensaje")
			}
			if err != nil {
				return LecturaPropia{}, err
			}
			ahora := m.ahora()
			l, err := q.AvanzarLectura(ctx, mensajeriabd.AvanzarLecturaParams{ConversacionID: c.ID, UsuarioID: a.usuario, HastaSeq: f.Seq,
				HastaMensajeID: f.ID, Ahora: ahora})
			switch {
			case err == nil:
				if err := emitirEventoChat(ctx, t, c, EventoLeido, &l.HastaMensajeID, &a.usuario, ahora); err != nil {
					return LecturaPropia{}, err
				}
			case esSinFilas(err):
				// Ya había leído hasta ahí o más allá: la lectura solo avanza.
				if l, err = q.LecturaDe(ctx, mensajeriabd.LecturaDeParams{ConversacionID: c.ID, UsuarioID: a.usuario}); err != nil {
					return LecturaPropia{}, err
				}
			default:
				return LecturaPropia{}, err
			}
			nl, err := q.NoLeidos(ctx, mensajeriabd.NoLeidosParams{Usuario: a.usuario, Ids: []int64{c.ID}})
			if err != nil {
				return LecturaPropia{}, err
			}
			r := LecturaPropia{ConversacionID: c.ID, LeidoHastaMensajeID: &l.HastaMensajeID}
			if len(nl) > 0 {
				r.NoLeidos = nl[0].NoLeidos
			}
			return r, nil
		},
	})
}

// Mensajes ----------------------------------------------------------------------------------------

// EntradaListarMensajes es GET /chat/conversaciones/{id}/mensajes.
type EntradaListarMensajes struct {
	ID int64 `path:"id" minimum:"1"`
	nucleo.ParamsPagina
	Direccion string `query:"direccion" enum:"anteriores,siguientes" default:"anteriores" doc:"anteriores: hacia atrás (sin cursor, los más recientes); siguientes: hacia adelante (sin cursor, desde el primero)"`
	DesdeSeq  int64  `query:"desde_seq" minimum:"0" doc:"Ancla explícita en vez de cursor: los de seq mayor (siguientes) o menor (anteriores) que este"`
}

func nuevoListarMensajes(m *Modulo) *auditoria.Caso[EntradaListarMensajes, nucleo.Pagina[MensajeChat]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarMensajes, nucleo.Pagina[MensajeChat]]{
		Accion: "chat.listar_mensajes", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[nucleo.Pagina[MensajeChat]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarMensajes) (nucleo.Pagina[MensajeChat], error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return nucleo.Pagina[MensajeChat]{}, err
			}
			q := mensajeriabd.New(t)
			c, err := m.conversacionVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return nucleo.Pagina[MensajeChat]{}, err
			}
			siguientes := in.Direccion == "siguientes"
			desde := in.DesdeSeq
			var seq int64
			con, err := nucleo.DecodificarCursor(in.Cursor, &seq)
			switch {
			case err != nil:
				return nucleo.Pagina[MensajeChat]{}, err
			case con:
				desde = seq
			case in.DesdeSeq == 0 && !siguientes:
				desde = c.UltimoSeq + 1
			}
			limite := in.Limite()
			fs, err := q.ListarMensajes(ctx, mensajeriabd.ListarMensajesParams{ConversacionID: c.ID, Siguientes: siguientes, Desde: desde,
				Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
			if err != nil {
				return nucleo.Pagina[MensajeChat]{}, err
			}
			hayMas := len(fs) > limite
			if hayMas {
				fs = fs[:limite]
			}
			data := make([]MensajeChat, len(fs))
			for i, f := range fs {
				msj := aMensaje(f.MensajeriaChatMensaje, f.AutorNombre, f.AdjuntoEstado)
				if siguientes {
					data[i] = msj
				} else {
					data[len(fs)-1-i] = msj // siempre en orden de seq ascendente
				}
			}
			p := nucleo.Pagina[MensajeChat]{Data: data}
			if hayMas && len(fs) > 0 {
				cur := nucleo.CodificarCursor(fs[len(fs)-1].MensajeriaChatMensaje.Seq)
				p.NextCursor = &cur
			}
			return p, nil
		},
	})
}

// Cambios (sondeo) --------------------------------------------------------------------------------

// EntradaCambios es GET /chat/cambios.
type EntradaCambios struct {
	Cursor string `query:"cursor" maxLength:"20" doc:"El nextCursor anterior o el id del último evento SSE (Last-Event-ID). Vacío: la cabeza actual (sin eventos)"`
	Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"100"`
}

// CambiosChat es el feed desde un cursor (el mismo de SSE).
type CambiosChat struct {
	Data       []EventoChat `json:"data"`
	NextCursor string       `json:"nextCursor" doc:"Siempre presente: el cursor para la siguiente consulta (o el id para Last-Event-ID)"`
	HayMas     bool         `json:"hayMas" doc:"true: hay más eventos ya disponibles; consulta de nuevo sin esperar"`
}

func nuevoCambios(m *Modulo) *auditoria.Caso[EntradaCambios, CambiosChat] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambios, CambiosChat]{
		Accion: "chat.cambios", Permiso: nucleo.PermChatVer, AuditarExito: sinAuditar[CambiosChat],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambios) (CambiosChat, error) {
			a, err := alcanceDe(ctx)
			if err != nil {
				return CambiosChat{}, err
			}
			q := mensajeriabd.New(t)
			desde, err := cursorFeed(ctx, q, a, "cursor", in.Cursor)
			if err != nil {
				return CambiosChat{}, err
			}
			limite := in.Limit
			if limite <= 0 || limite > 200 {
				limite = 100
			}
			evs, hayMas, siguiente, err := m.leerFeed(ctx, q, a, desde, limite)
			if err != nil {
				return CambiosChat{}, err
			}
			return CambiosChat{Data: evs, NextCursor: itoa(siguiente), HayMas: hayMas}, nil
		},
	})
}

// leerFeed lee hasta limite eventos después de desde, como los ve el usuario.
func (m *Modulo) leerFeed(ctx context.Context, q *mensajeriabd.Queries, a alcanceChat, desde int64, limite int) ([]EventoChat, bool, int64, error) {
	filas, err := q.EventosChat(ctx, mensajeriabd.EventosChatParams{Desde: desde, Todas: a.todas, Empresa: a.empresa, Usuario: a.usuario,
		SupGlobal: a.global, SupZonas: a.zonas, SupCuadrillas: a.cuadrillas, Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
	if err != nil {
		return nil, false, desde, err
	}
	hayMas := len(filas) > limite
	if hayMas {
		filas = filas[:limite]
	}
	evs, err := eventosPara(ctx, q, a, filas)
	if err != nil {
		return nil, false, desde, err
	}
	siguiente := desde
	if len(filas) > 0 {
		siguiente = filas[len(filas)-1].ID
	} else if !hayMas {
		// Sin eventos visibles: avanza hasta la cabeza de la empresa (lo de otras conversaciones no se
		// vuelve a leer).
		e, err := q.EstadoFeedChat(ctx, a.empresa)
		if err != nil {
			return nil, false, desde, err
		}
		siguiente = max(siguiente, e.CabezaEmpresa)
	}
	return evs, hayMas, siguiente, nil
}
