"use client";

import { DesdeConversacion } from "@/componentes/conversaciones/desde-conversacion";
import { NuevoProspecto } from "../../../comercial/nuevo/nuevo-prospecto";

/** Crear prospecto desde una conversación sin cliente (§5.3). */
export default function Pagina() {
    return (
        <DesdeConversacion permisos={["prospectos.crear"]}>
            {(c) => <NuevoProspecto conversacion={{ id: c.id, titulo: c.titulo, numero: c.numero, perfilNombre: c.perfilNombre ?? undefined }} />}
        </DesdeConversacion>
    );
}
