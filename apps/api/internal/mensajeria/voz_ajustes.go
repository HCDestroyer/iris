package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Ajustes de llamadas por número (§3.9), ajustes de voz de la empresa (los que usa E1) y el
// mantenimiento de las llamadas vivas.

// RequisitoLlamadas es un requisito de Meta para tener llamadas en un número.
type RequisitoLlamadas struct {
	Codigo  string `json:"codigo" enum:"conexion_api,limite_mensajeria,webhook_calls,calling_habilitado,plantilla_permiso"`
	Cumple  bool   `json:"cumple"`
	Detalle string `json:"detalle" doc:"Texto llano, listo para mostrar (nunca el mensaje crudo de Meta)"`
	// Aviso: no impide encender las llamadas.
	Aviso bool `json:"aviso"`
}

// RequisitosLlamadas es la última comprobación.
type RequisitosLlamadas struct {
	Cumple       bool                `json:"cumple"`
	Requisitos   []RequisitoLlamadas `json:"requisitos"`
	ComprobadoEn *time.Time          `json:"comprobadoEn,omitempty"`
}

// MensajePerdida: el mensaje automático por una llamada que nadie contestó.
type MensajePerdida struct {
	Activo bool   `json:"activo"`
	Texto  string `json:"texto" minLength:"1" maxLength:"500"`
}

// LlamadasCanal son los ajustes de llamadas de un número.
type LlamadasCanal struct {
	CanalID         int64              `json:"canalId"`
	Version         int32              `json:"version" doc:"0 si nunca se configuraron"`
	TipoConexion    string             `json:"tipoConexion" enum:"api,qr" doc:"Las llamadas solo existen en números por API"`
	Disponible      bool               `json:"disponible" doc:"false en un número por QR: el panel avisa y no deja encenderlas"`
	LlamadasActivas bool               `json:"llamadasActivas"`
	ColaID          *int64             `json:"colaId,omitempty" doc:"Ausente: la cola del número"`
	HorarioID       *int64             `json:"horarioId,omitempty" doc:"Ausente: el de la cola o el del número"`
	SegundosSonando int32              `json:"segundosSonando"`
	FueraDeHorario  string             `json:"fueraDeHorario" enum:"rechazar_con_mensaje,buzon,asistente"`
	IconoVisible    bool               `json:"iconoVisible"`
	Devolucion      bool               `json:"devolucion"`
	MensajePerdida  MensajePerdida     `json:"mensajePerdida"`
	Requisitos      RequisitosLlamadas `json:"requisitos"`
	SincronizadoEn  *time.Time         `json:"sincronizadoEn,omitempty"`
}

// EntradaLlamadasCanal es POST /canales/{id}/llamadas/configurar.
type EntradaLlamadasCanal struct {
	ID              int64          `json:"-"`
	Version         int32          `json:"version" minimum:"0"`
	LlamadasActivas bool           `json:"llamadasActivas"`
	ColaID          *int64         `json:"colaId,omitempty" minimum:"1"`
	HorarioID       *int64         `json:"horarioId,omitempty" minimum:"1"`
	SegundosSonando int32          `json:"segundosSonando" minimum:"15" maximum:"45"`
	FueraDeHorario  string         `json:"fueraDeHorario" enum:"rechazar_con_mensaje,buzon,asistente"`
	IconoVisible    bool           `json:"iconoVisible"`
	Devolucion      bool           `json:"devolucion"`
	MensajePerdida  MensajePerdida `json:"mensajePerdida"`
}

func llamadasDeFabrica(canal int64, empresa *int64) mensajeriabd.MensajeriaVozAjustesCanal {
	return mensajeriabd.MensajeriaVozAjustesCanal{CanalID: canal, EmpresaID: empresa, SegundosSonando: 30, FueraDeHorario: "rechazar_con_mensaje",
		IconoVisible: true, MensajePerdida: true, MensajePerdidaTexto: "No pudimos contestar tu llamada. Escríbenos por aquí y te atendemos."}
}

