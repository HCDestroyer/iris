import { describe, expect, it } from "vitest";
import { pieDelResumen, textoMensajesNuevos } from "./resumen-asistente";

describe("resumen guardado del asistente", () => {
    const AHORA = Date.parse("2026-11-18T16:45:00Z");

    it("pie: quién lo pidió, cuándo y con qué modelo", () => {
        const pie = pieDelResumen({ generadoEn: "2026-11-18T16:42:00Z", modelo: "qwen3:32b", pedidoPor: { id: 7, nombre: "Andrea Castillo" } }, AHORA);
        expect(pie).toMatch(/^Generado por Andrea · .+ · qwen3:32b$/);
    });

    it("pie sin quien lo pidió (esa persona ya no existe) ni modelo: no deja huecos", () => {
        const pie = pieDelResumen({ generadoEn: "2026-11-18T16:42:00Z", modelo: "" }, AHORA);
        expect(pie).toMatch(/^Generado · [^·]+$/);
    });

    it("mensajes nuevos, en singular y en plural", () => {
        expect(textoMensajesNuevos(1)).toBe("Hay 1 mensaje nuevo desde este resumen");
        expect(textoMensajesNuevos(2)).toBe("Hay 2 mensajes nuevos desde este resumen");
        expect(textoMensajesNuevos(0)).toBe("Hay mensajes nuevos desde este resumen");
    });
});
