"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { FormularioHorario } from "../../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <EditarHorario />
        </RequierePermiso>
    );
}

function EditarHorario() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => (id === null ? null : resolver(api().GET("/api/v1/mensajeria/horarios/{id}", { params: { path: { id } }, signal }))));
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el horario" noEncontrado="Horario no encontrado">
            {(h) => <FormularioHorario horario={h} />}
        </CargaRegistro>
    );
}
