import type { Metadata } from "next";
import { Suspense } from "react";
import { PantallaConversaciones } from "./pantalla";

export const metadata: Metadata = { title: "Conversaciones" };

export default function Pagina() {
    return (
        <Suspense>
            <PantallaConversaciones />
        </Suspense>
    );
}
