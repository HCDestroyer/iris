// Componentes de la app nativa con la identidad Horus (el equivalente de `@iris/ui` para React
// Native: `packages/ui` es shadcn/ui para la web). Las pantallas no usan `Pressable`, `TextInput`
// ni `Modal` crudos: pasan por aquí (regla 10 del repo, traducida a RN).
import type { TonoEstado } from "@iris/sync/campo";
import type { LucideIcon } from "lucide-react-native";
import type { ReactNode } from "react";
import {
    ActivityIndicator,
    Modal,
    Pressable,
    ScrollView,
    StyleSheet,
    Text,
    TextInput,
    View,
    type StyleProp,
    type TextInputProps,
    type TextStyle,
    type ViewStyle,
} from "react-native";
import { useTema } from "../tema/tema";

export type Variante = "primario" | "secundario" | "fantasma" | "peligro";

export interface PropsBoton {
    children: ReactNode;
    alPresionar: () => void;
    variante?: Variante;
    icono?: LucideIcon;
    deshabilitado?: boolean;
    cargando?: boolean;
    /** Texto para lectores de pantalla (y para las pruebas) si el visible no basta. */
    etiqueta?: string;
    estilo?: StyleProp<ViewStyle>;
    testID?: string;
}

/** Botón táctil de 48 px de alto (dedo con guante), con icono Lucide opcional. */
export function Boton({ children, alPresionar, variante = "primario", icono: Icono, deshabilitado, cargando, etiqueta, estilo, testID }: PropsBoton) {
    const { colores, radio } = useTema();
    const fondo = variante === "primario" ? colores.primario : variante === "peligro" ? colores.error : variante === "secundario" ? colores.fondoElevado : "transparent";
    const texto = variante === "primario" ? colores.sobrePrimario : variante === "peligro" ? "#ffffff" : variante === "secundario" ? colores.texto : colores.primario;
    const inactivo = deshabilitado === true || cargando === true;
    return (
        <Pressable
            accessibilityRole="button"
            accessibilityLabel={etiqueta}
            accessibilityState={{ disabled: inactivo, busy: cargando === true }}
            disabled={inactivo}
            onPress={alPresionar}
            testID={testID}
            style={({ pressed }) => [
                estilos.boton,
                {
                    backgroundColor: fondo,
                    borderRadius: radio,
                    borderColor: variante === "secundario" ? colores.borde : "transparent",
                    opacity: inactivo ? 0.55 : pressed ? 0.8 : 1,
                },
                estilo,
            ]}
        >
            {cargando ? <ActivityIndicator color={texto} size="small" /> : Icono ? <Icono color={texto} size={18} /> : null}
            <Text style={[estilos.textoBoton, { color: texto }]}>{children}</Text>
        </Pressable>
    );
}

export interface PropsCampo extends Omit<TextInputProps, "style" | "onChangeText" | "value"> {
    etiqueta: string;
    valor: string;
    alCambiar: (texto: string) => void;
    error?: string | undefined;
    ayuda?: string;
    /** Unidad que se ve a la derecha (dBm, m…). */
    sufijo?: string | null | undefined;
}

/** Campo de texto con etiqueta visible, ayuda y error (texto + color, nunca solo color). */
export function Campo({ etiqueta, valor, alCambiar, error, ayuda, sufijo, editable, ...resto }: PropsCampo) {
    const { colores, radio } = useTema();
    return (
        <View style={estilos.campo}>
            <Text style={[estilos.etiqueta, { color: colores.texto }]}>{etiqueta}</Text>
            <View style={estilos.filaCampo}>
                <TextInput
                    accessibilityLabel={etiqueta}
                    value={valor}
                    onChangeText={alCambiar}
                    editable={editable}
                    placeholderTextColor={colores.textoTenue}
                    style={[
                        estilos.entrada,
                        {
                            color: colores.texto,
                            backgroundColor: editable === false ? colores.fondoElevado : colores.fondo,
                            borderColor: error ? colores.error : colores.borde,
                            borderRadius: radio,
                        },
                    ]}
                    {...resto}
                />
                {sufijo ? <Text style={[estilos.sufijo, { color: colores.textoSuave }]}>{sufijo}</Text> : null}
            </View>
            {ayuda && !error ? <Text style={[estilos.ayuda, { color: colores.textoSuave }]}>{ayuda}</Text> : null}
            {error ? (
                <Text accessibilityRole="alert" style={[estilos.ayuda, { color: colores.error }]}>
                    {error}
                </Text>
            ) : null}
        </View>
    );
}

