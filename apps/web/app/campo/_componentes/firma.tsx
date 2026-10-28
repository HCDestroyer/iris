"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { OrdenVista } from "@iris/sync";
import { Boton, Campo, InsigniaEstado, LienzoFirma, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo, hayFirma, trazosAPng, trazosASvg, type Trazo } from "@iris/ui";
import { PenLine } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { esquemaFirma, type DatosFirma } from "@/lib/campo/esquemas";
import { useCampo } from "./contexto";
import { EstadoAdjunto } from "./fotos";

/** Firma del cliente: nombre, documento y trazo → SVG (a la API) y PNG (adjunto `firma`). */
export function Firma({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const { estado, motor, ejecutar } = useCampo();
    const [rehacer, setRehacer] = useState(false);
    const [trazos, setTrazos] = useState<{ trazos: Trazo[]; ancho: number; alto: number }>({ trazos: [], ancho: 0, alto: 0 });
    const [errorTrazo, setErrorTrazo] = useState<string | undefined>();
    const form = useForm<DatosFirma>({ resolver: zodResolver(esquemaFirma), defaultValues: { nombre: orden.contacto.nombre ?? "", documento: "" } });
    const firma = orden.firma;

    const guardar = form.handleSubmit(async (d) => {
        if (!hayFirma(trazos.trazos)) {
            setErrorTrazo("Pide al cliente que firme en el recuadro");
            return;
        }
        setErrorTrazo(undefined);
        const ok = await ejecutar(async () => {
            const svg = trazosASvg(trazos.trazos, trazos.ancho, trazos.alto);
            const png = new Uint8Array(await (await trazosAPng(trazos.trazos, trazos.ancho, trazos.alto)).arrayBuffer());
            const { adjunto } = await motor.agregarAdjunto({
                ordenId: orden.id,
                tipo: "firma",
                datos: png,
                mime: "image/png",
                obligatorio: orden.requiereFirma,
                tomadoEn: new Date().toISOString(),
            });
            await motor.registrar("firma.capturar", orden.id, {
                nombre: d.nombre,
                svg,
                adjuntoId: adjunto.id,
                ...(d.documento ? { documento: d.documento } : {}),
            });
        }, "Firma guardada");
        if (ok) {
            setRehacer(false);
        }
    });

    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Firma del cliente</TarjetaTitulo>
                <TarjetaDescripcion>{orden.requiereFirma ? "Obligatoria para cerrar esta orden." : "Opcional."}</TarjetaDescripcion>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {firma && !rehacer ? (
                    <div className="flex flex-wrap items-center gap-2">
                        <InsigniaEstado tono="success" icono={PenLine} texto="Firmada" />
                        <span className="text-sm">
                            {firma.nombre}
                            {firma.documento ? ` · ${firma.documento}` : ""}
                        </span>
                        {firma.adjuntoId ? <EstadoAdjunto adjunto={estado.adjuntos.find((a) => a.id === firma.adjuntoId)} /> : null}
                        {editable ? (
                            <Boton variante="fantasma" tamano="sm" className="ml-auto" onClick={() => setRehacer(true)}>
                                Volver a firmar
                            </Boton>
                        ) : null}
                    </div>
                ) : editable ? (
                    <form onSubmit={guardar} className="grid gap-3" noValidate aria-label="Firma del cliente">
                        <Campo etiqueta="Nombre de quien firma" autoComplete="off" error={form.formState.errors.nombre?.message} {...form.register("nombre")} />
                        <Campo etiqueta="Documento (DPI)" inputMode="numeric" autoComplete="off" error={form.formState.errors.documento?.message} {...form.register("documento")} />
                        <LienzoFirma alCambiar={(t, tam) => setTrazos({ trazos: t, ...tam })} />
                        {errorTrazo ? <p className="text-sm text-error">{errorTrazo}</p> : null}
                        <Boton type="submit" variante="secundario" tamano="lg" disabled={form.formState.isSubmitting}>
                            Guardar firma
                        </Boton>
                    </form>
                ) : (
                    <p className="text-sm text-muted-foreground">Sin firma.</p>
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}
