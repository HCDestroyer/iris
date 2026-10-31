"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { FormularioBuzon } from "../../formulario-buzon";

/** `/administracion/buzones/[id]/editar`: lee el buzón y abre su formulario en dos pasos. */
export function EditarBuzon() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const buzon = useLectura(async (signal) => (id === null ? null : resolver(api().GET("/api/v1/buzones/{id}", { params: { path: { id } }, signal }))));
    return (
        <CargaRegistro lectura={buzon} que="el buzón" noEncontrado="Buzón no encontrado">
            {(b) => <FormularioBuzon buzon={b} />}
        </CargaRegistro>
    );
}
