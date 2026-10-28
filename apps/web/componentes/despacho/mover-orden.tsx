"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoArea, CampoSelector, Casilla, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie, cn, iconoPorTono } from "@iris/ui";
import { ExternalLink, LoaderCircle, RotateCw } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { InsigniaEstadoOrden, InsigniaTipoOrden } from "@/componentes/ordenes/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import type { FilaTecnico } from "@/lib/despacho/calendario";
import { diaDesdeIso, esDia, etiquetaDia, horaDe, isoDesdeDia, minutosDeTexto, type Dia } from "@/lib/despacho/fechas";
import {
    MOTIVOS_NO_DISPONIBLE,
    PERMISO_MOVER,
    accionMover,
    arrastrePermitido,
    comprobarDestino,
    entradaAsignar,
    entradaProgramar,
    esquemaMover,
    valoresIniciales,
    type Asignable,
    type DatosMover,
    type Movible,
} from "@/lib/despacho/mover";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import type { Orden } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import { useAsignables } from "./datos";

/** Destino elegido al soltar: técnico, día y minuto de Guatemala (`null` = no cambia la hora). */
export type Destino = { tecnicoId: number | null; dia: Dia | null; minutos: number | null };

const TITULOS = { programar: "Programar", reprogramar: "Reprogramar", asignar: "Asignar" } as const;

type Props = {
    movible: Movible;
    destino: Destino | undefined;
    /** La orden ya no está en lo leído (otro la movió fuera del rango o la cerró). */
    desaparecida: boolean;
    filas: readonly FilaTecnico[];
    hoy: Dia;
    alCerrar: () => void;
    alHecho: (orden: Orden, datos: DatosMover) => void;
    /** Relee el calendario (409): la versión nueva llega por `movible`. */
    recargar: () => Promise<void>;
};

/**
 * «Mover a…»: lo abre el arrastre (con el destino precargado), el teclado o el teléfono. Comprueba
 * el destino con `GET /tecnicos/asignables`, manda la acción explícita (programar, reprogramar o
 * asignar) con `version` e `Idempotency-Key`, y ante un 422 o un 409 conserva lo elegido.
 */
