import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { Encuestas } from "./encuestas";

export const metadata: Metadata = { title: "Encuestas" };

export default function PaginaEncuestas() {
    return (
        <RequierePermiso permisos={["encuestas.ver"]}>
            <Suspense>
                <Encuestas />
            </Suspense>
        </RequierePermiso>
    );
}
