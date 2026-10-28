import type { Metadata } from "next";
import { Proximamente } from "@/componentes/proximamente";

export const metadata: Metadata = { title: "Órdenes" };

export default function Pagina() {
    return <Proximamente href="/ordenes" descripcion="El tablero de órdenes de trabajo con evidencias llega en la fase 2." />;
}
