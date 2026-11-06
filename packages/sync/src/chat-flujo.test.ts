// Flujo SSE del chat en la app nativa (`crearFlujoFetch` sobre `expo/fetch`): parseo, cabeceras y
// `Last-Event-ID`, latido, 401/403 que detienen, 429 que espera, cierre en segundo plano y el paso al
// sondeo. El servidor es un `fetch` falso que entrega el cuerpo por partes (ReadableStream).
import { afterEach, describe, expect, it } from "vitest";
import { AlmacenChatMemoria } from "./chat/almacen-memoria";
import { crearFlujoFetch, crearLectorSSE, type FetchFlujo, type RespuestaFlujo } from "./chat/flujo-fetch";
import { MotorChat } from "./chat/motor";
import type { EventoChat, OyentesFlujo } from "./chat/tipos";
import type { ErrorTransporte } from "./errores";
import { ServidorChatFalso } from "./pruebas/chat-falso";
import { RelojManual, vaciarPromesas } from "./pruebas/reloj-manual";

const YO = 1;
const ANDREA = { id: 7, nombre: "Andrea Castillo" };
const codificar = (t: string) => new TextEncoder().encode(t);

/** Un flujo abierto del `fetch` falso: se le empujan trozos y se puede cerrar desde el servidor. */
interface FlujoFalso {
    url: string;
    cabeceras: Record<string, string>;
    senal: AbortSignal;
    empujar(texto: string): void;
    terminar(): void;
}

function fetchFalso(responder?: (url: string) => { status: number; cuerpo?: unknown; cabeceras?: Record<string, string> } | null) {
    const flujos: FlujoFalso[] = [];
    const fetch: FetchFlujo = async (url, init) => {
        const r = responder?.(url);
        if (r) {
            const cuerpo = JSON.stringify(r.cuerpo ?? {});
            const respuesta: RespuestaFlujo = {
                ok: r.status >= 200 && r.status < 300,
                status: r.status,
                headers: new Headers(r.cabeceras ?? {}),
                body: null,
                json: async () => JSON.parse(cuerpo) as unknown,
            };
            return respuesta;
        }
        let controlador!: ReadableStreamDefaultController<Uint8Array>;
        const body = new ReadableStream<Uint8Array>({
            start(c) {
                controlador = c;
            },
        });
        const f: FlujoFalso = {
            url,
            cabeceras: init.headers,
            senal: init.signal,
            empujar: (t) => controlador.enqueue(codificar(t)),
            terminar: () => controlador.close(),
        };
        init.signal.addEventListener("abort", () => {
            try {
                controlador.error(new DOMException("abortado", "AbortError"));
            } catch {
                // Ya cerrado.
            }
        });
        flujos.push(f);
        return { ok: true, status: 200, headers: new Headers(), body, json: async () => ({}) } as RespuestaFlujo;
    };
    return { fetch, flujos, abiertos: () => flujos.filter((f) => !f.senal.aborted) };
}

/** Temporizador manual para el plazo del latido. */
function temporizador() {
    const pendientes = new Map<number, () => void>();
    let siguiente = 0;
    return {
        programar: (fn: () => void) => {
            siguiente += 1;
            pendientes.set(siguiente, fn);
            return siguiente;
        },
        cancelar: (id: unknown) => {
            pendientes.delete(id as number);
        },
        vencer: () => {
            for (const [id, fn] of [...pendientes]) {
                pendientes.delete(id);
                fn();
            }
        },
        cuantos: () => pendientes.size,
    };
}

function oyentes() {
    const eventos: EventoChat[] = [];
    const registro = { abierto: 0, cerrado: 0, error: null as ErrorTransporte | null, eventos };
    const o: OyentesFlujo = {
        alAbrir: () => {
            registro.abierto += 1;
        },
        alEvento: (e) => eventos.push(e),
        alCerrar: (error) => {
            registro.cerrado += 1;
            registro.error = error ?? null;
        },
    };
    return { o, registro };
}

const eventoTexto = (id: string, tipo = "leido") => `id: ${id}\nevent: ${tipo}\ndata: ${JSON.stringify({ id, tipo, conversacion_id: 12, ocurrido_en: "2026-11-05T17:00:00Z" })}\n\n`;

