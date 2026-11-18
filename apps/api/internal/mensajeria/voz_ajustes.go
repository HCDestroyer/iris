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
	// SinComprobar: Iris no puede comprobarlo (el panel lo muestra como «sin comprobar», no como cumplido).
	SinComprobar bool `json:"sinComprobar,omitempty"`
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
	CanalID         int64          `json:"canalId"`
	Version         int32          `json:"version" doc:"0 si nunca se configuraron"`
	TipoConexion    string         `json:"tipoConexion" enum:"api,qr" doc:"Las llamadas solo existen en números por API"`
	Disponible      bool           `json:"disponible" doc:"false en un número por QR: el panel avisa y no deja encenderlas"`
	LlamadasActivas bool           `json:"llamadasActivas"`
	ColaID          *int64         `json:"colaId,omitempty" doc:"Ausente: la cola del número"`
	HorarioID       *int64         `json:"horarioId,omitempty" doc:"Ausente: el de la cola o el del número"`
	SegundosSonando int32          `json:"segundosSonando"`
	FueraDeHorario  string         `json:"fueraDeHorario" enum:"rechazar_con_mensaje,buzon,asistente"`
	IconoVisible    bool           `json:"iconoVisible"`
	Devolucion      bool           `json:"devolucion"`
	MensajePerdida  MensajePerdida `json:"mensajePerdida"`
	// PlantillaPermiso: la plantilla de permiso de llamada del número (fuera de la ventana de 24 h).
	PlantillaPermiso       *string             `json:"plantillaPermiso,omitempty"`
	PlantillaPermisoIdioma string              `json:"plantillaPermisoIdioma,omitempty"`
	Requisitos             RequisitosLlamadas  `json:"requisitos"`
	SincronizadoEn         *time.Time          `json:"sincronizadoEn,omitempty"`
	WhatsApp               EstadoEnWhatsApp    `json:"whatsapp" doc:"Si los ajustes del número ya están aplicados en WhatsApp"`
	HorasWhatsApp          *HorasMeta          `json:"horasWhatsapp,omitempty" doc:"El horario de llamadas que tiene WhatsApp (espejo). Ausente: abierto siempre y el horario lo aplica Iris"`
	RechazadasApagadas     *RechazadasApagadas `json:"rechazadasApagadas,omitempty" doc:"Solo con llamadasActivas = false: llegaron llamadas a este número (en WhatsApp sí están encendidas) y se rechazaron porque en Iris están apagadas. Ausente si no hubo ninguna en los últimos 7 días"`
}

// RechazadasApagadas: llamadas rechazadas por tener las llamadas del número apagadas en Iris. Solo
// cuándo y cuántas: nunca quién llamó.
type RechazadasApagadas struct {
	UltimaEn time.Time `json:"ultimaEn" doc:"Cuándo se rechazó la última"`
	Hoy      int32     `json:"hoy" doc:"Cuántas se rechazaron hoy (día de Guatemala)"`
}

// conRechazadas añade el aviso de las llamadas rechazadas con las llamadas apagadas (si las hay).
func conRechazadas(ctx context.Context, q *mensajeriabd.Queries, out LlamadasCanal, ahora time.Time) (LlamadasCanal, error) {
	if out.LlamadasActivas || out.TipoConexion != ConexionAPI {
		return out, nil
	}
	r, err := q.RechazadasApagadas(ctx, mensajeriabd.RechazadasApagadasParams{CanalID: out.CanalID, Hoy: inicioDia(ahora), Desde: ahora.Add(-7 * 24 * time.Hour)})
	if err != nil || r.Total == 0 {
		return out, err
	}
	out.RechazadasApagadas = &RechazadasApagadas{UltimaEn: r.UltimaEn, Hoy: r.Hoy}
	return out, nil
}

// EstadoEnWhatsApp: la escritura en WhatsApp de los ajustes del número (encendido, icono, devolución
// y horario de llamadas) es un pendiente con reintentos.
type EstadoEnWhatsApp struct {
	Estado           string     `json:"estado" enum:"aplicado,pendiente,error" doc:"pendiente: «pendiente de aplicar en WhatsApp»; error: se agotaron los reintentos"`
	Intentos         int32      `json:"intentos"`
	ProximoIntentoEn *time.Time `json:"proximoIntentoEn,omitempty"`
	Detalle          *string    `json:"detalle,omitempty" doc:"Texto llano, nunca el mensaje crudo de Meta"`
}

