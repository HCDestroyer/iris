package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Sincronización de las plantillas de una cuenta con Meta (contratos/plantillas-whatsapp.md §5):
// en vivo por el webhook (message_template_status_update, template_category_update y
// message_template_quality_update), una relectura cada noche (job de River único por cuenta) y el
// botón «Sincronizar con Meta». Lo que Meta ya no devuelve queda «Borrada en Meta», nunca se elimina.

// Origen de una sincronización.
const (
	SyncManual   = "manual"
	SyncNocturna = "nocturna"
	SyncWebhook  = "webhook"
	SyncCanal    = "canal"
)

// EsperaSincronizarManual: el botón no relee una cuenta sincronizada hace menos de esto (429).
const EsperaSincronizarManual = 2 * time.Minute

// EntradaSincronizarPWA es POST /plantillas-whatsapp/cuentas/{waba}/sincronizar.
type EntradaSincronizarPWA struct {
	WabaID string `json:"waba_id"`
}

// SincronizacionPedida es la respuesta 202: la relectura quedó en cola (único por cuenta). El
// resultado sale en GET /plantillas-whatsapp/cuentas (sincronizadaEn, resumen y error).
type SincronizacionPedida struct {
	WabaID         string     `json:"wabaId"`
	SincronizadaEn *time.Time `json:"sincronizadaEn" nullable:"true" doc:"La sincronización anterior (cambia al terminar la pedida)"`
}

// ResultadoSincronizacionPWA es lo que quedó (sincronización del sistema).
type ResultadoSincronizacionPWA struct {
	WabaID         string                   `json:"wabaId"`
	SincronizadaEn time.Time                `json:"sincronizadaEn"`
	Resumen        ResumenSincronizacionPWA `json:"resumen"`
}

type lecturaPW struct {
	// eventos: el meta_evento_en de cada fila antes de leer Meta. Si al escribir cambió, un aviso de Meta
	// llegó durante la lectura: esa fila ya está más al día que lo leído y no se pisa (se compara
	// el reloj de Meta con el de Meta, nunca con el de Iris).
	eventos map[int64]fotoPW
	waba    string
	empresa int64
	origen  string
	leidas  []PlantillaMetaLeida
	err     error
	cuenta  CuentaGraph
}

// fotoPW: cómo estaba una fila antes de leer Meta.
type fotoPW struct {
	metaEventoEn int64
	version      int32
}

// cambioDurante: la fila recibió un aviso de Meta después de la foto (una fila nueva cuenta desde 0).
func (l lecturaPW) cambioDurante(f mensajeriabd.MensajeriaPlantillaWA) bool {
	return l.eventos != nil && f.MetaEventoEn != l.eventos[f.ID].metaEventoEn
}

// igualQueEnLaFoto: la fila estaba antes de leer Meta y nadie la cambió mientras tanto (ni un aviso
// ni un envío). Solo una así se puede dar por «Borrada en Meta» si Meta no la devolvió.
func (l lecturaPW) igualQueEnLaFoto(f mensajeriabd.MensajeriaPlantillaWA) bool {
	foto, ok := l.eventos[f.ID]
	return ok && foto.version == f.Version && foto.metaEventoEn == f.MetaEventoEn
}

func (l lecturaPW) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"waba_id": l.waba, "origen": l.origen, "leidas": len(l.leidas), "error": l.err != nil})
}

func empresaPtr(e int64) *int64 {
	if e == 0 {
		return nil
	}
	return &e
}

// SincronizarPWA: el botón (encola la relectura) y la relectura del sistema (job).
type SincronizarPWA struct {
	m       *Modulo
	pedir   *auditoria.Caso[EntradaSincronizarPWA, SincronizacionPedida]
	aplicar *auditoria.Caso[lecturaPW, ResultadoSincronizacionPWA]
}

