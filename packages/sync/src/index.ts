// @iris/sync: motor de sincronización sin conexión de la app de campo (`contratos/sync.md`).
// Núcleo sin DOM; el adaptador de IndexedDB está en `@iris/sync/dexie` y los dobles de prueba
// en `@iris/sync/pruebas`.
export * from "./contrato-sync";
export type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
export { AlmacenMemoria } from "./almacen-memoria";
export { aplicarOperacion, descontarExistencias, fusionarMarca, AUTOR_LOCAL, TIPOS_EVIDENCIA } from "./aplicar";
export { codigoProblema, ErrorSync, ErrorTransporte, type CodigoErrorLocal } from "./errores";
export { crearSubidorTus, crearTransporteHttp, type OpcionesHttp } from "./http";
export type { AdjuntoLocal, EstadoOperacionLocal, EstadoSubida, MetaSync, OperacionLocal, OrdenLocal } from "./modelo";
export {
    CODIGOS_TERMINALES,
    DIA_MS,
    faltantesDe,
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
export { calcularEspera, ESPERA_POR_DEFECTO, relojSistema, type OpcionesEspera, type Reloj } from "./reloj";
export type { RespuestaApi, Subidor, Transporte } from "./transporte";
export { crearGeneradorUuid7, msDeUuid7, RE_UUID7, type FuenteAzar, type GeneradorUuid7 } from "./uuid7";
