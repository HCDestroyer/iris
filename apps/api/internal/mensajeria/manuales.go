package mensajeria

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Biblioteca de manuales (contratos/conversaciones.md §11): artículos por empresa con búsqueda de
// texto completo en español, sugerencias por reglas simples para una conversación (cada una con su
// motivo) y «enviar al cliente» (enlace público o texto) solo de los artículos para el cliente. Es la
// base de conocimiento del asistente de la fase 2: el diseño no depende de la bandeja (la conversación
// solo aporta las señales para sugerir).

// Manual es un artículo de la biblioteca.
type Manual struct {
	ID          int64     `json:"id"`
	Titulo      string    `json:"titulo"`
	Resumen     string    `json:"resumen"`
	Cuerpo      string    `json:"cuerpo" doc:"Markdown seguro: se guarda tal cual y el panel lo muestra sin HTML crudo"`
	Etiquetas   []string  `json:"etiquetas"`
	Equipo      *string   `json:"equipo" nullable:"true" doc:"Modelo de equipo (p. ej. ZTE F670L)"`
	Visibilidad string    `json:"visibilidad" enum:"interna,cliente"`
	Activo      bool      `json:"activo"`
	Version     int32     `json:"version"`
	Actualizado time.Time `json:"actualizadoEn"`
}

// ManualResumen es un artículo en una lista (sin el cuerpo).
type ManualResumen struct {
	ID          int64    `json:"id"`
	Titulo      string   `json:"titulo"`
	Resumen     string   `json:"resumen"`
	Etiquetas   []string `json:"etiquetas"`
	Equipo      *string  `json:"equipo" nullable:"true"`
	Visibilidad string   `json:"visibilidad" enum:"interna,cliente"`
	Activo      bool     `json:"activo"`
	Rango       float32  `json:"rango" doc:"Relevancia de la búsqueda (0 sin q)"`
}

// ListaManuales es { data, nextCursor }.
type ListaManuales struct {
	Data       []ManualResumen `json:"data"`
	NextCursor *string         `json:"nextCursor" nullable:"true"`
}

// MotivoSugerencia dice por qué se sugiere un artículo.
type MotivoSugerencia struct {
	Tipo    string `json:"tipo" enum:"etiqueta,equipo,ticket_abierto"`
	Detalle string `json:"detalle" doc:"La etiqueta, el modelo de equipo o el número del ticket"`
}

// ManualSugerido es un artículo sugerido para una conversación.
type ManualSugerido struct {
	ManualResumen
	Motivos []MotivoSugerencia `json:"motivos"`
}

// ListaSugeridos es { data, nextCursor }.
type ListaSugeridos struct {
	Data       []ManualSugerido `json:"data"`
	NextCursor *string          `json:"nextCursor" nullable:"true"`
}

func aManual(f mensajeriabd.MensajeriaManuale) Manual {
	return Manual{ID: f.ID, Titulo: f.Titulo, Resumen: f.Resumen, Cuerpo: f.Cuerpo, Etiquetas: lista(f.Etiquetas), Equipo: f.Equipo,
		Visibilidad: f.Visibilidad, Activo: f.Activo, Version: f.Version, Actualizado: f.UpdatedAt}
}

func resumenManual(f mensajeriabd.MensajeriaManuale) ManualResumen {
	return ManualResumen{ID: f.ID, Titulo: f.Titulo, Resumen: f.Resumen, Etiquetas: lista(f.Etiquetas), Equipo: f.Equipo, Visibilidad: f.Visibilidad, Activo: f.Activo}
}

// normalizarEtiquetas: minúsculas, sin espacios repetidos, sin repetir.
func normalizarEtiquetas(es []string) []string {
	out := []string{}
	for _, e := range es {
		e = strings.Join(strings.Fields(strings.ToLower(e)), " ")
		if e != "" && !slices.Contains(out, e) {
			out = append(out, recortar(e, 40))
		}
	}
	return out
}

// Casos ---------------------------------------------------------------------------------------------

// EntradaManual crea o actualiza un artículo.
type EntradaManual struct {
	ID          int64    `json:"-"`
	Version     int32    `json:"version,omitempty" minimum:"0" doc:"Obligatoria al actualizar"`
	Titulo      string   `json:"titulo" minLength:"1" maxLength:"160"`
	Resumen     string   `json:"resumen,omitempty" maxLength:"400"`
	Cuerpo      string   `json:"cuerpo" minLength:"1" maxLength:"50000"`
	Etiquetas   []string `json:"etiquetas,omitempty" maxItems:"20"`
	Equipo      *string  `json:"equipo,omitempty" maxLength:"80" nullable:"true"`
	Visibilidad string   `json:"visibilidad" enum:"interna,cliente"`
}

