"use client";

import { Skeleton } from "@heroui/react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/**
 * Marcador de carga: el `Skeleton` de HeroUI (pulso). Nunca se rellena con ceros: es un hueco
 * hasta que llega el dato.
 */
export function Esqueleto({ className, ...props }: ComponentProps<"div">) {
    return <Skeleton data-slot="esqueleto" aria-hidden animationType="pulse" className={cn("rounded-md bg-elevated", className)} {...props} />;
}
