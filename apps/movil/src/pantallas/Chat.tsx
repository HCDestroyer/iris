// Chat interno del equipo en la app nativa (tablero C-Chat, identidad «combinada»): la entrada con
// el punto de no leídos (`EntradaChat`, un solo componente para ponerlo en la isla o arriba), «Abrir
// chat de la orden», la lista (deslizar para actualizar), «Nuevo mensaje» y la conversación con
// burbujas, «visto», separadores de día, anteriores al subir, menú al mantener presionado y el
// compositor (foto, ubicación, respuestas rápidas) por encima del teclado. La lógica es la de la PWA
// (`@iris/sync/chat`).
import { ErrorTransporte } from "@iris/sync";
import {
    elementosHilo,
    enlaceUbicacion,
    ErrorChatLocal,
    estadoEnvio,
    horaLista,
    horaMensaje,
    puedeEliminar,
    RESPUESTAS_RAPIDAS,
    segmentosTexto,
    subtituloConversacion,
    TEXTO_ELIMINADO,
    textoContador,
    textoValido,
    tituloConversacion,
    vistaPrevia,
    type ConversacionVista,
    type MensajeVista,
} from "@iris/sync/chat";
import { iniciales } from "@iris/sync/campo";
import { ArrowLeft, Ban, Camera, ChevronRight, ClipboardList, MapPin, MessageCircle, MessageSquarePlus, RotateCcw, Send, Trash2, TriangleAlert, Users, WifiOff } from "lucide-react-native";
import { useEffect, useRef, useState, type ComponentRef, type ReactNode } from "react";
import { Image, Linking, RefreshControl, ScrollView, StyleSheet, Text, View, type NativeScrollEvent, type NativeSyntheticEvent } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Aviso, Boton, BotonRedondo, CampoMensaje, Cargando, Dialogo, EvitarTeclado, Pila, Texto, Titulo, Tocable, TocableLargo, useAltoTeclado } from "../componentes";
import { useChat, useChatOpcional, useConversacion } from "../campo/chat";
import { useCampo } from "../campo/contexto";
import { entorno } from "../entorno";
import { useTema } from "../tema/tema";

function useAhora(cadaMs: number): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = setInterval(() => setAhora(Date.now()), cadaMs);
        return () => clearInterval(t);
    }, [cadaMs]);
    return ahora;
}

function Contador({ n }: { n: number }) {
    const { colores } = useTema();
    return (
        <View style={[estilos.contador, { backgroundColor: colores.primario }]} testID="no-leidos-chat">
            <Text style={[estilos.textoContador, { color: colores.sobrePrimario }]}>{textoContador(n)}</Text>
        </View>
    );
}

/**
 * Entrada al chat con el punto de no leídos. `fila`: el botón ancho de «Yo»; `redondo`: 48 px
 * para la isla o el encabezado.
 */
export function EntradaChat({ variante = "fila" }: { variante?: "fila" | "redondo" }) {
    const contexto = useChatOpcional();
    const { navegar } = useCampo();
    const { campo, colores } = useTema();
    if (!contexto) {
        return null;
    }
    const n = contexto.estado.noLeidos;
    const etiqueta = n > 0 ? `Chat del equipo (${n} sin leer)` : "Chat del equipo";
    if (variante === "redondo") {
        return (
            <BotonRedondo icono={MessageCircle} etiqueta={etiqueta} alPresionar={() => navegar({ tipo: "chat" })} fondo={campo.panel2} tinta={colores.texto} testID="entrada-chat">
                {n > 0 ? (
                    <View style={estilos.contadorFlotante}>
                        <Contador n={n} />
                    </View>
                ) : null}
            </BotonRedondo>
        );
    }
    return (
        <Tocable etiqueta={etiqueta} alPresionar={() => navegar({ tipo: "chat" })} estilo={[estilos.filaEntrada, { backgroundColor: campo.panel, borderColor: colores.borde }]} testID="entrada-chat">
            <MessageCircle size={20} color={colores.texto} />
            <Text style={[estilos.flex, estilos.textoFila, { color: colores.texto }]}>Chat del equipo</Text>
            {n > 0 ? <Contador n={n} /> : null}
            <ChevronRight size={20} color={colores.textoSuave} />
        </Tocable>
    );
}

