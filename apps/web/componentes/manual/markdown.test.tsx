import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { bloquesManual, destinoEnlace, enlacesInternos, MarkdownManual, textoPlano } from "./markdown";

const html = (fuente: string) => renderToStaticMarkup(<MarkdownManual fuente={fuente} />);

describe("Markdown del manual", () => {
    it("arma títulos, párrafos, listas (con sublistas) y código", () => {
        const bloques = bloquesManual("# Uno\n\nTexto\nsigue\n\n## Dos\n\n1. a\n2. b\n    - b1\n    - b2\n\n- x\n\n```\ncodigo\n```\n\n### Tres");
        expect(bloques.map((b) => b.tipo)).toEqual(["titulo", "parrafo", "titulo", "lista", "lista", "codigo", "titulo"]);
        expect(bloques[1]).toEqual({ tipo: "parrafo", texto: "Texto sigue" });
        expect(bloques[3]).toEqual({
            tipo: "lista",
            ordenada: true,
            elementos: [
                { texto: "a", sublista: null },
                { texto: "b", sublista: { ordenada: false, elementos: ["b1", "b2"] } },
            ],
        });
        const salida = html("# Uno\n\n## Dos\n\n### Tres\n\n1. a\n2. b\n\n- x");
        expect(salida).toContain("<h1");
        expect(salida).toContain(">Uno</h1>");
        expect(salida).toContain(">Dos</h2>");
        expect(salida).toContain(">Tres</h3>");
        expect(salida).toMatch(/<ol[^>]*><li>a<\/li><li>b<\/li><\/ol>/);
        expect(salida).toMatch(/<ul[^>]*><li>x<\/li><\/ul>/);
    });

    it("una pregunta en negrita va en su propio párrafo", () => {
        expect(bloquesManual("**¿Dónde?**\nAquí mismo.")).toEqual([
            { tipo: "parrafo", texto: "**¿Dónde?**" },
            { tipo: "parrafo", texto: "Aquí mismo." },
        ]);
    });

    it("negrita y código en línea", () => {
        const salida = html("Pulsa **Guardar** y escribe `tickets.ver`.");
        expect(salida).toContain("<strong>Guardar</strong>");
        expect(salida).toMatch(/<code[^>]*>tickets\.ver<\/code>/);
    });

    it("enlaces internos al manual y externos con rel=noopener noreferrer", () => {
        const salida = html("Ver [la bandeja](/manual/tickets/lista), [el índice](/manual) y [Meta](https://developers.facebook.com/docs).");
        expect(salida).toContain('<a href="/manual/tickets/lista"');
        expect(salida).toContain('<a href="/manual"');
        expect(salida).toContain('href="https://developers.facebook.com/docs"');
        expect(salida).toContain('target="_blank" rel="noopener noreferrer"');
        // Los internos no abren otra pestaña.
        expect(salida).not.toMatch(/href="\/manual\/tickets\/lista"[^>]*target=/);
    });

    it("la ruta de los enlaces internos se puede cambiar (app de campo sin señal)", () => {
        const salida = renderToStaticMarkup(<MarkdownManual fuente="[Hoy](/manual/campo/hoy)" ruta={(c) => `/campo/manual/${c?.split("/")[1] ?? ""}`} />);
        expect(salida).toContain('href="/campo/manual/hoy"');
    });

    it("nunca deja pasar HTML: <script> y atributos salen como texto", () => {
        const salida = html('<script>alert("x")</script>\n\n<img src=x onerror="alert(1)">\n\n- <b>negrita cruda</b>');
        expect(salida).not.toContain("<script");
        expect(salida).not.toContain("<img");
        expect(salida).not.toContain("<b>");
        expect(salida).toContain("&lt;script&gt;");
        expect(salida).toContain("&lt;img src=x onerror=");
    });

    it("javascript:, data:, mailto: y rutas fuera del manual no son enlaces", () => {
        for (const url of [
            "javascript:alert(1)",
            "JaVaScRiPt:alert(1)",
            "data:text/html,<b>x</b>",
            "mailto:a@b.c",
            "/tickets",
            "//malo.test/x",
            "/manual/../tickets",
            "/manual/tickets/lista?x=1",
        ]) {
            expect(destinoEnlace(url)).toBeNull();
        }
        const salida = html("[malo](javascript:alert(1)) y [otro](data:text/html,hola) y [panel](/tickets)");
        expect(salida).not.toContain("<a");
        expect(salida).not.toContain("javascript:");
        expect(salida).toContain("malo");
        expect(salida).toContain("panel");
    });

    it("texto plano para buscar y enlaces internos de una página", () => {
        const fuente = "# T\n\nPulsa **Guardar** en [Tickets](/manual/tickets/lista).\n\n- `uno`\n    - dos";
        expect(textoPlano(fuente)).toBe("T Pulsa Guardar en Tickets. uno dos");
        expect(enlacesInternos(`${fuente} [índice](/manual) [fuera](https://x.test)`)).toEqual(["tickets/lista", null]);
    });
});
