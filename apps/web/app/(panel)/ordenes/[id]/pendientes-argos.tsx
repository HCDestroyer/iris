"use client";

import { avisar, Aviso, Boton, Esqueleto, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { CircleCheck, RotateCw, TriangleAlert } from "lucide-react";
import { useRef, useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { InsigniaPendiente } from "@/componentes/ordenes/insignias";
import { PieLista } from "@/componentes/pie-lista";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearFecha } from "@/lib/fechas";
import { reintentable, resumirError, textoOperacion, type Pendiente } from "@/lib/ordenes/presentacion";
import { usePaginado, type Paginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

/**
 * Pendientes hacia Argos de la orden (`GET /integraciones/pendientes?entidad=orden&entidadId=`):
 * estado, último error resumido, conflicto y «Reintentar» con `integraciones.reintentar`.
 */
export function PendientesArgos({ ordenId }: { ordenId: number }) {
    const paginado = usePaginado<Pendiente>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/integraciones/pendientes", {
                params: { query: { entidad: "orden", entidadId: String(ordenId), limit: 50, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera, items } = paginado;
    const abiertos = items.filter((p) => p.estado !== "hecho").length;

    return (
        <Tarjeta data-testid="pendientes-argos">
            <TarjetaEncabezado>
                <TarjetaTitulo>Pendientes en Argos</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {primera.estado === "cargando" ? (
                    <Esqueleto className="h-16" />
                ) : primera.estado === "error" ? (
                    <ErrorLectura error={primera.error} que="los pendientes en Argos" reintentar={primera.reintentar} />
                ) : items.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Esta orden no tiene acciones hacia Argos.</p>
                ) : (
                    <>
                        {abiertos === 0 && !paginado.siguiente ? (
                            <p className="flex items-center gap-1.5 text-sm text-success">
                                <CircleCheck aria-hidden className="size-4" />
                                Todo quedó aplicado en Argos.
                            </p>
                        ) : null}
                        <ul className="grid gap-3" aria-label="Pendientes en Argos">
                            {items.map((p) => (
                                <FilaPendiente key={p.id} pendiente={p} paginado={paginado} />
                            ))}
                        </ul>
                    </>
                )}
                <PieLista paginado={paginado} que="pendientes" />
            </TarjetaContenido>
        </Tarjeta>
    );
}

function FilaPendiente({ pendiente: p, paginado }: { pendiente: Pendiente; paginado: Paginado<Pendiente> }) {
    const { puede } = useSesion();
    const clave = useRef(nuevaClaveIdempotencia());
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const error409 = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";
    const resumen = resumirError(p.ultimoError);
    const puedeReintentar = puede(["integraciones.reintentar"]);

    async function reintentar() {
        setEnviando(true);
        setError(null);
        try {
            await resolver(
                api().POST("/api/v1/integraciones/pendientes/{id}/reintentar", {
                    params: { path: { id: p.id }, header: { "Idempotency-Key": clave.current } },
                    body: { version: p.version },
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            avisar({ tono: "info", titulo: "Reintento enviado a Argos" });
            await paginado.refrescar();
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    async function recargar() {
        setRecargando(true);
        try {
            await paginado.refrescar();
            setError(null);
        } finally {
            setRecargando(false);
        }
    }

    return (
        <li className="grid gap-2 rounded-lg border p-3" data-pendiente={p.operacion} data-estado={p.estado}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="font-medium">{textoOperacion(p.operacion)}</span>
                <InsigniaPendiente estado={p.estado} />
            </div>
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                <dt className="text-muted-foreground">Intentos</dt>
                <dd className="tabular-nums">{p.intentos}</dd>
                {p.estado === "pendiente" && p.proximoIntento ? (
                    <>
                        <dt className="text-muted-foreground">Próximo intento</dt>
                        <dd className="tabular-nums">{formatearFecha(p.proximoIntento)}</dd>
                    </>
                ) : null}
                {p.hechoEn ? (
                    <>
                        <dt className="text-muted-foreground">Aplicado</dt>
                        <dd className="tabular-nums">{formatearFecha(p.hechoEn)}</dd>
                    </>
                ) : null}
                {resumen && p.estado !== "hecho" ? (
                    <>
                        <dt className="text-muted-foreground">Último error</dt>
                        <dd className="min-w-0 break-words" title={p.ultimoError ?? undefined} data-testid="ultimo-error">
                            {p.ultimoStatus ? <span className="font-mono text-xs">HTTP {p.ultimoStatus} · </span> : null}
                            {resumen}
                        </dd>
                    </>
                ) : null}
            </dl>
            {p.conflicto ? (
                <Aviso tono="warning" titulo="Conflicto: corrígelo a mano en Argos" icono={TriangleAlert}>
                    La orden se volvió a cerrar con otros datos para esta acción y no se pisó lo que ya estaba en Argos
                    {p.conflictoEn ? ` (${formatearFecha(p.conflictoEn)})` : ""}.
                </Aviso>
            ) : null}
            {p.datosIncompletos ? (
                <p className="text-sm text-muted-foreground">La orden no traía todos los datos: no se puede reintentar; corrígelo en Argos.</p>
            ) : null}
            {p.estado === "bloqueado" ? <p className="text-sm text-muted-foreground">Se reintenta solo cuando la acción de la que depende quede hecha.</p> : null}
            {reintentable(p) ? (
                puedeReintentar ? (
                    <div className="flex flex-wrap items-center gap-2">
                        <Boton variante="secundario" tamano="sm" onClick={reintentar} disabled={enviando || error409}>
                            <RotateCw />
                            {enviando ? "Reintentando…" : "Reintentar"}
                        </Boton>
                    </div>
                ) : (
                    <p className="text-xs text-muted-foreground">Reintentar requiere el permiso de reintentar integraciones (supervisor).</p>
                )
            ) : null}
            {error409 ? (
                <Aviso
                    tono="warning"
                    titulo="El pendiente cambió mientras lo veías"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={recargar} disabled={recargando}>
                            <RotateCw />
                            {recargando ? "Recargando…" : "Recargar"}
                        </Boton>
                    }
                >
                    Recarga para ver su estado actual antes de reintentar.
                </Aviso>
            ) : error ? (
                <Aviso tono="error" titulo="No se pudo reintentar">
                    {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                </Aviso>
            ) : null}
        </li>
    );
}
