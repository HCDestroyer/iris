/*
 * Franjas de un horario (contratos/conversaciones.md §10.1): `dias` 1 = lunes … 7 = domingo,
 * `inicio` < `fin` en la misma fecha local (una franja que cruza la medianoche se escribe como
 * dos). Textos cortos para la lista de números y validación espejo del servidor.
 */

import type { Schema } from "@iris/contratos";

export type FranjaEditable = { dias: number[]; inicio: string; fin: string };
/** Franja como la devuelve la API. */
type FranjaApi = Schema<"FranjaHorario">;

const CORTOS = ["Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"];
const LARGOS = ["Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado", "Domingo"];

export const DIAS_SEMANA = LARGOS.map((nombre, i) => ({ dia: i + 1, nombre, corto: CORTOS[i] ?? "" }));

const HORA = /^([01]\d|2[0-3]):[0-5]\d$/;

/** «Lun–Vie», «Lun, Mié y Vie», «Todos los días». */
export function textoDias(dias: readonly number[] | null | undefined): string {
    const lista = [...new Set(dias ?? [])].filter((d) => d >= 1 && d <= 7).sort((a, b) => a - b);
    if (lista.length === 7) return "Todos los días";
    if (lista.length === 0) return "Ningún día";
    const corridos = lista.every((d, i) => i === 0 || d === (lista[i - 1] ?? 0) + 1);
    if (corridos && lista.length > 2) return `${CORTOS[(lista[0] ?? 1) - 1]}–${CORTOS[(lista.at(-1) ?? 1) - 1]}`;
    const nombres = lista.map((d) => CORTOS[d - 1] ?? "");
    return nombres.length === 1 ? (nombres[0] ?? "") : `${nombres.slice(0, -1).join(", ")} y ${nombres.at(-1)}`;
}

/** «7:00» (sin cero adelante en la hora, como en el diseño). */
export function horaCorta(hhmm: string): string {
    return hhmm.replace(/^0(\d)/, "$1");
}

/** «Lun–Sáb 7:00–19:00 · Dom 8:00–12:00». */
export function resumirFranjas(franjas: readonly FranjaApi[] | null | undefined): string {
    if (!franjas || franjas.length === 0) return "Sin franjas: siempre cerrado";
    return franjas.map((f) => `${textoDias(f.dias)} ${horaCorta(f.inicio)}–${horaCorta(f.fin)}`).join(" · ");
}

export type ErrorFranja = { indice: number; campo: "dias" | "inicio" | "fin"; mensaje: string };

/** Errores de las franjas (vacío si están bien): horas válidas, inicio < fin y sin choques el mismo día. */
export function validarFranjas(franjas: readonly FranjaEditable[]): ErrorFranja[] {
    const errores: ErrorFranja[] = [];
    franjas.forEach((f, i) => {
        if (f.dias.length === 0) errores.push({ indice: i, campo: "dias", mensaje: "Elige al menos un día" });
        if (!HORA.test(f.inicio)) errores.push({ indice: i, campo: "inicio", mensaje: "Hora inválida (HH:MM)" });
        if (!HORA.test(f.fin)) errores.push({ indice: i, campo: "fin", mensaje: "Hora inválida (HH:MM)" });
        else if (HORA.test(f.inicio) && f.inicio >= f.fin) errores.push({ indice: i, campo: "fin", mensaje: "Debe terminar después de empezar (si cruza la medianoche, usa dos franjas)" });
    });
    for (let a = 0; a < franjas.length; a++) {
        for (let b = a + 1; b < franjas.length; b++) {
            const fa = franjas[a];
            const fb = franjas[b];
            if (!fa || !fb) continue;
            const comun = fa.dias.some((d) => fb.dias.includes(d));
            if (comun && fa.inicio < fb.fin && fb.inicio < fa.fin && !errores.some((e) => e.indice === b)) {
                errores.push({ indice: b, campo: "inicio", mensaje: `Se cruza con la franja ${a + 1} el mismo día` });
            }
        }
    }
    return errores;
}