/** «Abrir chat de la orden» (`POST /chat/ordenes/{id}/abrir`); sin señal, el hilo ya guardado. */
/**
 * Abre (o crea) la conversación de la orden: con la conocida y sin señal va directo; si no, la pide
 * al servidor. `null` sin chat, o sin `chat.enviar` y sin conversación conocida. Lo usan el botón
 * del detalle y «Chat de la orden» del menú «⋯».
 */
export function useAbrirChatOrden(ordenId: number): { abrir: () => Promise<void>; abriendo: boolean; noLeidos: number } | null {
    const contexto = useChatOpcional();
    const { navegar, notificar, servicios } = useCampo();
    const [abriendo, setAbriendo] = useState(false);
    if (!contexto || (!contexto.puedeEnviar && !contexto.chat.conversacionDeOrden(ordenId))) {
        return null;
    }
    const conocida = contexto.chat.conversacionDeOrden(ordenId);
    const abrir = async () => {
        if (conocida && !servicios.enLinea()) {
            navegar({ tipo: "conversacion", id: conocida.id });
            return;
        }
        setAbriendo(true);
        try {
            const c = await contexto.chat.abrirOrden(ordenId);
            navegar({ tipo: "conversacion", id: c.id });
        } catch (e) {
            if (conocida) {
                navegar({ tipo: "conversacion", id: conocida.id });
            } else {
                notificar({ tono: "error", texto: e instanceof ErrorTransporte && e.tipo === "red" ? "Sin señal: el chat de la orden se abre al volver la señal." : "No se pudo abrir el chat de la orden." });
            }
        } finally {
            setAbriendo(false);
        }
    };
    return { abrir, abriendo, noLeidos: conocida?.no_leidos ?? 0 };
}

export function AbrirChatOrden({ ordenId }: { ordenId: number }) {
    const chatOrden = useAbrirChatOrden(ordenId);
    if (!chatOrden) {
        return null;
    }
    const n = chatOrden.noLeidos;
    return (
        <Boton variante="secundario" icono={MessageCircle} cargando={chatOrden.abriendo} alPresionar={() => void chatOrden.abrir()} testID="abrir-chat-orden">
            {n > 0 ? `Abrir chat de la orden · ${textoContador(n)} sin leer` : "Abrir chat de la orden"}
        </Boton>
    );
}

function Avatar({ c }: { c: ConversacionVista }) {
    const { campo, colores } = useTema();
    const Icono = c.tipo === "orden" ? ClipboardList : Users;
    return (
        <View style={[estilos.avatar, { backgroundColor: c.tipo === "directa" ? campo.marcaSuave : campo.panel2 }]}>
            {c.tipo === "directa" ? <Text style={[estilos.iniciales, { color: campo.marcaSuaveTexto }]}>{iniciales(tituloConversacion(c)) || "?"}</Text> : <Icono size={20} color={colores.texto} />}
        </View>
    );
}

