// Marco de la app (fase 4 de la PWA en nativo): encabezado «Iris Campo» con el punto de estado y la
// ubicación, la barra de estado discreta (solo crece cuando hace falta), la vista actual, el aviso
// flotante y la barra inferior Hoy · Por recibir · Pendientes · Yo. «Atrás» de Android vuelve a la
// vista anterior. Baja el paquete del día al abrir y al cambiar el día, con espera exponencial tras
// un error (como la PWA).
import { ErrorTransporte, type Conexion } from "@iris/sync";
import { CONEXION_TEXTO, etiquetaPunto, hoyEnGuatemala, horaCorta, partesEnvio, resumenEnvio, urlDeVista, type TonoEstado } from "@iris/sync/campo";
import { Building2, CloudUpload, KeyRound, LocateFixed, LocateOff, RefreshCw, TriangleAlert, Wifi, WifiOff, type LucideIcon } from "lucide-react-native";
import { useCallback, useEffect, useRef, useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Aviso, Boton, Insignia, Texto } from "../componentes";
import { useAtrasAndroid } from "../campo/atras";
import { useCampo, useEstadoPosiciones } from "../campo/contexto";
import type { EstadoRastreo } from "../campo/ubicacion";
import { CODIGO_CAMBIO_CONTRASENA } from "../sesion/sesion";
import { useTema } from "../tema/tema";
import { ListaOrdenes, type CargaPaquete } from "./ListaOrdenes";
import { MiBodega } from "./MiBodega";
import { NavegacionInferior } from "./NavegacionInferior";
import { DetalleOrden } from "./orden/DetalleOrden";
import { Pendientes } from "./Pendientes";
import { DetalleTraslado, PorRecibir } from "./PorRecibir";
import { Yo } from "./Yo";

const ICONOS_CONEXION: Record<Conexion, LucideIcon> = {
    en_linea: Wifi,
    desconocida: Wifi,
    sin_conexion: WifiOff,
    sin_sesion: KeyRound,
    sin_empresa: Building2,
};

const ESPERA_BASE_MS = 5_000;
const ESPERA_MAX_MS = 10 * 60_000;

export function Marco({ empresa, cambiarEmpresa, alSesionVencida }: { empresa: string | null; cambiarEmpresa?: () => void; alSesionVencida: () => void }) {
    const { motor, estado, sesion, vista, notificacion, atras } = useCampo();
    useAtrasAndroid(atras);
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
            <Encabezado empresa={empresa} nombre={sesion.nombre} />
            <BarraConexion />
            {/* Cada vista empieza arriba (como la PWA al navegar). */}
            <ScrollView key={urlDeVista(vista)} contentContainerStyle={estilos.contenido} keyboardShouldPersistTaps="handled">
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
                ) : (
                    <ListaOrdenes carga={carga} bajarPaquete={() => bajarPaquete(true)} />
                )}
            </ScrollView>
            {notificacion ? (
                <View style={[estilos.notificacion, { bottom: margen.bottom + 72 }]} pointerEvents="none">
                    <Aviso tono={notificacion.tono} titulo={notificacion.texto} testID="notificacion" />
                </View>
            ) : null}
            <NavegacionInferior />
        </View>
    );
}

/** «Iris Campo» con el punto de estado, quién está y en qué empresa, y la ubicación. */
function Encabezado({ nombre, empresa }: { nombre: string; empresa: string | null }) {
    const { colores } = useTema();
    return (
        <View style={[estilos.encabezado, { borderColor: colores.borde, backgroundColor: colores.vidrio }]}>
            <View style={estilos.fila}>
                <Texto negrita>Iris Campo</Texto>
                <PuntoConexion />
            </View>
            <Texto suave>
                {nombre}
                {empresa ? ` · ${empresa}` : ""}
            </Texto>
            <IndicadorUbicacion />
        </View>
    );
}

/**
 * Punto de estado junto al título: con conexión y todo enviado es solo esto («En línea, todo
 * enviado»). Cuando la barra está abierta, el punto acompaña y la barra explica.
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
            style={[estilos.punto, { backgroundColor: colores.tono[tono] }]}
        />
    );
}

/**
 * Barra que crece solo cuando hace falta: algo por enviar (sin señal o enviando), un rechazo, la
 * sesión vencida o la empresa no válida. Con todo enviado no se muestra (queda el punto).
 */
export function BarraConexion() {
    const { estado, motor, navegar } = useCampo();
    const { colores } = useTema();
    const r = resumenEnvio(estado);
    if (!r.abierta) {
        return null;
    }
    const c = CONEXION_TEXTO[estado.conexion];
    const { pendientes, rechazos } = partesEnvio(r);
    const hayAlgo = pendientes.length > 0 || rechazos !== null;
    const texto = [...pendientes, ...(rechazos ? [rechazos] : [])];
    return (
        <View accessibilityLabel="Estado de la sincronización" testID="barra-conexion" style={[estilos.barra, { borderColor: colores.borde, backgroundColor: colores.fondo }]}>
            <View style={estilos.filaBarra}>
                <Insignia tono={c.tono} icono={ICONOS_CONEXION[estado.conexion]} texto={r.texto} testID="estado-conexion" />
                <View style={estilos.flex} />
                {hayAlgo ? (
                    <Boton
                        variante={rechazos ? "secundario" : "fantasma"}
                        icono={rechazos ? TriangleAlert : CloudUpload}
                        etiqueta={`${texto.join(", ")}. Ver detalle`}
                        testID="pendientes-por-enviar"
                        alPresionar={() => navegar({ tipo: "pendientes" })}
                    >
                        {texto.join(" · ")}
                    </Boton>
                ) : null}
            </View>
            {estado.proximoIntento !== null && estado.conexion !== "sin_sesion" && hayAlgo ? (
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
    fila: { flexDirection: "row", alignItems: "center", gap: 8 },
    encabezado: { paddingHorizontal: 16, paddingVertical: 8, borderBottomWidth: 1, gap: 2 },
    punto: { width: 10, height: 10, borderRadius: 5 },
    barra: { paddingHorizontal: 16, paddingVertical: 6, borderBottomWidth: 1, gap: 4 },
    filaBarra: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 8 },
    contenido: { padding: 16, gap: 14, paddingBottom: 32 },
    notificacion: { position: "absolute", left: 16, right: 16 },
});
