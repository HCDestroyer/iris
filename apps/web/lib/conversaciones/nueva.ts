import { z } from "zod";
import type { PlantillaDisponible } from "./tipos";

/*
 * «Nueva conversación» (fase 1.1): validación (espejo de `POST /conversaciones`) y vista previa de
 * la plantilla. La vista previa es texto plano: lo que escribe el agente nunca se interpreta.
 */

/** `{{1}}` o `{{nombre}}` en el texto de referencia de una plantilla. */
const VARIABLE = /\{\{\s*([\w.]+)\s*\}\}/g;

export type TrozoVista = { tipo: "texto"; texto: string } | { tipo: "variable"; nombre: string; valor: string };

/** Parte la vista previa en texto y variables (para resaltar lo que se reemplazó). */
export function trozosVistaPrevia(vista: string, valores: Readonly<Record<string, string>>): TrozoVista[] {
    const trozos: TrozoVista[] = [];
    let desde = 0;
    for (const m of vista.matchAll(VARIABLE)) {
        const i = m.index;
        if (i > desde) trozos.push({ tipo: "texto", texto: vista.slice(desde, i) });
        const nombre = m[1] ?? "";
        trozos.push({ tipo: "variable", nombre, valor: (valores[nombre] ?? "").trim() || `{{${nombre}}}` });
        desde = i + m[0].length;
    }
    if (desde < vista.length) trozos.push({ tipo: "texto", texto: vista.slice(desde) });
    return trozos;
}

export const textoVistaPrevia = (vista: string, valores: Readonly<Record<string, string>>) =>
    trozosVistaPrevia(vista, valores)
        .map((t) => (t.tipo === "texto" ? t.texto : t.valor))
        .join("");

/** Primer nombre para la variable «nombre» o `{{1}}` («Carlos Ruiz» → «Carlos»). */
export function primerNombre(nombre: string): string {
    return /^\+?\d/.test(nombre.trim()) ? "" : (nombre.trim().split(/\s+/)[0] ?? "");
}

/** Valores iniciales de las variables: el primer nombre del contacto en la primera variable de nombre. */
export function valoresIniciales(p: PlantillaDisponible | undefined, nombreContacto: string | undefined): Record<string, string> {
    const v: Record<string, string> = {};
    for (const [i, variable] of (p?.variables ?? []).entries()) {
        v[variable.nombre] = nombreContacto && (variable.nombre === "nombre" || (i === 0 && variable.nombre === "1")) ? primerNombre(nombreContacto) : "";
    }
    return v;
}

export const esquemaNuevaConversacion = z.object({
    contacto: z.string().min(1, "Elige a quién escribirle"),
    canalId: z.string().min(1, "Elige el número de salida"),
    plantilla: z.string().min(1, "Elige una plantilla aprobada"),
    variables: z.record(z.string(), z.string().max(1024, "Hasta 1024 caracteres")),
});

/** Variables vacías: todas las de una plantilla de Meta son obligatorias (el servidor da 422); el panel lo dice antes. */
export function variablesFaltantes(p: PlantillaDisponible | undefined, valores: Readonly<Record<string, string>>): string[] {
    return (p?.variables ?? []).filter((v) => !(valores[v.nombre] ?? "").trim()).map((v) => v.nombre);
}

export type ValoresNueva = z.infer<typeof esquemaNuevaConversacion>;
