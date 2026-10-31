"use client";

import { Input } from "@heroui/react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/**
 * Campo de texto: HeroUI `Input` (un `<input>` nativo con los estados de React Aria), así que
 * `register` de react-hook-form, `ref` y los eventos nativos funcionan igual. 16 px en el
 * teléfono para que iOS no haga zoom.
 */
export function Entrada({ className, type = "text", ...props }: ComponentProps<"input">) {
    const invalido = props["aria-invalid"] === true || props["aria-invalid"] === "true";
    return (
        <Input
            data-slot="entrada"
            type={type}
            fullWidth
            data-invalid={invalido ? "true" : undefined}
            className={cn("h-10 min-w-0 disabled:cursor-not-allowed aria-invalid:border-error", className)}
            {...props}
        />
    );
}
