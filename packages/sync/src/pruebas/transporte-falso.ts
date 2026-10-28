import type {
    EstadoOrden,
    OperacionCable,
    PaqueteDia,
    PeticionLote,
    RespuestaCambios,
    RespuestaLote,
    ResultadoOperacion,
} from "../contrato-sync";
import { ErrorTransporte } from "../errores";
import type { RespuestaApi, Transporte } from "../transporte";

const ESTADO_TRAS: Partial<Record<string, EstadoOrden>> = {
    "orden.iniciar_camino": "en_camino",
    "orden.llegar": "en_sitio",
    "orden.cerrar": "cerrada",
    "orden.fallar": "fallida",
};

export type Llamada =
    | { tipo: "lote"; peticion: PeticionLote }
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
    /** Hora del servidor que se informa (`null` = sin cabecera `Date`). */
    horaServidor: number | null = null;
    /** Si se asigna, las próximas N llamadas fallan con este error. */
    private fallas: ErrorTransporte[] = [];
    /** Versión y estado por orden que lleva el servidor falso. */
    readonly ordenes = new Map<string, { version: number; estado: EstadoOrden }>();

    /** Resultado por defecto: aplica la operación (versión + 1 y estado según el tipo). */
    aplicar(op: OperacionCable): ResultadoOperacion {
        const previa = this.ordenes.get(op.ordenId) ?? { version: op.versionBase, estado: "programada" as EstadoOrden };
        const orden = { version: previa.version + 1, estado: ESTADO_TRAS[op.tipo] ?? previa.estado };
        this.ordenes.set(op.ordenId, orden);
        return { id: op.id, estado: "aplicada", orden };
    }

    lote: Respondedor<PeticionLote, RespuestaLote> = (p) => ({ resultados: p.operaciones.map((op) => this.aplicar(op)) });
    paquete: Respondedor<string, PaqueteDia> = (fecha) => ({ fecha, ordenes: [], existencias: [], ruta: null, cursor: "c0" });
    cambios: Respondedor<string, RespuestaCambios> = (cursor) => ({ cambios: [], cursor, hayMas: false });

    fallar(veces: number, error: ErrorTransporte = new ErrorTransporte("red", "Sin señal")): void {
        for (let i = 0; i < veces; i += 1) {
            this.fallas.push(error);
        }
    }

    get lotes(): PeticionLote[] {
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

    enviarLote(peticion: PeticionLote): Promise<RespuestaApi<RespuestaLote>> {
        return this.responder({ tipo: "lote", peticion }, this.lote, peticion);
    }

    obtenerPaquete(fecha: string): Promise<RespuestaApi<PaqueteDia>> {
        return this.responder({ tipo: "paquete", fecha }, this.paquete, fecha);
    }

    obtenerCambios(cursor: string): Promise<RespuestaApi<RespuestaCambios>> {
        return this.responder({ tipo: "cambios", cursor }, this.cambios, cursor);
    }
}
