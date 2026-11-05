import { describe, expect, it } from "vitest";
import { ErrorApi } from "../api/errores";
import { FILTROS_INICIALES, vistaDeUrl, VISTAS_PREDEFINIDAS } from "./filtros";
import { enLote, textoLoteAsignado } from "./lote";
import { contadorDe, slaCumplido, textoContador, type DiaTickets } from "./contadores";
import type { Ticket } from "./sla";

const dia = (resueltosConSla: number, resueltosEnSla: number): DiaTickets => ({ dia: "2026-11-05", creados: 0, resueltos: resueltosConSla, resueltosConSla, resueltosEnSla, vencidos: 0 });

describe("resumen de la bandeja", () => {
    it("% de SLA cumplido de varios días; sin base, null (nunca 0 % ni 100 % inventados)", () => {
        expect(slaCumplido([dia(4, 3), dia(6, 6)])).toBe(90);
        expect(slaCumplido([dia(0, 0)])).toBeNull();
    });

    it("contador solo con el resumen leído; «99+» desde 100", () => {
        expect(contadorDe(null, "activos")).toBeUndefined();
        expect(contadorDe({ estado: "cargando" } as never, "activos")).toBeUndefined();
        expect(contadorDe({ estado: "listo", datos: { activos: 7 } } as never, "activos")).toBe(7);
        expect(textoContador(99)).toBe("99");
        expect(textoContador(100)).toBe("99+");
    });

    it("cada pestaña con número dice qué contador usa; «Todos» no lleva", () => {
        expect(VISTAS_PREDEFINIDAS.map((v) => [v.id, v.contador])).toEqual([
            ["activos", "activos"],
            ["mios", "mios"],
            ["sin_asignar", "sinAsignar"],
            ["en_espera", "enEspera"],
            ["todos", undefined],
        ]);
    });

    it("?vista= abre una vista predefinida o guardada; otra cosa, ninguna", () => {
        expect(vistaDeUrl("sin_asignar", [])?.filtros.asignacion).toBe("sin_asignar");
        const propia = { id: "v-1", nombre: "Altas", filtros: { ...FILTROS_INICIALES, prioridad: "alta" as const }, propia: true };
        expect(vistaDeUrl("v-1", [propia])?.nombre).toBe("Altas");
        expect(vistaDeUrl("<script>", [propia])).toBeUndefined();
        expect(vistaDeUrl(null, [])).toBeUndefined();
    });
});

describe("acciones en lote", () => {
    const t = (id: number) => ({ id, numero: `TK-${id}`, version: 1 }) as Ticket;

    it("una por ticket; un fallo no detiene a los demás y dice por qué", async () => {
        const vistos: number[] = [];
        const r = await enLote([t(1), t(2), t(3)], async (x) => {
            vistos.push(x.id);
            if (x.id === 2) throw new ErrorApi({ estado: 409, codigo: "version", titulo: "Conflicto", detalle: "La versión cambió" });
            return { ...x, version: 2 };
        });
        expect(vistos).toEqual([1, 2, 3]);
        expect(r.hechos.map((x) => x.id)).toEqual([1, 3]);
        expect(r.fallidos.map((f) => f.ticket.id)).toEqual([2]);
    });

    it("texto del aviso", () => {
        expect(textoLoteAsignado(1, "Ana")).toBe("1 ticket asignado a Ana");
        expect(textoLoteAsignado(3, "Ana")).toBe("3 tickets asignados a Ana");
        expect(textoLoteAsignado(2, null)).toBe("2 tickets sin asignar");
    });
});
