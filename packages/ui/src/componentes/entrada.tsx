import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/** Campo de texto (16 px en formularios para que iOS no haga zoom). */
export function Entrada({ className, type = "text", ...props }: ComponentProps<"input">) {
    return (
        <input
            data-slot="entrada"
            type={type}
            className={cn(
                "h-10 w-full min-w-0 rounded-md border border-input bg-background px-3 text-base text-foreground",
                "placeholder:text-dimmed transition-[border-color,box-shadow] duration-150",
                "focus-visible:border-primary focus-visible:outline-2 focus-visible:outline-offset-0",
                "disabled:cursor-not-allowed disabled:opacity-55",
                "aria-invalid:border-error",
                className,
            )}
            {...props}
        />
    );
}
