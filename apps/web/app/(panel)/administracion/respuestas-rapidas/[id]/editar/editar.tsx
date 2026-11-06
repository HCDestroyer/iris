"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { FormularioRespuesta } from "../../formulario";

/**
 * `/administracion/respuestas-rapidas/[id]/editar`: no hay lectura de una sola; se busca en la
 * lista de la empresa (con las inactivas).
 */
export function EditarRespuesta() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        const r = await resolver(api().GET("/api/v1/mensajeria/respuestas-rapidas", { params: { query: { incluirInactivas: true } }, signal }));
        return (r.data ?? []).find((x) => x.id === id) ?? null;
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.titulo : null);
    return (
        <CargaRegistro lectura={lectura} que="la respuesta rápida" noEncontrado="Respuesta rápida no encontrada">
            {(r) => <FormularioRespuesta respuesta={r} />}
        </CargaRegistro>
    );
}
