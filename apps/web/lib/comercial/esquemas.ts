import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { esDia, isoDesdeDia, minutosDeTexto } from "../despacho/fechas";
import type { OrigenProspecto, TipoActividad, TipoLinea } from "./presentacion";

/*
 * Espejo de los validadores de la API comercial (OpenAPI: EntradaCrearProspecto,
 * EntradaActividad, EntradaPerder, EntradaFactibilidad, EntradaCrearCotizacion, EntradaFirmar;
 * apps/api/internal/comercial). Los formularios trabajan con texto (lo que hay en los campos) y
 * `cuerpo…` arma la entrada tipada del contrato.
 */

const runas = (s: string) => [...s].length;
const hasta = (max: number, que = "Admite") => (v: string) => runas(v) <= max || `${que} hasta ${max} caracteres`;
const textoOpcional = (max: number) => z.string().trim().refine((v) => runas(v) <= max, `Admite hasta ${max} caracteres`);
const textoObligatorio = (max: number, falta: string) =>
    z
        .string()
        .trim()
        .min(1, falta)
        .refine((v) => runas(v) <= max, `Admite hasta ${max} caracteres`);

// net/mail (Go) acepta casi cualquier «algo@dominio».
const PATRON_CORREO = /^[^\s@]+@[^\s@]+$/;
/** Monto en texto decimal, como lo pide la API (`^\d{1,12}(\.\d{1,2})?$`). */
export const PATRON_MONTO = /^[0-9]{1,12}(\.[0-9]{1,2})?$/;
/** Cantidad de una línea (`^\d{1,8}(\.\d{1,2})?$`). */
export const PATRON_CANTIDAD = /^[0-9]{1,8}(\.[0-9]{1,2})?$/;

/** Número decimal de un campo de texto (coma o punto); `undefined` si está vacío o no es número. */
export function leerNumero(v: string): number | undefined {
    const limpio = v.trim().replace(",", ".");
    if (limpio === "") return undefined;
    const n = Number(limpio);
    return Number.isFinite(n) ? n : undefined;
}

const coordenada = (min: number, max: number, nombre: string) =>
    z
        .string()
        .trim()
        .refine((v) => v === "" || (leerNumero(v) !== undefined && (leerNumero(v) ?? 0) >= min && (leerNumero(v) ?? 0) <= max), `${nombre} entre ${min} y ${max}`);

const ubicacion = {
    lat: coordenada(-90, 90, "Latitud"),
    lon: coordenada(-180, 180, "Longitud"),
};

/** La API exige lat y lon juntas (422 en `lat`). */
function coordenadasJuntas(d: { lat: string; lon: string }, ctx: z.RefinementCtx) {
    if ((d.lat.trim() === "") !== (d.lon.trim() === "")) {
        ctx.addIssue({ code: "custom", path: [d.lat.trim() === "" ? "lat" : "lon"], message: "Indica latitud y longitud juntas" });
    }
}

// --- Prospecto ---------------------------------------------------------------------------------

export const esquemaProspecto = z
    .object({
        nombre: textoObligatorio(200, "Escribe el nombre").refine((v) => v === "" || runas(v) >= 2, "Al menos 2 caracteres"),
        direccion: textoObligatorio(300, "Escribe la dirección"),
        telefono: textoOpcional(30),
        correo: textoOpcional(200).refine((v) => v === "" || PATRON_CORREO.test(v), "Escribe un correo válido"),
        documento: textoOpcional(40),
        origen: z.enum(["panel", "web", "referido", "otro"] satisfies OrigenProspecto[]),
        origenDetalle: textoOpcional(200),
        zonaId: z.string(),
        planInteresNombre: textoOpcional(200),
        notas: textoOpcional(4000),
        ...ubicacion,
    })
    .superRefine(coordenadasJuntas);

export type DatosProspecto = z.infer<typeof esquemaProspecto>;

export const PROSPECTO_VACIO: DatosProspecto = {
    nombre: "",
    direccion: "",
    telefono: "",
    correo: "",
    documento: "",
    origen: "panel",
    origenDetalle: "",
    zonaId: "",
    planInteresNombre: "",
    notas: "",
    lat: "",
    lon: "",
};

const opcional = <K extends string>(clave: K, v: string): Partial<Record<K, string>> => (v.trim() ? ({ [clave]: v.trim() } as Record<K, string>) : {});

export function cuerpoCrearProspecto(d: DatosProspecto): Schema<"EntradaCrearProspecto"> {
    const zona = Number(d.zonaId);
    const lat = leerNumero(d.lat);
    const lon = leerNumero(d.lon);
    return {
        nombre: d.nombre.trim(),
        direccion: d.direccion.trim(),
        origen: d.origen,
        ...opcional("telefono", d.telefono),
        ...opcional("correo", d.correo),
        ...opcional("documento", d.documento),
        ...opcional("origenDetalle", d.origenDetalle),
        ...opcional("planInteresNombre", d.planInteresNombre),
        ...opcional("notas", d.notas),
        ...(Number.isSafeInteger(zona) && zona > 0 ? { zonaId: zona } : {}),
        ...(lat !== undefined && lon !== undefined ? { lat, lon } : {}),
    };
}

