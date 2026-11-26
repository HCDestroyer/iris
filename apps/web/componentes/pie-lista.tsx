"use client";

import { Aviso, Boton } from "@iris/ui";
import { ChevronDown, RotateCw } from "lucide-react";
import type { ReactNode } from "react";
import { mensajeDeError } from "@/lib/api/errores";
import type { Paginado } from "@/lib/paginado";

/** Pie de una lista con cursor: «Cargar más», o el error de la página siguiente con «Reintentar». */
export function PieLista<T>({ paginado, que }: { paginado: Paginado<T>; que: string }) {
    if (paginado.errorMas) {
        return <ErrorCargarMas error={paginado.errorMas} que={que} reintentar={paginado.cargarMas} />;
    }
    if (!paginado.siguiente) {
        return null;
    }
    return (
        <Boton variante="contorno" tamano="sm" className="justify-self-center" onClick={paginado.cargarMas} disabled={paginado.cargandoMas}>
            <ChevronDown />
            {paginado.cargandoMas ? "Cargando…" : "Cargar más"}
        </Boton>
    );
}

/** Error de «Cargar más» con «Reintentar». */
export function ErrorCargarMas({ error, que, reintentar }: { error: unknown; que: string; reintentar: () => void }) {
    return (
        <Aviso
            tono="error"
            titulo={`No se pudieron cargar más ${que}`}
            accion={
                <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            {mensajeDeError(error).detalle}
        </Aviso>
    );
}

export type PieListaFilas = {
    resumen?: string;
    alCargarMas?: () => void;
    cargandoMas: boolean;
    errorMas?: ReactNode;
};

type EstadoPie = {
    /** Filas a la vista. */
    cuantos: number;
    siguiente: string | null | undefined;
    cargarMas: () => unknown;
    cargandoMas: boolean;
    errorMas: unknown;
};

/**
 * Pie de `ListaFilas` para una lista con cursor: «Mostrando n …» y «Cargar más» mientras haya
 * más; «No hay más ….» al terminar; el error de la página siguiente con «Reintentar». Con la lista
 * vacía no hay resumen: el aviso de lista vacía ya lo dice, y «No hay más» debajo sobraría.
 */
export function pieListaFilas(p: EstadoPie, que: string): PieListaFilas {
    const cargar = () => void p.cargarMas();
    return {
        ...(p.siguiente ? { resumen: `Mostrando ${p.cuantos} ${que}` } : p.cuantos > 0 ? { resumen: `No hay más ${que}.` } : {}),
        ...(p.siguiente ? { alCargarMas: cargar } : {}),
        cargandoMas: p.cargandoMas,
        ...(p.errorMas ? { errorMas: <ErrorCargarMas error={p.errorMas} que={que} reintentar={cargar} /> } : {}),
    };
}

/** Lo mismo desde `usePaginado`. */
export function piePaginado<T>(paginado: Paginado<T>, que: string): PieListaFilas {
    return pieListaFilas({ cuantos: paginado.items.length, siguiente: paginado.siguiente, cargarMas: paginado.cargarMas, cargandoMas: paginado.cargandoMas, errorMas: paginado.errorMas }, que);
}
