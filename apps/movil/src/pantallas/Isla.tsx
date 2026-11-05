// Isla de menú flotante (estilo elegido por el PM, nov 2026, como la PWA): píldora centrada abajo
// con Hoy, Chat (punto teal de no leídos, lo único teal de la isla), «+» al centro (icono de 26 px
// en color de texto, sin relleno ni borde), Agenda y Yo. La activa es una píldora en «tinta» con
// icono y nombre (se despliega al cambiar); las demás, solo el icono en gris. Zonas de 52 px (48
// encogida). Al bajar se encoge y al subir vuelve. «Por recibir» es el mosaico de Hoy;
// el estado (sincronización y GPS) va en el encabezado. Sobre la isla no flota nada. El «+» abre la
// hoja de acciones rápidas con lo que ya tiene API.
import type { PlantillaFormulario } from "@iris/sync";
import { accionesDe, accionesRapidas, contadoresPestanas, estadoIslaPasos, nombreCliente, pestanaDe, pestanasIsla, type PasoOrden, type Pestana, type PrimarioIsla, type Vista } from "@iris/sync/campo";
import {
    ArrowLeft,
    ArrowRight,
    CalendarClock,
    CalendarDays,
    Check,
    CircleCheck,
    FilePlus2,
    House,
    MessageCircle,
    PenLine,
    User,
    Plus,
    Save,
    type LucideIcon,
} from "lucide-react-native";
import { useEffect, useState } from "react";
import { AccessibilityInfo, LayoutAnimation, StyleSheet, Text, View } from "react-native";
import { Boton, BotonRedondo, Dialogo, Insignia, Opciones, Texto, Tocable } from "../componentes";
import { useGuardarPaso, useRecienGuardado, useSinGuardar } from "../campo/borradores";
import { useChatOpcional } from "../campo/chat";
import { useCampo, useOrden } from "../campo/contexto";
import { useTema } from "../tema/tema";

const ICONOS: Record<Pestana, LucideIcon> = { hoy: House, chat: MessageCircle, agenda: CalendarDays, yo: User };

export function IslaMenu({ encogida, abajo }: { encogida: boolean; abajo: number }) {
    const { vista, navegar, sesion } = useCampo();
    const chat = useChatOpcional();
    const { campo, colores } = useTema();
    const [acciones, setAcciones] = useState(false);
    const actual = pestanaDe(vista);
    const cuentas = contadoresPestanas(chat?.estado.noLeidos ?? 0);
    const pestanas = pestanasIsla(chat !== null);
    const tamano = encogida ? 48 : 52;
    const movimientoReducido = useMovimientoReducido();
    const boton = (p: (typeof pestanas)[number]) => {
        const activa = actual === p.id;
        const cuenta = cuentas[p.id];
        const Icono = ICONOS[p.id];
        const ir = () => {
            // El nombre se despliega en la nueva activa y se recoge en la anterior.
            if (!movimientoReducido && !activa) {
                LayoutAnimation.configureNext(LayoutAnimation.create(240, LayoutAnimation.Types.easeInEaseOut, LayoutAnimation.Properties.opacity));
            }
            navegar(p.vista);
        };
        return (
            <Tocable
                key={p.id}
                rol="tab"
                etiqueta={p.id === "yo" ? `Yo · ${sesion.nombre}` : cuenta > 0 ? `${p.texto} (${cuenta} sin leer)` : p.texto}
                seleccionado={activa}
                testID={`pestana-${p.id}`}
                alPresionar={ir}
                estilo={[
                    estilos.pestana,
                    { height: tamano, minHeight: tamano, minWidth: tamano, borderRadius: tamano / 2, paddingHorizontal: activa ? 16 : (tamano - 24) / 2 },
                    { backgroundColor: activa ? campo.tinta : "transparent" },
                ]}
            >
                <Icono size={24} color={activa ? campo.sobreTinta : colores.textoSuave} strokeWidth={activa ? 2.3 : 2} />
                {activa ? (
                    <Text testID={`nombre-pestana-${p.id}`} numberOfLines={1} style={[estilos.nombrePestana, { color: campo.sobreTinta }]}>
                        {p.texto}
                    </Text>
                ) : null}
                {/* El chat: un punto teal cuando hay mensajes sin leer (no con el chat abierto). */}
                {cuenta > 0 && !activa ? <View testID="punto-no-leidos" style={[estilos.punto, { backgroundColor: colores.primario, borderColor: campo.isla }]} /> : null}
            </Tocable>
        );
    };
    return (
        <>
            <View pointerEvents="box-none" style={[estilos.contenedor, { bottom: abajo + (encogida ? 12 : 24) }]}>
                <View
                    accessibilityRole="tablist"
                    accessibilityLabel="Secciones de la app"
                    testID="navegacion-inferior"
                    style={[estilos.isla, { padding: encogida ? 4 : 6, gap: 2, backgroundColor: campo.isla, borderColor: colores.borde, shadowColor: campo.sombra }]}
                >
                    {pestanas.filter((p) => p.id === "hoy" || p.id === "chat").map(boton)}
                    {/* «+» sin relleno ni borde: icono de 26 px en color de texto. */}
                    <BotonRedondo icono={Plus} etiqueta="Acciones rápidas" tamano={tamano} tamanoIcono={26} fondo="transparent" tinta={colores.texto} testID="boton-mas" alPresionar={() => setAcciones(true)} />
                    {pestanas.filter((p) => p.id === "agenda" || p.id === "yo").map(boton)}
                </View>
            </View>
            {/* Se monta al abrir: cada vez empieza sin elegir (con una sola orden, ya elegida). */}
            {acciones ? <HojaAcciones alCerrar={() => setAcciones(false)} /> : null}
        </>
    );
}

