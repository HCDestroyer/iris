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
    leerRespuestaSdp,
    llamarContacto,
    rechazarLlamada,
    reclamarLlamada,
} from "@/lib/voz/api";
import { colgarSeguro, soltarSeguro, type Fin } from "@/lib/voz/asegurar";
import { escucharEventos, escucharFuente, fuenteAjena, publicarEvento } from "@/lib/voz/canal";
import {
    alCerrarPestana,
    alSalir,
    CONFIRMAR_MS,
    debeSonar,
    esFinal,
    LIBRE,
    llamadaDe,
    MARCANDO_MAX_MS,
    MEDIO_SALIENTE_MS,
    ocupada,
    RESERVA_MS,
    siguiente,
    TIMBRE_MAX_MS,
    type EstadoVoz,
    type Etapa,
    type EventoVoz,
} from "@/lib/voz/maquina";
import { conectar, ErrorMicrofono, navegadorAdmiteLlamadas, ofrecer, pedirMicrofono, permisoMicrofono, soltarMicrofono, type EstadoMicrofono, type Medio, type Oferta } from "@/lib/voz/medios";
import { motivoDeCodigo, SIN_CONTESTAR_RIESGO } from "@/lib/voz/presentacion";
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
 * - Salientes (E1b): micrófono → oferta del navegador → `llamar` → «Llamando…» → al aviso
 *   `llamada_respuesta` se lee y aplica la respuesta SDP → `en_curso` cuando el cliente contesta.
 *   Desde que sale `llamar` la llamada puede existir en el servidor: dejarla es colgarla, también si
 *   la respuesta se perdió. El flujo de eventos sigue abierto mientras dure (es su latido).
 * - La llamada es de la empresa con la que se montó el proveedor: todo lo que la termina viaja con
 *   esa empresa, y cambiar de empresa o cerrar sesión con una llamada viva pregunta antes.
 */

/** Cuánto se espera, al irse, a que el servidor termine la llamada antes de decir que no se pudo. */
const TOPE_SALIDA_MS = 6000;

/** Tiempo límite propio de `reclamar` y de `confirmar-medio` (peticiones que no pueden quedarse colgadas). */
const LIMITE_PETICION_MS = 8000;

/** Tiempo límite de `llamar` (el servidor habla con WhatsApp antes de responder). */
const LIMITE_LLAMAR_MS = 20_000;

/** A quién se llama y con qué (lo sabe la pantalla que ofrece «Llamar»). */
export type DestinoLlamada = { id: number; titulo: string };
export type OpcionesLlamar = {
    /** «Devolver la llamada»: la devolución pendiente que se cierra si el cliente contesta. */
    devolucionId?: number | undefined;
    /** Salientes seguidas sin contestar (del permiso): con tres se confirma antes de llamar. */
    sinContestar?: number | undefined;
};

export type EstadoRecibir =
    | { estado: "cargando" }
    | { estado: "error"; error: unknown }
    | { estado: "listo"; recibe: boolean };

