import { describe, expect, it } from "vitest";
import { fechaEncabezadoCorta } from "./fechas";

describe("fechaEncabezadoCorta", () => {
    it("«Sáb 21 nov» en la zona de Guatemala, sin puntos ni comas", () => {
        // 21 nov 2026, 10:00 en Guatemala (16:00 UTC).
        expect(fechaEncabezadoCorta(Date.UTC(2026, 10, 21, 16))).toBe("Sáb 21 nov");
        // 1 dic 2026, 02:00 UTC: todavía 30 nov en Guatemala.
        expect(fechaEncabezadoCorta(Date.UTC(2026, 11, 1, 2))).toBe("Lun 30 nov");
    });
});
