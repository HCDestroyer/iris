// `src/componentes` sobre HeroUI Native: mismas props y el comportamiento que se ve. Zonas táctiles de
// 44 px o más, estados con icono + texto + color, el «Atrás» de Android cierra la hoja y el
// lienzo de firma no suelta el trazo al desplazamiento de la vista.
import { fireEvent, render, screen } from "@testing-library/react-native";
import { CircleAlert, Wifi } from "lucide-react-native";
import { useState } from "react";
import { StyleSheet } from "react-native";
import { Aviso, Boton, Campo, Cargando, Dialogo, Insignia, Opciones, Tarjeta, Tocable, ZONA_TACTIL } from "../src/componentes";
import { ControladorFirma, LienzoFirma } from "../src/pantallas/orden/LienzoFirma";
import { ProveedorTema } from "../src/tema/tema";

const alto = (id: string) => (StyleSheet.flatten(screen.getByTestId(id).props.style) as { minHeight?: number }).minHeight ?? 0;

describe.each(["light", "dark"] as const)("componentes con HeroUI Native (%s)", (esquema) => {
    const montar = (ui: React.ReactElement) => render(<ProveedorTema esquema={esquema}>{ui}</ProveedorTema>);

    it("Boton: 48 px, nombre accesible, deshabilitado y ocupado", async () => {
        const tocar = jest.fn();
        await montar(
            <>
                <Boton alPresionar={tocar} testID="b1">
                    Guardar
                </Boton>
                <Boton alPresionar={tocar} deshabilitado variante="secundario" testID="b2">
                    No
                </Boton>
                <Boton alPresionar={tocar} cargando etiqueta="Enviando la orden" testID="b3">
                    Enviando…
                </Boton>
            </>,
        );
        expect(alto("b1")).toBeGreaterThanOrEqual(ZONA_TACTIL);
        await fireEvent.press(screen.getByRole("button", { name: "Guardar" }));
        expect(tocar).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId("b2")).toBeDisabled();
        await fireEvent.press(screen.getByTestId("b2"));
        expect(tocar).toHaveBeenCalledTimes(1);
        expect(screen.getByRole("button", { name: "Enviando la orden" }).props.accessibilityState).toMatchObject({ busy: true, disabled: true });
    });

    it("Tocable: 44 px como mínimo, rol y `selected`", async () => {
        const tocar = jest.fn();
        await montar(
            <Tocable rol="tab" etiqueta="Hoy" seleccionado alPresionar={tocar} estilo={null} testID="t">
                <Cargando etiqueta="Leyendo" />
            </Tocable>,
        );
        expect(alto("t")).toBeGreaterThanOrEqual(ZONA_TACTIL);
        expect(screen.getByRole("tab", { name: "Hoy" })).toBeSelected();
        await fireEvent.press(screen.getByRole("tab", { name: "Hoy" }));
        expect(tocar).toHaveBeenCalled();
        expect(screen.getByLabelText("Leyendo")).toBeTruthy();
    });

    it("Opciones: radios con nombre, elegida marcada y el error con texto", async () => {
        function Prueba() {
            const [v, setV] = useState<"si" | "no" | null>(null);
            return (
                <Opciones
                    etiqueta="¿Hay señal?"
                    opciones={[
                        { valor: "si", etiqueta: "Sí" },
                        { valor: "no", etiqueta: "No" },
                    ]}
                    valor={v}
                    alCambiar={setV}
                    error={v === null ? "Elige una opción" : undefined}
                />
            );
        }
        await montar(<Prueba />);
        expect(screen.getByLabelText("¿Hay señal?")).toBeTruthy();
        expect(screen.getByRole("alert")).toHaveTextContent("Elige una opción");
        await fireEvent.press(screen.getByRole("radio", { name: "No" }));
        expect(screen.getByRole("radio", { name: "No" })).toBeChecked();
        expect(screen.getByRole("radio", { name: "Sí" })).not.toBeChecked();
        expect(screen.queryByRole("alert")).toBeNull();
        for (const r of screen.getAllByRole("radio")) {
            expect((StyleSheet.flatten(r.props.style) as { minHeight: number }).minHeight).toBeGreaterThanOrEqual(ZONA_TACTIL);
        }
    });

    it("Campo: etiqueta visible, escribe y muestra el error con texto", async () => {
        const cambiar = jest.fn();
        await montar(<Campo etiqueta="Serie" valor="" alCambiar={cambiar} error="La serie es obligatoria" sufijo="SN" />);
        await fireEvent.changeText(screen.getByLabelText("Serie"), "ABC123");
        expect(cambiar).toHaveBeenCalledWith("ABC123");
        expect(screen.getByText("Serie")).toBeTruthy();
        expect(screen.getByRole("alert")).toHaveTextContent("La serie es obligatoria");
        expect(screen.getByText("SN")).toBeTruthy();
    });

    it("Insignia y Aviso: icono + texto (nunca solo color); un error es `alert` y lo demás no", async () => {
        await montar(
            <>
                <Insignia tono="success" icono={Wifi} texto="En línea" testID="i" />
                <Aviso tono="error" icono={CircleAlert} titulo="No se pudo enviar" testID="a1" accion={<Boton alPresionar={() => undefined}>Reintentar</Boton>}>
                    Revisa la señal.
                </Aviso>
                <Aviso tono="info" titulo="Se envía al volver la señal" testID="a2" />
                <Tarjeta titulo="Resumen" descripcion="Lo hecho">
                    <Insignia tono="info" icono={Wifi} texto="Info" />
                </Tarjeta>
            </>,
        );
        expect(screen.getByTestId("i")).toHaveTextContent("En línea");
        expect(screen.getByTestId("a1").props.role).toBe("alert");
        expect(screen.getByTestId("a1")).toHaveTextContent(/No se pudo enviar.*Revisa la señal.*Reintentar/);
        expect(screen.getByTestId("a2").props.role).toBe("summary");
        expect(screen.getByRole("header", { name: "Resumen" })).toBeTruthy();
    });

    it("Dialogo: título accesible y el «Atrás» de Android lo cierra", async () => {
        const cerrar = jest.fn();
        await montar(
            <Dialogo abierto alCerrar={cerrar} titulo="Pausar OT-000001" descripcion="Despacho ve el motivo.">
                <Boton alPresionar={cerrar}>Volver</Boton>
            </Dialogo>,
        );
        expect(screen.getByRole("header", { name: "Pausar OT-000001" })).toBeTruthy();
        await fireEvent(screen.getByTestId("dialogo"), "requestClose");
        expect(cerrar).toHaveBeenCalledTimes(1);
    });
});

