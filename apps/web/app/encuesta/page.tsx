import type { Metadata } from "next";
import { MarcoEncuesta } from "@/componentes/encuestas/marco-publico";
import { EncuestaSinServidor } from "./encuesta-sin-servidor";

// El token viaja en el fragmento (#): ni el servidor ni el Referer lo ven; la página no se indexa.
export const metadata: Metadata = { title: "Encuesta de satisfacción", referrer: "no-referrer", robots: { index: false, follow: false } };

export default function PaginaEncuesta() {
    return (
        <MarcoEncuesta>
            <EncuestaSinServidor />
        </MarcoEncuesta>
    );
}
