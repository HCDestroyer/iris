// Flujo SSE del chat (`GET /api/v1/mensajeria/chat/eventos`) leído con un `fetch` que entrega el
// cuerpo por partes: en la app nativa, `expo/fetch` (Expo 58), que comparte las cookies de la sesión
// con el `fetch` de React Native (Android: el `CookieJar` de OkHttp; iOS: `HTTPCookieStorage`). A
// diferencia de `EventSource`, ve el estado HTTP (401/403 detienen, 429/503 esperan) y manda las
// cabeceras de la app (`X-Iris-Empresa`, `X-Iris-Canal`, `X-Iris-App`) y `Last-Event-ID`.
//
// - Un latido (`: latido`, cada 25 s) o cualquier byte renueva el plazo; sin nada en `sinLatidoMs`
//   (60 s) el flujo se da por muerto: se corta y avisa `alCerrar` (el motor reconecta).
// - `cerrar()` aborta la petición y no avisa (lo pide el motor: segundo plano, detener).
// - El contenido de un mensaje nunca se registra.
import { ErrorTransporte } from "../errores";
import type { Problema } from "../contrato";
import type { AbrirFlujoChat, EventoChat, OyentesFlujo } from "./tipos";

/** Tipos de evento del flujo (`event:`); otro tipo se ignora. */
const TIPOS_EVENTO: ReadonlySet<string> = new Set<EventoChat["tipo"]>(["mensaje_nuevo", "mensaje_eliminado", "leido", "conversacion_actualizada"]);

export const RUTA_FLUJO_CHAT = "/api/v1/mensajeria/chat/eventos";
/** El servidor manda un latido cada 25 s: sin nada en 60 s, el flujo está muerto. */
export const SIN_LATIDO_MS = 60_000;

export type EventoSSE = { id: string | undefined; tipo: string; datos: string };

/**
 * Lector incremental de `text/event-stream` (WHATWG «Server-sent events» §9.2.6): recibe trozos de
 * texto y devuelve los eventos que cerró (línea en blanco). Los comentarios (`: latido`) se ignoran.
 */
export function crearLectorSSE(): { agregar: (trozo: string) => EventoSSE[] } {
    let resto = "";
    let id: string | undefined;
    let tipo = "";
    let datos: string[] = [];
    let hayDatos = false;
    const linea = (texto: string, salida: EventoSSE[]) => {
        if (texto === "") {
            if (hayDatos) {
                salida.push({ id, tipo: tipo || "message", datos: datos.join("\n") });
            }
            id = undefined;
            tipo = "";
            datos = [];
            hayDatos = false;
            return;
        }
        if (texto.startsWith(":")) {
            return;
        }
        const dos = texto.indexOf(":");
        const campo = dos === -1 ? texto : texto.slice(0, dos);
        let valor = dos === -1 ? "" : texto.slice(dos + 1);
        if (valor.startsWith(" ")) {
            valor = valor.slice(1);
        }
        if (campo === "data") {
            datos.push(valor);
            hayDatos = true;
        } else if (campo === "event") {
            tipo = valor;
        } else if (campo === "id" && !valor.includes("\0")) {
            id = valor;
        }
        // `retry:` se ignora: los plazos de reconexión los decide el motor.
    };
    return {
        agregar(trozo) {
            const salida: EventoSSE[] = [];
            let texto = resto + trozo;
            // Un CR al final puede ser la mitad de un CRLF: se guarda para el siguiente trozo.
            const guardaCR = texto.endsWith("\r");
            if (guardaCR) {
                texto = texto.slice(0, -1);
            }
            const partes = texto.split(/\r\n|\n|\r/);
            resto = (partes.pop() ?? "") + (guardaCR ? "\r" : "");
            for (const p of partes) {
                linea(p, salida);
            }
            return salida;
        },
    };
}

/** Lo mínimo de una respuesta de `fetch` con el cuerpo por partes (`expo/fetch` y el de Node). */
export interface RespuestaFlujo {
    ok: boolean;
    status: number;
    headers: { get(nombre: string): string | null };
    body: { getReader(): { read(): Promise<{ done: boolean; value?: Uint8Array }>; cancel(): Promise<void> } } | null;
    json(): Promise<unknown>;
}

export type FetchFlujo = (url: string, init: { method: "GET"; headers: Record<string, string>; signal: AbortSignal }) => Promise<RespuestaFlujo>;

