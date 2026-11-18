import type { Schema } from "@iris/contratos";
import { haceCuanto } from "../fechas";
import { textoMotivoPaso } from "./fase3b";

/*
 * Quién atiende la conversación (contratos/conversaciones.md §17.11 y §17.4): el campo
 * `atencionAsistente` de la lista, el detalle y el feed, en llano. Sin el campo no se pinta nada
 * (la empresa no usa el asistente atendiendo solo, o la conversación está resuelta).
 */

export type AtencionAsistente = Schema<"AtencionAsistente">;
export type MotivoPausa = Schema<"PausaAsistente">["motivo"];

type ConAtencion = { atencionAsistente?: AtencionAsistente | undefined; asistente?: { motivoPaso?: string | null | undefined } | undefined; asignado?: unknown };

/** El motivo de una pausa, para ir después de «por»; `manual` no dice nada. */
const MOTIVO_PAUSA: Record<string, string> = {
    caso_delicado: "caso delicado",
    respuesta_incorrecta: "respondió mal",
    cliente_lo_pidio: "lo pidió el cliente",
};
export const textoMotivoPausa = (motivo: string | null | undefined): string | null => (motivo && Object.hasOwn(MOTIVO_PAUSA, motivo) ? (MOTIVO_PAUSA[motivo] ?? null) : null);

/** Los motivos que se pueden elegir al pausar (además del toque simple, sin motivo). */
export const MOTIVOS_PAUSA: readonly { valor: Exclude<MotivoPausa, "manual">; etiqueta: string }[] = [
    { valor: "caso_delicado", etiqueta: "Es un caso delicado" },
    { valor: "respuesta_incorrecta", etiqueta: "Respondió mal" },
    { valor: "cliente_lo_pidio", etiqueta: "Lo pidió el cliente" },
];

export type Franja = {
    /** Para `data-estado` y las pruebas. */
    estado: "atiende" | NonNullable<AtencionAsistente["motivo"]>;
    /** `marca`: todo bien, atiende el asistente. `aviso`: hace falta que alguien actúe. `neutro`: solo se informa. */
    tono: "marca" | "aviso" | "neutro";
    texto: string;
    /** Botones a la vista, en orden. */
    acciones: readonly ("pausar" | "reanudar" | "tomar")[];
    /** Texto del botón de reanudar («Reanudar» tras una pausa; «Que siga el asistente» en lo demás). */
    textoReanudar: string;
};

/** La franja bajo la cabecera de la conversación; `null` si no hay nada que decir ahí. */
export function franjaAtencion(conv: ConAtencion, ahora: number): Franja | null {
    const a = conv.atencionAsistente;
    if (!a) return null;
    const base = { textoReanudar: "Que siga el asistente", acciones: [] } as const;
    if (a.atiende) return { ...base, estado: "atiende", tono: "marca", texto: "El asistente atiende esta conversación", acciones: ["pausar", "tomar"] };
    switch (a.motivo) {
        case "pausado": {
            const quien = [a.pausa?.por?.nombre.split(" ")[0], a.pausa ? haceCuanto(a.pausa.en, ahora) : ""].filter(Boolean).join(", ");
            const motivo = textoMotivoPausa(a.pausa?.motivo);
            return {
                estado: "pausado",
                tono: "neutro",
                texto: ["Asistente en pausa", quien, motivo].filter(Boolean).join(" · "),
                acciones: a.reanudable ? ["reanudar"] : [],
                textoReanudar: "Reanudar",
            };
        }
        case "persona_presente":
            // La que se veía vacía: nadie la tiene y el asistente ya no contesta. Debe notarse.
            return {
                ...base,
                estado: "persona_presente",
                tono: "aviso",
                texto: "Nadie la está atendiendo: el asistente se apartó cuando una persona la tomó",
                acciones: a.reanudable ? ["reanudar", "tomar"] : ["tomar"],
            };
        case "espera_persona": {
            const motivo = textoMotivoPaso(conv.asistente?.motivoPaso);
            return { ...base, estado: "espera_persona", tono: "aviso", texto: motivo ? `El asistente la pasó a una persona porque ${motivo}` : "El asistente la pasó a una persona: espera en la cola", acciones: conv.asignado ? [] : ["tomar"] };
        }
        case "tope":
            return { ...base, estado: "tope", tono: "neutro", texto: "El asistente está en pausa: se llegó al tope del mes" };
        case "bot":
            return { ...base, estado: "bot", tono: "neutro", texto: "La atiende el bot de menú" };
        case "categoria_sugiere":
            return { ...base, estado: "categoria_sugiere", tono: "neutro", texto: "En esta categoría el asistente solo sugiere" };
        case "horario_solo_sugiere":
            return { ...base, estado: "horario_solo_sugiere", tono: "neutro", texto: "A esta hora el asistente solo sugiere" };
        // `persona_asignada`: ya se ve quién la tiene; un motivo que el panel no conoce: nada.
        default:
            return null;
    }
}

