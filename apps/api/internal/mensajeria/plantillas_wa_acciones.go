package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Acciones que hablan con Meta: enviar a revisión, editar en Meta, borrar y reanudar. Cada una son
// dos casos de uso con la llamada a Meta en medio (fuera de la transacción): el primero revisa
// permisos y reglas y prepara la llamada; el segundo guarda lo que respondió Meta, audita y emite.

// EntidadAdjuntosPlantillaWA: el archivo de ejemplo del encabezado (entidad_id = la WABA).
const EntidadAdjuntosPlantillaWA = "plantilla_whatsapp"

// MaxArchivoEjemploPW: tope del archivo de ejemplo (Meta admite hasta 16 MB de video).
const MaxArchivoEjemploPW = 16 << 20

// EntradaVersionPWA es una acción sobre una plantilla con su versión.
type EntradaVersionPWA struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

// EntradaEditarMetaPWA corrige una plantilla que ya está en Meta (aprobada, rechazada o pausada).
type EntradaEditarMetaPWA struct {
	ID         int64         `json:"-"`
	Categoria  string        `json:"categoria,omitempty" enum:"UTILITY,MARKETING,AUTHENTICATION" doc:"Solo si no está aprobada (Meta no deja cambiarla)"`
	Definicion DefinicionPWA `json:"definicion"`
	Version    int32         `json:"version" minimum:"1"`
}

type archivoPW struct {
	nombre, mime string
	datos        []byte
}

// llamadaPW es lo que hay que pedirle a Meta (nunca se audita: lleva el token).
type llamadaPW struct {
	id         int64
	empresa    int64
	tipo       string // enviar, editar, borrar, reanudar
	cuenta     CuentaGraph
	nombre     string
	idioma     string
	metaID     string
	categoria  string
	formato    string
	cambiaCat  bool
	definicion DefinicionPWA
	archivo    *archivoPW
	local      bool
}

// MarshalJSON: lo auditado es la plantilla y la acción, nunca el token ni el archivo.
func (l llamadaPW) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"plantilla_id": l.id, "tipo": l.tipo})
}

type resultadoPW struct {
	l    llamadaPW
	resp RespuestaCrearMeta
	err  error
}

func (r resultadoPW) MarshalJSON() ([]byte, error) {
	e := map[string]any{"plantilla_id": r.l.id, "tipo": r.l.tipo}
	var em *ErrMetaPlantilla
	if errors.As(r.err, &em) {
		e["meta"] = map[string]any{"status": em.Status, "codigo": em.Codigo, "subcodigo": em.Subcodigo, "sin_respuesta": em.SinRespuesta}
	}
	return json.Marshal(e)
}

// AccionMetaPWA es una acción de dos pasos con la llamada a Meta en medio.
type AccionMetaPWA[In any] struct {
	m       *Modulo
	leer    *auditoria.Caso[In, llamadaPW]
	guardar *auditoria.Caso[resultadoPW, PlantillaWA]
}

// Ejecutar corre la acción completa.
func (a *AccionMetaPWA[In]) Ejecutar(ctx context.Context, in In) (PlantillaWA, error) {
	l, err := a.leer.Ejecutar(ctx, in)
	if err != nil {
		return PlantillaWA{}, err
	}
	r := resultadoPW{l: l}
	if !l.local {
		r.resp, r.err = a.m.llamarMetaPW(ctx, l)
	}
	return a.guardar.Ejecutar(ctx, r)
}

type aplicarPW func(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, r resultadoPW,
	ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, error)

