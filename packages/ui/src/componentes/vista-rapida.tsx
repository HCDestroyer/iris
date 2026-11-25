"use client";

import { X } from "lucide-react";
import { useId, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { PanelLateral } from "./panel-lateral";

export type PropsVistaRapida = {
    abierto: boolean;
    alCerrar: () => void;
    /** Lo que se ve («Lentitud por las noches»); también es el nombre accesible. */
    titulo: string;
    /** Código sobre el título, en monoespaciada («TCK-001238»). */
    codigo?: string;
    /** Estados bajo el título (`InsigniaEstado`: prioridad, estado, SLA). */
    insignias?: ReactNode;
    /** Acciones al pie: la principal a lo ancho y debajo las secundarias. */
    pie?: ReactNode;
    children?: ReactNode;
    className?: string;
};

/**
 * Vista rápida de una fila: flota sobre la lista sin quitarle ancho ni oscurecerla (no es modal:
 * los clics fuera le llegan a la lista). Encabezado «Vista rápida» con el código, el título y sus
 * estados; el contenido se desplaza por dentro; las acciones van al pie. Escape la cierra con el
 * foco adentro y el foco vuelve a la fila que la abrió (`PanelLateral` sin telón).
 */
export function VistaRapida({ abierto, alCerrar, titulo, codigo, insignias, pie, children, className }: PropsVistaRapida) {
    const idTitulo = useId();
    return (
        <PanelLateral
            abierto={abierto}
            alCerrar={alCerrar}
            etiqueta={`Vista rápida: ${titulo}`}
            data-slot="vista-rapida"
            className={cn("sm:w-[min(23.75rem,calc(100vw-2rem))] sm:top-[11.5rem] sm:right-6 sm:bottom-6 sm:rounded-[var(--iris-radio-modal)]", className)}
        >
            <header className="flex flex-none items-start gap-3 px-4 pt-4 pb-3">
                <div className="grid min-w-0 flex-1 gap-1">
                    <p className="text-[0.6875rem] font-bold tracking-[0.06em] text-[var(--iris-texto-apagado)] uppercase">Vista rápida</p>
                    {codigo ? <p className="font-mono text-xs text-muted-foreground">{codigo}</p> : null}
                    <h2 id={idTitulo} className="text-lg leading-tight font-semibold tracking-[-0.015em]">
                        {titulo}
                    </h2>
                    {insignias ? <div className="mt-1 flex flex-wrap items-center gap-1.5">{insignias}</div> : null}
                </div>
                <Boton variante="fantasma" tamano="icono-sm" aria-label="Cerrar vista rápida" title="Cerrar" onClick={alCerrar} data-foco-inicial>
                    <X />
                </Boton>
            </header>
            <div className="grid min-h-0 flex-1 content-start gap-4 overflow-y-auto overscroll-contain px-4 pb-4">
                {children}
            </div>
            {pie ? <footer className="grid flex-none gap-2 border-t border-border p-4">{pie}</footer> : null}
        </PanelLateral>
    );
}
