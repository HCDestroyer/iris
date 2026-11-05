"use client";

import { ErrorTransporte } from "@iris/sync";
import { DESPLAZAMIENTO_INICIAL, desplazar } from "@iris/sync/campo";
import { avisar, Aviso, Boton, cerrarAviso, ProveedorAvisosEmergentes } from "@iris/ui";
import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import { hoyEnGuatemala } from "@/lib/campo/fechas";
import { irACambiarContrasena, pideCambioContrasena } from "@/lib/campo/sesion";
import { AgendaDia, AgendaSemana } from "./agenda";
import { ConversacionPantalla, ListaChat, NuevoChat } from "./chat";
import { useCampo } from "./contexto";
import { DetalleOrden } from "./detalle-orden";
import { BarraCompacta, EncabezadoCampo, FranjaSuperior } from "./encabezado";
import { DetalleTraslado, MiBodega, PorRecibir } from "./inventario";
import { ProveedorBorradores } from "./borradores";
import { IslaMenu, IslaPasos } from "./isla";
import { ListaOrdenes } from "./lista-ordenes";
import { Pendientes } from "./pendientes";
import { RUTA_ENTRAR } from "./app-campo";
import { useUbicacion } from "./ubicacion";
import { Yo } from "./yo";

/** Espera tras el primer error al bajar el paquete; se duplica en cada falla, hasta 10 min. */
const ESPERA_BASE_MS = 5_000;
const ESPERA_MAX_MS = 10 * 60_000;

export type CargaPaquete = { estado: "inactiva" | "cargando" | "lista" } | { estado: "error"; mensaje: string };

/**
 * Encabezado que se va con el contenido y vuelve compacto al subir, e isla que se encoge al bajar
 * (`desplazar` de `@iris/sync/campo`).
 */
function useDesplazamiento(encabezado: RefObject<HTMLElement | null>) {
    const [estado, setEstado] = useState(DESPLAZAMIENTO_INICIAL);
    useEffect(() => {
        let marco = 0;
        const medir = () => {
            marco = 0;
            const alto = encabezado.current?.offsetHeight ?? 0;
            setEstado((e) => desplazar(e, window.scrollY, alto));
        };
        const alDesplazar = () => {
            if (!marco) {
                marco = requestAnimationFrame(medir);
            }
        };
        window.addEventListener("scroll", alDesplazar, { passive: true });
        return () => {
            window.removeEventListener("scroll", alDesplazar);
            if (marco) {
                cancelAnimationFrame(marco);
            }
        };
    }, [encabezado]);
    return estado;
}

/**
 * El aviso que entra desde arriba (`notificacion` del contexto): el `Toast` de HeroUI
 * (`avisar` de `@iris/ui`), con su animación de entrada y salida, la acción («Deshacer»,
 * «Revisar») si la tiene y su «X». Uno a la vez: el nuevo reemplaza al anterior; el tiempo lo lleva
 * el contexto (5 s; 8 s con acción).
 */