// MarshalJSON: sin el cuerpo (puede ser largo); su largo.
func (e EntradaManual) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "titulo": e.Titulo, "etiquetas": e.Etiquetas, "equipo": e.Equipo,
		"visibilidad": e.Visibilidad, "largo_cuerpo": len([]rune(e.Cuerpo))})
}

// EntradaBuscarManuales es GET /manuales.
type EntradaBuscarManuales struct {
	Q                string `query:"q" maxLength:"200" doc:"Texto libre (frases entre comillas, -excluir, or)"`
	Etiqueta         string `query:"etiqueta" maxLength:"40"`
	Visibilidad      string `query:"visibilidad" enum:"interna,cliente"`
	IncluirInactivos bool   `query:"incluirInactivos"`
	Limit            int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

// EntradaManualID es un artículo por id.
type EntradaManualID struct {
	ID int64 `path:"id" minimum:"1"`
}

func manualAuditable(m Manual) map[string]any {
	return map[string]any{"titulo": m.Titulo, "resumen": m.Resumen, "etiquetas": m.Etiquetas, "equipo": m.Equipo, "visibilidad": m.Visibilidad,
		"largo_cuerpo": len([]rune(m.Cuerpo)), "activo": m.Activo}
}

func nuevoBuscarManuales(*Modulo) *auditoria.Caso[EntradaBuscarManuales, ListaManuales] {
	return auditoria.Nuevo(auditoria.Caso[EntradaBuscarManuales, ListaManuales]{
		Accion: "manuales.buscar", Permiso: nucleo.PermManualesVer, AuditarExito: sinAuditar[ListaManuales],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaBuscarManuales) (ListaManuales, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaManuales{}, err
			}
			limite := in.Limit
			if limite <= 0 || limite > 100 {
				limite = 20
			}
			fs, err := mensajeriabd.New(t).BuscarManuales(ctx, mensajeriabd.BuscarManualesParams{Empresa: a.empresa, IncluirInactivos: in.IncluirInactivos,
				Q: strings.TrimSpace(in.Q), Etiqueta: strings.ToLower(strings.TrimSpace(in.Etiqueta)), Visibilidad: in.Visibilidad, Limite: int32(limite)}) //nolint:gosec // ≤ 100
			out := ListaManuales{Data: make([]ManualResumen, 0, len(fs))}
			for _, f := range fs {
				r := ManualResumen{ID: f.ID, Titulo: f.Titulo, Resumen: f.Resumen, Etiquetas: lista(f.Etiquetas), Equipo: f.Equipo, Visibilidad: f.Visibilidad,
					Activo: f.Activo, Rango: f.Rango}
				out.Data = append(out.Data, r)
			}
			return out, err
		},
	})
}

func nuevoVerManual(*Modulo) *auditoria.Caso[EntradaManualID, Manual] {
	return auditoria.Nuevo(auditoria.Caso[EntradaManualID, Manual]{
		Accion: "manuales.ver", Permiso: nucleo.PermManualesVer, AuditarExito: sinAuditar[Manual],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaManualID) (Manual, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return Manual{}, err
			}
			f, err := mensajeriabd.New(t).ManualPorID(ctx, mensajeriabd.ManualPorIDParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return Manual{}, nucleo.NoEncontrado("El manual no existe")
			}
			return aManual(f), err
		},
	})
}

func nuevoGuardarManual(_ *Modulo, crear bool) *auditoria.Caso[EntradaManual, Manual] {
	accion := "manuales.actualizar"
	if crear {
		accion = "manuales.crear"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaManual, Manual]{
		Accion: accion, Permiso: nucleo.PermManualesEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaManual) (Manual, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return Manual{}, err
			}
			titulo, cuerpo := strings.TrimSpace(in.Titulo), strings.TrimSpace(in.Cuerpo)
			if titulo == "" || cuerpo == "" {
				return Manual{}, campoConv("titulo", "Título y cuerpo son obligatorios", "required")
			}
			if in.Equipo != nil {
				e := strings.TrimSpace(*in.Equipo)
				if e == "" {
					in.Equipo = nil
				} else {
					in.Equipo = &e
				}
			}
			etiquetas := normalizarEtiquetas(in.Etiquetas)
			q := mensajeriabd.New(t)
			t.FijarEmpresa(a.empresa)
			if crear {
				f, err := q.InsertarManual(ctx, mensajeriabd.InsertarManualParams{EmpresaID: a.empresaPtr(), Titulo: titulo, Resumen: strings.TrimSpace(in.Resumen),
					Cuerpo: cuerpo, Etiquetas: etiquetas, Equipo: in.Equipo, Visibilidad: in.Visibilidad, CreadoPor: &a.usuario})
				if err != nil {
					return Manual{}, err
				}
				r := aManual(f)
				t.RegistrarCambios("manual", f.ID, nil, manualAuditable(r))
				return r, t.Emitir("manual.creado", map[string]any{"manual_id": f.ID, "empresa_id": f.EmpresaID, "visibilidad": f.Visibilidad})
			}
			antes, err := q.ManualParaActualizar(ctx, mensajeriabd.ManualParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return Manual{}, nucleo.NoEncontrado("El manual no existe")
			}
			if err != nil {
				return Manual{}, err
			}
			if antes.Version != in.Version {
				return Manual{}, nucleo.ConflictoVersion("manual", in.ID)
			}
			f, err := q.ActualizarManual(ctx, mensajeriabd.ActualizarManualParams{ID: in.ID, Titulo: titulo, Resumen: strings.TrimSpace(in.Resumen),
				Cuerpo: cuerpo, Etiquetas: etiquetas, Equipo: in.Equipo, Visibilidad: in.Visibilidad})
			if err != nil {
				return Manual{}, err
			}
			r := aManual(f)
			campos := t.RegistrarCambios("manual", f.ID, manualAuditable(aManual(antes)), manualAuditable(r))
			if campos == nil {
				campos = []string{}
			}
			return r, t.Emitir("manual.actualizado", map[string]any{"manual_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activo": f.Activo})
		},
	})
}

