// Tarea propia del técnico (botón «+», diseño aprobado por el PM): la pantalla «Nueva tarea»
// (react-hook-form + zod de `@iris/sync/campo`, espejo de la API; también sin señal) y la tarjeta
// con borde punteado de Hoy y Agenda, con «Hecha» y «Anular». Como la PWA (`_componentes/tareas.tsx`).
import { zodResolver } from "@hookform/resolvers/zod";
import type { TareaVista } from "@iris/sync";
import {
    DURACIONES_TAREA,
    datosTarea,
    detalleTarea,
    esquemaTarea,
    horaTarea,
    hoyEnGuatemala,
    nombreCliente,
    sumarDias,
    textoCuando,
    valoresInicialesTarea,
    type ValoresTarea,
} from "@iris/sync/campo";
import { ArrowLeft, Ban, Check, SquareCheck } from "lucide-react-native";
import { useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { StyleSheet, Text, View } from "react-native";
import {
    Boton,
    BotonRedondo,
    Campo,
    Dialogo,
    Opciones,
    Texto,
    Tocable,
} from "../componentes";
import { useCampo } from "../campo/contexto";
import { useTema } from "../tema/tema";

/** Botón primario «tinta» del diseño: fondo del texto, texto invertido, píldora de 52 px. */
export function BotonTinta({
    children,
    alPresionar,
    etiqueta,
    testID,
    deshabilitado,
}: {
    children: string;
    alPresionar: () => void;
    etiqueta?: string;
    testID?: string;
    deshabilitado?: boolean;
}) {
    const { campo } = useTema();
    return (
        <Tocable
            etiqueta={etiqueta ?? children}
            alPresionar={alPresionar}
            {...(deshabilitado === undefined ? {} : { deshabilitado })}
            {...(testID ? { testID } : {})}
            estilo={[
                estilos.tinta,
                {
                    backgroundColor: campo.tinta,
                    opacity: deshabilitado ? 0.5 : 1,
                },
            ]}
        >
            <Text style={[estilos.textoTinta, { color: campo.sobreTinta }]}>
                {children}
            </Text>
        </Tocable>
    );
}

/** Encabezado de una pantalla propia: «Atrás» redondo y el título centrado. */
export function EncabezadoPantalla({
    titulo,
    alAtras,
}: {
    titulo: string;
    alAtras: () => void;
}) {
    const { campo, colores } = useTema();
    return (
        <View style={estilos.encabezado}>
            <BotonRedondo
                icono={ArrowLeft}
                etiqueta="Atrás"
                alPresionar={alAtras}
                fondo={campo.panel}
                tinta={colores.texto}
                borde={colores.borde}
                testID="atras"
            />
            <Text
                accessibilityRole="header"
                style={[estilos.titulo, { color: colores.texto }]}
            >
                {titulo}
            </Text>
            <View style={estilos.hueco} />
        </View>
    );
}

export function NuevaTarea() {
    const { motor, estado, ejecutar, navegar } = useCampo();
    const { campo, colores } = useTema();
    const [inicial] = useState(() => valoresInicialesTarea());
    const form = useForm<ValoresTarea>({
        resolver: zodResolver(esquemaTarea),
        defaultValues: inicial,
    });
    const [saliendo, setSaliendo] = useState(false);
    const hoy = hoyEnGuatemala();
    const dias = [
        { valor: hoy, etiqueta: "Hoy" },
        { valor: sumarDias(hoy, 1), etiqueta: "Mañana" },
        { valor: sumarDias(hoy, 2), etiqueta: "Pasado mañana" },
    ];
    const ordenes = useMemo(
        () =>
            estado.ordenes.filter(
                (o) =>
                    !o.reasignada &&
                    !["cerrada", "fallida", "cancelada"].includes(o.estado),
            ),
        [estado.ordenes],
    );
    const volver = () => navegar({ tipo: "lista" });
    const atras = () => (form.formState.isDirty ? setSaliendo(true) : volver());
    const agendar = form.handleSubmit(async (v) => {
        if (
            await ejecutar(
                () => motor.registrarTarea("tarea.crear", datosTarea(v)),
                `Tarea agendada: ${textoCuando(v.dia, v.hora)}`,
            )
        ) {
            volver();
        }
    });
    const errores = form.formState.errors;
    return (
        <View testID="nueva-tarea" style={estilos.pila}>
            <EncabezadoPantalla titulo="Nueva tarea" alAtras={atras} />
            <View
                style={[
                    estilos.panel,
                    {
                        backgroundColor: campo.panel,
                        borderColor: colores.borde,
                    },
                ]}
            >
                <Controller
                    control={form.control}
                    name="que"
                    render={({ field }) => (
                        <Campo
                            etiqueta="Qué vas a hacer"
                            valor={field.value}
                            alCambiar={field.onChange}
                            onBlur={field.onBlur}
                            placeholder="Recoger material en bodega central"
                            maxLength={200}
                            error={errores.que?.message}
                            testID="tarea-que"
                        />
                    )}
                />
                <Controller
                    control={form.control}
                    name="dia"
                    render={({ field }) => (
                        <Opciones
                            etiqueta="Cuándo"
                            opciones={dias}
                            valor={field.value}
                            alCambiar={field.onChange}
                            error={errores.dia?.message}
                        />
                    )}
                />
                <Controller
                    control={form.control}
                    name="hora"
                    render={({ field }) => (
                        <Campo
                            etiqueta="Hora"
                            valor={field.value}
                            alCambiar={field.onChange}
                            onBlur={field.onBlur}
                            placeholder="14:00"
                            keyboardType="numbers-and-punctuation"
                            maxLength={5}
                            error={errores.hora?.message}
                            testID="tarea-hora"
                        />
                    )}
                />
                <Controller
                    control={form.control}
                    name="duracionMin"
                    render={({ field }) => (
                        <View
                            style={estilos.chips}
                            accessibilityRole="radiogroup"
                            accessibilityLabel="Duración"
                        >
                            <Text
                                style={[
                                    estilos.etiqueta,
                                    { color: colores.texto },
                                ]}
                            >
                                Duración
                            </Text>
                            <View style={estilos.filaChips}>
                                {DURACIONES_TAREA.map((d) => {
                                    const elegida = field.value === d.minutos;
                                    return (
                                        <Tocable
                                            key={d.minutos}
                                            etiqueta={d.texto}
                                            seleccionado={elegida}
                                            alPresionar={() =>
                                                field.onChange(d.minutos)
                                            }
                                            testID={`duracion-${d.minutos}`}
                                            estilo={[
                                                estilos.chip,
                                                {
                                                    backgroundColor: elegida
                                                        ? campo.tinta
                                                        : campo.panel2,
                                                },
                                            ]}
                                        >
                                            <Text
                                                style={[
                                                    estilos.textoChip,
                                                    {
                                                        color: elegida
                                                            ? campo.sobreTinta
                                                            : colores.texto,
                                                    },
                                                ]}
                                            >
                                                {d.texto}
                                            </Text>
                                        </Tocable>
                                    );
                                })}
                            </View>
                        </View>
                    )}
                />
                <Controller
                    control={form.control}
                    name="ligada"
                    render={({ field }) => (
                        <Opciones
                            etiqueta="Ligar a una orden (opcional)"
                            columna
                            valor={field.value}
                            alCambiar={field.onChange}
                            opciones={[
                                { valor: "", etiqueta: "Ninguna" },
                                ...ordenes.map((o) => ({
                                    valor: `orden:${o.id}`,
                                    etiqueta: `${o.numero} · ${nombreCliente(o)}`,
                                })),
                            ]}
                        />
                    )}
                />
            </View>
            <Texto suave>
                Aparece en tu agenda de Hoy. Tu supervisor la ve. Funciona sin
                señal: se envía al volver.
            </Texto>
            <BotonTinta
                alPresionar={() => void agendar()}
                deshabilitado={form.formState.isSubmitting}
                testID="agendar"
            >
                Agendar
            </BotonTinta>
            <Dialogo
                abierto={saliendo}
                alCerrar={() => setSaliendo(false)}
                titulo="¿Descartar la tarea?"
                descripcion="Lo que escribiste no se guardó."
                testID="descartar-tarea"
            >
                <Boton variante="peligro" alPresionar={volver}>
                    Descartar
                </Boton>
                <Boton
                    variante="secundario"
                    alPresionar={() => setSaliendo(false)}
                >
                    Seguir editando
                </Boton>
            </Dialogo>
        </View>
    );
}

/** Tarea propia en Hoy y Agenda: borde punteado, la hora a la izquierda, «Hecha» y «Anular». */
export function TarjetaTarea({ tarea }: { tarea: TareaVista }) {
    const { motor, ejecutar } = useCampo();
    const { campo, colores } = useTema();
    const [anulando, setAnulando] = useState(false);
    const hecha = tarea.estado === "completada";
    return (
        <View
            testID={`tarea-${tarea.id}`}
            accessibilityLabel={`Tarea propia: ${tarea.que}`}
            style={[
                estilos.tarjeta,
                {
                    backgroundColor: campo.panel,
                    borderColor: colores.textoTenue,
                },
            ]}
        >
            <View style={[estilos.hora, { backgroundColor: campo.panel2 }]}>
                <Text style={[estilos.textoHora, { color: colores.texto }]}>
                    {horaTarea(tarea)}
                </Text>
            </View>
            <View style={estilos.flex}>
                <Text
                    style={[
                        estilos.que,
                        { color: hecha ? colores.textoSuave : colores.texto },
                        hecha ? estilos.tachada : null,
                    ]}
                >
                    {tarea.que}
                </Text>
                <View style={estilos.detalle}>
                    <SquareCheck size={15} color={colores.textoSuave} />
                    <Text style={{ color: colores.textoSuave }}>
                        {detalleTarea(tarea)}
                    </Text>
                </View>
                {hecha ? null : (
                    <View style={estilos.acciones}>
                        <Boton
                            variante="secundario"
                            icono={Check}
                            alPresionar={() =>
                                void ejecutar(
                                    () =>
                                        motor.registrarTarea(
                                            "tarea.completar",
                                            { tareaId: tarea.id },
                                        ),
                                    "Tarea hecha",
                                )
                            }
                            testID="tarea-hecha"
                        >
                            Hecha
                        </Boton>
                        <Boton
                            variante="fantasma"
                            icono={Ban}
                            alPresionar={() => setAnulando(true)}
                            testID="tarea-anular"
                        >
                            Anular
                        </Boton>
                    </View>
                )}
            </View>
            <Dialogo
                abierto={anulando}
                alCerrar={() => setAnulando(false)}
                titulo="¿Anular la tarea?"
                descripcion={`«${tarea.que}» sale de tu agenda. Queda en el historial.`}
                testID="anular-tarea"
            >
                <Boton
                    variante="peligro"
                    alPresionar={() => {
                        setAnulando(false);
                        void ejecutar(
                            () =>
                                motor.registrarTarea("tarea.anular", {
                                    tareaId: tarea.id,
                                }),
                            "Tarea anulada",
                        );
                    }}
                >
                    Anular
                </Boton>
            </Dialogo>
        </View>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1, gap: 4 },
    pila: { gap: 16 },
    encabezado: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "space-between",
    },
    titulo: { fontSize: 20, fontWeight: "700" },
    hueco: { width: 48 },
    panel: { gap: 16, padding: 16, borderRadius: 22, borderWidth: 1 },
    etiqueta: { fontSize: 14, fontWeight: "600" },
    chips: { gap: 8 },
    filaChips: { flexDirection: "row", gap: 8 },
    chip: {
        flex: 1,
        minHeight: 44,
        borderRadius: 999,
        alignItems: "center",
        justifyContent: "center",
    },
    textoChip: { fontSize: 15, fontWeight: "600" },
    tinta: {
        minHeight: 52,
        borderRadius: 999,
        alignItems: "center",
        justifyContent: "center",
        paddingHorizontal: 20,
    },
    textoTinta: { fontSize: 16, fontWeight: "700" },
    tarjeta: {
        flexDirection: "row",
        alignItems: "center",
        gap: 14,
        padding: 14,
        borderRadius: 22,
        borderWidth: 1.5,
        borderStyle: "dashed",
    },
    hora: {
        width: 64,
        height: 56,
        borderRadius: 16,
        alignItems: "center",
        justifyContent: "center",
    },
    textoHora: {
        fontSize: 17,
        fontWeight: "700",
        fontVariant: ["tabular-nums"],
    },
    que: { fontSize: 16, fontWeight: "700" },
    tachada: { textDecorationLine: "line-through" },
    detalle: { flexDirection: "row", alignItems: "center", gap: 6 },
    acciones: { flexDirection: "row", gap: 8, marginTop: 4 },
});
