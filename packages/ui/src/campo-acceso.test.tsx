import { renderToStaticMarkup } from "react-dom/server";
import { Lock } from "lucide-react";
import { describe, expect, it } from "vitest";
import { CampoCodigo, CampoConIcono } from "./componentes/campo-acceso";

describe("campos de acceso", () => {
    it("contraseña: oculta lo escrito, con el ojo accesible y el error en línea enlazado", () => {
        const html = renderToStaticMarkup(<CampoConIcono id="c" etiqueta="Contraseña" icono={Lock} secreto autoComplete="current-password" error="Correo o contraseña incorrectos." />);
        expect(html).toMatch(/<input[^>]*type="password"/);
        expect(html).toMatch(/<input[^>]*autoComplete="current-password"|<input[^>]*autocomplete="current-password"/);
        expect(html).toContain('aria-label="Mostrar contraseña"');
        expect(html).toContain('aria-pressed="false"');
        expect(html).toMatch(/<input[^>]*aria-describedby="c-error"/);
        expect(html).toContain("Correo o contraseña incorrectos.");
    });

    it("sin `secreto` respeta el tipo (correo)", () => {
        const html = renderToStaticMarkup(<CampoConIcono id="m" etiqueta="Correo" type="email" />);
        expect(html).toMatch(/<input[^>]*type="email"/);
        expect(html).not.toContain("Mostrar contraseña");
    });

    it("código: InputOTP de 6 casillas, solo dígitos, `one-time-code` y error con role=alert", () => {
        const html = renderToStaticMarkup(<CampoCodigo etiqueta="Escribe los 6 dígitos" valor="48" alCambiar={() => undefined} error="Código incorrecto" />);
        expect(html).toMatch(/maxLength="6"|maxlength="6"/);
        expect(html).toMatch(/inputMode="numeric"|inputmode="numeric"/);
        expect(html).toMatch(/autoComplete="one-time-code"|autocomplete="one-time-code"/);
        expect(html.match(/data-slot="input-otp-slot"/g)?.length ?? html.match(/input-otp__slot/g)?.length).toBe(6);
        expect(html).toContain('role="alert"');
    });
});
