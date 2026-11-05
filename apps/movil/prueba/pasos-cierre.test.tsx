// La orden por pasos (fase 4 en nativo): isla de menú con contadores, pasos independientes con
// avance y «Anterior» / «Siguiente», «Atrás» de Android, la firma solo en el paso final, la guarda
// contra el doble toque al cerrar y la barra de estado discreta. Motor real sobre memoria.
import { ErrorTransporte } from "@iris/sync";
import { montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { BackHandler, StyleSheet } from "react-native";
import { irA, MAXIMO_PILA, pilaInicial, retroceder } from "../src/campo/navegacion";
import { abrirCampo, nube, paqueteCon, tipos, TODO_ENVIADO } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));
jest.mock("../src/pantallas/orden/LienzoFirma", () => jest.requireActual("./firma-falsa"));

/** Captura el «Atrás» de Android que registra la app. */
function atrasAndroid(): () => Promise<boolean> {
    const espia = jest.spyOn(BackHandler, "addEventListener");
    return async () => {
        const llamadas = espia.mock.calls.filter((c) => c[0] === "hardwareBackPress");
        const ultimo = llamadas.at(-1)?.[1];
        if (!ultimo) {
            throw new Error("La app no registró «Atrás»");
        }
        let r = false;
        await act(async () => {
            r = ultimo({} as never) === true;
        });
        return r;
    };
}

afterEach(() => {
    jest.restoreAllMocks();
});

describe.each(["light", "dark"] as const)("hoja «Conexión y envíos» (%s)", (esquema) => {
    it("con señal y algo pendiente ofrece «Enviar ahora», que sincroniza", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio" });
        await m.motor.cargarPaquete("2026-10-28");
        // El servidor falla (503): lo escrito queda pendiente aunque haya señal.
        m.transporte.fallar(1, new ErrorTransporte("http", "No disponible", 503));
        await m.motor.registrar("nota.agregar", 1, { texto: "Queda pendiente" });
        await m.motor.sincronizar().catch(() => undefined);
        await abrirCampo(m, { esquema });
        await waitFor(() => expect(nube()).toHaveProp("accessibilityLabel", "En línea, 1 por enviar, toca para ver el estado"));
        await fireEvent.press(nube());
        const lotes = m.transporte.lotes.length;
        await fireEvent.press(screen.getByTestId("enviar-ahora"));
        await waitFor(() => expect(m.transporte.lotes.length).toBeGreaterThan(lotes));
        await waitFor(() => expect(nube()).toHaveProp("accessibilityLabel", TODO_ENVIADO));
    });
});

describe("pila de «Atrás»", () => {
    it("apila vistas, una pestaña empieza de nuevo con «Hoy» debajo y en «Hoy» sale de la app", () => {
        let p = pilaInicial();
        p = irA(p, { tipo: "orden", id: 1 });
        p = irA(p, { tipo: "orden", id: 1, paso: "llegada" });
        p = irA(p, { tipo: "orden", id: 1, paso: "llegada" });
        expect(p.atras).toHaveLength(2);
        const r = retroceder(p);
        expect(r?.actual).toEqual({ tipo: "orden", id: 1 });
        p = irA(p, { tipo: "agenda" });
        expect(p).toEqual({ actual: { tipo: "agenda" }, atras: [{ tipo: "lista" }] });
        // «Por recibir» ya no es botón de la isla: se abre desde el mosaico de Hoy y se apila.
        expect(irA(p, { tipo: "recibir" }).atras).toEqual([{ tipo: "lista" }, { tipo: "agenda" }]);
        expect(retroceder(retroceder(p) ?? pilaInicial())).toBeNull();
        for (let i = 1; i <= MAXIMO_PILA + 5; i += 1) {
            p = irA(p, { tipo: "traslado", id: i });
        }
        expect(p.atras).toHaveLength(MAXIMO_PILA);
    });
});

