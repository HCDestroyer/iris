// Chat interno en la app nativa (tablero C-Chat), claro y oscuro: entrada desde «Yo» con los no
// leídos, lista, conversación con «visto», sin señal, «no enviado — Reintentar», eliminar con el
// menú del mensaje y «Abrir chat de la orden». Motor real de `@iris/sync/chat` sobre memoria y el
// servidor de chat falso.
import { MotorChat } from "@iris/sync/chat";
import { AlmacenChatMemoria, errorHttp, montar, ordenDePrueba, paqueteDePrueba, ServidorChatFalso, vaciarPromesas, type Montaje } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { abrirCampo, sesion } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const ANDREA = { id: 61, nombre: "Andrea Castillo" };
const CUADRILLA = 40;

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
    // La pantalla mide los 15 min con la hora real (`useAhora`): el servidor usa la misma.
    servidor.ahora = () => Date.now();
    servidor.nombreYo = sesion.nombre;
    servidor.nombres.set(ANDREA.id, ANDREA.nombre);
    servidor.crearConversacion({ id: CUADRILLA, tipo: "cuadrilla", titulo: "Cuadrilla Norte", cuadrilla_id: 3 });
    servidor.recibir(CUADRILLA, ANDREA, "La caja NAP 14 tiene la potencia baja.");
    const chat = await MotorChat.abrir({ almacen: new AlmacenChatMemoria(), transporte: servidor, subidor: servidor.subidor, usuarioId: sesion.usuarioId, reloj: m.reloj });
    abiertos.push(chat);
    return { m, chat, servidor };
}

async function abrirConversacion(chat: MotorChat, m: Montaje, esquema: "light" | "dark") {
    await abrirCampo(m, { esquema, chat, vista: { tipo: "chat" } });
    await act(async () => {
        await vaciarPromesas(10);
    });
    await fireEvent.press(await screen.findByLabelText(/^Cuadrilla Norte/));
    await screen.findByTestId("conversacion-chat");
    await act(async () => {
        await vaciarPromesas(10);
    });
}

async function escribir(texto: string) {
    await fireEvent.changeText(screen.getByLabelText("Mensaje"), texto);
    await fireEvent.press(screen.getByTestId("enviar-mensaje"));
    await act(async () => {
        await vaciarPromesas(10);
    });
}

