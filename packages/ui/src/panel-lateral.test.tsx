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
        // Sólido (la capa flotante del tema): a pantalla completa en el teléfono y, desde `sm`, isla
        // flotante de 470 px a 16 px de los bordes, con radio de isla (20 px) y la sombra de lo modal.
        expect(html).toContain("bg-[var(--overlay)]");
        expect(html).toMatch(/fixed inset-x-0 top-\[3\.75rem\] bottom-0 z-40/);
        expect(html).toContain("sm:top-[3.75rem]");
        expect(html).toContain("lg:top-4");
        expect(html).toContain("sm:right-4");
        expect(html).toContain("sm:bottom-4");
        expect(html).toContain("sm:w-[min(29.375rem,calc(100vw-2rem))]");
        expect(html).toContain("sm:rounded-[var(--iris-radio-isla)]");
        expect(html).toContain("var(--iris-sombra-modal)");
    });

    it("con telón es el Drawer de HeroUI (modal), con la misma isla", () => {
        const html = renderToStaticMarkup(
            <PanelLateral abierto telon alCerrar={() => undefined} etiqueta="Registrar visita">
                <p>Formulario</p>
            </PanelLateral>,
        );
        // El Drawer se monta en un portal (cliente): en el servidor no deja un panel sin telón.
        expect(html).not.toContain('aria-modal="false"');
    });
});
