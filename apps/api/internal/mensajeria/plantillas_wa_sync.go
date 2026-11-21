package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
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
)

// EntradaSincronizarPWA es POST /plantillas-whatsapp/cuentas/{waba}/sincronizar.
type EntradaSincronizarPWA struct {
	WabaID string `json:"waba_id"`
}

// ResultadoSincronizacionPWA es lo que quedó.
type ResultadoSincronizacionPWA struct {
	WabaID         string                   `json:"wabaId"`
	SincronizadaEn time.Time                `json:"sincronizadaEn"`
	Resumen        ResumenSincronizacionPWA `json:"resumen"`
}

type lecturaPW struct {
	waba    string
	empresa *int64
	origen  string
	leidas  []PlantillaMetaLeida
	err     error
	cuenta  CuentaGraph
}

func (l lecturaPW) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"waba_id": l.waba, "origen": l.origen, "leidas": len(l.leidas), "error": l.err != nil})
}

// SincronizarPWA es la sincronización manual (dos casos con Meta en medio) y la del sistema.
type SincronizarPWA struct {
	m       *Modulo
	leer    *auditoria.Caso[EntradaSincronizarPWA, lecturaPW]
	aplicar *auditoria.Caso[lecturaPW, ResultadoSincronizacionPWA]
}

