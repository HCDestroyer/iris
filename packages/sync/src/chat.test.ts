// Motor del chat (`contratos/chat.md` §3–§5): cola, reintentos, orden, deduplicación, flujo SSE con
// reanudación, sondeo, 410, 401, fotos y la lógica de pantalla compartida.
/// <reference types="node" />
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync, type SQLInputValue } from "node:sqlite";
import { IDBKeyRange, indexedDB } from "fake-indexeddb";
import { afterAll, afterEach, describe, expect, it } from "vitest";
import { AlmacenDexie } from "./almacen-dexie";
import { AlmacenSqlite, type ConexionSqlite, type ValorSqlite } from "./almacen-sqlite";
import { AlmacenChatMemoria } from "./chat/almacen-memoria";
import { ErrorChatLocal, MotorChat, type OpcionesMotorChat } from "./chat/motor";
import type { AlmacenChat, MensajeChat, SalidaChat } from "./chat/tipos";
import { elementosHilo, estadoEnvio, etiquetaDia, horaChat, horaLista, puedeEliminar, segmentosTexto, textoValido, vistaPrevia } from "./chat/vista";
import { pestanaDe, urlDeVista, vistaDeUrl } from "./campo/vista";
import { errorHttp, errorRed, ServidorChatFalso } from "./pruebas/chat-falso";
import { RelojManual, vaciarPromesas } from "./pruebas/reloj-manual";
import { RE_UUID7 } from "./uuid7";

const YO = 5;

/** El valor, o falla la prueba si no está. */
function hay<T>(v: T | null | undefined): T {
    if (v === null || v === undefined) {
        throw new Error("falta un valor esperado");
    }
    return v;
}
const ANDREA = { id: 7, nombre: "Andrea Castillo" };

const abiertos: MotorChat[] = [];
afterEach(() => {
    for (const m of abiertos.splice(0)) {
        m.detener();
    }
});

async function montar(o: Partial<OpcionesMotorChat> & { conFlujo?: boolean } = {}) {
    const reloj = new RelojManual("2026-11-05T17:00:00Z");
    const servidor = new ServidorChatFalso(YO);
    servidor.ahora = () => reloj.ahora();
    const almacen = o.almacen ?? new AlmacenChatMemoria();
    const conv = servidor.crearConversacion({ id: 12, tipo: "orden", titulo: "OT-000102", orden_id: 102, orden_estado: "en_sitio" });
    const motor = await MotorChat.abrir({
        almacen,
        transporte: servidor,
        subidor: servidor.subidor,
        usuarioId: YO,
        reloj,
        espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0.5 },
        ...(o.conFlujo ? { abrirFlujo: servidor.abrirFlujo } : {}),
        ...o,
    });
    abiertos.push(motor);
    return { reloj, servidor, almacen, motor, conv };
}

/** Arranca y deja correr el arranque del feed (cabeza + lista). */
async function arrancar(motor: MotorChat) {
    motor.iniciar();
    await vaciarPromesas(10);
}

const textos = (motor: MotorChat, id = 12) => motor.obtenerConversacion(id).mensajes.map((m) => m.texto);

describe("arranque (§5.1)", () => {
    it("toma la cabeza del feed sin cursor, lee la lista y abre los hilos del día", async () => {
        const { motor, servidor } = await montar();
        motor.fijarDelDia({ ordenes: [102, 103], cuadrillas: [3] });
        await arrancar(motor);
        expect(servidor.llamadasA("cambios")[0]).toEqual([null]);
        expect(servidor.llamadasA("listarConversaciones")).toHaveLength(1);
        expect(servidor.llamadasA("abrirCuadrilla")).toEqual([[3]]);
        expect(servidor.llamadasA("abrirOrden")).toEqual([[102], [103]]);
        const titulos = motor.obtenerEstado().conversaciones.map((c) => c.titulo);
        expect(titulos).toEqual(expect.arrayContaining(["OT-000102", "OT-000103", "Cuadrilla 3"]));
        expect(motor.obtenerEstado().lista.estado).toBe("lista");
    });
});

