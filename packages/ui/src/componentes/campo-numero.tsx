"use client";

import { Label, NumberField } from "@heroui/react";
import { Minus, Plus } from "lucide-react";
import { useRef, type ReactNode } from "react";
import { I18nProvider } from "react-aria-components";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";
import { useControlNativo, type PropsControlNativo } from "./control-nativo";
import { useErrorEstable } from "./error-estable";

/** Texto del formulario → número; `null` si está vacío o no es un número. */
export function aNumero(valor: string): number | null {
    if (valor.trim() === "") return null;
    const n = Number(valor);
    return Number.isFinite(n) ? n : null;
}

/** Número → texto del formulario (`250.5`, sin separador de miles); vacío si no hay número. */
export function deNumero(n: number | null | undefined): string {
    return n === null || n === undefined || Number.isNaN(n) ? "" : String(n);
}

export type PropsCampoNumero = PropsControlNativo & {
    etiqueta: ReactNode;
    ayuda?: ReactNode;
    error?: string | undefined;
    /** Decimales permitidos (2 para montos y cantidades). */
    decimales?: number;
    minimo?: number;
    maximo?: number;
    /** Botones − y + (cantidades). Los montos no los llevan. */
    conPasos?: boolean;
    paso?: number;
    placeholder?: string;
    id?: string;
    className?: string;
    disabled?: boolean | undefined;
    required?: boolean | undefined;
};

/**
 * Cantidad o monto: el `NumberField` de HeroUI (React Aria), con teclado numérico en el teléfono,
 * flechas para subir y bajar y formato de Guatemala (punto decimal). El valor del formulario es
 * el texto del número (`"250.5"`), como el de un input: sirve con `{...register()}` y con
 * `value` + `onChange(e)`. React Aria lo confirma al salir del campo o con Enter.
 */
export function CampoNumero({ etiqueta, ayuda, error, decimales = 2, minimo, maximo, conPasos = false, paso, placeholder, id, className, disabled, required, ...nativas }: PropsCampoNumero) {
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    const entrada = useRef<HTMLInputElement>(null);
    const nativo = useControlNativo(nativas, () => entrada.current?.focus());
    return (
        <div className={cn("grid gap-1.5", className)} data-valor={nativo.valor}>
            <I18nProvider locale="es-GT">
                <NumberField
                    data-slot="campo-numero"
                    className="grid gap-1.5"
                    id={ids.idCampo}
                    value={aNumero(nativo.valor) ?? Number.NaN}
                    onChange={(n) => nativo.cambiar(deNumero(n))}
                    onBlur={() => nativo.salir()}
                    formatOptions={{ maximumFractionDigits: decimales, useGrouping: false }}
                    {...(minimo !== undefined ? { minValue: minimo } : {})}
                    {...(maximo !== undefined ? { maxValue: maximo } : {})}
                    {...(paso !== undefined ? { step: paso } : {})}
                    isDisabled={disabled ?? false}
                    isRequired={required ?? false}
                    isInvalid={Boolean(visible)}
                    validationBehavior="aria"
                    {...(ids.control["aria-describedby"] ? { "aria-describedby": ids.control["aria-describedby"] } : {})}
                    fullWidth
                >
                    <Label className="text-sm font-medium">{etiqueta}</Label>
                    <NumberField.Group className="h-10 shadow-none">
                        {conPasos ? (
                            <NumberField.DecrementButton aria-label="Restar uno">
                                <Minus aria-hidden className="size-4" />
                            </NumberField.DecrementButton>
                        ) : null}
                        <NumberField.Input ref={entrada} placeholder={placeholder} className="text-base sm:text-sm" />
                        {conPasos ? (
                            <NumberField.IncrementButton aria-label="Sumar uno">
                                <Plus aria-hidden className="size-4" />
                            </NumberField.IncrementButton>
                        ) : null}
                    </NumberField.Group>
                </NumberField>
            </I18nProvider>
            {nativo.oculto}
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </div>
    );
}
