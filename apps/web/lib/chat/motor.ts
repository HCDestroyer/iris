"use client";
// Chat del equipo en el panel: el mismo `MotorChat` de `@iris/sync/chat` que la app de campo, con
// caché en memoria (el panel no trabaja sin conexión: al recargar se vuelve a leer), tiempo real
// por SSE leído con `fetch` (manda `X-Iris-Empresa`, que `EventSource` no puede) y sondeo de
// `/cambios` si el flujo no se sostiene. Antes de cada envío, sondeo y evento que se guarda verifica
// con `GET /yo` que la sesión del navegador sigue siendo de quien abrió el panel.
import type { ClienteIris } from "@iris/contratos";
import { crearSubidorApi, errorSesionAjena, ErrorTransporte } from "@iris/sync";
import { AlmacenChatMemoria, crearFlujoFetch, crearTransporteChatApi, MotorChat, type AbrirFlujoChat, type TransporteChat } from "@iris/sync/chat";
import { crearClienteIris } from "../api/cliente";
import { ErrorApi } from "../api/errores";
import { CABECERA_EMPRESA } from "../empresa";
import { verificarSesion, esSesionCambiada } from "../conversaciones/sesion";

export type IdentidadChat = { usuarioId: number; empresaId: number | undefined };

/** El transporte de `@iris/sync` marca cada petición como de la app de campo: el panel no lo es. */
function sinCanalCampo(cliente: ClienteIris): ClienteIris {
    cliente.use({
        onRequest({ request }) {
            request.headers.delete("X-Iris-Canal");
            request.headers.delete("X-Iris-App");
            return request;
        },
    });
    return cliente;
}

/** El error de la verificación como lo entiende el motor: sesión de otro (detiene), red o HTTP. */
export function errorDeVerificacion(e: unknown): unknown {
    if (esSesionCambiada(e)) return errorSesionAjena(e.message);
    if (e instanceof ErrorApi) {
        return e.estado === 0
            ? new ErrorTransporte("red", e.message)
            : new ErrorTransporte("http", e.titulo, e.estado, { type: `urn:iris:problema:${e.codigo}`, title: e.titulo, status: e.estado });
    }
    return e;
}

/** Flujo SSE del panel: `fetch` del navegador con la cookie y la empresa activa. */
export function flujoPanel(empresaId: number | undefined): AbrirFlujoChat {
    return crearFlujoFetch({
        baseUrl: "",
        fetch: (url, init) => fetch(url, { ...init, credentials: "include", cache: "no-store" }),
        cabeceras: () => (empresaId !== undefined ? { [CABECERA_EMPRESA]: String(empresaId) } : {}),
    });
}

export type ChatPanel = { motor: MotorChat; transporte: TransporteChat };

export async function abrirChatPanel(identidad: IdentidadChat): Promise<ChatPanel> {
    // La empresa va fija: nada de este chat sale con la cabecera de otra empresa.
    const cliente = sinCanalCampo(crearClienteIris({ leerEmpresa: () => identidad.empresaId, alProblemaEmpresa: () => undefined }));
    const transporte = crearTransporteChatApi(cliente);
    const motor = await MotorChat.abrir({
        almacen: new AlmacenChatMemoria(),
        transporte,
        subidor: crearSubidorApi(cliente),
        usuarioId: identidad.usuarioId,
        abrirFlujo: typeof ReadableStream === "undefined" ? null : flujoPanel(identidad.empresaId),
        verificarIdentidad: async () => {
            try {
                await verificarSesion(identidad, cliente);
            } catch (e) {
                throw errorDeVerificacion(e);
            }
        },
    });
    return { motor, transporte };
}