/** Tarjeta de contenido (`surface-card`): título, descripción y cuerpo. */
export function Tarjeta({ titulo, descripcion, children, accion, testID }: { titulo?: string; descripcion?: string; children?: ReactNode; accion?: ReactNode; testID?: string }) {
    const { colores, radioTarjeta } = useTema();
    return (
        <View testID={testID} style={[estilos.tarjeta, { backgroundColor: colores.tarjeta, borderColor: colores.borde, borderRadius: radioTarjeta }]}>
            {titulo || accion ? (
                <View style={estilos.filaEntre}>
                    <View style={estilos.flex}>
                        {titulo ? (
                            <Text accessibilityRole="header" style={[estilos.tituloTarjeta, { color: colores.texto }]}>
                                {titulo}
                            </Text>
                        ) : null}
                        {descripcion ? <Text style={[estilos.descripcion, { color: colores.textoSuave }]}>{descripcion}</Text> : null}
                    </View>
                    {accion}
                </View>
            ) : null}
            {children}
        </View>
    );
}

/** Estado con icono + texto + color (nunca solo color). */
export function Insignia({ tono, icono: Icono, texto, testID }: { tono: TonoEstado; icono: LucideIcon; texto: string; testID?: string }) {
    const { colores } = useTema();
    return (
        <View testID={testID} style={[estilos.insignia, { backgroundColor: colores.fondoTono[tono] }]}>
            <Icono color={colores.tono[tono]} size={14} />
            <Text style={[estilos.textoInsignia, { color: colores.tono[tono] }]}>{texto}</Text>
        </View>
    );
}

/** Aviso con tono, icono, título, detalle y una acción opcional («Reintentar», «Revisar»…). */
export function Aviso({
    tono,
    titulo,
    children,
    icono: Icono,
    accion,
    testID,
}: {
    tono: TonoEstado;
    titulo: string;
    children?: ReactNode;
    icono?: LucideIcon;
    accion?: ReactNode;
    testID?: string;
}) {
    const { colores, radio } = useTema();
    return (
        <View
            testID={testID}
            accessibilityRole={tono === "error" ? "alert" : "summary"}
            style={[estilos.aviso, { backgroundColor: colores.fondoTono[tono], borderColor: colores.tono[tono], borderRadius: radio }]}
        >
            <View style={estilos.fila}>
                {Icono ? <Icono color={colores.tono[tono]} size={18} /> : null}
                <Text style={[estilos.tituloAviso, { color: colores.tono[tono] }]}>{titulo}</Text>
            </View>
            {typeof children === "string" ? <Texto>{children}</Texto> : children}
            {accion ? <View style={estilos.accionAviso}>{accion}</View> : null}
        </View>
    );
}

export function Texto({ children, suave, mono, negrita, estilo, testID }: { children: ReactNode; suave?: boolean; mono?: boolean; negrita?: boolean; estilo?: StyleProp<TextStyle>; testID?: string }) {
    const { colores, fuenteMono } = useTema();
    return (
        <Text
            testID={testID}
            style={[
                estilos.texto,
                { color: suave ? colores.textoSuave : colores.texto },
                mono ? { fontFamily: fuenteMono } : null,
                negrita ? estilos.negrita : null,
                estilo,
            ]}
        >
            {children}
        </Text>
    );
}

