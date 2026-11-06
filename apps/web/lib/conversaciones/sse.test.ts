import { describe, expect, it } from "vitest";
import { crearLectorSSE } from "./sse";

describe("lector SSE", () => {
    it("arma eventos con id, tipo y datos de varias líneas, en trozos cortados en cualquier parte", () => {
        const comentarios: string[] = [];
        const lector = crearLectorSSE((c) => comentarios.push(c));
        const texto = 'retry: 3000\n\nid: 7\nevent: mensaje_nuevo\ndata: {"a":\ndata: 1}\n\n: latido\n\nid: 8\ndata: x\r\n\r\n';
        const eventos = [];
        for (let i = 0; i < texto.length; i += 3) {
            eventos.push(...lector.agregar(texto.slice(i, i + 3)));
        }
        expect(eventos).toEqual([
            { id: "7", tipo: "mensaje_nuevo", datos: '{"a":\n1}' },
            { id: "8", tipo: "message", datos: "x" },
        ]);
        expect(comentarios).toEqual(["latido"]);
    });

    it("un CR suelto al final de un trozo espera al siguiente (CRLF partido)", () => {
        const lector = crearLectorSSE();
        expect(lector.agregar("data: a\r")).toEqual([]);
        expect(lector.agregar("\n\r")).toEqual([]);
        expect(lector.agregar("\n")).toEqual([{ id: undefined, tipo: "message", datos: "a" }]);
    });

    it("un bloque sin data no produce evento", () => {
        const lector = crearLectorSSE();
        expect(lector.agregar("id: 3\nevent: x\n\n")).toEqual([]);
    });
});
