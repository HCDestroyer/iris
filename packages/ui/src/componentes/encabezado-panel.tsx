"use client";

import { Kbd, Tooltip } from "@heroui/react";
import { Search } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton, type PropsBoton } from "./boton";

/** Clases de una isla del encabezado: 44 px de alto, píldora, tarjeta con borde y sombra flotante. */
export const CLASE_ISLA_ENCABEZADO =
    "h-11 rounded-full border border-border bg-[var(--iris-tarjeta)] text-[0.84375rem] font-semibold text-foreground shadow-[var(--iris-sombra-flotante)] md:h-11";

export type PropsBotonFlotante = PropsBoton & {
    /** Solo ícono (redondo de 44 px); entonces `aria-label` es obligatorio. */
    redondo?: boolean;
    /** Relleno índigo tenue (el acceso rápido principal). */
    acento?: boolean;
};

/**
 * Botón suelto del encabezado (cada uno es una isla): el `Boton` de HeroUI en píldora de 44 px con
 * la tarjeta, el borde y la sombra flotante. Redondo para los de solo ícono (avisos, estado).
 */
export function BotonFlotante({ redondo = false, acento = false, className, variante, ...props }: PropsBotonFlotante) {
    return (
        <Boton
            variante={variante ?? "contorno"}
            data-isla-encabezado=""
            className={cn(
                CLASE_ISLA_ENCABEZADO,
                "relative gap-2 px-4 hover:bg-[var(--iris-relleno)] [&_svg]:size-[1.125rem]",
                redondo && "size-11 min-w-11 p-0 text-muted-foreground md:size-11",
                acento && "border-transparent bg-primario-suave text-primario-texto [--button-bg:var(--iris-primario-suave)] hover:bg-[color-mix(in_oklab,var(--iris-primario)_20%,transparent)]",
                className,
            )}
            {...props}
        />
    );
}

// Es un `Boton` por dentro: los disparadores (menú, popover, globo) se conectan solos.
BotonFlotante.esBotonAria = true;

export type PropsBotonBuscar = {
    alAbrir: () => void;
    /** «Buscar…» */
    texto?: string;
    /** Atajo que se muestra («Ctrl K»; «⌘ K» en Mac). */
    atajo?: string;
    /** Nombre accesible (el que usan las pruebas y los lectores). */
    etiqueta?: string;
    className?: string;
};

/**
 * «Buscar… Ctrl K» del encabezado: isla de 250 px con la lupa y el atajo. En el teléfono, redonda
 * (solo la lupa). Abre el buscador global (`PaletaBusqueda`).
 */
export function BotonBuscar({ alAbrir, texto = "Buscar…", atajo = "Ctrl K", etiqueta = "Buscador global", className }: PropsBotonBuscar) {
    return (
        <BotonFlotante
            aria-label={etiqueta}
            aria-keyshortcuts="Control+K Meta+K"
            title={`${texto.replace("…", "")} (${atajo})`}
            data-slot="boton-buscar"
            onClick={alAbrir}
            className={cn(
                "size-11 min-w-11 justify-center p-0 font-medium text-[var(--iris-texto-apagado)] sm:w-[15.625rem] sm:justify-start sm:px-4 md:size-11 md:sm:w-[15.625rem]",
                className,
            )}
        >
            <Search aria-hidden />
            <span className="hidden sm:inline">{texto}</span>
            <Kbd className="ml-auto hidden rounded-md border border-border bg-transparent px-1.5 text-[0.6875rem] font-semibold text-[var(--iris-texto-apagado)] sm:inline-flex">{atajo}</Kbd>
        </BotonFlotante>
    );
}

export type TonoIndicador = "bien" | "atencion" | "peligro" | "apagado";

const COLOR_PUNTO: Record<TonoIndicador, string> = {
    bien: "bg-[var(--iris-exito-punto)]",
    atencion: "bg-[var(--iris-atencion-punto)]",
    peligro: "bg-[var(--iris-peligro-punto)]",
    apagado: "bg-[var(--iris-texto-apagado)]",
};

export type PropsIndicadorEstado = {
    /** Ícono del indicador (p. ej. `Plug` para la conexión con Argos y Horus). */
    icono: ReactNode;
    tono: TonoIndicador;
    /** Lo que dice el globo y el nombre accesible («Argos y Horus conectados»). */
    texto: string;
    /** Cuando hay que actuar: el indicador crece y muestra este texto («Argos sin conexión»). */
    accion?: string;
    alPresionar?: () => void;
    className?: string;
};

/**
 * Indicador de estado discreto (regla 6): un botón redondo con un punto de color; si todo está bien
 * solo el punto lo dice. Crece y explica (texto visible, tono de atención o peligro) solo cuando
 * hay que actuar. El estado siempre está en el nombre accesible y en el globo.
 */
export function IndicadorEstado({ icono, tono, texto, accion, alPresionar, className }: PropsIndicadorEstado) {
    const crece = Boolean(accion);
    return (
        <Tooltip delay={300} closeDelay={150}>
            <BotonFlotante
                redondo={!crece}
                aria-label={accion ? `${texto}: ${accion}` : texto}
                data-slot="indicador-estado"
                data-tono={tono}
                {...(alPresionar ? { onClick: alPresionar } : {})}
                className={cn(crece && (tono === "peligro" ? "border-transparent bg-peligro-fondo text-danger" : "border-transparent bg-atencion-fondo text-warning"), className)}
            >
                {icono}
                {crece ? <span>{accion}</span> : null}
                <span aria-hidden data-slot="punto-estado" className={cn("absolute top-[0.5625rem] right-[0.5625rem] size-[0.5625rem] rounded-full ring-2 ring-[var(--iris-tarjeta)]", COLOR_PUNTO[tono], crece && "hidden")} />
            </BotonFlotante>
            <Tooltip.Content offset={8} placement="bottom" data-slot="globo" className="max-w-64 rounded-md border border-border px-2.5 py-1.5 text-xs text-foreground">
                {texto}
            </Tooltip.Content>
        </Tooltip>
    );
}

export type PropsEncabezadoPanel = {
    /** Izquierda, lo que haces: Buscar (Ctrl K), 2 accesos rápidos y «Más». */
    izquierda: ReactNode;
    /** Derecha, lo que vigilas: estados, avisos y la cuenta. */
    derecha: ReactNode;
    className?: string;
};

/**
 * Encabezado del panel sin barra (rediseño, nov. 2026): botones sueltos de 44 px, cada uno una
 * isla, a 16 px del borde de arriba. A la izquierda lo que haces y a la derecha lo que vigilas. No
 * tiene fondo: entre los botones se ve el lienzo.
 */
export function EncabezadoPanel({ izquierda, derecha, className }: PropsEncabezadoPanel) {
    return (
        <header data-slot="encabezado-panel" className={cn("pointer-events-none flex h-11 min-w-0 items-center gap-2.5 [&>*]:pointer-events-auto", className)}>
            <div className="flex min-w-0 items-center gap-2.5">{izquierda}</div>
            <div className="ml-auto flex min-w-0 items-center justify-end gap-2.5">{derecha}</div>
        </header>
    );
}
