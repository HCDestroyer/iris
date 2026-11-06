// Acciones de la orden y notas editables (como la PWA): encabezado con «● Estado · Tipo · ventana» y
// «⋯», acción principal en tinta con su grupo, el aviso de pausa con solo «Reanudar», el cliente con
// «Cómo llegar» y «Llamar», y las notas (ver, editar con contador, eliminar con pregunta). Claro y oscuro.
import type { NotaOrden, OrdenCampo } from "@iris/sync";
import { montar } from "@iris/sync/pruebas";
import { fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { CLARO, OSCURO } from "../src/tema/tokens";
import { abrirCampo, avisoArriba, paqueteCon, tipos } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const DE_DESPACHO: NotaOrden = { id: "0192f0a0-0000-7000-8000-0000000000d1", texto: "Portón negro. Perro en el patio.", autorId: 9, autorNombre: "Despacho", canal: "panel", ocurridoEn: new Date(Date.now() - 3_600_000).toISOString(), version: 1, editadaEn: null };
const MIA: NotaOrden = { id: "0192f0a0-0000-7000-8000-0000000000d2", texto: "Router en la sala.", autorId: 5, autorNombre: "Téc. Ana", canal: "campo", ocurridoEn: new Date(Date.now() - 600_000).toISOString(), version: 1, editadaEn: null };

async function abrir(esquema: "light" | "dark", orden: Partial<OrdenCampo>) {
    const m = await montar();
    m.transporte.paquete = paqueteCon({ notas: [DE_DESPACHO, MIA], direccion: "6a avenida 12-34, zona 1", ...orden });
    await m.motor.cargarPaquete("2026-10-28");
    m.transporte.sinRed = true;
    await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1 }, enLinea: () => false });
    return m;
}

