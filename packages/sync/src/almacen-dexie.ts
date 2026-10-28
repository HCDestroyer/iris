// Adaptador de `Almacen` sobre IndexedDB con Dexie, para la PWA (`@iris/sync/dexie`). Es el
// único archivo del paquete que toca IndexedDB; el núcleo no lo importa.
import Dexie, { type DexieOptions, type Table } from "dexie";
import type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
import { META_INICIAL, type AdjuntoLocal, type MetaSync, type OperacionLocal, type OrdenLocal } from "./modelo";
import type { AlmacenPosiciones, CambiosPosiciones, PosicionLocal } from "./posiciones";

interface FilaMeta {
    clave: keyof MetaSync;
    valor: unknown;
}

interface FilaBytes {
    id: string;
    datos: Uint8Array;
}

class BaseSync extends Dexie {
    operaciones!: Table<OperacionLocal, string>;
    ordenes!: Table<OrdenLocal, number>;
    adjuntos!: Table<AdjuntoLocal, string>;
    bytes!: Table<FilaBytes, string>;
    meta!: Table<FilaMeta, string>;
    posiciones!: Table<PosicionLocal, number>;

    constructor(nombre: string, opciones?: DexieOptions) {
        super(nombre, opciones);
        // Un cambio de forma = nueva `version(n)` con su `upgrade`.
        this.version(1).stores({
            operaciones: "id, secuencia, ordenId",
            ordenes: "id",
            adjuntos: "id, ordenId",
            bytes: "id",
            meta: "clave",
        });
        // Versión 2: ids de orden numéricos (los del OpenAPI). La 1 solo existió en desarrollo,
        // con ids de texto provisionales: lo del servidor se descarta y se baja el paquete otra vez.
        this.version(2)
            .stores({
                operaciones: "id, secuencia, ordenId",
                ordenes: "id",
                adjuntos: "id, ordenId",
                bytes: "id",
                meta: "clave",
            })
            .upgrade(async (tx) => {
                await tx.table("ordenes").clear();
                await tx.table("meta").bulkDelete(["cursor", "fecha", "existencias", "ruta"]);
            });
        // Versión 3: cola de la ubicación en vivo (clave = hora del punto en ms).
        this.version(3).stores({
            operaciones: "id, secuencia, ordenId",
            ordenes: "id",
            adjuntos: "id, ordenId",
            bytes: "id",
            meta: "clave",
            posiciones: "ms",
        });
    }
}

async function aplicar<T, K extends string | number>(tabla: Table<T, K>, cambios: CambiosTabla<T, K> | undefined): Promise<void> {
    if (cambios?.borrar?.length) {
        await tabla.bulkDelete(cambios.borrar);
    }
    if (cambios?.poner?.length) {
        await tabla.bulkPut(cambios.poner);
    }
}

export interface OpcionesAlmacenDexie {
    /** Nombre de la base IndexedDB; una por usuario y empresa (`iris-campo-<usuario>-<empresa>`). */
    nombre: string;
    /** `indexedDB` e `IDBKeyRange` propios (p. ej. `fake-indexeddb` en pruebas). */
    dexie?: DexieOptions;
}

export class AlmacenDexie implements Almacen, AlmacenPosiciones {
    private readonly db: BaseSync;

    constructor(opciones: OpcionesAlmacenDexie) {
        this.db = new BaseSync(opciones.nombre, opciones.dexie);
    }

    async cargar(): Promise<ContenidoAlmacen> {
        return this.db.transaction("r", [this.db.operaciones, this.db.ordenes, this.db.adjuntos, this.db.meta], async () => {
            const [operaciones, ordenes, adjuntos, filas] = await Promise.all([
                this.db.operaciones.orderBy("secuencia").toArray(),
                this.db.ordenes.toArray(),
                this.db.adjuntos.toArray(),
                this.db.meta.toArray(),
            ]);
            const meta: Record<string, unknown> = { ...META_INICIAL };
            for (const fila of filas) {
                meta[fila.clave] = fila.valor;
            }
            return { operaciones, ordenes, adjuntos, meta: meta as unknown as MetaSync };
        });
    }

    async escribir(lote: LoteEscritura): Promise<void> {
        const tablas = [this.db.operaciones, this.db.ordenes, this.db.adjuntos, this.db.bytes, this.db.meta];
        await this.db.transaction("rw", tablas, async () => {
            await aplicar(this.db.operaciones, lote.operaciones);
            await aplicar(this.db.ordenes, lote.ordenes);
            await aplicar(this.db.adjuntos, lote.adjuntos);
            await aplicar(this.db.bytes, lote.bytes);
            if (lote.meta) {
                const filas = Object.entries(lote.meta).map(([clave, valor]) => ({ clave: clave as keyof MetaSync, valor }));
                await this.db.meta.bulkPut(filas);
            }
        });
    }

    async leerBytes(id: string): Promise<Uint8Array | null> {
        const fila = await this.db.bytes.get(id);
        return fila ? fila.datos : null;
    }

    async cargarPosiciones(): Promise<PosicionLocal[]> {
        return this.db.posiciones.orderBy("ms").toArray();
    }

    async escribirPosiciones(cambios: CambiosPosiciones): Promise<void> {
        await this.db.transaction("rw", this.db.posiciones, async () => {
            await aplicar(this.db.posiciones, cambios);
        });
    }

    /** Cierra la conexión (al salir de la sesión). */
    cerrar(): void {
        this.db.close();
    }

    /** Borra la base entera (al cerrar sesión en un teléfono compartido). */
    async borrar(): Promise<void> {
        await this.db.delete();
    }
}
