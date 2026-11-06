// Encabezado de la app (opción elegida por el PM, nov 2026, como la PWA): no queda fijo, va dentro
// del desplazamiento y se va con el contenido; al subir vuelve la barra compacta (`BarraCompacta`,
// sobre el contenido). A la izquierda la fecha y, en Hoy, Mañana y Semana, «Hola, <nombre>»; a la
// derecha los indicadores (sincronización y GPS, abren la hoja de estado) y buscar. La cuenta está en
// «Yo» de la isla. En Hoy, Mañana y Semana, la barra segmentada del día con su contador (solo con
// datos leídos). Las reglas son de `@iris/sync/campo`.
import { buscarOrdenes, CHIPS_DIA, ESPERA_BUSQUEDA_MS, chipDe, citasDelDia, esVistaDeDia, fechaEncabezado, mananaEnGuatemala, nombreCliente, primerNombre, resumenHoy, TIPOS_ORDEN, type ChipDia } from "@iris/sync/campo";
import { ChevronRight, Search } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { Animated, Easing, StyleSheet, Text, View } from "react-native";
import { BotonRedondo, Campo, Dialogo, Pila, Texto, Tocable } from "../componentes";
import { useAgenda } from "../campo/agenda";
import { useDemorado } from "../campo/demora";
import { useCampo } from "../campo/contexto";
import { useTema } from "../tema/tema";
import { IndicadoresEstado } from "./Estado";
import { InsigniaOrden } from "./ListaOrdenes";

export interface SalidasEstado {
    /** La sesión venció: volver a entrar (desde la hoja de estado). */
    alEntrar: () => void;
    alElegirEmpresa?: (() => void) | undefined;
}

export function Encabezado({ alEntrar, alElegirEmpresa }: SalidasEstado) {
    const { sesion, vista } = useCampo();
    const { colores } = useTema();
    const deDia = esVistaDeDia(vista);
    return (
        <View testID="encabezado-campo" style={estilos.encabezado}>
            <View style={estilos.fila}>
                <View style={[estilos.flex, estilos.titulos]}>
                    <Text style={[estilos.textoFecha, { color: colores.textoSuave }]}>{fechaEncabezado()}</Text>
                    {deDia ? (
                        <Text accessibilityRole="header" style={[estilos.saludo, { color: colores.texto }]}>
                            Hola, {primerNombre(sesion.nombre)}
                        </Text>
                    ) : null}
                </View>
                <BotonesCuenta alEntrar={alEntrar} alElegirEmpresa={alElegirEmpresa} />
            </View>
            {deDia ? <ChipsDia /> : null}
        </View>
    );
}

function BotonesCuenta({ compacta = false, ...salidas }: SalidasEstado & { compacta?: boolean }) {
    const { campo, colores } = useTema();
    const [buscando, setBuscando] = useState(false);
    return (
        <View style={estilos.botones}>
            <IndicadoresEstado compacta={compacta} {...salidas} />
            <BotonRedondo icono={Search} etiqueta="Buscar orden" fondo={campo.panel} tinta={colores.texto} borde={colores.borde} alPresionar={() => setBuscando(true)} />
            <BuscarOrden abierto={buscando} alCerrar={() => setBuscando(false)} />
        </View>
    );
}

/**
 * Hoy · Mañana · Semana en una sola barra segmentada (opciones excluyentes, `radio` accesible) con
 * su contador (solo con datos leídos: nunca un total inventado).
 */
