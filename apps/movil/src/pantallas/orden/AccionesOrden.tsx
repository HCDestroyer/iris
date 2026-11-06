// Acciones de la orden (decisión del PM, como la PWA): encabezado ← | número con «● Estado · Tipo ·
// ventana» | «⋯» (Agregar nota, Agregar formulario —deshabilitado con el motivo si no aplica—,
// Chat de la orden —abre o crea su conversación, con los no leídos—, Historial del cliente, Copiar
// dirección); la acción principal en
// «tinta» con [Pausar | No se pudo] debajo ([En camino | No se pudo] si está programada) y, en
// pausa, el aviso naranja «En pausa desde HH:MM · motivo» con solo «Reanudar». Las reglas vienen de
// `accionesPantallaOrden` y `encabezadoOrden` de `@iris/sync/campo`.
import { ErrorTransporte, type OrdenHistorial, type OrdenVista } from "@iris/sync";
import { textoContador } from "@iris/sync/chat";
import { accionesPantallaOrden, encabezadoOrden, motivoSinFormulario, ESTADOS_TEXTO, fechaHoraCorta, puedeReanudar, TIPOS_ORDEN, textoProblema } from "@iris/sync/campo";
import { ArrowLeft, CircleX, Clock, Copy, Ellipsis, FilePlus2, History, ListChecks, MessageCircle, Navigation, PauseCircle, PenLine, Play, type LucideIcon } from "lucide-react-native";
import { useCallback, useEffect, useState } from "react";
import { Share, StyleSheet, Text, View } from "react-native";
import { Aviso, Boton, BotonRedondo, Cargando, Dialogo, GrupoBotones, Texto, Tocable } from "../../componentes";
import { useCampo } from "../../campo/contexto";
import { useLeerHistorial } from "../../campo/historial";
import { botonTrabajo } from "../../campo/pasos";
import { useTema } from "../../tema/tema";
import { useAbrirChatOrden } from "../Chat";
import { HojaAgregarFormulario } from "./Checklist";
import { HojaFallar } from "./Cierre";
import { HojaPausar, useReanudar } from "./Pausa";

export function EncabezadoOrden({ orden, alVolver, alAgregarNota }: { orden: OrdenVista; alVolver: () => void; alAgregarNota: (() => void) | null }) {
    const { campo, colores } = useTema();
    const { sesion } = useCampo();
    const [menu, setMenu] = useState(false);
    const [historial, setHistorial] = useState(false);
    const [formulario, setFormulario] = useState(false);
    const sinFormulario = motivoSinFormulario(orden, sesion.permisos);
    const e = encabezadoOrden(orden);
    const clienteId = orden.cliente?.id ?? orden.clienteId ?? null;
    const chatOrden = useAbrirChatOrden(orden.id);
    // Sin módulo de portapapeles en la app (sin dependencias nuevas): la hoja de compartir del
    // sistema trae «Copiar».
    const copiar = () => void Share.share({ message: orden.direccion }).catch(() => undefined);
    const elegir = (accion: () => void) => () => {
        setMenu(false);
        accion();
    };
    const opciones: { texto: string; icono: LucideIcon; alPresionar: () => void; motivo?: string; nota?: string }[] = [
        ...(alAgregarNota ? [{ texto: "Agregar nota", icono: PenLine, alPresionar: elegir(alAgregarNota) }] : []),
        { texto: "Agregar formulario", icono: FilePlus2, alPresionar: elegir(() => setFormulario(true)), ...(sinFormulario ? { motivo: sinFormulario } : {}) },
        ...(chatOrden
            ? [{ texto: "Chat de la orden", icono: MessageCircle, alPresionar: elegir(() => void chatOrden.abrir()), ...(chatOrden.noLeidos > 0 ? { nota: `${textoContador(chatOrden.noLeidos)} sin leer` } : {}) }]
            : []),
        ...(clienteId !== null ? [{ texto: "Historial del cliente", icono: History, alPresionar: elegir(() => setHistorial(true)) }] : []),
        ...(orden.direccion ? [{ texto: "Copiar dirección", icono: Copy, alPresionar: elegir(copiar) }] : []),
    ];
    return (
        <View style={estilos.encabezado} testID="encabezado-orden">
            <BotonRedondo icono={ArrowLeft} etiqueta="Mis órdenes" fondo={campo.panel} tinta={colores.texto} borde={colores.borde} alPresionar={alVolver} />
            <View style={estilos.centro}>
                <Text accessibilityRole="header" style={[estilos.numero, { color: colores.texto }]}>
                    {orden.numero}
                </Text>
                <View accessible accessibilityLabel={e.texto} testID="linea-estado" style={estilos.linea}>
                    <View style={[estilos.punto, { backgroundColor: colores.tono[e.tono] }]} />
                    <Text numberOfLines={1} style={[estilos.textoLinea, { color: colores.textoSuave }]}>
                        {e.texto}
                    </Text>
                </View>
            </View>
            <BotonRedondo icono={Ellipsis} etiqueta="Más acciones de la orden" fondo={campo.panel} tinta={colores.texto} borde={colores.borde} alPresionar={() => setMenu(true)} testID="menu-orden" />
            <Dialogo abierto={menu} alCerrar={() => setMenu(false)} titulo={`Más acciones de ${orden.numero}`} testID="hoja-menu-orden">
                {opciones.map((o) => (
                    <Tocable key={o.texto} etiqueta={o.motivo ? `${o.texto}: ${o.motivo}` : o.nota ? `${o.texto}, ${o.nota}` : o.texto} alPresionar={o.alPresionar} deshabilitado={Boolean(o.motivo)} estilo={[estilos.opcion, o.motivo ? estilos.deshabilitada : null]} testID={`opcion-${o.texto}`}>
                        <o.icono size={20} color={o.motivo ? colores.textoSuave : colores.texto} />
                        <View style={estilos.flex}>
                            <Text style={[estilos.textoOpcion, { color: o.motivo ? colores.textoSuave : colores.texto }]}>{o.texto}</Text>
                            {o.motivo ? <Text style={[estilos.motivo, { color: colores.textoSuave }]}>{o.motivo}</Text> : null}
                            {o.nota ? <Text style={[estilos.motivo, { color: colores.tono.info }]}>{o.nota}</Text> : null}
                        </View>
                    </Tocable>
                ))}
            </Dialogo>
            <HojaAgregarFormulario orden={orden} abierto={formulario} alCambiar={setFormulario} />
            {historial && clienteId !== null ? <HojaHistorial orden={orden} clienteId={clienteId} alCerrar={() => setHistorial(false)} /> : null}
        </View>
    );
}

