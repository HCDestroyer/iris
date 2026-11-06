// Tarea en segundo plano de las notificaciones (`contratos/push.md` §4): con la app cerrada o en
// segundo plano, FCM entrega el mensaje de datos aquí y la app arma la notificación; también llegan
// aquí «Responder» y «Marcar como leído» (no abren la app). Como la del GPS, `defineTask` corre al
// cargar el bundle: se importa desde `index.ts`.
import { crearGeneradorUuid7 } from "@iris/sync";
import * as Crypto from "expo-crypto";
import * as Notifications from "expo-notifications";
import * as TaskManager from "expo-task-manager";
import { entorno } from "../entorno";
import { cargarCsrf, cliente, sesionGuardada } from "../sesion/sesion";
import { ejecutarRespuesta } from "./acciones";
import { crearApiPush } from "./api";
import { leerMensajePush } from "./mensaje";
import { presentarMensaje, prepararNotificaciones, quitarGrupoNativo } from "./nativo";

export const TAREA_PUSH = "iris-push";

const uuid7 = crearGeneradorUuid7(Date.now, (bytes) => {
    Crypto.getRandomValues(bytes);
});

/** `data` del mensaje: en Android puede venir como `dataString` (JSON). */
function dataDe(payload: Record<string, unknown>): Record<string, unknown> {
    const d = (payload.data ?? {}) as Record<string, unknown>;
    if (typeof d.dataString === "string") {
        try {
            return JSON.parse(d.dataString) as Record<string, unknown>;
        } catch {
            return {};
        }
    }
    return d;
}

TaskManager.defineTask<Notifications.NotificationTaskPayload>(TAREA_PUSH, async ({ data, error }) => {
    if (error || !data) {
        return;
    }
    const claves = entorno().claves;
    const sesion = sesionGuardada();
    if ("actionIdentifier" in data) {
        // Un botón con la app cerrada: se hace en nombre de quien tiene la sesión en el teléfono.
        const mensaje = leerMensajePush(data.notification.request.content.data as Record<string, unknown>);
        if (!mensaje) {
            return;
        }
        await cargarCsrf();
        const accion = data.actionIdentifier === "responder" || data.actionIdentifier === "leido" ? data.actionIdentifier : null;
        if (!accion) {
            return;
        }
        await ejecutarRespuesta(
            { accion, mensaje, ...(data.userText ? { texto: data.userText } : {}) },
            { api: crearApiPush(cliente), claves, generarId: uuid7, usuarioId: sesion?.usuarioId ?? null, quitarGrupo: (g) => quitarGrupoNativo(g, claves) },
        );
        return;
    }
    const mensaje = leerMensajePush(dataDe(data as Record<string, unknown>));
    // Para otra persona (cambió la sesión del teléfono) o sin sesión: no se muestra.
    if (!mensaje || !sesion || mensaje.usuarioId !== sesion.usuarioId) {
        return;
    }
    await prepararNotificaciones();
    await presentarMensaje(mensaje, claves);
});

/** Pide a expo-notifications que entregue los mensajes de datos a la tarea (una vez por arranque). */
export async function registrarTareaPush(): Promise<void> {
    try {
        await Notifications.registerTaskAsync(TAREA_PUSH);
    } catch {
        // Sin módulo nativo (Expo Go) o ya registrada.
    }
}
