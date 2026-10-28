import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { MENU } from "@/lib/menu";
import { IndiceAdministracion } from "./indice";

export const metadata: Metadata = { title: "Administración" };

export default function Pagina() {
    const entrada = MENU.find((e) => e.href === "/administracion");
    return (
        <RequierePermiso permisos={entrada?.permisos ?? null}>
            <IndiceAdministracion />
        </RequierePermiso>
    );
}
