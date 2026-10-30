// @iris/sync: motor de sincronización sin conexión de la app de campo (`contratos/sync.md`).
// Núcleo sin DOM; el adaptador de IndexedDB está en `@iris/sync/dexie` y los dobles de prueba
// en `@iris/sync/pruebas`. Las formas del cable vienen de `@iris/contratos`.
export * from "./contrato";
export type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
export { AlmacenMemoria } from "./almacen-memoria";
export {
    aplicarOperacion,
    bodegaDeOrden,
    descontarExistencias,
    formularioConRespuestas,
    fusionarMarca,
    itemsDeFormulario,
    segundosPausada,
    valorDe,
    ESTADOS_PAUSABLES,
    ESTADOS_TERMINALES,
    TIPOS_BLOQUEADOS_EN_PAUSA,
    TIPOS_EVIDENCIA,
} from "./aplicar";
export { CODIGO_SESION_AJENA, codigoProblema, errorSesionAjena, ErrorSync, ErrorTransporte, PREFIJO_PROBLEMA, type CodigoErrorLocal } from "./errores";
export { CABECERA_CANAL, crearSubidorApi, crearTransporteApi, crearTransportePosicionesApi } from "./http";
export type { AdjuntoLocal, EstadoOperacionLocal, EstadoSubida, AvisoServidor, MetaSync, OperacionLocal, OrdenLocal, TurnoDelDia } from "./modelo";
export {
    CODIGO_BLOQUEADA,
    CODIGOS_REINTENTABLES,
    CODIGOS_TERMINALES,
    compararOrdenesDelDia,
    cubreFaltante,
    DIA_MS,
    faltantesDe,
    inicioCita,
    MotorSync,
    type Aviso,
    type Conexion,
    type EntradaAdjunto,
    type ErrorVisible,
    type EstadoSync,
    type OpcionesMotor,
    type OrdenVista,
    type ResultadoRegistro,
    type ResumenSubidas,
} from "./motor";
export {
    ColaPosiciones,
    msDeRetryAfter,
    normalizarPunto,
    type AlmacenPosiciones,
    type CambiosPosiciones,
    type DescartadasLocales,
    type EstadoPosiciones,
    type OpcionesColaPosiciones,
    type PosicionLocal,
    type TransportePosiciones,
} from "./posiciones";
export { calcularEspera, ESPERA_POR_DEFECTO, relojSistema, type OpcionesEspera, type Reloj } from "./reloj";
export type { RespuestaApi, Subidor, Transporte } from "./transporte";
export { crearGeneradorUuid7, msDeUuid7, RE_UUID7, type FuenteAzar, type GeneradorUuid7 } from "./uuid7";
