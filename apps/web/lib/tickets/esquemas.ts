import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { ORDEN_PRIORIDADES, prioridadMayor, type Prioridad } from "./presentacion";

/*
 * Espejo de los validadores de la API de tickets (OpenAPI: Entrada*Ticket). Los formularios no
 * llevan `version`: la pone la pantalla con la que leyó. Los `satisfies` hacen fallar typecheck
 * si el contrato cambia de forma.
 */

const texto = (min: number, max: number, vacio: string, corto: string) =>
    z
        .string()
        .trim()
        .min(1, { message: vacio, abort: true })
        .min(min, corto)
        .max(max, `Admite hasta ${max.toLocaleString("es-GT")} caracteres`);

const textoOpcional = (max: number) =>
    z
        .string()
        .trim()
        .max(max, `Admite hasta ${max.toLocaleString("es-GT")} caracteres`)
        .transform((v) => (v === "" ? undefined : v));

const prioridad = z.enum(ORDEN_PRIORIDADES as [Prioridad, ...Prioridad[]]);

/** Id numérico opcional desde un `<select>` (cadena vacía = ninguno). */
const idOpcional = z
    .string()
    .transform((v) => (v === "" ? undefined : Number(v)))
    .pipe(z.number().int().positive().optional());

export const esquemaResponder = z.object({
    cuerpo: texto(1, 20000, "Escribe la respuesta", "Escribe la respuesta"),
}) satisfies z.ZodType<Omit<Schema<"EntradaResponderTicket">, "version">, { cuerpo: string }>;

export const esquemaNotaInterna = z.object({
    cuerpo: texto(1, 20000, "Escribe la nota", "Escribe la nota"),
}) satisfies z.ZodType<Omit<Schema<"EntradaNotaInterna">, "version">, { cuerpo: string }>;

export const esquemaAsignar = z.object({
    usuarioId: idOpcional,
}) satisfies z.ZodType<Omit<Schema<"EntradaAsignarTicket">, "version" | "cuadrillaId">, { usuarioId: string }>;

const motivo = texto(3, 500, "Escribe el motivo", "El motivo necesita al menos 3 caracteres");

/** La prioridad nueva, si se elige, debe ser mayor que la actual (422 `prioridad_no_mayor`). */
export function esquemaEscalar(actual: Prioridad) {
    return z.object({
        motivo,
        prioridad: z
            .union([z.literal(""), prioridad])
            .transform((v) => (v === "" ? undefined : v))
            .refine((v) => v === undefined || prioridadMayor(v, actual), "Elige una prioridad mayor que la actual"),
    }) satisfies z.ZodType<Omit<Schema<"EntradaEscalarTicket">, "version">, { motivo: string; prioridad: string }>;
}

export const esquemaPonerEnEspera = z.object({ motivo }) satisfies z.ZodType<Omit<Schema<"EntradaPonerEnEspera">, "version">>;

export const esquemaReanudar = z.object({
    motivo: textoOpcional(500),
}) satisfies z.ZodType<Omit<Schema<"EntradaReanudarTicket">, "version">, { motivo: string }>;

export const esquemaResolver = z.object({
    solucion: texto(3, 20000, "Describe la solución", "La solución necesita al menos 3 caracteres"),
}) satisfies z.ZodType<Omit<Schema<"EntradaResolverTicket">, "version">, { solucion: string }>;

export const esquemaReabrir = z.object({ motivo }) satisfies z.ZodType<Omit<Schema<"EntradaReabrirTicket">, "version">>;

/** El motivo es obligatorio si el ticket no está resuelto. */
export function esquemaCerrar(resuelto: boolean) {
    return z.object({
        motivo: resuelto ? textoOpcional(500) : texto(1, 500, "Escribe por qué se cierra sin resolver", "Escribe el motivo"),
    }) satisfies z.ZodType<Omit<Schema<"EntradaCerrarTicket">, "version">, { motivo: string }>;
}

/** El destino se escribe como número legible (TK-000123) o id; la pantalla lo resuelve a id. */
export const esquemaFusionar = z.object({
    destino: z
        .string()
        .trim()
        .min(1, "Escribe el número del ticket que se queda")
        .regex(/^(TK-?\d{1,12}|\d{1,12})$/i, "Escribe un número como TK-000123"),
    motivo: textoOpcional(500),
});

const PATRON_CORREO = /^[^\s@]+@[^\s@]+$/;

export const esquemaCrearTicket = z.object({
    asunto: texto(3, 200, "Escribe el asunto", "El asunto necesita al menos 3 caracteres"),
    categoriaId: idOpcional,
    prioridad,
    descripcion: textoOpcional(20000),
    clienteId: z.number().int().positive().optional(),
    servicioId: z.number().int().positive().optional(),
    contacto: z.object({
        nombre: textoOpcional(120),
        telefono: textoOpcional(40),
        correo: z
            .string()
            .trim()
            .max(254, "Admite hasta 254 caracteres")
            .refine((v) => v === "" || PATRON_CORREO.test(v), "Escribe un correo válido, como nombre@empresa.com")
            .transform((v) => (v === "" ? undefined : v)),
    }),
});

export type EntradaFormCrear = z.input<typeof esquemaCrearTicket>;
export type DatosCrearTicket = z.output<typeof esquemaCrearTicket>;

/** Cuerpo de `POST /tickets`: sin claves vacías (la API rechaza `contacto` con campos vacíos). */
export function entradaCrearTicket(datos: DatosCrearTicket): Schema<"EntradaCrearTicket"> {
    const contacto: Schema<"EntradaContacto"> = {};
    if (datos.contacto.nombre) contacto.nombre = datos.contacto.nombre;
    if (datos.contacto.telefono) contacto.telefono = datos.contacto.telefono;
    if (datos.contacto.correo) contacto.correo = datos.contacto.correo;
    return {
        asunto: datos.asunto,
        prioridad: datos.prioridad,
        ...(datos.categoriaId !== undefined ? { categoriaId: datos.categoriaId } : {}),
        ...(datos.descripcion !== undefined ? { descripcion: datos.descripcion } : {}),
        ...(datos.clienteId !== undefined ? { clienteId: datos.clienteId } : {}),
        ...(datos.clienteId !== undefined && datos.servicioId !== undefined ? { servicioId: datos.servicioId } : {}),
        ...(Object.keys(contacto).length ? { contacto } : {}),
    };
}

/** «TK-000123», «tk123» o «123» → texto para buscar y número para comparar. */
export function normalizarNumeroTicket(valor: string): { numero: string; digitos: number } {
    const digitos = Number(valor.replace(/\D/g, ""));
    return { numero: `TK-${String(digitos).padStart(6, "0")}`, digitos };
}