describe("escribir y la cola (§3, §5.2–§5.3)", () => {
    it("guarda primero con id uuid v7 y creado_en, lo muestra «enviando» y luego con su seq", async () => {
        const { motor, servidor, almacen } = await montar();
        await arrancar(motor);
        motor.establecerConexion(false);
        const s = await motor.escribir(12, { texto: "  La ONU tiene LOS en rojo.  " });
        expect(s.id).toMatch(RE_UUID7);
        expect((almacen as AlmacenChatMemoria).salida.get(s.id)?.texto).toBe("La ONU tiene LOS en rojo.");
        const local = motor.obtenerConversacion(12).mensajes.at(-1);
        expect(local).toMatchObject({ seq: null, propio: true, envio: "sin_senal" });
        expect(estadoEnvio(hay(local))).toEqual({ texto: "se envía al tener señal", tono: "warning" });
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(0);

        motor.establecerConexion(true);
        await vaciarPromesas(10);
        const [[conv, cuerpo]] = servidor.llamadasA("enviarMensaje") as [[number, { id: string; creado_en: string; texto: string }]];
        expect(conv).toBe(12);
        expect(cuerpo).toMatchObject({ id: s.id, creado_en: s.creadoEn, texto: "La ONU tiene LOS en rojo." });
        expect(motor.obtenerEstado().pendientes).toBe(0);
        expect(motor.obtenerEstado().conversaciones[0]?.ultimo_mensaje?.texto).toBe("La ONU tiene LOS en rojo.");
    });

    it("no guarda nada si la escritura del teléfono falla", async () => {
        const { motor, almacen } = await montar();
        await motor.abrirOrden(102);
        (almacen as AlmacenChatMemoria).fallarProxima = new Error("QuotaExceededError");
        await expect(motor.escribir(12, { texto: "hola" })).rejects.toThrow("QuotaExceededError");
        expect(motor.obtenerEstado().pendientes).toBe(0);
    });

    it("valida antes de encolar: vacío, más de 4000 caracteres y ubicación imposible", async () => {
        const { motor } = await montar();
        await motor.abrirOrden(102);
        await expect(motor.escribir(12, { texto: "   " })).rejects.toBeInstanceOf(ErrorChatLocal);
        await expect(motor.escribir(12, { texto: "x".repeat(4001) })).rejects.toMatchObject({ codigo: "texto_largo" });
        await expect(motor.escribir(12, { ubicacion: { lat: 120, lon: 0 } })).rejects.toMatchObject({ codigo: "ubicacion_invalida" });
        expect(textoValido("  hola ").valido).toBe(true);
        expect(textoValido("x".repeat(4001)).motivo).toMatch(/4000/);
    });

    it("sobrevive a cerrar la app: otro motor con el mismo almacén lo envía", async () => {
        const almacen = new AlmacenChatMemoria();
        const a = await montar({ almacen });
        await arrancar(a.motor);
        a.motor.establecerConexion(false);
        await a.motor.escribir(12, { texto: "uno" });
        a.motor.detener();

        const b = await MotorChat.abrir({ almacen, transporte: a.servidor, usuarioId: YO, reloj: a.reloj });
        abiertos.push(b);
        expect(b.obtenerConversacion(12).mensajes.map((m) => [m.texto, m.envio])).toEqual([["uno", "enviando"]]);
        await arrancar(b);
        expect(a.servidor.llamadasA("enviarMensaje")).toHaveLength(1);
        expect(b.obtenerEstado().pendientes).toBe(0);
    });

    it("sale en orden de creación, uno a uno; el servidor asigna seq en ese orden", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        motor.establecerConexion(false);
        for (const t of ["uno", "dos", "tres"]) {
            await motor.escribir(12, { texto: t });
        }
        motor.establecerConexion(true);
        await vaciarPromesas(15);
        expect(servidor.llamadasA("enviarMensaje").map(([, c]) => (c as { texto: string }).texto)).toEqual(["uno", "dos", "tres"]);
        const mensajes = motor.obtenerConversacion(12).mensajes;
        expect(mensajes.map((m) => [m.texto, m.seq])).toEqual([
            ["uno", 1],
            ["dos", 2],
            ["tres", 3],
        ]);
    });

    it("el pendiente va al final aunque lleguen mensajes de otros con seq", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        motor.establecerConexion(false);
        await motor.escribir(12, { texto: "mío sin señal" });
        await motor.aplicarEventos([servidor.recibir(12, ANDREA, "¿llegaste?") && hay(servidor.eventos.at(-1))]);
        expect(textos(motor)).toEqual(["¿llegaste?", "mío sin señal"]);
    });
});

