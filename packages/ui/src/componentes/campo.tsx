import { CircleAlert } from "lucide-react";
import { useId, type ComponentProps, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Entrada } from "./entrada";
import { Etiqueta } from "./etiqueta";

/** Props comunes de un campo con etiqueta, ayuda y error. */
export type PropsEnvolturaCampo = {
    etiqueta: ReactNode;
    /** Mensaje de error del campo (react-hook-form o errors[] del problem+json). */
    error?: string | undefined;
    ayuda?: ReactNode;
};

/** Ids enlazados (control, ayuda, error) y `aria-describedby` de un campo. */
export function useIdsCampo(id: string | undefined, error: string | undefined, ayuda: ReactNode) {
    const generado = useId();
    const idCampo = id ?? generado;
    const idAyuda = `${idCampo}-ayuda`;
    const idError = `${idCampo}-error`;
    const describe = [ayuda ? idAyuda : null, error ? idError : null].filter(Boolean).join(" ");
    return {
        idCampo,
        idAyuda,
        idError,
        control: { id: idCampo, "aria-invalid": error ? true : undefined, "aria-describedby": describe || undefined },
    };
}

/** Ayuda y error bajo el control. */
export function PieCampo({ idAyuda, idError, ayuda, error }: { idAyuda: string; idError: string; ayuda?: ReactNode; error?: string | undefined }) {
    return (
        <>
            {ayuda ? (
                <p id={idAyuda} data-slot="ayuda" className="description text-sm text-muted-foreground">
                    {ayuda}
                </p>
            ) : null}
            {error ? (
                <p id={idError} data-slot="error" className="flex items-center gap-1.5 text-sm text-error">
                    <CircleAlert aria-hidden className="size-4 shrink-0" />
                    {error}
                </p>
            ) : null}
        </>
    );
}

export type PropsCampo = ComponentProps<"input"> & PropsEnvolturaCampo;

/** Etiqueta + entrada + ayuda/error, enlazados por id y aria-describedby. */
export function Campo({ etiqueta, error, ayuda, id, className, ...props }: PropsCampo) {
    const ids = useIdsCampo(id, error, ayuda);
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <Entrada {...ids.control} {...props} />
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
        </div>
    );
}
