"use client";

import { Aviso, Boton, CampoBusqueda, cn, Esqueleto, GrupoSegmentado } from "@iris/ui";
import { horaLista, vistaPrevia, type ConversacionVista } from "@iris/sync/chat";
import { Lock, RotateCw } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { useAhora } from "@/componentes/tickets/insignias";
import { filtrarConversaciones, tituloChat, type FiltroChat } from "@/lib/chat/presentacion";
import { soloLecturaDe } from "@/lib/chat/api";
import { AvatarChat, InsigniaChat } from "./comunes";
import { useChatEquipo } from "./proveedor";

const ETIQUETAS_FILTRO: Record<FiltroChat, string> = { todas: "Todas", directas: "Directas", cuadrillas: "Cuadrillas", ordenes: "Órdenes" };

type Props = {
    seleccionada: number | null;
    filtro: FiltroChat;
    alFiltrar: (f: FiltroChat) => void;
    busqueda: string;
    alBuscar: (q: string) => void;
    /** Con enlace (página: la conversación va en la URL) o con botón (panel lateral). */
    hrefDe?: (id: number) => string;
    alElegir?: (id: number) => void;
    /** Título, «En vivo» y acciones de arriba. */
    encabezado: ReactNode;
    className?: string;
};

/** Lista de conversaciones del equipo: búsqueda, Todas / Directas / Cuadrillas / Órdenes y filas. */
export function ListaChat({ seleccionada, filtro, alFiltrar, busqueda, alBuscar, hrefDe, alElegir, encabezado, className }: Props) {
    const chat = useChatEquipo();
    const ahora = useAhora(30_000);
    if (!chat) return null;
    const { estado, contadores, yoId } = chat;
    const filas = filtrarConversaciones(estado.conversaciones, filtro, busqueda);
    const cargando = !estado.listo || (estado.lista.estado !== "lista" && estado.lista.estado !== "error" && estado.conversaciones.length === 0);
    return (
        <section aria-label="Conversaciones del equipo" className={cn("flex min-h-0 min-w-0 flex-col bg-card", className)}>
            <div className="grid gap-2.5 border-b border-border px-3.5 pt-3.5 pb-2.5">
                {encabezado}
                <CampoBusqueda etiqueta="Buscar en el chat del equipo" etiquetaVisible={false} tamano="sm" placeholder="Buscar persona, cuadrilla u orden" valor={busqueda} alCambiar={alBuscar} maxLength={80} />
                <GrupoSegmentado
                    etiqueta="Qué conversaciones ver"
                    className="flex w-full flex-nowrap"
                    claseOpcion="flex-1 min-w-0 px-1.5"
                    valor={filtro}
                    alCambiar={alFiltrar}
                    opciones={(Object.keys(ETIQUETAS_FILTRO) as FiltroChat[]).map((f) => ({ valor: f, etiqueta: ETIQUETAS_FILTRO[f], ...(contadores[f] > 0 ? { contador: contadores[f] } : {}) }))}
                />
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain" data-slot="lista-chat">
                {estado.lista.estado === "error" && estado.conversaciones.length === 0 ? (
                    <div className="p-3">
                        <Aviso
                            tono="error"
                            titulo="No se pudo leer el chat del equipo"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={chat.reintentar}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        >
                            {estado.lista.error?.mensaje ?? "Revisa tu conexión."}
                        </Aviso>
                    </div>
                ) : cargando ? (
                    <EsqueletoLista />
                ) : filas.length === 0 ? (
                    <p className="p-6 text-center text-sm text-muted-foreground">
                        {busqueda.trim() ? `Nada coincide con «${busqueda.trim()}».` : filtro === "todas" ? "Aún no tienes conversaciones del equipo." : `No tienes conversaciones de ${ETIQUETAS_FILTRO[filtro].toLowerCase()}.`}
                    </p>
                ) : (
                    <ul aria-label="Lista de conversaciones del equipo">
                        {filas.map((c) => (
                            <li key={c.id} className={cn("border-b border-border", c.id === seleccionada && "bg-primary/5 shadow-[inset_3px_0_0_var(--hx-primary)]")}>
                                <Fila c={c} actual={c.id === seleccionada} ahora={ahora} yoId={yoId} {...(hrefDe ? { href: hrefDe(c.id) } : {})} {...(alElegir ? { alElegir } : {})} />
                            </li>
                        ))}
                    </ul>
                )}
            </div>
        </section>
    );
}

const CLASES_FILA = cn(
    "grid w-full grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 px-3.5 py-2.5 text-left outline-none",
    "hover:bg-elevated focus-visible:bg-elevated focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-inset",
);

function Fila({ c, actual, ahora, yoId, href, alElegir }: { c: ConversacionVista; actual: boolean; ahora: number; yoId: number; href?: string; alElegir?: (id: number) => void }) {
    const titulo = tituloChat(c);
    const nuevo = c.no_leidos > 0;
    const contenido = (
        <>
            <AvatarChat c={c} nombre={titulo} className="row-span-2" />
            <span className={cn("truncate text-[0.875rem]", nuevo ? "font-bold" : "font-semibold")}>{titulo}</span>
            <span className="text-right text-xs whitespace-nowrap text-muted-foreground tabular-nums">{horaLista(c.ultimo_mensaje?.enviado_en ?? c.ultima_actividad, ahora)}</span>
            <span className={cn("truncate text-[0.8125rem]", nuevo ? "text-foreground" : "text-muted-foreground")}>{vistaPrevia(c, yoId)}</span>
            <span className="flex items-center justify-end gap-1.5 whitespace-nowrap">
                {soloLecturaDe(c) ? (
                    <span className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground">
                        <Lock aria-hidden className="size-3.5" />
                        Cerrada
                    </span>
                ) : null}
                <InsigniaChat c={c} />
            </span>
        </>
    );
    if (href) {
        return (
            <Link href={href} scroll={false} aria-current={actual ? "true" : undefined} data-conversacion-chat={c.id} className={CLASES_FILA}>
                {contenido}
            </Link>
        );
    }
    return (
        <Boton
            variante="fantasma"
            aria-current={actual ? "true" : undefined}
            data-conversacion-chat={c.id}
            className={cn(CLASES_FILA, "h-auto justify-stretch rounded-none font-normal")}
            onClick={() => alElegir?.(c.id)}
        >
            {contenido}
        </Boton>
    );
}

function EsqueletoLista() {
    return (
        <div aria-busy="true" className="grid gap-px">
            <span className="sr-only" role="status">
                Cargando el chat del equipo…
            </span>
            {Array.from({ length: 6 }, (_, i) => (
                <div key={i} className="flex items-center gap-3 px-3.5 py-3">
                    <Esqueleto className="size-10 rounded-full" />
                    <div className="grid flex-1 gap-1.5">
                        <Esqueleto className="h-3.5 w-2/3" />
                        <Esqueleto className="h-3 w-5/6" />
                    </div>
                </div>
            ))}
        </div>
    );
}
