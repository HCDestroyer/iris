import type { Cambios, EntradaLote, EstadoOrden, OperacionLote, Paquete, RespuestaLote, ResultadoOperacion } from "../contrato";
import { ErrorTransporte } from "../errores";
import type { RespuestaApi, Transporte } from "../transporte";

const ESTADO_TRAS: Partial<Record<string, EstadoOrden>> = {
    "orden.iniciar_camino": "en_camino",
    "orden.llegar": "en_sitio",
    "orden.cerrar": "cerrada",
    "orden.fallar": "fallida",
};

export type Llamada =
    | { tipo: "lote"; peticion: EntradaLote }
    | { tipo: "paquete"; fecha: string }
    | { tipo: "cambios"; cursor: string };

type Respondedor<E, S> = (entrada: E) => S | Promise<S>;

/**
 * Transporte en memoria. Por defecto aplica todo el lote (`aplicada`, versión +1) y no hay
 * cambios. Cada respuesta se puede reemplazar; `sinRed` hace fallar todo con error de red.
 */
export class TransporteFalso implements Transporte {
    readonly llamadas: Llamada[] = [];
    sinRed = false;
    /** Hora del servidor que se informa (`null` = sin hora). */
    horaServidor: number | null = null;
    /** Si se asigna, las próximas N llamadas fallan con este error. */
    private fallas: ErrorTransporte[] = [];
    /** Versión y estado por orden que lleva el servidor falso. */
    readonly ordenes = new Map<number, { version: number; estado: EstadoOrden }>();

    /** Resultado por defecto: aplica la operación (versión + 1 y estado según el tipo). */
    aplicar(op: OperacionLote): ResultadoOperacion {
        const previa = this.ordenes.get(op.ordenId) ?? { version: op.versionBase ?? 1, estado: "programada" as EstadoOrden };
        const orden = { version: previa.version + 1, estado: ESTADO_TRAS[op.tipo] ?? previa.estado };
        this.ordenes.set(op.ordenId, orden);
        return { id: op.id, estado: "aplicada", orden };
    }

    lote: Respondedor<EntradaLote, Omit<RespuestaLote, "servidorEn" | "cursor" | "desfaseMs">> = (p) => ({
        resultados: (p.operaciones ?? []).map((op) => this.aplicar(op)),
    });
    paquete: Respondedor<string, Paquete> = (fecha) => ({
        fecha,
        servidorEn: "2026-10-28T15:00:00Z",
        ordenes: [],
        existencias: { disponible: false, motivo: "sin_conector", bodegas: [] },
        cursor: "c0",
    });
    cambios: Respondedor<string, Cambios> = (cursor) => ({ cambios: [], cursor, hayMas: false });

    fallar(veces: number, error: ErrorTransporte = new ErrorTransporte("red", "Sin señal")): void {
        for (let i = 0; i < veces; i += 1) {
            this.fallas.push(error);
        }
    }

    get lotes(): EntradaLote[] {
        return this.llamadas.flatMap((l) => (l.tipo === "lote" ? [l.peticion] : []));
    }

    private async responder<E, S>(llamada: Llamada, fn: Respondedor<E, S>, entrada: E): Promise<RespuestaApi<S>> {
        this.llamadas.push(structuredClone(llamada));
        const falla = this.fallas.shift();
        if (falla) {
            throw falla;
        }
        if (this.sinRed) {
            throw new ErrorTransporte("red", "Sin señal");
        }
        return { datos: structuredClone(await fn(entrada)), horaServidor: this.horaServidor };
    }

    async enviarLote(peticion: EntradaLote): Promise<RespuestaApi<RespuestaLote>> {
        const r = await this.responder({ tipo: "lote", peticion }, this.lote, peticion);
        const servidorEn = new Date(this.horaServidor ?? Date.parse("2026-10-28T15:00:00Z")).toISOString();
        return { ...r, datos: { cursor: peticion.cursor ?? "", desfaseMs: 0, servidorEn, ...r.datos } };
    }

    obtenerPaquete(fecha: string): Promise<RespuestaApi<Paquete>> {
        return this.responder({ tipo: "paquete", fecha }, this.paquete, fecha);
    }

    obtenerCambios(cursor: string): Promise<RespuestaApi<Cambios>> {
        return this.responder({ tipo: "cambios", cursor }, this.cambios, cursor);
    }
}
