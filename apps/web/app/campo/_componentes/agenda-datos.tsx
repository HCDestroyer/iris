"use client";

import type { OrdenCampo } from "@iris/sync";
import { lecturaUnica, sumarDias, type CargarLectura } from "@iris/sync/campo";
import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import { hoyEnGuatemala } from "@/lib/campo/fechas";
import type { Lectura } from "./inventario-datos";

/** La semana leída: desde hoy hasta `hasta` (7 días). */
export type SemanaLeida = { desde: string; hasta: string; ordenes: OrdenCampo[] };

type ContextoAgenda = { semana: Lectura<SemanaLeida>; cargarSemana: CargarLectura };

const Contexto = createContext<ContextoAgenda | null>(null);

export function useAgenda(): ContextoAgenda {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useAgenda fuera de ProveedorAgenda");
    }
    return c;
}

/**
 * Agenda de «Mañana» y «Semana»: se lee con señal (`GET /campo/paquete?fecha=` del último día) y
 * queda en memoria del motor abierto, como «Por recibir»; no se guarda en el teléfono. Sin leer
 * no hay contadores: nunca un total sobre datos que no se pudieron leer.
 */
export function ProveedorAgenda({ leer, children }: { leer: (hasta: string) => Promise<OrdenCampo[]>; children: ReactNode }) {
    const [semana, setSemana] = useState<Lectura<SemanaLeida>>({ estado: "inactiva", datos: null, error: null, leidoEn: null });
    // Una lectura a la vez; la pedida mientras otra corre (p. ej. al volver la señal) se hace al terminar.
    const cargarSemana = useMemo(
        () =>
            lecturaUnica(() => {
                const desde = hoyEnGuatemala();
                const hasta = sumarDias(desde, 6);
                setSemana((l) => ({ estado: "cargando", datos: l.datos, error: null, leidoEn: l.leidoEn }));
                return leer(hasta)
                    .then((ordenes) => setSemana({ estado: "lista", datos: { desde, hasta, ordenes }, error: null, leidoEn: Date.now() }))
                    .catch((error: unknown) => setSemana((l) => ({ estado: "error", datos: l.datos, error, leidoEn: l.leidoEn })));
            }),
        [leer],
    );
    const valor = useMemo(() => ({ semana, cargarSemana }), [semana, cargarSemana]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
