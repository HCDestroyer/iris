import { crearLectorSSE } from "./sse";

/*
 * Tiempo real de la bandeja con el mismo patrón que el chat interno (contratos/chat.md §4 y
 * contratos/conversaciones.md §9): un feed por usuario y empresa con cursor creciente. El flujo
 * SSE y el sondeo (`/cambios`) entregan el mismo evento con el mismo cursor, así que se puede pasar
 * de uno al otro sin perder nada.
 *
 * - El flujo se lee con `fetch` (no `EventSource`): lleva `X-Iris-Empresa` y `Last-Event-ID`, y el
 *   panel ve el estado HTTP.
 * - **Antes de abrir cada flujo o sondear, se comprueba que la sesión del navegador sigue siendo
 *   de este usuario y empresa** (`verificarSesion`). Si en otra pestaña entró otra persona, el feed
 *   se detiene sin entregar nada (`alSesionCambiada`): nunca se mezclan en pantalla eventos de
 *   otra cuenta.
 * - 401/403 → sin sesión o sin permiso: no reintenta. 410 `cursor_vencido` → recargar todo y seguir
 *   desde la cabeza. 429/503 → sondeo cada 12 s y vuelve a probar el flujo a los 5 min.
 * - Sin latido en 60 s (el servidor manda uno cada 25 s) → corta y reconecta.
 * - **Un corte no es «sin conexión»**: un flujo que ya estaba abierto y se corta (un proxy que cierra
 *   conexiones largas, el servidor que reinicia, un cambio de red) se reconecta al momento con
 *   `Last-Event-ID`, sin avisar. «Sin conexión» solo se dice tras dos intentos fallidos seguidos o
 *   si pasan más de 9 s sin poder conectar. Al volver se sigue desde el último id: nada se pierde ni se repite.
 *   Excepción: si el flujo abre y se corta de golpe al poco (menos de 3 s) dos veces seguidas, sí se
 *   avisa, y el aviso se quita cuando un flujo dura.
 * - Los eventos se aplican por `id`: uno puede llegar dos veces.
 */

export type EventoFeed = { id: string };

export type PaginaCambios<E> = { data: E[]; nextCursor: string; hayMas?: boolean };

export type EstadoFeed =
    /** Abriendo el flujo o leyendo la cabeza. */
    | "conectando"
    /** Flujo SSE abierto. */
    | "en_vivo"
    /** Sondeo cada 12 s (la API no dio flujo: 429 o 503). */
    | "sondeo"
    /** Sin red: reintenta con espera. */
    | "sin_conexion"
    /** 401/403 o la sesión cambió: detenido. */
    | "detenido";

/** Error con estado HTTP (el de `ErrorApi` lo cumple). */
type ConEstado = { estado: number };
const estadoDe = (e: unknown): number | undefined =>
    typeof e === "object" && e !== null && typeof (e as ConEstado).estado === "number" ? (e as ConEstado).estado : undefined;

export type OpcionesFeed<E extends EventoFeed> = {
    /** Abre el flujo SSE (`Accept: text/event-stream`, `Last-Event-ID` si hay cursor). */
    abrirFlujo: (cursor: string | undefined, senal: AbortSignal) => Promise<Response>;
    /** `GET …/cambios?cursor=`: lanza un error con `estado` si la API respondió con error. */
    leerCambios: (cursor: string | undefined, senal: AbortSignal) => Promise<PaginaCambios<E>>;
    /** Lanza si la sesión del navegador ya no es la de este usuario y empresa (`GET /yo`). */
    verificarSesion: () => Promise<void>;
    /** ¿El error de `verificarSesion` dice que la sesión es de otra persona o empresa? */
    esSesionAjena?: (error: unknown) => boolean;
    /** Interpreta un `data:` del flujo; `null` si no es un evento conocido. */
    leerEvento: (datos: string) => E | null;
    alEvento: (evento: E) => void;
    alEstado?: (estado: EstadoFeed) => void;
    /** 410 `cursor_vencido`: la pantalla vuelve a leer listas y conversación. */
    alCursorVencido?: () => void;
    /** 401/403 o sesión ajena: la pantalla avisa y no reintenta. */
    alDetenido?: (motivo: "sin_sesion" | "sin_permiso" | "sesion_cambiada") => void;
    /** Plazos (ms), para pruebas. */
    plazos?: Partial<Plazos>;
    /** Temporizadores y eventos del navegador, para pruebas. */
    reloj?: Reloj;
};

