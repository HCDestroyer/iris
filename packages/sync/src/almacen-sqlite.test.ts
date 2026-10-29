// Adaptador SQLite probado con `node:sqlite` (Node 22) envuelto con la forma de `expo-sqlite`.
/// <reference types="node" />
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync, type SQLInputValue } from "node:sqlite";
import { afterAll, describe, expect, it } from "vitest";
import { AlmacenSqlite, ESPERA_OCUPADA_MS, type ConexionSqlite, type ValorSqlite } from "./almacen-sqlite";
import type { OperacionLocal } from "./modelo";
import { MotorSync } from "./motor";
import { ColaPosiciones } from "./posiciones";
import { ordenDePrueba } from "./pruebas/fabricas";
import { paqueteDePrueba, FECHA } from "./pruebas/montaje";
import { TransportePosicionesFalso } from "./pruebas/posiciones-falso";
import { RelojManual } from "./pruebas/reloj-manual";
import { SubidorFalso } from "./pruebas/subidor-falso";
import { TransporteFalso } from "./pruebas/transporte-falso";

const dir = mkdtempSync(join(tmpdir(), "iris-sqlite-"));
afterAll(() => rmSync(dir, { recursive: true, force: true }));

/** `node:sqlite` (síncrono) con la API asíncrona de `expo-sqlite`. */
function conexionNode(ruta: string): ConexionSqlite & { sentencias: string[] } {
    const db = new DatabaseSync(ruta);
    const sentencias: string[] = [];
    const params = (p: ValorSqlite[]) => p as SQLInputValue[];
    return {
        sentencias,
        execAsync: async (sql) => {
            sentencias.push(sql);
            db.exec(sql);
        },
        runAsync: async (sql, p) => {
            sentencias.push(sql);
            return db.prepare(sql).run(...params(p));
        },
        getAllAsync: async <T>(sql: string, p: ValorSqlite[]) => db.prepare(sql).all(...params(p)) as T[],
        closeAsync: async () => {
            db.close();
        },
    };
}

let n = 0;
const ruta = () => join(dir, `campo-${++n}.db`);

const op: OperacionLocal = {
    id: "0190f000-0000-7000-8000-000000000001",
    tipo: "nota.agregar",
    ordenId: 1,
    versionBase: 1,
    ocurridoEn: "2026-10-28T15:00:00.000Z",
    datos: { texto: "hola" },
    secuencia: 1,
    estado: "pendiente",
    error: null,
    intentos: 0,
};

