import path from "node:path";
import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { conSesionTecnico, simularApiCampo, type ApiCampo } from "./api-campo";
import { alResumen, empezarTrabajo, irAPaso, pestanaCampo, type Paso } from "./ayudas-campo";
import { ordenesDelDia, TECNICO } from "./datos-campo";

/*
 * Formularios y pausa en la app de campo (teléfono, claro y oscuro; API simulada tipada): varios
 * formularios por orden con su versión, agregar uno del catálogo y quitarlo sin señal, ítem de
 * selección, pausa con motivo y nota, tiempo pausado, reanudar, rechazos visibles y el cierre
 * incompleto por formulario.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
type OrdenCampo = Schema<"OrdenCampo">;

const FORM_VISITA: Schema<"FormularioOrden"> = {
    id: "0192f0c0-0000-7000-8000-00000000f102",
    plantillaId: 2,
    codigo: "visita",
    nombre: "Visita técnica",
    versionId: 23,
    numeroVersion: 3,
    requiereFirma: false,
    origen: "tipo",
    agregadoPor: null,
    agregadoEn: "2026-10-28T12:00:00Z",
    conRespuestas: false,
};

const INVENTARIO: Schema<"PlantillaFormulario"> = {
    id: 8,
    codigo: "inventario",
    nombre: "Inventario de equipos",
    descripcion: "",
    empresaId: null,
    activa: true,
    porDefecto: false,
    requiereFirma: false,
    tipo: null,
    version: 4,
    versionActual: 2,
    versionId: 82,
    numeroVersion: 2,
    creadoEn: "2026-10-01T00:00:00Z",
    actualizadoEn: "2026-10-20T00:00:00Z",
    items: [
        { id: 801, codigo: "router", etiqueta: "Modelo de router", tipo: "seleccion", obligatorio: true, unidad: "", posicion: 1, opciones: ["Huawei", "ZTE", "TP-Link"] },
        { id: 802, codigo: "potencia_inv", etiqueta: "Potencia en la roseta", tipo: "medicion", obligatorio: false, unidad: "dBm", posicion: 2 },
    ],
};

/** OT-000102 en sitio, con el formulario de su tipo (versión 3) y un ítem obligatorio. */
function ordenesConFormularios(estado: OrdenCampo["estado"] = "en_sitio"): OrdenCampo[] {
    return ordenesDelDia().map((o) =>
        o.id === 102
            ? {
                  ...o,
                  estado,
                  formularios: [FORM_VISITA],
                  checklist: (o.checklist ?? []).map((i) => ({ ...i, formularioId: FORM_VISITA.id })),
              }
            : o,
    );
}

/** Abre la orden en el paso pedido (por defecto, el checklist; `null`, el resumen). */
async function abrirOrden(page: Page, numero = "OT-000102", paso: Paso | null = "Checklist") {
    await page.goto("/campo");
    await expect(page.getByRole("heading", { name: "Mis órdenes de hoy" })).toBeVisible();
    await page.getByRole("link", { name: new RegExp(numero) }).click();
    await expect(page.getByRole("heading", { name: numero })).toBeVisible();
    if (paso) {
        await irAPaso(page, paso);
    }
}

async function sinSenal(context: BrowserContext, api: ApiCampo, sin: boolean) {
    api.sinRed = sin;
    await context.setOffline(sin);
}

const operaciones = (api: ApiCampo) => api.lotes.flatMap((l) => l.operaciones ?? []);
const notificacion = (page: Page) => page.locator('[data-slot="notificacion"]');