/**
 * `flujoMinimo`: un flujo que se cierra antes de esto se reintenta con espera creciente (no en bucle).
 * `reintentoRapido`: lo que se espera tras el primer fallo (después, `esperaMin` y creciendo).
 * `gracia`: tiempo sin flujo a partir del cual se dice «sin conexión» aunque solo haya un fallo.
 * `fallosParaAvisar`: intentos fallidos seguidos que hacen falta para decirlo antes.
 */
type Plazos = { sondeo: number; volverAlFlujo: number; sinLatido: number; esperaMin: number; esperaMax: number; flujoMinimo: number; reintentoRapido: number; gracia: number; fallosParaAvisar: number };
export const PLAZOS_FEED: Plazos = { sondeo: 12_000, volverAlFlujo: 300_000, sinLatido: 60_000, esperaMin: 3_000, esperaMax: 30_000, flujoMinimo: 3_000, reintentoRapido: 1_000, gracia: 9_000, fallosParaAvisar: 2 };
const PLAZOS = PLAZOS_FEED;

export type Reloj = {
    esperar: (ms: number, senal: AbortSignal) => Promise<void>;
    ahora: () => number;
};

const relojNavegador: Reloj = {
    esperar: (ms, senal) =>
        new Promise((resolver) => {
            if (senal.aborted) {
                resolver();
                return;
            }
            const t = setTimeout(fin, ms);
            const enLinea = () => fin();
            function fin() {
                clearTimeout(t);
                senal.removeEventListener("abort", fin);
                if (typeof window !== "undefined") {
                    window.removeEventListener("online", enLinea);
                }
                resolver();
            }
            senal.addEventListener("abort", fin);
            // Al volver la red no se espera el plazo entero.
            if (typeof window !== "undefined") {
                window.addEventListener("online", enLinea);
            }
        }),
    ahora: () => Date.now(),
};

class Detener extends Error {}

/**
 * Arranca el feed y devuelve la función que lo cierra. Primero lee la cabeza (`/cambios` sin
 * cursor) y llama `alListo` con ella: la pantalla lee sus listas después, así nada que ocurra
 * entre la lectura y la apertura del flujo se pierde.
 */
