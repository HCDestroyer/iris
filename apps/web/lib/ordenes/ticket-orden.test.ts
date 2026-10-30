import { describe, expect, it } from "vitest";
import {
    entradaCrearOrdenDeTicket,
    esquemaCrearOrdenDeTicket,
    igualacionesPosibles,
    MENSAJE_ASIGNACION_REQUERIDA,
    type EntradaFormCrearOrdenDeTicket,
} from "./ticket-orden";

const base: EntradaFormCrearOrdenDeTicket = { tipo: "visita", tecnicoId: "", cuadrillaId: "", fecha: "", hora: "", descripcion: "", direccion: "" };

const errores = (r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
    Object.fromEntries((r.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));

describe("crear orden desde el ticket", () => {
    it("no deja guardar sin técnico ni cuadrilla y lo dice en los dos campos", () => {
        const r = esquemaCrearOrdenDeTicket.safeParse(base);
        expect(r.success).toBe(false);
        expect(errores(r)).toEqual({ tecnicoId: MENSAJE_ASIGNACION_REQUERIDA, cuadrillaId: MENSAJE_ASIGNACION_REQUERIDA });
    });

    it("basta la cuadrilla (queda pendiente) o el técnico", () => {
        expect(esquemaCrearOrdenDeTicket.safeParse({ ...base, cuadrillaId: "4" }).success).toBe(true);
        expect(esquemaCrearOrdenDeTicket.safeParse({ ...base, tecnicoId: "12" }).success).toBe(true);
    });

    it("para programar exige técnico, fecha y hora válidas", () => {
        expect(errores(esquemaCrearOrdenDeTicket.safeParse({ ...base, cuadrillaId: "4", fecha: "2026-11-02", hora: "09:00" }))).toEqual({
            tecnicoId: "Para programar la visita elige el técnico",
        });
        expect(errores(esquemaCrearOrdenDeTicket.safeParse({ ...base, tecnicoId: "12", fecha: "2026-11-02" }))).toEqual({ hora: "Elige la hora (o borra la fecha)" });
        expect(errores(esquemaCrearOrdenDeTicket.safeParse({ ...base, tecnicoId: "12", hora: "09:00" }))).toEqual({ fecha: "Elige la fecha (o borra la hora)" });
        expect(errores(esquemaCrearOrdenDeTicket.safeParse({ ...base, tecnicoId: "12", fecha: "2026-02-30", hora: "09:00" }))).toEqual({ fecha: "Esa fecha no existe" });
    });

    it("arma el cuerpo sin claves vacías y con la hora de Guatemala en UTC", () => {
        const d = esquemaCrearOrdenDeTicket.parse({ ...base, tecnicoId: "12", cuadrillaId: "4", fecha: "2026-11-02", hora: "09:30", descripcion: "  Revisar ONT " });
        expect(entradaCrearOrdenDeTicket(d)).toEqual({ tipo: "visita", tecnicoId: 12, cuadrillaId: 4, inicio: "2026-11-02T15:30:00Z", descripcion: "Revisar ONT" });
        expect(entradaCrearOrdenDeTicket(esquemaCrearOrdenDeTicket.parse({ ...base, tipo: "retiro", cuadrillaId: "4" }))).toEqual({ tipo: "retiro", cuadrillaId: 4 });
    });
});

describe("igualar la asignación del ticket y su orden", () => {
    const ticket = { asignadoA: 12, cuadrillaId: 4, estado: "abierto" as const };
    const orden = { tecnicoId: null, cuadrillaId: null, estado: "pendiente" as const };
    const tecnicos = (id: number) => id === 12 || id === 13;

    it("iguales: nada que ofrecer", () => {
        expect(igualacionesPosibles(ticket, { ...orden, tecnicoId: 12, cuadrillaId: 4 }, tecnicos)).toEqual([]);
        expect(igualacionesPosibles({ ...ticket, asignadoA: null, cuadrillaId: null }, orden, tecnicos)).toEqual([]);
    });

    it("el ticket asignado y la orden no: la orden como el ticket", () => {
        expect(igualacionesPosibles(ticket, orden, tecnicos)).toEqual(["ticket"]);
    });

    it("la orden asignada y el ticket no: el ticket como la orden", () => {
        expect(igualacionesPosibles({ ...ticket, asignadoA: null, cuadrillaId: null }, { ...orden, tecnicoId: 13 }, tecnicos)).toEqual(["orden"]);
    });

    it("distintas: los dos sentidos", () => {
        expect(igualacionesPosibles(ticket, { ...orden, tecnicoId: 13, estado: "programada" }, tecnicos)).toEqual(["ticket", "orden"]);
    });

    it("no ofrece pasar a la orden un responsable que no es técnico (solo si la lista se conoce)", () => {
        const agente = { ...ticket, asignadoA: 99 };
        expect(igualacionesPosibles(agente, orden, tecnicos)).toEqual([]);
        expect(igualacionesPosibles(agente, orden, null)).toEqual(["ticket"]);
    });

    it("no ofrece cambiar un lado que ya no acepta asignación", () => {
        expect(igualacionesPosibles(ticket, { ...orden, estado: "cerrada" }, tecnicos)).toEqual([]);
        expect(igualacionesPosibles({ ...ticket, asignadoA: null, cuadrillaId: null }, { ...orden, tecnicoId: 13, estado: "cancelada" }, tecnicos)).toEqual([]);
        expect(igualacionesPosibles({ ...ticket, asignadoA: null, cuadrillaId: null }, { ...orden, tecnicoId: 13, estado: "en_sitio" }, tecnicos)).toEqual(["orden"]);
        expect(igualacionesPosibles(ticket, { ...orden, tecnicoId: 13, estado: "en_sitio" }, tecnicos)).toEqual(["orden"]);
        expect(igualacionesPosibles({ ...ticket, estado: "resuelto" }, { ...orden, tecnicoId: 13 }, tecnicos)).toEqual(["ticket"]);
    });
});
