import { cn, type Tono } from "@iris/ui";
import { CircleCheck, CircleHelp, CircleX, Clock, Info, TriangleAlert, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

const FONDO: Record<Tono, string> = {
    success: "bg-[color-mix(in_oklab,var(--hx-success)_10%,var(--hx-card))]",
    warning: "bg-[color-mix(in_oklab,var(--hx-warning)_10%,var(--hx-card))]",
    error: "bg-[color-mix(in_oklab,var(--hx-error)_10%,var(--hx-card))]",
    info: "bg-[color-mix(in_oklab,var(--hx-info)_10%,var(--hx-card))]",
    neutral: "bg-background-secondary",
};
const TEXTO: Record<Tono, string> = { success: "text-success", warning: "text-warning", error: "text-error", info: "text-info", neutral: "text-muted-foreground" };
const ICONO_ENCABEZADO: Record<Tono, LucideIcon> = { success: CircleCheck, warning: TriangleAlert, error: CircleX, info: Clock, neutral: Info };
const ICONO_LINEA: Record<Tono, LucideIcon> = { success: CircleCheck, warning: TriangleAlert, error: CircleX, info: Clock, neutral: CircleHelp };

export type LineaEstado = { titulo: string; texto: string; tono: Tono; clave?: string };

/**
 * Tarjeta de estado: encabezado tintado (icono + etiqueta + título + texto + acción) y, debajo, una
 * línea por comprobación, cada una con icono, texto y color. La usan «Conexión con Meta» y el QR.
 */
export function TarjetaEstado({
    etiqueta,
    titulo,
    texto,
    tono,
    accion,
    lineas = [],
    icono,
    "data-estado": dataEstado,
}: {
    etiqueta: string;
    titulo: string;
    texto?: ReactNode;
    tono: Tono;
    accion?: ReactNode;
    lineas?: readonly LineaEstado[];
    icono?: LucideIcon;
    "data-estado"?: string;
}) {
    const Icono = icono ?? ICONO_ENCABEZADO[tono];
    return (
        <section aria-label={etiqueta} data-estado={dataEstado} className="surface-card overflow-hidden p-0" aria-live="polite">
            <div className={cn("flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:p-5", FONDO[tono])}>
                <div className="flex min-w-0 flex-1 items-start gap-3">
                    <span className={cn("grid size-10 shrink-0 place-items-center rounded-full bg-card", TEXTO[tono])}>
                        <Icono aria-hidden className="size-5" />
                    </span>
                    <div className="grid min-w-0 gap-0.5">
                        <p className="text-sm text-muted-foreground">{etiqueta}</p>
                        <h2 className={cn("text-lg font-semibold", TEXTO[tono])}>{titulo}</h2>
                        {texto ? <p className="text-sm text-muted-foreground">{texto}</p> : null}
                    </div>
                </div>
                {accion ? <div className="shrink-0">{accion}</div> : null}
            </div>
            {lineas.length > 0 ? (
                <ul className="divide-y divide-border border-t border-border">
                    {lineas.map((l) => {
                        const I = ICONO_LINEA[l.tono];
                        return (
                            <li key={l.clave ?? l.titulo} data-linea={l.clave} data-tono={l.tono} className="flex items-start gap-3 px-4 py-3 sm:px-5">
                                <I aria-hidden className={cn("mt-0.5 size-4 shrink-0", TEXTO[l.tono])} />
                                <div className="grid min-w-0 gap-0.5 text-sm">
                                    <p className="font-medium">{l.titulo}</p>
                                    <p className="text-muted-foreground [overflow-wrap:anywhere]">{l.texto}</p>
                                </div>
                            </li>
                        );
                    })}
                </ul>
            ) : null}
        </section>
    );
}
