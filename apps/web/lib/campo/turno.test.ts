import { describe, expect, it } from "vitest";
import { escribirTurnoLocal, leerTurnoLocal, proximaFrontera, siguienteTramo, tramoEn, tramosDeHoy } from "./turno";

const T = (h: string) => `2026-10-28T${h}:00Z`;
const ms = (h: string) => Date.parse(T(h));

describe("turno de la agenda", () => {
    it("toma los tramos del día del paquete, en orden; sin tramos usa inicio y fin", () => {
        const turno = {
            fecha: "2026-10-28",
            inicio: T("14:00"),
            fin: T("23:00"),
            tramos: [
                { inicio: T("19:00"), fin: T("23:00") },
                { inicio: T("14:00"), fin: T("18:00") },
            ],
        };
        expect(tramosDeHoy(turno, "2026-10-28")).toEqual([
            { inicio: ms("14:00"), fin: ms("18:00") },
            { inicio: ms("19:00"), fin: ms("23:00") },
        ]);
        expect(tramosDeHoy({ ...turno, tramos: null }, "2026-10-28")).toEqual([{ inicio: ms("14:00"), fin: ms("23:00") }]);
        // Otro día, sin turno o sin tramos válidos: sin agenda.
        expect(tramosDeHoy(turno, "2026-10-29")).toBeNull();
        expect(tramosDeHoy(null, "2026-10-28")).toBeNull();
        expect(tramosDeHoy({ ...turno, tramos: [] }, "2026-10-28")).toBeNull();
        expect(tramosDeHoy({ ...turno, tramos: [{ inicio: T("18:00"), fin: T("14:00") }] }, "2026-10-28")).toBeNull();
    });

    it("tramo en curso, siguiente y próxima frontera ([inicio, fin))", () => {
        const tramos = [
            { inicio: ms("14:00"), fin: ms("18:00") },
            { inicio: ms("19:00"), fin: ms("23:00") },
        ];
        expect(tramoEn(tramos, ms("14:00"))).toEqual(tramos[0]);
        expect(tramoEn(tramos, ms("18:00"))).toBeNull();
        expect(siguienteTramo(tramos, ms("18:00"))).toEqual(tramos[1]);
        expect(siguienteTramo(tramos, ms("19:00"))).toBeNull();
        expect(proximaFrontera(tramos, ms("13:00"))).toBe(ms("14:00"));
        expect(proximaFrontera(tramos, ms("14:00"))).toBe(ms("18:00"));
        expect(proximaFrontera(tramos, ms("23:00"))).toBeNull();
    });

    it("lee el turno local guardado, también el formato anterior (solo la fecha)", () => {
        const t = { fecha: "2026-10-28", enTurno: false, trabajo: false, hastaTramo: null, apagadoHasta: ms("18:00") };
        expect(leerTurnoLocal(escribirTurnoLocal(t))).toEqual(t);
        expect(leerTurnoLocal("2026-10-28")).toEqual({ fecha: "2026-10-28", enTurno: true, trabajo: true, hastaTramo: null, apagadoHasta: null });
        expect(leerTurnoLocal(undefined)).toBeNull();
        expect(leerTurnoLocal("{roto")).toBeNull();
        expect(leerTurnoLocal(JSON.stringify({ ...t, enTurno: "si" }))).toBeNull();
    });
});
