import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PanelLateral } from "./componentes/panel-lateral";

describe("PanelLateral", () => {
    it("cerrado no pinta nada", () => {
        const html = renderToStaticMarkup(
            <PanelLateral abierto={false} alCerrar={() => undefined} etiqueta="Chat del equipo">
                <p>Contenido</p>
            </PanelLateral>,
        );
        expect(html).toBe("");
    });

    it("abierto es un diálogo no modal, con nombre y enfocable, sin telón", () => {
        const html = renderToStaticMarkup(
            <PanelLateral abierto alCerrar={() => undefined} etiqueta="Chat del equipo">
                <p>Contenido</p>
            </PanelLateral>,
        );
        expect(html).toContain('role="dialog"');
        expect(html).toContain('aria-modal="false"');
        expect(html).toContain('aria-label="Chat del equipo"');
        expect(html).toContain('tabindex="-1"');
        expect(html).toContain('data-slot="panel-lateral"');
        expect(html).toContain("Contenido");
        // Sin telón: ni el de HeroUI ni uno propio.
        expect(html).not.toMatch(/backdrop/);
        // Sólido (la capa flotante del tema), a la derecha bajo la barra superior.
        expect(html).toContain("bg-[var(--overlay)]");
        expect(html).toMatch(/fixed top-14 right-0/);
    });
});
