import type { Schema } from "@iris/contratos";

export type Adjunto = Schema<"Adjunto">;

const PATRON_UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Imágenes que la API entrega `inline` y el panel muestra (el resto se descargaría: no se abre aquí). */
export const MIME_IMAGEN: ReadonlySet<string> = new Set(["image/jpeg", "image/png", "image/webp", "image/gif"]);

export function esIdAdjunto(id: string | null | undefined): id is string {
    return typeof id === "string" && PATRON_UUID.test(id);
}

/**
 * Ruta de descarga autenticada de un adjunto (`GET /api/v1/adjuntos/{id}`, mismo origen por el
 * proxy). Se arma siempre desde el id validado: nunca se sigue una URL que venga en los datos.
 */
export function rutaDescargaAdjunto(id: string): string | undefined {
    return esIdAdjunto(id) ? `/api/v1/adjuntos/${id.toLowerCase()}` : undefined;
}

/** `urlDescarga` del adjunto solo si es exactamente la ruta esperada; si no, `undefined`. */
export function urlDescargaConfiable(a: Pick<Adjunto, "id" | "urlDescarga">): string | undefined {
    const esperada = rutaDescargaAdjunto(a.id);
    if (!esperada) return undefined;
    return a.urlDescarga === esperada || a.urlDescarga.toLowerCase() === esperada ? esperada : undefined;
}

/** ¿Se puede pedir y mostrar como imagen? (completo y de un tipo que se muestra en línea). */
export function esImagenMostrable(a: Pick<Adjunto, "id" | "estado" | "mime">): boolean {
    return a.estado === "completo" && MIME_IMAGEN.has(a.mime.toLowerCase()) && esIdAdjunto(a.id);
}

/** Tipo del contenido descargado (sin parámetros) si es una imagen que se puede mostrar. */
export function tipoImagen(contentType: string | null | undefined): string | undefined {
    const tipo = contentType?.split(";")[0]?.trim().toLowerCase();
    return tipo && MIME_IMAGEN.has(tipo) ? tipo : undefined;
}

/** Enlace a OpenStreetMap para unas coordenadas (el mapa propio con MapLibre llega en la fase 3). */
export function enlaceMapa(lat: number, lon: number): string | undefined {
    if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) return undefined;
    const la = lat.toFixed(6);
    const lo = lon.toFixed(6);
    return `https://www.openstreetmap.org/?mlat=${la}&mlon=${lo}#map=18/${la}/${lo}`;
}

export function textoCoordenadas(lat: number, lon: number): string {
    return `${lat.toFixed(6)}, ${lon.toFixed(6)}`;
}
