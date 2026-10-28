import { describe, expect, it } from "vitest";
import { BYTES_OBJETIVO, codificarConTope, dimensionesObjetivo } from "./imagen";

describe("compresión de fotos", () => {
    it("reduce el lado mayor a 1600 px conservando la proporción y no agranda", () => {
        expect(dimensionesObjetivo(4000, 3000)).toEqual({ ancho: 1600, alto: 1200 });
        expect(dimensionesObjetivo(3000, 4000)).toEqual({ ancho: 1200, alto: 1600 });
        expect(dimensionesObjetivo(800, 600)).toEqual({ ancho: 800, alto: 600 });
        expect(dimensionesObjetivo(4000, 3000, 1280)).toEqual({ ancho: 1280, alto: 960 });
    });

    it("baja la calidad hasta quedar en ~300 KB", async () => {
        const pedidos: [number, number][] = [];
        // Tamaño simulado: proporcional al área y a la calidad.
        const codificar = (lado: number, calidad: number) => {
            pedidos.push([lado, calidad]);
            return Promise.resolve(new Blob([new Uint8Array(Math.round((lado * lado * calidad) / 4))]));
        };
        const blob = await codificarConTope(codificar, 1600);
        expect(blob.size).toBeLessThanOrEqual(BYTES_OBJETIVO);
        expect(pedidos[0]).toEqual([1600, 0.82]);
        expect(pedidos.every(([l]) => l === 1600)).toBe(true);
    });

    it("si ninguna calidad alcanza, achica el lado y devuelve lo más chico", async () => {
        const codificar = (lado: number) => Promise.resolve(new Blob([new Uint8Array(lado * 1000)]));
        const blob = await codificarConTope(codificar, 1600, 10);
        expect(blob.size).toBe(Math.round(1600 * 0.64) * 1000);
    });
});