// EsperasMeta: espera antes de cada reintento de escribir los ajustes en WhatsApp. Al agotarlas queda en error.
var EsperasMeta = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

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
	// PlantillaPermiso: opcional; ausente, se conserva la que hay. Vacía, se quita.
	PlantillaPermiso       *string `json:"plantillaPermiso,omitempty" maxLength:"200" pattern:"^[a-z0-9_]*$" doc:"Nombre de la plantilla call_permission_request aprobada del número (para pedir permiso de llamada fuera de la ventana de 24 h)"`
	PlantillaPermisoIdioma string  `json:"plantillaPermisoIdioma,omitempty" pattern:"^[a-z]{2}(_[A-Z]{2})?$" doc:"Idioma de esa plantilla (es de fábrica)"`
}

func llamadasDeFabrica(canal int64, empresa *int64) mensajeriabd.MensajeriaVozAjustesCanal {
	return mensajeriabd.MensajeriaVozAjustesCanal{CanalID: canal, EmpresaID: empresa, SegundosSonando: 30, FueraDeHorario: "rechazar_con_mensaje",
		IconoVisible: true, MensajePerdida: true, MensajePerdidaTexto: "No pudimos contestar tu llamada. Escríbenos por aquí y te atendemos."}
}

func aLlamadasCanal(c mensajeriabd.MensajeriaCanal, f mensajeriabd.MensajeriaVozAjustesCanal, guardado bool) LlamadasCanal {
	r := LlamadasCanal{CanalID: c.ID, TipoConexion: c.TipoConexion, Disponible: c.TipoConexion == ConexionAPI, LlamadasActivas: f.LlamadasActivas,
		ColaID: f.ColaID, HorarioID: f.HorarioID, SegundosSonando: f.SegundosSonando, FueraDeHorario: f.FueraDeHorario, IconoVisible: f.IconoVisible,
		Devolucion: f.Devolucion, MensajePerdida: MensajePerdida{Activo: f.MensajePerdida, Texto: f.MensajePerdidaTexto},
		Requisitos: RequisitosLlamadas{Requisitos: []RequisitoLlamadas{}}, SincronizadoEn: f.SincronizadoEn,
		WhatsApp: EstadoEnWhatsApp{Estado: "aplicado"}}
	if guardado {
		r.Version = f.Version
		r.PlantillaPermiso, r.PlantillaPermisoIdioma = f.PlantillaPermiso, f.PlantillaPermisoIdioma
		r.WhatsApp = EstadoEnWhatsApp{Estado: f.MetaEstado, Intentos: f.MetaIntentos, Detalle: f.MetaDetalle}
		if f.MetaEstado == "pendiente" {
			r.WhatsApp.ProximoIntentoEn = f.MetaProximoEn
		}
		if len(f.HorasMeta) > 0 {
			var h HorasMeta
			if json.Unmarshal(f.HorasMeta, &h) == nil && h.Zona != "" {
				r.HorasWhatsApp = &h
			}
		}
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

func nuevoVerLlamadasCanal(m *Modulo) *auditoria.Caso[EntradaIDConv, LlamadasCanal] {
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
			if err != nil {
				return LlamadasCanal{}, err
			}
			return conRechazadas(ctx, q, aLlamadasCanal(c, f, guardado), m.ahora())
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
			if in.PlantillaPermiso != nil {
				f.PlantillaPermiso = nil
				if *in.PlantillaPermiso != "" {
					f.PlantillaPermiso = in.PlantillaPermiso
				}
				if in.PlantillaPermisoIdioma != "" {
					f.PlantillaPermisoIdioma = in.PlantillaPermisoIdioma
				}
				if err := q.FijarPlantillaPermiso(ctx, mensajeriabd.FijarPlantillaPermisoParams{CanalID: c.ID, Plantilla: f.PlantillaPermiso, Idioma: f.PlantillaPermisoIdioma}); err != nil {
					return LlamadasCanal{}, err
				}
			}
			vista := func(x mensajeriabd.MensajeriaVozAjustesCanal) map[string]any {
				return map[string]any{"llamadas_activas": x.LlamadasActivas, "cola_id": x.ColaID, "horario_id": x.HorarioID, "segundos_sonando": x.SegundosSonando,
					"fuera_de_horario": x.FueraDeHorario, "icono_visible": x.IconoVisible, "devolucion": x.Devolucion, "mensaje_perdida": x.MensajePerdida,
					"mensaje_perdida_texto": x.MensajePerdidaTexto, "plantilla_permiso": x.PlantillaPermiso}
			}
			campos := t.RegistrarCambios("llamadas_canal", c.ID, vista(antes), vista(f))
			if campos == nil {
				campos = []string{}
			}
			// Lo que también vive en WhatsApp (encendido, icono, devolución y horario de llamadas) queda
			// como pendiente con reintentos: se intenta ya, después del COMMIT, y si falla lo repite el
			// mantenimiento. El panel ve el estado en `whatsapp`.
			if c.TipoConexion == ConexionAPI && (!guardado || antes.LlamadasActivas != f.LlamadasActivas || antes.IconoVisible != f.IconoVisible ||
				antes.Devolucion != f.Devolucion || !mismoPtr(antes.HorarioID, f.HorarioID) || antes.MetaEstado != "aplicado") {
				ahora := m.ahora()
				if f, err = q.FijarMetaAjustesCanal(ctx, mensajeriabd.FijarMetaAjustesCanalParams{CanalID: c.ID, MetaEstado: "pendiente", MetaProximoEn: &ahora,
					HorasMeta: f.HorasMeta}); err != nil {
					return LlamadasCanal{}, err
				}
				bd := nucleo.BDDe(ctx)
				t.AlConfirmar(func(ctx context.Context) { _ = m.AplicarLlamadasEnWhatsApp(ctx, bd, c.ID) })
			}
			return aLlamadasCanal(c, f, true), t.Emitir("canal_mensajeria.llamadas_cambiadas", map[string]any{"canal_id": c.ID, "empresa_id": c.EmpresaID,
				"campos": campos, "llamadas_activas": f.LlamadasActivas})
		},
	})
}

