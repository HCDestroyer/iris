import type { Metadata } from "next";
import { Proximamente } from "@/componentes/proximamente";

export const metadata: Metadata = { title: "Tickets" };

export default function Pagina() {
    return <Proximamente href="/tickets" descripcion="La bandeja de tickets con SLA, filtros y vistas llega en la fase 1." />;
}