func nuevoCambioManual(_ *Modulo, accion string, activo bool) *auditoria.Caso[EntradaManualID, Manual] {
	return auditoria.Nuevo(auditoria.Caso[EntradaManualID, Manual]{
		Accion: accion, Permiso: nucleo.PermManualesEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaManualID) (Manual, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return Manual{}, err
			}
			q := mensajeriabd.New(t)
			antes, err := q.ManualParaActualizar(ctx, mensajeriabd.ManualParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return Manual{}, nucleo.NoEncontrado("El manual no existe")
			}
			if err != nil || antes.Activo == activo {
				return aManual(antes), err
			}
			t.FijarEmpresa(a.empresa)
			f, err := q.FijarManualActivo(ctx, mensajeriabd.FijarManualActivoParams{ID: in.ID, Activo: activo})
			if err != nil {
				return Manual{}, err
			}
			t.RegistrarCambios("manual", f.ID, map[string]any{"activo": antes.Activo}, map[string]any{"activo": f.Activo})
			return aManual(f), t.Emitir("manual.actualizado", map[string]any{"manual_id": f.ID, "empresa_id": f.EmpresaID, "campos": []string{"activo"}, "activo": f.Activo})
		},
	})
}

// Sugerencias -----------------------------------------------------------------------------------

// SugerirManuales es GET /conversaciones/{id}/manuales-sugeridos: las señales de la conversación en
// una transacción, los equipos del servicio en Argos fuera de ella y las reglas en otra.
type SugerirManuales struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaConversacion, convLeida]
	final  *auditoria.Caso[entradaSugerir, ListaSugeridos]
}

type entradaSugerir struct {
	ID      int64    `json:"conversacion_id"`
	equipos []string // modelos en minúsculas
}

var palabraTicket = regexp.MustCompile(`[\p{L}\p{N}]{4,}`)

// consultaDeAsunto arma un tsquery «a | b | c» con las palabras del asunto (sin operadores).
func consultaDeAsunto(asunto string) string {
	var ps []string
	for _, p := range palabraTicket.FindAllString(strings.ToLower(asunto), 8) {
		if strings.IndexFunc(p, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) == -1 && !slices.Contains(ps, p) {
			ps = append(ps, p)
		}
	}
	return strings.Join(ps, " | ")
}

