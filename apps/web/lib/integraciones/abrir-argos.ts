import type { Schema } from "@iris/contratos";
import { api, nuevaClaveIdempotencia, resolver } from "../api/cliente";
import { ErrorApi } from "../api/errores";

/*
 * «Abrir en Argos» y «Vincular mi usuario de Argos»: autologin Iris → Argos (contrato con Argos
 * 0.7.0 §1.3; `POST /integraciones/argosisp/abrir`, permiso `integraciones.abrir_argos`). El
 * servidor devuelve un enlace de **un solo uso** que dura 60 s y el panel lo abre enseguida en otra
 * pestaña. Sin `destino` llega al inicio de Argos: es «Vincular» (la primera vez Argos pide el
 * correo y la contraseña de Argos y crea el vínculo que autoriza aprobar y rechazar boletas).
 */

export type DestinoArgos = Schema<"DestinoArgos">;

/** El enlace solo se abre si es http(s) y sin usuario ni contraseña (nunca `javascript:` ni otro). */
export function urlArgosSegura(url: string): string | null {
    let u: URL;
    try {
        u = new URL(url);
    } catch {
        return null;
    }
    if ((u.protocol !== "https:" && u.protocol !== "http:") || u.username || u.password) return null;
    return u.toString();
}

/** Cada clic lleva una `Idempotency-Key` nueva: la respuesta no se guarda (409 `idempotencia_no_repetible`). */
export function pedirEnlaceArgos(destino?: DestinoArgos) {
    return resolver(api().POST("/api/v1/integraciones/argosisp/abrir", { params: { header: { "Idempotency-Key": nuevaClaveIdempotencia() } }, body: destino ? { destino } : {} }));
}

export type ResultadoAbrir = { abierto: true } | { abierto: false; error: unknown };

/**
 * Pide el enlace y lo abre en otra pestaña sin `opener`. La pestaña se abre **en el mismo clic**
 * (en blanco) y se manda al enlace cuando llega: si se abriera después de esperar al servidor, el
 * navegador la bloquearía como ventana emergente. Si algo falla, la pestaña se cierra.
 */
export async function abrirEnArgos(destino?: DestinoArgos): Promise<ResultadoAbrir> {
    const pestana = typeof window === "undefined" ? null : window.open("about:blank", "_blank");
    if (pestana) pestana.opener = null;
    try {
        const { url } = await pedirEnlaceArgos(destino);
        const segura = urlArgosSegura(url);
        if (!segura) throw new ErrorApi({ estado: 502, codigo: "enlace_invalido", titulo: "Argos devolvió un enlace que no se puede abrir" });
        if (pestana) pestana.location.replace(segura);
        else window.open(segura, "_blank", "noopener,noreferrer");
        return { abierto: true };
    } catch (error) {
        pestana?.close();
        return { abierto: false, error };
    }
}

/** Por qué no se pudo abrir Argos, en palabras de quien pulsó. `reintentar`: tiene sentido volver a pulsar. */
export function explicarErrorAbrir(e: unknown): { texto: string; reintentar: boolean } {
    if (!(e instanceof ErrorApi)) return { texto: "No se pudo abrir Argos.", reintentar: true };
    switch (e.codigo) {
        case "conector_apagado":
            return { texto: "El conector con Argos está apagado. Un administrador lo activa en Administración › Integraciones.", reintentar: false };
        case "iris_apagado":
            return { texto: "Argos tiene apagada su conexión con Iris. Pide a un administrador de Argos que la active.", reintentar: false };
        case "argos_sin_aprobacion":
            return { texto: "Actualiza Argos a 1.15.0 o posterior para entrar desde Iris.", reintentar: false };
        case "solo_personas":
            return { texto: "Solo una persona con su sesión del panel puede abrir Argos.", reintentar: false };
        case "demasiados_intentos":
            return { texto: `Demasiados intentos. Vuelve a probar en ${Math.max(1, Math.ceil(e.reintentarEn ?? 60))} s.`, reintentar: true };
        case "argos_no_disponible":
            return { texto: "Argos no respondió. Vuelve a intentarlo.", reintentar: true };
        case "sin_conexion":
        case "api_no_disponible":
            return { texto: "No hay conexión con Iris. Revisa tu red.", reintentar: true };
        default:
            return { texto: e.detalle ?? e.titulo, reintentar: e.estado >= 500 };
    }
}
