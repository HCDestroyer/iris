// Transporte y subidor reales sobre el cliente `openapi-fetch` de `@iris/contratos`: las rutas,
// los parámetros y las respuestas los tipa el OpenAPI de la API. Quien crea el cliente decide
// las credenciales (cookie + `X-CSRF-Token` en la PWA, Bearer en Expo) y la empresa activa
// (`X-Iris-Empresa`); aquí se agrega el canal `X-Iris-Canal: campo` a toda petición.
import type { ClienteIris } from "@iris/contratos";
import type { MiBodega, Problema, Traslado } from "./contrato";
import { ErrorTransporte } from "./errores";
import type { TransportePosiciones } from "./posiciones";
import type { RespuestaApi, Subidor, Transporte } from "./transporte";

export const CABECERA_CANAL = "X-Iris-Canal";
const CANAL = { [CABECERA_CANAL]: "campo" } as const;
const VERSION_TUS = "1.0.0";

/**
 * Versión y plataforma de la app que hace la petición: `X-Iris-App: campo/<versión> (<plataforma>)`.
 * La API la registra (Administración → Actividad de la app) y la usa para el canal del historial de
 * accesos (PWA o nativa). `X-Iris-Pendientes` lleva las operaciones que la cola no ha enviado.
 */
export const CABECERA_APP = "X-Iris-App";
export const CABECERA_PENDIENTES = "X-Iris-Pendientes";

export interface IdentidadApp {
    /** Versión de la app (semver o la etiqueta de la compilación). */
    version: string;
    /** `pwa`, `android`, `ios`… */
    plataforma: string;
    /** Operaciones sin enviar en la cola local (null = no se informa). */
    pendientes?: () => number | null;
}

/** Valor de `X-Iris-App`, con la versión y la plataforma limpias para el formato que acepta la API. */
export function valorCabeceraApp(version: string, plataforma: string): string {
    const v = version.trim().replace(/[^0-9A-Za-z.+_-]/g, "").replace(/^[^0-9A-Za-z]+/, "").slice(0, 40) || "0";
    const p = plataforma.trim().toLowerCase().replace(/[^a-z0-9_-]/g, "").slice(0, 40) || "desconocida";
    return `campo/${v} (${p})`;
}

/** Cabeceras de la app (`X-Iris-App` y, si se sabe, `X-Iris-Pendientes`); vacío sin app. */
export function cabecerasApp(app?: IdentidadApp): Record<string, string> {
    if (!app) return {};
    const cabeceras: Record<string, string> = { [CABECERA_APP]: valorCabeceraApp(app.version, app.plataforma) };
    const pendientes = app.pendientes?.();
    if (typeof pendientes === "number" && Number.isInteger(pendientes) && pendientes >= 0) {
        cabeceras[CABECERA_PENDIENTES] = String(Math.min(pendientes, 999_999));
    }
    return cabeceras;
}

const conApp = (app: IdentidadApp | undefined, extra: Record<string, string> = {}) => ({ ...CANAL, ...cabecerasApp(app), ...extra });

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

/**
 * `POST /sync/lote`, `GET /campo/paquete` y `GET /campo/cambios` con el cliente de la API. Con `app`,
 * cada petición lleva además `X-Iris-App` y `X-Iris-Pendientes`.
 */
export function crearTransporteApi(cliente: ClienteIris, app?: IdentidadApp): Transporte {
    return {
        async enviarLote(peticion) {
            const { datos, respuesta } = await resolver(cliente.POST("/api/v1/sync/lote", { body: peticion, headers: conApp(app) }));
            return conHora(datos, respuesta, datos.servidorEn);
        },
        async obtenerPaquete(fecha) {
            const { datos, respuesta } = await resolver(
                cliente.GET("/api/v1/campo/paquete", { params: { query: { fecha } }, headers: conApp(app) }),
            );
            return conHora(datos, respuesta, datos.servidorEn);
        },
        async obtenerCambios(cursor) {
            const { datos, respuesta } = await resolver(
                cliente.GET("/api/v1/campo/cambios", { params: { query: { cursor } }, headers: conApp(app) }),
            );
            return conHora(datos, respuesta);
        },
    };
}

/** Lecturas del inventario del técnico (contrato Iris ↔ ArgosISP 0.5.0), solo con señal. */
export interface InventarioCampo {
    /** Traslados enviados a su bodega que faltan por recibir (con la recepción que mandó, si la hay). */
    porRecibir(): Promise<Traslado[]>;
    /** Su bodega en Argos: existencias, unidades y kardex reciente. */
    miBodega(): Promise<MiBodega>;
}

/**
 * `GET /campo/inventario/traslados?estado=enviado` (todas las páginas, hasta 10) y
 * `GET /campo/inventario/mi-bodega` con el cliente de la API. Lanzan `ErrorTransporte`.
 */
export function crearInventarioApi(cliente: ClienteIris, app?: IdentidadApp): InventarioCampo {
    return {
        async porRecibir() {
            const todos: Traslado[] = [];
            let cursor: string | undefined;
            for (let pagina = 0; pagina < 10; pagina += 1) {
                const { datos } = await resolver(
                    cliente.GET("/api/v1/campo/inventario/traslados", {
                        params: { query: { estado: "enviado", ...(cursor ? { cursor } : {}) } },
                        headers: conApp(app),
                    }),
                );
                todos.push(...(datos.data ?? []));
                if (!datos.nextCursor) {
                    break;
                }
                cursor = datos.nextCursor;
            }
            return todos;
        },
        async miBodega() {
            const { datos } = await resolver(cliente.GET("/api/v1/campo/inventario/mi-bodega", { headers: conApp(app) }));
            return datos;
        },
    };
}

/** `POST /campo/posiciones` (ubicación en vivo) con el cliente de la API. */
export function crearTransportePosicionesApi(cliente: ClienteIris, app?: IdentidadApp): TransportePosiciones {
    return {
        async enviarPosiciones(entrada) {
            const { datos, respuesta } = await resolver(cliente.POST("/api/v1/campo/posiciones", { body: entrada, headers: conApp(app) }));
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
export function crearSubidorApi(cliente: ClienteIris, app?: IdentidadApp): Subidor {
    return {
        async crear(entrada) {
            const { datos } = await resolver(
                cliente.POST("/api/v1/adjuntos", {
                    body: entrada,
                    params: { header: { "Idempotency-Key": entrada.id } },
                    headers: conApp(app),
                }),
            );
            return datos;
        },
        async consultarOffset(id) {
            const { respuesta } = await resolver(
                cliente.HEAD("/api/v1/adjuntos/{id}/subida", {
                    params: { path: { id }, header: { "Tus-Resumable": VERSION_TUS } },
                    headers: conApp(app, { "Cache-Control": "no-store" }),
                }),
            );
            return offsetDe(respuesta);
        },
        async enviarParte(id, offset, parte) {
            const { respuesta } = await resolver(
                cliente.PATCH("/api/v1/adjuntos/{id}/subida", {
                    params: { path: { id }, header: { "Tus-Resumable": VERSION_TUS, "Upload-Offset": offset } },
                    headers: conApp(app, { "Content-Type": "application/offset+octet-stream" }),
                    // El OpenAPI declara el cuerpo como texto binario: van los bytes tal cual.
                    body: "",
                    bodySerializer: () => parte as Uint8Array<ArrayBuffer>,
                }),
            );
            return offsetDe(respuesta);
        },
    };
}