func nuevoSugerirManuales(m *Modulo) *SugerirManuales {
	k := &SugerirManuales{m: m, previo: nuevoLeerConvPara("manuales.sugerir", nucleo.PermManualesVer)}
	k.final = auditoria.Nuevo(auditoria.Caso[entradaSugerir, ListaSugeridos]{
		Accion: "manuales.sugerir", Permiso: nucleo.PermManualesVer, AuditarExito: sinAuditar[ListaSugeridos],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaSugerir) (ListaSugeridos, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaSugeridos{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return ListaSugeridos{}, err
			}
			porID := map[int64]*ManualSugerido{}
			var orden []int64
			sumar := func(fs []mensajeriabd.MensajeriaManuale, motivo MotivoSugerencia) {
				for _, f := range fs {
					s, ok := porID[f.ID]
					if !ok {
						s = &ManualSugerido{ManualResumen: resumenManual(f), Motivos: []MotivoSugerencia{}}
						porID[f.ID] = s
						orden = append(orden, f.ID)
					}
					if !slices.Contains(s.Motivos, motivo) {
						s.Motivos = append(s.Motivos, motivo)
					}
				}
			}
			// 1. Etiquetas de la conversación (por nombre).
			es, err := q.EtiquetasDeConversaciones(ctx, []int64{c.ID})
			if err != nil {
				return ListaSugeridos{}, err
			}
			for _, e := range es {
				nombre := normalizarEtiquetas([]string{e.Nombre})
				fs, err := q.ManualesPorEtiquetas(ctx, mensajeriabd.ManualesPorEtiquetasParams{Empresa: a.empresa, Etiquetas: nombre})
				if err != nil {
					return ListaSugeridos{}, err
				}
				sumar(fs, MotivoSugerencia{Tipo: "etiqueta", Detalle: e.Nombre})
			}
			// 2. Equipo del servicio (Argos).
			for _, eq := range in.equipos {
				fs, err := q.ManualesPorEquipos(ctx, mensajeriabd.ManualesPorEquiposParams{Empresa: a.empresa, Equipos: []string{eq}})
				if err != nil {
					return ListaSugeridos{}, err
				}
				sumar(fs, MotivoSugerencia{Tipo: "equipo", Detalle: eq})
			}
			// 3. Tickets abiertos del cliente.
			if c.ClienteID != nil {
				ts, err := q.TicketsAbiertosDelCliente(ctx, mensajeriabd.TicketsAbiertosDelClienteParams{ClienteID: c.ClienteID, Empresa: c.EmpresaID})
				if err != nil {
					return ListaSugeridos{}, err
				}
				for _, tk := range ts {
					consulta := consultaDeAsunto(tk.Asunto)
					if consulta == "" {
						continue
					}
					fs, err := q.ManualesPorConsulta(ctx, mensajeriabd.ManualesPorConsultaParams{Empresa: a.empresa, Consulta: consulta})
					if err != nil {
						return ListaSugeridos{}, err
					}
					sumar(fs, MotivoSugerencia{Tipo: "ticket_abierto", Detalle: tk.Numero})
				}
			}
			out := ListaSugeridos{Data: []ManualSugerido{}}
			for _, id := range orden {
				if len(out.Data) == 5 {
					break
				}
				out.Data = append(out.Data, *porID[id])
			}
			return out, nil
		},
	})
	return k
}

// Ejecutar sugiere los manuales de la conversación.
func (k *SugerirManuales) Ejecutar(ctx context.Context, in EntradaConversacion) (ListaSugeridos, error) {
	l, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return ListaSugeridos{}, err
	}
	e := entradaSugerir{ID: l.ID}
	if k.m.cfg.Argos != nil && l.conv.ServicioID != nil {
		// Sin Argos (caído, sin conector) la regla del equipo no aplica: las otras siguen.
		if eqs, err := k.m.cfg.Argos.EquiposEnComodato(ctx, l.alcance.empresa, *l.conv.ServicioID); err == nil {
			for _, x := range eqs {
				if mo := strings.ToLower(strings.TrimSpace(x.Modelo)); mo != "" && !slices.Contains(e.equipos, mo) {
					e.equipos = append(e.equipos, mo)
				}
			}
		}
	}
	return k.final.Ejecutar(ctx, e)
}

// Enviar al cliente ----------------------------------------------------------------------------------

// EntradaEnviarManual es POST /conversaciones/{id}/enviar-manual.
type EntradaEnviarManual struct {
	ConversacionID int64     `json:"-"`
	ID             uuid.UUID `json:"id" doc:"uuid v7 del mensaje (idempotente por él)"`
	ManualID       int64     `json:"manualId" minimum:"1"`
	Modo           string    `json:"modo" enum:"enlace,texto" doc:"enlace: URL pública firmada; texto: el cuerpo en el mensaje"`
	IdempotencyKey string    `json:"-"`
}

