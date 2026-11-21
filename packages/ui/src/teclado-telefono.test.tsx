import { Mic } from "lucide-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ControlLlamada, TecladoTelefono } from "./componentes/teclado-telefono";

describe("Teléfono", () => {
    it("el teclado tiene 12 teclas con nombre; las letras no se leen", () => {
        const html = renderToStaticMarkup(<TecladoTelefono alTocar={() => undefined} etiqueta="Marcar" />);
        expect(html.match(/<button/g)).toHaveLength(12);
        expect(html).toContain('aria-label="Marcar"');
        expect(html).toContain('aria-label="Asterisco"');
        expect(html).toContain('aria-label="Numeral"');
        expect(html).toContain('aria-hidden="true"');
        expect(html).toContain("PQRS");
        expect(html).toContain("h-14");
    });

    it("un control es un interruptor con texto, y colgar va en rojo", () => {
        const silencio = renderToStaticMarkup(<ControlLlamada icono={Mic} etiqueta="Silenciar" activo={false} />);
        expect(silencio).toContain('aria-pressed="false"');
        expect(silencio).toContain("Silenciar");
        const activo = renderToStaticMarkup(<ControlLlamada icono={Mic} etiqueta="Silenciada" activo />);
        expect(activo).toContain('aria-pressed="true"');
        expect(activo).toContain('data-activo="true"');
        const colgar = renderToStaticMarkup(<ControlLlamada icono={Mic} etiqueta="Colgar" peligro />);
        expect(colgar).not.toContain("aria-pressed");
        expect(colgar).toContain("bg-error");
    });
});
