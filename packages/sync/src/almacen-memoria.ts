import type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
import { META_INICIAL, type AdjuntoLocal, type MetaSync, type OperacionLocal, type OrdenLocal } from "./modelo";

function aplicar<T extends { id: string }>(tabla: Map<string, T>, cambios: CambiosTabla<T> | undefined): void {
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
export class AlmacenMemoria implements Almacen {
    readonly operaciones = new Map<string, OperacionLocal>();
    readonly ordenes = new Map<string, OrdenLocal>();
    readonly adjuntos = new Map<string, AdjuntoLocal>();
    readonly bytes = new Map<string, { id: string; datos: Uint8Array }>();
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
}
