package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Configuración de la bandeja: número (uso, cola, avisos), colas, etiquetas y respuestas rápidas.

// Usos de un número.
var UsosCanal = []string{"avisos", "soporte", "cobros", "ventas"}

func esDuplicado(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// usuarioAtiende: un usuario persona, activo, de la empresa y con conversaciones.ver. supervisa:
// tiene conversaciones.supervisar.
func usuarioAtiende(ctx context.Context, t *auditoria.Tx, empresa, id int64) (UsuarioRef, bool, error) {
	q := mensajeriabd.New(t)
	u, err := q.UsuarioChat(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UsuarioRef{}, false, usuarioNoDisponibleConv()
	}
	if err != nil {
		return UsuarioRef{}, false, err
	}
	if u.Tipo != "persona" || !u.Activo {
		return UsuarioRef{}, false, usuarioNoDisponibleConv()
	}
	if empresa != 0 {
		en, err := identidadbd.New(t).UsuarioEnEmpresa(ctx, identidadbd.UsuarioEnEmpresaParams{UsuarioID: id, EmpresaID: empresa})
		if err != nil {
			return UsuarioRef{}, false, err
		}
		if !en {
			return UsuarioRef{}, false, usuarioNoDisponibleConv()
		}
	}
	perms, err := identidadbd.New(t).PermisosDeUsuario(ctx, id)
	if err != nil {
		return UsuarioRef{}, false, err
	}
	ve, sup := false, false
	for _, p := range perms {
		ve = ve || p.Permiso == string(nucleo.PermConversacionesVer)
		sup = sup || p.Permiso == string(nucleo.PermConversacionesSupervisar)
	}
	if !ve {
		return UsuarioRef{}, false, usuarioNoDisponibleConv()
	}
	return UsuarioRef{ID: u.ID, Nombre: u.Nombre}, sup, nil
}

// Configurar un número (canales.configurar) ----------------------------------------------------------

// EntradaConfigurarCanal es POST /canales/{id}/configurar.
type EntradaConfigurarCanal struct {
	ID            int64    `json:"-"`
	Version       int32    `json:"version" minimum:"1"`
	Usos          []string `json:"usos" minItems:"1" maxItems:"4" uniqueItems:"true" doc:"avisos, soporte, cobros, ventas"`
	AvisosEmpresa bool     `json:"avisosEmpresa" doc:"El número por el que salen los avisos de la empresa (uno solo; marcarlo lo quita del anterior). Exige avisos en usos"`
	ColaID        *int64   `json:"colaId,omitempty" minimum:"1" nullable:"true" doc:"Cola de las conversaciones nuevas (null = sin cola)"`
	HorarioID     *int64   `json:"horarioId,omitempty" minimum:"1" nullable:"true" doc:"Horario del número (null = siempre abierto; el de la cola manda si lo tiene)"`
	// FueraDeHorario: qué hacer con el horario cerrado (ausente = no cambia).
	FueraDeHorario *FueraDeHorario `json:"fueraDeHorario,omitempty"`
	// Bot: cuando no hay agentes (ausente = no cambia; §14.6).
	Bot *BotNumero `json:"bot,omitempty"`
}

func validarUsos(usos []string, avisos bool) error {
	for _, u := range usos {
		if !slices.Contains(UsosCanal, u) {
			return campoConv("usos", "Uso desconocido: "+recortar(u, 20), "enum")
		}
	}
	if avisos && !slices.Contains(usos, "avisos") {
		return reglaConv("numero_avisos_requiere_uso", "El número de avisos de la empresa debe tener el uso avisos")
	}
	return nil
}

// fijarAvisos deja este número como el de avisos de la empresa: lo quita de los demás.
func fijarAvisos(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, empresa *int64, id int64) error {
	e := int64(0)
	if empresa != nil {
		e = *empresa
	}
	otros, err := q.QuitarAvisosEmpresa(ctx, mensajeriabd.QuitarAvisosEmpresaParams{EmpresaID: e, Excepto: id})
	if err != nil {
		return err
	}
	for _, o := range otros {
		if err := t.Emitir("canal_mensajeria.actualizado", map[string]any{"canal_id": o, "empresa_id": empresa, "campos": []string{"avisos_empresa"}}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Modulo) colaDeEmpresa(ctx context.Context, q *mensajeriabd.Queries, empresa int64, id *int64) error {
	if id == nil {
		return nil
	}
	if _, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *id, Empresa: empresa}); errors.Is(err, pgx.ErrNoRows) {
		return campoConv("colaId", "No es una cola de la empresa", "cola")
	} else if err != nil {
		return err
	}
	return nil
}

func nuevoConfigurarCanal(m *Modulo) *auditoria.Caso[EntradaConfigurarCanal, Canal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfigurarCanal, Canal]{
		Accion: "canales.configurar", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfigurarCanal) (Canal, error) {
			// Como la bandeja: solo una persona con sesión (no un token ni una integración).
			if actor := contexto.ActorDe(ctx); actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
				return Canal{}, nucleo.NoEncontrado("El número no existe")
			}
			if err := validarUsos(in.Usos, in.AvisosEmpresa); err != nil {
				return Canal{}, err
			}
			antes, err := m.canalParaActualizar(ctx, t, in.ID)
			if err != nil {
				return Canal{}, err
			}
			if antes.Version != in.Version {
				return Canal{}, nucleo.ConflictoVersion("canal", in.ID)
			}
			q := mensajeriabd.New(t)
			empresa := int64(0)
			if antes.EmpresaID != nil {
				empresa = *antes.EmpresaID
			}
			if err := m.colaDeEmpresa(ctx, q, empresa, in.ColaID); err != nil {
				return Canal{}, err
			}
			if err := m.horarioDeEmpresa(ctx, q, empresa, in.HorarioID); err != nil {
				return Canal{}, err
			}
			fuera := fueraDeHorarioDe(antes.FueraHorario)
			if in.FueraDeHorario != nil {
				fuera = *in.FueraDeHorario
				if err := validarFueraDeHorario(ctx, t, empresa, &fuera); err != nil {
					return Canal{}, err
				}
			}
			fueraCrudo, _ := json.Marshal(fuera)
			bot := botDe(antes.Bot)
			if in.Bot != nil {
				bot = *in.Bot
				if err := validarBot(ctx, t, empresa, &bot); err != nil {
					return Canal{}, err
				}
			}
			botCrudo, _ := json.Marshal(bot)
			fijarEmpresaCanal(t, antes.EmpresaID)
			if in.AvisosEmpresa && antes.Activo {
				if err := fijarAvisos(ctx, t, q, antes.EmpresaID, antes.ID); err != nil {
					return Canal{}, err
				}
			}
			if _, err := q.ConfigurarCanal(ctx, mensajeriabd.ConfigurarCanalParams{ID: in.ID, Usos: in.Usos, AvisosEmpresa: in.AvisosEmpresa, ColaID: in.ColaID}); err != nil {
				return Canal{}, conflictoCanal(err)
			}
			if _, err := q.ConfigurarHorarioCanal(ctx, mensajeriabd.ConfigurarHorarioCanalParams{ID: in.ID, HorarioID: in.HorarioID, FueraHorario: fueraCrudo}); err != nil {
				return Canal{}, err
			}
			f, err := q.ConfigurarBotCanal(ctx, mensajeriabd.ConfigurarBotCanalParams{ID: in.ID, Bot: botCrudo})
			if err != nil {
				return Canal{}, err
			}
			campos := t.RegistrarCambios("canal_mensajeria", f.ID,
				map[string]any{"usos": antes.Usos, "avisos_empresa": antes.AvisosEmpresa, "cola_id": antes.ColaID, "horario_id": antes.HorarioID,
					"fuera_horario": json.RawMessage(antes.FueraHorario), "bot": json.RawMessage(antes.Bot)},
				map[string]any{"usos": f.Usos, "avisos_empresa": f.AvisosEmpresa, "cola_id": f.ColaID, "horario_id": f.HorarioID,
					"fuera_horario": json.RawMessage(f.FueraHorario), "bot": json.RawMessage(f.Bot)})
			if campos == nil {
				campos = []string{}
			}
			return aCanal(f), t.Emitir("canal_mensajeria.actualizado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos})
		},
	})
}

