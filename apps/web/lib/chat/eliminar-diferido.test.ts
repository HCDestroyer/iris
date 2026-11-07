import { describe, expect, it } from "vitest";
import { EliminacionesDiferidas, sePuedeEliminar } from "./eliminar-diferido";

const AHORA = Date.parse("2026-11-06T16:20:00Z");
const msj = (id: string, enviadoEn = "2026-11-06T16:19:00Z") => ({ id, propio: true, eliminado: false, envio: "enviado", enviadoEn });

function reloj() {
    const tareas = new Map<number, () => void>();
    let siguiente = 1;
    return {
        programar: (fn: () => void) => {
            const id = siguiente++;
            tareas.set(id, fn);
            return id;
        },
        cancelar: (id: unknown) => tareas.delete(id as number),
        correr: () => {
            for (const [id, fn] of [...tareas]) {
                tareas.delete(id);
                fn();
            }
        },
        pendientes: () => tareas.size,
    };
}

describe("EliminacionesDiferidas", () => {
    it("espera 5 s y luego elimina; «Deshacer» cancela sin que nada salga", () => {
        const r = reloj();
        const hechos: string[] = [];
        const e = new EliminacionesDiferidas({ ejecutar: (id) => hechos.push(`eliminar ${id}`), alSalir: (id) => hechos.push(`salir ${id}`), alCambiar: () => undefined, alVencer: (id) => hechos.push(`vencido ${id}`), ahora: () => AHORA, ...r });
        e.pedir(msj("a"));
        e.pedir(msj("b"));
        expect([...e.pendientes]).toEqual(["a", "b"]);
        expect(e.deshacer("a")).toBe(true);
        expect(e.deshacer("a")).toBe(false);
        r.correr();
        expect(hechos).toEqual(["eliminar b"]);
        expect(e.pendientes.size).toBe(0);
    });

    it("al salir de la página manda lo pendiente con keepalive y no lo repite", () => {
        const r = reloj();
        const hechos: string[] = [];
        const e = new EliminacionesDiferidas({ ejecutar: (id) => hechos.push(`eliminar ${id}`), alSalir: (id) => hechos.push(`salir ${id}`), alCambiar: () => undefined, alVencer: (id) => hechos.push(`vencido ${id}`), ahora: () => AHORA, ...r });
        e.pedir(msj("a"));
        e.vaciarAlSalir();
        r.correr();
        expect(hechos).toEqual(["salir a"]);
        expect(r.pendientes()).toBe(0);
    });
});

describe("plazo al pedir y al salir", () => {
    it("no pide fuera de plazo y, si vence durante la espera, no lo manda", () => {
        const r = reloj();
        const hechos: string[] = [];
        let ahora = AHORA;
        const e = new EliminacionesDiferidas({ ejecutar: (id) => hechos.push(`eliminar ${id}`), alSalir: () => undefined, alCambiar: () => undefined, alVencer: (id) => hechos.push(`vencido ${id}`), ahora: () => ahora, ...r });
        expect(e.pedir(msj("viejo", "2026-11-06T16:00:00Z"))).toBe(false);
        expect(e.pedir(msj("justo", new Date(AHORA - 14 * 60_000 - 45_000).toISOString()))).toBe(true);
        ahora += 6_000;
        r.correr();
        expect(hechos).toEqual(["vencido justo"]);
    });
});

describe("sacar todo (cerrar sesión, ocultar la pestaña)", () => {
    it("cancela las esperas y devuelve lo pendiente sin ejecutarlo ni mandarlo con keepalive", () => {
        const r = reloj();
        const hechos: string[] = [];
        const e = new EliminacionesDiferidas({ ejecutar: (id) => hechos.push(`eliminar ${id}`), alSalir: (id) => hechos.push(`salir ${id}`), alCambiar: () => undefined, alVencer: (id) => hechos.push(`vencido ${id}`), ahora: () => AHORA, ...r });
        e.pedir(msj("a"));
        e.pedir(msj("b"));
        expect(e.sacarTodo()).toEqual(["a", "b"]);
        expect(e.sacarTodo()).toEqual([]);
        r.correr();
        expect(hechos).toEqual([]);
        expect(e.pendientes.size).toBe(0);
    });
});

describe("plazo para eliminar", () => {
    const ahora = Date.parse("2026-11-06T16:20:00Z");
    const m = { propio: true, eliminado: false, envio: "enviado", enviadoEn: "2026-11-06T16:10:00Z" };

    it("no ofrece eliminar si quedan menos de 10 s de los 15 min", () => {
        expect(sePuedeEliminar(m, ahora)).toBe(true);
        expect(sePuedeEliminar({ ...m, enviadoEn: new Date(ahora - 15 * 60_000 + 9_000).toISOString() }, ahora)).toBe(false);
        expect(sePuedeEliminar({ ...m, propio: false }, ahora)).toBe(false);
        expect(sePuedeEliminar({ ...m, envio: "enviando" }, ahora)).toBe(false);
    });
});