/** Lista de conversaciones con «deslizar para actualizar» (pantalla propia, con la isla). */
export function ListaChat() {
    const { chat, estado, puedeEnviar } = useChat();
    const { navegar, sesion } = useCampo();
    const { campo, colores } = useTema();
    const margen = useSafeAreaInsets();
    const [actualizando, setActualizando] = useState(false);
    const ahora = useAhora(30_000);
    const actualizar = () => {
        setActualizando(true);
        void chat.recargar().finally(() => setActualizando(false));
    };
    useEffect(() => {
        void chat.recargar();
    }, [chat]);
    const error = estado.lista.estado === "error" ? estado.lista.error : null;
    const sinDatos = estado.conversaciones.length === 0;
    return (
        <ScrollView
            style={{ backgroundColor: campo.lienzo }}
            contentContainerStyle={[estilos.contenido, { paddingTop: margen.top + 16, paddingBottom: margen.bottom + 180 }]}
            refreshControl={<RefreshControl refreshing={actualizando} onRefresh={actualizar} tintColor={colores.primario} colors={[colores.primario]} />}
            testID="lista-chat"
        >
            <Pila espacio={4}>
                <Titulo>Chat del equipo</Titulo>
                <Texto suave>Despacho, tu cuadrilla y tus órdenes. El cliente no lo ve.</Texto>
            </Pila>
            {puedeEnviar ? (
                <Boton icono={MessageSquarePlus} alPresionar={() => navegar({ tipo: "chat_nuevo" })}>
                    Nuevo mensaje
                </Boton>
            ) : null}
            {!estado.enLinea ? (
                <View style={estilos.filaAviso}>
                    <WifiOff size={16} color={colores.tono.warning} />
                    <Text style={[estilos.flex, { color: colores.tono.warning }]}>Sin señal: ves lo guardado; lo que escribas se envía al volver la señal.</Text>
                </View>
            ) : null}
            {estado.sesionCerrada ? (
                <Aviso tono="error" titulo="Tu sesión terminó">
                    Entra de nuevo para leer y enviar mensajes. Lo que escribiste sigue guardado.
                </Aviso>
            ) : null}
            {error && sinDatos ? (
                <Aviso tono="error" titulo="No se pudieron leer tus conversaciones" accion={<Boton variante="secundario" alPresionar={actualizar}>Reintentar</Boton>}>
                    {estado.enLinea ? "Inténtalo de nuevo en un momento." : "Sin señal. Se intentará al volver la señal."}
                </Aviso>
            ) : error ? (
                <View style={estilos.filaAviso}>
                    <TriangleAlert size={16} color={colores.tono.warning} />
                    <Text style={[estilos.flex, { color: colores.textoSuave }]}>No se pudo actualizar; ves lo guardado.</Text>
                    <Boton variante="fantasma" alPresionar={actualizar}>
                        Reintentar
                    </Boton>
                </View>
            ) : null}
            {sinDatos && !error ? (
                estado.lista.estado === "cargando" ? (
                    <Cargando etiqueta="Leyendo tus conversaciones" />
                ) : (
                    <Aviso tono="neutral" icono={MessageCircle} titulo="Todavía no tienes conversaciones">
                        Abre el chat desde una orden o escribe un «Nuevo mensaje» a alguien de tu equipo.
                    </Aviso>
                )
            ) : (
                estado.conversaciones.map((c) => {
                    const titulo = tituloConversacion(c);
                    const etiqueta = [titulo, c.no_leidos > 0 ? `${c.no_leidos} sin leer` : null, c.fallidos > 0 ? "con mensajes no enviados" : null].filter(Boolean).join(", ");
                    return (
                        <Tocable key={c.id} etiqueta={etiqueta} alPresionar={() => navegar({ tipo: "conversacion", id: c.id })} estilo={[estilos.filaConversacion, { backgroundColor: campo.panel, borderColor: colores.borde }]} testID={`conversacion-${c.id}`}>
                            <Avatar c={c} />
                            <View style={estilos.flex}>
                                <View style={estilos.filaEntre}>
                                    <Text numberOfLines={1} style={[estilos.flex, estilos.tituloFila, { color: colores.texto, fontWeight: c.no_leidos > 0 ? "700" : "600" }]}>
                                        {titulo}
                                    </Text>
                                    <Text style={{ color: c.no_leidos > 0 ? colores.primario : colores.textoSuave, fontSize: 12 }}>{horaLista(c.ultimo_mensaje?.enviado_en ?? c.ultima_actividad, ahora)}</Text>
                                </View>
                                <View style={estilos.filaEntre}>
                                    <Text numberOfLines={1} style={[estilos.flex, { color: c.no_leidos > 0 ? colores.texto : colores.textoSuave, fontSize: 14 }]}>
                                        {c.fallidos > 0 ? <Text style={{ color: colores.error, fontWeight: "600" }}>No enviado · </Text> : c.pendientes > 0 ? <Text style={{ color: colores.tono.warning }}>Por enviar · </Text> : null}
                                        {vistaPrevia(c, sesion.usuarioId)}
                                    </Text>
                                    {c.no_leidos > 0 ? <Contador n={c.no_leidos} /> : null}
                                </View>
                            </View>
                        </Tocable>
                    );
                })
            )}
        </ScrollView>
    );
}

