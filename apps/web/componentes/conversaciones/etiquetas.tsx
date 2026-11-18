"use client";

import { cn, ListaRecortada } from "@iris/ui";
import { CLASE_ETIQUETA } from "@/lib/conversaciones/presentacion";
import type { Conversacion } from "@/lib/conversaciones/tipos";

type Etiqueta = NonNullable<Conversacion["etiquetas"]>[number];

/**
 * Una etiqueta de conversación. Nunca se sale de donde está: el nombre largo se recorta con
 * puntos y el nombre completo queda en el título (y lo leen los lectores de pantalla).
 */
export function ChipEtiqueta({ etiqueta, className }: { etiqueta: Etiqueta; className?: string }) {
    return (
        <span
            data-etiqueta={etiqueta.id}
            title={etiqueta.nombre}
            className={cn("inline-flex h-[1.375rem] max-w-full min-w-0 items-center rounded-md px-2 text-xs font-medium", CLASE_ETIQUETA[etiqueta.color] ?? CLASE_ETIQUETA.neutro, className)}
        >
            <span data-slot="etiqueta-texto" className="truncate">
                {etiqueta.nombre}
            </span>
        </span>
    );
}

/** Todas las etiquetas, en las líneas que hagan falta (cabecera del hilo y ficha del cliente). */
export function EtiquetasEnvueltas({ etiquetas, slot = "etiquetas", className }: { etiquetas: readonly Etiqueta[]; slot?: string; className?: string }) {
    if (etiquetas.length === 0) return null;
    return (
        <ul aria-label="Etiquetas" data-slot={slot} className={cn("flex min-w-0 flex-wrap gap-1.5", className)}>
            {etiquetas.map((e) => (
                <li key={e.id} className="flex max-w-full min-w-0">
                    <ChipEtiqueta etiqueta={e} />
                </li>
            ))}
        </ul>
    );
}

/** En la fila de la lista: una sola línea con las que caben y «+N»; todas, en el título. */
export function EtiquetasEnFila({ etiquetas, className }: { etiquetas: readonly Etiqueta[]; className?: string }) {
    if (etiquetas.length === 0) return null;
    const todas = etiquetas.map((e) => e.nombre).join(", ");
    return (
        <span className={cn("flex min-w-0", className)}>
            <span className="sr-only">Etiquetas: {todas}.</span>
            <ListaRecortada
                aria-hidden
                data-slot="etiquetas-fila"
                title={todas}
                className="w-full"
                elementos={etiquetas.map((e) => ({ clave: e.id, nodo: <ChipEtiqueta etiqueta={e} className="max-w-[9rem]" /> }))}
            />
        </span>
    );
}
