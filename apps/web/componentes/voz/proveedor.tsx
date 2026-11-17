"use client";

import { avisar, DialogoConfirmar } from "@iris/ui";
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
} from "@/lib/voz/api";
import { colgarSeguro, soltarSeguro, type Fin } from "@/lib/voz/asegurar";
import { escucharEventos, escucharFuente, fuenteAjena, publicarEvento } from "@/lib/voz/canal";
import { alCerrarPestana, alSalir, CONFIRMAR_MS, debeSonar, LIBRE, llamadaDe, ocupada, RESERVA_MS, siguiente, TIMBRE_MAX_MS, type EstadoVoz, type Etapa, type EventoVoz } from "@/lib/voz/maquina";
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
 * - **Nunca se cierra el audio local sin asegurar que el servidor termina la llamada** (`alSalir`
 *   y `lib/voz/asegurar.ts`): si `confirmar-medio` ya salió, se cuelga (pudo aceptarse); si solo
 *   estaba reservada, se suelta, y si soltar no sale, se comprueba y se cuelga.
 * - Varias pestañas: suena en todas; un candado del navegador (`navigator.locks`) deja contestar
 *   solo a una, y el servidor avisa a las demás. Sin candado no se manda `soltar` (podría liberar
 *   la reserva de otra pestaña): la reserva vence sola.
 * - Al cerrar la pestaña: `soltar` (aún sin aceptar) o `colgar` (pudo aceptarse), con `keepalive`.
 * - La llamada es de la empresa con la que se montó el proveedor: todo lo que la termina viaja con
 *   esa empresa, y cambiar de empresa o cerrar sesión con una llamada viva pregunta antes.
 */

/** Tiempo límite propio de `reclamar` y de `confirmar-medio` (peticiones que no pueden quedarse colgadas). */
const LIMITE_PETICION_MS = 8000;

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
    /** El servidor apagó «Recibir» en este navegador (pasó del máximo por persona): se ofrece volver a encender. */
    desplazado: boolean;
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
    /**
     * Antes de cambiar de empresa o cerrar sesión: con una llamada viva o sonando pregunta y, si la
     * persona sigue, la cuelga o suelta **antes** de irse. `false`: se queda.
     */
    antesDeSalir: () => Promise<boolean>;
};

const Contexto = createContext<ContextoVoz | null>(null);

/** La voz del panel, o `null` si esta persona no contesta llamadas. */
export function useVoz(): ContextoVoz | null {
    return use(Contexto);
}

/**
 * Candado entre pestañas del mismo navegador: `null` si otra pestaña ya está contestando esa llamada.
 * `seguro`: el candado es de verdad (`navigator.locks`); sin él no se sabe si el `reclamar` que ganó
 * fue el de esta pestaña.
 */
function tomarCandado(nombre: string): Promise<{ liberar: () => void; seguro: boolean } | null> {
    if (typeof navigator === "undefined" || !navigator.locks) return Promise.resolve({ liberar: () => undefined, seguro: false });
    return new Promise((resolver) => {
        navigator.locks
            .request(nombre, { ifAvailable: true }, (candado) => {
                if (!candado) {
                    resolver(null);
                    return undefined;
                }
                return new Promise<void>((soltar) => resolver({ liberar: soltar, seguro: true }));
            })
            .catch(() => resolver({ liberar: () => undefined, seguro: false }));
    });
}

const esperar = (ms: number) => new Promise<void>((resolver) => window.setTimeout(resolver, ms));
const accionEnCurso = (e: unknown) => e instanceof ErrorApi && e.codigo === "accion_en_curso";

/** Cada cuánto se relee si este navegador sigue recibiendo (el servidor guarda como mucho 10 por persona). */
const RELEER_RECIBIR_MS = 60_000;

type Intento = { conexion: Conexion | null; microfono: MediaStream | null; plazo: number; liberar: (() => void) | null; etapa: Etapa; reservaSegura: boolean };

