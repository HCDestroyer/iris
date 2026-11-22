import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { BarraProgreso, Medidor } from "./componentes/barra-progreso";
import { GraficaSerie, topeRedondo, tramosSinDatos, ultimoConDato } from "./componentes/grafica-serie";

const base = { instantes: ["10:00", "10:01", "10:02", "10:03"], marcas: ["hace 3 min", "hace 1 min", "ahora"] as const, formato: (v: number) => `${Math.round(v)} %` };

describe("GraficaSerie", () => {
    it("el tope del eje es un número redondo y nunca cero", () => {
        expect([0, 0.8, 1.3, 2.4, 3.9, 28, 64, 180].map(topeRedondo)).toEqual([1, 1, 2, 2.5, 5, 50, 100, 200]);
    });

    it("un tramo sin dato en ninguna serie se marca; el último valor es el último punto con dato", () => {
        const a = { nombre: "Uso", color: 1 as const, valores: [10, null, null, 30] };
        const b = { nombre: "Memoria", color: 2 as const, valores: [20, 21, null, null] };
        expect(tramosSinDatos([a, b], 4)).toEqual([[2, 2]]);
        expect(tramosSinDatos([a], 4)).toEqual([[1, 2]]);
        expect(tramosSinDatos([{ ...a, valores: [1, 2, null, null] }], 4)).toEqual([[2, 3]]);
        expect(ultimoConDato(b.valores)).toEqual({ indice: 1, valor: 21 });
        expect(ultimoConDato([null, null])).toBeNull();
    });

    it("las marcas del eje no se repiten: con valores pequeños y formato entero no sale «0, 1, 1»", () => {
        const entero = (v: number) => String(Math.round(v));
        const marcas = (html: string) => [...html.matchAll(/data-slot="marca-eje"[^>]*>([^<]*)</g)].map((m) => m[1]);
        const pocas = renderToStaticMarkup(<GraficaSerie titulo="Peticiones por minuto" {...base} formato={entero} series={[{ nombre: "Peticiones", color: 1, valores: [0, 1, 1, 0] }]} />);
        expect(marcas(pocas)).toEqual(["0", "1"]);
        const muchas = renderToStaticMarkup(<GraficaSerie titulo="Peticiones por minuto" {...base} formato={entero} series={[{ nombre: "Peticiones", color: 1, valores: [4, 18, 12, 7] }]} />);
        expect(new Set(marcas(muchas)).size).toBe(marcas(muchas).length);
        expect(marcas(muchas)).toHaveLength(3);
    });

    it("con dos series hay leyenda y cada una dice su último valor; el texto no usa el color de la serie", () => {
        const html = renderToStaticMarkup(
            <GraficaSerie titulo="Uso y memoria de video" {...base} maximo={100} series={[{ nombre: "Uso", color: 1, valores: [40, 50, 60, 64] }, { nombre: "Memoria de video", color: 2, valores: [50, 52, 53, 54] }]} />,
        );
        expect(html).toContain('aria-label="Series"');
        expect(html).toContain("Uso: 64 %, Memoria de video: 54 %");
        expect(html).toContain('stroke="var(--hx-grafico-1)"');
        expect(html).toContain('stroke="var(--hx-grafico-2)"');
        expect(html).toContain("Ver tabla");
        expect(html).not.toMatch(/<text[^>]*fill="var\(--hx-grafico/);
    });

    it("una sola serie no lleva leyenda; sin ningún dato lo dice en vez de dibujar una línea en cero", () => {
        const una = renderToStaticMarkup(<GraficaSerie titulo="Peticiones por minuto" {...base} area series={[{ nombre: "Peticiones", color: 1, valores: [1, 2, 3, 4] }]} />);
        expect(una).not.toContain('aria-label="Series"');
        const vacia = renderToStaticMarkup(<GraficaSerie titulo="Peticiones por minuto" {...base} series={[{ nombre: "Peticiones", color: 1, valores: [null, null, null, null] }]} />);
        expect(vacia).toContain("Sin datos en este periodo");
        expect(vacia).not.toContain('role="img"');
    });

    it("la línea se corta donde falta el dato y el hueco queda sombreado", () => {
        const html = renderToStaticMarkup(<GraficaSerie titulo="Uso" {...base} maximo={100} series={[{ nombre: "Uso", color: 1, valores: [40, 50, null, null] }]} />);
        expect(html).toContain('data-slot="tramo-sin-datos"');
        // Un solo tramo dibujado: empieza con M y no vuelve a empezar.
        expect((html.match(/<path d="M[^"]*"/g) ?? []).every((d) => (d.match(/M/g) ?? []).length === 1)).toBe(true);
    });
});

describe("Medidor", () => {
    it("es un `meter` con su texto; sin dato no dibuja relleno ni inventa un cero", () => {
        const html = renderToStaticMarkup(<Medidor parte={0.546} etiqueta="Memoria de video" texto="13.1 de 24 GB" />);
        // El `Meter` de HeroUI (React Aria) agrega `progressbar` de respaldo para lectores viejos.
        expect(html).toMatch(/role="meter( progressbar)?"/);
        expect(html).toContain('class="meter ');
        expect(html).toContain('aria-valuenow="55"');
        expect(html).toContain('aria-valuetext="13.1 de 24 GB"');
        const sin = renderToStaticMarkup(<Medidor parte={null} etiqueta="Memoria de video" texto="Sin datos" />);
        expect(sin).toContain('role="meter"');
        expect(sin).not.toContain("aria-valuenow");
        expect(sin).not.toContain("width:");
    });
});

describe("BarraProgreso", () => {
    it("es el `ProgressBar` de HeroUI con su nombre y su texto", () => {
        const html = renderToStaticMarkup(<BarraProgreso valor={3} maximo={4} etiqueta="Avance de la orden" textoValor="3 de 4 pasos listos" />);
        expect(html).toContain('role="progressbar"');
        expect(html).toContain('class="progress-bar ');
        expect(html).toContain('aria-label="Avance de la orden"');
        expect(html).toContain('aria-valuenow="3"');
        expect(html).toContain('aria-valuemax="4"');
        expect(html).toContain('aria-valuetext="3 de 4 pasos listos"');
        expect(html).toContain("width:75%");
    });

    it("por pasos dibuja un segmento por paso, marcados los hechos", () => {
        const html = renderToStaticMarkup(<BarraProgreso pasos valor={2} maximo={4} etiqueta="Avance" />);
        expect(html.match(/data-hecho="true"/g)).toHaveLength(2);
        expect(html.match(/h-1\.5 rounded-full/g)).toHaveLength(4);
    });

    it("sin rango no divide por cero: barra vacía", () => {
        const html = renderToStaticMarkup(<BarraProgreso valor={0} maximo={0} etiqueta="Avance" textoValor="—" />);
        expect(html).toContain('aria-valuenow="0"');
        expect(html).not.toContain("NaN");
    });
});
