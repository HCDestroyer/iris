// Adaptador de `Almacen` (y de la cola de posiciones) sobre SQLite, para la app nativa de Expo
// (`@iris/sync/sqlite`). No importa `expo-sqlite`: recibe una conexión con la forma mínima de su
// `SQLiteDatabase` (`execAsync`, `runAsync`, `getAllAsync`, `closeAsync`), así el paquete no suma
// dependencias y se prueba en Node con `node:sqlite`.
//
// Cada fila guarda su registro como JSON (las mismas formas que Dexie), con las columnas que hacen
// falta para ordenar; los bytes de los adjuntos van en BLOB. Todas las operaciones pasan por una
// sola fila de espera: una escritura es `BEGIN … COMMIT` sin nada intercalado (el motor y la cola de
// posiciones comparten la conexión), y si algo falla se hace `ROLLBACK` (atómica, como Dexie).
import type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
import { META_INICIAL, type AdjuntoLocal, type MetaSync, type OperacionLocal, type OrdenLocal } from "./modelo";
import type { AlmacenPosiciones, CambiosPosiciones, PosicionLocal } from "./posiciones";

/** Valores que se enlazan a una sentencia (los mismos que acepta `expo-sqlite`). */
export type ValorSqlite = string | number | null | Uint8Array;

/** Lo que el adaptador usa de una conexión (`SQLiteDatabase` de `expo-sqlite` lo cumple). */
export interface ConexionSqlite {
    execAsync(sql: string): Promise<void>;
    runAsync(sql: string, params: ValorSqlite[]): Promise<unknown>;
    getAllAsync<T>(sql: string, params: ValorSqlite[]): Promise<T[]>;
    closeAsync(): Promise<void>;
}

/** Espera ante otra conexión con el archivo bloqueado (`PRAGMA busy_timeout`). */
export const ESPERA_OCUPADA_MS = 5000;

/** Versión del esquema (`PRAGMA user_version`). Un cambio de forma = otra versión con su migración. */
export const VERSION_ESQUEMA_SQLITE = 1;

const ESQUEMA = `
CREATE TABLE IF NOT EXISTS operaciones (id TEXT PRIMARY KEY NOT NULL, secuencia INTEGER NOT NULL, orden_id INTEGER NOT NULL, json TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS operaciones_secuencia ON operaciones (secuencia);
CREATE TABLE IF NOT EXISTS ordenes (id INTEGER PRIMARY KEY NOT NULL, json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS adjuntos (id TEXT PRIMARY KEY NOT NULL, orden_id INTEGER NOT NULL, json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS bytes (id TEXT PRIMARY KEY NOT NULL, datos BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS meta (clave TEXT PRIMARY KEY NOT NULL, json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS posiciones (ms INTEGER PRIMARY KEY NOT NULL, json TEXT NOT NULL);
`;

interface FilaJson {
    json: string;
}

function exigirClave(valor: unknown, tabla: string): string | number {
    if (typeof valor !== "string" && typeof valor !== "number") {
        throw new Error(`Fila de ${tabla} sin clave`);
    }
    return valor;
}

/** Bytes de una columna BLOB: `expo-sqlite` da `Uint8Array`; `node:sqlite` también (o un Buffer, que lo es). */
function aBytes(valor: unknown): Uint8Array {
    if (valor instanceof Uint8Array) {
        return new Uint8Array(valor);
    }
    if (valor instanceof ArrayBuffer) {
        return new Uint8Array(valor);
    }
    throw new Error("Columna de bytes ilegible");
}

export class AlmacenSqlite implements Almacen, AlmacenPosiciones {
    private readonly db: ConexionSqlite;
    private cola: Promise<unknown> = Promise.resolve();
    private preparada: Promise<void> | null = null;
    private cerrada = false;

    constructor(db: ConexionSqlite) {
        this.db = db;
    }

    /** Crea las tablas si faltan. Se llama sola antes de la primera operación. */
    preparar(): Promise<void> {
        // `busy_timeout`: con la app cerrada la tarea del GPS abre otra conexión al mismo archivo; si
        // choca con un lote, espera hasta 5 s en vez de fallar con SQLITE_BUSY.
        this.preparada ??= this.db.execAsync(`PRAGMA busy_timeout = ${ESPERA_OCUPADA_MS};PRAGMA journal_mode = WAL;${ESQUEMA}PRAGMA user_version = ${VERSION_ESQUEMA_SQLITE};`);
        return this.preparada;
    }