func mismoPtr(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// resultadoAjustesMeta es lo que respondió WhatsApp al escribir los ajustes del número.
type resultadoAjustesMeta struct {
	CanalID int64 `json:"canal_id"`
	Fallo   bool  `json:"fallo"`
	horas   *HorasMeta
}

// AplicarLlamadasEnWhatsApp escribe en WhatsApp los ajustes pendientes del número (dos fases: leer,
// llamar a Meta sin transacción, guardar el resultado). Si falla, se reintenta con espera creciente
// (1 min, 5 min, 30 min, 2 h, 12 h) y al agotarse queda en `error` hasta que alguien lo reintente.
func (m *Modulo) AplicarLlamadasEnWhatsApp(ctx context.Context, bd nucleo.BD, canalID int64) error {
	ctx = ctxSistema(context.WithoutCancel(ctx), bd)
	q := mensajeriabd.New(bd)
	f, err := q.VozAjustesCanal(ctx, canalID)
	if err != nil || f.MetaEstado != "pendiente" {
		return err
	}
	cg, err := m.canalGraphDe(ctx, q, canalID)
	if err != nil {
		return err
	}
	meta := AjustesMetaLlamadas{Habilitadas: f.LlamadasActivas, IconoVisible: f.IconoVisible, Devolucion: f.Devolucion}
	// El horario de llamadas de WhatsApp sale del de Iris (el del ajuste; si no, el del número),
	// cuando cabe en los límites de Meta.
	if cal, err := calendarioEfectivo(ctx, q, f.HorarioID, canalID); err == nil {
		meta.Horas = horasMetaDe(cal)
	}
	errMeta := m.graph().FijarAjustes(ctx, cg, meta)
	_, err = m.Voz.resultadoAjustes.Ejecutar(ctx, resultadoAjustesMeta{CanalID: canalID, Fallo: errMeta != nil, horas: meta.Horas})
	return err
}

func nuevoResultadoAjustesMeta(m *Modulo) *auditoria.Caso[resultadoAjustesMeta, bool] {
	return auditoria.Nuevo(auditoria.Caso[resultadoAjustesMeta, bool]{
		Accion: "llamadas.aplicar_en_whatsapp", Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoAjustesMeta) (bool, error) {
			if err := soloSistemaVoz(ctx); err != nil {
				return false, err
			}
			q := mensajeriabd.New(t)
			f, err := q.VozAjustesCanalParaActualizar(ctx, in.CanalID)
			if err != nil || f.MetaEstado != "pendiente" {
				return false, err
			}
			t.FijarEmpresa(derefEmpresa(f.EmpresaID))
			t.FijarEntidad("canal_mensajeria", in.CanalID)
			ahora := m.ahora()
			p := mensajeriabd.FijarMetaAjustesCanalParams{CanalID: in.CanalID, MetaEstado: "aplicado", HorasMeta: f.HorasMeta}
			if !in.Fallo {
				p.SincronizadoEn = &ahora
				p.HorasMeta = nil
				if in.horas != nil {
					if p.HorasMeta, err = json.Marshal(in.horas); err != nil {
						return false, err
					}
				}
			} else {
				p.MetaIntentos = f.MetaIntentos + 1
				if int(f.MetaIntentos) < len(EsperasMeta) {
					p.MetaEstado = "pendiente"
					p.MetaProximoEn = ptrT(ahora.Add(EsperasMeta[f.MetaIntentos]))
					p.MetaDetalle = nombrePtr("WhatsApp no respondió o no aceptó los ajustes. Se vuelve a intentar solo")
				} else {
					p.MetaEstado = "error"
					p.MetaDetalle = nombrePtr("WhatsApp no aceptó los ajustes del número después de varios intentos. Revisa el número en WhatsApp Manager y usa «Reintentar»")
				}
			}
			t.Anotar(map[string]any{"estado": p.MetaEstado, "intentos": p.MetaIntentos})
			_, err = q.FijarMetaAjustesCanal(ctx, p)
			return true, err
		},
	})
}

