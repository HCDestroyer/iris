import { mensajeDeError } from "../api/errores";
import type { Ticket } from "./sla";

export type ResultadoLote = { hechos: Ticket[]; fallidos: { ticket: Ticket; motivo: string }[] };

/**
 * Aplica una acción explícita a varios tickets, de uno en uno (cada uno con su `version` y su
 * `Idempotency-Key`: es la misma acción que desde la ficha, nunca un PATCH en lote). Un fallo no
 * detiene a los demás: se informa cuáles no se pudieron y por qué.
 */
export async function enLote(tickets: readonly Ticket[], accion: (t: Ticket) => Promise<Ticket>): Promise<ResultadoLote> {
    const resultado: ResultadoLote = { hechos: [], fallidos: [] };
    for (const t of tickets) {
        try {
            resultado.hechos.push(await accion(t));
        } catch (error) {
            const m = mensajeDeError(error);
            resultado.fallidos.push({ ticket: t, motivo: [m.titulo, m.detalle].filter(Boolean).join(". ") });
        }
    }
    return resultado;
}

/** «3 tickets asignados a Ana López», «1 ticket sin asignar». */
export function textoLoteAsignado(n: number, nombre: string | null): string {
    const tickets = n === 1 ? "1 ticket" : `${n} tickets`;
    if (nombre === null) return `${tickets} sin asignar`;
    return `${tickets} ${n === 1 ? "asignado" : "asignados"} a ${nombre}`;
}
