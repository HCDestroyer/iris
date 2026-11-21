import { api, resolver } from "../api/cliente";
import { errorDesdeRespuesta, errorSinConexion } from "../api/errores";
import { nuevoIdMensaje } from "./api";

/*
 * Adjunto del agente para la conversación (contratos/conversaciones.md §7.2.1 y §8): se registra
 * en el módulo de adjuntos con la entidad `conversacion` y se sube por partes (tus: cada parte un
 * `PATCH` con su `Upload-Offset`), así se ve el avance y un fallo se retoma donde quedó. Después
 * se manda el mensaje con `adjuntoId`: la API admite **un adjunto por mensaje**.
 *
 * Límites (los del servidor, no inventados): JPG, PNG, WebP y PDF, hasta 16 MB (§8). El módulo de
 * adjuntos no admite video ni audio de fábrica (`IRIS_ADJUNTOS_MIME`).
 */

/** Fotos que WhatsApp acepta del panel. */
export const ACEPTA_FOTO = "image/jpeg,image/png,image/webp";
/** Documentos. */
export const ACEPTA_DOCUMENTO = "application/pdf";
/** Imágenes y documentos que WhatsApp acepta del panel. */
export const ACEPTA_ADJUNTO = `${ACEPTA_FOTO},${ACEPTA_DOCUMENTO}`;
export const MAX_ADJUNTO = 16 * 1024 * 1024;
/** Cuántos archivos se preparan de una vez (cada uno sale en su mensaje). */
export const MAX_ARCHIVOS = 10;
/** Tamaño de cada parte de la subida. */
export const PARTE = 512 * 1024;