describe.each(["light", "dark"] as const)("acciones de la orden (%s)", (esquema) => {
    it("en sitio: encabezado, «Continuar trabajo» en tinta y [Pausar | No se pudo]; el cliente con sus botones redondos", async () => {
        await abrir(esquema, { estado: "en_sitio", enSitioEn: "2026-10-28T15:00:00Z" });
        expect(await screen.findByTestId("linea-estado")).toHaveProp("accessibilityLabel", expect.stringMatching(/^En sitio · Visita técnica/));
        expect(screen.getByTestId("accion-principal")).toHaveTextContent("Continuar trabajo");
        const grupo = screen.getByTestId("grupo-acciones");
        expect(within(grupo).getAllByRole("button").map((b) => b.props.accessibilityLabel)).toEqual(["Pausar", "No se pudo"]);
        expect(within(screen.getByTestId("cliente-orden")).getByRole("button", { name: "Cómo llegar" })).toBeTruthy();
        // «Pausar» del grupo abre su hoja.
        await fireEvent.press(screen.getByTestId("grupo-pausar"));
        expect(await screen.findByText("Pausar OT-000001")).toBeTruthy();
    });

    it("programada: [En camino | No se pudo] y «En camino» va a la cola", async () => {
        const m = await abrir(esquema, { estado: "programada" });
        expect(await screen.findByTestId("accion-principal")).toHaveTextContent("Empezar trabajo");
        expect(within(screen.getByTestId("grupo-acciones")).getAllByRole("button").map((b) => b.props.accessibilityLabel)).toEqual(["En camino", "No se pudo"]);
        await fireEvent.press(screen.getByTestId("grupo-en-camino"));
        await waitFor(() => expect(tipos(m)).toEqual(["orden.iniciar_camino"]));
    });

    it("menú «⋯»: Agregar nota, Agregar formulario (deshabilitado con el motivo si no aplica), Historial, Copiar dirección", async () => {
        await abrir(esquema, { estado: "programada", cliente: undefined, clienteId: 3101 });
        await fireEvent.press(await screen.findByTestId("menu-orden"));
        const menu = await screen.findByTestId("hoja-menu-orden");
        expect(within(menu).getAllByRole("button").map((b) => b.props.accessibilityLabel).filter((n: string) => n !== "Cerrar")).toEqual([
            "Agregar nota",
            "Agregar formulario: Disponible con el trabajo empezado (en camino o en sitio).",
            "Historial del cliente",
            "Copiar dirección",
        ]);
        expect(within(menu).getByTestId("opcion-Agregar formulario")).toBeDisabled();
        await fireEvent.press(within(menu).getByTestId("opcion-Agregar nota"));
        expect(await screen.findByTestId("hoja-nueva-nota")).toBeTruthy();
    });

    it("con el trabajo empezado, «Agregar formulario» del menú abre el catálogo", async () => {
        await abrir(esquema, { estado: "en_sitio", enSitioEn: "2026-10-28T15:00:00Z" });
        await fireEvent.press(await screen.findByTestId("menu-orden"));
        const opcion = within(await screen.findByTestId("hoja-menu-orden")).getByTestId("opcion-Agregar formulario");
        expect(opcion).not.toBeDisabled();
        await fireEvent.press(opcion);
        expect(await screen.findByText("Agregar formulario")).toBeTruthy();
        expect(screen.getByText("Elige uno del catálogo. Se guarda en el teléfono y se envía al volver la señal.")).toBeTruthy();
    });

    it("pausada: el aviso «En pausa desde HH:MM · motivo» y solo «Reanudar»", async () => {
        await abrir(esquema, {
            estado: "pausada",
            pausadaDesde: "en_sitio",
            pausadaEn: "2026-10-28T18:10:00Z",
            pausas: [{ id: 1, motivo: "otro", nota: "Almuerzo", desdeEstado: "en_sitio", inicio: "2026-10-28T18:10:00Z", fin: null, duracionSeg: null, cierre: null, pausadaPor: 5, cerradaPor: null }],
        });
        expect(await screen.findByTestId("pausa-orden")).toHaveProp("accessibilityLabel", "En pausa desde 12:10 · Almuerzo");
        // «Reanudar» en «tinta», como «Continuar trabajo» (no teal).
        expect(screen.getByTestId("reanudar")).toHaveStyle({ backgroundColor: (esquema === "light" ? CLARO : OSCURO).texto });
        expect(screen.getByTestId("reanudar")).not.toHaveStyle({ backgroundColor: (esquema === "light" ? CLARO : OSCURO).primario });
        expect(screen.queryByTestId("accion-principal")).toBeNull();
        expect(screen.queryByTestId("grupo-acciones")).toBeNull();
    });

    it("notas: la de despacho solo se ve; la propia se edita con contador y se elimina tras preguntar", async () => {
        const m = await abrir(esquema, { estado: "en_sitio", enSitioEn: "2026-10-28T15:00:00Z" });
        await fireEvent.press(await screen.findByTestId(`nota-${DE_DESPACHO.id}`));
        expect(await screen.findByText(/^Despacho · (hoy|ayer) \d{2}:\d{2} · desde el panel$/)).toBeTruthy();
        expect(screen.queryByTestId("editar-nota")).toBeNull();
        await fireEvent.press(screen.getByTestId("hoja-nota-cerrar"));

        await fireEvent.press(screen.getByTestId(`nota-${MIA.id}`));
        await fireEvent.press(await screen.findByTestId("editar-nota"));
        expect(screen.getByTestId("contador-nota")).toHaveTextContent("18 / 4000");
        expect(screen.getByTestId("guardar-nota")).toBeDisabled();
        await fireEvent.changeText(screen.getByLabelText("Texto de la nota"), "Router en la sala, junto a la TV.");
        expect(screen.getByTestId("contador-nota")).toHaveTextContent("33 / 4000");
        await fireEvent.press(screen.getByTestId("guardar-nota"));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/Nota editada/));
        expect(m.motor.obtenerEstado().cola.at(-1)).toMatchObject({ tipo: "nota.editar", datos: { notaId: MIA.id, versionNota: 1, texto: "Router en la sala, junto a la TV." } });

        await fireEvent.press(await screen.findByTestId(`nota-${MIA.id}`));
        await fireEvent.press(await screen.findByTestId("eliminar-nota"));
        expect(await screen.findByText("¿Eliminar la nota?")).toBeTruthy();
        expect(screen.getByText("Se quita de la orden. Queda registrada en el historial de la orden.")).toBeTruthy();
        await fireEvent.press(screen.getByTestId("confirmar-eliminar-nota"));
        await waitFor(() => expect(screen.queryByTestId(`nota-${MIA.id}`)).toBeNull());
        expect(m.motor.obtenerEstado().cola.at(-1)).toMatchObject({ tipo: "nota.eliminar", datos: { notaId: MIA.id, versionNota: 2 } });
    });
});
