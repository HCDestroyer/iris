import { describe, expect, it } from "vitest";
import { cuerpoCuandoNoHayAgentes, esquemaCuandoNoHayAgentes, MAX_OPCIONES_MENU, textoBotParaCliente } from "./menu-bot";
import { primerNombre, textoVistaPrevia, trozosVistaPrevia, valoresIniciales, variablesFaltantes } from "./nueva";

const PLANTILLA = {
    codigo: "confirmar_visita",
    nombre: "Confirmar visita técnica",
    categoria: "servicio" as const,
    variables: [
        { nombre: "1", formato: "texto" as const, obligatoria: true },
        { nombre: "2", formato: "texto" as const, obligatoria: true },
        { nombre: "3", formato: "texto" as const, obligatoria: false },
    ],
    vistaPrevia: "Hola {{1}}, queremos confirmar tu visita de {{2}}.",
};

describe("vista previa de la plantilla", () => {
    it("reemplaza las variables y deja a la vista las que faltan", () => {
        expect(textoVistaPrevia(PLANTILLA.vistaPrevia, { "1": "Carlos", "2": "hoy" })).toBe("Hola Carlos, queremos confirmar tu visita de hoy.");
        expect(textoVistaPrevia(PLANTILLA.vistaPrevia, { "1": "Carlos" })).toBe("Hola Carlos, queremos confirmar tu visita de {{2}}.");
        expect(trozosVistaPrevia("{{ 1 }}!", { "1": "<b>x</b>" })).toEqual([
            { tipo: "variable", nombre: "1", valor: "<b>x</b>" },
            { tipo: "texto", texto: "!" },
        ]);
    });

    it("llena la primera variable con el primer nombre del contacto", () => {
        expect(primerNombre("Carlos Ruiz")).toBe("Carlos");
        expect(primerNombre("+502 5544 0018")).toBe("");
        expect(valoresIniciales(PLANTILLA, "Carlos Ruiz")).toEqual({ "1": "Carlos", "2": "", "3": "" });
    });

    it("dice qué variables obligatorias faltan (las opcionales no)", () => {
        expect(variablesFaltantes(PLANTILLA, { "1": "Carlos", "2": "  ", "3": "" })).toEqual(["2"]);
    });
});

describe("menú del bot (cuando no hay agentes)", () => {
    const base = { activo: true, atiende: "bot" as const, saludo: "Gracias por escribir.", conMenu: true, minutosSinConfirmacion: "30", minutosSinDisponibles: "3" };
    const opcion = (i: number) => ({ texto: `Opción ${i}`, colaId: "2", etiquetaIds: [] });

    it("acepta hasta 9 opciones y rechaza la décima", () => {
        expect(esquemaCuandoNoHayAgentes.safeParse({ ...base, menu: Array.from({ length: MAX_OPCIONES_MENU }, (_, i) => opcion(i)) }).success).toBe(true);
        const r = esquemaCuandoNoHayAgentes.safeParse({ ...base, menu: Array.from({ length: 10 }, (_, i) => opcion(i)) });
        expect(r.success).toBe(false);
        expect(r.error?.issues[0]?.message).toBe("Hasta 9 opciones");
    });

    it("pide texto y cola en cada opción, y al menos una si el menú está prendido", () => {
        const r = esquemaCuandoNoHayAgentes.safeParse({ ...base, menu: [{ texto: " ", colaId: "", etiquetaIds: [] }] });
        expect(r.error?.issues.map((i) => i.path.join("."))).toEqual(["menu.0.texto", "menu.0.colaId"]);
        expect(esquemaCuandoNoHayAgentes.safeParse({ ...base, menu: [] }).error?.issues[0]?.message).toBe("Agrega al menos una opción o apaga el menú");
        expect(esquemaCuandoNoHayAgentes.safeParse({ ...base, conMenu: false, menu: [] }).success).toBe(true);
        // Apagado, lo que quedó escrito en el menú no se valida (no se manda).
        expect(esquemaCuandoNoHayAgentes.safeParse({ ...base, conMenu: false, menu: [{ texto: "", colaId: "", etiquetaIds: [] }] }).success).toBe(true);
    });

    it("no deja elegir el agente autónomo (fase 3) ni minutos fuera de rango", () => {
        expect(esquemaCuandoNoHayAgentes.safeParse({ ...base, conMenu: false, menu: [], atiende: "agente_autonomo" }).success).toBe(false);
        expect(esquemaCuandoNoHayAgentes.safeParse({ ...base, conMenu: false, menu: [], minutosSinDisponibles: "0" }).success).toBe(false);
        expect(esquemaCuandoNoHayAgentes.safeParse({ ...base, conMenu: false, menu: [], minutosSinConfirmacion: "2.5" }).success).toBe(false);
    });

    it("arma el cuerpo y el texto que ve el cliente", () => {
        const v = esquemaCuandoNoHayAgentes.parse({ ...base, menu: [opcion(1), { texto: "Pagos", colaId: "3", etiquetaIds: [5] }] });
        expect(cuerpoCuandoNoHayAgentes(v)).toEqual({
            activo: true,
            atiende: "bot",
            saludo: "Gracias por escribir.",
            menu: [
                { texto: "Opción 1", colaId: 2, etiquetaIds: [] },
                { texto: "Pagos", colaId: 3, etiquetaIds: [5] },
            ],
            minutosSinConfirmacion: 30,
            minutosSinDisponibles: 3,
        });
        expect(textoBotParaCliente("Hola.", [{ texto: "Soporte" }, { texto: "Pagos" }])).toBe("Hola.\n\n1. Soporte\n2. Pagos");
    });
});