// Colas -------------------------------------------------------------------------------------------

// ColaConversaciones es una cola de conversaciones.
type ColaConversaciones struct {
	ID              int64        `json:"id"`
	EmpresaID       *int64       `json:"empresaId" nullable:"true"`
	Nombre          string       `json:"nombre"`
	Descripcion     string       `json:"descripcion"`
	SLARespuestaMin *int32       `json:"slaRespuestaMin" nullable:"true" doc:"Minutos para contestar un mensaje del cliente (null = sin SLA)"`
	SLAPorVencerMin *int32       `json:"slaPorVencerMin" nullable:"true" doc:"Cuánto antes de vencer pasa a «por vencer» (null = 25 % del SLA)"`
	HorarioID       *int64       `json:"horarioId" nullable:"true" doc:"Horario de la cola (manda sobre el del número)"`
	Miembros        []UsuarioRef `json:"miembros"`
	Supervisores    []UsuarioRef `json:"supervisores" doc:"Ven, asignan y pasan todo lo de la cola sin ser miembros"`
	Disponibles     []int64      `json:"disponibles" doc:"Ids de los miembros y supervisores disponibles ahora"`
	// Minutos del bot de la cola (null = los del número).
	BotSinConfirmacionMin *int32 `json:"botSinConfirmacionMin" nullable:"true" doc:"Minutos sin que nadie la tome antes de que conteste el bot (null = los del número)"`
	BotSinDisponiblesMin  *int32 `json:"botSinDisponiblesMin" nullable:"true" doc:"Minutos sin agentes disponibles antes de que el bot tome la conversación (null = los del número)"`
	Activa          bool         `json:"activa"`
	Version         int32        `json:"version"`
}