func aLlamadasCanal(c mensajeriabd.MensajeriaCanal, f mensajeriabd.MensajeriaVozAjustesCanal, guardado bool) LlamadasCanal {
	r := LlamadasCanal{CanalID: c.ID, TipoConexion: c.TipoConexion, Disponible: c.TipoConexion == ConexionAPI, LlamadasActivas: f.LlamadasActivas,
		ColaID: f.ColaID, HorarioID: f.HorarioID, SegundosSonando: f.SegundosSonando, FueraDeHorario: f.FueraDeHorario, IconoVisible: f.IconoVisible,
		Devolucion: f.Devolucion, MensajePerdida: MensajePerdida{Activo: f.MensajePerdida, Texto: f.MensajePerdidaTexto},
		Requisitos: RequisitosLlamadas{Requisitos: []RequisitoLlamadas{}}, SincronizadoEn: f.SincronizadoEn}
	if guardado {
		r.Version = f.Version
	}
	if len(f.Requisitos) > 0 {
		_ = json.Unmarshal(f.Requisitos, &r.Requisitos)
		if r.Requisitos.Requisitos == nil {
			r.Requisitos.Requisitos = []RequisitoLlamadas{}
		}
	}
	return r
}

// canalDeLlamadas: el número (propio de la empresa activa: los ajustes de llamadas son de la dueña).
func canalDeLlamadas(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, id int64) (mensajeriabd.MensajeriaCanal, error) {
	c, err := q.CanalPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && derefEmpresa(c.EmpresaID) != a.empresa) {
		return c, nucleo.NoEncontrado("El número no existe")
	}
	return c, err
}

func leerLlamadasCanal(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCanal, bloquear bool) (mensajeriabd.MensajeriaVozAjustesCanal, bool, error) {
	var f mensajeriabd.MensajeriaVozAjustesCanal
	var err error
	if bloquear {
		f, err = q.VozAjustesCanalParaActualizar(ctx, c.ID)
	} else {
		f, err = q.VozAjustesCanal(ctx, c.ID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return llamadasDeFabrica(c.ID, c.EmpresaID), false, nil
	}
	return f, true, err
}

func guardarLlamadasCanal(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaVozAjustesCanal) (mensajeriabd.MensajeriaVozAjustesCanal, error) {
	return q.GuardarVozAjustesCanal(ctx, mensajeriabd.GuardarVozAjustesCanalParams{CanalID: f.CanalID, EmpresaID: f.EmpresaID, LlamadasActivas: f.LlamadasActivas,
		Requisitos: f.Requisitos, ColaID: f.ColaID, HorarioID: f.HorarioID, SegundosSonando: f.SegundosSonando, FueraDeHorario: f.FueraDeHorario,
		IconoVisible: f.IconoVisible, Devolucion: f.Devolucion, HorasMeta: f.HorasMeta, MensajePerdida: f.MensajePerdida,
		MensajePerdidaTexto: f.MensajePerdidaTexto, SincronizadoEn: f.SincronizadoEn})
}

func nuevoVerLlamadasCanal(*Modulo) *auditoria.Caso[EntradaIDConv, LlamadasCanal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIDConv, LlamadasCanal]{
		Accion: "llamadas.ver_ajustes", Permiso: nucleo.PermLlamadasConfigurar, AuditarExito: sinAuditar[LlamadasCanal],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIDConv) (LlamadasCanal, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasConfigurar)
			if err != nil {
				return LlamadasCanal{}, err
			}
			q := mensajeriabd.New(t)
			c, err := canalDeLlamadas(ctx, q, a, in.ID)
			if err != nil {
				return LlamadasCanal{}, err
			}
			f, guardado, err := leerLlamadasCanal(ctx, q, c, false)
			return aLlamadasCanal(c, f, guardado), err
		},
	})
}

