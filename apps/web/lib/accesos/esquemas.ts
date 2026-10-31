import type { operations, Schema } from "@iris/contratos";
import { z } from "zod";

/*
 * Espejo de los validadores de la API (identidad/roles.go y accesos.go). `satisfies` con el
 * contrato hace que typecheck falle si cambia su forma.
 */

export const PATRON_CODIGO_ROL = /^[a-z][a-z0-9_]{1,39}$/;

export const esquemaRol = z.object({
    codigo: z
        .string()
        .trim()
        .min(2, "Escribe un código de 2 caracteres o más")
        .max(40, "El código admite hasta 40 caracteres")
        .regex(PATRON_CODIGO_ROL, "Solo minúsculas, números y guion bajo; empieza con una letra (p. ej. despacho_norte)"),
    nombre: z.string().trim().min(1, "Escribe un nombre").max(80, "El nombre admite hasta 80 caracteres"),
    descripcion: z.string().trim().max(300, "La descripción admite hasta 300 caracteres"),
    permisos: z.array(z.string()).min(1, "Elige al menos un permiso"),
});

export type DatosRol = z.infer<typeof esquemaRol>;

/** Cuerpo de `POST /roles` a partir del formulario. */
export function cuerpoCrearRol(d: DatosRol): Schema<"EntradaCrearRol"> {
    return {
        codigo: d.codigo.trim(),
        nombre: d.nombre.trim(),
        ...(d.descripcion.trim() ? { descripcion: d.descripcion.trim() } : {}),
        permisos: [...new Set(d.permisos)].sort() as Schema<"EntradaCrearRol">["permisos"],
    };
}

/** Cuerpo de `POST /roles/{id}/editar` (el código no cambia). */
export function cuerpoEditarRol(d: DatosRol, version: number): Schema<"EntradaEditarRol"> {
    return {
        version,
        nombre: d.nombre.trim(),
        descripcion: d.descripcion.trim(),
        permisos: [...new Set(d.permisos)].sort() as NonNullable<Schema<"EntradaEditarRol">["permisos"]>,
    };
}

export const esquemaAsignarRol = z
    .object({
        rol: z.string().min(1, "Elige un rol"),
        alcance: z.enum(["global", "zona", "cuadrilla"]),
        zonaId: z.string(),
        cuadrillaId: z.string(),
    })
    .superRefine((d, ctx) => {
        if (d.alcance === "zona" && !d.zonaId) {
            ctx.addIssue({ code: "custom", path: ["zonaId"], message: "Elige la zona" });
        }
        if (d.alcance === "cuadrilla" && !d.cuadrillaId) {
            ctx.addIssue({ code: "custom", path: ["cuadrillaId"], message: "Elige la cuadrilla" });
        }
    });

export type DatosAsignarRol = z.infer<typeof esquemaAsignarRol>;

/** Cuerpo de `POST /usuarios/{id}/asignar-rol`: zona o cuadrilla, nunca ambas. */
export function cuerpoAsignarRol(d: DatosAsignarRol): Schema<"AsignacionRol"> {
    if (d.alcance === "zona") return { rol: d.rol, zonaId: Number(d.zonaId) };
    if (d.alcance === "cuadrilla") return { rol: d.rol, cuadrillaId: Number(d.cuadrillaId) };
    return { rol: d.rol };
}

/** ¿Falta algo para poder guardar la asignación? (el botón queda deshabilitado y se dice qué). */
export function faltaEnAsignacion(d: Partial<DatosAsignarRol>): string | null {
    if (!d.rol) return "Elige un rol para poder asignarlo.";
    if (d.alcance === "zona" && !d.zonaId) return "Elige la zona del alcance.";
    if (d.alcance === "cuadrilla" && !d.cuadrillaId) return "Elige la cuadrilla del alcance.";
    return null;
}

export const esquemaFiltrosAccesos = z
    .object({
        usuarioId: z.string(),
        canal: z.string(),
        resultado: z.string(),
        desde: z.string(),
        hasta: z.string(),
    })
    .refine((d) => !d.desde || !d.hasta || d.hasta >= d.desde, { path: ["hasta"], message: "«Hasta» debe ser igual o posterior a «Desde»" });

export type FiltrosAccesos = z.infer<typeof esquemaFiltrosAccesos>;

export const FILTROS_VACIOS: FiltrosAccesos = { usuarioId: "", canal: "", resultado: "", desde: "", hasta: "" };

/** Una fecha `aaaa-mm-dd` del día en Guatemala (UTC−6, sin horario de verano) a su inicio en UTC. */
export function inicioDiaGuatemala(fecha: string): string {
    return new Date(`${fecha}T00:00:00-06:00`).toISOString();
}

export type QueryAccesos = NonNullable<operations["listarAccesos"]["parameters"]["query"]>;

/** Query de `GET /accesos` a partir de los filtros (hasta = fin del día elegido). */
export function queryAccesos(f: FiltrosAccesos): QueryAccesos {
    const q: QueryAccesos = {};
    if (f.usuarioId) q.usuarioId = Number(f.usuarioId);
    if (f.canal) q.canal = f.canal as QueryAccesos["canal"];
    if (f.resultado) q.resultado = f.resultado as QueryAccesos["resultado"];
    if (f.desde) q.desde = inicioDiaGuatemala(f.desde);
    if (f.hasta) q.hasta = new Date(Date.parse(inicioDiaGuatemala(f.hasta)) + 24 * 60 * 60 * 1000).toISOString();
    return q;
}
