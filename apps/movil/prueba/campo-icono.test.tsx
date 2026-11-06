// Reporte del PM (APK 0.16.0, login en oscuro): el sobre y el candado quedaban fuera de la caja, a
// la izquierda, y el ojo fuera a la derecha (la contraseña salía más angosta que el correo). Ahora
// el icono y el ojo son adornos del `InputGroup` de HeroUI Native: dentro de la caja, centrados, y la
// caja a todo el ancho; los mismos 48 px y 20 px de icono en todos los campos.
import { fireEvent, render, screen, within } from "@testing-library/react-native";
import { Lock, Mail, Search } from "lucide-react-native";
import { StyleSheet } from "react-native";
import { ALTO_CAMPO, Campo, TAMANO_ICONO_CAMPO } from "../src/componentes";
import { TemaDePrueba } from "./raiz";

const estilo = (id: string) => StyleSheet.flatten(screen.getByTestId(id, { includeHiddenElements: true }).props.style) as Record<string, unknown> | undefined;

describe.each(["light", "dark"] as const)("Campo con icono (%s)", (esquema) => {
    const montar = (ui: React.ReactElement) => render(<TemaDePrueba esquema={esquema}>{ui}</TemaDePrueba>);

    it("el icono va dentro de la caja como adorno inicial, centrado y sin tapar el texto", async () => {
        await montar(<Campo etiqueta="Correo" icono={Mail} valor="tecnico@empresa.gt" alCambiar={jest.fn()} testID="correo" />);
        const caja = screen.getByTestId("correo-caja");
        // El adorno y la entrada comparten la caja (antes, el icono era una columna aparte).
        expect(within(caja).getByTestId("correo-icono", { includeHiddenElements: true })).toBeTruthy();
        expect(within(caja).getByTestId("correo")).toBeTruthy();
        // Adorno de HeroUI: decorativo (no se toca, lo lee la etiqueta del campo) y con relleno simétrico.
        const icono = screen.getByTestId("correo-icono", { includeHiddenElements: true });
        expect(icono.props.pointerEvents).toBe("none");
        expect(estilo("correo-icono") ?? {}).toMatchObject({ paddingStart: 14 });
        expect(estilo("correo") ?? {}).toMatchObject({ minHeight: ALTO_CAMPO });
        expect(TAMANO_ICONO_CAMPO).toBe(20);
        expect(screen.getByLabelText("Correo")).toBeTruthy();
    });

    it("contraseña: candado al inicio y el ojo al final dentro de la misma caja, que ocupa todo el ancho", async () => {
        const cambiar = jest.fn();
        await montar(
            <>
                <Campo etiqueta="Correo" icono={Mail} valor="" alCambiar={jest.fn()} testID="correo" />
                <Campo etiqueta="Contraseña" icono={Lock} secreto valor="secreta" alCambiar={cambiar} testID="clave" />
            </>,
        );
        const caja = screen.getByTestId("clave-caja");
        const fin = within(caja).getByTestId("clave-fin");
        const ojo = within(fin).getByRole("button", { name: "Mostrar contraseña" });
        expect(StyleSheet.flatten(ojo.props.style)).toMatchObject({ width: ALTO_CAMPO, height: ALTO_CAMPO });
        // Ninguna de las dos cajas tiene un ancho propio: las dos se estiran igual que el correo.
        for (const id of ["correo-caja", "clave-caja"]) {
            expect((estilo(id) ?? {}).width).toBeUndefined();
        }
        expect(screen.getByTestId("clave").props.secureTextEntry).toBe(true);
        await fireEvent.press(ojo);
        expect(screen.getByTestId("clave").props.secureTextEntry).toBe(false);
        expect(within(fin).getByRole("button", { name: "Ocultar contraseña" })).toBeTruthy();
    });

    it("con el campo deshabilitado, el ojo tampoco se puede tocar", async () => {
        await montar(<Campo etiqueta="Contraseña" icono={Lock} secreto editable={false} valor="secreta" alCambiar={jest.fn()} testID="clave" />);
        const ojo = screen.getByRole("button", { name: "Mostrar contraseña" });
        expect(ojo.props.accessibilityState).toMatchObject({ disabled: true });
        await fireEvent.press(ojo);
        expect(screen.getByTestId("clave").props.secureTextEntry).toBe(true);
    });

    it("búsqueda y unidad: la lupa al inicio y la unidad al final, también dentro", async () => {
        await montar(
            <>
                <Campo etiqueta="Buscar" icono={Search} valor="" alCambiar={jest.fn()} testID="buscar" />
                <Campo etiqueta="Potencia" sufijo="dBm" valor="" alCambiar={jest.fn()} testID="potencia" />
            </>,
        );
        expect(within(screen.getByTestId("buscar-caja")).getByTestId("buscar-icono", { includeHiddenElements: true })).toBeTruthy();
        const fin = within(screen.getByTestId("potencia-caja")).getByTestId("potencia-fin");
        // La unidad sí la leen los lectores de pantalla (no es adorno).
        expect(within(fin).getByText("dBm")).toBeTruthy();
    });
});