func nuevoConfigurarLlamadasCanal(m *Modulo) *auditoria.Caso[EntradaLlamadasCanal, LlamadasCanal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamadasCanal, LlamadasCanal]{
		Accion: "llamadas.configurar", Permiso: nucleo.PermLlamadasConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamadasCanal) (LlamadasCanal, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasConfigurar)
			if err != nil {
				return LlamadasCanal{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("canal_mensajeria", in.ID)
			q := mensajeriabd.New(t)
			c, err := canalDeLlamadas(ctx, q, a, in.ID)
			if err != nil {
				return LlamadasCanal{}, err
			}
			antes, guardado, err := leerLlamadasCanal(ctx, q, c, true)
			if err != nil {
				return LlamadasCanal{}, err
			}
			if (!guardado && in.Version != 0) || (guardado && antes.Version != in.Version) {
				return LlamadasCanal{}, nucleo.ConflictoVersion("llamadas_canal", in.ID)
			}
			// De fábrica y sin excepción: llamadas solo en números por API.
			if in.LlamadasActivas && c.TipoConexion != ConexionAPI {
				return LlamadasCanal{}, reglaVoz("llamadas_no_disponibles_qr", "Las llamadas de WhatsApp solo se pueden encender en un número conectado por API, no en uno por QR")
			}
			if in.SegundosSonando < 15 || in.SegundosSonando > 45 {
				return LlamadasCanal{}, campoConv("segundosSonando", "Entre 15 y 45 segundos", "rango")
			}
			switch in.FueraDeHorario {
			case "rechazar_con_mensaje", "buzon":
			case "asistente":
				return LlamadasCanal{}, reglaVoz("asistente_voz_no_disponible", "El asistente todavía no contesta llamadas: elige rechazar con mensaje o buzón de voz",
					nucleo.ErrorCampo{Field: "fueraDeHorario", Message: "Aún no disponible", Rule: "asistente_voz_no_disponible"})
			default:
				return LlamadasCanal{}, campoConv("fueraDeHorario", "rechazar_con_mensaje o buzon", "enum")
			}
			texto := strings.TrimSpace(in.MensajePerdida.Texto)
			if texto == "" || utf8.RuneCountInString(texto) > 500 {
				return LlamadasCanal{}, campoConv("mensajePerdida.texto", "Entre 1 y 500 caracteres", "longitud")
			}
			if in.ColaID != nil {
				if co, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *in.ColaID, Empresa: a.empresa}); errors.Is(err, pgx.ErrNoRows) || (err == nil && !co.Activa) {
					return LlamadasCanal{}, campoConv("colaId", "No es una cola activa de la empresa", "cola")
				} else if err != nil {
					return LlamadasCanal{}, err
				}
			}
			if err := m.horarioDeEmpresa(ctx, q, a.empresa, in.HorarioID); err != nil {
				return LlamadasCanal{}, err
			}
			if in.LlamadasActivas && !antes.LlamadasActivas {
				var r RequisitosLlamadas
				_ = json.Unmarshal(antes.Requisitos, &r)
				if !r.Cumple {
					errs := []nucleo.ErrorCampo{}
					for _, x := range r.Requisitos {
						if !x.Cumple && !x.Aviso {
							errs = append(errs, nucleo.ErrorCampo{Field: x.Codigo, Message: x.Detalle, Rule: "requisito"})
						}
					}
					return LlamadasCanal{}, reglaVoz("requisitos_sin_cumplir", "El número todavía no cumple los requisitos de WhatsApp para tener llamadas. Usa «Comprobar» y revisa cada uno", errs...)
				}
			}
			f := antes
			f.LlamadasActivas, f.ColaID, f.HorarioID, f.SegundosSonando, f.FueraDeHorario = in.LlamadasActivas, in.ColaID, in.HorarioID, in.SegundosSonando, in.FueraDeHorario
			f.IconoVisible, f.Devolucion, f.MensajePerdida, f.MensajePerdidaTexto = in.IconoVisible, in.Devolucion, in.MensajePerdida.Activo, texto
			if f, err = guardarLlamadasCanal(ctx, q, f); err != nil {
				return LlamadasCanal{}, err
			}
			vista := func(x mensajeriabd.MensajeriaVozAjustesCanal) map[string]any {
				return map[string]any{"llamadas_activas": x.LlamadasActivas, "cola_id": x.ColaID, "horario_id": x.HorarioID, "segundos_sonando": x.SegundosSonando,
					"fuera_de_horario": x.FueraDeHorario, "icono_visible": x.IconoVisible, "devolucion": x.Devolucion, "mensaje_perdida": x.MensajePerdida,
					"mensaje_perdida_texto": x.MensajePerdidaTexto}
			}
			campos := t.RegistrarCambios("llamadas_canal", c.ID, vista(antes), vista(f))
			if campos == nil {
				campos = []string{}
			}
			// Lo que también vive en Meta se escribe después del COMMIT (sin transacción abierta).
			if c.TipoConexion == ConexionAPI && (antes.LlamadasActivas != f.LlamadasActivas || antes.IconoVisible != f.IconoVisible || antes.Devolucion != f.Devolucion) {
				if cg, err := m.canalGraphDe(ctx, q, c.ID); err == nil {
					meta := AjustesMetaLlamadas{Habilitadas: f.LlamadasActivas, IconoVisible: f.IconoVisible, Devolucion: f.Devolucion}
					t.AlConfirmar(func(ctx context.Context) {
						if err := m.graph().FijarAjustes(context.WithoutCancel(ctx), cg, meta); err != nil {
							slog.WarnContext(ctx, "voz: escribir en Meta los ajustes de llamadas del número; usa «Sincronizar»", "canal_id", c.ID, "err", err)
						}
					})
				}
			}
			return aLlamadasCanal(c, f, true), t.Emitir("canal_mensajeria.llamadas_cambiadas", map[string]any{"canal_id": c.ID, "empresa_id": c.EmpresaID,
				"campos": campos, "llamadas_activas": f.LlamadasActivas})
		},
	})
}

