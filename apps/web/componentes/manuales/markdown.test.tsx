import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { bloquesMarkdown, markdownATexto, MarkdownSeguro, urlSegura } from "./markdown";

const html = (fuente: string) => renderToStaticMarkup(<MarkdownSeguro fuente={fuente} />);

describe("Markdown seguro de los manuales", () => {
    it("arma títulos, listas, citas y código", () => {
        expect(bloquesMarkdown("# Uno\n\nTexto\nsigue\n\n- a\n- b\n\n1. x\n2. y\n\n> cita\n\n```\ncodigo\n```").map((b) => b.tipo)).toEqual([
            "titulo",
            "parrafo",
            "lista",
            "lista",
            "cita",
            "codigo",
        ]);
    });

    it("nunca deja pasar HTML: etiquetas y atributos salen como texto", () => {
        const salida = html('<script>alert(1)</script>\n\n<img src=x onerror="alert(1)">');
        expect(salida).not.toContain("<script");
        expect(salida).not.toContain("<img");
        expect(salida).toContain("&lt;script&gt;");
    });

    it("solo http, https y mailto son enlaces; javascript: y data: quedan como texto", () => {
        expect(urlSegura("javascript:alert(1)")).toBeNull();
        expect(urlSegura("data:text/html,x")).toBeNull();
        expect(urlSegura("/relativo")).toBeNull();
        expect(urlSegura("https://iris.test/a")).toBe("https://iris.test/a");
        const salida = html("[malo](javascript:alert(1)) y [bueno](https://iris.test)");
        expect(salida).not.toContain('href="javascript');
        expect(salida).toContain('href="https://iris.test/"');
        expect(salida).toContain('rel="noopener noreferrer nofollow"');
    });

    it("negrita, cursiva y código en línea", () => {
        expect(html("**fuerte** _suave_ `F670L`")).toBe(
            '<div class="grid gap-3 text-sm leading-relaxed [overflow-wrap:anywhere]"><p><strong>fuerte</strong> <em>suave</em> <code class="rounded bg-background-secondary px-1 font-mono text-[0.85em]">F670L</code></p></div>',
        );
    });

    it("a texto plano para WhatsApp", () => {
        expect(markdownATexto("## Reiniciar\n\n1. **Apaga** el módem\n2. Espera\n\nMás en [la guía](https://iris.test/g).")).toBe(
            "Reiniciar\n\n1. Apaga el módem\n2. Espera\n\nMás en la guía (https://iris.test/g).",
        );
    });
});
