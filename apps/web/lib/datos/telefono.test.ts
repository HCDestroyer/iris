import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { documento, enlaceTel, principal, telefonos, type TipoTelefono } from "./telefono";

// Los mismos casos que la API (Go): los dos lados deben formatear igual.
const ruta = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../api/internal/integracion/argos/formato/testdata/telefonos.json");
const casos = JSON.parse(readFileSync(ruta, "utf8")) as {
    telefonos: { tipo: TipoTelefono; texto: string; esperado: { original: string; e164: string | null; mostrar: string }[] }[];
    documentos: { tipo: string; numero: string; mostrar: string }[];
};

describe("teléfonos (espejo de la API)", () => {
    for (const c of casos.telefonos) {
        it(JSON.stringify(c.texto), () => {
            expect(telefonos(c.tipo, c.texto)).toEqual(c.esperado.map((e) => ({ tipo: c.tipo, ...e })));
        });
    }
    it("principal: el móvil interpretable primero", () => {
        const lista = [...telefonos("telefono", "2233-4455"), ...telefonos("movil", "1801 / 5512-3344")];
        expect(principal(lista)?.e164).toBe("+50255123344");
        expect(principal(telefonos("telefono", "ext 12"))?.original).toBe("ext 12");
        expect(principal([])).toBeNull();
    });
    it("enlace tel:", () => {
        expect(enlaceTel({ e164: "+50255123344", original: "5512-3344" })).toBe("tel:+50255123344");
        expect(enlaceTel({ e164: null, original: "1801" })).toBe("tel:1801");
        expect(enlaceTel({ e164: null, original: "ext" })).toBeNull();
    });
});

describe("documento fiscal (espejo de la API)", () => {
    for (const c of casos.documentos) {
        it(`${c.tipo} ${c.numero}`, () => {
            expect(documento(c.tipo, c.numero)).toBe(c.mostrar);
        });
    }
});