// Comprobar los requisitos de Meta (dos fases: leer el número, preguntar a Meta, guardar) --------------

type canalPorComprobar struct {
	canal mensajeriabd.MensajeriaCanal
	graph CanalGraph
}

type entradaComprobado struct {
	ID          int64 `json:"id"`
	Sincronizar bool  `json:"sincronizar"`
	meta        AjustesMetaLlamadas
	fallo       bool
}

func nuevoPrepararComprobacion(m *Modulo) *auditoria.Caso[EntradaIDConv, canalPorComprobar] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIDConv, canalPorComprobar]{
		Accion: "llamadas.comprobar", Permiso: nucleo.PermLlamadasConfigurar, AuditarExito: sinAuditar[canalPorComprobar],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIDConv) (canalPorComprobar, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasConfigurar)
			if err != nil {
				return canalPorComprobar{}, err
			}
			q := mensajeriabd.New(t)
			c, err := canalDeLlamadas(ctx, q, a, in.ID)
			if err != nil {
				return canalPorComprobar{}, err
			}
			r := canalPorComprobar{canal: c}
			if c.TipoConexion == ConexionAPI {
				if r.graph, err = m.canalGraphDe(ctx, q, c.ID); err != nil {
					return canalPorComprobar{}, err
				}
			}
			return r, nil
		},
	})
}

