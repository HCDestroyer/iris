import type { Reloj } from "../reloj";

interface Temporizador {
    vence: number;
    resolver: () => void;
}

/** Reloj que solo avanza con `avanzar`; registra cada espera pedida. */
export class RelojManual implements Reloj {
    private ms: number;
    private temporizadores: Temporizador[] = [];
    /** Duraciones pedidas a `esperar`, en orden. */
    readonly esperas: number[] = [];

    constructor(inicio: number | string = "2026-10-28T15:00:00Z") {
        this.ms = typeof inicio === "number" ? inicio : Date.parse(inicio);
    }

    ahora(): number {
        return this.ms;
    }

    esperar(ms: number, senal?: AbortSignal): Promise<void> {
        this.esperas.push(ms);
        return new Promise((resolver) => {
            if (senal?.aborted) {
                resolver();
                return;
            }
            const t: Temporizador = { vence: this.ms + ms, resolver };
            this.temporizadores.push(t);
            senal?.addEventListener(
                "abort",
                () => {
                    this.temporizadores = this.temporizadores.filter((x) => x !== t);
                    resolver();
                },
                { once: true },
            );
        });
    }

    /** Temporizadores pendientes. */
    get pendientes(): number {
        return this.temporizadores.length;
    }

    /** Mueve el reloj y despierta lo vencido; luego deja correr las promesas. */
    async avanzar(ms: number): Promise<void> {
        this.ms += ms;
        const vencidos = this.temporizadores.filter((t) => t.vence <= this.ms);
        this.temporizadores = this.temporizadores.filter((t) => t.vence > this.ms);
        for (const t of vencidos) {
            t.resolver();
        }
        await vaciarPromesas();
    }

    /** Solo mueve la hora (sin despertar). */
    fijar(ms: number): void {
        this.ms = ms;
    }
}

/** Deja correr todas las promesas pendientes (varias vueltas de la cola de tareas real). */
export async function vaciarPromesas(vueltas = 5): Promise<void> {
    for (let i = 0; i < vueltas; i += 1) {
        await new Promise<void>((resolver) => setTimeout(resolver, 0));
    }
}
