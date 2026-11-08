"use client";

import { Aviso, Boton, Cargando, cn, type Tono } from "@iris/ui";
import { CircleCheck, CircleX, Clock, Info, KeyRound, PencilLine, RefreshCw, RotateCw, type LucideIcon } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { mensajeDeError } from "@/lib/api/errores";
import { presentarPrueba, type AccionPrueba, type LineaPrueba } from "@/lib/numeros/conexion";
import type { PruebaCanal } from "@/lib/numeros/tipos";

const FONDO: Record<Tono, string> = {
    success: "bg-[color-mix(in_oklab,var(--hx-success)_10%,var(--hx-card))]",
    warning: "bg-[color-mix(in_oklab,var(--hx-warning)_10%,var(--hx-card))]",
    error: "bg-[color-mix(in_oklab,var(--hx-error)_10%,var(--hx-card))]",
    info: "bg-[color-mix(in_oklab,var(--hx-info)_10%,var(--hx-card))]",
    neutral: "bg-background-secondary",
};
const TEXTO: Record<Tono, string> = { success: "text-success", warning: "text-warning", error: "text-error", info: "text-info", neutral: "text-muted-foreground" };
const ICONO_TONO: Record<Tono, LucideIcon> = { success: CircleCheck, warning: Clock, error: CircleX, info: Info, neutral: Info };
const ICONO_LINEA: Record<LineaPrueba["icono"], LucideIcon> = { check: CircleCheck, x: CircleX, reloj: Clock, info: Info };

export type AccionesPrueba = {
    alVolverAComprobar: () => void;
    alCambiarToken?: () => void;
    /** Pantalla de credenciales (corregir número o Phone number ID). */
    credencialesHref: string;
    listaHref: string;
};

/**
 * Resultado de «Probar conexión» (`POST /canales/{id}/probar`): encabezado con el estado y la
 * acción que toca, y las dos líneas (credenciales de Meta y webhook). Si la prueba no se pudo
 * hacer (red, 5xx), el error con «Reintentar»; nunca un estado inventado.
 */
export function TarjetaPrueba({
    prueba,
    error,
    probando,
    numero,
    acciones,
}: {
    prueba: PruebaCanal | null;
    error: unknown;
    probando: boolean;
    numero: string;
    acciones: AccionesPrueba;
}) {
    if (probando) {
        return (
            <div className="surface-card flex items-center gap-3 p-4 sm:p-5" aria-live="polite">
                <Cargando texto="Probando la conexión con Meta…" />
            </div>
        );
    }
    if (error) {
        return (
            <Aviso
                tono="error"
                titulo="No se pudo probar la conexión"
                accion={
                    <Boton variante="contorno" tamano="sm" onClick={acciones.alVolverAComprobar}>
                        <RotateCw aria-hidden />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(error).titulo}. El número quedó guardado.
            </Aviso>
        );
    }
    if (!prueba) return null;
    const p = presentarPrueba(prueba, numero);
    const Icono = ICONO_TONO[p.tono];
    return (
        <section aria-labelledby="titulo-prueba" data-prueba={prueba.estado} data-conexion={prueba.conexion} className="surface-card overflow-hidden p-0" aria-live="polite">
            <div className={cn("flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:p-5", FONDO[p.tono])}>
                <div className="flex min-w-0 flex-1 items-start gap-3">
                    <span className={cn("grid size-10 shrink-0 place-items-center rounded-full bg-card", TEXTO[p.tono])}>
                        <Icono aria-hidden className="size-5" />
                    </span>
                    <div className="grid min-w-0 gap-0.5">
                        <p className="text-sm text-muted-foreground">Probar conexión</p>
                        <h2 id="titulo-prueba" className={cn("text-lg font-semibold", TEXTO[p.tono])}>
                            {p.titulo}
                        </h2>
                        <p className="text-sm text-muted-foreground">{p.texto}</p>
                    </div>
                </div>
                <div className="shrink-0">{accionPrincipal(p.accion, acciones)}</div>
            </div>
            <ul className="divide-y divide-border border-t border-border">
                <Linea linea={p.credenciales} />
                <Linea linea={p.webhook} />
            </ul>
        </section>
    );
}

function accionPrincipal(accion: AccionPrueba, a: AccionesPrueba): ReactNode {
    switch (accion) {
        case "lista":
            return (
                <Boton asChild variante="contorno">
                    <Link href={a.listaHref}>Ir a la lista</Link>
                </Boton>
            );
        case "cambiar_token":
            return a.alCambiarToken ? (
                <Boton onClick={a.alCambiarToken}>
                    <KeyRound aria-hidden />
                    Cambiar token
                </Boton>
            ) : (
                <Boton asChild>
                    <Link href={a.credencialesHref}>
                        <KeyRound aria-hidden />
                        Cambiar token
                    </Link>
                </Boton>
            );
        case "corregir_datos":
            return (
                <Boton asChild>
                    <Link href={a.credencialesHref}>
                        <PencilLine aria-hidden />
                        Corregir datos
                    </Link>
                </Boton>
            );
        default:
            return (
                <Boton onClick={a.alVolverAComprobar}>
                    <RefreshCw aria-hidden />
                    Volver a comprobar
                </Boton>
            );
    }
}

function Linea({ linea }: { linea: LineaPrueba }) {
    const Icono = ICONO_LINEA[linea.icono];
    return (
        <li className="flex items-start gap-3 px-4 py-3 sm:px-5">
            <Icono aria-hidden className={cn("mt-0.5 size-4 shrink-0", TEXTO[linea.tono])} />
            <div className="grid min-w-0 gap-0.5 text-sm">
                <p className="font-medium">{linea.titulo}</p>
                <p className="text-muted-foreground [overflow-wrap:anywhere]">{linea.texto}</p>
            </div>
        </li>
    );
}
