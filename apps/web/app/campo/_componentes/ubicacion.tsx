"use client";

import type { ColaPosiciones, EstadoPosiciones } from "@iris/sync";
import { Aviso, Boton, cn, Dialogo, DialogoContenido, DialogoPie, InsigniaEstado, type Tono } from "@iris/ui";
import { Locate, LocateOff, MapPin, MapPinOff, Play, RefreshCw, Square, type LucideIcon } from "lucide-react";
import { createContext, Fragment, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { horaCorta } from "@/lib/campo/fechas";
import { obtenerPosicion, type Posicion } from "@/lib/campo/gps";
import { useEstadoPosiciones } from "@/lib/campo/motor";
import type { AvisoTurno, EstadoUbicacion, RastreadorUbicacion } from "@/lib/campo/ubicacion";
import { useCampo, type Notificacion } from "./contexto";

/** Aviso cada vez que el turno empieza solo («En camino» / «Llegué» o el inicio de un tramo de la agenda). */
export const AVISO_TURNO_INICIADO = "Turno iniciado: se envía tu ubicación cada minuto";
const AVISOS: Record<AvisoTurno, Notificacion> = {
    iniciado_por_agenda: { tono: "info", texto: AVISO_TURNO_INICIADO },
    terminado_por_agenda: { tono: "info", texto: "Turno terminado: terminó tu horario de la agenda. Ya no se envía tu ubicación." },
    terminado_por_ordenes: { tono: "info", texto: "Turno terminado: no te quedan órdenes abiertas hoy. Ya no se envía tu ubicación." },
};
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
    /**
     * Tras «En camino» / «Llegué»: fuera de turno lo empieza solo (si puede); en turno, lo mantiene
     * hasta la última orden aunque termine el tramo. Devuelve si empezó.
     */
    iniciarTurnoSolo: () => boolean;
    terminarTurno: () => void;
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

/**
 * Turno de la agenda junto al estado: «En turno hasta 17:00», «Fuera de turno · empieza 08:00».
 * Sin turno en la agenda hoy, nada.
 */
export function textoTurno(estado: EstadoUbicacion): string | null {
    if (estado.tramos === null) {
        return null;
    }
    if (estado.enTurno) {
        return estado.hasta !== null ? `En turno hasta ${horaCorta(ISO(estado.hasta))}` : "En turno fuera de horario";
    }
    return estado.empieza !== null ? `empieza ${horaCorta(ISO(estado.empieza))}` : null;
}

/** Texto completo del indicador: estado de la ubicación y, con agenda, el turno. */
export function textoIndicador(estado: EstadoUbicacion): string {
    const r = resumenUbicacion(estado);
    const turno = textoTurno(estado);
    if (turno === null) {
        return r.texto;
    }
    // «Fuera de turno · empieza 08:00»; en turno, «Ubicación activa · En turno hasta 17:00».
    return `${r.texto} · ${turno}`;
}

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
                await rastreador.pedirPermiso();
            } else {
                // «Ahora no» en esta visita: las acciones no vuelven a preguntar ni el turno se enciende solo.
                rastreador.declinarEnVisita();
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

    const turnoEmpezariaSolo = useCallback(() => rastreador.puedeEmpezarSolo(), [rastreador]);
    const iniciarTurnoSolo = useCallback(() => rastreador.alTrabajar(), [rastreador]);
    const terminarTurno = useCallback(() => rastreador.terminarTurno(), [rastreador]);

    const posicionParaAccion = useCallback(
        async (esperaMs?: number) => {
            if (!rastreador.explicacionAceptada() && !rastreador.declinadoEnVisita()) {
                await explicar();
            }
            const permiso = rastreador.obtenerEstado().permiso;
            if (rastreador.declinadoEnVisita() || permiso === "denegado" || permiso === "no_disponible") {
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
            posicionParaAccion,
            posicionSiPermitida,
            abrirPanel: () => setPanel(true),
        }),
        [estado, envio, iniciarTurno, turnoEmpezariaSolo, iniciarTurnoSolo, terminarTurno, posicionParaAccion, posicionSiPermitida],
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
                            Si tienes turno en la agenda, empieza y termina solo a esas horas (te avisamos). Si sigues trabajando al terminar,
                            sigue hasta tu última orden abierta.
                        </li>
                        <li>
                            Sin turno en la agenda, empieza cuando tocas «Iniciar turno», o solo al marcar «En camino» o «Llegué» (te
                            avisamos), y termina solo cuando ya no te quedan órdenes abiertas del día (cerraste o no se pudo la última, o te
                            la cancelaron o reasignaron).
                        </li>
                        <li>Siempre puedes terminarlo tú; como máximo, termina a medianoche.</li>
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
    const turno = textoTurno(estado);
    const Icono = r.icono;
    return (
        <Boton
            variante="fantasma"
            tamano="sm"
            data-testid="indicador-ubicacion"
            data-tono={r.tono}
            className={cn("h-auto min-h-8 gap-1.5 px-2 py-1 whitespace-normal text-left text-xs font-semibold", className)}
            onClick={abrirPanel}
            aria-label={`Ubicación: ${textoIndicador(estado)}${envio.pendientes > 0 ? `. Pendientes de enviar: ${envio.pendientes}` : ""}. Ver detalle`}
        >
            <Icono aria-hidden className={cn("size-4 shrink-0", TEXTO_TONO[r.tono])} />
            <span className={TEXTO_TONO[r.tono]}>{r.texto}</span>{" "}
            {turno !== null ? (
                <span className="font-normal text-foreground" data-testid="turno-ubicacion">
                    · {turno}
                </span>
            ) : null}{" "}
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
                        {textoTurno(estado) !== null ? (
                            <span className="text-muted-foreground">{textoTurno(estado)}</span>
                        ) : estado.enTurno ? (
                            <span className="text-muted-foreground">Turno iniciado hoy</span>
                        ) : null}
                    </div>
                    <p className="text-muted-foreground" data-testid="turno-agenda">
                        {estado.tramos === null ? (
                            "Hoy no tienes turno en la agenda: el turno lo inicias tú, o «En camino» y «Llegué»."
                        ) : (
                            <>
                                Tu turno de hoy en la agenda:{" "}
                                {estado.tramos.map((t, i) => (
                                    <Fragment key={t.inicio}>
                                        {i > 0 ? ", " : null}
                                        <span className="whitespace-nowrap tabular-nums">
                                            {horaCorta(ISO(t.inicio))}–{horaCorta(ISO(t.fin))}
                                        </span>
                                    </Fragment>
                                ))}
                                .
                            </>
                        )}
                    </p>

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
 * Turno automático (va dentro de `ProveedorCampo`): pasa al rastreador el turno de la agenda que
 * guarda el motor y las órdenes abiertas del día, y avisa lo que el rastreador hizo solo
 * (encender o apagar al empezar o terminar un tramo, o al quedarse sin órdenes abiertas).
 */
export function TurnoAutomatico({ rastreador }: { rastreador: RastreadorUbicacion }) {
    const { estado, notificar } = useCampo();
    const abiertas = estado.ordenes.filter((o) => !TERMINALES.has(o.estado) && !o.reasignada).length;
    const pendiente = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(
        () => {
            const quitar = rastreador.alAvisar((aviso) => {
                if (pendiente.current) {
                    clearTimeout(pendiente.current);
                }
                // Después del aviso de la acción que lo provocó («Orden cerrada…»), para que no lo tape.
                pendiente.current = setTimeout(() => notificar(AVISOS[aviso]), 300);
            });
            return () => {
                quitar();
                if (pendiente.current) {
                    clearTimeout(pendiente.current);
                }
            };
        },
        [rastreador, notificar],
    );

    useEffect(() => rastreador.establecerAgenda(estado.turno), [rastreador, estado.turno]);
    useEffect(() => {
        rastreador.alCambiarOrdenesAbiertas(abiertas);
    }, [rastreador, abiertas]);
    return null;
}
