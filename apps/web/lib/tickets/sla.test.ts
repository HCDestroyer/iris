import { describe, expect, it } from "vitest";
import { describirSla, estadoEfectivo, formatearDuracion, relojPrincipal, type RelojSLA, type Ticket } from "./sla";

const T0 = Date.parse("2026-10-28T15:00:00Z");
const MIN = 60_000;
const reloj = (p: Partial<RelojSLA>): RelojSLA => ({ estado: "en_tiempo", objetivoMin: 240, riesgoEn: null, venceEn: null, cumplidoEn: null, ...p });

describe("formatearDuracion", () => {
    it("minutos, horas y días legibles", () => {
        expect(formatearDuracion(30_000)).toBe("menos de 1 min");
        expect(formatearDuracion(45 * MIN)).toBe("45 min");
        expect(formatearDuracion(60 * MIN)).toBe("1 h");
        expect(formatearDuracion(135 * MIN)).toBe("2 h 15 min");
        expect(formatearDuracion(-135 * MIN)).toBe("2 h 15 min");
        expect(formatearDuracion((3 * 24 + 4) * 60 * MIN)).toBe("3 d 4 h");
        expect(formatearDuracion(48 * 60 * MIN)).toBe("2 d");
    });
});

describe("estadoEfectivo", () => {
    const r = reloj({ riesgoEn: new Date(T0 + 10 * MIN).toISOString(), venceEn: new Date(T0 + 30 * MIN).toISOString() });
    it("respeta el estado de la API mientras no cruza los umbrales", () => {
        expect(estadoEfectivo(r, T0)).toBe("en_tiempo");
    });
    it("pasa a en riesgo y a vencido con la hora local", () => {
        expect(estadoEfectivo(r, T0 + 10 * MIN)).toBe("en_riesgo");
        expect(estadoEfectivo(r, T0 + 30 * MIN)).toBe("vencido");
        expect(estadoEfectivo({ ...r, estado: "en_riesgo" }, T0 + 31 * MIN)).toBe("vencido");
    });
    it("pausado, cumplido y sin SLA no cambian", () => {
        expect(estadoEfectivo({ ...r, estado: "pausado", venceEn: null }, T0 + 99 * MIN)).toBe("pausado");
        expect(estadoEfectivo({ ...r, estado: "cumplido" }, T0 + 99 * MIN)).toBe("cumplido");
        expect(estadoEfectivo(reloj({ estado: "sin_sla", objetivoMin: null }), T0)).toBe("sin_sla");
    });
});

describe("describirSla: icono + texto + color", () => {
    it("en riesgo: ámbar, texto y tiempo restante con fecha en Guatemala", () => {
        const d = describirSla(reloj({ estado: "en_riesgo", venceEn: new Date(T0 + 20 * MIN).toISOString() }), T0);
        expect(d).toMatchObject({ estado: "en_riesgo", tono: "warning", texto: "En riesgo", detalle: "vence en 20 min", fecha: "28/10/2026 09:20" });
        expect(d.icono).toBeTypeOf("object");
    });
    it("vencido: rojo y hace cuánto", () => {
        const d = describirSla(reloj({ estado: "vencido", venceEn: new Date(T0 - 45 * MIN).toISOString() }), T0);
        expect(d).toMatchObject({ tono: "error", texto: "Vencido", detalle: "venció hace 45 min" });
    });
    it("en tiempo, pausado y cumplido", () => {
        expect(describirSla(reloj({ venceEn: new Date(T0 + 135 * MIN).toISOString() }), T0)).toMatchObject({ tono: "success", texto: "En tiempo", detalle: "vence en 2 h 15 min" });
        expect(describirSla(reloj({ estado: "pausado" }), T0)).toMatchObject({ tono: "neutral", texto: "Pausado", detalle: "en espera, sin contar" });
        expect(describirSla(reloj({ estado: "cumplido", cumplidoEn: "2026-10-28T14:00:00Z" }), T0)).toMatchObject({
            tono: "success",
            texto: "Cumplido",
            fecha: "28/10/2026 08:00",
        });
    });
});

describe("relojPrincipal", () => {
    const base = { estado: "abierto", slaRespuesta: reloj({ estado: "en_riesgo" }), slaResolucion: reloj({}) } as Ticket;
    it("la primera respuesta manda mientras está pendiente", () => {
        expect(relojPrincipal(base).tipo).toBe("respuesta");
    });
    it("con la respuesta cumplida, o el ticket resuelto, manda la resolución", () => {
        expect(relojPrincipal({ ...base, slaRespuesta: reloj({ estado: "cumplido" }) }).tipo).toBe("resolucion");
        expect(relojPrincipal({ ...base, estado: "resuelto" }).tipo).toBe("resolucion");
    });
});
