"use client";

import type { ColaPosiciones, EstadoPosiciones } from "@iris/sync";
import { Aviso, Boton, cn, Dialogo, DialogoContenido, DialogoPie, InsigniaEstado, type Tono } from "@iris/ui";
import { Locate, LocateOff, MapPin, MapPinOff, Play, RefreshCw, Square, type LucideIcon } from "lucide-react";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { horaCorta } from "@/lib/campo/fechas";
import { obtenerPosicion, type Posicion } from "@/lib/campo/gps";
import { useEstadoPosiciones } from "@/lib/campo/motor";
import type { EstadoUbicacion, RastreadorUbicacion } from "@/lib/campo/ubicacion";
import { useCampo } from "./contexto";

/** Aviso cada vez que el turno empieza solo («En camino» / «Llegué»). */
export const AVISO_TURNO_INICIADO = "Turno iniciado: se envía tu ubicación cada minuto";
const AVISO_TURNO_TERMINADO = "Turno terminado: no te quedan órdenes abiertas hoy. Ya no se envía tu ubicación.";
const RUTA_VOLVER_A_ENTRAR = "/entrar?siguiente=%2Fcampo&motivo=expirada";

type ContextoUbicacion = {
    estado: EstadoUbicacion;
    envio: EstadoPosiciones;
    /** «Iniciar turno» a mano: primero la explicación si no se aceptó o el navegador aún no preguntó. */
    iniciarTurno: () => Promise<void>;
    /**
     * ¿«En camino» / «Llegué» empezarían el turno solo? Solo con la explicación aceptada, sin «Ahora
     * no» en esta visita, con permiso y fuera de turno.
     */
    turnoEmpezariaSolo: () => boolean;
    /** Empieza el turno solo (tras «En camino» / «Llegué»); devuelve si empezó. */
    iniciarTurnoSolo: () => boolean;
    terminarTurno: () => void;
    /** Órdenes abiertas del día: sin ninguna (tras tener alguna), el turno termina solo. */
    alCambiarOrdenesAbiertas: (abiertas: number) => boolean;
    /**
     * Posición para «En camino» / «Llegué»: la primera vez por usuario (o mientras no la acepte),
     * la explicación antes, aunque el permiso ya esté dado. Sin permiso o con «Ahora no», `null`
     * (la acción sigue sin GPS).
     */
    posicionParaAccion: (esperaMs?: number) => Promise<Posicion | null>;
    /** Posición solo si el permiso ya está dado (fotos y cierre, dentro de otros diálogos): nunca pregunta. */
    posicionSiPermitida: (esperaMs?: number) => Promise<Posicion | null>;
    abrirPanel: () => void;
};

const Contexto = createContext<ContextoUbicacion | null>(null);

export function useUbicacion(): ContextoUbicacion {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useUbicacion fuera de ProveedorUbicacion");
    }
    return c;
}

const ISO = (ms: number | null) => (ms === null ? undefined : new Date(ms).toISOString());

/** Lo que muestra el indicador: icono + texto + color. */
export function resumenUbicacion(estado: EstadoUbicacion): { tono: Tono; icono: LucideIcon; texto: string } {
    if (estado.permiso === "denegado") {
        return { tono: "error", icono: MapPinOff, texto: "Sin permiso" };
    }
    if (estado.permiso === "no_disponible") {
        return { tono: "neutral", icono: MapPinOff, texto: "Sin GPS" };
    }
    if (!estado.enTurno) {
        return { tono: "neutral", icono: MapPinOff, texto: "Fuera de turno" };
    }
    if (estado.permiso !== "concedido") {
        return { tono: "warning", icono: MapPinOff, texto: "Sin permiso" };
    }
    if (estado.gps === "sin_senal") {
        return { tono: "warning", icono: LocateOff, texto: "Sin señal GPS" };
    }
    if (estado.gps === "buscando") {
        return { tono: "neutral", icono: Locate, texto: "Buscando GPS…" };
    }
    return { tono: "success", icono: MapPin, texto: "Ubicación activa" };
}

const TEXTO_TONO: Record<Tono, string> = {
    neutral: "text-muted-foreground",
    info: "text-info",
    success: "text-success",
    warning: "text-warning",
    error: "text-error",
};

/**
 * Ubicación en vivo: rastreador (un punto por minuto en turno) sobre la cola de posiciones, el
 * bucle de envío y los diálogos (explicación previa al permiso y panel de estado).
 */
