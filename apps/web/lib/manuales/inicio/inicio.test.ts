import { describe, expect, it } from "vitest";
import { bloquesMarkdown, markdownATexto } from "@/componentes/manuales/markdown";
import { esquemaManual, leerEtiquetas } from "../esquema";
import { claveManualInicio, entradaDe, MANUALES_INICIO, textoResultadoCarga, tituloComparable, yaExistentes } from ".";

// Límites del servidor (`EntradaManual` en apps/api/internal/mensajeria/manuales.go).
const SERVIDOR = { titulo: 160, resumen: 400, cuerpo: 50_000, etiquetas: 20, equipo: 80 };

describe("manuales de inicio", () => {
    it("son al menos 12, con código y título únicos", () => {
        expect(MANUALES_INICIO.length).toBeGreaterThanOrEqual(12);
        expect(new Set(MANUALES_INICIO.map((m) => m.codigo)).size).toBe(MANUALES_INICIO.length);
        expect(new Set(MANUALES_INICIO.map((m) => tituloComparable(m.titulo))).size).toBe(MANUALES_INICIO.length);
    });

    it("cubren fibra, inalámbrico y lo general", () => {
        const tecnologias = new Set(MANUALES_INICIO.map((m) => m.tecnologia));
        expect([...tecnologias].sort()).toEqual(["ftth", "general", "wisp"]);
    });

    it.each(MANUALES_INICIO.map((m) => [m.codigo, m] as const))("%s pasa el validador del formulario y los límites del servidor", (_codigo, m) => {
        const entrada = entradaDe(m);
        const r = esquemaManual.safeParse({ ...entrada, etiquetas: (entrada.etiquetas ?? []).join(", "), equipo: entrada.equipo ?? "" });
        expect(r.success, JSON.stringify(r.error?.issues)).toBe(true);
        expect(m.titulo.length).toBeLessThanOrEqual(SERVIDOR.titulo);
        expect(m.resumen.length).toBeLessThanOrEqual(SERVIDOR.resumen);
        expect(m.cuerpo.length).toBeLessThanOrEqual(SERVIDOR.cuerpo);
        expect(m.etiquetas.length).toBeLessThanOrEqual(SERVIDOR.etiquetas);
        // Las etiquetas llegan como el formulario las dejaría (minúsculas, sin repetir, hasta 10).
        expect(leerEtiquetas(m.etiquetas.join(", "))).toEqual(m.etiquetas);
        expect(m.etiquetas[0]).toBe(m.tecnologia);
        expect(entrada.visibilidad).toBe("cliente");
    });

    it.each(MANUALES_INICIO.map((m) => [m.codigo, m] as const))("%s es corto, se lee con la vista segura y dice cuándo llamar a soporte", (_codigo, m) => {
        const bloques = bloquesMarkdown(m.cuerpo);
        const titulos = bloques.filter((b) => b.tipo === "titulo").map((b) => (b.tipo === "titulo" ? b.texto : ""));
        expect(titulos.length).toBeGreaterThanOrEqual(3);
        expect(titulos).toContain("Cuándo llamar a soporte");
        // Corto y escaneable: ningún párrafo largo, y el artículo entero cabe en una lectura.
        for (const b of bloques) if (b.tipo === "parrafo") expect(b.texto.length, b.texto).toBeLessThanOrEqual(320);
        expect(m.cuerpo.length).toBeLessThanOrEqual(4_000);
        // Solo el subconjunto seguro: nada de HTML, tablas, imágenes ni enlaces (los datos de la
        // empresa van entre corchetes para completarlos).
        expect(m.cuerpo).not.toMatch(/<[a-z/]/i);
        expect(m.cuerpo).not.toMatch(/^\s*\|/m);
        expect(m.cuerpo).not.toMatch(/\]\(/);
        expect(m.cuerpo).not.toMatch(/(^|\s)_|_(\s|$)/m);
        // En texto plano (WhatsApp) no queda ninguna marca de Markdown.
        expect(markdownATexto(m.cuerpo)).not.toMatch(/\*\*|^#|^>/m);
    });

    it("no promete precios", () => {
        for (const m of MANUALES_INICIO) expect(`${m.resumen} ${m.cuerpo}`, m.codigo).not.toMatch(/Q\s?\d|\$\s?\d|gratis|garantizad/i);
    });

    it("la clave de idempotencia es estable, por empresa y por revisión", () => {
        const [m] = MANUALES_INICIO;
        if (!m) throw new Error("sin manuales");
        expect(claveManualInicio(7, m)).toBe(claveManualInicio(7, m));
        expect(claveManualInicio(7, m)).not.toBe(claveManualInicio(8, m));
        expect(claveManualInicio(7, m)).not.toBe(claveManualInicio(7, { ...m, revision: m.revision + 1 }));
        expect(claveManualInicio(7, m).length).toBeLessThanOrEqual(255);
    });

    it("reconoce los ya cargados por título, sin importar mayúsculas, acentos ni espacios", () => {
        const [a, b] = MANUALES_INICIO;
        if (!a || !b) throw new Error("sin manuales");
        const existentes = yaExistentes(MANUALES_INICIO, [`  ${a.titulo.toUpperCase()} `, b.titulo.normalize("NFD").replace(/\p{Diacritic}/gu, ""), "Otro manual"]);
        expect([...existentes]).toEqual([a.codigo, b.codigo]);
    });

    it("el aviso final dice cuántos se crearon y cuáles se omitieron", () => {
        expect(textoResultadoCarga(12, [])).toEqual({ titulo: "12 manuales de inicio creados" });
        expect(textoResultadoCarga(1, ["A"])).toEqual({ titulo: "1 manual de inicio creado", descripcion: "Se omitió 1 que ya estaba en la biblioteca: «A»." });
        expect(textoResultadoCarga(0, ["A", "B", "C", "D"]).descripcion).toBe("Se omitieron 4 que ya estaban en la biblioteca: «A», «B» y 2 más.");
    });
});
