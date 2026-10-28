import { Label } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

export function Etiqueta({ className, ...props }: ComponentProps<typeof Label.Root>) {
    return (
        <Label.Root
            data-slot="etiqueta"
            className={cn("text-sm font-medium text-foreground select-none", className)}
            {...props}
        />
    );
}