describe("reintentos (§3)", () => {
    it("sin red y 5xx: espera exponencial y reenvía el mismo id", async () => {
        const { motor, servidor, reloj } = await montar();
        await arrancar(motor);
        servidor.fallar("enviarMensaje", errorRed());
        servidor.fallar("enviarMensaje", errorHttp(503, "no_disponible"));
        const s = await motor.escribir(12, { texto: "hola" });
        await vaciarPromesas(10);
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(1);
        expect(motor.obtenerConversacion(12).mensajes.at(-1)?.envio).toBe("enviando");
        await reloj.avanzar(1000);
        await vaciarPromesas(10);
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(2);
        await reloj.avanzar(2000);
        await vaciarPromesas(10);
        const ids = servidor.llamadasA("enviarMensaje").map(([, c]) => (c as { id: string }).id);
        expect(ids).toEqual([s.id, s.id, s.id]);
        expect(motor.obtenerEstado().pendientes).toBe(0);
    });

    it("429: espera `Retry-After` y sigue con el mismo id", async () => {
        const { motor, servidor, reloj } = await montar();
        await arrancar(motor);
        servidor.fallar("enviarMensaje", errorHttp(429, "demasiados_mensajes", { "Retry-After": "7" }));
        const s = await motor.escribir(12, { texto: "hola" });
        await vaciarPromesas(10);
        await reloj.avanzar(6000);
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(1);
        await reloj.avanzar(1000);
        await vaciarPromesas(10);
        expect(servidor.llamadasA("enviarMensaje").map(([, c]) => (c as { id: string }).id)).toEqual([s.id, s.id]);
    });

    it.each([
        [403, "sin_permiso"],
        [404, "no_encontrado"],
        [422, "mensaje_vacio"],
    ])("%i no se reintenta: «no enviado» hasta «Reintentar»", async (status, codigo) => {
        const { motor, servidor, reloj } = await montar();
        await arrancar(motor);
        servidor.fallar("enviarMensaje", errorHttp(status, codigo));
        const s = await motor.escribir(12, { texto: "hola" });
        await vaciarPromesas(10);
        await reloj.avanzar(10 * 60_000);
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(1);
        const m = motor.obtenerConversacion(12).mensajes.at(-1);
        expect(m).toMatchObject({ envio: "fallido", error: { status, codigo } });
        expect(estadoEnvio(hay(m))?.texto).toBe("no enviado");
        expect(motor.obtenerEstado().conversaciones.find((c) => c.id === 12)?.fallidos).toBe(1);

        await motor.reintentar(s.id);
        await vaciarPromesas(10);
        expect(servidor.llamadasA("enviarMensaje").map(([, c]) => (c as { id: string }).id)).toEqual([s.id, s.id]);
        expect(motor.obtenerEstado().fallidos).toBe(0);
    });

    it("409 `mensaje_id_reutilizado`: no se reintenta solo; «Reintentar» lo manda con un id nuevo", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.fallar("enviarMensaje", errorHttp(409, "mensaje_id_reutilizado"));
        const s = await motor.escribir(12, { texto: "hola" });
        await vaciarPromesas(10);
        expect(motor.obtenerEstado().fallidos).toBe(1);
        await motor.reintentar(s.id);
        await vaciarPromesas(10);
        const ids = servidor.llamadasA("enviarMensaje").map(([, c]) => (c as { id: string }).id);
        expect(ids).toHaveLength(2);
        expect(ids[1]).not.toBe(s.id);
        expect(ids[1]).toMatch(RE_UUID7);
    });

    it("401: se detiene todo con la cola intacta", async () => {
        const { motor, servidor, reloj } = await montar();
        await arrancar(motor);
        servidor.fallar("enviarMensaje", errorHttp(401, "no_autenticado"));
        await motor.escribir(12, { texto: "hola" });
        await vaciarPromesas(10);
        await reloj.avanzar(60 * 60_000);
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(1);
        expect(motor.obtenerEstado()).toMatchObject({ sesionCerrada: true, pendientes: 1, tiempoReal: "detenido" });
    });

    it("sesión ajena (otra persona entró en otra pestaña) segundos después de verificar: ni envía ni guarda lo que lee", async () => {
        let ajena = false;
        const verificaciones: number[] = [];
        const { motor, servidor, reloj, almacen } = await montar({
            verificarIdentidad: async () => {
                verificaciones.push(reloj.ahora());
                if (ajena) {
                    throw errorHttp(409, "sesion_ajena");
                }
            },
        });
        await arrancar(motor);
        expect(verificaciones.length).toBeGreaterThan(0);
        // Un segundo después (dentro de la ventana de 60 s que había antes) entra B.
        await reloj.avanzar(1000);
        ajena = true;
        await motor.escribir(12, { texto: "de A" });
        await vaciarPromesas(10);
        await reloj.avanzar(5 * 60_000);
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(0);
        expect(motor.obtenerEstado().pendientes).toBe(1);
        // Lo que el servidor tenga con la cookie de B no se pide ni se guarda en la base de A.
        servidor.recibir(12, ANDREA, "para B");
        const antes = servidor.llamadasA("cambios").length;
        await motor.recargar();
        await reloj.avanzar(5 * 60_000);
        expect(servidor.llamadasA("cambios")).toHaveLength(antes);
        expect(textos(motor)).not.toContain("para B");
        expect((await almacen.cargarChat()).mensajes.map((m) => m.texto)).not.toContain("para B");
    });

    it("sesión ajena con el flujo abierto: el evento que llega no se guarda y el flujo se cierra", async () => {
        let ajena = false;
        const { motor, servidor, almacen } = await montar({
            conFlujo: true,
            verificarIdentidad: async () => {
                if (ajena) {
                    throw errorHttp(409, "sesion_ajena");
                }
            },
        });
        await arrancar(motor);
        await motor.cargarConversacion(12);
        expect(motor.obtenerEstado().tiempoReal).toBe("sse");
        ajena = true;
        servidor.recibir(12, ANDREA, "por el flujo");
        await vaciarPromesas(10);
        expect(textos(motor)).not.toContain("por el flujo");
        expect((await almacen.cargarChat()).mensajes.map((m) => m.texto)).not.toContain("por el flujo");
        expect(servidor.flujosAbiertos).toBe(0);
    });

    it("descartar un no enviado lo quita de la cola", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.fallar("enviarMensaje", errorHttp(422, "mensaje_vacio"));
        const s = await motor.escribir(12, { texto: "hola" });
        await vaciarPromesas(10);
        await motor.descartar(s.id);
        expect(motor.obtenerEstado().pendientes).toBe(0);
        expect(textos(motor)).toEqual([]);
    });
});

