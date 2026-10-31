"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    avisar,
    Aviso,
    Boton,
    BotonEnviar,
    CampoArea,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Esqueleto,
    GrupoOpciones,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
    useCambiosSinGuardar,
} from "@iris/ui";
import { CircleCheck, HandCoins, RotateCw, TriangleAlert, Wrench } from "lucide-react";
import { useRef, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { ErrorLectura } from "@/componentes/error-lectura";
import { InsigniaPendiente } from "@/componentes/ordenes/insignias";
import { PieLista } from "@/componentes/pie-lista";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearFecha } from "@/lib/fechas";
import { cuerpoResolverAMano, esquemaResolverAMano, resolubleAMano, textoCorregible, type DatosResolverAMano, type DecisionCargo } from "@/lib/ordenes/pendientes";
import { reintentable, resumirError, textoOperacion, type Pendiente } from "@/lib/ordenes/presentacion";
import { usePaginado, type Paginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

/**
 * Pendientes hacia Argos de la orden (`GET /integraciones/pendientes?entidad=orden&entidadId=`):
 * estado, último error resumido (con su código), conflicto, los errores que se corrigen desde
 * Iris, los cargos que Argos emitió, «Reintentar» con `integraciones.reintentar` y «Resolver a
 * mano» con `integraciones.resolver_pendiente`. `alCambiar` avisa a la ficha (su marca).
 */
export function PendientesArgos({ ordenId, alCambiar }: { ordenId: number; alCambiar?: () => void }) {
    const paginado = usePaginado<Pendiente>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/integraciones/pendientes", {
                params: { query: { entidad: "orden", entidadId: String(ordenId), limit: 50, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera, items } = paginado;
    const abiertos = items.filter((p) => p.estado !== "hecho" && p.estado !== "resuelto_manual").length;

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
                                <FilaPendiente key={p.id} pendiente={p} paginado={paginado} {...(alCambiar ? { alCambiar } : {})} />
                            ))}
                        </ul>
                    </>
                )}
                <PieLista paginado={paginado} que="pendientes" />
            </TarjetaContenido>
        </Tarjeta>
    );
}

