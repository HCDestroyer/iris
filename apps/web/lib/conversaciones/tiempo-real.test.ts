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

    it("un 410 repetido no entra en bucle: el segundo y los siguientes esperan con backoff", async () => {
        const esperas: number[] = [];
        let aperturas = 0;
        let vencidos = 0;
        const { opciones } = base({
            abrirFlujo: async () => {
                aperturas++;
                return new Response(null, { status: 410 });
            },
            alCursorVencido: () => vencidos++,
            plazos: { ...plazos, esperaMin: 5, esperaMax: 40 },
            reloj: {
                ahora: () => 0,
                esperar: async (ms) => {
                    esperas.push(ms);
                    await new Promise((r) => setTimeout(r, 1));
                },
            },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => aperturas >= 6);
        cerrar();
        // El primer 410 recarga sin esperar; desde el segundo, espera creciente.
        expect(vencidos).toBeGreaterThanOrEqual(5);
        expect(esperas.slice(0, 4)).toEqual([5, 10, 20, 40]);
        expect(esperas.length).toBeGreaterThanOrEqual(aperturas - 2);
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

    it("un flujo que se cierra al instante no entra en bucle: espera cada vez más", async () => {
        const esperas: number[] = [];
        let intentos = 0;
        const { opciones, estados } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                return flujoSSE([], s, true);
            },
            plazos: { ...plazos, esperaMin: 5, esperaMax: 40, flujoMinimo: 5_000 },
            reloj: {
                ahora: () => 0,
                esperar: async (ms) => {
                    esperas.push(ms);
                    await new Promise((r) => setTimeout(r, 1));
                },
            },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => intentos >= 6);
        cerrar();
        // Antes de cada reapertura hubo una espera, y crece hasta el máximo.
        expect(esperas.length).toBeGreaterThanOrEqual(intentos - 1);
        expect(esperas.slice(0, 5)).toEqual([5, 10, 20, 40, 40]);
        expect(estados).not.toContain("sin_conexion");
    });

    it("tras un flujo largo reconecta sin esperar", async () => {
        const esperas: number[] = [];
        let intentos = 0;
        let t = 0;
        const { opciones } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                // Como la red real: cada apertura cede un turno al bucle de eventos.
                await new Promise((r) => setTimeout(r, 1));
                return flujoSSE([], s, true);
            },
            plazos: { ...plazos, flujoMinimo: 5_000 },
            reloj: {
                // Cada lectura del reloj avanza 10 s: todos los flujos «duran».
                ahora: () => (t += 10_000),
                esperar: async (ms) => {
                    esperas.push(ms);
                    await new Promise((r) => setTimeout(r, 1));
                },
            },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => intentos >= 3);
        cerrar();
        expect(esperas).toEqual([]);
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

    /** Un flujo que entrega `bloques` y luego se corta de golpe, como cuando un proxy tira la conexión. */
    function flujoCortado(bloques: string[]): Response {
        const cuerpo = new ReadableStream<Uint8Array>({
            start(c) {
                for (const b of bloques) c.enqueue(new TextEncoder().encode(b));
                setTimeout(() => c.error(new TypeError("network error")), 3);
            },
        });
        return new Response(cuerpo, { status: 200, headers: { "Content-Type": "text/event-stream" } });
    }
    /** Reloj en el que cada lectura avanza 10 s (todo flujo «dura») y las esperas quedan anotadas. */
    function relojRapido(esperas: number[]) {
        let t = 0;
        return {
            ahora: () => (t += 10_000),
            esperar: async (ms: number) => {
                esperas.push(ms);
                await new Promise((r) => setTimeout(r, 1));
            },
        };
    }

    it("un flujo abierto que se corta de golpe reconecta al momento con el último id: sin «sin conexión», sin perder ni repetir", async () => {
        const cursores: (string | undefined)[] = [];
        const esperas: number[] = [];
        const { opciones, eventos, estados } = base({
            abrirFlujo: async (c, s) => {
                cursores.push(c);
                if (cursores.length === 1) return flujoCortado([sse("11")]);
                if (cursores.length === 2) return flujoCortado([sse("11"), sse("12")]);
                return flujoSSE([sse("12"), sse("13")], s);
            },
            reloj: relojRapido(esperas),
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => eventos.length >= 3);
        cerrar();
        expect(cursores.slice(0, 3)).toEqual(["10", "11", "12"]);
        expect(eventos.map((e) => e.id)).toEqual(["11", "12", "13"]);
        expect(estados).not.toContain("sin_conexion");
        // Reconectó sin esperar.
        expect(esperas).toEqual([]);
    });

    it("un solo intento fallido no avisa: reintenta enseguida y sigue", async () => {
        let intentos = 0;
        const esperas: number[] = [];
        const { opciones, eventos, estados } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                if (intentos === 1) throw new TypeError("Failed to fetch");
                return flujoSSE([sse("11")], s);
            },
            plazos: { ...plazos, reintentoRapido: 2, gracia: 5_000 },
            reloj: relojRapido(esperas),
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => eventos.length >= 1);
        cerrar();
        expect(estados).not.toContain("sin_conexion");
        expect(esperas).toEqual([2]);
    });

    it("un 502 de un proxy tampoco avisa a la primera; a la segunda seguida sí, con espera creciente", async () => {
        let intentos = 0;
        const esperas: number[] = [];
        const vistos: EstadoFeed[][] = [];
        const { opciones, eventos, estados } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                vistos.push([...estados]);
                if (intentos <= 3) return new Response(null, { status: 502 });
                return flujoSSE([sse("11")], s);
            },
            plazos: { ...plazos, reintentoRapido: 2, gracia: 5_000 },
            reloj: relojRapido(esperas),
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => eventos.length >= 1);
        cerrar();
        // Al abrir el 2.º intento (tras 1 fallo) aún no se había avisado; al abrir el 3.º, sí.
        expect(vistos[1]).not.toContain("sin_conexion");
        expect(vistos[2]).toContain("sin_conexion");
        expect(esperas).toEqual([2, 5, 10]);
        expect(estados.at(-1)).toBe("en_vivo");
    });

    it("si el reintento se queda colgado, avisa al pasar la gracia", async () => {
        let intentos = 0;
        const { opciones, estados } = base({
            abrirFlujo: (_c, s) => {
                intentos++;
                if (intentos === 1) return Promise.reject(new TypeError("Failed to fetch"));
                // Ni responde ni falla (un proxy que no contesta).
                return new Promise<Response>((_r, rechazar) => s.addEventListener("abort", () => rechazar(new DOMException("abortado", "AbortError"))));
            },
            plazos: { ...plazos, reintentoRapido: 1, gracia: 30 },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => intentos >= 2);
        expect(estados).not.toContain("sin_conexion");
        await hasta(() => estados.includes("sin_conexion"));
        cerrar();
    });

    it("al volver el flujo los fallos se olvidan: fallos sueltos entre flujos buenos nunca avisan", async () => {
        let intentos = 0;
        const { opciones, estados } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                await new Promise((r) => setTimeout(r, 1));
                if (intentos % 2 === 1) throw new TypeError("Failed to fetch");
                return intentos >= 8 ? flujoSSE([], s) : flujoCortado([]);
            },
            plazos: { ...plazos, reintentoRapido: 1, gracia: 5_000 },
            reloj: relojRapido([]),
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => intentos >= 8);
        cerrar();
        expect(estados).not.toContain("sin_conexion");
    });

    it("un flujo que abre y se corta de golpe al poco, una y otra vez: avisa desde el segundo corte y lo quita cuando uno dura", async () => {
        let intentos = 0;
        const vistos: EstadoFeed[][] = [];
        const { opciones, estados } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                vistos.push([...estados]);
                // Tres que se cortan a los 3 ms; el cuarto se sostiene.
                return intentos <= 3 ? flujoCortado([": latido\n\n"]) : flujoSSE([": latido\n\n"], s);
            },
            plazos: { ...plazos, flujoMinimo: 60 },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => intentos >= 4);
        // Tras el primer corte no se había dicho nada; tras el segundo, sí.
        expect(vistos[1]).not.toContain("sin_conexion");
        expect(vistos[2]).toContain("sin_conexion");
        // El cuarto abre, pero no se da por bueno hasta que dura.
        expect(estados.at(-1)).toBe("sin_conexion");
        await hasta(() => estados.at(-1) === "en_vivo");
        cerrar();
        // No parpadeó entre corte y corte.
        expect(estados.filter((e) => e === "sin_conexion")).toHaveLength(1);
    });

    it("los cierres limpios al poco de abrir (el servidor cierra bien) siguen sin avisar", async () => {
        let intentos = 0;
        const { opciones, estados } = base({
            abrirFlujo: async (_c, s) => {
                intentos++;
                return flujoSSE([": latido\n\n"], s, true);
            },
        });
        const cerrar = iniciarFeed(opciones);
        await hasta(() => intentos >= 4);
        cerrar();
        expect(estados).not.toContain("sin_conexion");
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