describe.each(["light", "dark"] as const)("orden por pasos (%s)", (esquema) => {
    it("isla de menú: Hoy · «+» · Agenda · Yo de 54 px y navegación; sin chat no hay «Chat»", async () => {
        const atras = atrasAndroid();
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio" });
        await m.motor.cargarPaquete("2026-10-28");
        m.transporte.sinRed = true;
        await m.motor.registrar("nota.agregar", 1, { texto: "Portón cerrado" });
        await abrirCampo(m, { esquema });

        const isla = screen.getByTestId("navegacion-inferior");
        const pestanas = within(isla).getAllByRole("tab");
        expect(pestanas.map((p) => p.props.accessibilityLabel)).toEqual(["Hoy", "Agenda", "Yo · Téc. Ana"]);

        expect(within(isla).getByRole("button", { name: "Acciones rápidas" })).toBeTruthy();
        expect(screen.getByRole("tab", { name: "Hoy" })).toBeSelected();
        for (const id of ["hoy", "agenda", "yo"]) {
            expect(screen.getByTestId(`pestana-${id}`)).toHaveStyle({ minHeight: 54, width: 54 });
        }
        expect(screen.getByTestId("boton-mas")).toHaveStyle({ minHeight: 54 });
        // «Por enviar» no es pestaña: la nube del encabezado abre «Conexión y envíos», que lleva ahí.
        await fireEvent.press(nube());
        await fireEvent.press(await screen.findByRole("button", { name: "Ver «Por enviar»" }));
        expect(await screen.findByText("Por enviar")).toBeTruthy();
        await fireEvent.press(screen.getByRole("tab", { name: /^Yo/ }));
        expect(await screen.findByRole("button", { name: "Mi bodega" })).toBeTruthy();
        // «Atrás» desde una pestaña vuelve a «Hoy»; en «Hoy» deja salir de la app.
        expect(await atras()).toBe(true);
        expect(await screen.findByText("Mis órdenes de hoy")).toBeTruthy();
        expect(await atras()).toBe(false);
    });

    it("pasos independientes con avance, «Anterior» / «Siguiente» y «Atrás» de Android; la firma solo al final", async () => {
        const atras = atrasAndroid();
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "programada" });
        await m.motor.cargarPaquete("2026-10-28");
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1 } });

        // Resumen: avance y «Empezar trabajo»; la firma no está.
        expect(screen.getByTestId("avance-orden")).toHaveTextContent("2 de 4 pasos listos");
        expect(screen.queryByText(/Firma del cliente$/)).toBeNull();
        await fireEvent.press(screen.getByRole("button", { name: /Empezar trabajo/ }));
        expect(await screen.findByText("Paso 1 de 5", { exact: false })).toBeTruthy();
        expect(screen.getByRole("button", { name: "1. Llegada: falta 1" })).toBeSelected();

        // «Firma y cierre» con candado mientras falte algo: dice qué falta y lleva ahí.
        await fireEvent.press(screen.getByRole("button", { name: "5. Firma del cliente y cierre: al terminar lo demás" }));
        const falta = await screen.findByTestId("falta-para-firmar");
        expect(falta).toHaveTextContent(/Marca tu llegada al sitio/);
        expect(falta).toHaveTextContent(/Falta «Señal verificada» del checklist/);
        expect(screen.queryByRole("button", { name: /Firmar y cerrar orden/ })).toBeNull();
        expect(screen.queryByLabelText(/Recuadro para la firma del cliente/)).toBeNull();
        await fireEvent.press(within(falta).getByRole("button", { name: "Ir a llegada" }));
        expect(await screen.findByRole("button", { name: /Llegué/ })).toBeTruthy();

        // «Atrás» de Android vuelve al paso anterior (el cierre) y luego al paso 1 y al resumen.
        expect(await atras()).toBe(true);
        expect(await screen.findByTestId("falta-para-firmar")).toBeTruthy();
        expect(await atras()).toBe(true);
        expect(await screen.findByText("Paso 1 de 5", { exact: false })).toBeTruthy();

        // Llegada → Checklist: cada paso se guarda en el teléfono al tocar.
        await fireEvent.press(screen.getByRole("button", { name: /Llegué/ }));
        await waitFor(() => expect(m.motor.obtenerOrden(1)?.estado).toBe("en_sitio"));
        await fireEvent.press(screen.getByRole("button", { name: /Siguiente: checklist/ }));
        await fireEvent.press(await screen.findByRole("radio", { name: "Sí" }));
        await waitFor(() => expect(screen.getByTestId("avance-orden")).toHaveTextContent("4 de 4 pasos listos"));
        expect(tipos(m)).toEqual(["orden.llegar", "checklist.marcar"]);
        // Ningún paso de trabajo muestra la firma.
        for (const paso of [/Siguiente: materiales y equipos/, /Siguiente: fotos/]) {
            expect(screen.queryByLabelText(/Recuadro para la firma del cliente/)).toBeNull();
            await fireEvent.press(screen.getByRole("button", { name: paso }));
        }
        expect(screen.queryByLabelText(/Recuadro para la firma del cliente/)).toBeNull();
        await fireEvent.press(screen.getByRole("button", { name: "Anterior" }));
        expect(await screen.findByText("Paso 3 de 5", { exact: false })).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: /Siguiente: fotos/ }));
        await fireEvent.press(await screen.findByRole("button", { name: /Firma y cierre/ }));

        // Paso final: resumen del trabajo y firma del cliente.
        expect(await screen.findByTestId("resumen-trabajo")).toHaveTextContent(/checklist 1\/1/);
        expect(screen.getByText("Paso final", { exact: false })).toBeTruthy();
        expect(screen.getByLabelText(/Firmar en Recuadro para la firma del cliente/)).toBeTruthy();
    });

    it("firma obligatoria: sin trazo no cierra; con firma encola firma y cierre juntos una sola vez aunque se toque dos veces", async () => {
        let soltarGps: (p: null) => void = () => undefined;
        const gps = new Promise<null>((r) => {
            soltarGps = r;
        });
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio", enSitioEn: "2026-10-28T15:00:00Z", requiereFirma: true, contacto: { nombre: "Juan Pérez", telefono: "" } });
        await m.motor.cargarPaquete("2026-10-28");
        await m.motor.registrar("checklist.marcar", 1, { itemId: 1, valor: true });
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "cierre" }, servicios: { posicion: () => gps } });

        const cerrar = await screen.findByRole("button", { name: /Firmar y cerrar orden/ });
        // El paso final dice que todo lo obligatorio está listo (icono + texto) antes del resumen.
        expect(screen.getByTestId("listo-para-firmar")).toHaveTextContent("Todo lo obligatorio está listo");
        expect((StyleSheet.flatten(cerrar.props.style) as { minHeight: number }).minHeight).toBeGreaterThanOrEqual(44);
        await fireEvent.press(cerrar);
        // Sin trazo: el error es un aviso (`alert`, con icono y texto), no solo texto en rojo.
        expect(await screen.findByTestId("error-firma")).toHaveTextContent("Pide al cliente que firme en el recuadro");
        expect(screen.getByTestId("error-firma").props.role).toBe("alert");
        expect(tipos(m)).toEqual(["checklist.marcar"]);

        await fireEvent.press(screen.getByLabelText(/Firmar en Recuadro para la firma del cliente/));
        // Doble toque mientras se espera el GPS: el segundo no encola otro cierre.
        await fireEvent.press(screen.getByTestId("firmar-y-cerrar"));
        await fireEvent.press(screen.getByTestId("firmar-y-cerrar"));
        expect(screen.getByTestId("firmar-y-cerrar").props.accessibilityState).toMatchObject({ busy: true });
        await act(async () => soltarGps(null));
        await waitFor(() => expect(m.motor.obtenerOrden(1)?.estado).toBe("cerrada"));
        expect(tipos(m)).toEqual(["checklist.marcar", "adjunto.registrar", "firma.capturar", "orden.cerrar"]);
        expect(m.motor.obtenerEstado().cola.find((op) => op.tipo === "firma.capturar")?.datos).toMatchObject({ nombre: "Juan Pérez" });
        expect(await screen.findByTestId("orden-cerrada")).toHaveTextContent(/Firmó Juan Pérez/);
    });

    it("indicadores del encabezado: discretos con todo enviado; la nube crece sin señal con pendientes o con la sesión vencida; cada uno abre solo su hoja", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio" });
        await m.motor.cargarPaquete("2026-10-28");
        await abrirCampo(m, { esquema });

        // Discretos: sin número ni texto visible; el estado va en su nombre accesible (zona de 44 px).
        expect(nube()).toHaveProp("accessibilityLabel", TODO_ENVIADO);
        expect(within(nube()).queryByText(/./)).toBeNull();
        expect(nube()).toHaveStyle({ width: 44, height: 44 });
        expect(screen.getByTestId("indicador-ubicacion")).toHaveProp("accessibilityLabel", "GPS apagado, toca para ver el estado");
        // El GPS abre solo «Ubicación» (con el interruptor); la X la cierra.
        await fireEvent.press(screen.getByTestId("indicador-ubicacion"));
        expect(screen.getByText("Ubicación")).toBeTruthy();
        expect(screen.getByRole("switch", { name: "Compartir mi ubicación" })).toBeTruthy();
        expect(screen.queryByTestId("seccion-sincronizacion")).toBeNull();
        await fireEvent.press(screen.getByLabelText("Cerrar"));
        await waitFor(() => expect(screen.queryByTestId("seccion-ubicacion")).toBeNull());

        m.transporte.sinRed = true;
        await act(async () => {
            await m.motor.registrar("nota.agregar", 1, { texto: "Sin señal" });
            await m.motor.sincronizar().catch(() => undefined);
        });
        await waitFor(() => expect(nube()).toHaveProp("accessibilityLabel", "Sin señal, 1 por enviar, toca para ver el estado"));
        expect(within(nube()).getByText("1")).toBeTruthy();
        // La nube abre solo «Conexión y envíos».
        await fireEvent.press(nube());
        expect(screen.getByText("Conexión y envíos")).toBeTruthy();
        expect(screen.queryByTestId("seccion-ubicacion")).toBeNull();
        expect(screen.getByTestId("estado-conexion")).toHaveTextContent("Sin conexión");
        expect(screen.getByTestId("pendientes-por-enviar")).toHaveTextContent("1 pendiente por enviar");
        await fireEvent.press(screen.getByRole("button", { name: "Ver «Por enviar»" }));
        expect(await screen.findByTestId("cola")).toHaveTextContent(/Nota · OT-000001/);

        m.transporte.sinRed = false;
        m.transporte.fallar(5, new ErrorTransporte("http", "No autenticado", 401, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 }));
        await act(() => m.motor.sincronizar().catch(() => undefined));
        await waitFor(() => expect(nube()).toHaveProp("accessibilityLabel", "Sesión vencida, 1 por enviar, toca para ver el estado"));
        expect(screen.getByText("Tu sesión venció")).toBeTruthy();
    });
});
