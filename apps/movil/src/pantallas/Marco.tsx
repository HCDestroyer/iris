// Marco de la app con el diseño «combinada» (como la PWA): el encabezado va dentro del desplazamiento
// (se va al bajar y vuelve compacto al subir), la vista actual, el aviso que entra desde arriba con su
// acción, la píldora de estado (la barra discreta de la fase 4) y la isla de menú Hoy · Por recibir ·
// «+» · Agenda · Yo, que se encoge al bajar. «Atrás» de Android vuelve a la vista anterior. Baja el
// paquete del día al abrir y al cambiar el día, con espera exponencial tras un error (como la PWA).
import { ErrorTransporte } from "@iris/sync";
import { CONEXION_TEXTO, DESPLAZAMIENTO_INICIAL, desplazar, etiquetaPunto, hoyEnGuatemala, horaCorta, resumenEnvio, urlDeVista, type TonoEstado } from "@iris/sync/campo";
import { Building2, KeyRound, LocateFixed, LocateOff, RefreshCw, type LucideIcon } from "lucide-react-native";
import { useCallback, useEffect, useRef, useState } from "react";
import { ScrollView, StyleSheet, View, type NativeScrollEvent, type NativeSyntheticEvent } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Aviso, AvisoEmergente, Boton, Insignia, Texto } from "../componentes";
import { ProveedorBorradores } from "../campo/borradores";
import { useAtrasAndroid } from "../campo/atras";
import { useCampo, useEstadoPosiciones } from "../campo/contexto";
import type { EstadoRastreo } from "../campo/ubicacion";
import { activarHuella, CODIGO_CAMBIO_CONTRASENA, debeOfrecerHuella } from "../sesion/sesion";
import { useTema } from "../tema/tema";
import { AgendaDia, AgendaSemana } from "./Agenda";
import { ConversacionChat, ListaChat, NuevoChat } from "./Chat";
import { BarraCompacta, Encabezado } from "./Encabezado";
import { AvisoIsla, IslaMenu, IslaPasos } from "./Isla";
import { ListaOrdenes, type CargaPaquete } from "./ListaOrdenes";
import { MiBodega } from "./MiBodega";
import { DetalleOrden } from "./orden/DetalleOrden";
import { Pendientes } from "./Pendientes";
import { DetalleTraslado, PorRecibir } from "./PorRecibir";
import { Yo } from "./Yo";

const ESPERA_BASE_MS = 5_000;
const ESPERA_MAX_MS = 10 * 60_000;

