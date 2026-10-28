// Interfaz de persistencia del motor. Adaptadores: `AlmacenMemoria` (pruebas) y
// `AlmacenDexie` (IndexedDB, en `@iris/sync/dexie`). La app de Expo trae el suyo (SQLite).
import type { AdjuntoLocal, MetaSync, OperacionLocal, OrdenLocal } from "./modelo";

export interface CambiosTabla<T> {
    poner?: T[];
    borrar?: string[];
}

/** Escritura atómica: o se guarda todo el lote o nada. */
export interface LoteEscritura {
    operaciones?: CambiosTabla<OperacionLocal>;
    ordenes?: CambiosTabla<OrdenLocal>;
    adjuntos?: CambiosTabla<AdjuntoLocal>;
    bytes?: CambiosTabla<{ id: string; datos: Uint8Array }>;
    meta?: Partial<MetaSync>;
}

export interface ContenidoAlmacen {
    operaciones: OperacionLocal[];
    ordenes: OrdenLocal[];
    adjuntos: AdjuntoLocal[];
    meta: MetaSync;
}

export interface Almacen {
    /** Todo menos los bytes de los adjuntos. */
    cargar(): Promise<ContenidoAlmacen>;
    escribir(lote: LoteEscritura): Promise<void>;
    leerBytes(id: string): Promise<Uint8Array | null>;
}
