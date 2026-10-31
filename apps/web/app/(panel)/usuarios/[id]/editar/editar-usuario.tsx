"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { FormularioEditarUsuario } from "@/componentes/usuarios/editar-usuario";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";

/** `/usuarios/[id]/editar`: lee el usuario (solo con un id numérico válido) y abre su formulario. */
export function EditarUsuario() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => (id === null ? null : resolver(api().GET("/api/v1/usuarios/{id}", { params: { path: { id } }, signal }))));
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.nombre : null);
    return <CargaRegistro lectura={lectura} que="el usuario" noEncontrado="Usuario no encontrado">{(u) => <FormularioEditarUsuario usuario={u} />}</CargaRegistro>;
}
