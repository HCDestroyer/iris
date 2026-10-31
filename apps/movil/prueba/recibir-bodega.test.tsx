// «Por recibir» y «Mi bodega» en nativo: recepción con lo que faltó y la firma del técnico por
// `motor.recibirTraslado` (motor real sobre memoria), la firma que espera si el traslado aún no
// llega (404), el rechazo `traslado_no_encontrado` (solo Descartar) y «Mi bodega» de solo lectura
// con «Reintentar».
import { ErrorTransporte, type Problema, type ResultadoOperacion } from "@iris/sync";
import { montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { abrirCampo, InventarioFalso, trasladoDePrueba } from "./arnes";
import { retenerPng } from "./firma-falsa";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));
jest.mock("../src/pantallas/orden/LienzoFirma", () => jest.requireActual("./firma-falsa"));

const problema = (codigo: string, status = 422): Problema => ({ type: `urn:iris:problema:${codigo}`, title: codigo, status });

async function recibirConFaltante(m: Awaited<ReturnType<typeof montar>>, inventario: InventarioFalso, esquema: "light" | "dark") {
    await abrirCampo(m, { esquema, inventario });
    // El contador de «Por recibir» sale de la lista leída de Argos.
    expect(await screen.findByRole("tab", { name: "Por recibir (1)" })).toBeTruthy();
    await fireEvent.press(screen.getByRole("tab", { name: "Por recibir (1)" }));
    await fireEvent.press(await screen.findByRole("button", { name: "Traslado 31, Por recibir" }));
    // Faltó una serie de la ONU.
    const linea = await screen.findByTestId("linea-2");
    await fireEvent.press(within(linea).getByRole("radio", { name: "Faltó algo" }));
    await fireEvent.press(within(screen.getByTestId("linea-2")).getAllByRole("radio", { name: "No llegó" })[1] as never);
    // Sin firma no se confirma.
    await fireEvent.press(screen.getByTestId("confirmar-recepcion"));
    expect(await screen.findByTestId("error-firma")).toHaveTextContent("Firma en el recuadro para confirmar que recibiste");
    expect(m.motor.obtenerEstado().recepciones).toHaveLength(0);
    await fireEvent.press(screen.getByLabelText(/Firmar en Recuadro para la firma del técnico/));
    // Doble toque mientras se guarda la firma: una sola recepción.
    const soltar = retenerPng();
    await fireEvent.press(screen.getByTestId("confirmar-recepcion"));
    await fireEvent.press(screen.getByTestId("confirmar-recepcion"));
    expect(screen.getByTestId("confirmar-recepcion").props.accessibilityState).toMatchObject({ busy: true });
    await act(async () => soltar());
    await waitFor(() => expect(m.motor.obtenerEstado().recepciones).toHaveLength(1));
}

