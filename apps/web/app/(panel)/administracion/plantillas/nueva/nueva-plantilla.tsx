"use client";

import { avisar, Aviso } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { EditorPlantilla } from "@/componentes/plantillas/editor";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { cuerpoCrearPlantilla, PLANTILLA_VACIA, type DatosCrearPlantilla } from "@/lib/plantillas/esquemas";
import { puedeTocarComun } from "@/lib/plantillas/permisos";
import { useSesion } from "@/lib/sesion";

const CAMPOS = ["codigo", "nombre", "descripcion", "tipo"] as const;

export function NuevaPlantilla() {
    const { yo } = useSesion();
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const comun = puedeTocarComun(yo);
    return (
        <>
            <EncabezadoPagina titulo="Nueva plantilla" descripcion={comun ? undefined : "Será de la empresa activa."} />
            <EditorPlantilla
                modo="crear"
                valores={PLANTILLA_VACIA}
                puedeComun={comun}
                aviso="Como borrador nace inactiva y puedes corregirla sin crear versiones; al activarla, cada edición crea una versión nueva."
                error={
                    error ? (
                        <Aviso tono="error" titulo="No se pudo crear la plantilla">
                            {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null
                }
                alCancelar={() => router.push("/administracion/plantillas")}
                alGuardar={async (d: DatosCrearPlantilla, setErrorCampo) => {
                    setError(null);
                    try {
                        const p = await resolver(api().POST("/api/v1/plantillas", { params: { header: { "Idempotency-Key": clave.actual() } }, body: cuerpoCrearPlantilla(d) }));
                        clave.renovar();
                        avisar({ titulo: `Plantilla «${p.nombre}» creada` });
                        router.push(`/administracion/plantillas/${p.id}`);
                    } catch (e) {
                        clave.trasError(e);
                        if (e instanceof ErrorApi && e.codigo === "codigo_duplicado") {
                            setErrorCampo("codigo", { message: e.detalle ?? "Ya hay una plantilla con ese código" });
                            return;
                        }
                        const nombres = [...CAMPOS, ...d.items.flatMap((_, i) => [`items.${i}.codigo`, `items.${i}.etiqueta`, `items.${i}.opciones`, `items.${i}.unidad`] as const)];
                        if (!erroresACampos(e, nombres, setErrorCampo)) setError(e);
                    }
                }}
            />
        </>
    );
}
