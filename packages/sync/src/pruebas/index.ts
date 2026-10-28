// Dobles para probar el motor y las pantallas que lo usan (`@iris/sync/pruebas`): reloj manual,
// transporte y subidor falsos en memoria. No se usan en producción.
export { AlmacenMemoria } from "../almacen-memoria";
export { RelojManual, vaciarPromesas } from "./reloj-manual";
export { TransporteFalso, type Llamada } from "./transporte-falso";
export { SubidorFalso } from "./subidor-falso";
export { ordenDePrueba, itemDePrueba } from "./fabricas";
export { CABLE, FECHA, montar, paqueteDePrueba, type Montaje } from "./montaje";
