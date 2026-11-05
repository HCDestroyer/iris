"use client";
// Chat interno en la PWA: el `MotorChat` de `@iris/sync/chat` sobre la misma base IndexedDB del
// motor (una por usuario y empresa) y, en tiempo real, `EventSource` con la cookie de la sesión.
import type { ClienteIris } from "@iris/contratos";
import { crearSubidorApi, type IdentidadApp } from "@iris/sync";
import { crearTransporteChatApi, MotorChat, type AbrirFlujoChat, type EventoChat } from "@iris/sync/chat";
import { AlmacenDexie } from "@iris/sync/dexie";

const TIPOS_EVENTO: readonly EventoChat["tipo"][] = ["mensaje_nuevo", "mensaje_eliminado", "leido", "conversacion_actualizada"];

/** Prefijo de las bases de la app de campo (`iris-campo-<usuario>-<empresa>`). */
const PREFIJO_BASE = "iris-campo-";

/**
 * `GET /chat/eventos` con `EventSource` (la cookie va sola). EventSource no dice el status de un
 * error ni deja fijar cabeceras: ante cualquier error se cierra y avisa, y el motor hace un sondeo
 * de `/cambios` que pone al día y descubre 410/401/403 antes de abrir otro desde el último id
 * (`?desde=`). Así nunca quedan dos flujos abiertos (tope de 5 por usuario).
 */
export const flujoEventSource: AbrirFlujoChat = (desde, oyentes) => {
    const url = `/api/v1/mensajeria/chat/eventos${desde ? `?desde=${encodeURIComponent(desde)}` : ""}`;
    const fuente = new EventSource(url, { withCredentials: true });
    let cerrado = false;
    const cerrar = () => {
        if (!cerrado) {
            cerrado = true;
            fuente.close();
        }
    };
    fuente.onopen = () => oyentes.alAbrir();
    const alMensaje = (e: MessageEvent<string>) => {
        try {
            oyentes.alEvento(JSON.parse(e.data) as EventoChat);
        } catch {
            // Un evento ilegible no rompe el flujo (y su contenido nunca se registra).
        }
    };
    for (const tipo of TIPOS_EVENTO) {
        fuente.addEventListener(tipo, alMensaje as EventListener);
    }
    fuente.onerror = () => {
        cerrar();
        oyentes.alCerrar();
    };
    return { cerrar };
};

export interface OpcionesChatPwa {
    usuarioId: number;
    almacen: AlmacenDexie;
    cliente: ClienteIris;
    app: IdentidadApp;
    verificarIdentidad: () => Promise<void>;
    /**
     * EventSource no manda `X-Iris-Empresa`: con varias empresas el flujo no sabría cuál es la
     * activa (pedido a I1 en `i4.md`), así que solo con una (o sin conector); si no, sondeo.
     */
    conFlujo: boolean;
}

export function abrirChat(o: OpcionesChatPwa): Promise<MotorChat> {
    return MotorChat.abrir({
        almacen: o.almacen,
        transporte: crearTransporteChatApi(o.cliente, o.app),
        subidor: crearSubidorApi(o.cliente, o.app),
        usuarioId: o.usuarioId,
        verificarIdentidad: o.verificarIdentidad,
        abrirFlujo: o.conFlujo && typeof EventSource !== "undefined" ? flujoEventSource : null,
    });
}

/**
 * «¿No eres tú?» en la entrada: borra la caché del chat (conversaciones y mensajes) de todas las
 * bases de campo de este navegador. La cola de salida y la de órdenes no se tocan: salen cuando su
 * técnico vuelva a entrar.
 */
export async function borrarCachesChat(): Promise<void> {
    const listar = (indexedDB as IDBFactory & { databases?: () => Promise<{ name?: string }[]> }).databases;
    const nombres = listar ? (await listar.call(indexedDB)).map((d) => d.name ?? "").filter((n) => n.startsWith(PREFIJO_BASE)) : [];
    for (const nombre of nombres) {
        if (!(await AlmacenDexie.existe(nombre))) {
            continue;
        }
        const almacen = new AlmacenDexie({ nombre });
        try {
            await almacen.borrarCacheChat();
        } finally {
            almacen.cerrar();
        }
    }
}