export function Marco({ empresa, cambiarEmpresa, alSesionVencida }: { empresa: string | null; cambiarEmpresa?: () => void; alSesionVencida: () => void }) {
    const { motor, estado, vista, atras } = useCampo();
    useAtrasAndroid(atras);
    useAvisoDeRechazos();
    useOfertaHuella();
    const { campo } = useTema();
    const margen = useSafeAreaInsets();
    // Por vista: cada una empieza arriba, con el encabezado completo y la isla sin encoger.
    const url = urlDeVista(vista);
    const [porVista, setPorVista] = useState({ url, d: DESPLAZAMIENTO_INICIAL });
    const desplazamiento = porVista.url === url ? porVista.d : DESPLAZAMIENTO_INICIAL;
    const altoEncabezado = useRef(0);
    const alDesplazar = useCallback(
        (e: NativeSyntheticEvent<NativeScrollEvent>) => {
            const y = e.nativeEvent.contentOffset.y;
            setPorVista((p) => ({ url, d: desplazar(p.url === url ? p.d : DESPLAZAMIENTO_INICIAL, y, altoEncabezado.current) }));
        },
        [url],
    );
    // Los pasos de la orden llevan su propia isla («Anterior» · «Guardar» · «Siguiente»); la firma y
    // el cierre, su botón al pie.
    const paso = vista.tipo === "orden" ? vista.paso : undefined;
    // La conversación ocupa la pantalla (encabezado propio y compositor sobre el teclado): sin isla.
    const conIsla = paso === undefined && vista.tipo !== "conversacion";
    // El chat lleva su propio desplazamiento (deslizar para actualizar, el hilo).
    const pantallaChat = vista.tipo === "chat" || vista.tipo === "chat_nuevo" || vista.tipo === "conversacion";
    const conIslaPasos = vista.tipo === "orden" && paso !== undefined && paso !== "cierre";
    const [carga, setCarga] = useState<CargaPaquete>({ estado: "inactiva" });
    const descargando = useRef(false);
    const espera = useRef({ fallos: 0, hasta: 0 });
    const ultimoDia = useRef<string | null>(null);

    const bajarPaquete = useCallback(
        async (manual = true) => {
            if (descargando.current || (!manual && Date.now() < espera.current.hasta)) {
                return;
            }
            descargando.current = true;
            const hoy = hoyEnGuatemala();
            ultimoDia.current = hoy;
            if (manual || motor.obtenerEstado().fecha === null) {
                setCarga({ estado: "cargando" });
            }
            try {
                await motor.cargarPaquete(hoy);
                espera.current = { fallos: 0, hasta: 0 };
                setCarga({ estado: "lista" });
            } catch (e) {
                if (e instanceof ErrorTransporte && e.codigo === CODIGO_CAMBIO_CONTRASENA) {
                    setCarga({ estado: "error", mensaje: "Tienes que cambiar tu contraseña en el panel de Iris antes de seguir." });
                    return;
                }
                if (e instanceof ErrorTransporte && e.esDeSesion) {
                    alSesionVencida();
                }
                const fallos = espera.current.fallos + 1;
                espera.current = { fallos, hasta: Date.now() + Math.min(ESPERA_BASE_MS * 2 ** (fallos - 1), ESPERA_MAX_MS) };
                const sinSenal = e instanceof ErrorTransporte && e.tipo === "red";
                setCarga({ estado: "error", mensaje: sinSenal ? "Sin señal: se usan las órdenes guardadas en el teléfono." : "No se pudieron bajar las órdenes del día." });
            } finally {
                descargando.current = false;
            }
        },
        [motor, alSesionVencida],
    );

    useEffect(() => {
        const revisar = () => void bajarPaquete(false);
        revisar();
        // Con la app abierta al pasar la medianoche de Guatemala se baja el paquete del día nuevo.
        const t = setInterval(() => {
            if (ultimoDia.current !== hoyEnGuatemala()) {
                void bajarPaquete(false);
            }
        }, 60_000);
        return () => clearInterval(t);
    }, [bajarPaquete]);

    return (
        <ProveedorBorradores>
        <View className="flex-1 bg-background" style={[estilos.pantalla, { backgroundColor: campo.lienzo }]}>
            {pantallaChat ? (
                vista.tipo === "conversacion" ? (
                    <ConversacionChat id={vista.id} />
                ) : vista.tipo === "chat_nuevo" ? (
                    <NuevoChat />
                ) : (
                    <ListaChat />
                )
            ) : (
            /* Cada vista empieza arriba (como la PWA al navegar). */
            <ScrollView
                key={url}
                contentContainerStyle={[estilos.contenido, { paddingTop: margen.top + 8, paddingBottom: margen.bottom + 180 }]}
                keyboardShouldPersistTaps="handled"
                onScroll={alDesplazar}
                scrollEventThrottle={16}
                testID="desplazamiento"
            >
                <View
                    onLayout={(e) => {
                        altoEncabezado.current = e.nativeEvent.layout.height;
                    }}
                >
                    <Encabezado punto={<PuntoConexion />} ubicacion={<IndicadorUbicacion />} empresa={empresa} />
                </View>
                <ExplicacionUbicacion />
                {estado.conexion === "sin_sesion" ? (
                    <Aviso tono="error" icono={KeyRound} titulo="Tu sesión venció" accion={<Boton alPresionar={alSesionVencida}>Volver a entrar</Boton>}>
                        Lo pendiente sigue guardado y se envía al volver a entrar.
                    </Aviso>
                ) : null}
                {estado.conexion === "sin_empresa" ? (
                    <Aviso
                        tono="error"
                        icono={Building2}
                        titulo="Ya no puedes trabajar en esta empresa"
                        accion={cambiarEmpresa ? <Boton variante="secundario" alPresionar={cambiarEmpresa}>Elegir empresa</Boton> : undefined}
                    >
                        Lo pendiente sigue guardado en el teléfono.
                    </Aviso>
                ) : null}
                {vista.tipo === "orden" ? (
                    <DetalleOrden id={vista.id} paso={vista.paso} />
                ) : vista.tipo === "pendientes" ? (
                    <Pendientes />
                ) : vista.tipo === "recibir" ? (
                    <PorRecibir />
                ) : vista.tipo === "traslado" ? (
                    <DetalleTraslado id={vista.id} />
                ) : vista.tipo === "yo" ? (
                    <Yo empresa={empresa} cambiarEmpresa={cambiarEmpresa} bajarPaquete={() => bajarPaquete(true)} />
                ) : vista.tipo === "bodega" ? (
                    <MiBodega />
                ) : vista.tipo === "agenda" ? (
                    <AgendaDia dia={vista.dia} />
                ) : vista.tipo === "semana" ? (
                    <AgendaSemana />
                ) : (
                    <ListaOrdenes carga={carga} bajarPaquete={() => bajarPaquete(true)} />
                )}
            </ScrollView>
            )}
            {/* Franja sólida bajo la barra de estado: el contenido que sube nunca se ve detrás de la hora. */}
            <View testID="franja-superior" pointerEvents="none" style={[estilos.franja, { height: margen.top, backgroundColor: campo.lienzo }]} />
            <BarraCompacta visible={desplazamiento.compacto} arriba={margen.top} />
            <AvisoIsla
                abajo={margen.bottom + (conIsla || conIslaPasos ? (desplazamiento.islaEncogida ? 72 : 100) : 24)}
                alEntrar={alSesionVencida}
                alElegirEmpresa={cambiarEmpresa}
            />
            {conIsla ? <IslaMenu encogida={desplazamiento.islaEncogida} abajo={margen.bottom} /> : null}
            {conIslaPasos && vista.tipo === "orden" && paso ? <IslaPasos id={vista.id} paso={paso} encogida={desplazamiento.islaEncogida} abajo={margen.bottom} /> : null}
            <AvisoDelMarco />
        </View>
        </ProveedorBorradores>
    );
}

