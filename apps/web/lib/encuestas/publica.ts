import { z } from "zod";
import { MAX_COMENTARIO } from "./presentacion";

/*
 * Página pública `/encuesta#<token>`: el token se lee solo del fragmento (nunca de la query ni de
 * la ruta), se borra de la URL al leerlo y vive en memoria; nunca va a analytics ni a la consola.
 */

/** Largo que admite la API para el token (32 bytes en base64url = 43; margen por si cambia). */
export const TOKEN_MIN = 20;
export const TOKEN_MAX = 200;

export function tokenDesdeFragmento(hash: string): { token: string } | { invalido: true } | null {
    let valor = hash.startsWith("#") ? hash.slice(1) : hash;
    try {
        valor = decodeURIComponent(valor);
    } catch {
        return { invalido: true };
    }
    valor = valor.trim();
    if (valor === "") return null;
    if (valor.length < TOKEN_MIN || valor.length > TOKEN_MAX || !/^[A-Za-z0-9_-]+$/.test(valor)) {
        return { invalido: true };
    }
    return { token: valor };
}

/** Formulario de la encuesta: espejo del validador de la API (1–5 y comentario ≤ 1000). */
export const esquemaRespuestaEncuesta = z.object({
    calificacion: z
        .number({ message: "Elige una calificación" })
        .optional()
        .refine((v) => v !== undefined && Number.isInteger(v) && v >= 1 && v <= 5, "Elige una calificación")
        .transform((v) => v as number),
    comentario: z
        .string()
        .trim()
        .max(MAX_COMENTARIO, `Como mucho ${MAX_COMENTARIO} caracteres`),
});

export type DatosRespuestaEncuesta = z.infer<typeof esquemaRespuestaEncuesta>;

/** Cuerpo para la API: sin comentario si quedó vacío. */
export function cuerpoRespuesta(d: DatosRespuestaEncuesta): { calificacion: number; comentario?: string } {
    const comentario = d.comentario.trim();
    return comentario ? { calificacion: d.calificacion, comentario } : { calificacion: d.calificacion };
}

/** Pantallas de la página pública según la respuesta de la API. */
export type PasoEncuesta =
    | "cargando"
    | "formulario"
    | "gracias"
    | "ya_respondida"
    | "vencida"
    | "anulada"
    | "invalido"
    | "sin_enlace"
    | "limite"
    | "error";

/** Paso por el problema de la API (`codigo` del problem+json y estado HTTP). */
export function pasoPorProblema(estado: number, codigo: string | undefined): PasoEncuesta {
    if (codigo === "encuesta_anulada") return "anulada";
    if (codigo === "encuesta_vencida" || estado === 410) return "vencida";
    if (codigo === "encuesta_respondida" || estado === 409) return "ya_respondida";
    if (estado === 404) return "invalido";
    if (estado === 429) return "limite";
    return "error";
}
