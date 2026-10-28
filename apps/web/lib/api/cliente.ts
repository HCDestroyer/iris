import { crearCliente, type ClienteIris } from "@iris/contratos";
import { CODIGO_CAMBIO_REQUERIDO, notificarCambioRequerido } from "../cuenta/sesion";
import { CABECERA_EMPRESA, empresaGuardada, esProblemaEmpresa, notificarProblemaEmpresa, type ProblemaEmpresa } from "../empresa";
import { ErrorApi, errorDesdeRespuesta, errorSinConexion } from "./errores";

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
    /** Empresa activa elegida (cabecera `X-Iris-Empresa`); por defecto, la guardada en el navegador. */
    leerEmpresa?: () => number | undefined;
    /** Aviso de un 422/403 de empresa en cualquier respuesta; por defecto, a los oyentes del panel. */
    alProblemaEmpresa?: (problema: ProblemaEmpresa) => void;
    /** Aviso de un 403 `cambio_contrasena_requerido` (contraseña temporal); por defecto, al panel. */
    alCambioRequerido?: () => void;
};

const PREFIJO_PROBLEMA = "urn:iris:problema:";

/** Código de un problem+json 403/422 (`empresa_requerida`, `cambio_contrasena_requerido`…), si la respuesta lo es. */
async function codigoProblema(respuesta: Response): Promise<string | undefined> {
    if ((respuesta.status !== 403 && respuesta.status !== 422) || !respuesta.headers.get("Content-Type")?.includes("json")) {
        return undefined;
    }
    try {
        const cuerpo: unknown = await respuesta.clone().json();
        const tipo = typeof cuerpo === "object" && cuerpo !== null ? (cuerpo as { type?: unknown }).type : undefined;
        if (typeof tipo === "string" && tipo.startsWith(PREFIJO_PROBLEMA)) {
            return tipo.slice(PREFIJO_PROBLEMA.length);
        }
    } catch {
        // Cuerpo ilegible: no es un problema conocido.
    }
    return undefined;
}

/**
 * Cliente `openapi-fetch` del panel: manda las cookies (`credentials: "include"`) y añade
 * `X-CSRF-Token` (desde la cookie `iris_csrf`) en todo método que cambia estado, y
 * `X-Iris-Empresa` (empresa activa elegida) en toda petición. Los arreglos de la query van
 * separados por coma (`estado=nuevo,abierto`), como los lee la API.
 */
export function crearClienteIris(opciones: OpcionesCliente = {}): ClienteIris {
    const leerCookies = opciones.leerCookies ?? (() => (typeof document === "undefined" ? "" : document.cookie));
    const leerEmpresa = opciones.leerEmpresa ?? empresaGuardada;
    const alProblemaEmpresa = opciones.alProblemaEmpresa ?? notificarProblemaEmpresa;
    const alCambioRequerido = opciones.alCambioRequerido ?? notificarCambioRequerido;
    const cliente = crearCliente({
        querySerializer: { array: { style: "form", explode: false } },
        baseUrl: opciones.baseUrl ?? (typeof window === "undefined" ? "http://localhost" : window.location.origin),
        ...(opciones.fetch ? { fetch: opciones.fetch } : {}),
    });
    cliente.use({
        onRequest({ request }) {
            const empresa = leerEmpresa();
            if (empresa !== undefined && !request.headers.has(CABECERA_EMPRESA)) {
                request.headers.set(CABECERA_EMPRESA, String(empresa));
            }
            if (METODOS_SIN_ESTADO.has(request.method.toUpperCase())) {
                return request;
            }
            const csrf = leerCookie(COOKIE_CSRF, leerCookies());
            if (csrf && !request.headers.has(CABECERA_CSRF)) {
                request.headers.set(CABECERA_CSRF, csrf);
            }
            return request;
        },
        async onResponse({ response }) {
            const codigo = await codigoProblema(response);
            if (codigo !== undefined && esProblemaEmpresa(codigo)) {
                alProblemaEmpresa(codigo);
            } else if (codigo === CODIGO_CAMBIO_REQUERIDO && response.status === 403) {
                alCambioRequerido();
            }
            return undefined;
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

/** Clave nueva para `Idempotency-Key` (una por intento lógico; se reusa al reintentar el mismo). */
export function nuevaClaveIdempotencia(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        return crypto.randomUUID();
    }
    return `${Date.now().toString(16)}-${Math.random().toString(16).slice(2)}`;
}

/**
 * Clave para el siguiente intento tras un error: si la API respondió (aunque sea con error), la
 * respuesta quedó guardada con esa clave y reusarla la repetiría; solo sin respuesta (red) se
 * reusa la misma para no duplicar la operación.
 */
export function claveTrasError(error: unknown, actual: string): string {
    return error instanceof ErrorApi && error.estado === 0 ? actual : nuevaClaveIdempotencia();
}

let clienteNavegador: ClienteIris | undefined;

/** Cliente único del navegador. */
export function api(): ClienteIris {
    clienteNavegador ??= crearClienteIris();
    return clienteNavegador;
}
