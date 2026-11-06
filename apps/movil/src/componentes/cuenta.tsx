// Componentes de «Yo» (diseño aprobado por el PM, nov 2026) sobre HeroUI Native: el control
// segmentado de la apariencia, las secciones con su título en versalitas, el grupo de filas (una
// tarjeta con separadores) y cada fila de ajuste, el punto de estado, el encabezado con «Atrás» de
// las pantallas propias y el botón de contorno rojo de «Cerrar sesión». Como `index.tsx`: las
// pantallas no usan `Pressable` ni `Switch` crudos; las medidas táctiles van en `style`.
import type { TonoEstado } from "@iris/sync/campo";
import { Button } from "heroui-native/button";
import { PressableFeedback } from "heroui-native/pressable-feedback";
import { Switch } from "heroui-native/switch";
import { ArrowLeft, Check, ChevronRight, type LucideIcon } from "lucide-react-native";
import { Children, Fragment, type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import { useTema } from "../tema/tema";
import { BotonRedondo, ZONA_TACTIL } from "./index";

export interface OpcionSegmentada<T extends string> {
    valor: T;
    texto: string;
    icono: LucideIcon;
}

/**
 * Control segmentado (radios en una pista): la elegida va en una píldora elevada, con su icono y su
 * texto en negrita (`checked` accesible: nunca solo color). 48 px de alto.
 */
export function Segmentado<T extends string>({ etiqueta, opciones, valor, alCambiar, testID }: { etiqueta: string; opciones: readonly OpcionSegmentada<T>[]; valor: T; alCambiar: (v: T) => void; testID?: string }) {
    const { campo, colores, oscuro } = useTema();
    return (
        <View accessibilityRole="radiogroup" accessibilityLabel={etiqueta} testID={testID} style={[estilos.pista, { backgroundColor: campo.panel2 }]}>
            {opciones.map((o) => {
                const elegida = o.valor === valor;
                const Icono = o.icono;
                const tinta = elegida ? colores.texto : colores.textoSuave;
                return (
                    <PressableFeedback
                        key={o.valor}
                        accessibilityRole="radio"
                        accessibilityLabel={o.texto}
                        accessibilityState={{ checked: elegida, selected: elegida }}
                        onPress={() => alCambiar(o.valor)}
                        testID={testID ? `${testID}-${o.valor}` : undefined}
                        style={[
                            estilos.segmento,
                            elegida ? [estilos.segmentoElegido, { backgroundColor: oscuro ? campo.lienzo : campo.panel, shadowColor: campo.sombra }] : null,
                        ]}
                    >
                        <PressableFeedback.Highlight />
                        <Icono size={18} color={tinta} strokeWidth={elegida ? 2.4 : 2} />
                        <Text numberOfLines={1} style={[estilos.textoSegmento, { color: tinta, fontWeight: elegida ? "700" : "500" }]}>
                            {o.texto}
                        </Text>
                    </PressableFeedback>
                );
            })}
        </View>
    );
}

/** Sección de «Yo»: título en versalitas («APARIENCIA») y su contenido. */
export function Seccion({ titulo, children }: { titulo: string; children: ReactNode }) {
    const { colores } = useTema();
    return (
        <View style={estilos.seccion}>
            <Text accessibilityRole="header" style={[estilos.tituloSeccion, { color: colores.textoSuave }]}>
                {titulo.toLocaleUpperCase("es")}
            </Text>
            {children}
        </View>
    );
}

/** Tarjeta de 28 px con sus filas separadas por una raya (lo que no se dibuja no deja raya). */
export function GrupoFilas({ children, testID }: { children: ReactNode; testID?: string }) {
    const { campo, colores, radioTarjeta } = useTema();
    const filas = Children.toArray(children).filter(Boolean);
    return (
        <View testID={testID} style={[estilos.grupo, { backgroundColor: campo.panel, borderColor: colores.borde, borderRadius: radioTarjeta }]}>
            {filas.map((f, i) => (
                <Fragment key={i}>
                    {i > 0 ? <View style={[estilos.raya, { backgroundColor: colores.borde }]} /> : null}
                    {f}
                </Fragment>
            ))}
        </View>
    );
}

/** Punto de estado discreto + texto (verde «Activa», neutro «Apagada»): nunca solo color. */
export function PuntoEstado({ tono, texto, testID }: { tono: TonoEstado; texto: string; testID?: string }) {
    const { colores } = useTema();
    const color = colores.tono[tono];
    return (
        <View testID={testID} style={estilos.punto}>
            <View style={[estilos.circulo, tono === "neutral" ? { borderWidth: 1.5, borderColor: color } : { backgroundColor: color }]} />
            <Text style={[estilos.textoPunto, { color }]}>{texto}</Text>
        </View>
    );
}

/**
 * Fila de ajuste: icono, título y detalle, y a la derecha un valor, un punto o un interruptor. Con
 * `alPresionar` toda la fila es un botón (y lleva el ">"); su nombre accesible es el título y el
 * estado de la derecha.
 */
export function FilaAjuste({
    icono: Icono,
    titulo,
    detalle,
    derecha,
    etiqueta,
    alPresionar,
    tinta,
    deshabilitado,
    testID,
}: {
    /** Sin icono, queda el hueco (la fila se alinea con las de arriba). */
    icono?: LucideIcon;
    titulo: string;
    detalle?: string | null;
    derecha?: ReactNode;
    /** Nombre accesible si el título no basta («Por enviar (3)»). */
    etiqueta?: string;
    alPresionar?: () => void;
    /** Color del icono y del título (rojo para salir). */
    tinta?: string;
    /** Atenuada y sin tocar (p. ej. con las notificaciones bloqueadas en el teléfono). */
    deshabilitado?: boolean;
    testID?: string;
}) {
    const { colores } = useTema();
    const contenido = (
        <>
            {Icono ? <Icono size={22} color={tinta ?? colores.texto} strokeWidth={1.8} /> : <View style={estilos.hueco} />}
            <View style={estilos.flex}>
                <Text style={[estilos.tituloFila, { color: tinta ?? colores.texto }]}>{titulo}</Text>
                {detalle ? <Text style={[estilos.detalleFila, { color: colores.textoSuave }]}>{detalle}</Text> : null}
            </View>
            {derecha}
            {alPresionar ? <ChevronRight size={20} color={colores.textoSuave} /> : null}
        </>
    );
    if (!alPresionar) {
        return (
            <View testID={testID} style={[estilos.fila, deshabilitado ? estilos.atenuada : null]}>
                {contenido}
            </View>
        );
    }
    return (
        <PressableFeedback
            accessibilityRole="button"
            accessibilityLabel={etiqueta ?? titulo}
            accessibilityState={deshabilitado ? { disabled: true } : undefined}
            isDisabled={deshabilitado}
            onPress={alPresionar}
            testID={testID}
            animation={{ scale: { value: 0.99 } }}
            style={[estilos.fila, deshabilitado ? estilos.atenuada : null]}
        >
            <PressableFeedback.Highlight />
            {contenido}
        </PressableFeedback>
    );
}

/** Fila con interruptor (HeroUI `Switch`): `switch` accesible con su estado; toda la fila lo cambia. */
export function FilaInterruptor({
    icono,
    titulo,
    detalle,
    valor,
    alCambiar,
    deshabilitado,
    testID,
}: {
    icono: LucideIcon;
    titulo: string;
    detalle?: string;
    valor: boolean;
    alCambiar: (v: boolean) => void;
    deshabilitado?: boolean;
    testID?: string;
}) {
    return (
        <FilaAjuste
            icono={icono}
            titulo={titulo}
            detalle={detalle ?? null}
            deshabilitado={deshabilitado}
            derecha={
                <Switch
                    isSelected={valor}
                    isDisabled={deshabilitado}
                    onSelectedChange={alCambiar}
                    accessibilityLabel={titulo}
                    accessibilityState={{ checked: valor, disabled: deshabilitado === true }}
                    testID={testID}
                    style={estilos.zonaInterruptor}
                />
            }
        />
    );
}

/**
 * Fila de una elección (radio): el círculo marcado, título, detalle y, si se da, algo debajo (la
 * muestra del aviso). `radio` accesible con `checked`: nunca solo color.
 */
export function FilaRadio({
    titulo,
    detalle,
    elegida,
    alElegir,
    deshabilitado,
    children,
    testID,
}: {
    titulo: string;
    detalle?: string;
    elegida: boolean;
    alElegir: () => void;
    deshabilitado?: boolean;
    children?: ReactNode;
    testID?: string;
}) {
    const { colores } = useTema();
    return (
        <PressableFeedback
            accessibilityRole="radio"
            accessibilityLabel={titulo}
            accessibilityState={{ checked: elegida, selected: elegida, disabled: deshabilitado === true }}
            isDisabled={deshabilitado}
            onPress={alElegir}
            testID={testID}
            animation={{ scale: { value: 0.99 } }}
            style={[estilos.fila, deshabilitado ? estilos.atenuada : null]}
        >
            <PressableFeedback.Highlight />
            <View style={[estilos.radio, { borderColor: elegida ? colores.primario : colores.borde }]}>
                {elegida ? <View style={[estilos.radioPunto, { backgroundColor: colores.primario }]} /> : null}
            </View>
            <View style={[estilos.flex, estilos.columnaRadio]}>
                <View>
                    <Text style={[estilos.tituloFila, { color: colores.texto }]}>{titulo}</Text>
                    {detalle ? <Text style={[estilos.detalleFila, { color: colores.textoSuave }]}>{detalle}</Text> : null}
                </View>
                {children}
            </View>
        </PressableFeedback>
    );
}

/** Encabezado de una pantalla propia: «Atrás» redondo de 48 px y el título. */
export function EncabezadoPantalla({ titulo, alVolver, etiquetaVolver = "Atrás" }: { titulo: string; alVolver: () => void; etiquetaVolver?: string }) {
    const { campo, colores } = useTema();
    return (
        <View style={estilos.encabezado}>
            <BotonRedondo icono={ArrowLeft} etiqueta={etiquetaVolver} fondo={campo.panel} tinta={colores.texto} borde={colores.borde} alPresionar={alVolver} testID="volver" />
            <Text accessibilityRole="header" style={[estilos.tituloPantalla, { color: colores.texto }]}>
                {titulo}
            </Text>
        </View>
    );
}

/** Regla cumplida bajo un campo («12 caracteres o más»), en verde con su marca. */
export function AyudaCumplida({ texto, cumplida, testID }: { texto: string; cumplida: boolean; testID?: string }) {
    const { colores } = useTema();
    if (!cumplida) {
        return null;
    }
    return (
        <View testID={testID} style={estilos.ayuda}>
            <Check size={16} color={colores.exito} />
            <Text style={[estilos.textoAyuda, { color: colores.exito }]}>{texto}</Text>
        </View>
    );
}

/** Número de un paso («1», «2») en su círculo. */
export function NumeroPaso({ numero }: { numero: number }) {
    const { campo, colores } = useTema();
    return (
        <View style={[estilos.numero, { backgroundColor: campo.panel2 }]}>
            <Text style={[estilos.textoNumero, { color: colores.texto }]}>{numero}</Text>
        </View>
    );
}

/** Botón de contorno, de lado a lado, con el texto y el icono en rojo («Cerrar sesión»). */
export function BotonContornoPeligro({ children, icono: Icono, alPresionar, cargando, testID }: { children: string; icono: LucideIcon; alPresionar: () => void; cargando?: boolean; testID?: string }) {
    const { campo, colores } = useTema();
    return (
        <Button
            variant="outline"
            isDisabled={cargando === true}
            onPress={alPresionar}
            accessibilityLabel={children}
            accessibilityState={{ disabled: cargando === true, busy: cargando === true }}
            testID={testID}
            className="h-auto rounded-full"
            style={[estilos.contorno, { borderColor: colores.borde, backgroundColor: campo.panel }]}
        >
            <Icono size={20} color={colores.error} />
            <Button.Label className="font-semibold" style={[estilos.textoContorno, { color: colores.error }]}>
                {children}
            </Button.Label>
        </Button>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    pista: { flexDirection: "row", borderRadius: 999, padding: 4, gap: 4 },
    segmento: { flex: 1, minHeight: 48, borderRadius: 999, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 6, paddingHorizontal: 6 },
    segmentoElegido: { shadowOpacity: 0.12, shadowRadius: 8, shadowOffset: { width: 0, height: 2 }, elevation: 2 },
    textoSegmento: { fontSize: 15, flexShrink: 1 },
    seccion: { gap: 8 },
    tituloSeccion: { fontSize: 13, fontWeight: "600", letterSpacing: 0.8, paddingHorizontal: 6 },
    grupo: { borderWidth: 1, overflow: "hidden" },
    raya: { height: StyleSheet.hairlineWidth, marginLeft: 0 },
    fila: { minHeight: 56, flexDirection: "row", alignItems: "center", gap: 14, paddingHorizontal: 16, paddingVertical: 12 },
    tituloFila: { fontSize: 16, fontWeight: "500" },
    detalleFila: { fontSize: 13, marginTop: 2 },
    punto: { flexDirection: "row", alignItems: "center", gap: 6 },
    circulo: { width: 8, height: 8, borderRadius: 4 },
    textoPunto: { fontSize: 15, fontWeight: "500" },
    zonaInterruptor: { minHeight: ZONA_TACTIL, minWidth: ZONA_TACTIL, justifyContent: "center" },
    atenuada: { opacity: 0.55 },
    hueco: { width: 22, height: 22 },
    radio: { width: 24, height: 24, borderRadius: 12, borderWidth: 2, alignItems: "center", justifyContent: "center" },
    radioPunto: { width: 12, height: 12, borderRadius: 6 },
    columnaRadio: { gap: 10 },
    encabezado: { flexDirection: "row", alignItems: "center", gap: 14 },
    tituloPantalla: { flex: 1, fontSize: 22, fontWeight: "700", letterSpacing: -0.3 },
    ayuda: { flexDirection: "row", alignItems: "center", gap: 6 },
    textoAyuda: { fontSize: 13, fontWeight: "500" },
    numero: { width: 28, height: 28, borderRadius: 14, alignItems: "center", justifyContent: "center" },
    textoNumero: { fontSize: 14, fontWeight: "700" },
    contorno: { minHeight: 52, borderWidth: 1, borderRadius: 999, flexDirection: "row", gap: 8 },
    textoContorno: { fontSize: 16 },
});