describe("AlmacenSqlite", () => {
    it("empieza vacío con la meta inicial y espera ante otra conexión (busy_timeout)", async () => {
        const conexion = conexionNode(ruta());
        const almacen = new AlmacenSqlite(conexion);
        expect(await almacen.cargar()).toMatchObject({ operaciones: [], ordenes: [], adjuntos: [], meta: { secuencia: 0, cursor: null, avisos: [] } });
        expect(await conexion.getAllAsync("PRAGMA busy_timeout", [])).toEqual([{ timeout: ESPERA_OCUPADA_MS }]);
        await almacen.cerrar();
    });

    it("guarda, borra y lee lo escrito, también tras reabrir el archivo", async () => {
        const archivo = ruta();
        const almacen = new AlmacenSqlite(conexionNode(archivo));
        await almacen.escribir({
            operaciones: { poner: [op, { ...op, id: "b", secuencia: 2 }] },
            ordenes: { poner: [{ id: 1, base: ordenDePrueba(1), reasignada: false }] },
            bytes: { poner: [{ id: "f1", datos: Uint8Array.from([1, 2, 3]) }] },
            meta: { cursor: "c1", secuencia: 2, existencias: { disponible: true, motivo: null, bodegas: [{ bodegaId: 7, lineas: [{ productoId: 3, descripcion: "X", unidad: "u", cantidad: 1 }] }] } },
        });
        await almacen.escribir({ operaciones: { borrar: ["b"] }, meta: { cursor: "c2" } });
        await almacen.cerrar();
        await almacen.cerrar(); // idempotente
        await expect(almacen.cargar()).rejects.toThrow(/cerrado/);

        const otra = new AlmacenSqlite(conexionNode(archivo));
        const contenido = await otra.cargar();
        expect(contenido.operaciones).toEqual([op]);
        expect(contenido.ordenes[0]?.base.numero).toBe("OT-000001");
        expect(contenido.meta).toMatchObject({ cursor: "c2", secuencia: 2, desfaseMs: 0 });
        expect(contenido.meta.existencias?.bodegas).toHaveLength(1);
        expect(await otra.leerBytes("f1")).toEqual(Uint8Array.from([1, 2, 3]));
        expect(await otra.leerBytes("no")).toBeNull();
        await otra.escribir({ bytes: { borrar: ["f1"] } });
        expect(await otra.leerBytes("f1")).toBeNull();
        await otra.cerrar();
    });

    it("carga la cola en orden de secuencia", async () => {
        const almacen = new AlmacenSqlite(conexionNode(ruta()));
        await almacen.escribir({ operaciones: { poner: [{ ...op, id: "z", secuencia: 3 }, { ...op, id: "y", secuencia: 1 }, { ...op, id: "x", secuencia: 2 }] } });
        expect((await almacen.cargar()).operaciones.map((o) => o.id)).toEqual(["y", "x", "z"]);
        await almacen.cerrar();
    });

    it("una escritura es atómica: si falla, no queda nada a medias (ROLLBACK)", async () => {
        const conexion = conexionNode(ruta());
        const almacen = new AlmacenSqlite(conexion);
        const invalida = { ...op, id: undefined } as unknown as OperacionLocal; // sin clave primaria
        await expect(
            almacen.escribir({ ordenes: { poner: [{ id: 1, base: ordenDePrueba(1), reasignada: false }] }, operaciones: { poner: [invalida] } }),
        ).rejects.toThrow();
        expect(conexion.sentencias).toContain("ROLLBACK");
        expect((await almacen.cargar()).ordenes).toEqual([]);
        // La conexión sigue usable después del fallo.
        await almacen.escribir({ operaciones: { poner: [op] } });
        expect((await almacen.cargar()).operaciones).toHaveLength(1);
        await almacen.cerrar();
    });

    it("las escrituras concurrentes no se intercalan (motor y posiciones comparten conexión)", async () => {
        const conexion = conexionNode(ruta());
        const almacen = new AlmacenSqlite(conexion);
        await Promise.all([
            almacen.escribir({ operaciones: { poner: [op] } }),
            almacen.escribirPosiciones({ poner: [{ ms: 1, lat: 14.6, lon: -90.5, registradaEn: "2026-10-28T15:00:00.000Z" }] }),
            almacen.escribir({ meta: { cursor: "x" } }),
        ]);
        const transacciones = conexion.sentencias.filter((s) => s === "BEGIN IMMEDIATE" || s === "COMMIT");
        expect(transacciones).toEqual(["BEGIN IMMEDIATE", "COMMIT", "BEGIN IMMEDIATE", "COMMIT", "BEGIN IMMEDIATE", "COMMIT"]);
        expect(await almacen.cargarPosiciones()).toHaveLength(1);
        await almacen.escribirPosiciones({ borrar: [1] });
        expect(await almacen.cargarPosiciones()).toEqual([]);
        await almacen.cerrar();
    });

    it("el motor y la cola de posiciones funcionan de punta a punta sobre SQLite y sobreviven al reinicio", async () => {
        const archivo = ruta();
        const transporte = new TransporteFalso();
        transporte.paquete = (fecha) => paqueteDePrueba(fecha);
        const reloj = new RelojManual();
        const subidor = new SubidorFalso();
        const almacen = new AlmacenSqlite(conexionNode(archivo));
        const motor = await MotorSync.abrir({ almacen, transporte, reloj, subidor });
        await motor.cargarPaquete(FECHA);
        transporte.sinRed = true;
        await motor.registrar("orden.iniciar_camino", 1, {});
        const { adjunto } = await motor.agregarAdjunto({ ordenId: 1, tipo: "foto", datos: new Uint8Array(5).fill(9), mime: "image/jpeg" });
        const posiciones = new TransportePosicionesFalso(reloj);
        const cola = await ColaPosiciones.abrir({ almacen, transporte: posiciones, reloj, dispositivoId: motor.dispositivoId });
        await cola.agregar({ lat: 14.6, lon: -90.5, registradaEn: new Date(reloj.ahora()).toISOString() });
        await almacen.cerrar();

        const otra = new AlmacenSqlite(conexionNode(archivo));
        const reabierto = await MotorSync.abrir({ almacen: otra, transporte, reloj, subidor });
        expect(reabierto.dispositivoId).toBe(motor.dispositivoId);
        expect(reabierto.obtenerEstado()).toMatchObject({ pendientes: 2, fecha: FECHA });
        expect(reabierto.obtenerOrden(1)?.estado).toBe("en_camino");
        const colaReabierta = await ColaPosiciones.abrir({ almacen: otra, transporte: posiciones, reloj, dispositivoId: reabierto.dispositivoId });
        expect(colaReabierta.obtenerEstado().pendientes).toBe(1);

        transporte.sinRed = false;
        await reabierto.sincronizar();
        expect(reabierto.obtenerEstado().pendientes).toBe(0);
        expect(subidor.completo(adjunto.id)).toBe(true);
        await otra.cerrar();
    });
});
