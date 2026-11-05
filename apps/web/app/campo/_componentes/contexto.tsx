"use client";

import { ErrorSync, type EstadoSync, type MotorSync, type OrdenVista } from "@iris/sync";
import type { Tono } from "@iris/ui";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { SesionCampo } from "@/lib/campo/sesion";
import { useEstadoMotor } from "@/lib/campo/motor";
import { urlDeVista, vistaDeUrl, type Vista } from "@iris/sync/campo";

export { urlDeVista, type Vista };

/** Acción del aviso que entra desde arriba («Deshacer», «Revisar»). */
export type AccionNotificacion = { texto: string; alPresionar: () => void };

export type Notificacion = { tono: Tono; texto: string; accion?: AccionNotificacion };

/** Cierra la sesión del técnico: borra el teléfono o conserva solo lo pendiente (cola y archivos). */
export type CerrarSesion = (opciones: { conservarPendientes: boolean }) => Promise<void>;

type ContextoCampo = {
    motor: MotorSync;
    estado: EstadoSync;
    sesion: SesionCampo;
    vista: Vista;
    navegar: (v: Vista) => void;
    notificar: (n: Notificacion) => void;
    notificacion: Notificacion | null;
    cerrarNotificacion: () => void;
    /**
     * Corre una acción del técnico: avisa «Guardado en el teléfono» (o el error). Devuelve si se
     * guardó. `deshacer` (con lo que devolvió la acción) agrega «Deshacer» al aviso.
     */
    ejecutar: <T>(accion: () => Promise<T>, guardado?: string, opciones?: { deshacer?: (resultado: T) => () => void }) => Promise<boolean>;
    /** Miniaturas de las fotos tomadas en esta visita (URL de objeto por id de adjunto). */
    miniaturas: ReadonlyMap<string, string>;
    agregarMiniatura: (id: string, datos: Blob) => void;
    cerrarSesion: CerrarSesion;
};

const Contexto = createContext<ContextoCampo | null>(null);

export function useCampo(): ContextoCampo {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useCampo fuera de ProveedorCampo");
    }
    return c;
}

export function useOrden(id: number): OrdenVista | null {
    const { estado } = useCampo();
    return estado.ordenes.find((o) => o.id === id) ?? null;
}

function mensajeError(e: unknown): string {
    if (e instanceof ErrorSync) {
        return e.message;
    }
    if (e instanceof Error && /quota/i.test(e.name + e.message)) {
        return "El teléfono no tiene espacio para guardar esto. Libera espacio y vuelve a intentarlo.";
    }
    return "No se pudo guardar en el teléfono. Vuelve a intentarlo.";
}

export function ProveedorCampo({
    motor,
    sesion,
    cerrarSesion,
    children,
}: {
    motor: MotorSync;
    sesion: SesionCampo;
    cerrarSesion: CerrarSesion;
    children: ReactNode;
}) {
    const estado = useEstadoMotor(motor);
    const [vista, setVista] = useState<Vista>({ tipo: "lista" });
    const [notificacion, setNotificacion] = useState<Notificacion | null>(null);
    const [miniaturas, setMiniaturas] = useState<ReadonlyMap<string, string>>(new Map());
    const temporizador = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => {
        const leer = () => setVista(vistaDeUrl(window.location.search));
        leer();
        window.addEventListener("popstate", leer);
        return () => window.removeEventListener("popstate", leer);
    }, []);

    useEffect(
        () => () => {
            for (const url of miniaturas.values()) {
                URL.revokeObjectURL(url);
            }
        },
        // Solo al desmontar.
        // eslint-disable-next-line react-hooks/exhaustive-deps
        [],
    );

    const navegar = useCallback((v: Vista) => {
        // pushState sin pasar por el router: la página es una sola y funciona sin señal. La misma
        // vista no se repite en el historial (así «Atrás» vuelve a la anterior de verdad).
        const url = urlDeVista(v);
        if (url !== window.location.pathname + window.location.search) {
            window.history.pushState(null, "", url);
        }
        setVista(v);
        window.scrollTo({ top: 0 });
    }, []);

    const cerrarNotificacion = useCallback(() => {
        if (temporizador.current) {
            clearTimeout(temporizador.current);
        }
        setNotificacion(null);
    }, []);

    const notificar = useCallback((n: Notificacion) => {
        setNotificacion(n);
        if (temporizador.current) {
            clearTimeout(temporizador.current);
        }
        // Con una acción («Deshacer», «Revisar») queda más tiempo: hay que alcanzar a tocarla.
        temporizador.current = setTimeout(() => setNotificacion(null), n.accion ? 8000 : 5000);
    }, []);

    const ejecutar = useCallback(
        async <T,>(accion: () => Promise<T>, guardado = "Guardado en el teléfono", opciones: { deshacer?: (resultado: T) => () => void } = {}) => {
            try {
                const resultado = await accion();
                const deshacer = opciones.deshacer?.(resultado);
                notificar({
                    tono: "success",
                    texto: navigator.onLine ? `${guardado}. Enviando…` : `${guardado}. Se enviará al volver la señal.`,
                    ...(deshacer ? { accion: { texto: "Deshacer", alPresionar: deshacer } } : {}),
                });
                return true;
            } catch (e) {
                notificar({ tono: "error", texto: mensajeError(e) });
                return false;
            }
        },
        [notificar],
    );

    const agregarMiniatura = useCallback((id: string, datos: Blob) => {
        setMiniaturas((m) => new Map(m).set(id, URL.createObjectURL(datos)));
    }, []);

    const valor = useMemo<ContextoCampo>(
        () => ({ motor, estado, sesion, vista, navegar, notificar, notificacion, cerrarNotificacion, ejecutar, miniaturas, agregarMiniatura, cerrarSesion }),
        [motor, estado, sesion, vista, navegar, notificar, notificacion, cerrarNotificacion, ejecutar, miniaturas, agregarMiniatura, cerrarSesion],
    );
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