describe("lienzo de firma (Gesture Handler 3 en la raíz)", () => {
    it("toma el trazo y no lo suelta al desplazamiento de la vista", async () => {
                await render(
            <ProveedorTema esquema="light">
                <LienzoFirma alCambiar={() => undefined} />
            </ProveedorTema>,
        );
        const lienzo = screen.getByTestId("lienzo-firma");
        const toque = (t: number) => ({ nativeEvent: { touches: [{}], changedTouches: [] }, touchHistory: { numberActiveTouches: 1, mostRecentTimeStamp: t, indexOfSingleActiveTouch: 0, touchBank: [{ touchActive: true, startPageX: 1, startPageY: 1, currentPageX: 5, currentPageY: 9, previousPageX: 1, previousPageY: 1, startTimeStamp: 0, currentTimeStamp: t, previousTimeStamp: 0 }] } });
        expect(lienzo.props.onStartShouldSetResponderCapture(toque(0))).toBe(true);
        expect(lienzo.props.onMoveShouldSetResponderCapture(toque(1))).toBe(true);
        expect(lienzo.props.onResponderTerminationRequest(toque(2))).toBe(false);
        expect(screen.getByRole("button", { name: "Borrar firma" })).toBeTruthy();
    });

    it("cada toque empieza un trazo y «Borrar firma» los quita", () => {
        const c = new ControladorFirma();
        const vistos: number[][] = [];
        c.conectar((l) => vistos.push(l.trazos.map((t) => t.length)));
        c.empezar(1, 1);
        c.seguir(5, 8);
        c.seguir(9, 12);
        c.empezar(40, 40);
        expect(c.obtener().map((t) => t.length)).toEqual([3, 1]);
        c.borrar();
        expect(c.obtener()).toEqual([]);
        expect(vistos.at(-1)).toEqual([]);
    });
});
