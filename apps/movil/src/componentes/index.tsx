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
import { Toast, useToast } from "heroui-native/toast";
import { CircleCheck, Info, TriangleAlert, X, type LucideIcon } from "lucide-react-native";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Keyboard, Modal, Platform, ScrollView, StyleSheet, Text, useWindowDimensions, View, type StyleProp, type TextInputProps, type TextStyle, type ViewStyle } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
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

/**
 * Botón solo con icono (escanear junto a un campo, linterna): 48 × 48 px y nombre accesible
 * obligatorio, porque no hay texto visible. `seleccionado` lo marca encendido (linterna).
 */
export function BotonIcono({
    icono: Icono,
    etiqueta,
    alPresionar,
    variante = "secundario",
    seleccionado,
    colorIcono,
    testID,
}: {
    icono: LucideIcon;
    etiqueta: string;
    alPresionar: () => void;
    variante?: Variante;
    /** Para un interruptor (linterna): `selected` accesible. */
    seleccionado?: boolean;
    /** Color del icono si va sobre un fondo propio (el visor de la cámara). */
    colorIcono?: string;
    testID?: string;
}) {
    const { colores } = useTema();
    const color = colorIcono ?? (variante === "primario" ? colores.sobrePrimario : variante === "fantasma" ? colores.primario : colores.texto);
    return (
        <Button
            variant={VARIANTE_HEROUI[variante]}
            isIconOnly
            onPress={alPresionar}
            accessibilityLabel={etiqueta}
            accessibilityState={seleccionado === undefined ? undefined : { selected: seleccionado }}
            testID={testID}
            className="rounded-lg"
            style={estilos.botonIcono}
        >
            <Icono color={color} size={22} />
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

/**
 * HeroUI `Card` (sólida, borde de 1 px): título, descripción y cuerpo. Radio de 28 px (diseño
 * «combinada»), sobre el panel del tema.
 */
export function Tarjeta({ titulo, descripcion, children, accion, testID }: { titulo?: string; descripcion?: string; children?: ReactNode; accion?: ReactNode; testID?: string }) {
    const { campo, colores, radioTarjeta } = useTema();
    return (
        <Card
            testID={testID}
            className="gap-3 rounded-card border border-border bg-surface p-4"
            style={{ borderRadius: radioTarjeta, backgroundColor: campo.panel, borderColor: colores.borde }}
        >
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
 * Alto del teclado del teléfono mientras está abierto (0 cerrado). Con edge-to-edge (Android 15+)
 * la ventana ya no se encoge sola: quien tenga un campo abajo lo usa para quedar por encima.
 */
export function useAltoTeclado(): number {
    const [alto, setAlto] = useState(0);
    useEffect(() => {
        const abrir = Keyboard.addListener(Platform.OS === "ios" ? "keyboardWillShow" : "keyboardDidShow", (e) => setAlto(e.endCoordinates.height));
        const cerrar = Keyboard.addListener(Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide", () => setAlto(0));
        return () => {
            abrir.remove();
            cerrar.remove();
        };
    }, []);
    return alto;
}

export interface PropsDialogo {
    abierto: boolean;
    alCerrar: () => void;
    titulo: string;
    descripcion?: string;
    children: ReactNode;
    /** Hay algo escrito o elegido sin guardar: cerrar (X, «Atrás») pregunta antes. */
    cambios?: boolean;
    /** Algo fijo arriba del desplazamiento (el campo de búsqueda): nunca se va con la lista. */
    fijo?: ReactNode;
    testID?: string;
}

/**
 * Hoja modal desde abajo, con título, «X» para cerrar, descripción y su propio desplazamiento. Es el
 * `Modal` de React Native (no el `Dialog` de HeroUI) para que el «Atrás» de Android la cierre en vez
 * de cambiar de vista debajo; por dentro lleva la capa flotante de Horus (sólida, `bg-overlay`).
 * Con el teclado abierto la hoja sube por encima de él. Con `cambios`, la «X» y «Atrás» preguntan
 * «¿Descartar los cambios?» antes de cerrar (y un segundo «Atrás» sigue editando).
 */
export function Dialogo({ abierto, alCerrar, titulo, descripcion, children, cambios = false, fijo, testID = "dialogo" }: PropsDialogo) {
    const { colores, radioTarjeta } = useTema();
    const teclado = useAltoTeclado();
    const { height } = useWindowDimensions();
    const margen = useSafeAreaInsets();
    const [confirmando, setConfirmando] = useState(false);
    const intentarCerrar = () => {
        if (confirmando) {
            // Segundo «Atrás» con la pregunta abierta: sigue editando.
            setConfirmando(false);
        } else if (cambios) {
            setConfirmando(true);
        } else {
            setConfirmando(false);
            alCerrar();
        }
    };
    // Sobre el teclado (si está abierto) y nunca debajo de la barra de estado.
    const altoMaximo = Math.max(240, (height - teclado - margen.top - 12) * (teclado > 0 ? 1 : 0.9));
    return (
        <Modal visible={abierto} transparent animationType="slide" onRequestClose={intentarCerrar} testID={testID}>
            <View className="flex-1 justify-end bg-backdrop" testID={`${testID}-velo`} style={[estilos.velo, { paddingBottom: teclado }]}>
                <View
                    accessibilityViewIsModal
                    className="border border-border bg-overlay"
                    style={[estilos.hoja, { maxHeight: altoMaximo, backgroundColor: colores.fondo, borderColor: colores.borde, borderTopLeftRadius: radioTarjeta, borderTopRightRadius: radioTarjeta }]}
                >
                    <View style={estilos.cabezaHoja}>
                        <View style={estilos.flex}>
                            <Text accessibilityRole="header" className="text-lg font-semibold text-foreground" style={[estilos.tituloHoja, { color: colores.texto }]}>
                                {titulo}
                            </Text>
                            {descripcion ? (
                                <Text className="text-[13px] text-muted" style={{ color: colores.textoSuave }}>
                                    {descripcion}
                                </Text>
                            ) : null}
                        </View>
                        <BotonIcono variante="fantasma" icono={X} etiqueta="Cerrar" colorIcono={colores.textoSuave} alPresionar={intentarCerrar} testID={`${testID}-cerrar`} />
                    </View>
                    {confirmando ? (
                        <View style={estilos.confirmar}>
                            <Aviso
                                tono="warning"
                                titulo="¿Descartar los cambios?"
                                testID={`${testID}-confirmar`}
                                accion={
                                    <>
                                        <Boton
                                            variante="peligro"
                                            alPresionar={() => {
                                                setConfirmando(false);
                                                alCerrar();
                                            }}
                                        >
                                            Descartar
                                        </Boton>
                                        <Boton variante="secundario" alPresionar={() => setConfirmando(false)}>
                                            Seguir editando
                                        </Boton>
                                    </>
                                }
                            >
                                Lo que escribiste aquí no se guardó.
                            </Aviso>
                        </View>
                    ) : null}
                    {fijo ? <View style={estilos.fijoHoja}>{fijo}</View> : null}
                    <ScrollView contentContainerStyle={[estilos.contenidoHoja, { paddingBottom: (teclado > 0 ? 16 : 32) + (teclado > 0 ? 0 : margen.bottom) }]} keyboardShouldPersistTaps="handled">
                        {children}
                    </ScrollView>
                </View>
            </View>
        </Modal>
    );
}

/** Un aviso emergente: tono, texto y, si la tiene, una acción («Deshacer», «Revisar»). */
export type ContenidoAvisoEmergente = { tono: TonoEstado; texto: string; accion?: { texto: string; alPresionar: () => void } };

const ICONO_AVISO: Record<TonoEstado, LucideIcon> = { error: TriangleAlert, warning: TriangleAlert, success: CircleCheck, info: Info, neutral: Info };

/** Variante del `Toast` de HeroUI Native para cada tono (HeroUI no tiene «info»: va en «default»). */
const VARIANTE_TOAST = { error: "danger", warning: "warning", success: "success", info: "default", neutral: "default" } as const;

/**
 * Aviso emergente: el `Toast` de HeroUI Native, que entra y sale desde arriba con su animación (y se
 * desliza para cerrarlo), con la acción si la tiene o «Cerrar aviso». Uno a la vez: uno nuevo
 * reemplaza al anterior; `aviso` en `null` lo cierra (el tiempo lo lleva quien avisa).
 */
export function AvisoEmergente({ aviso, alCerrar }: { aviso: ContenidoAvisoEmergente | null; alCerrar: () => void }) {
    const { toast } = useToast();
    const tema = useTema();
    const actual = useRef<{ id: string; aviso: ContenidoAvisoEmergente } | null>(null);
    const cuenta = useRef(0);
    useEffect(() => {
        const previo = actual.current;
        if (previo && previo.aviso !== aviso) {
            actual.current = null;
            // «all» y no su id: el `hide` de un render anterior puede no conocerlo todavía. Solo hay
            // avisos de este componente (uno a la vez).
            toast.hide("all");
        }
        if (!aviso || actual.current) {
            return;
        }
        cuenta.current += 1;
        const id = `aviso-${cuenta.current}`;
        actual.current = { id, aviso };
        toast.show({
            id,
            duration: "persistent",
            // Deslizado fuera: quien avisó también lo olvida.
            onHide: () => {
                if (actual.current?.id === id) {
                    actual.current = null;
                    alCerrar();
                }
            },
            component: ({ id: _id, ...props }) => (
                <Toast
                    {...props}
                    id={_id}
                    variant={VARIANTE_TOAST[aviso.tono]}
                    placement="top"
                    accessibilityLiveRegion="polite"
                    style={[estilos.aviso, estilos.avisoEmergente, { backgroundColor: tema.campo.panel, borderColor: bordeAviso(aviso.tono, tema), shadowColor: tema.campo.sombra }]}
                >
                    <ContenidoToast aviso={aviso} alCerrar={alCerrar} />
                </Toast>
            ),
        });
    }, [aviso, toast, tema, alCerrar]);
    // Al desmontarse (cerrar sesión, cambiar de empresa) no queda un aviso huérfano.
    const ocultar = useRef(toast.hide);
    useEffect(() => {
        ocultar.current = toast.hide;
    });
    useEffect(
        () => () => {
            if (actual.current) {
                actual.current = null;
                ocultar.current("all");
            }
        },
        [],
    );
    return null;
}

function bordeAviso(t: TonoEstado, { campo, colores }: ReturnType<typeof useTema>): string {
    return t === "error" ? campo.errorBorde : t === "warning" ? campo.avisoBorde : colores.borde;
}

function ContenidoToast({ aviso, alCerrar }: { aviso: ContenidoAvisoEmergente; alCerrar: () => void }) {
    const { campo, colores } = useTema();
    const t = aviso.tono;
    const Icono = ICONO_AVISO[t];
    const fondoIcono = t === "error" ? campo.errorFondo : t === "warning" ? campo.avisoFondo : t === "success" ? campo.marcaSuave : t === "info" ? campo.infoFondo : campo.panel2;
    const tintaIcono = t === "success" ? campo.marcaSuaveTexto : colores.tono[t];
    return (
        <View testID="notificacion" style={estilos.filaAviso}>
            <View style={[estilos.iconoAviso, { backgroundColor: fondoIcono }]}>
                <Icono size={20} color={tintaIcono} />
            </View>
            <Toast.Title accessibilityRole={t === "error" ? "alert" : "text"} className="flex-1 text-sm font-medium" style={[estilos.textoAviso, { color: t === "error" ? colores.error : colores.texto }]}>
                {aviso.texto}
            </Toast.Title>
            {aviso.accion ? (
                <Tocable
                    etiqueta={aviso.accion.texto}
                    alPresionar={() => {
                        aviso.accion?.alPresionar();
                        alCerrar();
                    }}
                    estilo={[estilos.accionToast, { backgroundColor: campo.tinta }]}
                >
                    <Text style={[estilos.textoAccionToast, { color: campo.sobreTinta }]}>{aviso.accion.texto}</Text>
                </Tocable>
            ) : (
                <BotonRedondo icono={X} etiqueta="Cerrar aviso" tamano={ZONA_TACTIL} fondo="transparent" tinta={colores.textoSuave} alPresionar={alCerrar} />
            )}
        </View>
    );
}

/** HeroUI `PressableFeedback` (tarjeta de orden, paso, pestaña): rol, etiqueta y realce al tocar. */
export function Tocable({
    children,
    alPresionar,
    etiqueta,
    estilo,
    seleccionado,
    deshabilitado,
    rol = "button",
    testID,
}: {
    children: ReactNode;
    alPresionar: () => void;
    etiqueta: string;
    estilo: StyleProp<ViewStyle>;
    /** La vista o la pestaña actual (`selected` accesible). */
    seleccionado?: boolean;
    /** No se puede tocar (p. ej. un código que no es una MAC): `disabled` accesible. */
    deshabilitado?: boolean;
    rol?: "button" | "tab";
    testID?: string;
}) {
    const estado = seleccionado === undefined && deshabilitado === undefined ? undefined : { ...(seleccionado === undefined ? {} : { selected: seleccionado }), ...(deshabilitado === undefined ? {} : { disabled: deshabilitado }) };
    return (
        <PressableFeedback
            accessibilityRole={rol}
            accessibilityLabel={etiqueta}
            accessibilityState={estado}
            isDisabled={deshabilitado}
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

/**
 * Botón redondo (diseño «combinada»): la isla, buscar, la cuenta, − / +, «Cómo llegar». Solo icono
 * (o un texto corto como las iniciales) con nombre accesible obligatorio; `tamano` en px y nunca
 * menos de 44. `fondo` y `tinta` vienen del tema (`useTema().campo`).
 */
export function BotonRedondo({
    icono: Icono,
    texto,
    etiqueta,
    alPresionar,
    tamano = 48,
    fondo,
    tinta,
    borde,
    seleccionado,
    rol = "button",
    testID,
    children,
}: {
    icono?: LucideIcon;
    texto?: string;
    etiqueta: string;
    alPresionar: () => void;
    tamano?: number;
    fondo: string;
    tinta: string;
    borde?: string;
    seleccionado?: boolean;
    rol?: "button" | "tab";
    testID?: string;
    /** Algo encima del icono (el contador de «Por recibir»). */
    children?: ReactNode;
}) {
    const lado = Math.max(ZONA_TACTIL, tamano);
    return (
        <Tocable
            etiqueta={etiqueta}
            alPresionar={alPresionar}
            rol={rol}
            {...(seleccionado === undefined ? {} : { seleccionado })}
            {...(testID ? { testID } : {})}
            estilo={[
                estilos.redondo,
                { width: lado, height: lado, minHeight: lado, borderRadius: lado / 2, backgroundColor: fondo },
                borde ? { borderWidth: 1, borderColor: borde } : null,
            ]}
        >
            {Icono ? <Icono size={Math.round(lado * 0.42)} color={tinta} strokeWidth={seleccionado ? 2.4 : 2} /> : null}
            {texto ? <Text style={[estilos.textoRedondo, { color: tinta }]}>{texto}</Text> : null}
            {children}
        </Tocable>
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
    botonIcono: { minHeight: 48, minWidth: 48, borderRadius: 6 },
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
    contenidoHoja: { paddingHorizontal: 16, paddingTop: 4, gap: 12 },
    cabezaHoja: { flexDirection: "row", alignItems: "flex-start", gap: 8, paddingLeft: 16, paddingRight: 8, paddingTop: 12, paddingBottom: 4 },
    tituloHoja: { paddingTop: 10 },
    confirmar: { paddingHorizontal: 16, paddingBottom: 8 },
    fijoHoja: { paddingHorizontal: 16, paddingBottom: 8, gap: 8 },
    tocable: { minHeight: ZONA_TACTIL },
    avisoEmergente: {
        flexDirection: "row",
        alignItems: "center",
        gap: 12,
        padding: 12,
        borderRadius: 22,
        shadowOpacity: 0.14,
        shadowRadius: 18,
        shadowOffset: { width: 0, height: 10 },
        elevation: 10,
    },
    filaAviso: { flexDirection: "row", alignItems: "center", gap: 12 },
    iconoAviso: { width: 40, height: 40, borderRadius: 20, alignItems: "center", justifyContent: "center" },
    textoAviso: { flex: 1, fontSize: 14, fontWeight: "500" },
    accionToast: { minHeight: ZONA_TACTIL, borderRadius: 999, paddingHorizontal: 14, justifyContent: "center" },
    textoAccionToast: { fontWeight: "600", fontSize: 13 },
    cerrarToast: { minWidth: ZONA_TACTIL, minHeight: ZONA_TACTIL, borderRadius: 22 },
    redondo: { alignItems: "center", justifyContent: "center" },
    textoRedondo: { fontSize: 15, fontWeight: "600" },
});
