import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { CampoSecreto } from "./componentes/campo-secreto";
import { BarraGuardar } from "./componentes/formulario-pagina";

describe("CampoSecreto", () => {
    it("oculta lo escrito, sin que el gestor de contraseñas lo tome ni el corrector, con el ojo que dice qué muestra", () => {
        const html = renderToStaticMarkup(<CampoSecreto id="t" etiqueta="Token de acceso" ayuda="Iris lo guarda cifrado." />);
        // En el servidor (sin saber si hay `-webkit-text-security`) es `password`; nunca en claro.
        expect(html).toMatch(/<input[^>]*type="password"/);
        expect(html).toMatch(/<input[^>]*autocomplete="new-password"/i);
        expect(html).toMatch(/<input[^>]*data-oculto="true"/);
        expect(html).toMatch(/<input[^>]*data-1p-ignore/);
        expect(html).toMatch(/<input[^>]*data-lpignore="true"/);
        expect(html).toMatch(/<input[^>]*spellcheck="false"/i);
        expect(html).toMatch(/<input[^>]*autocapitalize="off"/i);
        expect(html).toContain('aria-label="Mostrar token de acceso"');
        expect(html).toContain('aria-pressed="false"');
        expect(html).toMatch(/<input[^>]*aria-describedby="t-ayuda"/);
    });

    it("no deja que la página cambie el tipo ni el autocompletado", () => {
        // @ts-expect-error: `type` no es una prop de CampoSecreto.
        const html = renderToStaticMarkup(<CampoSecreto id="s" etiqueta="App secret" type="text" />);
        expect(html).toMatch(/<input[^>]*type="password"/);
        expect(html).toContain('aria-label="Mostrar app secret"');
    });

    it("error en línea enlazado al campo", () => {
        const html = renderToStaticMarkup(<CampoSecreto id="e" etiqueta="App secret" nombre="el app secret" error="Entre 16 y 256 caracteres" />);
        expect(html).toMatch(/<input[^>]*aria-invalid="true"/);
        expect(html).toMatch(/<input[^>]*aria-describedby="e-error"/);
        expect(html).toContain("Entre 16 y 256 caracteres");
        expect(html).toContain('aria-label="Mostrar el app secret"');
    });
});

describe("BarraGuardar con nota", () => {
    it("muestra la nota junto a los botones", () => {
        const html = renderToStaticMarkup(<BarraGuardar textoGuardar="Guardar y probar" nota="Al guardar, Iris prueba la conexión con Meta." />);
        expect(html).toContain('data-slot="nota-barra"');
        expect(html).toContain("Al guardar, Iris prueba la conexión con Meta.");
        expect(html).toContain("Guardar y probar");
    });
});
