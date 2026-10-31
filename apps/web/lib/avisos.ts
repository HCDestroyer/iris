import type { Schema } from "@iris/contratos";

/*
 * Textos de los avisos emergentes tras una acción («Orden OT-000042 asignada a Luis Pérez»):
 * cortos, con el número del registro y, si aplica, a quién. Los errores no van aquí: se quedan en
 * la pantalla, junto a su campo o en su aviso con «Reintentar».
 */

type ConNombre = { id: number; nombre: string };

const nombreDe = (lista: readonly ConNombre[] | null | undefined, id: number | null | undefined) =>
    id == null ? undefined : lista?.find((u) => u.id === id)?.nombre;

export type AccionTicketAviso = "responder" | "nota_interna" | "asignar" | "escalar" | "poner_en_espera" | "reanudar" | "resolver" | "reabrir" | "cerrar" | "fusionar";

export function avisoTicket(accion: AccionTicketAviso, t: Pick<Schema<"Ticket">, "numero" | "asignadoA">, usuarios?: readonly ConNombre[] | null): string {
    const n = `Ticket ${t.numero}`;
    switch (accion) {
        case "responder":
            return `Respuesta enviada en el ticket ${t.numero}`;
        case "nota_interna":
            return `Nota interna agregada al ticket ${t.numero}`;
        case "asignar": {
            const a = nombreDe(usuarios, t.asignadoA);
            return t.asignadoA == null ? `${n} sin asignar` : `${n} asignado${a ? ` a ${a}` : ""}`;
        }
        case "escalar":
            return `${n} escalado`;
        case "poner_en_espera":
            return `${n} en espera`;
        case "reanudar":
            return `${n} reanudado`;
        case "resolver":
            return `${n} resuelto`;
        case "reabrir":
            return `${n} reabierto`;
        case "cerrar":
            return `${n} cerrado`;
        case "fusionar":
            return `${n} fusionado`;
    }
}

export type AccionOrdenAviso = "asignar" | "programar" | "reprogramar" | "pausar" | "reanudar" | "cancelar" | "reabrir";

export function avisoOrden(accion: AccionOrdenAviso, o: Pick<Schema<"Orden">, "numero" | "tecnicoId">, usuarios?: readonly ConNombre[] | null): string {
    const n = `Orden ${o.numero}`;
    switch (accion) {
        case "asignar": {
            const t = nombreDe(usuarios, o.tecnicoId);
            return o.tecnicoId == null ? `${n} sin técnico` : `${n} asignada${t ? ` a ${t}` : ""}`;
        }
        case "programar":
            return `${n} programada`;
        case "reprogramar":
            return `${n} reprogramada`;
        case "pausar":
            return `${n} pausada`;
        case "reanudar":
            return `${n} reanudada`;
        case "cancelar":
            return `${n} cancelada`;
        case "reabrir":
            return `${n} reabierta`;
    }
}

export type AccionProspectoAviso = "contactar" | "asignar" | "ganar" | "perder" | "reabrir";

export function avisoProspecto(accion: AccionProspectoAviso, numero: string): string {
    const n = `Prospecto ${numero}`;
    switch (accion) {
        case "contactar":
            return `${n} marcado como contactado`;
        case "asignar":
            return `${n}: vendedor asignado`;
        case "ganar":
            return `${n} ganado`;
        case "perder":
            return `${n} marcado como perdido`;
        case "reabrir":
            return `${n} reabierto`;
    }
}

/** Activar / desactivar algo con nombre (zona, cuadrilla, técnico, rol, buzón…), sin adivinar el género. */
export function avisoEstado(nombre: string, activo: boolean): string {
    return `Se ${activo ? "activó" : "desactivó"} «${nombre}»`;
}
