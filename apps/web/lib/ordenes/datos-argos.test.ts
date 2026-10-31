import { describe, expect, it } from "vitest";
import { cambiosDeArgos, textoCamposArgos } from "./datos-argos";

describe("dato de Argos cambió", () => {
    it("solo con la forma esperada", () => {
        expect(cambiosDeArgos({})).toBeUndefined();
        expect(cambiosDeArgos({ datosArgosCambiados: null })).toBeUndefined();
        expect(cambiosDeArgos({ datosArgosCambiados: { campos: ["x"] } })).toBeUndefined();
        expect(cambiosDeArgos({ datosArgosCambiados: { en: "2026-10-31T10:00:00Z", campos: ["servicio.direccion", 3] } })).toEqual({
            en: "2026-10-31T10:00:00Z",
            campos: ["servicio.direccion"],
        });
    });

    it("dice qué cambió en palabras, sin repetir", () => {
        expect(textoCamposArgos(["servicio.lat", "servicio.lon", "servicio.contacto_telefono"])).toBe("ubicación del servicio, teléfono de contacto en sitio");
        expect(textoCamposArgos(["otro.campo"])).toBe("otro.campo");
        expect(textoCamposArgos(null)).toBe("");
    });
});
