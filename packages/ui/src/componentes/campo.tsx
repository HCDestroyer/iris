"use client";

import { TextField } from "@heroui/react";
import { CircleAlert } from "lucide-react";
import { useCallback, useId, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { cn } from "../utilidades";
import { asignarRef, conAvisoValor } from "./control-nativo";
import { Entrada } from "./entrada";
import { useErrorEstable } from "./error-estable";
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

function aTexto(valor: ComponentProps<"input">["value"]): string {
    if (valor === undefined || valor === null) return "";
    return Array.isArray(valor) ? String(valor[0] ?? "") : String(valor);
}

/**
 * Etiqueta + entrada + ayuda/error: el `TextField` de HeroUI (React Aria), que enlaza la etiqueta,
 * la ayuda y el error y marca el campo inválido. Sirve igual que un `<input>`:
 *
 * - con `{...register()}`: el `ref` recibe el propio `<input>` envuelto (`conAvisoValor`), así que
 *   cuando react-hook-form escribe `value` (valores iniciales, `reset`, `setValue`) el `TextField`
 *   se entera; `onChange` y `onBlur` reciben el evento nativo del `<input>` visible;
 * - con `value` + `onChange(e)` (controlado) o con `defaultValue`.
 *
 * El resto de las props (`type`, `name`, `placeholder`, `autoComplete`, `inputMode`, `onKeyDown`,
 * `data-*`…) va directo al `<input>`.
 */
export function Campo({ etiqueta, error, ayuda, id, className, ref, value, defaultValue, disabled, readOnly, required, ...nativas }: PropsCampo) {
    // Un error que aparece al salir del campo no mueve el botón que se está pulsando.
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    const [interno, setInterno] = useState(() => aTexto(defaultValue));
    const controlado = value !== undefined;
    const valor = controlado ? aTexto(value) : interno;
    // Un solo envoltorio por input: react-hook-form compara el elemento para no registrarlo dos veces.
    const envuelto = useRef<{ input: HTMLInputElement; proxy: HTMLInputElement } | null>(null);
    const conectar = useCallback(
        (el: HTMLInputElement | null) => {
            let paraRef: HTMLInputElement | null = null;
            if (el) {
                if (envuelto.current?.input !== el) {
                    envuelto.current = { input: el, proxy: conAvisoValor(el, setInterno, () => el.focus()) };
                }
                paraRef = envuelto.current.proxy;
            }
            asignarRef(ref, paraRef);
        },
        [ref],
    );
    return (
        <TextField
            data-slot="campo"
            id={ids.idCampo}
            className={cn("grid gap-1.5", className)}
            value={valor}
            onChange={(nuevo) => {
                if (!controlado) setInterno(nuevo);
            }}
            isDisabled={disabled ?? false}
            isReadOnly={readOnly ?? false}
            isRequired={required ?? false}
            isInvalid={Boolean(visible)}
            validationBehavior="aria"
        >
            <Etiqueta>{etiqueta}</Etiqueta>
            {/* La ayuda y el error son de Iris (`PieCampo`): sus ids mandan sobre los de React Aria. */}
            <Entrada ref={conectar} aria-describedby={ids.control["aria-describedby"]} {...nativas} />
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </TextField>
    );
}
