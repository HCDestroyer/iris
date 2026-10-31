"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, CampoArea, CampoSelector, Casilla, DialogoCerrar, DialogoPie } from "@iris/ui";
import Link from "next/link";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { DialogoFormulario } from "@/componentes/cierre-seguro";
import { MOTIVOS, entradaAusencia, erroresDe, esquemaAusencia, valoresAusencia, type TecnicoDetalle, type ValoresAusencia } from "@/lib/agenda/esquemas";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { diaDesdeIso, etiquetaDia, horaDe, hoyGuatemala } from "@/lib/despacho/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import type { Schema } from "@iris/contratos";

type Respuesta = Schema<"RespuestaAusencia">;

/**
 * Registrar una ausencia (hora de Guatemala). La API no mueve las citas que caen dentro: las
 * devuelve y aquí se muestran para reprogramarlas.
 */
export function FormularioAusencia({ tecnico, alCerrar, alGuardar }: { tecnico: TecnicoDetalle; alCerrar: () => void; alGuardar: () => void }) {
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
            form.reset(v);
            avisar({ titulo: `Ausencia de ${tecnico.nombre} registrada` });
            if ((r.citasEnConflicto ?? []).length > 0) {
                setHecho(r);
            } else {
                alGuardar();
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
            <DialogoFormulario titulo="Ausencia registrada" sucio={false} alCerrar={alGuardar} data-testid="ausencia-conflictos">
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
                <DialogoPie>
                    <Boton asChild variante="contorno">
                        <Link href={`/agenda?vista=dia&dia=${diaDesdeIso(citas[0]?.inicio ?? "") ?? ""}`}>Abrir la agenda de ese día</Link>
                    </Boton>
                    <Boton onClick={alGuardar}>Entendido</Boton>
                </DialogoPie>
            </DialogoFormulario>
        );
    }

    return (
        <DialogoFormulario titulo={`Ausencia de ${tecnico.nombre}`} descripcion="Vacaciones, enfermedad, capacitación o permiso. Hora de Guatemala." sucio={isDirty} alCerrar={alCerrar} data-testid="formulario-ausencia">
            <form noValidate className="grid gap-4" aria-label="Registrar ausencia" onSubmit={form.handleSubmit(guardar)}>
                <CampoSelector etiqueta="Motivo" error={mensaje("motivo")} {...form.register("motivo")}>
                    <option value="">Elige el motivo</option>
                    {MOTIVOS.map((m) => (
                        <option key={m.valor} value={m.valor}>
                            {m.texto}
                        </option>
                    ))}
                </CampoSelector>
                <Casilla etiqueta="Todo el día" {...form.register("todoElDia")} />
                <div className="grid grid-cols-2 gap-3">
                    <Campo etiqueta="Desde el día" type="date" error={mensaje("desdeDia")} {...form.register("desdeDia")} />
                    {valores.todoElDia ? null : <Campo etiqueta="Hora" type="time" step={900} error={mensaje("desdeHora")} {...form.register("desdeHora")} />}
                    <Campo etiqueta="Hasta el día" type="date" error={mensaje("hastaDia")} {...form.register("hastaDia")} />
                    {valores.todoElDia ? null : <Campo etiqueta="Hora" type="time" step={900} error={mensaje("hastaHora")} {...form.register("hastaHora")} />}
                </div>
                <CampoArea etiqueta="Detalle (opcional)" rows={2} maxLength={500} error={mensaje("detalle")} {...form.register("detalle")} />
                {error ? (
                    <Aviso tono="error" titulo="No se pudo registrar la ausencia">
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Volver
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting || Object.keys(vivos).length > 0}>
                        {isSubmitting ? "Guardando…" : "Registrar ausencia"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoFormulario>
    );
}
