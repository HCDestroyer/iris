import { crearCliente, type ClienteIris } from "@iris/contratos";
import { errorDesdeRespuesta, errorSinConexion } from "./errores";

export const COOKIE_CSRF = "iris_csrf";
export const COOKIE_SESION = "iris_sesion";
export const CABECERA_CSRF = "X-CSRF-Token";

const METODOS_SIN_ESTADO = new Set(["GET", "HEAD", "OPTIONS"]);

/** Valor de una cookie legible (`iris_csrf` no es HttpOnly; `iris_sesion` sí y no se ve aquí). */
export function leerCookie(nombre: string, cookies: string): string | undefined {
    for (const parte of cookies.split(";")) {
        const [clave, ...resto] = parte.trim().split("=");
        if (clave === nombre) {
            const valor = resto.join("=");
            try {
                return decodeURIComponent(valor);
            } catch {
                return valor;
            }
        }
    }
    return undefined;
}

export type OpcionesCliente = {
    /** Origen de la API; en el navegador, vacío (mismo origen, por el proxy de Next). */
    baseUrl?: string;
    /** Lee `document.cookie`; inyectable para pruebas. */
    leerCookies?: () => string;
    fetch?: typeof globalThis.fetch;
};

/**
 * Cliente `openapi-fetch` del panel: manda las cookies (`credentials: "include"`) y añade
 * `X-CSRF-Token` (desde la cookie `iris_csrf`) en todo método que cambia estado.
 */
export function crearClienteIris(opciones: OpcionesCliente = {}): ClienteIris {
    const leerCookies = opciones.leerCookies ?? (() => (typeof document === "undefined" ? "" : document.cookie));
    const cliente = crearCliente({
        baseUrl: opciones.baseUrl ?? (typeof window === "undefined" ? "http://localhost" : window.location.origin),
        ...(opciones.fetch ? { fetch: opciones.fetch } : {}),
    });
    cliente.use({
        onRequest({ request }) {
            if (METODOS_SIN_ESTADO.has(request.method.toUpperCase())) {
                return undefined;
            }
            const csrf = leerCookie(COOKIE_CSRF, leerCookies());
            if (csrf && !request.headers.has(CABECERA_CSRF)) {
                request.headers.set(CABECERA_CSRF, csrf);
            }
            return request;
        },
    });
    return cliente;
}

type Resultado<D> = { data?: D; error?: unknown; response: Response };

/**
 * Espera una llamada del cliente y devuelve `data`, o lanza `ErrorApi` (problem+json o red).
 *
 *   const yo = await resolver(api.GET("/api/v1/yo"));
 */
export async function resolver<D>(llamada: Promise<Resultado<D>>): Promise<D> {
    let resultado: Resultado<D>;
    try {
        resultado = await llamada;
    } catch (causa) {
        throw errorSinConexion(causa);
    }
    if (!resultado.response.ok) {
        throw errorDesdeRespuesta(resultado.response, resultado.error);
    }
    return resultado.data as D;
}

let clienteNavegador: ClienteIris | undefined;

/** Cliente único del navegador. */
export function api(): ClienteIris {
    clienteNavegador ??= crearClienteIris();
    return clienteNavegador;
}
