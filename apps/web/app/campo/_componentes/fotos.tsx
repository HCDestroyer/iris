"use client";

import type { AdjuntoLocal, OrdenVista } from "@iris/sync";
import { Camara, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { CircleCheck, CloudUpload, ImageOff, TriangleAlert } from "lucide-react";
import { useCallback, useState } from "react";
import { leerExif } from "@/lib/campo/exif";
import { obtenerPosicion } from "@/lib/campo/gps";
import { comprimirFoto } from "@/lib/campo/imagen";
import { useCampo } from "./contexto";

export interface OpcionesFoto {
    ordenId: number;
    obligatorio?: boolean;
    item?: { itemId: number; codigo: string };
}

/**
 * Foto de la cámara → comprimida (≤ 1600 px, ~300 KB) → hora y GPS del EXIF (o la posición
 * actual) → guardada en la cola del teléfono; se sube sola en segundo plano.
 */
export function useTomarFoto() {
    const { motor, agregarMiniatura, notificar } = useCampo();
    const [procesando, setProcesando] = useState(false);
    const tomar = useCallback(
        async (archivo: File, opciones: OpcionesFoto): Promise<string | null> => {
            setProcesando(true);
            try {
                const original = new Uint8Array(await archivo.arrayBuffer());
                const exif = archivo.type === "image/jpeg" || /\.jpe?g$/i.test(archivo.name) ? leerExif(original) : {};
                const foto = await comprimirFoto(archivo);
                const posicion = exif.lat !== undefined ? null : await obtenerPosicion(4000);
                const lat = exif.lat ?? posicion?.lat;
                const lon = exif.lon ?? posicion?.lon;
                const { adjunto } = await motor.agregarAdjunto({
                    ordenId: opciones.ordenId,
                    tipo: "foto",
                    datos: foto.bytes,
                    mime: foto.mime,
                    tomadoEn: exif.tomadoEn ?? new Date().toISOString(),
                    ...(opciones.obligatorio ? { obligatorio: true } : {}),
                    ...(opciones.item ? { item: opciones.item } : {}),
                    ...(lat !== undefined && lon !== undefined ? { lat, lon } : {}),
                });
                agregarMiniatura(adjunto.id, new Blob([foto.bytes as Uint8Array<ArrayBuffer>], { type: foto.mime }));
                notificar({ tono: "success", texto: "Foto guardada en el teléfono. Se sube en segundo plano." });
                return adjunto.id;
            } catch (e) {
                notificar({ tono: "error", texto: e instanceof Error && e.message ? `No se pudo guardar la foto: ${e.message}` : "No se pudo guardar la foto." });
                return null;
            } finally {
                setProcesando(false);
            }
        },
        [motor, agregarMiniatura, notificar],
    );
    return { tomar, procesando };
}

/** Estado de subida de un adjunto: icono + texto + tono. */
export function EstadoAdjunto({ adjunto }: { adjunto: AdjuntoLocal | undefined }) {
    if (!adjunto || adjunto.estado === "subido") {
        return <InsigniaEstado tono="success" icono={CircleCheck} texto={adjunto ? "Subida" : "Registrada"} />;
    }
    if (adjunto.estado === "rechazado") {
        return <InsigniaEstado tono="error" icono={TriangleAlert} texto="Rechazada" />;
    }
    const pct = adjunto.tamano > 0 ? Math.floor((adjunto.offset / adjunto.tamano) * 100) : 0;
    return <InsigniaEstado tono="warning" icono={CloudUpload} texto={adjunto.estado === "por_crear" ? "Por subir" : `Subiendo ${pct} %`} />;
}

export function Miniatura({ id, alt }: { id: string; alt: string }) {
    const { miniaturas } = useCampo();
    const url = miniaturas.get(id);
    return url ? (
        // eslint-disable-next-line @next/next/no-img-element -- URL de objeto local (blob:), sin optimizar.
        <img src={url} alt={alt} className="size-16 shrink-0 rounded-md border border-border object-cover" />
    ) : (
        <span className="grid size-16 shrink-0 place-content-center rounded-md border border-dashed border-border text-muted-foreground" title={alt}>
            <ImageOff aria-hidden className="size-5" />
        </span>
    );
}

/** Fotos sueltas de la orden (evidencia), también en una orden cancelada. */
export function FotosOrden({ orden }: { orden: OrdenVista }) {
    const { estado } = useCampo();
    const { tomar, procesando } = useTomarFoto();
    const delItem = new Set((orden.checklist ?? []).map((i) => String(i.valor)));
    const fotos = (orden.adjuntos ?? []).filter((a) => a.tipo === "foto" && !delItem.has(a.id) && a.itemId === null);
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Fotos</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {fotos.length > 0 ? (
                    <ul className="grid gap-2">
                        {fotos.map((f, i) => (
                            <li key={f.id} className="flex items-center gap-3">
                                <Miniatura id={f.id} alt={`Foto ${i + 1}`} />
                                <span className="flex-1 text-sm">Foto {i + 1}</span>
                                <EstadoAdjunto adjunto={estado.adjuntos.find((a) => a.id === f.id)} />
                            </li>
                        ))}
                    </ul>
                ) : (
                    <p className="text-sm text-muted-foreground">Sin fotos todavía.</p>
                )}
                <Camara
                    disabled={procesando}
                    aria-label="Agregar foto de la orden"
                    alElegir={(archivo) => void tomar(archivo, { ordenId: orden.id })}
                    className="w-full"
                >
                    {procesando ? "Procesando foto…" : "Agregar foto"}
                </Camara>
            </TarjetaContenido>
        </Tarjeta>
    );
}
