// Chat interno en la app nativa: el `MotorChat` de `@iris/sync/chat` (el mismo de la PWA) y su
// estado para las pantallas. Arranca el envío al montarse; el sondeo solo con la app al frente
// (AppState), y la red de expo-network despierta la cola. Las fotos tomadas en esta visita guardan su
// uri para mostrarlas antes de subir.
import type { EstadoChat, MotorChat, VistaConversacion } from "@iris/sync/chat";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { useCampo } from "./contexto";

interface ContextoChat {
    chat: MotorChat;
    estado: EstadoChat;
    /** Con `chat.enviar`: escribir, abrir y eliminar. */
    puedeEnviar: boolean;
    /** uri local de una foto tomada en esta visita (por id de adjunto). */
    fotosLocales: ReadonlyMap<string, string>;
    agregarFotoLocal: (id: string, uri: string) => void;
}

const Contexto = createContext<ContextoChat | null>(null);

export function useChat(): ContextoChat {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useChat fuera de ProveedorChat");
    }
    return c;
}

/** `null` sin chat (sin `chat.ver`, o en pruebas de otras pantallas). */
export function useChatOpcional(): ContextoChat | null {
    return useContext(Contexto);
}

export function useConversacion(id: number): VistaConversacion {
    const { chat } = useChat();
    const obtener = useCallback(() => chat.obtenerConversacion(id), [chat, id]);
    return useSyncExternalStore(chat.suscribir, obtener, obtener);
}

export function ProveedorChat({
    chat,
    puedeEnviar = true,
    red,
    alCambiarPlano,
    children,
}: {
    chat: MotorChat;
    puedeEnviar?: boolean;
    red?: { leer: () => Promise<boolean>; escuchar: (fn: (enLinea: boolean) => void) => () => void };
    alCambiarPlano?: ((fn: (alFrente: boolean) => void) => () => void) | undefined;
    children: ReactNode;
}) {
    const estado = useSyncExternalStore(chat.suscribir, chat.obtenerEstado, chat.obtenerEstado);
    const { estado: campo } = useCampo();
    const [fotosLocales, setFotos] = useState<ReadonlyMap<string, string>>(new Map());
    const agregarFotoLocal = useCallback((id: string, uri: string) => setFotos((m) => new Map(m).set(id, uri)), []);

    useEffect(() => {
        chat.iniciar();
        void red?.leer().then((v) => chat.establecerConexion(v)).catch(() => undefined);
        const quitarRed = red?.escuchar((v) => chat.establecerConexion(v));
        const quitarPlano = alCambiarPlano?.((alFrente) => (alFrente ? chat.primerPlano() : chat.segundoPlano()));
        return () => {
            quitarRed?.();
            quitarPlano?.();
            chat.detener();
        };
    }, [chat, red, alCambiarPlano]);

    const vivas = campo.ordenes.filter((o) => !["cerrada", "fallida", "cancelada"].includes(o.estado));
    const clave = vivas.map((o) => `${o.id}:${o.cuadrillaId ?? 0}`).join(",");
    useEffect(() => {
        const ids = clave ? clave.split(",").map((x) => x.split(":").map(Number)) : [];
        chat.fijarDelDia({ ordenes: ids.map(([o]) => o ?? 0).filter((o) => o > 0), cuadrillas: [...new Set(ids.map(([, c]) => c ?? 0))] });
    }, [chat, clave]);

    const valor = useMemo(() => ({ chat, estado, puedeEnviar, fotosLocales, agregarFotoLocal }), [chat, estado, puedeEnviar, fotosLocales, agregarFotoLocal]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
