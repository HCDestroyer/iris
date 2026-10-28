"use client";

import { CODIGOS_TERMINALES, codigoProblema, faltantesDe, type AdjuntoLocal, type OperacionLocal } from "@iris/sync";
import { Aviso, Boton, EstadoVacio, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ArrowLeft, CloudCheck, CloudUpload, Hourglass, RefreshCw, Trash2, TriangleAlert, Undo2 } from "lucide-react";
import { formatearFecha } from "@/lib/fechas";
import { describirCampo, detallesProblema, OPERACIONES, textoProblema } from "@/lib/campo/textos";
import { useCampo } from "./contexto";

/** Lo que queda por enviar, lo que espera adjuntos y los rechazos del servidor con su motivo. */
export function Pendientes() {
    const { estado, navegar, motor } = useCampo();
    const adjuntosRechazados = estado.adjuntos.filter((a) => a.estado === "rechazado");
    const subiendo = estado.adjuntos.filter((a) => a.estado === "por_crear" || a.estado === "subiendo");
    const nada = estado.cola.length + estado.rechazos.length + estado.esperandoAdjuntos.length + estado.adjuntos.filter((a) => a.estado !== "subido").length === 0;

    return (
        <section aria-labelledby="titulo-pendientes" className="grid gap-4">
            <Boton variante="fantasma" className="-ml-2 w-fit" onClick={() => navegar({ tipo: "lista" })}>
                <ArrowLeft aria-hidden />
                Mis órdenes
            </Boton>
            <div className="flex items-center justify-between gap-2">
                <h1 id="titulo-pendientes" className="text-2xl font-semibold tracking-[-0.015em]">
                    Por enviar
                </h1>
                {!nada ? (
                    <Boton variante="secundario" onClick={() => motor.reintentarAhora()}>
                        <RefreshCw aria-hidden />
                        Reintentar
                    </Boton>
                ) : null}
            </div>
            {nada ? <EstadoVacio icono={CloudCheck} titulo="Todo enviado" descripcion="No queda nada por enviar desde este teléfono." /> : null}

            {estado.rechazos.length > 0 ? (
                <section aria-labelledby="titulo-rechazos" className="grid gap-3">
                    <h2 id="titulo-rechazos" className="text-lg font-semibold">
                        Rechazados ({estado.rechazos.length})
                    </h2>
                    {estado.rechazos.map((op) => (
                        <Rechazo key={op.id} op={op} />
                    ))}
                </section>
            ) : null}

            {adjuntosRechazados.length > 0 ? (
                <section aria-labelledby="titulo-adjuntos-rechazados" className="grid gap-3">
                    <h2 id="titulo-adjuntos-rechazados" className="text-lg font-semibold">
                        Archivos rechazados ({adjuntosRechazados.length})
                    </h2>
                    {adjuntosRechazados.map((a) => (
                        <AdjuntoRechazado key={a.id} adjunto={a} />
                    ))}
                </section>
            ) : null}

            {estado.esperandoAdjuntos.length > 0 ? (
                <section aria-labelledby="titulo-esperando" className="grid gap-3">
                    <h2 id="titulo-esperando" className="text-lg font-semibold">
                        Esperando archivos
                    </h2>
                    {estado.esperandoAdjuntos.map((op) => (
                        <Esperando key={op.id} op={op} />
                    ))}
                </section>
            ) : null}

            {subiendo.length > 0 ? (
                <Tarjeta>
                    <TarjetaEncabezado>
                        <TarjetaTitulo>Archivos por subir ({subiendo.length})</TarjetaTitulo>
                    </TarjetaEncabezado>
                    <TarjetaContenido>
                        <ul className="grid gap-2 text-sm">
                            {subiendo.map((a) => (
                                <li key={a.id} className="flex items-center justify-between gap-2">
                                    <span>
                                        {a.tipo === "firma" ? "Firma" : "Foto"} · {numeroOrden(estado.ordenes, a.ordenId)}
                                    </span>
                                    <InsigniaEstado
                                        tono="warning"
                                        icono={CloudUpload}
                                        texto={a.estado === "por_crear" ? "Por subir" : `${Math.floor((a.offset / Math.max(1, a.tamano)) * 100)} %`}
                                    />
                                </li>
                            ))}
                        </ul>
                        {subiendo.some((a) => a.ultimoError) ? <p className="mt-2 text-xs text-muted-foreground">Se reintenta solo con espera.</p> : null}
                    </TarjetaContenido>
                </Tarjeta>
            ) : null}

            {estado.cola.length > 0 ? (
                <Tarjeta>
                    <TarjetaEncabezado>
                        <TarjetaTitulo>Pendientes por enviar ({estado.cola.length})</TarjetaTitulo>
                    </TarjetaEncabezado>
                    <TarjetaContenido>
                        <ol className="grid gap-2 text-sm" aria-label="Cola de envío">
                            {estado.cola.map((op) => (
                                <li key={op.id} className="flex items-center justify-between gap-2">
                                    <span>
                                        {OPERACIONES[op.tipo]} · {numeroOrden(estado.ordenes, op.ordenId)}
                                    </span>
                                    <span className="text-xs text-muted-foreground tabular-nums">{formatearFecha(op.ocurridoEn)?.slice(-5)}</span>
                                </li>
                            ))}
                        </ol>
                    </TarjetaContenido>
                </Tarjeta>
            ) : null}
        </section>
    );
}

