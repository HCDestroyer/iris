"use client";

import { Aviso, Boton, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ClipboardList, OctagonAlert, RotateCw } from "lucide-react";
import Link from "next/link";
import { useEnvio } from "@/componentes/comercial/comun";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { ESTADOS_ALTA, OPERACIONES_ALTA, pendienteReintentable, presentacionPendiente, type PendienteArgos, type ProspectoDetalle } from "@/lib/comercial/presentacion";
import { resumirError } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";

type Props = { detalle: ProspectoDetalle; recargar: () => Promise<void> };

/**
 * Después de ganar: el alta en Argos (cliente → servicio → orden de instalación) o local (orden
 * ya creada). Mientras el alta no esté completa, que no haya orden no es un error.
 */
export function TarjetaAlta({ detalle, recargar }: Props) {
    const alta = detalle.alta;
    if (!alta) return null;
    const p = alta.modo === "local" ? { tono: "success" as const, icono: ClipboardList, texto: "Alta local" } : ESTADOS_ALTA[alta.estado];
    const pendientes = detalle.pendientes ?? [];
    return (
        <Tarjeta data-testid="alta">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Alta del cliente</TarjetaTitulo>
                <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-alta={alta.estado} />
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {alta.error ? (
                    <Aviso tono="error" icono={OctagonAlert} titulo="El alta en Argos se detuvo y necesita revisión" data-testid="alta-error">
                        {alta.error} No se reintenta sola: revísalo en Argos con el equipo de integración.
                    </Aviso>
                ) : null}
                {alta.modo === "local" ? (
                    <p className="text-sm text-muted-foreground">Iris trabaja sin conexión con Argos: se creó la orden de instalación con los datos del prospecto.</p>
                ) : (
                    <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                        <dt className="text-muted-foreground">Cliente en Argos</dt>
                        <dd className="tabular-nums">{alta.clienteArgosId ?? "pendiente"}</dd>
                        <dt className="text-muted-foreground">Servicio en Argos</dt>
                        <dd className="tabular-nums">{alta.servicioArgosId ?? "pendiente"}</dd>
                        {alta.routerId !== null ? (
                            <>
                                <dt className="text-muted-foreground">Router</dt>
                                <dd className="tabular-nums">{alta.routerId}</dd>
                            </>
                        ) : null}
                    </dl>
                )}
                {alta.ordenId !== null ? (
                    <Boton asChild variante="secundario" tamano="sm" className="justify-self-start">
                        <Link href={`/ordenes/${alta.ordenId}`}>
                            <ClipboardList />
                            Orden de instalación {alta.ordenNumero ?? `#${alta.ordenId}`}
                        </Link>
                    </Boton>
                ) : alta.estado !== "completa" && !alta.error ? (
                    <p className="text-sm text-muted-foreground">La orden de instalación se crea cuando Argos confirme el cliente y el servicio.</p>
                ) : null}
                {pendientes.length > 0 ? (
                    <ul className="grid gap-2" aria-label="Pendientes del alta en Argos">
                        {pendientes.map((x) => (
                            <FilaPendiente key={x.id} pendiente={x} recargar={recargar} />
                        ))}
                    </ul>
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function FilaPendiente({ pendiente: p, recargar }: { pendiente: PendienteArgos; recargar: () => Promise<void> }) {
    const { puede } = useSesion();
    const envio = useEnvio();
    const pr = presentacionPendiente(p.estado);
    const error = resumirError(p.ultimoError);
    async function reintentar() {
        const r = await envio.enviar((clave) =>
            resolver(api().POST("/api/v1/integraciones/pendientes/{id}/reintentar", { params: { path: { id: p.id }, header: { "Idempotency-Key": clave } }, body: { version: p.version } })),
        );
        if (r) await recargar();
    }
    return (
        <li className="grid gap-2 rounded-lg border p-3 text-sm" data-pendiente={p.operacion} data-estado={p.estado}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="font-medium">{OPERACIONES_ALTA[p.operacion] ?? p.operacion.replaceAll("_", " ")}</span>
                <InsigniaEstado tono={pr.tono} icono={pr.icono} texto={pr.texto} />
            </div>
            <p className="text-muted-foreground">
                Intentos: <span className="tabular-nums">{p.intentos}</span>
                {error && p.estado !== "hecho" ? <> · {error}</> : null}
            </p>
            {pendienteReintentable(p.estado) ? (
                puede(["integraciones.reintentar"]) ? (
                    <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={reintentar} disabled={envio.enviando}>
                        <RotateCw />
                        {envio.enviando ? "Reintentando…" : "Reintentar"}
                    </Boton>
                ) : (
                    <p className="text-xs text-muted-foreground">Reintentar requiere el permiso de reintentar integraciones (supervisor).</p>
                )
            ) : null}
            {envio.error ? (
                <Aviso
                    tono={envio.error instanceof ErrorApi && envio.error.estado === 409 ? "warning" : "error"}
                    titulo={envio.error instanceof ErrorApi && envio.error.estado === 409 ? "El pendiente cambió mientras lo veías" : "No se pudo reintentar"}
                    accion={
                        <Boton
                            variante="secundario"
                            tamano="sm"
                            onClick={async () => {
                                await recargar();
                                envio.setError(null);
                            }}
                        >
                            <RotateCw />
                            Recargar
                        </Boton>
                    }
                >
                    {mensajeDeError(envio.error).detalle}
                </Aviso>
            ) : null}
        </li>
    );
}