/** Lo que ofrece el menú «⋯» de la conversación: lo que no está ya a la vista en la franja. */
export function accionDeMenu(conv: ConAtencion): "pausar" | "reanudar" | null {
    const a = conv.atencionAsistente;
    if (!a) return null;
    if (a.atiende || a.motivo === "persona_asignada") return "pausar";
    // Con la franja de «la pasó a una persona» a la vista, «Tomar» es lo principal; devolverla, aquí.
    if (a.motivo === "espera_persona" && a.reanudable) return "reanudar";
    return null;
}

/** ¿Hay que avisar antes de devolverle la conversación al asistente? (el cliente pidió una persona) */
export function pideConfirmarReanudar(conv: ConAtencion): boolean {
    const motivo = conv.asistente?.motivoPaso;
    return motivo === "cliente_lo_pidio" || motivo === "enojo";
}

/**
 * Al quitar la asignación: ¿preguntar si la sigue atendiendo el asistente? Solo si el asistente se
 * había apartado porque una persona la tenía (`antes`) y, ya sin nadie, puede retomarla
 * (`despues`). Si el cliente pidió una persona, no se ofrece.
 */
export function ofrecerQueSigaElAsistente(antes: ConAtencion, despues: ConAtencion): boolean {
    return (
        antes.atencionAsistente?.motivo === "persona_asignada" &&
        despues.atencionAsistente?.motivo === "persona_presente" &&
        despues.atencionAsistente.reanudable &&
        !pideConfirmarReanudar(despues)
    );
}

/** El punto de la fila de la lista: solo lo que conviene ver de un vistazo. */
export function puntoAtencion(a: AtencionAsistente | undefined): { estado: "pausado" | "persona_presente"; tono: "neutro" | "aviso"; texto: string } | null {
    if (!a || a.atiende) return null;
    if (a.motivo === "pausado") return { estado: "pausado", tono: "neutro", texto: "Asistente en pausa" };
    if (a.motivo === "persona_presente") return { estado: "persona_presente", tono: "aviso", texto: "Nadie la atiende" };
    return null;
}

type EventoAsistente = { tipo: string; actorNombre?: string | null | undefined; detalle?: string | null | undefined; minutos?: number | null | undefined };

/** Eventos de sistema del asistente y del bot que no contestó, para el hilo; `null` si no es uno de ellos. */
export function textoEventoAsistente(e: EventoAsistente): string | null {
    const actor = e.actorNombre ?? "Iris";
    switch (e.tipo) {
        case "bot_sin_respuesta": {
            const porque = "el cliente no acepta mensajes o pasaron 24 h";
            return e.detalle === "sin_disponibles" ? `Sin agentes disponibles y el bot no pudo contestar: ${porque}` : `Nadie la tomó en ${e.minutos ?? 30} min y el bot no pudo contestar: ${porque}`;
        }
        case "asistente_pausado": {
            const motivo = textoMotivoPausa(e.detalle);
            return `${actor} pausó el asistente${motivo ? `: ${motivo}` : ""}`;
        }
        case "asistente_reanudado":
            return `${actor} le devolvió la conversación al asistente`;
        default:
            return null;
    }
}
