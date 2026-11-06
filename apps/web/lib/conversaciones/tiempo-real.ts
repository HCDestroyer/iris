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

type Plazos = { sondeo: number; volverAlFlujo: number; sinLatido: number; esperaMin: number; esperaMax: number };
const PLAZOS: Plazos = { sondeo: 12_000, volverAlFlujo: 300_000, sinLatido: 60_000, esperaMin: 3_000, esperaMax: 30_000 };

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

    /** Lee el flujo hasta que se corte. Devuelve qué hacer después. */
    async function flujo(): Promise<"reconectar" | "sondeo" | "vencido" | "red"> {
        await verificar();
        const local = new AbortController();
        const cortar = () => local.abort();
        senal.addEventListener("abort", cortar);
        try {
            const respuesta = await opciones.abrirFlujo(cursor, local.signal);
            if (!respuesta.ok || !respuesta.body) {
                return tratarEstado(respuesta.status);
            }
            cambiar("en_vivo");
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
            return "reconectar";
        } catch (error) {
            if (error instanceof Detener || senal.aborted) {
                throw error;
            }
            const e = estadoDe(error);
            return e === undefined ? (local.signal.aborted ? "reconectar" : "red") : tratarEstado(e);
        } finally {
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
        while (!senal.aborted) {
            try {
                // Reconectar tras un cierre normal (cada 30 min) no es «conectando»: sigue al día.
                if (estado !== "en_vivo" && estado !== "sondeo") cambiar("conectando");
                if (cursor === undefined) {
                    await cabeza();
                }
                if (!listo) {
                    listo = true;
                    alListo?.();
                }
                let siguiente: "reconectar" | "sondeo" | "vencido" | "red" | "flujo" = await flujo();
                if (siguiente === "sondeo") {
                    siguiente = await sondear();
                }
                if (siguiente === "vencido") {
                    await vencido();
                    continue;
                }
                if (siguiente === "red") {
                    cambiar("sin_conexion");
                    await reloj.esperar(espera, senal);
                    espera = Math.min(espera * 2, plazos.esperaMax);
                    continue;
                }
                espera = plazos.esperaMin;
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
                        await vencido();
                    } catch {
                        // Se reintenta en la vuelta siguiente.
                    }
                    continue;
                }
                cambiar("sin_conexion");
                await reloj.esperar(espera, senal);
                espera = Math.min(espera * 2, plazos.esperaMax);
            }
        }
    }

    void bucle();
    return () => control.abort();
}
