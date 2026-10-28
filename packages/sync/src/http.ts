// Transporte y subidor reales sobre el cliente `openapi-fetch` de `@iris/contratos`: las rutas,
// los parámetros y las respuestas los tipa el OpenAPI de la API. Quien crea el cliente decide
// las credenciales (cookie + `X-CSRF-Token` en la PWA, Bearer en Expo) y la empresa activa
// (`X-Iris-Empresa`); aquí se agrega el canal `X-Iris-Canal: campo` a toda petición.
import type { ClienteIris } from "@iris/contratos";
import type { Problema } from "./contrato";
import { ErrorTransporte } from "./errores";
import type { TransportePosiciones } from "./posiciones";
import type { RespuestaApi, Subidor, Transporte } from "./transporte";

export const CABECERA_CANAL = "X-Iris-Canal";
const CANAL = { [CABECERA_CANAL]: "campo" } as const;
const VERSION_TUS = "1.0.0";

type Resultado<D> = { data?: D; error?: unknown; response: Response };

function esProblema(valor: unknown): valor is Problema {
    return typeof valor === "object" && valor !== null && "type" in valor && "status" in valor && "title" in valor;
}

function msDe(texto: string | null | undefined): number | null {
    const ms = texto ? Date.parse(texto) : Number.NaN;
    return Number.isNaN(ms) ? null : ms;
}

/** Espera una llamada del cliente: `data` o `ErrorTransporte` (`red` sin respuesta, `http` con ella). */
async function resolver<D>(llamada: Promise<Resultado<D>>): Promise<{ datos: D; respuesta: Response }> {
    let r: Resultado<D>;
    try {
        r = await llamada;
    } catch (e) {
        // fetch rechazado (sin señal, CORS, cortado) o cuerpo ilegible: se reintenta.
        throw new ErrorTransporte("red", e instanceof Error ? e.message : "Sin conexión");
    }
    if (!r.response.ok) {
        const problema = esProblema(r.error) ? r.error : null;
        throw new ErrorTransporte("http", problema?.title ?? `HTTP ${r.response.status}`, r.response.status, problema, r.response.headers);
    }
    return { datos: r.data as D, respuesta: r.response };
}

function conHora<D>(datos: D, respuesta: Response, servidorEn?: string): RespuestaApi<D> {
    return { datos, horaServidor: msDe(servidorEn) ?? msDe(respuesta.headers.get("Date")) };
}

/** `POST /sync/lote`, `GET /campo/paquete` y `GET /campo/cambios` con el cliente de la API. */
export function crearTransporteApi(cliente: ClienteIris): Transporte {
    return {
        async enviarLote(peticion) {
            const { datos, respuesta } = await resolver(cliente.POST("/api/v1/sync/lote", { body: peticion, headers: CANAL }));
            return conHora(datos, respuesta, datos.servidorEn);
        },
        async obtenerPaquete(fecha) {
            const { datos, respuesta } = await resolver(
                cliente.GET("/api/v1/campo/paquete", { params: { query: { fecha } }, headers: CANAL }),
            );
            return conHora(datos, respuesta, datos.servidorEn);
        },
        async obtenerCambios(cursor) {
            const { datos, respuesta } = await resolver(
                cliente.GET("/api/v1/campo/cambios", { params: { query: { cursor } }, headers: CANAL }),
            );
            return conHora(datos, respuesta);
        },
    };
}

/** `POST /campo/posiciones` (ubicación en vivo) con el cliente de la API. */
export function crearTransportePosicionesApi(cliente: ClienteIris): TransportePosiciones {
    return {
        async enviarPosiciones(entrada) {
            const { datos, respuesta } = await resolver(cliente.POST("/api/v1/campo/posiciones", { body: entrada, headers: CANAL }));
            return conHora(datos, respuesta, datos.servidorEn);
        },
    };
}

function offsetDe(respuesta: Response): number {
    const texto = respuesta.headers.get("Upload-Offset");
    const valor = texto === null || texto.trim() === "" ? Number.NaN : Number(texto);
    if (!Number.isInteger(valor) || valor < 0) {
        throw new ErrorTransporte("http", "Respuesta tus sin Upload-Offset", respuesta.status);
    }
    return valor;
}

/** Alta (`POST /adjuntos`) y tus 1.0 (`HEAD`/`PATCH /adjuntos/{id}/subida`) con el cliente de la API. */
export function crearSubidorApi(cliente: ClienteIris): Subidor {
    return {
        async crear(entrada) {
            const { datos } = await resolver(
                cliente.POST("/api/v1/adjuntos", {
                    body: entrada,
                    params: { header: { "Idempotency-Key": entrada.id } },
                    headers: CANAL,
                }),
            );
            return datos;
        },
        async consultarOffset(id) {
            const { respuesta } = await resolver(
                cliente.HEAD("/api/v1/adjuntos/{id}/subida", {
                    params: { path: { id }, header: { "Tus-Resumable": VERSION_TUS } },
                    headers: { ...CANAL, "Cache-Control": "no-store" },
                }),
            );
            return offsetDe(respuesta);
        },
        async enviarParte(id, offset, parte) {
            const { respuesta } = await resolver(
                cliente.PATCH("/api/v1/adjuntos/{id}/subida", {
                    params: { path: { id }, header: { "Tus-Resumable": VERSION_TUS, "Upload-Offset": offset } },
                    headers: { ...CANAL, "Content-Type": "application/offset+octet-stream" },
                    // El OpenAPI declara el cuerpo como texto binario: van los bytes tal cual.
                    body: "",
                    bodySerializer: () => parte as Uint8Array<ArrayBuffer>,
                }),
            );
            return offsetDe(respuesta);
        },
    };
}
