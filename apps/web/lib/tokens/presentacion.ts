import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import { Ban, CircleCheck, Hourglass, type LucideIcon } from "lucide-react";

export type TokenAPI = Schema<"TokenAPI">;
export type EstadoToken = "vigente" | "revocado" | "vencido";

/** Vigente (aún vale), revocado o vencido. Revocado gana si las dos cosas pasaron. */
export function estadoToken(t: Pick<TokenAPI, "revocadoEn" | "expiraEn">, ahora: number): EstadoToken {
    if (t.revocadoEn) return "revocado";
    if (t.expiraEn && Date.parse(t.expiraEn) <= ahora) return "vencido";
    return "vigente";
}

export const PRESENTACION_ESTADO: Record<EstadoToken, { texto: string; tono: Tono; icono: LucideIcon }> = {
    vigente: { texto: "Vigente", tono: "success", icono: CircleCheck },
    revocado: { texto: "Revocado", tono: "neutral", icono: Ban },
    vencido: { texto: "Vencido", tono: "warning", icono: Hourglass },
};

/** Lo que el menú de un token ofrece según su estado y los permisos. */
export function accionesToken(estado: EstadoToken, puedeRevocar: boolean, puedeEliminar: boolean): ("revocar" | "revocar_eliminar" | "eliminar")[] {
    if (estado === "vigente") {
        return [...(puedeRevocar ? (["revocar"] as const) : []), ...(puedeRevocar && puedeEliminar ? (["revocar_eliminar"] as const) : [])];
    }
    return puedeEliminar ? ["eliminar"] : [];
}

/** «1 token», «3 tokens». */
export function cuantosTokens(n: number): string {
    return n === 1 ? "1 token" : `${n} tokens`;
}

/** Título de la confirmación del lote. */
export function tituloLote(n: number): string {
    return n === 1 ? "¿Eliminar 1 token revocado o vencido?" : `¿Eliminar ${n} tokens revocados y vencidos?`;
}
