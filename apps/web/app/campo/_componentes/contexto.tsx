"use client";

import { ErrorSync, type EstadoSync, type MotorSync, type OrdenVista } from "@iris/sync";
import type { Tono } from "@iris/ui";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { SesionCampo } from "@/lib/campo/sesion";
import { useEstadoMotor } from "@/lib/campo/motor";
import { urlDeVista, vistaDeUrl, type Vista } from "@iris/sync/campo";

export { urlDeVista, type Vista };

export type Notificacion = { tono: Tono; texto: string };

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
    /**
     * Corre una acción del técnico: avisa «Guardado en el teléfono» (o el error). Devuelve si se
     * guardó.
     */
    ejecutar: (accion: () => Promise<unknown>, guardado?: string) => Promise<boolean>;
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
        // pushState sin pasar por el router: la página es una sola y funciona sin señal.
        window.history.pushState(null, "", urlDeVista(v));
        setVista(v);
        window.scrollTo({ top: 0 });
    }, []);

    const notificar = useCallback((n: Notificacion) => {
        setNotificacion(n);
        if (temporizador.current) {
            clearTimeout(temporizador.current);
        }
        temporizador.current = setTimeout(() => setNotificacion(null), 5000);
    }, []);

    const ejecutar = useCallback(
        async (accion: () => Promise<unknown>, guardado = "Guardado en el teléfono") => {
            try {
                await accion();
                notificar({ tono: "success", texto: navigator.onLine ? `${guardado}. Enviando…` : `${guardado}. Se enviará al volver la señal.` });
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
        () => ({ motor, estado, sesion, vista, navegar, notificar, notificacion, ejecutar, miniaturas, agregarMiniatura, cerrarSesion }),
        [motor, estado, sesion, vista, navegar, notificar, notificacion, ejecutar, miniaturas, agregarMiniatura, cerrarSesion],
    );
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
