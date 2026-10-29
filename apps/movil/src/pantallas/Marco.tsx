// Marco de la app: encabezado, barra de conexión (siempre visible: conexión + «N pendientes por
// enviar»), ubicación, la vista actual y el aviso flotante. Baja el paquete del día al abrir y al
// cambiar el día, con espera exponencial tras un error (como la PWA).
import { ErrorTransporte, type Conexion } from "@iris/sync";
import { hoyEnGuatemala, horaCorta } from "@iris/sync/campo";
import {
    Building2,
    CloudCheck,
    CloudUpload,
    KeyRound,
    LocateFixed,
    LocateOff,
    LogOut,
    RefreshCw,
    TriangleAlert,
    Wifi,
    WifiOff,
    type LucideIcon,
} from "lucide-react-native";
import { useCallback, useEffect, useRef, useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Aviso, Boton, Dialogo, Insignia, Texto } from "../componentes";
import { useCampo, useEstadoPosiciones } from "../campo/contexto";
import type { EstadoRastreo } from "../campo/ubicacion";
import { CODIGO_CAMBIO_CONTRASENA } from "../sesion/sesion";
import { useTema } from "../tema/tema";
import type { TonoEstado } from "@iris/sync/campo";
import { ListaOrdenes, type CargaPaquete } from "./ListaOrdenes";
import { DetalleOrden } from "./orden/DetalleOrden";
import { Pendientes } from "./Pendientes";

const CONEXION: Record<Conexion, { tono: TonoEstado; icono: LucideIcon; texto: string }> = {
    en_linea: { tono: "success", icono: Wifi, texto: "En línea" },
    desconocida: { tono: "neutral", icono: Wifi, texto: "Conectando…" },
    sin_conexion: { tono: "warning", icono: WifiOff, texto: "Sin conexión" },
    sin_sesion: { tono: "error", icono: KeyRound, texto: "Sesión vencida" },
    sin_empresa: { tono: "error", icono: Building2, texto: "Empresa no válida" },
};

const ESPERA_BASE_MS = 5_000;
const ESPERA_MAX_MS = 10 * 60_000;

export function Marco({ empresa, cambiarEmpresa, alSesionVencida }: { empresa: string | null; cambiarEmpresa?: () => void; alSesionVencida: () => void }) {
    const { motor, estado, sesion, vista, notificacion } = useCampo();
    const { colores } = useTema();
    const margen = useSafeAreaInsets();
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
        <View style={[estilos.pantalla, { backgroundColor: colores.fondo, paddingTop: margen.top }]}>
            <Encabezado empresa={empresa} cambiarEmpresa={cambiarEmpresa} nombre={sesion.nombre} />
            <BarraConexion />
            <ScrollView contentContainerStyle={[estilos.contenido, { paddingBottom: margen.bottom + 80 }]} keyboardShouldPersistTaps="handled">
                <ExplicacionUbicacion />
                {vista.tipo === "orden" ? <DetalleOrden id={vista.id} /> : vista.tipo === "pendientes" ? <Pendientes /> : <ListaOrdenes carga={carga} bajarPaquete={() => bajarPaquete(true)} />}
            </ScrollView>
            {notificacion ? (
                <View style={[estilos.notificacion, { bottom: margen.bottom + 16 }]} pointerEvents="none">
                    <Aviso tono={notificacion.tono} titulo={notificacion.texto} testID="notificacion" />
                </View>
            ) : null}
            {estado.conexion === "sin_sesion" ? (
                <View style={[estilos.notificacion, { bottom: margen.bottom + 16 }]}>
                    <Aviso tono="error" icono={KeyRound} titulo="Tu sesión venció" accion={<Boton alPresionar={alSesionVencida}>Volver a entrar</Boton>}>
                        Lo pendiente sigue guardado y se envía al volver a entrar.
                    </Aviso>
                </View>
            ) : null}
        </View>
    );
}

