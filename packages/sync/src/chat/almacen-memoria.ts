// Almacén del chat en memoria con copia profunda (como IndexedDB): pruebas y simular un reinicio
// de la app (otro motor con el mismo almacén).
import type { CambiosTabla } from "../almacen";
import { META_CHAT_INICIAL, type AlmacenChat, type ContenidoChat, type ConversacionChat, type LoteChat, type MensajeChat, type MetaChat, type SalidaChat } from "./tipos";

function aplicar<K extends string | number, T extends { id: K }>(tabla: Map<K, T>, cambios: CambiosTabla<T, K> | undefined): void {
    for (const id of cambios?.borrar ?? []) {
        tabla.delete(id);
    }
    for (const fila of cambios?.poner ?? []) {
        tabla.set(fila.id, structuredClone(fila));
    }
}

export class AlmacenChatMemoria implements AlmacenChat {
    readonly salida = new Map<string, SalidaChat>();
    readonly conversaciones = new Map<number, ConversacionChat>();
    readonly mensajes = new Map<string, MensajeChat>();
    readonly bytes = new Map<string, { id: string; datos: Uint8Array }>();
    meta: MetaChat = structuredClone(META_CHAT_INICIAL);
    /** Lotes escritos, en orden. */
    readonly escrituras: LoteChat[] = [];
    /** Si se asigna, la próxima escritura falla con este error. */
    fallarProxima: Error | null = null;

    cargarChat(): Promise<ContenidoChat> {
        return Promise.resolve(
            structuredClone({
                salida: [...this.salida.values()],
                conversaciones: [...this.conversaciones.values()],
                mensajes: [...this.mensajes.values()],
                meta: this.meta,
            }),
        );
    }

    escribirChat(lote: LoteChat): Promise<void> {
        if (this.fallarProxima) {
            const e = this.fallarProxima;
            this.fallarProxima = null;
            return Promise.reject(e);
        }
        this.escrituras.push(structuredClone(lote));
        aplicar(this.salida, lote.salida);
        aplicar(this.conversaciones, lote.conversaciones);
        aplicar(this.mensajes, lote.mensajes);
        aplicar(this.bytes, lote.bytes);
        if (lote.meta) {
            this.meta = { ...this.meta, ...structuredClone(lote.meta) };
        }
        return Promise.resolve();
    }

    leerBytesChat(id: string): Promise<Uint8Array | null> {
        const fila = this.bytes.get(id);
        return Promise.resolve(fila ? new Uint8Array(fila.datos) : null);
    }

    borrarCacheChat(): Promise<void> {
        this.conversaciones.clear();
        this.mensajes.clear();
        this.meta = { ...this.meta, cursor: null, cargadas: [] };
        return Promise.resolve();
    }
}
