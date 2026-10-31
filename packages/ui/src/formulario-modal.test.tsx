import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { asignarErrores, BotonEnviar, campoDelFormulario, ErrorFormulario, textoFaltan } from "./componentes/formulario-modal";

describe("patrón de formularios en modales", () => {
    it("textoFaltan arma la frase con una o varias selecciones", () => {
        expect(textoFaltan([])).toBe("");
        expect(textoFaltan(["el cliente"])).toBe("Falta elegir el cliente para guardar.");
        expect(textoFaltan(["el cliente", "el servicio", "la zona"])).toBe("Falta elegir el cliente, el servicio y la zona para guardar.");
    });

    it("BotonEnviar se deshabilita y explica lo que falta (aria-describedby)", () => {
        const html = renderToStaticMarkup(
            <BotonEnviar id="g" faltan={["el técnico"]}>
                Guardar
            </BotonEnviar>,
        );
        expect(html).toMatch(/<button[^>]*type="submit"[^>]*disabled=""/);
        expect(html).toMatch(/<button[^>]*aria-describedby="g-falta"/);
        expect(html).toContain('id="g-falta"');
        expect(html).toContain("Falta elegir el técnico para guardar.");
    });

    it("BotonEnviar sin faltantes queda habilitado y sin mensaje; enviando muestra su texto", () => {
        const listo = renderToStaticMarkup(<BotonEnviar>Guardar</BotonEnviar>);
        expect(listo).not.toContain('disabled=""');
        expect(listo).not.toContain("Falta elegir");
        const enviando = renderToStaticMarkup(
            <BotonEnviar enviando textoEnviando="Guardando…">
                Guardar
            </BotonEnviar>,
        );
        expect(enviando).toMatch(/disabled=""/);
        expect(enviando).toContain("Guardando…");
    });

    it("campoDelFormulario traduce las rutas de la API", () => {
        expect(campoDelFormulario("body.contacto.correo")).toBe("contacto.correo");
        expect(campoDelFormulario("body.lineas[2].precio")).toBe("lineas.2.precio");
        expect(campoDelFormulario("motivo")).toBe("motivo");
    });

    it("asignarErrores pone cada error en su campo y devuelve los que no tienen campo", () => {
        const puestos: [string, string][] = [];
        const restantes = asignarErrores(
            [
                { field: "body.motivo", message: "Muy corto" },
                { field: "body.lineas[0].precio", message: "Precio inválido" },
                { field: "body.version", message: "Versión vieja" },
            ],
            ["motivo", "lineas.0.precio"] as const,
            (campo, e) => puestos.push([campo, e.message]),
        );
        expect(puestos).toEqual([
            ["motivo", "Muy corto"],
            ["lineas.0.precio", "Precio inválido"],
        ]);
        expect(restantes).toEqual([{ field: "body.version", message: "Versión vieja" }]);
    });

    it("ErrorFormulario anuncia el error y lista los errores sin campo", () => {
        const html = renderToStaticMarkup(<ErrorFormulario titulo="Datos inválidos" detalle="Revisa el formulario" errores={[{ field: "x", message: "Algo falló" }]} />);
        expect(html).toContain('role="alert"');
        expect(html).toContain("Datos inválidos");
        expect(html).toContain("<li>Algo falló</li>");
    });
});
