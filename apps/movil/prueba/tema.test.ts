/// <reference types="node" />
// El tema Horus de la app nativa no inventa colores: los `--hx-*` de `src/global.css` (HeroUI
// Native con Uniwind) y los de `src/tema/tokens.ts` (íconos y lienzo) son los de
// `docs/marca/horus-tokens.css`, la única fuente de color, en claro y en oscuro.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { CLARO, OSCURO, crearTema, type Colores } from "../src/tema/tokens";

const leer = (ruta: string) => readFileSync(join(__dirname, ruta), "utf8");
const MARCA = leer("../../../docs/marca/horus-tokens.css");
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

/** Las variables `--hx-*` de un bloque `{ … }` (sin comentarios). */
function variables(bloque: string): Map<string, string> {
    const limpio = bloque.replace(/\/\*[\s\S]*?\*\//g, "");
    return new Map([...limpio.matchAll(/(--hx-[a-z0-9-]+)\s*:\s*([^;]+);/g)].map((m) => [m[1] ?? "", normalizar(m[2] ?? "")]));
}

function bloqueTras(css: string, inicio: string): string {
    const i = css.indexOf(inicio);
    expect(i).toBeGreaterThanOrEqual(0);
    const abre = css.indexOf("{", i);
    return css.slice(abre + 1, css.indexOf("}", abre));
}

const marca = {
    light: variables(bloqueTras(MARCA, ":root,")),
    dark: variables(bloqueTras(MARCA, ".dark,")),
};
const app = {
    light: variables(bloqueTras(GLOBAL, "@variant light")),
    dark: variables(bloqueTras(GLOBAL, "@variant dark")),
};

describe.each(["light", "dark"] as const)("tema Horus (%s)", (esquema) => {
    it("cada --hx-* de global.css es el de docs/marca/horus-tokens.css", () => {
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

it("un solo color de marca: HeroUI `accent`, `focus` y `link` son el teal de Horus", () => {
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
