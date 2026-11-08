// Reportes del PM (APK 0.18.0, Android) en el chat del equipo:
// 1. Con el teclado abierto el compositor subía (#131) pero el hilo no: el último mensaje quedaba
//    debajo del teclado o del compositor. Ahora el hilo sigue al final (teclado, envío, llegada) y,
//    si la persona subió a leer, no se la mueve: aparece «Mensajes nuevos».
// 2. «Nuevo mensaje» solo ofrecía a quien el teléfono ya conocía (nadie, en un teléfono nuevo): ahora
//    busca en `GET /chat/personas` a cualquiera de la empresa con `chat.ver` (soporte, despacho…).
import { MotorChat } from "@iris/sync/chat";
import { AlmacenChatMemoria, errorHttp, errorRed, montar, ordenDePrueba, paqueteDePrueba, ServidorChatFalso, vaciarPromesas, type Montaje } from "@iris/sync/pruebas";
import { act, fireEvent, renderHook, screen, waitFor, within } from "@testing-library/react-native";
import { ScrollView } from "react-native";
import { useSeguirFinal } from "../src/campo/seguir-final";
import { abrirCampo, avisoArriba, sesion } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const ANDREA = { id: 61, nombre: "Andrea Castillo" };
const SOFIA = { id: 77, nombre: "Sofía Soporte" };
const DIEGO = { id: 78, nombre: "Diego Despacho" };
const CUADRILLA = 40;

/** `scrollToEnd` del `ScrollView` simulado (un `jest.fn` del prototipo): solo las llamadas del hilo. */
const alFinal = ScrollView.prototype.scrollToEnd as unknown as jest.Mock;
const bajadasDelHilo = () => alFinal.mock.contexts.map((c, i) => [c, alFinal.mock.calls[i]?.[0]] as const).filter(([c]) => (c as { props?: { testID?: string } }).props?.testID === "hilo-chat");

const abiertos: MotorChat[] = [];
afterEach(() => {
    for (const c of abiertos.splice(0)) {
        c.detener();
    }
});

async function conChat(): Promise<{ m: Montaje; chat: MotorChat; servidor: ServidorChatFalso }> {
    const m = await montar();
    m.transporte.paquete = (fecha) => ({ ...paqueteDePrueba(fecha), ruta: undefined as never, ordenes: [ordenDePrueba(1, { tipo: "visita", estado: "en_sitio" })] });
    await m.motor.cargarPaquete("2026-10-28");
    const servidor = new ServidorChatFalso(sesion.usuarioId);
    servidor.ahora = () => Date.now();
    servidor.nombreYo = sesion.nombre;
    servidor.crearConversacion({ id: CUADRILLA, tipo: "cuadrilla", titulo: "Cuadrilla Norte", cuadrilla_id: 3 });
    servidor.recibir(CUADRILLA, ANDREA, "La caja NAP 14 tiene la potencia baja.");
    const chat = await MotorChat.abrir({ almacen: new AlmacenChatMemoria(), transporte: servidor, subidor: servidor.subidor, usuarioId: sesion.usuarioId, reloj: m.reloj });
    abiertos.push(chat);
    return { m, chat, servidor };
}

async function abrirConversacion(chat: MotorChat, m: Montaje) {
    await abrirCampo(m, { chat, vista: { tipo: "chat" } });
    await act(async () => {
        await vaciarPromesas(10);
    });
    await fireEvent.press(await screen.findByLabelText(/^Cuadrilla Norte/));
    await screen.findByTestId("conversacion-chat");
    await act(async () => {
        await vaciarPromesas(10);
    });
    return screen.getByTestId("hilo-chat");
}

const medir = (hilo: ReturnType<typeof screen.getByTestId>, alto: number) => fireEvent(hilo, "layout", { nativeEvent: { layout: { x: 0, y: 0, width: 390, height: alto } } });
const desplazar = (hilo: ReturnType<typeof screen.getByTestId>, y: number) =>
    fireEvent.scroll(hilo, { nativeEvent: { contentOffset: { x: 0, y }, contentSize: { width: 390, height: 2000 }, layoutMeasurement: { width: 390, height: 500 } } });