export function Titulo({ children }: { children: ReactNode }) {
    const { colores } = useTema();
    return (
        <Text accessibilityRole="header" style={[estilos.titulo, { color: colores.texto }]}>
            {children}
        </Text>
    );
}

export interface Opcion<T extends string> {
    valor: T;
    etiqueta: string;
    detalle?: string;
    icono?: LucideIcon;
}

/**
 * Elegir una opción: botones en fila (pocas) o en columna (muchas). Reemplaza al `<select>` de la
 * PWA; la elegida se marca con color + icono de check + `selected` accesible.
 */
export function Opciones<T extends string>({
    etiqueta,
    opciones,
    valor,
    alCambiar,
    error,
    deshabilitado,
    columna,
}: {
    etiqueta: string;
    opciones: readonly Opcion<T>[];
    valor: T | undefined | null;
    alCambiar: (v: T) => void;
    error?: string | undefined;
    deshabilitado?: boolean;
    columna?: boolean;
}) {
    const { colores, radio } = useTema();
    const enColumna = columna ?? opciones.length > 3;
    return (
        <View style={estilos.campo} accessibilityRole="radiogroup" accessibilityLabel={etiqueta}>
            <Text style={[estilos.etiqueta, { color: colores.texto }]}>{etiqueta}</Text>
            <View style={enColumna ? estilos.columna : estilos.filaOpciones}>
                {opciones.map((o) => {
                    const elegida = o.valor === valor;
                    const Icono = o.icono;
                    return (
                        <Pressable
                            key={o.valor}
                            accessibilityRole="radio"
                            accessibilityLabel={o.etiqueta}
                            accessibilityState={{ selected: elegida, checked: elegida, disabled: deshabilitado === true }}
                            disabled={deshabilitado}
                            onPress={() => alCambiar(o.valor)}
                            style={[
                                estilos.opcion,
                                enColumna ? null : estilos.flex,
                                {
                                    borderRadius: radio,
                                    borderColor: elegida ? colores.primario : colores.borde,
                                    backgroundColor: elegida ? colores.fondoTono.info : colores.fondo,
                                    opacity: deshabilitado ? 0.6 : 1,
                                },
                            ]}
                        >
                            {Icono ? <Icono size={16} color={elegida ? colores.primario : colores.textoSuave} /> : null}
                            <View style={enColumna ? estilos.flex : null}>
                                <Text style={[estilos.textoOpcion, { color: elegida ? colores.primario : colores.texto }]}>{o.etiqueta}</Text>
                                {o.detalle ? <Text style={[estilos.descripcion, { color: colores.textoSuave }]}>{o.detalle}</Text> : null}
                            </View>
                        </Pressable>
                    );
                })}
            </View>
            {error ? (
                <Text accessibilityRole="alert" style={[estilos.ayuda, { color: colores.error }]}>
                    {error}
                </Text>
            ) : null}
        </View>
    );
}

/** Hoja modal desde abajo, con título, descripción y su propio desplazamiento. */
export function Dialogo({ abierto, alCerrar, titulo, descripcion, children }: { abierto: boolean; alCerrar: () => void; titulo: string; descripcion?: string; children: ReactNode }) {
    const { colores, radioTarjeta } = useTema();
    return (
        <Modal visible={abierto} transparent animationType="slide" onRequestClose={alCerrar}>
            <View style={estilos.velo}>
                <View style={[estilos.hoja, { backgroundColor: colores.fondo, borderColor: colores.borde, borderTopLeftRadius: radioTarjeta, borderTopRightRadius: radioTarjeta }]}>
                    <ScrollView contentContainerStyle={estilos.contenidoHoja} keyboardShouldPersistTaps="handled">
                        <Text accessibilityRole="header" style={[estilos.tituloTarjeta, { color: colores.texto }]}>
                            {titulo}
                        </Text>
                        {descripcion ? <Text style={[estilos.descripcion, { color: colores.textoSuave }]}>{descripcion}</Text> : null}
                        {children}
                    </ScrollView>
                </View>
            </View>
        </Modal>
    );
}

