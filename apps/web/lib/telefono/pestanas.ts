/*
 * Una sola pestaña con el teléfono (contratos/voz.md §3.12, E4c: «suena en una sola pestaña»). El
 * servidor admite una conexión por persona (la nueva reemplaza a la vieja con `4001`); aquí se evita
 * que dos pestañas se la quiten una a la otra:
 *
 * - La pestaña con el teléfono tiene un candado (`navigator.locks`, por persona). Otra pestaña que
 *   abre Iris ve el candado tomado y muestra «En otra pestaña» sin conectarse.
 * - «Usar el teléfono aquí» avisa por `BroadcastChannel`: la que lo tenía suelta el teléfono y el
 *   candado, y esta lo toma.
 *
 * Sin `navigator.locks` (navegadores viejos) cada pestaña se conecta y manda el servidor: la última
 * gana y las demás reciben `4001`.
 */

export type Pestanas = {
    /** Toma el teléfono si nadie lo tiene. */
    tomar: () => Promise<boolean>;
    /** Pide a la otra pestaña que lo suelte y lo toma (espera hasta `esperaMs`). */
    quitar: (esperaMs?: number) => Promise<boolean>;
    soltar: () => void;
    /** Hay otra pestaña con el teléfono. */
    otraLoTiene: () => Promise<boolean>;
    cerrar: () => void;
};

type Mensaje = { tipo: "ceder"; de: string };

const idAzar = () => Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) => b.toString(16).padStart(2, "0")).join("");

export function coordinarPestanas(usuarioId: number, alPerder: () => void): Pestanas {
    const nombre = `iris-telefono-${usuarioId}`;
    const yo = idAzar();
    const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
    const canal = typeof BroadcastChannel !== "undefined" ? new BroadcastChannel(nombre) : null;
    const tenencia: { liberar: (() => void) | null } = { liberar: null };
    const soltar = () => tenencia.liberar?.();
    let cerrado = false;

    function sostener(lock: Lock | null): Promise<void> | boolean {
        if (!lock) return false;
        return new Promise<void>((resolve) => {
            tenencia.liberar = () => {
                tenencia.liberar = null;
                resolve();
            };
        });
    }

    async function pedir(opciones: LockOptions): Promise<boolean> {
        if (!locks) return true;
        if (tenencia.liberar) return true;
        const listo = new Promise<boolean>((resolve) => {
            void locks
                .request(nombre, opciones, (lock) => {
                    resolve(lock !== null);
                    return sostener(lock);
                })
                .catch(() => resolve(false));
        });
        const r = await listo;
        if (r && cerrado) {
            soltar();
            return false;
        }
        return r;
    }

    if (canal) {
        canal.onmessage = (e: MessageEvent<Mensaje>) => {
            if (e.data?.tipo === "ceder" && e.data.de !== yo && tenencia.liberar) {
                alPerder();
                tenencia.liberar();
            }
        };
    }

    return {
        tomar: () => pedir({ ifAvailable: true }),
        quitar: async (esperaMs = 3000) => {
            if (!locks) return true;
            canal?.postMessage({ tipo: "ceder", de: yo } satisfies Mensaje);
            const control = new AbortController();
            const t = globalThis.setTimeout(() => control.abort(), esperaMs);
            try {
                return await pedir({ signal: control.signal });
            } finally {
                globalThis.clearTimeout(t);
            }
        },
        soltar,
        otraLoTiene: async () => {
            if (!locks || tenencia.liberar) return false;
            try {
                const estado = await locks.query();
                return (estado.held ?? []).some((l) => l.name === nombre);
            } catch {
                return false;
            }
        },
        cerrar: () => {
            cerrado = true;
            soltar();
            canal?.close();
        },
    };
}
