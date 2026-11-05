// Isla de menú flotante (opción elegida por el PM, nov 2026, como la PWA): píldora centrada abajo
// con Hoy, Chat (punto de no leídos), «+» en teal al centro, Agenda y Yo (icono de usuario), zonas
// de 54 px (44 encogida). Al bajar se encoge y al subir vuelve. «Por recibir» es el mosaico de Hoy;
// el estado (sincronización y GPS) va en el encabezado. Sobre la isla no flota nada. El «+» abre la
// hoja de acciones rápidas con lo que ya tiene API.
import type { PlantillaFormulario } from "@iris/sync";
import { accionesDe, accionesRapidas, contadoresPestanas, nombreCliente, pestanaDe, pestanasIsla, type PasoOrden, type Pestana } from "@iris/sync/campo";
import {
    ArrowLeft,
    ArrowRight,
    CalendarClock,
    CalendarDays,
    FilePlus2,
    House,
    User,
    MessageCircle,
    Plus,
    Save,
    type LucideIcon,
} from "lucide-react-native";
import { useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Boton, BotonRedondo, Dialogo, Insignia, Opciones, Texto } from "../componentes";
import { useGuardarPaso, useSinGuardar } from "../campo/borradores";
import { useChatOpcional } from "../campo/chat";
import { useCampo, useOrden } from "../campo/contexto";
import { navegacionPaso } from "../campo/pasos";
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
    const tamano = encogida ? 44 : 54;
    const boton = (p: (typeof pestanas)[number]) => {
        const activa = actual === p.id;
        const cuenta = cuentas[p.id];
        return (
            <BotonRedondo
                key={p.id}
                rol="tab"
                icono={ICONOS[p.id]}
                etiqueta={p.id === "yo" ? `Yo · ${sesion.nombre}` : cuenta > 0 ? `${p.texto} (${cuenta} sin leer)` : p.texto}
                seleccionado={activa}
                tamano={tamano}
                fondo={activa ? campo.tinta : "transparent"}
                tinta={activa ? campo.sobreTinta : colores.textoSuave}
                testID={`pestana-${p.id}`}
                alPresionar={() => navegar(p.vista)}
            >
                {/* El chat: un punto (discreto) cuando hay mensajes sin leer. */}
                {cuenta > 0 ? <View testID="punto-no-leidos" style={[estilos.punto, { backgroundColor: colores.primario, borderColor: campo.isla }]} /> : null}
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
                    {pestanas.filter((p) => p.id === "hoy" || p.id === "chat").map(boton)}
                    <BotonRedondo icono={Plus} etiqueta="Acciones rápidas" tamano={tamano} fondo={colores.primario} tinta={colores.sobrePrimario} testID="boton-mas" alPresionar={() => setAcciones(true)} />
                    {pestanas.filter((p) => p.id === "agenda" || p.id === "yo").map(boton)}
                </View>
            </View>
            {/* Se monta al abrir: cada vez empieza sin elegir (con una sola orden, ya elegida). */}
            {acciones ? <HojaAcciones alCerrar={() => setAcciones(false)} /> : null}
        </>
    );
}

/**
 * Isla de los pasos de la orden: la misma píldora que la isla de menú, con «Anterior» y «Siguiente»
 * redondos y un solo botón primario «Guardar» al centro, que guarda lo escrito y todavía sin guardar
 * de todo el paso (con su contador) y avisa qué pasó. Lo demás ya se guardó al hacerlo. Pasar de paso
 * también guarda primero (si algo no valida, se queda y lo dice). Con la orden en pausa, «Guardar»
 * está deshabilitado: no hay trabajo que registrar.
 */
export function IslaPasos({ id, paso, encogida, abajo }: { id: number; paso: PasoOrden; encogida: boolean; abajo: number }) {
    const { navegar } = useCampo();
    const orden = useOrden(id);
    const { campo, colores } = useTema();
    const { guardar, guardando } = useGuardarPaso();
    const sinGuardar = useSinGuardar();
    if (!orden) {
        return null;
    }
    const nav = navegacionPaso(orden, paso);
    const enPausa = accionesDe(orden).enPausa;
    const tamano = encogida ? 44 : 54;
    const ir = async (vista: Parameters<typeof navegar>[0]) => {
        if (await guardar({ silencioso: true })) {
            navegar(vista);
        }
    };
    return (
        <View pointerEvents="box-none" style={[estilos.contenedor, { bottom: abajo + (encogida ? 12 : 24) }]}>
            <View
                accessibilityLabel="Pasos"
                testID="barra-pasos"
                style={[estilos.isla, { padding: encogida ? 4 : 6, gap: encogida ? 4 : 6, backgroundColor: campo.isla, borderColor: colores.borde, shadowColor: campo.sombra }]}
            >
                <BotonRedondo
                    icono={ArrowLeft}
                    etiqueta={nav.anterior.texto}
                    tamano={tamano}
                    fondo={campo.panel2}
                    tinta={colores.texto}
                    testID="paso-anterior"
                    alPresionar={() => void ir(nav.anterior.vista)}
                />
                <Boton
                    icono={Save}
                    cargando={guardando}
                    deshabilitado={enPausa}
                    etiqueta={sinGuardar > 0 ? `Guardar (${sinGuardar} sin guardar)` : "Guardar"}
                    testID="guardar-paso"
                    alPresionar={() => void guardar()}
                    estilo={[estilos.guardar, { minHeight: tamano }]}
                >
                    {sinGuardar > 0 ? `Guardar · ${sinGuardar}` : "Guardar"}
                </Boton>
                {nav.siguiente ? (
                    <BotonRedondo
                        icono={ArrowRight}
                        etiqueta={nav.siguiente.texto}
                        tamano={tamano}
                        fondo={campo.panel2}
                        tinta={colores.texto}
                        testID="paso-siguiente"
                        alPresionar={() => nav.siguiente && void ir(nav.siguiente.vista)}
                    />
                ) : null}
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
    guardar: { borderRadius: 999, minWidth: 150, paddingHorizontal: 20 },
});
