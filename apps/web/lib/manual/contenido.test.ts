import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { enlacesInternos } from "@/componentes/manual/markdown";
import { buscarEnManual, normalizar } from "./busqueda";
import { entradasBusqueda, esClaveCampo, INDICE_MANUAL, PAGINAS_CAMPO, PAGINAS_MANUAL, paginaManual, rutaDeClave } from "./contenido";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const docs = path.resolve(web, "../../docs/manual");

/** Las partes que pidió el PM en cada página, en este orden. */
const PARTES = ["Para qué sirve", "Cómo se usa", "Qué significa cada estado", "Preguntas frecuentes", "Permisos necesarios"];

describe("contenido del manual", () => {
    it("el JSON generado está al día con docs/manual (pnpm --filter @iris/web manual)", () => {
        expect(() => execFileSync("node", ["scripts/manual.mjs", "--comprobar"], { cwd: web, stdio: "pipe" })).not.toThrow();
    });

    it("cada página tiene título y las cinco partes, en orden", () => {
        expect(PAGINAS_MANUAL.length).toBeGreaterThan(50);
        for (const p of PAGINAS_MANUAL) {
            expect(p.fuente.startsWith(`# ${p.titulo}\n`), p.clave).toBe(true);
            const partes = [...p.fuente.matchAll(/^## (.+)$/gm)].map((m) => m[1]);
            expect(partes, p.clave).toEqual(PARTES);
        }
    });

    it("los títulos no se repiten (el buscador y las migas los usan)", () => {
        const titulos = PAGINAS_MANUAL.map((p) => p.titulo);
        expect(new Set(titulos).size).toBe(titulos.length);
    });

    it("todo enlace interno lleva a una página que existe", () => {
        for (const fuente of [INDICE_MANUAL, ...PAGINAS_MANUAL.map((p) => p.fuente)]) {
            for (const clave of enlacesInternos(fuente)) {
                if (clave !== null) expect(paginaManual(clave), clave).not.toBeNull();
            }
        }
    });

    it("el índice enlaza todas las páginas", () => {
        const enlazadas = new Set(enlacesInternos(INDICE_MANUAL));
        for (const p of PAGINAS_MANUAL) expect(enlazadas.has(p.clave), p.clave).toBe(true);
    });

    it("docs/manual/claves.md lista exactamente las claves de las páginas, con su archivo", () => {
        const tabla = readFileSync(path.join(docs, "claves.md"), "utf8");
        const filas = [...tabla.matchAll(/^\| .+ \| `([a-z0-9-]+\/[a-z0-9-]+)` \| `docs\/manual\/([^`]+)\.md` \|$/gm)];
        expect(filas.map((f) => f[1]).sort()).toEqual(PAGINAS_MANUAL.map((p) => p.clave).sort());
        for (const f of filas) expect(f[2]).toBe(f[1]);
        // Y cada archivo de una carpeta es una página.
        const archivos = readdirSync(docs, { withFileTypes: true })
            .filter((d) => d.isDirectory())
            .flatMap((d) => readdirSync(path.join(docs, d.name)).map((a) => `${d.name}/${a.replace(/\.md$/, "")}`));
        expect(archivos.sort()).toEqual(PAGINAS_MANUAL.map((p) => p.clave).sort());
    });

    it("la app del técnico tiene su sección y abre en /campo/manual", () => {
        expect(PAGINAS_CAMPO.length).toBeGreaterThan(10);
        expect(PAGINAS_CAMPO.every((p) => esClaveCampo(p.clave))).toBe(true);
        expect(rutaDeClave("campo/hoy")).toBe("/campo/manual/hoy");
        expect(rutaDeClave("tickets/lista")).toBe("/manual/tickets/lista");
        expect(rutaDeClave(null)).toBe("/manual");
    });
});

describe("buscador del manual", () => {
    const entradas = entradasBusqueda();

    it("no distingue mayúsculas ni acentos", () => {
        expect(normalizar("Órdenes ÑANDÚ")).toBe("ordenes ñandu".normalize("NFD").replace(/\p{Diacritic}/gu, ""));
        const conAcento = buscarEnManual(entradas, "Órdenes").map((r) => r.clave);
        expect(buscarEnManual(entradas, "ordenes").map((r) => r.clave)).toEqual(conAcento);
    });

    it("pide todas las palabras y pone primero las que están en el título", () => {
        const r = buscarEnManual(entradas, "firma cierre");
        expect(r[0]?.clave).toBe("campo/cierre");
        expect(r.every((x) => normalizar(`${x.titulo} ${x.seccion} ${x.texto}`).includes("firma"))).toBe(true);
        expect(buscarEnManual(entradas, "zzzz-no-existe")).toEqual([]);
        expect(buscarEnManual(entradas, "   ")).toEqual([]);
    });

    it("cada resultado trae un extracto corto con lo buscado", () => {
        const [primero] = buscarEnManual(entradas, "ventana de 24");
        expect(primero).toBeDefined();
        expect(normalizar(primero?.extracto ?? "")).toContain("ventana de 24");
        expect((primero?.extracto ?? "").length).toBeLessThanOrEqual(162);
    });
});
