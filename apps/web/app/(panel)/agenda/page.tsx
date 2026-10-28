import type { Metadata } from "next";
import { Proximamente } from "@/componentes/proximamente";

export const metadata: Metadata = { title: "Agenda" };

export default function Pagina() {
    return <Proximamente href="/agenda" descripcion="El calendario por técnico y zona llega en la fase 3." />;
}
