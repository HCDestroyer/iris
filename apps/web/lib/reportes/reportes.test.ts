import { describe, expect, it } from "vitest";
import {
    atajoDe,
    diasDelRango,
    diasPara,
    errorDeRango,
    filtrosDesdeUrl,
    nombreCsv,
    periodoDe,
    queryDe,
    urlDe,
    type Filtros,
} from "./filtros";
import { etiquetaCategoria, ordenarPorPrioridad, textoMinutos, textoPeriodo, textoPorcentaje } from "./presentacion";

const HOY = "2026-10-29";
const base: Filtros = {
    desde: "2026-10-01",
    hasta: "2026-10-29",
    zonaId: null,
    cuadrillaId: null,
    tecnicoId: null,
    categoriaId: null,
    prioridades: [],
    dias: null,
    entidad: null,
};

describe("atajos de período (días de Guatemala)", () => {
    it("este mes, mes anterior y últimos 90 días", () => {
        expect(periodoDe("este_mes", HOY)).toEqual({ desde: "2026-10-01", hasta: "2026-10-29" });
        expect(periodoDe("mes_anterior", HOY)).toEqual({ desde: "2026-09-01", hasta: "2026-09-30" });
        expect(periodoDe("ultimos_90", HOY)).toEqual({ desde: "2026-08-01", hasta: "2026-10-29" });
        expect(diasDelRango("2026-08-01", "2026-10-29")).toBe(90);
    });

    it("mes anterior cruza el año y respeta febrero", () => {
        expect(periodoDe("mes_anterior", "2027-01-15")).toEqual({ desde: "2026-12-01", hasta: "2026-12-31" });
        expect(periodoDe("mes_anterior", "2028-03-02")).toEqual({ desde: "2028-02-01", hasta: "2028-02-29" });
    });

    it("reconoce el atajo del período, o ninguno", () => {
        expect(atajoDe({ desde: "2026-09-01", hasta: "2026-09-30" }, HOY)).toBe("mes_anterior");
        expect(atajoDe({ desde: "2026-09-02", hasta: "2026-09-30" }, HOY)).toBeUndefined();
    });
});

describe("validación del período", () => {
    it("pide las dos fechas, en orden y hasta 366 días", () => {
        expect(errorDeRango("", "2026-10-01")).toMatch(/dos fechas/);
        expect(errorDeRango("2026-10-02", "2026-10-01")).toMatch(/antes/);
        expect(errorDeRango("2025-10-29", "2026-10-29")).toBeUndefined(); // 366 días
        expect(errorDeRango("2025-10-28", "2026-10-29")).toMatch(/366/);
        expect(errorDeRango("2026-10-01", "2026-10-01")).toBeUndefined();
    });
});

describe("filtros en la URL", () => {
    it("ida y vuelta con todos los filtros", () => {
        const f: Filtros = { ...base, zonaId: 3, cuadrillaId: 4, tecnicoId: 31, categoriaId: 2, prioridades: ["media", "critica"], dias: 15, entidad: "orden" };
        const url = urlDe("reincidencias", f);
        expect(url).toBe("/reportes?reporte=reincidencias&desde=2026-10-01&hasta=2026-10-29&zona=3&cuadrilla=4&tecnico=31&categoria=2&prioridad=media%2Ccritica&dias=15&entidad=orden");
        const p = new URLSearchParams(url.split("?")[1]);
        expect(filtrosDesdeUrl(p, HOY)).toEqual(f);
    });

    it("sin parámetros o inválidos: este mes y sin filtros", () => {
        expect(filtrosDesdeUrl(new URLSearchParams(""), HOY)).toEqual(base);
        const raro = new URLSearchParams("desde=ayer&hasta=2026-10-10&zona=-1&tecnico=abc&prioridad=urgente,alta,alta&dias=500");
        expect(filtrosDesdeUrl(raro, HOY)).toEqual({ ...base, prioridades: ["alta"] });
    });
});

describe("query de cada reporte", () => {
    const f: Filtros = { ...base, zonaId: 3, tecnicoId: 31, categoriaId: 2, prioridades: ["alta"], dias: 5 };

    it("los de tickets llevan categoría y prioridad; los de campo no", () => {
        expect(queryDe("sla", f)).toEqual({ desde: "2026-10-01", hasta: "2026-10-29", zonaId: 3, tecnicoId: 31, categoriaId: 2, prioridad: ["alta"] });
        expect(queryDe("tecnicos", f)).toEqual({ desde: "2026-10-01", hasta: "2026-10-29", zonaId: 3, tecnicoId: 31 });
    });

    it("satisfacción lleva categoría y entidad, sin prioridad ni dias", () => {
        expect(queryDe("satisfaccion", { ...f, entidad: "ticket" })).toEqual({ desde: "2026-10-01", hasta: "2026-10-29", zonaId: 3, tecnicoId: 31, categoriaId: 2, entidad: "ticket" });
        expect(queryDe("sla", { ...f, entidad: "ticket" }).entidad).toBeUndefined();
    });

    it("dias solo en primera visita (7–90) y reincidencias (1–90), con el valor por omisión", () => {
        expect(queryDe("primera-visita", f).dias).toBe(7);
        expect(queryDe("reincidencias", f).dias).toBe(5);
        expect(queryDe("resolucion", f).dias).toBeUndefined();
        expect(diasPara("primera-visita", { dias: null })).toBe(30);
        expect(diasPara("reincidencias", { dias: 90 })).toBe(90);
    });

    it("nombre del CSV con el reporte y el período", () => {
        expect(nombreCsv("primera-visita", base)).toBe("reporte-primera-visita_2026-10-01_2026-10-29.csv");
    });
});

describe("presentación", () => {
    it("porcentaje null es «Sin datos», nunca 0 %", () => {
        expect(textoPorcentaje(null)).toBe("Sin datos");
        expect(textoPorcentaje(0)).toBe("0 %");
        expect(textoPorcentaje(87.5)).toBe("87.5 %");
        expect(textoPorcentaje(100)).toBe("100 %");
    });

    it("minutos legibles", () => {
        expect(textoMinutos(null)).toBe("Sin datos");
        expect(textoMinutos(45.4)).toBe("45 min");
        expect(textoMinutos(125)).toBe("2 h 5 min");
        expect(textoMinutos(120)).toBe("2 h");
        expect(textoMinutos(60 * 24 * 3 + 60 * 4 + 10)).toBe("3 d 4 h");
    });

    it("período corto y cruzando el año", () => {
        expect(textoPeriodo("2026-10-01", "2026-10-31")).toBe("1 oct – 31 oct 2026");
        expect(textoPeriodo("2025-12-15", "2026-01-14")).toBe("15 dic 2025 – 14 ene 2026");
    });

    it("ordena por prioridad de mayor a menor y nombra la categoría", () => {
        const filas = [{ prioridad: "baja" as const }, {}, { prioridad: "critica" as const }, { prioridad: "media" as const }];
        expect(ordenarPorPrioridad(filas).map((f) => f.prioridad)).toEqual(["critica", "media", "baja", undefined]);
        expect(etiquetaCategoria({ categoria: "Sin internet", categoriaId: 2 })).toBe("Sin internet");
        expect(etiquetaCategoria({})).toBe("Sin categoría");
    });
});
