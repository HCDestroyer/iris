"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Campo, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { guardarAjustes, leerAjustes } from "@/lib/conversaciones/api-11";
import type { AjustesBandeja } from "@/lib/conversaciones/contrato-11";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

/** Espejo de `POST /conversaciones/ajustes` (§14.5): 1 a 480 minutos. */
export const esquemaAjustes = z.object({
    minutosInactividad: z
        .string()
        .trim()
        .refine((v) => /^\d+$/.test(v) && Number(v) >= 1 && Number(v) <= 480, "Entre 1 y 480 minutos"),
});

/** `/administracion/bandeja`: ajustes de la bandeja de la empresa (por ahora, la inactividad). */
export function AjustesDeBandeja() {
    const lectura = useLectura((signal) => leerAjustes(signal));
    return (
        <CargaRegistro lectura={lectura} que="los ajustes" noEncontrado="Sin ajustes">
            {(a) => <Formulario ajustes={a} />}
        </CargaRegistro>
    );
}

function Formulario({ ajustes }: { ajustes: AjustesBandeja }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm({ resolver: zodResolver(esquemaAjustes), defaultValues: { minutosInactividad: String(ajustes.minutosInactividad) } });
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <FormularioPagina
            titulo="Ajustes de la bandeja"
            descripcion="Valen para todas las personas que atienden conversaciones en la empresa."
            alEnviar={form.handleSubmit(async (d) => {
                setError(null);
                try {
                    const r = await guardarAjustes({ version: ajustes.version, minutosInactividad: Number(d.minutosInactividad) }, clave.actual());
                    clave.renovar();
                    avisar({ titulo: `Inactividad: ${r.minutosInactividad} min para pasar a No disponible` });
                    await liberarProteccion();
                    router.replace("/administracion/colas");
                } catch (e) {
                    clave.trasError(e);
                    if (!erroresACampos(e, ["minutosInactividad"], form.setError)) setError(e);
                }
            })}
            navegar={router.push}
            volverA="/administracion/colas"
            sucio={form.formState.isDirty}
            barra={{ enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los ajustes: vuelve a abrirlos." } : {}) }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los ajustes" : "No se pudo guardar"}>
                        {conflicto ? "Recarga la página para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Disponibilidad">
                <Campo
                    etiqueta="Minutos de inactividad para pasar a No disponible"
                    inputMode="numeric"
                    ayuda="Sin teclado, ratón ni la pestaña a la vista durante este tiempo, la persona pasa a No disponible. También al cerrar sesión o fuera de su horario."
                    error={form.formState.errors.minutosInactividad?.message}
                    {...form.register("minutosInactividad")}
                />
            </SeccionFormulario>
        </FormularioPagina>
    );
}
