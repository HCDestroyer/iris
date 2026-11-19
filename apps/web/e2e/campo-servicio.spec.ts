import { expect, test, type Page } from "@playwright/test";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { PERMISOS_TECNICO } from "./datos-campo";

/*
 * App de campo: la línea de servicio de la orden (contrato con Argos 0.8.0 §2.4). El doble de la API
 * recorta como el servidor (permiso y «sus órdenes»); el teléfono no guarda ni muestra la red ni la
 * ubicación de una orden que dejó de ser suya.
 */

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

const linea = (page: Page) => page.getByTestId("linea-servicio").first();

test("el técnico ve la línea de su orden: plan, estado, precio, zona, router e IP", async ({ page }) => {
    await simularApiCampo(page);
    await page.goto("/campo?orden=101");
    await expect(linea(page)).toContainText("Fibra 100 Mbps");
    await expect(linea(page)).toContainText("Q250.00 al mes");
    await expect(linea(page)).toContainText("Nodo Norte");
    await expect(linea(page)).toContainText("100.64.41.1");
});

test("sin servicios.ver_red el servidor no manda la IP ni el router", async ({ page }) => {
    await simularApiCampo(page, { permisos: PERMISOS_TECNICO.filter((p) => p !== "servicios.ver_red") });
    await page.goto("/campo?orden=101");
    await expect(linea(page)).toContainText("Fibra 100 Mbps");
    await expect(linea(page)).toContainText("Q250.00 al mes");
    await expect(linea(page)).not.toContainText("100.64.41.1");
    await expect(linea(page)).not.toContainText("Nodo Norte");
});

test("una orden cancelada pierde la red en el teléfono (y no se vuelve a mostrar al recargar)", async ({ page }) => {
    const api = await simularApiCampo(page);
    await page.goto("/campo?orden=101");
    await expect(linea(page)).toContainText("100.64.41.1");
    // Despacho la cancela en el servidor: sale del paquete y llega como cambio.
    const orden = api.ordenes.get(101);
    if (orden) orden.estado = "cancelada";
    api.cambiar({ tipo: "cancelada", ordenId: 101 });
    // La siguiente pasada (al volver a abrir la app) trae el cambio y quita la IP de lo guardado.
    await page.reload();
    await expect.poll(async () => (await page.getByTestId("linea-servicio").allTextContents()).join(" ")).not.toContain("100.64.41.1");
});
