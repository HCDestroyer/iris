"use client";

import { InputOTP } from "@heroui/react";
import { CircleAlert, Eye, EyeOff, type LucideIcon } from "lucide-react";
import { useState, type ComponentProps } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { Entrada } from "./entrada";
import { useErrorEstable } from "./error-estable";
import { Etiqueta } from "./etiqueta";

/*
 * Campos de acceso (la entrada de la app de campo): la contraseña con el ojo para mostrarla u
 * ocultarla y un icono opcional a la izquierda, y el código de verificación de 6 dígitos con el
 * `InputOTP` de HeroUI (una casilla por dígito, pegar el código completo, `one-time-code`).
 */

export type PropsCampoConIcono = Omit<ComponentProps<"input">, "type"> &
    PropsEnvolturaCampo & {
        /** Icono de Lucide a la izquierda (sobre, candado). */
        icono?: LucideIcon;
        /** Contraseña: oculta lo escrito y lleva el ojo (44 px) para mostrarlo u ocultarlo. */
        secreto?: boolean;
        type?: ComponentProps<"input">["type"];
    };

/** Etiqueta + entrada con icono (y ojo si es contraseña) + ayuda/error en línea. */
export function CampoConIcono({ etiqueta, error, ayuda, id, className, icono: Icono, secreto = false, type, ...props }: PropsCampoConIcono) {
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    const [mostrar, setMostrar] = useState(false);
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <div className="relative">
                {Icono ? <Icono aria-hidden className={cn("pointer-events-none absolute top-1/2 left-3 size-5 -translate-y-1/2", visible ? "text-error" : "text-muted-foreground")} /> : null}
                <Entrada
                    {...ids.control}
                    {...props}
                    type={secreto ? (mostrar ? "text" : "password") : type}
                    className={cn("h-12 text-base", Icono && "pl-10", secreto && "pr-12")}
                />
                {secreto ? (
                    <Boton
                        variante="fantasma"
                        tamano="icono"
                        aria-label={mostrar ? "Ocultar contraseña" : "Mostrar contraseña"}
                        aria-pressed={mostrar}
                        aria-controls={ids.idCampo}
                        className="absolute top-1/2 right-0.5 size-11 -translate-y-1/2 md:size-11"
                        onClick={() => setMostrar((m) => !m)}
                    >
                        {mostrar ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
                    </Boton>
                ) : null}
            </div>
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </div>
    );
}

export type PropsCampoCodigo = {
    etiqueta: string;
    valor: string;
    alCambiar: (valor: string) => void;
    alCompletar?: (codigo: string) => void;
    error?: string | undefined;
    largo?: number;
    autoFocus?: boolean;
    /** Nombre del campo (formularios). */
    name?: string;
};

/** Código de verificación: HeroUI `InputOTP` de `largo` casillas, solo dígitos, error en línea. */
export function CampoCodigo({ etiqueta, valor, alCambiar, alCompletar, error, largo = 6, autoFocus = true, name }: PropsCampoCodigo) {
    const ids = useIdsCampo(undefined, error, undefined);
    return (
        <div className="grid gap-2">
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <InputOTP
                id={ids.idCampo}
                maxLength={largo}
                value={valor}
                onChange={(v: string) => alCambiar(v.replace(/\D/g, ""))}
                {...(alCompletar ? { onComplete: alCompletar } : {})}
                pattern="^[0-9]*$"
                inputMode="numeric"
                autoComplete="one-time-code"
                autoFocus={autoFocus}
                isInvalid={Boolean(error)}
                aria-label={etiqueta}
                aria-describedby={error ? ids.idError : undefined}
                pasteTransformer={(t: string) => t.replace(/\D/g, "").slice(0, largo)}
                {...(name ? { name } : {})}
            >
                <InputOTP.Group className="flex w-full justify-between gap-2">
                    {Array.from({ length: largo }, (_, i) => (
                        <InputOTP.Slot key={i} index={i} className="h-14 min-w-11 flex-1 rounded-[0.875rem] text-xl font-semibold" />
                    ))}
                </InputOTP.Group>
            </InputOTP>
            {error ? (
                <p id={ids.idError} data-slot="error" role="alert" className="flex items-center gap-1.5 text-sm text-error">
                    <CircleAlert aria-hidden className="size-4 shrink-0" />
                    {error}
                </p>
            ) : null}
        </div>
    );
}
