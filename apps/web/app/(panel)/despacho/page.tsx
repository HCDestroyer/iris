import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PantallaDespacho } from "./despacho";

export const metadata: Metadata = { title: "Despacho" };

/*
 * `despacho.ver` aún no existe en el enum `Permiso` (pedido a I2 en .claude/integration/i3.md):
 * mientras tanto, `agenda.ver`, igual que la entrada del menú (lib/menu.ts).
 */
export default function Pagina() {
    return (
        <RequierePermiso permisos={["agenda.ver"]}>
            <PantallaDespacho />
        </RequierePermiso>
    );
}
