// Punto de entrada de @iris/contratos. Lo único escrito a mano en este paquete:
// `esquema.ts` lo genera openapi-typescript desde openapi.json (`pnpm contratos:build`).
import createClient, { type ClientOptions } from "openapi-fetch";
import type { components, operations, paths } from "./esquema";

export type { components, operations, paths };

/** Esquema con nombre de `components.schemas`, por ejemplo `Schema<"Salud">`. */
export type Schema<N extends keyof components["schemas"]> = components["schemas"][N];

/** Cliente HTTP tipado de la API de Iris. `baseUrl` es el origen, sin `/api/v1`. */
export function crearCliente(opciones: ClientOptions = {}) {
    return createClient<paths>({ credentials: "include", ...opciones });
}

export type ClienteIris = ReturnType<typeof crearCliente>;
