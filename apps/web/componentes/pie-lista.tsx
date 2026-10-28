"use client";

import { Aviso, Boton } from "@iris/ui";
import { ChevronDown, RotateCw } from "lucide-react";
import { mensajeDeError } from "@/lib/api/errores";
import type { Paginado } from "@/lib/paginado";

/** Pie de una lista con cursor: «Cargar más», o el error de la página siguiente con «Reintentar». */
export function PieLista<T>({ paginado, que }: { paginado: Paginado<T>; que: string }) {
    if (paginado.errorMas) {
        return (
            <Aviso
                tono="error"
                titulo={`No se pudieron cargar más ${que}`}
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={paginado.cargarMas}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(paginado.errorMas).detalle}
            </Aviso>
        );
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