func nuevoEnviarManual(m *Modulo) *auditoria.Caso[EntradaEnviarManual, MensajeConvEnviado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEnviarManual, MensajeConvEnviado]{
		Accion: "manuales.enviar", Permiso: nucleo.PermConversacionesResponder,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEnviarManual) (MensajeConvEnviado, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ConversacionID, false)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			if lectura, err := soloLecturaConv(ctx, q, c); err != nil || lectura {
				if err == nil {
					err = conversacionSoloLectura()
				}
				return MensajeConvEnviado{}, err
			}
			// Un reenvío del mismo id no crea otro enlace.
			if f, err := q.MensajeConversacionPorID(ctx, in.ID); err == nil && f.ConversacionID == c.ID {
				ms, err := mensajesConv(ctx, q, []mensajeriabd.MensajeriaConversacionMensaje{f})
				if err != nil {
					return MensajeConvEnviado{}, err
				}
				return MensajeConvEnviado{MensajeConversacion: ms[0], Repetido: true}, nil
			}
			man, err := q.ManualPorID(ctx, mensajeriabd.ManualPorIDParams{ID: in.ManualID, Empresa: empresaDeConv(c)})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !man.Activo) {
				return MensajeConvEnviado{}, nucleo.NoEncontrado("El manual no existe")
			}
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			if man.Visibilidad != "cliente" {
				return MensajeConvEnviado{}, reglaConv("manual_interno", "Ese manual es interno: no se envía al cliente")
			}
			var texto string
			if in.Modo == "enlace" {
				crudo := make([]byte, 32)
				if _, err := rand.Read(crudo); err != nil {
					return MensajeConvEnviado{}, err
				}
				token := base64.RawURLEncoding.EncodeToString(crudo)
				h := sha256.Sum256([]byte(token))
				if _, err := q.InsertarEnlaceManual(ctx, mensajeriabd.InsertarEnlaceManualParams{HashToken: h[:], ManualID: man.ID, EmpresaID: man.EmpresaID,
					ConversacionID: &c.ID, EnviadoPor: &a.usuario, VenceEn: m.ahora().Add(m.cfg.EnlaceManual)}); err != nil {
					return MensajeConvEnviado{}, err
				}
				texto = man.Titulo + "\n" + strings.TrimRight(m.cfg.BaseURL, "/") + Prefijo + "/publico/manuales/" + token
			} else {
				texto = recortar(man.Titulo+"\n\n"+man.Cuerpo, MaxTextoWhatsApp)
			}
			r, err := m.Conv.Responder.Ejecutar(ctx, EntradaResponder{ConversacionID: c.ID, ID: in.ID, Texto: &texto, IdempotencyKey: in.IdempotencyKey})
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			cb, err := q.ConversacionClienteParaActualizar(ctx, c.ID)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			id := strconv.FormatInt(man.ID, 10)
			if err := m.sistema(ctx, t, &cb, EventoSistema{Tipo: "manual_enviado", ActorNombre: actorNombre(ctx, q, a.usuario), Referencia: &man.Titulo,
				EntidadID: &id, Detalle: &in.Modo}); err != nil {
				return MensajeConvEnviado{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("manual", man.ID)
			return r, t.Emitir("manual.enviado", map[string]any{"manual_id": man.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID,
				"mensaje_id": r.ID, "modo": in.Modo})
		},
	})
}

// Enlaces enviados y revocación ------------------------------------------------------------------------

// EnlaceManual es un enlace público de un manual enviado en una conversación (nunca el token).
type EnlaceManual struct {
	ID         int64       `json:"id"`
	ManualID   int64       `json:"manualId"`
	Titulo     string      `json:"titulo"`
	EnviadoPor *UsuarioRef `json:"enviadoPor,omitempty"`
	VenceEn    time.Time   `json:"venceEn"`
	RevocadoEn *time.Time  `json:"revocadoEn" nullable:"true"`
	CreadoEn   time.Time   `json:"creadoEn"`
}

// ListaEnlacesManual es GET /conversaciones/{id}/manual-enlaces.
type ListaEnlacesManual struct {
	Data []EnlaceManual `json:"data"`
}

func nuevoListarEnlacesManual(*Modulo) *auditoria.Caso[EntradaConversacion, ListaEnlacesManual] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, ListaEnlacesManual]{
		Accion: "manuales.listar_enlaces", Permiso: nucleo.PermConversacionesVer, AuditarExito: func(ListaEnlacesManual) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (ListaEnlacesManual, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaEnlacesManual{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return ListaEnlacesManual{}, err
			}
			fs, err := q.EnlacesManualDeConversacion(ctx, &c.ID)
			if err != nil {
				return ListaEnlacesManual{}, err
			}
			out := ListaEnlacesManual{Data: make([]EnlaceManual, 0, len(fs))}
			for _, f := range fs {
				e := EnlaceManual{ID: f.ID, ManualID: f.ManualID, Titulo: f.Titulo, VenceEn: f.VenceEn, RevocadoEn: f.RevocadoEn, CreadoEn: f.CreatedAt}
				if f.EnviadoPor != nil {
					e.EnviadoPor = &UsuarioRef{ID: *f.EnviadoPor}
					if n := actorNombre(ctx, q, *f.EnviadoPor); n != nil {
						e.EnviadoPor.Nombre = *n
					}
				}
				out.Data = append(out.Data, e)
			}
			return out, nil
		},
	})
}

// EntradaRevocarEnlace es POST /manual-enlaces/{id}/revocar.
type EntradaRevocarEnlace struct {
	ID int64 `json:"-"`
}

