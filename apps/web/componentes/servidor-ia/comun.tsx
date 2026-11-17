"use client";

import { Boton, cn, EstadoVacio, Tarjeta } from "@iris/ui";
import { Bell, LayoutDashboard, Lock, Wrench, type LucideIcon } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { BotonCopiar } from "@/componentes/cuenta/boton-copiar";
import { ErrorApi } from "@/lib/api/errores";
import { leerTablero } from "@/lib/servidor-ia/api";
import { REFRESCO_MS, type Tono } from "@/lib/servidor-ia/presentacion";
import { PERMISO_ADMINISTRAR, PERMISO_VER, PERMISO_VER_USO, type Rango, type Tablero } from "@/lib/servidor-ia/tipos";
import { useSesion } from "@/lib/sesion";

export const RUTA = "/administracion/servidor-ia";

/**
 * Quién mira: quien administra la instalación (`global`, y además `administra`) o el admin de una
 * empresa (`uso`). Como la API: `servidor_ia.ver` y `.administrar` solo valen con `empresas.todas`
 * (§4.8); con el permiso pero sin `empresas.todas` no hay nada que ver.
 */
export function usePermisosServidorIa(): { global: boolean; administra: boolean; uso: boolean } {
    const { yo, puede } = useSesion();
    const global = puede([PERMISO_VER]) && yo.todasLasEmpresas;
    return { global, administra: global && puede([PERMISO_ADMINISTRAR]), uso: puede([PERMISO_VER_USO]) };
}

/**
 * La puerta de cada pantalla de la sección: `ver` deja pasar a quien administra la instalación o
 * al admin de una empresa; `administrar`, solo a quien la administra. Quien no pasa ve «Sin
 * permiso» y la pantalla no llega a montarse (no pide nada a la API).
 */
export function PuertaServidorIa({ nivel, children }: { nivel: "ver" | "administrar"; children: ReactNode }) {
    const { global, administra, uso } = usePermisosServidorIa();
    if (nivel === "administrar" ? administra : global || uso) return children;
    return (
        <Tarjeta data-slot="sin-permiso">
            <EstadoVacio icono={Lock} titulo="No tienes permiso para ver esta sección" descripcion="El servidor de IA lo administra quien ve todas las empresas de la instalación." />
        </Tarjeta>
    );
}

/** Un comando para pegar en el servidor, con «Copiar». */
export function Comando({ texto, className }: { texto: string; className?: string }) {
    return (
        <div data-slot="comando" className={cn("flex min-w-0 items-center gap-2 rounded-lg bg-elevated py-1.5 pr-1.5 pl-3", className)}>
            <code className="min-w-0 flex-1 font-mono text-[0.8125rem] [overflow-wrap:anywhere]">{texto}</code>
            <BotonCopiar texto={texto} />
        </div>
    );
}

const CLASE_TONO: Record<Tono, { punto: string; texto: string }> = {
    ok: { punto: "bg-success", texto: "text-foreground" },
    aviso: { punto: "bg-warning", texto: "text-warning" },
    error: { punto: "bg-error", texto: "text-error" },
    neutro: { punto: "bg-muted-foreground/60", texto: "text-foreground" },
};

/** La línea de estado: un punto discreto que crece solo cuando hay que actuar, y el texto llano. */
export function LineaEstado({ linea, estado }: { linea: { tono: Tono; titulo: string; detalle: string }; estado: string | null }) {
    const clase = CLASE_TONO[linea.tono];
    const actuar = linea.tono === "aviso" || linea.tono === "error";
    return (
        <p role="status" data-slot="estado-servidor" data-estado={estado ?? "sin_servidor"} className="flex min-w-0 items-start gap-2 text-sm">
            <span aria-hidden className={cn("mt-1.5 shrink-0 rounded-full", clase.punto, actuar ? "size-2.5" : "size-2")} />
            <span className="min-w-0">
                <span className={cn("font-semibold", clase.texto)}>{linea.titulo}</span>
                {linea.detalle ? <span className="text-muted-foreground"> · {linea.detalle}</span> : null}
            </span>
        </p>
    );
}

const SECCIONES: readonly { href: string; texto: string; icono: LucideIcon }[] = [
    { href: RUTA, texto: "Tablero", icono: LayoutDashboard },
    { href: `${RUTA}/avisos`, texto: "Avisos", icono: Bell },
    { href: `${RUTA}/mantenimiento`, texto: "Mantenimiento", icono: Wrench },
];

