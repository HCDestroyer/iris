"use client";

import { avisar, Boton, Cargando, cn, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento, MenuDesplegableEtiqueta, MenuDesplegableSeparador } from "@iris/ui";
import { CircleCheck, Sparkles, Tag, UserCheck, X } from "lucide-react";
import { useState } from "react";
import { ChipCategoria } from "@/componentes/asistente/chip-categoria";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { categorizarConversacion } from "@/lib/asistente/api-fase3";
import { asistenteAtiende, COLOR_DE_TONO, tonoDe, type Categoria } from "@/lib/asistente/fase3";
import { PERMISO_CATEGORIZAR } from "@/lib/asistente/permisos";
import type { Filtros } from "@/lib/conversaciones/estado";
import { CLASE_ETIQUETA } from "@/lib/conversaciones/presentacion";
import type { Contadores, Conversacion } from "@/lib/conversaciones/tipos";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { TEXTO_CAMBIO, useAtender } from "./atencion";
import { useCatalogos } from "./catalogos";

/*
 * Categorías en la bandeja (fase 3 del asistente, contratos/conversaciones.md §17): los filtros
 * con sus conteos arriba de la lista, la línea «qué hizo el asistente» de cada fila, la categoría
 * de la conversación abierta con «Cambiar» y la franja «El asistente está atendiendo · Tomar».
 */

/** Chips de filtro por categoría, con «Todas» primero. Una tira que se desliza si no cabe. */
export function FiltroCategorias({ filtros, contadores, alCambiar }: { filtros: Filtros; contadores: Contadores | null; alCambiar: (f: Filtros) => void }) {
    const { categorias } = useCatalogos();
    if (categorias.estado !== "listo" || categorias.datos.length === 0) return null;
    // Sin asistente no hay categoría (§17.3): la tira aparece cuando alguna conversación ya tiene una.
    if (filtros.categoriaId === null && !(contadores?.porCategoria ?? []).some((x) => x.total > 0)) return null;
    const total = (id: number) => contadores?.porCategoria?.find((x) => x.id === id)?.total;
    return (
        <div role="group" aria-label="Filtrar por categoría" data-slot="filtro-categorias" data-desborde-permitido className="-mx-3.5 flex gap-1.5 overflow-x-auto px-3.5 pb-0.5 [scrollbar-width:none]">
            <ChipFiltro activo={filtros.categoriaId === null} nombre="Todas" total={contadores?.todas} clase="bg-elevated text-foreground" alPresionar={() => alCambiar({ ...filtros, categoriaId: null })} />
            {categorias.datos.map((c) => (
                <ChipFiltro
                    key={c.id}
                    activo={filtros.categoriaId === c.id}
                    nombre={c.nombre}
                    total={total(c.id)}
                    clase={CLASE_ETIQUETA[COLOR_DE_TONO[tonoDe(c.tono)]] ?? ""}
                    alPresionar={() => alCambiar({ ...filtros, categoriaId: filtros.categoriaId === c.id ? null : c.id })}
                />
            ))}
        </div>
    );
}

function ChipFiltro({ activo, nombre, total, clase, alPresionar }: { activo: boolean; nombre: string; total: number | undefined; clase: string; alPresionar: () => void }) {
    return (
        <Boton
            variante="fantasma"
            tamano="sm"
            aria-pressed={activo}
            onClick={alPresionar}
            className={cn("h-7 shrink-0 gap-1 rounded-full px-2.5 text-xs font-semibold hover:opacity-90", activo ? "bg-primary text-primary-foreground hover:bg-primary" : clase)}
        >
            {activo ? <CircleCheck aria-hidden className="size-3.5" /> : null}
            <span className="max-w-[9rem] truncate">{nombre}</span>
            {total !== undefined ? <span className="tabular-nums">{total}</span> : null}
        </Boton>
    );
}

/** Tercera línea de la fila: el chip de la categoría y lo que hizo el asistente (texto del servidor). */
export function LineaCategoria({ conv }: { conv: Pick<Conversacion, "categoria" | "actividadAsistente"> }) {
    if (!conv.categoria && !conv.actividadAsistente) return null;
    return (
        <span className="col-span-2 col-start-2 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 pt-0.5" data-slot="linea-categoria">
            {conv.categoria ? <ChipCategoria categoria={conv.categoria} /> : null}
            {conv.actividadAsistente ? (
                <span className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground" data-actividad={conv.actividadAsistente.tipo}>
                    <Sparkles aria-hidden className="size-3.5 shrink-0 text-primary" />
                    <span className="sr-only">Asistente: </span>
                    <span className="truncate">{conv.actividadAsistente.texto}</span>
                </span>
            ) : null}
        </span>
    );
}

/**
 * Bajo la cabecera de la conversación: su categoría con «Cambiar» (`POST …/categorizar`) y, si el
 * asistente está atendiendo solo, la franja con «Tomar» (la acción de tomar de siempre: al tomarla
 * una persona, el asistente se detiene).
 */
type PropsBarra = {
    conv: Conversacion;
    alActualizar: (c: Conversacion) => void;
    /** Relee el detalle de la conversación (tras un 409 de `version`). */
    recargar: () => Promise<void>;
    bloqueada: boolean;
    asistenteActivo: boolean;
};

