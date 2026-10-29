import type { Schema } from "@iris/contratos";
import { z } from "zod";

/*
 * Plantillas de formulario (espejo de EntradaCrearPlantilla, EntradaEditarPlantilla y
 * EntradaItemPlantilla; apps/api/internal/campo/plantillas.go). El editor trabaja con texto y
 * `cuerpo…` arma la entrada del contrato: los ítems van en el orden de la lista (posición 1..n).
 */

export type Plantilla = Schema<"PlantillaFormulario">;
export type PlantillaDetalle = Schema<"PlantillaFormularioDetalle">;
export type TipoItem = Schema<"EntradaItemPlantilla">["tipo"];
export type TipoOrdenPlantilla = NonNullable<Plantilla["tipo"]>;

export const TIPOS_ITEM: Record<TipoItem, string> = {
    si_no: "Sí / no",
    texto: "Texto",
    numero: "Número",
    foto: "Foto",
    medicion: "Medición",
    seleccion: "Selección",
};
export const ORDEN_TIPOS_ITEM: readonly TipoItem[] = ["si_no", "texto", "numero", "medicion", "foto", "seleccion"];
/** Tipos con unidad (dBm, Mbps…). */
export const CON_UNIDAD: readonly TipoItem[] = ["numero", "medicion"];

export const MAX_ITEMS = 100;
export const MAX_OPCIONES = 50;
const PATRON_CODIGO_PLANTILLA = /^[a-z0-9_]{2,60}$/;
const PATRON_CODIGO_ITEM = /^[a-z0-9_]{1,60}$/;

const runas = (s: string) => [...s].length;

/** Opciones de una selección: una por línea, sin vacías. */
export function opcionesDe(texto: string): string[] {
    return texto
        .split("\n")
        .map((o) => o.trim())
        .filter((o) => o !== "");
}

/** Código sugerido a partir de un texto: minúsculas, sin acentos, `_` entre palabras. */
export function codigoDesde(texto: string, max = 60): string {
    return texto
        .normalize("NFD")
        .replace(/[̀-ͯ]/g, "")
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "_")
        .replace(/^_+|_+$/g, "")
        .slice(0, max)
        .replace(/_+$/, "");
}

export const esquemaItem = z
    .object({
        etiqueta: z
            .string()
            .trim()
            .min(1, "Escribe la pregunta o la indicación")
            .refine((v) => runas(v) <= 200, "Admite hasta 200 caracteres"),
        codigo: z.string().trim().regex(PATRON_CODIGO_ITEM, "Usa minúsculas, números y _ (hasta 60)"),
        tipo: z.enum(["si_no", "texto", "numero", "foto", "medicion", "seleccion"]),
        obligatorio: z.boolean(),
        unidad: z
            .string()
            .trim()
            .refine((v) => runas(v) <= 20, "Admite hasta 20 caracteres"),
        /** Una opción por línea (solo `seleccion`). */
        opciones: z.string(),
    })
    .superRefine((d, ctx) => {
        if (d.tipo !== "seleccion") return;
        const lista = opcionesDe(d.opciones);
        if (lista.length === 0) ctx.addIssue({ code: "custom", path: ["opciones"], message: "Escribe al menos una opción" });
        else if (lista.length > MAX_OPCIONES) ctx.addIssue({ code: "custom", path: ["opciones"], message: `Admite hasta ${MAX_OPCIONES} opciones` });
        else if (new Set(lista).size !== lista.length) ctx.addIssue({ code: "custom", path: ["opciones"], message: "Hay opciones repetidas" });
    });
export type DatosItem = z.infer<typeof esquemaItem>;

export const ITEM_VACIO: DatosItem = { etiqueta: "", codigo: "", tipo: "si_no", obligatorio: false, unidad: "", opciones: "" };