/** Las tres partes de la sección, solo para quien la administra (Avisos y Mantenimiento piden `servidor_ia.administrar`). */
export function NavServidorIa() {
    const ruta = usePathname();
    const { administra } = usePermisosServidorIa();
    if (!administra) return null;
    return (
        <nav aria-label="Secciones del Servidor de IA" className="-mx-1 mb-4 flex gap-1 overflow-x-auto px-1">
            {SECCIONES.map((s) => {
                const actual = ruta === s.href;
                return (
                    <Boton key={s.href} asChild variante={actual ? "secundario" : "fantasma"} tamano="sm" className="shrink-0">
                        <Link href={s.href as Route} aria-current={actual ? "page" : undefined}>
                            <s.icono />
                            {s.texto}
                        </Link>
                    </Boton>
                );
            })}
        </nav>
    );
}

export type LecturaTablero = { estado: "cargando" } | { estado: "error"; error: unknown } | { estado: "listo"; datos: Tablero };

/** Estos no se arreglan reintentando: sesión vencida, sin permiso o no existe. Queda «Reintentar» a mano. */
export const sinReintento = (error: unknown): boolean => error instanceof ErrorApi && (error.estado === 401 || error.estado === 403 || error.estado === 404);

/** Espera antes del siguiente intento tras `fallos` seguidos: 10 → 20 → 40 → 60 s (nunca menos que el ritmo del periodo). */
export function esperaTrasFallos(fallos: number, base: number): number {
    if (fallos <= 0) return base;
    return Math.max(base, Math.min(60_000, 10_000 * 2 ** (fallos - 1)));
}

/**
 * El tablero del rango, releído solo: cada 10 s en «Ahora» (más despacio en los rangos largos), sin
 * pasar por «cargando» (no parpadea), en pausa con la pestaña oculta y al momento al volver. Si una
 * relectura falla **no se quedan las cifras anteriores**: se dice el error y se sigue intentando,
 * cada vez más espaciado (vuelve a su ritmo al primer éxito). Con 401, 403 o 404 deja de insistir.
 */
export function useTablero(rango: Rango): { lectura: LecturaTablero; refrescar: () => void; ahora: number } {
    const [resultado, setResultado] = useState<{ rango: Rango; lectura: LecturaTablero }>({ rango, lectura: { estado: "cargando" } });
    const [ahora, setAhora] = useState(() => Date.now());
    const refrescar = useRef<() => void>(() => undefined);

    useEffect(() => {
        let control: AbortController | null = null;
        let vivo = true;
        let fallos = 0;
        let detenido = false;
        let reloj: number | undefined;
        const programar = () => {
            window.clearTimeout(reloj);
            if (!vivo || detenido) return;
            reloj = window.setTimeout(leer, esperaTrasFallos(fallos, REFRESCO_MS[rango]));
        };
        function leer() {
            window.clearTimeout(reloj);
            // Con la pestaña oculta no se lee ni se programa: al volver, `visibilitychange` lo reanuda.
            if (document.visibilityState !== "visible") return;
            control?.abort();
            const mio = new AbortController();
            control = mio;
            leerTablero(rango, mio.signal).then(
                (datos) => {
                    if (!vivo || mio.signal.aborted) return;
                    fallos = 0;
                    setAhora(Date.now());
                    setResultado({ rango, lectura: { estado: "listo", datos } });
                    programar();
                },
                (error: unknown) => {
                    if (!vivo || mio.signal.aborted) return;
                    fallos++;
                    detenido = sinReintento(error);
                    setResultado({ rango, lectura: { estado: "error", error } });
                    programar();
                },
            );
        }
        const alVolver = () => {
            if (!detenido) leer();
        };
        // «Reintentar» a mano siempre lee, también después de un 401, 403 o 404.
        refrescar.current = () => {
            detenido = false;
            leer();
        };
        leer();
        document.addEventListener("visibilitychange", alVolver);
        return () => {
            vivo = false;
            control?.abort();
            window.clearTimeout(reloj);
            document.removeEventListener("visibilitychange", alVolver);
        };
    }, [rango]);

    // Al cambiar de rango, lo del anterior no es de este: se carga de nuevo.
    const lectura: LecturaTablero = resultado.rango === rango ? resultado.lectura : { estado: "cargando" };
    return { lectura, refrescar: () => refrescar.current(), ahora };
}
