"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    avisar,
    Aviso,
    Boton,
    Campo,
    CampoArea,
    CampoSelector,
    Casilla,
    FormularioPagina,
    liberarProteccion,
    SeccionFormulario,
    Tarjeta,
} from "@iris/ui";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { MOTIVOS, entradaAusencia, erroresDe, esquemaAusencia, valoresAusencia, type TecnicoDetalle, type ValoresAusencia } from "@/lib/agenda/esquemas";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { diaDesdeIso, etiquetaDia, horaDe, hoyGuatemala } from "@/lib/despacho/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import type { Schema } from "@iris/contratos";

type Respuesta = Schema<"RespuestaAusencia">;

/**
 * Registrar una ausencia en pantalla propia (`/tecnicos/[id]/ausencias/nueva`, hora de
 * Guatemala). La API no mueve las citas que caen dentro: las devuelve y aquí se muestran para
 * reprogramarlas; sin citas en conflicto, aviso y vuelta a la ficha.
 */
export function FormularioAusencia({ tecnico }: { tecnico: TecnicoDetalle }) {
    const router = useRouter();
    const ficha = `/tecnicos/${tecnico.id}?pestana=ausencias`;
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [hecho, setHecho] = useState<Respuesta | null>(null);
    const form = useForm<ValoresAusencia>({ resolver: zodResolver(esquemaAusencia), defaultValues: valoresAusencia(hoyGuatemala()), mode: "onTouched" });
    const valores = useWatch({ control: form.control }) as ValoresAusencia;
    const vivos = erroresDe(esquemaAusencia.safeParse(valores));
    const { errors, isSubmitting, isDirty } = form.formState;
    const mensaje = (campo: keyof ValoresAusencia) => errors[campo]?.message ?? vivos[campo];

    async function guardar(v: ValoresAusencia) {
        setError(null);
        try {
            const r = await resolver(
                api().POST("/api/v1/tecnicos/{id}/ausencias", { params: { path: { id: tecnico.id }, header: { "Idempotency-Key": clave.actual() } }, body: entradaAusencia(v) }),
            );
            clave.renovar();
            avisar({ titulo: `Ausencia de ${tecnico.nombre} registrada` });
            await liberarProteccion();
            if ((r.citasEnConflicto ?? []).length > 0) {
                setHecho(r);
            } else {
                router.replace(ficha);
            }
        } catch (e) {
            clave.trasError(e);
            const campos = ["motivo", "detalle"] as const;
            if (!erroresACampos<ValoresAusencia>(e, campos, form.setError)) setError(e);
        }
    }

    if (hecho) {
        const citas = hecho.citasEnConflicto ?? [];
        return (
            <section className="mx-auto grid w-full max-w-3xl gap-4" aria-labelledby="titulo-ausencia-registrada">
                <h1 id="titulo-ausencia-registrada" className="text-xl font-semibold tracking-[-0.015em] sm:text-2xl">
                    Ausencia registrada
                </h1>
                <Tarjeta className="grid gap-4 p-4 sm:p-5" data-testid="ausencia-conflictos">
                    <Aviso tono="warning" titulo={citas.length === 1 ? "1 cita queda dentro de la ausencia" : `${citas.length} citas quedan dentro de la ausencia`}>
                        Iris no las mueve sola: reprográmalas en la agenda o asígnalas a otro técnico.
                    </Aviso>
                    <ul className="grid gap-1.5 text-sm">
                        {citas.map((c) => (
                            <li key={c.ordenId} className="flex flex-wrap items-center gap-x-2">
                                <Link href={`/ordenes/${c.ordenId}`} className="font-medium text-primary underline-offset-4 hover:underline">
                                    {c.numero}
                                </Link>
                                <span className="text-muted-foreground tabular-nums">
                                    {etiquetaDia(diaDesdeIso(c.inicio) ?? "")} {horaDe(c.inicio)}–{horaDe(c.fin)}
                                </span>
                            </li>
                        ))}
                    </ul>
                    <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                        <Boton asChild variante="contorno">
                            <Link href={`/agenda?vista=dia&dia=${diaDesdeIso(citas[0]?.inicio ?? "") ?? ""}`}>Abrir la agenda de ese día</Link>
                        </Boton>
                        <Boton onClick={() => router.replace(ficha)}>Entendido</Boton>
                    </div>
                </Tarjeta>
            </section>
        );
    }

    const invalidos = Object.keys(vivos);
    return (
        <FormularioPagina
            titulo={`Ausencia de ${tecnico.nombre}`}
            descripcion="Vacaciones, enfermedad, capacitación o permiso. Hora de Guatemala."
            data-testid="formulario-ausencia"
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={ficha}
            sucio={isDirty}
            barra={{
                textoGuardar: "Registrar ausencia",
                enviando: isSubmitting,
                ...(vivos.motivo ? { faltan: ["el motivo"] } : invalidos.length > 0 ? { faltan: ["fechas válidas"], motivo: "Corrige las fechas marcadas para guardar." } : {}),
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo="No se pudo registrar la ausencia">
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="1. Motivo">
                <CampoSelector etiqueta="Motivo" error={mensaje("motivo")} {...form.register("motivo")}>
                    <option value="">Elige el motivo</option>
                    {MOTIVOS.map((m) => (
                        <option key={m.valor} value={m.valor}>
                            {m.texto}
                        </option>
                    ))}
                </CampoSelector>
                <CampoArea etiqueta="Detalle (opcional)" rows={2} maxLength={500} error={mensaje("detalle")} {...form.register("detalle")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="2. Cuándo">
                <Casilla etiqueta="Todo el día" {...form.register("todoElDia")} />
                <div className="grid grid-cols-2 gap-3">
                    <Campo etiqueta="Desde el día" type="date" error={mensaje("desdeDia")} {...form.register("desdeDia")} />
                    {valores.todoElDia ? null : <Campo etiqueta="Hora" type="time" step={900} error={mensaje("desdeHora")} {...form.register("desdeHora")} />}
                    <Campo etiqueta="Hasta el día" type="date" error={mensaje("hastaDia")} {...form.register("hastaDia")} />
                    {valores.todoElDia ? null : <Campo etiqueta="Hora" type="time" step={900} error={mensaje("hastaHora")} {...form.register("hastaHora")} />}
                </div>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
