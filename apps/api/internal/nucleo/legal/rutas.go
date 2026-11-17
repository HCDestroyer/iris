package legal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de los datos legales (contratos/legal.md). Handlers delgados.

const etiqueta = "datos legales"

// CachePublica: las páginas públicas (y cualquier caché intermedia) pueden guardar la respuesta un
// minuto. Un rastreador que insiste no llega a la base en cada petición.
const CachePublica = "public, max-age=60"

var seguridad = []map[string][]string{{"sesion": {}}}

type cuerpo[T any] struct {
	Body T
}

type entradaGuardarHTTP struct {
	nucleo.ConIdempotencia
	Body CuerpoGuardar
}

type salidaPublica struct {
	CacheControl string `header:"Cache-Control"`
	Body         DatosPublicos
}

// Registrar publica las rutas.
func (m *Modulo) Registrar(api huma.API) {
	const contrato = " Contrato: `.claude/integration/contratos/legal.md`."

	huma.Register(api, huma.Operation{
		OperationID: "verDatosLegales", Method: http.MethodGet, Path: "/api/v1/datos-legales", Summary: "Ver los datos legales del titular",
		Description: "Permiso `datos_legales.ver`. Los datos de quien opera la instalación (razón social, nombre comercial, contacto de privacidad, " +
			"dirección y país) que muestran las páginas públicas de privacidad, términos y eliminación de datos. Sin datos guardados: campos vacíos, " +
			"`configurado: false` y `version: 0`. `faltan` lista los obligatorios vacíos; con `completo: true` las páginas están listas para Meta. " +
			"Son de la instalación: exige operar sin límite de empresa (si no, 403). Solo personas." + contrato,
		Tags: []string{etiqueta}, DefaultStatus: http.StatusOK, Security: seguridad,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, _ *struct{}) (*cuerpo[DatosLegales], error) {
		r, err := m.Ver.Ejecutar(ctx, struct{}{})
		if err != nil {
			return nil, err
		}
		return &cuerpo[DatosLegales]{Body: r}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "guardarDatosLegales", Method: http.MethodPost, Path: "/api/v1/datos-legales/guardar", Summary: "Guardar los datos legales del titular",
		Description: "Permiso `datos_legales.configurar`. Crea o reemplaza los datos. `version` la que leíste (0 la primera vez): distinta, 409. " +
			"Cualquier campo puede ir vacío (se guarda lo que haya y `faltan` dice qué falta); el correo debe ser una sola dirección y el teléfono " +
			"solo dígitos, espacios, +, guiones y paréntesis (422 con `errors[].field`). Si nada cambia no sube la versión ni la fecha. La fecha de " +
			"«última actualización» de las páginas públicas es la de este cambio. Emite `datos_legales.cambiados`." + contrato,
		Tags: []string{etiqueta}, DefaultStatus: http.StatusOK, Security: seguridad,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *entradaGuardarHTTP) (*cuerpo[DatosLegales], error) {
		r, err := m.Guardar.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		return &cuerpo[DatosLegales]{Body: r}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "verDatosLegalesPublicos", Method: http.MethodGet, Path: "/api/v1/publico/datos-legales", Summary: "Datos legales públicos del titular",
		Description: "Sin sesión. Lo que el titular publica en las páginas de privacidad, términos y eliminación de datos: nombre comercial, razón " +
			"social, correo de privacidad, teléfono, dirección, país y la fecha del último cambio. Sin datos guardados responde 200 con los campos " +
			"vacíos, `actualizadoEn: null` y `completo: false` (las páginas no muestran huecos). Respuesta con `Cache-Control: public, max-age=60`." + contrato,
		Tags: []string{etiqueta}, DefaultStatus: http.StatusOK,
	}, func(ctx context.Context, _ *struct{}) (*salidaPublica, error) {
		r, err := m.Publico.Ejecutar(ctx, struct{}{})
		if err != nil {
			return nil, err
		}
		return &salidaPublica{CacheControl: CachePublica, Body: r}, nil
	})
}
