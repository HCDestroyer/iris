"use client";

import type { LucideIcon } from "lucide-react";
import { Children, type ComponentProps, type MouseEvent, type ReactNode } from "react";
import { Link } from "react-aria-components";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { Cargando } from "./cargando";

export type TonoIconoFila = "marca" | "neutro" | "info" | "exito" | "atencion" | "peligro";

const FONDO_ICONO: Record<TonoIconoFila, string> = {
    marca: "bg-primario-suave text-primario-texto",
    neutro: "bg-relleno text-muted-foreground",
    info: "bg-info-fondo text-info",
    exito: "bg-exito-fondo text-success",
    atencion: "bg-atencion-fondo text-warning",
    peligro: "bg-peligro-fondo text-danger",
};

export type PropsListaFilas = Omit<ComponentProps<"ul">, "children"> & {
    /** Nombre accesible de la lista («Tickets»). */
    etiqueta: string;
    children: ReactNode;
    /** Encima de las filas: el buscador, las vistas y «Filtros (n)». */
    cabecera?: ReactNode;
    /** Barra de la selección múltiple («1 elegido · Asignar… · Quitar selección»). */
    seleccion?: ReactNode;
    /** Al pie: «Mostrando 8 de 42». */
    resumen?: ReactNode;
    /** «Cargar más» (paginación por cursor). */
    alCargarMas?: () => void;
    cargandoMas?: boolean;
    /** Falló «Cargar más»: el aviso con «Reintentar» (`Aviso`) va en el pie en lugar del botón. */
    errorMas?: ReactNode;
    /** Sin filas: lo que se muestra en su lugar (`EstadoVacio`). */
    vacio?: ReactNode;
    className?: string;
};

/**
 * Lista en lugar de cuadro (rediseño, regla 7): una tarjeta con su cabecera (buscador y filtros),
 * la barra de la selección, las filas separadas por una línea y el pie con «Mostrando n de m» y
 * «Cargar más». Es una lista semántica (`<ul>` con nombre): cada fila lleva su enlace o botón
 * principal, su casilla y su menú `⋯`, que funcionan con el teclado como cualquier control.
 */
export function ListaFilas({ etiqueta, children, cabecera, seleccion, resumen, alCargarMas, cargandoMas = false, errorMas, vacio, className, ...props }: PropsListaFilas) {
    // Las filas, sin contar los huecos (`null`, `false`) ni las listas vacías de un `map`.
    const conFilas = Children.toArray(children).length > 0;
    return (
        <section data-slot="lista-filas" aria-label={etiqueta} className={cn("surface-card flex min-h-0 min-w-0 flex-col overflow-hidden", className)}>
            {cabecera ? <div className="flex flex-none flex-wrap items-center gap-2 border-b border-border p-3">{cabecera}</div> : null}
            {seleccion ? <div className="flex flex-none flex-wrap items-center gap-3 border-b border-border bg-primario-suave px-4 py-2 text-sm">{seleccion}</div> : null}
            {conFilas ? (
                <ul aria-label={etiqueta} className="min-h-0 flex-1 divide-y divide-border overflow-y-auto overscroll-contain" {...props}>
                    {children}
                </ul>
            ) : (
                <div className="p-6">{vacio}</div>
            )}
            {resumen || alCargarMas || errorMas ? (
                <footer className="flex flex-none flex-wrap items-center gap-3 border-t border-border px-4 py-3 text-sm text-muted-foreground">
                    {resumen ? <span className="min-w-0 flex-1 truncate">{resumen}</span> : <span className="flex-1" />}
                    {errorMas ? (
                        <div className="w-full">{errorMas}</div>
                    ) : alCargarMas ? (
                        <Boton variante="contorno" tamano="sm" onClick={alCargarMas} disabled={cargandoMas}>
                            {cargandoMas ? <Cargando tamano="sm" decorativo /> : null}
                            {cargandoMas ? "Cargando…" : "Cargar más"}
                        </Boton>
                    ) : null}
                </footer>
            ) : null}
        </section>
    );
}

