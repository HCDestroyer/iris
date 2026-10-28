// Registros locales que guarda el teléfono (en el `Almacen`).
import type {
    DatosPorTipo,
    Existencia,
    OrdenCampo,
    Problema,
    RutaPublicada,
    TipoAdjunto,
    TipoOperacion,
} from "./contrato-sync";

/**
 * - `pendiente`: por enviar (o bloqueada detrás de un rechazo de su orden).
 * - `rechazada`: la API la rechazó; espera que el técnico la descarte o la rehaga.
 * - `esperando_adjuntos`: `orden.cerrar` rechazada con `faltan_adjuntos` cuyos adjuntos aún se
 *   suben desde este teléfono; vuelve sola a `pendiente` cuando terminan.
 */
export type EstadoOperacionLocal = "pendiente" | "rechazada" | "esperando_adjuntos";

export type OperacionLocal = {
    [T in TipoOperacion]: {
        id: string;
        tipo: T;
        ordenId: string;
        versionBase: number;
        ocurridoEn: string;
        datos: DatosPorTipo[T];
        /** Orden de la cola: se conserva al rehacer o reintentar con un id nuevo. */
        secuencia: number;
        estado: EstadoOperacionLocal;
        error: Problema | null;
        /** Veces que salió en un lote sin respuesta definitiva. */
        intentos: number;
    };
}[TipoOperacion];

export interface OrdenLocal {
    id: string;
    /** Última versión conocida del servidor (paquete, cambios o resultados del lote). */
    base: OrdenCampo;
    /** El servidor la quitó de este técnico; se conserva mientras haya operaciones suyas. */
    reasignada: boolean;
}

export type EstadoSubida = "por_crear" | "subiendo" | "subido";

export interface AdjuntoLocal {
    id: string;
    ordenId: string;
    tipo: TipoAdjunto;
    /** El checklist que lo exige (ítem tipo `foto`), si aplica. */
    checklistItemId: string | null;
    obligatorio: boolean;
    mime: string;
    tamano: number;
    sha256: string;
    estado: EstadoSubida;
    urlSubida: string | null;
    /** Bytes confirmados por el servidor. */
    offset: number;
    intentos: number;
    ultimoError: string | null;
}

export interface MetaSync {
    dispositivoId: string | null;
    /** Contador de `secuencia` de la cola. */
    secuencia: number;
    /** Hora del servidor − hora del teléfono, medida en el último contacto (ms). */
    desfaseMs: number;
    cursor: string | null;
    fecha: string | null;
    existencias: Existencia[];
    ruta: RutaPublicada | null;
    /** Último contacto correcto con la API (hora del teléfono). */
    ultimaSincronizacion: number | null;
}

export const META_INICIAL: MetaSync = {
    dispositivoId: null,
    secuencia: 0,
    desfaseMs: 0,
    cursor: null,
    fecha: null,
    existencias: [],
    ruta: null,
    ultimaSincronizacion: null,
};
