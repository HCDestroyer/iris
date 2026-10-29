import type { LucideIcon } from "lucide-react";
import { useId, type ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo } from "./campo";

export type OpcionGrupo<V extends string | number> = {
    valor: V;
    /** Texto visible de la opción: obligatorio (nunca solo un icono o una estrella). */
    etiqueta: string;
    icono?: LucideIcon;
    /** Texto pequeño bajo la etiqueta. */
    detalle?: string;
};

export type PropsGrupoOpciones<V extends string | number> = {
    /** Pregunta del grupo (`legend` del fieldset). */
    leyenda: ReactNode;
    /** `name` de los radios. */
    nombre: string;
    opciones: readonly OpcionGrupo<V>[];
    valor: V | undefined;
    alCambiar: (valor: V) => void;
    error?: string | undefined;
    ayuda?: ReactNode;
    disabled?: boolean;
    className?: string;
};

/**
 * Opciones excluyentes como tarjetas grandes (radios nativos dentro de un fieldset): flechas para
 * moverse y espacio para elegir, como cualquier radio del sistema. Pensado para formularios
 * públicos y táctiles (p. ej. la calificación 1–5 de la encuesta), con área táctil ≥ 44 px.
 */
export function GrupoOpciones<V extends string | number>({
    leyenda,
    nombre,
    opciones,
    valor,
    alCambiar,
    error,
    ayuda,
    disabled,
    className,
}: PropsGrupoOpciones<V>) {
    const id = useId();
    const idAyuda = `${id}-ayuda`;
    const idError = `${id}-error`;
    const describe = [ayuda ? idAyuda : null, error ? idError : null].filter(Boolean).join(" ") || undefined;
    return (
        <fieldset data-slot="grupo-opciones" className={cn("grid gap-2", className)} aria-describedby={describe} aria-invalid={error ? true : undefined} disabled={disabled}>
            <legend className="mb-1 text-sm font-medium">{leyenda}</legend>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(6.5rem,1fr))]">
                {opciones.map((o) => {
                    const Icono = o.icono;
                    const marcado = valor === o.valor;
                    return (
                        <label
                            key={String(o.valor)}
                            className={cn(
                                "flex min-h-11 cursor-pointer items-center gap-2.5 rounded-lg border border-input bg-background px-3 py-2.5 text-sm",
                                "transition-[border-color,box-shadow,background-color] duration-150 hover:bg-elevated",
                                "has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-primary",
                                "sm:flex-col sm:justify-center sm:gap-1.5 sm:py-3 sm:text-center",
                                marcado && "border-primary bg-primary/5 font-semibold shadow-[0_0_0_1px_var(--hx-primary)]",
                                error && !marcado && "border-error",
                            )}
                        >
                            <input
                                type="radio"
                                name={nombre}
                                value={String(o.valor)}
                                checked={marcado}
                                onChange={() => alCambiar(o.valor)}
                                className="size-4 shrink-0 accent-primary sm:sr-only"
                            />
                            {Icono ? <Icono aria-hidden className={cn("size-5 shrink-0 sm:size-6", marcado ? "text-primary" : "text-muted-foreground")} /> : null}
                            <span className="grid gap-0.5">
                                <span>{o.etiqueta}</span>
                                {o.detalle ? <span className="text-xs font-normal text-muted-foreground">{o.detalle}</span> : null}
                            </span>
                        </label>
                    );
                })}
            </div>
            <PieCampo idAyuda={idAyuda} idError={idError} ayuda={ayuda} error={error} />
        </fieldset>
    );
}
