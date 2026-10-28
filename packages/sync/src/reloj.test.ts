import { describe, expect, it } from "vitest";
import { calcularEspera } from "./reloj";

describe("calcularEspera", () => {
    const opciones = (azar: number) => ({ baseMs: 1000, maximoMs: 60_000, azar: () => azar });

    it("crece al doble con jitter entre la mitad y el techo", () => {
        expect([0, 1, 2, 3].map((n) => calcularEspera(n, opciones(0)))).toEqual([500, 1000, 2000, 4000]);
        expect([0, 1, 2, 3].map((n) => calcularEspera(n, opciones(0.999999)))).toEqual([1000, 2000, 4000, 8000]);
    });

    it("no pasa del tope", () => {
        expect(calcularEspera(30, opciones(0.999999))).toBe(60_000);
        expect(calcularEspera(30, opciones(0))).toBe(30_000);
    });
});
