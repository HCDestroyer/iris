"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoNumero, CampoSelector, Casilla, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { ChevronDown, ChevronRight, CircleCheck } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { guardarAjustesAsistente, leerAjustesAsistente } from "@/lib/asistente/api";
import { CLAVES_ACCION, completar, entradaLimites, esquemaLimites, minutosLegibles, NOMBRE_ACCION, NOMBRE_PRIORIDAD, PRIORIDADES, valoresLimites, type ClaveAccion, type ValoresLimites } from "@/lib/asistente/fase3";
import { perfilesDe, resumenPerfil } from "@/lib/asistente/fase3b";
import type { AjustesAsistente } from "@/lib/asistente/tipos";
import { leerColas } from "@/lib/conversaciones/api-equipo";
import type { Cola } from "@/lib/conversaciones/tipos";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const SIEMPRE: readonly string[] = [
    "Se presenta como asistente virtual",
    "Cada acción queda en el historial y en la auditoría",
    "Si duda, no actúa: deja la conversación en la cola",
    "Al llegar una persona, ella toma el control",
];

const CAMPOS = ["prioridadMaxima", "diasAdelanteMax", "maxTurnos", "presupuestoAutonomoPct", "verificacionFuerteMin"] as const;
const AVANZADOS: readonly (typeof CAMPOS)[number][] = ["presupuestoAutonomoPct", "verificacionFuerteMin"];

/**
 * `/administracion/asistente/limites` (`mensajeria.asistente_configurar`): hasta dónde llega el
 * asistente cuando actúa solo (contratos/conversaciones.md §17.1): prioridad máxima de los tickets
 * que crea, con cuántos días de adelanto agenda, cuántos mensajes intenta antes de pasar a una
 * persona y qué acciones quedan apagadas en cada cola.
 */
export function LimitesAsistente() {
    const lectura = useLectura(async (signal) => {
        const [ajustes, colas] = await Promise.all([leerAjustesAsistente(signal), leerColas(signal)]);
        return { ajustes, colas };
    });
    return (
        <CargaRegistro lectura={lectura} que="los límites del asistente" noEncontrado="Sin ajustes">
            {(d) => <Formulario ajustes={d.ajustes} colas={d.colas} />}
        </CargaRegistro>
    );
}