export function ProveedorUbicacion({
    cola,
    rastreador,
    children,
}: {
    cola: ColaPosiciones;
    /** Lo crea quien abre el motor (cerrar sesión termina su turno antes de esperar a la red). */
    rastreador: RastreadorUbicacion;
    children: ReactNode;
}) {
    const estado = useSyncExternalStore(rastreador.suscribir, rastreador.obtenerEstado, rastreador.obtenerEstado);
    const envio = useEstadoPosiciones(cola);
    const [explicando, setExplicando] = useState(false);
    const [panel, setPanel] = useState(false);
    const resolverExplicacion = useRef<(() => void) | null>(null);
    /** «Ahora no» en esta visita: las acciones no vuelven a preguntar. */
    const declinado = useRef(false);

    useEffect(() => rastreador.montar(), [rastreador]);

    useEffect(() => {
        cola.iniciar();
        const alVolver = () => cola.reintentarAhora();
        window.addEventListener("online", alVolver);
        return () => {
            window.removeEventListener("online", alVolver);
            cola.detener();
        };
    }, [cola]);

    const explicar = useCallback(
        () =>
            new Promise<void>((resolver) => {
                resolverExplicacion.current = resolver;
                setExplicando(true);
            }),
        [],
    );

    const cerrarExplicacion = useCallback(
        async (permitir: boolean) => {
            if (permitir) {
                rastreador.marcarExplicacionAceptada();
                declinado.current = false;
                await rastreador.pedirPermiso();
            } else {
                declinado.current = true;
            }
            setExplicando(false);
            resolverExplicacion.current?.();
            resolverExplicacion.current = null;
        },
        [rastreador],
    );

    const sinPreguntar = (e: EstadoUbicacion) => e.permiso === "preguntar" || e.permiso === "desconocido";

    const iniciarTurno = useCallback(async () => {
        rastreador.iniciarTurno();
        if (!rastreador.explicacionAceptada() || sinPreguntar(rastreador.obtenerEstado())) {
            await explicar();
        }
    }, [rastreador, explicar]);

    const turnoEmpezariaSolo = useCallback(() => {
        const e = rastreador.obtenerEstado();
        return !e.enTurno && e.permiso === "concedido" && rastreador.explicacionAceptada() && !declinado.current;
    }, [rastreador]);

    const iniciarTurnoSolo = useCallback(() => {
        if (!turnoEmpezariaSolo()) {
            return false;
        }
        rastreador.iniciarTurno();
        return true;
    }, [rastreador, turnoEmpezariaSolo]);

    const terminarTurno = useCallback(() => rastreador.terminarTurno(), [rastreador]);
    const alCambiarOrdenesAbiertas = useCallback((abiertas: number) => rastreador.alCambiarOrdenesAbiertas(abiertas), [rastreador]);

    const posicionParaAccion = useCallback(
        async (esperaMs?: number) => {
            if (!rastreador.explicacionAceptada() && !declinado.current) {
                await explicar();
            }
            const permiso = rastreador.obtenerEstado().permiso;
            if (declinado.current || permiso === "denegado" || permiso === "no_disponible") {
                return null;
            }
            return obtenerPosicion(esperaMs);
        },
        [rastreador, explicar],
    );

    const posicionSiPermitida = useCallback(
        async (esperaMs?: number) => (rastreador.obtenerEstado().permiso === "concedido" ? obtenerPosicion(esperaMs) : null),
        [rastreador],
    );

    const valor = useMemo<ContextoUbicacion>(
        () => ({
            estado,
            envio,
            iniciarTurno,
            turnoEmpezariaSolo,
            iniciarTurnoSolo,
            terminarTurno,
            alCambiarOrdenesAbiertas,
            posicionParaAccion,
            posicionSiPermitida,
            abrirPanel: () => setPanel(true),
        }),
        [estado, envio, iniciarTurno, turnoEmpezariaSolo, iniciarTurnoSolo, terminarTurno, alCambiarOrdenesAbiertas, posicionParaAccion, posicionSiPermitida],
    );

    return (
        <Contexto.Provider value={valor}>
            {children}
            <Dialogo open={explicando} onOpenChange={(abierto) => !abierto && void cerrarExplicacion(false)}>
                <DialogoContenido titulo="¿Compartir tu ubicación?" descripcion="Antes de que el teléfono te lo pregunte, esto es para qué la usa Iris.">
                    <ul className="grid list-disc gap-1.5 pl-5 text-sm">
                        <li>Registrar dónde marcas «En camino», «Llegué», las fotos y el cierre.</li>
                        <li>Mientras estás en turno y la app está abierta, enviar tu ubicación cada minuto a despacho.</li>
                        <li>
                            El turno empieza cuando tocas «Iniciar turno», o solo al marcar «En camino» o «Llegué» (te avisamos). Termina solo
                            cuando ya no te quedan órdenes abiertas del día (cerraste o no se pudo la última, o te la cancelaron o
                            reasignaron), cuando tú lo terminas o, como máximo, a medianoche.
                        </li>
                        <li>Fuera de turno no se registra nada. Si no la permites, la app funciona igual.</li>
                    </ul>
                    <DialogoPie className="mt-4">
                        <Boton variante="fantasma" onClick={() => void cerrarExplicacion(false)}>
                            Ahora no
                        </Boton>
                        <Boton onClick={() => void cerrarExplicacion(true)}>
                            <MapPin aria-hidden />
                            Permitir ubicación
                        </Boton>
                    </DialogoPie>
                </DialogoContenido>
            </Dialogo>
            <PanelUbicacion abierto={panel} alCambiar={setPanel} rastreador={rastreador} cola={cola} contexto={valor} />
        </Contexto.Provider>
    );
}