describe("deduplicación y orden por seq (§4)", () => {
    it("el mismo evento dos veces deja un solo mensaje; se ordena por seq, no por llegada", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        servidor.recibir(12, ANDREA, "uno");
        servidor.recibir(12, ANDREA, "dos");
        const [e1, e2] = servidor.eventos;
        await motor.aplicarEventos([hay(e2), hay(e1), hay(e2)]);
        expect(textos(motor)).toEqual(["uno", "dos"]);
        expect(motor.obtenerEstado().conversaciones[0]?.no_leidos).toBe(2);
    });

    it("el eco propio por el feed antes de la respuesta no duplica la burbuja", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        motor.establecerConexion(false);
        const s = await motor.escribir(12, { texto: "hola" });
        const eco: MensajeChat = { id: s.id, conversacion_id: 12, seq: 1, autor_id: YO, autor_nombre: "Yo", texto: "hola", creado_en: s.creadoEn, enviado_en: "2026-11-05T17:00:01Z", eliminado: false, eliminado_en: null, menciones: [] };
        await motor.aplicarEventos([{ id: "9", tipo: "mensaje_nuevo", conversacion_id: 12, no_leidos: 0, menciones: 0, ocurrido_en: eco.enviado_en, mensaje: eco }]);
        expect(motor.obtenerConversacion(12).mensajes.map((m) => [m.texto, m.seq, m.envio])).toEqual([["hola", 1, "enviado"]]);
        void servidor;
    });

    it("un mensaje viejo que llega ya eliminado se muestra como lápida", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        const m = servidor.recibir(12, ANDREA, "secreto");
        const lapida = { ...m, texto: null, eliminado: true, eliminado_en: m.enviado_en };
        await motor.aplicarEventos([{ id: "5", tipo: "mensaje_nuevo", conversacion_id: 12, no_leidos: 0, menciones: 0, ocurrido_en: m.enviado_en, mensaje: lapida }]);
        expect(motor.obtenerConversacion(12).mensajes[0]).toMatchObject({ eliminado: true, texto: null });
        expect(motor.obtenerEstado().conversaciones[0]?.ultimo_mensaje).toMatchObject({ contenido: "eliminado", texto: null });
    });
});

describe("tiempo real: SSE (§4)", () => {
    it("abre el flujo desde el cursor, aplica eventos y al cerrarse reanuda desde el último id", async () => {
        const { motor, servidor, reloj } = await montar({ conFlujo: true });
        await arrancar(motor);
        await motor.cargarConversacion(12);
        expect(motor.obtenerEstado().tiempoReal).toBe("sse");
        const primero = servidor.flujos[0];
        expect(primero?.desde).toBe("0");
        servidor.recibir(12, ANDREA, "uno");
        await vaciarPromesas();
        expect(textos(motor)).toEqual(["uno"]);

        servidor.cortarFlujos();
        // Mientras está cerrado llegan dos más: el sondeo al cerrar o el flujo nuevo los trae una vez.
        servidor.recibir(12, ANDREA, "dos");
        servidor.recibir(12, ANDREA, "tres");
        await vaciarPromesas(10);
        await reloj.avanzar(3000);
        await vaciarPromesas(10);
        expect(servidor.flujosAbiertos).toBe(1);
        expect(servidor.flujos.at(-1)?.desde).toBe("3");
        expect(textos(motor)).toEqual(["uno", "dos", "tres"]);
    });

    it("nunca abre un segundo flujo sin cerrar el anterior; segundo plano lo cierra y primer plano reanuda", async () => {
        const { motor, servidor } = await montar({ conFlujo: true });
        await arrancar(motor);
        motor.primerPlano();
        await vaciarPromesas(10);
        expect(servidor.flujosAbiertos).toBe(1);
        motor.segundoPlano();
        await vaciarPromesas(10);
        expect(servidor.flujosAbiertos).toBe(0);
        expect(motor.obtenerEstado().tiempoReal).toBe("detenido");
        servidor.recibir(12, ANDREA, "mientras");
        motor.primerPlano();
        await vaciarPromesas(10);
        expect(servidor.flujosAbiertos).toBe(1);
        expect(servidor.flujos.at(-1)?.desde).toBe("0");
    });

    it("flujos cortos (< 5 s) esperan el doble cada vez y tras 3 pasan al sondeo antes de volver a probar", async () => {
        const { motor, servidor, reloj } = await montar({
            abrirFlujo: (_desde, oyentes) => {
                servidor.llamadas.push({ metodo: "cambios", args: ["flujo-fallido"] });
                queueMicrotask(() => oyentes.alCerrar());
                return { cerrar: () => undefined };
            },
        });
        await arrancar(motor);
        const intentos = () => servidor.llamadas.filter((l) => l.args[0] === "flujo-fallido").length;
        expect(intentos()).toBe(1);
        // 3 s tras el primero, 6 s tras el segundo (nunca un bucle de reconexión).
        await reloj.avanzar(3000);
        await vaciarPromesas(10);
        expect(intentos()).toBe(2);
        await reloj.avanzar(5000);
        await vaciarPromesas(10);
        expect(intentos()).toBe(2);
        await reloj.avanzar(1000);
        await vaciarPromesas(10);
        expect(intentos()).toBe(3);
        // Tercero corto seguido: el flujo queda bloqueado unos minutos y se sondea.
        await reloj.avanzar(12_000);
        await vaciarPromesas(10);
        expect(motor.obtenerEstado().tiempoReal).toBe("sondeo");
        await reloj.avanzar(12_000);
        await vaciarPromesas(10);
        expect(intentos()).toBe(3);
    });
});

