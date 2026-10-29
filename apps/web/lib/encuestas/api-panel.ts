import type { Schema } from "@iris/contratos";
import { api, resolver } from "../api/cliente";
import type { Encuesta } from "./presentacion";

/** La encuesta del ticket o de la orden (una por entidad), o `null` si todavía no hay. */
export async function encuestaDe(filtro: { ticketId: number } | { ordenId: number }, signal?: AbortSignal): Promise<Encuesta | null> {
    const pagina = await resolver(api().GET("/api/v1/encuestas", { params: { query: { ...filtro, limit: 1 } }, signal }));
    return pagina.data?.[0] ?? null;
}

/**
 * Enlace para compartir una encuesta pendiente de canal `enlace` (`encuestas.compartir`, acción
 * auditada). No se guarda: se copia y se muestra solo en el diálogo.
 */
export function pedirEnlace(id: number): Promise<Schema<"EnlaceEncuesta">> {
    return resolver(api().POST("/api/v1/encuestas/{id}/enlace", { params: { path: { id } } }));
}