// ListaColas es { data, nextCursor }.
type ListaColas struct {
	Data       []ColaConversaciones `json:"data"`
	NextCursor *string              `json:"nextCursor" nullable:"true"`
}

func aCola(f mensajeriabd.MensajeriaCola, miembros []UsuarioRef) ColaConversaciones {
	if miembros == nil {
		miembros = []UsuarioRef{}
	}
	return ColaConversaciones{ID: f.ID, EmpresaID: f.EmpresaID, Nombre: f.Nombre, Descripcion: f.Descripcion, SLARespuestaMin: f.SlaRespuestaMin,
		SLAPorVencerMin: f.SlaPorVencerMin, HorarioID: f.HorarioID, Miembros: miembros, Supervisores: []UsuarioRef{}, Disponibles: []int64{},
		BotSinConfirmacionMin: f.BotSinConfirmacionMin, BotSinDisponiblesMin: f.BotSinDisponiblesMin, Activa: f.Activa, Version: f.Version}
}

func colasConMiembros(ctx context.Context, q *mensajeriabd.Queries, fs []mensajeriabd.MensajeriaCola) ([]ColaConversaciones, error) {
	ids := make([]int64, 0, len(fs))
	for _, f := range fs {
		ids = append(ids, f.ID)
	}
	miembros := map[int64][]UsuarioRef{}
	if len(ids) > 0 {
		ms, err := q.MiembrosDeColas(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, mi := range ms {
			miembros[mi.ColaID] = append(miembros[mi.ColaID], UsuarioRef{ID: mi.ID, Nombre: mi.Nombre})
		}
	}
	supervisores := map[int64][]UsuarioRef{}
	personas := map[int64][]int64{} // empresa → miembros y supervisores
	if len(ids) > 0 {
		ss, err := q.SupervisoresDeColas(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, s := range ss {
			supervisores[s.ColaID] = append(supervisores[s.ColaID], UsuarioRef{ID: s.ID, Nombre: s.Nombre})
		}
	}
	for _, f := range fs {
		e := empresaDeCola(f)
		for _, u := range append(slices.Clone(miembros[f.ID]), supervisores[f.ID]...) {
			personas[e] = append(personas[e], u.ID)
		}
	}
	disp := map[int64]map[int64]DisponibilidadAgente{}
	for e, us := range personas {
		d, err := disponibilidades(ctx, q, e, us)
		if err != nil {
			return nil, err
		}
		disp[e] = d
	}
	out := make([]ColaConversaciones, 0, len(fs))
	for _, f := range fs {
		c := aCola(f, miembros[f.ID])
		if s := supervisores[f.ID]; s != nil {
			c.Supervisores = s
		}
		for _, u := range append(slices.Clone(c.Miembros), c.Supervisores...) {
			if dispDe(disp[empresaDeCola(f)], u.ID).Estado == Disponible && !slices.Contains(c.Disponibles, u.ID) {
				c.Disponibles = append(c.Disponibles, u.ID)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func empresaDeCola(f mensajeriabd.MensajeriaCola) int64 {
	if f.EmpresaID == nil {
		return 0
	}
	return *f.EmpresaID
}

// EntradaCola crea o actualiza una cola.
type EntradaCola struct {
	ID              int64  `json:"-"`
	Version         int32  `json:"version,omitempty" minimum:"0" doc:"Obligatoria al actualizar"`
	Nombre          string `json:"nombre" minLength:"1" maxLength:"80"`
	Descripcion     string `json:"descripcion,omitempty" maxLength:"300"`
	SLARespuestaMin *int32 `json:"slaRespuestaMin,omitempty" minimum:"1" maximum:"10080" nullable:"true"`
	SLAPorVencerMin *int32 `json:"slaPorVencerMin,omitempty" minimum:"1" maximum:"10080" nullable:"true"`
	HorarioID       *int64 `json:"horarioId,omitempty" minimum:"1" nullable:"true" doc:"Horario de la cola (al actualizar, ausente = sin horario)"`
	// Minutos del bot de la cola (al actualizar, ausente = los del número).
	BotSinConfirmacionMin *int32 `json:"botSinConfirmacionMin,omitempty" minimum:"1" maximum:"1440" nullable:"true"`
	BotSinDisponiblesMin  *int32 `json:"botSinDisponiblesMin,omitempty" minimum:"1" maximum:"1440" nullable:"true"`
}

// EntradaMiembrosCola fija los miembros.
type EntradaMiembrosCola struct {
	ID         int64   `json:"-"`
	Version    int32   `json:"version" minimum:"1"`
	UsuarioIDs []int64 `json:"usuarioIds" maxItems:"200" uniqueItems:"true"`
}

// EntradaIDConv es un recurso por id (activar, desactivar).
type EntradaIDConv struct {
	ID int64 `json:"id"`
}

func conflictoNombre(err error, que string) error {
	if esDuplicado(err) {
		return nucleo.Conflicto("nombre_existente", "Ya hay "+que+" con ese nombre en la empresa")
	}
	return err
}

func nuevoListarColas(*Modulo) *auditoria.Caso[struct{}, ListaColas] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ListaColas]{
		Accion: "conversaciones.listar_colas", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ListaColas],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ListaColas, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaColas{}, err
			}
			q := mensajeriabd.New(t)
			fs, err := q.ListarColas(ctx, a.empresa)
			if err != nil {
				return ListaColas{}, err
			}
			cs, err := colasConMiembros(ctx, q, fs)
			return ListaColas{Data: cs}, err
		},
	})
}