// --- Actividad ---------------------------------------------------------------------------------

export const esquemaActividad = z
    .object({
        tipo: z.enum(["llamada", "visita", "nota", "mensaje"] satisfies TipoActividad[]),
        descripcion: textoObligatorio(4000, "Describe qué pasó"),
        resultado: textoOpcional(500),
        proximaAccion: textoOpcional(500),
        /** Día de Guatemala (`AAAA-MM-DD`) y hora opcional (`HH:MM`). */
        proximaDia: z.string(),
        proximaHora: z.string(),
    })
    .superRefine((d, ctx) => {
        if (d.proximaDia !== "" && !esDia(d.proximaDia)) {
            ctx.addIssue({ code: "custom", path: ["proximaDia"], message: "Fecha inválida" });
        }
        if (d.proximaHora !== "" && minutosDeTexto(d.proximaHora) === undefined) {
            ctx.addIssue({ code: "custom", path: ["proximaHora"], message: "Hora inválida" });
        }
        if (d.proximaHora !== "" && d.proximaDia === "") {
            ctx.addIssue({ code: "custom", path: ["proximaDia"], message: "Indica el día" });
        }
        if (d.proximaDia !== "" && d.proximaAccion.trim() === "") {
            ctx.addIssue({ code: "custom", path: ["proximaAccion"], message: "¿Qué hay que hacer ese día?" });
        }
    });

export type DatosActividad = z.infer<typeof esquemaActividad>;

export const ACTIVIDAD_VACIA: DatosActividad = { tipo: "llamada", descripcion: "", resultado: "", proximaAccion: "", proximaDia: "", proximaHora: "" };

export function cuerpoActividad(d: DatosActividad): Schema<"EntradaActividad"> {
    // Sin hora, las 9:00 de Guatemala (la API guarda un instante).
    const minutos = d.proximaHora ? (minutosDeTexto(d.proximaHora) ?? 9 * 60) : 9 * 60;
    return {
        tipo: d.tipo,
        descripcion: d.descripcion.trim(),
        ...opcional("resultado", d.resultado),
        ...opcional("proximaAccion", d.proximaAccion),
        ...(d.proximaDia && esDia(d.proximaDia) ? { proximaFecha: isoDesdeDia(d.proximaDia, minutos) } : {}),
    };
}

// --- Transiciones ------------------------------------------------------------------------------

export const esquemaPerder = z.object({ motivo: textoObligatorio(1000, "Escribe por qué se perdió") });
export type DatosPerder = z.infer<typeof esquemaPerder>;

export const esquemaReabrir = z.object({ motivo: textoOpcional(1000) });
export type DatosReabrir = z.infer<typeof esquemaReabrir>;

// --- Factibilidad manual (`POST /prospectos/{id}/factibilidad-manual`) ---------------------------

/** Mínimo de las notas de la visita técnica (EntradaFactibilidadManual). */
export const MINIMO_NOTAS_VISITA = 20;

/**
 * `anulaAutomatica`: la última evaluación es automática y `no_viable`; un resultado distinto la
 * anula y la API exige el motivo.
 */
export function esquemaFactibilidadManual(anulaAutomatica: boolean) {
    return z
        .object({
            resultado: z.enum(["viable", "requiere_obra", "no_viable"]),
            caja: textoOpcional(120),
            routerId: z.string(),
            notas: z
                .string()
                .trim()
                .refine((v) => runas(v) >= MINIMO_NOTAS_VISITA, `Describe la visita (al menos ${MINIMO_NOTAS_VISITA} caracteres)`)
                .refine(hasta(2000)),
            motivoAnulacion: textoOpcional(1000),
        })
        .superRefine((d, ctx) => {
            if (anulaAutomatica && d.resultado !== "no_viable" && d.motivoAnulacion === "") {
                ctx.addIssue({ code: "custom", path: ["motivoAnulacion"], message: "Explica por qué la visita contradice la evaluación automática" });
            }
        });
}
export type DatosFactibilidadManual = z.infer<ReturnType<typeof esquemaFactibilidadManual>>;

export const FACTIBILIDAD_MANUAL_VACIA: DatosFactibilidadManual = { resultado: "viable", caja: "", routerId: "", notas: "", motivoAnulacion: "" };

export function cuerpoFactibilidadManual(d: DatosFactibilidadManual, version: number): Schema<"EntradaFactibilidadManual"> {
    const router = Number(d.routerId);
    return {
        version,
        resultado: d.resultado,
        notas: d.notas.trim(),
        ...opcional("caja", d.caja),
        ...(Number.isSafeInteger(router) && router > 0 ? { routerId: router } : {}),
        ...(d.resultado !== "no_viable" ? opcional("motivoAnulacion", d.motivoAnulacion) : {}),
    };
}

// --- Cotización --------------------------------------------------------------------------------

