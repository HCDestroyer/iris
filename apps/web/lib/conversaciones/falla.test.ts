import { describe, expect, it } from "vitest";
import { explicarFalla, textoCodigoFalla } from "./falla";
import { textoRechazadasApagadas } from "../voz/presentacion";
import { motivoFalla, textoEntrega } from "./presentacion";

const fallido = (motivo: string | null, codigoFalla?: { status: number | null; codigo: number | null; subcodigo: number | null }) => ({ estadoEntrega: "fallido" as const, motivoFalla: motivo, ...(codigoFalla ? { codigoFalla } : {}) });

describe("por qué no se envió un mensaje", () => {
    it("token rechazado: al agente, que avise; a quien administra, a dónde ir", () => {
        expect(explicarFalla(fallido("token_invalido"), false)).toEqual({ texto: "No se envió: WhatsApp rechazó la conexión de este número. Avisa a un administrador.", revisarNumero: false, codigo: null });
        expect(explicarFalla(fallido("token_invalido", { status: 401, codigo: 190, subcodigo: 463 }), true)).toEqual({
            texto: "No se envió: el token de WhatsApp de este número venció o no sirve. Revísalo en Números de WhatsApp.",
            revisarNumero: true,
            codigo: "Código de WhatsApp: 190 (HTTP 401), subcódigo 463",
        });
    });

    it("falta un permiso en Meta", () => {
        expect(explicarFalla(fallido("sin_permiso_meta"), false)?.texto).toBe("No se envió: a la conexión de este número le falta un permiso en WhatsApp. Avisa a un administrador.");
        expect(explicarFalla(fallido("sin_permiso_meta", { status: 403, codigo: 200, subcodigo: null }), true)).toMatchObject({ revisarNumero: true, codigo: "Código de WhatsApp: 200 (HTTP 403)" });
    });

    it("rechazado por WhatsApp por otra causa: no manda a revisar el número; el código, solo si vino", () => {
        expect(explicarFalla(fallido("rechazado_por_canal"), true)).toEqual({ texto: "WhatsApp no aceptó el mensaje.", revisarNumero: false, codigo: null });
        // Iris no la mandó: la plantilla lleva datos del cliente y el cliente no está confirmado (no es cosa de WhatsApp).
        expect(explicarFalla(fallido("cliente_sin_confirmar"), false)?.texto).toMatch(/cliente de esta conversación no está confirmado/);
        expect(explicarFalla(fallido("rechazado_por_canal", { status: 403, codigo: 131031, subcodigo: null }), true)?.codigo).toBe("Código de WhatsApp: 131031 (HTTP 403)");
    });

    it("los demás motivos se quedan con el pie de la burbuja (salvo que venga un código)", () => {
        for (const motivo of ["numero_invalido", "fuera_de_ventana", "agotado", null]) expect(explicarFalla(fallido(motivo), true)).toBeNull();
        expect(explicarFalla(fallido("agotado", { status: 500, codigo: null, subcodigo: null }), true)).toEqual({ texto: "", revisarNumero: false, codigo: "Código de WhatsApp: HTTP 500" });
        // Un mensaje que no falló no explica nada aunque traiga motivo.
        expect(explicarFalla({ estadoEntrega: "enviado", motivoFalla: "token_invalido" }, true)).toBeNull();
    });

    it("el código: cada parte puede faltar (el status falta si el rechazo llegó después, por webhook)", () => {
        expect(textoCodigoFalla({ status: null, codigo: 190, subcodigo: null })).toBe("Código de WhatsApp: 190");
        expect(textoCodigoFalla({ status: null, codigo: 190, subcodigo: 460 })).toBe("Código de WhatsApp: 190, subcódigo 460");
        expect(textoCodigoFalla({ status: null, codigo: null, subcodigo: 460 })).toBe("Código de WhatsApp: subcódigo 460");
        expect(textoCodigoFalla({ status: null, codigo: null, subcodigo: null })).toBeNull();
        expect(textoCodigoFalla(undefined)).toBeNull();
    });

    it("el pie de la burbuja ya no dice «token invalido» ni «sin permiso meta»", () => {
        expect(motivoFalla("token_invalido")).toBe("WhatsApp rechazó la conexión del número");
        expect(motivoFalla("sin_permiso_meta")).toBe("falta un permiso en WhatsApp");
    });

    it("el pie no repite lo que ya se explica debajo; los demás motivos siguen en el pie", () => {
        for (const motivo of ["token_invalido", "sin_permiso_meta", "rechazado_por_canal"]) expect(textoEntrega({ estadoEntrega: "fallido", motivoFalla: motivo })).toBe("no se envió");
        expect(textoEntrega({ estadoEntrega: "fallido", motivoFalla: "numero_invalido" })).toBe("no se envió (número inválido)");
        expect(textoEntrega({ estadoEntrega: "fallido", motivoFalla: null })).toBe("no se envió");
    });

    it("el código nunca se pinta a quien no administra los números, aunque llegara", () => {
        const c = { status: 401, codigo: 190, subcodigo: 463 };
        expect(explicarFalla(fallido("token_invalido", c), false)?.codigo).toBeNull();
        expect(explicarFalla(fallido("rechazado_por_canal", c), false)).toEqual({ texto: "WhatsApp no aceptó el mensaje.", revisarNumero: false, codigo: null });
        // Otro motivo, sin permiso: no hay nada que añadir.
        expect(explicarFalla(fallido("agotado", c), false)).toBeNull();
    });
});

describe("llamadas rechazadas con las llamadas apagadas", () => {
    const AHORA = Date.parse("2026-11-18T16:00:00Z");
    const base = "Llegaron llamadas a este número y se rechazaron porque las llamadas están apagadas en Iris.";

    it("la última y cuántas van hoy", () => {
        expect(textoRechazadasApagadas({ ultimaEn: "2026-11-18T15:48:00Z", hoy: 3 }, AHORA)).toBe(`${base} La última, hace 12 min; hoy van 3.`);
        expect(textoRechazadasApagadas({ ultimaEn: "2026-11-18T15:48:00Z", hoy: 1 }, AHORA)).toBe(`${base} La última, hace 12 min; hoy va 1.`);
    });

    it("si hoy no llegó ninguna, solo cuándo fue la última", () => {
        expect(textoRechazadasApagadas({ ultimaEn: "2026-11-18T13:00:00Z", hoy: 0 }, AHORA)).toBe(`${base} La última, hace 3 h.`);
    });

    it("una fecha ilegible no rompe la frase", () => {
        expect(textoRechazadasApagadas({ ultimaEn: "no-es-fecha", hoy: 2 }, AHORA)).toBe(`${base} La última, hace poco; hoy van 2.`);
    });
});
