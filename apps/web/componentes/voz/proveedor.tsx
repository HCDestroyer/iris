"use client";

import { avisar } from "@iris/ui";
import { createContext, use, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { abrirFlujo, leerCambios, leerEventoFlujo } from "@/lib/conversaciones/api";
import { esSesionCambiada, verificarSesion } from "@/lib/conversaciones/sesion";
import { iniciarFeed, type EstadoFeed } from "@/lib/conversaciones/tiempo-real";
import type { EventoBandeja } from "@/lib/conversaciones/tipos";
import { useSesion } from "@/lib/sesion";
import {
    apagarRecibir,
    avisarAlCerrar,
    colgarLlamada,
    confirmarMedio,
    contestarLlamada,
    encenderRecibir,
    leerLlamadaActiva,
    leerRecibir,
    rechazarLlamada,
    reclamarLlamada,
    soltarLlamada,
} from "@/lib/voz/api";
import { escucharEventos, escucharFuente, fuenteAjena, publicarEvento } from "@/lib/voz/canal";
import { alCerrarPestana, debeSoltar, debeSonar, LIBRE, llamadaDe, ocupada, siguiente, type EstadoVoz, type EventoVoz } from "@/lib/voz/maquina";
import { conectar, ErrorMicrofono, navegadorAdmiteLlamadas, pedirMicrofono, permisoMicrofono, soltarMicrofono, type Conexion, type EstadoMicrofono } from "@/lib/voz/medios";
import { idNavegador } from "@/lib/voz/navegador";
import { motivoDeCodigo } from "@/lib/voz/presentacion";
import { sonarTimbre } from "@/lib/voz/timbre";
import type { Llamada } from "@/lib/voz/tipos";

/*
 * La voz del panel (contratos/voz.md §3.3 y §3.4), a nivel de layout: así el aviso de una llamada
 * entrante aparece en cualquier pantalla y la barra de la llamada en curso sigue al navegar.
 *
 * - «Recibir llamadas» es de este usuario en este navegador; solo con él encendido suena aquí.
 * - Contestar: `reclamar` → micrófono → `RTCPeerConnection` con la oferta → `contestar` →
 *   medio conectado → `confirmar-medio`. La reserva dura 12 s y confirmar, 5 s: si se pasan, se
 *   suelta y se dice en llano.
 * - Varias pestañas: suena en todas; un candado del navegador (`navigator.locks`) deja contestar
 *   solo a una, y el servidor avisa a las demás.
 * - Al cerrar la pestaña: `soltar` (aún sin aceptar) o `colgar` (en curso), con `keepalive`.
 */

/** Plazo para `confirmar-medio` después de `contestar` (§3.3, paso 6). */
const PLAZO_CONFIRMAR_MS = 5000;

export type EstadoRecibir =
    | { estado: "cargando" }
    | { estado: "error"; error: unknown }
    | { estado: "listo"; recibe: boolean };

type ContextoVoz = {
    estado: EstadoVoz;
    contestar: () => void;
    rechazar: () => void;
    colgar: () => void;
    silenciar: (silenciada: boolean) => void;
    cerrar: () => void;
    /** No se pudo avisar al servidor que colgaste: se ofrece reintentar. */
    errorColgar: unknown;
    reintentarColgar: () => void;
    recibir: EstadoRecibir;
    /** Lo que se sabe del micrófono en este navegador (`null`: aún no se preguntó). */
    microfono: EstadoMicrofono | null;
    /** ¿Este navegador puede contestar? (micrófono y WebRTC, en contexto seguro) */
    admite: boolean;
    cambiando: boolean;
    errorCambio: unknown;
    encender: () => Promise<boolean>;
    apagar: () => Promise<void>;
    reintentarRecibir: () => void;
    /** El flujo en vivo por el que llegan las llamadas; sin él no suena nada. */
    feed: EstadoFeed | null;
    /** ¿Está sonando el timbre? (para pruebas y lectores de pantalla) */
    timbre: boolean;
};

const Contexto = createContext<ContextoVoz | null>(null);

/** La voz del panel, o `null` si esta persona no contesta llamadas. */
export function useVoz(): ContextoVoz | null {
    return use(Contexto);
}

/** Candado entre pestañas del mismo navegador: `null` si otra pestaña ya está contestando esa llamada. */
function tomarCandado(nombre: string): Promise<(() => void) | null> {
    if (typeof navigator === "undefined" || !navigator.locks) return Promise.resolve(() => undefined);
    return new Promise((resolver) => {
        navigator.locks
            .request(nombre, { ifAvailable: true }, (candado) => {
                if (!candado) {
                    resolver(null);
                    return undefined;
                }
                return new Promise<void>((soltar) => resolver(soltar));
            })
            .catch(() => resolver(() => undefined));
    });
}

type Intento = { conexion: Conexion | null; microfono: MediaStream | null; plazo: number; liberar: (() => void) | null };

export function ProveedorVoz({ children }: { children: ReactNode }) {
    const { yo } = useSesion();
    const yoId = yo.usuario.id;
    const empresaId = yo.empresaActiva?.id;
    const admite = useSyncExternalStore(
        () => () => undefined,
        navegadorAdmiteLlamadas,
        () => true,
    );

    // --- Estado de la llamada (la máquina) ---------------------------------------------------
    const estadoRef = useRef<EstadoVoz>(LIBRE);
    const [estado, setEstado] = useState<EstadoVoz>(LIBRE);
    const intento = useRef<Intento | null>(null);
    const [errorColgar, setErrorColgar] = useState<unknown>(null);

    const limpiar = useCallback(() => {
        const i = intento.current;
        intento.current = null;
        if (!i) return;
        window.clearTimeout(i.plazo);
        if (i.conexion) i.conexion.cerrar();
        else soltarMicrofono(i.microfono);
        i.liberar?.();
    }, []);

    const despachar = useCallback(
        (ev: EventoVoz): EstadoVoz => {
            const antes = estadoRef.current;
            const despues = siguiente(antes, ev, yoId);
            if (despues === antes) return antes;
            estadoRef.current = despues;
            setEstado(despues);
            const soltar = debeSoltar(antes, despues);
            if (soltar) void soltarLlamada(soltar, despues.fase === "aviso" ? despues.motivo : "error").catch(() => undefined);
            if (!ocupada(despues)) limpiar();
            return despues;
        },
        [yoId, limpiar],
    );

    // --- «Recibir llamadas» -------------------------------------------------------------------
    const [recibir, setRecibir] = useState<EstadoRecibir>({ estado: "cargando" });
    const [intentoRecibir, setIntentoRecibir] = useState(0);
    const [microfono, setMicrofono] = useState<EstadoMicrofono | null>(null);
    const [cambiando, setCambiando] = useState(false);
    const [errorCambio, setErrorCambio] = useState<unknown>(null);
    const recibe = recibir.estado === "listo" && recibir.recibe;
    const recibeRef = useRef(recibe);
    useEffect(() => {
        recibeRef.current = recibe;
    });

    useEffect(() => {
        const control = new AbortController();
        (async () => {
            try {
                const r = await leerRecibir(idNavegador(), control.signal);
                if (control.signal.aborted) return;
                let encendido = r.recibe;
                if (encendido) {
                    // Encendido pero el navegador ya no deja usar el micrófono: no puede contestar.
                    const permiso = admiteAhora() ? await permisoMicrofono() : "denied";
                    if (control.signal.aborted) return;
                    if (permiso === "denied") {
                        encendido = false;
                        setMicrofono(admiteAhora() ? "denegado" : "no_disponible");
                        void apagarRecibir(idNavegador()).catch(() => undefined);
                    } else if (permiso === "granted") {
                        setMicrofono("concedido");
                    }
                }
                setRecibir({ estado: "listo", recibe: encendido });
                if (!encendido) return;
                // Al abrir el panel: ¿hay una llamada mía viva o sonándome?
                const activa = await leerLlamadaActiva(control.signal).catch(() => null);
                if (control.signal.aborted || !activa) return;
                if (activa.estado === "sonando") despachar({ tipo: "entrante", llamada: activa });
                else if ((activa.estado === "en_curso" || activa.estado === "conectando") && activa.usuario?.id === yoId && activa.puede.colgar) avisarHuerfana(activa);
            } catch (error) {
                if (!control.signal.aborted) setRecibir({ estado: "error", error });
            }
        })();
        return () => control.abort();
    }, [intentoRecibir, despachar, yoId]);

    const encender = useCallback(async () => {
        setCambiando(true);
        setErrorCambio(null);
        try {
            // El permiso se pide aquí, con calma, y no cuando ya está sonando (§3.4).
            soltarMicrofono(await pedirMicrofono());
            setMicrofono("concedido");
            const r = await encenderRecibir(idNavegador());
            setRecibir({ estado: "listo", recibe: r.recibe });
            avisar({ titulo: "Este navegador ya recibe llamadas" });
            return true;
        } catch (e) {
            if (e instanceof ErrorMicrofono) setMicrofono(e.estado);
            else setErrorCambio(e);
            return false;
        } finally {
            setCambiando(false);
        }
    }, []);

    const apagar = useCallback(async () => {
        setCambiando(true);
        setErrorCambio(null);
        try {
            const r = await apagarRecibir(idNavegador());
            setRecibir({ estado: "listo", recibe: r.recibe });
            avisar({ titulo: "Este navegador ya no recibe llamadas" });
        } catch (e) {
            setErrorCambio(e);
        } finally {
            setCambiando(false);
        }
    }, []);

    // --- El flujo de eventos -------------------------------------------------------------------
    const ajena = useSyncExternalStore(escucharFuente, fuenteAjena, () => null);
    const [propia, setPropia] = useState<EstadoFeed | null>(null);
    const necesitaFlujo = recibe || ocupada(estado);
    const abrirPropio = necesitaFlujo && ajena === null;
    useEffect(() => {
        if (!abrirPropio) return;
        const cerrar = iniciarFeed<EventoBandeja>({
            abrirFlujo,
            leerCambios,
            verificarSesion: () => verificarSesion({ usuarioId: yoId, empresaId }),
            esSesionAjena: esSesionCambiada,
            leerEvento: leerEventoFlujo,
            alEvento: publicarEvento,
            alEstado: setPropia,
        });
        return () => {
            cerrar();
            setPropia(null);
        };
    }, [abrirPropio, yoId, empresaId]);
    const feed = ajena ?? (abrirPropio ? (propia ?? "conectando") : null);

    useEffect(
        () =>
            escucharEventos((evento) => {
                if (!evento.llamada) return;
                if (evento.tipo === "llamada_entrante") {
                    // Solo suena en un navegador que tiene «Recibir llamadas» encendido.
                    if (recibeRef.current) despachar({ tipo: "entrante", llamada: evento.llamada });
                } else if (evento.tipo === "llamada_actualizada" || evento.tipo === "llamada_terminada") {
                    despachar({ tipo: "actualizada", llamada: evento.llamada });
                }
            }),
        [despachar],
    );

    // --- Acciones --------------------------------------------------------------------------------
    const contestando = useRef(false);
    const contestar = useCallback(() => {
        const e = estadoRef.current;
        if (e.fase !== "sonando" || contestando.current) return;
        contestando.current = true;
        const id = e.llamada.id;
        void (async () => {
            const liberar = await tomarCandado(`iris-llamada-${id}`);
            contestando.current = false;
            if (!liberar) {
                despachar({ tipo: "otra_pestana" });
                return;
            }
            if (estadoRef.current.fase !== "sonando" || llamadaDe(estadoRef.current)?.id !== id) {
                liberar();
                return;
            }
            const mio: Intento = { conexion: null, microfono: null, plazo: 0, liberar };
            intento.current = mio;
            const vivo = () => intento.current === mio;
            const plazoHasta = (cuando: number) => {
                window.clearTimeout(mio.plazo);
                mio.plazo = window.setTimeout(() => vivo() && despachar({ tipo: "plazo_vencido" }), Math.max(0, cuando - Date.now()));
            };
            despachar({ tipo: "contestar" });
            try {
                const r = await reclamarLlamada(id);
                if (!vivo()) return;
                despachar({ tipo: "reclamada", llamada: r.llamada, venceEn: Date.parse(r.venceEn) });
                if (!vivo()) return;
                plazoHasta(Date.parse(r.venceEn));
                const mic = await pedirMicrofono();
                if (!vivo()) {
                    soltarMicrofono(mic);
                    return;
                }
                mio.microfono = mic;
                despachar({ tipo: "microfono_listo" });
                const conexion = await conectar(r.sdpOferta, r.ice.servidores ?? [], mic);
                if (!vivo()) {
                    conexion.cerrar();
                    return;
                }
                mio.conexion = conexion;
                const respondida = await contestarLlamada(id, conexion.sdpRespuesta, nuevaClaveIdempotencia());
                if (!vivo()) return;
                despachar({ tipo: "respuesta_enviada", llamada: respondida });
                if (!vivo()) return;
                plazoHasta(Date.now() + PLAZO_CONFIRMAR_MS);
                await conexion.conectado;
                if (!vivo()) return;
                const enCurso = await confirmarMedio(id, nuevaClaveIdempotencia());
                if (!vivo()) return;
                window.clearTimeout(mio.plazo);
                conexion.alCaer(() => {
                    if (!vivo()) return;
                    despachar({ tipo: "medio_caido", ahora: Date.now() });
                    // El servidor aún la cree viva (el flujo sigue abierto): se le avisa.
                    void colgarLlamada(id, nuevaClaveIdempotencia()).catch(() => undefined);
                });
                despachar({ tipo: "medio_confirmado", llamada: enCurso, ahora: Date.now() });
            } catch (error) {
                if (!vivo()) return;
                if (error instanceof ErrorMicrofono) {
                    setMicrofono(error.estado);
                    despachar({ tipo: "fallo", motivo: error.estado === "denegado" ? "microfono_denegado" : "sin_microfono" });
                } else if (error instanceof ErrorApi) {
                    if (error.codigo === "llamada_ya_contestada" && error.errores[0]?.rule === "misma_persona") despachar({ tipo: "otra_pestana" });
                    else despachar({ tipo: "fallo", motivo: motivoDeCodigo(error.codigo, error.estado), detalle: error.codigo === "llamada_ya_contestada" ? error.detalle : undefined });
                } else {
                    despachar({ tipo: "fallo", motivo: "medio_fallido" });
                }
            }
        })();
    }, [despachar]);

    const rechazar = useCallback(() => {
        const e = estadoRef.current;
        if (e.fase !== "sonando") return;
        despachar({ tipo: "rechazar" });
        rechazarLlamada(e.llamada.id).catch((error: unknown) => {
            // Ya terminó o la tomó otro: para quien rechaza es lo mismo.
            if (error instanceof ErrorApi && (error.estado === 409 || error.estado === 404)) return;
            avisar({ titulo: "No se pudo rechazar la llamada", descripcion: mensajeDeError(error).titulo });
        });
    }, [despachar]);

    const avisarColgar = useCallback(
        (id: string) => {
            setErrorColgar(null);
            colgarLlamada(id, nuevaClaveIdempotencia()).then(
                (l) => despachar({ tipo: "actualizada", llamada: l }),
                (error: unknown) => {
                    // Ya había terminado (colgó el cliente a la vez): no hay nada que reintentar.
                    if (error instanceof ErrorApi && (error.codigo === "llamada_terminada" || error.estado === 404)) return;
                    setErrorColgar(error);
                },
            );
        },
        [despachar],
    );

    const colgar = useCallback(() => {
        const e = estadoRef.current;
        if (e.fase !== "en_curso") return;
        despachar({ tipo: "colgar", ahora: Date.now() });
        avisarColgar(e.llamada.id);
    }, [despachar, avisarColgar]);

    const reintentarColgar = useCallback(() => {
        const l = llamadaDe(estadoRef.current);
        if (l) avisarColgar(l.id);
    }, [avisarColgar]);

    const silenciar = useCallback(
        (silenciada: boolean) => {
            intento.current?.conexion?.silenciar(silenciada);
            despachar({ tipo: "silenciar", silenciada });
        },
        [despachar],
    );

    const cerrar = useCallback(() => {
        setErrorColgar(null);
        despachar({ tipo: "cerrar" });
    }, [despachar]);

    // --- Timbre ----------------------------------------------------------------------------------
    const suena = debeSonar(estado);
    useEffect(() => {
        if (!suena) return;
        const timbre = sonarTimbre();
        return () => timbre.detener();
    }, [suena]);

    // --- Cierre de la pestaña, del proveedor o pérdida de red ---------------------------------
    useEffect(() => {
        const alIrse = () => {
            const que = alCerrarPestana(estadoRef.current);
            if (que) avisarAlCerrar(que.id, que.accion);
        };
        window.addEventListener("pagehide", alIrse);
        return () => {
            window.removeEventListener("pagehide", alIrse);
            // Cambio de empresa o cierre de sesión con una llamada viva: se suelta o se cuelga.
            alIrse();
            limpiar();
        };
    }, [limpiar]);

    const reintentarRecibir = useCallback(() => {
        setRecibir({ estado: "cargando" });
        setIntentoRecibir((n) => n + 1);
    }, []);

    const valor = useMemo<ContextoVoz>(
        () => ({ estado, contestar, rechazar, colgar, silenciar, cerrar, errorColgar, reintentarColgar, recibir, microfono, admite, cambiando, errorCambio, encender, apagar, reintentarRecibir, feed, timbre: suena }),
        [estado, contestar, rechazar, colgar, silenciar, cerrar, errorColgar, reintentarColgar, recibir, microfono, admite, cambiando, errorCambio, encender, apagar, reintentarRecibir, feed, suena],
    );
    return <Contexto value={valor}>{children}</Contexto>;
}

const admiteAhora = () => navegadorAdmiteLlamadas();

/**
 * Al abrir el panel había una llamada mía viva (se recargó la pestaña): el audio no se puede
 * retomar (el SDP ya se usó). Se dice y se ofrece colgarla; si no, el servidor la termina solo.
 */
function avisarHuerfana(llamada: Llamada) {
    avisar({
        titulo: `Tenías una llamada con ${llamada.titulo}`,
        descripcion: "El audio no se puede retomar en esta pestaña.",
        accion: {
            texto: "Colgar",
            alPresionar: () => {
                colgarLlamada(llamada.id, nuevaClaveIdempotencia()).then(
                    () => avisar({ titulo: "Llamada terminada" }),
                    (error: unknown) => {
                        if (error instanceof ErrorApi && error.codigo === "llamada_terminada") return;
                        avisar({ titulo: "No se pudo colgar", descripcion: mensajeDeError(error).titulo });
                    },
                );
            },
        },
    });
}