describe.each(["light", "dark"] as const)("chat interno (%s)", (esquema) => {
    it("«Chat» de la isla lleva a la lista con vista previa y contador", async () => {
        const { m, chat } = await conChat();
        await abrirCampo(m, { esquema, chat });
        await act(async () => {
            await vaciarPromesas(10);
        });
        await fireEvent.press(await screen.findByLabelText("Chat (1 sin leer)"));
        const fila = await screen.findByLabelText("Cuadrilla Norte, 1 sin leer");
        expect(within(fila).getByText(/Andrea: La caja NAP 14/)).toBeTruthy();
        // El hilo de la orden en curso se abrió solo (idempotente).
        expect(await screen.findByLabelText("OT-000001")).toBeTruthy();
    });

    it("isla: Hoy · Chat (punto de no leídos) · «+» · Agenda · Yo; «Chat» abre la lista", async () => {
        const { m, chat } = await conChat();
        await abrirCampo(m, { esquema, chat });
        await act(async () => {
            await vaciarPromesas(10);
        });
        const isla = screen.getByTestId("navegacion-inferior");
        await waitFor(() => expect(within(isla).getAllByRole("tab").map((p) => p.props.accessibilityLabel)).toEqual(["Hoy", "Chat (1 sin leer)", "Agenda", `Yo · ${sesion.nombre}`]));
        expect(within(screen.getByTestId("pestana-chat")).getByTestId("punto-no-leidos")).toBeTruthy();
        await fireEvent.press(screen.getByTestId("pestana-chat"));
        expect(await screen.findByTestId("lista-chat")).toBeTruthy();
        expect(screen.getByTestId("pestana-chat")).toBeSelected();
        // Con el chat abierto: píldora «Chat» con su nombre y sin el punto.
        expect(screen.getByTestId("nombre-pestana-chat")).toHaveTextContent("Chat");
        expect(within(screen.getByTestId("pestana-chat")).queryByTestId("punto-no-leidos")).toBeNull();
        expect(screen.queryByTestId("nombre-pestana-hoy")).toBeNull();
    });

    it("conversación: de otros con autor, envío propio «enviado» y luego «visto»", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirConversacion(chat, m, esquema);
        expect(screen.getByText(ANDREA.nombre)).toBeTruthy();
        expect(screen.getByTestId("separador-dia")).toBeTruthy();
        await escribir("Voy para allá");
        expect(servidor.mensajes.get(CUADRILLA)?.at(-1)?.texto).toBe("Voy para allá");
        await waitFor(() => expect(screen.getByText(/· enviado$/)).toBeTruthy());
        servidor.leyo(CUADRILLA, ANDREA, servidor.mensajes.get(CUADRILLA)?.at(-1)?.id ?? "");
        await act(async () => {
            await chat.aplicarEventos(servidor.eventos.slice(-1));
        });
        await waitFor(() => expect(screen.getByText(/· visto$/)).toBeTruthy());
        expect(servidor.llamadasA("leer").length).toBeGreaterThan(0);
    });

    it("sin señal: «se envía al tener señal» y sale al volver", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirConversacion(chat, m, esquema);
        await act(async () => {
            chat.establecerConexion(false);
        });
        await escribir("Sin señal");
        expect(screen.getByText(/se envía al tener señal/)).toBeTruthy();
        expect(servidor.llamadasA("enviarMensaje")).toHaveLength(0);
        await act(async () => {
            chat.establecerConexion(true);
            await vaciarPromesas(10);
        });
        await waitFor(() => expect(screen.getByText(/· enviado$/)).toBeTruthy());
    });

    it("422: «No enviado — Reintentar» y al tocarlo sale", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirConversacion(chat, m, esquema);
        servidor.fallar("enviarMensaje", errorHttp(422, "mensaje_vacio"));
        await escribir("Falla una vez");
        expect(await screen.findByTestId("mensaje-fallido")).toBeTruthy();
        await fireEvent.press(screen.getByTestId("reintentar-mensaje"));
        await act(async () => {
            await vaciarPromesas(10);
        });
        await waitFor(() => expect(screen.getByText(/· enviado$/)).toBeTruthy());
    });

    it("mantener presionado el propio → «Eliminar para todos» deja la lápida", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirConversacion(chat, m, esquema);
        await escribir("Me equivoqué");
        await waitFor(() => expect(screen.getByText(/· enviado$/)).toBeTruthy());
        const burbujas = screen.getAllByTestId("burbuja");
        await fireEvent(burbujas[burbujas.length - 1] as never, "longPress");
        await fireEvent.press(await screen.findByText("Eliminar para todos"));
        await act(async () => {
            await vaciarPromesas(10);
        });
        // La lápida (y el aviso «Mensaje eliminado» que entra desde arriba).
        await waitFor(() => expect(screen.getAllByText("Mensaje eliminado").length).toBeGreaterThanOrEqual(2));
        expect(servidor.llamadasA("eliminarMensaje")).toHaveLength(1);
        expect(screen.queryByText("Me equivoqué")).toBeNull();
    });

    it("el texto es plano: el HTML se ve tal cual", async () => {
        const { m, chat, servidor } = await conChat();
        servidor.recibir(CUADRILLA, ANDREA, "<b>hola</b> https://iris.gt/x");
        await abrirConversacion(chat, m, esquema);
        expect(screen.getByText("https://iris.gt/x")).toBeTruthy();
        expect(screen.getByText(/<b>hola<\/b>/)).toBeTruthy();
    });

    it("«Abrir chat de la orden» desde el detalle abre su hilo", async () => {
        const { m, chat, servidor } = await conChat();
        await abrirCampo(m, { esquema, chat, vista: { tipo: "orden", id: 1 } });
        await fireEvent.press(await screen.findByTestId("abrir-chat-orden"));
        await act(async () => {
            await vaciarPromesas(10);
        });
        expect(await screen.findByText("Chat de la orden con despacho · el cliente no lo ve")).toBeTruthy();
        expect(servidor.llamadasA("abrirOrden").map(([id]) => id)).toContain(1);
    });
});