export interface OpcionesFlujoFetch {
    /** Origen de la API, sin `/` final (`https://iris.ejemplo.gt`). */
    baseUrl: string;
    /** `fetch` que entrega el cuerpo por partes: `expo/fetch` en la app nativa. */
    fetch: FetchFlujo;
    /** Cabeceras de la app en cada apertura (empresa, canal, versión; un `Authorization` si lo hay). */
    cabeceras?: () => Record<string, string>;
    sinLatidoMs?: number;
    /** Temporizador del plazo del latido (en pruebas, uno falso). */
    programar?: (fn: () => void, ms: number) => unknown;
    cancelar?: (id: unknown) => void;
}

function esProblema(v: unknown): v is Problema {
    return typeof v === "object" && v !== null && typeof (v as Problema).type === "string" && typeof (v as Problema).status === "number";
}

async function errorDe(r: RespuestaFlujo): Promise<ErrorTransporte> {
    let problema: Problema | null = null;
    try {
        const cuerpo = await r.json();
        problema = esProblema(cuerpo) ? cuerpo : null;
    } catch {
        // Sin cuerpo legible: basta el estado.
    }
    const cabeceras = new Headers();
    const retry = r.headers.get("Retry-After");
    if (retry !== null) {
        cabeceras.set("Retry-After", retry);
    }
    return new ErrorTransporte("http", problema?.title ?? `HTTP ${r.status}`, r.status, problema, cabeceras);
}

/**
 * `AbrirFlujoChat` sobre un `fetch` con el cuerpo por partes. Cada llamada abre una sola petición;
 * el motor nunca abre otra sin cerrar la anterior.
 */
export function crearFlujoFetch(o: OpcionesFlujoFetch): AbrirFlujoChat {
    const sinLatidoMs = o.sinLatidoMs ?? SIN_LATIDO_MS;
    const programar = o.programar ?? ((fn: () => void, ms: number) => setTimeout(fn, ms));
    const cancelar = o.cancelar ?? ((id: unknown) => clearTimeout(id as ReturnType<typeof setTimeout>));
    return (desde, oyentes: OyentesFlujo) => {
        const control = new AbortController();
        let cerrado = false;
        let plazo: unknown = null;
        const fin = (error?: ErrorTransporte) => {
            if (cerrado) {
                return;
            }
            cerrado = true;
            if (plazo !== null) {
                cancelar(plazo);
                plazo = null;
            }
            control.abort();
            oyentes.alCerrar(error);
        };
        const renovar = () => {
            if (plazo !== null) {
                cancelar(plazo);
            }
            plazo = programar(() => fin(), sinLatidoMs);
        };
        const url = `${o.baseUrl}${RUTA_FLUJO_CHAT}${desde ? `?desde=${encodeURIComponent(desde)}` : ""}`;
        const cabeceras: Record<string, string> = { ...o.cabeceras?.(), Accept: "text/event-stream", "Cache-Control": "no-cache" };
        if (desde) {
            cabeceras["Last-Event-ID"] = desde;
        }
        void (async () => {
            try {
                renovar();
                const r = await o.fetch(url, { method: "GET", headers: cabeceras, signal: control.signal });
                if (cerrado) {
                    return;
                }
                if (!r.ok || !r.body) {
                    fin(r.ok ? new ErrorTransporte("red", "El flujo no trajo cuerpo") : await errorDe(r));
                    return;
                }
                oyentes.alAbrir();
                renovar();
                const lector = r.body.getReader();
                const decodificador = new TextDecoder();
                const sse = crearLectorSSE();
                for (;;) {
                    const { done, value } = await lector.read();
                    if (cerrado) {
                        void lector.cancel().catch(() => undefined);
                        return;
                    }
                    if (done) {
                        break;
                    }
                    renovar();
                    for (const e of sse.agregar(decodificador.decode(value, { stream: true }))) {
                        if (!TIPOS_EVENTO.has(e.tipo)) {
                            continue;
                        }
                        let evento: EventoChat;
                        try {
                            evento = JSON.parse(e.datos) as EventoChat;
                        } catch {
                            // Un evento ilegible no rompe el flujo (y su contenido nunca se registra).
                            continue;
                        }
                        oyentes.alEvento(evento);
                    }
                }
                fin();
            } catch (e) {
                // Red cortada o abortado por `cerrar()` (en ese caso `fin` ya no avisa).
                fin(cerrado ? undefined : new ErrorTransporte("red", e instanceof Error ? e.message : "Sin conexión"));
            }
        })();
        return {
            cerrar: () => {
                if (cerrado) {
                    return;
                }
                cerrado = true;
                if (plazo !== null) {
                    cancelar(plazo);
                    plazo = null;
                }
                control.abort();
            },
        };
    };
}
