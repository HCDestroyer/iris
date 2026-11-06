import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { TecnicosArgosSinFicha } from "./sin-ficha";

export const metadata: Metadata = { title: "Técnicos de Argos sin ficha" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["agenda.vincular_argos"]}>
            <TecnicosArgosSinFicha />
        </RequierePermiso>
    );
}
