import { describe, expect, it } from "vitest";
import { idDeParametro } from "./parametros";

describe("idDeParametro", () => {
    it("acepta solo enteros positivos escritos con dígitos", () => {
        expect(idDeParametro("12")).toBe(12);
        expect(idDeParametro("999999999999999")).toBe(999999999999999);
    });

    it("rechaza lo demás", () => {
        for (const crudo of [null, undefined, "", "0", "-3", "1.5", "1e3", "0x10", " 7", "7 ", "abc", "12abc", "9999999999999999", "<script>"]) {
            expect(idDeParametro(crudo)).toBeNull();
        }
    });
});