describe("lector SSE", () => {
    it("arma eventos de trozos partidos, con CRLF, varias líneas de data y comentarios", () => {
        const l = crearLectorSSE();
        expect(l.agregar("retry: 3000\n\n: latido\n\nid: 4\nev")).toEqual([]);
        expect(l.agregar("ent: leido\r\ndata: {\"a\":\r")).toEqual([]);
        expect(l.agregar("\ndata: 1}\n\n")).toEqual([{ id: "4", tipo: "leido", datos: "{\"a\":\n1}" }]);
        expect(l.agregar("data: sin tipo\n\n")).toEqual([{ id: undefined, tipo: "message", datos: "sin tipo" }]);
    });
});

describe("crearFlujoFetch", () => {
    it("abre con las cabeceras de la app, Last-Event-ID y ?desde=, y entrega los eventos conocidos", async () => {
        const f = fetchFalso();
        const t = temporizador();
        const abrir = crearFlujoFetch({ baseUrl: "https://iris.prueba", fetch: f.fetch, cabeceras: () => ({ "X-Iris-Empresa": "3", "X-Iris-Canal": "campo" }), programar: t.programar, cancelar: t.cancelar });
        const { o, registro } = oyentes();
        abrir("41", o);
        await vaciarPromesas(5);
        const flujo = f.flujos[0];
        expect(flujo?.url).toBe("https://iris.prueba/api/v1/mensajeria/chat/eventos?desde=41");
        expect(flujo?.cabeceras).toMatchObject({ "X-Iris-Empresa": "3", "X-Iris-Canal": "campo", "Last-Event-ID": "41", Accept: "text/event-stream" });
        expect(registro.abierto).toBe(1);
        flujo?.empujar(": latido\n\n" + eventoTexto("42") + "id: 43\nevent: otro\ndata: {}\n\n" + "id: 44\nevent: leido\ndata: {roto\n\n");
        await vaciarPromesas(5);
        expect(registro.eventos.map((e) => e.id)).toEqual(["42"]);
        flujo?.terminar();
        await vaciarPromesas(5);
        expect(registro.cerrado).toBe(1);
        expect(registro.error).toBeNull();
    });

    it("401 y 403 cierran con el error (el motor se detiene); 429 trae su Retry-After", async () => {
        for (const status of [401, 403, 429]) {
            const f = fetchFalso(() => ({ status, cuerpo: { type: "urn:iris:problema:x", title: "No", status }, cabeceras: status === 429 ? { "Retry-After": "20" } : {} }));
            const { o, registro } = oyentes();
            crearFlujoFetch({ baseUrl: "https://iris.prueba", fetch: f.fetch })(null, o);
            await vaciarPromesas(5);
            expect(registro.abierto).toBe(0);
            expect(registro.error?.status).toBe(status);
            if (status === 429) {
                expect(registro.error?.cabeceras?.get("Retry-After")).toBe("20");
            }
        }
    });

    it("sin latido en el plazo corta y avisa; cada trozo renueva el plazo", async () => {
        const f = fetchFalso();
        const t = temporizador();
        const { o, registro } = oyentes();
        crearFlujoFetch({ baseUrl: "https://iris.prueba", fetch: f.fetch, programar: t.programar, cancelar: t.cancelar })(null, o);
        await vaciarPromesas(5);
        f.flujos[0]?.empujar(": latido\n\n");
        await vaciarPromesas(5);
        expect(t.cuantos()).toBe(1);
        t.vencer();
        await vaciarPromesas(5);
        expect(registro.cerrado).toBe(1);
        expect(f.flujos[0]?.senal.aborted).toBe(true);
    });

    it("cerrar() aborta la petición sin avisar alCerrar", async () => {
        const f = fetchFalso();
        const { o, registro } = oyentes();
        const flujo = crearFlujoFetch({ baseUrl: "https://iris.prueba", fetch: f.fetch })(null, o);
        await vaciarPromesas(5);
        flujo.cerrar();
        await vaciarPromesas(5);
        expect(f.flujos[0]?.senal.aborted).toBe(true);
        expect(registro.cerrado).toBe(0);
    });
});

