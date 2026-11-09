import type { Permiso } from "@/lib/menu";

// Permisos del asistente de IA (contratos/conversaciones.md §16.5).
/** Ver, pedir, usar y calificar sugerencias y el resumen (quienes responden). */
export const PERMISO_SUGERENCIAS: Permiso = "conversaciones.sugerencias";
/** Ajustes, probar, modelos y uso (solo admin). */
export const PERMISO_CONFIGURAR_ASISTENTE: Permiso = "mensajeria.asistente_configurar";
