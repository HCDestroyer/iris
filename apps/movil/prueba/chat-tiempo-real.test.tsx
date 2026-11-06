// Chat en tiempo real en la app nativa: el flujo SSE (en la app, `crearFlujoFetch` sobre
// `expo/fetch`) solo con la app al frente. AppState → `escucharPlano` → `primerPlano`/`segundoPlano`
// del motor: al pasar a segundo plano se cierra la conexión, al volver se abre una (nunca dos), y el
// encabezado de la conversación dice «en vivo» con el flujo abierto y nada con sondeo.
import { AlmacenChatMemoria, MotorChat, type AbrirFlujoChat, type OyentesFlujo } from "@iris/sync/chat";
import { montar, ServidorChatFalso, vaciarPromesas, type Montaje } from "@iris/sync/pruebas";
import { act, fireEvent, screen } from "@testing-library/react-native";
import { AppState, type AppStateStatus } from "react-native";
import { escucharPlano } from "../src/campo/plano";
import { abrirCampo, paqueteCon, sesion } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

type Oyente = (estado: AppStateStatus) => void;

/** AppState simulado: guarda los oyentes y deja cambiar el estado. */
function simularAppState() {
    const oyentes = new Set<Oyente>();
    const espia = jest.spyOn(AppState, "addEventListener").mockImplementation(((evento: string, fn: Oyente) => {
        if (evento === "change") {
            oyentes.add(fn);
        }
        return { remove: () => oyentes.delete(fn) };
    }) as typeof AppState.addEventListener);
    return {
        cambiar: async (estado: AppStateStatus) => {
            await act(async () => {
                for (const fn of [...oyentes]) {
                    fn(estado);
                }
                await vaciarPromesas(20);
            });
        },
        restaurar: () => espia.mockRestore(),
    };
}

/** Flujo falso: cuenta las conexiones abiertas (el de verdad lo prueba `@iris/sync`). */
function flujoFalso() {
    const abiertos = new Set<OyentesFlujo>();
    let aperturas = 0;
    const abrir: AbrirFlujoChat = (_desde, oyentes) => {
        aperturas += 1;
        abiertos.add(oyentes);
        queueMicrotask(() => abiertos.has(oyentes) && oyentes.alAbrir());
        return { cerrar: () => abiertos.delete(oyentes) };
    };
    return { abrir, abiertos: () => abiertos.size, aperturas: () => aperturas };
}

const abiertosMotores: MotorChat[] = [];
afterEach(() => {
    for (const m of abiertosMotores.splice(0)) {
        m.detener();
    }
});

async function conChat(abrirFlujo: AbrirFlujoChat | null): Promise<{ m: Montaje; chat: MotorChat }> {
    const m = await montar();
    m.transporte.paquete = paqueteCon({});
    await m.motor.cargarPaquete("2026-10-28");
    const servidor = new ServidorChatFalso(sesion.usuarioId);
    servidor.ahora = () => Date.now();
    servidor.crearConversacion({ id: 12, tipo: "cuadrilla", titulo: "Cuadrilla Norte", cuadrilla_id: 3 });
    const chat = await MotorChat.abrir({ almacen: new AlmacenChatMemoria(), transporte: servidor, usuarioId: sesion.usuarioId, reloj: m.reloj, abrirFlujo });
    abiertosMotores.push(chat);
    return { m, chat };
}

describe.each(["light", "dark"] as const)("chat en tiempo real (%s)", (esquema) => {
    it("con la app al frente hay una conexión; en segundo plano se cierra y al volver se abre otra", async () => {
        const app = simularAppState();
        try {
            const flujo = flujoFalso();
            const { m, chat } = await conChat(flujo.abrir);
            await abrirCampo(m, { esquema, chat, alCambiarPlano: escucharPlano, vista: { tipo: "chat" } });
            await act(async () => {
                await vaciarPromesas(20);
            });
            expect(flujo.abiertos()).toBe(1);
            expect(chat.obtenerEstado().tiempoReal).toBe("sse");

            // `inactive` (iOS, la persiana) y luego `background`: se cierra una vez.
            await app.cambiar("inactive");
            await app.cambiar("background");
            expect(flujo.abiertos()).toBe(0);
            expect(chat.obtenerEstado().tiempoReal).toBe("detenido");
            const antes = flujo.aperturas();
            await app.cambiar("background");
            expect(flujo.aperturas()).toBe(antes);

            await app.cambiar("active");
            expect(flujo.abiertos()).toBe(1);
            expect(flujo.aperturas()).toBe(antes + 1);
            // Repetir `active` no abre una segunda conexión.
            await app.cambiar("active");
            expect(flujo.abiertos()).toBe(1);
        } finally {
            app.restaurar();
        }
    });

    it("si la app ya arranca en segundo plano, no abre el flujo hasta volver al frente", async () => {
        const app = simularAppState();
        const estadoOriginal = AppState.currentState;
        Object.defineProperty(AppState, "currentState", { value: "background", configurable: true, writable: true });
        try {
            const flujo = flujoFalso();
            const { m, chat } = await conChat(flujo.abrir);
            await abrirCampo(m, { esquema, chat, alCambiarPlano: escucharPlano, vista: { tipo: "chat" } });
            await act(async () => {
                await vaciarPromesas(20);
            });
            expect(flujo.abiertos()).toBe(0);
            expect(chat.obtenerEstado().tiempoReal).toBe("detenido");
            await app.cambiar("active");
            expect(flujo.abiertos()).toBe(1);
        } finally {
            Object.defineProperty(AppState, "currentState", { value: estadoOriginal, configurable: true, writable: true });
            app.restaurar();
        }
    });

    it("escucharPlano avisa el estado inicial solo si no está al frente", () => {
        const estadoOriginal = AppState.currentState;
        const app = simularAppState();
        try {
            for (const [estado, esperado] of [["active", []], [undefined, []], ["inactive", [false]], ["background", [false]]] as const) {
                Object.defineProperty(AppState, "currentState", { value: estado, configurable: true, writable: true });
                const avisos: boolean[] = [];
                const quitar = escucharPlano((v) => avisos.push(v));
                expect(avisos).toEqual(esperado);
                quitar();
            }
        } finally {
            Object.defineProperty(AppState, "currentState", { value: estadoOriginal, configurable: true, writable: true });
            app.restaurar();
        }
    });

    it("la conversación dice «en vivo» con el flujo abierto y nada con sondeo", async () => {
        const flujo = flujoFalso();
        const { m, chat } = await conChat(flujo.abrir);
        await abrirCampo(m, { esquema, chat, vista: { tipo: "chat" } });
        await act(async () => {
            await vaciarPromesas(20);
        });
        await fireEvent.press(await screen.findByLabelText(/^Cuadrilla Norte/));
        await screen.findByTestId("conversacion-chat");
        expect(screen.getByLabelText("Chat en vivo")).toHaveTextContent("en vivo");
    });

    it("sin flujo (sondeo) el encabezado no muestra nada", async () => {
        const { m, chat } = await conChat(null);
        await abrirCampo(m, { esquema, chat, vista: { tipo: "chat" } });
        await act(async () => {
            await vaciarPromesas(20);
        });
        await fireEvent.press(await screen.findByLabelText(/^Cuadrilla Norte/));
        await screen.findByTestId("conversacion-chat");
        expect(chat.obtenerEstado().tiempoReal).toBe("sondeo");
        expect(screen.queryByTestId("chat-en-vivo")).toBeNull();
    });
});
