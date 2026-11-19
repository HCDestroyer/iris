import { describe, expect, it } from "vitest";
import { textoRafaga } from "./presentacion";

describe("ráfaga en el hilo (entrante rechazada sola, motivoFin limite)", () => {
    const ahora = Date.parse("2026-11-19T16:00:00Z");
    const hasta = "2026-11-19T16:07:00Z"; // 10:07 en Guatemala

    it("a quien configura: la hora a la que vuelve a sonar y los reintentos; vigente para ofrecer «Dejar que vuelva a llamar»", () => {
        expect(textoRafaga({ hasta, levantada: false, intentos: 0 }, ahora, true)).toEqual({
            detalle: "Demasiadas llamadas seguidas de este número; vuelve a sonar a las 10:07",
            vigente: true,
        });
        expect(textoRafaga({ hasta, levantada: false, intentos: 1 }, ahora, true).detalle).toBe("Demasiadas llamadas seguidas de este número; vuelve a sonar a las 10:07 · 1 intento más");
        expect(textoRafaga({ hasta, levantada: false, intentos: 4 }, ahora, true).detalle).toContain("· 4 intentos más");
    });

    it("ya levantada o ya vencida: lo dice y no ofrece nada", () => {
        expect(textoRafaga({ hasta, levantada: true, intentos: 2 }, ahora, true)).toEqual({
            detalle: "Iris la rechazó: demasiadas llamadas seguidas · 2 intentos más. Ya se dejó que vuelva a llamar",
            vigente: false,
        });
        expect(textoRafaga({ hasta, levantada: false, intentos: 0 }, Date.parse(hasta) + 1, true)).toEqual({
            detalle: "Iris la rechazó: demasiadas llamadas seguidas. Ya puede volver a llamar",
            vigente: false,
        });
    });

    it("quien no configura solo sabe que fue automático; sin datos de la ráfaga, el motivo sin hora", () => {
        // `vigente` no depende de quién mira: el botón lo decide `llamadas.desbloquear_numero`.
        expect(textoRafaga({ hasta, levantada: false, intentos: 3 }, ahora, false)).toEqual({ detalle: "Iris la rechazó sola: demasiadas llamadas seguidas", vigente: true });
        expect(textoRafaga({ hasta, levantada: true, intentos: 3 }, ahora, false).vigente).toBe(false);
        expect(textoRafaga(undefined, ahora, true)).toEqual({ detalle: "Iris la rechazó: demasiadas llamadas seguidas de este número", vigente: false });
    });
});
