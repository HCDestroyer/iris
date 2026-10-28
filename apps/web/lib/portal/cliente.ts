import { crearCliente, type ClienteIris } from "@iris/contratos";
import { CABECERA_CSRF, leerCookie } from "../api/cliente";

/*
 * Cliente HTTP del portal de soporte (`/soporte`). Es otro actor que el panel (i1.md, «Portal de
 * soporte»): la sesión es la cookie HttpOnly `iris_portal` y el CSRF va en `X-CSRF-Token` desde la
 * cookie legible `iris_portal_csrf` (no `iris_csrf`). Nunca manda `X-Iris-Empresa`: la empresa
 * es la de la sesión.
 */

export const COOKIE_PORTAL = "iris_portal";
export const COOKIE_PORTAL_CSRF = "iris_portal_csrf";
const CABECERA_EMPRESA = "X-Iris-Empresa";
const PREFIJO_PORTAL = "/api/v1/portal/";
const RUTA_SESION = "/api/v1/portal/sesion";
const METODOS_SIN_ESTADO = new Set(["GET", "HEAD", "OPTIONS"]);

export type OpcionesClientePortal = {
    baseUrl?: string;
    leerCookies?: () => string;
    fetch?: typeof globalThis.fetch;
    /** Un 401 en `/api/v1/portal/*` (salvo el canje): la sesión del portal terminó. */
    alSesionTerminada?: () => void;
};

type Oyente = () => void;
const oyentes = new Set<Oyente>();

/** Escucha el fin de la sesión del portal (401). Devuelve la función para dejar de escuchar. */
export function alTerminarSesionPortal(oyente: Oyente): () => void {
    oyentes.add(oyente);
    return () => oyentes.delete(oyente);
}

function avisarSesionTerminada() {
    for (const o of oyentes) o();
}

/** ¿Este 401 significa que la sesión del portal terminó? El canje (`POST /portal/sesion`) responde 401 por un enlace inválido, no por sesión. */
export function esSesionTerminada(metodo: string, ruta: string, estado: number): boolean {
    return estado === 401 && ruta.startsWith(PREFIJO_PORTAL) && !(metodo.toUpperCase() === "POST" && ruta === RUTA_SESION);
}

export function crearClientePortal(opciones: OpcionesClientePortal = {}): ClienteIris {
    const leerCookies = opciones.leerCookies ?? (() => (typeof document === "undefined" ? "" : document.cookie));
    const alSesionTerminada = opciones.alSesionTerminada ?? avisarSesionTerminada;
    const cliente = crearCliente({
        querySerializer: { array: { style: "form", explode: false } },
        baseUrl: opciones.baseUrl ?? (typeof window === "undefined" ? "http://localhost" : window.location.origin),
        ...(opciones.fetch ? { fetch: opciones.fetch } : {}),
    });
    cliente.use({
        onRequest({ request }) {
            request.headers.delete(CABECERA_EMPRESA);
            if (METODOS_SIN_ESTADO.has(request.method.toUpperCase())) {
                return request;
            }
            const csrf = leerCookie(COOKIE_PORTAL_CSRF, leerCookies());
            if (csrf && !request.headers.has(CABECERA_CSRF)) {
                request.headers.set(CABECERA_CSRF, csrf);
            }
            return request;
        },
        onResponse({ request, response }) {
            if (esSesionTerminada(request.method, new URL(request.url).pathname, response.status)) {
                alSesionTerminada();
            }
            return undefined;
        },
    });
    return cliente;
}

let clienteNavegador: ClienteIris | undefined;

/** Cliente único del portal en el navegador. */
export function apiPortal(): ClienteIris {
    clienteNavegador ??= crearClientePortal();
    return clienteNavegador;
}
