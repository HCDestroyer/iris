import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { CampoArea } from "./componentes/area-texto";
import { Casilla } from "./componentes/casilla";
import { GrupoOpciones } from "./componentes/grupo-opciones";
import { ListaEleccion } from "./componentes/lista-eleccion";
import { CampoSelector } from "./componentes/selector";

describe("campos de formulario", () => {
    it("CampoSelector enlaza etiqueta, error y aria-invalid", () => {
        const html = renderToStaticMarkup(
            <CampoSelector id="p" etiqueta="Prioridad" error="Elige una">
                <option value="alta">Alta</option>
            </CampoSelector>,
        );
        // La etiqueta de HeroUI nombra el botón (con el valor elegido delante, como React Aria).
        expect(html).toMatch(/<span[^>]*id="([^"]+)"[^>]*data-slot="label"[^>]*>Prioridad<\/span>[\s\S]*<button[^>]*id="p"[^>]*aria-labelledby="[^"]*\1"/);
        expect(html).toMatch(/class="select[^"]*"[^>]*data-invalid="true"|data-invalid="true"[^>]*class="select/);
        expect(html).toMatch(/<button[^>]*aria-describedby="[^"]*p-error/);
        expect(html).toContain('id="p-error"');
        expect(html).toContain("Elige una");
    });

    it("CampoArea con ayuda", () => {
        const html = renderToStaticMarkup(<CampoArea id="m" etiqueta="Motivo" ayuda="Lo verá el equipo" />);
        expect(html).toContain('aria-describedby="m-ayuda"');
        expect(html).toContain("<textarea");
        expect(html).not.toContain('aria-invalid="true"');
    });

    it("Casilla: el Checkbox de HeroUI con un checkbox real dentro de su etiqueta, con ayuda", () => {
        const html = renderToStaticMarkup(<Casilla id="s" etiqueta="Responder por correo" ayuda="Usa SMTP" defaultChecked />);
        expect(html).toContain('class="checkbox ');
        expect(html).toMatch(/<label[^>]*checkbox__content[^>]*>[\s\S]*<input id="s"[^>]*type="checkbox"[^>]*checked=""/);
        expect(html).toMatch(/<input[^>]*aria-describedby="[^"]*s-ayuda"/);
        expect(html).toContain("Responder por correo");
        expect(html).toContain("Usa SMTP");
        // La marca se dibuja desde el input (`:checked`): sigue a `register`, `reset` y `setValue`.
        expect(html).toContain("group-has-[input:checked]:bg-primary");
        expect(html).toContain("lucide-check");
    });

    it("GrupoOpciones: el RadioGroup de HeroUI con radios reales, texto en cada opción y error enlazado", () => {
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
        expect(html).toMatch(/class="radio-group[^"]*"[^>]*role="radiogroup"[^>]*aria-invalid="true"/);
        expect(html).toMatch(/aria-labelledby="([^"]+)"[\s\S]*id="\1"[^>]*>¿Cómo te atendimos\?/);
        expect(html.match(/type="radio"/g)).toHaveLength(2);
        expect(html).toMatch(/<input[^>]*type="radio"[^>]*name="calificacion" checked="" value="5"/);
        expect(html).toMatch(/aria-describedby="[^"]*-error"[^>]*role="radiogroup"/);
        expect(html).toContain("Muy mal");
        expect(html).toContain("Elige una calificación");
    });

    it("ListaEleccion: radios reales con contenido propio; la deshabilitada no se elige y la atenuada sí", () => {
        const html = renderToStaticMarkup(
            <ListaEleccion
                leyenda="Compañero de Soporte"
                nombre="usuario"
                opciones={[
                    { valor: "1", texto: "Rodrigo Sánchez, disponible", contenido: <span>Rodrigo Sánchez</span>, fin: <span>Disponible</span> },
                    { valor: "2", texto: "Mario Ixcot, no disponible", contenido: <span>Mario Ixcot</span>, atenuada: true },
                    { valor: "3", texto: "Carlos Cifuentes, no acepta mensajes", contenido: <span>Carlos Cifuentes</span>, deshabilitada: true },
                ]}
                valor="2"
                alCambiar={() => undefined}
            />,
        );
        expect(html).toMatch(/role="radiogroup"/);
        expect(html.match(/type="radio"/g)).toHaveLength(3);
        expect(html).toMatch(/<input[^>]*aria-label="Mario Ixcot, no disponible"[^>]*name="usuario" checked=""/);
        expect(html).toMatch(/<input[^>]*aria-label="Carlos Cifuentes, no acepta mensajes"[^>]*disabled=""/);
        expect(html).not.toMatch(/<input[^>]*aria-label="Mario Ixcot, no disponible"[^>]*disabled=""/);
        expect(html).toContain('data-elegida="true"');
    });
});
