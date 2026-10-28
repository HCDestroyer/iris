import { describe, expect, it } from "vitest";
import { consultaDeFiltros, FILTROS_INICIALES, mismosFiltros, VISTAS_PREDEFINIDAS } from "./filtros";

describe("filtros de la bandeja", () => {
    it("activos = nuevo, abierto y en espera", () => {
        expect(consultaDeFiltros(FILTROS_INICIALES, 1)).toEqual({ estado: ["nuevo", "abierto", "en_espera"] });
    });
    it("asignados a mí, sin asignar, prioridad, categoría y texto", () => {
        expect(consultaDeFiltros({ estado: "todos", prioridad: "alta", categoria: 3, asignacion: "mios", q: " TK-1 " }, 9)).toEqual({
            prioridad: ["alta"],
            categoriaId: 3,
            asignadoA: 9,
            q: "TK-1",
        });
        expect(consultaDeFiltros({ ...FILTROS_INICIALES, estado: "cerrado", asignacion: "sin_asignar" }, 9)).toEqual({ estado: ["cerrado"], sinAsignar: true });
    });
    it("la vista activa se reconoce por sus filtros", () => {
        const mios = VISTAS_PREDEFINIDAS.find((v) => v.id === "mios");
        expect(mios && mismosFiltros(mios.filtros, { ...FILTROS_INICIALES, asignacion: "mios" })).toBe(true);
        expect(mismosFiltros(FILTROS_INICIALES, { ...FILTROS_INICIALES, q: "x" })).toBe(false);
    });
});