func nuevaAccionMetaPW[In any](m *Modulo, accion string, permiso nucleo.Permiso,
	preparar func(ctx context.Context, q *mensajeriabd.Queries, t *auditoria.Tx, in In) (llamadaPW, error), aplicar aplicarPW) *AccionMetaPWA[In] {
	a := &AccionMetaPWA[In]{m: m}
	a.leer = auditoria.Nuevo(auditoria.Caso[In, llamadaPW]{
		Accion: accion, Permiso: permiso, AuditarExito: func(llamadaPW) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in In) (llamadaPW, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return llamadaPW{}, err
			}
			return preparar(ctx, mensajeriabd.New(t), t, in)
		},
	})
	a.guardar = auditoria.Nuevo(auditoria.Caso[resultadoPW, PlantillaWA]{
		Accion: accion, Permiso: permiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, r resultadoPW) (PlantillaWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return PlantillaWA{}, err
			}
			q := mensajeriabd.New(t)
			antes, err := m.plantillaConCandado(ctx, q, r.l.id)
			if err != nil {
				return PlantillaWA{}, err
			}
			t.FijarEntidad("plantilla_whatsapp", antes.ID)
			t.FijarEmpresa(contexto.EmpresaDe(ctx))
			ahora := m.ahora()
			if r.err != nil {
				p := problemaMetaPW(r.err)
				if codigo := errorDeCuentaPW(r.err); codigo != "" {
					t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
						return mensajeriabd.New(tx).EstadoCuentaWA(ctx, mensajeriabd.EstadoCuentaWAParams{WabaID: antes.WabaID, Empresa: derefEmpresa(antes.EmpresaID),
							Error: &codigo, ErrorEn: &ahora})
					})
				}
				if p.Codigo() == "nombre_en_uso" {
					if s, err := nombreLibre(ctx, q, derefEmpresa(antes.EmpresaID), antes.WabaID, antes.Nombre, ahora); err == nil && s != antes.Nombre {
						p.Sugerencia = &s
					}
				}
				return PlantillaWA{}, p
			}
			f, err := aplicar(ctx, t, q, antes, r, ahora)
			if err != nil {
				return PlantillaWA{}, err
			}
			t.RegistrarCambios("plantilla_whatsapp", f.ID, auditablePW(antes), auditablePW(f))
			if !r.l.local {
				// Meta respondió: la cuenta deja de mostrar el último error.
				if err := q.EstadoCuentaWA(ctx, mensajeriabd.EstadoCuentaWAParams{WabaID: f.WabaID, Empresa: derefEmpresa(f.EmpresaID)}); err != nil {
					return PlantillaWA{}, err
				}
			}
			return aPlantillaWA(f, ahora), nil
		},
	})
	return a
}

// Errores de Meta ----------------------------------------------------------------------------------

var codigosLimiteMeta = []int{4, 17, 32, 613, 80007, 80008, 130429}

// errorDeCuentaPW: el error es de la cuenta (no de la plantilla) y se muestra en la cabecera.
func errorDeCuentaPW(err error) string {
	switch p := problemaMetaPW(err); p.Codigo() {
	case "meta_sin_permiso", "meta_token_invalido", "meta_no_responde":
		return p.Codigo()
	}
	return ""
}

// problemaMetaPW traduce un error de Meta a uno en llano.
func problemaMetaPW(err error) *nucleo.Problema {
	var p *nucleo.Problema
	if errors.As(err, &p) {
		return p
	}
	var e *ErrMetaPlantilla
	if !errors.As(err, &e) || e.SinRespuesta {
		return nucleo.NuevoProblema(http.StatusBadGateway, "meta_no_responde", "No pudimos hablar con Meta",
			"Meta no contestó a tiempo. No cambiamos nada: las plantillas siguen como estaban y los avisos siguen saliendo con las aprobadas.")
	}
	switch {
	case e.Codigo == 190:
		return nucleo.NuevoProblema(http.StatusFailedDependency, "meta_token_invalido", "Meta no acepta el token",
			"El token del número ya no sirve (venció o lo revocaron). Pega uno nuevo en «Números de WhatsApp».")
	case e.Codigo == 200 || e.Codigo == 10 || e.Codigo == 3 || (e.Status == http.StatusForbidden && e.Codigo == 0):
		return nucleo.NuevoProblema(http.StatusFailedDependency, "meta_sin_permiso", "La conexión con Meta no deja ver ni crear plantillas",
			"El token sirve para mandar mensajes, pero no para administrar plantillas. Quien administra Meta debe darle el permiso "+
				"«whatsapp_business_management» (administrar la cuenta de WhatsApp) y pegar el token nuevo en Iris.")
	case slices.Contains(codigosLimiteMeta, e.Codigo):
		return nucleo.NuevoProblema(http.StatusTooManyRequests, "meta_limite", "Meta pide esperar",
			"Meta limita cuántas plantillas se crean o cambian por hora. Espera un rato y vuelve a intentar.")
	case e.Codigo == 100 && e.Subcodigo == 2388024:
		return nucleo.NuevoProblema(http.StatusConflict, "nombre_en_uso", "Nombre en uso",
			"Meta ya tiene una plantilla con ese nombre en ese idioma. Tu plantilla quedó guardada; no se perdió nada.",
			nucleo.ErrorCampo{Field: "nombre", Message: "Ese nombre ya está en uso en este idioma", Rule: "nombre_en_uso"})
	}
	detalle := "Meta no aceptó la plantilla. Revisa el texto y los ejemplos, y vuelve a intentar."
	if e.Mensaje != "" {
		detalle = "Meta no aceptó la plantilla. Meta dijo: «" + e.Mensaje + "»"
	}
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "meta_rechazo", "Meta no la aceptó", detalle)
}