function ChipsDia() {
    const { vista, navegar, estado } = useCampo();
    const { semana } = useAgenda();
    const { campo, colores } = useTema();
    const manana = mananaEnGuatemala();
    const actual = chipDe(vista, manana);
    const cuentas: Partial<Record<ChipDia, number>> = {
        ...(estado.fecha !== null ? { hoy: resumenHoy(estado.ordenes).activas } : {}),
        ...(semana.datos ? { manana: citasDelDia(semana.datos.ordenes, manana).length } : {}),
    };
    return (
        <View accessibilityLabel="Día" accessibilityRole="radiogroup" style={[estilos.chips, { backgroundColor: campo.panel, borderColor: colores.borde }]}>
            {CHIPS_DIA.map((c) => {
                const activo = actual === c.id;
                const cuenta = cuentas[c.id];
                const texto = cuenta === undefined ? c.texto : `${c.texto} · ${cuenta}`;
                return (
                    <Tocable
                        key={c.id}
                        etiqueta={texto}
                        seleccionado={activo}
                        testID={`chip-${c.id}`}
                        alPresionar={() => navegar(c.vista)}
                        estilo={[estilos.chip, { backgroundColor: activo ? campo.tinta : "transparent" }]}
                    >
                        <Text style={[estilos.textoChip, { color: activo ? campo.sobreTinta : colores.texto, fontWeight: activo ? "600" : "500" }]}>{texto}</Text>
                    </Tocable>
                );
            })}
        </View>
    );
}

/**
 * Barra compacta: entra desde arriba al subir con el encabezado fuera de vista; al bajar se va
 * deslizándose entera hacia arriba. Siempre sólida (el panel del tema, claro u oscuro): nunca queda
 * a medio transparentar sobre el contenido (no se anima la opacidad, solo la posición). Va justo
 * debajo de la franja de la barra de estado (`top: arriba`), que queda siempre a la vista. Oculta, no
 * se toca ni la leen los lectores de pantalla.
 *
 * La posición se anima en JS (`useNativeDriver: false`) con una interpolación estable: con el driver
 * nativo y la arquitectura nueva, cada evento del desplazamiento vuelve a pintar el marco y el valor
 * JS (todavía «oculta») pisaba el nativo, así que la barra entraba y desaparecía al instante.
 */
export function BarraCompacta({ visible, arriba, ...salidas }: { visible: boolean; arriba: number } & SalidasEstado) {
    const { sesion } = useCampo();
    const { campo, colores } = useTema();
    const [alto, setAlto] = useState(64);
    const [posicion] = useState(() => new Animated.Value(visible ? 0 : 1));
    useEffect(() => {
        const animacion = Animated.timing(posicion, { toValue: visible ? 0 : 1, duration: 200, easing: Easing.out(Easing.cubic), useNativeDriver: false });
        animacion.start();
        return () => animacion.stop();
    }, [visible, posicion]);
    // Fuera de vista del todo: su alto y la sombra (el recorte la deja bajo la franja).
    const translateY = useMemo(() => posicion.interpolate({ inputRange: [0, 1], outputRange: [0, -(alto + 24)] }), [posicion, alto]);
    return (
        // Recorta lo que sube: al irse no cruza la franja de la barra de estado (ni en Android, donde
        // la elevación de la barra la pondría encima). Deja pasar los toques al contenido.
        <View testID="recorte-compacta" pointerEvents="box-none" style={[estilos.recorte, { top: arriba }]}>
            <Animated.View
                testID="barra-compacta"
                pointerEvents={visible ? "auto" : "none"}
                accessibilityElementsHidden={!visible}
                importantForAccessibility={visible ? "auto" : "no-hide-descendants"}
                onLayout={(e) => setAlto(e.nativeEvent.layout.height)}
                style={[estilos.compacta, { paddingTop: 6, backgroundColor: campo.panel, borderColor: colores.borde, shadowColor: campo.sombra, transform: [{ translateY }] }]}
            >
                <Text numberOfLines={1} style={[estilos.flex, estilos.textoCompacta, { color: colores.texto }]}>
                    Hola, {primerNombre(sesion.nombre)}
                </Text>
                <BotonesCuenta compacta {...salidas} />
            </Animated.View>
        </View>
    );
}

/**
 * Busca entre las órdenes del teléfono (número, cliente o dirección); funciona sin señal. En vivo:
 * filtra al dejar de escribir (`ESPERA_BUSQUEDA_MS`) y el campo queda fijo arriba de la hoja, que
 * sube por encima del teclado (`Dialogo`); los resultados se desplazan debajo.
 */
