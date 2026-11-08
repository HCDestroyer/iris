import { z } from "zod";

/** «sin servicio, ONU zte» → ["sin servicio", "onu zte"] (minúsculas, sin repetir, hasta 10). */
export function leerEtiquetas(texto: string): string[] {
    return [...new Set(texto.split(",").map((e) => e.trim().toLowerCase().replace(/\s+/g, " ")).filter(Boolean))].slice(0, 10);
}

/** Espejo del validador del servidor (contratos/conversaciones.md §11), un poco más estricto. */
export const esquemaManual = z.object({
    titulo: z.string().trim().min(3, "Escribe el título").max(140, "Hasta 140 caracteres"),
    resumen: z.string().trim().min(1, "Escribe un resumen de una o dos líneas").max(300, "Hasta 300 caracteres"),
    cuerpo: z.string().trim().min(1, "Escribe el contenido").max(20000, "Hasta 20000 caracteres"),
    etiquetas: z.string().max(400).refine((t) => leerEtiquetas(t).every((e) => e.length <= 40), "Cada etiqueta, hasta 40 caracteres"),
    equipo: z.string().max(80, "Hasta 80 caracteres"),
    visibilidad: z.enum(["interna", "cliente"]),
});
