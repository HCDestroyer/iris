"use client";

import { inputVariants, listboxItemVariants } from "@heroui/styles";
import { Search, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Autocomplete, Dialog, Header, Heading, Input, ListBox, ListBoxItem, ListBoxSection, Modal, ModalOverlay, SearchField } from "react-aria-components";
import { cn } from "../utilidades";
import { animacionPanel, clasesFondo } from "./dialogo";

export type ElementoPaleta = {
    id: string;
    etiqueta: string;
    /** Texto secundario (número, estado, cliente…). */
    detalle?: string;
    icono?: LucideIcon;
    /** A dónde lleva: `alElegir` lo recibe con el elemento. */
    href: string;
};

export type SeccionPaleta = {
    id: string;
    titulo: string;
    elementos: readonly ElementoPaleta[];
};

export type PropsPaletaBusqueda = {
    abierta: boolean;
    alCambiarAbierta: (abierta: boolean) => void;
    texto: string;
    alCambiarTexto: (texto: string) => void;
    secciones: readonly SeccionPaleta[];
    alElegir: (elemento: ElementoPaleta) => void;
    /** Estado bajo la lista: «Buscando…», errores con «Reintentar», «Escribe 3 letras…». */
    estado?: ReactNode;
    /** Texto cuando no hay ningún resultado. */
    sinResultados?: string;
    titulo?: string;
    placeholder?: string;
};

const piezaElemento = listboxItemVariants();

/**
 * Buscador global (Ctrl/⌘ + K): un diálogo con un campo de búsqueda y los resultados por secciones
 * (React Aria `Autocomplete`). El foco se queda en el campo y las flechas recorren los resultados
 * (foco virtual); Enter elige y Escape cierra. Quien lo usa decide qué buscar y a dónde llevar.
 */
export function PaletaBusqueda({
    abierta,
    alCambiarAbierta,
    texto,
    alCambiarTexto,
    secciones,
    alElegir,
    estado,
    sinResultados = "Sin resultados",
    titulo = "Buscar en Iris",
    placeholder = "Pantalla, ticket, orden, cliente…",
}: PropsPaletaBusqueda) {
    const conElementos = secciones.filter((s) => s.elementos.length > 0);
    const porId = new Map(conElementos.flatMap((s) => s.elementos.map((e) => [e.id, e] as const)));
    return (
        <ModalOverlay isOpen={abierta} onOpenChange={alCambiarAbierta} isDismissable className={clasesFondo}>
            <Modal
                className={cn(
                    "glass-strong fixed z-(--z-index-overlay) flex max-h-[85dvh] w-full flex-col overflow-hidden shadow-xl",
                    "inset-x-0 top-0 rounded-t-none sm:inset-x-auto sm:top-[12dvh] sm:left-1/2 sm:w-[min(40rem,calc(100vw-2rem))] sm:-translate-x-1/2",
                    animacionPanel,
                )}
            >
                <Dialog data-slot="paleta-busqueda" className="flex min-h-0 flex-1 flex-col outline-none">
                    <Heading slot="title" className="sr-only">
                        {titulo}
                    </Heading>
                    <Autocomplete inputValue={texto} onInputChange={alCambiarTexto}>
                        {/* Escape cierra el buscador aunque haya texto (SearchField solo lo borraría). */}
                        <SearchField
                            aria-label={titulo}
                            autoFocus
                            className="relative border-b p-3"
                            onKeyDown={(e) => {
                                if (e.key === "Escape") alCambiarAbierta(false);
                                else e.continuePropagation();
                            }}
                        >
                            <Search aria-hidden className="pointer-events-none absolute top-1/2 left-6 size-4 -translate-y-1/2 text-muted-foreground" />
                            <Input
                                placeholder={placeholder}
                                className={inputVariants({ fullWidth: true, className: "h-11 pl-10 text-base [&::-webkit-search-cancel-button]:hidden" })}
                            />
                        </SearchField>
                        <ListBox
                            aria-label="Resultados"
                            className="min-h-0 flex-1 overflow-y-auto p-2 outline-none empty:hidden"
                            onAction={(clave) => {
                                const elegido = porId.get(String(clave));
                                if (elegido) alElegir(elegido);
                            }}
                            renderEmptyState={() => (texto.trim() && !estado ? <p className="px-3 py-6 text-center text-sm text-muted-foreground">{sinResultados}</p> : null)}
                        >
                            {conElementos.map((s) => (
                                <ListBoxSection key={s.id} id={s.id} className="grid gap-0.5 pb-2">
                                    <Header className="px-2.5 pt-1 pb-1 text-xs font-medium text-muted-foreground">{s.titulo}</Header>
                                    {s.elementos.map((e) => {
                                        const Icono = e.icono;
                                        return (
                                            <ListBoxItem
                                                key={e.id}
                                                id={e.id}
                                                textValue={e.detalle ? `${e.etiqueta} ${e.detalle}` : e.etiqueta}
                                                className={piezaElemento.item({
                                                    className:
                                                        "flex min-h-10 cursor-pointer items-center gap-3 rounded-md px-2.5 py-1.5 text-sm outline-none data-focused:bg-primary/10 data-focused:text-foreground data-hovered:bg-elevated",
                                                })}
                                            >
                                                {Icono ? <Icono aria-hidden className="size-4 shrink-0 text-muted-foreground" /> : null}
                                                <span className="grid min-w-0 flex-1">
                                                    <span className="truncate">{e.etiqueta}</span>
                                                    {e.detalle ? <span className="truncate text-xs text-muted-foreground">{e.detalle}</span> : null}
                                                </span>
                                            </ListBoxItem>
                                        );
                                    })}
                                </ListBoxSection>
                            ))}
                        </ListBox>
                    </Autocomplete>
                    {estado ? <div className="grid gap-2 border-t p-3 text-sm">{estado}</div> : null}
                    <p className="hidden border-t px-3 py-2 text-xs text-muted-foreground sm:block">
                        <kbd>↑</kbd> <kbd>↓</kbd> para moverte · <kbd>Enter</kbd> para abrir · <kbd>Esc</kbd> para cerrar
                    </p>
                </Dialog>
            </Modal>
        </ModalOverlay>
    );
}
