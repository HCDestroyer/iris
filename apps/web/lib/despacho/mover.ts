import type { Schema } from "@iris/contratos";
import { z } from "zod";
import type { Permiso } from "@/lib/menu";
import type { EstadoOrden, Orden, TipoOrden } from "@/lib/ordenes/presentacion";
import type { CitaAgenda } from "./calendario";
import { esDia, horaDe, isoDesdeDia, minutosDeTexto, type Dia } from "./fechas";

export type Asignable = Schema<"Asignable">;
export type RespuestaAsignables = Schema<"RespuestaAsignables">;

/** Lo que se puede mover en el calendario: una cita o una orden aún sin programar. */
export type Movible = {
    ordenId: number;
    numero: string;
    tipo: TipoOrden;
    estado: EstadoOrden;
    /** Versión de la orden leída (409 si cambió). */
    version: number;
    tecnicoId: number | null;
    zonaId: number | null;
    cuadrillaId: number | null;
    inicio: string | null;
    fin: string | null;
    ventanaInicio: string | null;
    ventanaFin: string | null;
    direccion: string;
};

export function movibleDeCita(c: CitaAgenda): Movible {
    return {
        ordenId: c.ordenId,
        numero: c.numero,
        tipo: c.tipo,
        estado: c.estado,
        version: c.ordenVersion,
        tecnicoId: c.tecnicoId,
        zonaId: c.zonaId,
        cuadrillaId: c.cuadrillaId,
        inicio: c.inicio,
        fin: c.fin,
        ventanaInicio: c.ventanaInicio,
        ventanaFin: c.ventanaFin,
        direccion: c.direccion,
    };
}

export function movibleDeOrden(o: Orden): Movible {
    return {
        ordenId: o.id,
        numero: o.numero,
        tipo: o.tipo,
        estado: o.estado,
        version: o.version,
        tecnicoId: o.tecnicoId,
        zonaId: o.zonaId,
        cuadrillaId: o.cuadrillaId,
        inicio: o.programadaInicio,
        fin: o.programadaFin,
        ventanaInicio: null,
        ventanaFin: null,
        direccion: o.direccion,
    };
}

/** Acciones explícitas del despacho (README de campo): nunca un PATCH de estado. */
export type AccionMover = "programar" | "reprogramar" | "asignar";

export const PERMISO_MOVER: Record<AccionMover, Permiso> = {
    programar: "ordenes.programar",
    reprogramar: "ordenes.reprogramar",
    asignar: "ordenes.asignar",
};

const CON_HORA: Partial<Record<EstadoOrden, AccionMover>> = { pendiente: "programar", programada: "reprogramar", en_camino: "reprogramar", fallida: "reprogramar" };
const SIN_HORA: readonly EstadoOrden[] = ["pendiente", "programada", "en_camino"];

/**
 * Qué acción mueve la orden: con hora, programar (pendiente) o reprogramar (programada, en camino
 * o fallida); sin hora, asignar el técnico. `null` si el estado no lo admite (en sitio o final).
 */
export function accionMover(estado: EstadoOrden, conHora: boolean): AccionMover | null {
    if (conHora) return CON_HORA[estado] ?? null;
    return SIN_HORA.includes(estado) ? "asignar" : null;
}

/** ¿Se puede mover con alguna acción permitida? (para mostrar «Mover a…» y hacerla arrastrable). */
export function sePuedeMover(estado: EstadoOrden, puede: (p: readonly Permiso[]) => boolean): boolean {
    return [true, false].some((conHora) => {
        const a = accionMover(estado, conHora);
        return a !== null && puede([PERMISO_MOVER[a]]);
    });
}

/** Duración que se conserva al mover una cita (minutos); `null` sin cita: la API usa la del tipo. */
export function duracionDe(m: Pick<Movible, "inicio" | "fin">): number | null {
    if (!m.inicio || !m.fin) return null;
    const min = Math.round((Date.parse(m.fin) - Date.parse(m.inicio)) / 60_000);
    return min > 0 ? min : null;
}

