// Componentes de la app nativa con la identidad Horus: el equivalente de `@iris/ui` en React Native.
// Por dentro son HeroUI Native (Uniwind) con el tema de `src/global.css`; los nombres y las props
// no cambiaron, así que las pantallas siguen igual. Las pantallas no usan `Pressable`, `TextInput`
// ni `Modal` crudos ni importan de `heroui-native`: pasan por aquí (regla 10 del repo, en RN).
//
// - Las medidas táctiles (44 px los botones y las opciones, 48 px los campos) van en `style`, no solo
//   en clases: son la regla que se prueba y no dependen del compilador de Uniwind.
// - Los íconos son de Lucide y toman el color de `useTema()` (los mismos `--hx-*`).
// - `accent` es la marca (teal) y nunca un estado; «info» es el azul de Horus (`bg-suave-info`).
import type { TonoEstado } from "@iris/sync/campo";
// Importes por componente (no desde la raíz de `heroui-native`): el bundle solo lleva lo que se usa.
import { Alert } from "heroui-native/alert";
import { Button } from "heroui-native/button";
import { Card } from "heroui-native/card";
import { Chip } from "heroui-native/chip";
import { Description } from "heroui-native/description";
import { FieldError } from "heroui-native/field-error";
import { Input } from "heroui-native/input";
import { Label } from "heroui-native/label";
import { PressableFeedback } from "heroui-native/pressable-feedback";
import { Radio } from "heroui-native/radio";
import { RadioGroup } from "heroui-native/radio-group";
import { Spinner } from "heroui-native/spinner";
import { TextField } from "heroui-native/text-field";
import type { LucideIcon } from "lucide-react-native";
import type { ReactNode } from "react";
import { Modal, ScrollView, StyleSheet, Text, View, type StyleProp, type TextInputProps, type TextStyle, type ViewStyle } from "react-native";
import { useTema } from "../tema/tema";

/** Alto mínimo de cualquier zona táctil (WCAG 2.5.8 y guía de Iris: 44 px; con guante, más). */
export const ZONA_TACTIL = 44;

export type Variante = "primario" | "secundario" | "fantasma" | "peligro";

const VARIANTE_HEROUI = { primario: "primary", secundario: "outline", fantasma: "ghost", peligro: "danger" } as const;

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

/**
 * HeroUI `Button` con la esquina de Horus (6 px, no píldora) y 48 px de alto (dedo con guante). El
 * texto puede ocupar dos líneas: el alto crece, nunca se corta.
 */
