import { ErrorTransporte } from "@iris/sync";
import { describe, expect, it } from "vitest";
import { EliminacionesDiferidas, sePuedeEliminar } from "./eliminar-diferido";
import { idTrasErrorChat } from "./enviar";

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
        const e = new EliminacionesDiferidas({ ejecutar: (id) => hechos.push(`eliminar ${id}`), alSalir: (id) => hechos.push(`salir ${id}`), alCambiar: () => undefined, ...r });
        e.pedir("a");
        e.pedir("b");
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
        const e = new EliminacionesDiferidas({ ejecutar: (id) => hechos.push(`eliminar ${id}`), alSalir: (id) => hechos.push(`salir ${id}`), alCambiar: () => undefined, ...r });
        e.pedir("a");
        e.vaciarAlSalir();
        r.correr();
        expect(hechos).toEqual(["salir a"]);
        expect(r.pendientes()).toBe(0);
    });
});

describe("plazo y reintentos", () => {
    const ahora = Date.parse("2026-11-06T16:20:00Z");
    const m = { propio: true, eliminado: false, envio: "enviado", enviadoEn: "2026-11-06T16:10:00Z" };

    it("no ofrece eliminar si quedan menos de 10 s de los 15 min", () => {
        expect(sePuedeEliminar(m, ahora)).toBe(true);
        expect(sePuedeEliminar({ ...m, enviadoEn: new Date(ahora - 15 * 60_000 + 9_000).toISOString() }, ahora)).toBe(false);
        expect(sePuedeEliminar({ ...m, propio: false }, ahora)).toBe(false);
        expect(sePuedeEliminar({ ...m, envio: "enviando" }, ahora)).toBe(false);
    });

    it("id nuevo solo tras un 4xx", () => {
        const nuevo = () => "nuevo";
        expect(idTrasErrorChat(new ErrorTransporte("red", "x"), "a", nuevo)).toBe("a");
        expect(idTrasErrorChat(new ErrorTransporte("http", "x", 503), "a", nuevo)).toBe("a");
        expect(idTrasErrorChat(new ErrorTransporte("http", "x", 422), "a", nuevo)).toBe("nuevo");
    });
});
