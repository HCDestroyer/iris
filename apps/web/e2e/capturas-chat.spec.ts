import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { CONV, ORDEN_CERRADA, ORDEN_VIVA } from "./api-chat-panel";
import { esTelefono } from "./ayudas";

// Capturas del chat del equipo para revisión visual (a 2×): IRIS_CAPTURAS=<carpeta> (por defecto
// test-results/capturas). Nombre: <vista>-<1280|390>-<claro|oscuro>.png.
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
test.use({ deviceScaleFactor: 2 });

function nombre(page: Page, vista: string): string {
    const ancho = esTelefono(page) ? "390" : "1280";
    const tema = test.info().project.name.endsWith("oscuro") ? "oscuro" : "claro";
    return path.join(carpeta, `chat-${vista}-${ancho}-${tema}.png`);
}

async function abrir(page: Page, ruta: string) {
    const api = await simularApi(page, { chat: true });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

test("capturas del chat del equipo", async ({ page }) => {
    test.setTimeout(120_000);
    // Panel lateral sobre la agenda: lista y conversación.
    await abrir(page, "/agenda");
    await page.locator('[data-slot="boton-chat-equipo"]').click();
    const panel = page.getByRole("dialog", { name: "Chat del equipo" });
    await expect(panel.getByText("En vivo")).toBeVisible({ timeout: 10_000 });
    await page.screenshot({ path: nombre(page, "panel-lista") });
    await panel.locator(`[data-conversacion-chat="${CONV.ordenViva}"]`).click();
    await expect(panel.getByText("la bodega de Villa Nueva")).toBeVisible();
    await page.screenshot({ path: nombre(page, "panel-conversacion") });
    // «@» abre las menciones.
    await panel.getByRole("combobox", { name: /Mensaje para/ }).pressSequentially("Revisa @");
    await expect(page.getByRole("listbox", { name: "Mencionar a…" })).toBeVisible();
    await page.screenshot({ path: nombre(page, "panel-mencion") });
    await page.keyboard.press("Escape");
    await panel.getByRole("button", { name: "Atrás" }).click();
    await panel.getByRole("button", { name: "Nuevo mensaje" }).click();
    const nuevo = page.getByRole("dialog", { name: "Nuevo mensaje" });
    await nuevo.getByRole("radio", { name: "Cuadrilla" }).click();
    await expect(nuevo.getByText("Cuadrilla Centro")).toBeVisible();
    await nuevo.getByRole("radio", { name: "Cuadrilla Sur" }).click({ force: true });
    await page.screenshot({ path: nombre(page, "nuevo-mensaje") });
});

test("capturas de la página Equipo y de la orden", async ({ page }) => {
    test.setTimeout(120_000);
    await abrir(page, "/equipo");
    await expect(page.getByRole("heading", { name: "Equipo", level: 1 })).toBeVisible();
    await page.screenshot({ path: nombre(page, "pagina-lista") });
    await page.goto(`/equipo?c=${CONV.ordenViva}`);
    await expect(page.getByText("la bodega de Villa Nueva").filter({ visible: true })).toBeVisible();
    await page.screenshot({ path: nombre(page, "pagina-conversacion") });

    await page.goto(`/ordenes/${ORDEN_VIVA}?pestana=chat`);
    await expect(page.locator('[data-slot="chat-orden"]').getByText("la bodega de Villa Nueva")).toBeVisible();
    await page.screenshot({ path: nombre(page, "orden"), fullPage: !esTelefono(page) });
    await page.goto(`/ordenes/${ORDEN_CERRADA}?pestana=chat`);
    await expect(page.locator('[data-slot="compositor-bloqueado"]')).toBeVisible();
    await page.screenshot({ path: nombre(page, "orden-cerrada"), fullPage: !esTelefono(page) });
});

test("captura de «Reconectando…»", async ({ page }) => {
    const api = await simularApi(page, { chat: true });
    if (!api.chat) throw new Error("sin chat");
    api.chat.caido = true;
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(`/equipo?c=${CONV.ordenViva}`);
    await expect(page.locator('[data-slot="aviso"]').filter({ hasText: "Sin conexión en vivo" })).toBeVisible({ timeout: 15_000 });
    await page.screenshot({ path: nombre(page, "reconectando") });
});