/** El aviso que entra desde arriba (`notificacion` del contexto): el `Toast` de HeroUI Native. */
function AvisoDelMarco() {
    const { notificacion, cerrarNotificacion } = useCampo();
    return <AvisoEmergente aviso={notificacion} alCerrar={cerrarNotificacion} />;
}

/**
 * Tras entrar con la contraseña, el primer aviso ofrece la huella una sola vez: «no» queda guardado
 * al mostrarlo (si lo ignora o lo cierra, no vuelve a salir); «Usar huella» la enciende.
 */
function useOfertaHuella() {
    const { notificar } = useCampo();
    useEffect(() => {
        let vivo = true;
        void debeOfrecerHuella().then((ofrecer) => {
            if (!vivo || !ofrecer) {
                return;
            }
            activarHuella(false);
            notificar({
                tono: "info",
                texto: "¿Usar tu huella la próxima vez?",
                accion: {
                    texto: "Usar huella",
                    alPresionar: () => {
                        activarHuella(true);
                        notificar({ tono: "success", texto: "Listo: Iris te pedirá la huella al abrir. Puedes apagarlo en «Yo»." });
                    },
                },
            });
        });
        return () => {
            vivo = false;
        };
    }, [notificar]);
}

/**
 * Un rechazo nuevo del servidor entra desde arriba con «Revisar» (lleva a «Por enviar»): nunca pasa
 * en silencio. Solo cuando crece; los que ya estaban al abrir los muestra la píldora.
 */
function useAvisoDeRechazos() {
    const { estado, notificar, navegar } = useCampo();
    const rechazos = estado.rechazos.length + estado.subidas.rechazadas;
    const previos = useRef(rechazos);
    useEffect(() => {
        if (rechazos > previos.current) {
            const nuevos = rechazos - previos.current;
            notificar({
                tono: "error",
                texto: nuevos === 1 ? "El servidor rechazó una acción." : `El servidor rechazó ${nuevos} acciones.`,
                accion: { texto: "Revisar", alPresionar: () => navegar({ tipo: "pendientes" }) },
            });
        }
        previos.current = rechazos;
    }, [rechazos, notificar, navegar]);
}

/**
 * Punto de estado junto al título: con conexión y todo enviado es solo esto («En línea, todo
 * enviado»), sin texto ni caja. Cuando la barra está abierta, el punto acompaña y la barra explica.
 * El nombre accesible siempre dice el estado completo (nunca solo color).
 */
export function PuntoConexion() {
    const { estado } = useCampo();
    const { colores } = useTema();
    const r = resumenEnvio(estado);
    const tono = CONEXION_TEXTO[estado.conexion].tono;
    return (
        <View
            accessible
            accessibilityRole="text"
            accessibilityLabel={etiquetaPunto(estado.conexion, r)}
            accessibilityLiveRegion="polite"
            testID="punto-conexion"
            style={[estilos.halo, { backgroundColor: colores.fondoTono[tono] }]}
        >
            <View style={[estilos.punto, { backgroundColor: colores.tono[tono] }]} />
        </View>
    );
}

