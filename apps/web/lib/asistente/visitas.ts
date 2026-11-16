import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { conPerfilDefecto, entero, entradaGuardada, perfilDefecto } from "./fase3";
import type { AjustesAsistente, EntradaAjustesAsistente } from "./tipos";

/*
 * El asistente agenda una visita técnica (contratos/conversaciones.md §17.10). El bloque `visitas`
 * de los ajustes dice qué visitas agenda, en qué horarios las ofrece y con qué reglas; el servidor
 * lo guarda **completo** (el POST reemplaza el bloque). Si el GET no lo trae (un servidor anterior),
 * el panel no lo muestra ni lo inventa. Tipos de `packages/contratos`; el esquema zod es el espejo
 * del validador del servidor.
 */

export type Visitas = Schema<"VisitasAsistente">;
export type BloqueVisita = Schema<"BloqueVisita">;

export const TIPOS_VISITA = ["visita", "mantenimiento"] as const;
export type TipoVisita = (typeof TIPOS_VISITA)[number];
export const NOMBRE_TIPO_VISITA: Record<TipoVisita, { titulo: string; detalle: string }> = {
    visita: { titulo: "Visita técnica", detalle: "Revisar una falla en casa del cliente." },
    mantenimiento: { titulo: "Mantenimiento", detalle: "Revisión o ajuste programado." },
};
const esTipo = (t: string): t is TipoVisita => (TIPOS_VISITA as readonly string[]).includes(t);

export const MAX_BLOQUES = 6;
const HORA = /^([01]\d|2[0-3]):[0-5]\d$/;

/** ¿El servidor manda los ajustes de visitas? Uno anterior no: ni se muestran ni se guardan. */
export const conVisitas = (a: Pick<AjustesAsistente, "visitas">): a is Pick<AjustesAsistente, "visitas"> & { visitas: Visitas } => a.visitas !== undefined && a.visitas !== null;

/** ¿Antes de agendar hay que verificar la identidad (DPI o NIT)? Ausente = no: basta su número (§17.10). */
export const visitaExigeIdentidad = (p: { exigeVerificacionFuerte?: { agendar_visita?: boolean | undefined } | undefined }): boolean => p.exigeVerificacionFuerte?.agendar_visita ?? false;

const bloque = z.object({ inicio: z.string(), fin: z.string() });

export const esquemaVisitas = z
    .object({
        tipos: z.object({ visita: z.boolean(), mantenimiento: z.boolean() }),
        bloques: z.array(bloque).min(1, "Agrega al menos un horario").max(MAX_BLOQUES, `Hasta ${MAX_BLOQUES} horarios`),
        anticipacionHoras: entero(0, 168, "Entre 0 y 168 horas (una semana)"),
        diasAdelante: entero(1, 30, "Entre 1 y 30 días"),
        exigeTicket: z.boolean(),
        permitirConMora: z.boolean(),
        /** `perfiles[0].exigeVerificacionFuerte.agendar_visita`. */
        pedirDocumento: z.boolean(),
        maxAbiertasPorCliente: entero(1, 5, "Entre 1 y 5"),
        ofertasPorDia: entero(1, 10, "Entre 1 y 10"),
        visitasPorDia: entero(1, 5, "Entre 1 y 5"),
        visitasPorDiaEmpresa: entero(1, 500, "Entre 1 y 500"),
    })
    .superRefine((v, ctx) => {
        if (!v.tipos.visita && !v.tipos.mantenimiento) ctx.addIssue({ code: "custom", path: ["tipos"], message: "Elige al menos un tipo de visita" });
        v.bloques.forEach((b, i) => {
            if (!HORA.test(b.inicio)) ctx.addIssue({ code: "custom", path: ["bloques", i, "inicio"], message: "Hora inválida (HH:MM)" });
            if (!HORA.test(b.fin)) ctx.addIssue({ code: "custom", path: ["bloques", i, "fin"], message: "Hora inválida (HH:MM)" });
            else if (HORA.test(b.inicio) && b.inicio >= b.fin) ctx.addIssue({ code: "custom", path: ["bloques", i, "fin"], message: "Debe terminar después de empezar" });
            const anterior = v.bloques[i - 1];
            if (anterior && HORA.test(anterior.fin) && HORA.test(b.inicio) && b.inicio < anterior.fin) {
                ctx.addIssue({ code: "custom", path: ["bloques", i, "inicio"], message: "Va después del horario anterior, sin cruzarse" });
            }
        });
    });
export type ValoresVisitas = z.infer<typeof esquemaVisitas>;

export function valoresVisitas(a: Pick<AjustesAsistente, "perfiles">, v: Visitas): ValoresVisitas {
    const tipos = (v.tiposPermitidos ?? []).filter(esTipo);
    return {
        tipos: { visita: tipos.includes("visita"), mantenimiento: tipos.includes("mantenimiento") },
        bloques: (v.bloques ?? []).map((b) => ({ inicio: b.inicio, fin: b.fin })),
        anticipacionHoras: String(v.anticipacionHoras),
        diasAdelante: String(v.diasAdelante),
        exigeTicket: v.exigeTicket,
        permitirConMora: v.permitirConMora,
        pedirDocumento: visitaExigeIdentidad(perfilDefecto(a)),
        maxAbiertasPorCliente: String(v.maxAbiertasPorCliente),
        ofertasPorDia: String(v.ofertasPorDia),
        visitasPorDia: String(v.visitasPorDia),
        visitasPorDiaEmpresa: String(v.visitasPorDiaEmpresa),
    };
}

/**
 * El bloque `visitas` completo para guardar. El orden de los tipos se conserva (el asistente usa el
 * primero): los que ya estaban quedan donde estaban y los nuevos van al final.
 */
