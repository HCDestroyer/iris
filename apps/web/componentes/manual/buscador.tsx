"use client";

import { CampoBusqueda, EstadoVacio } from "@iris/ui";
import { SearchX } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { buscarEnManual, type EntradaBusqueda } from "@/lib/manual/busqueda";

/**
 * Buscador del manual: filtra en el navegador (las páginas ya vienen en la pantalla, sin llamar a la
 * API). Sin texto muestra `children` (el índice); con texto, los resultados con su extracto.
 */
export function BuscadorManual({ entradas, rutas, children }: { entradas: readonly EntradaBusqueda[]; rutas: Readonly<Record<string, string>>; children: ReactNode }) {
    const [consulta, setConsulta] = useState("");
    const resultados = useMemo(() => buscarEnManual(entradas, consulta), [entradas, consulta]);
    const buscando = consulta.trim().length > 0;
    return (
        <>
            <CampoBusqueda etiqueta="Buscar en el manual" valor={consulta} alCambiar={setConsulta} placeholder="Ticket, orden, firma, sin señal…" maxLength={100} />
            <p className="sr-only" role="status" aria-live="polite">
                {buscando ? (resultados.length === 1 ? "1 resultado" : `${resultados.length} resultados`) : ""}
            </p>
            {!buscando ? (
                children
            ) : resultados.length === 0 ? (
                <EstadoVacio icono={SearchX} titulo="Nada coincide con tu búsqueda" descripcion="Prueba con otra palabra, por ejemplo el nombre de la pantalla o de un botón." />
            ) : (
                <ul aria-label="Resultados de la búsqueda" className="grid gap-2">
                    {resultados.map((r) => {
                        const id = `manual-${r.clave.replace("/", "-")}`;
                        return (
                            <li key={r.clave}>
                                <a
                                    href={rutas[r.clave] ?? `/manual/${r.clave}`}
                                    aria-labelledby={`${id}-titulo`}
                                    aria-describedby={`${id}-seccion ${id}-extracto`}
                                    className="grid gap-1 rounded-[var(--hx-radius-card)] border border-border bg-card px-4 py-3 transition-colors hover:bg-elevated focus-visible:outline-2 focus-visible:outline-primary"
                                >
                                    <span id={`${id}-seccion`} className="text-xs text-muted-foreground">
                                        {r.seccion}
                                    </span>
                                    <span id={`${id}-titulo`} className="font-semibold text-primary">
                                        {r.titulo}
                                    </span>
                                    <span id={`${id}-extracto`} className="text-sm text-muted-foreground">
                                        {r.extracto}
                                    </span>
                                </a>
                            </li>
                        );
                    })}
                </ul>
            )}
        </>
    );
}
