import type { Schema } from "@iris/contratos";
import type { Lectura } from "../lectura";

/* Cuentas del resumen de la bandeja (`GET /tickets/resumen`), sin React: se prueban solas. */

export type ResumenTickets = Schema<"ResumenTickets">;
export type DiaTickets = Schema<"DiaTickets">;

/** Contador listo para mostrar, o `undefined` si el resumen no está (cargando, error o sin permiso). */
export function contadorDe(
    resumen: Lectura<ResumenTickets> | null,
    campo: "activos" | "mios" | "sinAsignar" | "enEspera" | "slaVenceHoy" | "slaVencidos" | "slaEnRiesgo",
): number | undefined {
    return resumen?.estado === "listo" ? resumen.datos[campo] : undefined;
}

/** «99+» a partir de 100: el contador de la barra lateral no ensancha el menú. */
export function textoContador(n: number): string {
    return n > 99 ? "99+" : String(n);
}

/** % de SLA cumplido de unos días (resueltos dentro del plazo entre los que tenían SLA); `null` sin base. */
export function slaCumplido(dias: readonly DiaTickets[]): number | null {
    const con = dias.reduce((n, d) => n + d.resueltosConSla, 0);
    if (con === 0) return null;
    const en = dias.reduce((n, d) => n + d.resueltosEnSla, 0);
    return Math.round((en / con) * 100);
}
