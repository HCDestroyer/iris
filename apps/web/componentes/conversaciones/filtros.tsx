"use client";

import { Boton, Casilla, cn, PanelEmergente } from "@iris/ui";
import { ListFilter, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { cuantosFiltros, FILTROS_INICIALES, type Filtros } from "@/lib/conversaciones/estado";
import { CLASE_PILDORA, CLASE_PUNTO, nombreUso, tonoDeCanal, usoPrincipal } from "@/lib/conversaciones/presentacion";
import type { CanalRef, Contadores } from "@/lib/conversaciones/tipos";
import { useCatalogos } from "./catalogos";

/**
 * Filtros en un solo botón con su contador (diseño compacto del PM): abre un panel con número,
 * estado, etiqueta y cola. Uno por grupo (como la API); tocar el elegido lo quita.
 */
export function BotonFiltros({ filtros, contadores, alCambiar }: { filtros: Filtros; contadores: Contadores | null; alCambiar: (f: Filtros) => void }) {
    const [abierto, setAbierto] = useState(false);
    const activos = cuantosFiltros(filtros);
    return (
        <PanelEmergente
            titulo="Filtros"
            abierto={abierto}
            alCambiar={setAbierto}
            lado="bottom end"
            className="w-[min(18rem,calc(100vw-2rem))] p-1.5"
            disparador={
                <Boton variante="contorno" tamano="icono-sm" aria-label={activos ? `Filtros (${activos} activos)` : "Filtros"} title="Filtros" className="relative">
                    <ListFilter />
                    {activos ? (
                        <span aria-hidden className="absolute -top-1 -right-1 min-w-4 rounded-full bg-foreground px-1 text-[0.625rem] leading-4 font-bold text-background">
                            {activos}
                        </span>
                    ) : null}
                </Boton>
            }
        >
            <ContenidoFiltros filtros={filtros} contadores={contadores} alCambiar={alCambiar} />
        </PanelEmergente>
    );
}

function ContenidoFiltros({ filtros, contadores, alCambiar }: { filtros: Filtros; contadores: Contadores | null; alCambiar: (f: Filtros) => void }) {
    const { canales, colas, etiquetas } = useCatalogos();
    const total = (lista: readonly { id: number; total: number }[] | undefined, id: number) => lista?.find((x) => x.id === id)?.total;
    const estados: { clave: string; etiqueta: string; activo: boolean; aplicar: (f: Filtros, on: boolean) => Filtros; n?: number | undefined }[] = [
        { clave: "por_vencer", etiqueta: "SLA por vencer", activo: filtros.sla === "por_vencer", aplicar: (f, on) => ({ ...f, sla: on ? "por_vencer" : null }), n: contadores?.porVencer },
        { clave: "vencido", etiqueta: "SLA vencido", activo: filtros.sla === "vencido", aplicar: (f, on) => ({ ...f, sla: on ? "vencido" : null }), n: contadores?.vencidas },
        {
            clave: "abierta",
            etiqueta: "Sin contestar",
            activo: filtros.estado === "abierta",
            aplicar: (f, on) => ({ ...f, estado: on ? "abierta" : "activas" }),
            n: contadores?.porEstado?.abierta,
        },
        {
            clave: "esperando_cliente",
            etiqueta: "Esperando al cliente",
            activo: filtros.estado === "esperando_cliente",
            aplicar: (f, on) => ({ ...f, estado: on ? "esperando_cliente" : "activas" }),
            n: contadores?.porEstado?.esperando_cliente,
        },
        { clave: "resuelta", etiqueta: "Resueltas", activo: filtros.estado === "resuelta", aplicar: (f, on) => ({ ...f, estado: on ? "resuelta" : "activas" }) },
    ];
    return (
        <div className="grid max-h-[min(28rem,70dvh)] gap-0.5 overflow-y-auto">
            {canales.estado === "listo" && canales.datos.length > 1 ? (
                <Grupo titulo="Número">
                    {canales.datos.map((canal) => (
                        <Opcion
                            key={canal.id}
                            activo={filtros.canalId === canal.id}
                            n={total(contadores?.porCanal, canal.id)}
                            alCambiar={(on) => alCambiar({ ...filtros, canalId: on ? canal.id : null })}
                            etiqueta={<PildoraCanal canal={canal} />}
                            texto={canal.nombre}
                        />
                    ))}
                </Grupo>
            ) : null}
            <Grupo titulo="Estado">
                {estados.map((e) => (
                    <Opcion key={e.clave} activo={e.activo} n={e.n} etiqueta={e.etiqueta} texto={e.etiqueta} alCambiar={(on) => alCambiar(e.aplicar(filtros, on))} />
                ))}
            </Grupo>
            {etiquetas.estado === "listo" && etiquetas.datos.length > 0 ? (
                <Grupo titulo="Etiqueta">
                    {etiquetas.datos.map((e) => (
                        <Opcion
                            key={e.id}
                            activo={filtros.etiquetaId === e.id}
                            n={total(contadores?.porEtiqueta, e.id)}
                            etiqueta={e.nombre}
                            texto={e.nombre}
                            alCambiar={(on) => alCambiar({ ...filtros, etiquetaId: on ? e.id : null })}
                        />
                    ))}
                </Grupo>
            ) : null}
            {colas.estado === "listo" && colas.datos.length > 1 ? (
                <Grupo titulo="Cola">
                    {colas.datos.map((c) => (
                        <Opcion
                            key={c.id}
                            activo={filtros.colaId === c.id}
                            n={total(contadores?.porCola, c.id)}
                            etiqueta={c.nombre}
                            texto={c.nombre}
                            alCambiar={(on) => alCambiar({ ...filtros, colaId: on ? c.id : null })}
                        />
                    ))}
                </Grupo>
            ) : null}
            {cuantosFiltros(filtros) > 0 ? (
                <Boton variante="fantasma" tamano="sm" className="mt-1 justify-start" onClick={() => alCambiar({ ...FILTROS_INICIALES, vista: filtros.vista, q: filtros.q })}>
                    <X />
                    Quitar filtros
                </Boton>
            ) : null}
        </div>
    );
}

function Grupo({ titulo, children }: { titulo: string; children: ReactNode }) {
    return (
        <fieldset className="grid gap-0.5">
            <legend className="px-2.5 pt-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">{titulo}</legend>
            {children}
        </fieldset>
    );
}

function Opcion({ activo, n, etiqueta, texto, alCambiar }: { activo: boolean; n: number | undefined; etiqueta: ReactNode; texto: string; alCambiar: (on: boolean) => void }) {
    return (
        <div className={cn("flex min-h-9 min-w-0 items-center gap-2 rounded-md px-2.5", activo && "bg-elevated")}>
            <Casilla
                className="min-w-0 flex-1 [&_label]:min-w-0 [&_label]:truncate"
                checked={activo}
                onChange={(e) => alCambiar(e.target.checked)}
                etiqueta={<span className="flex min-w-0 items-center gap-2 truncate">{etiqueta}</span>}
                aria-label={texto}
            />
            {n !== undefined ? <span className="text-xs text-muted-foreground tabular-nums">{n}</span> : null}
        </div>
    );
}

export function PildoraCanal({ canal, className }: { canal: Pick<CanalRef, "nombre" | "usos">; className?: string }) {
    const tono = tonoDeCanal(canal);
    const uso = usoPrincipal(canal.usos ?? []);
    return (
        <span className={cn("inline-flex h-5 max-w-full min-w-0 items-center gap-1 rounded-full px-2 text-[0.71875rem] font-semibold", CLASE_PILDORA[tono], className)}>
            <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", CLASE_PUNTO[tono])} />
            <span className="truncate">{canal.nombre || (uso ? nombreUso(uso) : "Número")}</span>
        </span>
    );
}