func problemaReglas(r ResultadoReglasPWA) *nucleo.Problema {
	errs := []nucleo.ErrorCampo{}
	for _, x := range reglasFallidas(r.Reglas) {
		errs = append(errs, nucleo.ErrorCampo{Field: derefTexto(x.Campo), Message: x.Mensaje, Rule: x.Regla})
	}
	n := len(errs)
	detalle := "Corrige 1 cosa para poder enviarla."
	if n != 1 {
		detalle = fmt.Sprintf("Corrige %d cosas para poder enviarla.", n)
	}
	return reglaPW("reglas_meta", detalle, errs...)
}

// Meta ----------------------------------------------------------------------------------------------

// cuentaGraph: el token de un número de la empresa dueña de la cuenta.
func (m *Modulo) cuentaGraph(ctx context.Context, q *mensajeriabd.Queries, empresa int64, waba string) (CuentaGraph, error) {
	canales, err := canalesDeCuentaEmpresa(ctx, q, empresa, waba)
	if err != nil {
		return CuentaGraph{}, err
	}
	for _, c := range canales {
		cr, err := m.credencialesDe(c)
		if err != nil || cr.Token == "" {
			continue
		}
		cg := CuentaGraph{WabaID: waba, Token: cr.Token}
		if con, err := m.conexion(ctx, q); err == nil {
			cg.AppID = con.appID
		}
		return cg, nil
	}
	return CuentaGraph{}, reglaPW("cuenta_sin_token", "Ningún número de esta cuenta tiene el token guardado: pégalo en «Números de WhatsApp»")
}

func (m *Modulo) llamarMetaPW(ctx context.Context, l llamadaPW) (RespuestaCrearMeta, error) {
	g := m.graphPlantillas()
	switch l.tipo {
	case "enviar", "editar":
		if tiene, leido := g.Permiso(ctx, l.cuenta); leido && !tiene {
			return RespuestaCrearMeta{}, &ErrMetaPlantilla{Status: http.StatusForbidden, Codigo: 200}
		}
		handle := ""
		if l.archivo != nil {
			h, err := g.SubirArchivo(ctx, l.cuenta, l.archivo.nombre, l.archivo.mime, l.archivo.datos)
			if err != nil {
				return RespuestaCrearMeta{}, err
			}
			handle = h
		}
		if l.tipo == "enviar" {
			return g.Crear(ctx, l.cuenta, cuerpoCrearMeta(l.nombre, l.idioma, l.categoria, l.formato, l.definicion, handle))
		}
		cuerpo := map[string]any{"components": componentesAMeta(l.categoria, l.formato, l.definicion, handle)}
		if l.cambiaCat {
			cuerpo["category"] = l.categoria
		}
		return RespuestaCrearMeta{}, g.Editar(ctx, l.cuenta, l.metaID, cuerpo)
	case "borrar":
		return RespuestaCrearMeta{}, g.Borrar(ctx, l.cuenta, l.nombre, l.metaID)
	case "reanudar":
		return RespuestaCrearMeta{}, g.Reanudar(ctx, l.cuenta, l.metaID)
	}
	return RespuestaCrearMeta{}, fmt.Errorf("plantillas: acción desconocida %q", l.tipo)
}