function AvisoDelMarco() {
    const { notificacion, cerrarNotificacion } = useCampo();
    useEffect(() => {
        if (!notificacion) {
            return;
        }
        const id = avisar({
            tono: notificacion.tono,
            titulo: notificacion.texto,
            duracion: 0,
            ...(notificacion.accion
                ? {
                      accion: {
                          texto: notificacion.accion.texto,
                          alPresionar: () => {
                              notificacion.accion?.alPresionar();
                              cerrarNotificacion();
                          },
                      },
                  }
                : {}),
        });
        return () => cerrarAviso(id);
    }, [notificacion, cerrarNotificacion]);
    return <ProveedorAvisosEmergentes ubicacion="top" className="campo-avisos" />;
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

/** Marco de la app: encabezado, la vista actual, el aviso que entra desde arriba y la isla. */
export function Pantallas({ empresa, pedirEmpresa }: { empresa: number | null; pedirEmpresa: () => void }) {
    const { motor, estado, sesion, vista } = useCampo();
    const encabezado = useRef<HTMLElement | null>(null);
    const desplazamiento = useDesplazamiento(encabezado);
    useAvisoDeRechazos();
    // Los pasos de la orden llevan su propia isla («Anterior» · «Guardar» · «Siguiente»); la firma
    // y el cierre, su botón al pie.
    const paso = vista.tipo === "orden" ? vista.paso : undefined;
    // La conversación ocupa la pantalla (encabezado propio y compositor abajo): sin isla.
    const conIsla = paso === undefined && vista.tipo !== "conversacion";
    const conIslaPasos = vista.tipo === "orden" && paso !== undefined && paso !== "cierre";

    const [carga, setCarga] = useState<CargaPaquete>({ estado: "inactiva" });
    const { envio } = useUbicacion();
    const bajadoEnVisita = useRef(false);
    /** Una descarga a la vez. */
    const descargando = useRef(false);
    /** Día (Guatemala) del último intento automático o manual. */
    const ultimoIntento = useRef<string | null>(null);
    /** Tras un error: fallos seguidos y hasta cuándo no se reintenta solo (espera exponencial). */
    const espera = useRef({ fallos: 0, hasta: 0 });
    /** Sin sesión o sin empresa: no se reintenta solo (hay que entrar o elegir empresa). */
    const bloqueada = useRef(false);

    /**
     * Baja el paquete del día. `manual` (botones): muestra «Bajando…» y no mira la espera. Lo
     * automático con datos ya en el teléfono va en segundo plano (la pantalla no parpadea).
     */
    const bajarPaquete = useCallback(
        async (manual = true) => {
            if (descargando.current) {
                return;
            }
            descargando.current = true;
            const hoy = hoyEnGuatemala();
            ultimoIntento.current = hoy;
            if (manual || motor.obtenerEstado().fecha === null) {
                setCarga({ estado: "cargando" });
            }
            try {
                await motor.cargarPaquete(hoy);
                bajadoEnVisita.current = true;
                espera.current = { fallos: 0, hasta: 0 };
                bloqueada.current = false;
                setCarga({ estado: "lista" });
            } catch (e) {
                if (e instanceof ErrorTransporte && pideCambioContrasena(e.problema)) {
                    irACambiarContrasena();
                    return;
                }
                if (e instanceof ErrorTransporte && (e.esDeSesion || e.esDeEmpresa)) {
                    bloqueada.current = true;
                } else {
                    const fallos = espera.current.fallos + 1;
                    espera.current = { fallos, hasta: Date.now() + Math.min(ESPERA_BASE_MS * 2 ** (fallos - 1), ESPERA_MAX_MS) };
                }
                const sinSenal = e instanceof ErrorTransporte && e.tipo === "red";
                setCarga({
                    estado: "error",
                    mensaje: sinSenal ? "Sin señal: se usan las órdenes guardadas en el teléfono." : "No se pudieron bajar las órdenes del día.",
                });
            } finally {
                descargando.current = false;
            }
        },
        [motor],
    );

    // Conexión del navegador → motor; paquete al abrir (y al cambiar de empresa: otro motor), al
    // volver la señal o a la app (respetando la espera tras un error) y al cambiar de día; luego el
    // bucle pide cambios cada minuto.
    useEffect(() => {
        const revisar = () => {
            const enLinea = navigator.onLine;
            motor.establecerConexion(enLinea);
            const { fecha, conexion } = motor.obtenerEstado();
            if (!enLinea || bloqueada.current || conexion === "sin_sesion" || conexion === "sin_empresa") {
                return;
            }
            if ((bajadoEnVisita.current && fecha === hoyEnGuatemala()) || Date.now() < espera.current.hasta) {
                return;
            }
            void bajarPaquete(false);
        };
        const alVer = () => document.visibilityState === "visible" && revisar();
        const sinSenal = () => motor.establecerConexion(false);
        revisar();
        motor.iniciar();
        // Con la app abierta al cambiar el día en Guatemala: el paquete (y el turno) del día nuevo.
        // Solo una vez por día nuevo; los reintentos tras un error van por `online` y `visibilitychange`.
        const cadaMinuto = setInterval(() => {
            if (ultimoIntento.current !== null && ultimoIntento.current !== hoyEnGuatemala()) {
                espera.current = { fallos: 0, hasta: 0 };
                ultimoIntento.current = hoyEnGuatemala();
                revisar();
            }
        }, 60_000);
        window.addEventListener("online", revisar);
        window.addEventListener("offline", sinSenal);
        document.addEventListener("visibilitychange", alVer);
        return () => {
            clearInterval(cadaMinuto);
            window.removeEventListener("online", revisar);
            window.removeEventListener("offline", sinSenal);
            document.removeEventListener("visibilitychange", alVer);
            motor.detener();
        };
    }, [motor, bajarPaquete]);

    // Contraseña temporal (403 `cambio_contrasena_requerido` en el motor o en las ubicaciones).
    const pideContrasena = pideCambioContrasena(estado.ultimoError?.problema) || pideCambioContrasena(envio.ultimoError?.problema);
    useEffect(() => {
        if (pideContrasena) {
            irACambiarContrasena();
        }
    }, [pideContrasena]);

    const nombreEmpresa = sesion.empresas.find((e) => e.id === empresa)?.nombre;

    return (
        <ProveedorBorradores>
        <div className="campo-app relative mx-auto flex min-h-dvh w-full max-w-2xl flex-col pb-[calc(11rem+env(safe-area-inset-bottom))]">
            <FranjaSuperior />
            <EncabezadoCampo ref={encabezado} empresa={nombreEmpresa} pedirEmpresa={pedirEmpresa} />
            <BarraCompacta visible={desplazamiento.compacto} pedirEmpresa={pedirEmpresa} />
            {estado.conexion === "sin_sesion" ? (
                <div className="px-4 pt-3">
                    <Aviso
                        tono="error"
                        titulo="Tu sesión terminó"
                        accion={
                            <Boton variante="secundario" tamano="lg" onClick={() => window.location.assign(`${RUTA_ENTRAR}&motivo=expirada`)}>
                                Entrar de nuevo
                            </Boton>
                        }
                    >
                        Lo que hiciste sigue guardado en el teléfono y se enviará al volver a entrar.
                    </Aviso>
                </div>
            ) : null}
            {estado.conexion === "sin_empresa" ? (
                <div className="px-4 pt-3">
                    <Aviso
                        tono="error"
                        titulo="Ya no puedes trabajar en esta empresa"
                        accion={
                            <Boton variante="secundario" tamano="lg" onClick={pedirEmpresa}>
                                Elegir empresa
                            </Boton>
                        }
                    >
                        Lo pendiente sigue guardado en el teléfono.
                    </Aviso>
                </div>
            ) : null}
            <main className="flex-1 px-[1.125rem] pt-4">
                {vista.tipo === "orden" ? (
                    <DetalleOrden id={vista.id} paso={vista.paso} />
                ) : vista.tipo === "pendientes" ? (
                    <Pendientes />
                ) : vista.tipo === "recibir" ? (
                    <PorRecibir />
                ) : vista.tipo === "traslado" ? (
                    <DetalleTraslado id={vista.id} />
                ) : vista.tipo === "yo" ? (
                    <Yo nombreEmpresa={nombreEmpresa} pedirEmpresa={pedirEmpresa} bajarPaquete={bajarPaquete} />
                ) : vista.tipo === "bodega" ? (
                    <MiBodega />
                ) : vista.tipo === "agenda" ? (
                    <AgendaDia dia={vista.dia} />
                ) : vista.tipo === "semana" ? (
                    <AgendaSemana />
                ) : vista.tipo === "chat" ? (
                    <ListaChat />
                ) : vista.tipo === "chat_nuevo" ? (
                    <NuevoChat />
                ) : vista.tipo === "conversacion" ? (
                    <ConversacionPantalla id={vista.id} />
                ) : (
                    <ListaOrdenes carga={carga} bajarPaquete={bajarPaquete} />
                )}
            </main>
            <AvisoDelMarco />
            {conIsla ? <IslaMenu encogida={desplazamiento.islaEncogida} /> : null}
            {conIslaPasos && vista.tipo === "orden" && paso ? <IslaPasos id={vista.id} paso={paso} encogida={desplazamiento.islaEncogida} /> : null}
        </div>
        </ProveedorBorradores>
    );
}
