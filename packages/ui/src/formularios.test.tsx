import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { CampoArea } from "./componentes/area-texto";
import { CampoSelector } from "./componentes/selector";

describe("campos de formulario", () => {
    it("CampoSelector enlaza etiqueta, error y aria-invalid", () => {
        const html = renderToStaticMarkup(
            <CampoSelector id="p" etiqueta="Prioridad" error="Elige una">
                <option value="alta">Alta</option>
            </CampoSelector>,
        );
        expect(html).toContain('for="p"');
        expect(html).toMatch(/<select[^>]*id="p"[^>]*>/);
        expect(html).toMatch(/<select[^>]*aria-invalid="true"/);
        expect(html).toMatch(/<select[^>]*aria-describedby="p-error"/);
        expect(html).toContain('id="p-error"');
        expect(html).toContain("Elige una");
    });

    it("CampoArea con ayuda", () => {
        const html = renderToStaticMarkup(<CampoArea id="m" etiqueta="Motivo" ayuda="Lo verá el equipo" />);
        expect(html).toContain('aria-describedby="m-ayuda"');
        expect(html).toContain("<textarea");
        expect(html).not.toContain('aria-invalid="true"');
    });
});
