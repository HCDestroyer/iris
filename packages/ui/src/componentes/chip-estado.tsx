"use client";

import { Chip } from "@heroui/react";
import type { ComponentProps, ReactNode } from "react";
import type { Tono } from "./tonos";

type PropsChipEstado = Omit<ComponentProps<"span">, "color" | "children"> & {
    color: "default" | "success" | "warning" | "danger";
    tono: Tono;
    texto: string;
    /** El icono, ya dibujado (así sirve desde un componente de servidor). */
    children: ReactNode;
};

/**
 * `Chip` suave de HeroUI con el `data-slot="insignia-estado"` de Iris (HeroUI pone el suyo,
 * `chip`, después de las props: se fija en el `<span>` final). Uso interno de `InsigniaEstado`.
 */
export function ChipEstado({ color, tono, texto, children, ...props }: PropsChipEstado) {
    return (
        <Chip
            color={color}
            variant="soft"
            size="sm"
            {...props}
            data-tono={tono}
            render={(dom) => <span {...(dom as ComponentProps<"span">)} data-slot="insignia-estado" />}
        >
            {children}
            <Chip.Label className="p-0">{texto}</Chip.Label>
        </Chip>
    );
}