// nuevoReintentarAjustesMeta: «Reintentar» del panel (también desde `error`).
func nuevoReintentarAjustesMeta(m *Modulo) *auditoria.Caso[EntradaIDConv, LlamadasCanal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIDConv, LlamadasCanal]{
		Accion: "llamadas.reintentar_en_whatsapp", Permiso: nucleo.PermLlamadasConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIDConv) (LlamadasCanal, error) {
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
			f, guardado, err := leerLlamadasCanal(ctx, q, c, true)
			if err != nil {
				return LlamadasCanal{}, err
			}
			if !guardado || c.TipoConexion != ConexionAPI {
				return LlamadasCanal{}, nucleo.Conflicto("sin_ajustes", "Este número todavía no tiene ajustes de llamadas que aplicar")
			}
			ahora := m.ahora()
			if f, err = q.FijarMetaAjustesCanal(ctx, mensajeriabd.FijarMetaAjustesCanalParams{CanalID: c.ID, MetaEstado: "pendiente", MetaProximoEn: &ahora,
				HorasMeta: f.HorasMeta}); err != nil {
				return LlamadasCanal{}, err
			}
			return aLlamadasCanal(c, f, true), nil
		},
	})
}

// ReintentarEnWhatsApp vuelve a intentar ya la escritura de los ajustes del número.
func (m *Modulo) ReintentarEnWhatsApp(ctx context.Context, id int64) (LlamadasCanal, error) {
	if _, err := m.Voz.reintentarAjustes.Ejecutar(ctx, EntradaIDConv{ID: id}); err != nil {
		return LlamadasCanal{}, err
	}
	if err := m.AplicarLlamadasEnWhatsApp(ctx, nucleo.BDDe(ctx), id); err != nil {
		return LlamadasCanal{}, err
	}
	return m.Voz.VerCanal.Ejecutar(ctx, EntradaIDConv{ID: id})
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
			recibio, err := q.CanalRecibioLlamadas(ctx, &c.ID)
			if err != nil {
				return LlamadasCanal{}, err
			}
			leido := api && !in.fallo
			reqs := []RequisitoLlamadas{
				{Codigo: "conexion_api", Cumple: api, Detalle: si(api, "El número está conectado por API", "El número está conectado por QR: las llamadas no están disponibles")},
				{Codigo: "limite_mensajeria", Cumple: leido && in.meta.LimiteMensajeria >= 2000, Detalle: si(leido,
					si(in.meta.LimiteMensajeria >= 2000, "El límite de mensajería del número alcanza", "WhatsApp exige un límite de al menos 2 000 destinatarios por día"), sinMeta)},
				// La suscripción al campo calls no se puede leer con el token del número: solo se da por
				// cumplida si ya llegó alguna llamada a este número; si no, queda «sin comprobar» (aviso).
				{Codigo: "webhook_calls", Cumple: recibio, Aviso: !recibio, SinComprobar: !recibio, Detalle: si(recibio, "La app recibe los avisos de llamadas",
					"Iris no puede comprobarlo: revisa en la app de Meta que el campo «calls» del webhook esté suscrito. Se confirma con la primera llamada que llegue")},
				{Codigo: "calling_habilitado", Cumple: leido && in.meta.Habilitadas, Detalle: si(leido,
					si(in.meta.Habilitadas, "Las llamadas están habilitadas en el número", "Falta habilitar las llamadas en el número, en WhatsApp Manager"), sinMeta)},
				{Codigo: "plantilla_permiso", Cumple: f.PlantillaPermiso != nil, Aviso: true, Detalle: si(f.PlantillaPermiso != nil,
					"El número tiene indicada su plantilla de permiso de llamada",
					"Para pedir permiso de llamada fuera de la ventana de 24 h indica la plantilla call_permission_request aprobada del número")},
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
			return conRechazadas(ctx, q, aLlamadasCanal(c, f, true), ahora)
		},
	})
}