func nuevoSincronizarPWA(m *Modulo) *SincronizarPWA {
	s := &SincronizarPWA{m: m}
	s.pedir = auditoria.Nuevo(auditoria.Caso[EntradaSincronizarPWA, SincronizacionPedida]{
		Accion: "plantillas_whatsapp.sincronizar", Permiso: nucleo.PermPlantillasWhatsAppSincronizar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSincronizarPWA) (SincronizacionPedida, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return SincronizacionPedida{}, err
			}
			q := mensajeriabd.New(t)
			empresa := contexto.EmpresaDe(ctx)
			if _, err := canalesDeCuentaEmpresa(ctx, q, empresa, in.WabaID); err != nil {
				return SincronizacionPedida{}, err
			}
			k, err := q.CuentaWA(ctx, in.WabaID)
			if err != nil {
				return SincronizacionPedida{}, err
			}
			t.FijarEntidad("cuenta_whatsapp", in.WabaID)
			t.FijarEmpresa(empresa)
			if k.SincronizadaEn != nil {
				if falta := k.SincronizadaEn.Add(EsperaSincronizarManual).Sub(m.ahora()); falta > 0 {
					seg := int(falta.Seconds()) + 1
					p := nucleo.NuevoProblema(http.StatusTooManyRequests, "sincronizada_hace_poco", "Se sincronizó hace poco",
						"Esta cuenta se sincronizó hace menos de 2 minutos. Espera un momento y vuelve a intentar.")
					disponible := k.SincronizadaEn.Add(EsperaSincronizarManual).UTC()
					p.DisponibleEn = &disponible
					return SincronizacionPedida{}, p.ConCabecera("Retry-After", strconv.Itoa(seg))
				}
			}
			return SincronizacionPedida{WabaID: in.WabaID, SincronizadaEn: k.SincronizadaEn},
				m.encolar(ctx, t.Tx, ArgsSincronizarPlantillas{WabaID: in.WabaID, Origen: SyncManual})
		},
	})
	// aplicar corre para el sistema (job): Autenticado; la persona ya pasó por pedir.
	s.aplicar = auditoria.Nuevo(auditoria.Caso[lecturaPW, ResultadoSincronizacionPWA]{
		Accion: "plantillas_whatsapp.sincronizar", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in lecturaPW) (ResultadoSincronizacionPWA, error) {
			q := mensajeriabd.New(t)
			t.FijarEntidad("cuenta_whatsapp", in.waba)
			t.FijarEmpresa(in.empresa)
			ahora := m.ahora()
			if in.err != nil {
				p := problemaMetaPW(in.err)
				codigo := p.Codigo()
				t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
					return mensajeriabd.New(tx).EstadoCuentaWA(ctx, mensajeriabd.EstadoCuentaWAParams{WabaID: in.waba, Empresa: in.empresa,
						Error: &codigo, ErrorEn: &ahora})
				})
				return ResultadoSincronizacionPWA{}, p
			}
			r, err := m.aplicarLectura(ctx, t, q, in, ahora)
			if err != nil {
				return ResultadoSincronizacionPWA{}, err
			}
			t.Anotar(map[string]any{"total": r.Total, "nuevas": len(r.Nuevas), "cambiadas": len(r.Cambiadas), "borradas": len(r.Borradas), "origen": in.origen})
			return ResultadoSincronizacionPWA{WabaID: in.waba, SincronizadaEn: ahora, Resumen: r}, nil
		},
	})
	return s
}

// Ejecutar es el botón «Sincronizar con Meta»: encola la relectura.
func (s *SincronizarPWA) Ejecutar(ctx context.Context, in EntradaSincronizarPWA) (SincronizacionPedida, error) {
	return s.pedir.Ejecutar(ctx, in)
}

// leerCuentaMeta comprueba el permiso del token y lee todas las plantillas de la cuenta.
func (m *Modulo) leerCuentaMeta(ctx context.Context, c CuentaGraph) ([]PlantillaMetaLeida, error) {
	g := m.graphPlantillas()
	if tiene, leido := g.Permiso(ctx, c); leido && !tiene {
		return nil, &ErrMetaPlantilla{Status: 403, Codigo: 200}
	}
	return g.Listar(ctx, c)
}