/** «Nuevo mensaje»: personas que el teléfono ya conoce del chat (no hay lista de contactos en la API). */
export function NuevoChat() {
    const { chat } = useChat();
    const { navegar, notificar, atras } = useCampo();
    const { campo } = useTema();
    const margen = useSafeAreaInsets();
    const [abriendo, setAbriendo] = useState<number | null>(null);
    const contactos = chat.contactosConocidos();
    const abrir = async (id: number) => {
        setAbriendo(id);
        try {
            const c = await chat.abrirDirecta(id);
            navegar({ tipo: "conversacion", id: c.id });
        } catch (e) {
            const codigo = e instanceof ErrorTransporte ? e.codigo : null;
            notificar({ tono: "error", texto: codigo === "usuario_no_disponible" ? "Esa persona ya no está disponible para el chat." : "No se pudo abrir la conversación." });
        } finally {
            setAbriendo(null);
        }
    };
    return (
        <ScrollView style={{ backgroundColor: campo.lienzo }} contentContainerStyle={[estilos.contenido, { paddingTop: margen.top + 16, paddingBottom: margen.bottom + 180 }]}>
            <View style={estilos.filaEntre}>
                <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => (atras() ? undefined : navegar({ tipo: "chat" }))}>
                    Chat
                </Boton>
            </View>
            <Titulo>Nuevo mensaje</Titulo>
            <Texto suave>Personas de tu equipo con las que ya coincidiste en el chat (tu cuadrilla, despacho y tus órdenes).</Texto>
            {contactos.length === 0 ? (
                <Aviso tono="neutral" icono={Users} titulo="Aún no hay personas para elegir">
                    Cuando alguien de despacho o de tu cuadrilla te escriba, aparecerá aquí. Mientras, usa el chat de tu cuadrilla o el de una orden.
                </Aviso>
            ) : (
                contactos.map((p) => (
                    <Boton key={p.id} variante="secundario" cargando={abriendo === p.id} deshabilitado={abriendo !== null} alPresionar={() => void abrir(p.id)}>
                        {p.nombre}
                    </Boton>
                ))
            )}
        </ScrollView>
    );
}

function TextoPlano({ texto, color }: { texto: string; color: string }) {
    // Texto plano (nunca HTML); solo http(s) es enlace y se abre fuera de la app.
    return (
        <Text style={[estilos.textoBurbuja, { color }]} selectable>
            {segmentosTexto(texto).map((s, i) =>
                s.tipo === "enlace" ? (
                    <Text key={i} accessibilityRole="link" style={estilos.enlace} onPress={() => void Linking.openURL(s.url)}>
                        {s.texto}
                    </Text>
                ) : (
                    s.texto
                ),
            )}
        </Text>
    );
}

function FotoBurbuja({ m }: { m: MensajeVista }) {
    const { fotosLocales } = useChat();
    const { campo, colores } = useTema();
    if (!m.foto) {
        return null;
    }
    const uri = m.foto.url ? `${entorno().baseUrl}${m.foto.url}` : (fotosLocales.get(m.foto.id) ?? null);
    return uri ? (
        <Image source={{ uri }} accessibilityLabel={m.propio ? "Foto que enviaste" : `Foto de ${m.autorNombre}`} style={estilos.foto} resizeMode="cover" />
    ) : (
        <View accessibilityLabel="Foto subiéndose" style={[estilos.foto, estilos.centro, { backgroundColor: campo.panel2 }]}>
            <Text style={{ color: colores.textoSuave, fontSize: 12 }}>Foto subiéndose…</Text>
        </View>
    );
}

