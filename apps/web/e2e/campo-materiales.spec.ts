import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { conSesionTecnico, simularApiCampo, type ApiCampo } from "./api-campo";
import { abrirMateriales, avisoCampo, elegirMaterial } from "./ayudas-campo";
import { CONECTOR, ordenesDelDia } from "./datos-campo";

/*
 * Paso de materiales en la app de campo (teléfono, 390 px, claro y oscuro): buscar en las
 * existencias de su bodega por nombre o código (del paquete del día, sin señal también), lo que queda
 * y la modalidad de cada material, el equipo en comodato que va a «Equipos» con su serie, y la
 * cantidad con − / + de 48 px y atajos, que dice cuánto quedaría. Sin señal se registra igual y
 * sale al volver.
 */

type OrdenCampo = Schema<"OrdenCampo">;
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

function conUnaEnSitio(): OrdenCampo[] {
    const ahora = new Date().toISOString();
    return ordenesDelDia().map((o) => (o.id === 102 ? { ...o, estado: "en_sitio", enCaminoEn: ahora, enSitioEn: ahora } : o));
}

async function sinSenal(page: Page, api: ApiCampo, sin: boolean) {
    api.sinRed = sin;
    await page.context().setOffline(sin);
}

async function alto(page: Page, nombre: string): Promise<number> {
    return (await page.getByRole("button", { name: nombre }).boundingBox())?.height ?? 0;
}

test("busca por código, ve lo que queda y registra sin señal con − / + y atajos", async ({ page }, info) => {
    const api = await simularApiCampo(page, { ordenes: conUnaEnSitio() });
    await page.goto("/campo?orden=102&paso=materiales");
    await expect(page.getByRole("heading", { level: 1, name: "Materiales y equipos" })).toBeVisible();
    await expect(page.getByText("De tu bodega: Bodega Norte")).toBeVisible();

    // Sin señal: la lista sale del paquete guardado en el teléfono.
    await sinSenal(page, api, true);
    const lista = await abrirMateriales(page);
    await expect(lista.getByRole("option")).toHaveCount(3);
    await expect(lista.getByRole("option").filter({ hasText: "Cable drop 1 hilo" })).toContainText("CAB-DROP-1H · Quedan 120 m · Consumible");
    const porCodigo = await abrirMateriales(page, "con-sc");
    await expect(porCodigo.getByRole("option")).toHaveCount(1);
    await expect(porCodigo.getByRole("option")).toContainText("CON-SCAPC · Quedan 10 u · Se cobra al cliente");
    await page.screenshot({ path: path.join(carpeta, `campo-materiales-buscar-${info.project.name}.png`) });
    await elegirMaterial(page, "Conector SC/APC", "con-sc");

    const elegido = page.getByTestId("material-elegido");
    await expect(elegido).toContainText("Código CON-SCAPC");
    await expect(elegido).toContainText("Se cobra al cliente");
    await expect(elegido).toContainText("En tu bodega: 10 u");
    await expect(page.getByLabel("Cantidad")).toHaveValue("1");
    await expect(page.getByTestId("existencia-despues")).toHaveText("Te quedarían 9 u.");
    // Zonas táctiles de 48 px.
    for (const b of ["Quitar uno", "Agregar uno", "Agregar 10", "Registrar material"]) {
        expect(await alto(page, b)).toBeGreaterThanOrEqual(48);
    }
    await page.getByRole("button", { name: "Agregar 10" }).click();
    await expect(page.getByLabel("Cantidad")).toHaveValue("11");
    await expect(page.getByTestId("existencia-despues")).toHaveText("Supera lo que tienes por 1 u: se registra igual y se avisa al supervisor.");
    await page.getByRole("button", { name: "Quitar uno" }).click();
    await expect(page.getByTestId("existencia-despues")).toHaveText("Te quedarían 0 u.");
    await page.screenshot({ path: path.join(carpeta, `campo-materiales-cantidad-${info.project.name}.png`), fullPage: true });
    await page.getByRole("button", { name: "Registrar material" }).click();
    await expect(avisoCampo(page)).toContainText("Material registrado");
    await expect(page.getByRole("list", { name: "Materiales usados" })).toContainText("Conector SC/APC10 u");
    // La existencia del teléfono ya descuenta lo que está en la cola.
    await elegirMaterial(page, "Conector SC/APC", "conector");
    await expect(page.getByTestId("material-elegido")).toContainText("En tu bodega: 0 u");

    // Con señal, sale en el lote.
    await sinSenal(page, api, false);
    await expect
        .poll(() => api.lotes.flatMap((l) => l.operaciones ?? []).find((op) => op.tipo === "material.usar")?.datos)
        .toMatchObject({ productoId: CONECTOR, cantidad: 10 });
});

test("un equipo en comodato no se registra como material: va a «Equipos» con su serie", async ({ page }, info) => {
    await simularApiCampo(page, { ordenes: conUnaEnSitio() });
    await page.goto("/campo?orden=102&paso=materiales");
    await elegirMaterial(page, "ONU ZTE F670L", "f670");
    const elegido = page.getByTestId("material-elegido");
    await expect(elegido).toContainText("Equipo con serie");
    await expect(elegido.getByText("Se registra en «Equipos»")).toBeVisible();
    await expect(elegido).toContainText("regístralo en «Equipos» con su número de serie");
    await expect(page.getByRole("button", { name: "Registrar material" })).toHaveCount(0);
    await expect(page.getByLabel("Cantidad")).toHaveCount(0);
    await page.screenshot({ path: path.join(carpeta, `campo-materiales-comodato-${info.project.name}.png`) });
});

test("sin coincidencias lo dice", async ({ page }) => {
    await simularApiCampo(page, { ordenes: conUnaEnSitio() });
    await page.goto("/campo?orden=102&paso=materiales");
    const lista = await abrirMateriales(page, "fibra 48 hilos");
    await expect(lista).toContainText("Ningún material de tu bodega coincide.");
});