function numeroOrden(ordenes: readonly { id: number; numero: string }[], id: number): string {
    return ordenes.find((o) => o.id === id)?.numero ?? `Orden ${id}`;
}

function Rechazo({ op }: { op: OperacionLocal }) {
    const { estado, motor, ejecutar } = useCampo();
    const orden = estado.ordenes.find((o) => o.id === op.ordenId);
    const codigo = codigoProblema(op.error) ?? "";
    const terminal = CODIGOS_TERMINALES.has(codigo);
    const detalles = detallesProblema(op.error, orden);
    return (
        <Aviso
            tono="error"
            icono={TriangleAlert}
            titulo={`${OPERACIONES[op.tipo]} · ${orden?.numero ?? `Orden ${op.ordenId}`}`}
            data-testid="rechazo"
        >
            <p>{textoProblema(op.error)}</p>
            {detalles.length > 0 ? (
                <ul className="mt-1 list-disc pl-4">
                    {detalles.map((d) => (
                        <li key={d}>{d}</li>
                    ))}
                </ul>
            ) : null}
            <div className="mt-3 flex flex-wrap gap-2">
                <Boton variante="secundario" onClick={() => void ejecutar(() => motor.descartar(op.id), "Se descartó")}>
                    <Trash2 aria-hidden />
                    Descartar
                </Boton>
                {!terminal ? (
                    <Boton variante="secundario" onClick={() => void ejecutar(() => motor.rehacer(op.id), "Se volverá a enviar")}>
                        <Undo2 aria-hidden />
                        Rehacer
                    </Boton>
                ) : (
                    <Boton variante="secundario" onClick={() => void ejecutar(() => motor.descartarOrden(op.ordenId), "Se descartó lo pendiente de la orden")}>
                        <Trash2 aria-hidden />
                        Descartar todo de esta orden
                    </Boton>
                )}
            </div>
        </Aviso>
    );
}

function Esperando({ op }: { op: OperacionLocal }) {
    const { estado } = useCampo();
    const orden = estado.ordenes.find((o) => o.id === op.ordenId);
    const faltan = faltantesDe(op.error).map((f) => describirCampo(`adjuntos.${f}`, "", orden));
    return (
        <Aviso tono="info" icono={Hourglass} titulo={`${OPERACIONES[op.tipo]} · ${orden?.numero ?? `Orden ${op.ordenId}`}`} data-testid="esperando-adjuntos">
            <p>Se reintenta solo cuando terminen de subir:</p>
            <ul className="mt-1 list-disc pl-4">
                {faltan.map((f) => (
                    <li key={f}>{f}</li>
                ))}
            </ul>
        </Aviso>
    );
}

function AdjuntoRechazado({ adjunto }: { adjunto: AdjuntoLocal }) {
    const { estado, motor, ejecutar } = useCampo();
    return (
        <Aviso tono="error" titulo={`${adjunto.tipo === "firma" ? "Firma" : "Foto"} · ${numeroOrden(estado.ordenes, adjunto.ordenId)}`} data-testid="adjunto-rechazado">
            <p>{textoProblema(adjunto.problema, adjunto.ultimoError ?? "El servidor rechazó el archivo.")}</p>
            <div className="mt-3 flex flex-wrap gap-2">
                <Boton variante="secundario" onClick={() => void ejecutar(() => motor.descartarAdjunto(adjunto.id), "Se descartó el archivo")}>
                    <Trash2 aria-hidden />
                    Descartar
                </Boton>
                <Boton variante="secundario" onClick={() => void ejecutar(() => motor.rehacerAdjunto(adjunto.id), "Se volverá a subir")}>
                    <Undo2 aria-hidden />
                    Volver a subir
                </Boton>
            </div>
        </Aviso>
    );
}
