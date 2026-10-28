import Dexie from "dexie";
import { IDBKeyRange, indexedDB } from "fake-indexeddb";
import { describe, expect, it } from "vitest";
import { AlmacenDexie } from "./almacen-dexie";
import { AlmacenMemoria } from "./almacen-memoria";
import type { PuntoPosicion } from "./contrato";
import { errorSesionAjena, ErrorTransporte } from "./errores";
import { ColaPosiciones, msDeRetryAfter, normalizarPunto, type AlmacenPosiciones, type OpcionesColaPosiciones } from "./posiciones";
import { TransportePosicionesFalso } from "./pruebas/posiciones-falso";
import { RelojManual, vaciarPromesas } from "./pruebas/reloj-manual";

const MINUTO = 60_000;

function punto(reloj: RelojManual, haceMs = 0, extra: Partial<PuntoPosicion> = {}): PuntoPosicion {
    return { lat: 14.634915, lon: -90.506882, precision: 12, registradaEn: new Date(reloj.ahora() - haceMs).toISOString(), ...extra };
}

async function montar(opciones: Partial<OpcionesColaPosiciones> = {}, almacen: AlmacenPosiciones = new AlmacenMemoria()) {
    const reloj = new RelojManual();
    const transporte = new TransportePosicionesFalso(reloj);
    const abrir = (extra: Partial<OpcionesColaPosiciones> = {}) =>
        ColaPosiciones.abrir({ almacen, transporte, reloj, dispositivoId: "disp-1", espera: { azar: () => 0 }, ...opciones, ...extra });
    const cola = await abrir();
    return { cola, almacen, transporte, reloj, abrir };
}

/** Agrega `n` puntos, uno por minuto hacia atrás desde ahora (el último es el más nuevo). */
async function agregarMinutos(cola: ColaPosiciones, reloj: RelojManual, n: number) {
    for (let i = n - 1; i >= 0; i -= 1) {
        await cola.agregar(punto(reloj, i * MINUTO));
    }
}

describe("normalizarPunto", () => {
    it("deja solo lo que la API acepta", () => {
        const base = { lat: 14.6349151234, lon: -90.5068829, registradaEn: "2026-10-28T15:00:00-06:00" };
        expect(normalizarPunto({ ...base, precision: 8.04, velocidad: 3.456, rumbo: Number.NaN })).toEqual({
            lat: 14.634915,
            lon: -90.506883,
            precision: 8,
            velocidad: 3.46,
            registradaEn: "2026-10-28T21:00:00.000Z",
        });
        expect(normalizarPunto({ ...base, velocidad: 200, rumbo: 400 })).toEqual({ lat: 14.634915, lon: -90.506883, registradaEn: "2026-10-28T21:00:00.000Z" });
        expect(normalizarPunto({ ...base, precision: 150_000 })).toBeNull();
        expect(normalizarPunto({ ...base, lat: 91 })).toBeNull();
        expect(normalizarPunto({ ...base, lon: Number.NaN })).toBeNull();
        expect(normalizarPunto({ ...base, registradaEn: "ayer" })).toBeNull();
    });

    it("entiende Retry-After en segundos o como fecha HTTP", () => {
        const ahora = Date.parse("2026-10-28T15:00:00Z");
        expect(msDeRetryAfter("12", ahora)).toBe(12_000);
        expect(msDeRetryAfter("Wed, 28 Oct 2026 15:00:30 GMT", ahora)).toBe(30_000);
        expect(msDeRetryAfter("pronto", ahora)).toBeNull();
        expect(msDeRetryAfter(null, ahora)).toBeNull();
    });
});

