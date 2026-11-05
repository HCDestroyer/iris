// Reporte del PM sobre la app «combinada» (0.12.0), en claro y oscuro: el encabezado sólido (la
// franja bajo la barra de estado y la barra compacta, que se va deslizándose sin transparentarse),
// la orden en pausa de solo lectura con «Reanudar», la isla de los pasos (sin «Guardar» si no hay
// cambios; «Guardar y seguir» con el contador si los hay, y «Guardado» al guardar), los avisos con el
// `Toast` de HeroUI Native bajo el margen seguro, las hojas con «X» y «Atrás» de
// Android (y la pregunta si hay cambios) y la búsqueda en vivo sobre el teclado.
import { itemDePrueba, montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { Keyboard, StyleSheet } from "react-native";
import { AIRE_AVISO, insetsAvisos } from "../src/tema/tema";
import { crearTema } from "../src/tema/tokens";
import { abrirCampo, avisoArriba, paqueteCon, sesion, tipos } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const estilo = (id: string) => StyleSheet.flatten(screen.getByTestId(id).props.style) as Record<string, unknown>;
const HEX = /^#[0-9a-f]{6}$/i;

/** El `paddingTop` del contenedor con que HeroUI Native separa el `Toast` del borde de arriba. */
function arribaDelAviso(aviso: ReturnType<typeof screen.getByTestId>): number | undefined {
    for (let n = aviso.parent; n; n = n.parent) {
        const top = (StyleSheet.flatten(n.props.style) as Record<string, unknown> | undefined)?.paddingTop;
        if (typeof top === "number") {
            return top;
        }
    }
    return undefined;
}

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
        expect(within(barra()).getByRole("button", { name: "Buscar orden" })).toBeTruthy();
    });

    it("arriba (edge-to-edge): la franja sólida siempre a la vista; al subir, la barra compacta entra y no desaparece al seguir desplazando", async () => {
        const m = await enSitio();
        await abrirCampo(m, { esquema, margen: { top: 48, bottom: 24, left: 0, right: 0 } });
        // La franja: el alto de la barra de estado, el lienzo del tema, sólida y encima de lo demás.
        expect(estilo("franja-superior")).toMatchObject({ position: "absolute", top: 0, height: 48, backgroundColor: campo.lienzo, zIndex: 20 });
        expect(String(estilo("franja-superior").backgroundColor)).toMatch(HEX);
        // La compacta va debajo de la franja, recortada: al irse no la cruza.
        expect(estilo("recorte-compacta")).toMatchObject({ position: "absolute", top: 48, overflow: "hidden" });
        const barra = () => screen.getByTestId("barra-compacta", { includeHiddenElements: true });
        const y = () => (StyleSheet.flatten(barra().props.style) as { transform?: { translateY?: number }[] }).transform?.[0]?.translateY;
        // Al abrir: el encabezado completo arriba (dentro del desplazamiento) y la compacta fuera.
        const desplazamiento = screen.getByTestId("desplazamiento");
        expect(within(desplazamiento).getByTestId("encabezado-campo")).toBeTruthy();
        await act(async () => {
            fireEvent(barra(), "layout", { nativeEvent: { layout: { height: 60 } } });
            fireEvent(within(desplazamiento).getByTestId("encabezado-campo").parent ?? desplazamiento, "layout", { nativeEvent: { layout: { height: 200 } } });
        });
        expect(y()).toBe(-(60 + 24));
        expect(StyleSheet.flatten(barra().props.style)).toMatchObject({ backgroundColor: campo.panel });

        // Al bajar se va con el contenido (la compacta sigue fuera).
        await act(async () => {
            fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: 600 } } });
        });
        expect(barra().props.pointerEvents).toBe("none");
        // Al subir entra la compacta, sólida…
        await act(async () => {
            fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: 500 } } });
        });
        expect(barra().props.pointerEvents).toBe("auto");
        await waitFor(() => expect(y()).toBe(0));
        // …y sigue ahí con más eventos del desplazamiento (cada uno vuelve a pintar el marco): antes,
        // el valor de JS «oculta» pisaba el del driver nativo y la barra desaparecía al instante.
        for (const pos of [497, 494, 490, 470, 430]) {
            await act(async () => {
                fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: pos } } });
            });
            expect(y()).toBe(0);
            expect(barra().props.pointerEvents).toBe("auto");
        }
        // Arriba del todo: el encabezado completo otra vez y la compacta se va.
        await act(async () => {
            fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: 0 } } });
        });
        await waitFor(() => expect(y()).toBe(-(60 + 24)));
        expect(estilo("franja-superior")).toMatchObject({ height: 48, backgroundColor: campo.lienzo });
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
        // Solo lectura: la isla no ofrece guardar nada; «Siguiente» solo navega (no guarda).
        expect(screen.queryByRole("button", { name: /Guardar/ })).toBeNull();
        await fireEvent.press(screen.getByRole("button", { name: "Siguiente: materiales y equipos" }));
        expect(await screen.findByText("Paso 3 de 5", { exact: false })).toBeTruthy();
        expect(screen.queryByRole("button", { name: /Guardar/ })).toBeNull();
        expect(tipos(m)).toEqual(["orden.pausar"]);
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

    it("pasos: sin cambios no hay «Guardar» y el primario avanza; con cambios, «Guardar y seguir» con el contador; al guardar, «Guardado»", async () => {
        const m = await enSitio();
        await abrirCampo(m, { esquema, enLinea: () => false, vista: { tipo: "orden", id: 1, paso: "checklist" } });
        expect(await screen.findByText("Paso 2 de 5", { exact: false })).toBeTruthy();
        // La isla de pasos: misma píldora que la de menú (sólida, redonda, con borde).
        expect(estilo("barra-pasos")).toMatchObject({ backgroundColor: campo.isla, borderRadius: 999, borderWidth: 1 });
        expect(screen.queryByTestId("navegacion-inferior")).toBeNull();
        // Sin cambios: ningún «Guardar» (ni en la isla ni por campo); el primario es «Siguiente».
        expect(screen.queryByRole("button", { name: /Guardar/ })).toBeNull();
        expect(screen.getByRole("button", { name: "Anterior" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Siguiente: materiales y equipos" })).toHaveTextContent("Siguiente");

        // Lo que no valida: «Guardar y seguir» con el contador se queda en el paso y dice qué campo falló.
        await fireEvent.changeText(screen.getByLabelText("Potencia"), "abc");
        const guardarYSeguir = () => screen.getByRole("button", { name: "Guardar y seguir (1 sin guardar)" });
        expect(guardarYSeguir()).toHaveTextContent("Guardar y seguir · 1");
        expect(screen.queryByRole("button", { name: /^Siguiente/ })).toBeNull();
        await fireEvent.press(guardarYSeguir());
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/No se guardó «Potencia»/));
        expect(screen.getByText("Paso 2 de 5", { exact: false })).toBeTruthy();
        expect(tipos(m)).toEqual([]);
        expect(guardarYSeguir()).toBeTruthy();
        await fireEvent.press(within(avisoArriba()).getByRole("button", { name: "Cerrar aviso" }));
        await waitFor(() => expect(screen.queryAllByTestId("notificacion")).toHaveLength(0));

        // Válido: guarda, avanza y confirma «Guardado» en la isla (sin aviso que tape nada).
        await fireEvent.changeText(screen.getByLabelText("Potencia"), "-19.5");
        await fireEvent.press(guardarYSeguir());
        await waitFor(() => expect(tipos(m)).toEqual(["checklist.marcar"]));
        expect(m.motor.obtenerEstado().cola.at(-1)).toMatchObject({ datos: { itemId: 2, valor: -19.5 } });
        expect(await screen.findByText("Paso 3 de 5", { exact: false })).toBeTruthy();
        const guardado = screen.getByTestId("guardado-paso");
        expect(guardado).toHaveTextContent("Guardado");
        expect(guardado).toHaveProp("accessibilityLabel", "Guardado: 1 cambio en el teléfono. Se enviarán al volver la señal.");
        expect(screen.queryAllByTestId("notificacion")).toHaveLength(0);
        expect(screen.queryByRole("button", { name: /Guardar/ })).toBeNull();
        expect(screen.getByRole("button", { name: "Siguiente: fotos" })).toBeTruthy();
        // Es breve: se va solo.
        await waitFor(() => expect(screen.queryByTestId("guardado-paso")).toBeNull(), { timeout: 3000 });

        // De vuelta, sin cambios: «Anterior» no guarda nada.
        await fireEvent.press(screen.getByRole("button", { name: "Anterior" }));
        expect(await screen.findByText("Paso 2 de 5", { exact: false })).toBeTruthy();
        expect(screen.queryByRole("button", { name: /Guardar/ })).toBeNull();
        // Al salir del campo también se guarda solo, y «Guardar y seguir» vuelve a ser «Siguiente».
        await fireEvent.changeText(screen.getByLabelText("Potencia"), "-20");
        expect(guardarYSeguir()).toBeTruthy();
        await fireEvent(screen.getByLabelText("Potencia"), "endEditing");
        await waitFor(() => expect(tipos(m)).toEqual(["checklist.marcar", "checklist.marcar"]));
        expect(screen.queryByRole("button", { name: /Guardar/ })).toBeNull();
        // También lo confirma la isla, en silencio (sin aviso arriba).
        expect(screen.getByTestId("guardado-paso")).toHaveTextContent("Guardado");
        expect(screen.queryAllByTestId("notificacion")).toHaveLength(0);
        // Un sí/no entra al tocarlo (no deja nada por guardar).
        await fireEvent.press(screen.getByRole("radio", { name: "Sí" }));
        await waitFor(() => expect(tipos(m)).toHaveLength(3));
        expect(screen.queryByRole("button", { name: /Guardar/ })).toBeNull();
        await fireEvent.press(screen.getByRole("button", { name: "Siguiente: materiales y equipos" }));
        expect(await screen.findByText("Paso 3 de 5", { exact: false })).toBeTruthy();
        // Antes de la firma, el primario dice «Ir a firma».
        await fireEvent.press(screen.getByRole("button", { name: "Siguiente: fotos" }));
        expect(await screen.findByText("Paso 4 de 5", { exact: false })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Ir a firma y cierre" })).toHaveTextContent("Ir a firma");
    });

    it("avisos: el `Toast` de HeroUI Native queda bajo el margen seguro (la barra de estado y la franja sólida)", async () => {
        const m = await enSitio();
        await abrirCampo(m, { esquema, enLinea: () => false, vista: { tipo: "orden", id: 1, paso: "checklist" }, margen: { top: 48, bottom: 24, left: 0, right: 0 } });
        // La franja sólida mide lo mismo que el margen de arriba…
        expect(estilo("franja-superior")).toMatchObject({ height: 48 });
        await fireEvent.press(await screen.findByRole("radio", { name: "Sí" }));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/«Señal verificada» guardado/));
        // …y el aviso empieza debajo: margen de arriba + aire (antes, `top: 8` a secas, bajo la hora).
        expect(arribaDelAviso(avisoArriba())).toBe(48 + AIRE_AVISO.arriba);
        expect(insetsAvisos({ top: 48, left: 0, right: 0 })).toEqual({ top: 56, left: 14, right: 14 });
        expect(insetsAvisos({ top: 0, left: 0, right: 0 }).top).toBe(AIRE_AVISO.arriba);
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
