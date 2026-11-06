import { describe, expect, it } from "vitest";
import { iniciarFeed, type EstadoFeed, type OpcionesFeed, type PaginaCambios } from "./tiempo-real";

type Ev = { id: string; tipo: string };

class ErrorConEstado extends Error {
    constructor(readonly estado: number) {
        super(`HTTP ${estado}`);
    }
}

/** Respuesta SSE que entrega `bloques` y queda abierta hasta que se aborte. */
function flujoSSE(bloques: string[], senal: AbortSignal, cerrar = false): Response {
    const cuerpo = new ReadableStream<Uint8Array>({
        start(c) {
            for (const b of bloques) {
                c.enqueue(new TextEncoder().encode(b));
            }
            if (cerrar) {
                c.close();
                return;
            }
            senal.addEventListener("abort", () => c.error(new DOMException("abortado", "AbortError")));
        },
    });
    return new Response(cuerpo, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

const sse = (id: string, tipo = "mensaje_nuevo") => `id: ${id}\nevent: ${tipo}\ndata: ${JSON.stringify({ id, tipo })}\n\n`;

function hasta(condicion: () => boolean, ms = 2000): Promise<void> {
    const inicio = Date.now();
    return new Promise((resolver, rechazar) => {
        const t = setInterval(() => {
            if (condicion()) {
                clearInterval(t);
                resolver();
            } else if (Date.now() - inicio > ms) {
                clearInterval(t);
                rechazar(new Error("tiempo agotado"));
            }
        }, 2);
    });
}

const plazos = { sondeo: 5, volverAlFlujo: 40, sinLatido: 10_000, esperaMin: 5, esperaMax: 10 };

function base(sobre: Partial<OpcionesFeed<Ev>>) {
    const eventos: Ev[] = [];
    const estados: EstadoFeed[] = [];
    const opciones: OpcionesFeed<Ev> = {
        abrirFlujo: async (_c, s) => flujoSSE([], s),
        leerCambios: async (): Promise<PaginaCambios<Ev>> => ({ data: [], nextCursor: "10" }),
        verificarSesion: async () => undefined,
        leerEvento: (d) => JSON.parse(d) as Ev,
        alEvento: (e) => eventos.push(e),
        alEstado: (e) => estados.push(e),
        plazos,
        ...sobre,
    };
    return { opciones, eventos, estados };
}

describe("feed de la bandeja", () => {
    it("lee la cabeza, abre el flujo desde ella, entrega en orden y descarta repetidos", async () => {
        const cursores: (string | undefined)[] = [];
        const { opciones, eventos, estados } = base({
            abrirFlujo: async (c, s) => {
                cursores.push(c);
                return flujoSSE([sse("11"), ": latido\n\n", sse("12"), sse("11")], s);
            },
        });
        let listo = false;
        const cerrar = iniciarFeed(opciones, () => (listo = true));
        await hasta(() => eventos.length >= 2);
        cerrar();
        expect(listo).toBe(true);
        expect(cursores[0]).toBe("10");
        expect(eventos.map((e) => e.id)).toEqual(["11", "12"]);
        expect(estados).toContain("en_vivo");
    });

    it("al cortarse el flujo reconecta con el último id recibido", async () => {
        const cursores: (string | undefined)[] = [];
        const { opciones, eventos } = base({
            abrirFlujo: async (c, s) => {
                cursores.push(c);
                return cursores.length === 1 ? flujoSSE([sse("11")], s, true) : flujoSSE([sse("12")], s);
            },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => eventos.length >= 2);
        cerrar();
        expect(cursores.slice(0, 2)).toEqual(["10", "11"]);
    });

    it("410 cursor_vencido: avisa para recargar y sigue desde la cabeza nueva", async () => {
        let vencidos = 0;
        let lecturas = 0;
        const cursores: (string | undefined)[] = [];
        const { opciones, eventos } = base({
            leerCambios: async () => ({ data: [], nextCursor: lecturas++ === 0 ? "10" : "50" }),
            abrirFlujo: async (c, s) => {
                cursores.push(c);
                return c === "10" ? new Response(null, { status: 410 }) : flujoSSE([sse("51")], s);
            },
            alCursorVencido: () => vencidos++,
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => eventos.length >= 1);
        cerrar();
        expect(vencidos).toBe(1);
        expect(cursores).toEqual(["10", "50"]);
    });

    it("429 o 503: pasa al sondeo con el mismo cursor y vuelve a probar el flujo después", async () => {
        const pedidos: (string | undefined)[] = [];
        let flujos = 0;
        const { opciones, eventos, estados } = base({
            abrirFlujo: async (_c, s) => (flujos++ === 0 ? new Response(null, { status: 503 }) : flujoSSE([], s)),
            leerCambios: async (c) => {
                pedidos.push(c);
                if (c === undefined) return { data: [], nextCursor: "10" };
                if (c === "10") return { data: [{ id: "11", tipo: "x" }], nextCursor: "11", hayMas: true };
                return { data: [], nextCursor: c };
            },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => flujos >= 2);
        cerrar();
        expect(estados).toContain("sondeo");
        expect(eventos.map((e) => e.id)).toEqual(["11"]);
        expect(pedidos.slice(0, 3)).toEqual([undefined, "10", "11"]);
    });

    it("401: se detiene sin reintentar", async () => {
        let flujos = 0;
        let motivo = "";
        const { opciones, estados } = base({
            abrirFlujo: async () => {
                flujos++;
                return new Response(null, { status: 401 });
            },
            alDetenido: (m) => (motivo = m),
        });
        iniciarFeed(opciones);
        await hasta(() => motivo !== "");
        await new Promise((r) => setTimeout(r, 30));
        expect(motivo).toBe("sin_sesion");
        expect(flujos).toBe(1);
        expect(estados.at(-1)).toBe("detenido");
    });

    it("si la sesión del navegador ya es de otra persona, no abre el flujo ni entrega nada", async () => {
        let flujos = 0;
        let motivo = "";
        let verificaciones = 0;
        const ajena = new Error("sesion_ajena");
        const { opciones, eventos } = base({
            verificarSesion: async () => {
                verificaciones++;
                if (verificaciones > 1) throw ajena;
            },
            esSesionAjena: (e) => e === ajena,
            abrirFlujo: async (_c, s) => {
                flujos++;
                return flujoSSE([sse("11")], s, true);
            },
            alDetenido: (m) => (motivo = m),
        });
        iniciarFeed(opciones);
        await hasta(() => motivo !== "");
        // Primera verificación (cabeza) bien; la segunda (antes del flujo) ya es otra sesión.
        expect(motivo).toBe("sesion_cambiada");
        expect(flujos).toBe(0);
        expect(eventos).toEqual([]);
    });

    it("sin red: espera y vuelve a intentar", async () => {
        let intentos = 0;
        const { opciones, eventos, estados } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                if (intentos < 3) throw new TypeError("Failed to fetch");
                return flujoSSE([sse("11")], s);
            },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => eventos.length >= 1);
        cerrar();
        expect(estados).toContain("sin_conexion");
        expect(intentos).toBe(3);
    });

    it("un error con estado en la lectura de la cabeza se trata igual (403 detiene)", async () => {
        let motivo = "";
        const { opciones } = base({
            leerCambios: async () => {
                throw new ErrorConEstado(403);
            },
            alDetenido: (m) => (motivo = m),
        });
        iniciarFeed(opciones);
        await hasta(() => motivo !== "");
        expect(motivo).toBe("sin_permiso");
    });
});
