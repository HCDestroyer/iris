// Barra inferior de la app de campo: Hoy · Por recibir · Pendientes · Yo, con zonas táctiles de
// 56 px y el contador de lo que falta por enviar y por recibir (`contadoresPestanas` de
// `@iris/sync/campo`, igual que la PWA). La lista de traslados se lee al abrir la app con señal.
import { contadoresPestanas, pestanaDe, PESTANAS, type Pestana } from "@iris/sync/campo";
import { CalendarCheck, CloudUpload, PackageOpen, UserRound, type LucideIcon } from "lucide-react-native";
import { StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Tocable } from "../componentes";
import { useCampo } from "../campo/contexto";
import { useCargarAlAbrir, useInventario } from "../campo/inventario";
import { useTema } from "../tema/tema";

const ICONOS: Record<Pestana, LucideIcon> = { hoy: CalendarCheck, recibir: PackageOpen, pendientes: CloudUpload, yo: UserRound };

export function NavegacionInferior() {
    const { vista, navegar, estado } = useCampo();
    const { traslados, cargarTraslados } = useInventario();
    // El contador de «Por recibir» se lee al abrir la app (y al volver la señal).
    useCargarAlAbrir(cargarTraslados);
    const { colores } = useTema();
    const margen = useSafeAreaInsets();
    const actual = pestanaDe(vista);
    const cuentas = contadoresPestanas(estado, traslados.datos);
    return (
        <View
            accessibilityRole="tablist"
            accessibilityLabel="Secciones de la app"
            testID="navegacion-inferior"
            style={[estilos.barra, { borderColor: colores.borde, backgroundColor: colores.vidrio, paddingBottom: margen.bottom }]}
        >
            {PESTANAS.map((p) => {
                const Icono = ICONOS[p.id];
                const activa = actual === p.id;
                const cuenta = cuentas[p.id];
                const color = activa ? colores.primario : colores.textoSuave;
                return (
                    <Tocable
                        key={p.id}
                        rol="tab"
                        seleccionado={activa}
                        etiqueta={cuenta > 0 ? `${p.texto} (${cuenta})` : p.texto}
                        testID={`pestana-${p.id}`}
                        alPresionar={() => navegar(p.vista)}
                        estilo={estilos.pestana}
                    >
                        {activa ? <View style={[estilos.marca, { backgroundColor: colores.primario }]} /> : null}
                        <View>
                            <Icono size={24} color={color} />
                            {cuenta > 0 ? (
                                <View style={[estilos.contador, { backgroundColor: colores.primario }]}>
                                    <Text style={[estilos.textoContador, { color: colores.sobrePrimario }]}>{cuenta > 99 ? "99+" : String(cuenta)}</Text>
                                </View>
                            ) : null}
                        </View>
                        <Text style={[estilos.texto, { color }]}>{p.texto}</Text>
                    </Tocable>
                );
            })}
        </View>
    );
}

const estilos = StyleSheet.create({
    barra: { flexDirection: "row", borderTopWidth: 1 },
    pestana: { flex: 1, minHeight: 56, alignItems: "center", justifyContent: "center", gap: 2, paddingVertical: 6 },
    marca: { position: "absolute", top: 0, left: 24, right: 24, height: 2, borderRadius: 999 },
    contador: { position: "absolute", top: -6, right: -12, minWidth: 18, height: 18, borderRadius: 9, paddingHorizontal: 4, alignItems: "center", justifyContent: "center" },
    textoContador: { fontSize: 11, fontWeight: "600" },
    texto: { fontSize: 12, fontWeight: "500" },
});