func nuevoGuardarComprobacion(m *Modulo) *auditoria.Caso[entradaComprobado, LlamadasCanal] {
	return auditoria.Nuevo(auditoria.Caso[entradaComprobado, LlamadasCanal]{
		Accion: "llamadas.comprobar", Permiso: nucleo.PermLlamadasConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaComprobado) (LlamadasCanal, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasConfigurar)
			if err != nil {
				return LlamadasCanal{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("canal_mensajeria", in.ID)
			q := mensajeriabd.New(t)
			c, err := canalDeLlamadas(ctx, q, a, in.ID)
			if err != nil {
				return LlamadasCanal{}, err
			}
			f, _, err := leerLlamadasCanal(ctx, q, c, true)
			if err != nil {
				return LlamadasCanal{}, err
			}
			ahora := m.ahora()
			api := c.TipoConexion == ConexionAPI
			si := func(ok bool, bien, mal string) string {
				if ok {
					return bien
				}
				return mal
			}
			sinMeta := "No se pudo consultar a WhatsApp. Vuelve a intentar"
			leido := api && !in.fallo
			reqs := []RequisitoLlamadas{
				{Codigo: "conexion_api", Cumple: api, Detalle: si(api, "El número está conectado por API", "El número está conectado por QR: las llamadas no están disponibles")},
				{Codigo: "limite_mensajeria", Cumple: leido && in.meta.LimiteMensajeria >= 2000, Detalle: si(leido,
					si(in.meta.LimiteMensajeria >= 2000, "El límite de mensajería del número alcanza", "WhatsApp exige un límite de al menos 2 000 destinatarios por día"), sinMeta)},
				{Codigo: "webhook_calls", Cumple: leido && in.meta.WebhookCalls, Detalle: si(leido,
					si(in.meta.WebhookCalls, "La app recibe los avisos de llamadas", "Falta suscribir el campo «calls» en el webhook de la app de Meta"), sinMeta)},
				{Codigo: "calling_habilitado", Cumple: leido && in.meta.Habilitadas, Detalle: si(leido,
					si(in.meta.Habilitadas, "Las llamadas están habilitadas en el número", "Falta habilitar las llamadas en el número, en WhatsApp Manager"), sinMeta)},
				{Codigo: "plantilla_permiso", Aviso: true, Detalle: "Para devolver llamadas fuera de la ventana de 24 h hará falta la plantilla de permiso (llega con las llamadas salientes)"},
			}
			res := RequisitosLlamadas{Cumple: true, Requisitos: reqs, ComprobadoEn: &ahora}
			for _, r := range reqs {
				res.Cumple = res.Cumple && (r.Cumple || r.Aviso)
			}
			if f.Requisitos, err = json.Marshal(res); err != nil {
				return LlamadasCanal{}, err
			}
			if in.Sincronizar && leido {
				f.IconoVisible, f.Devolucion, f.SincronizadoEn = in.meta.IconoVisible, in.meta.Devolucion, &ahora
			}
			// Si Meta ya no cumple, las llamadas no pueden seguir encendidas.
			if f.LlamadasActivas && !res.Cumple && leido {
				f.LlamadasActivas = false
			}
			t.Anotar(map[string]any{"cumple": res.Cumple, "sincronizar": in.Sincronizar})
			if f, err = guardarLlamadasCanal(ctx, q, f); err != nil {
				return LlamadasCanal{}, err
			}
			return aLlamadasCanal(c, f, true), nil
		},
	})
}

// ComprobarLlamadas pregunta a Meta si el número cumple los requisitos y guarda el resultado.
// Con sincronizar, además copia a Iris el icono y la devolución que tiene Meta.
func (m *Modulo) ComprobarLlamadas(ctx context.Context, id int64, sincronizar bool) (LlamadasCanal, error) {
	p, err := m.Voz.prepararCanal.Ejecutar(ctx, EntradaIDConv{ID: id})
	if err != nil {
		return LlamadasCanal{}, err
	}
	in := entradaComprobado{ID: id, Sincronizar: sincronizar}
	if p.canal.TipoConexion == ConexionAPI {
		if in.meta, err = m.graph().Ajustes(ctx, p.graph); err != nil {
			in.fallo = true
		}
	}
	return m.Voz.comprobar.Ejecutar(ctx, in)
}

// Ajustes de voz de la empresa (E1) -------------------------------------------------------------------

// AjustesVoz son los ajustes de voz de la empresa que usa E1 (E2 los amplía).
type AjustesVoz struct {
	Version                  int32  `json:"version" doc:"0 si nunca se guardaron (valores de fábrica)"`
	Grabar                   string `json:"grabar" enum:"nunca,todas,asistente" doc:"De fábrica nunca. asistente no graba nada hasta E2"`
	DuracionMaxPersonaMin    int32  `json:"duracionMaxPersonaMin" doc:"Una llamada contestada por una persona se corta a estos minutos (10–120)"`
	SegundosAsignado         int32  `json:"segundosAsignado" doc:"Cuánto suena primero solo a quien tiene la conversación (0–20)"`
	RetencionGrabacionesDias int32  `json:"retencionGrabacionesDias" doc:"Días que se guarda una grabación (1–365)"`
}

