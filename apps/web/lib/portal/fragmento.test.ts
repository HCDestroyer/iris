import { describe, expect, it } from "vitest";
import { ticketDesdeFragmento } from "./fragmento";

describe("ticketDesdeFragmento", () => {
    it("lee el ticket después de #", () => {
        expect(ticketDesdeFragmento("#abcdefghijklmnop_QRS-123")).toEqual({ ticket: "abcdefghijklmnop_QRS-123" });
    });
    it("sin fragmento no hay ticket", () => {
        expect(ticketDesdeFragmento("")).toBeNull();
        expect(ticketDesdeFragmento("#")).toBeNull();
    });
    it("corto, largo, con espacios o mal codificado es un enlace inválido", () => {
        expect(ticketDesdeFragmento("#corto")).toEqual({ invalido: true });
        expect(ticketDesdeFragmento(`#${"a".repeat(201)}`)).toEqual({ invalido: true });
        expect(ticketDesdeFragmento("#abcdefgh%20ijklmnopq")).toEqual({ invalido: true });
        expect(ticketDesdeFragmento("#%E0%A4%A")).toEqual({ invalido: true });
    });
});
