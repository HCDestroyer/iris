"use client";

import { avisar, Boton, cn } from "@iris/ui";
import { Copy } from "lucide-react";
import { useId, useRef, type ReactNode } from "react";

/**
 * Valor de solo lectura para pegar en Meta (URL del webhook, token de verificación) con «Copiar».
 * Si el navegador no deja copiar, selecciona el texto para copiarlo a mano. El valor nunca se
 * escribe en un log ni en la URL; el aviso dice qué se copió, no el valor.
 */
export function CampoCopiar({
    etiqueta,
    valor,
    queSeCopio,
    ayuda,
    acciones,
    placeholder,
}: {
    etiqueta: string;
    /** `null`: todavía no hay valor (se muestra `placeholder`, sin «Copiar»). */
    valor: string | null;
    /** «URL copiada», «Token de verificación copiado». */
    queSeCopio: string;
    ayuda?: ReactNode;
    acciones?: ReactNode;
    placeholder?: string;
}) {
    const id = useId();
    const caja = useRef<HTMLElement>(null);

    async function copiar() {
        if (!valor) return;
        try {
            await navigator.clipboard.writeText(valor);
            avisar({ titulo: queSeCopio });
        } catch {
            const nodo = caja.current;
            const seleccion = window.getSelection();
            if (nodo && seleccion) {
                const rango = document.createRange();
                rango.selectNodeContents(nodo);
                seleccion.removeAllRanges();
                seleccion.addRange(rango);
            }
            avisar({ titulo: "No se pudo copiar: el texto quedó seleccionado, cópialo con Ctrl + C" });
        }
    }

    return (
        <div className="grid min-w-0 gap-1.5">
            <p id={`${id}-etiqueta`} className="text-sm font-medium">
                {etiqueta}
            </p>
            <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
                <code
                    ref={caja}
                    aria-labelledby={`${id}-etiqueta`}
                    aria-describedby={ayuda ? `${id}-ayuda` : undefined}
                    data-valor-copiable
                    className={cn(
                        "flex min-h-10 min-w-0 flex-1 items-center rounded-[var(--hx-radius)] border border-border bg-background-secondary px-3 py-2 font-mono text-sm break-all select-all",
                        !valor && "text-muted-foreground select-none",
                    )}
                >
                    {valor ?? placeholder}
                </code>
                {valor || acciones ? (
                    <div className="flex shrink-0 gap-2 [&>*]:flex-1 sm:[&>*]:flex-none">
                        {valor ? (
                            <Boton variante="contorno" onClick={copiar} aria-label={`Copiar ${etiqueta.toLowerCase()}`}>
                                <Copy aria-hidden />
                                Copiar
                            </Boton>
                        ) : null}
                        {acciones}
                    </div>
                ) : null}
            </div>
            {ayuda ? (
                <p id={`${id}-ayuda`} className="text-sm text-muted-foreground">
                    {ayuda}
                </p>
            ) : null}
        </div>
    );
}
