import type { Metadata } from "next";
import { Suspense } from "react";
import { PantallaEquipo } from "./pantalla";

export const metadata: Metadata = { title: "Equipo" };

export default function Pagina() {
    return (
        <Suspense>
            <PantallaEquipo />
        </Suspense>
    );
}
