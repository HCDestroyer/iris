// Reporte del PM (nov 2026): «la app, cuando se va a firmar, el botón lo tiene hasta abajo y no está
// flotante». «Firmar y cerrar orden» y «Confirmar recepción» van en una isla fija abajo (como la de
// los pasos), fuera del desplazamiento: siempre a la vista sin bajar, sobre el área segura y sin
// otra isla encima ni debajo. Firmar apaga el desplazamiento de la pantalla mientras dura el trazo.
import { montar } from "@iris/sync/pruebas";
import { fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { StyleSheet } from "react-native";
import { abrirCampo, InventarioFalso, paqueteCon, trasladoDePrueba } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));
jest.mock("../src/pantallas/orden/LienzoFirma", () => jest.requireActual("./firma-falsa"));

const MARGEN = { top: 24, bottom: 34, left: 0, right: 0 };

/** La isla fija: fuera del `ScrollView`, abajo sobre el margen seguro, sin otra isla. */
function comprobarIsla(prueba: string) {
    const isla = screen.getByTestId("isla-accion");
    // Fuera del desplazamiento: no hace falta bajar para verla.
    expect(within(screen.getByTestId("desplazamiento")).queryByTestId(prueba)).toBeNull();
    expect(within(isla).getByTestId(prueba)).toBeTruthy();
    const contenedor = isla.parent;
    expect(StyleSheet.flatten(contenedor?.props.style)).toMatchObject({ position: "absolute", bottom: MARGEN.bottom + 24 });
    // Sólida y de 44 px o más.
    expect(StyleSheet.flatten(isla.props.style).backgroundColor).toEqual(expect.any(String));
    expect((StyleSheet.flatten(screen.getByTestId(prueba).props.style) as { minHeight: number }).minHeight).toBeGreaterThanOrEqual(44);
    expect(screen.queryByTestId("navegacion-inferior")).toBeNull();
    expect(screen.queryByTestId("barra-pasos")).toBeNull();
}

describe.each(["light", "dark"] as const)("botón fijo de las pantallas con firma (%s)", (esquema) => {
    it("firma y cierre: «Firmar y cerrar orden» en la isla fija, valida, cierra y «Anterior» vuelve a las fotos", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio", enSitioEn: "2026-10-28T15:00:00Z", requiereFirma: true, contacto: { nombre: "Juan Pérez", telefono: "" } });
        await m.motor.cargarPaquete("2026-10-28");
        await m.motor.registrar("checklist.marcar", 1, { itemId: 1, valor: true });
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "cierre" }, margen: MARGEN });

        await screen.findByTestId("listo-para-firmar");
        comprobarIsla("firmar-y-cerrar");
        expect(screen.getByTestId("firmar-y-cerrar")).toHaveTextContent("Firmar y cerrar orden");
        // El contenido deja sitio debajo para la isla (nunca tapa el recuadro de la firma).
        const relleno = StyleSheet.flatten(screen.getByTestId("desplazamiento").props.contentContainerStyle) as { paddingBottom: number };
        expect(relleno.paddingBottom).toBeGreaterThanOrEqual(MARGEN.bottom + 24 + 54 + 24);

        // Sin trazo, la isla valida igual que antes.
        await fireEvent.press(screen.getByTestId("firmar-y-cerrar"));
        expect(await screen.findByTestId("error-firma")).toHaveTextContent("Pide al cliente que firme en el recuadro");

        // «Anterior» redondo, como en los pasos: a las fotos, con la isla de pasos de nuevo.
        await fireEvent.press(within(screen.getByTestId("isla-accion")).getByRole("button", { name: "Anterior" }));
        expect(await screen.findByTestId("barra-pasos")).toBeTruthy();
        expect(screen.queryByTestId("isla-accion")).toBeNull();
        await fireEvent.press(screen.getByTestId("paso-siguiente"));

        await fireEvent.press(await screen.findByLabelText(/Firmar en Recuadro para la firma del cliente/));
        await fireEvent.press(screen.getByTestId("firmar-y-cerrar"));
        await waitFor(() => expect(m.motor.obtenerOrden(1)?.estado).toBe("cerrada"));
        // Cerrada no hay acción al pie: vuelve la pantalla sin isla del paso final.
        expect(await screen.findByTestId("orden-cerrada")).toBeTruthy();
        expect(screen.queryByTestId("isla-accion")).toBeNull();
    });

    it("recepción de un traslado: «Confirmar recepción» en la isla fija y «Por recibir» vuelve a la lista", async () => {
        const m = await montar();
        const inventario = new InventarioFalso();
        inventario.traslados = [trasladoDePrueba(31)];
        await abrirCampo(m, { esquema, inventario, vista: { tipo: "traslado", id: 31 }, margen: MARGEN });

        await screen.findByTestId("linea-2");
        comprobarIsla("confirmar-recepcion");
        await fireEvent.press(screen.getByTestId("confirmar-recepcion"));
        expect(await screen.findByTestId("error-firma")).toHaveTextContent("Firma en el recuadro para confirmar que recibiste");
        await fireEvent.press(within(screen.getByTestId("isla-accion")).getByRole("button", { name: "Por recibir" }));
        expect(await screen.findByTestId("navegacion-inferior")).toBeTruthy();
        expect(screen.queryByTestId("isla-accion")).toBeNull();
    });
});
