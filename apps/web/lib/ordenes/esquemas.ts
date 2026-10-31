import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { ORDEN_TIPOS, type TipoOrden } from "./presentacion";

/*
 * Espejo de los validadores de la API de órdenes (OpenAPI: EntradaCrearOrden,
 * EntradaAsignarOrden, EntradaProgramarOrden, EntradaMotivo). Los formularios no llevan
 * `version`: la pone la pantalla con la que leyó. Los `satisfies` hacen fallar typecheck si el
 * contrato cambia de forma.
 */

const maximo = (max: number) => `Admite hasta ${max.toLocaleString("es-GT")} caracteres`;

const textoOpcional = (max: number) =>
    z
        .string()
        .trim()
        .max(max, maximo(max))
        .transform((v) => (v === "" ? undefined : v));

/** Id numérico opcional desde un `<select>` o un campo de texto (vacío = ninguno). */
const idOpcional = z
    .string()
    .trim()
    .refine((v) => v === "" || /^\d{1,15}$/.test(v), "Escribe un número")
    .transform((v) => (v === "" ? undefined : Number(v)))
    .pipe(z.number().int().positive("Escribe un número mayor que 0").optional());

const coordenada = (min: number, max: number, nombre: string) =>
    z
        .string()
        .trim()
        .refine((v) => v === "" || (/^-?\d{1,3}(\.\d+)?$/.test(v) && Number(v) >= min && Number(v) <= max), `La ${nombre} va de ${min} a ${max}`)
        .transform((v) => (v === "" ? undefined : Number(v)));

/** Motivo de cancelar, reabrir y reprogramar: 3 a 500 caracteres. */
const motivo = z
    .string()
    .trim()
    .min(1, { message: "Escribe el motivo", abort: true })
    .min(3, "El motivo necesita al menos 3 caracteres")
    .max(500, maximo(500));

const tipo = z.enum(ORDEN_TIPOS as [TipoOrden, ...TipoOrden[]], { error: "Elige el tipo de orden" });

export const esquemaCrearOrden = z
    .object({
        tipo,
        plantillaId: idOpcional,
        descripcion: textoOpcional(4000),
        direccion: textoOpcional(300),
        zonaId: idOpcional,
        lat: coordenada(-90, 90, "latitud"),
        lon: coordenada(-180, 180, "longitud"),
        contacto: z.object({ nombre: textoOpcional(120), telefono: textoOpcional(40) }),
        /** Los pone la pantalla (ticket de origen o cliente elegido en el buscador). */
        ticketId: z.number().int().positive().optional(),
        clienteId: z.number().int().positive().optional(),
        servicioId: z.number().int().positive().optional(),
    })
    .superRefine((d, ctx) => {
        // Sin ticket ni cliente no hay instantánea de Argos de donde sacar la dirección (422 en la API).
        if (d.ticketId === undefined && d.clienteId === undefined && d.direccion === undefined) {
            ctx.addIssue({ code: "custom", path: ["direccion"], message: "Escribe la dirección: sin cliente de Argos no hay de dónde tomarla" });
        }
        if ((d.lat === undefined) !== (d.lon === undefined)) {
            ctx.addIssue({ code: "custom", path: [d.lat === undefined ? "lat" : "lon"], message: "Escribe la latitud y la longitud, o ninguna" });
        }
    });

export type EntradaFormCrearOrden = z.input<typeof esquemaCrearOrden>;
export type DatosCrearOrden = z.output<typeof esquemaCrearOrden>;

/** Cuerpo de `POST /ordenes` sin claves vacías. */
export function entradaCrearOrden(d: DatosCrearOrden): Schema<"EntradaCrearOrden"> {
    const contacto: Schema<"EntradaContactoOrden"> = {};
    if (d.contacto.nombre) contacto.nombre = d.contacto.nombre;
    if (d.contacto.telefono) contacto.telefono = d.contacto.telefono;
    return {
        tipo: d.tipo,
        ...(d.ticketId !== undefined ? { ticketId: d.ticketId } : {}),
        ...(d.clienteId !== undefined ? { clienteId: d.clienteId } : {}),
        ...(d.clienteId !== undefined && d.servicioId !== undefined ? { servicioId: d.servicioId } : {}),
        ...(d.plantillaId !== undefined ? { plantillaId: d.plantillaId } : {}),
        ...(d.descripcion !== undefined ? { descripcion: d.descripcion } : {}),
        ...(d.direccion !== undefined ? { direccion: d.direccion } : {}),
        ...(d.zonaId !== undefined ? { zonaId: d.zonaId } : {}),
        ...(d.lat !== undefined && d.lon !== undefined ? { lat: d.lat, lon: d.lon } : {}),
        ...(Object.keys(contacto).length ? { contacto } : {}),
    };
}

