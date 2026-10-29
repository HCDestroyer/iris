// Tarea de segundo plano del GPS. `defineTask` tiene que correr al cargar el bundle (también
// cuando el sistema despierta la app sin pantalla), por eso se importa desde `index.ts`.
import { AlmacenSqlite } from "@iris/sync/sqlite";
import { openDatabaseAsync } from "expo-sqlite";
import * as TaskManager from "expo-task-manager";
import { entorno } from "../entorno";
import { sesionGuardada, usableSinSenal } from "../sesion/sesion";
import { gpsNativo } from "./gps-nativo";
import { recibirLecturas, TAREA_UBICACION, type LecturaGps } from "./ubicacion";

interface DatosTarea {
    locations: LecturaGps[];
}

TaskManager.defineTask<DatosTarea>(TAREA_UBICACION, async ({ data, error }) => {
    if (error || !data) {
        return;
    }
    await recibirLecturas(data.locations, {
        claves: entorno().claves,
        detenerGps: () => gpsNativo.detener(),
        // Misma regla que abrir la app sin señal: sesión de ese usuario verificada hace < 12 h.
        sesionVigente: (usuarioId) => {
            const s = sesionGuardada();
            return s !== null && s.usuarioId === usuarioId && usableSinSenal(s);
        },
        async guardarEnBase(base, puntos) {
            // App cerrada: se guardan en la cola de esa base; `ColaPosiciones` los envía al abrir.
            const almacen = new AlmacenSqlite(await openDatabaseAsync(base));
            try {
                await almacen.escribirPosiciones({ poner: puntos.map((p) => ({ ...p, ms: Date.parse(p.registradaEn) })) });
            } finally {
                await almacen.cerrar();
            }
        },
    });
});
