"use client";

import { Label } from "@heroui/react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/** Etiqueta de un campo (HeroUI `Label`, un `<label>` nativo de React Aria). */
export function Etiqueta({ className, ...props }: ComponentProps<"label">) {
    return <Label data-slot="etiqueta" className={cn("text-sm font-medium text-foreground select-none", className)} {...props} />;
}