test.describe("en el teléfono", () => {
    test.beforeEach(async ({ context, baseURL }, info) => {
        test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
        await conSesionTecnico(context, baseURL ?? "");
    });

    test("muestra cada formulario con su versión y agrega uno del catálogo sin señal (con un ítem de selección)", async ({ page, context }) => {
        test.setTimeout(60_000);
        const api = await simularApiCampo(page, { ordenes: ordenesConFormularios(), plantillas: [INVENTARIO] });
        await abrirOrden(page);
        const visita = page.getByTestId("formulario-visita");
        await expect(visita).toContainText("Visita técnica");
        await expect(visita).toContainText("Versión 3 · Del tipo de orden");
        await expect(visita.getByRole("button", { name: "Quitar" })).toHaveCount(0);

        await sinSenal(context, api, true);
        await page.getByRole("button", { name: "Agregar formulario" }).click();
        const dialogo = page.getByRole("dialog", { name: "Agregar formulario" });
        await expect(dialogo).toContainText("Versión 2 · 2 ítems");
        await dialogo.getByRole("button", { name: /Inventario de equipos/ }).click();
        await expect(notificacion(page)).toContainText("«Inventario de equipos» agregado. Se enviará al volver la señal.");

        const inventario = page.getByTestId("formulario-inventario");
        await expect(inventario).toContainText("Versión 2 · Agregado por ti · 0 de 2 completos");
        await expect(inventario.getByRole("button", { name: "Quitar" })).toBeVisible();
        // Selección: una de las opciones de la plantilla.
        await inventario.getByRole("radiogroup", { name: "Modelo de router" }).getByRole("radio", { name: "ZTE" }).click();
        await expect(inventario).toContainText("1 de 2 completos");
        // Con respuestas ya no se puede quitar.
        await expect(inventario.getByRole("button", { name: "Quitar" })).toHaveCount(0);
        expect(operaciones(api)).toHaveLength(0);

        await sinSenal(context, api, false);
        await expect.poll(() => api.ordenes.get(102)?.formularios?.length, { timeout: 15_000 }).toBe(2);
        const ops = operaciones(api);
        expect(ops.map((o) => o.tipo)).toEqual(["orden.agregar_formulario", "checklist.marcar"]);
        expect(ops[0]?.datos).toEqual({ plantillaId: 8, versionId: 82 });
        expect(ops[1]?.datos).toMatchObject({ itemId: 801, valor: "ZTE" });
        // El id de la operación es el del formulario.
        expect(api.ordenes.get(102)?.formularios?.[1]?.id).toBe(ops[0]?.id);
        expect(api.ordenes.get(102)?.checklist?.find((i) => i.itemId === 801)?.valor).toBe("ZTE");
    });

    test("quita un formulario que agregó sin respuestas; el del tipo nunca", async ({ page, context }) => {
        const api = await simularApiCampo(page, { ordenes: ordenesConFormularios(), plantillas: [INVENTARIO] });
        await abrirOrden(page);
        await sinSenal(context, api, true);
        await page.getByRole("button", { name: "Agregar formulario" }).click();
        await page.getByRole("dialog", { name: "Agregar formulario" }).getByRole("button", { name: /Inventario de equipos/ }).click();
        await page.getByTestId("formulario-inventario").getByRole("button", { name: "Quitar" }).click();
        await expect(page.getByTestId("formulario-inventario")).toHaveCount(0);
        await sinSenal(context, api, false);
        await expect.poll(() => operaciones(api).map((o) => o.tipo), { timeout: 15_000 }).toEqual(["orden.agregar_formulario", "orden.quitar_formulario"]);
        const [agregar, quitar] = operaciones(api);
        expect(quitar?.datos).toEqual({ formularioId: agregar?.id });
        expect(api.ordenes.get(102)?.formularios?.map((f) => f.codigo)).toEqual(["visita"]);
    });

    test("si despacho desactiva la plantilla mientras no hay señal, el rechazo queda a la vista", async ({ page, context }) => {
        const api = await simularApiCampo(page, { ordenes: ordenesConFormularios(), plantillas: [INVENTARIO] });
        await abrirOrden(page);
        await sinSenal(context, api, true);
        await page.getByRole("button", { name: "Agregar formulario" }).click();
        await page.getByRole("dialog", { name: "Agregar formulario" }).getByRole("button", { name: /Inventario de equipos/ }).click();
        api.plantillas = [];
        await sinSenal(context, api, false);
        await expect.poll(() => api.resultados.map((r) => r.estado), { timeout: 30_000 }).toEqual(["rechazada"]);
        await expect(page.getByText("1 acción rechazada por el servidor")).toBeVisible({ timeout: 15_000 });
        await expect(page.getByTestId("formulario-inventario")).toHaveCount(0);
        await page.getByRole("button", { name: "Revisar" }).click();
        await expect(page.getByText(/Ese formulario se desactivó mientras no tenías señal/)).toBeVisible();
        // El catálogo de cambios ya no lo ofrece.
        await page.getByRole("button", { name: "Descartar" }).click();
        await pestanaCampo(page, "Hoy");
        await page.getByRole("link", { name: /OT-000102/ }).click();
        await irAPaso(page, "Checklist");
        await page.getByRole("button", { name: "Agregar formulario" }).click();
        await expect(page.getByRole("dialog", { name: "Agregar formulario" })).toContainText("No hay más formularios en el catálogo del teléfono.");
    });

    test("pausa sin señal con motivo y nota, sigue con evidencia, muestra el tiempo pausado y reanuda", async ({ page, context }) => {
        test.setTimeout(60_000);
        await page.clock.install({ time: new Date("2026-10-28T10:00:00-06:00") });
        const api = await simularApiCampo(page, { ordenes: ordenesConFormularios() });
        await abrirOrden(page, "OT-000102", null);
        await expect(page.getByRole("button", { name: "No se pudo" })).toBeVisible();

        await sinSenal(context, api, true);
        await page.getByRole("button", { name: "Pausar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Pausar OT-000102" });
        await dialogo.getByLabel("Motivo").selectOption("otro");
        await dialogo.getByRole("button", { name: "Pausar" }).click();
        await expect(dialogo.getByText("Con «Otro motivo» explica qué pasa")).toBeVisible();
        await dialogo.getByLabel("Nota").fill("El cliente pidió volver después del almuerzo");
        await dialogo.getByRole("button", { name: "Pausar" }).click();

        const pausada = page.getByTestId("orden-pausada");
        await expect(pausada).toContainText("Orden pausada: Otro motivo");
        await expect(pausada).toContainText("El cliente pidió volver después del almuerzo");
        await expect(page.locator('[data-slot="insignia-estado"]', { hasText: "Pausada" }).first()).toBeVisible();
        for (const nombre of ["No se pudo", "Llegué", "En camino", "Pausar"]) {
            await expect(page.getByRole("button", { name: nombre, exact: true })).toHaveCount(0);
        }
        // Evidencia: el checklist y las notas siguen.
        await irAPaso(page, "Checklist");
        await page.getByRole("radiogroup", { name: "Señal restablecida" }).getByRole("radio", { name: "Sí" }).click();
        await expect(notificacion(page)).toContainText("«Señal restablecida» guardado");
        // Con la orden pausada no se cierra: el paso final lo dice.
        await irAPaso(page, "Firma del cliente y cierre");
        await expect(page.getByText("La orden está pausada")).toBeVisible();
        await alResumen(page);

        await page.clock.fastForward("15:00");
        await expect(page.getByTestId("tiempo-pausado").first()).toContainText("15 min en total");
        await pausada.getByRole("button", { name: "Reanudar" }).click();
        await expect(page.getByRole("button", { name: "No se pudo" })).toBeVisible();
        await expect(page.getByTestId("tiempo-pausado")).toContainText("Tiempo pausado en esta visita: 15 min");

        await sinSenal(context, api, false);
        await expect.poll(() => operaciones(api).map((o) => o.tipo), { timeout: 15_000 }).toEqual(["orden.pausar", "checklist.marcar", "orden.reanudar"]);
        expect(api.ordenes.get(102)?.estado).toBe("en_sitio");
        expect(operaciones(api)[0]?.datos).toEqual({ motivo: "otro", nota: "El cliente pidió volver después del almuerzo" });
        // 15 min del reloj de Playwright (más lo poco que corre el reloj real durante la prueba).
        expect(api.ordenes.get(102)?.tiempoPausadoSeg).toBeGreaterThanOrEqual(900);
        expect(api.ordenes.get(102)?.tiempoPausadoSeg).toBeLessThan(960);
    });

    test("una orden pausada no mantiene la ubicación encendida; al reanudar vuelve", async ({ page, context }) => {
        await context.grantPermissions(["geolocation"]);
        await context.setGeolocation({ latitude: 14.6407, longitude: -90.5133, accuracy: 12 });
        await context.addInitScript((clave) => window.localStorage.setItem(clave, "aceptada"), `iris-campo-ubicacion-explicada-${TECNICO.id}`);
        const api = await simularApiCampo(page, { ordenes: ordenesConFormularios("en_camino") });
        await abrirOrden(page, "OT-000102", null);
        await empezarTrabajo(page);
        const indicador = page.getByTestId("indicador-ubicacion");
        await page.getByRole("button", { name: "Llegué" }).click();
        await expect(indicador).toHaveText("Ubicación activa");

        await page.getByRole("button", { name: "Pausar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Pausar OT-000102" });
        await dialogo.getByLabel("Motivo").selectOption("esperando_material");
        await dialogo.getByRole("button", { name: "Pausar" }).click();
        await expect(notificacion(page)).toContainText("Turno terminado: no tienes órdenes en curso");
        await expect(indicador).toHaveText("Fuera de turno");

        await page.getByTestId("orden-pausada").getByRole("button", { name: "Reanudar" }).click();
        await expect(notificacion(page)).toContainText("Orden reanudada. Turno iniciado: se envía tu ubicación cada minuto");
        await expect(indicador).toHaveText("Ubicación activa");
        await expect
            .poll(() => operaciones(api).map((o) => o.tipo), { timeout: 15_000 })
            .toEqual(["orden.llegar", "orden.pausar", "orden.reanudar"]);
        expect(api.ordenes.get(102)?.estado).toBe("en_sitio");
    });

    test("cierre incompleto: el mensaje de la API por formulario y el ítem que falta señalado", async ({ page }) => {
        const api = await simularApiCampo(page, { ordenes: ordenesConFormularios() });
        await abrirOrden(page, "OT-000102", "Firma del cliente y cierre");
        // El teléfono dice qué falta y de qué formulario antes de la firma y el cierre.
        await expect(page.getByTestId("falta-para-firmar")).toContainText("Falta «Señal restablecida» (Visita técnica).");
        await page.getByTestId("falta-para-firmar").getByRole("button", { name: "Ir a checklist" }).click();

        // Lo marca, pero el servidor responde que falta (agrupado por formulario).
        api.rechazar((op) => op.tipo === "orden.cerrar", {
            type: "urn:iris:problema:cierre_incompleto",
            title: "Cierre incompleto",
            status: 422,
            errors: [{ field: `formularios.${FORM_VISITA.id}.checklist.senal_ok`, message: "Falta: Señal restablecida (Visita técnica)" }],
        });
        await page.getByRole("radiogroup", { name: "Señal restablecida" }).getByRole("radio", { name: "Sí" }).click();
        await irAPaso(page, "Firma del cliente y cierre");
        await page.getByRole("button", { name: "Cerrar orden" }).click();
        await expect(page.getByText("1 acción rechazada por el servidor")).toBeVisible({ timeout: 15_000 });
        await irAPaso(page, "Checklist");
        const visita = page.getByTestId("formulario-visita");
        await expect(visita).toContainText("Faltan 1 para cerrar");
        await expect(visita.locator('[data-slot="insignia-estado"]', { hasText: "Falta para cerrar" })).toBeVisible();
        await page.getByRole("button", { name: "Revisar" }).click();
        await expect(page.getByText("Falta: Señal restablecida (Visita técnica)")).toBeVisible();
    });
});