func nuevoSincronizarPWA(m *Modulo) *SincronizarPWA {
	s := &SincronizarPWA{m: m}
	s.leer = auditoria.Nuevo(auditoria.Caso[EntradaSincronizarPWA, lecturaPW]{
		Accion: "plantillas_whatsapp.sincronizar", Permiso: nucleo.PermPlantillasWhatsAppSincronizar, AuditarExito: func(lecturaPW) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSincronizarPWA) (lecturaPW, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return lecturaPW{}, err
			}
			q := mensajeriabd.New(t)
			canales, err := canalesDeCuentaEmpresa(ctx, q, contexto.EmpresaDe(ctx), in.WabaID)
			if err != nil {
				return lecturaPW{}, err
			}
			c, err := m.cuentaGraph(ctx, q, contexto.EmpresaDe(ctx), in.WabaID)
			if err != nil {
				return lecturaPW{}, err
			}
			return lecturaPW{waba: in.WabaID, empresa: canales[0].EmpresaID, origen: SyncManual, cuenta: c}, nil
		},
	})
	// aplicar corre para personas (manual) y para el sistema (noche y webhook): Autenticado; el
	// permiso de la persona ya lo revisó leer.
	s.aplicar = auditoria.Nuevo(auditoria.Caso[lecturaPW, ResultadoSincronizacionPWA]{
		Accion: "plantillas_whatsapp.sincronizar", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in lecturaPW) (ResultadoSincronizacionPWA, error) {
			q := mensajeriabd.New(t)
			t.FijarEntidad("cuenta_whatsapp", in.waba)
			fijarEmpresaCanal(t, in.empresa)
			ahora := m.ahora()
			if in.err != nil {
				p := problemaMetaPW(in.err)
				codigo := p.Codigo()
				t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
					return mensajeriabd.New(tx).GuardarCuentaWA(ctx, mensajeriabd.GuardarCuentaWAParams{WabaID: in.waba, EmpresaID: in.empresa,
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

// Ejecutar es la sincronización manual.
func (s *SincronizarPWA) Ejecutar(ctx context.Context, in EntradaSincronizarPWA) (ResultadoSincronizacionPWA, error) {
	l, err := s.leer.Ejecutar(ctx, in)
	if err != nil {
		return ResultadoSincronizacionPWA{}, err
	}
	l.leidas, l.err = s.m.leerCuentaMeta(ctx, l.cuenta)
	return s.aplicar.Ejecutar(ctx, l)
}

// leerCuentaMeta comprueba el permiso del token y lee todas las plantillas de la cuenta.
func (m *Modulo) leerCuentaMeta(ctx context.Context, c CuentaGraph) ([]PlantillaMetaLeida, error) {
	g := m.graphPlantillas()
	if tiene, leido := g.Permiso(ctx, c); leido && !tiene {
		return nil, &ErrMetaPlantilla{Status: 403, Codigo: 200}
	}
	return g.Listar(ctx, c)
}

// SincronizarCuentaSistema sincroniza una cuenta desde un job (noche o webhook).
func (m *Modulo) SincronizarCuentaSistema(ctx context.Context, bd nucleo.BD, waba, origen string) error {
	var l lecturaPW
	err := nucleo.EnTx(nucleo.ConBD(ctx, bd), func(ctx context.Context, tx pgx.Tx) error {
		q := mensajeriabd.New(tx)
		canales, err := q.CanalesDeCuentaWA(ctx, waba)
		if err != nil {
			return err
		}
		if len(canales) == 0 {
			return pgx.ErrNoRows
		}
		c, err := m.cuentaGraph(ctx, q, -1, waba)
		if err != nil {
			return err
		}
		l = lecturaPW{waba: waba, empresa: canales[0].EmpresaID, origen: origen, cuenta: c}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // ya no hay números activos en la cuenta
	}
	var p *nucleo.Problema
	if errors.As(err, &p) {
		slog.WarnContext(ctx, "plantillas: la cuenta no tiene token para sincronizar", "waba_id", waba, "codigo", p.Codigo())
		return nil
	}
	if err != nil {
		return err
	}
	l.leidas, l.err = m.leerCuentaMeta(ctx, l.cuenta)
	_, err = m.PlantillasWA.Sincronizar.aplicar.Ejecutar(ctxSistema(ctx, bd), l)
	if errors.As(err, &p) {
		// Meta no respondió o el token no sirve: queda anotado en la cuenta; la próxima noche se reintenta.
		slog.WarnContext(ctx, "plantillas: sincronización con Meta fallida", "waba_id", waba, "codigo", p.Codigo(), "origen", origen)
		return nil
	}
	return err
}

func cambioPW(f mensajeriabd.MensajeriaPlantillaWA) CambioCuentaPWA {
	return CambioCuentaPWA{ID: f.ID, Nombre: f.Nombre, Etiqueta: f.Etiqueta, Estado: f.Estado, Codigo: f.Codigo, Usos: lista(f.Usos)}
}

// aplicarLectura compara lo que dijo Meta con lo que tiene Iris.
func (m *Modulo) aplicarLectura(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, in lecturaPW, ahora time.Time) (ResumenSincronizacionPWA, error) {
	if err := q.BloquearCuentaPlantillas(ctx, in.waba); err != nil {
		return ResumenSincronizacionPWA{}, err
	}
	filas, err := q.PlantillasWADeCuenta(ctx, in.waba)
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
		if vistas[f.ID] || f.Estado == EstadoPWBorrada || f.Estado == EstadoPWBorrador {
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
	if err := q.GuardarCuentaWA(ctx, mensajeriabd.GuardarCuentaWAParams{WabaID: in.waba, EmpresaID: in.empresa, SincronizadaEn: &ahora, Resumen: resumen}); err != nil {
		return r, err
	}
	return r, t.EmitirEnEmpresa("plantilla_whatsapp.sincronizada", in.empresa, map[string]any{"waba_id": in.waba, "empresa_id": in.empresa,
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
	f, err := q.InsertarPlantillaWA(ctx, mensajeriabd.InsertarPlantillaWAParams{EmpresaID: in.empresa, WabaID: in.waba, MetaID: &metaID, Nombre: p.Name,
		Idioma: p.Language, Etiqueta: etiqueta, Categoria: categoria, Estado: normalizarEstadoMeta(p.Status, EstadoPWPendiente), Motivo: motivoMeta(p.RejectedReason),
		Calidad: p.Calidad(), Formato: formato, Componentes: comp, ComponentesMeta: p.Components, Parametros: params, Usos: []string{}, Origen: OrigenPWMeta,
		SincronizadaEn: &ahora})
	if err != nil {
		return f, err
	}
	if err := historialPW(ctx, q, f.ID, "creada", nil, &f.Estado, map[string]any{"origen": "meta"}, "sincronizacion", nil, ahora); err != nil {
		return f, err
	}
	return f, t.EmitirEnEmpresa("plantilla_whatsapp.creada", in.empresa, conPW(basePW(f), map[string]any{"nombre": f.Nombre, "idioma": f.Idioma,
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

// InsertOpts: cola de mensajería, uno por cuenta cada 10 minutos.
func (ArgsSincronizarPlantillas) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 10 * time.Minute}}
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
				if err := m.webhookPlantilla(ctx, t, q, canal, e.ID, c.Field, c.Value, ahora); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// webhookPlantilla aplica un cambio de plantilla que avisó Meta. waba: entry[].id.
func (m *Modulo) webhookPlantilla(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, waba, campo string,
	crudo json.RawMessage, ahora time.Time) error {
	// Solo cuenta lo de la cuenta del número que recibió el webhook (la firma es la de su app).
	if canal.WabaID == "" || waba != canal.WabaID {
		t.Anotar(map[string]any{"plantilla_de_otra_cuenta": true})
		return nil
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
	f, err := q.PlantillaWAPorMeta(ctx, mensajeriabd.PlantillaWAPorMetaParams{WabaID: waba, MetaID: &id, Nombre: v.MessageTemplateName, Idioma: v.MessageTemplateLanguage})
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
		f.Estado, f.EstadoEn = ev, ahora
		f.PausadaHasta = nil
		if ev == EstadoPWPausada && v.OtherInfo != nil {
			switch toUpper(v.OtherInfo.Title) {
			case "FIRST_PAUSE":
				h := ahora.Add(3 * time.Hour)
				f.PausadaHasta = &h
			case "SECOND_PAUSE":
				h := ahora.Add(6 * time.Hour)
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
		if err := historialPW(ctx, q, g.ID, "estado", &antes.Estado, &g.Estado, map[string]any{"motivo": g.Motivo, "otro": v.OtherInfo}, "webhook", nil, ahora); err != nil {
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

// Noche ---------------------------------------------------------------------------------------------

// HoraSincronizacionNocturna: entre las 2:00 y las 5:00 (Guatemala) se releen las cuentas que no se
// sincronizan hace más de 20 horas.
const HoraSincronizacionNocturna = 2

// programarSincronizaciones encola (únicos por cuenta) los jobs de la noche.
func (m *Modulo) programarSincronizaciones(ctx context.Context, bd nucleo.BD) error {
	ahora := m.ahora()
	if h := ahora.In(m.cfg.Zona).Hour(); h < HoraSincronizacionNocturna || h >= HoraSincronizacionNocturna+3 {
		return nil
	}
	antes := ahora.Add(-20 * time.Hour)
	return nucleo.EnTx(nucleo.ConBD(ctx, bd), func(ctx context.Context, tx pgx.Tx) error {
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
