// «Yo» de la app de campo (diseño aprobado por el PM, nov 2026): apariencia del teléfono, la línea
// «Rol · Empresa» del encabezado, el cambio de contraseña (espejo del validador de la API:
// `identidad/cambiar_contrasena.go`) y la verificación en dos pasos (`POST /yo/mfa` → `activar`).
// Compartido por la PWA y la app nativa. Sin React.
import type { Schema } from "@iris/contratos";
import { z } from "zod";

/** Apariencia elegida en «Yo»: «Teléfono» sigue al sistema (lo de siempre); se guarda en el dispositivo. */
export type Apariencia = "telefono" | "claro" | "oscuro";

/** Clave del almacén del dispositivo (la nativa; la PWA usa la de `next-themes`, `iris-tema`). */
export const CLAVE_APARIENCIA = "iris-campo-apariencia";

export const APARIENCIAS: readonly { valor: Apariencia; texto: string }[] = [
    { valor: "telefono", texto: "Teléfono" },
    { valor: "claro", texto: "Claro" },
    { valor: "oscuro", texto: "Oscuro" },
];

export function esApariencia(v: unknown): v is Apariencia {
    return v === "telefono" || v === "claro" || v === "oscuro";
}

/** ¿Va oscuro? Con «Teléfono», lo que diga el sistema. */
export function esOscuro(apariencia: Apariencia, sistemaOscuro: boolean): boolean {
    return apariencia === "oscuro" || (apariencia === "telefono" && sistemaOscuro);
}

/** Nombre visible de los roles base (los demás, con su código tal cual). */
const ROLES: Record<string, string> = {
    admin: "Administrador",
    supervisor: "Supervisor",
    despacho: "Despacho",
    tecnico: "Técnico",
    soporte: "Soporte",
    ventas: "Ventas",
    lectura: "Lectura",
};

export function nombreRol(codigo: string): string {
    return ROLES[codigo] ?? codigo;
}

/**
 * «Técnico · Fibra GT» bajo el nombre: el rol (el de técnico primero, sin repetir) y la empresa
 * activa, con lo que dé `GET /yo`. La cuadrilla no tiene nombre en `/yo`: no se inventa.
 */
export function lineaPerfil(roles: readonly Schema<"AsignacionRol">[] | null | undefined, empresa: string | null | undefined): string {
    const codigos = [...new Set((roles ?? []).map((r) => r.rol))].sort((a, b) => (a === "tecnico" ? -1 : b === "tecnico" ? 1 : 0));
    return [...codigos.slice(0, 2).map(nombreRol), ...(empresa ? [empresa] : [])].join(" · ");
}

// --- Contraseña -------------------------------------------------------------------------------

/** Mínimo de la API (`identidad.LongitudMinimaContrasena`). */
export const LONGITUD_MINIMA_CONTRASENA = 12;
/** `maxLength` del contrato. */
export const LONGITUD_MAXIMA_CONTRASENA = 200;

/** Caracteres (puntos de código) como los cuenta Go (`utf8.RuneCountInString`). */
export function largoContrasena(texto: string): number {
    return [...texto].length;
}

export const esquemaContrasenaPropia = z
    .object({
        actual: z
            .string()
            .min(1, "Escribe tu contraseña actual")
            .refine((v) => largoContrasena(v) <= LONGITUD_MAXIMA_CONTRASENA, `Admite hasta ${LONGITUD_MAXIMA_CONTRASENA} caracteres`),
        nueva: z
            .string()
            .refine((v) => largoContrasena(v) >= LONGITUD_MINIMA_CONTRASENA, `Usa ${LONGITUD_MINIMA_CONTRASENA} caracteres o más`)
            .refine((v) => largoContrasena(v) <= LONGITUD_MAXIMA_CONTRASENA, `Admite hasta ${LONGITUD_MAXIMA_CONTRASENA} caracteres`),
        repetir: z.string().min(1, "Repite la contraseña nueva"),
    })
    .superRefine((d, ctx) => {
        if (d.actual !== "" && d.nueva === d.actual) {
            ctx.addIssue({ code: "custom", path: ["nueva"], message: "Debe ser distinta de la actual" });
        }
        if (d.repetir !== "" && d.repetir !== d.nueva) {
            ctx.addIssue({ code: "custom", path: ["repetir"], message: "No coincide con la nueva" });
        }
    });

