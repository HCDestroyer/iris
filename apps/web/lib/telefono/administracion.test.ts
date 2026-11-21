import { describe, expect, it } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import { credencialRechazada, esperaPruebaRegistro } from "./administracion";

describe("«Probar registro» en espera (re-verificación de #252)", () => {
    const e = (codigo: string, reintentarEn?: number, estado = 429) => new ErrorApi({ estado, codigo, titulo: "x", reintentarEn });

    it("dice cuánto esperar con Retry-After", () => {
        expect(esperaPruebaRegistro(e("demasiados_intentos", 42))).toBe("Espera 42 s.");
        expect(esperaPruebaRegistro(e("demasiados_rechazos", 540))).toContain("espera 9 min");
        expect(esperaPruebaRegistro(e("credencial_rechazada", 1700))).toContain("«Cambiar la contraseña»");
    });

    it("otro error no es una espera", () => {
        expect(esperaPruebaRegistro(e("no_encontrado", undefined, 404))).toBeNull();
        expect(esperaPruebaRegistro(new Error("red"))).toBeNull();
    });

    it("la contraseña rechazada es la guardada hasta que se escriba otra", () => {
        expect(credencialRechazada({ sipGuardadoEn: "2026-11-21T10:00:00Z", sipRechazadoEn: "2026-11-21T10:00:00Z" })).toBe(true);
        expect(credencialRechazada({ sipGuardadoEn: "2026-11-21T10:01:00Z", sipRechazadoEn: "2026-11-21T10:00:00Z" })).toBe(false);
        expect(credencialRechazada({ sipGuardadoEn: "2026-11-21T10:00:00Z", sipRechazadoEn: null })).toBe(false);
    });
});