function Encabezado({ nombre, empresa, cambiarEmpresa }: { nombre: string; empresa: string | null; cambiarEmpresa: (() => void) | undefined }) {
    const { colores } = useTema();
    const { cerrarSesion, estado } = useCampo();
    const [saliendo, setSaliendo] = useState(false);
    const pendientes = estado.cola.length + estado.rechazos.length + estado.esperandoAdjuntos.length + estado.subidas.pendientes;
    return (
        <View style={[estilos.encabezado, { borderColor: colores.borde, backgroundColor: colores.vidrio }]}>
            <View style={estilos.flex}>
                <Texto negrita>Iris Campo</Texto>
                <Texto suave>
                    {nombre}
                    {empresa ? ` · ${empresa}` : ""}
                </Texto>
            </View>
            {cambiarEmpresa ? (
                <Boton variante="fantasma" icono={Building2} etiqueta="Cambiar de empresa" alPresionar={cambiarEmpresa}>
                    Empresa
                </Boton>
            ) : null}
            <Boton variante="fantasma" icono={LogOut} etiqueta="Cerrar sesión" alPresionar={() => setSaliendo(true)}>
                Salir
            </Boton>
            <Dialogo
                abierto={saliendo}
                alCerrar={() => setSaliendo(false)}
                titulo="¿Cerrar sesión?"
                descripcion={pendientes > 0 ? `Quedan ${pendientes} cosas sin enviar en este teléfono.` : "Se borran las órdenes guardadas en este teléfono."}
            >
                {pendientes > 0 ? (
                    <>
                        <Aviso tono="warning" titulo="Si sales, lo pendiente no se envía hasta que vuelvas a entrar">
                            «Salir y conservarlos» guarda solo lo pendiente (la cola y las fotos) para enviarlo al volver.
                        </Aviso>
                        <Boton alPresionar={() => void cerrarSesion({ conservarPendientes: true })}>Salir y conservarlos</Boton>
                    </>
                ) : (
                    <Boton alPresionar={() => void cerrarSesion({ conservarPendientes: false })}>Cerrar sesión</Boton>
                )}
                <Boton variante="fantasma" alPresionar={() => setSaliendo(false)}>
                    Volver
                </Boton>
            </Dialogo>
        </View>
    );
}

/** Conexión (icono + texto + color), «N pendientes por enviar» con acceso a lo pendiente y la ubicación. */
export function BarraConexion() {
    const { estado, motor, navegar } = useCampo();
    const { colores } = useTema();
    const c = CONEXION[estado.conexion];
    const rechazos = estado.rechazos.length + estado.subidas.rechazadas;
    const porEnviar = estado.pendientes + estado.esperandoAdjuntos.length;
    const fotos = estado.subidas.pendientes - estado.subidas.rechazadas;
    const nada = porEnviar === 0 && fotos === 0 && rechazos === 0;
    const partes = [
        porEnviar > 0 ? `${porEnviar} ${porEnviar === 1 ? "pendiente" : "pendientes"} por enviar` : null,
        fotos > 0 ? `${fotos} ${fotos === 1 ? "archivo" : "archivos"} por subir` : null,
        rechazos > 0 ? `${rechazos} ${rechazos === 1 ? "rechazado" : "rechazados"}` : null,
    ].filter((p): p is string => p !== null);
    return (
        <View accessibilityLabel="Estado de la sincronización" style={[estilos.barra, { borderColor: colores.borde, backgroundColor: colores.fondo }]}>
            <View style={estilos.filaBarra}>
                <Insignia tono={c.tono} icono={c.icono} texto={estado.sincronizando && estado.conexion !== "sin_conexion" ? "Sincronizando…" : c.texto} testID="estado-conexion" />
                <View style={estilos.flex} />
                <Boton
                    variante={rechazos > 0 ? "secundario" : "fantasma"}
                    icono={nada ? CloudCheck : rechazos > 0 ? TriangleAlert : CloudUpload}
                    etiqueta={nada ? "Todo enviado. Ver detalle" : `${partes.join(", ")}. Ver detalle`}
                    testID="pendientes-por-enviar"
                    alPresionar={() => navegar({ tipo: "pendientes" })}
                >
                    {nada ? "Todo enviado" : partes.join(" · ")}
                </Boton>
            </View>
            <IndicadorUbicacion />
            {estado.proximoIntento !== null && estado.conexion !== "sin_sesion" && !nada ? (
                <View style={estilos.filaBarra}>
                    <Texto suave>Reintento a las {horaCorta(new Date(estado.proximoIntento).toISOString()) ?? ""}</Texto>
                    <Boton variante="fantasma" icono={RefreshCw} alPresionar={() => motor.reintentarAhora()}>
                        Reintentar ahora
                    </Boton>
                </View>
            ) : null}
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
    encabezado: { flexDirection: "row", alignItems: "center", gap: 4, paddingHorizontal: 16, paddingVertical: 8, borderBottomWidth: 1 },
    barra: { paddingHorizontal: 16, paddingVertical: 6, borderBottomWidth: 1, gap: 4 },
    filaBarra: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 8 },
    contenido: { padding: 16, gap: 14 },
    notificacion: { position: "absolute", left: 16, right: 16 },
});
