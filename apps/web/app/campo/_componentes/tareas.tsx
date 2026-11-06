"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { TareaVista } from "@iris/sync";
import {
    DURACIONES_TAREA,
    datosTarea,
    detalleTarea,
    esquemaTarea,
    horaTarea,
    hoyEnGuatemala,
    nombreCliente,
    textoCuando,
    valoresInicialesTarea,
    type ValoresTarea,
} from "@iris/sync/campo";
import {
    Boton,
    Campo,
    CampoSelector,
    DialogoConfirmar,
    SelectorFecha,
    SelectorHora,
    cn,
} from "@iris/ui";
import { ArrowLeft, Ban, Check, SquareCheck } from "lucide-react";
import { useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useCampo } from "./contexto";

/** Botón primario «tinta» del diseño: fondo del texto, texto invertido. */
export const BOTON_TINTA =
    "h-[3.25rem] w-full rounded-full bg-[var(--campo-tinta)] text-base font-semibold text-[var(--campo-sobre-tinta)] hover:opacity-90";

/** «Nueva tarea»: pantalla propia (formulario de más de 3 campos), también sin señal. */
export function NuevaTarea() {
    const { motor, estado, ejecutar, navegar } = useCampo();
    const [inicial] = useState(() => valoresInicialesTarea());
    const form = useForm<ValoresTarea>({
        resolver: zodResolver(esquemaTarea),
        defaultValues: inicial,
    });
    const [saliendo, setSaliendo] = useState(false);
    const ordenes = useMemo(
        () =>
            estado.ordenes.filter(
                (o) =>
                    !o.reasignada &&
                    !["cerrada", "fallida", "cancelada"].includes(o.estado),
            ),
        [estado.ordenes],
    );
    const volver = () => navegar({ tipo: "lista" });
    const atras = () => (form.formState.isDirty ? setSaliendo(true) : volver());
    const enviar = form.handleSubmit(async (v) => {
        const ok = await ejecutar(
            () => motor.registrarTarea("tarea.crear", datosTarea(v)),
            `Tarea agendada: ${textoCuando(v.dia, v.hora)}`,
        );
        if (ok) {
            volver();
        }
    });
    const errores = form.formState.errors;
    return (
        <section
            aria-labelledby="titulo-nueva-tarea"
            className="grid gap-4"
            data-slot="nueva-tarea"
        >
            <header className="grid grid-cols-[3rem_1fr_3rem] items-center">
                <Boton
                    variante="secundario"
                    aria-label="Atrás"
                    className="size-12 rounded-full p-0"
                    onClick={atras}
                >
                    <ArrowLeft aria-hidden className="size-5" />
                </Boton>
                <h1
                    id="titulo-nueva-tarea"
                    className="text-center text-xl font-semibold"
                >
                    Nueva tarea
                </h1>
            </header>
            <form onSubmit={enviar} noValidate className="grid gap-4">
                <div className="grid gap-4 rounded-[1.375rem] border border-border bg-[var(--campo-panel)] p-4">
                    <Campo
                        etiqueta="Qué vas a hacer"
                        placeholder="Recoger material en bodega central"
                        maxLength={200}
                        error={errores.que?.message}
                        {...form.register("que")}
                    />
                    <div className="grid grid-cols-2 gap-3">
                        {/* Día y hora de Guatemala (es-GT, 24 h): el input nativo sigue el idioma del navegador. */}
                        <Controller
                            control={form.control}
                            name="dia"
                            render={({ field }) => (
                                <SelectorFecha
                                    etiqueta="Día"
                                    min={hoyEnGuatemala()}
                                    valor={field.value || null}
                                    alCambiar={(v) => field.onChange(v ?? "")}
                                    error={errores.dia?.message}
                                />
                            )}
                        />
                        <Controller
                            control={form.control}
                            name="hora"
                            render={({ field }) => (
                                <SelectorHora
                                    etiqueta="Hora"
                                    valor={field.value || null}
                                    alCambiar={(v) => field.onChange(v ?? "")}
                                    error={errores.hora?.message}
                                />
                            )}
                        />
                    </div>
                    <Controller
                        control={form.control}
                        name="duracionMin"
                        render={({ field }) => (
                            <fieldset className="grid gap-2">
                                <legend className="mb-2 text-sm font-medium">
                                    Duración
                                </legend>
                                <div
                                    className="grid grid-cols-4 gap-2"
                                    role="radiogroup"
                                    aria-label="Duración"
                                >
                                    {DURACIONES_TAREA.map((d) => {
                                        const elegida =
                                            field.value === d.minutos;
                                        return (
                                            <Boton
                                                key={d.minutos}
                                                variante="fantasma"
                                                role="radio"
                                                aria-checked={elegida}
                                                className={cn(
                                                    "h-11 w-full min-w-0 rounded-full px-0 text-[0.9375rem]",
                                                    elegida
                                                        ? "bg-[var(--campo-tinta)] font-semibold text-[var(--campo-sobre-tinta)] hover:bg-[var(--campo-tinta)]"
                                                        : "bg-[var(--campo-panel-2)] font-medium text-foreground",
                                                )}
                                                onClick={() =>
                                                    field.onChange(d.minutos)
                                                }
                                            >
                                                {d.texto}
                                            </Boton>
                                        );
                                    })}
                                </div>
                            </fieldset>
                        )}
                    />
                    <CampoSelector
                        etiqueta="Ligar a una orden (opcional)"
                        error={errores.ligada?.message}
                        {...form.register("ligada")}
                    >
                        <option value="">Ninguna</option>
                        {ordenes.map((o) => (
                            <option key={o.id} value={`orden:${o.id}`}>
                                {o.numero} · {nombreCliente(o)}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
                <p className="px-1 text-sm text-muted-foreground">
                    Aparece en tu agenda de Hoy. Tu supervisor la ve. Funciona
                    sin señal: se envía al volver.
                </p>
                <Boton
                    type="submit"
                    tamano="lg"
                    className={BOTON_TINTA}
                    disabled={form.formState.isSubmitting}
                >
                    Agendar
                </Boton>
            </form>
            <DialogoConfirmar
                open={saliendo}
                onOpenChange={setSaliendo}
                titulo="¿Descartar la tarea?"
                descripcion="Lo que escribiste no se guardó."
                textoCancelar="Seguir editando"
                textoConfirmar="Descartar"
                peligro
                alConfirmar={volver}
            />
        </section>
    );
}

/**
 * Tarea propia en Hoy y Agenda: borde punteado, la hora a la izquierda y «Hecha» / «Anular».
 * Completar y anular van por la cola (también sin señal).
 */
export function TarjetaTarea({ tarea }: { tarea: TareaVista }) {
    const { motor, ejecutar } = useCampo();
    const [anulando, setAnulando] = useState(false);
    const hecha = tarea.estado === "completada";
    return (
        <>
            <article
                data-slot="tarea-propia"
                data-estado={tarea.estado}
                aria-label={`Tarea propia: ${tarea.que}`}
                className="flex items-center gap-4 rounded-[1.375rem] border border-dashed border-[var(--hx-text-muted,var(--hx-border))] bg-[var(--campo-panel)] p-4"
            >
                <span className="grid h-14 w-[4.5rem] shrink-0 place-content-center rounded-2xl bg-[var(--campo-panel-2)] text-lg font-semibold tabular-nums">
                    {horaTarea(tarea)}
                </span>
                <div className="grid min-w-0 flex-1 gap-1">
                    <p
                        className={cn(
                            "text-[1.0625rem] leading-snug font-semibold",
                            hecha && "text-muted-foreground line-through",
                        )}
                    >
                        {tarea.que}
                    </p>
                    <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
                        <SquareCheck aria-hidden className="size-4 shrink-0" />
                        {detalleTarea(tarea)}
                        {tarea.pendientes > 0 ? (
                            <span className="sr-only"> · por enviar</span>
                        ) : null}
                    </p>
                    {hecha ? null : (
                        <div className="mt-1 flex gap-2">
                            <Boton
                                variante="secundario"
                                className="h-11 rounded-full"
                                onClick={() =>
                                    void ejecutar(
                                        () =>
                                            motor.registrarTarea(
                                                "tarea.completar",
                                                { tareaId: tarea.id },
                                            ),
                                        "Tarea hecha",
                                    )
                                }
                            >
                                <Check aria-hidden />
                                Hecha
                            </Boton>
                            <Boton
                                variante="fantasma"
                                className="h-11 rounded-full"
                                onClick={() => setAnulando(true)}
                            >
                                <Ban aria-hidden />
                                Anular
                            </Boton>
                        </div>
                    )}
                </div>
            </article>
            <DialogoConfirmar
                open={anulando}
                onOpenChange={setAnulando}
                titulo="¿Anular la tarea?"
                descripcion={`«${tarea.que}» sale de tu agenda. Queda en el historial.`}
                textoConfirmar="Anular"
                peligro
                alConfirmar={async () => {
                    await ejecutar(
                        () =>
                            motor.registrarTarea("tarea.anular", {
                                tareaId: tarea.id,
                            }),
                        "Tarea anulada",
                    );
                    setAnulando(false);
                }}
            />
        </>
    );
}