func colaAuditable(c ColaConversaciones) map[string]any {
	return map[string]any{"nombre": c.Nombre, "descripcion": c.Descripcion, "sla_respuesta_min": c.SLARespuestaMin,
		"sla_por_vencer_min": c.SLAPorVencerMin, "horario_id": c.HorarioID, "bot_sin_confirmacion_min": c.BotSinConfirmacionMin,
		"bot_sin_disponibles_min": c.BotSinDisponiblesMin}
}

func nuevoCrearCola(m *Modulo) *auditoria.Caso[EntradaCola, ColaConversaciones] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCola, ColaConversaciones]{
		Accion: "canales.crear_cola", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCola) (ColaConversaciones, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ColaConversaciones{}, err
			}
			q := mensajeriabd.New(t)
			if err := m.horarioDeEmpresa(ctx, q, a.empresa, in.HorarioID); err != nil {
				return ColaConversaciones{}, err
			}
			f, err := q.InsertarCola(ctx, mensajeriabd.InsertarColaParams{EmpresaID: a.empresaPtr(), Nombre: strings.TrimSpace(in.Nombre),
				Descripcion: in.Descripcion, SlaRespuestaMin: in.SLARespuestaMin, SlaPorVencerMin: in.SLAPorVencerMin})
			if err != nil {
				return ColaConversaciones{}, conflictoNombre(err, "una cola")
			}
			if in.HorarioID != nil {
				if f, err = q.FijarHorarioCola(ctx, mensajeriabd.FijarHorarioColaParams{ID: f.ID, HorarioID: in.HorarioID}); err != nil {
					return ColaConversaciones{}, err
				}
			}
			if in.BotSinConfirmacionMin != nil || in.BotSinDisponiblesMin != nil {
				if f, err = q.FijarBotCola(ctx, mensajeriabd.FijarBotColaParams{ID: f.ID, SinConfirmacion: in.BotSinConfirmacionMin, SinDisponibles: in.BotSinDisponiblesMin}); err != nil {
					return ColaConversaciones{}, err
				}
			}
			c := aCola(f, nil)
			t.FijarEmpresa(a.empresa)
			t.RegistrarCambios("cola", f.ID, nil, colaAuditable(c))
			return c, t.Emitir("cola.creada", map[string]any{"cola_id": f.ID, "empresa_id": f.EmpresaID, "nombre": f.Nombre})
		},
	})
}

