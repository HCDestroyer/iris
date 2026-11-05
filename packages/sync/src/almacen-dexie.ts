// Adaptador de `Almacen` sobre IndexedDB con Dexie, para la PWA (`@iris/sync/dexie`). Es el
// único archivo del paquete que toca IndexedDB; el núcleo no lo importa.
import Dexie, { type DexieOptions, type Table } from "dexie";
import type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
import { META_INICIAL, type AdjuntoLocal, type MetaSync, type OperacionLocal, type OrdenLocal } from "./modelo";
import type { AlmacenPosiciones, CambiosPosiciones, PosicionLocal } from "./posiciones";
import { META_CHAT_INICIAL, type AlmacenChat, type ContenidoChat, type ConversacionChat, type LoteChat, type MensajeChat, type MetaChat, type SalidaChat } from "./chat/tipos";

interface FilaMetaChat {
    clave: keyof MetaChat;
    valor: unknown;
}

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
    chatSalida!: Table<SalidaChat, string>;
    chatConversaciones!: Table<ConversacionChat, number>;
    chatMensajes!: Table<MensajeChat, string>;
    chatBytes!: Table<FilaBytes, string>;
    chatMeta!: Table<FilaMetaChat, string>;

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
        // Versión 4: chat interno (cola de salida con sus fotos y caché de conversaciones y mensajes).
        this.version(4).stores({
            operaciones: "id, secuencia, ordenId",
            ordenes: "id",
            adjuntos: "id, ordenId",
            bytes: "id",
            meta: "clave",
            posiciones: "ms",
            chatSalida: "id, secuencia",
            chatConversaciones: "id",
            chatMensajes: "id, conversacion_id",
            chatBytes: "id",
            chatMeta: "clave",
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

export class AlmacenDexie implements Almacen, AlmacenPosiciones, AlmacenChat {
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

    async cargarChat(): Promise<ContenidoChat> {
        const t = [this.db.chatSalida, this.db.chatConversaciones, this.db.chatMensajes, this.db.chatMeta];
        return this.db.transaction("r", t, async () => {
            const [salida, conversaciones, mensajes, filas] = await Promise.all([
                this.db.chatSalida.orderBy("secuencia").toArray(),
                this.db.chatConversaciones.toArray(),
                this.db.chatMensajes.toArray(),
                this.db.chatMeta.toArray(),
            ]);
            const meta: Record<string, unknown> = { ...structuredClone(META_CHAT_INICIAL) };
            for (const fila of filas) {
                meta[fila.clave] = fila.valor;
            }
            return { salida, conversaciones, mensajes, meta: meta as unknown as MetaChat };
        });
    }

    async escribirChat(lote: LoteChat): Promise<void> {
        const t = [this.db.chatSalida, this.db.chatConversaciones, this.db.chatMensajes, this.db.chatBytes, this.db.chatMeta];
        await this.db.transaction("rw", t, async () => {
            await aplicar(this.db.chatSalida, lote.salida);
            await aplicar(this.db.chatConversaciones, lote.conversaciones);
            await aplicar(this.db.chatMensajes, lote.mensajes);
            await aplicar(this.db.chatBytes, lote.bytes);
            if (lote.meta) {
                await this.db.chatMeta.bulkPut(Object.entries(lote.meta).map(([clave, valor]) => ({ clave: clave as keyof MetaChat, valor })));
            }
        });
    }

    async leerBytesChat(id: string): Promise<Uint8Array | null> {
        const fila = await this.db.chatBytes.get(id);
        return fila ? fila.datos : null;
    }

    /** Borra la caché del chat (conversaciones, mensajes y cursor); la cola de salida queda. */
    async borrarCacheChat(): Promise<void> {
        await this.db.transaction("rw", [this.db.chatConversaciones, this.db.chatMensajes, this.db.chatMeta], async () => {
            await this.db.chatConversaciones.clear();
            await this.db.chatMensajes.clear();
            await this.db.chatMeta.bulkDelete(["cursor", "cargadas"]);
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