/** «En camino» desde el grupo: con GPS si el permiso ya está dado; sin él, igual a la cola. */
function useEnCamino(orden: OrdenVista) {
    const { motor, ejecutar, servicios, rastreo } = useCampo();
    const [ocupado, setOcupado] = useState(false);
    const correr = async () => {
        setOcupado(true);
        const p = await servicios.posicion();
        const datos = p ? { lat: p.lat, lon: p.lon, precision: p.precision } : {};
        if (await ejecutar(() => motor.registrar("orden.iniciar_camino", orden.id, datos), p ? "En camino, con ubicación" : "En camino, sin ubicación")) {
            void rastreo?.accionPropia();
        }
        setOcupado(false);
    };
    return { correr, ocupado };
}

export function AccionesPrincipales({ orden }: { orden: OrdenVista }) {
    const { navegar, sesion } = useCampo();
    const { colores } = useTema();
    const a = accionesPantallaOrden(orden, puedeReanudar(sesion.permisos));
    const reanudar = useReanudar(orden);
    const camino = useEnCamino(orden);
    const [pausando, setPausando] = useState(false);
    const [fallando, setFallando] = useState(false);
    if (a.pausa) {
        return (
            <View style={estilos.pila}>
                <View accessible accessibilityRole="text" accessibilityLabel={a.pausa.texto} testID="pausa-orden" style={[estilos.pausa, { borderColor: colores.tono.warning, backgroundColor: colores.fondoTono.warning }]}>
                    <Clock size={20} color={colores.tono.warning} />
                    <Text style={[estilos.textoPausa, { color: colores.tono.warning }]}>{a.pausa.texto}</Text>
                </View>
                {a.reanudar ? (
                    <Boton variante="tinta" icono={Play} alPresionar={() => void reanudar()} estilo={estilos.pildora} testID="reanudar">
                        Reanudar
                    </Boton>
                ) : (
                    <Texto suave>No tienes permiso para reanudarla: pídeselo a despacho.</Texto>
                )}
            </View>
        );
    }
    if (!a.principal && a.grupo.length === 0) {
        return null;
    }
    const trabajo = botonTrabajo(orden);
    return (
        <View style={estilos.pila}>
            {a.principal ? (
                <Boton variante="tinta" icono={a.principal.empezado ? ListChecks : Play} alPresionar={() => navegar(trabajo.vista)} estilo={estilos.pildora} testID="accion-principal">
                    {a.principal.texto}
                </Boton>
            ) : null}
            {a.grupo.length > 0 ? (
                <GrupoBotones
                    etiqueta="Más acciones de la orden"
                    testID="grupo-acciones"
                    botones={a.grupo.map((g) =>
                        g === "en_camino"
                            ? { texto: camino.ocupado ? "Ubicando…" : "En camino", icono: Navigation, deshabilitado: camino.ocupado, alPresionar: () => void camino.correr(), testID: "grupo-en-camino" }
                            : g === "pausar"
                              ? { texto: "Pausar", icono: PauseCircle, alPresionar: () => setPausando(true), testID: "grupo-pausar" }
                              : { texto: "No se pudo", icono: CircleX, peligro: true, alPresionar: () => setFallando(true), testID: "grupo-fallar" },
                    )}
                />
            ) : null}
            <HojaPausar orden={orden} abierto={pausando} alCambiar={setPausando} />
            <HojaFallar orden={orden} abierto={fallando} alCambiar={setFallando} />
        </View>
    );
}

