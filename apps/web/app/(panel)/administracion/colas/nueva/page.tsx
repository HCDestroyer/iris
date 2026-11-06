"use client";

import { CargaRegistro } from "@/componentes/carga-registro";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { leerAgentesDisponibles, leerCanalesConBot } from "@/lib/conversaciones/api-11";
import { useLectura } from "@/lib/lectura";
import { FormularioCola } from "../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <NuevaCola />
        </RequierePermiso>
    );
}

function NuevaCola() {
    const lectura = useLectura(async (signal) => {
        const [agentes, canales] = await Promise.all([leerAgentesDisponibles(signal), leerCanalesConBot(signal)]);
        return { agentes, canales };
    });
    return (
        <CargaRegistro lectura={lectura} que="las personas" noEncontrado="No se encontró">
            {(d) => <FormularioCola agentes={d.agentes} canales={d.canales} />}
        </CargaRegistro>
    );
}
