// Isla de menú flotante (estilo elegido por el PM, nov 2026, como la PWA): píldora centrada abajo
// con Hoy, Chat (punto teal de no leídos, lo único teal de la isla), «+» al centro (icono de 26 px
// en color de texto, sin relleno ni borde), Agenda y Yo. La activa es una píldora en «tinta» con
// icono y nombre (se despliega al cambiar); las demás, solo el icono en gris. Zonas de 52 px (48
// encogida). Al bajar se encoge y al subir vuelve. «Por recibir» es el mosaico de Hoy;
// el estado (sincronización y GPS) va en el encabezado. Sobre la isla no flota nada. El «+» abre la
// hoja de acciones rápidas: «Agendar tarea propia» y «Tomar una orden libre».
import { accionesDe, accionesRapidas, contadoresPestanas, estadoIslaPasos, pestanaDe, pestanasIsla, type PasoOrden, type Pestana, type PrimarioIsla, type Vista } from "@iris/sync/campo";
import {
    ArrowLeft,
    ArrowRight,
    CalendarClock,
    CalendarDays,
    Check,
    ChevronRight,
    CircleCheck,
    Hand,
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
import { Boton, BotonRedondo, Dialogo, Texto, Tocable } from "../componentes";
import { useGuardarPaso, useRecienGuardado, useSinGuardar } from "../campo/borradores";
import { useChatOpcional } from "../campo/chat";
import { useCampo, useOrden } from "../campo/contexto";
import { useLibres } from "../campo/libres";
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
            {/* Se monta al abrir: cada vez relee el contador de órdenes libres. */}
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
 * Hoja del «+» (diseño aprobado por el PM, nov 2026): «Agendar tarea propia» (también sin señal) y
 * «Tomar una orden libre» con su contador (requiere señal; oculta si la empresa no lo permite o sin
 * permiso). «Agregar formulario» vive en el menú «⋯» de la orden.
 */
function HojaAcciones({ alCerrar }: { alCerrar: () => void }) {
    const { estado, sesion, navegar } = useCampo();
    const { libres, cargarLibres } = useLibres();
    const r = accionesRapidas(sesion.permisos, estado.tomarLibres);
    const enLinea = estado.conexion !== "sin_conexion";
    useEffect(() => {
        if (r.libres && enLinea) {
            void cargarLibres();
        }
    }, [r.libres, enLinea, cargarLibres]);
    const ir = (v: Vista) => {
        alCerrar();
        navegar(v);
    };
    const total = libres.estado === "lista" ? (libres.datos?.total ?? null) : null;
    return (
        <Dialogo abierto alCerrar={alCerrar} titulo="¿Qué quieres hacer?">
            {r.tarea ? (
                <FilaAccion testID="accion-tarea" icono={CalendarClock} titulo="Agendar tarea propia" detalle="Algo tuyo: recoger material, revisar una caja…" alPresionar={() => ir({ tipo: "nueva_tarea" })} />
            ) : null}
            {r.libres ? (
                <FilaAccion
                    testID="accion-libres"
                    icono={Hand}
                    titulo="Tomar una orden libre"
                    detalle={enLinea ? "Órdenes de tu zona sin técnico" : "Necesita señal"}
                    cuenta={total}
                    alPresionar={() => ir({ tipo: "libres" })}
                />
            ) : null}
        </Dialogo>
    );
}

function FilaAccion({ testID, icono: Icono, titulo, detalle, cuenta = null, alPresionar }: { testID: string; icono: LucideIcon; titulo: string; detalle: string; cuenta?: number | null; alPresionar: () => void }) {
    const { campo, colores } = useTema();
    return (
        <Tocable etiqueta={cuenta !== null ? `${titulo} (${cuenta})` : titulo} alPresionar={alPresionar} testID={testID} estilo={estilos.filaAccion}>
            <View style={[estilos.iconoAccion, { backgroundColor: campo.panel2 }]}>
                <Icono size={22} color={colores.texto} />
            </View>
            <View style={estilos.flex}>
                <Text style={[estilos.tituloAccion, { color: colores.texto }]}>{titulo}</Text>
                <Texto suave>{detalle}</Texto>
            </View>
            {cuenta !== null && cuenta > 0 ? (
                <View style={[estilos.cuentaAccion, { backgroundColor: campo.tinta }]}>
                    <Text style={[estilos.textoContador, { color: campo.sobreTinta, fontSize: 13 }]}>{cuenta > 99 ? "99+" : String(cuenta)}</Text>
                </View>
            ) : null}
            <ChevronRight size={20} color={colores.textoSuave} />
        </Tocable>
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
    filaAccion: { flexDirection: "row", alignItems: "center", gap: 14, paddingVertical: 14, paddingHorizontal: 4, borderRadius: 18 },
    iconoAccion: { width: 48, height: 48, borderRadius: 16, alignItems: "center", justifyContent: "center" },
    tituloAccion: { fontSize: 17, fontWeight: "700" },
    cuentaAccion: { minWidth: 30, height: 30, borderRadius: 15, alignItems: "center", justifyContent: "center", paddingHorizontal: 6 },
    primario: { borderRadius: 999, minWidth: 150, paddingHorizontal: 20 },
    cuenta: { fontSize: 13, fontWeight: "700" },
    guardado: { flexDirection: "row", alignItems: "center", gap: 4, minHeight: 44, paddingHorizontal: 6 },
    textoGuardado: { fontSize: 13, fontWeight: "600" },
});