// EnlaceRevocado es la respuesta de revocar.
type EnlaceRevocado struct {
	ID         int64     `json:"id"`
	ManualID   int64     `json:"manualId"`
	RevocadoEn time.Time `json:"revocadoEn"`
}

func nuevoRevocarEnlaceManual(m *Modulo) *auditoria.Caso[EntradaRevocarEnlace, EnlaceRevocado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRevocarEnlace, EnlaceRevocado]{
		Accion: "manuales.revocar_enlace", Permiso: nucleo.PermManualesRevocarEnlace,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRevocarEnlace) (EnlaceRevocado, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return EnlaceRevocado{}, err
			}
			q := mensajeriabd.New(t)
			e, err := q.EnlaceManualParaRevocar(ctx, in.ID)
			noExiste := nucleo.NoEncontrado("El enlace no existe")
			if errors.Is(err, pgx.ErrNoRows) {
				return EnlaceRevocado{}, noExiste
			}
			if err != nil {
				return EnlaceRevocado{}, err
			}
			// Solo los de una conversación que ves (y por tanto de tu empresa).
			if e.ConversacionID == nil {
				return EnlaceRevocado{}, noExiste
			}
			c, err := convVisible(ctx, q, a, *e.ConversacionID, false)
			if err != nil {
				return EnlaceRevocado{}, noExiste
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("manual_enlace", e.ID)
			if e.RevocadoEn != nil {
				return EnlaceRevocado{ID: e.ID, ManualID: e.ManualID, RevocadoEn: *e.RevocadoEn}, nil
			}
			ahora := m.ahora()
			if err := q.RevocarEnlaceManual(ctx, mensajeriabd.RevocarEnlaceManualParams{ID: e.ID, RevocadoEn: &ahora, RevocadoPor: &a.usuario}); err != nil {
				return EnlaceRevocado{}, err
			}
			t.RegistrarCambios("manual_enlace", e.ID, map[string]any{"revocado_en": nil}, map[string]any{"revocado_en": ahora})
			return EnlaceRevocado{ID: e.ID, ManualID: e.ManualID, RevocadoEn: ahora},
				t.Emitir("manual.enlace_revocado", map[string]any{"enlace_id": e.ID, "manual_id": e.ManualID, "conversacion_id": c.ID,
					"empresa_id": c.EmpresaID, "actor_id": a.usuario})
		},
	})
}

// Enlace público ---------------------------------------------------------------------------------------

type manualPublico struct {
	html []byte
	vivo bool
}

func nuevoManualPublico(m *Modulo) *auditoria.Caso[string, manualPublico] {
	return auditoria.Nuevo(auditoria.Caso[string, manualPublico]{
		Accion: "manuales.ver_publico", Acceso: auditoria.Publico, AuditarExito: func(manualPublico) bool { return false },
		AuditarError: func(error) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, token string) (manualPublico, error) {
			h := sha256.Sum256([]byte(token))
			f, err := mensajeriabd.New(t).ManualPorEnlace(ctx, h[:])
			if errors.Is(err, pgx.ErrNoRows) {
				return manualPublico{}, nil
			}
			if err != nil {
				return manualPublico{}, err
			}
			if !f.Activo || f.Visibilidad != "cliente" || !m.ahora().Before(f.VenceEn) {
				return manualPublico{}, nil
			}
			return manualPublico{html: htmlManual(f.Titulo, f.Cuerpo), vivo: true}, nil
		},
	})
}

// htmlManual arma una página mínima y segura: todo el texto escapado, párrafos por líneas en blanco.
func htmlManual(titulo, cuerpo string) []byte {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<meta name="robots" content="noindex"><title>` + html.EscapeString(titulo) + `</title>`)
	b.WriteString(`<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:2rem auto;padding:0 1rem;line-height:1.5;color:#0f172a;background:#fff}` +
		`@media (prefers-color-scheme:dark){body{color:#e2e8f0;background:#0b1120}}h1{font-size:1.4rem}</style></head><body>`)
	b.WriteString("<h1>" + html.EscapeString(titulo) + "</h1>")
	for _, p := range strings.Split(strings.ReplaceAll(cuerpo, "\r\n", "\n"), "\n\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(p), "\n", "<br>") + "</p>")
	}
	b.WriteString("</body></html>")
	return []byte(b.String())
}

// Rutas -------------------------------------------------------------------------------------------------

