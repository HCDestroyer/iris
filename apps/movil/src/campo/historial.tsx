// «Historial del cliente» del menú de la orden (como la PWA): `GET /ordenes?clienteId=` con el
// alcance del técnico (sus órdenes con ese cliente), solo con señal y sin guardar.
import type { OrdenHistorial } from "@iris/sync";
import { createContext, useContext, type ReactNode } from "react";

export type LeerHistorial = (clienteId: number) => Promise<OrdenHistorial[]>;

const Contexto = createContext<LeerHistorial | null>(null);

export function ProveedorHistorial({ leer, children }: { leer: LeerHistorial | null; children: ReactNode }) {
    return <Contexto.Provider value={leer}>{children}</Contexto.Provider>;
}

/** El lector del motor abierto (`null` si no hay: la hoja lo dice). */
export function useLeerHistorial(): LeerHistorial | null {
    return useContext(Contexto);
}
