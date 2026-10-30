import type { Permiso } from "@/lib/menu";

/** Quien administra fichas de técnicos (alta, edición, estado, turnos o ausencias) ve «Técnicos». */
export const PERMISOS_TECNICOS: readonly Permiso[] = [
    "agenda.crear_tecnico",
    "agenda.editar_tecnico",
    "agenda.activar_tecnico",
    "agenda.desactivar_tecnico",
    "agenda.fijar_turnos",
    "agenda.registrar_ausencia",
    "agenda.anular_ausencia",
];

/** Administrar cuadrillas. */
export const PERMISOS_CUADRILLAS: readonly Permiso[] = ["agenda.crear_cuadrilla", "agenda.editar_cuadrilla", "agenda.activar_cuadrilla", "agenda.desactivar_cuadrilla"];

/** Administrar zonas. */
export const PERMISOS_ZONAS: readonly Permiso[] = ["agenda.crear_zona", "agenda.editar_zona", "agenda.activar_zona", "agenda.desactivar_zona"];
