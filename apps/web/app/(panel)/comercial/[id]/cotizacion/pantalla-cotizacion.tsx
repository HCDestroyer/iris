"use client";

import { Aviso, Boton } from "@iris/ui";
import Link from "next/link";
import { useParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { NuevaCotizacion } from "../cotizacion";

/** `/comercial/[id]/cotizacion`: lee el prospecto; si hoy no se puede cotizar, lo explica y vuelve a la ficha. */
export function PantallaCotizacion() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const detalle = useLectura(async (signal) => (id === null ? null : resolver(api().GET("/api/v1/prospectos/{id}", { params: { path: { id } }, signal }))));
    useNombreEnMigas(detalle.estado === "listo" ? detalle.datos?.nombre : null);
    return (
        <CargaRegistro lectura={detalle} que="el prospecto" noEncontrado="No encontramos este prospecto">
            {(d) =>
                d.acciones.cotizar ? (
                    <NuevaCotizacion detalle={d} recargar={detalle.refrescar} />
                ) : (
                    <Aviso
                        tono="warning"
                        titulo="Este prospecto no se puede cotizar ahora"
                        accion={
                            <Boton asChild variante="secundario" tamano="sm">
                                <Link href={`/comercial/${d.id}`}>Volver al prospecto</Link>
                            </Boton>
                        }
                    >
                        Se cotiza cuando la factibilidad es viable o con obra y el prospecto sigue abierto.
                    </Aviso>
                )
            }
        </CargaRegistro>
    );
}
