"use client";

import { Cargando } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

/**
 * Un número prestado a la empresa activa (`prestadoPor` con valor) no tiene credenciales, conexión ni
 * préstamos que tocar aquí: su pantalla es el detalle del préstamo. Devuelve true mientras redirige.
 */
export function useIrAPrestamo(canal: { id: number; prestadoPor?: { nombre: string } | null } | null | undefined): boolean {
    const router = useRouter();
    const id = canal?.id;
    const prestado = Boolean(canal?.prestadoPor);
    useEffect(() => {
        if (prestado && id !== undefined) router.replace(`/administracion/numeros/${id}/prestamo`);
    }, [prestado, id, router]);
    return prestado;
}

/** Lo que se ve mientras se va al detalle del préstamo. */
export function YendoAPrestamo() {
    return <Cargando visible texto="Es un número prestado: abriendo su detalle…" className="p-4" />;
}
