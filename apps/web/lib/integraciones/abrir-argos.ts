import type { Schema } from "@iris/contratos";
import { api, nuevaClaveIdempotencia, resolver } from "../api/cliente";
import { ErrorApi } from "../api/errores";

/*
 * «Abrir en Argos» y «Vincular mi usuario de Argos»: autologin Iris → Argos (contrato con Argos
 * 0.7.0 §1.3; `POST /integraciones/argosisp/abrir`, permiso `integraciones.abrir_argos`). El
 * servidor devuelve un enlace de **un solo uso** que dura 60 s y el panel lo abre enseguida en otra
 * pestaña. Sin `destino` llega al inicio de Argos: es «Vincular» (la primera vez Argos pide el
 * correo y la contraseña de Argos y crea el vínculo que autoriza aprobar y rechazar boletas).
 *
 * El enlace lleva un ticket de sesión: solo se abre si va por `https` (o `http` en este mismo
 * equipo, para desarrollo) **y** al mismo origen que el panel de Argos del conector (`urlPublica`
 * de `GET /integraciones/enlaces`). Cualquier otra dirección no se abre.
 */

export type DestinoArgos = Schema<"DestinoArgos">;

const LOCALES: readonly string[] = ["localhost", "127.0.0.1"];

/** Origen (`https://argos.isp.gt`) de una dirección http(s) sin usuario ni contraseña, o `null`. */
export function origenDe(url: string | null | undefined): string | null {
    if (!url) return null;
    let u: URL;
    try {
        u = new URL(url);
    } catch {
        return null;
    }
    if ((u.protocol !== "https:" && u.protocol !== "http:") || u.username || u.password) return null;
    return u.origin;
}

/**
 * El enlace del ticket, solo si se puede abrir: `https` (`http` únicamente en `localhost` o
 * `127.0.0.1`), sin usuario ni contraseña, y en el origen esperado (el panel de Argos del
 * conector). Si no, `null`: nunca `javascript:`, otro sitio ni un `http` por la red.
 */
export function urlArgosSegura(url: string, origenEsperado: string | null): string | null {
    let u: URL;
    try {
        u = new URL(url);
    } catch {
        return null;
    }
    if (u.username || u.password) return null;
    if (u.protocol !== "https:" && !(u.protocol === "http:" && LOCALES.includes(u.hostname))) return null;
    if (origenEsperado === null || u.origin !== origenEsperado) return null;
    return u.toString();
}

/** Cada clic lleva una `Idempotency-Key` nueva: la respuesta no se guarda (409 `idempotencia_no_repetible`). */
export function pedirEnlaceArgos(destino?: DestinoArgos) {
    return resolver(api().POST("/api/v1/integraciones/argosisp/abrir", { params: { header: { "Idempotency-Key": nuevaClaveIdempotencia() } }, body: destino ? { destino } : {} }));
}

/** Por qué no se abrió sin llegar a pedir (o a usar) el enlace. */
export type MotivoNoAbrio = "emergentes_bloqueadas" | "origen_desconocido" | "enlace_no_confiable";
export type ResultadoAbrir = { abierto: true } | { abierto: false; motivo: MotivoNoAbrio } | { abierto: false; error: unknown };

type Ventana = { opener: unknown; location: { replace: (url: string) => void }; close: () => void };
type Dependencias = { abrirVentana?: () => Ventana | null; pedir?: (destino?: DestinoArgos) => Promise<{ url: string }> };

/**
 * Pide el enlace y lo abre en otra pestaña sin `opener`. La pestaña se abre **en el mismo clic**
 * (en blanco) y se manda al enlace cuando llega: si se abriera después de esperar al servidor, el
 * navegador la bloquearía. Si el navegador no deja abrirla, **no se pide el ticket** (quedaría un
 * ticket vivo sin usar). Si el enlace no es del panel de Argos del conector, la pestaña se cierra.
 */
export async function abrirEnArgos(destino: DestinoArgos | undefined, origenEsperado: string | null, dep: Dependencias = {}): Promise<ResultadoAbrir> {
    if (origenEsperado === null) return { abierto: false, motivo: "origen_desconocido" };
    const abrirVentana = dep.abrirVentana ?? (() => window.open("about:blank", "_blank") as Ventana | null);
    const pestana = abrirVentana();
    if (!pestana) return { abierto: false, motivo: "emergentes_bloqueadas" };
    pestana.opener = null;
    try {
        const { url } = await (dep.pedir ?? pedirEnlaceArgos)(destino);
        const segura = urlArgosSegura(url, origenEsperado);
        if (!segura) {
            pestana.close();
            return { abierto: false, motivo: "enlace_no_confiable" };
        }
        pestana.location.replace(segura);
        return { abierto: true };
    } catch (error) {
        pestana.close();
        return { abierto: false, error };
    }
}

const TEXTO_MOTIVO: Record<MotivoNoAbrio, { texto: string; reintentar: boolean }> = {
    emergentes_bloqueadas: { texto: "Permite las ventanas emergentes para abrir Argos.", reintentar: true },
    origen_desconocido: { texto: "Iris no pudo comprobar la dirección del panel de Argos, así que no lo abrió. Un administrador la revisa en Administración › Integraciones.", reintentar: false },
    enlace_no_confiable: { texto: "El enlace no es del panel de Argos de tu conector, así que Iris no lo abrió. Avisa a un administrador.", reintentar: false },
};

/** Por qué no se pudo abrir Argos, en palabras de quien pulsó. `reintentar`: tiene sentido volver a pulsar. */
export function explicarNoAbrio(r: Exclude<ResultadoAbrir, { abierto: true }>): { texto: string; reintentar: boolean } {
    return "motivo" in r ? TEXTO_MOTIVO[r.motivo] : explicarErrorAbrir(r.error);
}

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
