import type { FieldErrors } from "react-hook-form";
import type { ValoresBuzon } from "./esquemas";

/** Pasos del formulario del buzón: «Recibir» (IMAP, verificación, tickets nuevos) y «Enviar» (SMTP). */
export type Paso = "recibir" | "enviar";

/** Campos del paso «Enviar»; todo lo demás es de «Recibir». */
const CAMPOS_ENVIAR: readonly (keyof ValoresBuzon)[] = ["conSmtp", "smtpHost", "smtpPuerto", "smtpTls", "smtpUsuario", "smtpContrasena"];

/** Paso que hay que mostrar al intentar guardar con errores: el primero que tenga alguno (o `null`). */
export function pasoConError(errores: FieldErrors<ValoresBuzon>): Paso | null {
    const campos = Object.keys(errores) as (keyof ValoresBuzon)[];
    if (campos.length === 0) return null;
    return campos.some((c) => !CAMPOS_ENVIAR.includes(c)) ? "recibir" : "enviar";
}
