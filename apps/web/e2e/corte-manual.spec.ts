import type { Schema } from "@iris/contratos";
import { expect, test, type BrowserContext } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { conSesionTecnico, simularApiCampo, type ApiCampo } from "./api-campo";
import { irAPaso } from "./ayudas-campo";
import { elegirOpcion } from "./controles";
import { ordenesDelDia } from "./datos-campo";
import { buscarDesbordes } from "./desbordes";

/*
 * Corte manual de respaldo del cable (contrato con Argos 0.15.0 §13.2, B34): la orden muestra la causa
 * y lo que Iris informa a Argos, y la reconexión va con prioridad alta (panel, 4 proyectos). En la app
 * de campo, el checklist de cierre se marca sin señal y sale por `POST /sync/lote` al volver.
 */

type OrdenCampo = Schema<"OrdenCampo">;

const CORTE: NonNullable<Schema<"OrdenDetalle">["corteManual"]> = {
    solicitudId: 7001,
    accion: "reconexion",
    causa: "sin_onu",
    causaTexto: "el servicio no tiene ONU registrada en SmartOLT",
    componente: "cable",
    estado: "en_curso",
    solicitadaEn: "2026-11-26T15:00:00Z",
};

test.describe("en el panel", () => {
    test("la orden de reconexión: causa, estado, solo el cable y prioridad alta", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        const o = api.ordenes.ordenes.find((x) => x.id === 3);
        if (!o) throw new Error("falta la orden 3");
        Object.assign(o, { tipo: "reconexion_catv", prioridad: "alta", corteManual: CORTE, origen: "argos" });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/3");
        const corte = page.getByTestId("corte-manual");
        await expect(corte).toContainText("Reconexión de cable pedida por Argos");
        await expect(corte.getByTestId("corte-causa")).toContainText("no tiene ONU registrada en SmartOLT");
        await expect(corte.getByTestId("corte-estado")).toContainText("En curso");
        await expect(corte).toContainText("el internet del servicio sigue igual");
        await expect(page.getByTestId("prioridad-alta").first()).toContainText("Prioridad alta");
        await expect.poll(() => buscarDesbordes(page, '[data-testid="corte-manual"]'), "nada se desborda").toEqual([]);
    });

    test("no realizada: con su motivo", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        const o = api.ordenes.ordenes.find((x) => x.id === 3);
        if (!o) throw new Error("falta la orden 3");
        Object.assign(o, { tipo: "corte_catv", corteManual: { ...CORTE, accion: "corte", estado: "no_realizada", motivoCodigo: "sin_acceso" } });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/3");
        const corte = page.getByTestId("corte-manual");
        await expect(corte).toContainText("Corte de cable pedido por Argos");
        await expect(corte.getByTestId("corte-estado")).toContainText("No se pudo");
        await expect(corte.getByTestId("corte-motivo")).toContainText("Sin acceso");
        await expect(page.getByTestId("prioridad-alta")).toHaveCount(0);
    });
});

/** OT-000102 como un corte de cable en sitio, con el checklist de cierre del corte. */
function ordenesConCorte(): OrdenCampo[] {
    return ordenesDelDia().map((o) =>
        o.id === 102
            ? {
                  ...o,
                  tipo: "corte_catv",
                  estado: "en_sitio",
                  origen: "argos",
                  descripcion: "Cortar el cable (TV) a mano en campo o en la OLT.",
                  corteManual: { ...CORTE, accion: "corte" },
                  checklist: [
                      {
                          itemId: 901,
                          codigo: "resultado",
                          etiqueta: "¿Se cortó el cable (TV)?",
                          tipo: "seleccion",
                          obligatorio: true,
                          unidad: "",
                          posicion: 1,
                          opciones: ["Hecho", "Sin acceso", "Equipo no encontrado", "Otro motivo"],
                          valor: null,
                          marcadoPor: null,
                          ocurridoEn: null,
                      },
                  ],
              }
            : o,
    );
}

async function sinSenal(context: BrowserContext, api: ApiCampo, sin: boolean) {
    api.sinRed = sin;
    await context.setOffline(sin);
}

test.describe("en la app de campo", () => {
    test.beforeEach(async ({ context, baseURL }, info) => {
        test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
        await conSesionTecnico(context, baseURL ?? "");
    });

    test("ve la causa y marca el checklist de cierre sin señal", async ({ page, context }) => {
        test.setTimeout(60_000);
        const api = await simularApiCampo(page, { ordenes: ordenesConCorte() });
        await page.goto("/campo");
        await page.getByRole("link", { name: /OT-000102/ }).click();
        await expect(page.getByRole("heading", { name: "OT-000102" })).toBeVisible();
        await expect(page.getByTestId("campo-corte-manual")).toContainText("no tiene ONU registrada en SmartOLT");
        await expect(page.getByTestId("campo-corte-manual")).toContainText("Solo el cable");
        await expect.poll(() => buscarDesbordes(page, '[data-testid="campo-corte-manual"]'), "nada se desborda").toEqual([]);

        await sinSenal(context, api, true);
        await irAPaso(page, "Checklist");
        await elegirOpcion(page.getByLabel("¿Se cortó el cable (TV)?"), "Hecho");
        expect(api.lotes).toHaveLength(0);
        await sinSenal(context, api, false);
        await expect
            .poll(() => api.lotes.flatMap((l) => l.operaciones ?? []).find((op) => op.tipo === "checklist.marcar")?.datos, { timeout: 15_000 })
            .toMatchObject({ itemId: 901, valor: "Hecho" });
    });
});