describe("motor del chat con el flujo de la app nativa", () => {
    const abiertos: MotorChat[] = [];
    afterEach(() => {
        for (const m of abiertos.splice(0)) {
            m.detener();
        }
    });

    async function montar(f: ReturnType<typeof fetchFalso>) {
        const reloj = new RelojManual("2026-11-05T17:00:00Z");
        const servidor = new ServidorChatFalso(YO);
        servidor.ahora = () => reloj.ahora();
        servidor.crearConversacion({ id: 12, tipo: "orden", titulo: "OT-000102", orden_id: 102, orden_estado: "en_sitio" });
        const motor = await MotorChat.abrir({
            almacen: new AlmacenChatMemoria(),
            transporte: servidor,
            usuarioId: YO,
            reloj,
            espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0.5 },
            abrirFlujo: crearFlujoFetch({ baseUrl: "https://iris.prueba", fetch: f.fetch, programar: () => 0, cancelar: () => undefined }),
        });
        abiertos.push(motor);
        motor.iniciar();
        await vaciarPromesas(20);
        return { motor, servidor, reloj };
    }

    it("un mensaje por el flujo llega al momento y se ve «sse»; una sola conexión", async () => {
        const f = fetchFalso();
        const { motor, servidor } = await montar(f);
        expect(motor.obtenerEstado().tiempoReal).toBe("sse");
        expect(f.abiertos()).toHaveLength(1);
        const m = servidor.recibir(12, ANDREA, "voy en camino");
        const evento = servidor.eventos.at(-1);
        expect(evento?.id).toBeDefined();
        f.flujos[0]?.empujar(`id: ${evento?.id}\nevent: mensaje_nuevo\ndata: ${JSON.stringify(evento)}\n\n`);
        await vaciarPromesas(20);
        expect(motor.obtenerEstado().conversaciones.find((c) => c.id === 12)?.ultimo_mensaje?.id).toBe(m.id);
        expect(f.abiertos()).toHaveLength(1);
    });

    it("segundo plano cierra la conexión y primer plano abre otra desde el cursor", async () => {
        const f = fetchFalso();
        const { motor } = await montar(f);
        motor.segundoPlano();
        await vaciarPromesas(10);
        expect(f.abiertos()).toHaveLength(0);
        expect(motor.obtenerEstado().tiempoReal).toBe("detenido");
        motor.primerPlano();
        await vaciarPromesas(20);
        expect(f.abiertos()).toHaveLength(1);
        expect(f.flujos.at(-1)?.cabeceras["Last-Event-ID"]).toBe("0");
    });

    it("401 al abrir el flujo detiene el tiempo real (no reintenta ni sondea)", async () => {
        const f = fetchFalso(() => ({ status: 401, cuerpo: { type: "urn:iris:problema:sin_sesion", title: "Sin sesión", status: 401 } }));
        const { motor, servidor, reloj } = await montar(f);
        const llamadas = servidor.llamadasA("cambios").length;
        expect(motor.obtenerEstado().tiempoReal).toBe("detenido");
        await reloj.avanzar(60_000);
        await vaciarPromesas(10);
        expect(f.flujos).toHaveLength(0);
        expect(servidor.llamadasA("cambios").length).toBe(llamadas);
    });

    it("un flujo que se cierra al instante reconecta con espera creciente y, tras 3, sondea", async () => {
        const f = fetchFalso();
        const { motor, reloj } = await montar(f);
        const cerrarUltimo = async () => {
            f.flujos.at(-1)?.terminar();
            await vaciarPromesas(20);
        };
        await cerrarUltimo();
        await reloj.avanzar(2999);
        await vaciarPromesas(10);
        expect(f.flujos).toHaveLength(1);
        await reloj.avanzar(1);
        await vaciarPromesas(20);
        expect(f.flujos).toHaveLength(2);
        await cerrarUltimo();
        await reloj.avanzar(6000);
        await vaciarPromesas(20);
        expect(f.flujos).toHaveLength(3);
        await cerrarUltimo();
        await reloj.avanzar(12_000);
        await vaciarPromesas(20);
        expect(f.flujos).toHaveLength(3);
        expect(motor.obtenerEstado().tiempoReal).toBe("sondeo");
    });

    it("sin red el flujo cae y el sondeo pone al día", async () => {
        const f = fetchFalso();
        const { motor, servidor, reloj } = await montar(f);
        servidor.recibir(12, ANDREA, "mientras se cortaba");
        f.flujos[0]?.senal.dispatchEvent(new Event("abort"));
        await vaciarPromesas(20);
        await reloj.avanzar(3000);
        await vaciarPromesas(20);
        expect(motor.obtenerEstado().conversaciones.find((c) => c.id === 12)?.no_leidos).toBe(1);
    });
});
