"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoSelector, DialogoCerrar, DialogoPie } from "@iris/ui";
import { CalendarCheck, Plus, RotateCw, Trash2 } from "lucide-react";
import { useState } from "react";
import { useFieldArray, useForm, useWatch } from "react-hook-form";
import { DialogoFormulario } from "@/componentes/cierre-seguro";
import { DIAS_SEMANA, TURNOS_HABILES, entradaTurnos, erroresDe, esquemaTurnos, valoresTurnos, type TecnicoDetalle, type ValoresTurnos } from "@/lib/agenda/esquemas";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useClaveIdempotencia } from "@/lib/idempotencia";

/**
 * Turnos semanales del técnico (hora de Guatemala): una fila por tramo, sin cruces en un día y con
 * el fin hasta 24:00. Sin turnos no se le puede programar nada.
 */
export function FormularioTurnos({ tecnico, alCerrar, alGuardar }: { tecnico: TecnicoDetalle; alCerrar: () => void; alGuardar: (t: TecnicoDetalle) => void }) {
    const [antes, setAntes] = useState(tecnico);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const form = useForm<ValoresTurnos>({ resolver: zodResolver(esquemaTurnos), defaultValues: valoresTurnos(tecnico.turnos) });
    const filas = useFieldArray({ control: form.control, name: "turnos" });
    const valores = useWatch({ control: form.control }) as ValoresTurnos;
    const vivos = erroresDe(esquemaTurnos.safeParse(valores));
    const { errors, isSubmitting, isDirty } = form.formState;
    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";
    // Día sin elegir: aviso inmediato (es una selección); horas: el error vivo también, para que el botón deshabilitado se explique.
    const mensaje = (i: number, campo: "dia" | "inicio" | "fin") => errors.turnos?.[i]?.[campo]?.message ?? vivos[`turnos.${i}.${campo}`];

    async function guardar(v: ValoresTurnos) {
        setError(null);
        try {
            const t = await resolver(
                api().POST("/api/v1/tecnicos/{id}/turnos", { params: { path: { id: antes.id }, header: { "Idempotency-Key": clave.actual() } }, body: entradaTurnos(v, antes.version) }),
            );
            clave.renovar();
            alGuardar(t);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        }
    }

    async function recargar() {
        setRecargando(true);
        try {
            setAntes(await resolver(api().GET("/api/v1/tecnicos/{id}", { params: { path: { id: antes.id } } })));
            setError(null);
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    return (
        <DialogoFormulario titulo={`Turnos de ${antes.nombre}`} descripcion="Hora de Guatemala. Un turno nocturno se parte en dos (hasta 24:00 y desde 00:00)." sucio={isDirty} alCerrar={alCerrar} data-testid="formulario-turnos">
            <form noValidate className="grid gap-4" aria-label="Turnos semanales" onSubmit={form.handleSubmit(guardar)}>
                {filas.fields.length === 0 ? (
                    <Aviso tono="warning" titulo="Sin turnos">
                        Sin turnos no se le pueden programar órdenes. Agrega al menos uno.
                    </Aviso>
                ) : null}
                <ul className="grid gap-3" aria-label="Turnos">
                    {filas.fields.map((f, i) => (
                        <li key={f.id} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-start gap-2 border-t border-border pt-3 first:border-t-0 first:pt-0 sm:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)_auto] sm:border-t-0 sm:pt-0" data-turno={i}>
                            <CampoSelector etiqueta="Día" className="col-span-3 sm:col-span-1" error={mensaje(i, "dia")} {...form.register(`turnos.${i}.dia`)}>
                                <option value="">Elige</option>
                                {DIAS_SEMANA.map((d) => (
                                    <option key={d.dia} value={d.dia}>
                                        {d.texto}
                                    </option>
                                ))}
                            </CampoSelector>
                            <Campo etiqueta="Desde" inputMode="numeric" placeholder="08:00" maxLength={5} error={mensaje(i, "inicio")} {...form.register(`turnos.${i}.inicio`)} />
                            <Campo etiqueta="Hasta" inputMode="numeric" placeholder="17:00" maxLength={5} error={mensaje(i, "fin")} {...form.register(`turnos.${i}.fin`)} />
                            <Boton type="button" variante="fantasma" tamano="icono" className="mt-6" aria-label={`Quitar el turno ${i + 1}`} title="Quitar" onClick={() => filas.remove(i)}>
                                <Trash2 />
                            </Boton>
                        </li>
                    ))}
                </ul>
                <div className="flex flex-wrap gap-2">
                    <Boton type="button" variante="secundario" tamano="sm" onClick={() => filas.append({ dia: "", inicio: "08:00", fin: "17:00" })}>
                        <Plus />
                        Agregar turno
                    </Boton>
                    <Boton type="button" variante="contorno" tamano="sm" onClick={() => filas.replace(TURNOS_HABILES.map((t) => ({ ...t })))}>
                        <CalendarCheck />
                        Lunes a viernes, 08:00–17:00
                    </Boton>
                </div>
                {conflicto ? (
                    <Aviso
                        tono="warning"
                        titulo="Alguien más cambió esta ficha"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={recargar} disabled={recargando}>
                                <RotateCw />
                                {recargando ? "Recargando…" : "Recargar"}
                            </Boton>
                        }
                    >
                        Recarga para tomar la versión nueva; los turnos que escribiste se conservan.
                    </Aviso>
                ) : error ? (
                    <Aviso tono="error" titulo="No se pudieron guardar los turnos">
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Volver
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting || conflicto || !isDirty || Object.keys(vivos).length > 0}>
                        {isSubmitting ? "Guardando…" : "Guardar turnos"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoFormulario>
    );
}
