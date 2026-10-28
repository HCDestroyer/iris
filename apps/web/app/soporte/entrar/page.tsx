import type { Metadata } from "next";
import { MarcoPortal } from "@/componentes/portal/marco";
import { EntrarSinServidor } from "./entrar-sin-servidor";

export const metadata: Metadata = { title: "Soporte", referrer: "no-referrer" };

export default function PaginaEntrarPortal() {
    return (
        <MarcoPortal>
            <EntrarSinServidor />
        </MarcoPortal>
    );
}