export function DialogoMoverOrden({ movible, destino, desaparecida, filas, hoy, alCerrar, alHecho, recargar }: Props) {
    const { puede } = useSesion();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const form = useForm<{ tecnicoId: string; soloAsignar: boolean; fecha: string; hora: string; motivo: string }, unknown, DatosMover>({
        resolver: zodResolver(esquemaMover(movible)),
        defaultValues: valoresIniciales(movible, destino, hoy, (conHora) => arrastrePermitido(movible, conHora, puede) !== null),
    });
    const [tecnicoTexto, soloAsignar, fecha, hora] = useWatch({ control: form.control, name: ["tecnicoId", "soloAsignar", "fecha", "hora"] });
    const accion = accionMover(movible.estado, !soloAsignar);
    const permitido = accion !== null && puede([PERMISO_MOVER[accion]]);
    const puedeSoloAsignar = arrastrePermitido(movible, false, puede) !== null;
    const puedeConHora = arrastrePermitido(movible, true, puede) !== null;

    const diaConsulta = esDia(fecha) ? fecha : hoy;
    const asignables = useAsignables({ fecha: diaConsulta, tipo: movible.tipo, zonaId: movible.zonaId, ordenId: movible.inicio ? movible.ordenId : null });
    const lista = asignables.estado === "listo" ? (asignables.datos.data ?? []) : null;
    const tecnicoId = /^\d+$/.test(tecnicoTexto) ? Number(tecnicoTexto) : null;
    const elegido: Asignable | undefined = lista && tecnicoId !== null ? lista.find((a) => a.usuarioId === tecnicoId) : undefined;
    const minutos = minutosDeTexto(hora);
    const inicio = soloAsignar || !esDia(fecha) || minutos === undefined ? null : isoDesdeDia(fecha, minutos);
    const comprobaciones =
        lista !== null && tecnicoId !== null ? comprobarDestino(movible, elegido, { inicio, duracionTipo: asignables.estado === "listo" ? asignables.datos.duracionMin : 60 }) : [];
    const bloqueado = comprobaciones.some((c) => c.tono === "error");
    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";

    async function enviar(d: DatosMover) {
        setError(null);
        try {
            const params = { path: { id: movible.ordenId }, header: { "Idempotency-Key": clave.actual() } };
            let orden: Orden;
            if (d.accion === "asignar") {
                orden = await resolver(api().POST("/api/v1/ordenes/{id}/asignar", { params, body: entradaAsignar(d.tecnicoId, movible.version, elegido?.cuadrillaId ?? movible.cuadrillaId) }));
            } else {
                const body = entradaProgramar(d, movible.version);
                orden =
                    d.accion === "programar"
                        ? await resolver(api().POST("/api/v1/ordenes/{id}/programar", { params, body }))
                        : await resolver(api().POST("/api/v1/ordenes/{id}/reprogramar", { params, body }));
            }
            clave.renovar();
            alHecho(orden, d);
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.estado === 422) {
                for (const x of e.errores) {
                    const campo = x.field.replace(/^body\./, "");
                    if (campo === "tecnicoId" || campo === "motivo") form.setError(campo, { type: "servidor", message: x.message });
                    if (campo === "inicio" || campo === "fin" || campo.startsWith("ventana")) form.setError("hora", { type: "servidor", message: x.message });
                }
            }
            setError(e);
        }
    }

    async function recargarOrden() {
        setRecargando(true);
        try {
            await recargar();
            setError(null);
        } finally {
            setRecargando(false);
        }
    }

    const errores = form.formState.errors;
    const enviando = form.formState.isSubmitting;
    const titulo = accion ? `${TITULOS[accion]} ${movible.numero}` : movible.numero;
    const opciones = lista ?? filas.map((f) => ({ usuarioId: f.usuarioId, nombre: f.nombre, disponible: true, motivo: null, minutosLibres: -1 }));
    const desconocido = tecnicoId !== null && !opciones.some((o) => o.usuarioId === tecnicoId);

    return (
        <Dialogo open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo={`Mover a… · ${movible.numero}`} descripcion={movible.direccion || "Sin dirección"}>
                <div className="flex flex-wrap items-center gap-2">
                    <InsigniaEstadoOrden estado={movible.estado} />
                    <InsigniaTipoOrden tipo={movible.tipo} />
                    {movible.inicio ? (
                        <span className="text-sm text-muted-foreground tabular-nums">
                            Ahora: {etiquetaDia(diaDesdeIso(movible.inicio) ?? hoy)} {horaDe(movible.inicio)}–{horaDe(movible.fin)}
                        </span>
                    ) : (
                        <span className="text-sm text-muted-foreground">Sin programar</span>
                    )}
                    <Boton asChild variante="enlace" tamano="sm" className="ml-auto">
                        <Link href={`/ordenes/${movible.ordenId}`}>
                            Ver orden
                            <ExternalLink />
                        </Link>
                    </Boton>
                </div>
                {desaparecida ? (
                    <Aviso tono="warning" titulo="Esta orden ya no está en el calendario">
                        Otra persona la movió fuera de estas fechas o cambió su estado. Ciérralo y revisa la orden.
                    </Aviso>
                ) : null}
                <form noValidate className="grid gap-4" onSubmit={form.handleSubmit(enviar)} aria-label={titulo}>
                    <CampoSelector etiqueta="Técnico" error={errores.tecnicoId?.message} {...form.register("tecnicoId")}>
                        <option value="">Elige el técnico</option>
                        {desconocido ? <option value={tecnicoId}>{filas.find((f) => f.usuarioId === tecnicoId)?.nombre ?? `Técnico ${tecnicoId}`} (no asignable)</option> : null}
                        {opciones.map((o) => (
                            <option key={o.usuarioId} value={o.usuarioId}>
                                {o.nombre}
                                {lista === null ? "" : o.disponible ? ` · libre ${Math.floor(o.minutosLibres / 60)} h ${o.minutosLibres % 60} min` : ` · ${o.motivo ? MOTIVOS_NO_DISPONIBLE[o.motivo] : "No disponible"}`}
                            </option>
                        ))}
                    </CampoSelector>
                    {puedeSoloAsignar && puedeConHora ? (
                        <Casilla
                            etiqueta={movible.estado === "pendiente" ? "Asignar el técnico sin programar la visita" : "Solo cambiar el técnico, a la misma hora"}
                            {...form.register("soloAsignar")}
                        />
                    ) : null}
                    {!soloAsignar ? (
                        <div className="grid grid-cols-2 gap-3">
                            <Campo etiqueta="Día" type="date" error={errores.fecha?.message} {...form.register("fecha")} />
                            <Campo etiqueta="Hora" type="time" step={900} error={errores.hora?.message} {...form.register("hora")} />
                        </div>
                    ) : null}
                    {!soloAsignar ? <p className="-mt-2 text-sm text-muted-foreground">Hora de Guatemala. Se conserva la duración de la cita.</p> : null}
                    {accion === "reprogramar" ? (
                        <CampoArea etiqueta="Motivo" rows={2} maxLength={500} placeholder="Por qué se mueve la visita" error={errores.motivo?.message} {...form.register("motivo")} />
                    ) : null}

                    <section aria-label="Disponibilidad" aria-live="polite" className="grid gap-1.5" data-testid="disponibilidad">
                        {asignables.estado === "cargando" ? (
                            <p className="flex items-center gap-2 text-sm text-muted-foreground">
                                <LoaderCircle aria-hidden className="size-4 animate-spin" />
                                Comprobando la disponibilidad…
                            </p>
                        ) : asignables.estado === "error" ? (
                            <Aviso
                                tono="warning"
                                titulo="No se pudo comprobar la disponibilidad"
                                accion={
                                    <Boton variante="secundario" tamano="sm" onClick={asignables.reintentar}>
                                        <RotateCw />
                                        Reintentar
                                    </Boton>
                                }
                            >
                                Puedes enviar igual: Iris valida el turno, las ausencias y la capacidad al guardar.
                            </Aviso>
                        ) : (
                            comprobaciones.map((c) => {
                                const Icono = iconoPorTono[c.tono];
                                return (
                                    <p key={c.texto} data-tono={c.tono} className={cn("flex items-start gap-2 text-sm", c.tono === "success" ? "text-success" : c.tono === "warning" ? "text-warning" : "text-error")}>
                                        <Icono aria-hidden className="mt-0.5 size-4 shrink-0" />
                                        <span className="text-foreground">{c.texto}</span>
                                    </p>
                                );
                            })
                        )}
                    </section>

                    {!permitido && accion ? (
                        <Aviso tono="warning" titulo="No tienes permiso para esta acción">
                            Hace falta el permiso «{PERMISO_MOVER[accion]}».
                        </Aviso>
                    ) : null}
                    {conflicto ? (
                        <Aviso
                            tono="warning"
                            titulo="Alguien más cambió esta orden"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={recargarOrden} disabled={recargando}>
                                    <RotateCw />
                                    {recargando ? "Recargando…" : "Recargar"}
                                </Boton>
                            }
                        >
                            Recarga para ver el cambio y vuelve a intentarlo; lo que elegiste se conserva.
                        </Aviso>
                    ) : error ? (
                        <Aviso tono="error" titulo={`No se pudo ${accion ? TITULOS[accion].toLowerCase() : "mover"} la orden`}>
                            {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={enviando}>
                                Volver
                            </Boton>
                        </DialogoCerrar>
                        <Boton type="submit" disabled={enviando || conflicto || bloqueado || !permitido || desaparecida}>
                            {enviando ? "Enviando…" : accion ? TITULOS[accion] : "Mover"}
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
