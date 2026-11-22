/*
 * Los errores que el teléfono distingue, por su forma (no por su clase): el panel y la app de campo
 * tienen cada uno su cliente HTTP (`ErrorApi` del panel, el de `/campo`) y su forma de pedir el
 * micrófono, y el motor no depende de ninguno.
 */

/** Un problem+json de la API ya leído: estado HTTP, código (`urn:iris:problema:<codigo>`) y `Retry-After`. */
export type ErrorDeApi = {
    readonly estado: number;
    readonly codigo: string;
    readonly titulo?: string;
    readonly detalle?: string | undefined;
    /** Segundos de `Retry-After`, si vino (429). */
    readonly reintentarEn?: number | undefined;
};

export function esErrorDeApi(e: unknown): e is ErrorDeApi {
    if (typeof e !== "object" || e === null) return false;
    const x = e as { estado?: unknown; codigo?: unknown };
    return typeof x.estado === "number" && typeof x.codigo === "string";
}

export type EstadoMicrofonoBloqueado = "denegado" | "sin_microfono" | "no_disponible";
const ESTADOS_MICROFONO: readonly string[] = ["denegado", "sin_microfono", "no_disponible"];

/** El micrófono no se pudo usar (`ErrorMicrofono` del panel o de la app): por qué. */
export function esErrorMicrofono(e: unknown): e is { readonly estado: EstadoMicrofonoBloqueado } {
    if (typeof e !== "object" || e === null) return false;
    const estado = (e as { estado?: unknown }).estado;
    return typeof estado === "string" && ESTADOS_MICROFONO.includes(estado);
}
