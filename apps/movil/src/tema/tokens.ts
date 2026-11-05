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

/**
 * Colores del diseño «combinada» (C-Hoy, nov 2026) llevados a los tokens: cada uno es un `--hx-*`
 * o la mezcla de dos (como `color-mix` en `apps/web/app/campo/campo.css`), sin colores nuevos.
 */
export interface ColoresCampo {
    /** Lienzo: apenas teñido de la marca en claro; el fondo de Horus en oscuro. */
    lienzo: string;
    /** Tarjetas y filas. */
    panel: string;
    /** Relleno de los botones redondos y del bloque de la hora. */
    panel2: string;
    /** Tarjeta de la siguiente orden y su borde. */
    heroe: string;
    heroeBorde: string;
    /** Lo elegido (chip, botón de la isla, paso actual): la tinta del texto con el fondo encima. */
    tinta: string;
    sobreTinta: string;
    /** Marca suave: pasos listos, «En sitio». */
    marcaSuave: string;
    marcaSuaveTexto: string;
    /** Aviso, info y error: el 10 % del tono sobre el fondo, borde al 30 %. */
    avisoFondo: string;
    avisoBorde: string;
    infoFondo: string;
    infoBorde: string;
    errorFondo: string;
    errorBorde: string;
    /** Lo que falta del anillo de avance. */
    anillo: string;
    /** Isla de menú (sólida, como todo lo flotante). */
    isla: string;
    /** Color de la sombra de la isla y de los avisos. */
    sombra: string;
}

export interface Tema {
    oscuro: boolean;
    colores: Colores;
    campo: ColoresCampo;
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

/** `#rrggbb` → [r, g, b]. */
function rgb(hex: string): [number, number, number] {
    const n = Number.parseInt(hex.replace("#", ""), 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** `color-mix(in srgb, a pct%, b)` de dos colores `#rrggbb`. */
export function mezclar(a: string, b: string, pct: number): string {
    const [x, y] = [rgb(a), rgb(b)];
    const canal = (i: number) =>
        Math.round((x[i] ?? 0) * (pct / 100) + (y[i] ?? 0) * (1 - pct / 100))
            .toString(16)
            .padStart(2, "0");
    return `#${canal(0)}${canal(1)}${canal(2)}`;
}

/** Los colores del diseño «combinada» desde los tokens del tema (claro u oscuro). */
export function coloresCampo(c: Colores, oscuro: boolean): ColoresCampo {
    const tono = (color: string, pct: number) => mezclar(color, c.fondo, pct);
    const comunes = {
        tinta: c.texto,
        sobreTinta: c.fondo,
        avisoFondo: tono(c.advertencia, 10),
        avisoBorde: tono(c.advertencia, 30),
        infoFondo: tono(c.info, 10),
        infoBorde: tono(c.info, 30),
        errorFondo: tono(c.error, 10),
        errorBorde: tono(c.error, 30),
        anillo: c.borde,
    };
    return oscuro
        ? {
              ...comunes,
              lienzo: c.fondo,
              panel: c.fondoSuave,
              panel2: c.fondoElevado,
              heroe: tono(c.primario, 7),
              heroeBorde: tono(c.primario, 22),
              marcaSuave: tono(c.primario, 14),
              marcaSuaveTexto: c.primario,
              isla: c.fondoElevado,
              sombra: "#000000",
          }
        : {
              ...comunes,
              lienzo: mezclar(c.primario, c.fondoElevado, 4),
              panel: c.fondo,
              panel2: mezclar(c.primario, c.fondoElevado, 4),
              heroe: c.fondo,
              heroeBorde: c.borde,
              marcaSuave: tono(c.primario, 18),
              marcaSuaveTexto: c.primarioPresionado,
              isla: c.fondo,
              sombra: c.texto,
          };
}

const CAMPO_CLARO = coloresCampo(CLARO, false);
const CAMPO_OSCURO = coloresCampo(OSCURO, true);

/**
 * `--hx-radius` (0,375 rem) a 16 px. Las tarjetas de la app de campo van con 28 px de radio (diseño
 * «combinada»; en la web, `--hx-radius-card` dentro de `.campo-app`).
 */
export function crearTema(oscuro: boolean): Tema {
    return { oscuro, colores: oscuro ? OSCURO : CLARO, campo: oscuro ? CAMPO_OSCURO : CAMPO_CLARO, radio: 6, radioTarjeta: 28, fuenteMono: "monospace" };
}
