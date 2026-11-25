"use client";

import { Label, NumberField } from "@heroui/react";
import { Minus, Plus } from "lucide-react";
import { useRef, type ReactNode } from "react";
import { I18nProvider } from "react-aria-components";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";
import { aNumero, deNumero } from "./campo-numero";
import { useControlNativo, type PropsControlNativo } from "./control-nativo";
import { useErrorEstable } from "./error-estable";

export type PropsCampoMoneda = PropsControlNativo & {
    etiqueta: ReactNode;
    ayuda?: ReactNode;
    error?: string | undefined;
    /** Moneda ISO 4217; quetzales por omisión. */
    moneda?: string;
    minimo?: number;
    maximo?: number;
    /** Paso de − y + y de las flechas (p. ej. 25 para «de 25 en 25»). 1 por omisión. */
    paso?: number;
    /** Botones − y + a los lados (como en la maqueta). Por omisión, sí. */
    conPasos?: boolean;
    placeholder?: string;
    id?: string;
    className?: string;
    disabled?: boolean | undefined;
    required?: boolean | undefined;
};

/** «Q 25.00»: el texto de los botones − / + dice cuánto suman o restan. */
function textoMonto(monto: number, moneda: string): string {
    return new Intl.NumberFormat("es-GT", { style: "currency", currency: moneda }).format(monto);
}

/**
 * Monto en dinero: el `NumberField` de HeroUI con formato de moneda (`Q150.00` en quetzales), dos
 * decimales, teclado numérico en el teléfono y − / + con su paso. El valor del formulario es el
 * texto del número (`"150"`, sin el símbolo), como `CampoNumero`: sirve con `{...register()}` y
 * con `value` + `onChange(e)`. React Aria lo confirma al salir del campo o con Enter.
 */
export function CampoMoneda({
    etiqueta,
    ayuda,
    error,
    moneda = "GTQ",
    minimo = 0,
    maximo,
    paso = 1,
    conPasos = true,
    placeholder,
    id,
    className,
    disabled,
    required,
    ...nativas
}: PropsCampoMoneda) {
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    const entrada = useRef<HTMLInputElement>(null);
    const nativo = useControlNativo(nativas, () => entrada.current?.focus());
    const texto = textoMonto(paso, moneda);
    return (
        <div className={cn("grid gap-1.5", className)} data-slot="campo-moneda" data-valor={nativo.valor}>
            <I18nProvider locale="es-GT">
                <NumberField
                    className="grid gap-1.5"
                    id={ids.idCampo}
                    value={aNumero(nativo.valor) ?? Number.NaN}
                    onChange={(n) => nativo.cambiar(deNumero(n))}
                    onBlur={() => nativo.salir()}
                    formatOptions={{ style: "currency", currency: moneda, currencyDisplay: "symbol", minimumFractionDigits: 2, maximumFractionDigits: 2 }}
                    minValue={minimo}
                    {...(maximo !== undefined ? { maxValue: maximo } : {})}
                    step={paso}
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
                            <NumberField.DecrementButton aria-label={`Restar ${texto}`}>
                                <Minus aria-hidden className="size-4" />
                            </NumberField.DecrementButton>
                        ) : null}
                        <NumberField.Input ref={entrada} placeholder={placeholder} className="text-center text-base font-semibold tabular-nums sm:text-sm" />
                        {conPasos ? (
                            <NumberField.IncrementButton aria-label={`Sumar ${texto}`}>
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