// casosHorarios reúne horarios y manuales.
type casosHorarios struct {
	Listar           *auditoria.Caso[struct{}, ListaHorarios]
	Ver              *auditoria.Caso[EntradaHorarioID, Horario]
	Estado           *auditoria.Caso[EntradaEstadoHorario, EstadoHorario]
	Crear            *auditoria.Caso[EntradaHorario, Horario]
	Actualizar       *auditoria.Caso[EntradaHorario, Horario]
	BuscarManuales   *auditoria.Caso[EntradaBuscarManuales, ListaManuales]
	VerManual        *auditoria.Caso[EntradaManualID, Manual]
	CrearManual      *auditoria.Caso[EntradaManual, Manual]
	ActualizarManual *auditoria.Caso[EntradaManual, Manual]
	ActivarManual    *auditoria.Caso[EntradaManualID, Manual]
	DesactivarManual *auditoria.Caso[EntradaManualID, Manual]
	Sugerir          *SugerirManuales
	Enviar           *auditoria.Caso[EntradaEnviarManual, MensajeConvEnviado]
	Enlaces          *auditoria.Caso[EntradaConversacion, ListaEnlacesManual]
	Revocar          *auditoria.Caso[EntradaRevocarEnlace, EnlaceRevocado]
	publico          *auditoria.Caso[string, manualPublico]
}

func nuevosCasosHorarios(m *Modulo) casosHorarios {
	return casosHorarios{Listar: nuevoListarHorarios(m), Ver: nuevoVerHorario(m), Estado: nuevoEstadoHorario(m),
		Crear: nuevoGuardarHorario(m, true), Actualizar: nuevoGuardarHorario(m, false),
		BuscarManuales: nuevoBuscarManuales(m), VerManual: nuevoVerManual(m), CrearManual: nuevoGuardarManual(m, true),
		ActualizarManual: nuevoGuardarManual(m, false), ActivarManual: nuevoCambioManual(m, "manuales.activar", true),
		DesactivarManual: nuevoCambioManual(m, "manuales.desactivar", false), Sugerir: nuevoSugerirManuales(m), Enviar: nuevoEnviarManual(m),
		Enlaces: nuevoListarEnlacesManual(m), Revocar: nuevoRevocarEnlaceManual(m), publico: nuevoManualPublico(m)}
}

type salidaHTML struct {
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	CSP          string `header:"Content-Security-Policy"`
	Body         []byte
}