describe("tiempo real: sondeo (§4)", () => {
    it("sin flujo sondea `/cambios` cada 12 s con el cursor guardado", async () => {
        const { motor, servidor, reloj } = await montar();
        await arrancar(motor);
        expect(motor.obtenerEstado().tiempoReal).toBe("sondeo");
        servidor.recibir(12, ANDREA, "hola");
        await reloj.avanzar(11_000);
        expect(servidor.llamadasA("cambios").at(-1)).toEqual(["0"]);
        await reloj.avanzar(1000);
        await vaciarPromesas(10);
        expect(servidor.llamadasA("cambios").at(-1)).toEqual(["0"]);
        expect(motor.obtenerEstado().conversaciones[0]?.no_leidos).toBe(1);
        await reloj.avanzar(12_000);
        await vaciarPromesas(10);
        expect(servidor.llamadasA("cambios").at(-1)).toEqual(["1"]);
    });

    it("410 `cursor_vencido`: borra la caché (no la cola), recarga y sigue desde la cabeza", async () => {
        const almacen = new AlmacenChatMemoria();
        const { motor, servidor, reloj } = await montar({ almacen });
        await arrancar(motor);
        await motor.cargarConversacion(12);
        servidor.recibir(12, ANDREA, "viejo");
        motor.establecerConexion(false);
        await motor.escribir(12, { texto: "en cola" });
        servidor.retencion = 5;
        motor.establecerConexion(true);
        await reloj.avanzar(12_000);
        await vaciarPromesas(15);
        const nulos = servidor.llamadasA("cambios").filter(([c]) => c === null);
        expect(nulos).toHaveLength(2);
        expect(almacen.meta.cargadas).toEqual([]);
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(1);
        expect(motor.obtenerEstado().conversaciones.map((c) => c.id)).toContain(12);
    });

    it("sin red no sondea; al volver la señal sigue al momento", async () => {
        const { motor, servidor, reloj } = await montar();
        await arrancar(motor);
        motor.establecerConexion(false);
        await reloj.avanzar(12_000);
        await vaciarPromesas(10);
        const antes = servidor.llamadasA("cambios").length;
        await reloj.avanzar(60_000);
        expect(servidor.llamadasA("cambios").length).toBe(antes);
        expect(motor.obtenerEstado().tiempoReal).toBe("sin_conexion");
        motor.establecerConexion(true);
        await vaciarPromesas(10);
        expect(servidor.llamadasA("cambios").length).toBe(antes + 1);
    });

    it("401/403 al leer el feed: no reintenta", async () => {
        const { motor, servidor, reloj } = await montar();
        await arrancar(motor);
        servidor.fallar("cambios", errorHttp(403, "sin_permiso"));
        await reloj.avanzar(12_000);
        await vaciarPromesas(10);
        const n = servidor.llamadasA("cambios").length;
        await reloj.avanzar(10 * 60_000);
        expect(servidor.llamadasA("cambios").length).toBe(n);
        expect(motor.obtenerEstado().tiempoReal).toBe("detenido");
    });
});

describe("conversación, leído y «visto» (§5.5–§5.6)", () => {
    it("carga el último tramo, pagina hacia atrás y llena el hueco desde el último seq", async () => {
        const { motor, servidor } = await montar();
        for (let i = 1; i <= 45; i += 1) {
            servidor.recibir(12, ANDREA, `m${i}`);
        }
        await arrancar(motor);
        await motor.cargarConversacion(12);
        let v = motor.obtenerConversacion(12);
        expect(v.mensajes.map((m) => m.seq)).toEqual(Array.from({ length: 30 }, (_, i) => i + 16));
        expect(v.hayAnteriores).toBe(true);
        await motor.cargarAnteriores(12);
        v = motor.obtenerConversacion(12);
        expect(v.mensajes[0]?.seq).toBe(1);
        expect(v.hayAnteriores).toBe(false);

        servidor.recibir(12, ANDREA, "nuevo");
        await motor.cargarConversacion(12);
        const siguiente = servidor.llamadasA("listarMensajes").at(-1);
        expect(siguiente?.[1]).toMatchObject({ direccion: "siguientes", desdeSeq: 45 });
        expect(motor.obtenerConversacion(12).mensajes.at(-1)?.texto).toBe("nuevo");
    });

    it("marca leído con el último recibido (no un pendiente) y muestra «visto» con la lectura de otro", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.recibir(12, ANDREA, "¿cómo va?");
        await motor.cargarConversacion(12);
        motor.establecerConexion(false);
        await motor.escribir(12, { texto: "pendiente" });
        motor.establecerConexion(true);
        await motor.marcarLeido(12);
        expect(servidor.llamadasA("leer")[0]).toEqual([12, servidor.mensajes.get(12)?.[0]?.id]);

        await vaciarPromesas(10);
        const mio = hay(motor.obtenerConversacion(12).mensajes.at(-1));
        expect(mio).toMatchObject({ envio: "enviado", visto: false });
        servidor.leyo(12, ANDREA, mio.id);
        await motor.aplicarEventos([hay(servidor.eventos.at(-1))]);
        const visto = hay(motor.obtenerConversacion(12).mensajes.at(-1));
        expect(visto.visto).toBe(true);
        expect(estadoEnvio(visto)?.texto).toBe("visto");
    });

    it("404 al abrir: ya no es miembro, se olvida y se avisa", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.fallar("verConversacion", errorHttp(404, "no_encontrado"));
        await motor.cargarConversacion(12);
        expect(motor.obtenerEstado().conversaciones.find((c) => c.id === 12)).toBeUndefined();
        expect(motor.obtenerEstado().avisos[0]?.texto).toMatch(/acceso/);
    });
});

