"use client";

import { Aviso, Boton, CampoBusqueda, Cargando, cn, Esqueleto, GrupoSegmentado } from "@iris/ui";
import { RotateCw, SquarePen, WifiOff } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { mensajeDeError } from "@/lib/api/errores";
import type { Filtros, Vista } from "@/lib/conversaciones/estado";
import { CLASE_PUNTO, tituloVisible, horaLista, iniciales, puntoSla, textoUltimo, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import type { EstadoFeed } from "@/lib/conversaciones/tiempo-real";
import type { Contadores, Conversacion } from "@/lib/conversaciones/tipos";
import type { Lectura } from "@/lib/lectura";
import { useDiferido } from "@/lib/diferido";
import { ErrorLectura } from "@/componentes/error-lectura";
import { useSesion } from "@/lib/sesion";
import { AvisoAtencion, esperaDe, LineaDisponibles } from "./atencion";
import { useAhora } from "./reloj";
import { BotonFiltros } from "./filtros";

type EstadoLista = { estado: "cargando" } | { estado: "error"; error: unknown } | { estado: "listo"; items: Conversacion[]; cursor: string | null };

type Props = {
    className?: string;
    filtros: Filtros;
    abierta: number | null;
    lista: EstadoLista;
    contadores: Lectura<Contadores>;
    cargarMas: () => void;
    cargandoMas: boolean;
    errorMas: unknown;
    reintentar: () => void;
    alFiltrar: (f: Filtros) => void;
    hrefDe: (id: number) => string;
    estadoFeed: EstadoFeed;
    detenido: "sin_sesion" | "sin_permiso" | "sesion_cambiada" | null;
    /** Tras «La atiendo» en una fila: la conversación tomada (se aplica y se abre). */
    alAtender: (c: Conversacion) => void;
};

/** Columna izquierda: título, filtros en un botón, búsqueda, Mías/Sin asignar/Todas y la lista. */
export function ListaConversaciones({ className, filtros, abierta, lista, contadores, cargarMas, cargandoMas, errorMas, reintentar, alFiltrar, hrefDe, estadoFeed, detenido, alAtender }: Props) {
    const ahora = useAhora(30_000);
    const { puede } = useSesion();
    // La búsqueda va a la URL (y al servidor) cuando dejas de escribir.
    const [texto, setTexto] = useState(filtros.q);
    const diferido = useDiferido(texto, 350);
    useEffect(() => {
        if (diferido.trim() !== filtros.q.trim()) alFiltrar({ ...filtros, q: diferido });
        // Solo cuando cambia lo escrito.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [diferido]);

    const c = contadores.estado === "listo" ? contadores.datos : null;
    const vistas: { valor: Vista; etiqueta: string; contador?: number }[] = [
        { valor: "mias", etiqueta: "Mías", ...(c ? { contador: c.mias } : {}) },
        { valor: "sin_asignar", etiqueta: "Sin asignar", ...(c ? { contador: c.sinAsignar } : {}) },
        { valor: "todas", etiqueta: "Todas", ...(c ? { contador: c.todas } : {}) },
    ];

    return (
        <section aria-label="Conversaciones" className={cn("min-h-0 min-w-0 flex-col border-r border-border bg-card", className)}>
            <div className="grid gap-2.5 border-b border-border px-3.5 pt-3.5 pb-2.5">
                <div className="flex min-w-0 items-center gap-2">
                    <h1 className="min-w-0 flex-1 truncate text-lg font-bold">Conversaciones</h1>
                    <IndicadorFeed estado={estadoFeed} detenido={detenido} />
                    <BotonFiltros filtros={filtros} contadores={c} alCambiar={alFiltrar} />
                    {puede(["conversaciones.iniciar"]) ? (
                        <Boton asChild variante="contorno" tamano="sm" className="h-8 shrink-0 px-2.5">
                            <Link href="/conversaciones/nueva" aria-label="Nueva conversación" title="Nueva conversación">
                                <SquarePen />
                                Nueva
                            </Link>
                        </Boton>
                    ) : null}
                </div>
                <CampoBusqueda etiqueta="Buscar conversaciones" etiquetaVisible={false} tamano="sm" placeholder="Buscar nombre, número o ticket" valor={texto} alCambiar={setTexto} maxLength={100} />
                <GrupoSegmentado
                    etiqueta="Qué conversaciones ver"
                    className="flex w-full flex-nowrap"
                    claseOpcion="flex-1 min-w-0"
                    valor={filtros.vista}
                    alCambiar={(vista) => alFiltrar({ ...filtros, vista })}
                    opciones={vistas}
                />
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain" data-slot="lista-conversaciones">
                {lista.estado === "cargando" ? (
                    <EsqueletoLista />
                ) : lista.estado === "error" ? (
                    <div className="p-3">
                        <ErrorLectura error={lista.error} reintentar={reintentar} que="las conversaciones" />
                    </div>
                ) : lista.items.length === 0 ? (
                    <p className="p-6 text-center text-sm text-muted-foreground">
                        {filtros.q.trim() ? `Nada coincide con «${filtros.q.trim()}».` : filtros.vista === "mias" ? "No tienes conversaciones asignadas." : "No hay conversaciones con estos filtros."}
                    </p>
                ) : (
                    <ul aria-label="Lista de conversaciones">
                        {lista.items.map((conv) => (
                            <li key={conv.id} className={cn("border-b border-border", conv.id === abierta && "bg-primary/5 shadow-[inset_3px_0_0_var(--hx-primary)]")}>
                                <FilaConversacion conv={conv} href={hrefDe(conv.id)} actual={conv.id === abierta} ahora={ahora} />
                                {esperaDe(conv) ? (
                                    <div className="grid gap-1.5 px-3.5 pb-2.5 pl-[3.75rem]">
                                        <AvisoAtencion conv={conv} ahora={ahora} compacto alHecho={alAtender} />
                                        <LineaDisponibles conv={conv} />
                                    </div>
                                ) : null}
                            </li>
                        ))}
                    </ul>
                )}
                {lista.estado === "listo" && lista.cursor ? (
                    <div className="grid gap-2 p-3">
                        {errorMas ? (
                            <Aviso tono="error" titulo="No se pudieron leer más">
                                {mensajeDeError(errorMas).titulo}
                            </Aviso>
                        ) : null}
                        <Boton variante="secundario" tamano="sm" onClick={cargarMas} disabled={cargandoMas}>
                            {cargandoMas ? <Cargando texto="Cargando…" /> : errorMas ? <RotateCw /> : null}
                            {cargandoMas ? null : errorMas ? "Reintentar" : "Cargar más"}
                        </Boton>
                    </div>
                ) : null}
            </div>
        </section>
    );
}

/** Fila de dos líneas: avatar con el punto del número, nombre y hora; último mensaje y estado. */
function FilaConversacion({ conv, href, actual, ahora }: { conv: Conversacion; href: string; actual: boolean; ahora: number }) {
    const tono = tonoDeCanal(conv.canal);
    const sla = puntoSla(conv.sla);
    const conNoLeidos = conv.noLeidos > 0;
    return (
        <Link
            href={href}
            scroll={false}
            aria-current={actual ? "true" : undefined}
            data-conversacion={conv.id}
            className={cn(
                "grid grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center gap-x-2.5 gap-y-0.5 px-3.5 py-2.5 outline-none",
                "hover:bg-elevated focus-visible:bg-elevated focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-inset",
            )}
        >
            <span aria-hidden className="relative row-span-2 flex size-9 items-center justify-center rounded-full bg-elevated text-[0.8125rem] font-semibold">
                {iniciales(conv.titulo)}
                <span className={cn("absolute -right-0.5 -bottom-0.5 size-3.5 rounded-full ring-2 ring-card", CLASE_PUNTO[tono])} />
            </span>
            <span className={cn("truncate text-[0.84375rem]", conNoLeidos ? "font-bold" : "font-semibold")}>{tituloVisible(conv)}</span>
            <span className="text-right text-xs whitespace-nowrap text-muted-foreground tabular-nums">{horaLista(conv.ultimaActividad, ahora)}</span>
            <span className={cn("truncate text-[0.8125rem]", conNoLeidos ? "text-foreground" : "text-muted-foreground")}>{textoUltimo(conv.ultimoMensaje)}</span>
            <span className="flex items-center justify-end gap-1.5 whitespace-nowrap">
                <span className={cn("size-2 rounded-full", sla.clase)} title={sla.texto} />
                <span className="sr-only">{sla.texto}.</span>
                {conv.asignado ? (
                    <span className="flex size-5 items-center justify-center rounded-full bg-primary/10 text-[0.5625rem] font-bold text-primary" title={`Asignada a ${conv.asignado.nombre}`}>
                        <span aria-hidden>{iniciales(conv.asignado.nombre)}</span>
                        <span className="sr-only">Asignada a {conv.asignado.nombre}.</span>
                    </span>
                ) : (
                    <span className="sr-only">Sin asignar.</span>
                )}
                {conNoLeidos ? (
                    <span className="min-w-[1.125rem] rounded-full bg-primary px-1.5 text-center text-[0.65625rem] leading-[1.125rem] font-bold text-primary-foreground tabular-nums">
                        {conv.noLeidos > 99 ? "99+" : conv.noLeidos}
                        <span className="sr-only"> sin leer</span>
                    </span>
                ) : null}
            </span>
        </Link>
    );
}

function EsqueletoLista() {
    return (
        <div aria-busy="true" className="grid gap-px">
            <span className="sr-only" role="status">
                Cargando conversaciones…
            </span>
            {Array.from({ length: 7 }, (_, i) => (
                <div key={i} className="flex items-center gap-2.5 px-3.5 py-3">
                    <Esqueleto className="size-9 rounded-full" />
                    <div className="grid flex-1 gap-1.5">
                        <Esqueleto className="h-3.5 w-2/3" />
                        <Esqueleto className="h-3 w-5/6" />
                    </div>
                </div>
            ))}
        </div>
    );
}

/**
 * Estado del tiempo real, discreto: un punto si todo va bien; crece con texto solo cuando hay que
 * saberlo (sin conexión) o actuar (la sesión cambió o se cerró).
 */
function IndicadorFeed({ estado, detenido }: { estado: EstadoFeed; detenido: Props["detenido"] }) {
    if (detenido) {
        const texto = detenido === "sesion_cambiada" ? "La sesión cambió" : detenido === "sin_permiso" ? "Sin permiso" : "Sesión cerrada";
        return (
            <Boton variante="secundario" tamano="sm" className="h-7 border-warning/50 px-2 text-xs text-warning" onClick={() => window.location.reload()} title="Recargar la página">
                <RotateCw />
                {texto}
            </Boton>
        );
    }
    if (estado === "sin_conexion") {
        return (
            <span role="status" className="inline-flex h-7 items-center gap-1 rounded-md bg-warning/10 px-2 text-xs font-medium whitespace-nowrap text-warning">
                <WifiOff aria-hidden className="size-3.5" />
                Sin conexión
            </span>
        );
    }
    const enVivo = estado === "en_vivo" || estado === "sondeo";
    return (
        <span role="status" className="inline-flex size-7 items-center justify-center" title={enVivo ? "Al día" : "Conectando…"}>
            <span className={cn("size-2 rounded-full", enVivo ? "bg-success" : "bg-border")} />
            <span className="sr-only">{enVivo ? "Conversaciones al día" : "Conectando…"}</span>
        </span>
    );
}