    cargar(): Promise<ContenidoAlmacen> {
        return this.enFila(async () => {
            const operaciones = await this.leerJson<OperacionLocal>("SELECT json FROM operaciones ORDER BY secuencia");
            const ordenes = await this.leerJson<OrdenLocal>("SELECT json FROM ordenes");
            const adjuntos = await this.leerJson<AdjuntoLocal>("SELECT json FROM adjuntos");
            const filas = await this.db.getAllAsync<{ clave: string; json: string }>("SELECT clave, json FROM meta", []);
            const meta: Record<string, unknown> = { ...structuredClone(META_INICIAL) };
            for (const fila of filas) {
                meta[fila.clave] = JSON.parse(fila.json) as unknown;
            }
            return { operaciones, ordenes, adjuntos, meta: meta as unknown as MetaSync };
        });
    }

    escribir(lote: LoteEscritura): Promise<void> {
        return this.enTransaccion(async () => {
            await this.aplicar("operaciones", lote.operaciones, (op) => [op.id, op.secuencia, op.ordenId], "id, secuencia, orden_id");
            await this.aplicar("ordenes", lote.ordenes, (o) => [o.id], "id");
            await this.aplicar("adjuntos", lote.adjuntos, (a) => [a.id, a.ordenId], "id, orden_id");
            for (const id of lote.bytes?.borrar ?? []) {
                await this.db.runAsync("DELETE FROM bytes WHERE id = ?", [id]);
            }
            for (const fila of lote.bytes?.poner ?? []) {
                await this.db.runAsync("INSERT OR REPLACE INTO bytes (id, datos) VALUES (?, ?)", [exigirClave(fila.id, "bytes"), fila.datos]);
            }
            for (const [clave, valor] of Object.entries(lote.meta ?? {})) {
                await this.db.runAsync("INSERT OR REPLACE INTO meta (clave, json) VALUES (?, ?)", [clave, JSON.stringify(valor ?? null)]);
            }
        });
    }

    leerBytes(id: string): Promise<Uint8Array | null> {
        return this.enFila(async () => {
            const [fila] = await this.db.getAllAsync<{ datos: unknown }>("SELECT datos FROM bytes WHERE id = ?", [id]);
            return fila ? aBytes(fila.datos) : null;
        });
    }

    cargarPosiciones(): Promise<PosicionLocal[]> {
        return this.enFila(() => this.leerJson<PosicionLocal>("SELECT json FROM posiciones ORDER BY ms"));
    }

    escribirPosiciones(cambios: CambiosPosiciones): Promise<void> {
        return this.enTransaccion(async () => {
            for (const ms of cambios.borrar ?? []) {
                await this.db.runAsync("DELETE FROM posiciones WHERE ms = ?", [ms]);
            }
            for (const punto of cambios.poner ?? []) {
                await this.db.runAsync("INSERT OR REPLACE INTO posiciones (ms, json) VALUES (?, ?)", [exigirClave(punto.ms, "posiciones"), JSON.stringify(punto)]);
            }
        });
    }

    /** Cierra la conexión (al salir de la sesión). Borrar el archivo es de quien lo abrió (`deleteDatabaseAsync`). */
    async cerrar(): Promise<void> {
        if (this.cerrada) {
            return;
        }
        await this.enFila(async () => {
            this.cerrada = true;
            await this.db.closeAsync();
        });
    }

    private async leerJson<T>(sql: string): Promise<T[]> {
        const filas = await this.db.getAllAsync<FilaJson>(sql, []);
        return filas.map((f) => JSON.parse(f.json) as T);
    }

    private async aplicar<T, K extends string | number>(
        tabla: string,
        cambios: CambiosTabla<T, K> | undefined,
        columnas: (fila: T) => ValorSqlite[],
        nombres: string,
    ): Promise<void> {
        for (const id of cambios?.borrar ?? []) {
            await this.db.runAsync(`DELETE FROM ${tabla} WHERE id = ?`, [id]);
        }
        for (const fila of cambios?.poner ?? []) {
            const valores = columnas(fila);
            exigirClave(valores[0], tabla);
            const marcas = valores.map(() => "?").join(", ");
            await this.db.runAsync(`INSERT OR REPLACE INTO ${tabla} (${nombres}, json) VALUES (${marcas}, ?)`, [...valores, JSON.stringify(fila)]);
        }
    }

    /** Una operación a la vez sobre la conexión (en el orden en que se pidieron). */
    private enFila<T>(tarea: () => Promise<T>): Promise<T> {
        if (this.cerrada) {
            return Promise.reject(new Error("El almacén SQLite está cerrado"));
        }
        const siguiente = this.cola.then(async () => {
            if (this.cerrada) {
                throw new Error("El almacén SQLite está cerrado");
            }
            await this.preparar();
            return tarea();
        });
        this.cola = siguiente.catch(() => undefined);
        return siguiente;
    }

    private enTransaccion(tarea: () => Promise<void>): Promise<void> {
        return this.enFila(async () => {
            await this.db.execAsync("BEGIN IMMEDIATE");
            try {
                await tarea();
                await this.db.execAsync("COMMIT");
            } catch (e) {
                await this.db.execAsync("ROLLBACK");
                throw e;
            }
        });
    }
}
