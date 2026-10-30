"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, BotonEnviar, CampoSelector, Dialogo, DialogoCerrar, DialogoProtegido, useCambiosSinGuardar, DialogoContenido, DialogoPie, Esqueleto, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { CirclePause, ClipboardList, FilePlus, PenLine, RotateCw, Trash2 } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import type { Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearFecha } from "@/lib/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { usePaginado } from "@/lib/paginado";
import {
    agruparChecklist,
    CIERRES_PAUSA,
    ESTADOS_FINALES,
    ESTADOS_ORDEN,
    formularioQuitable,
    MOTIVOS_PAUSA,
    ORIGENES_FORMULARIO,
    segundosPausada,
    textoDuracion,
    type FormularioOrden,
    type OrdenDetalle,
} from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import { ItemsChecklist, type Yo } from "./evidencias";

type Props = { orden: OrdenDetalle; usuarios: readonly Usuario[] | null | undefined; yo: Yo; recargar: () => Promise<void> };

/**
 * Formularios de la orden (el del tipo, los de despacho y los del técnico), cada uno con su
 * versión y sus ítems. «Agregar formulario» y «Quitar» con `ordenes.asignar_formulario`; el del
 * tipo nunca se quita y uno con respuestas tampoco.
 */
export function Formularios({ orden, usuarios, yo, recargar }: Props) {
    const { puede } = useSesion();
    const final = ESTADOS_FINALES.includes(orden.estado);
    const administra = puede(["ordenes.asignar_formulario"]) && !final;
    const grupos = agruparChecklist(orden);
    const [agregando, setAgregando] = useState(false);
    const [quitando, setQuitando] = useState<FormularioOrden | null>(null);
    return (
        <Tarjeta data-testid="formularios">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Formularios</TarjetaTitulo>
                {administra ? (
                    <Boton variante="secundario" tamano="sm" onClick={() => setAgregando(true)}>
                        <FilePlus />
                        Agregar formulario
                    </Boton>
                ) : null}
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4">
                {grupos.length === 0 ? <p className="text-sm text-muted-foreground">Esta orden no tiene formularios.</p> : null}
                {grupos.map((g) => (
                    <section key={g.formulario?.id ?? "sueltos"} className="grid gap-2 rounded-lg border p-3" aria-label={g.formulario?.nombre ?? "Checklist"} data-formulario={g.formulario?.codigo ?? ""}>
                        {g.formulario ? (
                            <header className="flex flex-wrap items-start justify-between gap-2">
                                <div className="grid min-w-0 gap-1">
                                    <span className="font-medium">{g.formulario.nombre}</span>
                                    <span className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                                        <InsigniaEstado tono="neutral" icono={ClipboardList} texto={ORIGENES_FORMULARIO[g.formulario.origen]} />
                                        <span className="tabular-nums">versión {g.formulario.numeroVersion}</span>
                                        {g.formulario.requiereFirma ? (
                                            <span className="inline-flex items-center gap-1">
                                                <PenLine aria-hidden className="size-3.5" />
                                                pide firma
                                            </span>
                                        ) : null}
                                        <span className="tabular-nums">· {formatearFecha(g.formulario.agregadoEn)}</span>
                                    </span>
                                </div>
                                {administra && formularioQuitable(g.formulario) ? (
                                    <Boton variante="fantasma" tamano="sm" onClick={() => setQuitando(g.formulario)} aria-label={`Quitar ${g.formulario.nombre}`}>
                                        <Trash2 />
                                        Quitar
                                    </Boton>
                                ) : null}
                            </header>
                        ) : (
                            <span className="font-medium">Checklist</span>
                        )}
                        <ItemsChecklist items={g.items} final={final} usuarios={usuarios} yo={yo} etiqueta={`Ítems de ${g.formulario?.nombre ?? "la orden"}`} />
                    </section>
                ))}
            </TarjetaContenido>
            <DialogoProtegido open={agregando} onOpenChange={setAgregando}>
                {agregando ? (
                    <AgregarFormulario
                        orden={orden}
                        recargar={recargar}
                        alHecho={async () => {
                            setAgregando(false);
                            await recargar();
                        }}
                    />
                ) : null}
            </DialogoProtegido>
            <Dialogo open={quitando !== null} onOpenChange={(v) => (v ? undefined : setQuitando(null))}>
                {quitando ? (
                    <QuitarFormulario
                        orden={orden}
                        formulario={quitando}
                        recargar={recargar}
                        alHecho={async () => {
                            setQuitando(null);
                            await recargar();
                        }}
                    />
                ) : null}
            </Dialogo>
        </Tarjeta>
    );
}

