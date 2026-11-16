"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { leerCategorias } from "@/lib/asistente/api-fase3";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { leerApoyo } from "../../datos";
import { FormularioCategoria } from "../../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <EditarCategoria />
        </RequierePermiso>
    );
}

function EditarCategoria() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        const [categorias, apoyo] = await Promise.all([leerCategorias(true, signal), leerApoyo(signal)]);
        const categoria = categorias.find((c) => c.id === id);
        return categoria ? { categoria, apoyo } : null;
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.categoria.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="la categoría" noEncontrado="Categoría no encontrada">
            {(d) => <FormularioCategoria categoria={d.categoria} apoyo={d.apoyo} />}
        </CargaRegistro>
    );
}