export const esquemaLinea = z.object({
    tipo: z.enum(["plan", "instalacion", "equipo", "descuento", "otro"] satisfies TipoLinea[]),
    /** Plan de Argos (con conector); vacío sin conector. */
    planId: z.string(),
    descripcion: textoOpcional(200),
    cantidad: z.string().trim().refine((v) => v === "" || PATRON_CANTIDAD.test(v), "Usa un número con hasta 2 decimales"),
    precioUnitario: z.string().trim().refine((v) => v === "" || PATRON_MONTO.test(v), "Usa un monto como 250 o 250.50"),
    recurrente: z.boolean(),
});
export type DatosLinea = z.infer<typeof esquemaLinea>;

/**
 * La cotización según haya planes de Argos (`planes`) o no (`null`, sin conector): exactamente
 * una línea de plan; con planes, el plan es de Argos (su precio no se pisa; si Argos no lo
 * publica, va a mano); sin planes, descripción y precio a mano. Las demás, descripción y precio.
 */
export function esquemaCotizacion(planes: readonly Schema<"PlanComercial">[] | null) {
    return z
        .object({
            lineas: z.array(esquemaLinea).min(1, "Agrega al menos el plan"),
            vigenteDia: z.string(),
            notas: textoOpcional(2000),
        })
        .superRefine((d, ctx) => {
            const lineasPlan = d.lineas.filter((l) => l.tipo === "plan").length;
            if (lineasPlan !== 1) {
                ctx.addIssue({ code: "custom", path: ["lineas"], message: "La cotización lleva exactamente una línea de plan" });
            }
            d.lineas.forEach((l, i) => {
                const enLinea = (campo: keyof DatosLinea, message: string) => ctx.addIssue({ code: "custom", path: ["lineas", i, campo], message });
                if (l.tipo === "plan" && planes !== null) {
                    const plan = planes.find((p) => String(p.id) === l.planId);
                    if (!plan) enLinea("planId", "Elige el plan");
                    else if (plan.precio === null && l.precioUnitario.trim() === "") enLinea("precioUnitario", "Argos no publica el precio de este plan: escríbelo");
                    return;
                }
                if (l.descripcion.trim() === "") enLinea("descripcion", "Es obligatoria");
                if (l.precioUnitario.trim() === "") enLinea("precioUnitario", "Es obligatorio");
            });
            if (d.vigenteDia !== "" && !esDia(d.vigenteDia)) {
                ctx.addIssue({ code: "custom", path: ["vigenteDia"], message: "Fecha inválida" });
            }
        });
}
export type DatosCotizacion = z.infer<ReturnType<typeof esquemaCotizacion>>;

export function lineaVacia(tipo: TipoLinea): DatosLinea {
    return { tipo, planId: "", descripcion: "", cantidad: "1", precioUnitario: "", recurrente: tipo === "plan" };
}

/** Planes de Argos con precio: el precio va del plan (la API rechaza pisarlo). */
export function cuerpoCotizacion(d: DatosCotizacion, version: number, planes: readonly Schema<"PlanComercial">[] | null): Schema<"EntradaCrearCotizacion"> {
    return {
        version,
        lineas: d.lineas.map((l): Schema<"EntradaLinea"> => {
            // La línea de plan va con cantidad 1 (la API la rechaza con otra).
            const cantidad = l.tipo !== "plan" && l.cantidad.trim() && l.cantidad.trim() !== "1" ? { cantidad: l.cantidad.trim() } : {};
            if (l.tipo === "plan" && planes !== null) {
                const plan = planes.find((p) => String(p.id) === l.planId);
                return {
                    tipo: "plan",
                    planId: Number(l.planId),
                    ...cantidad,
                    // Si Argos no publica el precio, va a mano.
                    ...(plan && plan.precio === null && l.precioUnitario.trim() ? { precioUnitario: l.precioUnitario.trim() } : {}),
                };
            }
            return {
                tipo: l.tipo,
                descripcion: l.descripcion.trim(),
                precioUnitario: l.precioUnitario.trim(),
                ...cantidad,
                ...(l.tipo === "plan" ? {} : l.tipo === "instalacion" ? {} : { recurrente: l.recurrente }),
            };
        }),
        ...(d.vigenteDia && esDia(d.vigenteDia) ? { vigenteHasta: isoDesdeDia(d.vigenteDia, 23 * 60 + 59) } : {}),
        ...opcional("notas", d.notas),
    };
}

// --- Firma del contrato ------------------------------------------------------------------------

export const esquemaFirma = z.object({
    firmanteNombre: textoObligatorio(200, "Escribe el nombre de quien firma"),
    firmanteDocumento: textoOpcional(40),
});
export type DatosFirma = z.infer<typeof esquemaFirma>;

export function cuerpoFirma(d: DatosFirma, version: number, svg: string): Schema<"EntradaFirmar"> {
    return { version, firmanteNombre: d.firmanteNombre.trim(), svg, ...opcional("firmanteDocumento", d.firmanteDocumento) };
}
