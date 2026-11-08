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
        // Sólido (la capa flotante del tema), bajo la barra superior: a pantalla completa en el
        // teléfono y, desde `sm`, tarjeta flotante de 380 px separada de los bordes.
        expect(html).toContain("bg-[var(--overlay)]");
        expect(html).toMatch(/fixed inset-x-0 top-14 bottom-0/);
        expect(html).toContain("sm:right-3");
        expect(html).toContain("sm:bottom-3");
        expect(html).toContain("sm:w-[min(23.75rem,calc(100vw-1.5rem))]");
        expect(html).toContain("sm:rounded-[var(--hx-radius-card)]");
    });
});
