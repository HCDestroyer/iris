import type { Permiso } from "@/lib/menu";

// Permisos del asistente de IA (contratos/conversaciones.md §16.5).
/** Ver, pedir, usar y calificar sugerencias y el resumen (quienes responden). */
export const PERMISO_SUGERENCIAS: Permiso = "conversaciones.sugerencias";
/** Ajustes, probar, modelos y uso (solo admin). */
export const PERMISO_CONFIGURAR_ASISTENTE: Permiso = "mensajeria.asistente_configurar";

// Fase 3.
/**
 * Cambiar la categoría de una conversación (§17). Provisional: aún no está en el enum `Permiso`
 * del OpenAPI; al mergear el contrato de `agente/i1-asistente-3` se quita el `as`.
 */
export const PERMISO_CATEGORIZAR = "conversaciones.categorizar" as Permiso;
/** Aprobar o rechazar la boleta de la conversación (§6.4, B3). */
export const PERMISO_APROBAR_BOLETA: Permiso = "boletas.aprobar";
export const PERMISO_RECHAZAR_BOLETA: Permiso = "boletas.rechazar";
/** «Abrir en Argos» y «Vincular mi usuario de Argos» (autologin Iris → Argos). */
export const PERMISO_ABRIR_ARGOS: Permiso = "integraciones.abrir_argos";
