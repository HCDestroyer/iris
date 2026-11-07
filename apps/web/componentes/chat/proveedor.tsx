"use client";

import { avisar } from "@iris/ui";
import type { EstadoChat, MotorChat, VistaConversacion } from "@iris/sync/chat";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { EliminacionesDiferidas, ESPERA_DESHACER_MS } from "@/lib/chat/eliminar-diferido";
import { abrirChatPanel } from "@/lib/chat/motor";
import { contadoresFiltros, hayPunto, type FiltroChat } from "@/lib/chat/presentacion";
import { eliminarAlSalir, mencionesDe, pedirResumen, type ResumenChat } from "@/lib/chat/api";
import { useSesion } from "@/lib/sesion";

/*
 * Chat del equipo en el panel: un solo motor (y una sola lista de conversaciones) para el panel
 * lateral, la página «Equipo» y la pestaña de la orden. Vive con la sesión y la empresa activa: al
 * cambiar de empresa se vuelve a montar.
 */

/** Cuánto puede durar «conectando» antes de decir «Reconectando…» (un flujo nuevo tarda ~1 s). */
const GRACIA_RECONEXION_MS = 4_000;
/** Respiro antes de volver a pedir `GET /resumen` tras un cambio (§2.1). */
const RESPIRO_RESUMEN_MS = 1_000;

export type EstadoEnVivo = "en_vivo" | "conectando" | "reconectando" | "sin_conexion" | "sesion";

type ContextoChatEquipo = {
    motor: MotorChat;
    estado: EstadoChat;
    yoId: number;
    puedeEnviar: boolean;
    puedeSupervisar: boolean;
    /** `GET /resumen`, o null si aún no llega o falló (entonces cuenta lo cargado). */
    resumen: ResumenChat | null;
    contadores: Record<FiltroChat, number>;
    /** Hay algo nuevo sin número (un punto). */
    punto: boolean;
    enVivo: EstadoEnVivo;
    reintentar: () => void;
    /** Mensajes que se ven eliminados mientras dura su «Deshacer». */
    eliminando: ReadonlySet<string>;
    eliminar: (mensajeId: string) => void;
    /** Panel lateral: abierto y la conversación que muestra (null = la lista). */
    panel: { abierto: boolean; conversacion: number | null };
    abrirPanel: (conversacion?: number | null) => void;
    cerrarPanel: () => void;
    elegirEnPanel: (conversacion: number | null) => void;
};

const Contexto = createContext<ContextoChatEquipo | null>(null);

/** El chat del equipo, o null sin `chat.ver` (o mientras arranca). */
export function useChatEquipo(): ContextoChatEquipo | null {
    return useContext(Contexto);
}

export function useConversacionChat(motor: MotorChat, id: number): VistaConversacion {
    const obtener = useCallback(() => motor.obtenerConversacion(id), [motor, id]);
    return useSyncExternalStore(motor.suscribir, obtener, obtener);
}

/** Monta el chat solo con `chat.ver` y la empresa resuelta; si no, deja pasar a los hijos. */
export function ProveedorChatEquipo({ children }: { children: ReactNode }) {
    const { yo, puede } = useSesion();
    const activo = puede(["chat.ver"]) && (yo.modoEmpresa === "sin_conector" || yo.empresaActiva !== undefined);
    if (!activo) return children;
    return (
        <ConMotor key={`${yo.usuario.id}-${yo.empresaActiva?.id ?? 0}`} usuarioId={yo.usuario.id} empresaId={yo.empresaActiva?.id}>
            {children}
        </ConMotor>
    );
}

function ConMotor({ usuarioId, empresaId, children }: { usuarioId: number; empresaId: number | undefined; children: ReactNode }) {
    const [abierto, setAbierto] = useState<MotorChat | null>(null);
    useEffect(() => {
        let vivo = true;
        let motor: MotorChat | null = null;
        void abrirChatPanel({ usuarioId, empresaId }).then((c) => {
            if (!vivo) return;
            motor = c;
            setAbierto(c);
        });
        return () => {
            vivo = false;
            motor?.detener();
        };
    }, [usuarioId, empresaId]);
    if (!abierto) return children;
    return (
        <ChatActivo motor={abierto} yoId={usuarioId}>
            {children}
        </ChatActivo>
    );
}

