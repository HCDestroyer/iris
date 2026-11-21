// Al firmar, el gesto de dibujar no desplaza la pantalla: el lienzo real (sin el doble de Jest) se
// queda con el trazo y, mientras el dedo está en el recuadro, el marco apaga el desplazamiento.
import { montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen } from "@testing-library/react-native";
import { abrirCampo, paqueteCon } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

/** Un toque del sistema de respuesta de React Native en (x, y) del recuadro. */
function toque(x: number, y: number) {
    const t = { touchActive: true, startPageX: x, startPageY: y, startTimeStamp: 0, currentPageX: x, currentPageY: y, currentTimeStamp: 0, previousPageX: x, previousPageY: y, previousTimeStamp: 0 };
    return {
        nativeEvent: { locationX: x, locationY: y, pageX: x, pageY: y, touches: [], changedTouches: [], identifier: 0, target: 0, timestamp: 0 },
        touchHistory: { touchBank: [t], numberActiveTouches: 1, indexOfSingleActiveTouch: 0, mostRecentTimeStamp: 0 },
    };
}

describe.each(["light", "dark"] as const)("firmar no desplaza (%s)", (esquema) => {
    it("el lienzo gana el gesto y apaga el desplazamiento mientras dura el trazo", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio", enSitioEn: "2026-10-28T15:00:00Z", requiereFirma: true });
        await m.motor.cargarPaquete("2026-10-28");
        await m.motor.registrar("checklist.marcar", 1, { itemId: 1, valor: true });
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "cierre" } });

        const lienzo = await screen.findByTestId("lienzo-firma");
        const desplazamiento = screen.getByTestId("desplazamiento");
        expect(desplazamiento.props.scrollEnabled).toBe(true);
        // El lienzo pide el gesto desde el primer toque y no lo suelta al `ScrollView`.
        expect(lienzo.props.onStartShouldSetResponderCapture(toque(10, 10))).toBe(true);
        expect(lienzo.props.onResponderTerminationRequest(toque(10, 40))).toBe(false);

        await act(async () => {
            fireEvent(lienzo, "responderGrant", toque(10, 10));
        });
        expect(screen.getByTestId("desplazamiento").props.scrollEnabled).toBe(false);
        await act(async () => {
            fireEvent(lienzo, "responderMove", toque(10, 120));
            fireEvent(lienzo, "responderRelease", toque(10, 120));
        });
        expect(screen.getByTestId("desplazamiento").props.scrollEnabled).toBe(true);
        // El trazo quedó en el recuadro: el botón de la isla ya no pide la firma.
        expect(screen.getByRole("button", { name: "Borrar firma" })).toBeTruthy();

        // Si el sistema le quita el gesto (una llamada), el desplazamiento vuelve igual.
        await act(async () => {
            fireEvent(lienzo, "responderGrant", toque(20, 20));
        });
        expect(screen.getByTestId("desplazamiento").props.scrollEnabled).toBe(false);
        await act(async () => {
            fireEvent(lienzo, "responderTerminate", toque(20, 20));
        });
        expect(screen.getByTestId("desplazamiento").props.scrollEnabled).toBe(true);
    });
});