describe("eliminar (§3, §5.7)", () => {
    it("dentro de 15 min deja la lápida; repetir no hace nada", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        await motor.escribir(12, { texto: "borrar" });
        await vaciarPromesas(10);
        const m = hay(motor.obtenerConversacion(12).mensajes[0]);
        expect(puedeEliminar(m, Date.parse("2026-11-05T17:10:00Z"))).toBe(true);
        expect(puedeEliminar(m, Date.parse("2026-11-05T17:16:00Z"))).toBe(false);
        motor.establecerConexion(false);
        await motor.eliminar(m.id);
        await motor.eliminar(m.id);
        expect(motor.obtenerConversacion(12).mensajes[0]).toMatchObject({ eliminado: true, envio: "eliminando" });
        motor.establecerConexion(true);
        await vaciarPromesas(10);
        expect(servidor.llamadasA("eliminarMensaje")).toEqual([[m.id]]);
        expect(motor.obtenerConversacion(12).mensajes[0]).toMatchObject({ eliminado: true, texto: null, envio: "enviado" });
    });

    it("encolado sin señal y llegado tarde (422 `plazo_vencido`): avisa y deja el mensaje", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        await motor.escribir(12, { texto: "se queda" });
        await vaciarPromesas(10);
        const m = hay(motor.obtenerConversacion(12).mensajes[0]);
        servidor.fallar("eliminarMensaje", errorHttp(422, "plazo_vencido"));
        await motor.eliminar(m.id);
        await vaciarPromesas(10);
        expect(motor.obtenerConversacion(12).mensajes[0]).toMatchObject({ eliminado: false, texto: "se queda" });
        expect(motor.obtenerEstado().avisos.at(-1)?.texto).toMatch(/15 minutos/);
    });

    it("pasados 15 min ni siquiera se encola", async () => {
        const { motor, reloj } = await montar();
        await arrancar(motor);
        await motor.cargarConversacion(12);
        await motor.escribir(12, { texto: "viejo" });
        await vaciarPromesas(10);
        reloj.fijar(reloj.ahora() + 16 * 60_000);
        await expect(motor.eliminar(hay(motor.obtenerConversacion(12).mensajes[0]).id)).rejects.toMatchObject({ codigo: "no_eliminable" });
    });
});

describe("fotos (§3)", () => {
    it("alta con entidad chat_conversacion, mensaje con adjunto_id y subida tus; los bytes se borran", async () => {
        const almacen = new AlmacenChatMemoria();
        const { motor, servidor } = await montar({ almacen, tamanoParte: 4 });
        await arrancar(motor);
        await motor.cargarConversacion(12);
        const datos = new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
        const s = await motor.escribir(12, { foto: { datos, mime: "image/jpeg" } });
        expect(motor.obtenerConversacion(12).mensajes[0]?.foto).toMatchObject({ estado: "local" });
        expect(await motor.leerFotoLocal(hay(s.foto).id)).toEqual(datos);
        await vaciarPromesas(20);
        const [[alta]] = servidor.llamadasA("crearAdjunto") as [[{ entidad: string; entidadId: string; tipo: string; tamano: number }]];
        expect(alta).toMatchObject({ entidad: "chat_conversacion", entidadId: "12", tipo: "foto", tamano: 10 });
        expect((servidor.llamadasA("enviarMensaje")[0]?.[1] as { adjunto_id: string }).adjunto_id).toBe(hay(s.foto).id);
        expect(servidor.llamadasA("enviarParte").map(([, o]) => o)).toEqual([0, 4, 8]);
        expect(almacen.bytes.size).toBe(0);
        expect(motor.obtenerConversacion(12).mensajes[0]?.foto).toMatchObject({ estado: "completo", url: `/api/v1/adjuntos/${hay(s.foto).id}` });
    });
});

