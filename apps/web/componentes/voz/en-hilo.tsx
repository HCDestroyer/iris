"use client";

import { Boton, cn } from "@iris/ui";
import { Clock, MessageSquare, Phone, PhoneCall, PhoneIncoming, PhoneMissed, PhoneOff, StickyNote, TriangleAlert, type LucideIcon } from "lucide-react";
import { hora } from "@/lib/conversaciones/presentacion";
import type { Mensaje } from "@/lib/conversaciones/tipos";
import { llamadaEnHilo, type IconoLlamada, type TonoLlamada } from "@/lib/voz/presentacion";
import { useDevoluciones } from "./devoluciones";

const ICONO: Record<IconoLlamada, LucideIcon> = {
    contestada: PhoneIncoming,
    perdida: PhoneMissed,
    rechazada: PhoneOff,
    fuera_de_horario: Clock,
    en_curso: PhoneCall,
    fallida: TriangleAlert,
};

const CLASE: Record<TonoLlamada, { caja: string; icono: string }> = {
    ok: { caja: "border-border bg-card", icono: "bg-success/15 text-success" },
    error: { caja: "border-error/40 bg-error/[0.06]", icono: "bg-error/15 text-error" },
    aviso: { caja: "border-warning/40 bg-warning/[0.06]", icono: "bg-warning/15 text-warning" },
    info: { caja: "border-primary/40 bg-primary/[0.06]", icono: "bg-primary/15 text-primary" },
    neutro: { caja: "border-border bg-card", icono: "bg-elevated text-muted-foreground" },
};

/**
 * «Escribirle»: lleva el cursor a donde se le escribe al cliente. Con la ventana de 24 h cerrada
 * (una llamada sola no la abre) solo se puede mandar una plantilla: el foco va a ese botón.
 */
function irAlCompositor(conversacionId: number) {
    const campo = document.getElementById(`compositor-${conversacionId}`) ?? document.querySelector<HTMLElement>('[data-slot="compositor"] [data-slot="solo-plantilla"] button');
    campo?.scrollIntoView({ block: "nearest" });
    campo?.focus();
}

/**
 * Una llamada en el hilo (`tipo: "llamada"`): tarjeta centrada con icono + texto + color. La
 * perdida es la única en rojo y trae qué hacer: «Escribirle» (devolver la llamada desde el panel
 * llega con las salientes) y, si sigue pendiente, «Descartar».
 */
export function LlamadaEnHilo({ m }: { m: Mensaje }) {
    const dev = useDevoluciones();
    if (!m.llamada) return null;
    const l = m.llamada;
    const v = llamadaEnHilo(l);
    const Icono = l.direccion === "saliente" && v.icono === "contestada" ? Phone : ICONO[v.icono];
    const clase = CLASE[v.tono];
    const pendiente = l.estado === "perdida" ? dev?.deLlamada(l.id) : undefined;
    return (
        <div
            data-mensaje={m.id}
            data-slot="llamada-hilo"
            data-estado={l.estado}
            data-tono={v.tono}
            className={cn("flex w-full max-w-[min(100%,26rem)] min-w-0 flex-col gap-2 self-center rounded-xl border p-3", clase.caja)}
        >
            <div className="flex min-w-0 items-center gap-2.5">
                <span aria-hidden className={cn("flex size-8 shrink-0 items-center justify-center rounded-full", clase.icono)}>
                    <Icono className="size-4" />
                </span>
                <div className="grid min-w-0 flex-1">
                    <p className={cn("truncate text-[0.84375rem] font-semibold", v.tono === "error" && "text-error")}>{v.titulo}</p>
                    <p className="truncate text-xs text-muted-foreground">
                        {v.detalle ? `${v.detalle} · ` : ""}
                        <span className="tabular-nums">{hora(m.creadoEn)}</span>
                    </p>
                </div>
                {l.tieneNota ? (
                    <span className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground" title="Tiene una nota (está en el hilo como nota interna)">
                        <StickyNote aria-hidden className="size-3.5" />
                        <span className="sr-only">Tiene una nota</span>
                    </span>
                ) : null}
            </div>
            {l.estado === "perdida" ? (
                <div className="flex flex-wrap gap-1.5">
                    <Boton variante="secundario" tamano="sm" onClick={() => irAlCompositor(m.conversacionId)}>
                        <MessageSquare />
                        Escribirle
                    </Boton>
                    {pendiente && dev?.puedeDescartar ? (
                        <Boton variante="fantasma" tamano="sm" onClick={() => dev.descartar(pendiente)}>
                            Descartar
                        </Boton>
                    ) : null}
                </div>
            ) : null}
        </div>
    );
}
