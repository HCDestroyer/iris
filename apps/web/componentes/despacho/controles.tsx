"use client";

import { Boton, Campo, CampoSelector } from "@iris/ui";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { sumarMeses } from "@/lib/agenda/mes";
import type { Lectura } from "@/lib/lectura";
import type { Cuadrilla, Tecnico, Zona } from "@/lib/despacho/calendario";
import { esDia, hoyGuatemala, sumarDias, type Dia } from "@/lib/despacho/fechas";
import type { FiltrosDespacho } from "./datos";

/** Anterior / Hoy / Siguiente y el día exacto. En semana, un paso es una semana; en mes, un mes. */
export function NavegacionDias({ dia, paso, alCambiar }: { dia: Dia; paso: 1 | 7 | "mes"; alCambiar: (d: Dia) => void }) {
    const unidad = paso === 1 ? "Día" : paso === 7 ? "Semana" : "Mes";
    const mover = (n: 1 | -1) => (paso === "mes" ? sumarMeses(dia, n) : sumarDias(dia, n * paso));
    return (
        <div className="flex flex-wrap items-end gap-2">
            <div className="flex items-center gap-1">
                <Boton variante="secundario" tamano="icono" aria-label={`${unidad} anterior`} title={`${unidad} anterior`} onClick={() => alCambiar(mover(-1))}>
                    <ChevronLeft />
                </Boton>
                <Boton variante="secundario" onClick={() => alCambiar(hoyGuatemala())}>
                    Hoy
                </Boton>
                <Boton variante="secundario" tamano="icono" aria-label={`${unidad} siguiente`} title={`${unidad} siguiente`} onClick={() => alCambiar(mover(1))}>
                    <ChevronRight />
                </Boton>
            </div>
            <Campo
                etiqueta="Fecha"
                type="date"
                className="w-40"
                value={dia}
                onChange={(e) => {
                    if (esDia(e.target.value)) alCambiar(e.target.value);
                }}
            />
        </div>
    );
}

/**
 * Filtros de cuadrilla y zona (catálogo de agenda). Si el catálogo no se pudo leer, el filtro se
 * desactiva y lo dice: nunca se ofrecen opciones inventadas.
 */
export function FiltrosAgenda({
    filtros,
    alCambiar,
    zonas,
    cuadrillas,
    tecnicos,
}: {
    filtros: FiltrosDespacho;
    alCambiar: (f: FiltrosDespacho) => void;
    zonas: Lectura<Zona[]>;
    cuadrillas: Lectura<Cuadrilla[]>;
    /** Con la lista, también filtra por técnico (`tecnicoId` = su usuario). */
    tecnicos?: Lectura<Tecnico[]>;
}) {
    const id = (v: string) => (v === "" ? null : Number(v));
    return (
        <div className={tecnicos ? "grid grid-cols-2 gap-3 sm:flex sm:flex-wrap sm:items-end" : "grid grid-cols-2 gap-3 sm:flex sm:items-end"}>
            {tecnicos ? (
                <CampoSelector
                    etiqueta="Técnico"
                    className="col-span-2 sm:w-52"
                    value={filtros.tecnicoId ?? ""}
                    disabled={tecnicos.estado !== "listo"}
                    ayuda={tecnicos.estado === "error" ? "No se pudo leer la lista" : undefined}
                    onChange={(e) => alCambiar({ ...filtros, tecnicoId: id(e.target.value) })}
                >
                    <option value="">Todos</option>
                    {tecnicos.estado === "listo"
                        ? tecnicos.datos
                              .filter((t) => t.activo || t.usuarioId === filtros.tecnicoId)
                              .map((t) => (
                                  <option key={t.usuarioId} value={t.usuarioId}>
                                      {t.nombre}
                                  </option>
                              ))
                        : null}
                </CampoSelector>
            ) : null}
            <CampoSelector
                etiqueta="Cuadrilla"
                className="sm:w-48"
                value={filtros.cuadrillaId ?? ""}
                disabled={cuadrillas.estado !== "listo"}
                ayuda={cuadrillas.estado === "error" ? "No se pudo leer el catálogo" : undefined}
                onChange={(e) => alCambiar({ ...filtros, cuadrillaId: id(e.target.value) })}
            >
                <option value="">Todas</option>
                {cuadrillas.estado === "listo"
                    ? cuadrillas.datos
                          .filter((c) => c.activa)
                          .map((c) => (
                              <option key={c.id} value={c.id}>
                                  {c.nombre}
                              </option>
                          ))
                    : null}
            </CampoSelector>
            <CampoSelector
                etiqueta="Zona"
                className="sm:w-48"
                value={filtros.zonaId ?? ""}
                disabled={zonas.estado !== "listo"}
                ayuda={zonas.estado === "error" ? "No se pudo leer el catálogo" : undefined}
                onChange={(e) => alCambiar({ ...filtros, zonaId: id(e.target.value) })}
            >
                <option value="">Todas</option>
                {zonas.estado === "listo"
                    ? zonas.datos
                          .filter((z) => z.activa)
                          .map((z) => (
                              <option key={z.id} value={z.id}>
                                  {z.nombre}
                              </option>
                          ))
                    : null}
            </CampoSelector>
        </div>
    );
}