export function Boton({ children, alPresionar, variante = "primario", icono: Icono, deshabilitado, cargando, etiqueta, estilo, testID }: PropsBoton) {
    const { colores } = useTema();
    const texto =
        variante === "primario" ? colores.sobrePrimario : variante === "peligro" ? colores.fondo : variante === "secundario" ? colores.texto : colores.primario;
    const inactivo = deshabilitado === true || cargando === true;
    return (
        <Button
            variant={VARIANTE_HEROUI[variante]}
            isDisabled={inactivo}
            onPress={alPresionar}
            accessibilityLabel={etiqueta}
            accessibilityState={{ disabled: inactivo, busy: cargando === true }}
            testID={testID}
            className="h-auto rounded-lg px-4 py-2"
            style={[estilos.boton, estilo]}
        >
            {cargando ? <Spinner size="sm" color={texto} /> : Icono ? <Icono color={texto} size={18} /> : null}
            <Button.Label className={variante === "fantasma" ? "shrink text-center font-semibold text-accent" : "shrink text-center font-semibold"} style={{ color: texto }}>
                {children}
            </Button.Label>
        </Button>
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

/** HeroUI `TextField`: etiqueta visible, ayuda y error (texto + color, nunca solo color). */
export function Campo({ etiqueta, valor, alCambiar, error, ayuda, sufijo, editable, ...resto }: PropsCampo) {
    const { colores } = useTema();
    const invalido = Boolean(error);
    return (
        <TextField isInvalid={invalido} isDisabled={editable === false} className="gap-1.5">
            <Label>
                <Label.Text className="text-sm font-semibold text-foreground">{etiqueta}</Label.Text>
            </Label>
            <View style={estilos.filaCampo}>
                <Input
                    accessibilityLabel={etiqueta}
                    value={valor}
                    onChangeText={alCambiar}
                    editable={editable}
                    placeholderTextColor={colores.textoTenue}
                    className="flex-1 px-3 text-base"
                    style={estilos.entrada}
                    {...resto}
                />
                {sufijo ? <Text className="text-sm text-muted">{sufijo}</Text> : null}
            </View>
            {ayuda && !invalido ? <Description className="text-[13px] text-muted">{ayuda}</Description> : null}
            {invalido ? (
                <FieldError accessible accessibilityRole="alert" className="text-[13px]">
                    {error}
                </FieldError>
            ) : null}
        </TextField>
    );
}

/** HeroUI `Card` (`surface-card`: casi sólida, radio 14 px, borde de 1 px): título, descripción y cuerpo. */
export function Tarjeta({ titulo, descripcion, children, accion, testID }: { titulo?: string; descripcion?: string; children?: ReactNode; accion?: ReactNode; testID?: string }) {
    return (
        <Card testID={testID} className="gap-3 rounded-card border border-border bg-surface p-4">
            {titulo || accion ? (
                <Card.Header className="flex-row items-start justify-between gap-2">
                    <View style={estilos.flex}>
                        {titulo ? (
                            <Card.Title accessibilityRole="header" className="text-lg font-semibold text-foreground">
                                {titulo}
                            </Card.Title>
                        ) : null}
                        {descripcion ? <Card.Description className="text-[13px] text-muted">{descripcion}</Card.Description> : null}
                    </View>
                    {accion}
                </Card.Header>
            ) : null}
            {children ? <Card.Body className="gap-3">{children}</Card.Body> : null}
        </Card>
    );
}

/** Color de HeroUI de cada tono (HeroUI no tiene «info»: va con el azul de Horus por clase). */
const CHIP_COLOR = { neutral: "default", info: "default", success: "success", warning: "warning", error: "danger" } as const;
const CHIP_CLASE: Record<TonoEstado, string> = {
    neutral: "self-start bg-default",
    info: "self-start bg-suave-info",
    success: "self-start bg-suave-success",
    warning: "self-start bg-suave-warning",
    error: "self-start bg-suave-danger",
};

/** HeroUI `Chip` suave: estado con icono + texto + color (nunca solo color). */
export function Insignia({ tono, icono: Icono, texto, testID }: { tono: TonoEstado; icono: LucideIcon; texto: string; testID?: string }) {
    const { colores } = useTema();
    return (
        <Chip testID={testID} variant="soft" color={CHIP_COLOR[tono]} size="sm" className={CHIP_CLASE[tono]} style={estilos.insignia}>
            <Icono color={colores.tono[tono]} size={14} />
            <Chip.Label className="text-[13px] font-semibold" style={{ color: colores.tono[tono] }}>
                {texto}
            </Chip.Label>
        </Chip>
    );
}

const ALERTA_ESTADO = { neutral: "default", info: "default", success: "success", warning: "warning", error: "danger" } as const;
const ALERTA_CLASE: Record<TonoEstado, string> = {
    neutral: "rounded-lg border border-border bg-default",
    info: "rounded-lg border border-info bg-suave-info",
    success: "rounded-lg border border-success bg-suave-success",
    warning: "rounded-lg border border-warning bg-suave-warning",
    error: "rounded-lg border border-danger bg-suave-danger",
};

/** HeroUI `Alert` con el fondo tintado de Horus: tono, icono, título, detalle y una acción opcional. */
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
    const { colores } = useTema();
    return (
        <Alert testID={testID} status={ALERTA_ESTADO[tono]} role={tono === "error" ? "alert" : "summary"} className={ALERTA_CLASE[tono]} style={estilos.aviso}>
            {Icono ? (
                <Alert.Indicator>
                    <Icono color={colores.tono[tono]} size={18} />
                </Alert.Indicator>
            ) : null}
            <Alert.Content className="gap-1.5">
                <Alert.Title className="text-[15px] font-semibold" style={{ color: colores.tono[tono] }}>
                    {titulo}
                </Alert.Title>
                {typeof children === "string" ? <Texto>{children}</Texto> : children}
                {accion ? <View style={estilos.accionAviso}>{accion}</View> : null}
            </Alert.Content>
        </Alert>
    );
}

export function Texto({ children, suave, mono, negrita, estilo, testID }: { children: ReactNode; suave?: boolean; mono?: boolean; negrita?: boolean; estilo?: StyleProp<TextStyle>; testID?: string }) {
    const { colores, fuenteMono } = useTema();
    return (
        <Text
            testID={testID}
            className={suave ? "text-[15px] leading-[21px] text-muted" : "text-[15px] leading-[21px] text-foreground"}
            style={[{ color: suave ? colores.textoSuave : colores.texto }, mono ? { fontFamily: fuenteMono } : null, negrita ? estilos.negrita : null, estilo]}
        >
            {children}
        </Text>
    );
}

export function Titulo({ children }: { children: ReactNode }) {
    const { colores } = useTema();
    return (
        <Text accessibilityRole="header" className="text-2xl font-semibold tracking-tight text-foreground" style={[estilos.titulo, { color: colores.texto }]}>
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
 * HeroUI `RadioGroup`: botones en fila (pocas) o en columna (muchas). Reemplaza al `<select>` de la
 * PWA; la elegida se marca con borde teal + el punto del `Radio` + `selected` accesible (nunca solo color).
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
    const { colores } = useTema();
    const enColumna = columna ?? opciones.length > 3;
    const porValor = new Map(opciones.map((o) => [o.valor as string, o.valor]));
    return (
        <View style={estilos.campo}>
            <Text className="text-sm font-semibold text-foreground" style={{ color: colores.texto }}>
                {etiqueta}
            </Text>
            <RadioGroup
                value={valor ?? undefined}
                onValueChange={(v) => {
                    const elegido = porValor.get(v);
                    if (elegido !== undefined) {
                        alCambiar(elegido);
                    }
                }}
                isDisabled={deshabilitado}
                isInvalid={Boolean(error)}
                accessibilityRole="radiogroup"
                accessibilityLabel={etiqueta}
                style={enColumna ? estilos.columna : estilos.filaOpciones}
            >
                {opciones.map((o) => {
                    const elegida = o.valor === valor;
                    const Icono = o.icono;
                    const tinta = elegida ? colores.primario : colores.texto;
                    return (
                        <RadioGroup.Item
                            key={o.valor}
                            value={o.valor}
                            accessibilityLabel={o.etiqueta}
                            accessibilityState={{ selected: elegida, checked: elegida, disabled: deshabilitado === true }}
                            className={elegida ? "rounded-lg border border-accent bg-suave-accent" : "rounded-lg border border-border bg-background"}
                            style={[
                                estilos.opcion,
                                enColumna ? null : estilos.flex,
                                { borderColor: elegida ? colores.primario : colores.borde, opacity: deshabilitado ? 0.6 : 1 },
                            ]}
                        >
                            {Icono ? <Icono size={16} color={elegida ? colores.primario : colores.textoSuave} /> : null}
                            <View style={enColumna ? estilos.flex : null}>
                                <Text className="text-[15px] font-semibold" style={{ color: tinta }}>
                                    {o.etiqueta}
                                </Text>
                                {o.detalle ? (
                                    <Text className="text-[13px] text-muted" style={{ color: colores.textoSuave }}>
                                        {o.detalle}
                                    </Text>
                                ) : null}
                            </View>
                            <Radio />
                        </RadioGroup.Item>
                    );
                })}
            </RadioGroup>
            {error ? (
                <Text accessibilityRole="alert" className="text-[13px] text-danger" style={{ color: colores.error }}>
                    {error}
                </Text>
            ) : null}
        </View>
    );
}

/**
 * Hoja modal desde abajo, con título, descripción y su propio desplazamiento. Es el `Modal` de React
 * Native (no el `Dialog` de HeroUI) para que el «Atrás» de Android la cierre en vez de cambiar de
 * vista debajo; por dentro lleva la capa flotante de Horus (vidrio fuerte, `bg-overlay`).
 */
export function Dialogo({ abierto, alCerrar, titulo, descripcion, children }: { abierto: boolean; alCerrar: () => void; titulo: string; descripcion?: string; children: ReactNode }) {
    const { colores, radioTarjeta } = useTema();
    return (
        <Modal visible={abierto} transparent animationType="slide" onRequestClose={alCerrar} testID="dialogo">
            <View className="flex-1 justify-end bg-backdrop" style={estilos.velo}>
                <View
                    accessibilityViewIsModal
                    className="border border-border bg-overlay"
                    style={[estilos.hoja, { backgroundColor: colores.fondo, borderColor: colores.borde, borderTopLeftRadius: radioTarjeta, borderTopRightRadius: radioTarjeta }]}
                >
                    <ScrollView contentContainerStyle={estilos.contenidoHoja} keyboardShouldPersistTaps="handled">
                        <Text accessibilityRole="header" className="text-lg font-semibold text-foreground" style={{ color: colores.texto }}>
                            {titulo}
                        </Text>
                        {descripcion ? (
                            <Text className="text-[13px] text-muted" style={{ color: colores.textoSuave }}>
                                {descripcion}
                            </Text>
                        ) : null}
                        {children}
                    </ScrollView>
                </View>
            </View>
        </Modal>
    );
}

/** HeroUI `PressableFeedback` (tarjeta de orden, paso, pestaña): rol, etiqueta y realce al tocar. */
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
    return (
        <PressableFeedback
            accessibilityRole={rol}
            accessibilityLabel={etiqueta}
            accessibilityState={seleccionado === undefined ? undefined : { selected: seleccionado }}
            onPress={alPresionar}
            testID={testID}
            animation={{ scale: { value: 0.99 } }}
            style={[estilos.tocable, estilo]}
        >
            <PressableFeedback.Highlight />
            {children}
        </PressableFeedback>
    );
}

export function Fila({ children, entre }: { children: ReactNode; entre?: boolean }) {
    return <View style={entre ? estilos.filaEntre : estilos.filaEnvuelta}>{children}</View>;
}

export function Pila({ children, espacio = 12 }: { children: ReactNode; espacio?: number }) {
    return <View style={{ gap: espacio }}>{children}</View>;
}

/** Indicador de carga de HeroUI con el teal de la marca y un nombre para lectores de pantalla. */
export function Cargando({ etiqueta }: { etiqueta: string }) {
    const { colores } = useTema();
    return (
        <View accessible accessibilityRole="progressbar" accessibilityLabel={etiqueta}>
            <Spinner color={colores.primario} />
        </View>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    filaEnvuelta: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 8 },
    filaEntre: { flexDirection: "row", alignItems: "flex-start", justifyContent: "space-between", gap: 8 },
    columna: { gap: 8 },
    filaOpciones: { flexDirection: "row", gap: 8 },
    boton: { minHeight: 48, minWidth: ZONA_TACTIL, borderRadius: 6 },
    campo: { gap: 6 },
    filaCampo: { flexDirection: "row", alignItems: "center", gap: 8 },
    entrada: { minHeight: 48 },
    insignia: { alignSelf: "flex-start", flexDirection: "row", alignItems: "center", gap: 4 },
    aviso: { borderWidth: 1 },
    accionAviso: { marginTop: 4, flexDirection: "row", flexWrap: "wrap", gap: 8 },
    negrita: { fontWeight: "600" },
    titulo: { fontSize: 24, fontWeight: "600", letterSpacing: -0.36 },
    opcion: { minHeight: 48, borderWidth: 1, borderRadius: 6, paddingHorizontal: 12, paddingVertical: 8, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8 },
    velo: { backgroundColor: "rgba(0,0,0,0.45)" },
    hoja: { maxHeight: "90%", borderWidth: 1 },
    contenidoHoja: { padding: 16, gap: 12, paddingBottom: 32 },
    tocable: { minHeight: ZONA_TACTIL },
});
