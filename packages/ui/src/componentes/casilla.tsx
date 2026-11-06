"use client";

import { Checkbox } from "@heroui/react";
import { Check } from "lucide-react";
import { useCallback, useRef, useState, type ChangeEvent, type ComponentProps, type ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";
import { asignarRef } from "./control-nativo";

export type PropsCasilla = Omit<ComponentProps<"input">, "type"> & {
    etiqueta: ReactNode;
    ayuda?: ReactNode;
    error?: string | undefined;
    /** `lg`: etiqueta de al menos 44 px de alto (zona táctil de la app de campo). */
    tamano?: "md" | "lg";
};

/**
 * Casilla de verificación: el `Checkbox` de HeroUI (`Checkbox`, `Checkbox.Content`,
 * `Checkbox.Control`, `Checkbox.Indicator`) con un `<input type="checkbox">` real de React Aria,
 * etiqueta clicable, ayuda y error. El color de marca solo marca la selección.
 *
 * Sirve igual con `register` de react-hook-form que controlada (`checked` + `onChange`):
 * - `onChange` recibe un evento con `target` = el `<input>` real;
 * - react-hook-form escribe `checked` directo en el elemento que recibe por `ref` (valores
 *   iniciales, `reset`, `setValue`), sin pasar por React. Por eso recibe el input envuelto
 *   (`conAviso`): cada escritura de `checked` también actualiza el estado de la casilla, y React
 *   Aria la dibuja igual que el input.
 */

/** Envuelve el input para enterarse cuando alguien le escribe `checked` (react-hook-form). */
function conAviso(input: HTMLInputElement, alMarcar: (marcada: boolean) => void): HTMLInputElement {
    return new Proxy(input, {
        get(destino, clave) {
            const valor: unknown = Reflect.get(destino, clave, destino);
            return typeof valor === "function" ? (valor as (...args: unknown[]) => unknown).bind(destino) : valor;
        },
        set(destino, clave, valor: unknown) {
            const hecho = Reflect.set(destino, clave, valor, destino);
            if (clave === "checked") alMarcar(Boolean(valor));
            return hecho;
        },
    });
}
/**
 * Estado de una casilla nativa (`Checkbox`, `Switch`): el `ref` envuelto para quien la usa, el
 * estado que sigue a react-hook-form y el evento de cambio con `target` = el input real.
 */
export function useMarcaNativa({ ref, defaultChecked, onChange }: Pick<ComponentProps<"input">, "ref" | "defaultChecked" | "onChange">) {
    const input = useRef<HTMLInputElement>(null);
    // Un solo envoltorio por input: react-hook-form compara el elemento para no registrarlo dos veces.
    const envuelto = useRef<{ input: HTMLInputElement; proxy: HTMLInputElement } | null>(null);
    const [marcada, setMarcada] = useState(defaultChecked ?? false);
    // El `<input>` real, envuelto, para el `ref` de quien usa la casilla (p. ej. `register`).
    const conectar = useCallback(
        (el: HTMLInputElement | null) => {
            input.current = el;
            let paraRef: HTMLInputElement | null = null;
            if (el) {
                if (envuelto.current?.input !== el) envuelto.current = { input: el, proxy: conAviso(el, setMarcada) };
                paraRef = envuelto.current.proxy;
            }
            asignarRef(ref, paraRef);
        },
        [ref],
    );
    const cambiar = (valor: boolean) => {
        setMarcada(valor);
        const el = input.current;
        if (el && onChange) onChange({ target: el, currentTarget: el, type: "change" } as unknown as ChangeEvent<HTMLInputElement>);
    };
    return { marcada, conectar, cambiar };
}

export function Casilla({
    etiqueta,
    ayuda,
    error,
    id,
    className,
    tamano = "md",
    ref,
    checked,
    defaultChecked,
    onChange,
    onBlur,
    onFocus,
    name,
    value,
    disabled,
    required,
    autoFocus,
}: PropsCasilla) {
    const ids = useIdsCampo(id, error, ayuda);
    const { marcada, conectar, cambiar } = useMarcaNativa({ ref, defaultChecked, onChange });
    const grande = tamano === "lg";
    return (
        <div data-slot="casilla" className={cn("grid gap-1", className)}>
            <Checkbox
                id={ids.idCampo}
                inputRef={conectar}
                {...(name !== undefined ? { name } : {})}
                {...(value !== undefined ? { value: String(value) } : {})}
                isSelected={checked ?? marcada}
                isDisabled={disabled ?? false}
                isRequired={required ?? false}
                isInvalid={Boolean(error)}
                {...(autoFocus ? { autoFocus } : {})}
                {...(ids.control["aria-describedby"] ? { "aria-describedby": ids.control["aria-describedby"] } : {})}
                onChange={cambiar}
                {...(onBlur ? { onBlur: onBlur as unknown as ComponentProps<typeof Checkbox>["onBlur"] } : {})}
                {...(onFocus ? { onFocus: onFocus as unknown as ComponentProps<typeof Checkbox>["onFocus"] } : {})}
                className="block"
            >
                <Checkbox.Content className={cn("iris-entrada-cubierta group inline-flex min-h-6 cursor-pointer items-center gap-2.5 text-sm font-medium", grande && "min-h-11 text-base")}>
                    <Checkbox.Control
                        className={cn(
                            "size-4 shrink-0 rounded-sm border border-input bg-background shadow-none",
                            grande && "size-5",
                            "group-has-[input:checked]:border-primary group-has-[input:checked]:bg-primary group-has-[input:checked]:text-primary-foreground",
                            "group-has-[input:focus-visible]:outline-2 group-has-[input:focus-visible]:outline-offset-2 group-has-[input:focus-visible]:outline-primary",
                            "group-has-[input:disabled]:cursor-not-allowed group-has-[input:disabled]:opacity-55",
                            error && "border-error",
                        )}
                    >
                        <Checkbox.Indicator className="size-full scale-100 opacity-0 group-has-[input:checked]:opacity-100">
                            <Check aria-hidden className="size-full p-px" strokeWidth={3} />
                        </Checkbox.Indicator>
                    </Checkbox.Control>
                    {etiqueta}
                </Checkbox.Content>
            </Checkbox>
            <div className={cn("grid gap-1", grande ? "pl-7.5" : "pl-6.5")}>
                <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
            </div>
        </div>
    );
}
