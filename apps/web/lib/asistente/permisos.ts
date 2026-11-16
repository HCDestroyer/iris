import type { Permiso } from "@/lib/menu";

// Permisos del asistente de IA (contratos/conversaciones.md §16.5).
/** Ver, pedir, usar y calificar sugerencias y el resumen (quienes responden). */
export const PERMISO_SUGERENCIAS: Permiso = "conversaciones.sugerencias";
/** Ajustes, probar, modelos y uso (solo admin). */
export const PERMISO_CONFIGURAR_ASISTENTE: Permiso = "mensajeria.asistente_configurar";

// Fase 3 (provisionales: aún no están en el enum `Permiso` del OpenAPI; al mergear los contratos
// de `agente/i1-asistente-3` y `agente/i1-pagos-b3` se quita el `as`).
/** Cambiar la categoría de una conversación (§17). */
export const PERMISO_CATEGORIZAR = "conversaciones.categorizar" as Permiso;
/** Aprobar o rechazar la boleta de la conversación (§6.4, B3). */
export const PERMISO_APROBAR_BOLETA = "boletas.aprobar" as Permiso;
export const PERMISO_RECHAZAR_BOLETA = "boletas.rechazar" as Permiso;
/** «Abrir en Argos» y «Vincular mi usuario de Argos» (autologin Iris → Argos). */
export const PERMISO_ABRIR_ARGOS = "integraciones.abrir_argos" as Permiso;
