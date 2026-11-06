// Diseño «combinada» de la app del técnico (nov 2026), como la PWA: encabezado con saludo y chips que
// se va al bajar y vuelve compacto al subir, «Hoy» con la siguiente orden en grande, mosaicos y
// «Después», la isla que se encoge, la píldora de estado (la hoja del «+» está en campo-mas.test.tsx),
// «Mañana» con la tira de 7 días,
// los chips de pasos y − / + en materiales. Motor real sobre memoria, claro y oscuro.
import { ErrorTransporte, type OrdenCampo, type PlantillaFormulario } from "@iris/sync";
import { hoyEnGuatemala, sumarDias } from "@iris/sync/campo";
import { itemDePrueba, montar, ordenDePrueba, paqueteDePrueba } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { abrirCampo, avisoArriba, InventarioFalso, trasladoDePrueba } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const PRUEBA_VELOCIDAD: PlantillaFormulario = {
    id: 9,
    codigo: "velocidad",
    nombre: "Prueba de velocidad",
    descripcion: "",
    empresaId: null,
    activa: true,
    porDefecto: false,
    requiereFirma: false,
    tipo: null,
    version: 1,
    versionActual: 1,
    versionId: 91,
    numeroVersion: 1,
    creadoEn: "2026-10-01T00:00:00Z",
    actualizadoEn: "2026-10-20T00:00:00Z",
    items: [{ id: 901, codigo: "bajada", etiqueta: "Bajada (Mbps)", tipo: "numero", obligatorio: false, unidad: "Mbps", posicion: 1 }],
};

/** OT-000001 en sitio (va en grande) y OT-000002 programada (en «Después»), con el catálogo. */
async function conUnaEnSitio() {
    const m = await montar();
    m.transporte.paquete = (fecha) => ({
        ...paqueteDePrueba(fecha),
        ruta: undefined as never,
        ordenes: [
            ordenDePrueba(2, { tipo: "retiro", contacto: { nombre: "Ana Pérez", telefono: "5555-0002" } }),
            ordenDePrueba(1, { tipo: "visita", estado: "en_sitio", enSitioEn: "2026-10-28T15:10:00Z", contacto: { nombre: "Jorge Méndez", telefono: "5555-0001" }, checklist: [itemDePrueba(1, { obligatorio: true })] }),
        ],
        plantillas: [PRUEBA_VELOCIDAD],
    });
    await m.motor.cargarPaquete("2026-10-28");
    return m;
}

/** Hora ISO del día `n` después de hoy (Guatemala). */
function diaMas(n: number, hora: string): string {
    return `${sumarDias(hoyEnGuatemala(), n)}T${hora}Z`;
}