describe("caché y cierre de sesión", () => {
    it("olvidarCache borra conversaciones y mensajes pero deja la cola", async () => {
        const almacen = new AlmacenChatMemoria();
        const { motor, servidor } = await montar({ almacen });
        await arrancar(motor);
        servidor.recibir(12, ANDREA, "privado");
        await motor.cargarConversacion(12);
        motor.establecerConexion(false);
        await motor.escribir(12, { texto: "sin enviar" });
        await motor.olvidarCache();
        expect(almacen.conversaciones.size).toBe(0);
        expect(almacen.mensajes.size).toBe(0);
        expect(almacen.meta.cursor).toBeNull();
        expect(almacen.salida.size).toBe(1);
    });

    it("las personas conocidas para «Nuevo mensaje» salen de lo ya visto", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.recibir(12, ANDREA, "hola");
        servidor.crearConversacion({ tipo: "directa", titulo: "Luis", otro_usuario: { id: 9, nombre: "Luis Pérez" } });
        await motor.recargar();
        expect(motor.contactosConocidos()).toEqual([
            { id: 7, nombre: "Andrea Castillo" },
            { id: 9, nombre: "Luis Pérez" },
        ]);
    });
});

describe("«Nuevo mensaje» → personas (GET /chat/personas)", () => {
    it("busca en la API a cualquiera con chat.ver (también soporte sin chat previo), sin ti y con búsqueda", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.nombres.set(ANDREA.id, ANDREA.nombre);
        servidor.nombres.set(21, "Sofía Soporte");
        servidor.nombres.set(YO, "Yo mismo");
        // Nadie le ha escrito: los conocidos están vacíos, la API no.
        expect(motor.contactosConocidos()).toEqual([]);
        const todas = await motor.buscarPersonas("");
        expect(todas.personas.map((p) => p.nombre)).toEqual(["Andrea Castillo", "Sofía Soporte"]);
        expect(todas.siguiente).toBeNull();
        const filtradas = await motor.buscarPersonas("sof");
        expect(filtradas.personas).toEqual([{ id: 21, nombre: "Sofía Soporte", conversacion_id: null }]);
        expect(servidor.llamadasA("personas")).toEqual([
            ["", undefined],
            ["sof", undefined],
        ]);
        // `q` va recortado a los 100 caracteres del contrato.
        await motor.buscarPersonas(` ${"á".repeat(150)} `);
        expect(hay(servidor.llamadasA("personas").at(-1))[0]).toBe("á".repeat(100));
    });

    it("pagina con nextCursor y deja pasar la falta de señal (la pantalla ofrece los conocidos)", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.tamanoPersonas = 1;
        servidor.nombres.set(ANDREA.id, ANDREA.nombre);
        servidor.nombres.set(21, "Sofía Soporte");
        const p1 = await motor.buscarPersonas("");
        expect(p1.personas.map((p) => p.id)).toEqual([ANDREA.id]);
        const p2 = await motor.buscarPersonas("", hay(p1.siguiente));
        expect(p2.personas.map((p) => p.id)).toEqual([21]);
        expect(p2.siguiente).toBeNull();
        servidor.fallar("personas", errorRed());
        await expect(motor.buscarPersonas("")).rejects.toMatchObject({ tipo: "red" });
    });

    it("abrir la directa con alguien de la lista la guarda y la abre", async () => {
        const { motor, servidor } = await montar();
        await arrancar(motor);
        servidor.nombres.set(21, "Sofía Soporte");
        const c = await motor.abrirDirecta(21);
        expect(c.otro_usuario).toEqual({ id: 21, nombre: "Sofía Soporte" });
        expect((await motor.buscarPersonas("")).personas).toEqual([{ id: 21, nombre: "Sofía Soporte", conversacion_id: c.id }]);
    });
});

