import { describe, expect, it } from "vitest";
import { deLaEmpresa, listaUsos, textoCompartido } from "./lista";

describe("números de la empresa activa", () => {
    const duena = { id: 9, empresaId: 1, nombre: "Avisos (vista de Red Sur)" };
    const prestada = { id: 9, empresaId: 2, nombre: "Avisos (prestado a Cable Norte)" };
    const propio = { id: 4, empresaId: 2, nombre: "Norte soporte" };
    const otro = { id: 5, empresaId: 3, nombre: "De Fibra Occidente" };

    it("con empresas.todas el número prestado no sale dos veces: queda la entrada de la activa", () => {
        expect(deLaEmpresa([duena, prestada, propio, otro], 2)).toEqual([prestada, propio]);
    });

    it("sin empresa activa (sin conector) no filtra por empresa, pero tampoco repite ids", () => {
        const sinEmpresa = { id: 7, empresaId: null, nombre: "Único" };
        expect(deLaEmpresa([sinEmpresa, { ...sinEmpresa, nombre: "repetido" }], null)).toEqual([sinEmpresa]);
        expect(deLaEmpresa([duena, otro], undefined)).toEqual([duena, otro]);
    });
});

describe("textos de la lista", () => {
    it("compartido y usos", () => {
        expect(textoCompartido(1)).toBe("Compartido con 1 empresa");
        expect(textoCompartido(2)).toBe("Compartido con 2 empresas");
        expect(listaUsos(["avisos", "soporte"], (u) => u.charAt(0).toUpperCase() + u.slice(1))).toBe("avisos y soporte");
        expect(listaUsos(["ventas"], (u) => u)).toBe("ventas");
    });
});
