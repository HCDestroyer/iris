import { CircleAlert } from "lucide-react";
import { useId, type ComponentProps, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Entrada } from "./entrada";
import { Etiqueta } from "./etiqueta";

export type PropsCampo = ComponentProps<"input"> & {
    etiqueta: ReactNode;
    /** Mensaje de error del campo (react-hook-form o errors[] del problem+json). */
    error?: string | undefined;
    ayuda?: ReactNode;
};

/** Etiqueta + entrada + ayuda/error, enlazados por id y aria-describedby. */
export function Campo({ etiqueta, error, ayuda, id, className, ...props }: PropsCampo) {
    const generado = useId();
    const idCampo = id ?? generado;
    const idAyuda = `${idCampo}-ayuda`;
    const idError = `${idCampo}-error`;
    const describe = [ayuda ? idAyuda : null, error ? idError : null].filter(Boolean).join(" ");
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={idCampo}>{etiqueta}</Etiqueta>
            <Entrada
                id={idCampo}
                aria-invalid={error ? true : undefined}
                aria-describedby={describe || undefined}
                {...props}
            />
            {ayuda ? (
                <p id={idAyuda} className="text-sm text-muted-foreground">
                    {ayuda}
                </p>
            ) : null}
            {error ? (
                <p id={idError} className="flex items-center gap-1.5 text-sm text-error">
                    <CircleAlert aria-hidden className="size-4 shrink-0" />
                    {error}
                </p>
            ) : null}
        </div>
    );
}
