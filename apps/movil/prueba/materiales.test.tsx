// Paso de materiales de la orden en la app nativa: buscar por nombre o código en las existencias de
// la bodega del técnico (del paquete, sin señal), ver lo que queda y la modalidad, el comodato que se
// registra en «Equipos», y la cantidad con − / + (48 px) y atajos. Motor real sobre memoria, claro
// y oscuro.
import type { Existencia } from "@iris/sync";
import { itemDePrueba, montar, ordenDePrueba, paqueteDePrueba } from "@iris/sync/pruebas";
import { fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { abrirCampo, avisoArriba } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const LINEAS: Existencia[] = [
    { productoId: 900, descripcion: "Cable drop 1 hilo", codigo: "CAB-DROP-1H", unidad: "m", cantidad: 120, modalidad: "consumible_gasto" },
    { productoId: 901, descripcion: "Conector SC/APC", codigo: "CON-SCAPC", unidad: "u", cantidad: 4, modalidad: "consumible_cobro" },
    { productoId: 902, descripcion: "ONU ZTE F670L", codigo: "ONU-F670", unidad: "u", cantidad: 2, modalidad: "comodato" },
];

/** OT-000001 en sitio, con las existencias de «Bodega Norte» en el paquete. */
async function enSitioConBodega() {
    const m = await montar();
    m.transporte.paquete = (fecha) => ({
        ...paqueteDePrueba(fecha),
        ruta: undefined as never,
        ordenes: [ordenDePrueba(1, { tipo: "instalacion", estado: "en_sitio", enSitioEn: "2026-10-28T15:10:00Z", checklist: [itemDePrueba(1)] })],
        existencias: { disponible: true, motivo: null, bodegas: [{ bodegaId: 7, nombre: "Bodega Norte", lineas: LINEAS }] },
    });
    await m.motor.cargarPaquete("2026-10-28");
    return m;
}

describe.each(["light", "dark"] as const)("materiales de la orden (%s)", (esquema) => {
    it("busca por código, muestra la existencia y registra con − / + y atajos, sin señal", async () => {
        const m = await enSitioConBodega();
        m.transporte.sinRed = true;
        await abrirCampo(m, { esquema, enLinea: () => false, vista: { tipo: "orden", id: 1, paso: "materiales" } });
        expect(await screen.findByText("De tu bodega: Bodega Norte")).toBeTruthy();
        const formulario = screen.getByTestId("formulario-material");
        // Sin texto: todos, con su código, lo que queda y la modalidad.
        expect(within(formulario).getAllByRole("radio")).toHaveLength(3);
        expect(within(formulario).getByText("CAB-DROP-1H · Quedan 120 m · Consumible")).toBeTruthy();

        await fireEvent.changeText(screen.getByLabelText("Buscar material"), "con-sc");
        expect(within(formulario).getAllByRole("radio")).toHaveLength(1);
        await fireEvent.press(screen.getByRole("radio", { name: "Conector SC/APC" }));
        expect(screen.getByTestId("modalidad-material")).toHaveTextContent("Se cobra al cliente");
        expect(screen.getByTestId("existencia-material")).toHaveTextContent("En tu bodega: 4 u");
        expect(screen.getByLabelText("Cantidad")).toHaveProp("value", "1");
        expect(screen.getByTestId("existencia-despues")).toHaveTextContent("Te quedarían 3 u.");
        await fireEvent.press(screen.getByRole("button", { name: "Agregar 5" }));
        expect(screen.getByLabelText("Cantidad")).toHaveProp("value", "6");
        expect(screen.getByTestId("existencia-despues")).toHaveTextContent("Supera lo que tienes por 2 u: se registra igual y se avisa al supervisor.");
        await fireEvent.press(screen.getByRole("button", { name: "Quitar uno" }));
        await fireEvent.press(screen.getByRole("button", { name: "Quitar uno" }));
        await fireEvent.press(screen.getByRole("button", { name: "Registrar material" }));
        await waitFor(() => expect(m.motor.obtenerEstado().cola.at(-1)).toMatchObject({ tipo: "material.usar", datos: { productoId: 901, cantidad: 4 } }));
        expect(avisoArriba()).toHaveTextContent(/Material registrado/);
        // La existencia del teléfono ya descuenta lo de la cola.
        await fireEvent.press(screen.getByRole("radio", { name: "Conector SC/APC" }));
        expect(screen.getByTestId("existencia-material")).toHaveTextContent("En tu bodega: 0 u");
    });

    it("un equipo en comodato no se registra como material: va con su serie en «Equipos»", async () => {
        const m = await enSitioConBodega();
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "materiales" } });
        await fireEvent.changeText(await screen.findByLabelText("Buscar material"), "zte");
        await fireEvent.press(screen.getByRole("radio", { name: "ONU ZTE F670L" }));
        expect(screen.getByTestId("modalidad-material")).toHaveTextContent("Equipo con serie");
        expect(screen.getByText("Se registra en «Equipos»")).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Registrar material" })).toBeNull();
        expect(screen.queryByLabelText("Cantidad")).toBeNull();
    });

    it("sin coincidencias lo dice", async () => {
        const m = await enSitioConBodega();
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "materiales" } });
        await fireEvent.changeText(await screen.findByLabelText("Buscar material"), "fibra 48 hilos");
        expect(screen.getByText("Ningún material de tu bodega coincide.")).toBeTruthy();
    });
});