// SincronizarCuentaSistema relee una cuenta desde un job (botón, noche, webhook o el formulario del
// número), con el token de un número de la empresa dueña y solo sus plantillas.
func (m *Modulo) SincronizarCuentaSistema(ctx context.Context, bd nucleo.BD, waba, origen string) error {
	var l lecturaPW
	err := nucleo.EnTx(nucleo.ConBD(ctx, bd), func(ctx context.Context, tx pgx.Tx) error {
		q := mensajeriabd.New(tx)
		k, err := q.CuentaWA(ctx, waba)
		if err != nil {
			return err
		}
		c, err := m.cuentaGraph(ctx, q, k.EmpresaID, waba)
		if err != nil {
			return err
		}
		l = lecturaPW{waba: waba, empresa: k.EmpresaID, origen: origen, cuenta: c, eventos: map[int64]fotoPW{}}
		fotos, err := q.EventosMetaDeCuentaPW(ctx, mensajeriabd.EventosMetaDeCuentaPWParams{WabaID: waba, Empresa: k.EmpresaID})
		if err != nil {
			return err
		}
		for _, f := range fotos {
			l.eventos[f.ID] = fotoPW{metaEventoEn: f.MetaEventoEn, version: f.Version}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // la cuenta no tiene dueña registrada
	}
	var p *nucleo.Problema
	if errors.As(err, &p) {
		slog.WarnContext(ctx, "plantillas: la cuenta no tiene números ni token para sincronizar", "waba_id", waba, "codigo", p.Codigo())
		return nil
	}
	if err != nil {
		return err
	}
	l.leidas, l.err = m.leerCuentaMeta(ctx, l.cuenta)
	_, err = m.PlantillasWA.Sincronizar.aplicar.Ejecutar(ctxSistema(ctx, bd), l)
	if errors.As(err, &p) {
		// Meta no respondió o el token no sirve: queda anotado en la cuenta; la próxima vez se reintenta.
		slog.WarnContext(ctx, "plantillas: sincronización con Meta fallida", "waba_id", waba, "codigo", p.Codigo(), "origen", origen)
		return nil
	}
	return err
}

func cambioPW(f mensajeriabd.MensajeriaPlantillaWA) CambioCuentaPWA {
	return CambioCuentaPWA{ID: f.ID, Nombre: f.Nombre, Etiqueta: f.Etiqueta, Estado: f.Estado, Codigos: lista(f.Codigos), Usos: lista(f.Usos)}
}

// aplicarLectura compara lo que dijo Meta con lo que tiene Iris.
func (m *Modulo) aplicarLectura(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, in lecturaPW, ahora time.Time) (ResumenSincronizacionPWA, error) {
	if err := q.BloquearCuentaPlantillas(ctx, in.waba); err != nil {
		return ResumenSincronizacionPWA{}, err
	}
	filas, err := q.PlantillasWADeCuenta(ctx, mensajeriabd.PlantillasWADeCuentaParams{WabaID: in.waba, Empresa: in.empresa})
	if err != nil {
		return ResumenSincronizacionPWA{}, err
	}
	r := ResumenSincronizacionPWA{Total: len(in.leidas), Nuevas: []CambioCuentaPWA{}, Cambiadas: []CambioCuentaPWA{}, Borradas: []CambioCuentaPWA{}, Origen: in.origen}
	vistas := map[int64]bool{}
	buscar := func(p PlantillaMetaLeida) int {
		if i := slices.IndexFunc(filas, func(f mensajeriabd.MensajeriaPlantillaWA) bool {
			return f.Estado != EstadoPWBorrada && f.MetaID != nil && *f.MetaID == p.ID
		}); i >= 0 {
			return i
		}
		// Migradas (sin id de Meta): por nombre e idioma.
		return slices.IndexFunc(filas, func(f mensajeriabd.MensajeriaPlantillaWA) bool {
			return f.Estado != EstadoPWBorrada && f.Estado != EstadoPWBorrador && f.MetaID == nil && f.Nombre == p.Name && f.Idioma == p.Language && !vistas[f.ID]
		})
	}
	for _, p := range in.leidas {
		if p.ID == "" || p.Name == "" || !reNombrePW.MatchString(p.Name) || !reIdiomaPW.MatchString(p.Language) {
			continue
		}
		i := buscar(p)
		if i < 0 {
			if toUpper(p.Status) == EstadoPWBorrada {
				continue
			}
			f, err := m.insertarDeMeta(ctx, t, q, in, p, ahora)
			if err != nil {
				return r, err
			}
			vistas[f.ID] = true
			if len(r.Nuevas) < 50 {
				r.Nuevas = append(r.Nuevas, cambioPW(f))
			}
			continue
		}
		f := filas[i]
		vistas[f.ID] = true
		if in.cambioDurante(f) {
			continue // un aviso de Meta llegó durante la lectura: ya está más al día
		}
		g, cambio, err := m.aplicarMetaLeida(ctx, t, q, f, p, "sincronizacion", ahora)
		if err != nil {
			return r, err
		}
		filas[i] = g
		if cambio && len(r.Cambiadas) < 50 {
			r.Cambiadas = append(r.Cambiadas, cambioPW(g))
		}
	}
	for _, f := range filas {
		if vistas[f.ID] || f.Estado == EstadoPWBorrada || f.Estado == EstadoPWBorrador || !in.igualQueEnLaFoto(f) {
			continue
		}
		// Meta ya no la tiene: «Borrada en Meta», con su historial y, si se usaba, sus usos.
		g, err := borrarPW(ctx, t, q, f, "meta", "sincronizacion", ahora)
		if err != nil {
			return r, err
		}
		if len(r.Borradas) < 50 {
			r.Borradas = append(r.Borradas, cambioPW(g))
		}
	}
	resumen, _ := json.Marshal(r)
	if err := q.EstadoCuentaWA(ctx, mensajeriabd.EstadoCuentaWAParams{WabaID: in.waba, Empresa: in.empresa, SincronizadaEn: &ahora, Resumen: resumen}); err != nil {
		return r, err
	}
	return r, t.EmitirEnEmpresa("plantilla_whatsapp.sincronizada", empresaPtr(in.empresa), map[string]any{"waba_id": in.waba, "empresa_id": empresaPtr(in.empresa),
		"origen": in.origen, "total": r.Total, "nuevas": len(r.Nuevas), "cambiadas": len(r.Cambiadas), "borradas": len(r.Borradas)})
}

func (m *Modulo) insertarDeMeta(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, in lecturaPW, p PlantillaMetaLeida, ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, error) {
	d := definicionDeMeta(p.Components, nil)
	comp, _ := json.Marshal(d)
	params, _ := json.Marshal(parametrosDe(d))
	formato := toUpper(p.ParameterFormat)
	if formato != "NAMED" && formato != "POSITIONAL" {
		formato = formatoDeDefinicion(d)
	}
	categoria := normalizarCategoria(p.Category)
	if categoria == "" {
		categoria = CategoriaUtility
	}
	metaID := p.ID
	etiqueta := recortar(strings.ReplaceAll(p.Name, "_", " "), 80)
	f, err := q.InsertarPlantillaWA(ctx, mensajeriabd.InsertarPlantillaWAParams{EmpresaID: empresaPtr(in.empresa), WabaID: in.waba, Codigos: []string{}, MetaID: &metaID, Nombre: p.Name,
		Idioma: p.Language, Etiqueta: etiqueta, Categoria: categoria, Estado: normalizarEstadoMeta(p.Status, EstadoPWPendiente), Motivo: motivoMeta(p.RejectedReason),
		Calidad: p.Calidad(), Formato: formato, Componentes: comp, ComponentesMeta: p.Components, Parametros: params, Usos: []string{}, Origen: OrigenPWMeta,
		SincronizadaEn: &ahora})
	if err != nil {
		return f, err
	}
	if err := historialPW(ctx, q, f.ID, "creada", nil, &f.Estado, map[string]any{"origen": "meta"}, "sincronizacion", nil, ahora); err != nil {
		return f, err
	}
	return f, t.EmitirEnEmpresa("plantilla_whatsapp.creada", empresaPtr(in.empresa), conPW(basePW(f), map[string]any{"nombre": f.Nombre, "idioma": f.Idioma,
		"categoria": f.Categoria, "origen": OrigenPWMeta}))
}

func motivoMeta(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "NONE") {
		return nil
	}
	return ptr(literalAcotado(s))
}

// aplicarMetaLeida pone en la fila lo que devolvió Meta (estado, categoría, calidad y contenido).
func (m *Modulo) aplicarMetaLeida(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, p PlantillaMetaLeida,
	origen string, ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, bool, error) {
	antes := f
	id := p.ID
	f.MetaID = &id
	if estado := normalizarEstadoMeta(p.Status, f.Estado); estado != f.Estado && estado != EstadoPWBorrada {
		f.Estado, f.EstadoEn = estado, ahora
		if estado != EstadoPWPausada {
			f.PausadaHasta = nil
		}
	}
	f.Motivo = motivoMeta(p.RejectedReason)
	if f.Estado != EstadoPWRechazada {
		f.Motivo, f.Rechazo = nil, nil
	}
	if c := p.Calidad(); c != "UNKNOWN" || f.Calidad == "" {
		f.Calidad = c
	}
	if cat := normalizarCategoria(p.Category); cat != "" && cat != f.Categoria {
		f.CategoriaAnterior, f.CategoriaCambiadaEn, f.Categoria = &antes.Categoria, &ahora, cat
		f.CategoriaProxima, f.CategoriaProximaEn = nil, nil
	}
	if len(p.Components) > 0 {
		d := definicionDeMeta(p.Components, definicionDe(f.Componentes).Variables)
		if prev := definicionDe(f.Componentes); prev.Encabezado != nil && prev.Encabezado.AdjuntoID != nil && d.Encabezado != nil && d.Encabezado.Tipo == prev.Encabezado.Tipo {
			d.Encabezado.AdjuntoID = prev.Encabezado.AdjuntoID
		}
		f.Componentes, _ = json.Marshal(d)
		f.ComponentesMeta = p.Components
		if pf := toUpper(p.ParameterFormat); pf == "NAMED" || pf == "POSITIONAL" {
			f.Formato = pf
		}
	}
	f.SincronizadaEn = &ahora
	g, err := guardarPW(ctx, q, f)
	if err != nil {
		return g, false, err
	}
	cambio := false
	if g.Estado != antes.Estado {
		cambio = true
		if err := historialPW(ctx, q, g.ID, "estado", &antes.Estado, &g.Estado, map[string]any{"motivo": g.Motivo}, origen, nil, ahora); err != nil {
			return g, cambio, err
		}
		if err := t.EmitirEnEmpresa("plantilla_whatsapp.estado_cambiado", g.EmpresaID, conPW(basePW(g), map[string]any{"de": antes.Estado, "a": g.Estado,
			"motivo": g.Motivo, "origen": origen, "pausada_hasta": g.PausadaHasta})); err != nil {
			return g, cambio, err
		}
	}
	if g.Categoria != antes.Categoria {
		cambio = true
		t.FijarEmpresa(derefEmpresa(g.EmpresaID))
		if err := emitirCategoriaPW(ctx, t, q, g, antes.Categoria, g.Categoria, origen, false, ahora); err != nil {
			return g, cambio, err
		}
	}
	if g.Calidad != antes.Calidad {
		cambio = true
		if err := historialPW(ctx, q, g.ID, "calidad", &antes.Calidad, &g.Calidad, nil, origen, nil, ahora); err != nil {
			return g, cambio, err
		}
		if err := t.EmitirEnEmpresa("plantilla_whatsapp.calidad_cambiada", g.EmpresaID, conPW(basePW(g), map[string]any{"de": antes.Calidad, "a": g.Calidad,
			"origen": origen})); err != nil {
			return g, cambio, err
		}
	}
	return g, cambio, nil
}

// Webhook -------------------------------------------------------------------------------------------

// valorPlantillaMeta son los campos de los webhooks de plantillas (entry[].changes[].value).
type valorPlantillaMeta struct {
	Event                   string      `json:"event"`
	MessageTemplateID       json.Number `json:"message_template_id"`
	MessageTemplateName     string      `json:"message_template_name"`
	MessageTemplateLanguage string      `json:"message_template_language"`
	MessageTemplateCategory string      `json:"message_template_category"`
	Reason                  *string     `json:"reason"`
	RejectionInfo           *struct {
		Reason         string `json:"reason"`
		Recommendation string `json:"recommendation"`
	} `json:"rejection_info"`
	OtherInfo *struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"other_info"`
	PreviousCategory        string `json:"previous_category"`
	NewCategory             string `json:"new_category"`
	CorrectCategory         string `json:"correct_category"`
	CategoryUpdateTimestamp int64  `json:"category_update_timestamp"`
	PreviousQualityScore    string `json:"previous_quality_score"`
	NewQualityScore         string `json:"new_quality_score"`
}

// CamposWebhookPlantillas son los campos de Meta que trae este módulo.
var CamposWebhookPlantillas = []string{"message_template_status_update", "template_category_update", "message_template_quality_update"}

// ArgsSincronizarPlantillas relee las plantillas de una cuenta (noche o un webhook de una plantilla
// que Iris no conoce). Único por cuenta.
type ArgsSincronizarPlantillas struct {
	WabaID string `json:"waba_id" river:"unique"`
	Origen string `json:"origen"`
}

// Kind implementa river.JobArgs.
func (ArgsSincronizarPlantillas) Kind() string { return "mensajeria.sincronizar_plantillas" }

// InsertOpts: cola de mensajería, uno por cuenta (en espera o corriendo) cada 2 minutos.
func (ArgsSincronizarPlantillas) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: EsperaSincronizarManual}}
}

type trabajadorSincronizarPlantillas struct {
	river.WorkerDefaults[ArgsSincronizarPlantillas]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorSincronizarPlantillas) Work(ctx context.Context, job *river.Job[ArgsSincronizarPlantillas]) error {
	return w.m.SincronizarCuentaSistema(ctxSistema(ctx, w.bd), w.bd, job.Args.WabaID, job.Args.Origen)
}

// plantillasDelWebhook busca en el cuerpo los cambios de plantillas y los aplica.
func (m *Modulo) plantillasDelWebhook(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, cuerpo []byte, ahora time.Time) error {
	if !strings.Contains(string(cuerpo), "template") {
		return nil
	}
	var w struct {
		Entry []struct {
			ID      string `json:"id"`
			Time    int64  `json:"time"`
			Changes []struct {
				Field string          `json:"field"`
				Value json.RawMessage `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if json.Unmarshal(cuerpo, &w) != nil {
		return nil
	}
	for _, e := range w.Entry {
		for _, c := range e.Changes {
			if slices.Contains(CamposWebhookPlantillas, c.Field) {
				if err := m.webhookPlantilla(ctx, t, q, canal, e.ID, e.Time, c.Field, c.Value, ahora); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// webhookPlantilla aplica un cambio de plantilla que avisó Meta. waba: entry[].id; ocurrido: entry[].time.
func (m *Modulo) webhookPlantilla(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, waba string, ocurrido int64,
	campo string, crudo json.RawMessage, ahora time.Time) error {
	// Solo cuenta lo de la cuenta del número que recibió el webhook (la firma es la de su app), y solo si
	// el número es de la empresa dueña de la cuenta.
	if canal.WabaID == "" || waba != canal.WabaID {
		t.Anotar(map[string]any{"plantilla_de_otra_cuenta": true})
		return nil
	}
	k, err := q.CuentaWA(ctx, waba)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && k.EmpresaID != derefEmpresa(canal.EmpresaID)) {
		t.Anotar(map[string]any{"plantilla_de_otra_empresa": true})
		return nil
	}
	if err != nil {
		return err
	}
	// El mismo candado que la relectura y los cambios de la cuenta (orden: cuenta y después la fila).
	if err := q.BloquearCuentaPlantillas(ctx, waba); err != nil {
		return err
	}
	var v valorPlantillaMeta
	if err := json.Unmarshal(crudo, &v); err != nil {
		t.Anotar(map[string]any{"plantilla_ilegible": true})
		return nil
	}
	id := v.MessageTemplateID.String()
	if !reDigitos.MatchString(id) {
		return nil
	}
	// El mismo aviso puede llegar por varios números de la cuenta (o repetido): se aplica una vez.
	if ocurrido > 0 {
		evento := toUpper(v.Event) + toUpper(v.NewCategory) + "/" + toUpper(v.CorrectCategory) + toUpper(v.NewQualityScore)
		n, err := q.RegistrarAvisoMetaPW(ctx, mensajeriabd.RegistrarAvisoMetaPWParams{WabaID: waba, MetaID: id, Campo: campo, Evento: recortar(evento, 60), Ocurrido: ocurrido})
		if err != nil {
			return err
		}
		if n == 0 {
			t.Anotar(map[string]any{"plantilla_aviso_repetido": true})
			return nil
		}
	}
	cuando := ahora
	if ocurrido > 0 {
		cuando = time.Unix(ocurrido, 0).UTC()
	}
	f, err := q.PlantillaWAPorMeta(ctx, mensajeriabd.PlantillaWAPorMetaParams{WabaID: waba, Empresa: k.EmpresaID, MetaID: &id, Nombre: v.MessageTemplateName, Idioma: v.MessageTemplateLanguage})
	if errors.Is(err, pgx.ErrNoRows) {
		// Una plantilla que Iris no conoce (creada en Meta): se relee la cuenta.
		return m.encolar(ctx, t.Tx, ArgsSincronizarPlantillas{WabaID: waba, Origen: SyncWebhook})
	}
	if err != nil {
		return err
	}
	fijarEmpresaCanal(t, f.EmpresaID)
	antes := f
	f.MetaID = &id
	switch campo {
	case "message_template_status_update":
		// Un aviso más viejo que el último que se aplicó (llegó tarde) no lo deshace: se compara con el
		// entry.time del último aplicado (meta_evento_en, reloj de Meta), nunca con estado_en. Como algo
		// pasó en Meta que Iris no vio en orden, se relee la cuenta.
		if ocurrido > 0 && ocurrido < f.MetaEventoEn {
			t.Anotar(map[string]any{"plantilla_aviso_viejo": true})
			return m.encolar(ctx, t.Tx, ArgsSincronizarPlantillas{WabaID: waba, Origen: SyncWebhook})
		}
		f.MetaEventoEn = max(f.MetaEventoEn, ocurrido)
		ev := toUpper(v.Event)
		switch ev {
		case "REINSTATED":
			ev = EstadoPWAprobada
		case "UNARCHIVED":
			// Vuelve al estado que tenía: se relee.
			return m.encolar(ctx, t.Tx, ArgsSincronizarPlantillas{WabaID: waba, Origen: SyncWebhook})
		}
		if ev == EstadoPWBorrada {
			if f.Estado == EstadoPWBorrada {
				return nil
			}
			_, err := borrarPW(ctx, t, q, f, "meta", "webhook", ahora)
			return err
		}
		if !slices.Contains(EstadosMetaPW, ev) {
			return nil
		}
		f.Estado, f.EstadoEn = ev, cuando
		f.PausadaHasta = nil
		if ev == EstadoPWPausada && v.OtherInfo != nil {
			switch toUpper(v.OtherInfo.Title) {
			case "FIRST_PAUSE":
				h := cuando.Add(3 * time.Hour)
				f.PausadaHasta = &h
			case "SECOND_PAUSE":
				h := cuando.Add(6 * time.Hour)
				f.PausadaHasta = &h
			}
		}
		f.Motivo, f.Rechazo = nil, nil
		if ev == EstadoPWRechazada {
			if v.Reason != nil {
				f.Motivo = motivoMeta(*v.Reason)
			}
			if v.RejectionInfo != nil {
				f.Rechazo, _ = json.Marshal(RechazoPWA{Motivo: recortar(v.RejectionInfo.Reason, 500), Recomendacion: recortar(v.RejectionInfo.Recommendation, 500)})
			}
		}
		if cat := normalizarCategoria(v.MessageTemplateCategory); cat != "" && cat != f.Categoria {
			f.CategoriaAnterior, f.CategoriaCambiadaEn, f.Categoria = &antes.Categoria, &ahora, cat
		}
	case "template_category_update":
		switch {
		case v.PreviousCategory != "" && normalizarCategoria(v.NewCategory) != "":
			if nueva := normalizarCategoria(v.NewCategory); nueva != f.Categoria {
				f.CategoriaAnterior, f.CategoriaCambiadaEn, f.Categoria = &antes.Categoria, &ahora, nueva
			}
			f.CategoriaProxima, f.CategoriaProximaEn = nil, nil
		case normalizarCategoria(v.CorrectCategory) != "":
			c := normalizarCategoria(v.CorrectCategory)
			f.CategoriaProxima = &c
			if v.CategoryUpdateTimestamp > 0 {
				e := time.Unix(v.CategoryUpdateTimestamp, 0).UTC()
				f.CategoriaProximaEn = &e
			}
		default:
			return nil
		}
	case "message_template_quality_update":
		switch c := toUpper(v.NewQualityScore); c {
		case "GREEN", "YELLOW", "RED", "UNKNOWN":
			f.Calidad = c
		default:
			return nil
		}
	}
	g, err := guardarPW(ctx, q, f)
	if err != nil {
		return err
	}
	t.FijarEntidad("plantilla_whatsapp", g.ID)
	if g.Estado != antes.Estado {
		if err := historialPW(ctx, q, g.ID, "estado", &antes.Estado, &g.Estado, map[string]any{"motivo": g.Motivo, "otro": otroInfoAcotado(v)}, "webhook", nil, ahora); err != nil {
			return err
		}
		if err := t.Emitir("plantilla_whatsapp.estado_cambiado", conPW(basePW(g), map[string]any{"de": antes.Estado, "a": g.Estado, "motivo": g.Motivo,
			"origen": SyncWebhook, "pausada_hasta": g.PausadaHasta})); err != nil {
			return err
		}
	}
	if g.Categoria != antes.Categoria {
		if err := emitirCategoriaPW(ctx, t, q, g, antes.Categoria, g.Categoria, SyncWebhook, false, ahora); err != nil {
			return err
		}
	}
	if g.CategoriaProxima != nil && derefTexto(antes.CategoriaProxima) != *g.CategoriaProxima {
		if err := emitirCategoriaPW(ctx, t, q, g, g.Categoria, *g.CategoriaProxima, SyncWebhook, true, ahora); err != nil {
			return err
		}
	}
	if g.Calidad != antes.Calidad {
		if err := historialPW(ctx, q, g.ID, "calidad", &antes.Calidad, &g.Calidad, nil, "webhook", nil, ahora); err != nil {
			return err
		}
		return t.Emitir("plantilla_whatsapp.calidad_cambiada", conPW(basePW(g), map[string]any{"de": antes.Calidad, "a": g.Calidad, "origen": SyncWebhook}))
	}
	return nil
}

// otroInfoAcotado: other_info de Meta recortado (título como literal; descripción corta).
func otroInfoAcotado(v valorPlantillaMeta) map[string]string {
	if v.OtherInfo == nil {
		return nil
	}
	return map[string]string{"titulo": literalAcotado(v.OtherInfo.Title), "descripcion": recortar(limpiarTextoMeta(v.OtherInfo.Description), 200)}
}

// Noche ---------------------------------------------------------------------------------------------

// HoraSincronizacionNocturna: entre las 2:00 y las 5:00 (Guatemala) se releen las cuentas que no se
// sincronizan hace más de 20 horas.
const HoraSincronizacionNocturna = 2

// DiasAvisosMetaPW: cuánto se guarda cada aviso de Meta aplicado (mensajeria.plantillas_whatsapp_avisos_meta).
const DiasAvisosMetaPW = 30

// programarSincronizaciones encola (únicos por cuenta) los jobs de la noche.
func (m *Modulo) programarSincronizaciones(ctx context.Context, bd nucleo.BD) error {
	ahora := m.ahora()
	if h := ahora.In(m.cfg.Zona).Hour(); h < HoraSincronizacionNocturna || h >= HoraSincronizacionNocturna+3 {
		return nil
	}
	antes := ahora.Add(-20 * time.Hour)
	return nucleo.EnTx(nucleo.ConBD(ctx, bd), func(ctx context.Context, tx pgx.Tx) error {
		// Los avisos de Meta ya aplicados se guardan 30 días (Meta no reenvía más allá).
		if _, err := mensajeriabd.New(tx).PurgarAvisosMetaPW(ctx, ahora.Add(-DiasAvisosMetaPW*24*time.Hour)); err != nil {
			return err
		}
		cuentas, err := mensajeriabd.New(tx).CuentasWAParaNoche(ctx, &antes)
		if err != nil {
			return err
		}
		for _, w := range cuentas {
			if err := m.encolar(ctx, tx, ArgsSincronizarPlantillas{WabaID: w, Origen: SyncNocturna}); err != nil {
				return err
			}
		}
		return nil
	})
}
