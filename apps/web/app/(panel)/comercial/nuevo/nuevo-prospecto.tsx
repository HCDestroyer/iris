"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Campo, CampoArea, CampoSelector, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { AvisoAccion, useEnvio, useZonas } from "@/componentes/comercial/comun";
import { api, resolver } from "@/lib/api/cliente";
import { cuerpoCrearProspecto, esquemaProspecto, PROSPECTO_VACIO, type DatosProspecto } from "@/lib/comercial/esquemas";
import { ORIGENES, ORIGENES_ELEGIBLES } from "@/lib/comercial/presentacion";

const CAMPOS = ["nombre", "direccion", "telefono", "correo", "documento", "origen", "origenDetalle", "zonaId", "planInteresNombre", "notas", "lat", "lon"] as const;

/**
 * «Nuevo prospecto» en pantalla propia (`/comercial/nuevo`, `prospectos.crear`): lo mínimo es
 * nombre y dirección. Al crear, aviso y ficha del prospecto.
 */
export function NuevoProspecto() {
    const router = useRouter();
    const lecturaZonas = useZonas();
    const zonas = lecturaZonas.estado === "listo" ? lecturaZonas.datos : null;
    const envio = useEnvio();
    const formulario = useForm<DatosProspecto>({ resolver: zodResolver(esquemaProspecto), defaultValues: PROSPECTO_VACIO });
    const { errors, isSubmitting, isDirty } = formulario.formState;

    async function enviar(datos: DatosProspecto) {
        const creado = await envio.enviar(
            (clave) => resolver(api().POST("/api/v1/prospectos", { params: { header: { "Idempotency-Key": clave } }, body: cuerpoCrearProspecto(datos) })),
            { nombres: CAMPOS, setError: formulario.setError },
        );
        if (creado) {
            avisar({ titulo: `Prospecto ${creado.numero} creado` });
            await liberarProteccion();
            router.replace(`/comercial/${creado.id}`);
        }
    }

    return (
        <FormularioPagina
            titulo="Nuevo prospecto"
            descripcion="Con nombre y dirección basta para empezar; lo demás se completa después."
            alEnviar={formulario.handleSubmit(enviar)}
            navegar={router.push}
            volverA="/comercial"
            sucio={isDirty}
            barra={{ textoGuardar: "Crear prospecto", textoEnviando: "Creando…", enviando: isSubmitting }}
            error={<AvisoAccion envio={envio} que="crear el prospecto" recargar={async () => undefined} />}
        >
            <SeccionFormulario titulo="1. Quién y dónde">
                <Campo etiqueta="Nombre" autoComplete="off" error={errors.nombre?.message} {...formulario.register("nombre")} />
                <Campo etiqueta="Dirección" autoComplete="off" error={errors.direccion?.message} {...formulario.register("direccion")} />
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Teléfono" type="tel" inputMode="tel" error={errors.telefono?.message} {...formulario.register("telefono")} />
                    <Campo etiqueta="Correo" type="email" inputMode="email" error={errors.correo?.message} {...formulario.register("correo")} />
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Latitud" inputMode="decimal" placeholder="14.6349" error={errors.lat?.message} {...formulario.register("lat")} />
                    <Campo etiqueta="Longitud" inputMode="decimal" placeholder="-90.5133" error={errors.lon?.message} {...formulario.register("lon")} />
                </div>
            </SeccionFormulario>
            <SeccionFormulario titulo="2. Origen e interés">
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
                <Campo etiqueta="Plan de interés" error={errors.planInteresNombre?.message} {...formulario.register("planInteresNombre")} />
                <CampoArea etiqueta="Notas" rows={3} error={errors.notas?.message} {...formulario.register("notas")} />
            </SeccionFormulario>
        </FormularioPagina>
    );
}
