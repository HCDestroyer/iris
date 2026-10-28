import { describe, expect, it } from "vitest";
import { formatearEspera, rutaSegura } from "./tiempo";

describe("formatearEspera", () => {
    it("redondea hacia arriba a minutos, o segundos bajo un minuto", () => {
        expect(formatearEspera(900)).toBe("15 min");
        expect(formatearEspera(61)).toBe("2 min");
        expect(formatearEspera(45)).toBe("45 s");
        expect(formatearEspera(-3)).toBe("0 s");
    });
});

describe("rutaSegura", () => {
    it("solo acepta rutas internas", () => {
        expect(rutaSegura("/usuarios?x=1")).toBe("/usuarios?x=1");
        expect(rutaSegura("//malo.example")).toBe("/");
        expect(rutaSegura("/\\malo.example")).toBe("/");
        expect(rutaSegura("https://malo.example")).toBe("/");
        expect(rutaSegura("/entrar")).toBe("/");
        expect(rutaSegura(null)).toBe("/");
    });
});