// ConfigurarLlamadas guarda los ajustes de llamadas del número. Si traen una plantilla de permiso,
// antes se comprueba en Meta (fuera de la transacción) que la cuenta la tiene, aprobada, en ese
// idioma y de una categoría que sirve para pedir permiso: una plantilla cualquiera no vale.
func (m *Modulo) ConfigurarLlamadas(ctx context.Context, in EntradaLlamadasCanal) (LlamadasCanal, error) {
	if in.PlantillaPermiso != nil && *in.PlantillaPermiso != "" {
		p, err := m.Voz.prepararCanal.Ejecutar(ctx, EntradaIDConv{ID: in.ID})
		if err != nil {
			return LlamadasCanal{}, err
		}
		if in.PlantillaPermisoIdioma == "" {
			in.PlantillaPermisoIdioma = "es"
		}
		info, err := m.graph().Plantilla(ctx, p.graph, *in.PlantillaPermiso, in.PlantillaPermisoIdioma)
		if err != nil {
			return LlamadasCanal{}, metaRechazo()
		}
		motivo := ""
		switch {
		case !info.Existe:
			motivo = "La cuenta de WhatsApp no tiene una plantilla con ese nombre en ese idioma"
		case !info.Aprobada:
			motivo = "Esa plantilla todavía no está aprobada por WhatsApp"
		case info.Categoria != "UTILITY" && info.Categoria != "MARKETING":
			motivo = "Esa plantilla no es de una categoría que sirva para pedir permiso de llamada"
		}
		if motivo != "" {
			return LlamadasCanal{}, reglaVoz("plantilla_permiso_invalida", motivo,
				nucleo.ErrorCampo{Field: "plantillaPermiso", Message: motivo, Rule: "plantilla_permiso_invalida"})
		}
	}
	return m.Voz.ConfigCanal.Ejecutar(ctx, in)
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
	Version               int32  `json:"version" doc:"0 si nunca se guardaron (valores de fábrica)"`
	Grabar                string `json:"grabar" enum:"nunca" doc:"Siempre nunca: la grabación de llamadas todavía no está disponible"`
	DuracionMaxPersonaMin int32  `json:"duracionMaxPersonaMin" doc:"Una llamada contestada por una persona se corta a estos minutos (10–120)"`
	SegundosAsignado      int32  `json:"segundosAsignado" doc:"Cuánto suena primero solo a quien tiene la conversación (0–20)"`
	// Límites de las llamadas salientes (cuestan dinero).
	MaxSalientesPorDia      int32 `json:"maxSalientesPorDia" doc:"Llamadas salientes de la empresa por día (0–1000; 0 = ninguna)"`
	TopeMinutosSalientesMes int32 `json:"topeMinutosSalientesMes" doc:"Minutos de llamadas salientes por mes (0–100000; 0 = ninguna)"`
	// Consumo, para mostrarlo junto a los topes (solo lectura).
	MinutosSalientesMes    int32 `json:"minutosSalientesMes" doc:"Minutos de llamadas salientes contestadas este mes (cuenta lo que llevan las que están en curso), para comparar con topeMinutosSalientesMes"`
	SalientesHoy           int32 `json:"salientesHoy" doc:"Llamadas salientes hechas hoy, para comparar con maxSalientesPorDia"`
	MaxColgadasMarcandoDia int32 `json:"maxColgadasMarcandoDia" doc:"Cuántas salientes a un mismo contacto se pueden colgar mientras marcan en un día antes de dejar de llamarlo (1–20; 0 = sin límite)"`
}

// EntradaAjustesVoz es POST /voz/ajustes/actualizar.
type EntradaAjustesVoz struct {
	Version               int32  `json:"version" minimum:"0"`
	Grabar                string `json:"grabar,omitempty" doc:"Opcional. Solo se acepta nunca: cualquier otro valor, 422 grabacion_no_disponible"`
	DuracionMaxPersonaMin int32  `json:"duracionMaxPersonaMin" minimum:"10" maximum:"120"`
	SegundosAsignado      int32  `json:"segundosAsignado" minimum:"0" maximum:"20"`
	// Opcionales: ausentes, se conservan los que hay.
	MaxSalientesPorDia      *int32 `json:"maxSalientesPorDia,omitempty" minimum:"0" maximum:"1000"`
	TopeMinutosSalientesMes *int32 `json:"topeMinutosSalientesMes,omitempty" minimum:"0" maximum:"100000"`
	MaxColgadasMarcandoDia  *int32 `json:"maxColgadasMarcandoDia,omitempty" minimum:"0" maximum:"20"`
}

func aAjustesVoz(f mensajeriabd.MensajeriaVozAjuste) AjustesVoz {
	return AjustesVoz{Version: f.Version, Grabar: "nunca", DuracionMaxPersonaMin: f.DuracionMaxPersonaMin, SegundosAsignado: f.SegundosAsignado,
		MaxSalientesPorDia: f.MaxSalientesPorDia, TopeMinutosSalientesMes: f.TopeMinutosSalientesMes, MaxColgadasMarcandoDia: f.MaxColgadasMarcandoDia}
}