test("capturas de formularios y pausa", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `campo-formularios-${que}-${info.project.name}.png`);
    await conSesionTecnico(context, baseURL ?? "");
    const ordenes = ordenesConFormularios().map((o) =>
        o.id === 102
            ? {
                  ...o,
                  estado: "pausada" as const,
                  pausadaDesde: "en_sitio" as const,
                  pausadaEn: new Date(Date.now() - 20 * 60_000).toISOString(),
                  tiempoPausadoSeg: 300,
                  pausas: [
                      {
                          id: 1,
                          motivo: "esperando_material" as const,
                          nota: null,
                          desdeEstado: "en_sitio" as const,
                          inicio: new Date(Date.now() - 20 * 60_000).toISOString(),
                          fin: null,
                          duracionSeg: null,
                          cierre: null,
                          pausadaPor: TECNICO.id,
                          cerradaPor: null,
                      },
                  ],
              }
            : o,
    );
    await simularApiCampo(page, { ordenes, plantillas: [INVENTARIO] });
    await abrirOrden(page, "OT-000102", null);
    await expect(page.getByTestId("orden-pausada")).toContainText("Orden pausada: Esperando material");
    await page.screenshot({ path: nombre("01-pausada"), fullPage: true });
    await irAPaso(page, "Checklist");
    await page.getByRole("button", { name: "Agregar formulario" }).click();
    await expect(page.getByRole("dialog", { name: "Agregar formulario" })).toBeVisible();
    await page.screenshot({ path: nombre("02-agregar") });
});