describe("cola de posiciones", () => {
    it("guarda primero y luego envía el lote con dispositivo y hora de envío", async () => {
        const { cola, almacen, transporte, reloj } = await montar();
        expect(await cola.agregar(punto(reloj, 0, { velocidad: 2, rumbo: 90 }))).toBe(true);
        expect((almacen as AlmacenMemoria).posiciones.size).toBe(1);
        expect(transporte.lotes).toHaveLength(0);
        expect(cola.obtenerEstado().pendientes).toBe(1);

        expect(await cola.enviar()).toBeNull();
        expect(transporte.lotes).toEqual([
            {
                dispositivoId: "disp-1",
                enviadoEn: new Date(reloj.ahora()).toISOString(),
                puntos: [{ lat: 14.634915, lon: -90.506882, precision: 12, velocidad: 2, rumbo: 90, registradaEn: new Date(reloj.ahora()).toISOString() }],
            },
        ]);
        expect((almacen as AlmacenMemoria).posiciones.size).toBe(0);
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 0, ultimoEnvio: reloj.ahora(), ultimaRespuesta: { aceptadas: 1 } });
    });

    it("si no se pudo guardar, no queda nada", async () => {
        const { cola, almacen, reloj } = await montar();
        (almacen as AlmacenMemoria).fallarProxima = new Error("QuotaExceededError");
        await expect(cola.agregar(punto(reloj))).rejects.toThrow("QuotaExceededError");
        expect(cola.obtenerEstado().pendientes).toBe(0);
    });

    it("ignora el mismo punto dos veces y uno de más de 24 h", async () => {
        const { cola, reloj } = await montar();
        const p = punto(reloj);
        expect(await cola.agregar(p)).toBe(true);
        expect(await cola.agregar(p)).toBe(false);
        expect(await cola.agregar(punto(reloj, 25 * 60 * MINUTO))).toBe(false);
        expect(cola.obtenerEstado().pendientes).toBe(1);
    });

    it("sin señal acumula y sobrevive a cerrar la app; al volver envía en orden", async () => {
        const { cola, transporte, reloj, abrir } = await montar();
        transporte.sinRed = true;
        await agregarMinutos(cola, reloj, 3);
        await expect(cola.enviar()).rejects.toBeInstanceOf(ErrorTransporte);
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 3, ultimoError: { mensaje: "Sin señal" } });

        const otra = await abrir();
        expect(otra.obtenerEstado().pendientes).toBe(3);
        transporte.sinRed = false;
        await otra.enviar();
        expect(transporte.lotes).toHaveLength(1);
        const horas = (transporte.lotes[0]?.puntos ?? []).map((p) => p.registradaEn);
        expect(horas).toEqual([...horas].sort());
        expect(otra.obtenerEstado()).toMatchObject({ pendientes: 0, ultimoError: null });
    });

    it("se limita a 6 lotes por minuto y sigue después sin recibir 429", async () => {
        const { cola, transporte, reloj } = await montar({ tamanoLote: 2 });
        await agregarMinutos(cola, reloj, 20);

        const espera = await cola.enviar();
        expect(transporte.lotes).toHaveLength(6);
        expect(transporte.intentos).toHaveLength(6);
        expect(espera).toBe(MINUTO);
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 8, esperaHasta: reloj.ahora() + MINUTO });

        await reloj.avanzar(MINUTO);
        expect(await cola.enviar()).toBeNull();
        expect(transporte.lotes).toHaveLength(10);
        expect(transporte.guardadas.size).toBe(20);
    });

    it("con 429 respeta Retry-After: no descarta, no reintenta antes y sigue después", async () => {
        // El teléfono cree que puede más que el servidor: el 7.º lote recibe 429.
        const { cola, transporte, reloj } = await montar({ tamanoLote: 1, lotesPorMinuto: 20 });
        await agregarMinutos(cola, reloj, 8);

        expect(await cola.enviar()).toBe(20_000);
        expect(transporte.lotes).toHaveLength(6);
        expect(transporte.intentos).toHaveLength(7);
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 2, ultimoError: null, esperaHasta: reloj.ahora() + 20_000 });

        // «Reintentar» o volver la señal no se salta el Retry-After.
        cola.reintentarAhora();
        await reloj.avanzar(10_000);
        expect(await cola.enviar()).toBe(10_000);
        expect(transporte.intentos).toHaveLength(7);

        await reloj.avanzar(10_000);
        await cola.enviar();
        expect(transporte.intentos.length).toBeGreaterThan(7);
        // Pasados 20 s, la ventana del servidor todavía tiene 6 lotes: otro 429, y se espera de nuevo.
        expect(cola.obtenerEstado().pendientes).toBe(2);
        await reloj.avanzar(MINUTO);
        expect(await cola.enviar()).toBeNull();
        expect(cola.obtenerEstado().pendientes).toBe(0);
        expect(transporte.guardadas.size).toBe(8);
    });

    it("con 429 sin Retry-After espera 30 s", async () => {
        const { cola, transporte, reloj } = await montar();
        transporte.fallar(1, new ErrorTransporte("http", "Demasiados", 429, { type: "urn:iris:problema:demasiadas_posiciones", title: "Demasiados", status: 429 }));
        await cola.agregar(punto(reloj));
        expect(await cola.enviar()).toBe(30_000);
        expect(cola.obtenerEstado().pendientes).toBe(1);
    });

    it("tope: descarta los más viejos y no admite uno más viejo que todo lo guardado", async () => {
        const { cola, almacen, reloj } = await montar({ tope: 3 });
        await agregarMinutos(cola, reloj, 5);
        const guardadas = [...(almacen as AlmacenMemoria).posiciones.keys()].sort();
        expect(guardadas).toEqual([reloj.ahora() - 2 * MINUTO, reloj.ahora() - MINUTO, reloj.ahora()]);
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 3, descartadas: { tope: 2 } });

        expect(await cola.agregar(punto(reloj, 10 * MINUTO))).toBe(false);
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 3, descartadas: { tope: 3 } });
    });

    it("borra lo que pasó de 24 h sin enviarse (al abrir y antes de enviar)", async () => {
        const { cola, transporte, reloj, abrir } = await montar();
        await cola.agregar(punto(reloj, 23 * 60 * MINUTO));
        await cola.agregar(punto(reloj, 60 * MINUTO));
        await reloj.avanzar(2 * 60 * MINUTO);

        const reabierta = await abrir();
        expect(reabierta.obtenerEstado()).toMatchObject({ pendientes: 1, descartadas: { vieja: 1 } });

        // La que quedaba (de hace 3 h) pasa de 24 h antes de que vuelva la señal.
        await reloj.avanzar(22 * 60 * MINUTO);
        await reabierta.agregar(punto(reloj));
        await reabierta.enviar();
        expect(transporte.lotes).toHaveLength(1);
        expect(transporte.lotes[0]?.puntos).toEqual([expect.objectContaining({ registradaEn: new Date(reloj.ahora()).toISOString() })]);
        expect(reabierta.obtenerEstado()).toMatchObject({ pendientes: 0, descartadas: { vieja: 2 } });
    });

    it("un 422 borra el lote a la vista y la cola sigue", async () => {
        const { cola, transporte, reloj } = await montar();
        transporte.fallar(
            1,
            new ErrorTransporte("http", "Datos inválidos", 422, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422 }),
        );
        await cola.agregar(punto(reloj, MINUTO));
        expect(await cola.enviar()).toBeNull();
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 0, descartadas: { invalida: 1 }, ultimoError: { status: 422 } });
        await cola.agregar(punto(reloj));
        await cola.enviar();
        expect(transporte.lotes).toHaveLength(1);
        expect(cola.obtenerEstado().ultimoError).toBeNull();
    });
});

