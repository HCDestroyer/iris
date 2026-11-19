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

/**
 * Lo que el menú de un token ofrece según su estado y los permisos. El de un conector activo
 * (`enUsoPorConector`) no se toca aquí: revocarlo cortaría la integración; se cambia desde
 * Integraciones.
 */
export function accionesToken(estado: EstadoToken, puedeRevocar: boolean, puedeEliminar: boolean, enUsoPorConector = false): ("revocar" | "revocar_eliminar" | "eliminar")[] {
    if (enUsoPorConector) return [];
    if (estado === "vigente") {
        return [...(puedeRevocar ? (["revocar"] as const) : []), ...(puedeRevocar && puedeEliminar ? (["revocar_eliminar"] as const) : [])];
    }
    return puedeEliminar ? ["eliminar"] : [];
}

/** Aviso cuando revocar los tokens de un usuario conservó el de un conector activo (con enlace: componentes/tokens/token-conservado.tsx). */
export const AVISO_TOKEN_CONSERVADO = "Se conservó el token de la integración con Argos u Horus. Si está comprometida, desactiva el conector en Integraciones.";

/** Resumen de lo revocado: «Se revocaron 2 tokens; se conservó el de la integración». */
export function textoTokensRevocados(revocados: number, conservados: number): string {
    const base = revocados === 1 ? "Se revocó 1 token" : `Se revocaron ${revocados} tokens`;
    return conservados > 0 ? `${base}; se conservó el de la integración.` : `${base}.`;
}

/** «1 token», «3 tokens». */
export function cuantosTokens(n: number): string {
    return n === 1 ? "1 token" : `${n} tokens`;
}

/** Título de la confirmación del lote. */
export function tituloLote(n: number): string {
    return n === 1 ? "¿Eliminar 1 token revocado o vencido?" : `¿Eliminar ${n} tokens revocados y vencidos?`;
}
