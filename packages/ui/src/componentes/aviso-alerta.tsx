"use client";

import { Alert } from "@heroui/react";
import { alertVariants } from "@heroui/styles";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";

type PropsAlertaAviso = Omit<ComponentProps<"div">, "title"> & {
    status: "default" | "success" | "warning" | "danger";
    /** El icono, ya dibujado (así `Aviso` sirve desde un componente de servidor). */
    indicador: ReactNode;
    titulo: ReactNode;
    claseTitulo: string;
    accion?: ReactNode;
};

/** Las piezas del `Alert` de HeroUI con el acomodo de Iris. Uso interno de `Aviso`. */
export function AlertaAviso({ status, indicador, titulo, claseTitulo, accion, className, children, ...props }: PropsAlertaAviso) {
    return (
        <Alert status={status} className={className} {...props}>
            <div className="flex min-w-0 flex-1 gap-2.5">
                <Alert.Indicator className={cn("mt-0.5 box-content size-4 shrink-0 p-0", claseTitulo)}>{indicador}</Alert.Indicator>
                <Alert.Content className="grid min-w-0 gap-0.5">
                    <Alert.Title className={cn("leading-normal", claseTitulo)}>{titulo}</Alert.Title>
                    {children ? (
                        // La descripción puede traer párrafos o listas: un `<div>` con la pieza de
                        // HeroUI (`Alert.Description` es un `<span>`).
                        <div data-slot="alert-description" className={alertVariants({ status }).description({ className: "block text-sm text-foreground" })}>
                            {children}
                        </div>
                    ) : null}
                </Alert.Content>
            </div>
            {accion ? <div className="shrink-0 sm:ml-2">{accion}</div> : null}
        </Alert>
    );
}
