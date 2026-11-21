package mensajeria

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de las plantillas de WhatsApp (contratos/plantillas-whatsapp.md §3). Acciones explícitas:
// enviar, editar (en Meta), usos, borrar, reanudar y sincronizar; nunca un PATCH de estado.

// PlantillaCreadaPWA es la respuesta de crear: la plantilla y, si se pidió enviarla y Meta no la
// aceptó, por qué (la plantilla queda guardada como borrador; no se pierde nada).
type PlantillaCreadaPWA struct {
	PlantillaWA
	ErrorEnvio *nucleo.Problema `json:"errorEnvio" nullable:"true" doc:"Se pidió enviarla y no se pudo: queda como borrador"`
}

type entradaSugeridaHTTP struct {
	nucleo.ConIdempotencia
	Clave string `path:"clave" pattern:"^[a-z_]{1,40}$"`
	Body  EntradaCrearSugeridaPWA
}

type entradaCuentaHTTP struct {
	nucleo.ConIdempotencia
	WabaID string `path:"waba" pattern:"^[0-9]{1,30}$"`
}

// crearYEnviar: con enviar, después de crear el borrador se manda a Meta.
func (m *Modulo) crearYEnviar(ctx context.Context, p PlantillaWA, enviar bool) (*cuerpo[PlantillaCreadaPWA], error) {
	out := PlantillaCreadaPWA{PlantillaWA: p}
	if enviar {
		e, err := m.PlantillasWA.Enviar.Ejecutar(ctx, EntradaVersionPWA{ID: p.ID, Version: p.Version})
		var pr *nucleo.Problema
		switch {
		case err == nil:
			out.PlantillaWA = e
		case errors.As(err, &pr):
			out.ErrorEnvio = pr
		default:
			return nil, err
		}
	}
	return &cuerpo[PlantillaCreadaPWA]{Body: out}, nil
}