const motivo = z.string().trim().min(1, { message: "Escribe el motivo", abort: true }).min(3, "El motivo necesita al menos 3 caracteres").max(500, "Admite hasta 500 caracteres");

/**
 * Formulario «Mover a…» (también lo precarga el arrastre): técnico, día y hora de Guatemala, y el
 * motivo al reprogramar. `soloAsignar` cambia el técnico sin tocar la hora (acción asignar).
 */
export function esquemaMover(m: Pick<Movible, "estado" | "inicio" | "fin">) {
    return z
        .object({
            tecnicoId: z.string().trim(),
            soloAsignar: z.boolean(),
            fecha: z.string().trim(),
            hora: z.string().trim(),
            motivo: z.string(),
        })
        .superRefine((d, ctx) => {
            if (!/^\d{1,15}$/.test(d.tecnicoId)) {
                ctx.addIssue({ code: "custom", path: ["tecnicoId"], message: "Elige el técnico" });
            }
            const accion = accionMover(m.estado, !d.soloAsignar);
            if (accion === null) {
                ctx.addIssue({ code: "custom", path: ["soloAsignar"], message: "Esta orden no se puede mover en su estado" });
                return;
            }
            if (accion === "asignar") return;
            if (!esDia(d.fecha)) {
                ctx.addIssue({ code: "custom", path: ["fecha"], message: d.fecha ? "Escribe la fecha como 2026-10-28" : "Elige el día" });
            }
            if (minutosDeTexto(d.hora) === undefined) {
                ctx.addIssue({ code: "custom", path: ["hora"], message: d.hora ? "Escribe la hora como 14:30" : "Elige la hora" });
            }
            if (accion === "reprogramar") {
                const r = motivo.safeParse(d.motivo);
                if (!r.success) ctx.addIssue({ code: "custom", path: ["motivo"], message: r.error.issues[0]?.message ?? "Escribe el motivo" });
            }
        })
        .transform((d) => {
            const accion = accionMover(m.estado, !d.soloAsignar) as AccionMover;
            const tecnicoId = Number(d.tecnicoId);
            if (accion === "asignar") return { accion, tecnicoId } as const;
            const inicio = isoDesdeDia(d.fecha, minutosDeTexto(d.hora) ?? 0);
            const duracion = duracionDe(m);
            const fin = duracion === null ? undefined : new Date(Date.parse(inicio) + duracion * 60_000).toISOString().replace(".000Z", "Z");
            const texto = d.motivo.trim();
            return { accion, tecnicoId, inicio, fin, motivo: texto === "" ? undefined : texto } as const;
        });
}

export type EntradaFormMover = z.input<ReturnType<typeof esquemaMover>>;
export type DatosMover = z.output<ReturnType<typeof esquemaMover>>;

/** Cuerpo de `POST /ordenes/{id}/programar|reprogramar`. */
export function entradaProgramar(d: Extract<DatosMover, { inicio: string }>, version: number): Schema<"EntradaProgramarOrden"> {
    return {
        version,
        inicio: d.inicio,
        tecnicoId: d.tecnicoId,
        ...(d.fin !== undefined ? { fin: d.fin } : {}),
        ...(d.motivo !== undefined ? { motivo: d.motivo } : {}),
    };
}

/**
 * Cuerpo de `POST /ordenes/{id}/asignar`. La cuadrilla ausente = ninguna: se manda la del técnico
 * destino (de asignables) o, si no se sabe, la que ya tenía la orden.
 */
export function entradaAsignar(tecnicoId: number, version: number, cuadrillaId: number | null): Schema<"EntradaAsignarOrden"> {
    return { version, tecnicoId, ...(cuadrillaId !== null ? { cuadrillaId } : {}) };
}

