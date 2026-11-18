import { urlApi } from "@/lib/proxy";
import type { DatosPublicos } from "./titular";

/** Segundos que el servidor web guarda los datos del titular: un rastreador insistente no llega a la API en cada visita. */
export const CACHE_SEGUNDOS = 60;

/**
 * Lee los datos públicos del titular desde el servidor (nunca desde el navegador: el texto va en
 * el HTML). Si la API no responde, devuelve `null` y la página se sirve igual, con el texto
 * genérico: la URL que Meta revisa nunca responde con un error por esto.
 */
export async function leerDatosPublicos(): Promise<DatosPublicos | null> {
    try {
        const r = await fetch(`${urlApi()}/api/v1/publico/datos-legales`, {
            headers: { Accept: "application/json" },
            next: { revalidate: CACHE_SEGUNDOS },
            signal: AbortSignal.timeout(4000),
        });
        if (!r.ok) {
            return null;
        }
        const datos: unknown = await r.json();
        return typeof datos === "object" && datos !== null ? (datos as DatosPublicos) : null;
    } catch {
        return null;
    }
}
