// Isla de menú flotante (diseño «combinada», como la PWA): píldora centrada abajo con Hoy, Por
// recibir (con su contador), «+» en teal al centro, Agenda y Yo, zonas de 54 px (44 encogida). Al
// bajar se encoge y al subir vuelve. Encima, la píldora de estado (`avisoIsla` de `@iris/sync/campo`,
// la barra discreta de la fase 4): sin señal con algo por enviar, un rechazo, la sesión vencida o
// la empresa no válida. El «+» abre la hoja de acciones rápidas con lo que ya tiene API.
import { accionesDe, accionesRapidas, avisoIsla, contadoresPestanas, estadoIslaPasos, pestanaDe, PESTANAS, type PasoOrden, type Pestana, type PrimarioIsla, type Vista } from "@iris/sync/campo";
import {
    ArrowLeft,
    ArrowRight,
    Building2,
    CalendarClock,
    CalendarDays,
    Check,
    ChevronRight,
    CircleCheck,
    CloudUpload,
    Hand,
    House,
    KeyRound,
    PackageOpen,
    PenLine,
    Plus,
    Save,
    TriangleAlert,
    UserRound,
    WifiOff,
    type LucideIcon,
} from "lucide-react-native";
import { useEffect, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Boton, BotonRedondo, Dialogo, Texto, Tocable } from "../componentes";
import { useGuardarPaso, useRecienGuardado, useSinGuardar } from "../campo/borradores";
import { useCampo, useOrden } from "../campo/contexto";
import { useCargarAlAbrir, useInventario } from "../campo/inventario";
import { useLibres } from "../campo/libres";
import { useTema } from "../tema/tema";

const ICONOS: Record<Pestana, LucideIcon> = { hoy: House, recibir: PackageOpen, agenda: CalendarDays, yo: UserRound };

export function IslaMenu({ encogida, abajo }: { encogida: boolean; abajo: number }) {
    const { vista, navegar, estado } = useCampo();
    const { traslados, cargarTraslados } = useInventario();
    // El contador de «Por recibir» se lee al abrir la app (y al volver la señal).
    useCargarAlAbrir(cargarTraslados);
    const { campo, colores } = useTema();
    const [acciones, setAcciones] = useState(false);
    const actual = pestanaDe(vista);
    const cuentas = contadoresPestanas(estado, traslados.datos);
    const tamano = encogida ? 44 : 54;
    const boton = (p: (typeof PESTANAS)[number]) => {
        const activa = actual === p.id;
        const cuenta = cuentas[p.id];
        return (
            <BotonRedondo
                key={p.id}
                rol="tab"
                icono={ICONOS[p.id]}
                etiqueta={cuenta > 0 ? `${p.texto} (${cuenta})` : p.texto}
                seleccionado={activa}
                tamano={tamano}
                fondo={activa ? campo.tinta : "transparent"}
                tinta={activa ? campo.sobreTinta : colores.textoSuave}
                testID={`pestana-${p.id}`}
                alPresionar={() => navegar(p.vista)}
            >
                {cuenta > 0 ? (
                    <View style={[estilos.contador, { backgroundColor: colores.primario }]}>
                        <Text style={[estilos.textoContador, { color: colores.sobrePrimario }]}>{cuenta > 99 ? "99+" : String(cuenta)}</Text>
                    </View>
                ) : null}
            </BotonRedondo>
        );
    };
    return (
        <>
            <View pointerEvents="box-none" style={[estilos.contenedor, { bottom: abajo + (encogida ? 12 : 24) }]}>
                <View
                    accessibilityRole="tablist"
                    accessibilityLabel="Secciones de la app"
                    testID="navegacion-inferior"
                    style={[estilos.isla, { padding: encogida ? 4 : 6, gap: encogida ? 2 : 4, backgroundColor: campo.isla, borderColor: colores.borde, shadowColor: campo.sombra }]}
                >
                    {PESTANAS.slice(0, 2).map(boton)}
                    <BotonRedondo icono={Plus} etiqueta="Acciones rápidas" tamano={tamano} fondo={colores.primario} tinta={colores.sobrePrimario} testID="boton-mas" alPresionar={() => setAcciones(true)} />
                    {PESTANAS.slice(2).map(boton)}
                </View>
            </View>
            {/* Se monta al abrir: cada vez relee el contador de órdenes libres. */}
            {acciones ? <HojaAcciones alCerrar={() => setAcciones(false)} /> : null}
        </>
    );
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
    const tamano = encogida ? 44 : 54;
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

/** Píldora de estado encima de la isla (o al pie, en los pasos sin isla). */
export function AvisoIsla({ abajo, alEntrar, alElegirEmpresa }: { abajo: number; alEntrar: () => void; alElegirEmpresa?: (() => void) | undefined }) {
    const { estado, navegar } = useCampo();
    const { campo, colores } = useTema();
    const a = avisoIsla(estado);
    if (!a) {
        return null;
    }
    const Icono = a.rechazo ? TriangleAlert : estado.conexion === "sin_sesion" ? KeyRound : estado.conexion === "sin_empresa" ? Building2 : estado.conexion === "sin_conexion" ? WifiOff : CloudUpload;
    const fondo = a.tono === "error" ? campo.errorFondo : a.tono === "warning" ? campo.avisoFondo : a.tono === "info" ? campo.infoFondo : campo.panel;
    const borde = a.tono === "error" ? campo.errorBorde : a.tono === "warning" ? campo.avisoBorde : a.tono === "info" ? campo.infoBorde : colores.borde;
    const tinta = colores.tono[a.tono];
    const tocar = () => {
        if (a.accion === "pendientes") {
            navegar({ tipo: "pendientes" });
        } else if (a.accion === "entrar") {
            alEntrar();
        } else if (a.accion === "empresa") {
            alElegirEmpresa?.();
        }
    };
    return (
        <View pointerEvents="box-none" style={[estilos.contenedor, { bottom: abajo }]}>
            <Tocable
                etiqueta={a.etiqueta}
                alPresionar={tocar}
                testID="barra-conexion"
                estilo={[estilos.pildora, { backgroundColor: fondo, borderColor: borde, shadowColor: campo.sombra }]}
            >
                <Icono size={16} color={tinta} />
                <Text testID="estado-conexion" style={[estilos.textoPildora, { color: tinta }]}>
                    {a.estado}
                </Text>
                {a.detalle ? (
                    <>
                        <Text style={[estilos.textoPildora, { color: tinta }]}>·</Text>
                        <Text testID="pendientes-por-enviar" style={[estilos.textoPildora, estilos.encoger, { color: tinta }]}>
                            {a.detalle}
                        </Text>
                    </>
                ) : null}
            </Tocable>
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
