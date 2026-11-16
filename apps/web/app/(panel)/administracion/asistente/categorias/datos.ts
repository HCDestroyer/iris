import type { Schema } from "@iris/contratos";
import { api, resolver } from "@/lib/api/cliente";
import { leerColas } from "@/lib/conversaciones/api-equipo";
import type { Cola } from "@/lib/conversaciones/tipos";

export type ManualBase = Pick<Schema<"ManualResumen">, "id" | "titulo" | "activo">;
export type Apoyo = { colas: Cola[]; manuales: ManualBase[] };

/** Colas y manuales para nombrar la cola y los manuales base de cada categoría. */
export async function leerApoyo(signal?: AbortSignal): Promise<Apoyo> {
    const [colas, manuales] = await Promise.all([leerColas(signal), resolver(api().GET("/api/v1/mensajeria/manuales", { params: { query: {} }, signal }))]);
    return { colas, manuales: (manuales.data ?? []).map((m) => ({ id: m.id, titulo: m.titulo, activo: m.activo })) };
}
