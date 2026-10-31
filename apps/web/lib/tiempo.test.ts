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

    it("rechaza caracteres de control y lo que resuelve a otro origen", () => {
        for (const malo of ["/\t/evil.com", "/\n/evil.com", "//evil.com", "/\\evil.com", "/\r//evil.com", "/\u0000x", "/x\u007f"]) {
            expect(rutaSegura(malo, "/", "https://iris.test")).toBe("/");
        }
        expect(rutaSegura("/campo?orden=3", "/", "https://iris.test")).toBe("/campo?orden=3");
    });

    it("rechaza lo que se normaliza a «//host» y devuelve la ruta resuelta", () => {
        for (const malo of ["/.//evil.com/x", "/..//evil.com/x", "/a/..//evil.com", "/%2e//evil.com", "/x\\evil.com", "/./entrar", "/a/../api/v1/yo"]) {
            expect(rutaSegura(malo, "/", "https://iris.test"), malo).toBe("/");
        }
        expect(rutaSegura("/tickets/../ordenes?x=1#a", "/", "https://iris.test")).toBe("/ordenes?x=1#a");
    });
});
