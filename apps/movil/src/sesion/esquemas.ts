// Formularios de inicio de sesión (zod), espejo de los del panel y de la API.
import type { Schema } from "@iris/contratos";
import { z } from "zod";

const PATRON_CORREO = /^[^\s@]+@[^\s@]+$/;

export const esquemaInicioSesion = z.object({
    correo: z
        .string()
        .trim()
        .min(1, "Escribe tu correo")
        .max(254, "El correo admite hasta 254 caracteres")
        .regex(PATRON_CORREO, "Escribe un correo válido, como nombre@empresa.com"),
    contrasena: z.string().min(1, "Escribe tu contraseña").max(200, "La contraseña admite hasta 200 caracteres"),
}) satisfies z.ZodType<Schema<"EntradaIniciarSesion">>;
export type DatosInicioSesion = z.infer<typeof esquemaInicioSesion>;

export const esquemaCodigoMFA = z.object({
    codigo: z
        .string()
        .transform((v) => v.replace(/\s+/g, ""))
        .pipe(z.string().regex(/^[0-9]{6}$/, "El código tiene 6 dígitos")),
}) satisfies z.ZodType<Schema<"EntradaVerificarMFA">, { codigo: string }>;
export type DatosCodigoMFA = z.infer<typeof esquemaCodigoMFA>;