func nuevoVerAjustesVoz(m *Modulo) *auditoria.Caso[struct{}, AjustesVoz] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesVoz]{
		Accion: "voz.ver_ajustes", Permiso: nucleo.PermVozConfigurar, AuditarExito: sinAuditar[AjustesVoz],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesVoz, error) {
			a, err := alcanceVoz(ctx, nucleo.PermVozConfigurar)
			if err != nil {
				return AjustesVoz{}, err
			}
			f, err := ajustesVoz(ctx, mensajeriabd.New(t), a.empresa)
			if err != nil {
				return AjustesVoz{}, err
			}
			return m.conConsumo(ctx, mensajeriabd.New(t), a.empresa, aAjustesVoz(f))
		},
	})
}

// conConsumo completa los ajustes con lo usado: minutos de salientes del mes y salientes de hoy
// (los mismos conteos que aplica llamar; hora de Guatemala).
func (m *Modulo) conConsumo(ctx context.Context, q *mensajeriabd.Queries, empresa int64, aj AjustesVoz) (AjustesVoz, error) {
	ahora := m.ahora()
	seg, err := q.SegundosSalientes(ctx, mensajeriabd.SegundosSalientesParams{Empresa: empresa, Desde: inicioMes(ahora), Hasta: ahora.Add(time.Hour), Menos: uuid.Nil, Ahora: ahora})
	if err != nil {
		return aj, err
	}
	hoy, err := q.SalientesDesde(ctx, mensajeriabd.SalientesDesdeParams{Empresa: empresa, Desde: inicioDia(ahora)})
	if err != nil {
		return aj, err
	}
	aj.MinutosSalientesMes, aj.SalientesHoy = int32(min(seg/60, 1<<30)), int32(min(hoy, 1<<30)) //nolint:gosec // acotados
	return aj, nil
}