/** Envío con `Idempotency-Key`, 409 → «Recargar» (la orden trae su versión nueva) y 422 con el `detail`. */
function useEnvioOrden(recargar: () => Promise<void>) {
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const conflicto = error instanceof ErrorApi && error.estado === 409 && !error.codigo.startsWith("idempotencia");
    /** `null` si salió bien; si no, el error (también queda para el aviso). */
    async function enviar(peticion: (clave: string) => Promise<unknown>): Promise<unknown> {
        setError(null);
        try {
            await peticion(clave.actual());
            clave.renovar();
            return null;
        } catch (e) {
            clave.trasError(e);
            setError(e);
            return e ?? new Error("error");
        }
    }
    const aviso = (que: string) =>
        conflicto ? (
            <Aviso
                tono="warning"
                titulo="Alguien más cambió esta orden"
                accion={
                    <Boton
                        type="button"
                        variante="secundario"
                        tamano="sm"
                        disabled={recargando}
                        onClick={async () => {
                            setRecargando(true);
                            try {
                                await recargar();
                                setError(null);
                            } finally {
                                setRecargando(false);
                            }
                        }}
                    >
                        <RotateCw />
                        {recargando ? "Recargando…" : "Recargar"}
                    </Boton>
                }
            >
                {error instanceof ErrorApi && error.codigo === "formulario_duplicado" ? mensajeDeError(error).detalle : "Recarga para ver el cambio y vuelve a intentarlo."}
            </Aviso>
        ) : error ? (
            <Aviso tono="error" titulo={`No se pudo ${que}`}>
                {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
            </Aviso>
        ) : null;
    return { enviar, error, setError, conflicto, aviso };
}

const LIMITE_PLANTILLAS = 50;

const esquemaAgregar = z.object({ plantillaId: z.string().regex(/^[1-9]\d*$/, "Elige el formulario") });

function AgregarFormulario({ orden, recargar, alHecho }: { orden: OrdenDetalle; recargar: () => Promise<void>; alHecho: () => Promise<void> }) {
    // Las del tipo de la orden y las de cualquier tipo, activas.
    // Paginadas por nombre: «Cargar más» trae las siguientes al selector.
    const paginado = usePaginado<Schema<"PlantillaFormulario">>((cursor, signal) =>
        resolver(api().GET("/api/v1/plantillas", { params: { query: { tipo: orden.tipo, limit: LIMITE_PLANTILLAS, ...(cursor ? { cursor } : {}) } }, ...(signal ? { signal } : {}) })),
    );
    const plantillas = paginado.primera;
    const envio = useEnvioOrden(recargar);
    const formulario = useForm<z.infer<typeof esquemaAgregar>>({ resolver: zodResolver(esquemaAgregar), defaultValues: { plantillaId: "" } });
    useCambiosSinGuardar(formulario.formState.isDirty);
    const sinPlantilla = !useWatch({ control: formulario.control, name: "plantillaId" });
    const presentes = new Set((orden.formularios ?? []).map((f) => f.plantillaId));
    const disponibles = paginado.items.filter((p) => p.activa && !presentes.has(p.id));
    async function enviar(d: z.infer<typeof esquemaAgregar>) {
        const error = await envio.enviar((clave) =>
            resolver(api().POST("/api/v1/ordenes/{id}/formularios", { params: { path: { id: orden.id }, header: { "Idempotency-Key": clave } }, body: { version: orden.version, plantillaId: Number(d.plantillaId) } })),
        );
        if (error === null) await alHecho();
        else if (erroresACampos(error, ["plantillaId"], formulario.setError)) envio.setError(null);
    }
    return (
        <DialogoContenido titulo="Agregar formulario" descripcion={`${orden.numero} · el técnico lo verá en su teléfono con la versión actual.`}>
            {plantillas.estado === "cargando" ? (
                <Esqueleto className="h-20" />
            ) : plantillas.estado === "error" ? (
                <ErrorLectura error={plantillas.error} que="los formularios disponibles" reintentar={plantillas.reintentar} />
            ) : disponibles.length === 0 && !paginado.siguiente ? (
                <p className="text-sm text-muted-foreground">No hay más formularios activos para este tipo de orden.</p>
            ) : (
                <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4" aria-label="Agregar formulario">
                    <CampoSelector
                        etiqueta="Formulario"
                        ayuda={sinPlantilla ? "Elige el formulario que llenará el técnico." : undefined}
                        error={formulario.formState.errors.plantillaId?.message}
                        {...formulario.register("plantillaId")}
                    >
                        <option value="">Elige…</option>
                        {disponibles.map((p) => (
                            <option key={p.id} value={String(p.id)}>
                                {p.nombre} · v{p.versionActual}
                                {p.empresaId === null ? " · común" : ""}
                            </option>
                        ))}
                    </CampoSelector>
                    <PieLista paginado={paginado} que="formularios" />
                    {envio.aviso("agregar el formulario")}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno">Volver</Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={formulario.formState.isSubmitting} textoEnviando="Agregando…" disabled={envio.conflicto} faltan={sinPlantilla ? ["el formulario"] : []}>
                            <FilePlus />
                            Agregar
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            )}
        </DialogoContenido>
    );
}

function QuitarFormulario({ orden, formulario, recargar, alHecho }: { orden: OrdenDetalle; formulario: FormularioOrden; recargar: () => Promise<void>; alHecho: () => Promise<void> }) {
    const envio = useEnvioOrden(recargar);
    const [enviando, setEnviando] = useState(false);
    async function quitar() {
        setEnviando(true);
        const error = await envio.enviar((clave) =>
            resolver(
                api().POST("/api/v1/ordenes/{id}/formularios/{fid}/quitar", {
                    params: { path: { id: orden.id, fid: formulario.id }, header: { "Idempotency-Key": clave } },
                    body: { version: orden.version },
                }),
            ),
        );
        setEnviando(false);
        if (error === null) await alHecho();
    }
    return (
        <DialogoContenido titulo={`Quitar «${formulario.nombre}»`} descripcion="Queda en el historial de la orden. Solo se puede mientras no tenga respuestas.">
            {envio.aviso("quitar el formulario")}
            <DialogoPie>
                <DialogoCerrar asChild>
                    <Boton variante="contorno">Volver</Boton>
                </DialogoCerrar>
                <Boton variante="peligro" onClick={quitar} disabled={enviando || envio.conflicto}>
                    <Trash2 />
                    {enviando ? "Quitando…" : "Quitar"}
                </Boton>
            </DialogoPie>
        </DialogoContenido>
    );
}

/** Estado de pausa, tiempo total pausado (con la pausa en curso) e historial de pausas. */
export function Pausas({ orden, ahora }: { orden: OrdenDetalle; ahora: number }) {
    const pausas = orden.pausas ?? [];
    if (pausas.length === 0 && orden.estado !== "pausada") return null;
    const total = segundosPausada(orden, ahora);
    const abierta = pausas.find((p) => p.fin === null);
    return (
        <Tarjeta data-testid="pausas">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Pausas</TarjetaTitulo>
                <span className="text-sm text-muted-foreground tabular-nums" data-testid="tiempo-pausado">
                    Total: {textoDuracion(total)}
                </span>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {orden.estado === "pausada" ? (
                    <Aviso tono="warning" icono={CirclePause} titulo={`Pausada${abierta ? `: ${MOTIVOS_PAUSA[abierta.motivo]}` : ""}`}>
                        {orden.pausadaEn ? `Desde ${formatearFecha(orden.pausadaEn)}` : ""}
                        {orden.pausadaDesde ? `, estaba ${ESTADOS_ORDEN[orden.pausadaDesde].texto.toLowerCase()}` : ""}. El técnico no puede llegar, cerrar ni
                        fallar hasta que se reanude; se puede reprogramar o cancelar.
                    </Aviso>
                ) : null}
                <ol className="grid gap-2" aria-label="Historial de pausas">
                    {[...pausas].reverse().map((p) => (
                        <li key={p.id} className="grid gap-0.5 border-l-2 pl-3 text-sm" data-pausa={p.motivo}>
                            <span className="font-medium">
                                {MOTIVOS_PAUSA[p.motivo]}
                                <span className="font-normal text-muted-foreground">
                                    {" "}
                                    · {p.duracionSeg === null ? "en curso" : textoDuracion(p.duracionSeg)}
                                    {p.cierre ? ` · ${CIERRES_PAUSA[p.cierre]}` : ""}
                                </span>
                            </span>
                            {p.nota ? <span className="text-muted-foreground">{p.nota}</span> : null}
                            <span className="text-xs text-muted-foreground tabular-nums">
                                {formatearFecha(p.inicio)}
                                {p.fin ? ` – ${formatearFecha(p.fin)}` : ""} · desde {ESTADOS_ORDEN[p.desdeEstado].texto.toLowerCase()}
                            </span>
                        </li>
                    ))}
                </ol>
            </TarjetaContenido>
        </Tarjeta>
    );
}
