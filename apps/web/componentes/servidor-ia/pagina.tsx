"use client";

import { useLectura } from "@/lib/lectura";
import { leerUmbrales } from "@/lib/servidor-ia/api";
import type { Umbrales } from "@/lib/servidor-ia/tipos";
import { usePermisosServidorIa } from "./comun";
import { TableroServidorIa } from "./tablero";

/**
 * El tablero con los umbrales de quien administra (para decir «avisa a los 85 °C» y pintar en ámbar
 * lo que se acerca). Si no se pudieron leer, el tablero usa los de fábrica solo para el color: las
 * cifras no dependen de ellos.
 */
export function PaginaServidorIa() {
    // Los umbrales los lee quien ve todo (`servidor_ia.ver`); cambiarlos pide `.administrar`.
    const { global } = usePermisosServidorIa();
    return global ? <ConUmbrales /> : <TableroServidorIa />;
}

function ConUmbrales() {
    const lectura = useLectura<Umbrales | null>((signal) => leerUmbrales(signal).catch(() => null));
    return <TableroServidorIa umbrales={lectura.estado === "listo" ? lectura.datos : null} />;
}
