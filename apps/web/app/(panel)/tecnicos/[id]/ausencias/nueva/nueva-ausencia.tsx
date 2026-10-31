"use client";

import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { FormularioAusencia } from "@/componentes/equipo/formulario-ausencia";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";

/** `/tecnicos/[id]/ausencias/nueva`: lee la ficha del técnico y abre el formulario de ausencia. */
export function NuevaAusencia() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const ficha = useLectura(async (signal) => (id === null ? null : resolver(api().GET("/api/v1/tecnicos/{id}", { params: { path: { id } }, signal }))));
    useNombreEnMigas(ficha.estado === "listo" ? ficha.datos?.nombre : null);
    return (
        <CargaRegistro lectura={ficha} que="la ficha del técnico" noEncontrado="Técnico no encontrado">
            {(t) => <FormularioAusencia tecnico={t} />}
        </CargaRegistro>
    );
}
