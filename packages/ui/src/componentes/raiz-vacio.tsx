"use client";

import { EmptyState } from "@heroui/react";
import type { ComponentProps } from "react";

/** El `EmptyState` de HeroUI (un componente de cliente). Uso interno de `EstadoVacio`. */
export function RaizVacio(props: ComponentProps<"div">) {
    return <EmptyState {...props} />;
}
