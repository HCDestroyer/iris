"use client";

import { AlertTriangle, FileText, Film, RotateCw, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { cn } from "../utilidades";
import { BarraProgreso } from "./barra-progreso";
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
 * Vista previa de los archivos elegidos antes de enviarlos, como una tira de miniaturas de 64 px que
 * se desliza a lo ancho (no crece hacia arriba: el hilo sigue a la vista en el teléfono). Imagen
 * ampliable o icono con el nombre corto (documento, video); cada una con su «X» para quitarla y su
 * estado con icono + texto + color: no sirve o falló (borde y franja en rojo, con «Reintentar»),
 * subiendo (barra). El nombre completo y el detalle van en el `title` y en texto para lectores; lo
 * que no sirve se explica en una línea bajo la tira. Es solo presentación: los nombres van como
 * texto (nunca HTML) y las URL las maneja quien la usa.
 */
export function VistaPreviaArchivos({ etiqueta, archivos, alQuitar, alReintentar, className, children }: PropsVistaPreviaArchivos) {
    const [ampliada, setAmpliada] = useState<string | null>(null);
    const grande = archivos.find((a) => a.id === ampliada && a.url);
    if (archivos.length === 0) return null;
    const conProblema = archivos.filter((a) => a.estado === "invalido" || a.estado === "error");
    return (
        <div role="group" aria-label={etiqueta} data-slot="vista-previa-archivos" className={cn("grid min-w-0 gap-1", className)}>
            <ul className="flex min-w-0 gap-2 overflow-x-auto px-0.5 pt-0.5 pb-1 [scrollbar-width:thin]" data-desborde-permitido>
                {archivos.map((a) => {
                    const ocupado = a.estado === "subiendo" || a.estado === "enviando";
                    const mal = a.estado === "invalido" || a.estado === "error";
                    const porciento = Math.round(Math.min(1, Math.max(0, a.progreso ?? 0)) * 100);
                    const valor = a.estado === "enviando" ? 100 : porciento;
                    return (
                        <li
                            key={a.id}
                            data-archivo={a.id}
                            data-estado={a.estado}
                            title={`${a.nombre} · ${a.detalle}`}
                            className={cn("relative size-16 shrink-0 overflow-hidden rounded-[0.625rem] border bg-elevated", mal ? "border-error" : "border-border")}
                        >
                            {a.url ? (
                                <Boton variante="fantasma" className="size-full rounded-none p-0" aria-label={`Ampliar ${a.nombre}`} title="Ampliar" onClick={() => setAmpliada(a.id)}>
                                    {/* Vista previa local (URL de objeto): no pasa por el optimizador de imágenes. */}
                                    <img src={a.url} alt="" className="size-full object-cover" />
                                </Boton>
                            ) : (
                                <span aria-hidden className="grid size-full place-content-center justify-items-center gap-0.5 px-1 text-muted-foreground">
                                    {a.clase === "video" ? <Film className="size-5" /> : <FileText className="size-5" />}
                                    <span className="max-w-14 truncate text-[0.625rem] leading-tight">{a.nombre}</span>
                                </span>
                            )}
                            <span className="sr-only" data-slot="archivo-nombre">
                                {a.nombre}
                            </span>
                            {mal ? (
                                <>
                                    <span className="sr-only" role={a.estado === "error" ? "alert" : undefined}>
                                        {a.mensaje ?? (a.estado === "invalido" ? "No se puede enviar." : "No se pudo subir.")}
                                    </span>
                                    <span aria-hidden className="pointer-events-none absolute inset-x-0 bottom-0 flex items-center justify-center gap-0.5 bg-error py-px text-[0.625rem] font-semibold text-background">
                                        <AlertTriangle className="size-3" />
                                        {a.estado === "invalido" ? "No sirve" : "Falló"}
                                    </span>
                                    {a.estado === "error" && alReintentar ? (
                                        <Boton variante="secundario" tamano="icono-sm" className="absolute top-1/2 left-1/2 size-8 -translate-x-1/2 -translate-y-1/2 rounded-full" aria-label="Reintentar" title="Reintentar" onClick={() => alReintentar(a.id)}>
                                            <RotateCw />
                                        </Boton>
                                    ) : null}
                                </>
                            ) : ocupado ? (
                                <>
                                    <span className="sr-only">{a.estado === "enviando" ? "Enviando…" : `Subiendo… ${porciento} %`}</span>
                                    <BarraProgreso valor={valor} etiqueta={`Subida de ${a.nombre}`} className="absolute inset-x-1 bottom-1 w-auto" claseRiel="h-1 bg-black/35" />
                                </>
                            ) : (
                                <span className="sr-only">Listo · {a.detalle}</span>
                            )}
                            <Boton
                                variante="fantasma"
                                tamano="icono-sm"
                                className="absolute top-0.5 right-0.5 size-6! rounded-full bg-black/60 text-white [--button-fg:#fff] [--button-bg-hover:rgb(0_0_0/0.75)] [&_svg]:size-3.5"
                                aria-label={`Quitar ${a.nombre}`}
                                title="Quitar"
                                disabled={ocupado}
                                onClick={() => alQuitar(a.id)}
                            >
                                <X />
                            </Boton>
                        </li>
                    );
                })}
            </ul>
            {conProblema.length > 0 ? (
                <ul aria-hidden className="grid gap-0.5 px-1 text-xs text-error" data-slot="problemas-archivos">
                    {conProblema.map((a) => (
                        <li key={a.id} className="flex min-w-0 items-start gap-1">
                            <AlertTriangle className="mt-px size-3.5 shrink-0" />
                            <span className="min-w-0 truncate">
                                <span className="font-semibold">{a.nombre}</span>: {a.mensaje ?? (a.estado === "invalido" ? "No se puede enviar." : "No se pudo subir.")}
                            </span>
                        </li>
                    ))}
                </ul>
            ) : null}
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