// EntradaAjustesVoz es POST /voz/ajustes/actualizar.
type EntradaAjustesVoz struct {
	Version                  int32  `json:"version" minimum:"0"`
	Grabar                   string `json:"grabar" enum:"nunca,todas,asistente"`
	DuracionMaxPersonaMin    int32  `json:"duracionMaxPersonaMin" minimum:"10" maximum:"120"`
	SegundosAsignado         int32  `json:"segundosAsignado" minimum:"0" maximum:"20"`
	RetencionGrabacionesDias int32  `json:"retencionGrabacionesDias" minimum:"1" maximum:"365"`
}

func aAjustesVoz(f mensajeriabd.MensajeriaVozAjuste) AjustesVoz {
	return AjustesVoz{Version: f.Version, Grabar: f.Grabar, DuracionMaxPersonaMin: f.DuracionMaxPersonaMin, SegundosAsignado: f.SegundosAsignado,
		RetencionGrabacionesDias: f.RetencionGrabacionesDias}
}

func nuevoVerAjustesVoz(*Modulo) *auditoria.Caso[struct{}, AjustesVoz] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesVoz]{
		Accion: "voz.ver_ajustes", Permiso: nucleo.PermVozConfigurar, AuditarExito: sinAuditar[AjustesVoz],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesVoz, error) {
			a, err := alcanceVoz(ctx, nucleo.PermVozConfigurar)
			if err != nil {
				return AjustesVoz{}, err
			}
			f, err := ajustesVoz(ctx, mensajeriabd.New(t), a.empresa)
			return aAjustesVoz(f), err
		},
	})
}

func nuevoFijarAjustesVoz(*Modulo) *auditoria.Caso[EntradaAjustesVoz, AjustesVoz] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAjustesVoz, AjustesVoz]{
		Accion: "voz.configurar", Permiso: nucleo.PermVozConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAjustesVoz) (AjustesVoz, error) {
			a, err := alcanceVoz(ctx, nucleo.PermVozConfigurar)
			if err != nil {
				return AjustesVoz{}, err
			}
			t.FijarEmpresa(a.empresa)
			switch {
			case in.Grabar != "nunca" && in.Grabar != "todas" && in.Grabar != "asistente":
				return AjustesVoz{}, campoConv("grabar", "nunca, todas o asistente", "enum")
			case in.DuracionMaxPersonaMin < 10 || in.DuracionMaxPersonaMin > 120:
				return AjustesVoz{}, campoConv("duracionMaxPersonaMin", "Entre 10 y 120 minutos", "rango")
			case in.SegundosAsignado < 0 || in.SegundosAsignado > 20:
				return AjustesVoz{}, campoConv("segundosAsignado", "Entre 0 y 20 segundos", "rango")
			case in.RetencionGrabacionesDias < 1 || in.RetencionGrabacionesDias > 365:
				return AjustesVoz{}, campoConv("retencionGrabacionesDias", "Entre 1 y 365 días", "rango")
			}
			q := mensajeriabd.New(t)
			antes, err := q.VozAjustesParaActualizar(ctx, a.empresa)
			nuevo := errors.Is(err, pgx.ErrNoRows)
			if err != nil && !nuevo {
				return AjustesVoz{}, err
			}
			if (nuevo && in.Version != 0) || (!nuevo && antes.Version != in.Version) {
				return AjustesVoz{}, nucleo.ConflictoVersion("voz_ajustes", a.empresa)
			}
			var f mensajeriabd.MensajeriaVozAjuste
			if nuevo {
				antes, _ = ajustesVoz(ctx, q, -1)
				f, err = q.InsertarVozAjustes(ctx, mensajeriabd.InsertarVozAjustesParams{EmpresaID: a.empresaPtr(), Grabar: in.Grabar,
					DuracionMaxPersonaMin: in.DuracionMaxPersonaMin, SegundosAsignado: in.SegundosAsignado, RetencionGrabacionesDias: in.RetencionGrabacionesDias})
				if errors.Is(err, pgx.ErrNoRows) {
					return AjustesVoz{}, nucleo.ConflictoVersion("voz_ajustes", a.empresa)
				}
			} else {
				f, err = q.ActualizarVozAjustes(ctx, mensajeriabd.ActualizarVozAjustesParams{ID: antes.ID, Grabar: in.Grabar,
					DuracionMaxPersonaMin: in.DuracionMaxPersonaMin, SegundosAsignado: in.SegundosAsignado, RetencionGrabacionesDias: in.RetencionGrabacionesDias})
			}
			if err != nil {
				return AjustesVoz{}, err
			}
			t.FijarEntidad("voz_ajustes", f.ID)
			vista := func(x mensajeriabd.MensajeriaVozAjuste) map[string]any {
				return map[string]any{"grabar": x.Grabar, "duracion_max_persona_min": x.DuracionMaxPersonaMin, "segundos_asignado": x.SegundosAsignado,
					"retencion_grabaciones_dias": x.RetencionGrabacionesDias}
			}
			campos := t.RegistrarCambios("voz_ajustes", f.ID, vista(antes), vista(f))
			if campos == nil {
				campos = []string{}
			}
			return aAjustesVoz(f), t.Emitir("voz.ajustes_cambiados", map[string]any{"empresa_id": f.EmpresaID, "campos": campos})
		},
	})
}

