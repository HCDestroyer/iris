import { describe, expect, it } from "vitest";
import { avisoEstado, avisoOrden, avisoProspecto, avisoTicket } from "./avisos";

const usuarios = [{ id: 7, nombre: "Luis Pérez" }];

describe("avisos tras una acción", () => {
    it("dicen qué pasó, con el número y a quién", () => {
        expect(avisoOrden("asignar", { numero: "OT-000042", tecnicoId: 7 }, usuarios)).toBe("Orden OT-000042 asignada a Luis Pérez");
        expect(avisoOrden("asignar", { numero: "OT-000042", tecnicoId: 99 }, usuarios)).toBe("Orden OT-000042 asignada");
        expect(avisoOrden("asignar", { numero: "OT-000042", tecnicoId: null }, usuarios)).toBe("Orden OT-000042 sin técnico");
        expect(avisoTicket("asignar", { numero: "TK-000101", asignadoA: 7 }, usuarios)).toBe("Ticket TK-000101 asignado a Luis Pérez");
        expect(avisoTicket("cerrar", { numero: "TK-000101", asignadoA: null })).toBe("Ticket TK-000101 cerrado");
        expect(avisoProspecto("ganar", "PR-000003")).toBe("Prospecto PR-000003 ganado");
        expect(avisoEstado("Mixco", false)).toBe("Se desactivó «Mixco»");
        expect(avisoEstado("Luis Pérez", true)).toBe("Se activó «Luis Pérez»");
    });
});