/** Con «Reducir movimiento» del sistema, la isla cambia sin animar. */
function useMovimientoReducido(): boolean {
    const [reducido, setReducido] = useState(false);
    useEffect(() => {
        let vivo = true;
        void AccessibilityInfo.isReduceMotionEnabled?.()
            .then((r) => {
                if (vivo) {
                    setReducido(r);
                }
            })
            .catch(() => undefined);
        const sub = AccessibilityInfo.addEventListener?.("reduceMotionChanged", setReducido);
        return () => {
            vivo = false;
            sub?.remove();
        };
    }, []);
    return reducido;
}

const ICONO_PRIMARIO: Record<PrimarioIsla["icono"], LucideIcon> = { siguiente: ArrowRight, firma: PenLine, terminar: CircleCheck, guardar: Save };

/**
 * Isla de los pasos de la orden: la misma píldora que la isla de menú, con «Anterior» redondo y un
 * primario que avanza (`estadoIslaPasos` de `@iris/sync/campo`). Sin cambios por guardar no hay
 * «Guardar»: el primario es «Siguiente» («Ir a firma» antes de la firma). Con algo escrito sin guardar
 * es «Guardar y seguir» con el contador: guarda y avanza, o se queda y dice qué campo falló. Al
 * guardar, «Guardado» con un check un momento en la isla (sin bloquear). Sin «Guardar» aparte: a
 * 360 px no cabe sin apretar los botones, y cada campo ya se guarda al salir de él. Con la orden en
 * pausa (solo lectura) nunca guarda: solo navega.
 */
export function IslaPasos({ id, paso, encogida, abajo }: { id: number; paso: PasoOrden; encogida: boolean; abajo: number }) {
    const { navegar } = useCampo();
    const orden = useOrden(id);
    const { campo, colores } = useTema();
    const { guardar, guardando } = useGuardarPaso();
    const guardado = useRecienGuardado();
    const sinGuardar = useSinGuardar();
    if (!orden) {
        return null;
    }
    const enPausa = accionesDe(orden).enPausa;
    const isla = estadoIslaPasos({ paso, pendientes: sinGuardar, enPausa, recienGuardado: guardado !== null });
    const vistaDe = (destino: PasoOrden | null): Vista => (destino ? { tipo: "orden", id, paso: destino } : { tipo: "orden", id });
    const tamano = encogida ? 48 : 54;
    // Con la orden en pausa no se guarda nada: solo se navega.
    const ir = async (destino: PasoOrden | null) => {
        if (enPausa || (await guardar())) {
            navegar(vistaDe(destino));
        }
    };
    const { primario } = isla;
    return (
        <View pointerEvents="box-none" style={[estilos.contenedor, { bottom: abajo + (encogida ? 12 : 24) }]}>
            <View
                accessibilityLabel="Pasos"
                testID="barra-pasos"
                style={[estilos.isla, { padding: encogida ? 4 : 6, gap: encogida ? 4 : 6, backgroundColor: campo.isla, borderColor: colores.borde, shadowColor: campo.sombra }]}
            >
                <BotonRedondo
                    icono={ArrowLeft}
                    etiqueta={isla.anterior.texto}
                    tamano={tamano}
                    fondo={campo.panel2}
                    tinta={colores.texto}
                    testID="paso-anterior"
                    alPresionar={() => void ir(isla.anterior.destino)}
                />
                {isla.guardado && guardado ? (
                    <View testID="guardado-paso" accessible accessibilityRole="text" accessibilityLabel={guardado} accessibilityLiveRegion="polite" style={estilos.guardado}>
                        <Check size={16} color={colores.tono.success} strokeWidth={2.6} />
                        <Text style={[estilos.textoGuardado, { color: colores.tono.success }]}>Guardado</Text>
                    </View>
                ) : null}
                <Boton
                    icono={ICONO_PRIMARIO[primario.icono]}
                    cargando={guardando}
                    etiqueta={primario.etiqueta}
                    testID={primario.accion === "guardar_y_seguir" ? "guardar-y-seguir" : "paso-siguiente"}
                    alPresionar={() => void ir(primario.destino)}
                    estilo={[estilos.primario, { minHeight: tamano }]}
                >
                    {primario.texto}
                    {primario.cuenta !== null ? <Text style={estilos.cuenta}>{` · ${primario.cuenta}`}</Text> : null}
                </Boton>
            </View>
        </View>
    );
}

