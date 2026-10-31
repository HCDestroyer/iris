// Registros locales que guarda el teléfono (en el `Almacen`).
import type {
    AvisoResultado,
    DatosPorTipo,
    EntradaRecibirTraslado,
    Existencias,
    OrdenCampo,
    PlantillaFormulario,
    Problema,
    RutaCampo,
    TipoAdjunto,
    TipoOperacion,
    TurnoCampo,
} from "./contrato";

/**
 * - `pendiente`: por enviar (o retenida detrás de un rechazo de su orden).
 * - `rechazada`: la API la rechazó; espera que el técnico la descarte o la rehaga.
 * - `esperando_adjuntos`: `orden.cerrar` rechazada con `faltan_adjuntos` cuyos adjuntos aún se
 *   suben desde este teléfono; vuelve sola a `pendiente` cuando terminan.
 */
export type EstadoOperacionLocal = "pendiente" | "rechazada" | "esperando_adjuntos";

/** Lo que comparte toda operación de la cola. */
interface OperacionBase {
    /** UUID v7 del teléfono: idempotencia en la API. */
    id: string;
    /** Hora del teléfono sin corregir (la API la corrige con `enviadoEn`). */
    ocurridoEn: string;
    secuencia: number;
    estado: EstadoOperacionLocal;
    error: Problema | null;
    intentos: number;
    esperaHasta?: number;
    esperaDesde?: number;
    esperas?: number;
}

/**
 * Clave de grupo de una recepción de traslado en la cola: `-trasladoId` (como `grupoDe` en la API).
 * Negativa para no chocar nunca con una orden (ni con `0`): la espera propia y los rechazos detienen
 * solo lo que sigue de ese mismo traslado.
 */
export function grupoTraslado(trasladoId: number): number {
    return -Math.abs(trasladoId);
}

/** `traslado.recibir` en la cola: no es de una orden; `ordenId` lleva la clave de grupo negativa. */
export interface OperacionTrasladoLocal extends OperacionBase {
    tipo: "traslado.recibir";
    /** `grupoTraslado(datos.trasladoId)`: siempre negativa. */
    ordenId: number;
    /** Sin versión: siempre 0 (no viaja en el lote). */
    versionBase: 0;
    datos: EntradaRecibirTraslado;
    plantilla?: undefined;
}

export type OperacionLocal = OperacionOrdenLocal | OperacionTrasladoLocal;

/** Operación de una orden (todas menos `traslado.recibir`). */
export type OperacionOrdenLocal = {
    [T in TipoOperacion]: {
        /** UUID v7 del teléfono: idempotencia en la API. */
        id: string;
        tipo: T;
        ordenId: number;
        versionBase: number;
        /** Hora del teléfono sin corregir (la API la corrige con `enviadoEn`). */
        ocurridoEn: string;
        datos: DatosPorTipo[T];
        /** Orden de la cola: se conserva al rehacer o reintentar con un id nuevo. */
        secuencia: number;
        estado: EstadoOperacionLocal;
        error: Problema | null;
        /** Veces que salió en un lote sin respuesta definitiva. */
        intentos: number;
        /**
         * Espera propia de la operación (`CODIGOS_ESPERA_OPERACION`): no sale antes de `esperaHasta`
         * (ms del reloj del motor); `esperaDesde` es la primera respuesta de espera (tope
         * `ESPERA_OPERACION_MAX_MS`) y `esperas` cuántas lleva. Ausentes si no espera.
         */
        esperaHasta?: number;
        esperaDesde?: number;
        esperas?: number;
        /**
         * Solo `orden.agregar_formulario`: la plantilla del catálogo que vio el técnico (con sus
         * ítems), para aplicarla sin señal. Es local: no viaja en el lote.
         */
        plantilla?: PlantillaFormulario;
    };
}[TipoOperacion];

export interface OrdenLocal {
    id: number;
    /** Última versión conocida del servidor (paquete, cambios o resultados del lote). */
    base: OrdenCampo;
    /** El servidor la quitó de este técnico; se conserva mientras haya operaciones suyas. */
    reasignada: boolean;
}

/**
 * - `por_crear`: falta el alta (`POST /adjuntos`).
 * - `subiendo`: alta hecha; faltan bytes (tus).
 * - `subido`: completo en el servidor; los bytes ya se borraron del teléfono.
 * - `rechazado`: la API no lo acepta (tipo, tamaño, sha256…); el técnico lo descarta o lo rehace.
 */
export type EstadoSubida = "por_crear" | "subiendo" | "subido" | "rechazado";

export interface AdjuntoLocal {
    id: string;
    /** Orden del adjunto; en la firma de un traslado, `grupoTraslado(trasladoId)` (negativa). */
    ordenId: number;
    /** Entidad del alta: ausente = `orden`; `traslado` = firma de una recepción. */
    entidad?: "traslado";
    /**
     * Firma de traslado con 404 en el alta (el traslado aún no llega a Iris): no se reintenta antes
     * de `esperaHasta`; `esperaDesde` es el primer 404 (tope `ESPERA_OPERACION_MAX_MS`).
     */
    esperaHasta?: number;
    esperaDesde?: number;
    esperas?: number;
    tipo: TipoAdjunto;
    /** Código del ítem foto del checklist que respalda (`claveChecklist` del alta). */
    claveChecklist: string | null;
    /** Cuenta para el cierre: `orden.cerrar` no sale mientras no esté subido. */
    obligatorio: boolean;
    mime: string;
    tamano: number;
    sha256: string;
    lat: number | null;
    lon: number | null;
    /** Hora de captura del teléfono sin corregir (EXIF o reloj). */
    tomadoEn: string | null;
    estado: EstadoSubida;
    /** Bytes confirmados por el servidor. */
    offset: number;
    intentos: number;
    ultimoError: string | null;
    /** Problema de la API si quedó `rechazado`. */
    problema: Problema | null;
}

/** Turno de la agenda guardado con el día del paquete al que pertenece (`YYYY-MM-DD`, Guatemala). */
export type TurnoDelDia = TurnoCampo & { fecha: string };

/**
 * La API aplicó una operación con un aviso que el técnico debe ver (`version_anterior`,
 * `pausa_ajustada`…): nunca se resuelve un conflicto en silencio. Queda hasta que lo descarta.
 */
export interface AvisoServidor {
    /** Id de la operación. */
    id: string;
    ordenId: number;
    tipo: TipoOperacion;
    aviso: AvisoResultado;
}

export interface MetaSync {
    dispositivoId: string | null;
    /** Contador de `secuencia` de la cola. */
    secuencia: number;
    /** Hora del servidor − hora del teléfono, medida en el último contacto (ms). */
    desfaseMs: number;
    cursor: string | null;
    fecha: string | null;
    existencias: Existencias | null;
    ruta: RutaCampo | null;
    /** Turno del día según la agenda; `null` = sin paquete o ese día no trabaja según la agenda. */
    turno: TurnoDelDia | null;
    /** Catálogo de plantillas activas (paquete o cambios) para agregar formularios sin señal. */
    plantillas: PlantillaFormulario[] | null;
    /** Avisos de operaciones aplicadas que el técnico todavía no vio. */
    avisos: AvisoServidor[];
    /** Último contacto correcto con la API (hora del teléfono). */
    ultimaSincronizacion: number | null;
}

export const META_INICIAL: MetaSync = {
    dispositivoId: null,
    secuencia: 0,
    desfaseMs: 0,
    cursor: null,
    fecha: null,
    existencias: null,
    ruta: null,
    turno: null,
    plantillas: null,
    avisos: [],
    ultimaSincronizacion: null,
};
