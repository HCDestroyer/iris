// Menciones del chat (`contratos/chat.md` §3.2): salen con el mensaje por la cola (mismo id,
// idempotente) y se aplican desde el feed (`EventoChat.menciones`, `MensajeChat.menciones`).
import { afterEach, describe, expect, it } from "vitest";
import { AlmacenChatMemoria } from "./chat/almacen-memoria";
import { ErrorChatLocal, MotorChat } from "./chat/motor";
import type { CuerpoEnviarMensaje, EventoChat, MensajeChat } from "./chat/tipos";
import { errorRed, ServidorChatFalso } from "./pruebas/chat-falso";
import { RelojManual, vaciarPromesas } from "./pruebas/reloj-manual";

const YO = 5;
const MARIO = { id: 7, nombre: "Mario Ixcot" };

const abiertos: MotorChat[] = [];
afterEach(() => {
    for (const m of abiertos.splice(0)) m.detener();
});

async function montar() {
    const reloj = new RelojManual("2026-11-05T17:00:00Z");
    const servidor = new ServidorChatFalso(YO);
    servidor.ahora = () => reloj.ahora();
    servidor.crearConversacion({ id: 12, tipo: "orden", titulo: "OT-000102", orden_id: 102, orden_estado: "en_sitio" });
    const motor = await MotorChat.abrir({ almacen: new AlmacenChatMemoria(), transporte: servidor, usuarioId: YO, reloj, espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0.5 } });
    abiertos.push(motor);
    motor.iniciar();
    await vaciarPromesas(10);
    return { reloj, servidor, motor };
}

function mensaje(datos: Partial<MensajeChat> & Pick<MensajeChat, "id" | "seq">): MensajeChat {
    return {
        conversacion_id: 12,
        autor_id: MARIO.id,
        autor_nombre: MARIO.nombre,
        texto: "@Ana revisa la caja",
        creado_en: "2026-11-05T17:00:00Z",
        enviado_en: "2026-11-05T17:00:00Z",
        eliminado: false,
        eliminado_en: null,
        menciones: [{ id: YO, nombre: "Ana López" }],
        ...datos,
    };
}

describe("menciones al enviar (§3.2)", () => {
    it("van en el cuerpo por la cola y el reintento repite el mismo id y las mismas menciones", async () => {
        const { motor, servidor, reloj } = await montar();
        servidor.fallar("enviarMensaje", errorRed());
        await motor.escribir(12, { texto: "@Mario Ixcot ya hay conectores", menciones: [MARIO.id, MARIO.id] });
        await vaciarPromesas(10);
        await reloj.avanzar(5_000);
        await vaciarPromesas(10);
        const envios = servidor.llamadasA("enviarMensaje").map((a) => a[1] as CuerpoEnviarMensaje);
        expect(envios).toHaveLength(2);
        expect(envios[0]?.menciones).toEqual([MARIO.id]);
        expect(envios[1]).toEqual(envios[0]);
    });

    it("sin menciones no manda el campo", async () => {
        const { motor, servidor } = await montar();
        await motor.escribir(12, { texto: "hola" });
        await vaciarPromesas(10);
        const cuerpo = servidor.llamadasA("enviarMensaje")[0]?.[1] as CuerpoEnviarMensaje;
        expect(cuerpo).not.toHaveProperty("menciones");
    });

    it("rechaza en el teléfono: sin texto, a uno mismo o más de 20", async () => {
        const { motor } = await montar();
        await expect(motor.escribir(12, { ubicacion: { lat: 14.6, lon: -90.5 }, menciones: [MARIO.id] })).rejects.toBeInstanceOf(ErrorChatLocal);
        await expect(motor.escribir(12, { texto: "yo", menciones: [YO] })).rejects.toMatchObject({ codigo: "mencion_invalida" });
        await expect(motor.escribir(12, { texto: "todos", menciones: Array.from({ length: 21 }, (_, i) => 100 + i) })).rejects.toMatchObject({ codigo: "mencion_invalida" });
    });

    it("el mensaje en la cola ya lleva sus menciones (por id)", async () => {
        const { motor, servidor } = await montar();
        servidor.fallar("enviarMensaje", errorRed(), 5);
        await motor.escribir(12, { texto: "@Mario Ixcot", menciones: [MARIO.id] });
        expect(motor.obtenerConversacion(12).mensajes.at(-1)?.menciones).toEqual([{ id: MARIO.id, nombre: "" }]);
    });
});

describe("menciones del feed", () => {
    it("el evento fija las menciones de la conversación y el mensaje trae a quién menciona", async () => {
        const { motor } = await montar();
        await motor.cargarConversacion(12);
        const m = mensaje({ id: "01930000-0000-7000-8000-000000000001", seq: 1 });
        const e: EventoChat = { id: "50", tipo: "mensaje_nuevo", conversacion_id: 12, ocurrido_en: m.enviado_en, no_leidos: 1, menciones: 1, mensaje: m };
        await motor.aplicarEventos([e]);
        expect(motor.obtenerEstado().conversaciones.find((c) => c.id === 12)?.menciones).toBe(1);
        expect(motor.obtenerConversacion(12).mensajes.at(-1)?.menciones).toEqual([{ id: YO, nombre: "Ana López" }]);

        // Mi lectura desde otro dispositivo: baja el contador y las menciones.
        await motor.aplicarEventos([
            { id: "51", tipo: "leido", conversacion_id: 12, ocurrido_en: m.enviado_en, no_leidos: 0, menciones: 0, lectura: { usuario_id: YO, hasta_mensaje_id: m.id, leido_en: m.enviado_en } },
        ]);
        expect(motor.obtenerEstado().conversaciones.find((c) => c.id === 12)?.menciones).toBe(0);
    });

    it("una lápida no trae menciones", async () => {
        const { motor } = await montar();
        await motor.cargarConversacion(12);
        const m = mensaje({ id: "01930000-0000-7000-8000-000000000002", seq: 1, eliminado: true, texto: null, menciones: [] });
        await motor.aplicarEventos([{ id: "52", tipo: "mensaje_eliminado", conversacion_id: 12, ocurrido_en: m.enviado_en, no_leidos: 0, menciones: 0, mensaje: m }]);
        expect(motor.obtenerConversacion(12).mensajes.at(-1)?.menciones).toEqual([]);
    });

    it("leer todo deja las menciones en cero", async () => {
        const { motor, servidor } = await montar();
        await motor.cargarConversacion(12);
        const m = servidor.recibir(12, MARIO, "@Ana López mira");
        await motor.aplicarEventos([{ id: "60", tipo: "mensaje_nuevo", conversacion_id: 12, ocurrido_en: m.enviado_en, no_leidos: 1, menciones: 1, mensaje: { ...m, menciones: [{ id: YO, nombre: "Ana López" }] } }]);
        await motor.marcarLeido(12);
        expect(motor.obtenerEstado().conversaciones.find((c) => c.id === 12)).toMatchObject({ no_leidos: 0, menciones: 0 });
    });
});

describe("abrir el chat de una cuadrilla", () => {
    it("lo pide al servidor y queda en la lista", async () => {
        const { motor, servidor } = await montar();
        const c = await motor.abrirCuadrilla(3);
        expect(servidor.llamadasA("abrirCuadrilla")).toEqual([[3]]);
        expect(motor.obtenerEstado().conversaciones.map((x) => x.id)).toContain(c.id);
    });
});
