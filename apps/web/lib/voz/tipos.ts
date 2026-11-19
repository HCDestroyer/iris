import type { Schema } from "@iris/contratos";
import type { Permiso } from "@/lib/menu";

/*
 * Voz, etapa E1: personas contestan llamadas de WhatsApp en el panel
 * (`.claude/integration/contratos/voz.md` §1–§3). Alias de los tipos de `packages/contratos`.
 */

export type Llamada = Schema<"Llamada">;
export type EstadoLlamada = Llamada["estado"];
/** La llamada como viaja dentro de un mensaje del hilo (`tipo: "llamada"`). */
export type LlamadaEnMensaje = Schema<"LlamadaEnMensaje">;
/** Respuesta de `reclamar`: la llamada, la oferta SDP, los servidores ICE y hasta cuándo es mía. */
export type LlamadaReclamada = Schema<"LlamadaReclamada">;
export type RecibirLlamadas = Schema<"RecibirLlamadas">;
export type DevolucionLlamada = Schema<"DevolucionLlamada">;
export type ResultadoLlamada = NonNullable<Llamada["resultado"]>;

export type LlamadasCanal = Schema<"LlamadasCanal">;
export type EntradaLlamadasCanal = Schema<"EntradaLlamadasCanal">;
export type RequisitosLlamadas = Schema<"RequisitosLlamadas">;
export type RequisitoLlamadas = Schema<"RequisitoLlamadas">;
export type PermisoLlamada = Schema<"PermisoLlamada">;
export type AjustesVoz = Schema<"AjustesVoz">;
export type EntradaAjustesVoz = Schema<"EntradaAjustesVoz">;

export const PERMISO_VER_LLAMADAS = "llamadas.ver" satisfies Permiso;
export const PERMISO_CONTESTAR = "llamadas.contestar" satisfies Permiso;
export const PERMISO_ANOTAR = "llamadas.anotar" satisfies Permiso;
export const PERMISO_DESCARTAR_DEVOLUCION = "llamadas.descartar_devolucion" satisfies Permiso;
export const PERMISO_CONFIGURAR_LLAMADAS = "llamadas.configurar" satisfies Permiso;
export const PERMISO_LLAMAR = "llamadas.llamar" satisfies Permiso;
export const PERMISO_DESBLOQUEAR_NUMERO = "llamadas.desbloquear_numero" satisfies Permiso;
export const PERMISO_SOLICITAR_PERMISO = "llamadas.solicitar_permiso" satisfies Permiso;
export const PERMISO_CONFIGURAR_VOZ = "voz.configurar" satisfies Permiso;
