/// <reference types="node" />
// El tema de la app nativa no inventa colores: los `--hx-*` de `src/global.css` (HeroUI Native con
// Uniwind) y los de `src/tema/tokens.ts` (íconos y lienzo) valen lo que sus `--iris-*` de
// `docs/marca/iris-tokens.css` (rediseño, nov. 2026), la única fuente de color, en claro y oscuro.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { CLARO, OSCURO, coloresCampo, crearTema, mezclar, type Colores } from "../src/tema/tokens";

const leer = (ruta: string) => readFileSync(join(__dirname, ruta), "utf8");
const MARCA = leer("../../../docs/marca/iris-tokens.css");
const GLOBAL = leer("../src/global.css");

/** `#ABC`, `rgb(1 2 3 / 0.5)` y `rgba(1,2,3,0.5)` a una sola forma para comparar. */
function normalizar(color: string): string {
    const c = color.trim().toLowerCase();
    const rgb = /^rgba?\(([^)]+)\)$/.exec(c);
    if (rgb?.[1]) {
        const partes = rgb[1].split(/[\s,/]+/).filter(Boolean).map(Number);
        return `rgba(${[...partes, ...(partes.length === 3 ? [1] : [])].join(",")})`;
    }
    return c;
}

/** Las variables `--hx-*` (o `--iris-*`) de un bloque `{ … }` (sin comentarios). */
function variables(bloque: string, prefijo = "hx"): Map<string, string> {
    const limpio = bloque.replace(/\/\*[\s\S]*?\*\//g, "");
    return new Map([...limpio.matchAll(new RegExp(`(--${prefijo}-[a-z0-9-]+)\\s*:\\s*([^;]+);`, "g"))].map((m) => [m[1] ?? "", normalizar(m[2] ?? "")]));
}

/** Cada `--hx-*` de la app es el alias de este `--iris-*` (como en `iris-tokens.css` de la web). */
const ALIAS: Record<string, string> = {
    "--hx-primary": "--iris-primario",
    "--hx-primary-hover": "--iris-primario-hover",
    "--hx-on-primary": "--iris-sobre-primario",
    "--hx-success": "--iris-exito",
    "--hx-info": "--iris-info",
    "--hx-warning": "--iris-atencion",
    "--hx-error": "--iris-peligro",
    "--hx-bg": "--iris-tarjeta",
    "--hx-bg-muted": "--iris-fondo",
    "--hx-bg-elevated": "--iris-relleno",
    "--hx-border": "--iris-borde",
    "--hx-text": "--iris-texto",
    "--hx-text-muted": "--iris-texto-tenue",
    "--hx-text-dimmed": "--iris-texto-apagado",
    "--hx-glass": "--iris-tarjeta",
    "--hx-glass-strong": "--iris-flotante",
    "--hx-card": "--iris-tarjeta",
};

/** Los `--iris-*` de un tema, con el claro de base (el oscuro solo redefine colores). */
function tokensIris(bloque: string, base?: Map<string, string>): Map<string, string> {
    const propios = variables(bloque, "iris");
    const iris = new Map([...(base ?? new Map<string, string>()), ...propios]);
    return new Map(Object.entries(ALIAS).map(([hx, nombre]) => [hx, iris.get(nombre) ?? ""]));
}

function bloqueTras(css: string, inicio: string): string {
    const i = css.indexOf(inicio);
    expect(i).toBeGreaterThanOrEqual(0);
    const abre = css.indexOf("{", i);
    return css.slice(abre + 1, css.indexOf("}", abre));
}

const irisClaro = variables(bloqueTras(MARCA, ":root,"), "iris");
const marca = {
    light: tokensIris(bloqueTras(MARCA, ":root,")),
    dark: tokensIris(bloqueTras(MARCA, "[data-theme='dark'] {"), irisClaro),
};
const app = {
    light: variables(bloqueTras(GLOBAL, "@variant light")),
    dark: variables(bloqueTras(GLOBAL, "@variant dark")),
};

describe.each(["light", "dark"] as const)("tema de Iris (%s)", (esquema) => {
    it("cada --hx-* de global.css vale su --iris-* de docs/marca/iris-tokens.css", () => {
        expect(app[esquema].size).toBeGreaterThanOrEqual(15);
        for (const [nombre, valor] of app[esquema]) {
            expect([nombre, valor]).toEqual([nombre, marca[esquema].get(nombre)]);
        }
    });

    it("los dos temas de Uniwind definen las mismas variables", () => {
        expect([...app[esquema].keys()].sort()).toEqual([...app.light.keys()].sort());
    });

    it("los tokens de JS (íconos, lienzo) son los mismos", () => {
        const c: Colores = esquema === "dark" ? OSCURO : CLARO;
        const m = marca[esquema];
        const pares: [string, string][] = [
            ["--hx-primary", c.primario],
            ["--hx-primary-hover", c.primarioPresionado],
            ["--hx-on-primary", c.sobrePrimario],
            ["--hx-success", c.exito],
            ["--hx-info", c.info],
            ["--hx-warning", c.advertencia],
            ["--hx-error", c.error],
            ["--hx-bg", c.fondo],
            ["--hx-bg-muted", c.fondoSuave],
            ["--hx-bg-elevated", c.fondoElevado],
            ["--hx-border", c.borde],
            ["--hx-text", c.texto],
            ["--hx-text-muted", c.textoSuave],
            ["--hx-text-dimmed", c.textoTenue],
            ["--hx-card", c.tarjeta],
            ["--hx-glass-strong", c.vidrio],
        ];
        for (const [nombre, valor] of pares) {
            expect([nombre, normalizar(valor)]).toEqual([nombre, m.get(nombre)]);
        }
        expect(crearTema(esquema === "dark").colores).toBe(c);
    });
});

describe("diseño «combinada»: colores de la app de campo desde los tokens", () => {
    it("mezcla como color-mix en srgb", () => {
        expect(mezclar("#000000", "#ffffff", 50)).toBe("#808080");
        expect(mezclar("#4f46e5", "#ffffff", 100)).toBe("#4f46e5");
        expect(mezclar("#4f46e5", "#ffffff", 0)).toBe("#ffffff");
    });

    it.each([
        [false, CLARO],
        [true, OSCURO],
    ] as const)("oscuro=%s: índigo y solo tokens o mezclas de tokens", (oscuro, c) => {
        const campo = crearTema(oscuro).campo;
        expect(campo).toEqual(coloresCampo(c, oscuro));
        expect(campo.tinta).toBe(c.texto);
        expect(campo.sobreTinta).toBe(c.fondo);
        expect(campo.avisoFondo).toBe(mezclar(c.advertencia, c.fondo, 10));
        expect(campo.anillo).toBe(c.borde);
        // El color principal es el índigo del rediseño: claro #4f46e5, oscuro #818cf8.
        expect(c.primario).toBe(oscuro ? "#818cf8" : "#4f46e5");
        expect(campo.lienzo).toBe(oscuro ? c.fondo : mezclar(c.primario, c.fondoElevado, 4));
        expect(crearTema(oscuro).radioTarjeta).toBe(28);
    });
});

it("el color principal: HeroUI `accent`, `focus` y `link` son el índigo de Iris", () => {
    for (const variante of ["@variant light", "@variant dark"]) {
        const bloque = bloqueTras(GLOBAL, variante);
        for (const v of ["--accent", "--focus", "--link"]) {
            expect(bloque).toMatch(new RegExp(`${v}: var\\(--hx-primary\\);`));
        }
        expect(bloque).toMatch(/--danger: var\(--hx-error\);/);
        expect(bloque).toMatch(/--success: var\(--hx-success\);/);
        expect(bloque).toMatch(/--warning: var\(--hx-warning\);/);
    }
});

it("los fondos tintados son `bg-suave-*` (con `/10` o `bg-*-soft` Uniwind los deja en `unset`)", () => {
    const fuente = leer("../src/componentes/index.tsx");
    expect(fuente).not.toMatch(/\b(bg|border|text)-(info|success|warning|danger|accent)\/\d+/);
    expect(fuente).not.toMatch(/\bbg-(info|success|warning|danger|accent)-soft\b/);
    expect(fuente).toMatch(/bg-suave-danger/);
    expect(GLOBAL).toMatch(/@theme static \{[^}]*--color-suave-danger: color-mix\(in oklab, var\(--hx-error\) 10%, transparent\);/);
});
