import type { EventoBandeja } from "@/lib/conversaciones/tipos";
import type { EstadoFeed } from "@/lib/conversaciones/tiempo-real";

/*
 * Un solo flujo de eventos por pestaña (contratos/voz.md §3.10: «no se abre un segundo flujo»).
 * La bandeja ya abre el feed de conversaciones; cuando está montada lo anuncia aquí y reenvía sus
 * eventos, y el proveedor de voz los escucha. Fuera de la bandeja, si hace falta (este navegador
 * recibe llamadas o hay una viva), el proveedor abre el suyo.
 */

type Oyente = (evento: EventoBandeja) => void;
type OyenteFuente = () => void;

const oyentes = new Set<Oyente>();
const oyentesFuente = new Set<OyenteFuente>();
let fuentes = 0;
let estadoFuente: EstadoFeed | null = null;

/** La bandeja (u otra pantalla con el feed abierto) entrega cada evento que recibe. */
export function publicarEvento(evento: EventoBandeja): void {
    for (const o of oyentes) o(evento);
}

export function escucharEventos(o: Oyente): () => void {
    oyentes.add(o);
    return () => void oyentes.delete(o);
}

/** La bandeja anuncia que tiene el feed abierto; devuelve cómo retirarse. */
export function anunciarFuente(): { estado: (e: EstadoFeed) => void; retirar: () => void } {
    fuentes += 1;
    estadoFuente = "conectando";
    for (const o of oyentesFuente) o();
    let retirada = false;
    return {
        estado: (e) => {
            if (retirada) return;
            estadoFuente = e;
            for (const o of oyentesFuente) o();
        },
        retirar: () => {
            if (retirada) return;
            retirada = true;
            fuentes -= 1;
            if (fuentes === 0) estadoFuente = null;
            for (const o of oyentesFuente) o();
        },
    };
}

/** El estado del feed de otra pantalla, o `null` si nadie más lo tiene abierto. */
export const fuenteAjena = (): EstadoFeed | null => (fuentes > 0 ? estadoFuente : null);

export function escucharFuente(o: OyenteFuente): () => void {
    oyentesFuente.add(o);
    return () => void oyentesFuente.delete(o);
}
