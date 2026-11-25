import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Aviso } from "./componentes/aviso";
import { GrupoSegmentado } from "./componentes/grupo-segmentado";
import { InsigniaEstado } from "./componentes/insignia-estado";
import { LogoIris } from "./componentes/marca";

const leer = (ruta: string) => readFileSync(fileURLToPath(new URL(ruta, import.meta.url)), "utf8");

describe("identidad de Iris", () => {
    it("los tokens son copia exacta de docs/marca (los de Horus y los de Iris)", () => {
        expect(leer("./estilos/horus-tokens.css")).toBe(leer("../../../docs/marca/horus-tokens.css"));
        expect(leer("./estilos/iris-tokens.css")).toBe(leer("../../../docs/marca/iris-tokens.css"));
    });

    it("los tokens tienen los valores aprobados en claro y en oscuro", () => {
        const tokens = leer("./estilos/iris-tokens.css");
        const [claro = "", oscuro = ""] = tokens.split("/* ---------- Tema oscuro ---------- */");
        const valor = (bloque: string, nombre: string) => new RegExp(`--iris-${nombre}:\\s*([^;]+);`).exec(bloque)?.[1]?.trim();
        expect(valor(claro, "primario")).toBe("#4f46e5");
        expect(valor(oscuro, "primario")).toBe("#818cf8");
        expect(valor(oscuro, "sobre-primario")).toBe("#0b0f1a");
        expect(valor(claro, "secundario")).toBe("#06b6d4");
        expect(valor(claro, "fondo")).toBe("#f8f9fa");
        expect(valor(oscuro, "fondo")).toBe("#0b0f1a");
        expect(valor(claro, "tarjeta")).toBe("#ffffff");
        expect(valor(oscuro, "tarjeta")).toBe("#121828");
        expect(valor(claro, "borde")).toBe("#e7eaee");
        expect(valor(oscuro, "borde")).toBe("#222a40");
        expect(valor(claro, "radio-tarjeta")).toBe("12px");
        expect(valor(claro, "radio-modal")).toBe("16px");
        expect(valor(claro, "radio-isla")).toBe("20px");
        expect(valor(claro, "gradiente")).toBe("linear-gradient(135deg, #4f46e5, #06b6d4)");
        expect(valor(claro, "gradiente-texto")).toBe("linear-gradient(135deg, #4f46e5, #4338ca 55%, #0e7490)");
        // Cinco series de gráficas por tema, ninguna es un color de estado.
        const estados = ["exito", "atencion", "peligro", "info"].flatMap((e) => [valor(claro, e), valor(oscuro, e), valor(claro, `${e}-punto`), valor(oscuro, `${e}-punto`)]);
        for (const bloque of [claro, oscuro]) {
            const series = [1, 2, 3, 4, 5].map((n) => valor(bloque, `grafica-${n}`));
            expect(series.every(Boolean)).toBe(true);
            expect(new Set(series).size).toBe(5);
            for (const serie of series) expect(estados).not.toContain(serie);
        }
    });

    it("el logo es copia exacta de docs/marca/logo-mark.svg", () => {
        expect(leer("./marca/logo-mark.svg")).toBe(leer("../../../docs/marca/logo-mark.svg"));
    });

    it("LogoIris dibuja los mismos trazos que el SVG de la marca, sobre el degradado índigo → cian", () => {
        const svg = leer("./marca/logo-mark.svg");
        const html = renderToStaticMarkup(<LogoIris />);
        for (const d of svg.matchAll(/ d="([^"]+)"/g)) {
            expect(html).toContain(`d="${d[1]}"`);
        }
        for (const archivo of [svg, html]) {
            expect(archivo).toMatch(/stop-color="#4f46e5"/);
            expect(archivo).toMatch(/stop-color="#06b6d4"/);
            expect(archivo).not.toContain("#0f766e");
        }
        expect(html).toMatch(/<rect[^>]*fill="url\(#iris-degradado-[^)]+\)"/);
        // Dos logos en la misma página no comparten el id del degradado.
        const dos = renderToStaticMarkup(
            <>
                <LogoIris />
                <LogoIris />
            </>,
        );
        const ids = [...dos.matchAll(/linearGradient id="([^"]+)"/g)].map((m) => m[1]);
        expect(new Set(ids).size).toBe(2);
    });
});

describe("estados: icono + texto + color", () => {
    it("InsigniaEstado lleva icono, texto y tono", () => {
        const html = renderToStaticMarkup(<InsigniaEstado tono="success" texto="MFA activo" />);
        expect(html).toContain("<svg");
        expect(html).toContain("MFA activo");
        expect(html).toContain("text-success");
    });

    it("un Aviso de error se anuncia como alerta y muestra su acción", () => {
        const html = renderToStaticMarkup(
            <Aviso tono="error" titulo="No se pudo cargar" accion={<span>Reintentar</span>}>
                Detalle
            </Aviso>,
        );
        expect(html).toContain('role="alert"');
        expect(html).toContain("Reintentar");
    });
});

describe("GrupoSegmentado", () => {
    it("la opción elegida se distingue con el color de marca (no solo con el fondo)", () => {
        const html = renderToStaticMarkup(
            <GrupoSegmentado etiqueta="Mostrar" opciones={[{ valor: "a", etiqueta: "En curso" }, { valor: "b", etiqueta: "Todos" }]} valor="b" alCambiar={() => undefined} />,
        );
        // ToggleButtonGroup de HeroUI con selección única: radiogroup y radios.
        expect(html).toMatch(/class="toggle-button-group[^"]*"[^>]*role="radiogroup"/);
        expect(html).toMatch(/data-selected:text-primary[^>]*role="radio" aria-checked="true" data-selected="true">Todos</);
        expect(html).toMatch(/role="radio" aria-checked="false">En curso</);
        expect(html).toContain("data-selected:shadow-[0_0_0_1px_var(--hx-primary)]");
    });

    it("con contador lo muestra discreto junto a la etiqueta («Mías 4»)", () => {
        const html = renderToStaticMarkup(
            <GrupoSegmentado etiqueta="Vista" opciones={[{ valor: "mias", etiqueta: "Mías", contador: 4 }]} valor="mias" alCambiar={() => undefined} />,
        );
        expect(html).toMatch(/<span class="truncate">Mías<\/span><span class="[^"]*text-muted-foreground[^"]*" data-slot="contador">4<\/span>/);
    });
});