// archivoEjemplo lee el archivo de ejemplo del encabezado (imagen, video o documento).
func (m *Modulo) archivoEjemplo(ctx context.Context, t *auditoria.Tx, waba string, d DefinicionPWA, cuenta CuentaGraph) (*archivoPW, error) {
	if d.Encabezado == nil || d.Encabezado.AdjuntoID == nil || !slices.Contains([]string{"imagen", "video", "documento"}, d.Encabezado.Tipo) {
		return nil, nil
	}
	if !reDigitos.MatchString(cuenta.AppID) {
		return nil, reglaPW("sin_app_meta", "Para un encabezado con archivo, Iris necesita el app ID de tu app de Meta: complétalo en «Conexión con Meta»")
	}
	datos, a, err := adjuntos.Contenido(ctx, m.cfg.Almacen, t, *d.Encabezado.AdjuntoID, EntidadAdjuntosPlantillaWA, waba, MaxArchivoEjemploPW)
	if err != nil {
		return nil, campoConv("definicion.encabezado.adjuntoId", "El archivo de ejemplo no está disponible: súbelo de nuevo", "adjunto")
	}
	return &archivoPW{nombre: "ejemplo-" + uuid.NewString(), mime: a.Mime, datos: datos}, nil
}

// registrarAdjuntosPlantillaWA: el archivo de ejemplo de un encabezado (entidad_id = la WABA). Subir
// y ver: crear o editar plantillas y una cuenta con un número activo de la empresa. Si no, 404.
func registrarAdjuntosPlantillaWA() {
	adjuntos.RegistrarEntidadAdjuntable(EntidadAdjuntosPlantillaWA, func(ctx context.Context, db adjuntos.DBTX, id string, _ adjuntos.Uso) (int64, error) {
		p := contexto.PermisosDe(ctx)
		if !reDigitos.MatchString(id) || soloPersonasPW(ctx) != nil ||
			(!p.Tiene(string(nucleo.PermPlantillasWhatsAppCrear)) && !p.Tiene(string(nucleo.PermPlantillasWhatsAppEditar))) {
			return 0, cuentaWANoEncontrada()
		}
		empresa := contexto.EmpresaDe(ctx)
		if _, err := canalesDeCuentaEmpresa(ctx, mensajeriabd.New(db), empresa, id); err != nil {
			return 0, err
		}
		return empresa, nil
	})
}

// Enviar a Meta ------------------------------------------------------------------------------------

func nuevoEnviarPWA(m *Modulo) *AccionMetaPWA[EntradaVersionPWA] {
	return nuevaAccionMetaPW(m, "plantillas_whatsapp.enviar", nucleo.PermPlantillasWhatsAppCrear,
		func(ctx context.Context, q *mensajeriabd.Queries, t *auditoria.Tx, in EntradaVersionPWA) (llamadaPW, error) {
			f, err := m.plantillaDeEmpresa(ctx, q, in.ID, false)
			if err != nil {
				return llamadaPW{}, err
			}
			if f.Version != in.Version {
				return llamadaPW{}, nucleo.ConflictoVersion("plantilla_whatsapp", in.ID)
			}
			if f.Estado != EstadoPWBorrador {
				return llamadaPW{}, nucleo.Conflicto("ya_enviada", "La plantilla ya está en Meta")
			}
			empresa := contexto.EmpresaDe(ctx)
			d := definicionDe(f.Componentes)
			res, err := m.reglasCompletas(ctx, q, empresa, f.WabaID, f.ID, EntradaReglasPW{Nombre: f.Nombre, Idioma: f.Idioma, Categoria: f.Categoria, Definicion: d})
			if err != nil {
				return llamadaPW{}, err
			}
			if !res.Ok {
				return llamadaPW{}, problemaReglas(res)
			}
			cuenta, err := m.cuentaGraph(ctx, q, empresa, f.WabaID)
			if err != nil {
				return llamadaPW{}, err
			}
			archivo, err := m.archivoEjemplo(ctx, t, f.WabaID, d, cuenta)
			if err != nil {
				return llamadaPW{}, err
			}
			return llamadaPW{id: f.ID, empresa: empresa, tipo: "enviar", cuenta: cuenta, nombre: f.Nombre, idioma: f.Idioma, categoria: f.Categoria,
				formato: f.Formato, definicion: d, archivo: archivo}, nil
		},
		func(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, r resultadoPW, ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, error) {
			if f.Estado != EstadoPWBorrador {
				// Otra petición la envió mientras tanto: queda lo que hay.
				return f, nil
			}
			antes := f
			estado := normalizarEstadoMeta(r.resp.Status, EstadoPWPendiente)
			f.MetaID, f.Estado, f.EstadoEn, f.EnviadaEn = &r.resp.ID, estado, ahora, &ahora
			cat := normalizarCategoria(r.resp.Category)
			if cat != "" && cat != f.Categoria {
				// Meta la aprobó en otra categoría al crearla.
				f.CategoriaAnterior, f.CategoriaCambiadaEn, f.Categoria = &antes.Categoria, &ahora, cat
			}
			f, err := guardarPW(ctx, q, f)
			if err != nil {
				return f, err
			}
			if err := historialPW(ctx, q, f.ID, "enviada", ptr(EstadoPWBorrador), &f.Estado, map[string]any{"meta_id": f.MetaID}, "usuario", actorPW(ctx), ahora); err != nil {
				return f, err
			}
			if err := t.Emitir("plantilla_whatsapp.enviada", conPW(basePW(f), map[string]any{"meta_id": f.MetaID, "tipo": "creacion", "estado": f.Estado})); err != nil {
				return f, err
			}
			if f.Categoria != antes.Categoria {
				return f, emitirCategoriaPW(ctx, t, q, f, antes.Categoria, f.Categoria, "iris", false, ahora)
			}
			return f, nil
		})
}

