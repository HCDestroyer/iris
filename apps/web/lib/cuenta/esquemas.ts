import type { Schema } from "@iris/contratos";
import { z } from "zod";

/*
 * Espejo de los validadores de la API para la cuenta propia y el reinicio de MFA
 * (OpenAPI: EntradaCambiarContrasena, EntradaReiniciarMFA; identidad/cambiar_contrasena.go y
 * crear_usuario.go `hashValidado`). La API cuenta la longitud en runas
 * (`utf8.RuneCountInString`), no en unidades UTF-16: aquí igual.
 */

/** Mínimo de la API (`identidad.LongitudMinimaContrasena`). */
export const LONGITUD_MINIMA_CONTRASENA = 12;
/** `maxLength` del contrato. */
export const LONGITUD_MAXIMA_CONTRASENA = 200;

/** Caracteres (puntos de código) como los cuenta Go. */
export function longitud(texto: string): number {
    return [...texto].length;
}

const nuevaContrasena = z
    .string()
    .refine((v) => longitud(v) >= LONGITUD_MINIMA_CONTRASENA, `Usa al menos ${LONGITUD_MINIMA_CONTRASENA} caracteres`)
    .refine((v) => longitud(v) <= LONGITUD_MAXIMA_CONTRASENA, `Admite hasta ${LONGITUD_MAXIMA_CONTRASENA} caracteres`);

export const esquemaCambiarContrasena = z
    .object({
        actual: z
            .string()
            .min(1, "Escribe tu contraseña actual")
            .refine((v) => longitud(v) <= LONGITUD_MAXIMA_CONTRASENA, `Admite hasta ${LONGITUD_MAXIMA_CONTRASENA} caracteres`),
        nueva: nuevaContrasena,
        confirmar: z.string().min(1, "Repite la contraseña nueva"),
    })
    .superRefine((d, ctx) => {
        if (d.actual !== "" && d.nueva === d.actual) {
            ctx.addIssue({ code: "custom", path: ["nueva"], message: "Debe ser distinta de la actual" });
        }
        if (d.confirmar !== "" && d.confirmar !== d.nueva) {
            ctx.addIssue({ code: "custom", path: ["confirmar"], message: "No coincide con la contraseña nueva" });
        }
    });

export type DatosCambiarContrasena = z.infer<typeof esquemaCambiarContrasena>;

/** Cuerpo de `POST /yo/contrasena` (sin la confirmación, que solo vive en el formulario). */
export function cuerpoCambiarContrasena(d: DatosCambiarContrasena): Schema<"EntradaCambiarContrasena"> {
    return { actual: d.actual, nueva: d.nueva };
}

export const esquemaReiniciarMfa = z.object({
    motivo: z
        .string()
        .trim()
        .min(1, "Escribe el motivo")
        .max(500, "El motivo admite hasta 500 caracteres"),
    contrasenaTemporal: z
        .string()
        .refine((v) => longitud(v) >= LONGITUD_MINIMA_CONTRASENA, `La contraseña temporal necesita al menos ${LONGITUD_MINIMA_CONTRASENA} caracteres`)
        .refine((v) => longitud(v) <= LONGITUD_MAXIMA_CONTRASENA, `Admite hasta ${LONGITUD_MAXIMA_CONTRASENA} caracteres`),
    revocarTokens: z.boolean(),
});

export type DatosReiniciarMfa = z.infer<typeof esquemaReiniciarMfa>;

export function cuerpoReiniciarMfa(d: DatosReiniciarMfa, version: number): Schema<"EntradaReiniciarMFA"> {
    return { version, motivo: d.motivo, contrasenaTemporal: d.contrasenaTemporal, revocarTokens: d.revocarTokens };
}

/**
 * Alfabeto de la contraseña temporal: sin caracteres que se confunden al dictarla por teléfono
 * (0/O, 1/l/I) ni símbolos. 56 símbolos × 16 ≈ 93 bits.
 */
export const ALFABETO_TEMPORAL = "23456789abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ";
const GRUPOS = 4;
const POR_GRUPO = 4;

/** Llena `bytes` con aleatorios criptográficos (inyectable para pruebas). */
export type Aleatorio = (bytes: Uint8Array) => void;

const aleatorioNavegador: Aleatorio = (bytes) => {
    crypto.getRandomValues(bytes);
};

/**
 * Contraseña temporal segura generada en el navegador: 4 grupos de 4 separados por guion
 * (`k7Qm-3xTz-…`, 19 caracteres), fácil de dictar. Muestreo por rechazo para que cada símbolo
 * sea igual de probable.
 */
export function generarContrasenaTemporal(aleatorio: Aleatorio = aleatorioNavegador): string {
    const n = ALFABETO_TEMPORAL.length;
    const limite = 256 - (256 % n);
    const simbolos: string[] = [];
    const bytes = new Uint8Array(32);
    while (simbolos.length < GRUPOS * POR_GRUPO) {
        aleatorio(bytes);
        for (const b of bytes) {
            if (b < limite && simbolos.length < GRUPOS * POR_GRUPO) {
                simbolos.push(ALFABETO_TEMPORAL.charAt(b % n));
            }
        }
    }
    const grupos: string[] = [];
    for (let i = 0; i < GRUPOS; i++) {
        grupos.push(simbolos.slice(i * POR_GRUPO, (i + 1) * POR_GRUPO).join(""));
    }
    return grupos.join("-");
}

/** Secreto TOTP (base32) en grupos de 4 para escribirlo a mano en la app. */
export function agruparSecreto(secreto: string): string {
    return secreto.replace(/\s+/g, "").replace(/(.{4})(?=.)/g, "$1 ");
}