func (m *Modulo) registrarPlantillasWA(api huma.API) {
	k := m.PlantillasWA
	ver, sinc := nucleo.PermPlantillasWhatsAppVer, nucleo.PermPlantillasWhatsAppSincronizar
	crear, editar, borrar := nucleo.PermPlantillasWhatsAppCrear, nucleo.PermPlantillasWhatsAppEditar, nucleo.PermPlantillasWhatsAppBorrar
	const base = "/plantillas-whatsapp"
	errMeta := []int{http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests, http.StatusFailedDependency, http.StatusBadGateway}

	listar := op("listarPlantillasWhatsApp", http.MethodGet, base, "Plantillas de WhatsApp de las cuentas de la empresa", ver, http.StatusOK)
	listar.Description += " Por cuenta (WABA), con su estado en Meta, calidad, dónde se usan y por qué hay que atenderlas. `conteos` alimenta los filtros. " +
		"Las borradas en Meta que se usaban salen siempre (para elegir otra)."
	huma.Register(api, listar, func(ctx context.Context, in *EntradaListarPWA) (*cuerpo[ListaPlantillasWA], error) {
		return responder(k.Listar.Ejecutar(ctx, *in))
	})
	huma.Register(api, op("datosPlantillasWhatsApp", http.MethodGet, base+"/datos", "Datos que se pueden poner en una plantilla", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ListaDatosPWA], error) {
			return responder(k.Datos.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, op("cuentasPlantillasWhatsApp", http.MethodGet, base+"/cuentas", "Cuentas de WhatsApp de la empresa y su última sincronización", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ListaCuentasPWA], error) {
			return responder(k.Cuentas.Ejecutar(ctx, struct{}{}))
		})
	sincronizar := op("sincronizarPlantillasWhatsApp", http.MethodPost, base+"/cuentas/{waba}/sincronizar", "Sincronizar con Meta", sinc, http.StatusOK, errMeta...)
	sincronizar.Description += " Lee todas las plantillas de la cuenta en Meta y deja Iris igual: nuevas (creadas en Meta), cambiadas y «Borrada en Meta» " +
		"(nunca se eliminan en Iris). Si Meta no responde o el token no tiene `whatsapp_business_management`, no cambia nada (502 `meta_no_responde`, " +
		"424 `meta_sin_permiso` / `meta_token_invalido`) y la cuenta guarda el error."
	huma.Register(api, sincronizar, func(ctx context.Context, in *entradaCuentaHTTP) (*cuerpo[ResultadoSincronizacionPWA], error) {
		return responder(k.Sincronizar.Ejecutar(ctx, EntradaSincronizarPWA{WabaID: in.WabaID}))
	})
	huma.Register(api, op("sugeridasPlantillasWhatsApp", http.MethodGet, base+"/sugeridas", "Plantillas sugeridas para un ISP", ver, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaSugeridasPWA) (*cuerpo[ListaSugeridasPWA], error) {
			return responder(k.Sugeridas.Ejecutar(ctx, *in))
		})
	sugerida := op("crearSugeridaWhatsApp", http.MethodPost, base+"/sugeridas/{clave}/crear", "Crear una plantilla sugerida", crear, http.StatusCreated, errMeta...)
	sugerida.Description += " Ya conectada con los datos de Iris y Argos. Con `enviar`, además se manda a Meta; si Meta no la acepta, queda como borrador y `errorEnvio` dice por qué."
	huma.Register(api, sugerida, func(ctx context.Context, in *entradaSugeridaHTTP) (*cuerpo[PlantillaCreadaPWA], error) {
		in.Body.Clave = in.Clave
		p, err := k.CrearSugerida.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		return m.crearYEnviar(ctx, p, in.Body.Enviar)
	})
	huma.Register(api, op("ajustesPlantillasWhatsApp", http.MethodGet, base+"/ajustes", "Ajustes de plantillas de la empresa", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[AjustesPWA], error) {
			return responder(k.VerAjustes.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, op("guardarAjustesPlantillasWhatsApp", http.MethodPost, base+"/ajustes", "Guardar los ajustes de plantillas", editar, http.StatusOK, http.StatusConflict),
		func(ctx context.Context, in *cuerpoHTTP[EntradaAjustesPWA]) (*cuerpo[AjustesPWA], error) {
			return responder(k.GuardarAjustes.Ejecutar(ctx, in.Body))
		})
	validar := op("validarPlantillaWhatsApp", http.MethodPost, base+"/validar", "Revisar las reglas de Meta antes de enviar", ver, http.StatusOK, http.StatusNotFound)
	validar.Description += " No guarda nada: devuelve la lista «Antes de enviar a Meta» (cada regla ok o qué corregir y dónde)."
	huma.Register(api, validar, func(ctx context.Context, in *cuerpoHTTP[EntradaValidarPWA]) (*cuerpo[ResultadoReglasPWA], error) {
		return responder(k.Validar.Ejecutar(ctx, in.Body))
	})
	crearOp := op("crearPlantillaWhatsApp", http.MethodPost, base, "Crear una plantilla (borrador)", crear, http.StatusCreated, errMeta...)
	crearOp.Description += " Queda como borrador en Iris. Con `enviar`, además se manda a Meta (si no la acepta, `errorEnvio`). 409 `nombre_en_uso` / " +
		"`nombre_bloqueado` con `sugerencia`; 422 `marketing_no_permitido`."
	huma.Register(api, crearOp, func(ctx context.Context, in *cuerpoHTTP[EntradaCrearPWA]) (*cuerpo[PlantillaCreadaPWA], error) {
		p, err := k.Crear.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		return m.crearYEnviar(ctx, p, in.Body.Enviar)
	})
	huma.Register(api, op("verPlantillaWhatsApp", http.MethodGet, base+"/{id}", "Una plantilla con su historial", ver, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaPWID) (*cuerpo[PlantillaWADetalle], error) {
			return responder(k.Ver.Ejecutar(ctx, *in))
		})
	huma.Register(api, op("actualizarPlantillaWhatsApp", http.MethodPost, base+"/{id}/actualizar", "Editar un borrador", editar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaEditarPWA]) (*cuerpo[PlantillaWA], error) {
		in.Body.ID = in.ID
		return responder(k.Editar.Ejecutar(ctx, in.Body))
	})
	enviar := op("enviarPlantillaWhatsApp", http.MethodPost, base+"/{id}/enviar", "Enviar un borrador a Meta", crear, http.StatusOK, append(errMeta, http.StatusUnprocessableEntity)...)
	enviar.Description += " Revisa las reglas de Meta (422 `reglas_meta` con cada problema en `errors[]`) y el permiso `whatsapp_business_management` del token. " +
		"Meta la revisa en hasta 24 horas: el resultado llega por el webhook."
	huma.Register(api, enviar, func(ctx context.Context, in *idCuerpoHTTP[EntradaVersionPWA]) (*cuerpo[PlantillaWA], error) {
		in.Body.ID = in.ID
		return responder(k.Enviar.Ejecutar(ctx, in.Body))
	})
	corregir := op("editarPlantillaWhatsAppEnMeta", http.MethodPost, base+"/{id}/editar", "Corregir y volver a enviar", editar, http.StatusOK, append(errMeta, http.StatusUnprocessableEntity)...)
	corregir.Description += " Para una plantilla aprobada, rechazada o pausada. Una aprobada: 1 edición cada 24 h y 10 en 30 días (422 `limite_ediciones` con `disponibleEn`) y sin cambiar la categoría."
	huma.Register(api, corregir, func(ctx context.Context, in *idCuerpoHTTP[EntradaEditarMetaPWA]) (*cuerpo[PlantillaWA], error) {
		in.Body.ID = in.ID
		return responder(k.EditarMeta.Ejecutar(ctx, in.Body))
	})
	usos := op("usosPlantillaWhatsApp", http.MethodPost, base+"/{id}/usos", "Decidir dónde la usa Iris", editar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	usos.Description += " Usos (avisos, conversaciones, asistente) y el código del catálogo que atiende (Argos pide avisos por código): si otra plantilla de la cuenta lo tenía, lo suelta. " +
		"422 `datos_no_disponibles` si el aviso no trae algún dato de la plantilla. No pasa por Meta."
	huma.Register(api, usos, func(ctx context.Context, in *idCuerpoHTTP[EntradaUsosPWA]) (*cuerpo[PlantillaWA], error) {
		in.Body.ID = in.ID
		return responder(k.Usos.Ejecutar(ctx, in.Body))
	})
	borrarOp := op("borrarPlantillaWhatsApp", http.MethodPost, base+"/{id}/borrar", "Borrar una plantilla", borrar, http.StatusOK, append(errMeta, http.StatusUnprocessableEntity)...)
	borrarOp.Description += " La borra en Meta (solo ese idioma, por su id) y en Iris queda «Borrada» con su historial. Una aprobada deja el nombre bloqueado 30 días (`nombreLibreEn`). " +
		"Meta no deja borrar una deshabilitada (422 `deshabilitada_no_se_borra`)."
	huma.Register(api, borrarOp, func(ctx context.Context, in *idCuerpoHTTP[EntradaVersionPWA]) (*cuerpo[PlantillaWA], error) {
		in.Body.ID = in.ID
		return responder(k.Borrar.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("reanudarPlantillaWhatsApp", http.MethodPost, base+"/{id}/reanudar", "Reanudar una plantilla pausada", editar, http.StatusOK, errMeta...),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaVersionPWA]) (*cuerpo[PlantillaWA], error) {
			in.Body.ID = in.ID
			return responder(k.Reanudar.Ejecutar(ctx, in.Body))
		})
}
