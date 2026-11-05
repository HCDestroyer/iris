// Escáner de serie y MAC por campo con el motor real y la API simulada: abre con su objetivo, no
// acepta con un solo cuadro, lista varios códigos (lo del recuadro), elegir llena el campo correcto,
// «Seguir escaneando», QR con serie y MAC, aviso de repetido, linterna y sin permiso. El lector de
// expo-camera se simula: la prueba llama a `onBarcodeScanned` con resultados como los del teléfono.
import { montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import type { BarcodeScanningResult } from "expo-camera";
import { StyleSheet, Vibration } from "react-native";
import { codigoDeExpo } from "../src/campo/escaner";
import { abrirCampo, paqueteCon } from "./arnes";

interface PropsCamara {
    onBarcodeScanned?: (r: BarcodeScanningResult) => void;
    enableTorch?: boolean;
}
const camara: { props: PropsCamara | null; permiso: { granted: boolean; canAskAgain: boolean } } = { props: null, permiso: { granted: true, canAskAgain: true } };

jest.mock("expo-camera", () => ({
    CameraView: (props: PropsCamara) => {
        camara.props = props;
        return null;
    },
    useCameraPermissions: () => [camara.permiso, jest.fn()],
}));

/** Un resultado de expo-camera; `caja` en puntos de la vista (sin ella, `bounds` vacío como en iOS con Code 39). */
function resultado(data: string, type: string, caja?: { x: number; y: number; ancho: number; alto: number }): BarcodeScanningResult {
    return {
        data,
        type,
        cornerPoints: [],
        bounds: caja ? { origin: { x: caja.x, y: caja.y }, size: { width: caja.ancho, height: caja.alto } } : { origin: { x: 0, y: 0 }, size: { width: 0, height: 0 } },
    };
}

/** Cada código como un `onBarcodeScanned` (el lector nativo entrega uno por llamada). */
async function escanear(...codigos: BarcodeScanningResult[]) {
    await act(async () => {
        for (const c of codigos) {
            const leer = camara.props?.onBarcodeScanned;
            if (!leer) {
                throw new Error("el lector está detenido");
            }
            leer(c);
        }
    });
}

/** Termina la ventana de ~800 ms. */
const esperarVentana = () => act(() => new Promise<void>((r) => setTimeout(r, 900)));

const valor = (etiqueta: string) => (screen.getByLabelText(etiqueta).props as { value: string }).value;

async function abrirEquipos(esquema: "light" | "dark", equipos: { serie: string; mac: string }[] = []) {
    const m = await montar();
    m.transporte.paquete = paqueteCon({
        tipo: "instalacion",
        estado: "en_sitio",
        equipos: equipos.map((e, i) => ({ id: `eq-${i}`, accion: "instalar", serie: e.serie, mac: e.mac, modelo: "ZTE F670L", unidadId: null, condicion: null, validado: false, ocurridoEn: "2026-10-28T15:00:00Z" })),
    });
    await m.motor.cargarPaquete("2026-10-28");
    await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "materiales" } });
    await screen.findByRole("button", { name: "Escanear número de serie" });
    return m;
}

