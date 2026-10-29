// Error de la API normalizado desde problem+json (RFC 9457), como el del panel: `codigo` es la
// parte final de `type` (`urn:iris:problema:<codigo>`). Sin respuesta: `sin_conexion`, estado 0.
import type { Schema } from "@iris/contratos";
import { PREFIJO_PROBLEMA } from "@iris/sync";

type ErrorCampo = Schema<"ErrorCampo">;

export class ErrorApi extends Error {
    readonly estado: number;
    readonly codigo: string;
    readonly titulo: string;
    readonly detalle: string | undefined;
    readonly errores: ErrorCampo[];
    /** Segundos de `Retry-After` (429). */
    readonly reintentarEn: number | undefined;

    constructor(d: { estado: number; codigo: string; titulo: string; detalle?: string | undefined; errores?: ErrorCampo[] | null | undefined; reintentarEn?: number | undefined }) {
        super(d.detalle ? `${d.titulo}: ${d.detalle}` : d.titulo);
        this.name = "ErrorApi";
        this.estado = d.estado;
        this.codigo = d.codigo;
        this.titulo = d.titulo;
        this.detalle = d.detalle;
        this.errores = d.errores ?? [];
        this.reintentarEn = d.reintentarEn;
    }

    errorDeCampo(campo: string): string | undefined {
        return this.errores.find((e) => e.field === campo || e.field === `body.${campo}`)?.message;
    }
}

/** `Retry-After` en segundos (entero o fecha HTTP). */
export function leerRetryAfter(cabeceras: Headers, ahora = Date.now()): number | undefined {
    const valor = cabeceras.get("Retry-After")?.trim();
    if (!valor) {
        return undefined;
    }
    if (/^\d+$/.test(valor)) {
        return Number(valor);
    }
    const fecha = Date.parse(valor);
    return Number.isNaN(fecha) ? undefined : Math.max(0, Math.ceil((fecha - ahora) / 1000));
}

function esProblema(v: unknown): v is Schema<"Problema"> {
    const p = v as Record<string, unknown> | null;
    return typeof v === "object" && p !== null && typeof p.type === "string" && typeof p.title === "string" && typeof p.status === "number";
}

export function errorDesdeRespuesta(respuesta: Response, cuerpo: unknown): ErrorApi {
    const reintentarEn = leerRetryAfter(respuesta.headers);
    if (esProblema(cuerpo)) {
        return new ErrorApi({
            estado: respuesta.status,
            codigo: cuerpo.type.startsWith(PREFIJO_PROBLEMA) ? cuerpo.type.slice(PREFIJO_PROBLEMA.length) : cuerpo.type,
            titulo: cuerpo.title,
            detalle: cuerpo.detail ?? undefined,
            errores: cuerpo.errors,
            reintentarEn,
        });
    }
    return new ErrorApi({ estado: respuesta.status, codigo: `http_${respuesta.status}`, titulo: `Error ${respuesta.status} de la API`, reintentarEn });
}

export function errorSinConexion(causa: unknown): ErrorApi {
    return new ErrorApi({ estado: 0, codigo: "sin_conexion", titulo: "Sin conexión", detalle: causa instanceof Error ? causa.message : undefined });
}

type Resultado<D> = { data?: D; error?: unknown; response: Response };

/** Espera una llamada del cliente: `data` o `ErrorApi`. */
export async function resolver<D>(llamada: Promise<Resultado<D>>): Promise<D> {
    let r: Resultado<D>;
    try {
        r = await llamada;
    } catch (e) {
        throw errorSinConexion(e);
    }
    if (!r.response.ok) {
        throw errorDesdeRespuesta(r.response, r.error);
    }
    return r.data as D;
}

/** «Espera 45 s» / «espera 3 min» del bloqueo por 429. */
export function formatearEspera(segundos: number): string {
    const s = Math.max(0, Math.ceil(segundos));
    return s < 60 ? `${s} s` : `${Math.ceil(s / 60)} min`;
}
