// Estado de la app de campo para las pantallas: el motor (y su estado con
// `useSyncExternalStore`), la vista actual, los avisos flotantes y los servicios nativos (cámara y
// GPS), que las pruebas reemplazan. Igual que `ProveedorCampo` de la PWA.
import { ErrorSync, type ColaPosiciones, type EstadoPosiciones, type EstadoSync, type MotorSync, type OrdenVista } from "@iris/sync";
import type { TonoEstado } from "@iris/sync/campo";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import type { SesionCampo } from "../sesion/sesion";
import type { FotoTomada } from "./foto";
import type { Posicion } from "./gps-nativo";
import type { EstadoRastreo, RastreoUbicacion } from "./ubicacion";

/** Pantalla visible: la lista del día, el detalle de una orden o lo pendiente por enviar. */
export type Vista = { tipo: "lista" } | { tipo: "orden"; id: number } | { tipo: "pendientes" };

export type Notificacion = { tono: TonoEstado; texto: string };

export interface ServiciosNativos {
    /** Cámara del sistema con compresión; `null` si se cancela. */
    tomarFoto: () => Promise<FotoTomada | null>;
    /** GPS para una acción si el permiso ya está dado (nunca pregunta). */
    posicion: (esperaMs?: number) => Promise<Posicion | null>;
    /** ¿Hay red? (la notificación dice «Enviando…» o «al volver la señal»). */
    enLinea: () => boolean;
}

export type CerrarSesion = (opciones: { conservarPendientes: boolean }) => Promise<void>;

interface ContextoCampo {
    motor: MotorSync;
    estado: EstadoSync;
    posiciones: ColaPosiciones | null;
    rastreo: RastreoUbicacion | null;
    estadoRastreo: EstadoRastreo;
    sesion: SesionCampo;
    servicios: ServiciosNativos;
    vista: Vista;
    navegar: (v: Vista) => void;
    notificacion: Notificacion | null;
    notificar: (n: Notificacion) => void;
    /** Corre una acción del técnico y avisa «Guardado en el teléfono» o el error. Devuelve si se guardó. */
    ejecutar: (accion: () => Promise<unknown>, guardado?: string) => Promise<boolean>;
    /** Miniaturas de las fotos tomadas en esta visita (uri por id de adjunto). */
    miniaturas: ReadonlyMap<string, string>;
    agregarMiniatura: (id: string, uri: string) => void;
    cerrarSesion: CerrarSesion;
}

const Contexto = createContext<ContextoCampo | null>(null);

export function useCampo(): ContextoCampo {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useCampo fuera de ProveedorCampo");
    }
    return c;
}

export function useOrden(id: number): OrdenVista | null {
    return useCampo().estado.ordenes.find((o) => o.id === id) ?? null;
}

export function useEstadoMotor(motor: MotorSync): EstadoSync {
    return useSyncExternalStore(motor.suscribir, motor.obtenerEstado, motor.obtenerEstado);
}

const sinCola = { suscribir: () => () => undefined, obtener: () => null };

export function useEstadoPosiciones(cola: ColaPosiciones | null): EstadoPosiciones | null {
    return useSyncExternalStore(cola?.suscribir ?? sinCola.suscribir, cola?.obtenerEstado ?? sinCola.obtener, cola?.obtenerEstado ?? sinCola.obtener);
}

export function mensajeError(e: unknown): string {
    if (e instanceof ErrorSync) {
        return e.message;
    }
    if (e instanceof Error && /full|space|quota|SQLITE_FULL/i.test(e.message)) {
        return "El teléfono no tiene espacio para guardar esto. Libera espacio y vuelve a intentarlo.";
    }
    return "No se pudo guardar en el teléfono. Vuelve a intentarlo.";
}

export function ProveedorCampo({
    motor,
    posiciones = null,
    rastreo = null,
    estadoRastreo = { tipo: "fuera_de_curso" },
    sesion,
    servicios,
    cerrarSesion,
    vistaInicial = { tipo: "lista" },
    children,
}: {
    motor: MotorSync;
    posiciones?: ColaPosiciones | null;
    rastreo?: RastreoUbicacion | null;
    estadoRastreo?: EstadoRastreo;
    sesion: SesionCampo;
    servicios: ServiciosNativos;
    cerrarSesion: CerrarSesion;
    vistaInicial?: Vista;
    children: ReactNode;
}) {
    const estado = useEstadoMotor(motor);
    const [vista, setVista] = useState<Vista>(vistaInicial);
    const [notificacion, setNotificacion] = useState<Notificacion | null>(null);
    const [miniaturas, setMiniaturas] = useState<ReadonlyMap<string, string>>(new Map());
    const temporizador = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(
        () => () => {
            if (temporizador.current) {
                clearTimeout(temporizador.current);
            }
        },
        [],
    );

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
                notificar({ tono: "success", texto: servicios.enLinea() ? `${guardado}. Enviando…` : `${guardado}. Se enviará al volver la señal.` });
                return true;
            } catch (e) {
                notificar({ tono: "error", texto: mensajeError(e) });
                return false;
            }
        },
        [notificar, servicios],
    );

    const agregarMiniatura = useCallback((id: string, uri: string) => {
        setMiniaturas((m) => new Map(m).set(id, uri));
    }, []);

    const valor = useMemo<ContextoCampo>(
        () => ({
            motor,
            estado,
            posiciones,
            rastreo,
            estadoRastreo,
            sesion,
            servicios,
            vista,
            navegar: setVista,
            notificacion,
            notificar,
            ejecutar,
            miniaturas,
            agregarMiniatura,
            cerrarSesion,
        }),
        [motor, estado, posiciones, rastreo, estadoRastreo, sesion, servicios, vista, notificacion, notificar, ejecutar, miniaturas, agregarMiniatura, cerrarSesion],
    );
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