function Burbuja({ m, conAutor, alMenu, alReintentar }: { m: MensajeVista; conAutor: boolean; alMenu: (m: MensajeVista) => void; alReintentar: (m: MensajeVista) => void }) {
    const { colores, campo } = useTema();
    const estado = estadoEnvio(m);
    const hora = horaMensaje(m);
    const tinta = m.eliminado ? colores.textoSuave : m.propio ? colores.sobrePrimario : colores.texto;
    const fondo = m.eliminado ? "transparent" : m.propio ? colores.primario : campo.panel;
    return (
        <View style={[estilos.columnaBurbuja, m.propio ? estilos.derecha : estilos.izquierda]} testID={`mensaje-${m.envio}`}>
            {conAutor ? <Text style={[estilos.autor, { color: colores.textoSuave }]}>{m.autorNombre}</Text> : null}
            <TocableLargo
                etiqueta={`${m.propio ? "Tú" : m.autorNombre}, ${hora}${estado ? `, ${estado.texto}` : ""}: ${m.eliminado ? TEXTO_ELIMINADO : (m.texto ?? (m.foto ? "foto" : "ubicación"))}`}
                alMantener={() => alMenu(m)}
                estilo={[
                    estilos.burbuja,
                    m.propio ? estilos.colaDerecha : estilos.colaIzquierda,
                    { backgroundColor: fondo, borderColor: m.envio === "fallido" ? colores.error : m.eliminado || !m.propio ? colores.borde : fondo, borderStyle: m.eliminado ? "dashed" : "solid" },
                ]}
                testID="burbuja"
            >
                {m.eliminado ? (
                    <View style={estilos.filaAviso}>
                        <Ban size={16} color={tinta} />
                        <Text style={[estilos.textoBurbuja, { color: tinta, fontStyle: "italic" }]}>{TEXTO_ELIMINADO}</Text>
                    </View>
                ) : (
                    <>
                        <FotoBurbuja m={m} />
                        {m.ubicacion ? (
                            <Text accessibilityRole="link" style={[estilos.textoBurbuja, estilos.enlace, { color: tinta }]} onPress={() => m.ubicacion && void Linking.openURL(enlaceUbicacion(m.ubicacion.lat, m.ubicacion.lon))}>
                                📍 Ubicación compartida
                            </Text>
                        ) : null}
                        {m.texto ? <TextoPlano texto={m.texto} color={tinta} /> : null}
                    </>
                )}
                <Text style={[estilos.pie, { color: tinta, opacity: m.propio && !m.eliminado ? 0.85 : 1 }]}>
                    {hora}
                    {estado ? ` · ${estado.texto}` : ""}
                </Text>
            </TocableLargo>
            {m.envio === "fallido" ? (
                <Boton variante="fantasma" icono={TriangleAlert} alPresionar={() => alReintentar(m)} testID="reintentar-mensaje">
                    No enviado — Reintentar
                </Boton>
            ) : null}
        </View>
    );
}

function MenuMensaje({ m, alCerrar }: { m: MensajeVista | null; alCerrar: () => void }) {
    const { chat } = useChat();
    const { notificar, servicios } = useCampo();
    const ahora = useAhora(15_000);
    if (!m) {
        return null;
    }
    const eliminable = puedeEliminar(m, ahora);
    const correr = async (accion: () => Promise<void>, ok: string) => {
        alCerrar();
        try {
            await accion();
            notificar({ tono: "success", texto: ok });
        } catch (e) {
            notificar({ tono: "error", texto: e instanceof ErrorChatLocal ? e.message : "No se pudo guardar en el teléfono." });
        }
    };
    return (
        <Dialogo abierto alCerrar={alCerrar} titulo="Mensaje" testID="menu-mensaje">
            <Pila>
                {m.envio === "fallido" ? (
                    <>
                        <Boton variante="secundario" icono={RotateCcw} alPresionar={() => void correr(() => chat.reintentar(m.id), "Se enviará de nuevo")}>
                            Reintentar
                        </Boton>
                        <Boton variante="peligro" icono={Trash2} alPresionar={() => void correr(() => chat.descartar(m.id), "Mensaje descartado")}>
                            Descartar
                        </Boton>
                    </>
                ) : null}
                {eliminable ? (
                    <Boton variante="peligro" icono={Trash2} alPresionar={() => void correr(() => chat.eliminar(m.id), servicios.enLinea() ? "Mensaje eliminado" : "Se eliminará al volver la señal")}>
                        Eliminar para todos
                    </Boton>
                ) : m.propio && !m.eliminado && m.envio === "enviado" ? (
                    <Texto suave>Solo puedes eliminar un mensaje en los 15 minutos después de enviarlo.</Texto>
                ) : null}
                {m.texto ? <Texto suave>Para copiar el texto, mantenlo presionado en la burbuja.</Texto> : null}
                {!m.texto && m.envio !== "fallido" && !eliminable ? <Texto suave>No hay acciones para este mensaje.</Texto> : null}
            </Pila>
        </Dialogo>
    );
}

