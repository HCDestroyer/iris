"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, FormularioPagina, Interruptor, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { Clock, Moon, ShieldCheck, Users } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { guardarAjustesReconexion, leerAjustesReconexion, type AjustesReconexion } from "@/lib/asistente/reconexion";
import { estadoSinPersonal } from "@/lib/asistente/reconexion-presentacion";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const PRINCIPAL = "/administracion/asistente";

/** Espejo de `EntradaAjustesReconexion` (la versión la pone el panel). */
const esquema = z.object({ sinPersonal: z.boolean() });
type Valores = z.infer<typeof esquema>;

/**
 * `/administracion/asistente/reconexion` (`mensajeria.asistente_configurar`, solo admin): la
 * reconexión temporal también en horario hábil cuando nadie de caja está disponible (contrato con
 * Argos 0.8.0 §7.3, B4). Apagada de fábrica y aparte de la de fuera de horario (que se enciende en
 * «¿Qué puede hacer solo?»). Muestra en llano si de verdad se pedirá: Argos, rol Caja y quién está.
 */
export function ReconexionAsistente() {
    const lectura = useLectura((signal) => leerAjustesReconexion(signal));
    return (
        <CargaRegistro lectura={lectura} que="la reconexión temporal" noEncontrado="Sin ajustes">
            {(a) => <Formulario key={a.version} ajustes={a} />}
        </CargaRegistro>
    );
}

function Formulario({ ajustes }: { ajustes: AjustesReconexion }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<Valores>({ resolver: zodResolver(esquema), defaultValues: { sinPersonal: ajustes.sinPersonal } });
    const encendido = useWatch({ control: form.control, name: "sinPersonal" });
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const estado = estadoSinPersonal(ajustes, encendido);

    async function guardar(d: Valores) {
        setError(null);
        try {
            await guardarAjustesReconexion({ version: ajustes.version, sinPersonal: d.sinPersonal }, clave.actual());
            clave.renovar();
            avisar({ titulo: d.sinPersonal ? "Reconexión sin personal de caja: encendida" : "Reconexión sin personal de caja: apagada" });
            await liberarProteccion();
            router.replace(PRINCIPAL);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        }
    }

    return (
        <FormularioPagina
            titulo="Reconexión temporal"
            descripcion="Cuando un cliente cortado manda su boleta, el asistente puede pedirle a Argos que lo reconecte mientras caja la revisa. Argos decide con sus reglas."
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={PRINCIPAL}
            sucio={form.formState.isDirty}
            barra={{
                enviando: form.formState.isSubmitting,
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió este ajuste: vuelve a abrirlo." } : {}),
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió este ajuste" : "No se pudo guardar"}>
                        {conflicto ? "Recarga la página para ver el cambio." : `${mensajeDeError(error).titulo}${error instanceof ErrorApi && error.detalle ? `: ${error.detalle}` : ""}`}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Fuera de horario">
                <p className="flex items-start gap-1.5 text-sm text-muted-foreground">
                    <Moon aria-hidden className="mt-0.5 size-4 shrink-0" />
                    Se pide si encendiste «Reconexión temporal» en «¿Qué puede hacer solo?».
                </p>
            </SeccionFormulario>

            <SeccionFormulario titulo="En horario hábil">
                <Interruptor
                    alFinal
                    etiqueta="También cuando nadie de caja está disponible"
                    ayuda="Solo si ninguna persona con el rol Caja está disponible en la bandeja en ese momento."
                    {...form.register("sinPersonal")}
                />
                <Aviso tono={estado.tono} titulo={estado.titulo} data-testid="estado-sin-personal" data-tono={estado.tono}>
                    {estado.detalle}
                </Aviso>
                <ul className="grid gap-1.5 text-sm text-muted-foreground" aria-label="Siempre">
                    <li className="flex items-start gap-1.5">
                        <ShieldCheck aria-hidden className="mt-0.5 size-4 shrink-0" />
                        Siempre verifica la identidad del cliente y exige su boleta registrada.
                    </li>
                    <li className="flex items-start gap-1.5">
                        <Users aria-hidden className="mt-0.5 size-4 shrink-0" />
                        Lo decide Iris con la disponibilidad de caja, nunca lo que diga el cliente.
                    </li>
                    <li className="flex items-start gap-1.5">
                        <Clock aria-hidden className="mt-0.5 size-4 shrink-0" />
                        Al cliente le dice hasta cuándo vale y que su comprobante queda pendiente. Una sola por periodo, con cualquier motivo.
                    </li>
                </ul>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