export function ProveedorVoz({ children }: { children: ReactNode }) {
    const { yo } = useSesion();
    const yoId = yo.usuario.id;
    const empresaId = yo.empresaActiva?.id;
    // La empresa con la que se montó (el proveedor se remonta al cambiarla): la de sus llamadas.
    const [empresaMontada] = useState(empresaId);
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

    /** Lo último que se mandó a terminar en el servidor (para esperarlo antes de irse). */
    const salida = useRef<Promise<unknown>>(Promise.resolve());

    /** «Reintentar» del aviso de una llamada que no se pudo colgar (se asigna más abajo). */
    const reintentarAbierta = useRef<(id: string) => void>(() => undefined);

    /** Dice a la persona cómo acabó asegurar el final de una llamada que se dejó. */
    const trasAsegurar = useCallback((id: string, fin: Fin, hablaba: boolean) => {
        if (fin.tipo === "colgada") {
            const e = estadoRef.current;
            if (e.fase === "terminada" && e.llamada.id === id) {
                // Para la nota: la llamada como quedó en el servidor.
                estadoRef.current = { ...e, llamada: fin.llamada };
                setEstado(estadoRef.current);
            }
            if (!hablaba) avisar({ tono: "warning", titulo: "La llamada se conectó sin audio y se colgó", descripcion: "Escríbele al cliente para seguir por mensaje.", duracion: 12_000 });
        } else if (fin.tipo === "fallo") {
            const e = estadoRef.current;
            if (e.fase === "terminada" && e.llamada.id === id) setErrorColgar(fin.error);
            else {
                avisar({
                    tono: "error",
                    titulo: "No se pudo colgar una llamada que quedó abierta",
                    descripcion: "Para el cliente puede seguir en línea.",
                    duracion: 0,
                    accion: { texto: "Reintentar", alPresionar: () => reintentarAbierta.current(id) },
                });
            }
        }
    }, []);
    useEffect(() => {
        reintentarAbierta.current = (id) => void colgarSeguro(id, { empresa: empresaMontada, yoId }, true).then((f) => trasAsegurar(id, f, false));
    }, [empresaMontada, yoId, trasAsegurar]);

    const despachar = useCallback(
        (ev: EventoVoz): EstadoVoz => {
            const antes = estadoRef.current;
            const despues = siguiente(antes, ev, yoId);
            if (despues === antes) return antes;
            estadoRef.current = despues;
            setEstado(despues);
            if (ocupada(despues)) return despues;
            // Se deja la llamada: antes de cerrar el audio se decide qué asegurar en el servidor.
            const i = intento.current;
            const id = llamadaDe(antes)?.id;
            const accion = alSalir(antes, despues, i?.etapa ?? "en_curso", i?.reservaSegura ?? false);
            limpiar();
            if (accion && id) {
                const ctx = { empresa: empresaMontada, yoId };
                const hablaba = antes.fase === "en_curso";
                salida.current =
                    accion === "colgar"
                        ? // Con la llamada en curso es mía seguro; si solo pudo aceptarse, se comprueba antes.
                          colgarSeguro(id, ctx, !hablaba).then((fin) => trasAsegurar(id, fin, hablaba))
                        : soltarSeguro(id, despues.fase === "aviso" ? despues.motivo : "abandonada", ctx).then((fin) => trasAsegurar(id, fin, false));
            }
            return despues;
        },
        [yoId, limpiar, empresaMontada, trasAsegurar],
    );

    // --- «Recibir llamadas» -------------------------------------------------------------------
    const [recibir, setRecibir] = useState<EstadoRecibir>({ estado: "cargando" });
    const [intentoRecibir, setIntentoRecibir] = useState(0);
    const [microfono, setMicrofono] = useState<EstadoMicrofono | null>(null);
    const [cambiando, setCambiando] = useState(false);
    const [errorCambio, setErrorCambio] = useState<unknown>(null);
    const [desplazado, setDesplazado] = useState(false);
    const recibe = recibir.estado === "listo" && recibir.recibe;
    const recibeRef = useRef(recibe);
    const releerRecibir = useRef<() => void>(() => undefined);

    // Con «Recibir» encendido se relee de vez en cuando: si el servidor lo apagó aquí (se pasó de los
    // 10 navegadores por persona, o lleva mucho sin verse), se dice y se ofrece volver a encender.
    useEffect(() => {
        if (!recibe) return;
        let vivo = true;
        const leer = () => {
            if (document.visibilityState !== "visible") return;
            leerRecibir(idNavegador()).then(
                (r) => {
                    if (!vivo || r.recibe) return;
                    setRecibir({ estado: "listo", recibe: false });
                    setDesplazado(true);
                },
                // Una relectura fallida no cambia nada: el interruptor sigue como se leyó.
                () => undefined,
            );
        };
        releerRecibir.current = leer;
        const id = window.setInterval(leer, RELEER_RECIBIR_MS);
        document.addEventListener("visibilitychange", leer);
        return () => {
            vivo = false;
            releerRecibir.current = () => undefined;
            window.clearInterval(id);
            document.removeEventListener("visibilitychange", leer);
        };
    }, [recibe]);
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
            setDesplazado(false);
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
            setDesplazado(false);
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
        const ctx = { empresa: empresaMontada, yoId };
        void (async () => {
            const candado = await tomarCandado(`iris-llamada-${id}`);
            contestando.current = false;
            if (!candado) {
                despachar({ tipo: "otra_pestana" });
                return;
            }
            if (estadoRef.current.fase !== "sonando" || llamadaDe(estadoRef.current)?.id !== id) {
                candado.liberar();
                return;
            }
            const mio: Intento = { conexion: null, microfono: null, plazo: 0, liberar: candado.liberar, etapa: "reclamando", reservaSegura: candado.seguro };
            intento.current = mio;
            const vivo = () => intento.current === mio;
            const plazoDe = (ms: number) => {
                window.clearTimeout(mio.plazo);
                mio.plazo = window.setTimeout(() => vivo() && despachar({ tipo: "plazo_vencido" }), ms);
            };
            despachar({ tipo: "contestar" });
            // `reclamar` no puede quedarse colgado: pasado el límite se deja de esperar en pantalla.
            plazoDe(LIMITE_PETICION_MS);
            try {
                // 409 `accion_en_curso`: WhatsApp aún responde a otra acción de esta llamada; se reintenta.
                let r: Awaited<ReturnType<typeof reclamarLlamada>> | null = null;
                for (let vez = 0; r === null; vez++) {
                    try {
                        r = await reclamarLlamada(id);
                    } catch (e) {
                        if (!accionEnCurso(e) || vez >= 3 || !vivo()) throw e;
                        await esperar(1500);
                    }
                }
                if (!vivo()) {
                    // Respondió 200 cuando ya no se esperaba (se venció el límite, se cerró el aviso,
                    // cambió de empresa): la reserva quedó a mi nombre y hay que soltarla.
                    if (mio.reservaSegura) void soltarSeguro(id, "abandonada", ctx).then((fin) => trasAsegurar(id, fin, false));
                    return;
                }
                mio.etapa = "reservada";
                despachar({ tipo: "reclamada", llamada: r.llamada, venceEn: Date.parse(r.venceEn) });
                if (!vivo()) return;
                plazoDe(RESERVA_MS);
                const mic = await pedirMicrofono();
                if (!vivo()) {
                    soltarMicrofono(mic);
                    return;
                }
                mio.microfono = mic;
                setMicrofono("concedido");
                despachar({ tipo: "microfono_listo" });
                const conexion = await conectar(r.sdpOferta, r.ice.servidores ?? [], mic);
                if (!vivo()) {
                    conexion.cerrar();
                    return;
                }
                mio.conexion = conexion;
                // Desde que sale `contestar`, WhatsApp puede estar aceptando: la etapa cambia antes.
                mio.etapa = "respondida";
                const respondida = await contestarLlamada(id, conexion.sdpRespuesta, nuevaClaveIdempotencia());
                if (!vivo()) return;
                despachar({ tipo: "respuesta_enviada", llamada: respondida });
                if (!vivo()) return;
                plazoDe(CONFIRMAR_MS);
                // Si el medio se cae desde ya (también mientras se confirma), se deja la llamada.
                conexion.alCaer(() => {
                    if (!vivo()) return;
                    if (estadoRef.current.fase === "en_curso") despachar({ tipo: "medio_caido", ahora: Date.now() });
                    else despachar({ tipo: "fallo", motivo: "medio_fallido" });
                });
                await conexion.conectado;
                if (!vivo()) return;
                // El medio conectó: el plazo de 5 s deja de correr. Desde que sale `confirmar-medio`
                // el servidor puede aceptar la llamada; si esta pestaña la deja, hay que colgarla.
                mio.etapa = "confirmando";
                plazoDe(LIMITE_PETICION_MS);
                const enCurso = await confirmarMedio(id, nuevaClaveIdempotencia());
                // Respondió 200 pero la pestaña ya la había dejado (y cerrado el audio): `alSalir` ya
                // mandó colgar con la etapa «confirmando»; nada más que hacer aquí.
                if (!vivo()) return;
                window.clearTimeout(mio.plazo);
                mio.etapa = "en_curso";
                despachar({ tipo: "medio_confirmado", llamada: enCurso, ahora: Date.now() });
            } catch (error) {
                if (!vivo()) return;
                if (error instanceof ErrorMicrofono) {
                    setMicrofono(error.estado);
                    despachar({ tipo: "fallo", motivo: error.estado === "denegado" ? "microfono_denegado" : "sin_microfono" });
                } else if (error instanceof ErrorApi) {
                    if (error.codigo === "llamada_ya_contestada" && error.errores[0]?.rule === "misma_persona") despachar({ tipo: "otra_pestana" });
                    else despachar({ tipo: "fallo", motivo: motivoDeCodigo(error.codigo, error.estado), detalle: error.codigo === "llamada_ya_contestada" ? error.detalle : undefined });
                    // El servidor dice que este navegador ya no recibe: se relee para decirlo.
                    if (error.codigo === "no_recibe_llamadas") releerRecibir.current();
                } else {
                    despachar({ tipo: "fallo", motivo: "medio_fallido" });
                }
            }
        })();
    }, [despachar, empresaMontada, yoId, trasAsegurar]);

    const rechazar = useCallback(() => {
        const e = estadoRef.current;
        if (e.fase !== "sonando") return;
        despachar({ tipo: "rechazar" });
        rechazarLlamada(e.llamada.id, empresaMontada).catch((error: unknown) => {
            // Ya terminó o la tomó otro: para quien rechaza es lo mismo.
            if (error instanceof ErrorApi && (error.estado === 409 || error.estado === 404)) return;
            avisar({ titulo: "No se pudo rechazar la llamada", descripcion: mensajeDeError(error).titulo });
        });
    }, [despachar, empresaMontada]);

    /** «Colgar»: se cierra el audio y `despachar` asegura (con reintentos) que el servidor la termine. */
    const colgar = useCallback(() => {
        if (estadoRef.current.fase !== "en_curso") return;
        setErrorColgar(null);
        despachar({ tipo: "colgar", ahora: Date.now() });
    }, [despachar]);

    const reintentarColgar = useCallback(() => {
        const l = llamadaDe(estadoRef.current);
        if (!l) return;
        setErrorColgar(null);
        void colgarSeguro(l.id, { empresa: empresaMontada, yoId }, false).then((fin) => trasAsegurar(l.id, fin, true));
    }, [empresaMontada, yoId, trasAsegurar]);

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
    // Suena solo mientras llegan noticias: si el flujo se cae, calla (no se sabe si sigue sonando).
    const suena = debeSonar(estado) && feed !== "sin_conexion" && feed !== "detenido";
    useEffect(() => {
        if (!suena) return;
        const timbre = sonarTimbre();
        return () => timbre.detener();
    }, [suena]);
    // Tope propio: sin noticias de la llamada, deja de sonar aquí (como mucho 60 s).
    const sonando = estado.fase === "sonando" ? estado.llamada : null;
    const idSonando = sonando?.id;
    useEffect(() => {
        if (idSonando === undefined) return;
        const t = window.setTimeout(() => despachar({ tipo: "caducar" }), TIMBRE_MAX_MS);
        return () => window.clearTimeout(t);
    }, [idSonando, despachar]);

    // --- Cierre de la pestaña o del proveedor -------------------------------------------------
    useEffect(() => {
        const alIrse = () => {
            const i = intento.current;
            const que = alCerrarPestana(estadoRef.current, i?.etapa ?? "en_curso", i?.reservaSegura ?? false);
            if (que) avisarAlCerrar(que.id, que.accion, empresaMontada);
        };
        window.addEventListener("pagehide", alIrse);
        return () => {
            window.removeEventListener("pagehide", alIrse);
            // El proveedor se desmonta con una llamada viva (no debería: `antesDeSalir` pregunta
            // antes): se suelta o se cuelga igual, con la empresa de la llamada.
            alIrse();
            limpiar();
        };
    }, [limpiar, empresaMontada]);

    // --- Irse con una llamada viva o sonando: se pregunta antes -------------------------------
    const [pregunta, setPregunta] = useState<{ resolver: (seguir: boolean) => void; hablando: boolean; titulo: string } | null>(null);
    const antesDeSalir = useCallback(() => {
        const e = estadoRef.current;
        const l = e.fase === "sonando" || ocupada(e) ? llamadaDe(e) : null;
        if (!l) return Promise.resolve(true);
        return new Promise<boolean>((resolver) => setPregunta({ resolver, hablando: e.fase === "en_curso", titulo: l.titulo }));
    }, []);
    const dejarLlamada = useCallback(async () => {
        const e = estadoRef.current;
        const sonaba = e.fase === "sonando" ? e.llamada.id : null;
        despachar({ tipo: "abandonar" });
        // Se espera a que el servidor la termine (o a un tope): después ya no habrá sesión o será otra empresa.
        await Promise.race([salida.current.catch(() => undefined), esperar(6000)]);
        if (sonaba) await Promise.race([rechazarLlamada(sonaba, empresaMontada).catch(() => undefined), esperar(2000)]);
    }, [despachar, empresaMontada]);

    const reintentarRecibir = useCallback(() => {
        setRecibir({ estado: "cargando" });
        setIntentoRecibir((n) => n + 1);
    }, []);

    const valor = useMemo<ContextoVoz>(
        () => ({ estado, contestar, rechazar, colgar, silenciar, cerrar, errorColgar, reintentarColgar, recibir, desplazado, microfono, admite, cambiando, errorCambio, encender, apagar, reintentarRecibir, feed, timbre: suena, antesDeSalir }),
        [estado, contestar, rechazar, colgar, silenciar, cerrar, errorColgar, reintentarColgar, recibir, desplazado, microfono, admite, cambiando, errorCambio, encender, apagar, reintentarRecibir, feed, suena, antesDeSalir],
    );
    return (
        <Contexto value={valor}>
            {children}
            <DialogoConfirmar
                open={pregunta !== null}
                onOpenChange={(abierto) => {
                    if (abierto || !pregunta) return;
                    pregunta.resolver(false);
                    setPregunta(null);
                }}
                titulo={pregunta?.hablando ? `Tienes una llamada con ${pregunta.titulo}` : "Tienes una llamada entrando"}
                descripcion={pregunta?.hablando ? "Si sigues, la llamada se cuelga." : "Si sigues, deja de sonarte aquí y les sigue sonando a los demás."}
                textoConfirmar={pregunta?.hablando ? "Colgar y seguir" : "Seguir"}
                textoCancelar="Quedarme"
                textoOcupado="Terminando la llamada…"
                peligro={pregunta?.hablando ?? false}
                alConfirmar={async () => {
                    if (!pregunta) return;
                    await dejarLlamada();
                    pregunta.resolver(true);
                    setPregunta(null);
                }}
            />
        </Contexto>
    );
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
