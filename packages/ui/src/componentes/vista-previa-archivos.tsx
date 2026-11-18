"use client";

import { AlertTriangle, Check, FileText, Film, RotateCw, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { Dialogo, DialogoContenido } from "./dialogo";

export type ArchivoEnVista = {
    id: string;
    nombre: string;
    /** «PDF · 1,2 MB». */
    detalle: string;
    clase: "imagen" | "video" | "documento";
    /**
     * Solo para imágenes de mapa de bits: una URL de objeto (`URL.createObjectURL`) que quien usa
     * el componente crea y libera. Sin ella se pinta un icono (un SVG o un HTML nunca se incrustan).
     */
    url?: string | undefined;
    estado: "listo" | "invalido" | "subiendo" | "enviando" | "error";
    /** Por qué no sirve (`invalido`) o qué falló (`error`). */
    mensaje?: string | undefined;
    /** De 0 a 1, mientras sube. */
    progreso?: number | undefined;
};

export type PropsVistaPreviaArchivos = {
    /** Nombre accesible de la zona: «Archivos por enviar». */
    etiqueta: string;
    archivos: readonly ArchivoEnVista[];
    alQuitar: (id: string) => void;
    alReintentar?: (id: string) => void;
    /** Mientras se envía no se puede quitar lo que ya está subiendo. */
    className?: string;
    /** Debajo de la lista (un aviso, un contador). */
    children?: ReactNode;
};

/**
 * Vista previa de los archivos elegidos antes de enviarlos: miniatura ampliable para imágenes,
 * icono para documentos y videos, nombre, tipo y tamaño, y el estado de cada uno con icono, texto
 * y color (no sirve, subiendo con su barra, falló con «Reintentar»). Cada archivo se puede quitar.
 * Es solo presentación: los nombres van como texto (nunca HTML) y las URL las maneja quien la usa.
 */
export function VistaPreviaArchivos({ etiqueta, archivos, alQuitar, alReintentar, className, children }: PropsVistaPreviaArchivos) {
    const [ampliada, setAmpliada] = useState<string | null>(null);
    const grande = archivos.find((a) => a.id === ampliada && a.url);
    if (archivos.length === 0) return null;
    return (
        <div role="group" aria-label={etiqueta} data-slot="vista-previa-archivos" className={cn("grid min-w-0 gap-1.5", className)}>
            <ul className="grid max-h-56 min-w-0 gap-1.5 overflow-y-auto">
                {archivos.map((a) => {
                    const ocupado = a.estado === "subiendo" || a.estado === "enviando";
                    const porciento = Math.round(Math.min(1, Math.max(0, a.progreso ?? 0)) * 100);
                    return (
                        <li
                            key={a.id}
                            data-archivo={a.id}
                            data-estado={a.estado}
                            className={cn(
                                "grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-2.5 rounded-lg border p-1.5",
                                a.estado === "invalido" || a.estado === "error" ? "border-error/40 bg-error/5" : "border-border bg-background-secondary",
                            )}
                        >
                            {a.url ? (
                                <Boton variante="fantasma" className="size-12 shrink-0 overflow-hidden rounded-md p-0" aria-label={`Ampliar ${a.nombre}`} title="Ampliar" onClick={() => setAmpliada(a.id)}>
                                    {/* Vista previa local (URL de objeto): no pasa por el optimizador de imágenes. */}
                                    <img src={a.url} alt="" className="size-full object-cover" />
                                </Boton>
                            ) : (
                                <span aria-hidden className="flex size-12 shrink-0 items-center justify-center rounded-md bg-elevated text-muted-foreground">
                                    {a.clase === "video" ? <Film className="size-5" /> : <FileText className="size-5" />}
                                </span>
                            )}
                            <span className="grid min-w-0 gap-0.5">
                                <span className="truncate text-[0.8125rem] font-semibold" title={a.nombre} data-slot="archivo-nombre">
                                    {a.nombre}
                                </span>
                                {a.estado === "invalido" || a.estado === "error" ? (
                                    <span className="flex min-w-0 items-start gap-1 text-xs text-error" role={a.estado === "error" ? "alert" : undefined}>
                                        <AlertTriangle aria-hidden className="mt-px size-3.5 shrink-0" />
                                        <span className="min-w-0 [overflow-wrap:anywhere]">{a.mensaje ?? (a.estado === "invalido" ? "No se puede enviar." : "No se pudo subir.")}</span>
                                    </span>
                                ) : ocupado ? (
                                    <span className="grid min-w-0 gap-1 text-xs text-muted-foreground">
                                        <span className="truncate">{a.estado === "enviando" ? "Enviando…" : `Subiendo… ${porciento} %`}</span>
                                        <span
                                            role="progressbar"
                                            aria-label={`Subida de ${a.nombre}`}
                                            aria-valuemin={0}
                                            aria-valuemax={100}
                                            aria-valuenow={a.estado === "enviando" ? 100 : porciento}
                                            className="h-1 overflow-hidden rounded-full bg-elevated"
                                        >
                                            <span className="block h-full rounded-full bg-primary transition-[width] motion-reduce:transition-none" style={{ width: `${a.estado === "enviando" ? 100 : porciento}%` }} />
                                        </span>
                                    </span>
                                ) : (
                                    <span className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
                                        <Check aria-hidden className="size-3.5 shrink-0 text-success" />
                                        <span className="truncate">{a.detalle}</span>
                                    </span>
                                )}
                            </span>
                            <span className="flex shrink-0 items-center gap-0.5">
                                {a.estado === "error" && alReintentar ? (
                                    <Boton variante="secundario" tamano="sm" className="h-8 px-2" onClick={() => alReintentar(a.id)}>
                                        <RotateCw />
                                        Reintentar
                                    </Boton>
                                ) : null}
                                <Boton variante="fantasma" tamano="icono-sm" aria-label={`Quitar ${a.nombre}`} title="Quitar" disabled={ocupado} onClick={() => alQuitar(a.id)}>
                                    <X />
                                </Boton>
                            </span>
                        </li>
                    );
                })}
            </ul>
            {children}
            <Dialogo open={grande !== undefined} onOpenChange={(abierto) => (abierto ? undefined : setAmpliada(null))}>
                {grande ? (
                    <DialogoContenido titulo={<span className="[overflow-wrap:anywhere]">{grande.nombre}</span>} descripcion={grande.detalle} className="sm:w-[min(56rem,calc(100vw-2rem))]">
                        <img src={grande.url} alt={`Vista previa de ${grande.nombre}`} className="max-h-[70dvh] w-full rounded-lg object-contain" />
                    </DialogoContenido>
                ) : null}
            </Dialogo>
        </div>
    );
}
