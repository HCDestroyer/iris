// Formularios de la app de campo (react-hook-form + zod), espejo de las entradas de la API
// (apps/api/internal/campo: EntradaAgregarNota, EntradaUsarMaterial, EntradaEquipo,
// EntradaCapturarFirma, EntradaFallarOrden).
import { MOTIVOS_FALLA, MOTIVOS_PAUSA } from "@iris/sync";
import { z } from "zod";

const sinNul = (s: string) => !s.includes("\u0000");
const texto = (max: number) => z.string().trim().max(max, `Máximo ${max} caracteres`).refine(sinNul, "Texto no válido");

export const esquemaNota = z.object({
    texto: texto(4000).min(1, "Escribe la nota"),
});
export type DatosNota = z.infer<typeof esquemaNota>;

/** Cantidad con hasta 3 decimales, > 0 y ≤ 1 000 000 (metros de cable, unidades…). */
const cantidad = z
    .string()
    .trim()
    .min(1, "Indica la cantidad")
    .transform((s) => Number(s.replace(",", ".")))
    .pipe(
        z
            .number({ message: "Cantidad no válida" })
            .gt(0, "Debe ser mayor que 0")
            .max(1_000_000, "Cantidad demasiado grande")
            .refine((n) => Math.round(n * 1000) === n * 1000, "Hasta 3 decimales"),
    );

export const esquemaMaterial = z.object({
    productoId: z.string().min(1, "Elige el material").transform(Number).pipe(z.number().int().positive("Elige el material")),
    cantidad,
});
export type EntradaMaterial = z.input<typeof esquemaMaterial>;
export type DatosMaterial = z.output<typeof esquemaMaterial>;

const RE_MAC = /^([0-9A-Fa-f]{2}[:.-]?){5}[0-9A-Fa-f]{2}$/;

export const esquemaEquipo = z
    .object({
        accion: z.enum(["instalar", "retirar"]),
        serie: texto(100),
        mac: texto(30).refine((m) => m === "" || RE_MAC.test(m), "MAC no válida (12 dígitos hexadecimales)"),
        modelo: texto(100),
        unidadId: z.string(),
        condicion: z.enum(["", "buena", "danada", "perdido"]),
    })
    .superRefine((d, ctx) => {
        if (d.serie === "" && d.unidadId === "") {
            ctx.addIssue({ code: "custom", path: ["serie"], message: "Escribe o escanea la serie" });
        }
        if (d.accion === "retirar" && d.condicion === "") {
            ctx.addIssue({ code: "custom", path: ["condicion"], message: "Indica en qué condición se retira" });
        }
    });
export type DatosEquipo = z.infer<typeof esquemaEquipo>;

export const esquemaFirma = z.object({
    nombre: texto(120).min(2, "Escribe el nombre de quien firma"),
    documento: texto(40),
});
export type DatosFirma = z.infer<typeof esquemaFirma>;

export const esquemaFallar = z
    .object({
        motivoFalla: z.enum(MOTIVOS_FALLA, { message: "Elige el motivo" }),
        detalle: texto(2000),
    })
    .superRefine((d, ctx) => {
        if (d.motivoFalla === "otro" && d.detalle === "") {
            ctx.addIssue({ code: "custom", path: ["detalle"], message: "Explica el motivo" });
        }
    });
export type DatosFallar = z.infer<typeof esquemaFallar>;

export const esquemaPausar = z
    .object({
        motivo: z.enum(MOTIVOS_PAUSA, { message: "Elige el motivo" }),
        nota: texto(2000),
    })
    .superRefine((d, ctx) => {
        if (d.motivo === "otro" && d.nota === "") {
            ctx.addIssue({ code: "custom", path: ["nota"], message: "Con «Otro motivo» explica qué pasa" });
        }
    });
export type DatosPausar = z.infer<typeof esquemaPausar>;

export const esquemaCerrar = z.object({ resultado: z.enum(["exitosa", "parcial"]) });
export type DatosCerrar = z.infer<typeof esquemaCerrar>;

/** Valor numérico de un ítem `numero` o `medicion` (acepta coma decimal; puede ser negativo, p. ej. dBm). */
export const esquemaNumeroItem = z
    .string()
    .trim()
    .min(1, "Escribe el valor")
    .transform((s) => Number(s.replace(",", ".")))
    .pipe(z.number({ message: "Número no válido" }).finite("Número no válido"));