export type DatosContrasenaPropia = z.infer<typeof esquemaContrasenaPropia>;

/** Cuerpo de `POST /yo/contrasena` (la repetición solo vive en el formulario). */
export function cuerpoContrasenaPropia(d: DatosContrasenaPropia): Schema<"EntradaCambiarContrasena"> {
    return { actual: d.actual, nueva: d.nueva };
}

/** Ayudas en verde bajo los campos: cada regla que ya se cumple (nunca antes de escribir). */
export function reglasContrasena(d: Pick<DatosContrasenaPropia, "actual" | "nueva" | "repetir">): { largo: boolean; distinta: boolean; coinciden: boolean } {
    return {
        largo: largoContrasena(d.nueva) >= LONGITUD_MINIMA_CONTRASENA && largoContrasena(d.nueva) <= LONGITUD_MAXIMA_CONTRASENA,
        distinta: d.nueva !== "" && d.actual !== "" && d.nueva !== d.actual,
        coinciden: d.repetir !== "" && d.repetir === d.nueva,
    };
}

export const TEXTO_SESIONES_CONTRASENA = "Se cierran tus sesiones en otros equipos. En este teléfono sigues dentro.";
export const TEXTO_SIN_SENAL_CUENTA = "Cambiar tu contraseña o activar la verificación en dos pasos necesita señal: no se guarda para después.";

/** 429 `demasiados_intentos`: la API ya cerró la sesión y bloquea el cambio un rato. */
export function textoBloqueoContrasena(espera: string): { titulo: string; detalle: string } {
    return {
        titulo: `Demasiados intentos: vuelve a entrar en ${espera}`,
        detalle: "Escribiste mal tu contraseña actual demasiadas veces. Por seguridad se cerró tu sesión. Lo pendiente sigue guardado en el teléfono.",
    };
}

// --- Verificación en dos pasos ----------------------------------------------------------------

/** Secreto TOTP (base32) en grupos de 4 para escribirlo a mano en la app de códigos. */
export function agruparSecreto(secreto: string): string {
    return secreto.replace(/\s+/g, "").replace(/(.{4})(?=.)/g, "$1 ");
}

/**
 * Enlace `otpauth://` para «Abrir app»: el de la API si lo trae (es el del QR del panel) o, si no,
 * uno armado con el secreto como lo arma la API (`issuer=Iris`, SHA1, 6 dígitos, 30 s).
 */
export function enlaceOtpauth(mfa: Partial<Schema<"MFAConfigurado">> & Pick<Schema<"MFAConfigurado">, "secreto">, correo: string): string {
    if (mfa.url?.startsWith("otpauth://")) {
        return mfa.url;
    }
    const etiqueta = encodeURIComponent(`Iris:${correo}`);
    return `otpauth://totp/${etiqueta}?algorithm=SHA1&digits=6&issuer=Iris&period=30&secret=${encodeURIComponent(mfa.secreto.replace(/\s+/g, ""))}`;
}

export const esquemaCodigoActivar = z.object({
    codigo: z
        .string()
        .transform((v) => v.replace(/\s+/g, ""))
        .pipe(z.string().regex(/^[0-9]{6}$/, "El código tiene 6 dígitos")),
}) satisfies z.ZodType<Schema<"EntradaActivarMFA">, { codigo: string }>;

export const TEXTO_ACTIVAR_DOS_PASOS = "Desde ahora, al entrar en un equipo nuevo te pide este código. La huella de este teléfono sigue igual.";
export const TEXTO_REINICIAR_DOS_PASOS = "Para cambiar de teléfono, pide a un administrador que la reinicie.";

/** Estado del punto de «Verificación en dos pasos» en «Yo» (`null`: no se pudo leer, sin punto). */
export function estadoDosPasos(mfaActivo: boolean | null): { texto: string; tono: "success" | "neutral" } | null {
    if (mfaActivo === null) {
        return null;
    }
    return mfaActivo ? { texto: "Activa", tono: "success" } : { texto: "Apagada", tono: "neutral" };
}