export const MOTIVOS_NO_DISPONIBLE: Record<NonNullable<Asignable["motivo"]>, string> = {
    sin_turno: "No tiene turno ese día",
    ausente: "Está ausente ese día",
    capacidad_completa: "Ya completó su capacidad del día",
    sin_espacio: "No le queda un hueco del largo de esta orden",
};

export type Comprobacion = { tono: "success" | "warning" | "error"; texto: string };

const franjaTexto = (f: { inicio: string; fin: string }) => `${horaDe(f.inicio)}–${horaDe(f.fin)}`;

function lista(textos: string[]): string {
    return textos.length <= 1 ? (textos[0] ?? "") : `${textos.slice(0, -1).join(", ")} y ${textos.at(-1)}`;
}

/**
 * Comprueba el destino contra `GET /tecnicos/asignables` antes de enviar. `error` impide enviar
 * (la API respondería 422); `warning` avisa sin impedir (fuera de la ventana del cliente).
 */
export function comprobarDestino(
    m: Pick<Movible, "inicio" | "fin" | "ventanaInicio" | "ventanaFin">,
    asignable: Asignable | undefined,
    destino: { inicio: string | null; duracionTipo: number },
): Comprobacion[] {
    if (!asignable) {
        return [{ tono: "error", texto: "No aparece entre los técnicos asignables de esta orden: no atiende su zona, no puede verla o no tiene ficha activa." }];
    }
    if (destino.inicio === null) {
        return [{ tono: "success", texto: `${asignable.nombre} puede recibir esta orden.` }];
    }
    if (!asignable.disponible) {
        return [{ tono: "error", texto: `${asignable.nombre}: ${asignable.motivo ? MOTIVOS_NO_DISPONIBLE[asignable.motivo] : "no está disponible ese día"}.` }];
    }
    const resultado: Comprobacion[] = [];
    const duracion = duracionDe(m) ?? destino.duracionTipo;
    const a = Date.parse(destino.inicio);
    const b = a + duracion * 60_000;
    const libres = asignable.libres ?? [];
    const cabe = libres.some((f) => Date.parse(f.inicio) <= a && b <= Date.parse(f.fin));
    if (!cabe) {
        resultado.push({
            tono: "error",
            texto:
                libres.length === 0
                    ? `${asignable.nombre} no tiene tiempo libre ese día.`
                    : `Ese horario no está libre para ${asignable.nombre} (${duracion} min). Libre: ${lista(libres.map(franjaTexto))}.`,
        });
    } else {
        resultado.push({ tono: "success", texto: `${asignable.nombre} tiene libre ese horario (${duracion} min).` });
    }
    if (m.ventanaInicio && m.ventanaFin && (a < Date.parse(m.ventanaInicio) || a > Date.parse(m.ventanaFin))) {
        resultado.push({ tono: "warning", texto: `Queda fuera de la ventana acordada con el cliente (${franjaTexto({ inicio: m.ventanaInicio, fin: m.ventanaFin })}).` });
    }
    return resultado;
}

/** Valores iniciales del formulario a partir del destino (arrastre) o de la cita actual. */
export function valoresIniciales(m: Movible, destino: { tecnicoId?: number | null; dia?: Dia | null; minutos?: number | null } | undefined, hoy: Dia): EntradaFormMover {
    const dia = destino?.dia ?? (m.inicio ? new Date(Date.parse(m.inicio) - 6 * 3_600_000).toISOString().slice(0, 10) : hoy);
    const hora =
        destino?.minutos !== undefined && destino.minutos !== null
            ? `${String(Math.floor(destino.minutos / 60)).padStart(2, "0")}:${String(destino.minutos % 60).padStart(2, "0")}`
            : m.inicio
              ? horaDe(m.inicio)
              : "";
    const tecnico = destino?.tecnicoId ?? m.tecnicoId;
    return {
        tecnicoId: tecnico === null || tecnico === undefined ? "" : String(tecnico),
        soloAsignar: destino !== undefined && destino.minutos === null && destino.dia === null,
        fecha: dia,
        hora,
        motivo: "",
    };
}
