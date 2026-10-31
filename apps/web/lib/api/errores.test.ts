import { describe, expect, it } from "vitest";
import { leerRetryAfter } from "./errores";

describe("leerRetryAfter", () => {
    it("acepta segundos y fecha HTTP", () => {
        expect(leerRetryAfter(new Headers({ "Retry-After": "120" }))).toBe(120);
        const ahora = Date.parse("2026-10-28T10:00:00Z");
        expect(leerRetryAfter(new Headers({ "Retry-After": "Wed, 28 Oct 2026 10:05:00 GMT" }), ahora)).toBe(300);
        expect(leerRetryAfter(new Headers())).toBeUndefined();
        expect(leerRetryAfter(new Headers({ "Retry-After": "pronto" }))).toBeUndefined();
    });
});

describe("mensajeDeError con errors[]", () => {
    it("agrega los mensajes de errors[] al detalle", async () => {
        const { ErrorApi, mensajeDeError } = await import("./errores");
        const e = new ErrorApi({ estado: 422, codigo: "validacion", titulo: "Datos inválidos", detalle: "Revisa los campos", errores: [{ field: "body.version", message: "La versión es vieja" }] });
        expect(mensajeDeError(e)).toEqual({ titulo: "Datos inválidos", detalle: "Revisa los campos. La versión es vieja" });
        expect(mensajeDeError(new ErrorApi({ estado: 500, codigo: "x", titulo: "Error" }))).toEqual({ titulo: "Error", detalle: undefined });
    });
});