export function iniciarFeed<E extends EventoFeed>(opciones: OpcionesFeed<E>, alListo?: () => void): () => void {
    const plazos = { ...PLAZOS, ...opciones.plazos };
    const reloj = opciones.reloj ?? relojNavegador;
    const control = new AbortController();
    const senal = control.signal;
    let cursor: string | undefined;
    let estado: EstadoFeed | undefined;
    const vistos = new Set<string>();
    // Fallos seguidos desde el último flujo bueno, y desde cuándo no hay flujo.
    let fallos = 0;
    let avisoPendiente: ReturnType<typeof setTimeout> | undefined;

    /**
     * Un intento de conectar falló (no hay red, o el servidor o un proxy respondieron con error).
     * Solo se dice «sin conexión» al segundo fallo seguido, o si pasa la gracia sin haber vuelto
     * (p. ej. un reintento que se queda colgado).
     */
    function anotarFallo() {
        fallos++;
        if (fallos >= plazos.fallosParaAvisar) {
            cambiar("sin_conexion");
            return;
        }
        avisoPendiente ??= setTimeout(() => {
            avisoPendiente = undefined;
            if (!senal.aborted && fallos > 0) cambiar("sin_conexion");
        }, plazos.gracia);
    }

    /** Hay flujo (o sondeo) otra vez. */
    function recuperado() {
        fallos = 0;
        clearTimeout(avisoPendiente);
        avisoPendiente = undefined;
    }
    senal.addEventListener("abort", () => clearTimeout(avisoPendiente));
    // Flujos seguidos que abrieron bien y se cortaron de golpe al poco: a partir del segundo ya no
    // es «un corte», es que el flujo no se sostiene, y se avisa hasta que uno dure.
    let cortesCortos = 0;

    function cambiar(nuevo: EstadoFeed) {
        if (nuevo !== estado) {
            estado = nuevo;
            opciones.alEstado?.(nuevo);
        }
    }

    function entregar(evento: E) {
        if (senal.aborted) {
            return;
        }
        if (vistos.has(evento.id)) {
            // Ya aplicado (pasó del sondeo al flujo o llegó dos veces): se ignora.
            cursor = evento.id;
            return;
        }
        vistos.add(evento.id);
        if (vistos.size > 2000) {
            // Solo importan los recientes.
            const viejo = vistos.values().next().value;
            if (viejo !== undefined) {
                vistos.delete(viejo);
            }
        }
        cursor = evento.id;
        opciones.alEvento(evento);
    }

    async function verificar() {
        try {
            await opciones.verificarSesion();
        } catch (error) {
            if (opciones.esSesionAjena?.(error)) {
                detener("sesion_cambiada");
            }
            const e = estadoDe(error);
            if (e === 401) {
                detener("sin_sesion");
            }
            if (e === 403) {
                detener("sin_permiso");
            }
            throw error;
        }
    }

    function detener(motivo: "sin_sesion" | "sin_permiso" | "sesion_cambiada"): never {
        cambiar("detenido");
        opciones.alDetenido?.(motivo);
        control.abort();
        throw new Detener(motivo);
    }

    /** Errores de la API que cortan o cambian de modo; los demás son de red (reintentar). */
    function tratarEstado(e: number | undefined): "vencido" | "sondeo" | "red" {
        if (e === 401) {
            detener("sin_sesion");
        }
        if (e === 403) {
            detener("sin_permiso");
        }
        if (e === 410) {
            return "vencido";
        }
        if (e === 429 || e === 503) {
            return "sondeo";
        }
        return "red";
    }

    async function cabeza() {
        await verificar();
        const pagina = await opciones.leerCambios(undefined, senal);
        cursor = pagina.nextCursor;
    }

    async function vencido() {
        cursor = undefined;
        vistos.clear();
        await cabeza();
        opciones.alCursorVencido?.();
    }

    /**
     * Lee el flujo hasta que se corte. Devuelve qué hacer después: `reconectar` solo si el flujo duró
     * (el cierre normal es a los 30 min); si se cierra al poco de abrirse (un proxy que corta, un
     * servidor que cierra al instante), `corto`, que se reintenta con espera para no entrar en bucle.
     * Un flujo ya abierto que se corta de golpe (error de red a media lectura: un proxy que corta
     * conexiones largas, el servidor que reinicia) cuenta igual que un cierre limpio; `red` es que ni
     * siquiera se pudo abrir.
     */
    async function flujo(): Promise<"reconectar" | "corto" | "sondeo" | "vencido" | "red"> {
        await verificar();
        let inicio: number | undefined;
        const duro = (): "reconectar" | "corto" => (inicio !== undefined && reloj.ahora() - inicio >= plazos.flujoMinimo ? "reconectar" : "corto");
        const local = new AbortController();
        let asentado: ReturnType<typeof setTimeout> | undefined;
        const cortar = () => local.abort();
        senal.addEventListener("abort", cortar);
        try {
            const respuesta = await opciones.abrirFlujo(cursor, local.signal);
            if (!respuesta.ok || !respuesta.body) {
                return tratarEstado(respuesta.status);
            }
            recuperado();
            if (cortesCortos < plazos.fallosParaAvisar) {
                cambiar("en_vivo");
            } else {
                // Venía cortándose: no se da por bueno hasta que dure.
                asentado = setTimeout(() => {
                    cortesCortos = 0;
                    cambiar("en_vivo");
                }, plazos.flujoMinimo);
            }
            inicio = reloj.ahora();
            let ultimo = reloj.ahora();
            const lector = crearLectorSSE(() => {
                ultimo = reloj.ahora();
            });
            const vigia = setInterval(() => {
                if (reloj.ahora() - ultimo > plazos.sinLatido) {
                    local.abort();
                }
            }, Math.min(plazos.sinLatido, 10_000));
            const decodificador = new TextDecoder();
            const lectura = respuesta.body.getReader();
            try {
                for (;;) {
                    const { value, done } = await lectura.read();
                    if (done) {
                        break;
                    }
                    ultimo = reloj.ahora();
                    for (const ev of lector.agregar(decodificador.decode(value, { stream: true }))) {
                        const evento = opciones.leerEvento(ev.datos);
                        if (evento) {
                            entregar(evento);
                        } else if (ev.id) {
                            cursor = ev.id;
                        }
                    }
                }
            } finally {
                clearInterval(vigia);
                lectura.releaseLock();
            }
            // El servidor cierra cada flujo a los 30 min: se reconecta con el último id.
            const fin = duro();
            if (fin === "reconectar") cortesCortos = 0;
            return fin;
        } catch (error) {
            if (error instanceof Detener || senal.aborted) {
                throw error;
            }
            const e = estadoDe(error);
            if (e !== undefined) return tratarEstado(e);
            // Lo cortó el vigía (sin latido): reconexión normal.
            if (local.signal.aborted) return duro();
            // Sin abrir: no hay red. Abierto y cortado de golpe: una reconexión más, sin avisar… salvo
            // que se corte al poco de abrir una y otra vez.
            if (inicio === undefined) return "red";
            const fin = duro();
            if (fin === "reconectar") {
                cortesCortos = 0;
            } else {
                cortesCortos++;
                if (cortesCortos >= plazos.fallosParaAvisar) cambiar("sin_conexion");
            }
            return fin;
        } finally {
            clearTimeout(asentado);
            senal.removeEventListener("abort", cortar);
        }
    }

    /** Sondea hasta que toque volver a probar el flujo. */
    async function sondear(): Promise<"vencido" | "red" | "flujo"> {
        cambiar("sondeo");
        const hasta = reloj.ahora() + plazos.volverAlFlujo;
        while (!senal.aborted && reloj.ahora() < hasta) {
            await verificar();
            let pagina: PaginaCambios<E>;
            try {
                pagina = await opciones.leerCambios(cursor, senal);
            } catch (error) {
                if (error instanceof Detener || senal.aborted) {
                    throw error;
                }
                const r = tratarEstado(estadoDe(error));
                if (r === "vencido") {
                    return r;
                }
                if (r === "red") {
                    return "red";
                }
                await reloj.esperar(plazos.sondeo, senal);
                continue;
            }
            recuperado();
            for (const e of pagina.data) {
                entregar(e);
            }
            cursor = pagina.nextCursor || cursor;
            if (!pagina.hayMas) {
                await reloj.esperar(plazos.sondeo, senal);
            }
        }
        return "flujo";
    }

    async function bucle() {
        let espera = plazos.esperaMin;
        let listo = false;
        // 410 seguidos: el primero recarga al instante; si se repite, se espera con backoff.
        let vencidosSeguidos = 0;
        async function trasVencido() {
            if (vencidosSeguidos > 0) {
                await reloj.esperar(espera, senal);
                espera = Math.min(espera * 2, plazos.esperaMax);
            }
            vencidosSeguidos++;
        }
        while (!senal.aborted) {
            try {
                // Reconectar tras un cierre normal (cada 30 min) no es «conectando»: sigue al día.
                // …y si el flujo viene cortándose al poco de abrir, el aviso se queda fijo hasta que uno dure.
                const avisando = estado === "sin_conexion" && cortesCortos >= plazos.fallosParaAvisar;
                if (estado !== "en_vivo" && estado !== "sondeo" && !avisando) cambiar("conectando");
                if (cursor === undefined) {
                    await cabeza();
                }
                if (!listo) {
                    listo = true;
                    alListo?.();
                }
                let siguiente: "reconectar" | "corto" | "sondeo" | "vencido" | "red" | "flujo" = await flujo();
                if (siguiente === "sondeo") {
                    siguiente = await sondear();
                }
                if (siguiente === "vencido") {
                    await trasVencido();
                    await vencido();
                    continue;
                }
                vencidosSeguidos = 0;
                if (siguiente === "red") {
                    anotarFallo();
                    // El primer reintento es casi inmediato; después, espera creciente.
                    if (fallos === 1) {
                        await reloj.esperar(plazos.reintentoRapido, senal);
                    } else {
                        await reloj.esperar(espera, senal);
                        espera = Math.min(espera * 2, plazos.esperaMax);
                    }
                    continue;
                }
                if (siguiente === "corto") {
                    // Como un corte de red, pero sin «sin conexión»: lo recibido sigue al día.
                    await reloj.esperar(espera, senal);
                    espera = Math.min(espera * 2, plazos.esperaMax);
                    continue;
                }
                // Solo un flujo largo devuelve la espera al mínimo.
                if (siguiente === "reconectar") espera = plazos.esperaMin;
            } catch (error) {
                if (error instanceof Detener || senal.aborted) {
                    return;
                }
                const e = estadoDe(error);
                if (e === 401 || e === 403) {
                    try {
                        detener(e === 401 ? "sin_sesion" : "sin_permiso");
                    } catch {
                        return;
                    }
                }
                if (e === 410) {
                    try {
                        await trasVencido();
                        await vencido();
                    } catch {
                        // Se reintenta en la vuelta siguiente.
                    }
                    continue;
                }
                anotarFallo();
                if (fallos === 1) {
                    await reloj.esperar(plazos.reintentoRapido, senal);
                } else {
                    await reloj.esperar(espera, senal);
                    espera = Math.min(espera * 2, plazos.esperaMax);
                }
            }
        }
    }

    void bucle();
    return () => control.abort();
}
