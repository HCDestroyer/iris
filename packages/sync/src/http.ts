// Adaptadores con `fetch` (estándar WHATWG: navegador, Node 22 y React Native). Provisionales
// hasta que `packages/contratos` publique los endpoints de campo: entonces `Transporte` se
// implementa con el cliente `openapi-fetch` de `@iris/contratos` y este archivo se reduce al tus.
import type {
    EntradaCrearAdjunto,
    PaqueteDia,
    PeticionLote,
    Problema,
    RespuestaCambios,
    RespuestaCrearAdjunto,
    RespuestaLote,
} from "./contrato-sync";
import { ErrorTransporte } from "./errores";
import type { RespuestaApi, Subidor, Transporte } from "./transporte";

type Cabeceras = Record<string, string>;

export interface OpcionesHttp {
    /** Origen de la API, sin `/api/v1` (vacío = mismo origen). */
    baseUrl?: string;
    fetch?: typeof fetch;
    /** Cabeceras por petición (p. ej. `X-CSRF-Token`, `X-Iris-Empresa`). */
    cabeceras?: () => Cabeceras | Promise<Cabeceras>;
}

const VERSION_TUS = "1.0.0";

async function leerProblema(respuesta: Response): Promise<Problema | null> {
    try {
        const cuerpo: unknown = await respuesta.json();
        if (cuerpo && typeof cuerpo === "object" && "type" in cuerpo && "status" in cuerpo) {
            return cuerpo as Problema;
        }
    } catch {
        // Sin cuerpo JSON: solo queda el status.
    }
    return null;
}

function horaDe(respuesta: Response): number | null {
    const fecha = respuesta.headers.get("Date");
    const ms = fecha ? Date.parse(fecha) : Number.NaN;
    return Number.isNaN(ms) ? null : ms;
}

class ClienteHttp {
    private readonly baseUrl: string;
    private readonly fetchFn: typeof fetch;
    private readonly cabeceras: () => Cabeceras | Promise<Cabeceras>;

    constructor(opciones: OpcionesHttp) {
        this.baseUrl = opciones.baseUrl ?? "";
        this.fetchFn = opciones.fetch ?? ((entrada, init) => globalThis.fetch(entrada, init));
        this.cabeceras = opciones.cabeceras ?? (() => ({}));
    }

    url(ruta: string): string {
        return /^https?:\/\//.test(ruta) ? ruta : `${this.baseUrl}${ruta}`;
    }

    async pedir(ruta: string, init: RequestInit): Promise<Response> {
        const cabeceras = new Headers(init.headers);
        for (const [k, v] of Object.entries(await this.cabeceras())) {
            cabeceras.set(k, v);
        }
        cabeceras.set("X-Iris-Canal", "campo");
        let respuesta: Response;
        try {
            respuesta = await this.fetchFn(this.url(ruta), { credentials: "include", ...init, headers: cabeceras });
        } catch (e) {
            throw new ErrorTransporte("red", e instanceof Error ? e.message : "Sin conexión");
        }
        if (!respuesta.ok) {
            const problema = await leerProblema(respuesta);
            throw new ErrorTransporte("http", problema?.title ?? `HTTP ${respuesta.status}`, respuesta.status, problema);
        }
        return respuesta;
    }

    async json<T>(ruta: string, init: RequestInit): Promise<RespuestaApi<T>> {
        const respuesta = await this.pedir(ruta, init);
        let datos: T;
        try {
            datos = (await respuesta.json()) as T;
        } catch (e) {
            throw new ErrorTransporte("red", e instanceof Error ? e.message : "Respuesta cortada");
        }
        return { datos, horaServidor: horaDe(respuesta) };
    }
}

export function crearTransporteHttp(opciones: OpcionesHttp = {}): Transporte {
    const http = new ClienteHttp(opciones);
    return {
        enviarLote: (peticion: PeticionLote) =>
            http.json<RespuestaLote>("/api/v1/sync/lote", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(peticion),
            }),
        obtenerPaquete: (fecha: string) =>
            http.json<PaqueteDia>(`/api/v1/campo/paquete?fecha=${encodeURIComponent(fecha)}`, { method: "GET" }),
        obtenerCambios: (cursor: string) =>
            http.json<RespuestaCambios>(`/api/v1/campo/cambios?cursor=${encodeURIComponent(cursor)}`, { method: "GET" }),
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

/** Subidor tus 1.0 (núcleo del protocolo: HEAD y PATCH) sobre `POST /api/v1/adjuntos`. */
export function crearSubidorTus(opciones: OpcionesHttp = {}): Subidor {
    const http = new ClienteHttp(opciones);
    return {
        crear: async (entrada: EntradaCrearAdjunto) =>
            (
                await http.json<RespuestaCrearAdjunto>("/api/v1/adjuntos", {
                    method: "POST",
                    headers: { "Content-Type": "application/json", "Idempotency-Key": entrada.id },
                    body: JSON.stringify(entrada),
                })
            ).datos,
        consultarOffset: async (urlSubida: string) =>
            offsetDe(
                await http.pedir(urlSubida, {
                    method: "HEAD",
                    headers: { "Tus-Resumable": VERSION_TUS, "Cache-Control": "no-store" },
                }),
            ),
        enviarParte: async (urlSubida: string, offset: number, parte: Uint8Array) =>
            offsetDe(
                await http.pedir(urlSubida, {
                    method: "PATCH",
                    headers: {
                        "Tus-Resumable": VERSION_TUS,
                        "Upload-Offset": String(offset),
                        "Content-Type": "application/offset+octet-stream",
                    },
                    body: parte as Uint8Array<ArrayBuffer>,
                }),
            ),
    };
}
