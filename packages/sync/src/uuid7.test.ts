import { describe, expect, it } from "vitest";
import { crearGeneradorUuid7, msDeUuid7, RE_UUID7 } from "./uuid7";

describe("UUID v7", () => {
    it("tiene la forma de la RFC 9562 (versión 7, variante 10)", () => {
        const generar = crearGeneradorUuid7();
        for (let i = 0; i < 100; i += 1) {
            expect(generar()).toMatch(RE_UUID7);
        }
    });

    it("codifica los milisegundos y se ordena por tiempo", () => {
        let ahora = Date.parse("2026-10-28T15:00:00Z");
        const generar = crearGeneradorUuid7(() => ahora);
        const primero = generar();
        ahora += 1;
        const segundo = generar();
        expect(msDeUuid7(primero)).toBe(Date.parse("2026-10-28T15:00:00Z"));
        expect(segundo > primero).toBe(true);
    });

    it("sigue ordenado dentro del mismo milisegundo y si el reloj retrocede", () => {
        let ahora = 1_800_000_000_000;
        const generar = crearGeneradorUuid7(() => ahora);
        const ids: string[] = [];
        for (let i = 0; i < 5000; i += 1) {
            ids.push(generar());
        }
        ahora -= 60_000; // el técnico ajusta la hora del teléfono hacia atrás
        ids.push(generar());
        expect([...ids].sort()).toEqual(ids);
        expect(new Set(ids).size).toBe(ids.length);
    });

    it("usa la fuente de azar inyectada", () => {
        const generar = crearGeneradorUuid7(
            () => 0,
            (b) => b.fill(0),
        );
        expect(generar()).toBe("00000000-0000-7000-8000-000000000000");
    });
});
