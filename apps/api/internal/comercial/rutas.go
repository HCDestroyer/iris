package comercial

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta.

const etiqueta = "comercial"

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: metodo, Path: ruta, Summary: resumen,
		Description: "Permiso: `" + string(permiso) + "`.", Tags: []string{etiqueta}, DefaultStatus: status,
		Errors:     append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity}, errores...),
		Security:   seguridad,
		Extensions: map[string]any{"x-permiso": string(permiso)},
	}
}

func accion(id, ruta, resumen, detalle string, permiso nucleo.Permiso) huma.Operation {
	op := operacion(id, http.MethodPost, ruta, resumen, permiso, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	if detalle != "" {
		op.Description += " " + detalle
	}
	return op
}

func con(op huma.Operation, detalle string) huma.Operation {
	op.Description += " " + detalle
	return op
}

type cuerpo[T any] struct {
	Body T
}

type conID[T any] struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

// soloID: acción sin cuerpo sobre {id}.
type soloID struct {
	nucleo.ConIdempotencia
	ID int64 `path:"id" minimum:"1"`
}

func responder[T any](v T, err error) (*cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpo[T]{Body: v}, nil
}

// registrarConID publica POST <ruta> con {id}; fijar pone el id (y la Idempotency-Key) en la entrada.
func registrarConID[In, Out any](api huma.API, op huma.Operation, ejecutar func(context.Context, In) (Out, error), fijar func(*In, int64, string)) {
	huma.Register(api, op, func(ctx context.Context, in *conID[In]) (*cuerpo[Out], error) {
		e := in.Body
		fijar(&e, in.ID, in.IdempotencyKey)
		return responder(ejecutar(ctx, e))
	})
}

func lectura[In, Out any](api huma.API, op huma.Operation, ejecutar func(context.Context, In) (Out, error)) {
	huma.Register(api, op, func(ctx context.Context, in *In) (*cuerpo[Out], error) {
		return responder(ejecutar(ctx, *in))
	})
}

// Registrar publica las rutas de comercial.
func (m *Modulo) Registrar(api huma.API) {
	ver := nucleo.PermProspectosVer

	// Prospectos.
	lectura(api, con(operacion("listarProspectos", http.MethodGet, "/api/v1/prospectos", "Prospectos (embudo comercial)", ver, http.StatusOK),
		"Cursor (id desc). Con alcance por zona: los de tus zonas y los tuyos (vendedor)."), m.ListarProspectos.Ejecutar)
	crear := con(operacion("crearProspecto", http.MethodPost, "/api/v1/prospectos", "Registrar un prospecto", nucleo.PermProspectosCrear, http.StatusCreated),
		"Guarda la Idempotency-Key. Emite `prospecto.creado`.")
	huma.Register(api, crear, func(ctx context.Context, in *struct {
		nucleo.ConIdempotencia
		Body EntradaCrearProspecto
	}) (*cuerpo[Prospecto], error) {
		e := in.Body
		e.ClaveIdempotencia = in.IdempotencyKey
		return responder(m.CrearProspecto.Ejecutar(ctx, e))
	})
	lectura(api, con(operacion("verProspecto", http.MethodGet, "/api/v1/prospectos/{id}", "Prospecto con su embudo", ver, http.StatusOK, http.StatusNotFound),
		"Factibilidad vigente, cotización viva, contrato vivo, pendientes del alta en Argos y las acciones que corresponden."), m.VerProspecto.Ejecutar)
	lectura(api, operacion("eventosProspecto", http.MethodGet, "/api/v1/prospectos/{id}/eventos", "Historial del prospecto", ver, http.StatusOK, http.StatusNotFound),
		m.EventosProspecto.Ejecutar)
	registrarConID(api, accion("editarProspecto", "/api/v1/prospectos/{id}/editar", "Editar un prospecto",
		"Solo hasta cotizado. Emite `prospecto.actualizado`.", nucleo.PermProspectosEditar),
		m.EditarProspecto.Ejecutar, func(e *EntradaEditarProspecto, id int64, _ string) { e.ID = id })
	registrarConID(api, accion("asignarProspecto", "/api/v1/prospectos/{id}/asignar", "Asignar el vendedor",
		"El vendedor ve el prospecto (prospectos.ver con alcance) y opera en su empresa. Emite `prospecto.asignado`.", nucleo.PermProspectosAsignar),
		m.AsignarProspecto.Ejecutar, func(e *EntradaAsignarProspecto, id int64, _ string) { e.ID = id })
	registrarConID(api, accion("contactarProspecto", "/api/v1/prospectos/{id}/contactar", "Marcar contactado",
		"nuevo → contactado. Emite `prospecto.estado_cambiado`.", nucleo.PermProspectosContactar),
		m.Contactar.Ejecutar, func(e *EntradaTransicion, id int64, _ string) { e.ID = id })
	registrarConID(api, accion("perderProspecto", "/api/v1/prospectos/{id}/perder", "Marcar perdido",
		"Con motivo; anula el contrato vivo. Emite `prospecto.perdido`.", nucleo.PermProspectosPerder),
		m.Perder.Ejecutar, func(e *EntradaPerder, id int64, _ string) { e.ID = id })
	registrarConID(api, accion("reabrirProspecto", "/api/v1/prospectos/{id}/reabrir", "Reabrir un prospecto perdido",
		"perdido → contactado.", nucleo.PermProspectosReabrir),
		m.Reabrir.Ejecutar, func(e *EntradaTransicion, id int64, _ string) { e.ID = id })
	registrarConID(api, accion("ganarProspecto", "/api/v1/prospectos/{id}/ganar", "Ganar el prospecto",
		"Exige contrato firmado, su cotización aceptada y factibilidad viable (o requiere_obra con confirmarObra) sobre las coordenadas actuales. "+
			"Con conector: encola POST /clientes y, al completarse, POST /servicios (pendiente_instalacion) y la orden de instalación (alta.estado). "+
			"Sin conector: crea la orden con los datos capturados. Emite `prospecto.ganado`.", nucleo.PermProspectosGanar),
		m.Ganar.Ejecutar, func(e *EntradaGanar, id int64, _ string) { e.ID = id })

	// Factibilidad.
	registrarConID(api, accion("evaluarFactibilidad", "/api/v1/prospectos/{id}/factibilidad", "Evaluar la factibilidad",
		"Distancia al router más cercano de la instantánea de Argos con los umbrales de la empresa (o `manual` tras una visita). "+
			"Sin datos: indeterminado con motivo. Emite `factibilidad.evaluada`.", nucleo.PermFactibilidadEvaluar),
		m.EvaluarFactibilidad, func(e *EntradaFactibilidad, id int64, _ string) { e.ProspectoID = id })
	lectura(api, operacion("verUmbrales", http.MethodGet, "/api/v1/comercial/umbrales", "Umbrales de la factibilidad de la empresa", ver, http.StatusOK),
		func(ctx context.Context, _ struct{}) (Umbrales, error) {
			return m.VerUmbrales.Ejecutar(ctx, struct{}{})
		})
	huma.Register(api, con(operacion("fijarUmbrales", http.MethodPost, "/api/v1/comercial/umbrales", "Fijar los umbrales de la empresa",
		nucleo.PermFactibilidadConfigurar, http.StatusOK, http.StatusConflict), "Sin límite de zona. Emite `factibilidad.umbrales_cambiados`."),
		func(ctx context.Context, in *struct {
			nucleo.ConIdempotencia
			Body EntradaUmbrales
		}) (*cuerpo[Umbrales], error) {
			return responder(m.FijarUmbrales.Ejecutar(ctx, in.Body))
		})
	lectura(api, con(operacion("coberturaComercial", http.MethodGet, "/api/v1/comercial/cobertura", "Cobertura para el mapa", ver, http.StatusOK),
		"Routers con coordenadas de la instantánea de Argos y los umbrales (círculos viable y requiere obra)."),
		func(ctx context.Context, _ struct{}) (Cobertura, error) { return m.Cobertura.Ejecutar(ctx, struct{}{}) })

	// Actividades.
	huma.Register(api, operacion("listarActividades", http.MethodGet, "/api/v1/prospectos/{id}/actividades", "Actividades del prospecto", ver, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaListarActividades) (*cuerpo[nucleo.Pagina[Actividad]], error) {
			return responder(m.ListarActividades.Ejecutar(ctx, *in))
		})
	act := operacion("registrarActividad", http.MethodPost, "/api/v1/prospectos/{id}/actividades", "Registrar una llamada, visita o nota",
		nucleo.PermProspectosRegistrarActividad, http.StatusCreated, http.StatusNotFound)
	act.Description += " Guarda la Idempotency-Key. Emite `prospecto.actividad_registrada`."
	registrarConID(api, act, m.RegistrarActividad.Ejecutar, func(e *EntradaActividad, id int64, clave string) {
		e.ProspectoID, e.ClaveIdempotencia = id, clave
	})

	// Cotizaciones.
	lectura(api, con(operacion("planesComerciales", http.MethodGet, "/api/v1/comercial/planes", "Planes de Argos para cotizar", nucleo.PermCotizacionesCrear, http.StatusOK),
		"GET /planes de Argos por el conector (empresa activa). disponible = false con motivo si no se pueden leer."),
		func(ctx context.Context, _ struct{}) (PlanesDisponibles, error) { return m.Planes(ctx) })
	lectura(api, operacion("listarCotizaciones", http.MethodGet, "/api/v1/prospectos/{id}/cotizaciones", "Cotizaciones del prospecto (todas las revisiones)", ver,
		http.StatusOK, http.StatusNotFound), m.ListarCotizaciones.Ejecutar)
	cot := operacion("crearCotizacion", http.MethodPost, "/api/v1/prospectos/{id}/cotizaciones", "Cotizar (o una revisión nueva)",
		nucleo.PermCotizacionesCrear, http.StatusCreated, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable)
	cot.Description += " Exactamente una línea de plan (de Argos con conector). Reemplaza la vigente o aceptada sin borrarla y anula el contrato en borrador. " +
		"Genera el PDF (adjunto). Guarda la Idempotency-Key. Emite `cotizacion.creada`."
	registrarConID(api, cot, m.CrearCotizacion, func(e *EntradaCrearCotizacion, id int64, clave string) {
		e.ProspectoID, e.ClaveIdempotencia = id, clave
	})
	lectura(api, operacion("verCotizacion", http.MethodGet, "/api/v1/cotizaciones/{id}", "Cotización", ver, http.StatusOK, http.StatusNotFound), m.VerCotizacion.Ejecutar)
	registrarConID(api, accion("aceptarCotizacion", "/api/v1/cotizaciones/{id}/aceptar", "Aceptar la cotización",
		"La vigente y no vencida, con el prospecto en cotizado. Emite `cotizacion.aceptada`.", nucleo.PermCotizacionesAceptar),
		m.AceptarCotizacion.Ejecutar, func(e *EntradaAceptarCotizacion, id int64, _ string) { e.ID = id })
	huma.Register(api, accion("generarPdfCotizacion", "/api/v1/cotizaciones/{id}/pdf", "Volver a generar el PDF",
		"Si no quedó guardado (pdf.estado ≠ completo). Mismos datos, mismo PDF.", nucleo.PermCotizacionesCrear),
		func(ctx context.Context, in *soloID) (*cuerpo[Cotizacion], error) {
			return responder(m.GenerarPDFCotizacion(ctx, EntradaVerCotizacion{ID: in.ID}))
		})

	// Contratos.
	ctr := operacion("crearContrato", http.MethodPost, "/api/v1/prospectos/{id}/contrato", "Generar el contrato",
		nucleo.PermContratosCrear, http.StatusCreated, http.StatusNotFound, http.StatusConflict)
	ctr.Description += " Plantilla estandar_v1 con los datos del prospecto y la cotización aceptada; PDF borrador para leer. cotizado → contrato. Emite `contrato.creado`."
	registrarConID(api, ctr, m.CrearContrato, func(e *EntradaCrearContrato, id int64, _ string) { e.ProspectoID = id })
	lectura(api, operacion("verContrato", http.MethodGet, "/api/v1/contratos/{id}", "Contrato", ver, http.StatusOK, http.StatusNotFound), m.VerContrato.Ejecutar)
	firmar := accion("firmarContrato", "/api/v1/contratos/{id}/firmar", "Registrar la firma del cliente",
		"Firma capturada como imagen (SVG). Genera el PDF firmado como adjunto; su SHA-256 queda en la auditoría. Emite `contrato.firmado`.", nucleo.PermContratosFirmar)
	firmar.Errors = append(firmar.Errors, http.StatusServiceUnavailable)
	registrarConID(api, firmar, m.FirmarContrato, func(e *EntradaFirmar, id int64, _ string) { e.ContratoID = id })
	huma.Register(api, accion("generarPdfContrato", "/api/v1/contratos/{id}/pdf", "Volver a generar el PDF borrador",
		"Si no quedó guardado.", nucleo.PermContratosCrear),
		func(ctx context.Context, in *soloID) (*cuerpo[Contrato], error) {
			return responder(m.GenerarPDFContrato(ctx, EntradaVerContrato{ID: in.ID}))
		})
}
