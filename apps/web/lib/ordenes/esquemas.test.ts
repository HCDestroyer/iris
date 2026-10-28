import { describe, expect, it } from "vitest";
import {
    entradaCrearOrden,
    entradaProgramarOrden,
    esquemaAsignarOrden,
    esquemaCrearOrden,
    esquemaMotivoOrden,
    esquemaProgramarOrden,
    guatemalaDesdeIso,
    inicioDesdeGuatemala,
    type EntradaFormCrearOrden,
} from "./esquemas";

const base: EntradaFormCrearOrden = {
    tipo: "instalacion",
    plantillaId: "",
    descripcion: "",
    direccion: "",
    zonaId: "",
    lat: "",
    lon: "",
    contacto: { nombre: "", telefono: "" },
};

const errores = (r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
    Object.fromEntries((r.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));

describe("crear orden", () => {
    it("sin ticket ni cliente exige la dirección", () => {
        const r = esquemaCrearOrden.safeParse(base);
        expect(errores(r).direccion).toMatch(/Escribe la dirección/);
    });

    it("desde un ticket o con cliente la dirección es opcional", () => {
        expect(esquemaCrearOrden.safeParse({ ...base, ticketId: 7 }).success).toBe(true);
        expect(esquemaCrearOrden.safeParse({ ...base, clienteId: 501 }).success).toBe(true);
    });

    it("arma el cuerpo sin claves vacías", () => {
        const r = esquemaCrearOrden.parse({
            ...base,
            tipo: "visita",
            ticketId: 7,
            plantillaId: "3",
            descripcion: "  Revisar la ONT ",
            contacto: { nombre: "María", telefono: "" },
        });
        expect(entradaCrearOrden(r)).toEqual({ tipo: "visita", ticketId: 7, plantillaId: 3, descripcion: "Revisar la ONT", contacto: { nombre: "María" } });
    });

    it("coordenadas: van juntas y en rango; zona numérica", () => {
        expect(errores(esquemaCrearOrden.safeParse({ ...base, direccion: "x", lat: "14.6" })).lon).toMatch(/latitud y la longitud/);
        expect(errores(esquemaCrearOrden.safeParse({ ...base, direccion: "x", lat: "91", lon: "0" })).lat).toMatch(/latitud va de -90 a 90/);
        expect(errores(esquemaCrearOrden.safeParse({ ...base, direccion: "x", zonaId: "a1" })).zonaId).toBe("Escribe un número");
        const ok = esquemaCrearOrden.parse({ ...base, direccion: "4a calle", lat: "14.634915", lon: "-90.506882", zonaId: "4" });
        expect(entradaCrearOrden(ok)).toEqual({ tipo: "instalacion", direccion: "4a calle", zonaId: 4, lat: 14.634915, lon: -90.506882 });
    });

    it("respeta los máximos del contrato", () => {
        expect(errores(esquemaCrearOrden.safeParse({ ...base, direccion: "x".repeat(301) })).direccion).toMatch(/hasta 300/);
        expect(errores(esquemaCrearOrden.safeParse({ ...base, direccion: "x", descripcion: "x".repeat(4001) })).descripcion).toMatch(/hasta 4,000|hasta 4.000|hasta 4000/);
        expect(errores(esquemaCrearOrden.safeParse({ ...base, direccion: "x", contacto: { nombre: "", telefono: "1".repeat(41) } }))["contacto.telefono"]).toMatch(/hasta 40/);
    });
});

describe("programar y reprogramar", () => {
    it("convierte la hora de Guatemala (UTC−6) a UTC", () => {
        expect(inicioDesdeGuatemala("2026-10-28", "14:30")).toBe("2026-10-28T20:30:00Z");
        expect(inicioDesdeGuatemala("2026-10-28", "19:15")).toBe("2026-10-29T01:15:00Z");
        expect(inicioDesdeGuatemala("2026-02-30", "10:00")).toBeUndefined();
        expect(guatemalaDesdeIso("2026-10-29T01:15:00Z")).toEqual({ fecha: "2026-10-28", hora: "19:15" });
        expect(guatemalaDesdeIso(null)).toEqual({ fecha: "", hora: "" });
    });

    it("exige técnico si la orden no tiene", () => {
        const e = esquemaProgramarOrden({ requiereTecnico: true, reprogramar: false });
        expect(errores(e.safeParse({ tecnicoId: "", fecha: "2026-10-28", hora: "09:00", motivo: "" })).tecnicoId).toBe("Elige el técnico");
        const d = e.parse({ tecnicoId: "3", fecha: "2026-10-28", hora: "09:00", motivo: "" });
        expect(entradaProgramarOrden(d, 4)).toEqual({ version: 4, inicio: "2026-10-28T15:00:00Z", tecnicoId: 3 });
    });

    it("reprogramar exige motivo; fecha y hora válidas", () => {
        const e = esquemaProgramarOrden({ requiereTecnico: false, reprogramar: true });
        expect(errores(e.safeParse({ tecnicoId: "", fecha: "2026-10-28", hora: "09:00", motivo: "" })).motivo).toBe("Escribe el motivo");
        expect(errores(e.safeParse({ tecnicoId: "", fecha: "2026-10-28", hora: "25:00", motivo: "Lluvia" })).hora).toMatch(/14:30/);
        expect(errores(e.safeParse({ tecnicoId: "", fecha: "2026-02-30", hora: "09:00", motivo: "Lluvia" })).fecha).toBe("Esa fecha no existe");
        const d = e.parse({ tecnicoId: "", fecha: "2026-10-30", hora: "08:00", motivo: "Lluvia" });
        expect(entradaProgramarOrden(d, 2)).toEqual({ version: 2, inicio: "2026-10-30T14:00:00Z", motivo: "Lluvia" });
    });
});

describe("asignar, cancelar y reabrir", () => {
    it("asignar: vacío = sin técnico", () => {
        expect(esquemaAsignarOrden.parse({ tecnicoId: "" })).toEqual({ tecnicoId: undefined });
        expect(esquemaAsignarOrden.parse({ tecnicoId: "3" })).toEqual({ tecnicoId: 3 });
    });

    it("motivo de 3 a 500 caracteres (EntradaMotivo)", () => {
        expect(errores(esquemaMotivoOrden.safeParse({ motivo: " " })).motivo).toBe("Escribe el motivo");
        expect(errores(esquemaMotivoOrden.safeParse({ motivo: "no" })).motivo).toMatch(/al menos 3/);
        expect(errores(esquemaMotivoOrden.safeParse({ motivo: "x".repeat(501) })).motivo).toMatch(/hasta 500/);
        expect(esquemaMotivoOrden.parse({ motivo: " Cliente canceló " })).toEqual({ motivo: "Cliente canceló" });
    });
});
