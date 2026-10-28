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
