import type { Schema } from "@iris/contratos";
import type { Permiso } from "@/lib/menu";

// Nombres cortos de los tipos generados del Servidor de IA (contratos/voz.md §4.8, §4.11 y §4.12).
// Solo alias: la fuente es packages/contratos.

export const PERMISO_VER: Permiso = "servidor_ia.ver";
export const PERMISO_VER_USO: Permiso = "servidor_ia.ver_uso";
export const PERMISO_ADMINISTRAR: Permiso = "servidor_ia.administrar";

/** La tarjeta del servidor (`GET /voz/motores`, §4.8). */
export type Motor = Schema<"MotorIA">;
export type EstadoMotor = Motor["estado"];
export type Paquete = Schema<"PaqueteMotor">;
/** Por qué no son compatibles: `iris_antiguo` = hay que actualizar Iris; cualquier otro, el servidor. */
export type MotivoIncompatible = NonNullable<Paquete["motivoIncompatible"]>;
export type PasoPrueba = Schema<"PasoPrueba">;
/** El resultado de «Probar»: llega en `ultimaPrueba` de la tarjeta. */
export type ResultadoPrueba = NonNullable<Schema<"PruebaMotor">>;
/** `POST /voz/motores/enlazar` → 201. El código se muestra una sola vez. */
export type EnlaceCreado = Schema<"EnlaceMotor">;

export type Rango = "ahora" | "1h" | "24h" | "7d";
export const RANGOS: readonly Rango[] = ["ahora", "1h", "24h", "7d"];

/** `GET /servidor-ia/tablero?rango=` (§4.12). */
export type Tablero = Schema<"TableroIA">;
export type EstadoTablero = Schema<"EstadoTableroIA">;
export type Gpu = Schema<"GPUTablero">;
export type ModeloInstalado = Schema<"ModeloTablero">;
/** Un punto de las series; un valor `null` = en ese instante no hubo lectura (o no hay tarjeta gráfica). */
export type PuntoSerie = Schema<"PuntoSerieIA">;
export type UsoEmpresa = Schema<"UsoEmpresaIA">;
export type ErrorReciente = Schema<"ErrorRecienteIA">;
export type MotivoError = ErrorReciente["motivo"];
export type AvisoServidor = Schema<"AvisoServidorIA">;
export type TipoAviso = AvisoServidor["tipo"];

/** `GET /servidor-ia/umbrales` y `POST /servidor-ia/umbrales/actualizar` (§4.12). */
export type Umbrales = Schema<"UmbralesIA">;
export type EntradaUmbrales = Schema<"EntradaUmbralesIA">;

/** Lo que devuelven los ajustes del asistente de cada empresa (§4.11). */
export type ServidorIaEnAsistente = Schema<"ServidorIAAsistente">;
