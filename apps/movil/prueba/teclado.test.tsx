// Reporte del PM (APK 0.16.0, Android): «La búsqueda sigue quedando debajo del teclado, igual el input
// del chat». El arreglo anterior medía el teclado con los eventos `Keyboard` de React Native y lo
// ponía de `paddingBottom`: con edge-to-edge esos eventos restan la barra de navegación y, dentro de
// un `Modal` (la hoja de búsqueda, notas, el «+»), ni llegan. Ahora todo pasa por
// `react-native-keyboard-controller`: `EvitarTeclado` (`KeyboardAvoidingView` con `behavior="padding"`
// en Android e iOS) y `useAltoTeclado` (la altura del teclado sobre la pantalla entera).
import { render, renderHook, screen, within } from "@testing-library/react-native";
import { Platform, StyleSheet, Text } from "react-native";
import { useKeyboardState } from "react-native-keyboard-controller";
import { Campo, Dialogo, EvitarTeclado, useAltoTeclado } from "../src/componentes";
import { TemaDePrueba } from "./raiz";

const estadoTeclado = useKeyboardState as jest.Mock;

function conTeclado(abierto: boolean, alto = 320) {
    estadoTeclado.mockImplementation((selector?: (s: { isVisible: boolean; height: number }) => unknown) => {
        const s = { isVisible: abierto, height: abierto ? alto : 0 };
        return selector ? selector(s) : s;
    });
}

afterEach(() => {
    conTeclado(false);
});

describe.each(["android", "ios"] as const)("teclado en %s", (so) => {
    const original = Platform.OS;
    beforeAll(() => {
        Platform.OS = so;
    });
    afterAll(() => {
        Platform.OS = original;
    });

    it("EvitarTeclado: el KeyboardAvoidingView de keyboard-controller con padding (también en Android)", async () => {
        await render(
            <EvitarTeclado testID="evitar">
                <Text>Hola</Text>
            </EvitarTeclado>,
        );
        const contenedor = screen.getByTestId("evitar");
        expect(contenedor.props.behavior).toBe("padding");
        expect(StyleSheet.flatten(contenedor.props.style)).toMatchObject({ flex: 1 });
    });

    it("useAltoTeclado: el alto del teclado abierto y 0 cerrado", async () => {
        conTeclado(true, 336);
        expect((await renderHook(() => useAltoTeclado())).result.current).toBe(336);
        conTeclado(false);
        expect((await renderHook(() => useAltoTeclado())).result.current).toBe(0);
    });

    it.each(["light", "dark"] as const)("Dialogo (%s): la hoja, su campo fijo y la lista van dentro de EvitarTeclado y se acortan sobre el teclado", async (esquema) => {
        conTeclado(true, 300);
        await render(
            <TemaDePrueba esquema={esquema}>
                <Dialogo abierto alCerrar={jest.fn()} titulo="Buscar orden" testID="hoja" fijo={<Campo etiqueta="Buscar" valor="" alCambiar={jest.fn()} />}>
                    <Text>Resultados</Text>
                </Dialogo>
            </TemaDePrueba>,
        );
        const teclado = screen.getByTestId("hoja-teclado");
        expect(teclado.props.behavior).toBe("padding");
        expect(within(teclado).getByLabelText("Buscar")).toBeTruthy();
        expect(within(teclado).getByText("Resultados")).toBeTruthy();
        // El velo ya no suma el alto a mano (lo hace el contenedor): nunca dos veces.
        expect(StyleSheet.flatten(screen.getByTestId("hoja-velo").props.style).paddingBottom).toBeUndefined();
    });
});
