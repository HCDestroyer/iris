// «Eliminar» con «Deshacer» en el panel (`contratos/chat.md` §3.1): el servidor no restaura, así que
// la llamada se retrasa 5 s en el cliente. Mientras tanto el mensaje se ve eliminado aquí (y
// todavía no para los demás); «Deshacer» cancela el temporizador sin que nada salga. Si la página
// se cierra u oculta con una eliminación pendiente, sale ya con `keepalive`.
import { PLAZO_ELIMINAR_MS } from "@iris/sync/chat";

export const ESPERA_DESHACER_MS = 5_000;
/** No se ofrece «Eliminar» si al mensaje le quedan menos de 10 s de los 15 min (§3.1.5). */
export const MARGEN_ELIMINAR_MS = 10_000;

export function sePuedeEliminar(m: { propio: boolean; eliminado: boolean; envio: string; enviadoEn: string | null }, ahora: number): boolean {
    const enviado = m.enviadoEn ? Date.parse(m.enviadoEn) : Number.NaN;
    return m.propio && !m.eliminado && m.envio === "enviado" && !Number.isNaN(enviado) && ahora - enviado <= PLAZO_ELIMINAR_MS - MARGEN_ELIMINAR_MS;
}

export type MensajeEliminable = { id: string; propio: boolean; eliminado: boolean; envio: string; enviadoEn: string | null };

export type OpcionesEliminaciones = {
    /** La eliminación de verdad (la cola del motor), si el plazo todavía alcanza. */
    ejecutar: (mensajeId: string) => void;
    /** Pasó la espera pero ya no está dentro del plazo: no sale (el mensaje sigue visible). */
    alVencer: (mensajeId: string) => void;
    /** Al cerrar u ocultar la página: `fetch` con `keepalive`. */
    alSalir: (mensajeId: string) => void;
    alCambiar: () => void;
    esperaMs?: number;
    ahora?: () => number;
    programar?: (fn: () => void, ms: number) => unknown;
    cancelar?: (id: unknown) => void;
};

export class EliminacionesDiferidas {
    private readonly temporizadores = new Map<string, unknown>();
    private readonly mensajes = new Map<string, MensajeEliminable>();
    private readonly o: Required<OpcionesEliminaciones>;
    private instantanea: ReadonlySet<string> = new Set();

    constructor(o: OpcionesEliminaciones) {
        this.o = {
            esperaMs: ESPERA_DESHACER_MS,
            ahora: () => Date.now(),
            programar: (fn, ms) => setTimeout(fn, ms),
            cancelar: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
            ...o,
        };
    }

    /** Mensajes que se ven eliminados y aún esperan (el mismo objeto hasta un cambio). */
    get pendientes(): ReadonlySet<string> {
        return this.instantanea;
    }

    private cambio(): void {
        this.instantanea = new Set(this.temporizadores.keys());
        this.o.alCambiar();
    }

    /** Pide eliminar `m` (vuelve a comprobar el plazo). Devuelve si quedó pedido. */
    pedir(m: MensajeEliminable): boolean {
        const mensajeId = m.id;
        if (!sePuedeEliminar(m, this.o.ahora())) return false;
        if (this.temporizadores.has(mensajeId)) return true;
        this.mensajes.set(mensajeId, m);
        this.temporizadores.set(
            mensajeId,
            this.o.programar(() => this.ahora(mensajeId), this.o.esperaMs),
        );
        this.cambio();
        return true;
    }

    /** «Deshacer»: nada sale. Devuelve si había algo que deshacer. */
    deshacer(mensajeId: string): boolean {
        const t = this.temporizadores.get(mensajeId);
        if (t === undefined) return false;
        this.o.cancelar(t);
        this.temporizadores.delete(mensajeId);
        this.mensajes.delete(mensajeId);
        this.cambio();
        return true;
    }

    /** Pasó la espera (o se cerró el aviso): elimina ya. */
    ahora(mensajeId: string): void {
        const t = this.temporizadores.get(mensajeId);
        if (t === undefined) return;
        this.o.cancelar(t);
        this.temporizadores.delete(mensajeId);
        const m = this.mensajes.get(mensajeId);
        this.mensajes.delete(mensajeId);
        if (m && !sePuedeEliminar(m, this.o.ahora())) this.o.alVencer(mensajeId);
        else this.o.ejecutar(mensajeId);
        this.cambio();
    }

    /**
     * Cerrar sesión u ocultar la pestaña: cancela las esperas y devuelve lo pendiente para que salga
     * ya por la cola del motor (que verifica la sesión antes de enviar).
     */
    sacarTodo(): string[] {
        const ids = [...this.temporizadores.keys()];
        if (ids.length === 0) return ids;
        for (const t of this.temporizadores.values()) this.o.cancelar(t);
        this.temporizadores.clear();
        this.mensajes.clear();
        this.cambio();
        return ids;
    }

    /** La página se descarga (`pagehide`) o se desmonta el panel: todo lo pendiente sale ya con `keepalive`. */
    vaciarAlSalir(): void {
        if (this.temporizadores.size === 0) return;
        for (const [id, t] of this.temporizadores) {
            this.o.cancelar(t);
            this.o.alSalir(id);
        }
        this.temporizadores.clear();
        this.mensajes.clear();
        this.cambio();
    }
}
