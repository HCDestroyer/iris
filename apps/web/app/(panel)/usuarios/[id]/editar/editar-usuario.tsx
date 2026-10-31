"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { FormularioEditarUsuario } from "@/componentes/usuarios/editar-usuario";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";

/** `/usuarios/[id]/editar`: lee el usuario y abre su formulario. */
export function EditarUsuario() {
    const { id: crudo } = useParams<{ id: string }>();
    const id = Number(crudo);
    const valido = Number.isSafeInteger(id) && id > 0;
    const lectura = useLectura(async (signal) => (valido ? resolver(api().GET("/api/v1/usuarios/{id}", { params: { path: { id } }, signal })) : null));
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.nombre : null);
    return <CargaRegistro lectura={lectura} que="el usuario" noEncontrado="Usuario no encontrado">{(u) => <FormularioEditarUsuario usuario={u} />}</CargaRegistro>;
}
