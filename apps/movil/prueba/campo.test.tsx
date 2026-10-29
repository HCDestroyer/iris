// Pantallas clave de la app de campo con el motor real (`@iris/sync`) sobre un almacén en memoria y
// la API simulada (`TransporteFalso`, formas del contrato): cerrar una orden sin señal, la cola con
// un rechazo (conflicto) y la pausa con motivo. Claro y oscuro.
import type { OrdenCampo, Paquete } from "@iris/sync";
import { itemDePrueba, montar, ordenDePrueba, paqueteDePrueba, type Montaje } from "@iris/sync/pruebas";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react-native";
import { ProveedorCampo, type ServiciosNativos, type Vista } from "../src/campo/contexto";
import { Marco } from "../src/pantallas/Marco";
import type { SesionCampo } from "../src/sesion/sesion";
import { ProveedorTema } from "../src/tema/tema";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const sesion: SesionCampo = { usuarioId: 5, nombre: "Téc. Ana", correo: "ana@isp.gt", empresas: [], modoEmpresa: "sin_conector", empresaActiva: null, verificadoEn: Date.now() };

function paqueteCon(orden: Partial<OrdenCampo>): (fecha: string) => Paquete {
    return (fecha) => {
        // Sin ruta publicada: una sola orden de visita con un ítem obligatorio.
        const base = paqueteDePrueba(fecha);
        delete base.ruta;
        return { ...base, ordenes: [ordenDePrueba(1, { tipo: "visita", checklist: [itemDePrueba(1, { etiqueta: "Señal verificada", obligatorio: true })], ...orden })] };
    };
}

let enLinea = true;
const servicios: ServiciosNativos = { tomarFoto: async () => null, posicion: async () => null, enLinea: () => enLinea };

async function abrir(m: Montaje, esquema: "light" | "dark", vistaInicial: Vista = { tipo: "lista" }) {
    await render(
        <ProveedorTema esquema={esquema}>
            <ProveedorCampo motor={m.motor} sesion={sesion} servicios={servicios} cerrarSesion={async () => undefined} vistaInicial={vistaInicial}>
                <Marco empresa={null} alSesionVencida={jest.fn()} />
            </ProveedorCampo>
        </ProveedorTema>,
    );
}

const tipos = (m: Montaje) => m.motor.obtenerEstado().cola.map((op) => op.tipo);

