import type { Schema } from "@iris/contratos";
import { api, resolver } from "@/lib/api/cliente";

/*
 * Cuentas bancarias de la empresa (contrato con Argos 0.8.0 §7.8): son de Argos. El panel las lee del
 * servidor de Iris (que ya filtra: activas, de la empresa de la conversación y en quetzales) y nunca
 * las escribe a mano.
 */

export type CuentaBancaria = Schema<"CuentaBancaria">;
export type TextoCuentas = Schema<"TextoCuentasConv">;

const TIPOS: Record<string, string> = { monetaria: "Monetaria", ahorro: "Ahorro" };

/** «Banco Industrial · Monetaria» (sin tipo si Argos no lo informó). */
export function tituloCuenta(c: Pick<CuentaBancaria, "banco" | "tipo">): string {
    const tipo = c.tipo ? TIPOS[c.tipo] : undefined;
    return tipo ? `${c.banco} · ${tipo}` : c.banco;
}

/** El alias si lo tiene y no repite el banco. */
export function aliasCuenta(c: Pick<CuentaBancaria, "alias" | "banco">): string | null {
    const a = c.alias.trim();
    return a && a.toLowerCase() !== c.banco.trim().toLowerCase() ? a : null;
}

/** «A nombre de …» o el aviso de que falta en Argos. */
export function titularCuenta(c: Pick<CuentaBancaria, "titular">): string {
    return c.titular ? `A nombre de ${c.titular}` : "Sin titular en Argos";
}

/** Nota cuando algunas cuentas no van al cliente por falta de titular. */
export function textoSinTitular(n: number): string {
    return n === 1 ? "1 cuenta no va porque no tiene titular en Argos." : `${n} cuentas no van porque no tienen titular en Argos.`;
}

/** `GET /conversaciones/{id}/texto-cuentas`: lo que «Enviar cuentas» le mandaría al cliente. */
export function leerTextoCuentas(conversacionId: number, signal?: AbortSignal): Promise<TextoCuentas> {
    return resolver(api().GET("/api/v1/mensajeria/conversaciones/{id}/texto-cuentas", { params: { path: { id: conversacionId } }, signal }));
}