/** Indicador discreto (barra de conexión): estado de la ubicación y lo que falta enviar. */
export function IndicadorUbicacion({ className }: { className?: string }) {
    const { estado, envio, abrirPanel } = useUbicacion();
    const r = resumenUbicacion(estado);
    const Icono = r.icono;
    return (
        <Boton
            variante="fantasma"
            tamano="sm"
            data-testid="indicador-ubicacion"
            data-tono={r.tono}
            className={cn("h-auto min-h-8 gap-1.5 px-2 py-1 whitespace-normal text-left text-xs font-semibold", className)}
            onClick={abrirPanel}
            aria-label={`Ubicación: ${r.texto}${envio.pendientes > 0 ? `. Pendientes de enviar: ${envio.pendientes}` : ""}. Ver detalle`}
        >
            <Icono aria-hidden className={cn("size-4 shrink-0", TEXTO_TONO[r.tono])} />
            <span className={TEXTO_TONO[r.tono]}>{r.texto}</span>
            {envio.pendientes > 0 ? <span className="font-normal text-muted-foreground">· Pendientes de enviar: {envio.pendientes}</span> : null}
        </Boton>
    );
}

function PanelUbicacion({
    abierto,
    alCambiar,
    rastreador,
    cola,
    contexto,
}: {
    abierto: boolean;
    alCambiar: (abierto: boolean) => void;
    rastreador: RastreadorUbicacion;
    cola: ColaPosiciones;
    contexto: ContextoUbicacion;
}) {
    const { estado, envio } = contexto;
    const r = resumenUbicacion(estado);
    const [pidiendo, setPidiendo] = useState(false);
    const esperaServidor = envio.esperaHasta;

    return (
        <Dialogo open={abierto} onOpenChange={alCambiar}>
            <DialogoContenido
                titulo="Ubicación en vivo"
                descripcion="En turno y con la app abierta, Iris guarda tu ubicación cada minuto y la envía a despacho. Fuera de turno no se registra."
            >
                <div className="grid gap-4 text-sm">
                    <div className="flex flex-wrap items-center gap-2">
                        <InsigniaEstado tono={r.tono} icono={r.icono} texto={r.texto} data-testid="estado-ubicacion" />
                        {estado.enTurno ? <span className="text-muted-foreground">Turno iniciado hoy</span> : null}
                    </div>

                    {estado.permiso === "denegado" ? (
                        <Aviso tono="warning" titulo="El navegador tiene bloqueada la ubicación">
                            Actívala en los ajustes del sitio (el candado junto a la dirección) y vuelve a abrir la app. Mientras tanto, todo lo demás funciona.
                        </Aviso>
                    ) : null}
                    {estado.enTurno && (estado.permiso === "preguntar" || estado.permiso === "desconocido") ? (
                        <Boton
                            variante="secundario"
                            tamano="lg"
                            disabled={pidiendo}
                            onClick={async () => {
                                setPidiendo(true);
                                await rastreador.pedirPermiso();
                                setPidiendo(false);
                            }}
                        >
                            <MapPin aria-hidden />
                            {pidiendo ? "Esperando al teléfono…" : "Permitir ubicación"}
                        </Boton>
                    ) : null}

                    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                        <dt className="text-muted-foreground">Pendientes de enviar</dt>
                        <dd className="font-semibold tabular-nums" data-testid="posiciones-pendientes">
                            {envio.pendientes}
                        </dd>
                        <dt className="text-muted-foreground">Última ubicación</dt>
                        <dd className="tabular-nums">{horaCorta(ISO(estado.ultimaLectura ?? envio.ultimaPosicion?.ms ?? null)) ?? "—"}</dd>
                        <dt className="text-muted-foreground">Último envío</dt>
                        <dd className="tabular-nums">{horaCorta(ISO(envio.ultimoEnvio)) ?? "—"}</dd>
                    </dl>

                    {esperaServidor !== null ? (
                        <p className="text-muted-foreground">Se envía a las {horaCorta(ISO(esperaServidor))} (límite de envíos por minuto del servidor).</p>
                    ) : null}
                    {envio.detenida ? (
                        <Aviso
                            tono="error"
                            titulo="Tu sesión terminó: las ubicaciones no se envían"
                            accion={
                                <Boton asChild variante="secundario">
                                    <a href={RUTA_VOLVER_A_ENTRAR}>Vuelve a entrar</a>
                                </Boton>
                            }
                        >
                            Quedan guardadas en el teléfono ({envio.pendientes}) y se envían cuando vuelvas a entrar.
                        </Aviso>
                    ) : envio.pausada ? (
                        <Aviso
                            tono="warning"
                            titulo="Envío de ubicaciones en pausa"
                            accion={
                                <Boton variante="secundario" onClick={() => cola.reintentarAhora()}>
                                    <RefreshCw aria-hidden />
                                    Reintentar
                                </Boton>
                            }
                        >
                            El servidor no las aceptó ({envio.ultimoError?.mensaje ?? "error"}). Quedan guardadas en el teléfono; reintenta cuando se
                            resuelva o avisa a tu supervisor.
                        </Aviso>
                    ) : envio.ultimoError ? (
                        <Aviso
                            tono={envio.ultimoError.status === 422 ? "error" : "warning"}
                            titulo={envio.ultimoError.status === 422 ? "El servidor no aceptó un lote de ubicaciones" : "No se pudieron enviar las ubicaciones"}
                            accion={
                                envio.pendientes > 0 ? (
                                    <Boton variante="secundario" onClick={() => cola.reintentarAhora()}>
                                        <RefreshCw aria-hidden />
                                        Reintentar
                                    </Boton>
                                ) : undefined
                            }
                        >
                            {envio.ultimoError.status === 422
                                ? `Se descartaron ${envio.descartadas.invalida} ${envio.descartadas.invalida === 1 ? "punto" : "puntos"}.`
                                : "Quedan guardadas en el teléfono y se envían solas."}
                        </Aviso>
                    ) : null}

                    <DialogoPie>
                        {estado.enTurno ? (
                            <Boton variante="secundario" onClick={() => contexto.terminarTurno()}>
                                <Square aria-hidden />
                                Terminar turno
                            </Boton>
                        ) : (
                            <Boton
                                onClick={() => {
                                    alCambiar(false);
                                    void contexto.iniciarTurno();
                                }}
                            >
                                <Play aria-hidden />
                                Iniciar turno
                            </Boton>
                        )}
                    </DialogoPie>
                </div>
            </DialogoContenido>
        </Dialogo>
    );
}

const TERMINALES = new Set(["cerrada", "fallida", "cancelada"]);

/**
 * Termina el turno solo cuando el técnico se queda sin órdenes abiertas del día (cerró o falló la
 * última, o se la cancelaron o reasignaron) y lo avisa. Va dentro de `ProveedorCampo`.
 */
export function FinDeTurnoPorOrdenes() {
    const { estado, notificar } = useCampo();
    const { alCambiarOrdenesAbiertas } = useUbicacion();
    const abiertas = estado.ordenes.filter((o) => !TERMINALES.has(o.estado) && !o.reasignada).length;
    useEffect(() => {
        if (alCambiarOrdenesAbiertas(abiertas)) {
            // Después del aviso de la acción que la cerró («Orden cerrada…»), para que no lo tape.
            const t = setTimeout(() => notificar({ tono: "info", texto: AVISO_TURNO_TERMINADO }), 300);
            return () => clearTimeout(t);
        }
        return undefined;
    }, [abiertas, alCambiarOrdenesAbiertas, notificar]);
    return null;
}
