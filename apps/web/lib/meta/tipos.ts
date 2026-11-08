import type { Schema } from "@iris/contratos";
import type { Permiso } from "@/lib/menu";

/*
 * Conexión con Meta, WhatsApp por QR (coexistencia) y préstamo de números
 * (contratos/conversaciones.md §15). Solo alias de `packages/contratos`.
 */

/** Permiso de §15.1, solo del rol `admin`. */
export const PERMISO_CONEXION_META: Permiso = "mensajeria.conexion_meta";

export type ConexionMeta = Schema<"ConexionMeta">;
export type EntradaConexionMeta = Schema<"EntradaConexionMeta">;
export type RegistroMeta = Schema<"RegistroQR">;
export type EntradaCanalQr = Schema<"EntradaCanalQR">;
export type ModoConexion = ConexionMeta["modo"];
export type ClaveComprobacion = Schema<"ComprobacionMeta">["clave"];
export type EstadoComprobacion = Schema<"ComprobacionMeta">["estado"];
export type MotivoQr = NonNullable<RegistroMeta["motivo"]>;

/** Un número con lo de §15 (tipo de conexión, historial, préstamo). */
export type CanalMeta = Schema<"Canal">;
export type ConexionCanal = CanalMeta["conexion"];
export type Sincronizacion = Schema<"SincronizacionCanal">;
export type EstadoSincronizacion = Schema<"ParteSincronizacion">["estado"];

export type Prestamo = Schema<"Prestamo">;
export type OpcionesPrestamo = Schema<"OpcionesPrestamo">;

/** `MensajeConversacion.origen`: `null` = lo mandó Iris. */
export type OrigenMensaje = Schema<"MensajeConversacion">["origen"];
