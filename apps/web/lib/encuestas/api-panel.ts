import { api, resolver } from "../api/cliente";
import type { Encuesta } from "./presentacion";

/** La encuesta del ticket o de la orden (una por entidad), o `null` si todavía no hay. */
export async function encuestaDe(filtro: { ticketId: number } | { ordenId: number }, signal?: AbortSignal): Promise<Encuesta | null> {
    const pagina = await resolver(api().GET("/api/v1/encuestas", { params: { query: { ...filtro, limit: 1 } }, signal }));
    return pagina.data?.[0] ?? null;
}

/** Enlace para compartir una encuesta pendiente. */
export async function enlaceDe(encuesta: Encuesta): Promise<string> {
    if (!encuesta.enlace) throw new Error("El enlace no se puede volver a mostrar");
    return encuesta.enlace;
}
