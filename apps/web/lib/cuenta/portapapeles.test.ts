import { describe, expect, it, vi } from "vitest";
import { BORRAR_PORTAPAPELES_MS, borradorPortapapeles, borrarSiSigue, type Portapapeles } from "./portapapeles";

function falso(inicial = "", opciones: { leer?: "ok" | "falla" | "no" } = {}) {
    const p = { contenido: inicial, escrituras: [] as string[] };
    const api: Portapapeles = {
        writeText: async (t: string) => {
            p.escrituras.push(t);
            p.contenido = t;
        },
        ...(opciones.leer === "no"
            ? {}
            : {
                  readText: async () => {
                      if (opciones.leer === "falla") throw new Error("NotAllowedError");
                      return p.contenido;
                  },
              }),
    };
    return { p, api };
}

describe("portapapeles sensible", () => {
    it("borra si aún contiene lo copiado", async () => {
        const { p, api } = falso("abcd-efgh");
        expect(await borrarSiSigue("abcd-efgh", api)).toBe(true);
        expect(p.contenido).toBe("");
    });

    it("no borra si el usuario ya copió otra cosa", async () => {
        const { p, api } = falso("otra cosa");
        expect(await borrarSiSigue("abcd-efgh", api)).toBe(false);
        expect(p.escrituras).toEqual([]);
    });

    it("si no se puede leer (sin permiso o sin readText), borra igual", async () => {
        for (const leer of ["falla", "no"] as const) {
            const { p, api } = falso("otra cosa", { leer });
            expect(await borrarSiSigue("abcd-efgh", api)).toBe(true);
            expect(p.contenido).toBe("");
        }
    });

    it("nunca lanza: sin portapapeles o si escribir falla", async () => {
        expect(await borrarSiSigue("x", undefined)).toBe(false);
        const roto: Portapapeles = {
            writeText: async () => {
                throw new Error("NotAllowedError");
            },
        };
        expect(await borrarSiSigue("x", roto)).toBe(false);
    });

    it("borra a los 45 s del último copiado y no antes", async () => {
        vi.useFakeTimers();
        try {
            const { p, api } = falso();
            const b = borradorPortapapeles(() => api);
            await api.writeText("primera");
            b.copiado("primera");
            await vi.advanceTimersByTimeAsync(BORRAR_PORTAPAPELES_MS - 1000);
            await api.writeText("segunda");
            b.copiado("segunda");
            await vi.advanceTimersByTimeAsync(BORRAR_PORTAPAPELES_MS - 1);
            expect(p.contenido).toBe("segunda");
            await vi.advanceTimersByTimeAsync(1);
            expect(p.contenido).toBe("");
            expect(BORRAR_PORTAPAPELES_MS).toBe(45_000);
        } finally {
            vi.useRealTimers();
        }
    });

    it("al cerrar la pantalla borra lo pendiente ya, y solo una vez", async () => {
        vi.useFakeTimers();
        try {
            const { p, api } = falso();
            const b = borradorPortapapeles(() => api);
            await api.writeText("secreto");
            b.copiado("secreto");
            expect(await b.cerrar()).toBe(true);
            expect(p.contenido).toBe("");
            await vi.advanceTimersByTimeAsync(BORRAR_PORTAPAPELES_MS);
            expect(p.escrituras).toEqual(["secreto", ""]);
            expect(await b.cerrar()).toBe(false);
        } finally {
            vi.useRealTimers();
        }
    });
});
