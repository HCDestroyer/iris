"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { leerAgentes, leerNumeros, leerColas } from "@/lib/conversaciones/api-equipo";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { FormularioCola } from "../../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <EditarCola />
        </RequierePermiso>
    );
}

function EditarCola() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        const [colas, agentes, canales] = await Promise.all([leerColas(signal), leerAgentes(signal), leerNumeros(signal)]);
        const cola = colas.find((c) => c.id === id);
        return cola ? { cola, agentes, canales } : null;
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.cola.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="la cola" noEncontrado="Cola no encontrada">
            {(d) => <FormularioCola cola={d.cola} agentes={d.agentes} canales={d.canales} />}
        </CargaRegistro>
    );
}
