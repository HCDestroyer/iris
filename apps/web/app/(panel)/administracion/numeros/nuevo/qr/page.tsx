import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ConectarConQr } from "./qr";

export const metadata: Metadata = { title: "Conectar con QR" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <ConectarConQr />
        </RequierePermiso>
    );
}
