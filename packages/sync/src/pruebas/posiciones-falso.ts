import type { EntradaPosiciones, RespuestaPosiciones } from "../contrato";
import { ErrorTransporte } from "../errores";
import type { TransportePosiciones } from "../posiciones";
import type { Reloj } from "../reloj";
import type { RespuestaApi } from "../transporte";

const MINUTO_MS = 60_000;

/**
 * `POST /campo/posiciones` en memoria con las reglas de I2: hasta `lotesPorMinuto` lotes por
 * minuto (si no, 429 `demasiadas_posiciones` con `Retry-After`), misma hora = duplicada y más de
 * 24 h = descartada (`vieja`). `sinRed` falla con error de red; `fallar` encola errores.
 */
export class TransportePosicionesFalso implements TransportePosiciones {
    readonly lotes: EntradaPosiciones[] = [];
    /** Peticiones que llegaron (también las que respondieron 429). */
    readonly intentos: number[] = [];
    /** Horas guardadas (ms de `registradaEn`). */
    readonly guardadas = new Set<number>();
    sinRed = false;
    lotesPorMinuto = 6;
    /** Valor de `Retry-After` del 429 (`null` = sin cabecera). */
    retryAfter: string | null = "20";
    private fallas: ErrorTransporte[] = [];

    constructor(private readonly reloj: Reloj) {}

    fallar(veces: number, error: ErrorTransporte = new ErrorTransporte("red", "Sin señal")): void {
        for (let i = 0; i < veces; i += 1) {
            this.fallas.push(error);
        }
    }

    enviarPosiciones(entrada: EntradaPosiciones): Promise<RespuestaApi<RespuestaPosiciones>> {
        const ahora = this.reloj.ahora();
        if (this.sinRed) {
            return Promise.reject(new ErrorTransporte("red", "Sin señal"));
        }
        const falla = this.fallas.shift();
        if (falla) {
            return Promise.reject(falla);
        }
        this.intentos.push(ahora);
        if (this.intentos.filter((t) => t > ahora - MINUTO_MS).length > this.lotesPorMinuto) {
            const cabeceras = new Headers(this.retryAfter === null ? {} : { "Retry-After": this.retryAfter });
            const problema = { type: "urn:iris:problema:demasiadas_posiciones", title: "Demasiados lotes", status: 429 };
            return Promise.reject(new ErrorTransporte("http", problema.title, 429, problema, cabeceras));
        }
        this.lotes.push(structuredClone(entrada));
        const r: RespuestaPosiciones = { aceptadas: 0, duplicadas: 0, descartadas: 0, motivos: {}, servidorEn: new Date(ahora).toISOString(), desfaseMs: 0 };
        for (const p of entrada.puntos ?? []) {
            const ms = Date.parse(p.registradaEn);
            if (ms < ahora - 24 * 60 * MINUTO_MS) {
                r.descartadas += 1;
                r.motivos.vieja = (r.motivos.vieja ?? 0) + 1;
            } else if (this.guardadas.has(ms)) {
                r.duplicadas += 1;
            } else {
                this.guardadas.add(ms);
                r.aceptadas += 1;
            }
        }
        return Promise.resolve({ datos: r, horaServidor: ahora });
    }
}
