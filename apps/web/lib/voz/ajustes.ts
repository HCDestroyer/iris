import { z } from "zod";
import type { AjustesVoz, EntradaAjustesVoz, EntradaLlamadasCanal, LlamadasCanal } from "./tipos";

/* Formularios de los ajustes de llamadas (contratos/voz.md §3.9 y §7.2), espejo del servidor. */

const entero = (min: number, max: number, mensaje: string) =>
    z
        .string()
        .trim()
        .refine((v) => /^\d+$/.test(v) && Number(v) >= min && Number(v) <= max, mensaje);

/** Sin caracteres de control (salvo salto de línea y tabulación). */
const sinControl = (v: string) => ![...v].some((c) => c !== "\n" && c !== "\t" && (c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127));

// --- Por número: `POST /canales/{id}/llamadas/configurar` --------------------------------------

/** En E1 el asistente todavía no contesta llamadas: el servidor rechaza `asistente` (422). */
export const FUERA_DE_HORARIO = ["rechazar_con_mensaje", "buzon"] as const;

export const esquemaLlamadasCanal = z.object({
    llamadasActivas: z.boolean(),
    colaId: z.string(),
    horarioId: z.string(),
    segundosSonando: entero(15, 45, "Entre 15 y 45 segundos"),
    fueraDeHorario: z.enum(FUERA_DE_HORARIO),
    iconoVisible: z.boolean(),
    devolucion: z.boolean(),
    mensajeActivo: z.boolean(),
    // El servidor pide el texto aunque el mensaje esté apagado (queda listo para encenderlo).
    mensajeTexto: z.string().trim().min(1, "Escribe el mensaje").max(500, "Hasta 500 caracteres").refine(sinControl, "Tiene caracteres que no se pueden enviar"),
    // E1b: la plantilla `call_permission_request` aprobada del número (vacía: sin plantilla) y su idioma.
    plantillaPermiso: z
        .string()
        .trim()
        .max(200, "Hasta 200 caracteres")
        .regex(/^[a-z0-9_]*$/, "Solo minúsculas, números y guion bajo, como en Meta"),
    plantillaPermisoIdioma: z
        .string()
        .trim()
        .regex(/^([a-z]{2}(_[A-Z]{2})?)?$/, "El código del idioma, como «es» o «es_MX»"),
});
export type ValoresLlamadasCanal = z.infer<typeof esquemaLlamadasCanal>;

export function valoresDeCanal(c: LlamadasCanal): ValoresLlamadasCanal {
    return {
        llamadasActivas: c.llamadasActivas,
        colaId: c.colaId ? String(c.colaId) : "",
        horarioId: c.horarioId ? String(c.horarioId) : "",
        segundosSonando: String(c.segundosSonando),
        fueraDeHorario: c.fueraDeHorario === "buzon" ? "buzon" : "rechazar_con_mensaje",
        iconoVisible: c.iconoVisible,
        devolucion: c.devolucion,
        mensajeActivo: c.mensajePerdida.activo,
        mensajeTexto: c.mensajePerdida.texto,
        plantillaPermiso: c.plantillaPermiso ?? "",
        plantillaPermisoIdioma: c.plantillaPermisoIdioma ?? "",
    };
}

export function entradaDeCanal(v: ValoresLlamadasCanal, version: number): EntradaLlamadasCanal {
    return {
        version,
        llamadasActivas: v.llamadasActivas,
        ...(v.colaId ? { colaId: Number(v.colaId) } : {}),
        ...(v.horarioId ? { horarioId: Number(v.horarioId) } : {}),
        segundosSonando: Number(v.segundosSonando),
        fueraDeHorario: v.fueraDeHorario,
        iconoVisible: v.iconoVisible,
        devolucion: v.devolucion,
        mensajePerdida: { activo: v.mensajeActivo, texto: v.mensajeTexto.trim() },
        // Vacía, se quita. El idioma solo viaja con una plantilla (sin él, el servidor usa «es»).
        plantillaPermiso: v.plantillaPermiso.trim(),
        ...(v.plantillaPermiso.trim() && v.plantillaPermisoIdioma.trim() ? { plantillaPermisoIdioma: v.plantillaPermisoIdioma.trim() } : {}),
    };
}