export async function sha256Hex(datos: ArrayBuffer): Promise<string> {
    const resumen = await crypto.subtle.digest("SHA-256", datos);
    return [...new Uint8Array(resumen)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/** Motivo por el que un archivo no se puede adjuntar, o `null` si sirve. */
export function motivoArchivoInvalido(archivo: Pick<File, "type" | "size">): string | null {
    if (!ACEPTA_ADJUNTO.split(",").includes(archivo.type)) return archivo.type.startsWith("video/") ? "Los videos no se pueden enviar desde Iris: solo fotos (JPG, PNG, WebP) o PDF." : "Solo fotos (JPG, PNG, WebP) o PDF.";
    if (archivo.size === 0) return "El archivo está vacío.";
    if (archivo.size > MAX_ADJUNTO) return "El archivo pasa de 16 MB.";
    return null;
}

/** Qué es, para pintarlo: solo los mapas de bits se muestran como imagen (un SVG o un HTML, nunca). */
export function claseDeArchivo(mime: string): "imagen" | "video" | "documento" {
    if (ACEPTA_FOTO.split(",").includes(mime)) return "imagen";
    return mime.startsWith("video/") ? "video" : "documento";
}

/** «PDF», «JPG»… para la línea de detalle. */
export function tipoLegible(archivo: Pick<File, "type" | "name">): string {
    const conocidos: Record<string, string> = { "image/jpeg": "JPG", "image/png": "PNG", "image/webp": "WebP", "application/pdf": "PDF", "image/svg+xml": "SVG" };
    const conocido = conocidos[archivo.type];
    if (conocido) return conocido;
    if (archivo.type.startsWith("video/")) return "Video";
    const extension = /\.([A-Za-z0-9]{1,8})$/.exec(archivo.name)?.[1];
    return extension ? extension.toUpperCase() : "Archivo";
}

/** Por dónde va una subida: con esto se retoma tras un fallo sin volver a empezar. */
export type AvanceSubida = { id: string; registrado: boolean; enviado: number };

export function nuevaSubida(): AvanceSubida {
    return { id: nuevoIdMensaje(), registrado: false, enviado: 0 };
}

type OpcionesSubida = {
    /** Se retoma desde aquí (y se va actualizando). */
    avance?: AvanceSubida;
    /** Fracción subida, de 0 a 1. */
    alAvanzar?: (fraccion: number) => void;
    /** Se pregunta antes de cada parte: `false` corta la subida (quien la pidió ya no está). */
    seguir?: () => boolean;
    /** Entidad del adjunto (por defecto la conversación; `plantilla_whatsapp` con la WABA como id). */
    entidad?: string;
};

/** La subida se cortó porque quien la pidió ya no la quiere (`seguir` devolvió `false`). */
export class SubidaCortada extends Error {}

/** `Upload-Offset` de una respuesta, si es un entero entre 0 y `tamano`. */
export function offsetValido(cabecera: string | null, tamano: number): number | null {
    if (cabecera === null || !/^\d{1,15}$/.test(cabecera.trim())) return null;
    const n = Number(cabecera.trim());
    return n <= tamano ? n : null;
}

/** Tope de veces que se le pregunta al servidor por dónde va (para no entrar en bucle). */
const MAX_REANUDACIONES = 3;

/**
 * Registra y sube el archivo por partes; devuelve el id del adjunto ya completo. Si falla, `avance`
 * queda con lo que el servidor ya recibió: llamarla otra vez con el mismo `avance` sigue desde ahí.
 *
 * 409 `offset_distinto` (una parte llegó y su respuesta no): se sigue desde el `Upload-Offset` que
 * dice el servidor (el de la respuesta o, si no viene, el de `HEAD`), validado entre 0 y el tamaño.
 * Solo si no se puede saber se registra un adjunto nuevo y se empieza de cero, una vez.
 */
export async function subirAdjunto(archivo: File, conversacionId: number | string, { avance = nuevaSubida(), alAvanzar, seguir, entidad = "conversacion" }: OpcionesSubida = {}): Promise<string> {
    const datos = await archivo.arrayBuffer();
    const registrar = async () => {
        const sha256 = await sha256Hex(datos);
        await resolver(
            api().POST("/api/v1/adjuntos", {
                body: {
                    id: avance.id,
                    entidad,
                    entidadId: String(conversacionId),
                    tipo: archivo.type === "application/pdf" ? "documento" : "foto",
                    mime: archivo.type,
                    tamano: archivo.size,
                    sha256,
                },
            }),
        );
        avance.registrado = true;
        avance.enviado = 0;
    };
    /** Por dónde va el servidor: la cabecera del 409 o, si no la trae, `HEAD`. */
    const offsetDelServidor = async (respuesta: Response): Promise<number | null> => {
        const directo = offsetValido(respuesta.headers.get("Upload-Offset"), datos.byteLength);
        if (directo !== null) return directo;
        try {
            const { response } = await api().HEAD("/api/v1/adjuntos/{id}/subida", { params: { path: { id: avance.id }, header: { "Tus-Resumable": "1.0.0" } } });
            return response.ok ? offsetValido(response.headers.get("Upload-Offset"), datos.byteLength) : null;
        } catch {
            return null;
        }
    };
    if (!avance.registrado) await registrar();
    alAvanzar?.(datos.byteLength ? avance.enviado / datos.byteLength : 0);
    let reanudaciones = 0;
    let reinicios = 0;
    while (avance.enviado < datos.byteLength) {
        if (seguir && !seguir()) throw new SubidaCortada("Subida cortada");
        const parte = datos.slice(avance.enviado, Math.min(avance.enviado + PARTE, datos.byteLength));
        let resultado: Awaited<ReturnType<typeof enviarParte>>;
        try {
            resultado = await enviarParte(avance.id, avance.enviado, parte);
        } catch (causa) {
            throw errorSinConexion(causa);
        }
        const { response, error } = resultado;
        if (response.status === 409) {
            const offset = reanudaciones < MAX_REANUDACIONES ? await offsetDelServidor(response) : null;
            if (offset !== null) {
                reanudaciones++;
                avance.enviado = offset;
                alAvanzar?.(datos.byteLength ? offset / datos.byteLength : 1);
                continue;
            }
            // No se sabe por dónde va: un adjunto nuevo, de cero, una sola vez.
            if (reinicios === 0) {
                reinicios++;
                avance.id = nuevoIdMensaje();
                avance.registrado = false;
                await registrar();
                alAvanzar?.(0);
                continue;
            }
        }
        if (!response.ok) throw errorDesdeRespuesta(response, error);
        avance.enviado += parte.byteLength;
        alAvanzar?.(avance.enviado / datos.byteLength);
    }
    return avance.id;
}

function enviarParte(id: string, offset: number, parte: ArrayBuffer) {
    return api().PATCH("/api/v1/adjuntos/{id}/subida", {
        params: { path: { id }, header: { "Tus-Resumable": "1.0.0", "Upload-Offset": offset } },
        // El OpenAPI declara el cuerpo como texto binario: van los bytes tal cual (como packages/sync).
        body: "",
        bodySerializer: () => parte,
        headers: { "Content-Type": "application/offset+octet-stream" },
    });
}