function ChatActivo({ motor, yoId, children }: { motor: MotorChat; yoId: number; children: ReactNode }) {
    const { puede } = useSesion();
    const estado = useSyncExternalStore(motor.suscribir, motor.obtenerEstado, motor.obtenerEstado);

    // Ciclo de vida: envío siempre; tiempo real solo con la pestaña a la vista.
    useEffect(() => {
        motor.establecerConexion(navigator.onLine);
        if (document.visibilityState === "visible") motor.primerPlano();
        else motor.segundoPlano();
        motor.iniciar();
        const enLinea = () => motor.establecerConexion(true);
        const sinLinea = () => motor.establecerConexion(false);
        const visible = () => (document.visibilityState === "visible" ? motor.primerPlano() : motor.segundoPlano());
        window.addEventListener("online", enLinea);
        window.addEventListener("offline", sinLinea);
        document.addEventListener("visibilitychange", visible);
        return () => {
            window.removeEventListener("online", enLinea);
            window.removeEventListener("offline", sinLinea);
            document.removeEventListener("visibilitychange", visible);
            motor.detener();
        };
    }, [motor]);

    // «Eliminar» con 5 s para «Deshacer» (§3.1).
    const [, setVersionEliminando] = useState(0);
    const eliminaciones = useMemo(
        () =>
            new EliminacionesDiferidas({
                ejecutar: (id) => {
                    void motor.eliminar(id).catch((e: unknown) => {
                        avisar({ tono: "error", titulo: "No se pudo eliminar el mensaje", descripcion: e instanceof Error ? e.message : undefined });
                    });
                },
                alSalir: eliminarAlSalir,
                alCambiar: () => setVersionEliminando((v) => v + 1),
            }),
        [motor],
    );
    useEffect(() => {
        const salir = () => eliminaciones.vaciarAlSalir();
        const oculta = () => {
            if (document.visibilityState === "hidden") salir();
        };
        window.addEventListener("pagehide", salir);
        document.addEventListener("visibilitychange", oculta);
        return () => {
            window.removeEventListener("pagehide", salir);
            document.removeEventListener("visibilitychange", oculta);
            salir();
        };
    }, [eliminaciones]);
    const eliminar = useCallback(
        (mensajeId: string) => {
            eliminaciones.pedir(mensajeId);
            avisar({
                tono: "info",
                titulo: "Mensaje eliminado",
                duracion: ESPERA_DESHACER_MS,
                accion: {
                    texto: "Deshacer",
                    alPresionar: () => {
                        if (eliminaciones.deshacer(mensajeId)) avisar({ titulo: "Mensaje restaurado" });
                    },
                },
            });
        },
        [eliminaciones],
    );

    // Contadores: `GET /resumen` con un respiro tras cada cambio de lo no leído (§2.1).
    const [resumen, setResumen] = useState<ResumenChat | null>(null);
    const claveNoLeidos = estado.conversaciones.map((c) => `${c.id}:${c.no_leidos}:${mencionesDe(c)}`).join(",");
    const ultimaRecarga = useRef(0);
    useEffect(() => {
        if (!estado.listo || estado.sesionCerrada) return undefined;
        const control = new AbortController();
        const t = setTimeout(() => {
            pedirResumen(control.signal)
                .then((r) => {
                    setResumen(r);
                    // Una mención nueva llega por el feed sin su contador por conversación: si el
                    // total no cuadra con lo cargado, se vuelve a leer la lista (como mucho cada 10 s).
                    const locales = estado.conversaciones.reduce((n, c) => n + Math.min(mencionesDe(c), Math.max(0, c.no_leidos)), 0);
                    if (r.menciones !== locales && Date.now() - ultimaRecarga.current > 10_000) {
                        ultimaRecarga.current = Date.now();
                        void motor.recargar();
                    }
                })
                .catch(() => {
                    if (!control.signal.aborted) setResumen(null);
                });
        }, RESPIRO_RESUMEN_MS);
        return () => {
            clearTimeout(t);
            control.abort();
        };
        // Solo cuando cambia lo no leído.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [motor, claveNoLeidos, estado.listo, estado.sesionCerrada]);

    // «En vivo» / «Reconectando…»: conectar es normal entre flujos; solo se avisa si dura.
    const [largo, setLargo] = useState(false);
    const conectando = estado.tiempoReal === "conectando";
    useEffect(() => {
        if (!conectando) {
            setLargo(false);
            return undefined;
        }
        const t = setTimeout(() => setLargo(true), GRACIA_RECONEXION_MS);
        return () => clearTimeout(t);
    }, [conectando]);
    const enVivo: EstadoEnVivo = estado.sesionCerrada
        ? "sesion"
        : !estado.enLinea || estado.tiempoReal === "sin_conexion"
          ? "sin_conexion"
          : conectando
            ? largo
                ? "reconectando"
                : "conectando"
            : estado.tiempoReal === "detenido"
              ? "conectando"
              : "en_vivo";

    const [panel, setPanel] = useState<{ abierto: boolean; conversacion: number | null }>({ abierto: false, conversacion: null });
    const abrirPanel = useCallback((conversacion?: number | null) => setPanel((p) => ({ abierto: true, conversacion: conversacion === undefined ? p.conversacion : conversacion })), []);
    const cerrarPanel = useCallback(() => setPanel((p) => ({ ...p, abierto: false })), []);
    const elegirEnPanel = useCallback((conversacion: number | null) => setPanel({ abierto: true, conversacion }), []);
    const reintentar = useCallback(() => {
        motor.reintentarAhora();
        void motor.recargar();
    }, [motor]);

    const puedeEnviar = puede(["chat.enviar"]);
    const puedeSupervisar = puede(["chat.supervisar"]);
    const eliminando = eliminaciones.pendientes;
    const valor = useMemo<ContextoChatEquipo>(
        () => ({
            motor,
            estado,
            yoId,
            puedeEnviar,
            puedeSupervisar,
            resumen,
            contadores: contadoresFiltros(estado.conversaciones, resumen),
            punto: hayPunto(estado.conversaciones),
            enVivo,
            reintentar,
            eliminando,
            eliminar,
            panel,
            abrirPanel,
            cerrarPanel,
            elegirEnPanel,
        }),
        [motor, estado, yoId, puedeEnviar, puedeSupervisar, resumen, enVivo, reintentar, eliminando, eliminar, panel, abrirPanel, cerrarPanel, elegirEnPanel],
    );
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
