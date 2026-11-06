// Agenda de «Mañana» y «Semana» (diseño «combinada»): las órdenes del técnico con cita hasta el
// último día de la semana. El técnico no tiene `agenda.ver` (el calendario de despacho,
// `GET /agenda`): su agenda es `GET /campo/paquete?fecha=<último día>` («programadas hasta ese
// día»), leída con señal y en memoria como «Por recibir»; no toca la cola ni el paquete guardado.
// Como la PWA (`app/campo/_componentes/agenda-datos.tsx`).
import type { OrdenCampo } from "@iris/sync";
import { hoyEnGuatemala, sumarDias, type CargarLectura } from "@iris/sync/campo";
import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";
import { useLectura, type Lectura } from "./inventario";

export type SemanaLeida = { desde: string; hasta: string; ordenes: OrdenCampo[] };

/** Lee las órdenes con cita hasta `hasta` (`YYYY-MM-DD`). */
export type LeerAgenda = (hasta: string) => Promise<OrdenCampo[]>;

interface ContextoAgenda {
    semana: Lectura<SemanaLeida>;
    cargarSemana: CargarLectura;
}

const Contexto = createContext<ContextoAgenda | null>(null);

export function useAgenda(): ContextoAgenda {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useAgenda fuera de ProveedorAgenda");
    }
    return c;
}

export function ProveedorAgenda({ leer, children }: { leer: LeerAgenda; children: ReactNode }) {
    const leerSemana = useCallback(async (): Promise<SemanaLeida> => {
        const desde = hoyEnGuatemala();
        const hasta = sumarDias(desde, 6);
        return { desde, hasta, ordenes: await leer(hasta) };
    }, [leer]);
    const [semana, cargarSemana] = useLectura(leerSemana);
    const valor = useMemo(() => ({ semana, cargarSemana }), [semana, cargarSemana]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
