import type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
import { META_INICIAL, type AdjuntoLocal, type MetaSync, type OperacionLocal, type OrdenLocal } from "./modelo";
import type { AlmacenPosiciones, CambiosPosiciones, PosicionLocal } from "./posiciones";

function aplicar<K extends string | number, T extends { id: K }>(tabla: Map<K, T>, cambios: CambiosTabla<T, K> | undefined): void {
    for (const id of cambios?.borrar ?? []) {
        tabla.delete(id);
    }
    for (const fila of cambios?.poner ?? []) {
        tabla.set(fila.id, structuredClone(fila));
    }
}

/**
 * Almacén en memoria con copia profunda (como IndexedDB: lo guardado no cambia si el llamador
 * muta su objeto). Sirve para pruebas y para simular un reinicio de la app: crea otro motor con
 * el mismo almacén.
 */
export class AlmacenMemoria implements Almacen, AlmacenPosiciones {
    readonly operaciones = new Map<string, OperacionLocal>();
    readonly ordenes = new Map<number, OrdenLocal>();
    readonly adjuntos = new Map<string, AdjuntoLocal>();
    readonly bytes = new Map<string, { id: string; datos: Uint8Array }>();
    readonly posiciones = new Map<number, PosicionLocal>();
    meta: MetaSync = structuredClone(META_INICIAL);
    /** Lotes escritos, en orden (para afirmar «persistida antes de enviar»). */
    readonly escrituras: LoteEscritura[] = [];
    /** Si se asigna, la próxima escritura falla con este error. */
    fallarProxima: Error | null = null;

    cargar(): Promise<ContenidoAlmacen> {
        return Promise.resolve(
            structuredClone({
                operaciones: [...this.operaciones.values()],
                ordenes: [...this.ordenes.values()],
                adjuntos: [...this.adjuntos.values()],
                meta: this.meta,
            }),
        );
    }

    escribir(lote: LoteEscritura): Promise<void> {
        if (this.fallarProxima) {
            const error = this.fallarProxima;
            this.fallarProxima = null;
            return Promise.reject(error);
        }
        this.escrituras.push(structuredClone(lote));
        aplicar(this.operaciones, lote.operaciones);
        aplicar(this.ordenes, lote.ordenes);
        aplicar(this.adjuntos, lote.adjuntos);
        aplicar(this.bytes, lote.bytes);
        if (lote.meta) {
            this.meta = { ...this.meta, ...structuredClone(lote.meta) };
        }
        return Promise.resolve();
    }

    leerBytes(id: string): Promise<Uint8Array | null> {
        const fila = this.bytes.get(id);
        return Promise.resolve(fila ? fila.datos.slice() : null);
    }

    cargarPosiciones(): Promise<PosicionLocal[]> {
        return Promise.resolve(structuredClone([...this.posiciones.values()]));
    }

    escribirPosiciones(cambios: CambiosPosiciones): Promise<void> {
        if (this.fallarProxima) {
            const error = this.fallarProxima;
            this.fallarProxima = null;
            return Promise.reject(error);
        }
        for (const ms of cambios.borrar ?? []) {
            this.posiciones.delete(ms);
        }
        for (const punto of cambios.poner ?? []) {
            this.posiciones.set(punto.ms, structuredClone(punto));
        }
        return Promise.resolve();
    }
}
