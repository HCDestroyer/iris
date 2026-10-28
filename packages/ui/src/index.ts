// @iris/ui: componentes de shadcn/ui adaptados a la identidad Horus. Fuente TS (sin build): la
// app la transpila (`transpilePackages`). Estilos: `@iris/ui/estilos.css`.
export { cn } from "./utilidades";
export { Aviso, type PropsAviso } from "./componentes/aviso";
export { Boton, variantesBoton, type PropsBoton } from "./componentes/boton";
export { Campo, type PropsCampo } from "./componentes/campo";
export { Cargando, type PropsCargando } from "./componentes/cargando";
export { Entrada } from "./componentes/entrada";
export { Esqueleto } from "./componentes/esqueleto";
export { EstadoVacio, type PropsEstadoVacio } from "./componentes/estado-vacio";
export { Etiqueta } from "./componentes/etiqueta";
export { GrupoSegmentado, type OpcionSegmentada, type PropsGrupoSegmentado } from "./componentes/grupo-segmentado";
export { Hoja, HojaCerrar, HojaContenido, HojaDisparador, type PropsHojaContenido } from "./componentes/hoja";
export { InsigniaEstado, type PropsInsigniaEstado } from "./componentes/insignia-estado";
export { LogoIris, MarcaIris } from "./componentes/marca";
export { Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "./componentes/tabla";
export {
    Tarjeta,
    TarjetaContenido,
    TarjetaDescripcion,
    TarjetaEncabezado,
    TarjetaPie,
    TarjetaTitulo,
} from "./componentes/tarjeta";
export { iconoPorTono, type Tono } from "./componentes/tonos";
