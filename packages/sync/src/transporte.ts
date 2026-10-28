// Interfaces de red del motor: se inyectan, así el núcleo no sabe de `fetch`, cookies ni CSRF.
// Implementación sobre el cliente `openapi-fetch` de `@iris/contratos` en `http.ts`; en pruebas,
// `TransporteFalso` y `SubidorFalso`.
import type { Adjunto, Cambios, EntradaLote, EntradaRegistrarAdjunto, Paquete, RespuestaLote } from "./contrato";

export interface RespuestaApi<T> {
    datos: T;
    /**
     * Hora del servidor (ms) para medir el desfase del reloj: `servidorEn` del cuerpo si lo trae
     * (paquete y lote) o la cabecera `Date`.
     */
    horaServidor: number | null;
}

/** Llamadas a la API de campo. Toda falla se lanza como `ErrorTransporte` (`red` o `http`). */
export interface Transporte {
    /** `POST /api/v1/sync/lote` */
    enviarLote(peticion: EntradaLote): Promise<RespuestaApi<RespuestaLote>>;
    /** `GET /api/v1/campo/paquete?fecha=` */
    obtenerPaquete(fecha: string): Promise<RespuestaApi<Paquete>>;
    /** `GET /api/v1/campo/cambios?cursor=` */
    obtenerCambios(cursor: string): Promise<RespuestaApi<Cambios>>;
}

/** Subida reanudable tus 1.0 de adjuntos (I1). Toda falla se lanza como `ErrorTransporte`. */
export interface Subidor {
    /** `POST /api/v1/adjuntos`; idempotente por `id` (repetirlo devuelve lo recibido). */
    crear(entrada: EntradaRegistrarAdjunto): Promise<Adjunto>;
    /** `HEAD /api/v1/adjuntos/{id}/subida` → `Upload-Offset`. */
    consultarOffset(id: string): Promise<number>;
    /** `PATCH /api/v1/adjuntos/{id}/subida` desde `offset` → nuevo `Upload-Offset`. */
    enviarParte(id: string, offset: number, parte: Uint8Array): Promise<number>;
}
