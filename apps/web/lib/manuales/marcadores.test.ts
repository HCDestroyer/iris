import { describe, expect, it } from "vitest";
import { marcadoresPendientes, textoMarcadores, tieneMarcadores } from "./marcadores";

describe("marcadores de datos por completar", () => {
    it("encuentra lo que va entre corchetes, sin repetir y en orden", () => {
        const texto = "Llame al [número de soporte] o escriba al [número de WhatsApp].\n\nDe nuevo: [número de soporte]. Pague a **[Nombre del ISP]**.";
        expect(marcadoresPendientes(texto)).toEqual(["[número de soporte]", "[número de WhatsApp]", "[Nombre del ISP]"]);
        expect(tieneMarcadores(texto)).toBe(true);
    });

    it("un enlace de Markdown, un corchete suelto o de una letra no cuentan", () => {
        expect(marcadoresPendientes("Vea el [video](https://iris.test/v), la lista [ ] y [x].")).toEqual([]);
        expect(tieneMarcadores("Ya completado: llame al 2222-0000.")).toBe(false);
    });

    it("los nombra para el aviso", () => {
        expect(textoMarcadores(["[A]"])).toBe("[A]");
        expect(textoMarcadores(["[A]", "[B]"])).toBe("[A] y [B]");
        expect(textoMarcadores(["[A]", "[B]", "[C]", "[D]"])).toBe("[A], [B] y 2 más");
    });
});