function Compositor({ conversacionId, placeholder }: { conversacionId: number; placeholder: string }) {
    const { chat, agregarFotoLocal } = useChat();
    const { notificar, servicios } = useCampo();
    const { campo, colores } = useTema();
    const margen = useSafeAreaInsets();
    const teclado = useAltoTeclado();
    const [texto, setTexto] = useState("");
    const [ocupado, setOcupado] = useState(false);
    const [conUbicacion, setConUbicacion] = useState(false);
    const v = textoValido(texto);
    useEffect(() => {
        let vivo = true;
        void servicios.ubicacionPermitida?.().then((s) => vivo && setConUbicacion(s));
        return () => {
            vivo = false;
        };
    }, [servicios]);

    const enviar = async (entrada: Parameters<typeof chat.escribir>[1], limpiar: boolean) => {
        try {
            const s = await chat.escribir(conversacionId, entrada);
            if (limpiar) {
                setTexto("");
            }
            return s;
        } catch (e) {
            notificar({ tono: "error", texto: e instanceof ErrorChatLocal ? e.message : "No se pudo guardar el mensaje en el teléfono." });
            return null;
        }
    };
    const foto = async () => {
        setOcupado(true);
        try {
            const t = await servicios.tomarFoto();
            if (t) {
                const s = await enviar({ foto: { datos: t.foto.bytes, mime: t.foto.mime, tomadoEn: t.exif.tomadoEn ?? new Date().toISOString() }, ...(v.valido ? { texto } : {}) }, v.valido);
                if (s?.foto && t.foto.uri) {
                    agregarFotoLocal(s.foto.id, t.foto.uri);
                }
            }
        } catch (e) {
            notificar({ tono: "error", texto: e instanceof Error && e.message ? e.message : "No se pudo tomar la foto." });
        } finally {
            setOcupado(false);
        }
    };
    const ubicacion = async () => {
        setOcupado(true);
        const p = await servicios.posicion(8000);
        setOcupado(false);
        if (!p) {
            notificar({ tono: "warning", texto: "No se pudo leer tu ubicación ahora." });
            return;
        }
        await enviar({ ubicacion: { lat: p.lat, lon: p.lon } }, false);
    };
    return (
        <View style={[estilos.compositor, { backgroundColor: campo.panel, borderColor: colores.borde, paddingBottom: (teclado > 0 ? 8 : margen.bottom + 12) }]} testID="compositor-chat">
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={estilos.rapidas} keyboardShouldPersistTaps="handled">
                {RESPUESTAS_RAPIDAS.map((r) => (
                    <Boton key={r} variante="secundario" alPresionar={() => void enviar({ texto: r }, false)}>
                        {r}
                    </Boton>
                ))}
            </ScrollView>
            <View style={estilos.filaCompositor}>
                <BotonRedondo icono={Camera} etiqueta="Tomar foto" alPresionar={() => void (ocupado ? undefined : foto())} fondo={campo.panel2} tinta={colores.texto} testID="tomar-foto-chat" />
                {conUbicacion ? (
                    <BotonRedondo icono={MapPin} etiqueta="Compartir mi ubicación" alPresionar={() => void (ocupado ? undefined : ubicacion())} fondo={campo.panel2} tinta={colores.texto} testID="ubicacion-chat" />
                ) : null}
                <CampoMensaje etiqueta="Mensaje" valor={texto} alCambiar={setTexto} placeholder={placeholder} testID="campo-mensaje" />
                <BotonRedondo icono={Send} etiqueta="Enviar" alPresionar={() => v.valido && void enviar({ texto }, true)} fondo={v.valido ? colores.primario : campo.panel2} tinta={v.valido ? colores.sobrePrimario : colores.textoTenue} testID="enviar-mensaje" />
            </View>
            {v.motivo ? <Text style={{ color: colores.error, fontSize: 12 }}>{v.motivo}</Text> : null}
        </View>
    );
}

