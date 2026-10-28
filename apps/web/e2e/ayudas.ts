import { expect, type Page } from "@playwright/test";

/** En teléfono el menú está en una hoja lateral: la abre. En escritorio no hace nada. */
export async function abrirMenu(page: Page) {
    // Por debajo de `lg` (1024 px) la barra lateral se oculta y el menú va en la hoja.
    if ((page.viewportSize()?.width ?? 0) < 1024) {
        await page.getByRole("button", { name: "Abrir menú" }).click();
        await expect(page.getByRole("dialog")).toBeVisible();
    }
}

/** Menú principal visible (barra lateral o la hoja en teléfono). */
export function menu(page: Page) {
    return page.getByRole("navigation", { name: "Principal" }).filter({ visible: true });
}

export async function entrar(page: Page, correo: string, contrasena: string) {
    await page.getByLabel("Correo").fill(correo);
    await page.getByLabel("Contraseña").fill(contrasena);
    await page.getByRole("button", { name: "Entrar" }).click();
}

/** Aviso de error de la pantalla (no el anunciador de rutas de Next, que también es role=alert). */
export function avisoError(page: Page) {
    return page.locator('[data-slot="aviso"][role="alert"]');
}
