package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del servidor de IA (contratos/voz.md §4.8). Handlers delgados.

type cuerpoRotarMotor struct {
	Revocar bool `json:"revocar,omitempty" doc:"«Revocar ahora»: las credenciales anteriores dejan de valer en el acto"`
}

func (m *Modulo) registrarServidorIA(api huma.API) {
	k := m.ServidorIA
	ver, adm := nucleo.PermServidorIAVer, nucleo.PermServidorIAAdministrar
	ia := func(o huma.Operation, detalle string) huma.Operation {
		o.Description += " " + detalle + " El servidor de IA es de la instalación: solo personas con sesión del panel y `empresas.todas`; cualquier otro recibe 404. " +
			"Nunca se devuelven el token, el código de enlace ni claves. Contrato: `.claude/integration/contratos/voz.md` §4.8."
		return o
	}
	huma.Register(api, ia(op("listarMotoresIA", http.MethodGet, "/voz/motores", "El servidor de IA de la instalación", ver, http.StatusOK),
		"Uno como mucho. `huellaTls` es la huella del certificado que vio Iris: se compara con la que imprimió el instalador antes de activar."),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ListaMotoresIA], error) {
			return responder(k.Listar.Ejecutar(ctx, struct{}{}))
		})
	enl := ia(op("enlazarMotorIA", http.MethodPost, "/voz/motores/enlazar", "Pedir un código para enlazar el servidor de IA", adm,
		http.StatusCreated, http.StatusConflict, http.StatusUnprocessableEntity),
		"Devuelve **una vez** un código de un solo uso (160 bits) que vale 15 minutos y se escribe en el servidor con `iris-voz enlazar`. Iris lo guarda cifrado "+
			"hasta enlazar o hasta que vence, y lo borra. Enlazar de nuevo invalida el canal anterior. 409 `motor_ya_enlazado` si hay uno activo (desactívalo antes); "+
			"422 `url_no_permitida`. "+descNoRepetibleIA)
	huma.Register(api, enl, func(ctx context.Context, in *cuerpoHTTP[EntradaEnlazarMotor]) (*cuerpo[EnlaceMotor], error) {
		r, err := k.Enlazar.Ejecutar(ctx, in.Body)
		if err == nil {
			nucleo.NoRepetible(ctx)
		}
		return responder(r, err)
	})
	huma.Register(api, ia(op("conectarMotorIA", http.MethodPost, "/voz/motores/{id}/conectar", "Hacer el intercambio de enlace con el servidor de IA", adm,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway),
		"Después de escribir el código en el servidor. Iris y el servidor se demuestran que conocen el código, cada prueba atada al certificado del otro; el código "+
			"no viaja y el secreto inicial del canal se deriva en los dos lados. Queda pendiente de activar: `huellaTls` trae la huella a comparar. "+
			"409 `enlace_vencido`; 502 `motor_no_responde` (la misma respuesta para cualquier falla). Emite `voz.motor_enlazado`."),
		func(ctx context.Context, in *idHTTP) (*cuerpo[MotorIA], error) {
			return responder(k.Conectar.Ejecutar(ctx, in.ID))
		})
	huma.Register(api, ia(op("activarMotorIA", http.MethodPost, "/voz/motores/{id}/activar", "Activar el servidor de IA", adm,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway),
		"Cuando la huella coincide con la del instalador. Lo primero que hace Iris es cambiar el secreto del canal por uno aleatorio, sin convivencia; si eso falla, "+
			"no se activa. También vuelve a activar uno desactivado. `huellaTls` es la huella que el administrador comparó: 409 `huella_cambiada` si el servidor "+
			"enlazado ya no es ese (alguien enlazó otro entretanto). 409 `motor_sin_enlazar`, `enlace_vencido` (24 h sin activar) o `rotacion_en_curso`. "+
			"Emite `voz.motor_activado` y `voz.motor_token_rotado`."),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaActivarMotor]) (*cuerpo[MotorIA], error) {
			in.Body.ID = in.ID
			return responder(k.Activar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, ia(op("desactivarMotorIA", http.MethodPost, "/voz/motores/{id}/desactivar", "Desactivar el servidor de IA", adm,
		http.StatusOK, http.StatusNotFound),
		"Deja de usarse en el acto: el asistente de las empresas que lo usan falla al instante (sugerencias con «Reintentar»; el autónomo pasa a una persona). "+
			"Emite `voz.motor_desactivado` y `voz.motor_estado_cambiado`."),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaDesactivarMotor]) (*cuerpo[MotorIA], error) {
			in.Body.ID = in.ID
			return responder(k.Desactivar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, ia(op("probarMotorIA", http.MethodPost, "/voz/motores/{id}/probar", "Probar el servidor de IA", adm,
		http.StatusOK, http.StatusNotFound, http.StatusConflict),
		"Pasos `conexion`, `compatibilidad`, `gpu`, `stt`, `tts`, `llm` (una frase fija de ida y vuelta), `medios` y `capacidad`; los que esta instalación no trae "+
			"salen `omitido` y no vuelven `error` la prueba. Guarda el resultado en `ultimaPrueba`. No cuenta para los topes."),
		func(ctx context.Context, in *idHTTP) (*cuerpo[MotorIA], error) {
			return responder(k.Probar.Ejecutar(ctx, in.ID))
		})
	huma.Register(api, ia(op("rotarTokenMotorIA", http.MethodPost, "/voz/motores/{id}/rotar-token", "Rotar las credenciales del canal con el servidor de IA", adm,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway),
		"Las anteriores siguen valiendo 24 h; con `revocar: true` dejan de valer en el acto y también se cambia el certificado de cliente. Si Iris se vio "+
			"comprometido, rotar no basta: `iris-voz desenlazar` en el servidor y enlazar de nuevo. Emite `voz.motor_token_rotado`."),
		func(ctx context.Context, in *idCuerpoHTTP[cuerpoRotarMotor]) (*cuerpo[MotorIA], error) {
			return responder(k.Rotar.Ejecutar(ctx, EntradaRotarMotor{ID: in.ID, Revocar: in.Body.Revocar}))
		})
	huma.Register(api, ia(op("actualizarEmpresasMotorIA", http.MethodPost, "/voz/motores/{id}/empresas/actualizar", "Qué empresas usan el servidor de IA", adm,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		"`empresas: null` = todas. `simultaneasPorEmpresa`: peticiones al modelo a la vez por empresa (además, cada empresa activa tiene una plaza garantizada). "+
			"Quitar una empresa no borra sus ajustes: su asistente deja de poder usarlo. Emite `voz.motor_empresas_cambiadas`."),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaEmpresasMotor]) (*cuerpo[MotorIA], error) {
			in.Body.ID = in.ID
			return responder(k.Empresas.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, ia(op("actualizarModelosMotorIA", http.MethodPost, "/voz/motores/{id}/modelos/actualizar", "Qué modelos pueden elegir las empresas", adm,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		"De los que declara el servidor en `catalogo.llm` (los que su administrador deja usar; Iris no puede ampliarlos), los que las empresas pueden elegir. "+
			"`modelos: null` = todos. 422 `modelo_no_instalado` si alguno no está en el catálogo. Emite `voz.motor_modelos_cambiados`."),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaModelosMotor]) (*cuerpo[MotorIA], error) {
			in.Body.ID = in.ID
			return responder(k.Modelos.Ejecutar(ctx, in.Body))
		})

	// Tablero y umbrales (§4.12) -------------------------------------------------------------------
	tab := op("tableroServidorIA", http.MethodGet, "/servidor-ia/tablero", "Tablero del servidor de IA", nucleo.PermServidorIAVerUso, http.StatusOK, http.StatusNotFound)
	tab.Description += " Con `servidor_ia.ver` y `empresas.todas`: todo (GPU, CPU, memoria, disco, modelos, ocupación y el uso de todas las empresas). Con " +
		"`servidor_ia.ver_uso`: el estado general y solo el uso de la empresa activa (`servidor: null`, `avisos: []`, series solo con peticiones y tiempos). " +
		"Cualquier otro, 404. `estado.lecturaOk` es `false` si la última lectura del servidor falló: lo de `servidor` es de `estado.tomadoEn` y no debe " +
		"mostrarse como actual. Nunca lleva texto de prompts ni de respuestas. Contrato: `.claude/integration/contratos/voz.md` §4.12."
	huma.Register(api, tab, func(ctx context.Context, in *EntradaTableroIA) (*cuerpo[TableroIA], error) {
		return responder(k.Tablero.Ejecutar(ctx, *in))
	})
	huma.Register(api, ia(op("verUmbralesServidorIA", http.MethodGet, "/servidor-ia/umbrales", "Umbrales de los avisos del servidor de IA", ver, http.StatusOK, http.StatusNotFound),
		"Los valores de fábrica si nadie los cambió."),
		func(ctx context.Context, _ *struct{}) (*cuerpo[UmbralesIA], error) {
			return responder(k.Umbrales.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, ia(op("actualizarUmbralesServidorIA", http.MethodPost, "/servidor-ia/umbrales/actualizar", "Cambiar los umbrales de los avisos del servidor de IA", adm,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		"Emite `servidor_ia.ajustes_cambiados`."),
		func(ctx context.Context, in *cuerpoHTTP[EntradaUmbralesIA]) (*cuerpo[UmbralesIA], error) {
			return responder(k.FijarUmbrales.Ejecutar(ctx, in.Body))
		})
}

const descNoRepetibleIA = "La respuesta lleva un secreto que no se guarda para `Idempotency-Key`: repetir la clave responde 409 `idempotencia_no_repetible`."