// Mantenimiento de las llamadas vivas (cada 5 s) -------------------------------------------------------

type mantenimientoLlamada struct {
	Cambio string `json:"cambio,omitempty"`
}

// nuevoMantenerLlamada aplica a una llamada viva lo que el tiempo exige: perdida al vencer
// segundosSonando, soltar una reserva vencida, ampliar a la cola tras segundosAsignado, cortar por
// duración máxima o por el panel caído, y resolver una intención con Meta que quedó a medias.
func nuevoMantenerLlamada(m *Modulo) *auditoria.Caso[uuid.UUID, mantenimientoLlamada] {
	return auditoria.Nuevo(auditoria.Caso[uuid.UUID, mantenimientoLlamada]{
		Accion: "llamadas.mantener", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r mantenimientoLlamada) bool { return r.Cambio != "" },
		Logica: func(ctx context.Context, t *auditoria.Tx, id uuid.UUID) (mantenimientoLlamada, error) {
			nada := mantenimientoLlamada{}
			if err := soloSistemaVoz(ctx); err != nil {
				return nada, err
			}
			q := mensajeriabd.New(t)
			l, err := q.LlamadaParaActualizar(ctx, id)
			if err != nil {
				return nada, err
			}
			t.FijarEmpresa(derefEmpresa(l.EmpresaID))
			t.FijarEntidad("llamada", l.ID)
			ahora := m.ahora()
			pendiente := l.AccionMetaEstado != nil && *l.AccionMetaEstado == "pendiente" && l.AccionMetaEn != nil
			if llamadaFinal(l.Estado) {
				// terminate y reject son idempotentes en Meta: se repiten (hasta 2 minutos).
				if pendiente && ahora.Sub(*l.AccionMetaEn) > 2*PlazoMeta {
					if l.TerminadaEn != nil && ahora.Sub(*l.TerminadaEn) > 2*time.Minute {
						f := "fallida"
						l.AccionMetaEstado = &f
						_, err := guardarLlamada(ctx, q, l)
						return mantenimientoLlamada{Cambio: "accion_meta_agotada"}, err
					}
					l.AccionMetaEn = &ahora
					if _, err := guardarLlamada(ctx, q, l); err != nil {
						return nada, err
					}
					m.trasCommitMeta(ctx, t, l.ID)
				}
				return nada, nil
			}
			if l.ConversacionID == nil {
				return nada, nil
			}
			c, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
			if err != nil {
				return nada, err
			}
			cerrar := func(estado, motivo, accion string) (mantenimientoLlamada, error) {
				if err := m.cerrarLlamada(ctx, t, &l, &c, estado, motivo, ahora, accion); err != nil {
					return nada, err
				}
				_, err := guardarConv(ctx, q, c, false)
				return mantenimientoLlamada{Cambio: motivo}, err
			}
			// Una intención pre_accept o accept que quedó a medias no se repite a ciegas.
			if pendiente && ahora.Sub(*l.AccionMetaEn) > 2*PlazoMeta {
				return cerrar(LlamadaFallida, "error_meta", MetaTerminar)
			}
			voz, err := ajustesVoz(ctx, q, derefEmpresa(l.EmpresaID))
			if err != nil {
				return nada, err
			}
			switch l.Estado {
			case LlamadaSonando:
				if !ahora.Before(l.SuenaHasta) {
					return cerrar(LlamadaPerdida, "nadie_contesto", MetaRechazar)
				}
				if n, err := m.ofrecerLlamada(ctx, t, &l, c, voz, ahora); err != nil || n > 0 {
					return mantenimientoLlamada{Cambio: "ofrecida"}, err
				}
			case LlamadaConectando:
				if pendiente {
					return nada, nil // Meta todavía puede contestar
				}
				if l.ReclamadaHasta != nil && !ahora.Before(*l.ReclamadaHasta) {
					if !ahora.Before(l.SuenaHasta) {
						return cerrar(LlamadaPerdida, "nadie_contesto", MetaRechazar)
					}
					return mantenimientoLlamada{Cambio: "reserva_vencida"}, m.soltar(ctx, t, &l, ahora)
				}
			case LlamadaEnCurso:
				if l.ContestadaEn != nil && ahora.Sub(*l.ContestadaEn) >= time.Duration(voz.DuracionMaxPersonaMin)*time.Minute {
					return cerrar(LlamadaTerminada, "duracion_maxima", MetaTerminar)
				}
				// El panel de quien la atiende se cerró o se cayó: Meta ya no recibe audio.
				if l.UsuarioID != nil && l.ContestadaEn != nil && ahora.Sub(*l.ContestadaEn) > FlujoVivo {
					vivo, err := q.ReceptorVivo(ctx, mensajeriabd.ReceptorVivoParams{Empresa: derefEmpresa(l.EmpresaID), UsuarioID: *l.UsuarioID,
						VivoDesde: ahora.Add(-FlujoVivo - GraciaSinFlujo)})
					if err != nil {
						return nada, err
					}
					if !vivo {
						return cerrar(LlamadaTerminada, "error_medio", MetaTerminar)
					}
				}
			}
			return nada, nil
		},
	})
}