const base = {
    nombre: z
        .string()
        .trim()
        .min(1, "Escribe el nombre")
        .refine((v) => runas(v) <= 120, "Admite hasta 120 caracteres"),
    descripcion: z
        .string()
        .trim()
        .refine((v) => runas(v) <= 1000, "Admite hasta 1000 caracteres"),
    tipo: z.enum(["", "instalacion", "visita", "retiro", "cambio_equipo", "mantenimiento"]),
    requiereFirma: z.boolean(),
    items: z.array(esquemaItem).min(1, "Agrega al menos un ítem").max(MAX_ITEMS, `Admite hasta ${MAX_ITEMS} ítems`),
};

/** Códigos de ítem únicos en la plantilla (las fotos se suben con ese código). */
function codigosUnicos(d: { items: DatosItem[] }, ctx: z.RefinementCtx) {
    const vistos = new Map<string, number>();
    d.items.forEach((it, i) => {
        const c = it.codigo.trim();
        if (c === "") return;
        if (vistos.has(c)) ctx.addIssue({ code: "custom", path: ["items", i, "codigo"], message: "Ya hay otro ítem con este código" });
        else vistos.set(c, i);
    });
}

export const esquemaCrearPlantilla = z
    .object({
        ...base,
        codigo: z.string().trim().regex(PATRON_CODIGO_PLANTILLA, "Usa minúsculas, números y _ (de 2 a 60)"),
        comun: z.boolean(),
        borrador: z.boolean(),
    })
    .superRefine(codigosUnicos);
export type DatosCrearPlantilla = z.infer<typeof esquemaCrearPlantilla>;

export const esquemaEditarPlantilla = z.object(base).superRefine(codigosUnicos);
export type DatosEditarPlantilla = z.infer<typeof esquemaEditarPlantilla>;

export const PLANTILLA_VACIA: DatosCrearPlantilla = {
    codigo: "",
    nombre: "",
    descripcion: "",
    tipo: "",
    requiereFirma: false,
    comun: false,
    borrador: true,
    items: [{ ...ITEM_VACIO }],
};

function cuerpoItem(i: DatosItem): Schema<"EntradaItemPlantilla"> {
    return {
        codigo: i.codigo.trim(),
        etiqueta: i.etiqueta.trim(),
        tipo: i.tipo,
        obligatorio: i.obligatorio,
        ...(CON_UNIDAD.includes(i.tipo) && i.unidad.trim() ? { unidad: i.unidad.trim() } : {}),
        ...(i.tipo === "seleccion" ? { opciones: opcionesDe(i.opciones) } : {}),
    };
}

export function cuerpoCrearPlantilla(d: DatosCrearPlantilla): Schema<"EntradaCrearPlantilla"> {
    return {
        codigo: d.codigo.trim(),
        nombre: d.nombre.trim(),
        requiereFirma: d.requiereFirma,
        borrador: d.borrador,
        items: d.items.map(cuerpoItem),
        ...(d.descripcion.trim() ? { descripcion: d.descripcion.trim() } : {}),
        ...(d.tipo ? { tipo: d.tipo } : {}),
        ...(d.comun ? { comun: true } : {}),
    };
}

export function cuerpoEditarPlantilla(d: DatosEditarPlantilla, version: number): Schema<"EntradaEditarPlantilla"> {
    return {
        version,
        nombre: d.nombre.trim(),
        requiereFirma: d.requiereFirma,
        items: d.items.map(cuerpoItem),
        ...(d.descripcion.trim() ? { descripcion: d.descripcion.trim() } : {}),
        ...(d.tipo ? { tipo: d.tipo } : {}),
    };
}

/** Valores del editor a partir de la plantilla leída (su versión actual). */
export function datosDePlantilla(p: Pick<Plantilla, "nombre" | "descripcion" | "tipo" | "requiereFirma" | "items">): DatosEditarPlantilla {
    return {
        nombre: p.nombre,
        descripcion: p.descripcion,
        tipo: p.tipo ?? "",
        requiereFirma: p.requiereFirma,
        items: [...(p.items ?? [])]
            .sort((a, b) => a.posicion - b.posicion)
            .map((i) => ({ etiqueta: i.etiqueta, codigo: i.codigo, tipo: i.tipo, obligatorio: i.obligatorio, unidad: i.unidad, opciones: (i.opciones ?? []).join("\n") })),
    };
}
