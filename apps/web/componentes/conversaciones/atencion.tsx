"use client";

import { avisar, Boton, Cargando, cn } from "@iris/ui";
import { Clock, Timer, UserCheck } from "lucide-react";
import { useState } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import { avanceEspera, minutosParaBot, textoCuentaRegresiva, textoDisponibles, textoEsperaDesde } from "@/lib/conversaciones/atencion";
import type { Conversacion, Espera } from "@/lib/conversaciones/tipos";
import { iniciales } from "@/lib/conversaciones/presentacion";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";

/** La espera de una conversación, si está sin asignar y no resuelta (contrato §14.6). */
export function esperaDe(conv: Conversacion): Espera | null {
    const e = conv.espera;
    return e && !conv.asignado && conv.estado !== "resuelta" ? e : null;
}

/** ¿Hay que preguntar «¿Alguien la atiende?»? (cuenta regresiva del bot en curso). */
export function pideAtencion(conv: Conversacion): boolean {
    const e = esperaDe(conv);
    return Boolean(e && e.botContestaEn && !e.botAtendio);
}

/**
 * «La atiendo»: es `POST …/tomar` (contrato §14.6). Si otra persona ya la tomó (409
 * `ya_asignada`) lo dice en el aviso, que sigue a la vista con lo que pasó.
 */
export function useAtender(conv: Conversacion, alHecho: (c: Conversacion) => void) {
    const [atendiendo, setAtendiendo] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const clave = useClaveIdempotencia();
    async function atender() {
        setAtendiendo(true);
        setError(null);
        try {
            const c = await resolver(
                api().POST("/api/v1/mensajeria/conversaciones/{id}/tomar", {
                    params: { path: { id: conv.id }, header: cabeceraIdem(clave.actual()) },
                    body: { version: conv.version },
                }),
            );
            clave.renovar();
            alHecho(c);
            avisar({ titulo: `Atiendes a ${conv.titulo}` });
        } catch (e) {
            // Sin respuesta: la misma clave (no se toma dos veces); con respuesta, una nueva.
            clave.trasError(e);
            setError(e instanceof ErrorApi && e.codigo === "ya_asignada" ? (e.detalle ?? "Ya la tomó otra persona") : mensajeDeError(e).titulo);
        } finally {
            setAtendiendo(false);
        }
    }
    return { atender, atendiendo, error };
}

type PropsAviso = {
    conv: Conversacion;
    ahora: number;
    alHecho: (c: Conversacion) => void;
    /** En la fila de la lista: una línea corta. */
    compacto?: boolean;
    className?: string;
};

/**
 * «¿Alguien la atiende?» con la cuenta regresiva hasta que contesta el bot y «La atiendo».
 * Naranja de alerta: pide actuar. Va en la fila de la lista y arriba de la conversación.
 */
export function AvisoAtencion({ conv, ahora, alHecho, compacto = false, className }: PropsAviso) {
    const { puede } = useSesion();
    const espera = esperaDe(conv);
    const { atender, atendiendo, error } = useAtender(conv, alHecho);
    if (!espera || !espera.botContestaEn || espera.botAtendio) return null;
    const min = minutosParaBot(espera.botContestaEn, ahora) ?? 0;
    const avance = avanceEspera(conv.sla.esperaDesde, espera.botContestaEn, ahora);
    const desde = textoEsperaDesde(conv.sla.esperaDesde, ahora);
    const nombres = espera.disponibles.map((p) => p.nombre.split(" ")[0]).join(", ");
    const boton = puede(["conversaciones.tomar"]) ? (
        <Boton tamano="sm" className="shrink-0" onClick={() => void atender()} disabled={atendiendo} aria-label={`La atiendo: ${conv.titulo}`}>
            {atendiendo ? <Cargando texto="Tomando…" /> : <UserCheck />}
            {atendiendo ? null : "La atiendo"}
        </Boton>
    ) : null;
    return (
        <div
            role="group"
            aria-label={`¿Alguien atiende a ${conv.titulo}?`}
            data-slot="aviso-atencion"
            className={cn("relative overflow-hidden rounded-lg border border-warning/40 bg-[color-mix(in_oklab,var(--hx-warning)_10%,var(--hx-bg))]", compacto ? "px-2.5 py-2" : "px-3 py-2.5", className)}
        >
            <div className="flex min-w-0 items-center gap-2.5">
                <Timer aria-hidden className="size-4 shrink-0 text-warning" />
                <div className="grid min-w-0 flex-1">
                    <span className={cn("text-[0.8125rem] leading-tight font-semibold text-warning", compacto ? "[overflow-wrap:anywhere]" : "truncate")}>¿Alguien la atiende?</span>
                    <span className="truncate text-xs text-foreground">
                        {compacto ? textoCuentaRegresiva(min, true) : [desde, nombres ? `Avisamos a ${nombres}` : null].filter(Boolean).join(" · ")}
                    </span>
                </div>
                {compacto ? null : (
                    <span className="hidden shrink-0 items-center gap-1 text-xs font-semibold whitespace-nowrap text-warning @min-[34rem]/conv:inline-flex" aria-live="polite">
                        <Clock aria-hidden className="size-3.5" />
                        {textoCuentaRegresiva(min)}
                    </span>
                )}
                {boton}
            </div>
            {compacto ? null : <span className="sr-only">{textoCuentaRegresiva(min)}.</span>}
            {error ? (
                <p role="alert" className="mt-1.5 text-xs font-medium text-error">
                    {error}
                </p>
            ) : null}
            {compacto ? null : (
                <span aria-hidden className="absolute inset-x-0 bottom-0 h-0.5 bg-warning/15">
                    <span className="block h-full bg-warning" style={{ width: `${Math.round(avance * 100)}%` }} />
                </span>
            )}
        </div>
    );
}

/**
 * «2 disponibles en Soporte» con sus iniciales y un punto verde, o «Nadie disponible en Cobros» en
 * naranja de alerta. Solo en conversaciones que esperan (sin asignar).
 */
export function LineaDisponibles({ conv, className }: { conv: Conversacion; className?: string }) {
    const espera = esperaDe(conv);
    if (!espera) return null;
    const cola = conv.cola?.nombre ?? "la empresa";
    const total = espera.totalDisponibles;
    return (
        <p data-slot="disponibles" className={cn("flex min-w-0 items-center gap-1.5 text-xs", total === 0 ? "font-medium text-warning" : "text-muted-foreground", className)}>
            {total > 0 ? (
                <span aria-hidden className="flex shrink-0 gap-1">
                    {espera.disponibles.slice(0, 3).map((p) => (
                        <span key={p.id} className="relative flex h-5 min-w-6 items-center justify-center rounded-full bg-elevated px-1 text-[0.5625rem] leading-none font-bold text-foreground">
                            {iniciales(p.nombre)}
                            <span className="absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-success ring-1 ring-card" />
                        </span>
                    ))}
                </span>
            ) : null}
            <span className="truncate">{textoDisponibles(total, cola)}</span>
        </p>
    );
}
