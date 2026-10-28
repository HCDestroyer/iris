// Tipos del contrato API ↔ app de campo (`.claude/integration/contratos/sync.md`).
//
// DEUDA (anotada en `.claude/integration/i4.md`): es la única excepción del repo a «nada de
// interfaces a mano». Existen porque los endpoints de campo (`/campo/paquete`, `/campo/cambios`,
// `/sync/lote`) y de adjuntos (`/adjuntos`) todavía no están en `packages/contratos`. Cuando I2 e
// I1 los publiquen, este archivo se reemplaza por alias a `Schema<"…">` de `@iris/contratos` y lo
// que no coincida lo marca `tsc`. Nada fuera de este archivo declara formas del cable.
//
// Supuesto: claves en camelCase, como el resto de la API de Iris (huma); `sync.md` las escribe en
// snake_case solo como ejemplo. Los valores de `tipo` y `estado` sí son literales del contrato.

/** Problema RFC 9457 de la API de Iris: `type` = `urn:iris:problema:<codigo>`. */
export interface Problema {
    type: string;
    title: string;
    status: number;
    detail?: string;
    errors?: { field: string; message: string; rule?: string }[] | null;
}

export const ESTADOS_ORDEN = [
    "pendiente",
    "programada",
    "en_camino",
    "en_sitio",
    "cerrada",
    "fallida",
    "cancelada",
] as const;
export type EstadoOrden = (typeof ESTADOS_ORDEN)[number];

export const TIPOS_OPERACION = [
    "orden.iniciar_camino",
    "orden.llegar",
    "checklist.marcar",
    "nota.agregar",
    "material.usar",
    "equipo.instalar",
    "equipo.retirar",
    "firma.capturar",
    "adjunto.registrar",
    "orden.cerrar",
    "orden.fallar",
] as const;
export type TipoOperacion = (typeof TIPOS_OPERACION)[number];

export interface Posicion {
    lat: number;
    lon: number;
    precision?: number;
}

export type ValorChecklist = boolean | number | string;

/** `datos` de cada tipo de operación (§2). */
export interface DatosPorTipo {
    "orden.iniciar_camino": { posicion?: Posicion };
    "orden.llegar": { posicion?: Posicion };
    "checklist.marcar": { itemId: string; valor: ValorChecklist | null; comentario?: string };
    "nota.agregar": { texto: string };
    "material.usar": { productoId: string; cantidad: number };
    "equipo.instalar": { serie: string; mac?: string; unidadId?: string; condicion?: string };
    "equipo.retirar": { serie: string; mac?: string; unidadId?: string; condicion?: string };
    "firma.capturar": { nombre: string; documento: string; svg: string; adjuntoId?: string };
    "adjunto.registrar": { adjuntoId: string; tipo: TipoAdjunto; checklistItemId?: string };
    "orden.cerrar": { resultado: string; observaciones?: string; posicion?: Posicion };
    "orden.fallar": { motivo: string; detalle?: string; posicion?: Posicion };
}

export type TipoAdjunto = "foto" | "firma" | "documento";

/** Una operación tal como viaja en `POST /sync/lote`. */
export type OperacionCable = {
    [T in TipoOperacion]: {
        id: string;
        tipo: T;
        ordenId: string;
        versionBase: number;
        /** ISO 8601 UTC: hora del teléfono corregida con el desfase medido. */
        ocurridoEn: string;
        datos: DatosPorTipo[T];
    };
}[TipoOperacion];

export interface PeticionLote {
    dispositivoId: string;
    operaciones: OperacionCable[];
}

export interface OrdenVersion {
    version: number;
    estado: EstadoOrden;
}

export type EstadoResultado = "aplicada" | "duplicada" | "rechazada";

export interface ResultadoOperacion {
    id: string;
    estado: EstadoResultado;
    error?: Problema | null;
    orden?: OrdenVersion | null;
}

export interface RespuestaLote {
    resultados: ResultadoOperacion[];
    cursor?: string | null;
}

// --- Paquete del día y cambios (§1) ---------------------------------------------------------

export interface MarcaChecklist {
    valor: ValorChecklist | null;
    ocurridoEn: string;
    /** Usuario o «despacho»; el teléfono pone `yo` en sus marcas sin confirmar. */
    por: string;
}

export interface ItemChecklist {
    id: string;
    texto: string;
    tipo: "si_no" | "texto" | "numero" | "foto" | "medicion";
    obligatorio: boolean;
    valor: ValorChecklist | null;
    marcadoEn: string | null;
    marcadoPor: string | null;
    historial: MarcaChecklist[];
}

export interface EquipoComodato {
    unidadId: string;
    serie: string;
    mac?: string | null;
    modelo?: string | null;
}

export interface InstantaneaCliente {
    id: string;
    nombre: string;
    direccion?: string | null;
    lat?: number | null;
    lon?: number | null;
    telefono?: string | null;
}

export interface InstantaneaServicio {
    id: string;
    plan?: string | null;
    estado?: string | null;
}

export interface NotaOrden {
    id: string;
    texto: string;
    ocurridoEn: string;
}

export interface MaterialOrden {
    id: string;
    productoId: string;
    cantidad: number;
}

export interface EquipoOrden {
    id: string;
    accion: "instalar" | "retirar";
    serie: string;
    mac?: string | null;
}

export interface FirmaOrden {
    nombre: string;
    documento: string;
    adjuntoId?: string | null;
}

/** Orden tal como la entrega el paquete del día o un cambio. */
export interface OrdenCampo {
    id: string;
    numero: string;
    tipo: string;
    estado: EstadoOrden;
    version: number;
    cliente: InstantaneaCliente;
    servicio: InstantaneaServicio | null;
    inicio?: string | null;
    fin?: string | null;
    checklist: ItemChecklist[];
    equiposComodato: EquipoComodato[];
    notas: NotaOrden[];
    materiales: MaterialOrden[];
    equipos: EquipoOrden[];
    adjuntos: string[];
    firma: FirmaOrden | null;
}

export interface Existencia {
    productoId: string;
    nombre: string;
    unidad: string;
    cantidad: number;
}

export interface ParadaRuta {
    ordenId: string;
    posicion: number;
    llegadaEstimada?: string | null;
}

export interface RutaPublicada {
    id: string;
    fecha: string;
    paradas: ParadaRuta[];
}

/** `GET /api/v1/campo/paquete?fecha=` */
export interface PaqueteDia {
    fecha: string;
    ordenes: OrdenCampo[];
    existencias: Existencia[];
    ruta: RutaPublicada | null;
    cursor: string;
}

export type TipoCambio = "nueva" | "actualizada" | "reasignada" | "cancelada" | "reprogramada";

export interface CambioOrden {
    tipo: TipoCambio;
    ordenId: string;
    /** Orden completa tras el cambio (ausente en `reasignada`, que la quita del técnico). */
    orden?: OrdenCampo | null;
}

/** `GET /api/v1/campo/cambios?cursor=` */
export interface RespuestaCambios {
    cambios: CambioOrden[];
    cursor: string;
    hayMas: boolean;
}

// --- Adjuntos (§4, de I1) -------------------------------------------------------------------

/** `POST /api/v1/adjuntos` */
export interface EntradaCrearAdjunto {
    id: string;
    ordenId: string;
    tipo: TipoAdjunto;
    tamano: number;
    sha256: string;
}

export interface RespuestaCrearAdjunto {
    id: string;
    /** URL tus 1.0 (HEAD para el offset, PATCH con `Upload-Offset`). */
    urlSubida: string;
}
