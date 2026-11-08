"use client";

import { Boton } from "@iris/ui";
import { Lock } from "lucide-react";
import { useId } from "react";
import { fechaCorta } from "@/lib/numeros/conexion";

/**
 * Una credencial guardada, sin su valor: «•••• guardado el 3 nov. 2026» con su acción («Cambiar»,
 * «Generar otro»). Iris nunca devuelve el valor (contrato §1.1).
 */
export function SecretoGuardado({
    etiqueta,
    guardadoEn,
    accion,
    alAccion,
    ocupado = false,
}: {
    etiqueta: string;
    guardadoEn: string | null;
    accion: string;
    alAccion: () => void;
    ocupado?: boolean;
}) {
    const id = useId();
    return (
        <div className="grid gap-1.5">
            <p id={id} className="text-sm font-medium">
                {etiqueta}
            </p>
            <div
                role="group"
                aria-labelledby={id}
                className="flex min-h-10 min-w-0 items-center gap-2 rounded-[var(--hx-radius)] border border-dashed border-border px-3 py-1 text-sm text-muted-foreground"
            >
                <Lock aria-hidden className="size-4 shrink-0" />
                <span className="min-w-0 flex-1">
                    <span aria-hidden>•••• </span>
                    {guardadoEn ? `guardado el ${fechaCorta(guardadoEn)}` : "guardado"}
                </span>
                <Boton variante="enlace" tamano="sm" className="h-auto shrink-0 px-0" onClick={alAccion} disabled={ocupado} aria-label={`${accion}: ${etiqueta.toLowerCase()}`}>
                    {ocupado ? `${accion}…` : accion}
                </Boton>
            </div>
        </div>
    );
}
