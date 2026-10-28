// Interfaces de red del motor: se inyectan, así el núcleo no sabe de `fetch`, cookies ni CSRF.
// Implementación con `fetch` en `http.ts`; en pruebas, `TransporteFalso` y `SubidorFalso`.
import type {
    EntradaCrearAdjunto,
    PaqueteDia,
    PeticionLote,
    RespuestaCambios,
    RespuestaCrearAdjunto,
    RespuestaLote,
} from "./contrato-sync";

export interface RespuestaApi<T> {
    datos: T;
    /** Hora del servidor en la respuesta (cabecera `Date`), para medir el desfase del reloj. */
    horaServidor: number | null;
}

/**
 * Llamadas a la API de campo. Toda falla se lanza como `ErrorTransporte` (`red` o `http`).
 */
export interface Transporte {
    /** `POST /api/v1/sync/lote` */
    enviarLote(peticion: PeticionLote): Promise<RespuestaApi<RespuestaLote>>;
    /** `GET /api/v1/campo/paquete?fecha=` */
    obtenerPaquete(fecha: string): Promise<RespuestaApi<PaqueteDia>>;
    /** `GET /api/v1/campo/cambios?cursor=` */
    obtenerCambios(cursor: string): Promise<RespuestaApi<RespuestaCambios>>;
}

/**
 * Subida reanudable estilo tus 1.0 (I1). Toda falla se lanza como `ErrorTransporte`.
 */
export interface Subidor {
    /** `POST /api/v1/adjuntos`; idempotente por `id` (repetirlo devuelve la misma URL). */
    crear(entrada: EntradaCrearAdjunto): Promise<RespuestaCrearAdjunto>;
    /** `HEAD urlSubida` → `Upload-Offset`. */
    consultarOffset(urlSubida: string): Promise<number>;
    /** `PATCH urlSubida` con `Upload-Offset: offset` → nuevo `Upload-Offset`. */
    enviarParte(urlSubida: string, offset: number, parte: Uint8Array): Promise<number>;
}