describe("lógica de pantalla", () => {
    it("vistas del chat en la URL, del botón «Chat» de la isla", () => {
        for (const v of [{ tipo: "chat" as const }, { tipo: "chat_nuevo" as const }, { tipo: "conversacion" as const, id: 12 }]) {
            expect(vistaDeUrl(urlDeVista(v).slice("/campo".length))).toEqual(v);
            expect(pestanaDe(v)).toBe("chat");
        }
    });

    const ahora = Date.parse("2026-11-05T17:00:00Z");

    it("horas y días en America/Guatemala", () => {
        expect(horaChat("2026-11-05T17:14:00Z")).toBe("11:14");
        expect(etiquetaDia(Date.parse("2026-11-05T07:00:00Z"), ahora)).toBe("Hoy");
        expect(etiquetaDia(Date.parse("2026-11-05T05:00:00Z"), ahora)).toBe("Ayer");
        expect(horaLista("2026-11-02T17:00:00Z", ahora)).toMatch(/2 nov/);
    });

    it("separadores de día y autor en grupos solo al cambiar de autor", () => {
        const base = { seq: 1, autorNombre: "Andrea", propio: false, texto: "x", ubicacion: null, foto: null, enviadoEn: null, eliminado: false, menciones: [], envio: "enviado" as const, error: null, visto: false };
        const e = elementosHilo(
            [
                { ...base, id: "a", autorId: 7, creadoEn: "2026-11-04T17:00:00Z" },
                { ...base, id: "b", autorId: 7, creadoEn: "2026-11-05T15:00:00Z" },
                { ...base, id: "c", autorId: 7, creadoEn: "2026-11-05T15:01:00Z" },
                { ...base, id: "d", autorId: 5, propio: true, creadoEn: "2026-11-05T15:02:00Z" },
            ],
            true,
            ahora,
        );
        expect(e.map((x) => (x.tipo === "dia" ? x.texto : `${x.mensaje.id}${x.conAutor ? "*" : ""}`))).toEqual(["Ayer", "a*", "Hoy", "b*", "c", "d"]);
    });

    it("enlaces solo http(s), sin la puntuación final; nunca javascript:", () => {
        expect(segmentosTexto("Mira https://iris.gt/x, y javascript:alert(1) <b>no</b>")).toEqual([
            { tipo: "texto", texto: "Mira " },
            { tipo: "enlace", texto: "https://iris.gt/x", url: "https://iris.gt/x" },
            { tipo: "texto", texto: ", y javascript:alert(1) <b>no</b>" },
        ]);
    });

    it("vista previa de la lista", () => {
        const c = { id: 1, tipo: "cuadrilla" as const, titulo: "Norte", empresa_id: 1, cuadrilla_id: 3, orden_id: null, orden_estado: null, orden_tipo: null, no_leidos: 0, menciones: 0, solo_lectura: false, leido_hasta_mensaje_id: null, ultima_actividad: "", creada_en: "", pendientes: 0, fallidos: 0 };
        expect(vistaPrevia({ ...c, ultimo_mensaje: { id: "x", autor_id: 7, autor_nombre: "Andrea Castillo", contenido: "texto", texto: "hola", enviado_en: "" } }, YO)).toBe("Andrea: hola");
        expect(vistaPrevia({ ...c, ultimo_mensaje: { id: "x", autor_id: YO, autor_nombre: "Yo", contenido: "foto", texto: null, enviado_en: "" } }, YO)).toBe("Tú: Foto");
        expect(vistaPrevia(c, YO)).toBe("Sin mensajes todavía");
    });
});

// --- Adaptadores de almacenamiento ---------------------------------------------------------

const dir = mkdtempSync(join(tmpdir(), "iris-chat-"));
afterAll(() => rmSync(dir, { recursive: true, force: true }));

function conexionNode(ruta: string): ConexionSqlite {
    const db = new DatabaseSync(ruta);
    const p = (x: ValorSqlite[]) => x as SQLInputValue[];
    return {
        execAsync: async (sql) => {
            db.exec(sql);
        },
        runAsync: async (sql, x) => db.prepare(sql).run(...p(x)),
        getAllAsync: async <T>(sql: string, x: ValorSqlite[]) => db.prepare(sql).all(...p(x)) as T[],
        closeAsync: async () => {
            db.close();
        },
    };
}

const salidaDePrueba: SalidaChat = {
    id: "0193f000-0000-7000-8000-000000000001",
    tipo: "enviar",
    conversacionId: 12,
    secuencia: 1,
    mensajeId: "0193f000-0000-7000-8000-000000000001",
    texto: "hola",
    creadoEn: "2026-11-05T17:00:00.000Z",
    estado: "pendiente",
    enviado: false,
    intentos: 0,
    error: null,
};

describe.each<[string, () => AlmacenChat]>([
    ["Dexie", () => new AlmacenDexie({ nombre: `iris-chat-${Math.random()}`, dexie: { indexedDB, IDBKeyRange } })],
    ["SQLite", () => new AlmacenSqlite(conexionNode(join(dir, `chat-${Math.random()}.db`)))],
])("almacén %s", (_nombre, crear) => {
    it("guarda la cola, la caché y los bytes; borrarCacheChat deja la cola", async () => {
        const a = crear();
        const conv = new ServidorChatFalso(YO).crearConversacion({ id: 12, tipo: "orden", titulo: "OT-000102" });
        const m: MensajeChat = { id: "m1", conversacion_id: 12, seq: 1, autor_id: 7, autor_nombre: "Andrea", texto: "hola", creado_en: null, enviado_en: "2026-11-05T17:00:00Z", eliminado: false, eliminado_en: null, menciones: [] };
        await a.escribirChat({
            salida: { poner: [salidaDePrueba] },
            conversaciones: { poner: [conv] },
            mensajes: { poner: [m] },
            bytes: { poner: [{ id: "f1", datos: new Uint8Array([1, 2, 3]) }] },
            meta: { cursor: "15", cargadas: [12], secuencia: 2 },
        });
        let c = await a.cargarChat();
        expect(c.salida).toEqual([salidaDePrueba]);
        expect(c.conversaciones.map((x) => x.id)).toEqual([12]);
        expect(c.mensajes.map((x) => x.id)).toEqual(["m1"]);
        expect(c.meta).toEqual({ cursor: "15", cargadas: [12], secuencia: 2 });
        expect(await a.leerBytesChat("f1")).toEqual(new Uint8Array([1, 2, 3]));
        await a.borrarCacheChat();
        c = await a.cargarChat();
        expect(c.conversaciones).toEqual([]);
        expect(c.mensajes).toEqual([]);
        expect(c.meta).toMatchObject({ cursor: null, cargadas: [], secuencia: 2 });
        expect(c.salida).toHaveLength(1);
        expect(await a.leerBytesChat("f1")).toEqual(new Uint8Array([1, 2, 3]));
    });
});
