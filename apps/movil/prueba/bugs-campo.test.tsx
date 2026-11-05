// Reporte del PM sobre la app «combinada» (0.12.0), en claro y oscuro: el encabezado sólido (la
// franja bajo la barra de estado y la barra compacta, que se va deslizándose sin transparentarse),
// la orden en pausa de solo lectura con «Reanudar», un solo «Guardar» en la isla de los pasos (sin
// botón por campo), los avisos con el `Toast` de HeroUI Native, las hojas con «X» y «Atrás» de
// Android (y la pregunta si hay cambios) y la búsqueda en vivo sobre el teclado.
import { itemDePrueba, montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { Keyboard, StyleSheet } from "react-native";
import { crearTema } from "../src/tema/tokens";
import { abrirCampo, avisoArriba, paqueteCon, sesion, tipos } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const estilo = (id: string) => StyleSheet.flatten(screen.getByTestId(id).props.style) as Record<string, unknown>;
const HEX = /^#[0-9a-f]{6}$/i;

/** OT-000001 en sitio con un sí/no y un número (Potencia, dBm). */
async function enSitio(extra: Parameters<typeof paqueteCon>[0] = {}) {
    const m = await montar();
    m.transporte.paquete = paqueteCon({
        estado: "en_sitio",
        enSitioEn: "2026-10-28T15:00:00Z",
        contacto: { nombre: "Jorge Méndez", telefono: "" },
        checklist: [itemDePrueba(1, { etiqueta: "Señal verificada" }), itemDePrueba(2, { etiqueta: "Potencia", tipo: "numero", unidad: "dBm" })],
        ...extra,
    });
    await m.motor.cargarPaquete("2026-10-28");
    return m;
}

describe.each(["light", "dark"] as const)("reporte del PM (%s)", (esquema) => {
    const { campo } = crearTema(esquema === "dark");

    it("encabezado: franja sólida bajo la barra de estado; la compacta es sólida, entra y se va sin transparentarse", async () => {
        const m = await enSitio();
        await abrirCampo(m, { esquema });
        expect(estilo("franja-superior")).toMatchObject({ position: "absolute", top: 0, backgroundColor: campo.lienzo });
        expect(String(estilo("franja-superior").backgroundColor)).toMatch(HEX);

        const desplazamiento = screen.getByTestId("desplazamiento");
        await act(async () => {
            fireEvent(within(desplazamiento).getByTestId("encabezado-campo").parent ?? desplazamiento, "layout", { nativeEvent: { layout: { height: 200 } } });
        });
        // Oculta: no se toca ni la leen los lectores (pero sigue montada para irse deslizándose).
        const barra = () => screen.getByTestId("barra-compacta", { includeHiddenElements: true });
        expect(barra().props.pointerEvents).toBe("none");
        expect(barra().props.accessibilityElementsHidden).toBe(true);
        await act(async () => {
            fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: 600 } } });
        });
        await act(async () => {
            fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: 500 } } });
        });
        expect(barra().props.pointerEvents).toBe("auto");
        const s = StyleSheet.flatten(barra().props.style) as Record<string, unknown>;
        // Sólida (el panel del tema, sin alfa) y sin opacidad animada: solo se mueve.
        expect(s.backgroundColor).toBe(campo.panel);
        expect(String(s.backgroundColor)).toMatch(HEX);
        expect(s.opacity).toBeUndefined();
        expect(within(barra()).getByRole("button", { name: "Tu cuenta" })).toBeTruthy();
    });

    it("orden en pausa: pasos de solo lectura con «Orden en pausa» y «Reanudar»; al reanudar se puede trabajar", async () => {
        const m = await enSitio();
        await m.motor.registrar("orden.pausar", 1, { motivo: "clima" });
        await abrirCampo(m, { esquema, enLinea: () => false, vista: { tipo: "orden", id: 1, paso: "checklist" } });

        const aviso = await screen.findByTestId("orden-pausada");
        expect(aviso).toHaveTextContent(/Orden en pausa: Mal clima/);
        expect(aviso).toHaveTextContent(/no se registra trabajo/);
        expect(screen.getByRole("radio", { name: "Sí" })).toBeDisabled();
        expect(screen.getByLabelText("Potencia")).toHaveProp("editable", false);
        expect(screen.getByTestId("guardar-paso")).toBeDisabled();
        // Ni el motor lo deja (y la API tampoco: 422 `orden_pausada`).
        await expect(m.motor.registrar("checklist.marcar", 1, { itemId: 1, valor: true })).rejects.toMatchObject({ codigo: "orden_pausada" });
        // Fotos: se ven, no se agregan.
        await fireEvent.press(screen.getByTestId("chip-paso-fotos"));
        expect(await screen.findByText("Paso 4 de 5", { exact: false })).toBeTruthy();
        expect(screen.queryByRole("button", { name: /Agregar foto/ })).toBeNull();
        expect(screen.getByTestId("orden-pausada")).toBeTruthy();

        await fireEvent.press(within(screen.getByTestId("orden-pausada")).getByRole("button", { name: /Reanudar/ }));
        await waitFor(() => expect(m.motor.obtenerOrden(1)?.estado).toBe("en_sitio"));
        expect(screen.queryByTestId("orden-pausada")).toBeNull();
        expect(await screen.findByRole("button", { name: /Agregar foto/ })).toBeTruthy();
        expect(tipos(m)).toEqual(["orden.pausar", "orden.reanudar"]);
    });

    it("orden en pausa sin `ordenes.reanudar`: no ofrece «Reanudar» y dice a quién pedirlo", async () => {
        const m = await enSitio();
        await m.motor.registrar("orden.pausar", 1, { motivo: "acceso" });
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "materiales" }, sesion: { ...sesion, permisos: ["campo.sincronizar", "ordenes.registrar"] } });
        const aviso = await screen.findByTestId("orden-pausada");
        expect(within(aviso).queryByRole("button", { name: /Reanudar/ })).toBeNull();
        expect(aviso).toHaveTextContent(/pídeselo a despacho/);
    });

    it("pasos: un solo «Guardar» en la isla (como la del menú), sin botón por campo; guarda al salir del campo y avisa", async () => {
        const m = await enSitio();
        await abrirCampo(m, { esquema, enLinea: () => false, vista: { tipo: "orden", id: 1, paso: "checklist" } });
        expect(await screen.findByText("Paso 2 de 5", { exact: false })).toBeTruthy();
        // La isla de pasos: misma píldora que la de menú (sólida, redonda, con borde).
        expect(estilo("barra-pasos")).toMatchObject({ backgroundColor: campo.isla, borderRadius: 999, borderWidth: 1 });
        expect(screen.queryByTestId("navegacion-inferior")).toBeNull();
        expect(screen.getAllByRole("button", { name: /^Guardar/ })).toHaveLength(1);
        expect(screen.queryByRole("button", { name: "Guardar Potencia" })).toBeNull();
        expect(screen.getByRole("button", { name: "Anterior" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Siguiente: materiales y equipos" })).toBeTruthy();

        // Lo que no valida queda escrito y «Guardar» dice qué campo falló.
        await fireEvent.changeText(screen.getByLabelText("Potencia"), "abc");
        expect(screen.getByRole("button", { name: "Guardar (1 sin guardar)" })).toBeTruthy();
        await fireEvent.press(screen.getByTestId("guardar-paso"));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/No se guardó «Potencia»/));
        expect(tipos(m)).toEqual([]);
        // Pasar de paso con un error: se queda (y lo dijo).
        await fireEvent.press(screen.getByRole("button", { name: "Siguiente: materiales y equipos" }));
        expect(screen.getByText("Paso 2 de 5", { exact: false })).toBeTruthy();

        await fireEvent.changeText(screen.getByLabelText("Potencia"), "-19.5");
        await fireEvent.press(screen.getByTestId("guardar-paso"));
        await waitFor(() => expect(tipos(m)).toEqual(["checklist.marcar"]));
        expect(m.motor.obtenerEstado().cola.at(-1)).toMatchObject({ datos: { itemId: 2, valor: -19.5 } });
        await waitFor(() => expect(avisoArriba()).toHaveTextContent("Cambio guardado en el teléfono. Se enviarán al volver la señal."));
        expect(screen.getByRole("button", { name: "Guardar" })).toBeTruthy();
        // Sin nada escrito, «Guardar» dice que todo está guardado y cuánto falta por enviar.
        await fireEvent.press(screen.getByTestId("guardar-paso"));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/Todo está guardado en el teléfono · 1 por enviar/));

        // Al salir del campo también se guarda solo (sin tocar nada más).
        await fireEvent.changeText(screen.getByLabelText("Potencia"), "-20");
        await fireEvent(screen.getByLabelText("Potencia"), "endEditing");
        await waitFor(() => expect(tipos(m)).toEqual(["checklist.marcar", "checklist.marcar"]));
        // Un sí/no entra al tocarlo.
        await fireEvent.press(screen.getByRole("radio", { name: "Sí" }));
        await waitFor(() => expect(tipos(m)).toHaveLength(3));
        await fireEvent.press(screen.getByRole("button", { name: "Siguiente: materiales y equipos" }));
        expect(await screen.findByText("Paso 3 de 5", { exact: false })).toBeTruthy();
    });

    it("avisos: el `Toast` de HeroUI Native entra desde arriba con «Cerrar aviso» y el nuevo reemplaza al anterior", async () => {
        const m = await enSitio();
        await abrirCampo(m, { esquema, enLinea: () => false, vista: { tipo: "orden", id: 1, paso: "checklist" } });
        await fireEvent.press(await screen.findByRole("radio", { name: "Sí" }));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/«Señal verificada» guardado/));
        await fireEvent.press(screen.getByRole("radio", { name: "No" }));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/«Señal verificada» guardado/));
        // Uno a la vez (más la copia invisible con que HeroUI mide su alto).
        expect(screen.getAllByTestId("notificacion").length).toBeLessThanOrEqual(2);
        await fireEvent.press(within(avisoArriba()).getByRole("button", { name: "Cerrar aviso" }));
        await waitFor(() => expect(screen.queryAllByTestId("notificacion")).toHaveLength(0));
    });

    it("hojas: «X» con nombre accesible y «Atrás» de Android las cierran; con cambios, preguntan antes", async () => {
        const m = await enSitio();
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1 } });
        const titulo = () => screen.queryByRole("header", { name: "Pausar OT-000001" });

        // Sin cambios: la «X» cierra.
        await fireEvent.press(await screen.findByRole("button", { name: /^Pausar$/ }));
        expect(titulo()).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: "Cerrar" }));
        await waitFor(() => expect(titulo()).toBeNull());
        // Sin cambios: «Atrás» de Android (onRequestClose del Modal) cierra.
        await fireEvent.press(screen.getByRole("button", { name: /^Pausar$/ }));
        await fireEvent(screen.getByTestId("dialogo"), "requestClose");
        await waitFor(() => expect(titulo()).toBeNull());

        // Con un motivo elegido: la «X» pregunta; «Seguir editando» se queda.
        await fireEvent.press(screen.getByRole("button", { name: /^Pausar$/ }));
        await fireEvent.press(screen.getByRole("radio", { name: "Mal clima" }));
        await fireEvent.press(screen.getByRole("button", { name: "Cerrar" }));
        expect(screen.getByTestId("dialogo-confirmar")).toHaveTextContent(/¿Descartar los cambios\?/);
        await fireEvent.press(screen.getByRole("button", { name: "Seguir editando" }));
        expect(screen.queryByTestId("dialogo-confirmar")).toBeNull();
        expect(titulo()).toBeTruthy();
        // «Atrás» pregunta; otro «Atrás» sigue editando; «Atrás» y «Descartar» cierra sin pausar.
        await fireEvent(screen.getByTestId("dialogo"), "requestClose");
        expect(screen.getByTestId("dialogo-confirmar")).toBeTruthy();
        await fireEvent(screen.getByTestId("dialogo"), "requestClose");
        expect(screen.queryByTestId("dialogo-confirmar")).toBeNull();
        await fireEvent(screen.getByTestId("dialogo"), "requestClose");
        await fireEvent.press(screen.getByRole("button", { name: "Descartar" }));
        await waitFor(() => expect(titulo()).toBeNull());
        expect(tipos(m)).toEqual([]);

        // La hoja del «+» también tiene su «X».
        await abrirCampo(m, { esquema });
        await fireEvent.press(screen.getByRole("button", { name: "Acciones rápidas" }));
        expect(await screen.findByRole("header", { name: "Acciones rápidas" })).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: "Cerrar" }));
        await waitFor(() => expect(screen.queryByRole("header", { name: "Acciones rápidas" })).toBeNull());
    });

    it("buscar: filtra en vivo al escribir y la hoja sube por encima del teclado con el campo fijo arriba", async () => {
        type Oyente = (e: { endCoordinates: { height: number } }) => void;
        const oyentes = new Map<string, Oyente[]>();
        const original = Keyboard.addListener.bind(Keyboard);
        const espia = jest.spyOn(Keyboard, "addListener").mockImplementation(((evento: string, fn: Oyente) => {
            oyentes.set(evento, [...(oyentes.get(evento) ?? []), fn]);
            return original(evento as "keyboardDidShow", fn as never);
        }) as typeof Keyboard.addListener);
        try {
            const m = await enSitio();
            await abrirCampo(m, { esquema });
            await fireEvent.press(screen.getAllByRole("button", { name: "Buscar orden" })[0] ?? screen.getByTestId("desplazamiento"));
            expect(await screen.findByRole("header", { name: "Buscar orden" })).toBeTruthy();
            const campoBuscar = screen.getByLabelText("Buscar");
            expect(campoBuscar).toHaveProp("autoFocus", true);

            await fireEvent.changeText(campoBuscar, "jorge");
            await waitFor(() => expect(screen.getByTestId("resultados-busqueda")).toHaveTextContent(/1 orden/));
            expect(within(screen.getByTestId("resultados-busqueda")).getByRole("button", { name: "OT-000001, Jorge Méndez" })).toBeTruthy();
            await fireEvent.changeText(campoBuscar, "jorgex");
            await waitFor(() => expect(screen.getByTestId("sin-resultados")).toHaveTextContent(/«jorgex»/));
            await fireEvent.changeText(campoBuscar, "");
            await waitFor(() => expect(screen.queryByTestId("sin-resultados")).toBeNull());

            // Teclado abierto: la hoja deja su alto abajo y se acorta para quedar encima.
            const abrir = [...oyentes.entries()].filter(([e]) => /keyboard(Did|Will)Show/.test(e)).flatMap(([, fns]) => fns);
            expect(abrir.length).toBeGreaterThan(0);
            await act(async () => {
                for (const fn of abrir) {
                    fn({ endCoordinates: { height: 300 } });
                }
            });
            expect(StyleSheet.flatten(screen.getByTestId("buscar-orden-velo").props.style)).toMatchObject({ paddingBottom: 300 });
            expect(screen.getByLabelText("Buscar")).toBeTruthy();
            // Tocar un resultado lleva a la orden.
            await fireEvent.changeText(screen.getByLabelText("Buscar"), "OT-000001");
            await waitFor(() => expect(screen.getByTestId("resultados-busqueda")).toBeTruthy());
            await fireEvent.press(within(screen.getByTestId("resultados-busqueda")).getByRole("button", { name: "OT-000001, Jorge Méndez" }));
            expect(await screen.findByRole("header", { name: "OT-000001" })).toBeTruthy();
        } finally {
            espia.mockRestore();
        }
    });
});
