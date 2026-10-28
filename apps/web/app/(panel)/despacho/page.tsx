import type { Metadata } from "next";
import { Proximamente } from "@/componentes/proximamente";

export const metadata: Metadata = { title: "Despacho" };

export default function Pagina() {
    return <Proximamente href="/despacho" descripcion="El despacho con mapa y rutas del día llega en la fase 3." />;
}
