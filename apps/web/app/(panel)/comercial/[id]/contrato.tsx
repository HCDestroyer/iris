"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Boton,
    Campo,
    Dialogo,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    hayFirma,
    LienzoFirma,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
    trazosASvg,
    type Trazo,
} from "@iris/ui";
import { FilePen, PenLine } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { AvisoAccion, DescargarPdf, InsigniaContrato, useEnvio } from "@/componentes/comercial/comun";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { cuerpoFirma, esquemaFirma, type DatosFirma } from "@/lib/comercial/esquemas";
import type { Contrato, ProspectoDetalle } from "@/lib/comercial/presentacion";
import { formatearFecha } from "@/lib/fechas";
import { useSesion } from "@/lib/sesion";

type Props = { detalle: ProspectoDetalle; recargar: () => Promise<void> };

/**
 * Contrato: se genera con la cotización aceptada (PDF borrador para leer) y lo firma el cliente en
 * el dispositivo del vendedor (lienzo → SVG de trazos, como la firma de campo).
 */
export function TarjetaContrato({ detalle, recargar }: Props) {
    const { puede } = useSesion();
    const c = detalle.contrato;
    const puedeGenerar = detalle.acciones.contratar && puede(["contratos.crear"]);
    const puedeFirmar = detalle.acciones.firmar && puede(["contratos.firmar"]);
    const envio = useEnvio();
    const [firmando, setFirmando] = useState(false);

    async function generar() {
        const r = await envio.enviar((clave) =>
            resolver(
                api().POST("/api/v1/prospectos/{id}/contrato", {
                    params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                    body: { version: detalle.version },
                }),
            ),
        );
        if (r) await recargar();
    }

    async function regenerarPdf(contrato: Contrato) {
        await resolver(api().POST("/api/v1/contratos/{id}/pdf", { params: { path: { id: contrato.id } } }));
        await recargar();
    }

    return (
        <Tarjeta data-testid="contrato">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Contrato</TarjetaTitulo>
                {c ? <InsigniaContrato contrato={c} /> : null}
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {!c ? (
                    <p className="text-sm text-muted-foreground">Se genera cuando el cliente acepta la cotización.</p>
                ) : (
                    <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                        <dt className="text-muted-foreground">Número</dt>
                        <dd className="font-mono tabular-nums">{c.numero}</dd>
                        <dt className="text-muted-foreground">Generado</dt>
                        <dd className="tabular-nums">{formatearFecha(c.creadoEn)}</dd>
                        {c.estado === "firmado" ? (
                            <>
                                <dt className="text-muted-foreground">Firmó</dt>
                                <dd>
                                    {c.firmanteNombre}
                                    {c.firmanteDocumento ? <span className="text-muted-foreground"> · {c.firmanteDocumento}</span> : null}
                                </dd>
                                <dt className="text-muted-foreground">Fecha de firma</dt>
                                <dd className="tabular-nums">{formatearFecha(c.firmadoEn)}</dd>
                                {c.pdfFirmado?.sha256 ? (
                                    <>
                                        <dt className="text-muted-foreground">SHA-256</dt>
                                        <dd className="font-mono text-xs break-all" title="Huella del PDF firmado (queda en la auditoría)">
                                            {c.pdfFirmado.sha256}
                                        </dd>
                                    </>
                                ) : null}
                            </>
                        ) : null}
                        {c.motivoAnulacion ? (
                            <>
                                <dt className="text-muted-foreground">Anulado</dt>
                                <dd>{c.motivoAnulacion}</dd>
                            </>
                        ) : null}
                    </dl>
                )}
                {c && c.estado === "firmado" ? (
                    <DescargarPdf documento={c.pdfFirmado} nombre={`${c.numero}-firmado`} etiqueta="Descargar contrato firmado" />
                ) : c ? (
                    <DescargarPdf
                        documento={c.pdf}
                        nombre={`${c.numero}-borrador`}
                        etiqueta="Descargar borrador"
                        {...(puede(["contratos.crear"]) ? { regenerar: () => regenerarPdf(c) } : {})}
                    />
                ) : null}
                <div className="flex flex-wrap gap-2">
                    {puedeGenerar ? (
                        <Boton tamano="sm" onClick={generar} disabled={envio.enviando || envio.conflicto}>
                            <FilePen />
                            {envio.enviando ? "Generando…" : "Generar contrato"}
                        </Boton>
                    ) : null}
                    {c && puedeFirmar ? (
                        <Boton tamano="sm" onClick={() => setFirmando(true)}>
                            <PenLine />
                            Firmar con el cliente
                        </Boton>
                    ) : null}
                </div>
                <AvisoAccion envio={envio} que="generar el contrato" recargar={recargar} />
            </TarjetaContenido>
            <Dialogo open={firmando} onOpenChange={setFirmando}>
                {firmando && c ? (
                    <Firmar
                        contrato={c}
                        nombreSugerido={detalle.nombre}
                        documentoSugerido={detalle.documento ?? ""}
                        recargar={recargar}
                        alHecho={async () => {
                            setFirmando(false);
                            await recargar();
                        }}
                    />
                ) : null}
            </Dialogo>
        </Tarjeta>
    );
}

