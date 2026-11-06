// `ServicioPush` sobre `expo-notifications` (Android). El servidor manda mensajes **solo de datos**
// (`contratos/push.md` §4): la app arma cada notificación con su canal, una por conversación u
// orden (los mensajes se apilan), sus botones y el `data` original (para leerla al tocarla).
//
// Lo que `expo-notifications` no permite (sin código nativo propio): `MessagingStyle` con avatar por
// remitente, `setPublicVersion` con un título propio y `setWhen`. En la pantalla bloqueada los
// canales van con visibilidad `PRIVATE`: Android muestra «Iris Campo» sin el contenido.
import * as Notifications from "expo-notifications";
import { Linking, Platform } from "react-native";
import type { AlmacenClaves } from "../entorno";
import type { RespuestaPush } from "./acciones";
import { borrarGrupo, guardarLineasDeGrupo, lineasDeGrupo } from "./locales";
import { ACCIONES, armarNotificacion, CANALES, CATEGORIAS, categoriaDe, leerMensajePush, type AccionPush, type MensajePush } from "./mensaje";
import type { PermisoPush, ServicioPush } from "./servicio";

/** Marca de las notificaciones que arma la app (las muestra el handler; un push crudo, no). */
const LOCAL = "__iris_local";
const PAQUETE = "gt.horus.iris.campo";

/** El `data` del mensaje (todo texto), de un push recibido o de una notificación nuestra. */
function dataDe(n: Notifications.Notification): Record<string, unknown> {
    const trigger = n.request.trigger as { remoteMessage?: { data?: Record<string, string> } } | null;
    return trigger?.remoteMessage?.data ?? (n.request.content.data as Record<string, unknown> | undefined) ?? {};
}

function permisoDe(p: Notifications.NotificationPermissionsStatus): PermisoPush {
    if (p.granted) {
        return "concedido";
    }
    return p.status === "denied" || !p.canAskAgain ? "denegado" : "sin_preguntar";
}

function respuestaDe(r: Notifications.NotificationResponse): RespuestaPush | null {
    const mensaje = leerMensajePush(r.notification.request.content.data as Record<string, unknown>);
    if (!mensaje) {
        return null;
    }
    const accion: AccionPush | "abrir" =
        r.actionIdentifier === Notifications.DEFAULT_ACTION_IDENTIFIER ? "abrir" : r.actionIdentifier in ACCIONES ? (r.actionIdentifier as AccionPush) : "abrir";
    return { accion, mensaje, ...(r.userText ? { texto: r.userText } : {}) };
}

/** Arma y muestra la notificación de un mensaje (también desde la tarea en segundo plano). */
export async function presentarMensaje(m: MensajePush, claves: AlmacenClaves): Promise<void> {
    const n = armarNotificacion(m, lineasDeGrupo(claves, m.grupo));
    if (n.lineas.length > 0) {
        guardarLineasDeGrupo(claves, m.grupo, n.lineas);
    }
    await Notifications.scheduleNotificationAsync({
        identifier: n.identificador,
        content: {
            title: n.titulo,
            body: n.cuerpo,
            data: { ...mensajeComoData(m), [LOCAL]: "1" },
            ...(n.categoria ? { categoryIdentifier: n.categoria } : {}),
            ...(m.tipo === "orden" || m.tipo === "mencion" ? { priority: Notifications.AndroidNotificationPriority.HIGH } : {}),
        },
        trigger: { channelId: n.canal },
    });
}

export async function quitarGrupoNativo(grupo: string, claves: AlmacenClaves): Promise<void> {
    borrarGrupo(claves, grupo);
    try {
        await Notifications.dismissNotificationAsync(grupo);
    } catch {
        // Ya no estaba.
    }
}

let preparado: Promise<void> | null = null;

