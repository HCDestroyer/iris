import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_ASIGNAR_EXTENSION } from "@/lib/centralita/tipos";
import { ExtensionTelefono } from "./formulario";

export const metadata: Metadata = { title: "Extensión" };

export default async function Pagina({ params }: { params: Promise<{ extensionId: string }> }) {
    const { extensionId } = await params;
    const id = Number(extensionId);
    if (!Number.isSafeInteger(id) || id <= 0) notFound();
    return (
        <RequierePermiso permisos={[PERMISO_ASIGNAR_EXTENSION]}>
            <ExtensionTelefono extensionId={id} />
        </RequierePermiso>
    );
}