describe.each(["light", "dark"] as const)("diseño «combinada» (%s)", (esquema) => {
    it("Hoy: saludo, chips, la siguiente orden en grande con su anillo, mosaicos con datos reales y «Después»", async () => {
        const m = await conUnaEnSitio();
        const inventario = new InventarioFalso();
        inventario.traslados = [trasladoDePrueba(31)];
        await abrirCampo(m, { esquema, inventario });

        expect(screen.getByRole("header", { name: "Hola, Téc." })).toBeTruthy();
        expect(screen.getByTestId("chip-hoy")).toHaveProp("accessibilityLabel", "Hoy · 2");
        expect(screen.getByTestId("chip-hoy")).toBeSelected();
        const siguiente = screen.getByTestId("siguiente-orden");
        // La en curso va en grande aunque su cita sea después.
        expect(within(siguiente).getByRole("button", { name: "OT-000001, Jorge Méndez" })).toBeTruthy();
        expect(within(siguiente).getByLabelText(/de 4 pasos listos$/)).toBeTruthy();
        expect(within(siguiente).getByRole("button", { name: "Cómo llegar" })).toBeTruthy();
        expect(within(siguiente).getByRole("button", { name: "Llamar al cliente" })).toBeTruthy();
        expect(screen.getByTestId("mosaico-ruta")).toHaveTextContent(/2 órdenes/);
        await waitFor(() => expect(screen.getByTestId("mosaico-recibir")).toHaveTextContent(/1 traslado/));
        expect(screen.getByRole("tab", { name: "Por recibir (1)" })).toBeTruthy();
        expect(screen.getByLabelText("Después")).toHaveTextContent(/Ana Pérez/);

        await fireEvent.press(within(siguiente).getByRole("button", { name: /Continuar trabajo/ }));
        expect(await screen.findByTestId("chip-paso-checklist")).toBeSelected();
    });

    it("sin el paquete no hay mosaicos ni totales; sin traslados leídos, «Por recibir» no inventa", async () => {
        const m = await conUnaEnSitio();
        const inventario = new InventarioFalso();
        inventario.fallarTraslados = new ErrorTransporte("red", "Sin señal");
        await abrirCampo(m, { esquema, inventario });
        await waitFor(() => expect(screen.getByTestId("mosaico-recibir")).toHaveTextContent(/—/));
        expect(screen.getByTestId("mosaico-recibir")).toHaveTextContent(/Sin leer/);
    });

    it("al bajar se encoge la isla y no hay barra; al subir vuelve la barra compacta", async () => {
        const m = await conUnaEnSitio();
        await abrirCampo(m, { esquema });
        const desplazamiento = screen.getByTestId("desplazamiento");
        await act(async () => {
            fireEvent(within(desplazamiento).getByTestId("encabezado-campo").parent ?? desplazamiento, "layout", { nativeEvent: { layout: { height: 200 } } });
        });
        expect(screen.getByTestId("pestana-hoy")).toHaveStyle({ width: 54 });
        await act(async () => {
            fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: 600 } } });
        });
        expect(screen.getByTestId("pestana-hoy")).toHaveStyle({ width: 44 });
        expect(screen.queryByTestId("barra-compacta")).toBeNull();
        await act(async () => {
            fireEvent.scroll(desplazamiento, { nativeEvent: { contentOffset: { y: 500 } } });
        });
        expect(screen.getByTestId("barra-compacta")).toBeTruthy();
        expect(within(screen.getByTestId("barra-compacta")).getByRole("button", { name: "Tu cuenta" })).toBeTruthy();
        expect(screen.getByTestId("pestana-hoy")).toHaveStyle({ width: 54 });
    });

    it("Mañana: tira de 7 días con el día en teal y las citas en bloques, leídas con señal", async () => {
        const m = await conUnaEnSitio();
        const inventario = new InventarioFalso();
        const futura = (id: number, parcial: Partial<OrdenCampo>) => ordenDePrueba(id, { estado: "programada", ...parcial });
        inventario.ordenesAgenda = [
            futura(18, { tipo: "cambio_equipo", contacto: { nombre: "Rosa Lima", telefono: "" }, formularios: [], programadaInicio: diaMas(1, "17:00:00"), programadaFin: diaMas(1, "18:00:00") }),
            futura(17, { tipo: "instalacion", contacto: { nombre: "Luis Ortiz", telefono: "" }, programadaInicio: diaMas(1, "14:30:00"), programadaFin: diaMas(1, "16:30:00") }),
        ];
        await abrirCampo(m, { esquema, inventario });
        await fireEvent.press(screen.getByTestId("chip-manana"));
        expect(await screen.findByRole("header", { name: /^Mañana, / })).toBeTruthy();
        expect(inventario.hastaAgenda).toBe(sumarDias(hoyEnGuatemala(), 6));
        await waitFor(() => expect(screen.getByTestId("resumen-dia")).toHaveTextContent("2 citas · 3 h"));
        expect(screen.getByTestId(`dia-${sumarDias(hoyEnGuatemala(), 1)}`)).toBeSelected();
        expect(screen.getByTestId(`dia-${sumarDias(hoyEnGuatemala(), 1)}`)).toHaveProp("accessibilityLabel", expect.stringMatching(/: 2 citas$/));
        expect(screen.getByTestId("cita-17")).toHaveTextContent(/Instalación · Luis Ortiz/);
        expect(screen.getByTestId("cita-18")).toHaveTextContent(/Sin formulario/);
        expect(screen.getByTestId("chip-manana")).toHaveProp("accessibilityLabel", "Mañana · 2");
        expect(screen.getByRole("tab", { name: "Agenda" })).toBeSelected();
        await fireEvent.press(screen.getByTestId("chip-semana"));
        expect(await screen.findByRole("header", { name: "Esta semana" })).toBeTruthy();
    });

    it("Mañana sin señal: dice que se lee con señal, con «Reintentar» y sin contadores", async () => {
        const m = await conUnaEnSitio();
        const inventario = new InventarioFalso();
        inventario.fallarAgenda = new ErrorTransporte("red", "Sin señal");
        await abrirCampo(m, { esquema, inventario, vista: { tipo: "agenda" } });
        expect(await screen.findByTestId("error-agenda")).toBeTruthy();
        expect(screen.queryByTestId("resumen-dia")).toBeNull();
        await fireEvent.press(within(screen.getByTestId("error-agenda")).getByRole("button", { name: /Reintentar/ }));
        await waitFor(() => expect(screen.getByTestId("resumen-dia")).toHaveTextContent("Sin citas"));
    });

    it("pasos: chips (el actual en tinta), sin isla, y − / + en materiales", async () => {
        const m = await conUnaEnSitio();
        await abrirCampo(m, { esquema, vista: { tipo: "orden", id: 1, paso: "materiales" } });
        expect(await screen.findByRole("header", { name: "Materiales y equipos" })).toBeTruthy();
        expect(screen.getByTestId("chip-paso-materiales")).toBeSelected();
        expect(screen.getByTestId("chip-paso-llegada")).toHaveProp("accessibilityLabel", "1. Llegada: listo");
        expect(screen.queryByTestId("navegacion-inferior")).toBeNull();
        expect(screen.getByTestId("barra-pasos")).toBeTruthy();
        await fireEvent.press(screen.getByRole("radio", { name: "Cable UTP" }));
        // Al elegir queda en 1; − / + suman o restan desde ahí.
        expect(screen.getByLabelText("Cantidad")).toHaveProp("value", "1");
        await fireEvent.press(screen.getByRole("button", { name: "Agregar uno" }));
        await fireEvent.press(screen.getByRole("button", { name: "Agregar uno" }));
        await fireEvent.press(screen.getByRole("button", { name: "Agregar uno" }));
        await fireEvent.press(screen.getByRole("button", { name: "Quitar uno" }));
        expect(screen.getByLabelText("Cantidad")).toHaveProp("value", "3");
        await fireEvent.press(screen.getByRole("button", { name: "Registrar material" }));
        await waitFor(() => expect(m.motor.obtenerEstado().cola.at(-1)).toMatchObject({ tipo: "material.usar", datos: { cantidad: 3 } }));
        expect(avisoArriba()).toHaveTextContent(/Material registrado/);
    });
});
