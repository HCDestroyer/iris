import type { Schema } from "@iris/contratos";

/* Números de WhatsApp: alta, credenciales y conexión (contratos/conversaciones.md §1.1). */

/** Número con sus campos de conexión (`GET /canales`, `GET /canales/{id}`). */
export type Canal = Schema<"Canal">;

/** `POST /canales/{id}/probar`. */
export type PruebaCanal = Schema<"PruebaCanal">;

/** Resultado de una prueba. */
export type EstadoPrueba = PruebaCanal["estado"];

/** Columna «Conexión»: la calcula el servidor y el panel la muestra tal cual. */
export type Conexion = Canal["conexion"];

export type CalidadNumero = NonNullable<PruebaCanal["calidad"]>;