func nuevoFijarAjustesVoz(m *Modulo) *auditoria.Caso[EntradaAjustesVoz, AjustesVoz] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAjustesVoz, AjustesVoz]{
		Accion: "voz.configurar", Permiso: nucleo.PermVozConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAjustesVoz) (AjustesVoz, error) {
			a, err := alcanceVoz(ctx, nucleo.PermVozConfigurar)
			if err != nil {
				return AjustesVoz{}, err
			}
			t.FijarEmpresa(a.empresa)
			switch {
			case in.Grabar != "" && in.Grabar != "nunca":
				// La grabación de Meta llega con E1c: no se puede encender, ni a medias.
				return AjustesVoz{}, reglaVoz("grabacion_no_disponible", "La grabación de llamadas todavía no está disponible",
					nucleo.ErrorCampo{Field: "grabar", Message: "Aún no disponible", Rule: "grabacion_no_disponible"})
			case in.DuracionMaxPersonaMin < 10 || in.DuracionMaxPersonaMin > 120:
				return AjustesVoz{}, campoConv("duracionMaxPersonaMin", "Entre 10 y 120 minutos", "rango")
			case in.SegundosAsignado < 0 || in.SegundosAsignado > 20:
				return AjustesVoz{}, campoConv("segundosAsignado", "Entre 0 y 20 segundos", "rango")
			case in.MaxSalientesPorDia != nil && (*in.MaxSalientesPorDia < 0 || *in.MaxSalientesPorDia > 1000):
				return AjustesVoz{}, campoConv("maxSalientesPorDia", "Entre 0 y 1000 llamadas", "rango")
			case in.TopeMinutosSalientesMes != nil && (*in.TopeMinutosSalientesMes < 0 || *in.TopeMinutosSalientesMes > 100000):
				return AjustesVoz{}, campoConv("topeMinutosSalientesMes", "Entre 0 y 100 000 minutos", "rango")
			case in.MaxColgadasMarcandoDia != nil && (*in.MaxColgadasMarcandoDia < 0 || *in.MaxColgadasMarcandoDia > 20):
				return AjustesVoz{}, campoConv("maxColgadasMarcandoDia", "Entre 0 y 20", "rango")
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
			}
			salientes, tope := antes.MaxSalientesPorDia, antes.TopeMinutosSalientesMes
			if in.MaxSalientesPorDia != nil {
				salientes = *in.MaxSalientesPorDia
			}
			if in.TopeMinutosSalientesMes != nil {
				tope = *in.TopeMinutosSalientesMes
			}
			colgadas := antes.MaxColgadasMarcandoDia
			if in.MaxColgadasMarcandoDia != nil {
				colgadas = *in.MaxColgadasMarcandoDia
			}
			if nuevo {
				f, err = q.InsertarVozAjustes(ctx, mensajeriabd.InsertarVozAjustesParams{EmpresaID: a.empresaPtr(), Grabar: "nunca",
					DuracionMaxPersonaMin: in.DuracionMaxPersonaMin, SegundosAsignado: in.SegundosAsignado, RetencionGrabacionesDias: 30,
					MaxSalientesPorDia: salientes, TopeMinutosSalientesMes: tope, MaxColgadasMarcandoDia: colgadas})
				if errors.Is(err, pgx.ErrNoRows) {
					return AjustesVoz{}, nucleo.ConflictoVersion("voz_ajustes", a.empresa)
				}
			} else {
				f, err = q.ActualizarVozAjustes(ctx, mensajeriabd.ActualizarVozAjustesParams{ID: antes.ID, Grabar: "nunca",
					DuracionMaxPersonaMin: in.DuracionMaxPersonaMin, SegundosAsignado: in.SegundosAsignado, RetencionGrabacionesDias: antes.RetencionGrabacionesDias,
					MaxSalientesPorDia: salientes, TopeMinutosSalientesMes: tope, MaxColgadasMarcandoDia: colgadas})
			}
			if err != nil {
				return AjustesVoz{}, err
			}
			t.FijarEntidad("voz_ajustes", f.ID)
			vista := func(x mensajeriabd.MensajeriaVozAjuste) map[string]any {
				return map[string]any{"duracion_max_persona_min": x.DuracionMaxPersonaMin, "segundos_asignado": x.SegundosAsignado,
					"max_salientes_por_dia": x.MaxSalientesPorDia, "tope_minutos_salientes_mes": x.TopeMinutosSalientesMes,
					"max_colgadas_marcando_dia": x.MaxColgadasMarcandoDia}
			}
			campos := t.RegistrarCambios("voz_ajustes", f.ID, vista(antes), vista(f))
			if campos == nil {
				campos = []string{}
			}
			if err := t.Emitir("voz.ajustes_cambiados", map[string]any{"empresa_id": f.EmpresaID, "campos": campos}); err != nil {
				return AjustesVoz{}, err
			}
			return m.conConsumo(ctx, q, a.empresa, aAjustesVoz(f))
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
				return nada, nil // sus reject y terminate pendientes los repite el job de escrituras a Meta
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
			case LlamadaMarcando:
				// El cliente no contestó a tiempo: se cancela en Meta y cuenta como sin contestar.
				if !ahora.Before(l.SuenaHasta) {
					return cerrar(LlamadaPerdida, "nadie_contesto", MetaTerminar)
				}
				// La pestaña que llamó se cerró, se recargó o murió: nadie oiría al cliente si contesta. Se
				// cancela ya. Cuenta para «timbrar y colgar» y, si llegó a sonar en Meta, como sin contestar.
				if l.Direccion == "saliente" && ahora.Sub(l.SonoEn) > GraciaSinFlujo &&
					(l.PestanaVistaEn == nil || !l.PestanaVistaEn.After(ahora.Add(-FlujoVivo-GraciaSinFlujo))) {
					return cerrar(LlamadaTerminada, "error_medio", MetaTerminar)
				}
			case LlamadaEnCurso:
				// Una saliente se corta al llegar la empresa a su tope de minutos del mes (cuenta lo que
				// lleva esta y las demás en curso): con 299 de 300 minutos dura, como mucho, lo que queda.
				if l.Direccion == "saliente" && l.ContestadaEn != nil {
					usados, err := q.SegundosSalientes(ctx, mensajeriabd.SegundosSalientesParams{Empresa: derefEmpresa(l.EmpresaID), Desde: inicioMes(ahora),
						Hasta: ahora.Add(time.Hour), Menos: uuid.Nil, Ahora: ahora})
					if err != nil {
						return nada, err
					}
					if usados >= int64(voz.TopeMinutosSalientesMes)*60 {
						return cerrar(LlamadaTerminada, "tope_alcanzado", MetaTerminar)
					}
				}
				if l.ContestadaEn != nil && ahora.Sub(*l.ContestadaEn) >= time.Duration(voz.DuracionMaxPersonaMin)*time.Minute {
					return cerrar(LlamadaTerminada, "duracion_maxima", MetaTerminar)
				}
				// El panel de quien la atiende se cerró o se cayó: Meta ya no recibe audio.
				if l.UsuarioID != nil && l.ContestadaEn != nil && ahora.Sub(*l.ContestadaEn) > FlujoVivo {
					var vivo bool
					var err error
					if l.Direccion == "saliente" {
						// El latido de una saliente es el del flujo de eventos de la pestaña que llamó.
						vivo = l.PestanaVistaEn != nil && l.PestanaVistaEn.After(ahora.Add(-FlujoVivo-GraciaSinFlujo))
					} else {
						vivo, err = q.ReceptorVivo(ctx, mensajeriabd.ReceptorVivoParams{Empresa: derefEmpresa(l.EmpresaID), UsuarioID: *l.UsuarioID,
							VivoDesde: ahora.Add(-FlujoVivo - GraciaSinFlujo)})
					}
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

// MantenerLlamadas recorre las llamadas vivas y las intenciones con Meta que quedaron a medias. No
// llama a Meta: lo que haya que pedirle queda anotado para DespacharMetaVoz (otro job).
func (m *Modulo) MantenerLlamadas(ctx context.Context, bd nucleo.BD) error {
	ctx = sinMetaEnLinea(ctxSistema(ctx, bd))
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

// DespacharMetaVoz hace las escrituras a Meta que quedaron pendientes: los reject y terminate de
// llamadas ya cerradas (idempotentes en Meta; se repiten hasta 2 minutos) y los ajustes de números
// por aplicar. Corre en su propio job, fuera del ciclo del mantenimiento: una Graph API lenta no
// retrasa el timbre. Toma las filas con SKIP LOCKED: dos réplicas no repiten la misma.
func (m *Modulo) DespacharMetaVoz(ctx context.Context, bd nucleo.BD) error {
	ctx = ctxSistema(ctx, bd)
	q := mensajeriabd.New(bd)
	ahora := m.ahora()
	// Se toman pocas y quedan apartadas lote × plazo (lo que tarda este ciclo, en el peor caso, en
	// atenderlas todas): otro ciclo no retoma las mismas mientras tanto.
	// Mientras quede presupuesto se toma otro lote: con muchas pendientes, un terminate (p. ej. por
	// duración máxima) no espera varios ciclos. Lo ya tomado queda apartado: no se repite en el ciclo.
	// El apartado de cada lote cuenta desde que se toma ese lote (no desde que empezó el ciclo): un
	// segundo lote tomado cerca del presupuesto no se sale de su apartado.
	gracia := 2 * PlazoMeta
	inicio := time.Now()
	for {
		tomado := ahora.Add(time.Since(inicio))
		acciones, err := q.TomarAccionesMeta(ctx, mensajeriabd.TomarAccionesMetaParams{Antes: ptrT(ahora.Add(-gracia)), Lote: LoteMetaVoz,
			Aparte: tomado.Add(LoteMetaVoz*PlazoMeta - gracia)})
		if err != nil {
			return err
		}
		for _, a := range acciones {
			if a.TerminadaEn != nil && ahora.Sub(*a.TerminadaEn) > 2*time.Minute {
				// No se da por cerrada en silencio: pasa a las huérfanas y queda auditado.
				if _, err := m.Voz.agotarAccion.Ejecutar(ctx, a.ID); err != nil {
					return err
				}
				slog.WarnContext(ctx, "voz: Meta no aceptó cerrar una llamada en 2 minutos; pasa a las huérfanas", "llamada_id", a.ID)
				continue
			}
			if err := m.cumplirMeta(ctx, bd, a.ID, ""); err != nil {
				slog.WarnContext(ctx, "voz: acción pendiente con Meta", "llamada_id", a.ID, "err", err)
			}
		}
		if len(acciones) < LoteMetaVoz || time.Since(inicio) >= PresupuestoMetaVoz {
			break
		}
	}
	// Llamadas que existen en Meta y no en Iris: se terminan.
	if err := m.despacharHuerfanas(ctx, bd); err != nil {
		return err
	}
	// Solicitudes de permiso que quedaron sin confirmar: se le pregunta a Meta.
	if err := m.resolverSolicitudes(ctx, bd); err != nil {
		return err
	}
	// Aplicar los ajustes de un número son dos llamadas a Meta (leer y escribir).
	canales, err := q.TomarCanalesMetaPendiente(ctx, mensajeriabd.TomarCanalesMetaPendienteParams{Ahora: &ahora, Lote: LoteMetaVoz,
		Aparte: ahora.Add(time.Since(inicio) + LoteMetaVoz*2*PlazoMeta)})
	if err != nil {
		return err
	}
	for _, id := range canales {
		if err := m.AplicarLlamadasEnWhatsApp(ctx, bd, id); err != nil {
			slog.WarnContext(ctx, "voz: aplicar en WhatsApp los ajustes de un número", "canal_id", id, "err", err)
		}
	}
	return nil
}

// ArgsMetaVoz: las escrituras pendientes a Meta, en su propio job.
type ArgsMetaVoz struct{}

// Kind implementa river.JobArgs.
func (ArgsMetaVoz) Kind() string { return "mensajeria.voz_meta" }

// InsertOpts: uno cada 5 s aunque haya varias réplicas.
func (ArgsMetaVoz) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Second}}
}

type trabajadorMetaVoz struct {
	river.WorkerDefaults[ArgsMetaVoz]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorMetaVoz) Work(ctx context.Context, _ *river.Job[ArgsMetaVoz]) error {
	return w.m.DespacharMetaVoz(ctx, w.bd)
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