describe("bucle de envío", () => {
    it("envía al llegar un punto, reintenta con espera tras una falla y se detiene sin sesión", async () => {
        const { cola, transporte, reloj } = await montar();
        cola.iniciar();
        await vaciarPromesas();
        expect(transporte.lotes).toHaveLength(0);

        await cola.agregar(punto(reloj, MINUTO));
        await vaciarPromesas();
        expect(transporte.lotes).toHaveLength(1);

        transporte.fallar(1);
        await cola.agregar(punto(reloj));
        await vaciarPromesas();
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 1, proximoIntento: reloj.ahora() + 1000 });
        // Un punto nuevo no corta la espera exponencial…
        await reloj.avanzar(500);
        await cola.agregar(punto(reloj));
        expect(transporte.lotes).toHaveLength(1);
        // …y al vencer se envía todo.
        await reloj.avanzar(500);
        expect(transporte.lotes).toHaveLength(2);
        expect(cola.obtenerEstado().pendientes).toBe(0);

        transporte.fallar(1, new ErrorTransporte("http", "Requiere sesión de campo", 403, { type: "urn:iris:problema:requiere_sesion_campo", title: "Requiere sesión de campo", status: 403 }));
        await cola.agregar(punto(reloj, -1000));
        await vaciarPromesas();
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 1, detenida: true });
        await reloj.avanzar(10 * MINUTO);
        expect(transporte.lotes).toHaveLength(2);
        cola.detener();
    });

    it("con la sesión de otra persona no envía nada y la cola queda intacta", async () => {
        let ajena = false;
        const { cola, transporte, reloj } = await montar({
            verificarIdentidad: () => (ajena ? Promise.reject(errorSesionAjena()) : Promise.resolve()),
        });
        ajena = true;
        await cola.agregar(punto(reloj));
        await expect(cola.enviar()).rejects.toMatchObject({ codigo: "sesion_ajena" });
        expect(transporte.intentos).toHaveLength(0);
        expect(cola.obtenerEstado()).toMatchObject({ pendientes: 1, detenida: true });
    });
});

