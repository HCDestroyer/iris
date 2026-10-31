"use client";

import { TextArea } from "@heroui/react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { Etiqueta } from "./etiqueta";

/** Texto de varias líneas: HeroUI `TextArea` (`<textarea>` nativo). 16 px en el teléfono. */
export function AreaTexto({ className, rows = 4, ...props }: ComponentProps<"textarea">) {
    const invalido = props["aria-invalid"] === true || props["aria-invalid"] === "true";
    return (
        <TextArea
            data-slot="area-texto"
            rows={rows}
            fullWidth
            data-invalid={invalido ? "true" : undefined}
            className={cn("min-h-20 min-w-0 disabled:cursor-not-allowed aria-invalid:border-error", className)}
            {...props}
        />
    );
}

export type PropsCampoArea = ComponentProps<"textarea"> & PropsEnvolturaCampo;

/** Etiqueta + área de texto + ayuda/error. */
export function CampoArea({ etiqueta, error, ayuda, id, className, ...props }: PropsCampoArea) {
    const ids = useIdsCampo(id, error, ayuda);
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <AreaTexto {...ids.control} {...props} />
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
        </div>
    );
}
