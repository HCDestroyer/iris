import type { Metadata } from "next";
import { Proximamente } from "@/componentes/proximamente";

export const metadata: Metadata = { title: "Administración" };

export default function Pagina() {
    return <Proximamente href="/administracion" descripcion="Los tokens de API, roles y permisos se administrarán aquí." />;
}
