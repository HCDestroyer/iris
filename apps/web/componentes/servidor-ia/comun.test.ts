import { describe, expect, it } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import { esperaTrasFallos, sinReintento } from "./comun";

describe("sondeo del tablero del Servidor de IA", () => {
    it("tras fallos seguidos espera 10, 20, 40 y 60 s, nunca menos que el ritmo del periodo", () => {
        expect(esperaTrasFallos(0, 10_000)).toBe(10_000);
        expect([1, 2, 3, 4, 5, 9].map((n) => esperaTrasFallos(n, 10_000))).toEqual([10_000, 20_000, 40_000, 60_000, 60_000, 60_000]);
        expect(esperaTrasFallos(1, 30_000)).toBe(30_000);
        expect(esperaTrasFallos(3, 30_000)).toBe(40_000);
        expect(esperaTrasFallos(0, 60_000)).toBe(60_000);
    });

    it("con 401, 403 o 404 no se reintenta solo; con 5xx o sin red, sí", () => {
        const error = (estado: number) => new ErrorApi({ estado, codigo: "x", titulo: "x" });
        expect([401, 403, 404].map((e) => sinReintento(error(e)))).toEqual([true, true, true]);
        expect([0, 409, 500, 502, 503].map((e) => sinReintento(error(e)))).toEqual([false, false, false, false, false]);
        expect(sinReintento(new Error("red"))).toBe(false);
    });
});
