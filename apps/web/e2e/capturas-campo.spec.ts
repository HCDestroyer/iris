import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test, type Page } from "@playwright/test";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { CABLE } from "./datos-campo";

// Capturas de la app de campo para revisión visual: IRIS_CAPTURAS=<carpeta> (por defecto
// test-results/capturas). Teléfono (390 px) y escritorio (1280 px), claro y oscuro.
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const FOTO = fileURLToPath(new URL("./fixtures/foto-fachada.jpg", import.meta.url));

async function firmar(page: Page) {
    const lienzo = page.getByRole("img", { name: /Firma del cliente/ });
    await lienzo.evaluate((el) => el.scrollIntoView({ block: "center" }));
    const caja = await lienzo.boundingBox();
    if (!caja) {
        throw new Error("sin lienzo");
    }
    await page.mouse.move(caja.x + 30, caja.y + caja.height * 0.6);
    await page.mouse.down();
    for (let i = 1; i <= 14; i += 1) {
        await page.mouse.move(caja.x + 30 + i * 20, caja.y + caja.height * (0.6 - Math.sin(i / 2) * 0.3));
    }
    await page.mouse.up();
}

test("capturas de la app de campo", async ({ page, context, baseURL }, info) => {
    test.setTimeout(120_000);
    const telefono = info.project.name.startsWith("telefono");
    const nombre = (que: string) => path.join(carpeta, `campo-${que}-${info.project.name}.png`);
    await conSesionTecnico(context, baseURL ?? "");
    // Con el permiso de ubicación ya dado: el indicador muestra «Ubicación activa».
    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation({ latitude: 14.6407, longitude: -90.5133, accuracy: 12 });
    const api = await simularApiCampo(page);
    api.rechazar((op) => op.tipo === "orden.iniciar_camino" && op.ordenId === 103, {
        type: "urn:iris:problema:orden_cancelada",
        title: "Orden cancelada",
        status: 409,
    });

    await page.goto("/campo");
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
    await expect(page.getByRole("button", { name: /Todo enviado/ })).toBeVisible();
    await page.screenshot({ path: nombre("01-lista"), fullPage: true });

    await page.getByRole("link", { name: /OT-000101/ }).click();
    await expect(page.getByRole("button", { name: "Llegué" })).toBeVisible();
    await page.screenshot({ path: nombre("02-detalle-programada"), fullPage: true });
    if (!telefono) {
        return;
    }

    // Sin señal: llegada, checklist, foto, material y firma quedan en el teléfono.
    api.sinRed = true;
    await context.setOffline(true);
    await expect(page.getByTestId("estado-conexion")).toHaveText("Sin conexión");
    await page.getByRole("button", { name: "Llegué" }).click();
    await page.getByRole("radiogroup", { name: "La ONU enciende" }).getByRole("radio", { name: "Sí" }).click();
    await page.getByRole("textbox", { name: "Potencia óptica" }).fill("-18,5");
    await page.getByRole("button", { name: "Guardar Potencia óptica" }).click();
    await page.getByLabel("Tomar foto: Foto de la fachada").setInputFiles(FOTO);
    await expect(page.getByRole("img", { name: "Foto de la fachada" })).toBeVisible();
    await page.getByLabel("Material").selectOption(String(CABLE));
    await page.getByLabel("Cantidad").fill("35");
    await page.getByRole("button", { name: "Registrar material" }).click();
    await page.getByLabel("Nombre de quien firma").fill("María Castillo");
    await firmar(page);
    await page.evaluate(() => window.scrollTo({ top: 0 }));
    await page.screenshot({ path: nombre("03-en-sitio-sin-conexion"), fullPage: true });
    await page.getByRole("button", { name: "Guardar firma" }).click();
    await expect(page.getByText("Firmada")).toBeVisible();
    await page.getByRole("button", { name: "Cerrar orden" }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.screenshot({ path: nombre("04-cerrar-falta") });
    await page.getByRole("dialog").getByRole("button", { name: "Volver" }).click();

    await page.getByRole("button", { name: /pendientes por enviar/ }).click();
    await expect(page.getByRole("list", { name: "Cola de envío" })).toBeVisible();
    await page.screenshot({ path: nombre("05-por-enviar"), fullPage: true });

    // Con señal: se envía; la orden 103 tiene un rechazo visible.
    api.sinRed = false;
    await context.setOffline(false);
    await expect(page.getByText("No queda nada por enviar desde este teléfono.")).toBeVisible({ timeout: 30_000 });
    await page.getByRole("button", { name: "Mis órdenes" }).click();
    await page.getByRole("link", { name: /OT-000103/ }).click();
    await page.getByRole("button", { name: "En camino" }).click();
    await page.getByRole("button", { name: "Mis órdenes" }).click();
    await page.getByRole("button", { name: /rechazado/ }).click();
    await expect(page.getByTestId("rechazo")).toBeVisible({ timeout: 15_000 });
    await page.screenshot({ path: nombre("06-rechazo"), fullPage: true });
});