/**
 * Hoja del «+» con lo que ya tiene API: «Agregar formulario» a una orden con el trabajo empezado
 * (`orden.agregar_formulario`, también sin señal; solo con `ordenes.registrar`) y «Agendar tarea
 * propia», deshabilitada hasta que exista en la API.
 */
function HojaAcciones({ alCerrar }: { alCerrar: () => void }) {
    const { estado, sesion, motor, ejecutar } = useCampo();
    const { campo, colores } = useTema();
    const r = accionesRapidas(estado.ordenes, estado.plantillas, sesion.permisos);
    const ordenes = r.formulario?.ordenes ?? [];
    const [elegidaId, setOrdenId] = useState<string | null>(null);
    // Con una sola orden donde se puede, va elegida (también si llega después de abrir la hoja).
    const ordenId = elegidaId ?? (ordenes.length === 1 ? String(ordenes[0]?.orden.id) : null);
    const [plantillaId, setPlantillaId] = useState<string | null>(null);
    const [ocupado, setOcupado] = useState(false);
    const elegida = ordenes.find((o) => String(o.orden.id) === ordenId) ?? null;
    const plantilla: PlantillaFormulario | null = elegida?.plantillas.find((p) => String(p.id) === plantillaId) ?? null;

    const agregar = async () => {
        if (!elegida || !plantilla) {
            return;
        }
        setOcupado(true);
        const orden = elegida.orden;
        const ok = await ejecutar(
            () => motor.registrar("orden.agregar_formulario", orden.id, { plantillaId: plantilla.id, versionId: plantilla.versionId }),
            `«${plantilla.nombre}» agregado a ${orden.numero}`,
            {
                // El formulario lleva el id de la operación: quitarlo es el inverso exacto.
                deshacer: (res) => () => void ejecutar(() => motor.registrar("orden.quitar_formulario", orden.id, { formularioId: res.operacion.id }), `«${plantilla.nombre}» quitado`),
            },
        );
        setOcupado(false);
        if (ok) {
            alCerrar();
        }
    };

    return (
        <Dialogo abierto alCerrar={alCerrar} titulo="Acciones rápidas" cambios={plantillaId !== null && !ocupado}>
            {r.formulario ? (
                <View testID="accion-formulario" style={[estilos.accion, { backgroundColor: campo.panel2 }]}>
                    <View style={estilos.filaAccion}>
                        <View style={[estilos.iconoAccion, { backgroundColor: colores.primario }]}>
                            <FilePlus2 size={20} color={colores.sobrePrimario} />
                        </View>
                        <View style={estilos.flex}>
                            <Text accessibilityRole="header" style={[estilos.tituloAccion, { color: colores.texto }]}>
                                Agregar formulario
                            </Text>
                            <Texto suave>A una orden con el trabajo empezado. Se guarda en el teléfono.</Texto>
                        </View>
                    </View>
                    {ordenes.length === 0 ? (
                        <Texto suave>Ninguna orden en camino o en sitio: empieza una (o reanuda la pausada) para agregarle un formulario.</Texto>
                    ) : (
                        <>
                            {ordenes.length > 1 ? (
                                <Opciones
                                    etiqueta="Orden"
                                    columna
                                    valor={ordenId}
                                    alCambiar={(v) => {
                                        setOrdenId(v);
                                        setPlantillaId(null);
                                    }}
                                    opciones={ordenes.map(({ orden }) => ({ valor: String(orden.id), etiqueta: `${orden.numero} · ${nombreCliente(orden)}` }))}
                                />
                            ) : elegida ? (
                                <Texto>
                                    {elegida.orden.numero} · {nombreCliente(elegida.orden)}
                                </Texto>
                            ) : null}
                            {elegida ? (
                                elegida.plantillas.length === 0 ? (
                                    <Texto suave>Esta orden ya tiene todos los formularios del catálogo.</Texto>
                                ) : (
                                    <Opciones
                                        etiqueta="Plantilla"
                                        columna
                                        valor={plantillaId}
                                        alCambiar={setPlantillaId}
                                        opciones={elegida.plantillas.map((p) => ({ valor: String(p.id), etiqueta: p.nombre, detalle: `Versión ${p.numeroVersion}` }))}
                                    />
                                )
                            ) : null}
                            <Boton deshabilitado={!plantilla} cargando={ocupado} alPresionar={() => void agregar()} estilo={estilos.redondeado}>
                                {plantilla ? `Agregar «${plantilla.nombre}»` : "Elige una plantilla"}
                            </Boton>
                        </>
                    )}
                </View>
            ) : null}
            <View testID="accion-tarea" accessibilityState={{ disabled: true }} style={estilos.accion}>
                <View style={estilos.filaAccion}>
                    <View style={[estilos.iconoAccion, { backgroundColor: campo.panel2 }]}>
                        <CalendarClock size={20} color={colores.textoSuave} />
                    </View>
                    <View style={estilos.flex}>
                        <Text style={[estilos.tituloAccion, { color: colores.textoSuave }]}>Agendar tarea propia</Text>
                        <Texto suave>Aparecerá en tu agenda y la verá tu supervisor.</Texto>
                    </View>
                </View>
                <Insignia tono="neutral" icono={CalendarClock} texto={r.tarea.motivo} />
            </View>
        </Dialogo>
    );
}