export function visitasDeValores(d: ValoresVisitas, guardado: Visitas): Visitas {
    const antes = (guardado.tiposPermitidos ?? []).filter(esTipo);
    const elegidos = TIPOS_VISITA.filter((t) => d.tipos[t]);
    const tiposPermitidos = [...antes.filter((t) => elegidos.includes(t)), ...elegidos.filter((t) => !antes.includes(t))];
    return {
        tiposPermitidos,
        bloques: d.bloques.map((b) => ({ inicio: b.inicio, fin: b.fin })),
        anticipacionHoras: Number(d.anticipacionHoras),
        diasAdelante: Number(d.diasAdelante),
        exigeTicket: d.exigeTicket,
        permitirConMora: d.permitirConMora,
        maxAbiertasPorCliente: Number(d.maxAbiertasPorCliente),
        ofertasPorDia: Number(d.ofertasPorDia),
        visitasPorDia: Number(d.visitasPorDia),
        visitasPorDiaEmpresa: Number(d.visitasPorDiaEmpresa),
    };
}

/**
 * Cuerpo de `…/ajustes/actualizar` que cambia cómo agenda: el bloque `visitas` completo y, en el
 * perfil general, si antes pide documento (lo demás de `exigeVerificacionFuerte` queda como estaba).
 */
export function entradaVisitas(a: AjustesAsistente & { visitas: Visitas }, d: ValoresVisitas): EntradaAjustesAsistente {
    return {
        ...entradaGuardada(a),
        visitas: visitasDeValores(d, a.visitas),
        perfiles: conPerfilDefecto(a, { exigeVerificacionFuerte: { dar_saldo: perfilDefecto(a).exigeVerificacionFuerte?.dar_saldo ?? true, agendar_visita: d.pedirDocumento } }),
    };
}

const horaCorta = (hhmm: string) => {
    const [h = "0", m = "00"] = hhmm.split(":");
    return m === "00" ? String(Number(h)) : `${Number(h)}:${m}`;
};

/** «de 8 a 12 y de 13 a 17». */
export function textoBloques(bloques: readonly BloqueVisita[] | null | undefined): string {
    const partes = (bloques ?? []).map((b) => `de ${horaCorta(b.inicio)} a ${horaCorta(b.fin)}`);
    if (partes.length === 0) return "sin horarios";
    return partes.length === 1 ? (partes[0] ?? "") : `${partes.slice(0, -1).join(", ")} y ${partes.at(-1)}`;
}

/** «con 12 horas de anticipación», «con 1 día de anticipación», «sin anticipación». */
export function textoAnticipacion(horas: number): string {
    if (horas <= 0) return "sin anticipación";
    if (horas % 24 === 0) return `con ${horas / 24 === 1 ? "1 día" : `${horas / 24} días`} de anticipación`;
    return `con ${horas === 1 ? "1 hora" : `${horas} horas`} de anticipación`;
}

/**
 * Hasta cuántos días adelante ofrece de verdad: `visitas.diasAdelante`, acortado por
 * `limites.diasAdelanteMax` del perfil (§17.10).
 */
export function diasQueOfrece(a: Pick<AjustesAsistente, "perfiles">, v: Pick<Visitas, "diasAdelante">): number {
    return Math.min(v.diasAdelante, perfilDefecto(a).limites.diasAdelanteMax);
}

/** El resumen llano de «Cómo agenda», línea por línea, para la pantalla simple. */
export function resumenVisitas(a: Pick<AjustesAsistente, "perfiles">, v: Visitas): string[] {
    const tipos = (v.tiposPermitidos ?? []).filter(esTipo).map((t) => NOMBRE_TIPO_VISITA[t].titulo.toLowerCase());
    const dias = diasQueOfrece(a, v);
    return [
        `Agenda ${tipos.length ? tipos.join(" y ") : "visitas"}, ${textoBloques(v.bloques)}.`,
        `Ofrece ${textoAnticipacion(v.anticipacionHoras)} y hasta ${dias === 1 ? "1 día" : `${dias} días`} adelante.`,
        [v.exigeTicket ? "Crea un ticket antes si no hay." : null, v.permitirConMora ? "Agenda aunque el servicio esté cortado por mora." : "Si el servicio está cortado por mora, lo pasa a una persona.", visitaExigeIdentidad(perfilDefecto(a)) ? "Antes pide DPI o NIT." : null]
            .filter(Boolean)
            .join(" "),
    ];
}

const NUMERICOS = ["anticipacionHoras", "diasAdelante", "maxAbiertasPorCliente", "ofertasPorDia", "visitasPorDia", "visitasPorDiaEmpresa"] as const;
export type CampoVisitas = "tipos" | "bloques" | (typeof NUMERICOS)[number] | `bloques.${number}.inicio`;
/** Lo avanzado (plegado): si el servidor rechaza uno de estos, la sección se abre. */
export const CAMPOS_AVANZADOS = ["maxAbiertasPorCliente", "ofertasPorDia", "visitasPorDia", "visitasPorDiaEmpresa"] as const;

/**
 * El campo del formulario de un `errors[].field` del 422 del servidor (`visitas.ofertasPorDia`,
 * `body.visitas.bloques[1]`, `visitas.tiposPermitidos[0]`…); `null` si no es de esta pantalla.
 */
export function campoDeErrorVisitas(field: string): CampoVisitas | null {
    const m = /(?:^|\.)visitas\.(.+)$/.exec(field);
    if (!m?.[1]) return null;
    const resto = m[1];
    if (resto.startsWith("tiposPermitidos")) return "tipos";
    const b = /^bloques\[(\d+)\]/.exec(resto);
    if (b) return `bloques.${Number(b[1])}.inicio`;
    if (resto === "bloques") return "bloques";
    return NUMERICOS.find((c) => c === resto) ?? null;
}
