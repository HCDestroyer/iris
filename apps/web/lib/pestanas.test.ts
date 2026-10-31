import { describe, expect, it } from "vitest";
import { pestanaDeParametro, urlConPestana } from "./pestanas";

const VALIDAS = ["resumen", "conversacion", "ordenes", "historial"] as const;

describe("pestañas en la URL", () => {
    it("solo acepta una de la lista; lo demás abre la inicial", () => {
        expect(pestanaDeParametro("historial", VALIDAS, "resumen")).toBe("historial");
        for (const v of [null, undefined, "", "HISTORIAL", "historial ", "<script>", "__proto__", "constructor"]) {
            expect(pestanaDeParametro(v, VALIDAS, "resumen")).toBe("resumen");
        }
    });

    it("escribe la pestaña en la URL conservando lo demás; la inicial no se escribe", () => {
        expect(urlConPestana("/tickets/1", new URLSearchParams(""), "historial", "resumen")).toBe("/tickets/1?pestana=historial");
        expect(urlConPestana("/tickets/1", new URLSearchParams("pestana=historial&x=1"), "resumen", "resumen")).toBe("/tickets/1?x=1");
        expect(urlConPestana("/tickets/1", new URLSearchParams("x=1"), "ordenes", "resumen")).toBe("/tickets/1?x=1&pestana=ordenes");
    });
});
