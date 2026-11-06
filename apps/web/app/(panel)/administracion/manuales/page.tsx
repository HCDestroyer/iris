import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { BibliotecaManuales } from "./lista";

export const metadata: Metadata = { title: "Biblioteca de manuales" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["manuales.ver"]}>
            <Suspense>
                <BibliotecaManuales />
            </Suspense>
        </RequierePermiso>
    );
}