func (m *Modulo) colaParaActualizar(ctx context.Context, t *auditoria.Tx, a alcanceConv, id int64) (mensajeriabd.MensajeriaCola, error) {
	f, err := mensajeriabd.New(t).ColaParaActualizar(ctx, mensajeriabd.ColaParaActualizarParams{ID: id, Empresa: a.empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nucleo.NoEncontrado("La cola no existe")
	}
	t.FijarEntidad("cola", id)
	t.FijarEmpresa(a.empresa)
	return f, err
}

func nuevoActualizarCola(m *Modulo) *auditoria.Caso[EntradaCola, ColaConversaciones] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCola, ColaConversaciones]{
		Accion: "canales.actualizar_cola", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCola) (ColaConversaciones, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ColaConversaciones{}, err
			}
			antes, err := m.colaParaActualizar(ctx, t, a, in.ID)
			if err != nil {
				return ColaConversaciones{}, err
			}
			if antes.Version != in.Version {
				return ColaConversaciones{}, nucleo.ConflictoVersion("cola", in.ID)
			}
			q := mensajeriabd.New(t)
			if err := m.horarioDeEmpresa(ctx, q, a.empresa, in.HorarioID); err != nil {
				return ColaConversaciones{}, err
			}
			f, err := q.ActualizarCola(ctx, mensajeriabd.ActualizarColaParams{ID: in.ID, Nombre: strings.TrimSpace(in.Nombre), Descripcion: in.Descripcion,
				SlaRespuestaMin: in.SLARespuestaMin, SlaPorVencerMin: in.SLAPorVencerMin})
			if err != nil {
				return ColaConversaciones{}, conflictoNombre(err, "una cola")
			}
			if f, err = q.FijarHorarioCola(ctx, mensajeriabd.FijarHorarioColaParams{ID: f.ID, HorarioID: in.HorarioID}); err != nil {
				return ColaConversaciones{}, err
			}
			if f, err = q.FijarBotCola(ctx, mensajeriabd.FijarBotColaParams{ID: f.ID, SinConfirmacion: in.BotSinConfirmacionMin, SinDisponibles: in.BotSinDisponiblesMin}); err != nil {
				return ColaConversaciones{}, err
			}
			cs, err := colasConMiembros(ctx, q, []mensajeriabd.MensajeriaCola{f})
			if err != nil {
				return ColaConversaciones{}, err
			}
			campos := t.RegistrarCambios("cola", f.ID, colaAuditable(aCola(antes, nil)), colaAuditable(cs[0]))
			if campos == nil {
				campos = []string{}
			}
			return cs[0], t.Emitir("cola.actualizada", map[string]any{"cola_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activa": f.Activa})
		},
	})
}

func nuevoMiembrosCola(m *Modulo) *auditoria.Caso[EntradaMiembrosCola, ColaConversaciones] {
	return auditoria.Nuevo(auditoria.Caso[EntradaMiembrosCola, ColaConversaciones]{
		Accion: "canales.miembros_cola", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaMiembrosCola) (ColaConversaciones, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ColaConversaciones{}, err
			}
			antes, err := m.colaParaActualizar(ctx, t, a, in.ID)
			if err != nil {
				return ColaConversaciones{}, err
			}
			if antes.Version != in.Version {
				return ColaConversaciones{}, nucleo.ConflictoVersion("cola", in.ID)
			}
			q := mensajeriabd.New(t)
			previos, err := colasConMiembros(ctx, q, []mensajeriabd.MensajeriaCola{antes})
			if err != nil {
				return ColaConversaciones{}, err
			}
			for _, u := range in.UsuarioIDs {
				if _, _, err := usuarioAtiende(ctx, t, a.empresa, u); err != nil {
					return ColaConversaciones{}, err
				}
			}
			if err := q.BorrarMiembrosCola(ctx, in.ID); err != nil {
				return ColaConversaciones{}, err
			}
			for _, u := range in.UsuarioIDs {
				if err := q.InsertarMiembroCola(ctx, mensajeriabd.InsertarMiembroColaParams{ColaID: in.ID, UsuarioID: u}); err != nil {
					return ColaConversaciones{}, err
				}
			}
			f, err := q.TocarCola(ctx, in.ID)
			if err != nil {
				return ColaConversaciones{}, err
			}
			cs, err := colasConMiembros(ctx, q, []mensajeriabd.MensajeriaCola{f})
			if err != nil {
				return ColaConversaciones{}, err
			}
			ids := func(us []UsuarioRef) []int64 {
				out := []int64{}
				for _, u := range us {
					out = append(out, u.ID)
				}
				return out
			}
			t.RegistrarCambios("cola", f.ID, map[string]any{"miembros": ids(previos[0].Miembros)}, map[string]any{"miembros": ids(cs[0].Miembros)})
			return cs[0], t.Emitir("cola.actualizada", map[string]any{"cola_id": f.ID, "empresa_id": f.EmpresaID, "campos": []string{"miembros"}, "activa": f.Activa})
		},
	})
}

