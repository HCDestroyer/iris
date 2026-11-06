"use client";

import { Boton, Cargando, cn } from "@iris/ui";
import { AlertTriangle, ArrowLeftRight, Bot, FileText, MapPin, MessageCircle, Paperclip, PencilLine, RotateCw, Timer } from "lucide-react";
import { Fragment, useEffect, useLayoutEffect, useRef } from "react";
import type { Pendiente } from "@/lib/conversaciones/estado";
import { hora, separadorDia, tamanoLegible, textoEntrega, textoEvento } from "@/lib/conversaciones/presentacion";
import type { Mensaje } from "@/lib/conversaciones/tipos";
import { esEventoBot } from "@/lib/conversaciones/atencion";
import type { MensajeAutomatico } from "@/lib/conversaciones/contrato-11";
import { rutaDescargaAdjunto } from "@/lib/ordenes/adjuntos";
import { diaLocal } from "@/lib/conversaciones/presentacion";

type Props = {
    mensajes: readonly Mensaje[];
    pendientes: readonly Pendiente[];
    ahora: number;
    hayAnteriores: boolean;
    cargandoAnteriores: boolean;
    errorAnteriores: boolean;
    cargarAnteriores: () => void;
    reintentarPendiente: (p: Pendiente) => void;
};

/**
 * El hilo: burbujas del cliente a la izquierda, las nuestras a la derecha, notas internas en
 * amarillo punteado y los eventos de sistema centrados en una línea. Todo el texto es plano
 * (`white-space: pre-wrap`, nunca HTML). Se queda abajo al llegar mensajes si ya estabas abajo.
 */
export function Hilo({ mensajes, pendientes, ahora, hayAnteriores, cargandoAnteriores, errorAnteriores, cargarAnteriores, reintentarPendiente }: Props) {
    const caja = useRef<HTMLDivElement>(null);
    const abajo = useRef(true);
    const primero = mensajes[0]?.id;
    const altoAntes = useRef(0);

    // Al cargar anteriores, la vista no salta: se conserva la distancia al final.
    useLayoutEffect(() => {
        const el = caja.current;
        if (!el) return;
        if (abajo.current) {
            el.scrollTop = el.scrollHeight;
        } else if (altoAntes.current) {
            el.scrollTop += el.scrollHeight - altoAntes.current;
        }
        altoAntes.current = el.scrollHeight;
    }, [mensajes, pendientes, primero]);

    useEffect(() => {
        const el = caja.current;
        if (!el) return;
        const alDesplazar = () => {
            abajo.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
            altoAntes.current = el.scrollHeight;
        };
        el.addEventListener("scroll", alDesplazar, { passive: true });
        return () => el.removeEventListener("scroll", alDesplazar);
    }, []);

    // Separador de día antes del primer mensaje de cada día (hora de Guatemala).
    const separadores = mensajes.map((m, i) => {
        const anterior = mensajes[i - 1];
        return !anterior || diaLocal(Date.parse(anterior.creadoEn)) !== diaLocal(Date.parse(m.creadoEn)) ? separadorDia(m.creadoEn, ahora) : null;
    });
    return (
        <div
            ref={caja}
            role="log"
            aria-label="Mensajes"
            aria-live="polite"
            tabIndex={0}
            className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-[clamp(0.75rem,3cqw,2rem)] py-4 outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-inset"
        >
            {hayAnteriores ? (
                <div className="flex justify-center">
                    <Boton variante="fantasma" tamano="sm" onClick={cargarAnteriores} disabled={cargandoAnteriores}>
                        {cargandoAnteriores ? <Cargando texto="Cargando…" /> : errorAnteriores ? <RotateCw /> : null}
                        {cargandoAnteriores ? null : errorAnteriores ? "No se pudieron leer. Reintentar" : "Ver mensajes anteriores"}
                    </Boton>
                </div>
            ) : null}
            {mensajes.map((m, i) => {
                const separador = separadores[i];
                return (
                    <Fragment key={m.id}>
                        {separador ? <p className="self-center py-1.5 text-[0.71875rem] font-semibold tracking-wide text-muted-foreground uppercase">{separador}</p> : null}
                        <Burbuja m={m} />
                    </Fragment>
                );
            })}
            {pendientes.map((p) => (
                <div
                    key={p.id}
                    data-pendiente={p.id}
                    className={cn(
                        "flex max-w-[min(72%,35rem)] flex-col gap-0.5 self-end rounded-[0.875rem] rounded-br-sm px-3 py-2",
                        p.modo === "nota" ? "border border-dashed border-warning/50 bg-warning/10" : "bg-primary/10",
                    )}
                >
                    {p.modo === "nota" ? <EtiquetaNota /> : null}
                    <p className="text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{p.texto}</p>
                    {p.error ? (
                        <span className="flex flex-wrap items-center justify-end gap-x-2 text-xs text-error">
                            <AlertTriangle aria-hidden className="size-3.5" />
                            {p.error}
                            <Boton variante="enlace" className="text-xs" onClick={() => reintentarPendiente(p)}>
                                Reintentar
                            </Boton>
                        </span>
                    ) : (
                        <span className="self-end text-[0.71875rem] text-muted-foreground">Enviando…</span>
                    )}
                </div>
            ))}
        </div>
    );
}

