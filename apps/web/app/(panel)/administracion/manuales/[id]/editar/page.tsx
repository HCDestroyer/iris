"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { FormularioManual } from "../../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={["manuales.editar"]}>
            <EditarManual />
        </RequierePermiso>
    );
}

function EditarManual() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => (id === null ? null : resolver(api().GET("/api/v1/mensajeria/manuales/{id}", { params: { path: { id } }, signal }))));
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.titulo : null);
    return (
        <CargaRegistro lectura={lectura} que="el manual" noEncontrado="Manual no encontrado">
            {(m) => <FormularioManual manual={m} />}
        </CargaRegistro>
    );
}
