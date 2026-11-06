import { describe, expect, it } from "vitest";
import { resumirFranjas, textoDias, validarFranjas } from "./franjas";

describe("franjas de un horario", () => {
    it("días en corto", () => {
        expect(textoDias([1, 2, 3, 4, 5])).toBe("Lun–Vie");
        expect(textoDias([6])).toBe("Sáb");
        expect(textoDias([1, 3, 5])).toBe("Lun, Mié y Vie");
        expect(textoDias([7, 6, 5, 4, 3, 2, 1])).toBe("Todos los días");
        expect(textoDias([6, 7])).toBe("Sáb y Dom");
    });

    it("resumen para la lista de números", () => {
        expect(
            resumirFranjas([
                { dias: [1, 2, 3, 4, 5], inicio: "08:00", fin: "17:00" },
                { dias: [6], inicio: "08:00", fin: "12:00" },
            ]),
        ).toBe("Lun–Vie 8:00–17:00 · Sáb 8:00–12:00");
        expect(resumirFranjas([])).toBe("Sin franjas: siempre cerrado");
    });

    it("valida horas, inicio antes del fin y choques el mismo día", () => {
        expect(validarFranjas([{ dias: [1], inicio: "06:00", fin: "22:00" }])).toEqual([]);
        expect(validarFranjas([{ dias: [], inicio: "25:00", fin: "06:00" }]).map((e) => e.campo)).toEqual(["dias", "inicio"]);
        expect(validarFranjas([{ dias: [1], inicio: "22:00", fin: "06:00" }])[0]?.mensaje).toMatch(/cruza la medianoche/);
        const choque = validarFranjas([
            { dias: [1, 2], inicio: "08:00", fin: "12:00" },
            { dias: [2], inicio: "11:00", fin: "13:00" },
        ]);
        expect(choque).toEqual([{ indice: 1, campo: "inicio", mensaje: "Se cruza con la franja 1 el mismo día" }]);
    });
});