/** Por qué no se pueden encender las llamadas en este número, en llano; `null` si se puede. */
export function motivoNoEncender(c: Pick<LlamadasCanal, "tipoConexion" | "requisitos">): string | null {
    if (c.tipoConexion === "qr") return "Este número está conectado por QR y no puede recibir llamadas.";
    if (!c.requisitos.cumple) return "Falta cumplir los requisitos de Meta de arriba.";
    return null;
}

// --- De la empresa: `POST /voz/ajustes/actualizar` (campos de E1 y E1b) --------------------------

export const esquemaAjustesVoz = z.object({
    duracionMaxPersonaMin: entero(10, 120, "Entre 10 y 120 minutos"),
    segundosAsignado: entero(0, 20, "Entre 0 y 20 segundos"),
    // E1b, «Avanzado»: límites de las llamadas salientes (se cobran por minuto).
    maxSalientesPorDia: entero(0, 1000, "Entre 0 y 1000 llamadas"),
    topeMinutosSalientesMes: entero(0, 100_000, "Entre 0 y 100 000 minutos"),
    maxColgadasMarcandoDia: entero(0, 20, "Entre 0 y 20"),
});
export type ValoresAjustesVoz = z.infer<typeof esquemaAjustesVoz>;

export const valoresDeAjustes = (a: AjustesVoz): ValoresAjustesVoz => ({
    duracionMaxPersonaMin: String(a.duracionMaxPersonaMin),
    segundosAsignado: String(a.segundosAsignado),
    maxSalientesPorDia: String(a.maxSalientesPorDia),
    topeMinutosSalientesMes: String(a.topeMinutosSalientesMes),
    maxColgadasMarcandoDia: String(a.maxColgadasMarcandoDia),
});

/** La grabación todavía no existe (`grabar` es siempre `nunca`): no se ofrece ni se manda. */
export function entradaDeAjustes(v: ValoresAjustesVoz, a: Pick<AjustesVoz, "version">): EntradaAjustesVoz {
    return {
        version: a.version,
        duracionMaxPersonaMin: Number(v.duracionMaxPersonaMin),
        segundosAsignado: Number(v.segundosAsignado),
        maxSalientesPorDia: Number(v.maxSalientesPorDia),
        topeMinutosSalientesMes: Number(v.topeMinutosSalientesMes),
        maxColgadasMarcandoDia: Number(v.maxColgadasMarcandoDia),
    };
}

/** Cuánto se lleva de un tope de salientes, para decirlo junto a él. */
export type Consumo = { texto: string; nivel: "normal" | "cerca" | "tope" };

function consumo(lleva: unknown, tope: unknown, texto: (lleva: number, tope: number) => string): Consumo | null {
    // Sin el dato, o con el tope en 0 (salientes apagadas), no se inventa nada.
    if (typeof lleva !== "number" || typeof tope !== "number" || !Number.isFinite(lleva) || lleva < 0 || tope <= 0) return null;
    const parte = lleva / tope;
    return { texto: texto(Math.floor(lleva), tope), nivel: parte >= 1 ? "tope" : parte >= 0.8 ? "cerca" : "normal" };
}

/** «Llevas 120 de 300 minutos este mes» y «8 de 50 llamadas hoy», contra los topes **guardados**. */
export function consumoSalientes(a: Partial<Pick<AjustesVoz, "minutosSalientesMes" | "salientesHoy" | "topeMinutosSalientesMes" | "maxSalientesPorDia">>): { mes: Consumo | null; hoy: Consumo | null } {
    return {
        mes: consumo(a.minutosSalientesMes, a.topeMinutosSalientesMes, (l, t) => `Llevas ${l} de ${t} minutos este mes`),
        hoy: consumo(a.salientesHoy, a.maxSalientesPorDia, (l, t) => `${l} de ${t} llamadas hoy`),
    };
}

/** Resumen de «Avanzado» plegado. */
export function resumenSalientes(v: Partial<ValoresAjustesVoz>): string {
    const n = (t: string | undefined) => (t !== undefined && /^\d+$/.test(t) ? Number(t) : null);
    const dia = n(v.maxSalientesPorDia);
    const mes = n(v.topeMinutosSalientesMes);
    if (dia === 0 || mes === 0) return "Las llamadas salientes están apagadas";
    return `Hasta ${dia ?? "—"} llamadas salientes al día · ${mes ?? "—"} minutos al mes`;
}
