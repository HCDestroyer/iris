// @iris/ui: componentes de shadcn/ui adaptados a la identidad Horus. Fuente TS (sin build): la
// app la transpila (`transpilePackages`). Estilos: `@iris/ui/estilos.css`.
export { cn } from "./utilidades";
export { Aviso, type PropsAviso } from "./componentes/aviso";
export { AreaTexto, CampoArea, type PropsCampoArea } from "./componentes/area-texto";
export { Boton, variantesBoton, type PropsBoton } from "./componentes/boton";
export { Camara, type PropsCamara } from "./componentes/camara";
export { Campo, type PropsCampo, type PropsEnvolturaCampo } from "./componentes/campo";
export { Cargando, type PropsCargando } from "./componentes/cargando";
export { Casilla, type PropsCasilla } from "./componentes/casilla";
export {
    Dialogo,
    DialogoCerrar,
    DialogoContenido,
    DialogoDisparador,
    DialogoPie,
    type PropsDialogoContenido,
} from "./componentes/dialogo";
export { Entrada } from "./componentes/entrada";
export { abrirEscaner, Escaner, FORMATOS_CODIGO, lectorNativo, lectorZxing, type DependenciasEscaner, type Lector, type PropsEscaner } from "./componentes/escaner";
export { dibujarTrazos, hayFirma, MAX_SVG_FIRMA, simplificar, trazosAPng, trazosASvg, type Punto, type Trazo } from "./componentes/firma";
export { Esqueleto } from "./componentes/esqueleto";
export { EstadoVacio, type PropsEstadoVacio } from "./componentes/estado-vacio";
export { Etiqueta } from "./componentes/etiqueta";
export { GrupoSegmentado, type OpcionSegmentada, type PropsGrupoSegmentado } from "./componentes/grupo-segmentado";
export { Hoja, HojaCerrar, HojaContenido, HojaDisparador, type PropsHojaContenido } from "./componentes/hoja";
export { InsigniaEstado, type PropsInsigniaEstado } from "./componentes/insignia-estado";
export { LienzoFirma, type PropsLienzoFirma } from "./componentes/lienzo-firma";
export { LogoIris, MarcaIris } from "./componentes/marca";
export {
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    MenuDesplegableEtiqueta,
    MenuDesplegableSeparador,
    type PropsMenuDesplegableElemento,
} from "./componentes/menu-desplegable";
export { CampoSelector, Selector, type PropsCampoSelector, type PropsSelector } from "./componentes/selector";
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
