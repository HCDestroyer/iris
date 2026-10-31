"use client";

import { Migas } from "@iris/ui";
import { usePathname } from "next/navigation";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { migasDeRuta } from "@/lib/migas";

type ContextoMigas = { nombre: string | null; fijarNombre: (nombre: string | null) => void };

const Contexto = createContext<ContextoMigas | null>(null);

/** Guarda el nombre del registro que se está viendo para la última miga. */
export function ProveedorMigas({ children }: { children: ReactNode }) {
    const [nombre, fijarNombre] = useState<string | null>(null);
    const valor = useMemo(() => ({ nombre, fijarNombre }), [nombre]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}

/**
 * Una ficha avisa el nombre de su registro («OT-000042», «Supervisor») cuando ya lo leyó; al salir
 * de la ficha se borra. Fuera del panel no hace nada.
 */
export function useNombreEnMigas(nombre: string | null | undefined) {
    const contexto = useContext(Contexto);
    const fijar = contexto?.fijarNombre;
    useEffect(() => {
        if (!fijar) return undefined;
        fijar(nombre ?? null);
        return () => fijar(null);
    }, [fijar, nombre]);
}

/** Migas de la pantalla actual, arriba del contenido del panel. */
export function MigasPanel() {
    const ruta = usePathname();
    const contexto = useContext(Contexto);
    return <Migas migas={migasDeRuta(ruta, contexto?.nombre)} className="mb-3" />;
}
