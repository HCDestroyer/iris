"use client";

import type { EstadoChat, MotorChat, VistaConversacion } from "@iris/sync/chat";
import { createContext, useContext, useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { useCampo } from "./contexto";

type ContextoChat = { chat: MotorChat; estado: EstadoChat; /** Con `chat.enviar`: escribir, abrir y eliminar. */ puedeEnviar: boolean };

const Contexto = createContext<ContextoChat | null>(null);

export function useChat(): ContextoChat {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useChat fuera de ProveedorChat");
    }
    return c;
}

/** Estado del chat, o `null` fuera del proveedor (p. ej. en pruebas de otra pantalla). */
export function useChatOpcional(): ContextoChat | null {
    return useContext(Contexto);
}

/** Una conversación para pintar (`MotorChat.obtenerConversacion`, el mismo objeto hasta un cambio). */
export function useConversacion(id: number): VistaConversacion {
    const { chat } = useChat();
    const obtener = () => chat.obtenerConversacion(id);
    return useSyncExternalStore(chat.suscribir, obtener, obtener);
}

/** URL de objeto de una foto que solo está en el teléfono (se revoca al desmontar). */
export function useFotoLocal(id: string | null): string | null {
    const { chat } = useChat();
    const [url, setUrl] = useState<string | null>(null);
    useEffect(() => {
        if (!id) {
            return undefined;
        }
        let vivo = true;
        let creada: string | null = null;
        void chat.leerFotoLocal(id).then((bytes) => {
            if (vivo && bytes) {
                creada = URL.createObjectURL(new Blob([bytes as Uint8Array<ArrayBuffer>], { type: "image/jpeg" }));
                setUrl(creada);
            }
        });
        return () => {
            vivo = false;
            if (creada) {
                URL.revokeObjectURL(creada);
            }
        };
    }, [chat, id]);
    return url;
}

/**
 * El motor del chat vive con el del día: arranca el envío y, con la pestaña visible, el tiempo real
 * (SSE o sondeo); en segundo plano lo cierra. Abre los hilos de las órdenes y cuadrillas del día.
 */
export function ProveedorChat({ chat, puedeEnviar = true, children }: { chat: MotorChat; puedeEnviar?: boolean; children: ReactNode }) {
    const estado = useSyncExternalStore(chat.suscribir, chat.obtenerEstado, chat.obtenerEstado);
    const { estado: campo } = useCampo();

    useEffect(() => {
        chat.establecerConexion(navigator.onLine);
        if (document.visibilityState === "visible") {
            chat.primerPlano();
        } else {
            chat.segundoPlano();
        }
        chat.iniciar();
        const enLinea = () => chat.establecerConexion(true);
        const sinLinea = () => chat.establecerConexion(false);
        const visible = () => (document.visibilityState === "visible" ? chat.primerPlano() : chat.segundoPlano());
        window.addEventListener("online", enLinea);
        window.addEventListener("offline", sinLinea);
        document.addEventListener("visibilitychange", visible);
        return () => {
            window.removeEventListener("online", enLinea);
            window.removeEventListener("offline", sinLinea);
            document.removeEventListener("visibilitychange", visible);
            chat.detener();
        };
    }, [chat]);

    const claveDelDia = campo.ordenes
        .filter((o) => !["cerrada", "fallida", "cancelada"].includes(o.estado))
        .map((o) => `${o.id}:${o.cuadrillaId ?? 0}`)
        .join(",");
    useEffect(() => {
        const vivas = campo.ordenes.filter((o) => !["cerrada", "fallida", "cancelada"].includes(o.estado));
        chat.fijarDelDia({ ordenes: vivas.map((o) => o.id), cuadrillas: [...new Set(vivas.map((o) => o.cuadrillaId ?? 0))] });
        // Solo cuando cambian las órdenes vivas o sus cuadrillas.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [chat, claveDelDia]);

    const valor = useMemo(() => ({ chat, estado, puedeEnviar }), [chat, estado, puedeEnviar]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