describe.each(["light", "dark"] as const)("«Por recibir» y «Mi bodega» (%s)", (esquema) => {
    it("recibe un traslado con lo que faltó y la firma del técnico; con la firma en 404 espera y se reintenta sola", async () => {
        const m = await montar({ espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0 } });
        const inventario = new InventarioFalso();
        inventario.traslados = [trasladoDePrueba(31)];
        // El traslado todavía no llega a Iris: el alta de la firma responde 404.
        m.subidor.rechazarAlta = new ErrorTransporte("http", "No existe", 404, problema("traslado_no_encontrado", 404));
        await recibirConFaltante(m, inventario, esquema);

        const [op] = m.motor.obtenerEstado().recepciones;
        expect(op?.datos).toMatchObject({ trasladoId: 31, lineas: [{ lineaId: 2, cantidadRecibida: 1, seriesFaltantes: ["ZTE-2"] }] });
        expect(m.motor.obtenerEstado().adjuntos.find((a) => a.id === op?.datos.firmaAdjuntoId)).toMatchObject({ entidad: "traslado", tipo: "firma" });
        expect(await screen.findByTestId("situacion-en_telefono")).toHaveTextContent("Recibido · por enviar");

        await act(() => m.motor.sincronizar());
        expect(await screen.findByTestId("recepcion-en-telefono")).toHaveTextContent(/todavía no llega a Iris desde Argos: se reintenta solo/);
        expect(m.transporte.lotes.flatMap((l) => l.operaciones ?? []).some((o) => o.id === op?.id)).toBe(false);

        // Pasada la espera, la firma sube y la recepción sale.
        await act(async () => {
            await m.reloj.avanzar(1000);
            await m.motor.sincronizar();
        });
        await waitFor(() => expect(m.transporte.lotes.flatMap((l) => l.operaciones ?? []).some((o) => o.id === op?.id)).toBe(true));
    });

    it("rechazo traslado_no_encontrado: solo se descarta (sin Rehacer)", async () => {
        const m = await montar();
        const inventario = new InventarioFalso();
        inventario.traslados = [trasladoDePrueba(31)];
        m.transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map(
                (o): ResultadoOperacion => (o.tipo === "traslado.recibir" ? { id: o.id, estado: "rechazada", error: problema("traslado_no_encontrado", 404) } : m.transporte.aplicar(o)),
            ),
        });
        await recibirConFaltante(m, inventario, esquema);
        await act(() => m.motor.sincronizar());

        const rechazo = await screen.findByTestId("recepcion-rechazada");
        expect(within(rechazo).queryByRole("button", { name: /Rehacer/ })).toBeNull();
        // La barra discreta crece con el rechazo.
        expect(screen.getByTestId("pendientes-por-enviar")).toHaveTextContent(/1 rechazado/);
        await fireEvent.press(within(rechazo).getByRole("button", { name: /Descartar/ }));
        await waitFor(() => expect(m.motor.obtenerEstado().recepciones).toHaveLength(0));
        // Se puede volver a recibir.
        expect(await screen.findByTestId("confirmar-recepcion")).toBeTruthy();
    });

    it("sin señal: «Por recibir» dice que hay que conectarse y «Reintentar» vuelve a leer", async () => {
        const m = await montar();
        const inventario = new InventarioFalso();
        inventario.fallarTraslados = new ErrorTransporte("red", "Sin señal");
        await abrirCampo(m, { esquema, inventario, vista: { tipo: "recibir" } });
        expect(await screen.findByTestId("error-lectura")).toHaveTextContent(/Sin señal: conéctate para ver tus traslados\./);
        await fireEvent.press(screen.getByRole("button", { name: "Reintentar" }));
        expect(await screen.findByText("No tienes nada por recibir")).toBeTruthy();
    });

    it("«Mi bodega» en «Yo»: solo lectura; si falla dice por qué y «Reintentar» la lee", async () => {
        const m = await montar();
        const inventario = new InventarioFalso();
        inventario.fallarBodega = new ErrorTransporte("http", "Sin bodega", 422, problema("tecnico_sin_bodega"));
        await abrirCampo(m, { esquema, inventario, vista: { tipo: "yo" } });
        await fireEvent.press(screen.getByRole("button", { name: "Mi bodega" }));
        const error = await screen.findByTestId("error-lectura");
        expect(error).toHaveTextContent(/No tienes bodega en Argos/);
        // Nunca un total sobre datos que no se pudieron leer.
        expect(screen.queryByTestId("existencias")).toBeNull();
        await fireEvent.press(within(error).getByRole("button", { name: "Reintentar" }));
        const existencias = await screen.findByTestId("existencias");
        expect(existencias).toHaveTextContent(/Cable UTP/);
        expect(existencias).toHaveTextContent(/Comodato/);
        expect(screen.getByText(/ZTE-9/)).toBeTruthy();
        expect(screen.getByText(/Consumo · OT-000001/)).toBeTruthy();
        expect(inventario.lecturas.bodega).toBe(2);
        // Ninguna acción de escritura en «Mi bodega».
        expect(m.motor.obtenerEstado().cola).toHaveLength(0);
    });
});
