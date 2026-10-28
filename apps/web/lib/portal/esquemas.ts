import type { Schema } from "@iris/contratos";
import { z } from "zod";

/*
 * Espejo de los validadores de la API del portal (OpenAPI: EntradaCrearTicketPortal y
 * EntradaResponderTicketPortal). Los `satisfies` hacen fallar typecheck si el contrato cambia.
 */

const maximo = (max: number) => `Admite hasta ${max.toLocaleString("es-GT")} caracteres`;

export const esquemaNuevoTicket = z.object({
    asunto: z
        .string()
        .trim()
        .min(1, { message: "Escribe un asunto", abort: true })
        .min(3, "El asunto necesita al menos 3 caracteres")
        .max(200, maximo(200)),
    categoriaId: z
        .string()
        .transform((v) => (v === "" ? undefined : Number(v)))
        .pipe(z.number().int().positive().optional()),
    descripcion: z.string().trim().min(1, "Cuéntanos qué pasa").max(20000, maximo(20000)),
}) satisfies z.ZodType<Omit<Schema<"EntradaCrearTicketPortal">, "servicioId">, { asunto: string; categoriaId: string; descripcion: string }>;

export type EntradaFormNuevo = z.input<typeof esquemaNuevoTicket>;
export type DatosNuevoTicket = z.output<typeof esquemaNuevoTicket>;

/** Cuerpo de `POST /portal/tickets` (sin campos vacíos: la API rechaza los desconocidos). */
export function entradaNuevoTicket(d: DatosNuevoTicket): Schema<"EntradaCrearTicketPortal"> {
    return { asunto: d.asunto, descripcion: d.descripcion, ...(d.categoriaId !== undefined ? { categoriaId: d.categoriaId } : {}) };
}

export const esquemaResponderPortal = z.object({
    cuerpo: z.string().trim().min(1, "Escribe tu mensaje").max(20000, maximo(20000)),
}) satisfies z.ZodType<Omit<Schema<"EntradaResponderTicketPortal">, "version">, { cuerpo: string }>;

export type DatosResponderPortal = z.output<typeof esquemaResponderPortal>;
