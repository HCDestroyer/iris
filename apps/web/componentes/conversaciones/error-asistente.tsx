"use client";

import { Boton, cn } from "@iris/ui";
import { RotateCw } from "lucide-react";
import type { ExplicacionError } from "@/lib/asistente/presentacion";
import { useAhora } from "./reloj";

/**
 * Error del asistente (sugerencia o resumen) con «Reintentar» si sirve. Tras un 429 cuenta los
 * segundos que faltan y deja «Reintentar» deshabilitado hasta `esperarHasta`.
 */
export function ErrorAsistente({ explicacion, alReintentar, ocupado = false, className }: { explicacion: ExplicacionError; alReintentar: () => void; ocupado?: boolean; className?: string }) {
    const ahora = useAhora(1000);
    const faltan = explicacion.esperarHasta ? Math.max(0, Math.ceil((explicacion.esperarHasta - ahora) / 1000)) : 0;
    const texto = explicacion.esperarHasta ? (faltan > 0 ? `Vuelve a intentar en ${faltan} s.` : "Ya puedes volver a intentarlo.") : explicacion.texto;
    return (
        <div role="alert" data-espera={faltan > 0 ? faltan : undefined} className={cn("grid gap-2", className)}>
            <p className="text-sm">
                <span className="font-semibold">{explicacion.titulo}.</span> <span className="text-muted-foreground">{texto}</span>
            </p>
            {explicacion.reintentar ? (
                <div>
                    <Boton variante="secundario" tamano="sm" onClick={alReintentar} disabled={ocupado || faltan > 0}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                </div>
            ) : null}
        </div>
    );
}