func nuevoCambioCola(m *Modulo, accion string, activa bool) *auditoria.Caso[EntradaIDConv, ColaConversaciones] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIDConv, ColaConversaciones]{
		Accion: accion, Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIDConv) (ColaConversaciones, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ColaConversaciones{}, err
			}
			antes, err := m.colaParaActualizar(ctx, t, a, in.ID)
			if err != nil {
				return ColaConversaciones{}, err
			}
			q := mensajeriabd.New(t)
			f := antes
			if antes.Activa != activa {
				if f, err = q.FijarColaActiva(ctx, mensajeriabd.FijarColaActivaParams{ID: in.ID, Activa: activa}); err != nil {
					return ColaConversaciones{}, err
				}
				t.RegistrarCambios("cola", f.ID, map[string]any{"activa": antes.Activa}, map[string]any{"activa": f.Activa})
				if err := t.Emitir("cola.actualizada", map[string]any{"cola_id": f.ID, "empresa_id": f.EmpresaID, "campos": []string{"activa"}, "activa": f.Activa}); err != nil {
					return ColaConversaciones{}, err
				}
			}
			cs, err := colasConMiembros(ctx, q, []mensajeriabd.MensajeriaCola{f})
			if err != nil {
				return ColaConversaciones{}, err
			}
			return cs[0], nil
		},
	})
}

// Etiquetas ---------------------------------------------------------------------------------------

// EtiquetaConversacion es una etiqueta de conversaciones.
type EtiquetaConversacion struct {
	ID      int64  `json:"id"`
	Nombre  string `json:"nombre"`
	Color   string `json:"color" enum:"neutro,marca,info,exito,alerta,peligro"`
	Activa  bool   `json:"activa"`
	Version int32  `json:"version"`
}

// ListaEtiquetas es { data, nextCursor }.
type ListaEtiquetas struct {
	Data       []EtiquetaConversacion `json:"data"`
	NextCursor *string                `json:"nextCursor" nullable:"true"`
}

// EntradaEtiqueta crea o actualiza una etiqueta.
type EntradaEtiqueta struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version,omitempty" minimum:"0" doc:"Obligatoria al actualizar"`
	Nombre  string `json:"nombre" minLength:"1" maxLength:"40"`
	Color   string `json:"color,omitempty" enum:"neutro,marca,info,exito,alerta,peligro" doc:"neutro por defecto"`
}

func aEtiqueta(f mensajeriabd.MensajeriaEtiqueta) EtiquetaConversacion {
	return EtiquetaConversacion{ID: f.ID, Nombre: f.Nombre, Color: f.Color, Activa: f.Activa, Version: f.Version}
}

func nuevoListarEtiquetas(*Modulo) *auditoria.Caso[struct{}, ListaEtiquetas] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ListaEtiquetas]{
		Accion: "conversaciones.listar_etiquetas", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ListaEtiquetas],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ListaEtiquetas, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaEtiquetas{}, err
			}
			fs, err := mensajeriabd.New(t).ListarEtiquetas(ctx, a.empresa)
			out := ListaEtiquetas{Data: make([]EtiquetaConversacion, 0, len(fs))}
			for _, f := range fs {
				out.Data = append(out.Data, aEtiqueta(f))
			}
			return out, err
		},
	})
}

func nuevoGuardarEtiqueta(_ *Modulo, crear bool) *auditoria.Caso[EntradaEtiqueta, EtiquetaConversacion] {
	accion := "conversaciones.actualizar_etiqueta"
	if crear {
		accion = "conversaciones.crear_etiqueta"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaEtiqueta, EtiquetaConversacion]{
		Accion: accion, Permiso: nucleo.PermConversacionesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEtiqueta) (EtiquetaConversacion, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return EtiquetaConversacion{}, err
			}
			if in.Color == "" {
				in.Color = "neutro"
			}
			q := mensajeriabd.New(t)
			t.FijarEmpresa(a.empresa)
			nombre := strings.TrimSpace(in.Nombre)
			if crear {
				f, err := q.InsertarEtiqueta(ctx, mensajeriabd.InsertarEtiquetaParams{EmpresaID: a.empresaPtr(), Nombre: nombre, Color: in.Color})
				if err != nil {
					return EtiquetaConversacion{}, conflictoNombre(err, "una etiqueta")
				}
				t.RegistrarCambios("etiqueta", f.ID, nil, map[string]any{"nombre": f.Nombre, "color": f.Color})
				return aEtiqueta(f), t.Emitir("etiqueta.creada", map[string]any{"etiqueta_id": f.ID, "empresa_id": f.EmpresaID, "nombre": f.Nombre})
			}
			antes, err := q.EtiquetaParaActualizar(ctx, mensajeriabd.EtiquetaParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return EtiquetaConversacion{}, nucleo.NoEncontrado("La etiqueta no existe")
			}
			if err != nil {
				return EtiquetaConversacion{}, err
			}
			if antes.Version != in.Version {
				return EtiquetaConversacion{}, nucleo.ConflictoVersion("etiqueta", in.ID)
			}
			f, err := q.ActualizarEtiqueta(ctx, mensajeriabd.ActualizarEtiquetaParams{ID: in.ID, Nombre: nombre, Color: in.Color})
			if err != nil {
				return EtiquetaConversacion{}, conflictoNombre(err, "una etiqueta")
			}
			campos := t.RegistrarCambios("etiqueta", f.ID, map[string]any{"nombre": antes.Nombre, "color": antes.Color}, map[string]any{"nombre": f.Nombre, "color": f.Color})
			if campos == nil {
				campos = []string{}
			}
			return aEtiqueta(f), t.Emitir("etiqueta.actualizada", map[string]any{"etiqueta_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activa": f.Activa})
		},
	})
}

