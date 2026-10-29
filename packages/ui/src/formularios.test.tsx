import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { CampoArea } from "./componentes/area-texto";
import { Casilla } from "./componentes/casilla";
import { GrupoOpciones } from "./componentes/grupo-opciones";
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

    it("Casilla: checkbox dentro de su etiqueta, con ayuda", () => {
        const html = renderToStaticMarkup(<Casilla id="s" etiqueta="Responder por correo" ayuda="Usa SMTP" defaultChecked />);
        expect(html).toMatch(/<label[^>]*for="s"[^>]*><input type="checkbox"/);
        expect(html).toMatch(/<input[^>]*aria-describedby="s-ayuda"/);
        expect(html).toContain("Usa SMTP");
    });

    it("GrupoOpciones: radios nativos en un fieldset, con texto en cada opción y error enlazado", () => {
        const html = renderToStaticMarkup(
            <GrupoOpciones
                leyenda="¿Cómo te atendimos?"
                nombre="calificacion"
                opciones={[
                    { valor: 1, etiqueta: "Muy mal" },
                    { valor: 5, etiqueta: "Muy bien" },
                ]}
                valor={5}
                alCambiar={() => undefined}
                error="Elige una calificación"
            />,
        );
        expect(html).toMatch(/<fieldset[^>]*aria-invalid="true"/);
        expect(html).toContain("<legend");
        expect(html.match(/type="radio"/g)).toHaveLength(2);
        expect(html).toMatch(/<input type="radio"[^>]*name="calificacion" checked="" value="5"/);
        expect(html).toContain("Muy mal");
        expect(html).toContain("Elige una calificación");
    });
});
