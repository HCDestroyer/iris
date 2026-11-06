import { api, resolver } from "../api/cliente";
import { nuevoIdMensaje } from "./api";

/*
 * Adjunto del agente para la conversación (contratos/conversaciones.md §7.2.1 y §8): se registra
 * en el módulo de adjuntos con la entidad `conversacion` y se sube entero (archivos de hasta
 * 16 MB). Después se manda el mensaje con `adjuntoId`.
 */

/** Imágenes y documentos que WhatsApp acepta del panel. */
export const ACEPTA_ADJUNTO = "image/jpeg,image/png,image/webp,application/pdf";
export const MAX_ADJUNTO = 16 * 1024 * 1024;

export async function sha256Hex(datos: ArrayBuffer): Promise<string> {
    const resumen = await crypto.subtle.digest("SHA-256", datos);
    return [...new Uint8Array(resumen)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/** Motivo por el que un archivo no se puede adjuntar, o `null` si sirve. */
export function motivoArchivoInvalido(archivo: Pick<File, "type" | "size">): string | null {
    if (!ACEPTA_ADJUNTO.split(",").includes(archivo.type)) return "Solo fotos (JPG, PNG, WebP) o PDF.";
    if (archivo.size === 0) return "El archivo está vacío.";
    if (archivo.size > MAX_ADJUNTO) return "El archivo pasa de 16 MB.";
    return null;
}

/** Registra y sube el archivo; devuelve el id del adjunto ya completo. */
export async function subirAdjunto(archivo: File, conversacionId: number): Promise<string> {
    const datos = await archivo.arrayBuffer();
    const id = nuevoIdMensaje();
    const sha256 = await sha256Hex(datos);
    await resolver(
        api().POST("/api/v1/adjuntos", {
            body: {
                id,
                entidad: "conversacion",
                entidadId: String(conversacionId),
                tipo: archivo.type === "application/pdf" ? "documento" : "foto",
                mime: archivo.type,
                tamano: archivo.size,
                sha256,
            },
        }),
    );
    await resolver(
        api().PATCH("/api/v1/adjuntos/{id}/subida", {
            params: { path: { id }, header: { "Tus-Resumable": "1.0.0", "Upload-Offset": 0 } },
            body: datos as unknown as string,
            bodySerializer: (b: unknown) => b as BodyInit,
            headers: { "Content-Type": "application/offset+octet-stream" },
        }),
    );
    return id;
}