func nuevoCambioEtiqueta(_ *Modulo, accion string, activa bool) *auditoria.Caso[EntradaIDConv, EtiquetaConversacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIDConv, EtiquetaConversacion]{
		Accion: accion, Permiso: nucleo.PermConversacionesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIDConv) (EtiquetaConversacion, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return EtiquetaConversacion{}, err
			}
			q := mensajeriabd.New(t)
			antes, err := q.EtiquetaParaActualizar(ctx, mensajeriabd.EtiquetaParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return EtiquetaConversacion{}, nucleo.NoEncontrado("La etiqueta no existe")
			}
			if err != nil || antes.Activa == activa {
				return aEtiqueta(antes), err
			}
			t.FijarEmpresa(a.empresa)
			f, err := q.FijarEtiquetaActiva(ctx, mensajeriabd.FijarEtiquetaActivaParams{ID: in.ID, Activa: activa})
			if err != nil {
				return EtiquetaConversacion{}, err
			}
			t.RegistrarCambios("etiqueta", f.ID, map[string]any{"activa": antes.Activa}, map[string]any{"activa": f.Activa})
			return aEtiqueta(f), t.Emitir("etiqueta.actualizada", map[string]any{"etiqueta_id": f.ID, "empresa_id": f.EmpresaID, "campos": []string{"activa"}, "activa": f.Activa})
		},
	})
}

// Respuestas rápidas -------------------------------------------------------------------------------

// RespuestaRapida es un texto guardado para responder.
type RespuestaRapida struct {
	ID      int64  `json:"id"`
	ColaID  *int64 `json:"colaId" nullable:"true" doc:"null = todas las colas"`
	Atajo   string `json:"atajo" doc:"/visita"`
	Titulo  string `json:"titulo"`
	Texto   string `json:"texto" doc:"Texto plano; {{nombre}} lo reemplaza el panel"`
	Activa  bool   `json:"activa"`
	Version int32  `json:"version"`
}

// ListaRespuestasRapidas es { data, nextCursor }.
type ListaRespuestasRapidas struct {
	Data       []RespuestaRapida `json:"data"`
	NextCursor *string           `json:"nextCursor" nullable:"true"`
}

// EntradaListarRespuestas filtra las respuestas rápidas.
type EntradaListarRespuestas struct {
	ColaID  int64  `query:"colaId" minimum:"0" doc:"Las de esta cola y las generales"`
	Q       string `query:"q" maxLength:"80"`
	Inactiv bool   `query:"incluirInactivas" doc:"También las desactivadas (para administrarlas)"`
}

// EntradaRespuestaRapida crea o actualiza una respuesta rápida.
type EntradaRespuestaRapida struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version,omitempty" minimum:"0" doc:"Obligatoria al actualizar"`
	ColaID  *int64 `json:"colaId,omitempty" minimum:"1" nullable:"true"`
	Atajo   string `json:"atajo" pattern:"^/[a-z0-9_-]{1,30}$"`
	Titulo  string `json:"titulo" minLength:"1" maxLength:"80"`
	Texto   string `json:"texto" minLength:"1" maxLength:"4000"`
}

func aRespuesta(f mensajeriabd.MensajeriaRespuestaRapida) RespuestaRapida {
	return RespuestaRapida{ID: f.ID, ColaID: f.ColaID, Atajo: f.Atajo, Titulo: f.Titulo, Texto: f.Texto, Activa: f.Activa, Version: f.Version}
}

func nuevoListarRespuestas(*Modulo) *auditoria.Caso[EntradaListarRespuestas, ListaRespuestasRapidas] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarRespuestas, ListaRespuestasRapidas]{
		Accion: "conversaciones.listar_respuestas", Permiso: nucleo.PermConversacionesResponder, AuditarExito: sinAuditar[ListaRespuestasRapidas],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarRespuestas) (ListaRespuestasRapidas, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaRespuestasRapidas{}, err
			}
			var cola *int64
			if in.ColaID > 0 {
				cola = &in.ColaID
			}
			fs, err := mensajeriabd.New(t).ListarRespuestasRapidas(ctx, mensajeriabd.ListarRespuestasRapidasParams{Empresa: a.empresa,
				Todas: in.Inactiv, ColaID: cola, Q: strings.TrimSpace(in.Q)})
			out := ListaRespuestasRapidas{Data: make([]RespuestaRapida, 0, len(fs))}
			for _, f := range fs {
				out.Data = append(out.Data, aRespuesta(f))
			}
			return out, err
		},
	})
}

