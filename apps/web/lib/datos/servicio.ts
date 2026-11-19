import type { Schema } from "@iris/contratos";

type Servicio = Schema<"Servicio">;

/**
 * Lo que la línea de servicio sabe mostrar. Sale del esquema `Servicio` de la API: la ficha lo trae
 * entero; la conversación, el paquete de campo y las copias de tickets y órdenes, una parte (cada
 * uno se adapta con su función). Lo sensible viene solo si el servidor lo dejó pasar (§2.4).
 */
export type ServicioLinea = Pick<Servicio, "id" | "estado"> &
    Partial<Pick<Servicio, "numero" | "codigo" | "planNombre" | "zonaNombre" | "precioMensual" | "moneda" | "routerNombre" | "ip" | "direccion">>;

/** «Q250.00 al mes»; "" sin precio (Argos anterior a 1.18.0: nunca «Q0»). */
export function textoPrecio(s: Pick<ServicioLinea, "precioMensual" | "moneda">): string {
    const precio = s.precioMensual?.trim();
    if (!precio) return "";
    const simbolo = !s.moneda || s.moneda === "GTQ" ? "Q" : `${s.moneda} `;
    return `${simbolo}${precio} al mes`;
}

/** «Servicio 2 · Fibra 50 · Centro»: la etiqueta corta (la misma del asistente) para textos y opciones. */
export function etiquetaServicio(s: ServicioLinea): string {
    return [s.numero ? `Servicio ${s.numero}` : null, s.planNombre?.trim() || "Sin plan", s.zonaNombre?.trim() || null].filter(Boolean).join(" · ");
}

/** Servicio de la app de campo (`ServicioCampo`: plan, router, zona) como línea. */
export function lineaDeServicioCampo(s: Schema<"ServicioCampo">): ServicioLinea {
    return {
        id: s.id,
        estado: s.estado,
        codigo: s.codigo,
        planNombre: s.plan,
        ...(s.zona ? { zonaNombre: s.zona } : {}),
        ...(s.precioMensual ? { precioMensual: s.precioMensual } : {}),
        ...(s.moneda ? { moneda: s.moneda } : {}),
        ...(s.router ? { routerNombre: s.router } : {}),
        ...(s.ip ? { ip: s.ip } : {}),
        ...(s.direccion ? { direccion: s.direccion } : {}),
    };
}

/** Servicio copiado al crear un ticket o una orden (`CopiaServicio`) como línea. */
export function lineaDeCopia(s: Schema<"CopiaServicio">): ServicioLinea {
    return {
        id: s.id,
        estado: s.estado,
        planNombre: s.planNombre,
        zonaNombre: s.zonaNombre,
        ...(s.routerNombre ? { routerNombre: s.routerNombre } : {}),
        ...(s.ip ? { ip: s.ip } : {}),
        ...(s.direccion ? { direccion: s.direccion } : {}),
    };
}