type Trazos = { trazos: Trazo[]; ancho: number; alto: number };

function Firmar({
    contrato,
    nombreSugerido,
    documentoSugerido,
    recargar,
    alHecho,
}: {
    contrato: Contrato;
    nombreSugerido: string;
    documentoSugerido: string;
    recargar: () => Promise<void>;
    alHecho: () => Promise<void>;
}) {
    const envio = useEnvio();
    const [trazos, setTrazos] = useState<Trazos>({ trazos: [], ancho: 0, alto: 0 });
    const [faltaFirma, setFaltaFirma] = useState(false);
    const formulario = useForm<DatosFirma>({ resolver: zodResolver(esquemaFirma), defaultValues: { firmanteNombre: nombreSugerido, firmanteDocumento: documentoSugerido } });
    const { errors } = formulario.formState;

    async function enviar(d: DatosFirma) {
        if (!hayFirma(trazos.trazos)) {
            setFaltaFirma(true);
            return;
        }
        const svg = trazosASvg(trazos.trazos, trazos.ancho, trazos.alto);
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/contratos/{id}/firmar", {
                        params: { path: { id: contrato.id }, header: { "Idempotency-Key": clave } },
                        body: cuerpoFirma(d, contrato.version, svg),
                    }),
                ),
            { nombres: ["firmanteNombre", "firmanteDocumento"], setError: formulario.setError },
        );
        if (r) await alHecho();
    }

    const errorSvg = envio.error instanceof ErrorApi ? envio.error.errorDeCampo("svg") : undefined;

    return (
        <DialogoContenido titulo={`Firmar ${contrato.numero}`} descripcion="El cliente firma en esta pantalla. La firma queda dibujada en el PDF del contrato.">
            <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4" aria-label="Firma del contrato">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Nombre de quien firma" autoComplete="off" error={errors.firmanteNombre?.message} {...formulario.register("firmanteNombre")} />
                    <Campo etiqueta="DPI o NIT (opcional)" autoComplete="off" error={errors.firmanteDocumento?.message} {...formulario.register("firmanteDocumento")} />
                </div>
                <div className="grid gap-1.5">
                    <span className="text-sm font-medium">Firma</span>
                    <LienzoFirma
                        etiqueta="Firma del cliente"
                        alCambiar={(t, tam) => {
                            setTrazos({ trazos: t, ...tam });
                            if (hayFirma(t)) setFaltaFirma(false);
                        }}
                    />
                    {faltaFirma || errorSvg ? (
                        <p className="text-sm text-error" role="alert">
                            {errorSvg ?? "Falta la firma del cliente"}
                        </p>
                    ) : null}
                </div>
                <AvisoAccion envio={envio} que="firmar el contrato" recargar={recargar} />
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={envio.enviando}>
                            Volver
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={envio.enviando || envio.conflicto}>
                        <PenLine />
                        {envio.enviando ? "Firmando…" : "Guardar firma"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}
