import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { inicioDesdeGuatemala } from "./esquemas";
import { ORDEN_TIPOS, type EstadoOrden, type TipoOrden } from "./presentacion";

/*
 * Ticket y orden de la mano. Espejo de la API (OpenAPI: EntradaCrearOrdenDeTicket,
 * EntradaIgualarAsignacion): `POST /tickets/{id}/crear-orden` exige técnico o cuadrilla (422
 * `validacion` con regla `asignacion_requerida` en los dos campos) y, con fecha, el técnico.
 */

export const MENSAJE_ASIGNACION_REQUERIDA = "Elige un técnico o una cuadrilla";

const PATRON_FECHA = /^\d{4}-\d{2}-\d{2}$/;
const PATRON_HORA = /^([01]\d|2[0-3]):([0-5]\d)$/;

/** Id numérico opcional desde un `<select>` (vacío = ninguno). */
const idOpcional = z
    .string()
    .trim()
    .refine((v) => v === "" || /^\d{1,15}$/.test(v), "Escribe un número")
    .transform((v) => (v === "" ? undefined : Number(v)));

const textoOpcional = (max: number) =>
    z
        .string()
        .trim()
        .max(max, `Admite hasta ${max.toLocaleString("es-GT")} caracteres`)
        .transform((v) => (v === "" ? undefined : v));

export const esquemaCrearOrdenDeTicket = z
    .object({
        tipo: z.enum(ORDEN_TIPOS as [TipoOrden, ...TipoOrden[]], { error: "Elige el tipo de orden" }),
        tecnicoId: idOpcional,
        cuadrillaId: idOpcional,
        fecha: z.string().trim(),
        hora: z.string().trim(),
        descripcion: textoOpcional(4000),
        direccion: textoOpcional(300),
    })
    .superRefine((d, ctx) => {
        if (d.tecnicoId === undefined && d.cuadrillaId === undefined) {
            ctx.addIssue({ code: "custom", path: ["tecnicoId"], message: MENSAJE_ASIGNACION_REQUERIDA });
            ctx.addIssue({ code: "custom", path: ["cuadrillaId"], message: MENSAJE_ASIGNACION_REQUERIDA });
        }
        const conFecha = d.fecha !== "" || d.hora !== "";
        if (!conFecha) return;
        if (d.tecnicoId === undefined) {
            ctx.addIssue({ code: "custom", path: ["tecnicoId"], message: "Para programar la visita elige el técnico" });
        }
        if (d.fecha === "") {
            ctx.addIssue({ code: "custom", path: ["fecha"], message: "Elige la fecha (o borra la hora)" });
        } else if (!PATRON_FECHA.test(d.fecha)) {
            ctx.addIssue({ code: "custom", path: ["fecha"], message: "Escribe la fecha como 2026-10-28" });
        }
        if (d.hora === "") {
            ctx.addIssue({ code: "custom", path: ["hora"], message: "Elige la hora (o borra la fecha)" });
        } else if (!PATRON_HORA.test(d.hora)) {
            ctx.addIssue({ code: "custom", path: ["hora"], message: "Escribe la hora como 14:30" });
        }
        if (PATRON_FECHA.test(d.fecha) && PATRON_HORA.test(d.hora) && inicioDesdeGuatemala(d.fecha, d.hora) === undefined) {
            ctx.addIssue({ code: "custom", path: ["fecha"], message: "Esa fecha no existe" });
        }
    });

export type EntradaFormCrearOrdenDeTicket = z.input<typeof esquemaCrearOrdenDeTicket>;
export type DatosCrearOrdenDeTicket = z.output<typeof esquemaCrearOrdenDeTicket>;

/** Cuerpo de `POST /tickets/{id}/crear-orden` sin claves vacías (hora de Guatemala → UTC). */
export function entradaCrearOrdenDeTicket(d: DatosCrearOrdenDeTicket): Schema<"EntradaCrearOrdenDeTicket"> {
    const inicio = d.fecha && d.hora ? inicioDesdeGuatemala(d.fecha, d.hora) : undefined;
    return {
        tipo: d.tipo,
        ...(d.tecnicoId !== undefined ? { tecnicoId: d.tecnicoId } : {}),
        ...(d.cuadrillaId !== undefined ? { cuadrillaId: d.cuadrillaId } : {}),
        ...(inicio !== undefined ? { inicio } : {}),
        ...(d.descripcion !== undefined ? { descripcion: d.descripcion } : {}),
        ...(d.direccion !== undefined ? { direccion: d.direccion } : {}),
    };
}

// --- Igualar la asignación ------------------------------------------------------------------------

type AsignacionTicket = Pick<Schema<"Ticket">, "asignadoA" | "cuadrillaId" | "estado">;
type AsignacionOrden = { tecnicoId: number | null; cuadrillaId: number | null; estado: EstadoOrden };

/** Sentido de `POST /ordenes/{id}/igualar-asignacion`. */
export type Desde = Schema<"EntradaIgualarAsignacion">["desde"];

const ORDEN_ASIGNABLE: readonly EstadoOrden[] = ["pendiente", "programada", "en_camino"];
/** Una orden cerrada, fallida o cancelada ya no marca la asignación del ticket. */
const ORDEN_EN_CURSO: readonly EstadoOrden[] = [...ORDEN_ASIGNABLE, "en_sitio", "pausada"];
const TICKET_ASIGNABLE: readonly AsignacionTicket["estado"][] = ["nuevo", "abierto", "en_espera"];

const tiene = (a: { id: number | null; cuadrilla: number | null }) => a.id !== null || a.cuadrilla !== null;

/**
 * Qué igualaciones ofrecer entre un ticket y una de sus órdenes (nunca se hace sola):
 *  - `ticket`: la orden toma el responsable y la cuadrilla del ticket. Si el responsable del
 *    ticket no es técnico (p. ej. un agente de soporte), no se ofrece: la API lo rechazaría.
 *  - `orden`: el ticket toma el técnico y la cuadrilla de la orden.
 * Solo si difieren, la orden sigue en curso, el lado que se copia tiene algo y el lado que cambia
 * acepta asignación.
 * `esTecnico` = null: no se conoce la lista de usuarios (se ofrece y la API valida).
 */
export function igualacionesPosibles(ticket: AsignacionTicket, orden: AsignacionOrden, esTecnico: ((id: number) => boolean) | null): Desde[] {
    const t = { id: ticket.asignadoA, cuadrilla: ticket.cuadrillaId };
    const o = { id: orden.tecnicoId, cuadrilla: orden.cuadrillaId };
    if ((t.id === o.id && t.cuadrilla === o.cuadrilla) || !ORDEN_EN_CURSO.includes(orden.estado)) return [];
    const salida: Desde[] = [];
    if (tiene(t) && ORDEN_ASIGNABLE.includes(orden.estado) && (t.id === null || esTecnico === null || esTecnico(t.id))) {
        salida.push("ticket");
    }
    if (tiene(o) && TICKET_ASIGNABLE.includes(ticket.estado)) {
        salida.push("orden");
    }
    return salida;
}
