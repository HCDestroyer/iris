// Vista de la app de campo: qué pantalla se ve y a qué pestaña de la barra inferior pertenece.
// En la PWA cada vista va en la URL (`vistaDeUrl` / `urlDeVista`: se puede volver con «Atrás» y
// abrir un enlace directo); la app nativa usa las mismas vistas con su propia pila de «Atrás».
// Compartido con la app nativa.
import { PASOS_ORDEN, type PasoOrden } from "./pasos";

/** Pestañas de la barra inferior. */
export type Pestana = "hoy" | "recibir" | "pendientes" | "yo";

export type Vista =
    | { tipo: "lista" }
    /** Sin `paso`: el resumen de la orden. */
    | { tipo: "orden"; id: number; paso?: PasoOrden }
    | { tipo: "pendientes" }
    /** «Por recibir»: traslados de Argos hacia la bodega del técnico. */
    | { tipo: "recibir" }
    | { tipo: "traslado"; id: number }
    | { tipo: "yo" }
    /** «Mi bodega» (dentro de «Yo»). */
    | { tipo: "bodega" };

/** Pestañas en orden, con su texto y la vista con la que abren. */
export const PESTANAS: readonly { id: Pestana; texto: string; vista: Vista }[] = [
    { id: "hoy", texto: "Hoy", vista: { tipo: "lista" } },
    { id: "recibir", texto: "Por recibir", vista: { tipo: "recibir" } },
    { id: "pendientes", texto: "Pendientes", vista: { tipo: "pendientes" } },
    { id: "yo", texto: "Yo", vista: { tipo: "yo" } },
];

function idDe(valor: string | null): number | null {
    const n = Number(valor);
    return valor !== null && Number.isSafeInteger(n) && n > 0 ? n : null;
}

function esPaso(valor: string | null): valor is PasoOrden {
    return (PASOS_ORDEN as readonly string[]).includes(valor ?? "");
}

export function vistaDeUrl(busqueda: string): Vista {
    const p = new URLSearchParams(busqueda);
    const orden = idDe(p.get("orden"));
    if (orden !== null) {
        const paso = p.get("paso");
        return esPaso(paso) ? { tipo: "orden", id: orden, paso } : { tipo: "orden", id: orden };
    }
    const traslado = idDe(p.get("traslado"));
    if (traslado !== null) {
        return { tipo: "traslado", id: traslado };
    }
    switch (p.get("vista")) {
        case "pendientes":
            return { tipo: "pendientes" };
        case "recibir":
            return { tipo: "recibir" };
        case "yo":
            return { tipo: "yo" };
        case "bodega":
            return { tipo: "bodega" };
        default:
            return { tipo: "lista" };
    }
}

export function urlDeVista(v: Vista): string {
    switch (v.tipo) {
        case "orden":
            return v.paso ? `/campo?orden=${v.id}&paso=${v.paso}` : `/campo?orden=${v.id}`;
        case "traslado":
            return `/campo?traslado=${v.id}`;
        case "lista":
            return "/campo";
        default:
            return `/campo?vista=${v.tipo}`;
    }
}

/** Pestaña de la barra inferior a la que pertenece una vista. */
export function pestanaDe(v: Vista): Pestana {
    switch (v.tipo) {
        case "lista":
        case "orden":
            return "hoy";
        case "recibir":
        case "traslado":
            return "recibir";
        case "pendientes":
            return "pendientes";
        case "yo":
        case "bodega":
            return "yo";
    }
}

/** ¿Es la misma vista? (misma pantalla, misma orden y mismo paso). */
export function mismaVista(a: Vista, b: Vista): boolean {
    return urlDeVista(a) === urlDeVista(b);
}

/** Paso anterior y siguiente de la orden («Anterior» / «Siguiente»); `null` en los extremos. */
export function vecinosPaso(paso: PasoOrden): { anterior: PasoOrden | null; siguiente: PasoOrden | null } {
    const i = PASOS_ORDEN.indexOf(paso);
    return { anterior: PASOS_ORDEN[i - 1] ?? null, siguiente: PASOS_ORDEN[i + 1] ?? null };
}
