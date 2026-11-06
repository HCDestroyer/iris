// Reporte del PM (APK 0.16.0): «Nota editada · OT-000002 — Algún dato no es válido. • expected
// required property versionNota to be present». La orden venía guardada de 0.15 (notas sin
// `version`). El técnico nunca lee el mensaje en inglés del validador, y «Rehacer» trae la nota del
// servidor y la vuelve a enviar con su versión.
import type { NotaOrden, Problema } from "@iris/sync";
import { montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { abrirCampo, nube, paqueteCon, TODO_ENVIADO } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const NOTA: NotaOrden = {
    id: "0192f0a0-0000-7000-8000-000000000001",
    texto: "Portón negro",
    autorId: 1,
    autorNombre: "Técnico",
    canal: "campo",
    ocurridoEn: "2026-10-28T14:00:00Z",
    version: 1,
    editadaEn: null,
};

const SIN_VERSION_NOTA: Problema = {
    type: "urn:iris:problema:validacion",
    title: "Datos inválidos",
    status: 422,
    detail: "Los datos enviados no son válidos",
    errors: [{ field: "datos.", message: "expected required property versionNota to be present", rule: "esquema" }],
};

describe.each(["light", "dark"] as const)("nota editada con un dato faltante (%s)", (esquema) => {
    it("muestra el motivo en español (nunca el del validador) y «Rehacer» la recupera con la versión del servidor", async () => {
        const m = await montar();
        m.transporte.paquete = paqueteCon({ estado: "en_sitio", notas: [NOTA] });
        await m.motor.cargarPaquete("2026-10-28");
        const { operacion } = await m.motor.registrar("nota.editar", 1, { notaId: NOTA.id, versionNota: 1, texto: "Portón verde" });
        m.transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => ({ id: op.id, estado: "rechazada", error: SIN_VERSION_NOTA })) });
        await m.motor.sincronizar();
        // Despacho la editó en el panel: el servidor está en la versión 3.
        m.transporte.paquete = paqueteCon({ estado: "en_sitio", notas: [{ ...NOTA, version: 3 }] });
        m.transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => m.transporte.aplicar(op)) });
        await abrirCampo(m, { esquema, vista: { tipo: "pendientes" } });

        const rechazo = await screen.findByTestId("rechazo");
        expect(rechazo).toHaveTextContent(/Nota editada · OT-000001/);
        expect(rechazo).toHaveTextContent(/Algún dato no es válido\./);
        expect(rechazo).toHaveTextContent(/Falta un dato; toca «Rehacer» para enviarlo de nuevo\./);
        expect(rechazo).not.toHaveTextContent(/expected|required property|versionNota/);

        await fireEvent.press(within(rechazo).getByRole("button", { name: /Rehacer/ }));
        await waitFor(() => expect(m.motor.obtenerEstado().rechazos).toHaveLength(0));
        await act(() => m.motor.sincronizar());
        await waitFor(() => expect(nube()).toHaveProp("accessibilityLabel", TODO_ENVIADO));
        const enviada = m.transporte.lotes.flatMap((l) => l.operaciones ?? []).filter((o) => o.tipo === "nota.editar" && o.id !== operacion.id);
        expect(enviada.map((o) => (o.datos as { versionNota: number }).versionNota)).toEqual([3]);
    });
});