func (m *Modulo) registrarHorariosManuales(api huma.API) {
	k := m.Horarios
	ver, canalesConf := nucleo.PermConversacionesVer, nucleo.PermCanalesConfigurar
	huma.Register(api, op("listarHorarios", http.MethodGet, "/horarios", "Horarios de atención de la empresa", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ListaHorarios], error) {
			return responder(k.Listar.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, op("verHorario", http.MethodGet, "/horarios/{id}", "Un horario", ver, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaHorarioID) (*cuerpo[Horario], error) {
			return responder(k.Ver.Ejecutar(ctx, *in))
		})
	huma.Register(api, op("estadoHorario", http.MethodGet, "/horarios/{id}/estado", "¿Abierto ahora (o en un instante)?", ver, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaEstadoHorario) (*cuerpo[EstadoHorario], error) {
			return responder(k.Estado.Ejecutar(ctx, *in))
		})
	crear := op("crearHorario", http.MethodPost, "/horarios", "Crear un horario", canalesConf, http.StatusCreated, http.StatusConflict)
	crear.Description += " Franjas por día (1 = lunes … 7 = domingo) en hora local de `zona`; feriados que cierran el día o lo reemplazan. " +
		"`perfil` por franja es para los asistentes de la fase 2 (se guarda; hoy nada lo usa)."
	huma.Register(api, crear, func(ctx context.Context, in *cuerpoHTTP[EntradaHorario]) (*cuerpo[Horario], error) {
		return responder(k.Crear.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("actualizarHorario", http.MethodPost, "/horarios/{id}/actualizar", "Actualizar un horario", canalesConf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaHorario]) (*cuerpo[Horario], error) {
		in.Body.ID = in.ID
		return responder(k.Actualizar.Ejecutar(ctx, in.Body))
	})

	mver, medit := nucleo.PermManualesVer, nucleo.PermManualesEditar
	buscar := op("buscarManuales", http.MethodGet, "/manuales", "Buscar en la biblioteca de manuales", mver, http.StatusOK)
	buscar.Description += " Texto completo en español (Postgres websearch_to_tsquery) por título, resumen, etiquetas, equipo y cuerpo, por relevancia."
	huma.Register(api, buscar, func(ctx context.Context, in *EntradaBuscarManuales) (*cuerpo[ListaManuales], error) {
		return responder(k.BuscarManuales.Ejecutar(ctx, *in))
	})
	huma.Register(api, op("verManual", http.MethodGet, "/manuales/{id}", "Un manual", mver, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaManualID) (*cuerpo[Manual], error) {
			return responder(k.VerManual.Ejecutar(ctx, *in))
		})
	huma.Register(api, op("crearManual", http.MethodPost, "/manuales", "Crear un manual", medit, http.StatusCreated),
		func(ctx context.Context, in *cuerpoHTTP[EntradaManual]) (*cuerpo[Manual], error) {
			return responder(k.CrearManual.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, op("actualizarManual", http.MethodPost, "/manuales/{id}/actualizar", "Actualizar un manual", medit,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaManual]) (*cuerpo[Manual], error) {
		in.Body.ID = in.ID
		return responder(k.ActualizarManual.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("activarManual", http.MethodPost, "/manuales/{id}/activar", "Activar un manual", medit, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[Manual], error) {
			return responder(k.ActivarManual.Ejecutar(ctx, EntradaManualID{ID: in.ID}))
		})
	huma.Register(api, op("desactivarManual", http.MethodPost, "/manuales/{id}/desactivar", "Desactivar un manual", medit, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[Manual], error) {
			return responder(k.DesactivarManual.Ejecutar(ctx, EntradaManualID{ID: in.ID}))
		})
	sug := op("manualesSugeridos", http.MethodGet, "/conversaciones/{id}/manuales-sugeridos", "Manuales sugeridos para una conversación", mver,
		http.StatusOK, http.StatusNotFound)
	sug.Description += " Por reglas, cada uno con sus motivos: etiqueta de la conversación, equipo del servicio (Argos) y ticket abierto del cliente. Hasta 5."
	huma.Register(api, sug, func(ctx context.Context, in *EntradaConversacion) (*cuerpo[ListaSugeridos], error) {
		return responder(k.Sugerir.Ejecutar(ctx, *in))
	})
	env := op("enviarManualConversacion", http.MethodPost, "/conversaciones/{id}/enviar-manual", "Enviar un manual al cliente",
		nucleo.PermConversacionesResponder, http.StatusCreated, http.StatusNotFound)
	env.Description += " Solo los de visibilidad cliente (422 `manual_interno`). enlace: URL pública que vence a los 30 días; texto: el cuerpo. Respeta la ventana de 24 h."
	env.Responses = map[string]*huma.Response{"200": {Description: "Reenvío del mismo id: el mensaje tal como está ahora"}}
	huma.Register(api, env, func(ctx context.Context, in *idCuerpoHTTP[EntradaEnviarManual]) (*salidaMensajeConv, error) {
		in.Body.ConversacionID, in.Body.IdempotencyKey = in.ID, in.IdempotencyKey
		return respMensaje(k.Enviar.Ejecutar(ctx, in.Body))
	})
	reenvioConCuerpo(api, env.OperationID)
	huma.Register(api, op("enlacesManualConversacion", http.MethodGet, "/conversaciones/{id}/manual-enlaces",
		"Enlaces de manuales enviados en la conversación (sin el token)", nucleo.PermConversacionesVer, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaConversacion) (*cuerpo[ListaEnlacesManual], error) {
			return responder(k.Enlaces.Ejecutar(ctx, *in))
		})
	rev := op("revocarEnlaceManual", http.MethodPost, "/manual-enlaces/{id}/revocar", "Revocar el enlace público de un manual enviado",
		nucleo.PermManualesRevocarEnlace, http.StatusOK, http.StatusNotFound)
	rev.Description += " Desde ese momento el enlace responde 410. Revocar uno ya revocado: 200 sin cambios."
	huma.Register(api, rev, func(ctx context.Context, in *idHTTP) (*cuerpo[EnlaceRevocado], error) {
		return responder(k.Revocar.Ejecutar(ctx, EntradaRevocarEnlace{ID: in.ID}))
	})
	pub := op("manualPublico", http.MethodGet, "/publico/manuales/{token}", "Manual para el cliente (enlace público)", "", http.StatusOK, http.StatusGone)
	pub.Security = nil
	pub.Description = "Página HTML mínima y segura del artículo (todo el texto escapado). 410 si el enlace venció o el artículo se desactivó."
	pub.Responses = map[string]*huma.Response{"200": {Description: "El artículo",
		Content: map[string]*huma.MediaType{"text/html": {Schema: &huma.Schema{Type: huma.TypeString}}}}}
	huma.Register(api, pub, func(ctx context.Context, in *struct {
		Token string `path:"token" minLength:"20" maxLength:"100"`
	}) (*salidaHTML, error) {
		r, err := k.publico.Ejecutar(ctx, in.Token)
		if err != nil {
			return nil, err
		}
		if !r.vivo {
			return nil, nucleo.NuevoProblema(http.StatusGone, "enlace_vencido", "Enlace vencido", "Este enlace ya no está disponible")
		}
		return &salidaHTML{ContentType: "text/html; charset=utf-8", CacheControl: "no-store",
			CSP: "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'", Body: r.html}, nil
	})
}
