"use client";

import { Meter, ProgressBar } from "@heroui/react";
import { meterVariants } from "@heroui/styles";
import { cn } from "../utilidades";

/** Color del relleno: la marca, aviso o error. Va siempre con su texto al lado. */
export type TonoBarra = "normal" | "aviso" | "error";

const RELLENO: Record<TonoBarra, string> = { normal: "bg-primary", aviso: "bg-warning", error: "bg-error" };
const COLOR_HEROUI = { normal: "accent", aviso: "warning", error: "danger" } as const satisfies Record<TonoBarra, string>;

export type PropsBarraProgreso = {
    /** Cuánto va (entre `minimo` y `maximo`). */
    valor: number;
    minimo?: number;
    /** Tope (100 por omisión). Si no supera a `minimo` (nada que hacer), la barra queda vacía. */
    maximo?: number;
    /** Qué avanza, para lectores de pantalla («Avance de la carga»). */
    etiqueta: string;
    /** Cómo se lee el valor («3 de 4 pasos listos»); sin él, el porcentaje. */
    textoValor?: string;
    tono?: TonoBarra;
    /**
     * Por pasos: un segmento por unidad del tope (los pasos de la orden en la app de campo), en
     * lugar de un relleno continuo.
     */
    pasos?: boolean;
    /** Clases del contenedor (márgenes, ancho, posición). */
    className?: string;
    /** Clases del riel (alto y fondo). Por omisión, 6 px y `bg-elevated`. */
    claseRiel?: string;
    /** Clases del relleno (o de cada paso hecho). */
    claseRelleno?: string;
    /** Clases de cada paso pendiente (con `pasos`). */
    clasePendiente?: string;
};

/**
 * Barra de avance: el `ProgressBar` de HeroUI (React Aria: `role="progressbar"` con
 * `aria-valuemin`, `aria-valuemax`, `aria-valuenow` y `aria-valuetext`), sin rótulo visible (el
 * texto lo pone la pantalla al lado) y con los colores de Iris.
 */
export function BarraProgreso({ valor, minimo = 0, maximo = 100, etiqueta, textoValor, tono = "normal", pasos = false, className, claseRiel, claseRelleno, clasePendiente }: PropsBarraProgreso) {
    const total = Math.max(0, Math.round(maximo - minimo));
    // React Aria divide por (máximo − mínimo): sin rango, una barra vacía de 0 a 1.
    const conRango = maximo > minimo;
    return (
        <ProgressBar
            aria-label={etiqueta}
            value={conRango ? valor : minimo}
            minValue={minimo}
            maxValue={conRango ? maximo : minimo + 1}
            color={COLOR_HEROUI[tono]}
            {...(textoValor === undefined ? {} : { valueLabel: textoValor })}
            data-slot="barra-progreso"
            data-tono={tono}
            className={cn("gap-0", className)}
        >
            {pasos ? (
                    <span className="grid gap-1.5" style={{ gridArea: "track", gridTemplateColumns: `repeat(${Math.max(total, 1)}, minmax(0, 1fr))` }}>
                        {Array.from({ length: total }, (_, i) => (
                            <span key={i} data-hecho={i < valor - minimo || undefined} className={cn("h-1.5 rounded-full", i < valor - minimo ? cn(RELLENO[tono], claseRelleno) : cn("bg-elevated", clasePendiente))} />
                        ))}
                    </span>
                ) : (
                    <ProgressBar.Track className={cn("h-1.5 rounded-full bg-elevated", claseRiel)}>
                        <ProgressBar.Fill className={cn("rounded-full", RELLENO[tono], claseRelleno)} />
                    </ProgressBar.Track>
                )}
        </ProgressBar>
    );
}

export type PropsMedidor = {
    /** Parte usada, de 0 a 1; `null`: no hay dato (no se dibuja relleno). */
    parte: number | null;
    /** Qué mide, para lectores de pantalla («Memoria de video»). */
    etiqueta: string;
    /** Cómo se lee el valor («13.1 de 24 GB»). */
    texto: string;
    /** Color por estado: normal (marca), aviso o error. Va siempre con su texto al lado. */
    tono?: TonoBarra;
    className?: string;
    /** Clases del riel (alto y fondo). Por omisión, 6 px y `bg-elevated`. */
    claseRiel?: string;
};

const piezasMedidor = meterVariants();

/**
 * Barra de «cuánto de cuánto» (memoria, SLA cumplido): el `Meter` de HeroUI (`role="meter"`). Sin
 * dato (`parte: null`) no inventa un cero: el `Meter` de React Aria siempre anuncia un valor, así
 * que se dibuja el mismo riel (piezas `meter`/`meter__track`) con `role="meter"` y sin
 * `aria-valuenow`.
 */
export function Medidor({ parte, etiqueta, texto, tono = "normal", className, claseRiel }: PropsMedidor) {
    const riel = cn("h-1.5 w-full rounded-full bg-elevated", claseRiel);
    if (parte === null) {
        return (
            <div role="meter" aria-label={etiqueta} aria-valuemin={0} aria-valuemax={100} aria-valuetext={texto} data-slot="medidor" data-tono={tono} className={piezasMedidor.base({ className: cn("gap-0", className) })}>
                <div className={piezasMedidor.track({ className: riel })} style={{ gridArea: "track" }} />
            </div>
        );
    }
    const valor = Math.round(Math.min(Math.max(parte, 0), 1) * 100);
    return (
        <Meter aria-label={etiqueta} value={valor} valueLabel={texto} color={COLOR_HEROUI[tono]} data-slot="medidor" data-tono={tono} className={cn("gap-0", className)}>
            <Meter.Track className={riel}>
                <Meter.Fill className={cn("rounded-full", RELLENO[tono])} />
            </Meter.Track>
        </Meter>
    );
}