export type PropsFilaLista = {
    /** Título de la fila (lo que se lee primero). */
    titulo: ReactNode;
    /** Segunda línea: código, cliente, servicio. */
    detalle?: ReactNode;
    icono?: LucideIcon;
    tonoIcono?: TonoIconoFila;
    /** A la derecha del texto: estados (`InsigniaEstado`), hora, montos. */
    extremo?: ReactNode;
    /**
     * En el teléfono (menos de 640 px) el extremo se oculta por omisión; con `"debajo"` baja a su
     * propia línea bajo el título (para estados que hay que ver: SLA, prioridad, pendientes).
     */
    extremoTelefono?: "oculto" | "debajo";
    /** Al inicio: la casilla de la selección múltiple. */
    casilla?: ReactNode;
    /** Al final: el menú `⋯` (`MenuAcciones`). */
    menu?: ReactNode;
    /** Enlace de la fila (abre la ficha). */
    href?: string;
    /** O una acción (abre la vista rápida). Con los dos, manda `alAbrir`. */
    alAbrir?: () => void;
    /** Nombre accesible del enlace o botón principal; por omisión, el texto del título. */
    etiqueta?: string;
    /** Fila activa (su vista rápida está abierta o está elegida). */
    activa?: boolean;
    /** Clic derecho: abre el menú de la fila. */
    onContextMenu?: (evento: MouseEvent<HTMLLIElement>) => void;
    className?: string;
} & Omit<ComponentProps<"li">, "children" | "onContextMenu">;

/**
 * Una fila de `ListaFilas`: casilla, ícono en su ficha de color, título y detalle, estados a la
 * derecha y el menú `⋯`. El título es el control principal (enlace a la ficha o botón que abre la
 * vista rápida) y toda la fila responde al clic; la casilla y el menú no lo disparan.
 */
export function FilaLista({
    titulo,
    detalle,
    icono: Icono,
    tonoIcono = "neutro",
    extremo,
    extremoTelefono = "oculto",
    casilla,
    menu,
    href,
    alAbrir,
    etiqueta,
    activa = false,
    className,
    ...props
}: PropsFilaLista) {
    const contenido = (
        <>
            <span className="block truncate font-semibold text-foreground">{titulo}</span>
            {detalle ? <span className="block truncate text-xs text-muted-foreground">{detalle}</span> : null}
        </>
    );
    const clasePrincipal =
        "grid min-w-0 flex-1 gap-0.5 rounded-md text-left text-sm outline-none after:absolute after:inset-0 after:content-[''] data-focus-visible:outline-2 data-focus-visible:outline-primary focus-visible:outline-2 focus-visible:outline-primary";
    return (
        <li
            data-slot="fila-lista"
            data-activa={activa ? "true" : undefined}
            aria-current={activa ? "true" : undefined}
            className={cn(
                "relative flex min-w-0 items-center gap-3 px-4 py-2.5 transition-colors",
                extremo && extremoTelefono === "debajo" && "flex-wrap sm:flex-nowrap",
                activa ? "bg-primario-suave" : "hover:bg-[var(--iris-relleno)]/60",
                className,
            )}
            {...props}
        >
            {casilla ? <div className="relative z-[1] flex-none">{casilla}</div> : null}
            {Icono ? (
                <span aria-hidden className={cn("grid size-9 flex-none place-content-center rounded-[10px]", FONDO_ICONO[tonoIcono])}>
                    <Icono className="size-[1.0625rem]" />
                </span>
            ) : null}
            {alAbrir ? (
                <Boton variante="fantasma" className={cn(clasePrincipal, "h-auto justify-start rounded-md bg-transparent p-0 font-normal hover:bg-transparent")} onClick={alAbrir} {...(etiqueta ? { "aria-label": etiqueta } : {})}>
                    {contenido}
                </Boton>
            ) : href ? (
                <Link href={href} className={clasePrincipal} {...(etiqueta ? { "aria-label": etiqueta } : {})}>
                    {contenido}
                </Link>
            ) : (
                <div className="grid min-w-0 flex-1 gap-0.5 text-sm">{contenido}</div>
            )}
            {extremo ? (
                <div
                    data-slot="extremo-fila"
                    className={cn(
                        "relative z-[1] flex-none items-center gap-2",
                        extremoTelefono === "debajo" ? "order-last flex w-full flex-wrap sm:order-none sm:w-auto sm:flex-nowrap" : "hidden sm:flex",
                    )}
                >
                    {extremo}
                </div>
            ) : null}
            {menu ? <div className="relative z-[1] flex-none">{menu}</div> : null}
        </li>
    );
}
