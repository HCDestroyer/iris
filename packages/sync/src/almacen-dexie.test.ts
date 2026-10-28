// Adaptador de IndexedDB probado con `fake-indexeddb` (IndexedDB completo en memoria para Node).
import { IDBKeyRange, indexedDB } from "fake-indexeddb";
import { describe, expect, it } from "vitest";
import { AlmacenDexie } from "./almacen-dexie";
import type { OperacionLocal } from "./modelo";
import { MotorSync } from "./motor";
import { ordenDePrueba } from "./pruebas/fabricas";
import { paqueteDePrueba, FECHA } from "./pruebas/montaje";
import { RelojManual } from "./pruebas/reloj-manual";
import { SubidorFalso } from "./pruebas/subidor-falso";
import { TransporteFalso } from "./pruebas/transporte-falso";

let n = 0;
function nuevoAlmacen(nombre = `iris-campo-prueba-${++n}`): AlmacenDexie {
    return new AlmacenDexie({ nombre, dexie: { indexedDB, IDBKeyRange } });
}

const op: OperacionLocal = {
    id: "0190f000-0000-7000-8000-000000000001",
    tipo: "nota.agregar",
    ordenId: "A",
    versionBase: 1,
    ocurridoEn: "2026-10-28T15:00:00.000Z",
    datos: { texto: "hola" },
    secuencia: 1,
    estado: "pendiente",
    error: null,
    intentos: 0,
};

describe("AlmacenDexie", () => {
    it("empieza vacío con la meta inicial", async () => {
        const almacen = nuevoAlmacen();
        const contenido = await almacen.cargar();
        expect(contenido).toMatchObject({ operaciones: [], ordenes: [], adjuntos: [], meta: { secuencia: 0, cursor: null } });
        almacen.cerrar();
    });

    it("guarda, borra y lee lo escrito, también tras reabrir la base", async () => {
        const nombre = `iris-campo-prueba-${++n}`;
        const almacen = nuevoAlmacen(nombre);
        await almacen.escribir({
            operaciones: { poner: [op, { ...op, id: "b", secuencia: 2 }] },
            ordenes: { poner: [{ id: "A", base: ordenDePrueba("A"), reasignada: false }] },
            bytes: { poner: [{ id: "f1", datos: Uint8Array.from([1, 2, 3]) }] },
            meta: { cursor: "c1", secuencia: 2, existencias: [{ productoId: "x", nombre: "X", unidad: "u", cantidad: 1 }] },
        });
        await almacen.escribir({ operaciones: { borrar: ["b"] }, meta: { cursor: "c2" } });
        almacen.cerrar();

        const otra = nuevoAlmacen(nombre);
        const contenido = await otra.cargar();
        expect(contenido.operaciones).toEqual([op]);
        expect(contenido.ordenes[0]?.base.numero).toBe("OT-A");
        expect(contenido.meta).toMatchObject({ cursor: "c2", secuencia: 2, desfaseMs: 0 });
        expect(contenido.meta.existencias).toHaveLength(1);
        expect(await otra.leerBytes("f1")).toEqual(Uint8Array.from([1, 2, 3]));
        expect(await otra.leerBytes("no")).toBeNull();
        await otra.borrar();
    });

    it("carga la cola en orden de secuencia", async () => {
        const almacen = nuevoAlmacen();
        await almacen.escribir({ operaciones: { poner: [{ ...op, id: "z", secuencia: 3 }, { ...op, id: "y", secuencia: 1 }, { ...op, id: "x", secuencia: 2 }] } });
        expect((await almacen.cargar()).operaciones.map((o) => o.id)).toEqual(["y", "x", "z"]);
        almacen.cerrar();
    });

    it("una escritura es atómica: si falla, no queda nada a medias", async () => {
        const almacen = nuevoAlmacen();
        const invalida = { ...op, id: undefined } as unknown as OperacionLocal; // sin clave primaria
        await expect(
            almacen.escribir({ ordenes: { poner: [{ id: "A", base: ordenDePrueba("A"), reasignada: false }] }, operaciones: { poner: [invalida] } }),
        ).rejects.toThrow();
        expect((await almacen.cargar()).ordenes).toEqual([]);
        almacen.cerrar();
    });

    it("el motor funciona de punta a punta sobre IndexedDB y sobrevive al reinicio", async () => {
        const nombre = `iris-campo-prueba-${++n}`;
        const transporte = new TransporteFalso();
        transporte.paquete = (fecha) => paqueteDePrueba(fecha);
        const reloj = new RelojManual();
        const subidor = new SubidorFalso();
        const motor = await MotorSync.abrir({ almacen: nuevoAlmacen(nombre), transporte, reloj, subidor });
        await motor.cargarPaquete(FECHA);
        transporte.sinRed = true;
        await motor.registrar("orden.iniciar_camino", "A", {});
        const { adjunto } = await motor.agregarAdjunto({ ordenId: "A", tipo: "foto", datos: new Uint8Array(5).fill(9), mime: "image/jpeg" });

        const reabierto = await MotorSync.abrir({ almacen: nuevoAlmacen(nombre), transporte, reloj, subidor });
        expect(reabierto.dispositivoId).toBe(motor.dispositivoId);
        expect(reabierto.obtenerEstado()).toMatchObject({ pendientes: 2, fecha: FECHA });
        expect(reabierto.obtenerOrden("A")?.estado).toBe("en_camino");

        transporte.sinRed = false;
        await reabierto.sincronizar();
        expect(reabierto.obtenerEstado().pendientes).toBe(0);
        expect(subidor.completo(adjunto.id)).toBe(true);
    });
});
