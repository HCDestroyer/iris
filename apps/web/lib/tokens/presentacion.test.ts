import { describe, expect, it } from "vitest";
import { accionesToken, cuantosTokens, estadoToken, tituloLote } from "./presentacion";

const AHORA = Date.parse("2026-11-19T12:00:00Z");

describe("estadoToken", () => {
    it("vigente sin revocar ni vencer (o sin vencimiento)", () => {
        expect(estadoToken({ revocadoEn: null, expiraEn: null }, AHORA)).toBe("vigente");
        expect(estadoToken({ revocadoEn: null, expiraEn: "2026-11-20T00:00:00Z" }, AHORA)).toBe("vigente");
    });
    it("vencido al pasar expiraEn; revocado gana si las dos pasaron", () => {
        expect(estadoToken({ revocadoEn: null, expiraEn: "2026-11-19T12:00:00Z" }, AHORA)).toBe("vencido");
        expect(estadoToken({ revocadoEn: "2026-11-01T00:00:00Z", expiraEn: "2026-11-02T00:00:00Z" }, AHORA)).toBe("revocado");
    });
});

describe("accionesToken", () => {
    it("vigente: revocar y, con los dos permisos, revocar y eliminar", () => {
        expect(accionesToken("vigente", true, true)).toEqual(["revocar", "revocar_eliminar"]);
        expect(accionesToken("vigente", true, false)).toEqual(["revocar"]);
        expect(accionesToken("vigente", false, true)).toEqual([]);
    });
    it("el de un conector activo: nada (se cambia desde Integraciones)", () => {
        expect(accionesToken("vigente", true, true, true)).toEqual([]);
        expect(accionesToken("revocado", true, true, true)).toEqual([]);
    });
    it("revocado o vencido: solo eliminar (con tokens.eliminar)", () => {
        expect(accionesToken("revocado", true, true)).toEqual(["eliminar"]);
        expect(accionesToken("vencido", false, true)).toEqual(["eliminar"]);
        expect(accionesToken("vencido", true, false)).toEqual([]);
    });
});

describe("textos", () => {
    it("singular y plural", () => {
        expect(cuantosTokens(1)).toBe("1 token");
        expect(cuantosTokens(3)).toBe("3 tokens");
        expect(tituloLote(1)).toBe("¿Eliminar 1 token revocado o vencido?");
        expect(tituloLote(4)).toBe("¿Eliminar 4 tokens revocados y vencidos?");
    });
});
