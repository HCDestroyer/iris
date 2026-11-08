import { z } from "zod";

/*
 * Espejo del validador del servidor para `POST /canales` y `POST /canales/{id}/actualizar`
 * (OpenAPI `EntradaCrearCanal` / `EntradaActualizarCanal`): número E.164, ids de Meta numéricos,
 * token de 20 a 1024 caracteres y app secret de 16 a 256. Los espacios del principio y del final
 * de lo pegado se quitan antes de validar; en el número, también los de en medio y los guiones.
 */

export const USOS = ["soporte", "cobros", "ventas", "avisos"] as const;
export type Uso = (typeof USOS)[number];

/** «+502 2200-0040» → «+50222000040». */
export function normalizarNumero(texto: string): string {
    return texto.replace(/[\s\-().]/g, "");
}

const nombre = z.string().trim().min(1, "Escribe un nombre").max(120, "Hasta 120 caracteres");
const numero = z
    .string()
    .transform(normalizarNumero)
    .pipe(z.string().min(1, "Escribe el número").regex(/^\+[1-9][0-9]{7,14}$/, "Con + y código de país, de 8 a 15 dígitos (E.164): +50222000040"));
const phoneNumberId = z
    .string()
    .trim()
    .min(1, "Pega el Phone number ID")
    .regex(/^[0-9]{1,30}$/, "Solo números (hasta 30 dígitos)");
const wabaId = z
    .string()
    .trim()
    .regex(/^[0-9]{0,30}$/, "Solo números (hasta 30 dígitos)");

const MSJ_TOKEN = "El token tiene entre 20 y 1024 caracteres";
const MSJ_SECRETO = "El app secret tiene entre 16 y 256 caracteres";

export const esquemaNuevoNumero = z.object({
    nombre,
    numero,
    phoneNumberId,
    wabaId,
    token: z.string().trim().min(1, "Pega el token de acceso").min(20, MSJ_TOKEN).max(1024, MSJ_TOKEN),
    appSecret: z.string().trim().min(1, "Pega el app secret").min(16, MSJ_SECRETO).max(256, MSJ_SECRETO),
    usos: z.array(z.enum(USOS)).min(1, "Elige al menos un uso"),
    avisosEmpresa: z.boolean(),
    colaId: z.string(),
}).superRefine((v, ctx) => {
    if (v.avisosEmpresa && !v.usos.includes("avisos")) ctx.addIssue({ code: "custom", path: ["avisosEmpresa"], message: "Marca también el uso «Avisos»" });
});
export type EntradaNuevoNumero = z.input<typeof esquemaNuevoNumero>;
export type ValoresNuevoNumero = z.output<typeof esquemaNuevoNumero>;

/** Vacío = se queda el guardado. */
const secretoOpcional = (min: number, max: number, mensaje: string) =>
    z
        .string()
        .trim()
        .refine((s) => s === "" || (s.length >= min && s.length <= max), mensaje);

export const esquemaCredenciales = z.object({
    nombre,
    numero,
    phoneNumberId,
    wabaId,
    token: secretoOpcional(20, 1024, MSJ_TOKEN),
    appSecret: secretoOpcional(16, 256, MSJ_SECRETO),
});
export type EntradaCredenciales = z.input<typeof esquemaCredenciales>;
export type ValoresCredenciales = z.output<typeof esquemaCredenciales>;

/** Cambiar el token después de una prueba fallida (en «Conectar»); el app secret, opcional. */
export const esquemaCambiarToken = z.object({
    token: z.string().trim().min(1, "Pega el token nuevo").min(20, MSJ_TOKEN).max(1024, MSJ_TOKEN),
    appSecret: secretoOpcional(16, 256, MSJ_SECRETO),
});
export type EntradaCambiarToken = z.input<typeof esquemaCambiarToken>;
export type ValoresCambiarToken = z.output<typeof esquemaCambiarToken>;