// Editar en Meta -----------------------------------------------------------------------------------

func nuevoEditarMetaPWA(m *Modulo) *AccionMetaPWA[EntradaEditarMetaPWA] {
	return nuevaAccionMetaPW(m, "plantillas_whatsapp.editar", nucleo.PermPlantillasWhatsAppEditar,
		func(ctx context.Context, q *mensajeriabd.Queries, t *auditoria.Tx, in EntradaEditarMetaPWA) (llamadaPW, error) {
			f, err := m.plantillaDeEmpresa(ctx, q, in.ID, false)
			if err != nil {
				return llamadaPW{}, err
			}
			if f.Version != in.Version {
				return llamadaPW{}, nucleo.ConflictoVersion("plantilla_whatsapp", in.ID)
			}
			if f.MetaID == nil || !slices.Contains([]string{EstadoPWAprobada, EstadoPWRechazada, EstadoPWPausada}, f.Estado) {
				return llamadaPW{}, nucleo.Conflicto("no_editable", "Meta solo deja editar plantillas aprobadas, rechazadas o pausadas")
			}
			categoria := f.Categoria
			if in.Categoria != "" && in.Categoria != f.Categoria {
				if f.Estado == EstadoPWAprobada {
					return llamadaPW{}, reglaPW("categoria_aprobada", "Meta no deja cambiar la categoría de una plantilla aprobada")
				}
				categoria = in.Categoria
			}
			if f.Estado == EstadoPWAprobada {
				if e := edicionesDe(f.Ediciones, m.ahora()); e.PuedeEditarEn != nil {
					p := reglaPW("limite_ediciones", fmt.Sprintf("Meta deja editar una plantilla aprobada 1 vez al día y 10 veces en 30 días. Podrás editarla de nuevo el %s; mientras tanto sigue funcionando como está.",
						e.PuedeEditarEn.In(zonaPlantillas).Format("02/01/2006 15:04")))
					p.DisponibleEn = e.PuedeEditarEn
					return llamadaPW{}, p
				}
			}
			empresa := contexto.EmpresaDe(ctx)
			d := normalizarDefinicion(in.Definicion)
			res, err := m.reglasCompletas(ctx, q, empresa, f.WabaID, f.ID, EntradaReglasPW{Nombre: f.Nombre, Idioma: f.Idioma, Categoria: categoria, Definicion: d})
			if err != nil {
				return llamadaPW{}, err
			}
			if !res.Ok {
				return llamadaPW{}, problemaReglas(res)
			}
			formato := formatoDeDefinicion(d)
			for _, codigo := range f.Codigos {
				if err := revisarCodigo(codigo, categoria, d, formato, parametrosDe(d)); err != nil {
					return llamadaPW{}, err
				}
			}
			cuenta, err := m.cuentaGraph(ctx, q, empresa, f.WabaID)
			if err != nil {
				return llamadaPW{}, err
			}
			archivo, err := m.archivoEjemplo(ctx, t, f.WabaID, d, cuenta)
			if err != nil {
				return llamadaPW{}, err
			}
			return llamadaPW{id: f.ID, empresa: empresa, tipo: "editar", cuenta: cuenta, nombre: f.Nombre, idioma: f.Idioma, metaID: *f.MetaID,
				categoria: categoria, cambiaCat: categoria != f.Categoria, formato: formato, definicion: d, archivo: archivo}, nil
		},
		func(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, r resultadoPW, ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, error) {
			antes := f
			f.Componentes, _ = json.Marshal(r.l.definicion)
			f.Parametros, _ = json.Marshal(parametrosDe(r.l.definicion))
			f.Formato, f.Categoria = r.l.formato, r.l.categoria
			if antes.Estado == EstadoPWAprobada {
				f.Ediciones = append(slices.Clone(f.Ediciones), ahora)
			}
			// Meta la vuelve a revisar.
			f.Estado, f.EstadoEn, f.Motivo, f.Rechazo, f.PausadaHasta, f.EnviadaEn = EstadoPWPendiente, ahora, nil, nil, nil, &ahora
			f, err := guardarPW(ctx, q, f)
			if err != nil {
				return f, err
			}
			if err := historialPW(ctx, q, f.ID, "editada", &antes.Estado, &f.Estado, map[string]any{"en_meta": true}, "usuario", actorPW(ctx), ahora); err != nil {
				return f, err
			}
			if err := t.Emitir("plantilla_whatsapp.enviada", conPW(basePW(f), map[string]any{"meta_id": f.MetaID, "tipo": "edicion", "estado": f.Estado})); err != nil {
				return f, err
			}
			return f, t.Emitir("plantilla_whatsapp.estado_cambiado", conPW(basePW(f), map[string]any{"de": antes.Estado, "a": f.Estado, "motivo": "editada", "origen": "iris"}))
		})
}

