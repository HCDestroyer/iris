import { z } from "zod";
import type { Pendiente } from "./presentacion";

/** «Resolver a mano»: solo lo que Argos rechazó por negocio (también con datos incompletos). */
export function resolubleAMano(p: Pick<Pendiente, "estado">): boolean {
    return p.estado === "fallido_negocio";
}

/** Respuesta a «¿Argos ya cobró el cargo?»: Sí → el cargo también queda resuelto; No → sale hacia Argos. */
export type DecisionCargo = "resolver_tambien" | "liberar";

export const esquemaResolverAMano = z.object({
    motivo: z.string().trim().min(5, "Cuenta qué se hizo en Argos (al menos 5 caracteres)").max(1000, "Admite hasta 1000 caracteres"),
    cargo: z.enum(["resolver_tambien", "liberar"]).optional(),
});

export type DatosResolverAMano = z.infer<typeof esquemaResolverAMano>;

/** Cuerpo de `POST /integraciones/pendientes/{id}/resolver-manual`. */
export function cuerpoResolverAMano(d: DatosResolverAMano, version: number): { version: number; motivo: string; cargo?: DecisionCargo } {
    return { version, motivo: d.motivo.trim(), ...(d.cargo ? { cargo: d.cargo } : {}) };
}

/** Texto de un error de Argos que se corrige desde Iris (`corregible`). */
export function textoCorregible(codigo: string | null | undefined): string {
    switch (codigo) {
        case "tecnico_no_encontrado":
            return "Argos no encontró al técnico: vincúlalo con su usuario de Argos en su ficha y reintenta.";
        case "tecnico_sin_bodega":
            return "El técnico no tiene bodega en Argos: dásela en Argos y reintenta.";
        case "tecnico_ambiguo":
            return "Argos tiene más de un usuario para este técnico: vincúlalo con el correcto en su ficha y reintenta.";
        case "bodega_id_requerida":
            return "Argos volvió a una versión sin inventario por técnico: el reintento manda la bodega de la orden.";
        default:
            return "Se puede corregir: revisa el vínculo del técnico con Argos (o su bodega) y reintenta.";
    }
}