type ContextoVoz = {
    estado: EstadoVoz;
    /** ¿Esta persona contesta llamadas? (`llamadas.contestar`); si no, solo llama. */
    contesta: boolean;
    /** Llama al contacto de una conversación (E1b). Con tres sin contestar, confirma antes. */
    llamar: (destino: DestinoLlamada, opciones?: OpcionesLlamar) => void;
    contestar: () => void;
    rechazar: () => void;
    colgar: () => void;
    silenciar: (silenciada: boolean) => void;
    cerrar: () => void;
    /** No se pudo avisar al servidor que colgaste: se ofrece reintentar. */
    errorColgar: unknown;
    reintentarColgar: () => void;
    recibir: EstadoRecibir;
    /**
     * El servidor apagó «Recibir» en este navegador: `tope` (más de 10 navegadores), `inactivo` (30
     * días sin abrirlo) o `desconocido`. Se dice y se ofrece volver a encender.
     */
    desplazado: MotivoApagado | null;
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
    /** Quien supervisa: rechaza para todos la llamada que suena (deja de sonarles y queda rechazada). */
    rechazarParaTodos: () => Promise<void>;
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

export type MotivoApagado = "tope" | "inactivo" | "desconocido";

type Intento = {
    conexion: Medio | null;
    microfono: MediaStream | null;
    plazo: number;
    liberar: (() => void) | null;
    etapa: Etapa;
    reservaSegura: boolean;
    /** Saliente: la oferta de este navegador, a la espera de la respuesta SDP. */
    oferta?: Oferta;
    /** Saliente: ya se está trayendo (o se aplicó) la respuesta SDP. */
    respuesta?: boolean;
    /** Saliente: el audio conectó. */
    conecto?: boolean;
    /** Saliente con `llamar` en vuelo: cómo acaba de asegurarse si se deja antes de que responda. */
    enVuelo?: Promise<Fin | null>;
};

/** Algo que se mandó a terminar en el servidor y aún no se sabe que terminó (o falló). */
type Salida = { promesa: Promise<Fin | null>; hacer: () => Promise<Fin | null>; fallo: boolean };

/** ¿El error deja dudas de si el servidor creó la llamada? (red caída, tiempo agotado, 5xx que no es de WhatsApp) */
const incierto = (e: unknown) => !(e instanceof ErrorApi) || e.estado === 0 || (e.estado >= 500 && e.codigo !== "meta_rechazo");

export function ProveedorVoz({ children, contesta = true }: { children: ReactNode; contesta?: boolean }) {
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

    /**
     * Todo lo que se mandó a terminar en el servidor y no ha terminado bien: se espera antes de irse.
     * Una salida nueva no descarta una anterior en vuelo; una que falló se queda hasta que se logra.
     */
    const salidas = useRef(new Set<Salida>());
    const seguir = useCallback((s: Salida, promesa: Promise<Fin | null>) => {
        s.promesa = promesa;
        s.fallo = false;
        void promesa.then(
            (fin) => {
                if (s.promesa !== promesa) return;
                if (fin?.tipo === "fallo") s.fallo = true;
                else salidas.current.delete(s);
            },
            () => {
                if (s.promesa === promesa) s.fallo = true;
            },
        );
    }, []);
    const registrar = useCallback(
        (hacer: () => Promise<Fin | null>, primera?: Promise<Fin | null>) => {
            const s: Salida = { promesa: Promise.resolve(null), hacer, fallo: false };
            salidas.current.add(s);
            seguir(s, primera ?? hacer());
            return s.promesa;
        },
        [seguir],
    );
    /** Vuelve a intentar lo que falló; lo que sigue en vuelo se deja correr. */
    const reintentarSalidas = useCallback(() => {
        for (const s of [...salidas.current]) if (s.fallo) seguir(s, s.hacer());
    }, [seguir]);

    /** «Reintentar» del aviso de una llamada que no se pudo colgar. */
    const reintentarAbierta = useRef<() => void>(() => undefined);
    useEffect(() => {
        reintentarAbierta.current = reintentarSalidas;
    }, [reintentarSalidas]);

    /** Dice a la persona cómo acabó asegurar el final de una llamada que se dejó. `hablaba`: no hay nada raro que contar si se colgó. */
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
                    accion: { texto: "Reintentar", alPresionar: () => reintentarAbierta.current() },
                });
            }
        }
    }, []);

    /**
     * `llamar` salió y no se sabe si el servidor creó la llamada (se perdió la respuesta): se busca
     * mi saliente viva y se cuelga. Se mira dos veces, por si el servidor aún la estaba creando.
     */
    const colgarPerdida = useCallback(async (): Promise<Fin | null> => {
        const ctx = { empresa: empresaMontada, yoId };
        let ultimo: unknown = null;
        let leidas = 0;
        for (const espera of [0, 4000, 4000, 8000]) {
            if (espera) await esperar(espera);
            try {
                const activa = await leerLlamadaActiva(undefined, empresaMontada);
                leidas += 1;
                if (activa && activa.direccion === "saliente" && activa.usuario?.id === yoId && !esFinal(activa)) {
                    const fin = await colgarSeguro(activa.id, ctx, true);
                    trasAsegurar(activa.id, fin, true);
                    return fin;
                }
                if (leidas >= 2) return { tipo: "nada" };
            } catch (e) {
                ultimo = e;
            }
        }
        const fin: Fin = { tipo: "fallo", error: ultimo };
        trasAsegurar("", fin, true);
        return fin;
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
                // Una saliente que se cuelga marcando no «se conectó sin audio»: no hay nada que contar.
                const callada = hablaba || antes.fase === "llamando";
                const motivo = despues.fase === "aviso" ? despues.motivo : "abandonada";
                registrar(
                    accion === "colgar"
                        ? // Con la llamada en curso es mía seguro; si solo pudo aceptarse, se comprueba antes.
                          () => colgarSeguro(id, ctx, !hablaba).then((fin) => (trasAsegurar(id, fin, callada), fin))
                        : () => soltarSeguro(id, motivo, ctx).then((fin) => (trasAsegurar(id, fin, false), fin)),
                );
            } else if (accion === "colgar" && i?.enVuelo) {
                // Saliente con `llamar` aún en vuelo: no hay id. Cuando responda (o se pierda) se
                // cuelga; aquí queda anotado para esperarlo antes de irse.
                registrar(colgarPerdida, i.enVuelo);
            }
            return despues;
        },
        [yoId, limpiar, empresaMontada, trasAsegurar, registrar, colgarPerdida],
    );

    // --- «Recibir llamadas» -------------------------------------------------------------------
    const [recibir, setRecibir] = useState<EstadoRecibir>({ estado: "cargando" });
    const [intentoRecibir, setIntentoRecibir] = useState(0);
    const [microfono, setMicrofono] = useState<EstadoMicrofono | null>(null);
    const [cambiando, setCambiando] = useState(false);
    const [errorCambio, setErrorCambio] = useState<unknown>(null);
    const [desplazado, setDesplazado] = useState<MotivoApagado | null>(null);
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
            leerRecibir().then(
                (r) => {
                    if (!vivo || r.recibe) return;
                    setRecibir({ estado: "listo", recibe: false });
                    setDesplazado(r.motivo ?? "desconocido");
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
                if (!contesta) {
                    // Solo llama: «Recibir llamadas» no es para esta persona.
                    setRecibir({ estado: "listo", recibe: false });
                    return;
                }
                const r = await leerRecibir(control.signal);
                if (control.signal.aborted) return;
                let encendido = r.recibe;
                // Lo apagó el servidor desde la última vez: se dice por qué.
                if (!r.recibe && r.motivo) setDesplazado(r.motivo);
                if (encendido) {
                    // Encendido pero el navegador ya no deja usar el micrófono: no puede contestar.
                    const permiso = admiteAhora() ? await permisoMicrofono() : "denied";
                    if (control.signal.aborted) return;
                    if (permiso === "denied") {
                        encendido = false;
                        setMicrofono(admiteAhora() ? "denegado" : "no_disponible");
                        void apagarRecibir().catch(() => undefined);
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
    }, [intentoRecibir, despachar, yoId, contesta]);

    const encender = useCallback(async () => {
        setCambiando(true);
        setErrorCambio(null);
        try {
            // El permiso se pide aquí, con calma, y no cuando ya está sonando (§3.4).
            soltarMicrofono(await pedirMicrofono());
            setMicrofono("concedido");
            const r = await encenderRecibir();
            setRecibir({ estado: "listo", recibe: r.recibe });
            setDesplazado(null);
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
            const r = await apagarRecibir();
            setRecibir({ estado: "listo", recibe: r.recibe });
            setDesplazado(null);
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

    /**
     * Saliente: trae la respuesta SDP de WhatsApp y la aplica a la oferta de este navegador. Se pide
     * al aviso `llamada_respuesta` (y, por si ese aviso se perdió, cuando la llamada pasa a `en_curso`).
     * 409 `sin_respuesta`: aún no llegó; se reintenta.
     */
    const traerRespuesta = useCallback(
        (mio: Intento, id: string) => {
            if (mio.respuesta || !mio.oferta) return;
            mio.respuesta = true;
            const oferta = mio.oferta;
            const vivo = () => intento.current === mio;
            void (async () => {
                let error: unknown = null;
                for (let vez = 0; vez < 8 && vivo(); vez++) {
                    try {
                        const r = await leerRespuestaSdp(id, empresaMontada);
                        if (!vivo()) return;
                        await oferta.aplicarRespuesta(r.sdpRespuesta);
                        return;
                    } catch (e) {
                        error = e;
                        const reintentable = e instanceof ErrorApi && (e.codigo === "sin_respuesta" || e.estado === 0 || e.estado >= 500);
                        if (!reintentable) break;
                        await esperar(700);
                    }
                }
                if (!vivo()) return;
                // Sin la respuesta no hay audio: no se deja la llamada viva sin él.
                const otroNavegador = error instanceof ErrorApi && (error.codigo === "otro_navegador" || error.errores[0]?.rule === "otro_navegador");
                if (estadoRef.current.fase === "en_curso") despachar({ tipo: "medio_caido", ahora: Date.now() });
                else despachar({ tipo: "fallo", motivo: otroNavegador ? "otro_navegador" : "medio_fallido" });
            })();
        },
        [despachar, empresaMontada],
    );

    useEffect(
        () =>
            escucharEventos((evento) => {
                if (!evento.llamada) return;
                const llamada = evento.llamada;
                // `llamada_respuesta` todavía no está en el tipo generado del feed (i3.md): se compara como texto.
                const tipo: string = evento.tipo;
                if (tipo === "llamada_entrante") {
                    // Solo suena en un navegador que tiene «Recibir llamadas» encendido.
                    if (recibeRef.current) despachar({ tipo: "entrante", llamada });
                    return;
                }
                if (tipo !== "llamada_actualizada" && tipo !== "llamada_terminada" && tipo !== "llamada_respuesta") return;
                const antes = estadoRef.current;
                const mio = intento.current;
                const esMiSaliente = antes.fase === "llamando" && antes.llamada?.id === llamada.id && mio !== null;
                if (tipo === "llamada_respuesta") {
                    if (esMiSaliente) traerRespuesta(mio, llamada.id);
                    return;
                }
                if (esMiSaliente && llamada.estado === "en_curso") {
                    // El cliente contestó. Desde aquí la llamada está viva: dejarla es colgarla.
                    mio.etapa = "en_curso";
                    window.clearTimeout(mio.plazo);
                    // Nunca `en_curso` sin audio: si el medio no conecta pronto, se cuelga.
                    mio.plazo = window.setTimeout(() => intento.current === mio && !mio.conecto && despachar({ tipo: "medio_caido", ahora: Date.now() }), MEDIO_SALIENTE_MS);
                    traerRespuesta(mio, llamada.id);
                }
                despachar({ tipo: "actualizada", llamada, ahora: Date.now() });
            }),
        [despachar, traerRespuesta],
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
                    else {
                        const motivo = motivoDeCodigo(error.codigo, error.estado, error.errores[0]?.rule);
                        despachar({ tipo: "fallo", motivo, detalle: motivo === "otro_la_contesto" ? error.detalle : undefined });
                    }
                    // El servidor dice que este navegador ya no recibe: se relee para decirlo.
                    if (error.codigo === "no_recibe_llamadas") releerRecibir.current();
                } else {
                    despachar({ tipo: "fallo", motivo: "medio_fallido" });
                }
            }
        })();
    }, [despachar, empresaMontada, yoId, trasAsegurar]);

    // --- Llamar (E1b) ----------------------------------------------------------------------------
    const [riesgo, setRiesgo] = useState<{ destino: DestinoLlamada; opciones: OpcionesLlamar } | null>(null);

    const marcar = useCallback(
        (destino: DestinoLlamada, opciones: OpcionesLlamar, confirmarRiesgo: boolean) => {
            const libre = estadoRef.current.fase === "libre" || estadoRef.current.fase === "aviso";
            if (!libre || intento.current) return;
            const ctx = { empresa: empresaMontada, yoId };
            const mio: Intento = { conexion: null, microfono: null, plazo: 0, liberar: null, etapa: "preparando", reservaSegura: false };
            const vivo = () => intento.current === mio;
            setErrorColgar(null);
            despachar({ tipo: "llamar", conversacionId: destino.id, titulo: destino.titulo });
            if (estadoRef.current.fase !== "llamando") return;
            intento.current = mio;
            void (async () => {
                try {
                    const mic = await pedirMicrofono();
                    if (!vivo()) {
                        soltarMicrofono(mic);
                        return;
                    }
                    mio.microfono = mic;
                    setMicrofono("concedido");
                    const oferta = await ofrecer(mic);
                    if (!vivo()) {
                        oferta.cerrar();
                        return;
                    }
                    mio.conexion = oferta;
                    mio.oferta = oferta;
                    void oferta.conectado.then(
                        () => {
                            mio.conecto = true;
                            if (vivo() && estadoRef.current.fase === "en_curso") window.clearTimeout(mio.plazo);
                        },
                        () => {
                            if (!vivo()) return;
                            if (estadoRef.current.fase === "en_curso") despachar({ tipo: "medio_caido", ahora: Date.now() });
                            else despachar({ tipo: "fallo", motivo: "medio_fallido" });
                        },
                    );
                    oferta.alCaer(() => {
                        if (!vivo()) return;
                        if (estadoRef.current.fase === "en_curso") despachar({ tipo: "medio_caido", ahora: Date.now() });
                        else despachar({ tipo: "fallo", motivo: "medio_fallido" });
                    });
                    // Desde que sale `llamar` el servidor puede tener la llamada marcando: la etapa
                    // cambia antes, y dejarla desde aquí es colgarla.
                    let resuelveVuelo: (fin: Promise<Fin | null> | null) => void = () => undefined;
                    mio.enVuelo = new Promise<Fin | null>((r) => (resuelveVuelo = r));
                    mio.etapa = "llamando";
                    const control = new AbortController();
                    const limite = window.setTimeout(() => control.abort(), LIMITE_LLAMAR_MS);
                    let llamada: Llamada;
                    try {
                        llamada = await llamarContacto(destino.id, { sdpOferta: oferta.sdpOferta, devolucionId: opciones.devolucionId, confirmarRiesgo }, nuevaClaveIdempotencia(), empresaMontada, control.signal);
                    } catch (e) {
                        if (incierto(e)) {
                            // No se sabe si la creó: se busca y se cuelga, se siga aquí o no.
                            const fin = vivo() ? registrar(colgarPerdida) : colgarPerdida();
                            resuelveVuelo(fin);
                        } else {
                            // El servidor dijo que no: no hay llamada.
                            resuelveVuelo(null);
                        }
                        if (vivo()) {
                            mio.etapa = "preparando";
                            delete mio.enVuelo;
                        }
                        throw e;
                    } finally {
                        window.clearTimeout(limite);
                    }
                    if (!vivo()) {
                        // Respondió cuando ya se había dejado (se colgó, se cerró, se cambió de empresa): se cuelga.
                        const id = llamada.id;
                        resuelveVuelo(colgarSeguro(id, ctx, true).then((fin) => (trasAsegurar(id, fin, true), fin)));
                        return;
                    }
                    resuelveVuelo(null);
                    delete mio.enVuelo;
                    despachar({ tipo: "marcando", llamada });
                    if (!vivo()) return;
                    mio.plazo = window.setTimeout(() => vivo() && estadoRef.current.fase === "llamando" && despachar({ tipo: "plazo_vencido" }), MARCANDO_MAX_MS);
                } catch (error) {
                    if (!vivo()) return;
                    if (error instanceof ErrorMicrofono) {
                        setMicrofono(error.estado);
                        despachar({ tipo: "fallo", motivo: error.estado === "denegado" ? "microfono_denegado" : "sin_microfono" });
                    } else if (error instanceof ErrorApi && error.codigo === "confirmar_riesgo") {
                        // El servidor cuenta tres sin contestar (aquí no se sabía): se pregunta y, si sigue, se llama.
                        despachar({ tipo: "abandonar" });
                        setRiesgo({ destino, opciones });
                    } else if (error instanceof ErrorApi && !incierto(error)) {
                        const motivo = motivoDeCodigo(error.codigo, error.estado, error.errores[0]?.rule);
                        // Lo que no tiene texto propio se dice con las palabras del servidor.
                        despachar({ tipo: "fallo", motivo, detalle: motivo === "error" ? error.detalle || error.titulo : undefined });
                    } else {
                        despachar({ tipo: "fallo", motivo: error instanceof ErrorApi || error instanceof DOMException ? "sin_red" : "medio_fallido" });
                    }
                }
            })();
        },
        [despachar, empresaMontada, yoId, trasAsegurar, registrar, colgarPerdida],
    );

    const llamar = useCallback(
        (destino: DestinoLlamada, opciones: OpcionesLlamar = {}) => {
            // A la cuarta sin contestar WhatsApp quita el permiso: se avisa antes de gastarla.
            if ((opciones.sinContestar ?? 0) >= SIN_CONTESTAR_RIESGO) setRiesgo({ destino, opciones });
            else marcar(destino, opciones, false);
        },
        [marcar],
    );

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

    const rechazarParaTodos = useCallback(async () => {
        const e = estadoRef.current;
        if (e.fase !== "sonando") return;
        const { id, titulo } = e.llamada;
        try {
            await rechazarLlamada(id, empresaMontada, true);
            despachar({ tipo: "rechazar" });
            avisar({ titulo: `Llamada de ${titulo} rechazada para todos` });
        } catch (error) {
            // Ya la contestó alguien o terminó: el feed lo dirá; aquí solo se explica.
            avisar({ tono: "warning", titulo: "No se pudo rechazar para todos", descripcion: error instanceof ErrorApi && error.estado === 409 ? "La llamada ya no está sonando." : mensajeDeError(error).titulo });
        }
    }, [despachar, empresaMontada]);

    /** «Colgar»: se cierra el audio y `despachar` asegura (con reintentos) que el servidor la termine. */
    const colgar = useCallback(() => {
        const fase = estadoRef.current.fase;
        if (fase !== "en_curso" && fase !== "llamando") return;
        setErrorColgar(null);
        despachar({ tipo: "colgar", ahora: Date.now() });
    }, [despachar]);

    const reintentarColgar = useCallback(() => {
        setErrorColgar(null);
        reintentarSalidas();
    }, [reintentarSalidas]);

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
            if (que) avisarAlCerrar(que.id, que.accion, empresaMontada, que.propia === true);
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
    type Pregunta = {
        resolver: (seguir: boolean) => void;
        modo: "sonando" | "llamando" | "hablando";
        titulo: string;
        /** No se logró terminar la llamada a tiempo: se elige «Reintentar» o «Salir de todos modos». */
        sinTerminar: boolean;
        reintentando: boolean;
    };
    const [pregunta, setPregunta] = useState<Pregunta | null>(null);
    const preguntaRef = useRef<Pregunta | null>(null);
    useEffect(() => {
        preguntaRef.current = pregunta;
    });
    // Si el proveedor se desmonta con la pregunta abierta, quien la esperaba recibe «quedarme».
    useEffect(() => () => preguntaRef.current?.resolver(false), []);

    /**
     * ¿Terminó bien, dentro del tope, todo lo que se mandó a terminar en el servidor? También lo que
     * venía de antes (un «Colgar» en vuelo). Sin respuesta a tiempo, o con fallo: no.
     */
    const terminoATiempo = useCallback(async (): Promise<boolean> => {
        const todas = Promise.all([...salidas.current].map((s) => s.promesa.catch((): Fin => ({ tipo: "fallo", error: null })))).then((fines) => fines.every((f) => f?.tipo !== "fallo"));
        return Promise.race([todas, esperar(TOPE_SALIDA_MS).then(() => false)]);
    }, []);

    const antesDeSalir = useCallback(() => {
        const e = estadoRef.current;
        const viva = e.fase === "sonando" || ocupada(e);
        if (viva) {
            const titulo = e.fase === "llamando" ? e.titulo : (llamadaDe(e)?.titulo ?? "el cliente");
            const modo = e.fase === "sonando" ? "sonando" : e.fase === "llamando" ? "llamando" : "hablando";
            return new Promise<boolean>((resolver) => setPregunta({ resolver, modo, titulo, sinTerminar: false, reintentando: false }));
        }
        // Ya no hay llamada aquí, pero puede haber un «Colgar» en vuelo (o que falló): después no
        // habrá sesión, o será otra empresa, así que se espera; si no termina, se dice.
        if (salidas.current.size === 0) return Promise.resolve(true);
        return terminoATiempo().then((logrado) => logrado || new Promise<boolean>((resolver) => setPregunta({ resolver, modo: "hablando", titulo: "el cliente", sinTerminar: true, reintentando: false })));
    }, [terminoATiempo]);

    /** Deja la llamada y espera a que el servidor la termine. `false`: no se logró a tiempo. */
    const dejarLlamada = useCallback(async (): Promise<boolean> => {
        const e = estadoRef.current;
        const id = llamadaDe(e)?.id ?? null;
        const sonaba = e.fase === "sonando";
        despachar({ tipo: "abandonar" });
        // Después ya no habrá sesión, o será otra empresa: hay que terminarla antes.
        const logrado = await terminoATiempo();
        if (sonaba && id) await Promise.race([rechazarLlamada(id, empresaMontada).catch(() => undefined), esperar(2000)]);
        return logrado;
    }, [despachar, empresaMontada, terminoATiempo]);

    const reintentarSalida = useCallback(async () => {
        const p = preguntaRef.current;
        if (!p?.sinTerminar || p.reintentando) return;
        setPregunta({ ...p, reintentando: true });
        reintentarSalidas();
        const logrado = await terminoATiempo();
        // Si mientras tanto eligió «Salir de todos modos» (o se cerró), el diálogo no se reabre.
        if (logrado) p.resolver(true);
        setPregunta((actual) => (actual?.resolver !== p.resolver ? actual : logrado ? null : { ...actual, reintentando: false }));
    }, [reintentarSalidas, terminoATiempo]);

    const reintentarRecibir = useCallback(() => {
        setRecibir({ estado: "cargando" });
        setIntentoRecibir((n) => n + 1);
    }, []);

    const valor = useMemo<ContextoVoz>(
        () => ({ estado, contesta, llamar, contestar, rechazar, colgar, silenciar, cerrar, errorColgar, reintentarColgar, recibir, desplazado, microfono, admite, cambiando, errorCambio, encender, apagar, reintentarRecibir, feed, timbre: suena, antesDeSalir, rechazarParaTodos }),
        [estado, contesta, llamar, contestar, rechazar, colgar, silenciar, cerrar, errorColgar, reintentarColgar, recibir, desplazado, microfono, admite, cambiando, errorCambio, encender, apagar, reintentarRecibir, feed, suena, antesDeSalir, rechazarParaTodos],
    );
    return (
        <Contexto value={valor}>
            {children}
            {pregunta?.sinTerminar ? (
                // No se logró terminar la llamada a tiempo: no se sigue en silencio.
                <DialogoConfirmar
                    open
                    onOpenChange={(abierto) => {
                        // «Reintentar» (y Escape): se queda en el diálogo mientras lo intenta.
                        if (!abierto) void reintentarSalida();
                    }}
                    titulo="No se pudo colgar la llamada"
                    descripcion={pregunta.reintentando ? "Reintentando…" : `Para ${pregunta.titulo} la llamada puede seguir en línea. Reintenta o, si no hay manera, sal de todos modos: el servidor la corta solo al poco tiempo.`}
                    textoConfirmar="Salir de todos modos"
                    textoCancelar={pregunta.reintentando ? "Reintentando…" : "Reintentar"}
                    peligro
                    alConfirmar={() => {
                        pregunta.resolver(true);
                        setPregunta(null);
                    }}
                />
            ) : (
                <DialogoConfirmar
                    open={pregunta !== null}
                    onOpenChange={(abierto) => {
                        if (abierto || !pregunta) return;
                        pregunta.resolver(false);
                        setPregunta(null);
                    }}
                    titulo={pregunta?.modo === "hablando" ? `Tienes una llamada con ${pregunta.titulo}` : pregunta?.modo === "llamando" ? `Estás llamando a ${pregunta.titulo}` : "Tienes una llamada entrando"}
                    descripcion={pregunta?.modo === "sonando" ? "Si sigues, deja de sonarte aquí y les sigue sonando a los demás." : "Si sigues, la llamada se cuelga."}
                    textoConfirmar={pregunta?.modo === "sonando" ? "Seguir" : "Colgar y seguir"}
                    textoCancelar="Quedarme"
                    textoOcupado="Terminando la llamada…"
                    peligro={pregunta ? pregunta.modo !== "sonando" : false}
                    alConfirmar={async () => {
                        if (!pregunta) return;
                        if (!(await dejarLlamada())) {
                            setPregunta({ ...pregunta, sinTerminar: true });
                            return;
                        }
                        pregunta.resolver(true);
                        setPregunta(null);
                    }}
                />
            )}
            <DialogoConfirmar
                open={riesgo !== null}
                onOpenChange={(abierto) => !abierto && setRiesgo(null)}
                titulo="Ya van tres llamadas sin contestar"
                descripcion={`Si ${riesgo?.destino.titulo ?? "el cliente"} tampoco contesta esta, WhatsApp le quita a la empresa el permiso para llamarle y habrá que pedírselo otra vez. Quizá convenga escribirle antes.`}
                textoConfirmar="Llamar de todos modos"
                textoCancelar="No llamar"
                peligro
                alConfirmar={() => {
                    if (!riesgo) return;
                    const { destino, opciones } = riesgo;
                    setRiesgo(null);
                    marcar(destino, opciones, true);
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
