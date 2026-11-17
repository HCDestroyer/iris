// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { esMarcaEnVuelo, idNavegador, idPestana, marcarLlamarEnVuelo, tomarLlamarEnVuelo } from "./navegador";

describe("id de pestaña y marca de «llamar en vuelo» (revisión de seguridad de E1b)", () => {
    beforeEach(() => window.localStorage.clear());

    it("el id de pestaña son 128 bits al azar, estable en la pestaña, distinto del de navegador y no se guarda", () => {
        const id = idPestana();
        expect(id).toMatch(/^[0-9a-f]{32}$/);
        expect(idPestana()).toBe(id);
        expect(id).not.toBe(idNavegador());
        expect(JSON.stringify({ ...window.localStorage }) + JSON.stringify({ ...window.sessionStorage })).not.toContain(id);
    });

    it("la marca la toma una sola vez quien la atiende, solo en su empresa y mientras siga vigente", () => {
        marcarLlamarEnVuelo({ usuarioId: 7, empresa: 1, conversacionId: 41, desde: 1_000 });
        expect(esMarcaEnVuelo("iris-llamar-en-vuelo")).toBe(true);
        // No lleva el id de la pestaña (es lo que ata el latido).
        expect(window.localStorage.getItem("iris-llamar-en-vuelo")).not.toContain(idPestana());
        // Otro usuario (otra sesión en este navegador) u otra empresa no la toca y la deja para quien sí.
        expect(tomarLlamarEnVuelo(8, 1, 100_000, 2_000)).toBeNull();
        expect(tomarLlamarEnVuelo(7, 2, 100_000, 2_000)).toBeNull();
        expect(tomarLlamarEnVuelo(7, 1, 100_000, 2_000)).toEqual({ usuarioId: 7, empresa: 1, conversacionId: 41, desde: 1_000 });
        expect(tomarLlamarEnVuelo(7, 1, 100_000, 2_000)).toBeNull();
        // Vencida (el servidor ya la cortó o nunca existió): se borra sin hacer nada.
        marcarLlamarEnVuelo({ usuarioId: 7, empresa: null, conversacionId: 41, desde: 1_000 });
        expect(tomarLlamarEnVuelo(7, undefined, 100_000, 200_000)).toBeNull();
        expect(window.localStorage.length).toBe(0);
        // Basura en la clave: se ignora y se limpia.
        window.localStorage.setItem("iris-llamar-en-vuelo", JSON.stringify({ x: 1 }));
        expect(tomarLlamarEnVuelo(7, 1, 100_000, 2_000)).toBeNull();
        expect(window.localStorage.length).toBe(0);
    });
});
