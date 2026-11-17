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
    };
}

/** Por qué no se pueden encender las llamadas en este número, en llano; `null` si se puede. */
export function motivoNoEncender(c: Pick<LlamadasCanal, "tipoConexion" | "requisitos">): string | null {
    if (c.tipoConexion === "qr") return "Este número está conectado por QR y no puede recibir llamadas.";
    if (!c.requisitos.cumple) return "Falta cumplir los requisitos de Meta de arriba.";
    return null;
}

// --- De la empresa: `POST /voz/ajustes/actualizar` (campos de E1) --------------------------------

export const esquemaAjustesVoz = z.object({
    duracionMaxPersonaMin: entero(10, 120, "Entre 10 y 120 minutos"),
    segundosAsignado: entero(0, 20, "Entre 0 y 20 segundos"),
});
export type ValoresAjustesVoz = z.infer<typeof esquemaAjustesVoz>;

export const valoresDeAjustes = (a: AjustesVoz): ValoresAjustesVoz => ({ duracionMaxPersonaMin: String(a.duracionMaxPersonaMin), segundosAsignado: String(a.segundosAsignado) });

/** La grabación no se ofrece todavía: `grabar` y su retención viajan como vinieron. */
export function entradaDeAjustes(v: ValoresAjustesVoz, a: AjustesVoz): EntradaAjustesVoz {
    return {
        version: a.version,
        grabar: a.grabar,
        retencionGrabacionesDias: a.retencionGrabacionesDias,
        duracionMaxPersonaMin: Number(v.duracionMaxPersonaMin),
        segundosAsignado: Number(v.segundosAsignado),
    };
}