/** Superficie táctil (tarjeta de orden, paso, pestaña): rol de botón, etiqueta y realce al tocar. */
export function Tocable({
    children,
    alPresionar,
    etiqueta,
    estilo,
    seleccionado,
    rol = "button",
    testID,
}: {
    children: ReactNode;
    alPresionar: () => void;
    etiqueta: string;
    estilo: StyleProp<ViewStyle>;
    /** La vista o la pestaña actual (`selected` accesible). */
    seleccionado?: boolean;
    rol?: "button" | "tab";
    testID?: string;
}) {
    const { colores } = useTema();
    return (
        <Pressable
            accessibilityRole={rol}
            accessibilityLabel={etiqueta}
            accessibilityState={seleccionado === undefined ? undefined : { selected: seleccionado }}
            onPress={alPresionar}
            testID={testID}
            style={({ pressed }) => [estilo, pressed ? { backgroundColor: colores.fondoElevado } : null]}
        >
            {children}
        </Pressable>
    );
}

export function Fila({ children, entre }: { children: ReactNode; entre?: boolean }) {
    return <View style={entre ? estilos.filaEntre : estilos.filaEnvuelta}>{children}</View>;
}

export function Pila({ children, espacio = 12 }: { children: ReactNode; espacio?: number }) {
    return <View style={{ gap: espacio }}>{children}</View>;
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    fila: { flexDirection: "row", alignItems: "center", gap: 8 },
    filaEnvuelta: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 8 },
    filaEntre: { flexDirection: "row", alignItems: "flex-start", justifyContent: "space-between", gap: 8 },
    columna: { gap: 8 },
    filaOpciones: { flexDirection: "row", gap: 8 },
    boton: { minHeight: 48, paddingHorizontal: 16, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8, borderWidth: 1 },
    textoBoton: { fontSize: 16, fontWeight: "600" },
    campo: { gap: 6 },
    filaCampo: { flexDirection: "row", alignItems: "center", gap: 8 },
    etiqueta: { fontSize: 14, fontWeight: "600" },
    entrada: { flex: 1, minHeight: 48, borderWidth: 1, paddingHorizontal: 12, fontSize: 16 },
    sufijo: { fontSize: 14 },
    ayuda: { fontSize: 13 },
    tarjeta: { borderWidth: 1, padding: 16, gap: 12 },
    tituloTarjeta: { fontSize: 18, fontWeight: "600" },
    descripcion: { fontSize: 13 },
    insignia: { flexDirection: "row", alignItems: "center", gap: 4, paddingHorizontal: 8, paddingVertical: 3, borderRadius: 999, alignSelf: "flex-start" },
    textoInsignia: { fontSize: 13, fontWeight: "600" },
    aviso: { borderWidth: 1, padding: 12, gap: 6 },
    tituloAviso: { fontSize: 15, fontWeight: "600", flex: 1 },
    accionAviso: { marginTop: 4, flexDirection: "row", flexWrap: "wrap", gap: 8 },
    texto: { fontSize: 15, lineHeight: 21 },
    negrita: { fontWeight: "600" },
    titulo: { fontSize: 24, fontWeight: "600", letterSpacing: -0.36 },
    opcion: { minHeight: 48, borderWidth: 1, paddingHorizontal: 12, paddingVertical: 8, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8 },
    textoOpcion: { fontSize: 15, fontWeight: "600" },
    velo: { flex: 1, justifyContent: "flex-end", backgroundColor: "rgba(0,0,0,0.45)" },
    hoja: { maxHeight: "90%", borderWidth: 1 },
    contenidoHoja: { padding: 16, gap: 12, paddingBottom: 32 },
});
