// @iris/ui: componentes de Iris sobre HeroUI v3 (React Aria) con la identidad Horus. Fuente TS
// (sin build): la app la transpila (`transpilePackages`). Estilos: `@iris/ui/estilos.css`.
// Las pantallas importan de aquí, nunca de `@heroui/react` directamente (docs/ui/guia.md).
export { cn } from "./utilidades";
export { Aviso, type PropsAviso } from "./componentes/aviso";
export { AreaTexto, CampoArea, type PropsCampoArea } from "./componentes/area-texto";
export { Autocompletar, type OpcionAutocompletar, type PropsAutocompletar } from "./componentes/autocompletar";
export {
    avisar,
    cerrarAviso,
    colaAvisos,
    ProveedorAvisosEmergentes,
    useAvisoEmergente,
    type ContenidoAvisoEmergente,
    type PropsProveedorAvisosEmergentes,
} from "./componentes/aviso-emergente";
export { Boton, variantesBoton, type PropsBoton, type TamanoBoton, type VarianteBoton } from "./componentes/boton";
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
    type PropsDialogo,
    type PropsDialogoContenido,
    type PropsDisparador,
} from "./componentes/dialogo";
export { Entrada } from "./componentes/entrada";
export { abrirEscaner, Escaner, FORMATOS_CODIGO, lectorNativo, lectorZxing, type DependenciasEscaner, type Lector, type PropsEscaner } from "./componentes/escaner";
export { dibujarTrazos, hayFirma, MAX_SVG_FIRMA, simplificar, trazosAPng, trazosASvg, type Punto, type Trazo } from "./componentes/firma";
export {
    asignarErrores,
    BotonEnviar,
    campoDelFormulario,
    DialogoConfirmar,
    DialogoProtegido,
    ErrorFormulario,
    ProveedorCambios,
    textoFaltan,
    useCambiosSinGuardar,
    useRegistroCambios,
    type ErrorDeCampo,
    type PropsBotonEnviar,
    type PropsDialogoConfirmar,
    type PropsDialogoProtegido,
    type PropsErrorFormulario,
} from "./componentes/formulario-modal";
export {
    BarraGuardar,
    FormularioPagina,
    liberarProteccion,
    rutaInterna,
    rutaQueSale,
    SeccionFormulario,
    useProteccionSalida,
    type ClicEnlace,
    type PropsBarraGuardar,
    type PropsFormularioPagina,
    type PropsSeccionFormulario,
    type ProteccionSalida,
} from "./componentes/formulario-pagina";
export { Esqueleto } from "./componentes/esqueleto";
export { EstadoVacio, type PropsEstadoVacio } from "./componentes/estado-vacio";
export { Etiqueta } from "./componentes/etiqueta";
export { Globo, type PropsGlobo } from "./componentes/globo";
export { GrupoOpciones, type OpcionGrupo, type PropsGrupoOpciones } from "./componentes/grupo-opciones";
export { GrupoSegmentado, type OpcionSegmentada, type PropsGrupoSegmentado } from "./componentes/grupo-segmentado";
export { Hoja, HojaCerrar, HojaContenido, HojaDisparador, type PropsHojaContenido } from "./componentes/hoja";
export { InsigniaEstado, type PropsInsigniaEstado } from "./componentes/insignia-estado";
export { LienzoFirma, type PropsLienzoFirma } from "./componentes/lienzo-firma";
export { LogoIris, MarcaIris } from "./componentes/marca";
export { Migas, ProveedorRutas, type Miga, type PropsMigas, type PropsProveedorRutas } from "./componentes/migas";
export {
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    MenuDesplegableEtiqueta,
    MenuDesplegableSeparador,
    MenuEnLinea,
    type PropsMenuEnLinea,
    type PropsMenuDesplegable,
    type PropsMenuDesplegableContenido,
    type PropsMenuDesplegableElemento,
} from "./componentes/menu-desplegable";
export { PaletaBusqueda, type ElementoPaleta, type PropsPaletaBusqueda, type SeccionPaleta } from "./componentes/paleta-busqueda";
export { PanelEmergente, type PropsPanelEmergente } from "./componentes/panel-emergente";
export { Pestanas, type Pestana, type PropsPestanas } from "./componentes/pestanas";
export { CampoSelector, Selector, type PropsCampoSelector, type PropsSelector } from "./componentes/selector";
export {
    aFecha,
    hoyEnGuatemala,
    SelectorFecha,
    SelectorRangoFechas,
    ZONA_GUATEMALA,
    type PropsSelectorFecha,
    type PropsSelectorRangoFechas,
    type RangoFechas,
} from "./componentes/selector-fecha";
export { Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "./componentes/tabla";
export {
    Tarjeta,
    TarjetaContenido,
    TarjetaDescripcion,
    TarjetaEncabezado,
    TarjetaPie,
    TarjetaTitulo,
} from "./componentes/tarjeta";
export { estadoHeroui, iconoPorTono, type Tono } from "./componentes/tonos";
