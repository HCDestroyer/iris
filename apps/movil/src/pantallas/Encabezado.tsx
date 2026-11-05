// Encabezado de la app (diseño «combinada», como la PWA): no queda fijo, va dentro del
// desplazamiento y se va con el contenido; al subir vuelve la barra compacta (`BarraCompacta`, sobre
// el contenido). Lleva la fecha con el punto de conexión, buscar y la cuenta; en Hoy, Mañana y Semana
// además «Hola, <nombre>» y los chips con su contador (solo con datos leídos). La ubicación en vivo
// va siempre. Las reglas son de `@iris/sync/campo`.
import { buscarOrdenes, CHIPS_DIA, chipDe, citasDelDia, esVistaDeDia, fechaEncabezado, iniciales, mananaEnGuatemala, nombreCliente, primerNombre, resumenHoy, TIPOS_ORDEN, type ChipDia } from "@iris/sync/campo";
import { ChevronRight, Search } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import { BotonRedondo, Campo, Dialogo, Pila, Texto, Tocable } from "../componentes";
import { useAgenda } from "../campo/agenda";
import { useCampo } from "../campo/contexto";
import { useTema } from "../tema/tema";
import { InsigniaOrden } from "./ListaOrdenes";

export function Encabezado({ punto, ubicacion, empresa }: { punto: ReactNode; ubicacion: ReactNode; empresa: string | null }) {
    const { sesion, vista } = useCampo();
    const { colores } = useTema();
    const deDia = esVistaDeDia(vista);
    return (
        <View testID="encabezado-campo" style={estilos.encabezado}>
            <View style={estilos.fila}>
                <View style={[estilos.flex, estilos.titulos]}>
                    <View style={estilos.fecha}>
                        <Text style={[estilos.textoFecha, { color: colores.textoSuave }]}>{fechaEncabezado()}</Text>
                        {punto}
                    </View>
                    {deDia ? (
                        <Text accessibilityRole="header" style={[estilos.saludo, { color: colores.texto }]}>
                            Hola, {primerNombre(sesion.nombre)}
                        </Text>
                    ) : null}
                    {/* Quién está (un teléfono puede ser de varios técnicos) y, con varias, en qué empresa. */}
                    <Text numberOfLines={1} style={[estilos.textoFecha, { color: colores.textoSuave }]}>
                        {sesion.nombre}
                        {empresa ? ` · ${empresa}` : ""}
                    </Text>
                </View>
                <BotonesCuenta />
            </View>
            {ubicacion}
            {deDia ? <ChipsDia /> : null}
        </View>
    );
}

function BotonesCuenta() {
    const { sesion, navegar } = useCampo();
    const { campo, colores } = useTema();
    const [buscando, setBuscando] = useState(false);
    return (
        <View style={estilos.botones}>
            <BotonRedondo icono={Search} etiqueta="Buscar orden" fondo={campo.panel} tinta={colores.texto} borde={colores.borde} alPresionar={() => setBuscando(true)} />
            <BotonRedondo texto={iniciales(sesion.nombre) || "Yo"} etiqueta="Tu cuenta" fondo={colores.primario} tinta={colores.sobrePrimario} alPresionar={() => navegar({ tipo: "yo" })} />
            <BuscarOrden abierto={buscando} alCerrar={() => setBuscando(false)} />
        </View>
    );
}

/** Chips Hoy · Mañana · Semana con su contador (solo con datos leídos: nunca un total inventado). */
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
        <View accessibilityLabel="Día" style={estilos.chips}>
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
                        estilo={[
                            estilos.chip,
                            activo ? { backgroundColor: campo.tinta } : { backgroundColor: campo.panel, borderColor: colores.borde, borderWidth: 1 },
                        ]}
                    >
                        <Text style={[estilos.textoChip, { color: activo ? campo.sobreTinta : colores.texto, fontWeight: activo ? "600" : "500" }]}>{texto}</Text>
                    </Tocable>
                );
            })}
        </View>
    );
}

/** Barra compacta: vuelve arriba al subir con el encabezado fuera de vista; al bajar se va. */
export function BarraCompacta({ visible, arriba }: { visible: boolean; arriba: number }) {
    const { sesion } = useCampo();
    const { campo, colores } = useTema();
    if (!visible) {
        return null;
    }
    return (
        <View testID="barra-compacta" style={[estilos.compacta, { paddingTop: arriba + 6, backgroundColor: campo.panel, borderColor: colores.borde, shadowColor: campo.sombra }]}>
            <Text numberOfLines={1} style={[estilos.flex, estilos.textoCompacta, { color: colores.texto }]}>
                Hola, {primerNombre(sesion.nombre)}
            </Text>
            <BotonesCuenta />
        </View>
    );
}

/** Busca entre las órdenes del teléfono (número, cliente o dirección); funciona sin señal. */
function BuscarOrden({ abierto, alCerrar }: { abierto: boolean; alCerrar: () => void }) {
    const { estado, navegar } = useCampo();
    const { campo, colores } = useTema();
    const [texto, setTexto] = useState("");
    const encontradas = buscarOrdenes(estado.ordenes, texto);
    const cerrar = () => {
        setTexto("");
        alCerrar();
    };
    return (
        <Dialogo abierto={abierto} alCerrar={cerrar} titulo="Buscar orden" descripcion="Entre las órdenes de tu teléfono: número, cliente o dirección.">
            <Campo etiqueta="Buscar" valor={texto} alCambiar={setTexto} autoCorrect={false} autoCapitalize="none" />
            {texto.trim().length === 0 ? null : encontradas.length === 0 ? (
                <Texto suave>Ninguna orden del teléfono coincide.</Texto>
            ) : (
                <Pila espacio={8}>
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
    botones: { flexDirection: "row", gap: 8 },
    chips: { flexDirection: "row", gap: 8, flexWrap: "wrap" },
    chip: { height: 44, minHeight: 44, paddingHorizontal: 18, borderRadius: 22, alignItems: "center", justifyContent: "center" },
    textoChip: { fontSize: 14 },
    compacta: {
        position: "absolute",
        top: 0,
        left: 0,
        right: 0,
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