/** Conversación a pantalla completa (encabezado propio, hilo y compositor sobre el teclado). */
export function ConversacionChat({ id }: { id: number }) {
    const { chat, estado, puedeEnviar } = useChat();
    const { navegar, notificar, atras } = useCampo();
    const { campo, colores } = useTema();
    const margen = useSafeAreaInsets();
    const v = useConversacion(id);
    const lista = useRef<ComponentRef<typeof ScrollView>>(null);
    const alFinal = useRef(true);
    const [menu, setMenu] = useState<MensajeVista | null>(null);
    const ahora = useAhora(60_000);

    useEffect(() => {
        void chat.cargarConversacion(id);
    }, [chat, id]);
    useEffect(() => {
        for (const a of estado.avisos) {
            notificar({ tono: a.tono, texto: a.texto });
            chat.descartarAviso(a.id);
        }
    }, [estado.avisos, chat, notificar]);
    const ultimo = v.mensajes.at(-1)?.id;
    useEffect(() => {
        if (alFinal.current && v.carga !== "cargando") {
            void chat.marcarLeido(id);
        }
    }, [chat, id, ultimo, v.carga, v.conversacion?.no_leidos]);

    const alDesplazar = (e: NativeSyntheticEvent<NativeScrollEvent>) => {
        const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
        alFinal.current = contentSize.height - contentOffset.y - layoutMeasurement.height < 48;
        if (alFinal.current) {
            void chat.marcarLeido(id);
        }
        if (contentOffset.y < 80 && v.hayAnteriores && !v.cargandoAnteriores) {
            void chat.cargarAnteriores(id);
        }
    };
    const c = v.conversacion;
    const titulo = c ? tituloConversacion(c) : "Conversación";
    const volver = () => {
        if (!atras()) {
            navegar({ tipo: "chat" });
        }
    };
    const reintentar = async (m: MensajeVista) => {
        await chat.reintentar(m.id);
        notificar({ tono: "success", texto: "Se enviará de nuevo" });
    };

    let cuerpo: ReactNode;
    if (!c && v.carga === "error") {
        cuerpo = (
            <Aviso tono="error" titulo="No se pudo abrir la conversación" accion={<Boton variante="secundario" alPresionar={() => void chat.cargarConversacion(id)}>Reintentar</Boton>}>
                {v.error?.status === 404 ? "Ya no tienes acceso a esta conversación." : "Revisa la señal e inténtalo de nuevo."}
            </Aviso>
        );
    } else if (!c) {
        cuerpo = <Cargando etiqueta="Abriendo la conversación" />;
    } else {
        const elementos = elementosHilo(v.mensajes, c.tipo !== "directa", ahora);
        cuerpo = (
            <>
                {v.cargandoAnteriores ? <Texto suave>Cargando mensajes anteriores…</Texto> : null}
                {v.carga === "error" ? (
                    <Boton variante="fantasma" icono={TriangleAlert} alPresionar={() => void chat.cargarConversacion(id)}>
                        No se pudo actualizar · Reintentar
                    </Boton>
                ) : null}
                {elementos.length === 0 && v.carga !== "cargando" ? <Texto suave>Sin mensajes todavía. Escribe el primero.</Texto> : null}
                {elementos.map((e) =>
                    e.tipo === "dia" ? (
                        <View key={e.clave} style={[estilos.separador, { backgroundColor: campo.marcaSuave }]} testID="separador-dia">
                            <Text style={[estilos.textoSeparador, { color: campo.marcaSuaveTexto }]}>{e.texto}</Text>
                        </View>
                    ) : (
                        <Burbuja key={e.clave} m={e.mensaje} conAutor={e.conAutor} alMenu={setMenu} alReintentar={(m) => void reintentar(m)} />
                    ),
                )}
            </>
        );
    }
    return (
        // El hilo se acorta y el compositor queda sobre el teclado (edge-to-edge en Android e iOS).
        <EvitarTeclado estilo={{ backgroundColor: campo.lienzo }} testID="conversacion-chat-teclado">
        <View style={[estilos.pantalla, { backgroundColor: campo.lienzo }]} testID="conversacion-chat">
            <View style={[estilos.encabezado, { paddingTop: margen.top + 12, backgroundColor: campo.panel, borderColor: colores.borde }]}>
                <BotonRedondo icono={ArrowLeft} etiqueta="Volver" alPresionar={volver} fondo={campo.panel2} tinta={colores.texto} />
                <View style={estilos.flex}>
                    <Text accessibilityRole="header" numberOfLines={1} style={[estilos.tituloConversacion, { color: colores.texto }]}>
                        {titulo}
                    </Text>
                    <Text numberOfLines={1} style={{ color: colores.textoSuave, fontSize: 12 }}>
                        {c ? subtituloConversacion(c) : ""}
                        {!estado.enLinea ? " · sin señal" : ""}
                    </Text>
                </View>
                {/* Discreto: un punto y «en vivo» solo con el flujo SSE abierto; con sondeo, nada. */}
                {estado.enLinea && estado.tiempoReal === "sse" ? (
                    <View style={estilos.enVivo} accessible accessibilityLabel="Chat en vivo" testID="chat-en-vivo">
                        <View style={[estilos.puntoVivo, { backgroundColor: colores.exito }]} />
                        <Text style={{ color: colores.textoSuave, fontSize: 12 }}>en vivo</Text>
                    </View>
                ) : null}
            </View>
            <ScrollView
                ref={lista}
                style={estilos.flex}
                contentContainerStyle={estilos.hilo}
                onScroll={alDesplazar}
                scrollEventThrottle={64}
                maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
                onContentSizeChange={() => alFinal.current && lista.current?.scrollToEnd({ animated: false })}
                keyboardShouldPersistTaps="handled"
                testID="hilo-chat"
            >
                {cuerpo}
            </ScrollView>
            {c && puedeEnviar ? <Compositor conversacionId={id} placeholder={c.tipo === "orden" ? "Escribe a despacho…" : c.tipo === "cuadrilla" ? "Escribe a tu cuadrilla…" : `Escribe a ${tituloConversacion(c)}…`} /> : null}
            <MenuMensaje m={menu} alCerrar={() => setMenu(null)} />
        </View>
        </EvitarTeclado>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    centro: { alignItems: "center", justifyContent: "center" },
    pantalla: { flex: 1 },
    contenido: { paddingHorizontal: 18, gap: 12 },
    contador: { minWidth: 20, height: 20, borderRadius: 10, paddingHorizontal: 6, alignItems: "center", justifyContent: "center" },
    contadorFlotante: { position: "absolute", top: -2, right: -2 },
    textoContador: { fontSize: 11, fontWeight: "700" },
    filaEntrada: { minHeight: 56, flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 16, borderRadius: 18, borderWidth: 1 },
    textoFila: { fontSize: 16, fontWeight: "600" },
    filaAviso: { flexDirection: "row", alignItems: "center", gap: 8 },
    filaConversacion: { minHeight: 72, flexDirection: "row", alignItems: "center", gap: 12, padding: 12, borderRadius: 22, borderWidth: 1 },
    filaEntre: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
    tituloFila: { fontSize: 16 },
    avatar: { width: 48, height: 48, borderRadius: 24, alignItems: "center", justifyContent: "center" },
    iniciales: { fontSize: 16, fontWeight: "700" },
    encabezado: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 14, paddingBottom: 12, borderBottomWidth: 1 },
    tituloConversacion: { fontSize: 16, fontWeight: "600" },
    enVivo: { flexDirection: "row", alignItems: "center", gap: 6 },
    puntoVivo: { width: 8, height: 8, borderRadius: 4 },
    hilo: { paddingHorizontal: 14, paddingVertical: 12, gap: 10 },
    columnaBurbuja: { maxWidth: "80%", gap: 4 },
    derecha: { alignSelf: "flex-end", alignItems: "flex-end" },
    izquierda: { alignSelf: "flex-start", alignItems: "flex-start" },
    autor: { fontSize: 12, fontWeight: "600", paddingHorizontal: 4 },
    burbuja: { paddingHorizontal: 12, paddingVertical: 10, borderRadius: 18, borderWidth: 1, gap: 6, minHeight: 44 },
    colaDerecha: { borderBottomRightRadius: 6 },
    colaIzquierda: { borderBottomLeftRadius: 6 },
    textoBurbuja: { fontSize: 15, lineHeight: 22 },
    enlace: { textDecorationLine: "underline" },
    pie: { fontSize: 11, fontWeight: "500" },
    foto: { width: 220, height: 150, borderRadius: 14 },
    separador: { alignSelf: "center", borderRadius: 999, paddingHorizontal: 12, paddingVertical: 4 },
    textoSeparador: { fontSize: 12, fontWeight: "600" },
    compositor: { borderTopWidth: 1, paddingHorizontal: 14, paddingTop: 10, gap: 10 },
    rapidas: { gap: 8 },
    filaCompositor: { flexDirection: "row", alignItems: "flex-end", gap: 8 },
});
