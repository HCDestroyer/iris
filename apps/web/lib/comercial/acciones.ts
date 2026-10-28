import type { Permiso } from "../menu";
import type { ProspectoDetalle } from "./presentacion";

export type AccionProspecto = "contactar" | "asignar" | "ganar" | "perder" | "reabrir";

/** Permiso de cada acción del prospecto (mismo nombre que la acción de la API). */
export const PERMISO_ACCION: Record<AccionProspecto, Permiso> = {
    contactar: "prospectos.contactar",
    asignar: "prospectos.asignar",
    ganar: "prospectos.ganar",
    perder: "prospectos.perder",
    reabrir: "prospectos.reabrir",
};

/**
 * Qué botones van: los que la API marca en `acciones` (estado del embudo) y, de esos, los que el
 * usuario tiene en sus permisos de `GET /yo`. Asignar no está en `acciones` (pedido a I2): va con
 * el permiso y solo si el prospecto no está ganado ni perdido.
 */
export function accionesVisibles(d: Pick<ProspectoDetalle, "acciones" | "estado">, puede: (p: readonly Permiso[]) => boolean): AccionProspecto[] {
    const a = d.acciones;
    const lista: AccionProspecto[] = [];
    if (a.ganar && puede([PERMISO_ACCION.ganar])) lista.push("ganar");
    if (a.contactar && puede([PERMISO_ACCION.contactar])) lista.push("contactar");
    if (puede([PERMISO_ACCION.asignar]) && d.estado !== "ganado" && d.estado !== "perdido") lista.push("asignar");
    if (a.reabrir && puede([PERMISO_ACCION.reabrir])) lista.push("reabrir");
    if (a.perder && puede([PERMISO_ACCION.perder])) lista.push("perder");
    return lista;
}