func respuestaAuditable(r RespuestaRapida) map[string]any {
	// El texto no es de un cliente: se audita su largo, no el contenido.
	return map[string]any{"cola_id": r.ColaID, "atajo": r.Atajo, "titulo": r.Titulo, "largo_texto": len([]rune(r.Texto))}
}

func nuevoGuardarRespuesta(m *Modulo, crear bool) *auditoria.Caso[EntradaRespuestaRapida, RespuestaRapida] {
	accion := "conversaciones.actualizar_respuesta"
	if crear {
		accion = "conversaciones.crear_respuesta"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaRespuestaRapida, RespuestaRapida]{
		Accion: accion, Permiso: nucleo.PermConversacionesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRespuestaRapida) (RespuestaRapida, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return RespuestaRapida{}, err
			}
			q := mensajeriabd.New(t)
			if err := m.colaDeEmpresa(ctx, q, a.empresa, in.ColaID); err != nil {
				return RespuestaRapida{}, err
			}
			t.FijarEmpresa(a.empresa)
			dup := func(err error) error {
				if esDuplicado(err) {
					return nucleo.Conflicto("atajo_existente", "Ya hay una respuesta rápida con ese atajo")
				}
				return err
			}
			if crear {
				f, err := q.InsertarRespuestaRapida(ctx, mensajeriabd.InsertarRespuestaRapidaParams{EmpresaID: a.empresaPtr(), ColaID: in.ColaID,
					Atajo: in.Atajo, Titulo: in.Titulo, Texto: in.Texto})
				if err != nil {
					return RespuestaRapida{}, dup(err)
				}
				r := aRespuesta(f)
				t.RegistrarCambios("respuesta_rapida", f.ID, nil, respuestaAuditable(r))
				return r, t.Emitir("respuesta_rapida.creada", map[string]any{"respuesta_id": f.ID, "empresa_id": f.EmpresaID, "atajo": f.Atajo})
			}
			antes, err := q.RespuestaRapidaParaActualizar(ctx, mensajeriabd.RespuestaRapidaParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return RespuestaRapida{}, nucleo.NoEncontrado("La respuesta rápida no existe")
			}
			if err != nil {
				return RespuestaRapida{}, err
			}
			if antes.Version != in.Version {
				return RespuestaRapida{}, nucleo.ConflictoVersion("respuesta_rapida", in.ID)
			}
			f, err := q.ActualizarRespuestaRapida(ctx, mensajeriabd.ActualizarRespuestaRapidaParams{ID: in.ID, ColaID: in.ColaID, Atajo: in.Atajo,
				Titulo: in.Titulo, Texto: in.Texto})
			if err != nil {
				return RespuestaRapida{}, dup(err)
			}
			r := aRespuesta(f)
			campos := t.RegistrarCambios("respuesta_rapida", f.ID, respuestaAuditable(aRespuesta(antes)), respuestaAuditable(r))
			if campos == nil {
				campos = []string{}
			}
			return r, t.Emitir("respuesta_rapida.actualizada", map[string]any{"respuesta_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activa": f.Activa})
		},
	})
}

func nuevoCambioRespuesta(_ *Modulo, accion string, activa bool) *auditoria.Caso[EntradaIDConv, RespuestaRapida] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIDConv, RespuestaRapida]{
		Accion: accion, Permiso: nucleo.PermConversacionesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIDConv) (RespuestaRapida, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return RespuestaRapida{}, err
			}
			q := mensajeriabd.New(t)
			antes, err := q.RespuestaRapidaParaActualizar(ctx, mensajeriabd.RespuestaRapidaParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return RespuestaRapida{}, nucleo.NoEncontrado("La respuesta rápida no existe")
			}
			if err != nil || antes.Activa == activa {
				return aRespuesta(antes), err
			}
			t.FijarEmpresa(a.empresa)
			f, err := q.FijarRespuestaRapidaActiva(ctx, mensajeriabd.FijarRespuestaRapidaActivaParams{ID: in.ID, Activa: activa})
			if err != nil {
				return RespuestaRapida{}, err
			}
			t.RegistrarCambios("respuesta_rapida", f.ID, map[string]any{"activa": antes.Activa}, map[string]any{"activa": f.Activa})
			return aRespuesta(f), t.Emitir("respuesta_rapida.actualizada", map[string]any{"respuesta_id": f.ID, "empresa_id": f.EmpresaID,
				"campos": []string{"activa"}, "activa": f.Activa})
		},
	})
}