const TEXTO_RASTREO: Record<EstadoRastreo["tipo"], { tono: TonoEstado; icono: LucideIcon }> = {
    activa: { tono: "success", icono: LocateFixed },
    fuera_de_curso: { tono: "neutral", icono: LocateOff },
    explicar: { tono: "neutral", icono: LocateOff },
    sin_permiso: { tono: "warning", icono: LocateOff },
    sin_confirmar: { tono: "neutral", icono: LocateOff },
    vencida: { tono: "warning", icono: LocateOff },
    pausada_por_ti: { tono: "neutral", icono: LocateOff },
    error: { tono: "error", icono: LocateOff },
};

function textoRastreo(e: EstadoRastreo): string {
    switch (e.tipo) {
        case "activa": {
            const hasta = horaCorta(new Date(e.hasta).toISOString()) ?? "";
            return e.soloPrimerPlano ? `Ubicación activa con la app abierta · hasta las ${hasta}` : `Ubicación activa · orden en curso · hasta las ${hasta}`;
        }
        case "pausada_por_ti":
            return "Dejaste de compartir la ubicación · vuelve con tu próximo «En camino», «Llegué» o «Reanudar»";
        case "sin_confirmar":
            return "Ubicación en espera · se enciende al conectar con el servidor";
        case "vencida":
            return "Ubicación apagada: pasó el tope del día sin confirmar con el servidor · vuelve al sincronizar";
        case "fuera_de_curso":
            return "Ubicación apagada · sin orden en curso";
        case "explicar":
            return "Ubicación apagada";
        case "sin_permiso":
            return "Sin permiso de ubicación";
        case "error":
            return `GPS: ${e.mensaje}`;
    }
}

function IndicadorUbicacion() {
    const { estadoRastreo, posiciones, rastreo } = useCampo();
    const cola = useEstadoPosiciones(posiciones);
    const p = TEXTO_RASTREO[estadoRastreo.tipo];
    return (
        <View style={estilos.filaBarra}>
            <Insignia tono={p.tono} icono={p.icono} texto={textoRastreo(estadoRastreo)} testID="estado-ubicacion" />
            {estadoRastreo.tipo === "activa" && rastreo ? (
                <Boton variante="secundario" icono={LocateOff} alPresionar={() => void rastreo.dejarDeCompartir()}>
                    Dejar de compartir ubicación
                </Boton>
            ) : null}
            {cola && cola.pendientes > 0 ? <Texto suave>Puntos por enviar: {cola.pendientes}</Texto> : null}
            {cola?.ultimoError && !cola.detenida ? (
                <Boton variante="fantasma" icono={RefreshCw} alPresionar={() => posiciones?.reintentarAhora()}>
                    Reintentar
                </Boton>
            ) : null}
        </View>
    );
}

/** Explicación antes de que el sistema pida el permiso (la primera vez, con una orden en curso). */
function ExplicacionUbicacion() {
    const { estadoRastreo, rastreo } = useCampo();
    if (!rastreo || estadoRastreo.tipo !== "explicar" || estadoRastreo.ahoraNo) {
        if (estadoRastreo.tipo === "sin_permiso") {
            return (
                <Aviso tono="warning" icono={LocateOff} titulo="Sin permiso de ubicación">
                    Todo funciona igual; despacho no verá dónde vas. Puedes activarlo en los ajustes del teléfono (Ubicación → Iris Campo → «Permitir todo el tiempo»).
                </Aviso>
            );
        }
        return null;
    }
    return (
        <Aviso
            tono="info"
            icono={LocateFixed}
            titulo="¿Compartir tu ubicación?"
            testID="explicacion-ubicacion"
            accion={
                <>
                    <Boton alPresionar={() => void rastreo.aceptarExplicacion()}>Permitir ubicación</Boton>
                    <Boton variante="fantasma" alPresionar={() => rastreo.ahoraNo()}>
                        Ahora no
                    </Boton>
                </>
            }
        >
            Mientras tengas una orden en curso (en camino o en sitio) Iris envía un punto por minuto a despacho, también con la app en segundo plano. Con la orden pausada, cerrada o sin
            órdenes en curso no se registra nada. Sin la ubicación todo funciona igual.
        </Aviso>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    pantalla: { flex: 1 },
    halo: { width: 16, height: 16, borderRadius: 8, alignItems: "center", justifyContent: "center" },
    punto: { width: 8, height: 8, borderRadius: 4 },
    filaBarra: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 8 },
    contenido: { paddingHorizontal: 18, gap: 16 },
    franja: { position: "absolute", top: 0, left: 0, right: 0 },
});
