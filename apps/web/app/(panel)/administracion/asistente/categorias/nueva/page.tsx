"use client";

import { CargaRegistro } from "@/componentes/carga-registro";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { useLectura } from "@/lib/lectura";
import { leerApoyo } from "../datos";
import { FormularioCategoria } from "../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <NuevaCategoria />
        </RequierePermiso>
    );
}

function NuevaCategoria() {
    const lectura = useLectura((signal) => leerApoyo(signal));
    return (
        <CargaRegistro lectura={lectura} que="las colas y los manuales" noEncontrado="No se encontró">
            {(apoyo) => <FormularioCategoria apoyo={apoyo} />}
        </CargaRegistro>
    );
}
