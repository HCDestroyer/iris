import type { Schema } from "@iris/contratos";

type Problema = Schema<"Problema">;
type ErrorCampo = Schema<"ErrorCampo">;

const PREFIJO_TIPO = "urn:iris:problema:";

/**
 * Error de la API normalizado desde `application/problem+json` (RFC 9457). `codigo` es la parte
 * final de `type` (`urn:iris:problema:<codigo>`), p. ej. `demasiados_intentos` o `mfa_bloqueado`.
 * Sin respuesta (red caída, proxy sin API) el código es `sin_conexion` y el estado 0.
 */
export class ErrorApi extends Error {
    readonly estado: number;
    readonly codigo: string;
    readonly titulo: string;
    readonly detalle: string | undefined;
    readonly errores: ErrorCampo[];
    /** Segundos de `Retry-After`, si vino (429). */
    readonly reintentarEn: number | undefined;

    constructor(datos: {
        estado: number;
        codigo: string;
        titulo: string;
        detalle?: string | undefined;
        errores?: ErrorCampo[] | null | undefined;
        reintentarEn?: number | undefined;
    }) {
        super(datos.detalle ? `${datos.titulo}: ${datos.detalle}` : datos.titulo);
        this.name = "ErrorApi";
        this.estado = datos.estado;
        this.codigo = datos.codigo;
        this.titulo = datos.titulo;
        this.detalle = datos.detalle;
        this.errores = datos.errores ?? [];
        this.reintentarEn = datos.reintentarEn;
    }

    /** Mensaje del campo (`correo`, `contrasena`…) en `errors[]`; acepta `body.correo`. */
    errorDeCampo(campo: string): string | undefined {
        return this.errores.find((e) => e.field === campo || e.field === `body.${campo}`)?.message;
    }
}

function esProblema(valor: unknown): valor is Problema {
    if (typeof valor !== "object" || valor === null) {
        return false;
    }
    const v = valor as Record<string, unknown>;
    return typeof v.type === "string" && typeof v.title === "string" && typeof v.status === "number";
}

/** `Retry-After` en segundos (entero o fecha HTTP); `undefined` si no vino o no se entiende. */
export function leerRetryAfter(cabeceras: Headers, ahora: number = Date.now()): number | undefined {
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

const TITULOS_POR_ESTADO: Record<number, string> = {
    400: "Petición inválida",
    401: "No autenticado",
    403: "Sin permiso",
    404: "No encontrado",
    409: "Conflicto",
    422: "Datos inválidos",
    429: "Demasiados intentos",
    500: "Error del servidor",
    502: "La API no responde",
    503: "Servicio no disponible",
};

/** Convierte la respuesta de error (cuerpo ya leído por openapi-fetch) en `ErrorApi`. */
export function errorDesdeRespuesta(respuesta: Response, cuerpo: unknown): ErrorApi {
    const reintentarEn = leerRetryAfter(respuesta.headers);
    if (esProblema(cuerpo)) {
        return new ErrorApi({
            estado: cuerpo.status,
            codigo: cuerpo.type.startsWith(PREFIJO_TIPO) ? cuerpo.type.slice(PREFIJO_TIPO.length) : cuerpo.type,
            titulo: cuerpo.title,
            detalle: cuerpo.detail,
            errores: cuerpo.errors,
            reintentarEn,
        });
    }
    return new ErrorApi({
        estado: respuesta.status,
        codigo: `http_${respuesta.status}`,
        titulo: TITULOS_POR_ESTADO[respuesta.status] ?? `Error ${respuesta.status}`,
        reintentarEn,
    });
}

/** Error de red (no hubo respuesta). */
export function errorSinConexion(causa?: unknown): ErrorApi {
    const error = new ErrorApi({
        estado: 0,
        codigo: "sin_conexion",
        titulo: "Sin conexión con Iris",
        detalle: "Revisa tu conexión e inténtalo de nuevo.",
    });
    if (causa !== undefined) {
        Object.defineProperty(error, "cause", { value: causa });
    }
    return error;
}

/** Mensaje para mostrar al usuario: qué pasó, sin códigos crudos. */
export function mensajeDeError(error: unknown): { titulo: string; detalle: string | undefined } {
    if (error instanceof ErrorApi) {
        return { titulo: error.titulo, detalle: error.detalle };
    }
    return { titulo: "Algo salió mal", detalle: "Inténtalo de nuevo en un momento." };
}