async function llega(chat: MotorChat, servidor: ServidorChatFalso, texto: string) {
    servidor.recibir(CUADRILLA, ANDREA, texto);
    await act(async () => {
        await chat.aplicarEventos(servidor.eventos.slice(-1));
    });
}

describe("hilo del chat sobre el teclado", () => {
    it("al abrirse el teclado (el hilo se acorta) baja al último mensaje", async () => {
        const { m, chat } = await conChat();
        const hilo = await abrirConversacion(chat, m);
        await medir(hilo, 600);
        alFinal.mockClear();
        // El `KeyboardAvoidingView` acorta el hilo: sigue al final.
        await medir(hilo, 280);
        expect(bajadasDelHilo().length).toBeGreaterThan(0);
    });

    it("con el alto del teclado de keyboard-controller (por si el hilo no se vuelve a medir) baja; leyendo arriba, no", async () => {
        const lista = { current: { scrollToEnd: jest.fn() } };
        const { rerender, result } = await renderHook(({ teclado }: { teclado: number }) => useSeguirFinal(lista, { id: "a", propio: false }, teclado), { initialProps: { teclado: 0 } });
        await rerender({ teclado: 320 });
        expect(lista.current.scrollToEnd).toHaveBeenCalledWith({ animated: true });
        lista.current.scrollToEnd.mockClear();
        await rerender({ teclado: 0 });
        await act(async () => {
            result.current.alDesplazar({ nativeEvent: { contentOffset: { x: 0, y: 0 }, contentSize: { width: 390, height: 2000 }, layoutMeasurement: { width: 390, height: 500 } } } as never);
        });
        await rerender({ teclado: 320 });
        expect(lista.current.scrollToEnd).not.toHaveBeenCalled();
    });

    it("al enviar baja al final aunque estuviera leyendo arriba", async () => {
        const { m, chat } = await conChat();
        const hilo = await abrirConversacion(chat, m);
        await desplazar(hilo, 100);
        alFinal.mockClear();
        await fireEvent.changeText(screen.getByLabelText("Mensaje"), "Voy para allá");
        await fireEvent.press(screen.getByTestId("enviar-mensaje"));
        await act(async () => {
            await vaciarPromesas(10);
        });
        expect(bajadasDelHilo().length).toBeGreaterThan(0);
        expect(screen.queryByTestId("mensajes-nuevos")).toBeNull();
    });

    it("al final: un mensaje nuevo que llega baja solo (al crecer el contenido)", async () => {
        const { m, chat, servidor } = await conChat();
        const hilo = await abrirConversacion(chat, m);
        await desplazar(hilo, 1500);
        await llega(chat, servidor, "¿Ya llegaste?");
        alFinal.mockClear();
        await fireEvent(hilo, "contentSizeChange", 390, 2100);
        expect(bajadasDelHilo().length).toBe(1);
        expect(screen.queryByTestId("mensajes-nuevos")).toBeNull();
    });

    it("leyendo arriba: no se mueve (ni con el teclado) y aparece «Mensajes nuevos», que baja al tocarlo", async () => {
        const { m, chat, servidor } = await conChat();
        const hilo = await abrirConversacion(chat, m);
        await medir(hilo, 600);
        await desplazar(hilo, 100);
        alFinal.mockClear();
        await llega(chat, servidor, "Mensaje mientras leías");
        await fireEvent(hilo, "contentSizeChange", 390, 2100);
        await medir(hilo, 280);
        expect(bajadasDelHilo()).toHaveLength(0);
        const boton = await screen.findByTestId("mensajes-nuevos");
        expect(boton).toHaveTextContent("Mensajes nuevos");
        await fireEvent.press(boton);
        expect(bajadasDelHilo()).toEqual([[expect.anything(), { animated: true }]]);
        expect(screen.queryByTestId("mensajes-nuevos")).toBeNull();
    });

    it("el hilo de una orden usa la misma conversación (sigue al final con el teclado)", async () => {
        const { m, chat } = await conChat();
        await abrirCampo(m, { chat, vista: { tipo: "chat" } });
        await act(async () => {
            await vaciarPromesas(10);
        });
        await fireEvent.press(await screen.findByLabelText(/^OT-000001/));
        const hilo = await screen.findByTestId("hilo-chat");
        await medir(hilo, 600);
        alFinal.mockClear();
        await medir(hilo, 280);
        expect(bajadasDelHilo().length).toBeGreaterThan(0);
    });
});

