"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, CampoNumero, CampoSelector, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { CircleCheck } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { guardarAjustesFase3, leerAjustesFase3 } from "@/lib/asistente/api-fase3";
import { completar, entradaGuardada, esquemaLimites, NOMBRE_PRIORIDAD, PRIORIDADES, type AjustesCompletos, type ValoresLimites } from "@/lib/asistente/fase3";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const SIEMPRE: readonly string[] = [
    "Se presenta como asistente virtual",
    "Cada acción queda en el historial y en la auditoría",
    "Si duda, no actúa: deja la conversación en la cola",
    "Al llegar una persona, ella toma el control",
];

const CAMPOS = ["prioridadMaxima", "diasAdelanteMax", "maxTurnos"] as const;

/**
 * `/administracion/asistente/limites` (`mensajeria.asistente_configurar`): hasta dónde llega el
 * asistente cuando actúa solo (contratos/conversaciones.md §17): prioridad máxima de los tickets
 * que crea, con cuántos días de adelanto agenda y cuántos mensajes intenta antes de pasar a una
 * persona.
 */
export function LimitesAsistente() {
    const lectura = useLectura(async (signal) => completar(await leerAjustesFase3(signal)));
    return (
        <CargaRegistro lectura={lectura} que="los límites del asistente" noEncontrado="Sin ajustes">
            {(a) => <Formulario ajustes={a} />}
        </CargaRegistro>
    );
}

function Formulario({ ajustes }: { ajustes: AjustesCompletos }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresLimites>({
        resolver: zodResolver(esquemaLimites),
        defaultValues: { prioridadMaxima: ajustes.limites.prioridadMaxima, diasAdelanteMax: String(ajustes.limites.diasAdelanteMax), maxTurnos: String(ajustes.limites.maxTurnos) },
    });
    const errores = form.formState.errors;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <FormularioPagina
            titulo="Límites del asistente"
            descripcion="Hasta dónde llega cuando atiende solo, sin una persona."
            alEnviar={form.handleSubmit(async (d) => {
                setError(null);
                try {
                    await guardarAjustesFase3(
                        { ...entradaGuardada(ajustes), limites: { prioridadMaxima: d.prioridadMaxima, diasAdelanteMax: Number(d.diasAdelanteMax), maxTurnos: Number(d.maxTurnos) } },
                        clave.actual(),
                    );
                    clave.renovar();
                    avisar({ titulo: "Límites del asistente guardados" });
                    await liberarProteccion();
                    router.replace("/administracion/asistente");
                } catch (e) {
                    clave.trasError(e);
                    // El servidor nombra los campos dentro de `limites` (`body.limites.maxTurnos`).
                    const campo = e instanceof ErrorApi && e.estado === 422 ? CAMPOS.find((c) => e.errorDeCampo(`limites.${c}`) ?? e.errorDeCampo(c)) : undefined;
                    if (campo && e instanceof ErrorApi) form.setError(campo, { message: e.errorDeCampo(`limites.${campo}`) ?? e.errorDeCampo(campo) ?? e.titulo }, { shouldFocus: true });
                    else setError(e);
                }
            })}
            navegar={router.push}
            volverA="/administracion/asistente"
            sucio={form.formState.isDirty}
            barra={{
                enviando: form.formState.isSubmitting,
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los ajustes: vuelve a abrirlos." } : {}),
            }}
            error={
                <>
                    {ajustes.autonomoDisponible ? null : (
                        <Aviso tono="info" titulo="Se aplicarán cuando el asistente atienda solo">
                            Por ahora el asistente solo sugiere. Puedes dejar los límites listos.
                        </Aviso>
                    )}
                    {error ? (
                        <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los ajustes" : "No se pudo guardar"}>
                            {conflicto ? "Recarga la página para ver el cambio." : mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                </>
            }
        >
            <SeccionFormulario titulo="Tickets" descripcion="El asistente elige la categoría y la prioridad del ticket que crea.">
                <CampoSelector etiqueta="Prioridad máxima" ayuda="Nunca crea un ticket con más prioridad que esta; lo demás lo pasa a una persona." error={errores.prioridadMaxima?.message} {...form.register("prioridadMaxima")}>
                    {PRIORIDADES.map((p) => (
                        <option key={p} value={p}>
                            {NOMBRE_PRIORIDAD[p]}
                        </option>
                    ))}
                </CampoSelector>
            </SeccionFormulario>
            <SeccionFormulario titulo="Visitas" descripcion="Agenda en los espacios libres de la agenda de la zona.">
                <CampoNumero etiqueta="Días hábiles adelante, como mucho" minimo={1} maximo={30} error={errores.diasAdelanteMax?.message} {...form.register("diasAdelanteMax")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="Conversación">
                <CampoNumero
                    etiqueta="Mensajes del asistente antes de pasar a una persona"
                    minimo={1}
                    maximo={30}
                    ayuda="Si llega a este número sin resolver, deja la conversación en la cola."
                    error={errores.maxTurnos?.message}
                    {...form.register("maxTurnos")}
                />
            </SeccionFormulario>
            <SeccionFormulario titulo="Siempre">
                <ul className="grid gap-1.5" aria-label="Lo que el asistente hace siempre">
                    {SIEMPRE.map((t) => (
                        <li key={t} className="flex items-start gap-2 text-sm">
                            <CircleCheck aria-hidden className="mt-0.5 size-4 shrink-0 text-success" />
                            {t}
                        </li>
                    ))}
                </ul>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