type EstadoHistorial = { estado: "cargando" } | { estado: "lista"; ordenes: OrdenHistorial[] } | { estado: "error"; texto: string };

function HojaHistorial({ orden, clienteId, alCerrar }: { orden: OrdenVista; clienteId: number; alCerrar: () => void }) {
    const leer = useLeerHistorial();
    const { colores } = useTema();
    const [estado, setEstado] = useState<EstadoHistorial>(leer ? { estado: "cargando" } : { estado: "error", texto: "El historial no está disponible en este teléfono." });
    const pedir = useCallback(() => {
        if (!leer) {
            return;
        }
        leer(clienteId)
            .then((ordenes) => setEstado({ estado: "lista", ordenes: ordenes.filter((o) => o.id !== orden.id) }))
            .catch((e: unknown) =>
                setEstado({
                    estado: "error",
                    texto: e instanceof ErrorTransporte && e.tipo === "red" ? "Sin señal: el historial se lee con conexión." : textoProblema(e instanceof ErrorTransporte ? e.problema : null, "No se pudo leer el historial."),
                }),
            );
    }, [leer, clienteId, orden.id]);
    useEffect(() => {
        pedir();
    }, [pedir]);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "el cliente";
    return (
        <Dialogo abierto alCerrar={alCerrar} titulo="Historial del cliente" descripcion={`Las órdenes de ${nombre} que fueron tuyas.`} testID="hoja-historial">
            {estado.estado === "cargando" ? (
                <Cargando etiqueta="Leyendo el historial" />
            ) : estado.estado === "error" ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo leer el historial"
                    accion={
                        <Boton
                            variante="secundario"
                            alPresionar={() => {
                                setEstado({ estado: "cargando" });
                                pedir();
                            }}
                        >
                            Reintentar
                        </Boton>
                    }
                >
                    {estado.texto}
                </Aviso>
            ) : estado.ordenes.length === 0 ? (
                <Texto suave>No hay otras órdenes tuyas con este cliente.</Texto>
            ) : (
                estado.ordenes.map((o) => (
                    <View key={o.id} style={[estilos.historial, { borderColor: colores.borde }]}>
                        <View style={estilos.filaHistorial}>
                            <Texto mono negrita>
                                {o.numero}
                            </Texto>
                            <Texto suave>{ESTADOS_TEXTO[o.estado].texto}</Texto>
                        </View>
                        <Texto suave>
                            {TIPOS_ORDEN[o.tipo]}
                            {o.programadaInicio ? ` · ${fechaHoraCorta(o.programadaInicio) ?? ""}` : ""}
                        </Texto>
                    </View>
                ))
            )}
        </Dialogo>
    );
}

const estilos = StyleSheet.create({
    encabezado: { flexDirection: "row", alignItems: "center", gap: 8 },
    centro: { flex: 1, minWidth: 0, alignItems: "center", gap: 2 },
    numero: { fontSize: 20, fontWeight: "700", fontVariant: ["tabular-nums"] },
    linea: { flexDirection: "row", alignItems: "center", gap: 6, maxWidth: "100%" },
    punto: { width: 8, height: 8, borderRadius: 4 },
    textoLinea: { fontSize: 14, flexShrink: 1 },
    opcion: { flexDirection: "row", alignItems: "center", gap: 12, minHeight: 52, paddingHorizontal: 4 },
    textoOpcion: { fontSize: 16 },
    motivo: { fontSize: 13 },
    deshabilitada: { opacity: 0.7 },
    flex: { flex: 1 },
    pila: { gap: 12 },
    pildora: { minHeight: 52, borderRadius: 999 },
    pausa: { flexDirection: "row", alignItems: "center", gap: 10, minHeight: 48, borderWidth: 1, borderRadius: 16, paddingHorizontal: 16, paddingVertical: 10 },
    textoPausa: { flex: 1, fontSize: 16, fontWeight: "600" },
    historial: { borderWidth: 1, borderRadius: 16, padding: 12, gap: 2 },
    filaHistorial: { flexDirection: "row", justifyContent: "space-between", gap: 8 },
});
