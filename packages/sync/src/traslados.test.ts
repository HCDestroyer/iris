import { describe, expect, it } from "vitest";

import { esquemaRecibirTraslado } from "./campo";
import { TIPO_RECIBIR_TRASLADO, TIPOS_OPERACION, operacionRecibirTraslado, type EntradaRecibirTraslado } from "./index";

// traslado.recibir (contratos/sync.md §2 y §3): no es de una orden y no entra en las operaciones del motor.
describe("traslado.recibir", () => {
    const datos: EntradaRecibirTraslado = {
        trasladoId: 31,
        firmaAdjuntoId: "0190f000-0000-7000-8000-000000000001",
        recibidoEn: "2026-10-30T15:00:00-06:00",
        lineas: [{ lineaId: 1, cantidadRecibida: 9, seriesFaltantes: ["ZTE-2"] }],
    };

    it("arma la operación sin ordenId ni versionBase", () => {
        const op = operacionRecibirTraslado("0190f000-0000-7000-8000-0000000000aa", datos, "2026-10-30T21:00:00Z");
        expect(op).toEqual({ id: "0190f000-0000-7000-8000-0000000000aa", tipo: "traslado.recibir", ocurridoEn: "2026-10-30T21:00:00Z", datos });
        expect("ordenId" in op).toBe(false);
        expect("versionBase" in op).toBe(false);
    });

    it("no es una operación de orden del motor", () => {
        expect((TIPOS_OPERACION as readonly string[]).includes(TIPO_RECIBIR_TRASLADO)).toBe(false);
    });

    it("valida los datos como la API", () => {
        expect(esquemaRecibirTraslado.safeParse(datos).success).toBe(true);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, lineas: undefined }).success).toBe(true);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, trasladoId: 0 }).success).toBe(false);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, firmaAdjuntoId: "no-uuid" }).success).toBe(false);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, recibidoEn: "ayer" }).success).toBe(false);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, lineas: [{ lineaId: 1, cantidadRecibida: -1 }] }).success).toBe(false);
        const repetida = esquemaRecibirTraslado.safeParse({
            ...datos,
            lineas: [
                { lineaId: 1, cantidadRecibida: 1 },
                { lineaId: 1, cantidadRecibida: 2 },
            ],
        });
        expect(repetida.success).toBe(false);
        expect(repetida.error?.issues[0]?.path).toEqual(["lineas", 1, "lineaId"]);
    });
});
