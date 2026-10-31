import type { OrdenVista } from "@iris/sync";
import { estadoPasos, faltantesAntesDeFirmar, progresoPasos, siguientePaso } from "@iris/sync/campo";
import { describe, expect, it } from "vitest";

type Item = NonNullable<OrdenVista["checklist"]>[number];

const item = (codigo: string, obligatorio: boolean, valor: boolean | null = null): Item =>
    ({ itemId: codigo.length, codigo, etiqueta: codigo.toUpperCase(), tipo: "si_no", obligatorio, unidad: "", posicion: 1, valor, marcadoPor: null, ocurridoEn: null }) as Item;

function orden(cambios: Partial<OrdenVista> = {}): OrdenVista {
    return {
        estado: "programada",
        tipo: "instalacion",
        enSitioEn: null,
        checklist: [item("onu", true), item("limpieza", false)],
        formularios: [],
        equipos: [],
        materiales: [],
        adjuntos: [],
        firma: null,
        requiereFirma: true,
        ...cambios,
    } as OrdenVista;
}

describe("la orden paso a paso", () => {
    it("antes de llegar falta todo y el siguiente paso es la llegada", () => {
        const o = orden();
        expect(estadoPasos(o).map((p) => [p.paso, p.completo])).toEqual([
            ["llegada", false],
            ["checklist", false],
            ["materiales", false],
            ["fotos", true],
            ["cierre", false],
        ]);
        expect(siguientePaso(o)).toBe("llegada");
        expect(progresoPasos(o)).toEqual({ listos: 1, total: 4 });
        expect(faltantesAntesDeFirmar(o)).toEqual(["Marca tu llegada al sitio.", "Falta «ONU» del checklist.", "Registra el equipo instalado."]);
    });

    it("en sitio con el checklist y el equipo: la firma y el cierre quedan listos (la firma no cuenta antes)", () => {
        const o = orden({
            estado: "en_sitio",
            enSitioEn: "2026-10-31T15:00:00Z",
            checklist: [item("onu", true, true)],
            equipos: [{ accion: "instalar" } as NonNullable<OrdenVista["equipos"]>[number]],
        });
        expect(faltantesAntesDeFirmar(o)).toEqual([]);
        expect(siguientePaso(o)).toBe("cierre");
        expect(progresoPasos(o)).toEqual({ listos: 4, total: 4 });
    });

    it("pausada después de llegar cuenta como llegada; una visita no exige equipos", () => {
        const o = orden({ estado: "pausada", enSitioEn: "2026-10-31T15:00:00Z", tipo: "visita", checklist: [] });
        const pasos = estadoPasos(o);
        expect(pasos.find((p) => p.paso === "llegada")?.completo).toBe(true);
        expect(pasos.find((p) => p.paso === "materiales")).toMatchObject({ completo: true, opcional: true });
        expect(pasos.find((p) => p.paso === "checklist")).toMatchObject({ completo: true, opcional: true });
        expect(siguientePaso(o)).toBe("cierre");
    });
});
