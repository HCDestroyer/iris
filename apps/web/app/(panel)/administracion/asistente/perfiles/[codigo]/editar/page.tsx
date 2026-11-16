"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { leerAjustesAsistente } from "@/lib/asistente/api";
import { perfilesDe } from "@/lib/asistente/fase3b";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { useLectura } from "@/lib/lectura";
import { FormularioPerfil } from "../../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <EditarPerfil />
        </RequierePermiso>
    );
}

function EditarPerfil() {
    const codigo = decodeURIComponent(useParams<{ codigo: string }>().codigo);
    const lectura = useLectura(async (signal) => {
        const ajustes = await leerAjustesAsistente(signal);
        const perfil = perfilesDe(ajustes).find((p) => p.codigo === codigo);
        return perfil ? { ajustes, perfil } : null;
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.perfil.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el perfil" noEncontrado="Perfil no encontrado">
            {(d) => <FormularioPerfil ajustes={d.ajustes} perfil={d.perfil} />}
        </CargaRegistro>
    );
}