describe.each(["light", "dark"] as const)("app de campo (%s)", (esquema) => {
    afterEach(() => {
        enLinea = true;
    });

    it("cierra una orden sin señal: todo queda en la cola en orden y sale al volver la señal", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({});
        await m.motor.cargarPaquete("2026-10-28");
        m.transporte.sinRed = true;
        enLinea = false;
        await abrir(m, esquema);

        // Sin señal se ve el aviso y las órdenes guardadas en el teléfono.
        expect(await screen.findByText("Sin señal: se usan las órdenes guardadas en el teléfono.")).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: /OT-000001/ }));
        await fireEvent.press(await screen.findByRole("button", { name: /En camino/ }));
        expect(await screen.findByText("En camino, sin ubicación. Se enviará al volver la señal.")).toBeTruthy();
        await fireEvent.press(await screen.findByRole("button", { name: /Llegué/ }));
        await fireEvent.press(await screen.findByRole("radio", { name: "Sí" }));

        // Antes de cerrar se ve lo que falta (nada) y el resultado.
        await fireEvent.press(await screen.findByRole("button", { name: /Cerrar orden/ }));
        await fireEvent.press(await screen.findByRole("button", { name: /Confirmar cierre/ }));
        await waitFor(() => expect(tipos(m)).toEqual(["orden.iniciar_camino", "orden.llegar", "checklist.marcar", "orden.cerrar"]));
        expect(m.motor.obtenerOrden(1)?.estado).toBe("cerrada");
        expect(m.transporte.lotes).toHaveLength(0);
        expect(screen.getByTestId("pendientes-por-enviar")).toHaveTextContent(/4 pendientes por enviar/);
        expect(screen.getByTestId("estado-conexion")).toHaveTextContent("Sin conexión");

        // Vuelve la señal: sale en un lote, en orden, y el servidor queda con la orden cerrada.
        m.transporte.sinRed = false;
        enLinea = true;
        await act(() => m.motor.sincronizar());
        await waitFor(() => expect(screen.getByTestId("pendientes-por-enviar")).toHaveTextContent("Todo enviado"));
        expect(m.transporte.lotes.flatMap((l) => (l.operaciones ?? []).map((o) => o.tipo))).toEqual(["orden.iniciar_camino", "orden.llegar", "checklist.marcar", "orden.cerrar"]);
        expect(m.transporte.ordenes.get(1)?.estado).toBe("cerrada");
    });

    it("cola con un rechazo: lo muestra con su motivo, retiene lo que sigue de esa orden y deja Rehacer o Descartar", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_camino" });
        await m.motor.cargarPaquete("2026-10-28");
        m.transporte.sinRed = true;
        await m.motor.registrar("orden.llegar", 1, {});
        await m.motor.registrar("nota.agregar", 1, { texto: "Portón cerrado" });
        // Despacho canceló la orden mientras no había señal: la llegada se rechaza, la nota es evidencia.
        m.transporte.sinRed = false;
        m.transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) =>
                op.tipo === "orden.llegar"
                    ? { id: op.id, estado: "rechazada", error: { type: "urn:iris:problema:transicion_invalida", title: "Transición inválida", status: 409 }, orden: { version: 4, estado: "en_camino" } }
                    : { id: op.id, estado: "rechazada", error: { type: "urn:iris:problema:bloqueada_por_rechazo", title: "Bloqueada", status: 409 }, orden: { version: 4, estado: "en_camino" } },
            ),
        });
        await m.motor.sincronizar();
        await abrir(m, esquema, { tipo: "orden", id: 1 });

        expect(await screen.findByTestId("aviso-rechazos")).toHaveTextContent(/1 acción rechazada por el servidor/);
        await fireEvent.press(within(screen.getByTestId("aviso-rechazos")).getByRole("button", { name: "Revisar" }));
        const rechazo = await screen.findByTestId("rechazo");
        expect(rechazo).toHaveTextContent(/Llegué · OT-000001/);
        expect(rechazo).toHaveTextContent(/La orden ya no está en un estado que permita esta acción/);
        // La nota quedó retenida detrás del rechazo (no se perdió).
        expect(screen.getByTestId("cola")).toHaveTextContent(/Nota · OT-000001/);
        expect(screen.getByRole("button", { name: /Reintentar/ })).toBeTruthy();

        // Rehacer: vuelve a la cola con id nuevo y la API ahora la acepta.
        m.transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => m.transporte.aplicar(op)) });
        await fireEvent.press(within(rechazo).getByRole("button", { name: /Rehacer/ }));
        await waitFor(() => expect(m.motor.obtenerEstado().rechazos).toHaveLength(0));
        await act(() => m.motor.sincronizar());
        await waitFor(() => expect(screen.getByTestId("pendientes-por-enviar")).toHaveTextContent("Todo enviado"));
        expect(m.motor.obtenerOrden(1)?.estado).toBe("en_sitio");
    });

    it("descartar un rechazo terminal (orden reasignada) descarta lo pendiente de esa orden", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio" });
        await m.motor.cargarPaquete("2026-10-28");
        m.transporte.sinRed = true;
        await m.motor.registrar("checklist.marcar", 1, { itemId: 1, valor: true });
        m.transporte.sinRed = false;
        m.transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => ({ id: op.id, estado: "rechazada", error: { type: "urn:iris:problema:orden_reasignada", title: "Reasignada", status: 409 } })),
        });
        await m.motor.sincronizar();
        await abrir(m, esquema, { tipo: "pendientes" });
        const rechazo = await screen.findByTestId("rechazo");
        expect(rechazo).toHaveTextContent(/La orden se asignó a otro técnico/);
        expect(within(rechazo).queryByRole("button", { name: /Rehacer/ })).toBeNull();
        await fireEvent.press(within(rechazo).getByRole("button", { name: /Descartar todo de esta orden/ }));
        await waitFor(() => expect(screen.getByTestId("pendientes-por-enviar")).toHaveTextContent("Todo enviado"));
    });

    it("pausa con motivo (nota obligatoria con «Otro»), bloquea salir y cerrar, y se reanuda", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio" });
        await m.motor.cargarPaquete("2026-10-28");
        m.transporte.sinRed = true;
        await abrir(m, esquema, { tipo: "orden", id: 1 });

        await fireEvent.press(await screen.findByRole("button", { name: /^Pausar$/ }));
        await fireEvent.press(await screen.findByRole("radio", { name: "Otro motivo" }));
        await fireEvent.press(screen.getByRole("button", { name: "Confirmar pausa" }));
        expect(await screen.findByText("Con «Otro motivo» explica qué pasa")).toBeTruthy();
        expect(tipos(m)).toEqual([]);

        await fireEvent.press(screen.getByRole("radio", { name: "Mal clima" }));
        await fireEvent.press(screen.getByRole("button", { name: "Confirmar pausa" }));
        expect(await screen.findByTestId("orden-pausada")).toHaveTextContent(/Orden pausada: Mal clima/);
        expect(m.motor.obtenerEstado().cola.at(-1)).toMatchObject({ tipo: "orden.pausar", datos: { motivo: "clima" } });
        expect(screen.queryByRole("button", { name: /Cerrar orden/ })).toBeNull();
        expect(screen.queryByRole("button", { name: /Llegué/ })).toBeNull();

        await fireEvent.press(within(screen.getByTestId("orden-pausada")).getByRole("button", { name: /Reanudar/ }));
        await waitFor(() => expect(m.motor.obtenerOrden(1)?.estado).toBe("en_sitio"));
        expect(await screen.findByRole("button", { name: /Cerrar orden/ })).toBeTruthy();
        expect(tipos(m)).toEqual(["orden.pausar", "orden.reanudar"]);
    });
});
