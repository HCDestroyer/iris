"use client";

import { avisar } from "@iris/ui";
import { usePathname, useRouter } from "next/navigation";
import { createContext, use, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { nuevaClaveIdempotencia } from "../api/cliente";
import { ErrorApi } from "../api/errores";
import { ZONA } from "../fechas";
import { useLectura, type Lectura } from "../lectura";
import { rutaEntrar, useSesion } from "../sesion";
import { cambiarDisponibilidad, leerAgentes, leerDisponibilidad, mandarLatido } from "./api-equipo";
import type { Disponibilidad, EstadoDisponibilidad } from "./tipos";
import { crearLatido, esperaTrasFallos, tiemposLatido, type TrasFallo } from "./latido";

/*
 * Mi disponibilidad en la bandeja (fase 1.1): «Disponible» o «No disponible». Vive en el panel (el
 * control va junto a la cuenta, en la barra lateral y en «Más») y solo existe para quien tiene
 * `conversaciones.ver`.
 *
 * - Disponible, el panel manda un latido mientras estás activa (pestaña a la vista y algo de
 *   actividad hace poco), como mucho uno por minuto; oculta la pestaña o sin actividad, no manda
 *   nada y el servidor te pasa a «No disponible» al cumplirse los minutos de la empresa.
 * - Al cerrar sesión, el panel avisa al servidor antes de cerrarla.
 */

type ContextoDisponibilidad = {
    lectura: Lectura<Disponibilidad>;
    /** Colas de las que soy miembro o supervisora (para «te llegan Soporte y Cobros»). */
    colas: { id: number; nombre: string }[];
    cambiando: boolean;
    error: unknown;
    cambiar: (estado: EstadoDisponibilidad) => Promise<void>;
    /** Antes de cerrar sesión: «No disponible» en el servidor (sin esperar más de 2 s). */
    alSalir: () => Promise<void>;
};

const Contexto = createContext<ContextoDisponibilidad | null>(null);

/** Cada cuánto se relee el estado con la pestaña a la vista (más espaciado tras fallos). */
const RELECTURA_MS = 60_000;

/** Qué hacer tras un fallo de la API: esperar (red, 429, 5xx), detener (401) o seguir. */
export function clasificarFallo(error: unknown): TrasFallo {
    if (!(error instanceof ErrorApi)) return "esperar";
    if (error.estado === 401) return "detener";
    return error.estado === 0 || error.estado === 429 || error.estado >= 500 ? "esperar" : "seguir";
}

const EVENTOS_ACTIVIDAD = ["pointerdown", "keydown", "wheel", "touchstart", "mousemove", "scroll"] as const;

export function ProveedorDisponibilidad({ children }: { children: ReactNode }) {
    const { yo } = useSesion();
    const router = useRouter();
    const ruta = usePathname();
    const lectura = useLectura((signal) => leerDisponibilidad(signal));
    // Mis colas salen del catálogo de agentes (la disponibilidad no las trae).
    const agentes = useLectura((signal) => leerAgentes(signal));
    const colas = useMemo(() => {
        const mia = agentes.estado === "listo" ? agentes.datos.find((a) => a.id === yo.usuario.id) : undefined;
        const todas = [...(mia?.colas ?? []), ...(mia?.colasSupervisadas ?? [])];
        return todas.filter((c, i) => todas.findIndex((x) => x.id === c.id) === i);
    }, [agentes, yo.usuario.id]);
    const [cambiando, setCambiando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const disponible = lectura.estado === "listo" && lectura.datos.estado === "disponible";
    const minutos = lectura.estado === "listo" ? lectura.datos.minutosInactividad : 15;
    const { reemplazar, refrescar } = lectura;
    /** La sesión venció (401): se deja de leer y se pide entrar otra vez. */
    const rutaRef = useRef(ruta);
    const sesionVencida = useCallback(() => router.replace(rutaEntrar(rutaRef.current, "expirada")), [router]);
    const datos = lectura.estado === "listo" ? lectura.datos : null;
    const datosRef = useRef(datos);
    const colasRef = useRef(colas);
    useEffect(() => {
        datosRef.current = datos;
        colasRef.current = colas;
        rutaRef.current = ruta;
    });

    /** Aplica una disponibilidad leída solo si no es más vieja que la que se ve. */
    const aplicar = useCallback(
        (d: Disponibilidad) => {
            const actual = datosRef.current;
            if (actual && d.version < actual.version) return;
            reemplazar(d);
        },
        [reemplazar],
    );

    // Relectura sin pasar por «cargando»: el servidor me pudo pasar a «No disponible» (inactividad,
    // fuera de horario). Tras un fallo de red, 429 o 5xx se espera cada vez más (hasta 5 min); un 401
    // la detiene y lleva a entrar.
    const releer = useRef<() => void>(() => undefined);
    useEffect(() => {
        let vivo = true;
        let fallos = 0;
        let proxima = 0;
        let id = 0;
        const programar = (ms: number) => {
            window.clearTimeout(id);
            id = window.setTimeout(() => void leer(), ms);
        };
        async function leer() {
            if (!vivo) return;
            if (document.visibilityState !== "visible" || Date.now() < proxima) return programar(RELECTURA_MS);
            try {
                const d = await leerDisponibilidad();
                if (!vivo) return;
                fallos = 0;
                aplicar(d);
                programar(RELECTURA_MS);
            } catch (e) {
                if (!vivo) return;
                const que = clasificarFallo(e);
                if (que === "detener") {
                    vivo = false;
                    sesionVencida();
                    return;
                }
                if (que === "esperar") fallos += 1;
                const espera = que === "esperar" ? esperaTrasFallos(RELECTURA_MS, fallos) : RELECTURA_MS;
                proxima = Date.now() + espera;
                programar(espera);
            }
        }
        releer.current = () => {
            if (Date.now() >= proxima) programar(0);
        };
        programar(RELECTURA_MS);
        return () => {
            vivo = false;
            window.clearTimeout(id);
        };
    }, [aplicar, sesionVencida]);

    // Latido mientras estoy disponible y activa, con el intervalo que piden los minutos de la empresa.
    useEffect(() => {
        if (!disponible) return;
        const { intervaloMs, revisionMs, inactivoTrasMs } = tiemposLatido(minutos);
        const latido = crearLatido({
            // El latido responde la disponibilidad actual: si el servidor ya me pasó a «No disponible»,
            // se ve. Una respuesta que llega con el latido ya detenido, o más vieja, se ignora.
            enviar: () =>
                mandarLatido().then((r) => {
                    if (!latido.detenido()) aplicar(r);
                }),
            visible: document.visibilityState === "visible",
            intervaloMs,
            inactivoTrasMs,
            trasFallo: (e) => {
                const que = clasificarFallo(e);
                if (que === "detener") sesionVencida();
                return que;
            },
        });
        const alActuar = () => latido.actividad();
        const alVisibilidad = () => {
            latido.visibilidad(document.visibilityState === "visible");
            if (document.visibilityState === "visible") releer.current();
        };
        for (const e of EVENTOS_ACTIVIDAD) window.addEventListener(e, alActuar, { passive: true, capture: true });
        document.addEventListener("visibilitychange", alVisibilidad);
        const revision = window.setInterval(() => latido.revisar(), revisionMs);
        latido.actividad();
        return () => {
            latido.detener();
            for (const e of EVENTOS_ACTIVIDAD) window.removeEventListener(e, alActuar, { capture: true });
            document.removeEventListener("visibilitychange", alVisibilidad);
            window.clearInterval(revision);
        };
    }, [disponible, minutos, aplicar, sesionVencida]);

    const cambiar = useCallback(
        async (estado: EstadoDisponibilidad) => {
            setCambiando(true);
            setError(null);
            try {
                const d = await cambiarDisponibilidad(estado, datosRef.current?.version ?? 0, nuevaClaveIdempotencia());
                reemplazar(d);
                avisar({ titulo: textoAvisoDisponibilidad(d, colasRef.current) });
            } catch (e) {
                setError(e);
                // Cambió en otra pestaña o la cambió el servidor: se relee para ver la actual.
                if (e instanceof ErrorApi && e.estado === 409) void refrescar();
            } finally {
                setCambiando(false);
            }
        },
        [reemplazar, refrescar],
    );

    const alSalir = useCallback(async () => {
        if (datosRef.current?.estado !== "disponible") return;
        const version = datosRef.current.version;
        await Promise.race([cambiarDisponibilidad("no_disponible", version, nuevaClaveIdempotencia()).catch(() => undefined), new Promise((r) => setTimeout(r, 2000))]);
    }, []);

    const valor = useMemo(() => ({ lectura, colas, cambiando, error, cambiar, alSalir }), [lectura, colas, cambiando, error, cambiar, alSalir]);
    return <Contexto value={valor}>{children}</Contexto>;
}

/** La disponibilidad, o null si no hay bandeja (sin `conversaciones.ver`). */
export function useDisponibilidad(): ContextoDisponibilidad | null {
    return use(Contexto);
}

/** «Soporte y Cobros», «Soporte, Caja y Ventas». */
export function listaColas(colas: readonly { nombre: string }[]): string {
    const n = colas.map((c) => c.nombre);
    if (n.length <= 1) return n[0] ?? "";
    return `${n.slice(0, -1).join(", ")} y ${n.at(-1)}`;
}

export function textoAvisoDisponibilidad(d: Pick<Disponibilidad, "estado">, colas: readonly { nombre: string }[]): string {
    if (d.estado === "disponible") {
        return colas.length ? `Estás Disponible: te llegan ${listaColas(colas)}.` : "Estás Disponible.";
    }
    return "Estás No disponible: no te llegan conversaciones nuevas.";
}

const horaFmt = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, hour: "numeric", minute: "2-digit", hourCycle: "h23" });

/** «Pasas a No disponible tras 15 min sin actividad, al cerrar sesión o fuera de tu horario (hoy hasta las 22:00).» */
export function textoExplicacion(d: Pick<Disponibilidad, "minutosInactividad" | "horarioHasta" | "estado">): string {
    const hasta = d.estado === "disponible" && d.horarioHasta && !Number.isNaN(Date.parse(d.horarioHasta)) ? ` (hoy hasta las ${horaFmt.format(Date.parse(d.horarioHasta))})` : "";
    return `Pasas a No disponible tras ${d.minutosInactividad} min sin actividad, al cerrar sesión o fuera de tu horario${hasta}.`;
}

/** Por qué estoy «No disponible», si no lo elegí yo. */
export function textoMotivoPropio(d: Pick<Disponibilidad, "estado" | "origen" | "minutosInactividad">): string | null {
    if (d.estado === "disponible") return null;
    switch (d.origen) {
        case "inactividad":
            return `Pasaste a No disponible tras ${d.minutosInactividad} min sin actividad.`;
        case "horario":
            return "Terminó tu horario.";
        case "cierre_sesion":
            return "Se cerró tu sesión: vuelve a ponerte Disponible.";
        default:
            return null;
    }
}