function EtiquetaNota() {
    return (
        <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-warning">
            <PencilLine aria-hidden className="size-3.5" />
            Nota interna
        </span>
    );
}

function Burbuja({ m }: { m: Mensaje }) {
    if (m.direccion === "sistema") {
        const tipo = m.evento?.tipo ?? "";
        const bot = esEventoBot(tipo);
        const Icono = tipo === "bot_atendio" || tipo === "menu_elegido" ? Bot : tipo === "aviso_sin_atender" ? Timer : tipo === "pasada" ? ArrowLeftRight : MessageCircle;
        return (
            <p
                className={cn(
                    "flex max-w-full min-w-0 items-center gap-1.5 self-center rounded-full px-2.5 py-0.5 text-xs",
                    tipo === "bot_atendio" ? "border border-info/30 bg-info/10 text-info" : "bg-elevated text-muted-foreground",
                )}
                data-mensaje={m.id}
                data-evento={tipo || undefined}
                data-bot={bot || undefined}
            >
                <Icono aria-hidden className="size-3.5 shrink-0" />
                <span className="truncate">{m.evento ? textoEvento(m.evento) : (m.texto ?? "")}</span>
                <span className="shrink-0 tabular-nums">· {hora(m.creadoEn)}</span>
            </p>
        );
    }
    const propia = m.direccion === "saliente";
    const nota = m.direccion === "nota";
    // Fase 1.1: mensajes del bot (`automatico: "bot"`) con su etiqueta y borde punteado de información.
    const automatico = (m as MensajeAutomatico).automatico ?? null;
    const delBot = propia && automatico === "bot";
    const pie = [
        nota || (propia && !delBot) ? (m.autor?.nombre ?? (propia ? "Respuesta automática" : null)) : null,
        hora(m.creadoEn),
        propia ? textoEntrega(m) : null,
    ]
        .filter(Boolean)
        .join(" · ");
    return (
        <div
            data-mensaje={m.id}
            data-direccion={m.direccion}
            className={cn(
                "flex max-w-[min(72%,35rem)] min-w-0 flex-col gap-0.5 rounded-[0.875rem] px-3 py-2",
                m.direccion === "entrante" && "self-start rounded-bl-sm border border-border bg-card",
                propia && !delBot && "self-end rounded-br-sm bg-primary/10",
                delBot && "self-end rounded-br-sm border border-dashed border-info/40 bg-card",
                nota && "self-end rounded-br-sm border border-dashed border-warning/50 bg-warning/10",
                propia && m.estadoEntrega === "fallido" && "ring-1 ring-error/40",
            )}
        >
            {nota ? <EtiquetaNota /> : null}
            {delBot ? (
                <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-info" data-slot="etiqueta-bot">
                    <Bot aria-hidden className="size-3.5" />
                    Bot · mensaje automático
                </span>
            ) : null}
            {m.adjunto ? <Adjunto m={m} /> : null}
            {m.ubicacion ? (
                <a
                    className="flex items-center gap-1.5 text-sm text-primary underline-offset-2 hover:underline"
                    href={`https://www.openstreetmap.org/?mlat=${m.ubicacion.lat}&mlon=${m.ubicacion.lon}#map=17/${m.ubicacion.lat}/${m.ubicacion.lon}`}
                    target="_blank"
                    rel="noopener noreferrer"
                >
                    <MapPin aria-hidden className="size-4" />
                    Ver ubicación
                </a>
            ) : null}
            {m.plantilla ? <span className="text-[0.6875rem] font-semibold text-muted-foreground">Plantilla «{m.plantilla.codigo}»</span> : null}
            {m.texto ? <p className="text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{m.texto}</p> : null}
            {nota && (m.menciones?.length ?? 0) > 0 ? (
                <span className="text-xs text-muted-foreground [overflow-wrap:anywhere]">Para {(m.menciones ?? []).map((u) => `@${u.nombre}`).join(", ")}</span>
            ) : null}
            <span className={cn("self-end text-[0.71875rem] text-muted-foreground", m.estadoEntrega === "fallido" && "text-error")}>{pie}</span>
        </div>
    );
}

/** Imágenes que se muestran en línea. Otro `image/*` (un SVG, p. ej.) va como documento. */
const IMAGEN_EN_LINEA: ReadonlySet<string> = new Set(["image/jpeg", "image/png", "image/webp"]);

/**
 * Adjunto: miniatura si es imagen lista; si no, nombre, tamaño y estado («Descargando…»). El enlace
 * se arma siempre desde el id validado (`rutaDescargaAdjunto`), nunca desde la URL de los datos.
 */
function Adjunto({ m }: { m: Mensaje }) {
    const a = m.adjunto;
    if (!a) return null;
    const url = rutaDescargaAdjunto(a.id);
    const listo = a.estado === "completo" && url !== undefined;
    const imagen = IMAGEN_EN_LINEA.has(a.mime.toLowerCase());
    const nombre = imagen ? "Foto" : a.mime === "application/pdf" ? "Documento PDF" : "Archivo";
    if (listo && imagen) {
        return (
            <a href={url} target="_blank" rel="noopener noreferrer" className="block overflow-hidden rounded-lg" aria-label={`Abrir ${nombre.toLowerCase()} (${tamanoLegible(a.tamano)})`}>
                {/* La imagen viene del módulo de adjuntos (mismo origen): sin optimizar. */}
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={url} alt="" loading="lazy" className="max-h-64 w-auto max-w-full object-cover" />
            </a>
        );
    }
    return (
        <span className="flex min-w-0 items-center gap-2.5 rounded-lg bg-background-secondary p-2">
            <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-md bg-elevated text-muted-foreground">
                {imagen ? <Paperclip className="size-4" /> : <FileText className="size-4" />}
            </span>
            <span className="grid min-w-0">
                {listo ? (
                    <a href={url} target="_blank" rel="noopener noreferrer" className="truncate text-[0.8125rem] font-semibold text-primary hover:underline">
                        {nombre}
                    </a>
                ) : (
                    <span className="truncate text-[0.8125rem] font-semibold">{nombre}</span>
                )}
                <span className="truncate text-xs text-muted-foreground">
                    {tamanoLegible(a.tamano)} · {a.estado === "pendiente" || a.estado === "subiendo" ? "descargando de WhatsApp…" : a.estado === "rechazado" ? "no se pudo descargar" : "listo"}
                </span>
            </span>
        </span>
    );
}
