import type { Schema } from "@iris/contratos";
import type { Permiso } from "@/lib/menu";

/*
 * Centralita Issabel, etapa E4a (`.claude/integration/contratos/voz.md` §3.12): conexión de solo
 * lectura con el AMI, extensiones de cada usuario y sus llamadas (las mismas `Llamada` de la voz).
 */

export type Centralita = Schema<"Centralita">;
export type PruebaCentralita = Schema<"PruebaCentralita">;
export type ExtensionCentralita = Schema<"Extension">;
export type MiExtension = Schema<"MiExtension">;
export type EntradaCrearCentralita = Schema<"EntradaCrearCentralita">;
export type EntradaActualizarCentralita = Schema<"EntradaActualizarCentralita">;
export type EntradaProbarCentralita = Schema<"EntradaProbarCentralita">;
export type ListaLlamadasCentralita = Schema<"ListaLlamadasCentralita">;
export type CentralitaDeLlamada = Schema<"CentralitaDeLlamada">;

export const PERMISO_CONFIGURAR_CENTRALITA = "centralita.configurar" satisfies Permiso;
export const PERMISO_ASIGNAR_EXTENSION = "centralita.asignar_extension" satisfies Permiso;

/** El AMI cifrado (TLS). El 5038 sin cifrar no se acepta. */
export const PUERTO_AMI = 5039;
