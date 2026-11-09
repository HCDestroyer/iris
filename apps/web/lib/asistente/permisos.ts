import type { Permiso } from "@/lib/menu";

// Permisos del asistente de IA (contratos/conversaciones.md §16.5).
// PROVISIONAL: el `as` se quita al mergear agente/i1-asistente (enum del OpenAPI).
/** Ver, pedir, usar y calificar sugerencias y el resumen (quienes responden). */
export const PERMISO_SUGERENCIAS = "conversaciones.sugerencias" as Permiso;
/** Ajustes, probar, modelos y uso (solo admin). */
export const PERMISO_CONFIGURAR_ASISTENTE = "mensajeria.asistente_configurar" as Permiso;