// Borrar -------------------------------------------------------------------------------------------

func nuevoBorrarPWA(m *Modulo) *AccionMetaPWA[EntradaVersionPWA] {
	return nuevaAccionMetaPW(m, "plantillas_whatsapp.borrar", nucleo.PermPlantillasWhatsAppBorrar,
		func(ctx context.Context, q *mensajeriabd.Queries, _ *auditoria.Tx, in EntradaVersionPWA) (llamadaPW, error) {
			f, err := m.plantillaDeEmpresa(ctx, q, in.ID, false)
			if err != nil {
				return llamadaPW{}, err
			}
			if f.Version != in.Version {
				return llamadaPW{}, nucleo.ConflictoVersion("plantilla_whatsapp", in.ID)
			}
			switch {
			case f.Estado == EstadoPWBorrada:
				return llamadaPW{}, nucleo.Conflicto("ya_borrada", "La plantilla ya está borrada")
			case f.Estado == EstadoPWInhabil:
				return llamadaPW{}, reglaPW("deshabilitada_no_se_borra", "Meta no deja borrar una plantilla deshabilitada")
			case f.Estado == EstadoPWBorrador:
				return llamadaPW{id: f.ID, tipo: "borrar", local: true}, nil
			case f.MetaID == nil:
				// Sin su id de Meta, borrar por nombre se llevaría todos los idiomas.
				return llamadaPW{}, nucleo.Conflicto("sin_sincronizar", "Sincroniza con Meta antes de borrarla")
			}
			empresa := contexto.EmpresaDe(ctx)
			cuenta, err := m.cuentaGraph(ctx, q, empresa, f.WabaID)
			if err != nil {
				return llamadaPW{}, err
			}
			return llamadaPW{id: f.ID, empresa: empresa, tipo: "borrar", cuenta: cuenta, nombre: f.Nombre, metaID: *f.MetaID}, nil
		},
		func(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, r resultadoPW, ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, error) {
			if f.Estado == EstadoPWBorrada {
				return f, nil
			}
			return borrarPW(ctx, t, q, f, "iris", "usuario", ahora)
		})
}