/** Canales y categorías (una vez por proceso; repetir no cambia lo que el técnico ajustó). */
export function prepararNotificaciones(): Promise<void> {
    preparado ??= (async () => {
        if (Platform.OS === "android") {
            for (const c of CANALES) {
                await Notifications.setNotificationChannelAsync(c.id, {
                    name: c.nombre,
                    description: c.descripcion,
                    importance: c.alta ? Notifications.AndroidImportance.HIGH : Notifications.AndroidImportance.DEFAULT,
                    lockscreenVisibility: Notifications.AndroidNotificationVisibility.PRIVATE,
                    showBadge: true,
                    enableVibrate: true,
                });
            }
        }
        for (const acciones of CATEGORIAS) {
            const id = categoriaDe({ acciones });
            if (!id) {
                continue;
            }
            await Notifications.setNotificationCategoryAsync(
                id,
                acciones.map((a) => ({
                    identifier: a,
                    buttonTitle: ACCIONES[a],
                    // «Responder» y «Marcar como leído» no abren la app (la tarea en segundo plano los manda).
                    options: { opensAppToForeground: a !== "responder" && a !== "leido" },
                    ...(a === "responder" ? { textInput: { submitButtonTitle: "Enviar", placeholder: "Escribe tu respuesta" } } : {}),
                })),
            );
        }
    })().catch((e: unknown) => {
        preparado = null;
        throw e;
    });
    return preparado;
}

/**
 * Con la app al frente: un push crudo no se muestra solo (lo decide `alRecibir`); las notificaciones
 * que arma la app, sí.
 */
export function instalarManejador(): void {
    Notifications.setNotificationHandler({
        handleNotification: async (n) => {
            const nuestra = (n.request.content.data as Record<string, unknown> | undefined)?.[LOCAL] === "1";
            return { shouldShowBanner: nuestra, shouldShowList: nuestra, shouldPlaySound: nuestra, shouldSetBadge: false };
        },
    });
}

export function servicioPushNativo(claves: () => AlmacenClaves): ServicioPush {
    return {
        preparar: () => prepararNotificaciones(),
        permiso: async () => permisoDe(await Notifications.getPermissionsAsync()),
        pedirPermiso: async () => permisoDe(await Notifications.requestPermissionsAsync()),
        async token() {
            try {
                const t = await Notifications.getDevicePushTokenAsync();
                return typeof t.data === "string" && t.data ? t.data : null;
            } catch {
                // APK sin `google-services.json` (sin Firebase) o sin Google Play Services.
                return null;
            }
        },
        alCambiarToken(fn) {
            const s = Notifications.addPushTokenListener((t) => typeof t.data === "string" && fn(t.data));
            return () => s.remove();
        },
        alRecibir(fn) {
            const s = Notifications.addNotificationReceivedListener((n) => {
                const data = dataDe(n);
                if (data[LOCAL] !== "1") {
                    fn(data);
                }
            });
            return () => s.remove();
        },
        alResponder(fn) {
            // La que abrió la app cerrada (una sola vez) y las que lleguen con la app viva.
            const ultima = Notifications.getLastNotificationResponse();
            if (ultima) {
                Notifications.clearLastNotificationResponse();
                const r = respuestaDe(ultima);
                if (r) {
                    fn(r);
                }
            }
            const s = Notifications.addNotificationResponseReceivedListener((resp) => {
                const r = respuestaDe(resp);
                if (r) {
                    fn(r);
                }
            });
            return () => s.remove();
        },
        presentar: (m) => presentarMensaje(m, claves()),
        quitarGrupo: (grupo) => quitarGrupoNativo(grupo, claves()),
        async quitarTodas() {
            await Notifications.dismissAllNotificationsAsync();
        },
        async abrirAjustes() {
            try {
                await Linking.sendIntent("android.settings.APP_NOTIFICATION_SETTINGS", [{ key: "android.provider.extra.APP_PACKAGE", value: PAQUETE }]);
            } catch {
                await Linking.openSettings();
            }
        },
    };
}

/** El mensaje vuelto a la forma de `data` del contrato (para leerlo al tocar la notificación). */
export function mensajeComoData(m: MensajePush): Record<string, string> {
    return {
        v: "1",
        id: m.id,
        tipo: m.tipo,
        canal: m.tipo,
        usuarioId: String(m.usuarioId),
        empresaId: m.empresaId === null ? "" : String(m.empresaId),
        titulo: m.titulo,
        cuerpo: m.cuerpo,
        publicoTitulo: m.publicoTitulo,
        grupo: m.grupo,
        etiqueta: m.etiqueta,
        enlace: m.enlace,
        acciones: m.acciones.join(","),
        ocurridoEn: m.ocurridoEn,
        datos: JSON.stringify(m.datos),
    };
}
