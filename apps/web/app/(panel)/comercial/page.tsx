import type { Metadata } from "next";
import { Proximamente } from "@/componentes/proximamente";

export const metadata: Metadata = { title: "Comercial" };

export default function Pagina() {
    return <Proximamente href="/comercial" descripcion="El embudo comercial, la factibilidad y las cotizaciones llegan en la fase 4." />;
}
