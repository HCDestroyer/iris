// Reloj inyectable: el motor nunca llama a `Date.now` ni a `setTimeout` directo, para probar
// esperas y caducidad sin tiempo real (ver `RelojManual` en `@iris/sync/pruebas`).

export interface Reloj {
    /** Milisegundos desde la época, hora del teléfono. */
    ahora(): number;
    /** Resuelve tras `ms`, o antes si `senal` se aborta. */
    esperar(ms: number, senal?: AbortSignal): Promise<void>;
}

export const relojSistema: Reloj = {
    ahora: () => Date.now(),
    esperar: (ms, senal) =>
        new Promise<void>((resolver) => {
            if (senal?.aborted) {
                resolver();
                return;
            }
            const t = setTimeout(() => {
                senal?.removeEventListener("abort", alAbortar);
                resolver();
            }, ms);
            const alAbortar = () => {
                clearTimeout(t);
                resolver();
            };
            senal?.addEventListener("abort", alAbortar, { once: true });
        }),
};

export interface OpcionesEspera {
    /** Primera espera (ms). */
    baseMs: number;
    /** Tope de la espera (ms). */
    maximoMs: number;
    /** Número en [0, 1): `Math.random` por defecto. */
    azar: () => number;
}

export const ESPERA_POR_DEFECTO: OpcionesEspera = { baseMs: 2_000, maximoMs: 5 * 60_000, azar: Math.random };

/**
 * Espera exponencial con jitter «igual»: la mitad fija y la mitad al azar de min(tope, base·2^n).
 * `intento` empieza en 0 (primer reintento).
 */
export function calcularEspera(intento: number, opciones: OpcionesEspera = ESPERA_POR_DEFECTO): number {
    const techo = Math.min(opciones.maximoMs, opciones.baseMs * 2 ** Math.max(0, intento));
    return Math.round(techo / 2 + opciones.azar() * (techo / 2));
}