// MantenerLlamadas recorre las llamadas vivas y las intenciones con Meta que quedaron a medias.
func (m *Modulo) MantenerLlamadas(ctx context.Context, bd nucleo.BD) error {
	ctx = ctxSistema(ctx, bd)
	q := mensajeriabd.New(bd)
	vivas, err := q.LlamadasVivas(ctx)
	if err != nil {
		return err
	}
	pendientes, err := q.LlamadasConAccionPendiente(ctx, ptrT(m.ahora().Add(-2*PlazoMeta)))
	if err != nil {
		return err
	}
	vistas := map[uuid.UUID]bool{}
	for _, l := range append(vivas, pendientes...) {
		if vistas[l.ID] {
			continue
		}
		vistas[l.ID] = true
		if _, err := m.Voz.mantener.Ejecutar(ctx, l.ID); err != nil {
			slog.WarnContext(ctx, "voz: mantenimiento de una llamada", "llamada_id", l.ID, "err", err)
		}
	}
	_, err = m.Voz.vencer.Ejecutar(ctx, struct{}{})
	return err
}

// ArgsMantenimientoVoz: el mantenimiento de las llamadas vivas, cada 5 s.
type ArgsMantenimientoVoz struct{}

// Kind implementa river.JobArgs.
func (ArgsMantenimientoVoz) Kind() string { return "mensajeria.mantenimiento_voz" }

// InsertOpts: uno cada 5 s aunque haya varias réplicas.
func (ArgsMantenimientoVoz) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Second}}
}

type trabajadorMantenimientoVoz struct {
	river.WorkerDefaults[ArgsMantenimientoVoz]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorMantenimientoVoz) Work(ctx context.Context, _ *river.Job[ArgsMantenimientoVoz]) error {
	return w.m.MantenerLlamadas(ctx, w.bd)
}
