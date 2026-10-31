import { cn } from "@iris/ui";
import { TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

/*
 * Gráficos sencillos en HTML y CSS con la paleta categórica de Horus (`bg-grafico-1`,
 * `bg-grafico-2`): barras horizontales de 16 px con el extremo redondeado de 4 px, el valor en
 * texto al final de la barra (nunca del color de la serie) y «Sin datos» cuando no hay base.
 * Cada gráfico va acompañado de su tabla de desglose, que es la vista accesible completa.
 */

/** Indicador grande (KPI) con su etiqueta y una línea de contexto. */
export function Indicador({ etiqueta, valor, detalle }: { etiqueta: string; valor: string; detalle?: ReactNode }) {
    const sinDatos = valor === "Sin datos";
    return (
        <div className="grid content-start gap-1 rounded-lg border bg-background/40 p-3" data-indicador={etiqueta}>
            <p className="text-xs font-medium text-muted-foreground">{etiqueta}</p>
            <p className={cn("text-2xl font-semibold tracking-[-0.02em] tabular-nums", sinDatos && "text-base font-medium text-muted-foreground")}>{valor}</p>
            {detalle ? <p className="text-xs text-muted-foreground">{detalle}</p> : null}
        </div>
    );
}

export function FilaIndicadores({ children }: { children: ReactNode }) {
    return <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">{children}</div>;
}

export type Serie = { nombre: string; clase: "bg-grafico-1" | "bg-grafico-2" };

export type FilaGrafico = {
    etiqueta: string;
    /** Un valor por serie; `null` = sin datos (sin barra). */
    valores: { valor: number | null; texto: string }[];
    /** Texto del tooltip (al pasar el puntero) con el desglose. */
    detalle?: string;
    /** Fila que pide atención (p. ej. calificaciones de alerta): icono + texto, nunca solo color. */
    alerta?: string;
};

/**
 * Barras horizontales agrupadas (una o dos series). Con una serie no hay leyenda: el título la
 * nombra. Con dos, la leyenda va arriba y cada barra lleva su valor en texto.
 */
export function Barras({
    titulo,
    hallazgo,
    series,
    filas,
    maximo,
}: {
    titulo: string;
    /** Frase con lo principal que muestra el gráfico. */
    hallazgo?: string | undefined;
    series: readonly Serie[];
    filas: readonly FilaGrafico[];
    /** Valor de la barra completa (100 para porcentajes); por omisión, el mayor de los datos. */
    maximo?: number;
}) {
    const mayor = maximo ?? Math.max(0, ...filas.flatMap((f) => f.valores.map((v) => v.valor ?? 0)));
    return (
        <figure className="grid gap-3" aria-label={titulo}>
            <figcaption className="grid gap-0.5">
                <span className="text-sm font-medium">{titulo}</span>
                {hallazgo ? <span className="text-xs text-muted-foreground">{hallazgo}</span> : null}
            </figcaption>
            {series.length > 1 ? (
                <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Leyenda">
                    {series.map((s) => (
                        <li key={s.nombre} className="flex items-center gap-1.5">
                            <span aria-hidden className={cn("inline-block size-2.5 rounded-[2px]", s.clase)} />
                            {s.nombre}
                        </li>
                    ))}
                </ul>
            ) : null}
            <ul className="grid gap-2.5">
                {filas.map((f) => (
                    <li
                        key={f.etiqueta}
                        title={f.detalle}
                        className="grid grid-cols-[minmax(5.5rem,9rem)_minmax(0,1fr)] items-center gap-x-3 gap-y-1 rounded-sm hover:bg-background-secondary/50"
                    >
                        {f.alerta ? (
                            <span className="flex min-w-0 items-center gap-1 text-xs text-foreground" data-alerta>
                                <TriangleAlert aria-hidden className="size-3.5 shrink-0 text-warning" />
                                <span className="truncate">{f.etiqueta}</span>
                                <span className="sr-only">({f.alerta})</span>
                            </span>
                        ) : (
                            <span className="truncate text-xs text-foreground">{f.etiqueta}</span>
                        )}
                        <span className="grid gap-1 border-l border-border py-0.5">
                            {f.valores.map((v, i) => {
                                const serie = series[i];
                                const fraccion = v.valor === null || mayor <= 0 ? 0 : Math.min(1, Math.max(0, v.valor / mayor));
                                return (
                                    <span key={serie?.nombre ?? i} className="flex min-w-0 items-center gap-2">
                                        {v.valor !== null ? (
                                            <span
                                                aria-hidden
                                                className={cn("h-4 shrink-0 rounded-e-[4px]", serie?.clase, fraccion === 0 && "w-0.5 rounded-none")}
                                                style={fraccion > 0 ? { width: `calc((100% - 7rem) * ${fraccion.toFixed(4)})` } : undefined}
                                            />
                                        ) : null}
                                        <span className={cn("text-xs whitespace-nowrap tabular-nums", v.valor === null ? "pl-1 text-muted-foreground" : "font-medium text-foreground")}>
                                            {series.length > 1 && serie ? <span className="sr-only">{serie.nombre}: </span> : null}
                                            {v.texto}
                                        </span>
                                    </span>
                                );
                            })}
                        </span>
                    </li>
                ))}
            </ul>
        </figure>
    );
}
