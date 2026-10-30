"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Boton,
    Campo,
    CampoArea,
    CampoSelector,
    DialogoCerrar,
    DialogoContenido,
    DialogoDisparador,
    DialogoPie,
    DialogoProtegido,
    useCambiosSinGuardar,
} from "@iris/ui";
import { Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { AvisoAccion, useEnvio, type Zona } from "@/componentes/comercial/comun";
import { api, resolver } from "@/lib/api/cliente";
import { cuerpoCrearProspecto, esquemaProspecto, PROSPECTO_VACIO, type DatosProspecto } from "@/lib/comercial/esquemas";
import { ORIGENES, ORIGENES_ELEGIBLES } from "@/lib/comercial/presentacion";

const CAMPOS = ["nombre", "direccion", "telefono", "correo", "documento", "origen", "origenDetalle", "zonaId", "planInteresNombre", "notas", "lat", "lon"] as const;

/** Alta rápida de prospecto (`prospectos.crear`): lo mínimo es nombre y dirección. */
export function AltaProspecto({ zonas }: { zonas: readonly Zona[] | null }) {
    const [abierto, setAbierto] = useState(false);
    return (
        <DialogoProtegido open={abierto} onOpenChange={setAbierto}>
            <DialogoDisparador asChild>
                <Boton>
                    <Plus />
                    Nuevo prospecto
                </Boton>
            </DialogoDisparador>
            {abierto ? <FormularioAlta zonas={zonas} /> : null}
        </DialogoProtegido>
    );
}

function FormularioAlta({ zonas }: { zonas: readonly Zona[] | null }) {
    const router = useRouter();
    const envio = useEnvio();
    const formulario = useForm<DatosProspecto>({ resolver: zodResolver(esquemaProspecto), defaultValues: PROSPECTO_VACIO });
    useCambiosSinGuardar(formulario.formState.isDirty);
    const { errors, isSubmitting } = formulario.formState;

    async function enviar(datos: DatosProspecto) {
        const creado = await envio.enviar(
            (clave) => resolver(api().POST("/api/v1/prospectos", { params: { header: { "Idempotency-Key": clave } }, body: cuerpoCrearProspecto(datos) })),
            { nombres: CAMPOS, setError: formulario.setError },
        );
        if (creado) router.push(`/comercial/${creado.id}`);
    }

    return (
        <DialogoContenido titulo="Nuevo prospecto" descripcion="Con nombre y dirección basta para empezar; lo demás se completa después.">
            <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4" aria-label="Nuevo prospecto">
                <AvisoAccion envio={envio} que="crear el prospecto" recargar={async () => undefined} />
                <Campo etiqueta="Nombre" autoComplete="off" error={errors.nombre?.message} {...formulario.register("nombre")} />
                <Campo etiqueta="Dirección" autoComplete="off" error={errors.direccion?.message} {...formulario.register("direccion")} />
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Teléfono" type="tel" inputMode="tel" error={errors.telefono?.message} {...formulario.register("telefono")} />
                    <Campo etiqueta="Correo" type="email" inputMode="email" error={errors.correo?.message} {...formulario.register("correo")} />
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoSelector etiqueta="Origen" error={errors.origen?.message} {...formulario.register("origen")}>
                        {ORIGENES_ELEGIBLES.map((o) => (
                            <option key={o} value={o}>
                                {ORIGENES[o]}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoSelector
                        etiqueta="Zona"
                        error={errors.zonaId?.message}
                        ayuda={zonas === null ? "Sin el catálogo de zonas: la API usa la tuya si tu alcance es por zona." : undefined}
                        disabled={zonas === null}
                        {...formulario.register("zonaId")}
                    >
                        <option value="">Sin zona</option>
                        {(zonas ?? []).filter((z) => z.activa).map((z) => (
                            <option key={z.id} value={String(z.id)}>
                                {z.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
                <Campo etiqueta="Detalle del origen" ayuda="Quién lo refirió, campaña…" error={errors.origenDetalle?.message} {...formulario.register("origenDetalle")} />
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Latitud" inputMode="decimal" placeholder="14.6349" error={errors.lat?.message} {...formulario.register("lat")} />
                    <Campo etiqueta="Longitud" inputMode="decimal" placeholder="-90.5133" error={errors.lon?.message} {...formulario.register("lon")} />
                </div>
                <Campo etiqueta="Plan de interés" error={errors.planInteresNombre?.message} {...formulario.register("planInteresNombre")} />
                <CampoArea etiqueta="Notas" rows={2} error={errors.notas?.message} {...formulario.register("notas")} />
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting}>
                        <Plus />
                        {isSubmitting ? "Creando…" : "Crear prospecto"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}