// borrarPW deja la plantilla borrada (nunca se elimina la fila). Borrada desde Iris suelta el código y
// los usos; borrada en Meta los conserva para que el panel pida elegir otra.
func borrarPW(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, por, origen string, ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, error) {
	antes := f
	enUso := len(f.Codigos) > 0 || len(f.Usos) > 0
	f.Estado, f.EstadoEn, f.BorradaEn, f.BorradaPor = EstadoPWBorrada, ahora, &ahora, &por
	if !slices.Contains([]string{EstadoPWBorrador, EstadoPWPendiente, EstadoPWRechazada}, antes.Estado) && antes.MetaID != nil {
		libre := ahora.Add(30 * 24 * time.Hour)
		f.NombreLibreEn = &libre
	}
	if por == "iris" {
		f.Codigos, f.Usos = []string{}, []string{}
	}
	f, err := guardarPW(ctx, q, f)
	if err != nil {
		return f, err
	}
	if err := historialPW(ctx, q, f.ID, "borrada", &antes.Estado, &f.Estado, map[string]any{"por": por, "estaba_en_uso": enUso, "codigos": lista(antes.Codigos)},
		origen, actorPW(ctx), ahora); err != nil {
		return f, err
	}
	return f, t.Emitir("plantilla_whatsapp.borrada", conPW(basePW(f), map[string]any{"nombre": f.Nombre, "idioma": f.Idioma, "origen": por,
		"estaba_en_uso": enUso, "codigos": lista(antes.Codigos), "nombre_libre_en": f.NombreLibreEn}))
}

// Reanudar -----------------------------------------------------------------------------------------

func nuevoReanudarPWA(m *Modulo) *AccionMetaPWA[EntradaVersionPWA] {
	return nuevaAccionMetaPW(m, "plantillas_whatsapp.reanudar", nucleo.PermPlantillasWhatsAppEditar,
		func(ctx context.Context, q *mensajeriabd.Queries, _ *auditoria.Tx, in EntradaVersionPWA) (llamadaPW, error) {
			f, err := m.plantillaDeEmpresa(ctx, q, in.ID, false)
			if err != nil {
				return llamadaPW{}, err
			}
			if f.Version != in.Version {
				return llamadaPW{}, nucleo.ConflictoVersion("plantilla_whatsapp", in.ID)
			}
			if f.Estado != EstadoPWPausada || f.MetaID == nil {
				return llamadaPW{}, nucleo.Conflicto("no_pausada", "La plantilla no está pausada")
			}
			empresa := contexto.EmpresaDe(ctx)
			cuenta, err := m.cuentaGraph(ctx, q, empresa, f.WabaID)
			if err != nil {
				return llamadaPW{}, err
			}
			return llamadaPW{id: f.ID, empresa: empresa, tipo: "reanudar", cuenta: cuenta, metaID: *f.MetaID}, nil
		},
		func(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, _ resultadoPW, ahora time.Time) (mensajeriabd.MensajeriaPlantillaWA, error) {
			if f.Estado != EstadoPWPausada {
				return f, nil
			}
			f.Estado, f.EstadoEn, f.PausadaHasta = EstadoPWAprobada, ahora, nil
			f, err := guardarPW(ctx, q, f)
			if err != nil {
				return f, err
			}
			if err := historialPW(ctx, q, f.ID, "reanudada", ptr(EstadoPWPausada), &f.Estado, nil, "usuario", actorPW(ctx), ahora); err != nil {
				return f, err
			}
			return f, t.Emitir("plantilla_whatsapp.estado_cambiado", conPW(basePW(f), map[string]any{"de": EstadoPWPausada, "a": f.Estado,
				"motivo": "reanudada", "origen": "iris"}))
		})
}

// Normalización ------------------------------------------------------------------------------------

func normalizarEstadoMeta(s, porDefecto string) string {
	s = toUpper(s)
	if slices.Contains(EstadosMetaPW, s) {
		return s
	}
	return porDefecto
}

func normalizarCategoria(s string) string {
	s = toUpper(s)
	if slices.Contains([]string{CategoriaUtility, CategoriaMarketing, CategoriaAutenticacion}, s) {
		return s
	}
	return ""
}

func toUpper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// emitirCategoriaPW registra y emite el cambio de categoría (o el aviso de que viene uno).
func emitirCategoriaPW(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA, de, a, origen string,
	pendiente bool, ahora time.Time) error {
	hist := map[string]string{"iris": "usuario", "webhook": "webhook", "sincronizacion": "sincronizacion"}[origen]
	if hist == "" {
		hist = "sistema"
	}
	if err := historialPW(ctx, q, f.ID, "categoria", &de, &a, map[string]any{"pendiente": pendiente}, hist, nil, ahora); err != nil {
		return err
	}
	return t.Emitir("plantilla_whatsapp.categoria_cambiada", conPW(basePW(f), map[string]any{"de": de, "a": a, "origen": origen, "pendiente": pendiente,
		"en_uso": len(f.Codigos) > 0 || len(f.Usos) > 0}))
}
