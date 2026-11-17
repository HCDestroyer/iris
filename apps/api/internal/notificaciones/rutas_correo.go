package notificaciones

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del correo del sistema (contratos/correo-sistema.md). Handlers delgados.

const etiquetaCorreo = "correo del sistema"

func operacionCorreo(id, metodo, ruta, resumen, detalle string, permiso nucleo.Permiso, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: metodo, Path: ruta, Summary: resumen,
		Description: "Permiso `" + string(permiso) + "`. `{ambito}` es `global` (exige operar sin límite de empresa) o el id de la empresa " +
			"(exige estar operando en ella; si no, 404). " + detalle + " Contrato: `.claude/integration/contratos/correo-sistema.md`.",
		Tags: []string{etiquetaCorreo}, DefaultStatus: http.StatusOK, Security: seguridad,
		Errors: append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity}, errores...),
	}
}

type ambitoHTTP struct {
	Ambito string `path:"ambito" pattern:"^(global|[1-9][0-9]{0,17})$" doc:"global o el id de la empresa"`
}

type entradaGuardarCorreoHTTP struct {
	nucleo.ConIdempotencia
	Ambito string `path:"ambito" pattern:"^(global|[1-9][0-9]{0,17})$" doc:"global o el id de la empresa"`
	Body   CuerpoGuardarCorreo
}

type entradaProbarCorreoHTTP struct {
	Ambito string `path:"ambito" pattern:"^(global|[1-9][0-9]{0,17})$" doc:"global o el id de la empresa"`
	Body   CuerpoProbarCorreo
}

type versionCorreoSistema struct {
	Version int32 `json:"version" minimum:"1" doc:"La que leíste"`
}

type entradaActivarCorreoHTTP struct {
	nucleo.ConIdempotencia
	Ambito string `path:"ambito" pattern:"^(global|[1-9][0-9]{0,17})$" doc:"global o el id de la empresa"`
	Body   versionCorreoSistema
}

func (m *Modulo) registrarCorreoSistema(api huma.API) {
	ver, configurar := nucleo.PermCorreoSistemaVer, nucleo.PermCorreoSistemaConfigurar

	huma.Register(api, operacionCorreo("verCorreoSistema", http.MethodGet, "/api/v1/correo-sistema/{ambito}", "Ver el correo del sistema de un ámbito",
		"El ajuste guardado (o los valores por defecto con `configurado: false` y `version: 0`) y por dónde sale hoy el correo: `origen` "+
			"(`empresa`, `global`, `entorno` o `ninguno`) y `enUso` (este ajuste es el que se usa). Un ajuste apagado se salta. Nunca devuelve la "+
			"contraseña: solo `tieneContrasena`.", ver),
		func(ctx context.Context, in *ambitoHTTP) (*cuerpo[CorreoSistema], error) {
			r, err := m.VerCorreoSistema.Ejecutar(ctx, EntradaVerCorreo{Ambito: in.Ambito})
			if err != nil {
				return nil, err
			}
			return &cuerpo[CorreoSistema]{Body: r}, nil
		})

	huma.Register(api, operacionCorreo("guardarCorreoSistema", http.MethodPost, "/api/v1/correo-sistema/{ambito}/guardar", "Guardar el correo del sistema",
		"Crea o reemplaza el ajuste. `version` la que leíste (0 al crear): distinta, 409. `contrasena` ausente = se conserva; `\"\"` = se borra; "+
			"si cambias el servidor, el puerto o el usuario hay que volver a escribirla (422 `contrasena_requerida`). Puertos 25, 465, 587 o 2525. "+
			"`seguridad: ninguna` solo con un servidor de la red interna permitida y sin usuario ni contraseña (422 `sin_cifrado_solo_red_interna`, "+
			"`sin_cifrado_sin_credenciales`). Un servidor en una red no permitida: 422 `destino_no_permitido`. No prueba la conexión (usa `probar`). "+
			"Emite `correo_sistema.ajustes_cambiados`.", configurar, http.StatusConflict),
		func(ctx context.Context, in *entradaGuardarCorreoHTTP) (*cuerpo[CorreoSistema], error) {
			r, err := m.GuardarCorreoSistema.Ejecutar(ctx, EntradaGuardarCorreo{Ambito: in.Ambito, CuerpoGuardarCorreo: in.Body})
			if err != nil {
				return nil, err
			}
			return &cuerpo[CorreoSistema]{Body: r}, nil
		})

	huma.Register(api, operacionCorreo("probarCorreoSistema", http.MethodPost, "/api/v1/correo-sistema/{ambito}/probar", "Probar el correo del sistema sin guardar",
		"Manda un correo de asunto y cuerpo fijos a `para` (una sola dirección) con los valores del formulario, sin guardarlos. Sin `contrasena` usa "+
			"la guardada de ese ámbito, solo si el servidor, el puerto y el usuario son los guardados. Responde siempre 200 con `estado` (`ok` o "+
			"`error`), `causa` (lista cerrada) y `detalle` en lenguaje llano; nunca la respuesta cruda del servidor. Tope: 5 pruebas por usuario y "+
			"20 por instalación cada hora (429 `demasiadas_pruebas` con `Retry-After`). Emite `correo_sistema.probado`.", configurar, http.StatusTooManyRequests),
		func(ctx context.Context, in *entradaProbarCorreoHTTP) (*cuerpo[ResultadoPruebaCorreo], error) {
			r, err := m.ProbarCorreoSistema(ctx, EntradaProbarCorreo{Ambito: in.Ambito, CuerpoProbarCorreo: in.Body})
			if err != nil {
				return nil, err
			}
			return &cuerpo[ResultadoPruebaCorreo]{Body: r}, nil
		})

	activar := func(id, ruta, resumen, detalle string, caso func(context.Context, EntradaActivarCorreo) (CorreoSistema, error)) {
		huma.Register(api, operacionCorreo(id, http.MethodPost, ruta, resumen, detalle+" Sin ajuste guardado, 404; `version` distinta, 409. "+
			"Emite `correo_sistema.ajustes_cambiados`.", configurar, http.StatusConflict),
			func(ctx context.Context, in *entradaActivarCorreoHTTP) (*cuerpo[CorreoSistema], error) {
				r, err := caso(ctx, EntradaActivarCorreo{Ambito: in.Ambito, Version: in.Body.Version})
				if err != nil {
					return nil, err
				}
				return &cuerpo[CorreoSistema]{Body: r}, nil
			})
	}
	activar("activarCorreoSistema", "/api/v1/correo-sistema/{ambito}/activar", "Activar el correo del sistema de un ámbito",
		"Vuelve a usar el ajuste guardado.", m.ActivarCorreoSistema.Ejecutar)
	activar("desactivarCorreoSistema", "/api/v1/correo-sistema/{ambito}/desactivar", "Desactivar el correo del sistema de un ámbito",
		"El ajuste se conserva pero se salta: se usa el siguiente (el global, o el del servidor).", m.DesactivarCorreoSistema.Ejecutar)
}