function FilaPendiente({ pendiente: p, paginado, alCambiar }: { pendiente: Pendiente; paginado: Paginado<Pendiente>; alCambiar?: () => void }) {
    const { puede } = useSesion();
    const clave = useRef(nuevaClaveIdempotencia());
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const error409 = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";
    const resumen = resumirError(p.ultimoError);
    const puedeReintentar = puede(["integraciones.reintentar"]);
    const puedeResolver = puede(["integraciones.resolver_pendiente"]) && resolubleAMano(p);
    const [resolviendo, setResolviendo] = useState(false);

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
            alCambiar?.();
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
                {resumen && p.estado !== "hecho" && p.estado !== "resuelto_manual" ? (
                    <>
                        <dt className="text-muted-foreground">Último error</dt>
                        <dd className="min-w-0 break-words" title={p.ultimoError ?? undefined} data-testid="ultimo-error">
                            {p.ultimoStatus ? <span className="font-mono text-xs">HTTP {p.ultimoStatus} · </span> : null}
                            {p.codigoError ? <span className="font-mono text-xs">{p.codigoError} · </span> : null}
                            {resumen}
                        </dd>
                    </>
                ) : null}
                {p.estado === "resuelto_manual" ? (
                    <>
                        <dt className="text-muted-foreground">Resuelto a mano</dt>
                        <dd className="min-w-0 break-words" data-testid="resuelto-manual">
                            {p.resueltoMotivo ?? "Sin motivo"}
                            <span className="block text-xs text-muted-foreground">
                                {p.resueltoPor !== undefined ? `Usuario ${p.resueltoPor}` : "Alguien"}
                                {p.resueltoEn ? ` · ${formatearFecha(p.resueltoEn)}` : ""}
                            </span>
                        </dd>
                    </>
                ) : null}
            </dl>
            {(p.cargos ?? []).length > 0 ? (
                <div className="grid gap-1 text-sm" data-testid="cargos-argos">
                    <span className="flex items-center gap-1.5 font-medium">
                        <HandCoins aria-hidden className="size-4 text-muted-foreground" />
                        Cargos que Argos emitió{p.facturaId !== undefined ? ` (factura ${p.facturaId})` : ""}
                    </span>
                    <ul className="grid gap-0.5 pl-6" aria-label="Cargos de Argos">
                        {(p.cargos ?? []).map((c) => (
                            <li key={c.id} className="flex justify-between gap-3 tabular-nums">
                                <span>Producto {c.productoId}</span>
                                <span>Q {c.monto}</span>
                            </li>
                        ))}
                    </ul>
                </div>
            ) : null}
            {p.corregible && p.estado !== "hecho" && p.estado !== "resuelto_manual" ? (
                <Aviso tono="warning" titulo="Se corrige desde Iris" icono={Wrench} data-testid="corregible">
                    {textoCorregible(p.codigoError)}
                </Aviso>
            ) : null}
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
            {(reintentable(p) && puedeReintentar) || puedeResolver ? (
                <div className="flex flex-wrap items-center gap-2">
                    {reintentable(p) && puedeReintentar ? (
                        <Boton variante="secundario" tamano="sm" onClick={reintentar} disabled={enviando || error409}>
                            <RotateCw />
                            {enviando ? "Reintentando…" : "Reintentar"}
                        </Boton>
                    ) : null}
                    {puedeResolver ? (
                        <Boton variante="contorno" tamano="sm" onClick={() => setResolviendo(true)} disabled={enviando}>
                            <Wrench />
                            Resolver a mano
                        </Boton>
                    ) : null}
                </div>
            ) : null}
            {reintentable(p) && !puedeReintentar ? (
                <p className="text-xs text-muted-foreground">Reintentar requiere el permiso de reintentar integraciones (supervisor).</p>
            ) : null}
            <DialogoProtegido open={resolviendo} onOpenChange={setResolviendo}>
                {resolviendo ? (
                    <ResolverAMano
                        pendiente={p}
                        alHecho={async () => {
                            setResolviendo(false);
                            await paginado.refrescar();
                            alCambiar?.();
                        }}
                        recargar={recargar}
                    />
                ) : null}
            </DialogoProtegido>
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

/**
 * «Resolver a mano» (`POST /integraciones/pendientes/{id}/resolver-manual`): para lo que Argos
 * rechazó y ya se corrigió directo en Argos. Si es un consumo con un cargo esperándolo, la API
 * responde 422 `decision_cargo_requerida` y se pregunta «¿Argos ya cobró el cargo?».
 */
function ResolverAMano({ pendiente: p, alHecho, recargar }: { pendiente: Pendiente; alHecho: () => Promise<void>; recargar: () => Promise<void> }) {
    const clave = useRef(nuevaClaveIdempotencia());
    const [error, setError] = useState<unknown>(null);
    const [preguntarCargo, setPreguntarCargo] = useState(false);
    const form = useForm<DatosResolverAMano>({ resolver: zodResolver(esquemaResolverAMano), defaultValues: { motivo: "" } });
    const { errors, isSubmitting, isDirty } = form.formState;
    useCambiosSinGuardar(isDirty, isSubmitting);
    const cargo = useWatch({ control: form.control, name: "cargo" });
    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";

    async function enviar(d: DatosResolverAMano) {
        setError(null);
        try {
            await resolver(
                api().POST("/api/v1/integraciones/pendientes/{id}/resolver-manual", {
                    params: { path: { id: p.id }, header: { "Idempotency-Key": clave.current } },
                    body: cuerpoResolverAMano(d, p.version),
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            form.reset(d);
            avisar({ titulo: `${textoOperacion(p.operacion)}: resuelto a mano` });
            await alHecho();
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            if (e instanceof ErrorApi && e.estado === 422 && e.codigo === "decision_cargo_requerida") {
                setPreguntarCargo(true);
                return;
            }
            setError(e);
        }
    }

    return (
        <DialogoContenido titulo="Resolver a mano" descripcion={`${textoOperacion(p.operacion)} · ya se corrigió directo en Argos`}>
            <form noValidate className="grid gap-4" onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                <CampoArea
                    etiqueta="Qué se hizo en Argos"
                    rows={3}
                    maxLength={1000}
                    ayuda="Queda en el historial. Si rehiciste el consumo en Argos, hazlo con el origen de esta orden."
                    error={errors.motivo?.message}
                    {...form.register("motivo")}
                />
                {preguntarCargo ? (
                    <GrupoOpciones<DecisionCargo>
                        leyenda="¿Argos ya cobró el cargo?"
                        nombre={`cargo-${p.id}`}
                        ayuda="Un cargo de un producto de este consumo lo está esperando."
                        valor={cargo}
                        alCambiar={(v) => form.setValue("cargo", v, { shouldDirty: true })}
                        opciones={[
                            { valor: "resolver_tambien", etiqueta: "Sí", detalle: "El cargo también queda resuelto a mano y no se manda." },
                            { valor: "liberar", etiqueta: "No", detalle: "El cargo sale hacia Argos." },
                        ]}
                    />
                ) : null}
                {conflicto ? (
                    <Aviso
                        tono="warning"
                        titulo="El pendiente cambió mientras lo veías"
                        accion={
                            <Boton type="button" variante="secundario" tamano="sm" onClick={() => void recargar().then(() => setError(null))}>
                                <RotateCw />
                                Recargar
                            </Boton>
                        }
                    >
                        Recarga para ver su estado actual.
                    </Aviso>
                ) : error ? (
                    <Aviso tono="error" titulo="No se pudo resolver a mano">
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <BotonEnviar enviando={isSubmitting} textoEnviando="Guardando…" faltan={preguntarCargo && !cargo ? ["si Argos ya cobró el cargo"] : []} disabled={conflicto}>
                        Resolver a mano
                    </BotonEnviar>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}
