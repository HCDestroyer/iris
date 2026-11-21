import { describe, expect, it } from "vitest";
import type { Problema, ResultadoOperacion } from "./contrato";
import { montar } from "./pruebas/montaje";

// llamada.directa (E4c fase 3, contratos/sync.md §2): «Llamar desde mi celular» sin señal. El teléfono
// la encola con la hora de la llamada y la manda al volver la señal. Va sola: ni la detiene un
// rechazo de la orden ni su rechazo detiene la orden.

function rechazo(id: string, codigo: string): ResultadoOperacion {
    const error: Problema = { type: `urn:iris:problema:${codigo}`, title: codigo, status: 422, errors: null };
    return { id, estado: "rechazada", error };
}

describe("llamada.directa", () => {
    it("se guarda sin señal con la hora de la llamada y sale al volver la señal, sin cambiar la orden", async () => {
        const { motor, transporte, reloj } = await montar();
        transporte.sinRed = true;
        const hora = new Date(reloj.ahora()).toISOString();
        const { operacion } = await motor.registrar("llamada.directa", 1, { telefono: "contacto" });
        await expect(motor.sincronizar()).rejects.toBeTruthy();
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "programada", pendientes: 1 });

        transporte.sinRed = false;
        await reloj.avanzar(60_000);
        await motor.sincronizar();
        const enviada = transporte.lotes.at(-1)?.operaciones?.[0];
        expect(enviada).toMatchObject({ id: operacion.id, tipo: "llamada.directa", ordenId: 1, ocurridoEn: hora, datos: { telefono: "contacto" } });
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "programada", pendientes: 0 });
    });

    it("va sola: un rechazo de la orden no la detiene ni su rechazo detiene la orden", async () => {
        const { motor, transporte } = await montar();
        const llegar = await motor.registrar("orden.llegar", 1, {});
        const llamada = await motor.registrar("llamada.directa", 1, {});
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => (op.id === llegar.operacion.id ? rechazo(op.id, "transicion_invalida") : transporte.aplicar(op))),
        });
        await motor.sincronizar();
        // La llamada salió aunque la llegada se rechazó antes en la misma orden.
        expect(transporte.lotes.flatMap((l) => l.operaciones ?? []).map((op) => op.id)).toContain(llamada.operacion.id);
        expect(motor.obtenerEstado().rechazos.map((r) => r.id)).toEqual([llegar.operacion.id]);

        // Una llamada rechazada (hora del teléfono mal) no bloquea la orden.
        const otro = await montar();
        const mal = await otro.motor.registrar("llamada.directa", 1, {});
        const nota = await otro.motor.registrar("nota.agregar", 1, { texto: "después" });
        otro.transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => (op.id === mal.operacion.id ? rechazo(op.id, "hora_invalida") : otro.transporte.aplicar(op))),
        });
        await otro.motor.sincronizar();
        expect(otro.transporte.lotes.flatMap((l) => l.operaciones ?? []).map((op) => op.id)).toContain(nota.operacion.id);
        expect(otro.motor.obtenerOrden(1)).toMatchObject({ bloqueada: false });
    });

    it("se anota también en una orden cancelada o cerrada (es un registro, como una nota)", async () => {
        const { motor } = await montar();
        await motor.registrar("orden.iniciar_camino", 1, {});
        await motor.registrar("orden.llegar", 1, {});
        await motor.registrar("orden.fallar", 1, { motivoFalla: "cliente_ausente" });
        await expect(motor.registrar("llamada.directa", 1, {})).resolves.toBeTruthy();
    });
});