function BuscarOrden({ abierto, alCerrar }: { abierto: boolean; alCerrar: () => void }) {
    const { estado, navegar } = useCampo();
    const { campo, colores } = useTema();
    const [texto, setTexto] = useState("");
    const consulta = useDemorado(texto, ESPERA_BUSQUEDA_MS);
    const encontradas = useMemo(() => buscarOrdenes(estado.ordenes, consulta), [estado.ordenes, consulta]);
    const cerrar = () => {
        setTexto("");
        alCerrar();
    };
    return (
        <Dialogo
            abierto={abierto}
            alCerrar={cerrar}
            titulo="Buscar orden"
            descripcion="Entre las órdenes de tu teléfono: número, cliente o dirección."
            testID="buscar-orden"
            fijo={<Campo etiqueta="Buscar" icono={Search} valor={texto} alCambiar={setTexto} autoCorrect={false} autoCapitalize="none" autoFocus returnKeyType="search" />}
        >
            {consulta.trim().length === 0 ? null : encontradas.length === 0 ? (
                <Texto suave testID="sin-resultados">
                    Ninguna orden del teléfono coincide con «{consulta.trim()}».
                </Texto>
            ) : (
                <View accessibilityLabel="Órdenes encontradas" accessibilityLiveRegion="polite" testID="resultados-busqueda">
                    <Pila espacio={8}>
                        <Texto suave>{encontradas.length === 1 ? "1 orden" : `${encontradas.length} órdenes`}</Texto>
                        {encontradas.map((o) => (
                            <Tocable
                                key={o.id}
                                etiqueta={`${o.numero}, ${nombreCliente(o)}`}
                                alPresionar={() => {
                                    cerrar();
                                    navegar({ tipo: "orden", id: o.id });
                                }}
                                estilo={[estilos.resultado, { backgroundColor: campo.panel2 }]}
                            >
                                <View style={[estilos.flex, estilos.titulos]}>
                                    <View style={estilos.fecha}>
                                        <Texto mono>{o.numero}</Texto>
                                        <InsigniaOrden estado={o.estado} />
                                    </View>
                                    <Texto negrita>{nombreCliente(o)}</Texto>
                                    <Texto suave>{TIPOS_ORDEN[o.tipo]}</Texto>
                                </View>
                                <ChevronRight size={20} color={colores.textoSuave} />
                            </Tocable>
                        ))}
                    </Pila>
                </View>
            )}
        </Dialogo>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    encabezado: { gap: 12, paddingTop: 8 },
    fila: { flexDirection: "row", alignItems: "flex-start", gap: 12 },
    titulos: { gap: 6 },
    fecha: { flexDirection: "row", alignItems: "center", gap: 8, flexWrap: "wrap" },
    textoFecha: { fontSize: 13, fontWeight: "500" },
    saludo: { fontSize: 32, lineHeight: 34, fontWeight: "700", letterSpacing: -0.96 },
    botones: { flexDirection: "row", alignItems: "center", gap: 6 },
    chips: { flexDirection: "row", gap: 4, padding: 4, borderRadius: 26, borderWidth: 1 },
    chip: { flex: 1, height: 44, minHeight: 44, paddingHorizontal: 12, borderRadius: 22, alignItems: "center", justifyContent: "center" },
    textoChip: { fontSize: 14 },
    recorte: { position: "absolute", left: 0, right: 0, paddingBottom: 24, overflow: "hidden" },
    compacta: {
        flexDirection: "row",
        alignItems: "center",
        gap: 12,
        paddingHorizontal: 18,
        paddingBottom: 8,
        borderBottomWidth: 1,
        shadowOpacity: 0.08,
        shadowRadius: 16,
        shadowOffset: { width: 0, height: 6 },
        elevation: 6,
    },
    textoCompacta: { fontSize: 15, fontWeight: "600" },
    resultado: { flexDirection: "row", alignItems: "center", gap: 12, padding: 12, borderRadius: 22 },
});
