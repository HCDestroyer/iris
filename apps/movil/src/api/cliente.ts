// Cliente `openapi-fetch` de la app nativa. La sesión es la misma del panel: cookie `iris_sesion`
// (la guarda el sistema de red del teléfono, como un navegador) + `X-CSRF-Token`, que en el
// teléfono sale del cuerpo de `POST /sesion` (no hay `document.cookie`) y vive en el llavero.
import { crearCliente, type ClienteIris } from "@iris/contratos";
import { CABECERA_APP, CABECERA_CANAL, valorCabeceraApp } from "@iris/sync";
import { Platform } from "react-native";
import { VERSION_APP } from "./version";

export const CABECERA_CSRF = "X-CSRF-Token";
export const CABECERA_EMPRESA = "X-Iris-Empresa";
const METODOS_SIN_ESTADO = new Set(["GET", "HEAD", "OPTIONS"]);

export interface OpcionesClienteMovil {
    baseUrl: string;
    fetch: typeof globalThis.fetch;
    /** Token CSRF de la sesión (en memoria; `null` antes de entrar). */
    leerCsrf: () => string | null;
    /** Empresa fija de este cliente (la de la cola); `null`/ausente = sin cabecera. */
    empresa?: number | null;
}

/**
 * Toda petición sale con `X-Iris-Canal: campo`, `X-Iris-App: campo/<versión> (android|ios)` (la API
 * registra la versión y el canal nativo del acceso), la empresa y, si cambia estado, el CSRF.
 */
export function crearClienteMovil(o: OpcionesClienteMovil): ClienteIris {
    const cliente = crearCliente({ baseUrl: o.baseUrl, fetch: o.fetch, querySerializer: { array: { style: "form", explode: false } } });
    cliente.use({
        onRequest({ request }) {
            if (!request.headers.has(CABECERA_CANAL)) {
                request.headers.set(CABECERA_CANAL, "campo");
            }
            if (!request.headers.has(CABECERA_APP)) {
                request.headers.set(CABECERA_APP, valorCabeceraApp(VERSION_APP, Platform.OS));
            }
            if (o.empresa !== undefined && o.empresa !== null && !request.headers.has(CABECERA_EMPRESA)) {
                request.headers.set(CABECERA_EMPRESA, String(o.empresa));
            }
            const csrf = o.leerCsrf();
            if (csrf && !METODOS_SIN_ESTADO.has(request.method.toUpperCase()) && !request.headers.has(CABECERA_CSRF)) {
                request.headers.set(CABECERA_CSRF, csrf);
            }
            return request;
        },
    });
    return cliente;
}
