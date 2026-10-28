import type { Schema } from "@iris/contratos";
import type { Permiso } from "../menu";
import { rutaSegura } from "../tiempo";

/*
 * Contraseña temporal (i1.md, PR #21): tras un reinicio de MFA el usuario entra con la
 * contraseña que le dio un administrador y la API corta toda petición de esa sesión con 403
 * `cambio_contrasena_requerido` (salvo `GET /yo`, `POST /yo/contrasena` y entrar o salir) hasta
 * que la cambie. El panel lo detecta en `GET /yo` (`usuario.cambioContrasenaRequerido`), en el
 * inicio de sesión y en cualquier 403 con ese código, y lleva a `/cambiar-contrasena`.
 */

export const RUTA_CAMBIAR_CONTRASENA = "/cambiar-contrasena";
export const RUTA_MFA = "/cuenta/mfa";
export const CODIGO_CAMBIO_REQUERIDO = "cambio_contrasena_requerido";

/** Destino seguro para volver: ni el login ni las propias pantallas de la cuenta. */
export function siguienteSeguro(valor: string | null | undefined, origen?: string): string {
    const ruta = rutaSegura(valor, "/", origen);
    return ruta.startsWith(RUTA_CAMBIAR_CONTRASENA) ? "/" : ruta;
}

/** `/cambiar-contrasena?siguiente=<ruta>` (sin `siguiente` si es el inicio). */
export function rutaCambiarContrasena(siguiente: string): string {
    const destino = siguienteSeguro(siguiente);
    return destino === "/" ? RUTA_CAMBIAR_CONTRASENA : `${RUTA_CAMBIAR_CONTRASENA}?${new URLSearchParams({ siguiente: destino })}`;
}

/** `/cuenta/mfa?siguiente=<ruta>` (sin `siguiente` si es el inicio). */
export function rutaConfigurarMfa(siguiente: string): string {
    const destino = siguienteSeguro(siguiente);
    return destino === "/" ? RUTA_MFA : `${RUTA_MFA}?${new URLSearchParams({ siguiente: destino })}`;
}

/** La app de campo se abre con navegación completa (queda bajo su service worker). */
export function esRutaCampo(ruta: string): boolean {
    return ruta === "/campo" || ruta.startsWith("/campo?") || ruta.startsWith("/campo/");
}

export function requiereCambio(usuario: Pick<Schema<"Usuario">, "cambioContrasenaRequerido"> | undefined): boolean {
    return usuario?.cambioContrasenaRequerido === true;
}

/**
 * «Reiniciar MFA» se ofrece con `usuarios.reiniciar_mfa`, sobre otro usuario (la API responde
 * 422 `reinicio_mfa_propio`) y con MFA activo (sin MFA, 422 `mfa_no_configurado`).
 */
export function puedeReiniciarMfa(permisos: readonly Permiso[], yoId: number, usuario: Pick<Schema<"Usuario">, "id" | "mfaActivo">): boolean {
    return permisos.includes("usuarios.reiniciar_mfa") && usuario.id !== yoId && usuario.mfaActivo;
}

type Oyente = () => void;
const oyentes = new Set<Oyente>();

/** El panel escucha el 403 `cambio_contrasena_requerido` de cualquier petición. */
export function suscribirCambioRequerido(oyente: Oyente): () => void {
    oyentes.add(oyente);
    return () => {
        oyentes.delete(oyente);
    };
}

export function notificarCambioRequerido(): void {
    for (const oyente of oyentes) {
        oyente();
    }
}
