"use client";

import { useParams, useSearchParams } from "next/navigation";
import { CargaRegistro, CargandoRegistro } from "@/componentes/carga-registro";
import { AvisoCatalogos } from "@/componentes/despacho/catalogos";
import { useCatalogosAgenda } from "@/componentes/despacho/datos";
import { FormularioTecnico } from "@/componentes/equipo/formulario-tecnico";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";

/** `/tecnicos/[id]/editar`: «Cancelar» vuelve a la lista si se llegó desde ella (`?desde=lista`), si no a la ficha. */
export function EditarTecnico() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const desdeLista = useSearchParams().get("desde") === "lista";
    const catalogos = useCatalogosAgenda();
    const ficha = useLectura(async (signal) => (id === null ? null : resolver(api().GET("/api/v1/tecnicos/{id}", { params: { path: { id } }, signal }))));
    useNombreEnMigas(ficha.estado === "listo" ? ficha.datos?.nombre : null);
    if (catalogos.cuadrillas.estado === "cargando") return <CargandoRegistro que="la ficha del técnico" />;
    if (catalogos.cuadrillas.estado === "error") return <AvisoCatalogos {...catalogos} />;
    const cuadrillas = catalogos.cuadrillas.datos;
    return (
        <CargaRegistro lectura={ficha} que="la ficha del técnico" noEncontrado="Técnico no encontrado">
            {(t) => (
                <FormularioTecnico
                    tecnico={t}
                    cuadrillas={cuadrillas}
                    zonas={catalogos.zonas.estado === "listo" ? catalogos.zonas.datos : undefined}
                    volverA={desdeLista ? "/tecnicos" : `/tecnicos/${t.id}`}
                />
            )}
        </CargaRegistro>
    );
}
