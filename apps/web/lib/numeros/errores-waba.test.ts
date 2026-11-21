import { describe, expect, it } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import { errorDeWaba } from "./errores-waba";

describe("errorDeWaba", () => {
    it("explica la cuenta de otra empresa y la que no coincide", () => {
        expect(errorDeWaba(new ErrorApi({ estado: 409, codigo: "waba_de_otra_empresa", titulo: "Conflicto" }))).toMatch(/otra empresa/);
        expect(errorDeWaba(new ErrorApi({ estado: 422, codigo: "waba_no_coincide", titulo: "La cuenta no coincide" }))).toMatch(/no es de esa cuenta/);
    });
    it("deja pasar los demás errores", () => {
        expect(errorDeWaba(new ErrorApi({ estado: 409, codigo: "canal_existente", titulo: "x" }))).toBeNull();
        expect(errorDeWaba(new Error("x"))).toBeNull();
    });
});