describe("«Nuevo mensaje»: personas de la empresa", () => {
    async function abrirNuevo(servidor: ServidorChatFalso) {
        servidor.nombres.set(ANDREA.id, ANDREA.nombre);
        servidor.nombres.set(SOFIA.id, SOFIA.nombre);
        servidor.nombres.set(DIEGO.id, DIEGO.nombre);
        servidor.nombres.set(sesion.usuarioId, sesion.nombre);
    }

    it("lista a cualquiera con chat.ver (soporte y despacho sin chat previo), sin uno mismo, y busca por nombre", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirNuevo(servidor);
        await abrirCampo(m, { chat, vista: { tipo: "chat_nuevo" } });
        const lista = await screen.findByTestId("lista-personas");
        expect(within(lista).getAllByRole("button").map((b) => b.props.accessibilityLabel)).toEqual([ANDREA.nombre, DIEGO.nombre, SOFIA.nombre]);
        expect(screen.queryByLabelText(sesion.nombre)).toBeNull();
        await fireEvent.changeText(screen.getByLabelText("Buscar persona"), "sof");
        await waitFor(() => expect(within(screen.getByTestId("lista-personas")).getAllByRole("button").map((b) => b.props.accessibilityLabel)).toEqual([SOFIA.nombre]));
        expect(servidor.llamadasA("personas").map(([q]) => q)).toContain("sof");
        await fireEvent.changeText(screen.getByLabelText("Buscar persona"), "zzz");
        expect(await screen.findByText("Nadie con ese nombre")).toBeTruthy();
        expect(screen.queryByTestId("lista-personas")).toBeNull();
    });

    it("al elegir a alguien de soporte abre la directa (POST /chat/directas) y entra a la conversación", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirNuevo(servidor);
        await abrirCampo(m, { chat, vista: { tipo: "chat_nuevo" } });
        await fireEvent.press(await screen.findByLabelText(SOFIA.nombre));
        expect(await screen.findByTestId("conversacion-chat")).toBeTruthy();
        expect(servidor.llamadasA("abrirDirecta")).toEqual([[SOFIA.id]]);
        expect(screen.getByRole("header", { name: SOFIA.nombre })).toBeTruthy();
    });

    it("422 usuario_no_disponible al abrir: lo dice y se queda en la lista", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirNuevo(servidor);
        servidor.fallar("abrirDirecta", errorHttp(422, "usuario_no_disponible"));
        await abrirCampo(m, { chat, vista: { tipo: "chat_nuevo" } });
        await fireEvent.press(await screen.findByLabelText(DIEGO.nombre));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent("Esa persona ya no está disponible para el chat."));
        expect(screen.getByTestId("lista-personas")).toBeTruthy();
    });

    it("sin señal: lo dice, ofrece a quienes ya conoce y «Reintentar» vuelve a buscar", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirNuevo(servidor);
        await chat.cargarConversacion(CUADRILLA);
        servidor.fallar("personas", errorRed());
        await abrirCampo(m, { chat, vista: { tipo: "chat_nuevo" } });
        const aviso = await screen.findByTestId("personas-sin-senal");
        // Andrea le escribió en la cuadrilla: el teléfono la conoce.
        expect(within(screen.getByTestId("lista-personas")).getAllByRole("button").map((b) => b.props.accessibilityLabel)).toEqual([ANDREA.nombre]);
        await fireEvent.press(within(aviso).getByRole("button", { name: "Reintentar" }));
        await waitFor(() => expect(within(screen.getByTestId("lista-personas")).getAllByRole("button")).toHaveLength(3));
    });

    it("error de la API: «Reintentar» (nunca una lista inventada)", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirNuevo(servidor);
        servidor.fallar("personas", errorHttp(500, "interno"));
        await abrirCampo(m, { chat, vista: { tipo: "chat_nuevo" } });
        const aviso = await screen.findByTestId("error-personas");
        expect(screen.queryByTestId("lista-personas")).toBeNull();
        await fireEvent.press(within(aviso).getByRole("button", { name: "Reintentar" }));
        expect(await screen.findByTestId("lista-personas")).toBeTruthy();
    });
});