export const esquemaAsignarOrden = z.object({
    tecnicoId: idOpcional,
}) satisfies z.ZodType<Pick<Schema<"EntradaAsignarOrden">, "tecnicoId">, { tecnicoId: string }>;

const PATRON_FECHA = /^(\d{4})-(\d{2})-(\d{2})$/;
const PATRON_HORA = /^([01]\d|2[0-3]):([0-5]\d)$/;

/** Fecha y hora de America/Guatemala (UTC−6, sin horario de verano) → ISO UTC. */
export function inicioDesdeGuatemala(fecha: string, hora: string): string | undefined {
    const f = PATRON_FECHA.exec(fecha);
    const h = PATRON_HORA.exec(hora);
    if (!f || !h) return undefined;
    const [anio, mes, dia] = [Number(f[1]), Number(f[2]), Number(f[3])];
    const ms = Date.UTC(anio, mes - 1, dia, Number(h[1]) + 6, Number(h[2]));
    const local = new Date(ms - 6 * 3_600_000);
    if (local.getUTCFullYear() !== anio || local.getUTCMonth() !== mes - 1 || local.getUTCDate() !== dia) return undefined;
    return new Date(ms).toISOString().replace(".000", "");
}

/** ISO UTC → `{ fecha: AAAA-MM-DD, hora: HH:MM }` en America/Guatemala (para precargar). */
export function guatemalaDesdeIso(iso: string | null | undefined): { fecha: string; hora: string } {
    const ms = iso ? Date.parse(iso) : Number.NaN;
    if (Number.isNaN(ms)) return { fecha: "", hora: "" };
    const local = new Date(ms - 6 * 3_600_000).toISOString();
    return { fecha: local.slice(0, 10), hora: local.slice(11, 16) };
}

/**
 * Programar (pendiente → programada) y reprogramar. El técnico es obligatorio si la orden no
 * tiene; el motivo, al reprogramar. La hora se escribe en America/Guatemala.
 */
export function esquemaProgramarOrden({ requiereTecnico, reprogramar }: { requiereTecnico: boolean; reprogramar: boolean }) {
    return z
        .object({
            tecnicoId: idOpcional,
            fecha: z.string().trim().min(1, "Elige la fecha"),
            hora: z.string().trim().min(1, "Elige la hora").regex(PATRON_HORA, "Escribe la hora como 14:30"),
            motivo: reprogramar ? motivo : textoOpcional(500),
        })
        .superRefine((d, ctx) => {
            if (requiereTecnico && d.tecnicoId === undefined) {
                ctx.addIssue({ code: "custom", path: ["tecnicoId"], message: "Elige el técnico" });
            }
            if (d.fecha && !PATRON_FECHA.test(d.fecha)) {
                ctx.addIssue({ code: "custom", path: ["fecha"], message: "Escribe la fecha como 2026-10-28" });
            } else if (d.fecha && PATRON_HORA.test(d.hora) && inicioDesdeGuatemala(d.fecha, d.hora) === undefined) {
                ctx.addIssue({ code: "custom", path: ["fecha"], message: "Esa fecha no existe" });
            }
        })
        .transform((d) => ({
            tecnicoId: d.tecnicoId,
            inicio: inicioDesdeGuatemala(d.fecha, d.hora) ?? "",
            motivo: d.motivo,
        }));
}

export type DatosProgramarOrden = z.output<ReturnType<typeof esquemaProgramarOrden>>;

/** Cuerpo de `POST /ordenes/{id}/programar|reprogramar`. */
export function entradaProgramarOrden(d: DatosProgramarOrden, version: number): Schema<"EntradaProgramarOrden"> {
    return {
        version,
        inicio: d.inicio,
        ...(d.tecnicoId !== undefined ? { tecnicoId: d.tecnicoId } : {}),
        ...(d.motivo !== undefined ? { motivo: d.motivo } : {}),
    };
}

export const esquemaMotivoOrden = z.object({ motivo }) satisfies z.ZodType<Omit<Schema<"EntradaMotivo">, "version">>;

// --- Pausa (EntradaPausar) ------------------------------------------------------------------------

export const esquemaPausar = z
    .object({
        motivo: z.enum(["esperando_material", "cliente_ausente", "clima", "acceso", "otro"]),
        nota: z.string().trim().max(2000, "Admite hasta 2000 caracteres"),
    })
    .superRefine((d, ctx) => {
        if (d.motivo === "otro" && d.nota === "") {
            ctx.addIssue({ code: "custom", path: ["nota"], message: "Con «Otro», explica el motivo" });
        }
    });
export type DatosPausar = z.infer<typeof esquemaPausar>;

export function entradaPausar(d: DatosPausar, version: number): Schema<"EntradaPausar"> {
    return { version, motivo: d.motivo, ...(d.nota ? { nota: d.nota } : {}) };
}
