import { describe, expect, it } from "vitest";
import { formatearFecha, haceCuanto } from "./fechas";

describe("fechas en America/Guatemala", () => {
    it("dd/mm/aaaa HH:mm (UTC−6)", () => {
        expect(formatearFecha("2026-10-28T02:05:00Z")).toBe("27/10/2026 20:05");
        expect(formatearFecha(null)).toBeUndefined();
        expect(formatearFecha("no")).toBeUndefined();
    });
    it("relativas", () => {
        const ahora = Date.parse("2026-10-28T15:00:00Z");
        expect(haceCuanto("2026-10-28T14:55:00Z", ahora)).toBe("hace 5 min");
        expect(haceCuanto("2026-10-28T12:00:00Z", ahora)).toBe("hace 3 h");
        expect(haceCuanto("2026-10-26T12:00:00Z", ahora)).toBe("26/10/2026 06:00");
    });
});