function Formulario({ ajustes, colas }: { ajustes: AjustesAsistente; colas: Cola[] }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresLimites>({ resolver: zodResolver(esquemaLimites), defaultValues: valoresLimites(ajustes) });
    const porCola = useWatch({ control: form.control, name: "porCola" });
    const [presupuesto, vigencia] = useWatch({ control: form.control, name: ["presupuestoAutonomoPct", "verificacionFuerteMin"] });
    const errores = form.formState.errors;
    // Lo avanzado va plegado; se abre solo si tiene un error que corregir.
    const [abierto, setAbierto] = useState(false);
    const avanzado = abierto || AVANZADOS.some((c) => errores[c]);
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    // Las colas activas y, además, las que ya tienen algo apagado.
    const visibles = colas.filter((c) => c.activa || (porCola[String(c.id)]?.length ?? 0) > 0);
    const otros = perfilesDe(ajustes).slice(1);

    function alternar(colaId: number, accion: ClaveAccion, apagada: boolean) {
        const actual = form.getValues("porCola")[String(colaId)] ?? [];
        const nuevas = apagada ? [...actual.filter((a) => a !== accion), accion] : actual.filter((a) => a !== accion);
        form.setValue("porCola", { ...form.getValues("porCola"), [String(colaId)]: nuevas }, { shouldDirty: true });
    }

    return (
        <FormularioPagina
            titulo="Límites del asistente"
            descripcion="Hasta dónde llega cuando atiende solo, sin una persona."
            alEnviar={form.handleSubmit(async (d) => {
                setError(null);
                try {
                    await guardarAjustesAsistente(entradaLimites(ajustes, d), clave.actual());
                    clave.renovar();
                    avisar({ titulo: "Límites del asistente guardados" });
                    await liberarProteccion();
                    router.replace("/administracion/asistente");
                } catch (e) {
                    clave.trasError(e);
                    // El servidor nombra los campos dentro del perfil (`body.perfiles[0].limites.diasAdelanteMax`).
                    const campo = e instanceof ErrorApi && e.estado === 422 ? CAMPOS.find((c) => e.errores.some((x) => x.field.endsWith(c))) : undefined;
                    if (campo && e instanceof ErrorApi) form.setError(campo, { message: e.errores.find((x) => x.field.endsWith(campo))?.message ?? e.titulo }, { shouldFocus: true });
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
                    {completar(ajustes).autonomoDisponible ? null : (
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
                <CampoNumero etiqueta="Días adelante, como mucho" minimo={1} maximo={14} error={errores.diasAdelanteMax?.message} {...form.register("diasAdelanteMax")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="Conversación">
                <CampoNumero
                    etiqueta="Mensajes del asistente antes de pasar a una persona"
                    minimo={2}
                    maximo={20}
                    ayuda="Si llega a este número sin resolver, deja la conversación en la cola."
                    error={errores.maxTurnos?.message}
                    {...form.register("maxTurnos")}
                />
            </SeccionFormulario>
            <SeccionFormulario titulo="Apagado por cola" descripcion="Marca lo que el asistente no debe hacer solo en las conversaciones de cada cola, aunque esté encendido en general.">
                {visibles.length === 0 ? <p className="text-sm text-muted-foreground">Todavía no hay colas.</p> : null}
                {visibles.map((c) => {
                    const apagadas = porCola[String(c.id)] ?? [];
                    return (
                        <fieldset key={c.id} className="grid gap-2 rounded-lg border border-border p-3" data-cola={c.id}>
                            <legend className="px-1 text-sm font-semibold">
                                {c.nombre}
                                {apagadas.length > 0 ? <span className="font-normal text-muted-foreground"> · {apagadas.length === 1 ? "1 apagada" : `${apagadas.length} apagadas`}</span> : null}
                            </legend>
                            <div className="grid gap-x-4 gap-y-1.5 sm:grid-cols-2">
                                {CLAVES_ACCION.map((a) => (
                                    <Casilla key={a} etiqueta={`No ${NOMBRE_ACCION[a].charAt(0).toLowerCase()}${NOMBRE_ACCION[a].slice(1)}`} checked={apagadas.includes(a)} onChange={(e) => alternar(c.id, a, e.target.checked)} />
                                ))}
                            </div>
                        </fieldset>
                    );
                })}
            </SeccionFormulario>
            <SeccionFormulario titulo="Perfiles por horario" descripcion="Para que de noche o en fin de semana haga otra cosa. Aquí se ajusta el general; los demás se conservan como están.">
                {otros.length > 0 ? (
                    <ul aria-label="Perfiles por horario" className="grid gap-1.5 text-sm">
                        {otros.map((p) => (
                            <li key={p.codigo} className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                                <span className="font-semibold [overflow-wrap:anywhere]">{p.nombre}</span>
                                <span className="text-muted-foreground">{resumenPerfil(p)}</span>
                            </li>
                        ))}
                    </ul>
                ) : (
                    <p className="text-sm text-muted-foreground">Todavía no hay otros perfiles.</p>
                )}
                <Link href="/administracion/asistente/perfiles" className="inline-flex items-center gap-0.5 justify-self-start text-sm font-medium text-primary underline-offset-2 hover:underline">
                    Ver y editar los perfiles
                    <ChevronRight aria-hidden className="size-4" />
                </Link>
            </SeccionFormulario>
            <section aria-labelledby="titulo-avanzado" data-slot="avanzado" className="surface-card grid min-w-0 gap-4 p-4 sm:p-5">
                <h2 id="titulo-avanzado" className="min-w-0">
                    <Boton variante="fantasma" className="h-auto w-full justify-between gap-3 px-0 py-0 text-left hover:bg-transparent" aria-expanded={avanzado} aria-controls="campos-avanzados" onClick={() => setAbierto(!avanzado)}>
                        <span className="grid min-w-0 gap-0.5">
                            <span className="text-base font-semibold tracking-[-0.015em]">Avanzado</span>
                            <span className="text-sm font-normal text-muted-foreground [overflow-wrap:anywhere] whitespace-normal">
                                Gasta hasta el {presupuesto || "—"} % del tope atendiendo solo · la identidad vale {/^\d+$/.test(vigencia) ? minutosLegibles(Number(vigencia)) : "—"}
                            </span>
                        </span>
                        <ChevronDown aria-hidden className={avanzado ? "size-5 shrink-0 rotate-180 transition-transform" : "size-5 shrink-0 transition-transform"} />
                    </Boton>
                </h2>
                {avanzado ? (
                    <div id="campos-avanzados" className="grid gap-4 sm:grid-cols-2">
                        <CampoNumero
                            etiqueta="Parte del tope del mes para atender solo (%)"
                            minimo={1}
                            maximo={100}
                            ayuda="Al llegar a esta parte del tope, el asistente deja de atender solo y las conversaciones siguen a la cola. Las sugerencias a tus agentes siguen con el resto. De fábrica: 70."
                            error={errores.presupuestoAutonomoPct?.message}
                            {...form.register("presupuestoAutonomoPct")}
                        />
                        <CampoNumero
                            etiqueta="Minutos que vale la verificación de identidad"
                            minimo={5}
                            maximo={1440}
                            ayuda="Después de verificar su DPI o NIT, el cliente puede pedir promesas o reconexión durante este tiempo; luego se le pide otra vez. Si lo acortas, también se acortan las ya dadas. De fábrica: 60."
                            error={errores.verificacionFuerteMin?.message}
                            {...form.register("verificacionFuerteMin")}
                        />
                    </div>
                ) : null}
            </section>
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