describe("aislamiento por usuario y empresa (IndexedDB)", () => {
    const almacenDe = (usuario: number, empresa: number) => new AlmacenDexie({ nombre: `iris-campo-${usuario}-${empresa}`, dexie: { indexedDB, IDBKeyRange } });

    it("cada usuario y empresa tiene su cola; cerrar sesión borra solo la suya", async () => {
        const a = await montar({}, almacenDe(7, 1));
        const b = await montar({}, almacenDe(8, 1));
        const a2 = await montar({}, almacenDe(7, 2));
        await agregarMinutos(a.cola, a.reloj, 2);
        await b.cola.agregar(punto(b.reloj));

        expect((await almacenDe(7, 1).cargarPosiciones()).length).toBe(2);
        expect((await almacenDe(8, 1).cargarPosiciones()).length).toBe(1);
        expect(a2.cola.obtenerEstado().pendientes).toBe(0);

        await almacenDe(7, 1).borrar();
        expect((await almacenDe(7, 1).cargarPosiciones()).length).toBe(0);
        expect((await almacenDe(8, 1).cargarPosiciones()).length).toBe(1);
    });

    it("la versión 3 de la base conserva la cola de sync de la versión 2", async () => {
        const nombre = "iris-campo-9-1";
        const v2 = new Dexie(nombre, { indexedDB, IDBKeyRange });
        v2.version(2).stores({ operaciones: "id, secuencia, ordenId", ordenes: "id", adjuntos: "id, ordenId", bytes: "id", meta: "clave" });
        await v2.table("meta").put({ clave: "secuencia", valor: 4 });
        v2.close();

        const v3 = new AlmacenDexie({ nombre, dexie: { indexedDB, IDBKeyRange } });
        await v3.escribirPosiciones({ poner: [{ ms: 1, lat: 1, lon: 1, registradaEn: new Date(1).toISOString() }] });
        expect((await v3.cargar()).meta.secuencia).toBe(4);
        expect(await v3.cargarPosiciones()).toHaveLength(1);
    });
});