export function BarraCategoria({ conv, alActualizar, recargar, bloqueada, asistenteActivo }: PropsBarra) {
    const { puede } = useSesion();
    const { categorias } = useCatalogos();
    const clave = useClaveIdempotencia();
    const [ocupado, setOcupado] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const lista = categorias.estado === "listo" ? categorias.datos : [];
    // Corregir la que puso el asistente, o poner una a mano mientras el asistente está activo.
    const puedeCambiar = puede([PERMISO_CATEGORIZAR]) && !bloqueada && !conv.soloLectura && lista.length > 0 && (Boolean(conv.categoria) || asistenteActivo);
    const atiende = asistenteAtiende(conv);
    if (!conv.categoria && !puedeCambiar && !atiende) return null;

    async function cambiar(categoria: Pick<Categoria, "id" | "nombre"> | null) {
        if ((conv.categoria?.id ?? null) === (categoria?.id ?? null)) return;
        setOcupado(true);
        setError(null);
        try {
            const c = await categorizarConversacion(conv.id, categoria?.id ?? null, conv.version, clave.actual());
            clave.renovar();
            alActualizar(c);
            avisar({ titulo: categoria ? `Categoría «${categoria.nombre}» puesta en ${conv.titulo}` : `${conv.titulo} quedó sin categoría` });
        } catch (e) {
            clave.trasError(e);
            // Otra persona (o el asistente) la cambió: se relee y el siguiente intento lleva la versión nueva.
            if (e instanceof ErrorApi && e.estado === 409) {
                void recargar();
                setError(TEXTO_CAMBIO);
            } else setError(`No se pudo cambiar: ${mensajeDeError(e).titulo}`);
        } finally {
            setOcupado(false);
        }
    }

    return (
        <div className="shrink-0 border-b border-border bg-card" data-slot="barra-categoria">
            {conv.categoria || puedeCambiar ? (
                <div className="flex min-h-9 min-w-0 flex-wrap items-center gap-x-2 gap-y-1 px-3 py-1.5 @min-[700px]/conv:px-3.5">
                    <span className="sr-only">Categoría:</span>
                    {conv.categoria ? <ChipCategoria categoria={conv.categoria} /> : <span className="text-xs text-muted-foreground">Sin categoría</span>}
                    {puedeCambiar ? (
                        <MenuDesplegable>
                            <MenuDesplegableDisparador asChild>
                                <Boton variante="enlace" tamano="sm" className="h-auto px-0 text-xs" disabled={ocupado} aria-label={conv.categoria ? "Cambiar la categoría" : "Poner una categoría"}>
                                    {ocupado ? <Cargando texto="Cambiando…" /> : conv.categoria ? "Cambiar" : "Poner categoría"}
                                </Boton>
                            </MenuDesplegableDisparador>
                            <MenuDesplegableContenido>
                                <MenuDesplegableEtiqueta>Categoría de la conversación</MenuDesplegableEtiqueta>
                                <MenuDesplegableSeparador />
                                {lista.map((c) => (
                                    <MenuDesplegableElemento key={c.id} icono={c.id === conv.categoria?.id ? CircleCheck : Tag} onSelect={() => void cambiar(c)}>
                                        {c.nombre}
                                    </MenuDesplegableElemento>
                                ))}
                                {conv.categoria ? (
                                    <MenuDesplegableElemento key="ninguna" icono={X} onSelect={() => void cambiar(null)}>
                                        Sin categoría
                                    </MenuDesplegableElemento>
                                ) : null}
                            </MenuDesplegableContenido>
                        </MenuDesplegable>
                    ) : null}
                    {error ? (
                        <span role="alert" className="min-w-0 text-xs font-medium text-error [overflow-wrap:anywhere]">
                            {error}
                        </span>
                    ) : null}
                </div>
            ) : null}
            {atiende ? <FranjaAsistente conv={conv} alHecho={alActualizar} recargar={recargar} puedeTomar={puede(["conversaciones.tomar"]) && !bloqueada && !conv.soloLectura} /> : null}
        </div>
    );
}

/** «El asistente está atendiendo»: discreta, en teal, con «Tomar». */
function FranjaAsistente({ conv, alHecho, recargar, puedeTomar }: { conv: Conversacion; alHecho: (c: Conversacion) => void; recargar: () => Promise<void>; puedeTomar: boolean }) {
    const { atender, atendiendo, error } = useAtender(conv, alHecho, () => void recargar());
    return (
        <div role="status" data-slot="franja-asistente" className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 border-t border-primary/20 bg-primary/[0.06] px-3 py-1.5 @min-[700px]/conv:px-3.5">
            <Sparkles aria-hidden className="size-4 shrink-0 text-primary" />
            <span className="min-w-0 flex-1 text-[0.8125rem] font-medium text-primary">El asistente está atendiendo</span>
            {puedeTomar ? (
                <Boton variante="contorno" tamano="sm" className="h-7 shrink-0" onClick={() => void atender()} disabled={atendiendo} aria-label={`Tomar la conversación con ${conv.titulo}`}>
                    {atendiendo ? <Cargando texto="Tomando…" /> : <UserCheck />}
                    {atendiendo ? null : "Tomar"}
                </Boton>
            ) : null}
            {error ? (
                <span role="alert" className="basis-full text-xs font-medium text-error">
                    {error}
                </span>
            ) : null}
        </div>
    );
}
