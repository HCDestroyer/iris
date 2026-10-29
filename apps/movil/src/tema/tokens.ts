// Identidad Horus («vidrio y luz») traducida a React Native desde `docs/marca/horus-tokens.css`.
// Un solo color de marca (teal) y estados con icono + texto + color. Nunca colores crudos en las
// pantallas: se leen de `useTema()`.
import type { TonoEstado } from "@iris/sync/campo";

export interface Colores {
    primario: string;
    primarioPresionado: string;
    sobrePrimario: string;
    exito: string;
    info: string;
    advertencia: string;
    error: string;
    fondo: string;
    fondoSuave: string;
    fondoElevado: string;
    borde: string;
    texto: string;
    textoSuave: string;
    textoTenue: string;
    tarjeta: string;
    vidrio: string;
    /** Fondo de una insignia o aviso de cada tono (el color del tono al 12–16 %). */
    fondoTono: Record<TonoEstado, string>;
    /** Texto e icono de cada tono. */
    tono: Record<TonoEstado, string>;
}

export interface Tema {
    oscuro: boolean;
    colores: Colores;
    radio: number;
    radioTarjeta: number;
    fuenteMono: string;
}

/** `--hx-*` del tema claro (tonos 600–800: 4,5:1 sobre blanco). */
export const CLARO: Colores = {
    primario: "#0f766e",
    primarioPresionado: "#115e59",
    sobrePrimario: "#ffffff",
    exito: "#166534",
    info: "#1d4ed8",
    advertencia: "#92400e",
    error: "#b91c1c",
    fondo: "#ffffff",
    fondoSuave: "#fafafa",
    fondoElevado: "#f4f4f5",
    borde: "#e4e4e7",
    texto: "#18181b",
    textoSuave: "#52525b",
    textoTenue: "#71717a",
    tarjeta: "#ffffff",
    vidrio: "rgba(255,255,255,0.86)",
    tono: { neutral: "#52525b", info: "#1d4ed8", success: "#166534", warning: "#92400e", error: "#b91c1c" },
    fondoTono: {
        neutral: "#f4f4f5",
        info: "rgba(29,78,216,0.10)",
        success: "rgba(22,101,52,0.10)",
        warning: "rgba(146,64,14,0.10)",
        error: "rgba(185,28,28,0.10)",
    },
};

/** `--hx-*` del tema oscuro: azul noche, sin negro puro; el teal 400 es la «luz». */
export const OSCURO: Colores = {
    primario: "#2dd4bf",
    primarioPresionado: "rgba(45,212,191,0.9)",
    sobrePrimario: "#042f2e",
    exito: "#4ade80",
    info: "#60a5fa",
    advertencia: "#fbbf24",
    error: "#f87171",
    fondo: "#0b1014",
    fondoSuave: "#10151b",
    fondoElevado: "#1a2029",
    borde: "#232a33",
    texto: "#f4f4f5",
    textoSuave: "#a1a1aa",
    textoTenue: "#a1a1aa",
    tarjeta: "rgba(20,25,32,0.86)",
    vidrio: "rgba(16,21,27,0.86)",
    tono: { neutral: "#a1a1aa", info: "#60a5fa", success: "#4ade80", warning: "#fbbf24", error: "#f87171" },
    fondoTono: {
        neutral: "#1a2029",
        info: "rgba(96,165,250,0.14)",
        success: "rgba(74,222,128,0.14)",
        warning: "rgba(251,191,36,0.14)",
        error: "rgba(248,113,113,0.14)",
    },
};

/** `--hx-radius` (0,375 rem) y `--hx-radius-card` (0,875 rem) a 16 px. */
export function crearTema(oscuro: boolean): Tema {
    return { oscuro, colores: oscuro ? OSCURO : CLARO, radio: 6, radioTarjeta: 14, fuenteMono: "monospace" };
}