const estilos = StyleSheet.create({
    punto: { position: "absolute", top: 10, right: 10, width: 10, height: 10, borderRadius: 5, borderWidth: 2 },
    pestana: { flexDirection: "row", alignItems: "center", justifyContent: "center" },
    nombrePestana: { marginLeft: 6, fontSize: 16, fontWeight: "600" },
    flex: { flex: 1 },
    contenedor: { position: "absolute", left: 0, right: 0, alignItems: "center" },
    isla: {
        flexDirection: "row",
        alignItems: "center",
        borderRadius: 999,
        borderWidth: 1,
        shadowOpacity: 0.16,
        shadowRadius: 18,
        shadowOffset: { width: 0, height: 8 },
        elevation: 8,
    },
    contador: { position: "absolute", top: 8, right: 8, minWidth: 16, height: 16, borderRadius: 8, paddingHorizontal: 3, alignItems: "center", justifyContent: "center" },
    textoContador: { fontSize: 10, fontWeight: "700" },
    pildora: {
        flexDirection: "row",
        alignItems: "center",
        gap: 8,
        maxWidth: 360,
        paddingHorizontal: 14,
        borderRadius: 999,
        borderWidth: 1,
        shadowOpacity: 0.08,
        shadowRadius: 14,
        shadowOffset: { width: 0, height: 6 },
        elevation: 4,
    },
    textoPildora: { fontSize: 13, fontWeight: "600" },
    encoger: { flexShrink: 1 },
    accion: { gap: 12, padding: 12, borderRadius: 22 },
    filaAccion: { flexDirection: "row", alignItems: "center", gap: 12 },
    iconoAccion: { width: 44, height: 44, borderRadius: 22, alignItems: "center", justifyContent: "center" },
    tituloAccion: { fontSize: 15, fontWeight: "600" },
    redondeado: { borderRadius: 999 },
    primario: { borderRadius: 999, minWidth: 150, paddingHorizontal: 20 },
    cuenta: { fontSize: 13, fontWeight: "700" },
    guardado: { flexDirection: "row", alignItems: "center", gap: 4, minHeight: 44, paddingHorizontal: 6 },
    textoGuardado: { fontSize: 13, fontWeight: "600" },
});
