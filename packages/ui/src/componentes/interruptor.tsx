"use client";

import { Switch } from "@heroui/react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";
import { useMarcaNativa } from "./casilla";

export type PropsInterruptor = Omit<ComponentProps<"input">, "type"> & {
    etiqueta: ReactNode;
    ayuda?: ReactNode;
    error?: string | undefined;
};

/**
 * Interruptor de encendido/apagado: el `Switch` de HeroUI (`Switch.Content`, `Switch.Control`,
 * `Switch.Thumb`), con un `<input type="checkbox" role="switch">` real de React Aria. Para
 * ajustes que se prenden o se apagan y cambian lo que sigue («Enviar por correo», «Todo el día»);
 * para aceptar o elegir varias cosas de una lista, `Casilla`.
 *
 * Sirve igual con `register` de react-hook-form que controlado (`checked` + `onChange`), como
 * `Casilla` (`useMarcaNativa`).
 */
export function Interruptor({ etiqueta, ayuda, error, id, className, ref, checked, defaultChecked, onChange, onBlur, onFocus, name, value, disabled, required, autoFocus }: PropsInterruptor) {
    const ids = useIdsCampo(id, error, ayuda);
    const { marcada, conectar, cambiar } = useMarcaNativa({ ref, defaultChecked, onChange });
    return (
        <div data-slot="interruptor" className={cn("grid gap-1", className)}>
            <Switch
                id={ids.idCampo}
                inputRef={conectar}
                {...(name !== undefined ? { name } : {})}
                {...(value !== undefined ? { value: String(value) } : {})}
                isSelected={checked ?? marcada}
                isDisabled={disabled ?? false}
                {...(required ? { "aria-required": true } : {})}
                {...(autoFocus ? { autoFocus } : {})}
                {...(ids.control["aria-describedby"] ? { "aria-describedby": ids.control["aria-describedby"] } : {})}
                onChange={cambiar}
                {...(onBlur ? { onBlur: onBlur as unknown as ComponentProps<typeof Switch>["onBlur"] } : {})}
                {...(onFocus ? { onFocus: onFocus as unknown as ComponentProps<typeof Switch>["onFocus"] } : {})}
                className="block"
            >
                <Switch.Content className="iris-entrada-cubierta inline-flex min-h-6 cursor-pointer items-center gap-2.5 text-sm font-medium">
                    <Switch.Control className={cn("shrink-0", error && "outline outline-error")}>
                        <Switch.Thumb />
                    </Switch.Control>
                    {etiqueta}
                </Switch.Content>
            </Switch>
            <div className="grid gap-1 pl-12">
                <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
            </div>
        </div>
    );
}