describe.each(["light", "dark"] as const)("escáner por campo (%s)", (esquema) => {
    beforeEach(() => {
        camara.props = null;
        camara.permiso = { granted: true, canAskAgain: true };
    });

    it("abre con su objetivo, no acepta con un cuadro y lista varios códigos; elegir llena solo la serie", async () => {
        const vibrar = jest.spyOn(Vibration, "vibrate").mockImplementation(() => undefined);
        await abrirEquipos(esquema);
        // Cada campo con su botón de 48 px.
        const boton = screen.getByTestId("escanear-serie");
        expect((StyleSheet.flatten(boton.props.style) as { minHeight: number }).minHeight).toBeGreaterThanOrEqual(44);
        expect(screen.getByRole("button", { name: "Escanear MAC" })).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: "Escanear número de serie" }));
        expect(await screen.findByText("Escaneando: Número de serie")).toBeTruthy();
        expect(screen.getByText("Apunta solo al código junto a S/N")).toBeTruthy();

        // La vista mide 390 × 700: solo cuentan los del recuadro (centro en 195, 350).
        await act(async () => {
            fireEvent(screen.getByTestId("visor"), "layout", { nativeEvent: { layout: { x: 0, y: 0, width: 390, height: 700 } } });
        });
        expect(screen.getByText("Detectados: 0")).toBeTruthy();
        const dentro = { x: 120, y: 330, ancho: 150, alto: 40 };
        await escanear(resultado("6901443254538", "ean13", dentro));
        expect(screen.getByText("Detectados: 1")).toBeTruthy();
        // Un cuadro no basta: nada se elige ni se muestra todavía.
        expect(screen.queryByTestId("hoja-codigos")).toBeNull();
        expect(vibrar).toHaveBeenCalledWith(40);
        await escanear(
            resultado("2102311TUN10K5000123", "code128", dentro),
            resultado("48575443A1B2C3D4", "code128", dentro),
            resultado("A0B1C2D3E4F5", "code128", dentro),
            resultado("6901443254538", "ean13", dentro),
            // El modelo, arriba de la etiqueta: fuera del recuadro.
            resultado("EG8145V5", "code128", { x: 120, y: 40, ancho: 150, alto: 40 }),
        );
        expect(screen.getByText("Detectados: 4")).toBeTruthy();
        await esperarVentana();

        const hoja = screen.getByTestId("hoja-codigos");
        expect(within(hoja).getByText("Elige el código del número de serie")).toBeTruthy();
        const filas = within(hoja).getAllByTestId("codigo-detectado");
        expect(filas).toHaveLength(4);
        expect(filas[0]).toHaveTextContent(/Código de barras.*Code 128.*2102311TUN10K5000123.*Parece serie/);
        expect(filas[2]).toHaveTextContent(/Parece MAC/);
        expect(filas[3]).toHaveTextContent(/6901443254538.*Parece código de producto \(EAN\/UPC\)/);
        expect(within(hoja).queryByText("EG8145V5")).toBeNull();
        // Con la hoja abierta no se lee.
        expect(camara.props?.onBarcodeScanned).toBeUndefined();

        await fireEvent.press(within(hoja).getByRole("button", { name: /2102311TUN10K5000123/ }));
        await waitFor(() => expect(screen.queryByTestId("escaner")).toBeNull());
        expect(valor("Número de serie")).toBe("2102311TUN10K5000123");
        expect(valor("MAC")).toBe("");
        expect(screen.getByTestId("escaneado")).toHaveTextContent("Escaneado");
        expect(screen.getByTestId("notificacion")).toHaveTextContent("Serie: 2102311TUN10K5000123");
        // Se puede editar: la insignia se quita.
        await fireEvent.changeText(screen.getByLabelText("Número de serie"), "2102311TUN10K5000124");
        expect(screen.queryByTestId("escaneado")).toBeNull();
        vibrar.mockRestore();
    });

    it("MAC: un solo candidato se confirma, «Seguir escaneando», QR con serie y MAC y aviso de repetido", async () => {
        jest.spyOn(Vibration, "vibrate").mockImplementation(() => undefined);
        await abrirEquipos(esquema, [{ serie: "ZTEGC8F1A2B3", mac: "A0:B1:C2:D3:E4:F6" }]);
        await fireEvent.press(screen.getByRole("button", { name: "Escanear MAC" }));
        expect(await screen.findByText("Escaneando: MAC")).toBeTruthy();
        expect(screen.getByText("Apunta solo al código junto a MAC")).toBeTruthy();

        // Linterna.
        await fireEvent.press(screen.getByRole("button", { name: "Encender linterna" }));
        expect(camara.props?.enableTorch).toBe(true);
        expect(screen.getByRole("button", { name: "Apagar linterna" })).toBeSelected();

        // Sin medida de la vista (ni posición) cuentan todos.
        await escanear(resultado("a0:b1:c2:d3:e4:f7", "code128"), resultado("6972632140024", "ean13"));
        await esperarVentana();
        let hoja = screen.getByTestId("hoja-codigos");
        expect(within(hoja).getByText("¿Usar A0:B1:C2:D3:E4:F7 como MAC?")).toBeTruthy();
        const otro = within(within(hoja).getByTestId("otros-codigos")).getByTestId("codigo-detectado");
        expect(otro).toHaveTextContent(/No es una MAC/);
        expect(otro).toBeDisabled();

        await fireEvent.press(within(hoja).getByRole("button", { name: "Seguir escaneando" }));
        expect(screen.queryByTestId("hoja-codigos")).toBeNull();

        // QR con la serie y la MAC ya registradas en esta orden: se avisa antes de usarlas.
        await escanear(resultado("SN:ZTEGC8F1A2B3;MAC:A0B1C2D3E4F6", "qr"));
        await esperarVentana();
        hoja = screen.getByTestId("hoja-codigos");
        expect(within(hoja).getByText("¿Usar la serie ZTEGC8F1A2B3 y la MAC A0:B1:C2:D3:E4:F6?")).toBeTruthy();
        expect(hoja).toHaveTextContent(/QR con serie y MAC/);
        expect(hoja).toHaveTextContent(/Esta serie y esta MAC ya están en otro equipo de esta orden/);
        await fireEvent.press(within(hoja).getByRole("button", { name: "Usar" }));
        await waitFor(() => expect(screen.queryByTestId("escaner")).toBeNull());
        expect(valor("Número de serie")).toBe("ZTEGC8F1A2B3");
        expect(valor("MAC")).toBe("A0:B1:C2:D3:E4:F6");
        expect(screen.getAllByTestId("escaneado")).toHaveLength(2);
        expect(screen.getByTestId("notificacion")).toHaveTextContent("Serie: ZTEGC8F1A2B3 · MAC: A0:B1:C2:D3:E4:F6");
    });

    it("sin permiso de cámara: lo explica y se escribe a mano", async () => {
        camara.permiso = { granted: false, canAskAgain: false };
        await abrirEquipos(esquema);
        await fireEvent.press(screen.getByRole("button", { name: "Escanear MAC" }));
        expect(await screen.findByText("Actívalo en los ajustes del teléfono o escribe la serie y la MAC a mano.")).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Encender linterna" })).toBeNull();
        await fireEvent.press(screen.getByRole("button", { name: "Escribir a mano" }));
        await waitFor(() => expect(screen.queryByTestId("escaner")).toBeNull());
    });
});

describe("codigoDeExpo", () => {
    it("toma `bounds` y `cornerPoints`; un rectángulo vacío no es posición", () => {
        expect(codigoDeExpo(resultado("X", "qr", { x: 1, y: 2, ancho: 3, alto: 4 }))).toEqual({ valor: "X", formato: "qr", caja: { x: 1, y: 2, ancho: 3, alto: 4 }, esquinas: null });
        expect(codigoDeExpo({ ...resultado("Y", "code128"), cornerPoints: [{ x: 5, y: 6 }] })).toEqual({ valor: "Y", formato: "code128", caja: null, esquinas: [{ x: 5, y: 6 }] });
    });
});
