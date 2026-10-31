"use client";

import { ErrorTransporte } from "@iris/sync";
import { Aviso, Boton, LogoIris, cn } from "@iris/ui";
import { useCallback, useEffect, useRef, useState } from "react";
import { hoyEnGuatemala } from "@/lib/campo/fechas";
import { irACambiarContrasena, pideCambioContrasena } from "@/lib/campo/sesion";
import { BarraConexion, PuntoConexion } from "./barra-conexion";
import { useCampo } from "./contexto";
import { DetalleOrden } from "./detalle-orden";
import { DetalleTraslado, MiBodega, PorRecibir } from "./inventario";
import { ListaOrdenes } from "./lista-ordenes";
import { NavegacionInferior } from "./navegacion-inferior";
import { Pendientes } from "./pendientes";
import { RUTA_ENTRAR } from "./app-campo";
import { IndicadorUbicacion, useUbicacion } from "./ubicacion";
import { Yo } from "./yo";

/** Espera tras el primer error al bajar el paquete; se duplica en cada falla, hasta 10 min. */
const ESPERA_BASE_MS = 5_000;
const ESPERA_MAX_MS = 10 * 60_000;

export type CargaPaquete = { estado: "inactiva" | "cargando" | "lista" } | { estado: "error"; mensaje: string };

/** Marco de la app: encabezado, barra de conexión, la vista actual y el aviso flotante. */
export function Pantallas({ empresa, pedirEmpresa }: { empresa: number | null; pedirEmpresa: () => void }) {
    const { motor, estado, sesion, vista, notificacion } = useCampo();
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
        <div className="relative mx-auto flex min-h-dvh w-full max-w-2xl flex-col pb-[calc(5.5rem+env(safe-area-inset-bottom))]">
            <header className="glass-material sticky top-0 z-30 border-b border-border pt-[env(safe-area-inset-top)]">
                <div className="flex items-center gap-3 px-4 py-1.5">
                    <LogoIris className="size-8" />
                    <div className="min-w-0 flex-1 leading-tight">
                        <p className="flex items-center gap-2 font-semibold tracking-[-0.015em]">
                            Iris Campo
                            <PuntoConexion />
                        </p>
                        <p className="truncate text-xs text-muted-foreground">
                            {sesion.nombre}
                            {nombreEmpresa ? ` · ${nombreEmpresa}` : ""}
                        </p>
                    </div>
                    <IndicadorUbicacion className="-mr-2 max-w-[55%] shrink-0" />
                </div>
                <BarraConexion />
            </header>
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
            <main className="flex-1 px-4 pt-4">
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
                ) : (
                    <ListaOrdenes carga={carga} bajarPaquete={bajarPaquete} />
                )}
            </main>
            <div
                aria-live="polite"
                className="pointer-events-none fixed inset-x-0 bottom-[calc(4.5rem+env(safe-area-inset-bottom))] z-40 mx-auto w-full max-w-2xl px-4 pb-2"
            >
                {notificacion ? (
                    <div
                        data-slot="notificacion"
                        data-tono={notificacion.tono}
                        className={cn(
                            "pointer-events-auto rounded-lg border px-4 py-3 text-sm font-medium shadow-lg",
                            notificacion.tono === "error" ? "border-error/40 bg-card text-error" : "border-border bg-card text-foreground",
                        )}
                    >
                        {notificacion.texto}
                    </div>
                ) : null}
            </div>
            <NavegacionInferior />
        </div>
    );
}
