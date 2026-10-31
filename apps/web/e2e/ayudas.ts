import { expect, type Page } from "@playwright/test";

type Nombre = string | RegExp;

/** Por debajo de `lg` (1024 px) no hay barra lateral: barra inferior y «Más». */
export function esTelefono(page: Page): boolean {
    return (page.viewportSize()?.width ?? 0) < 1024;
}

/** Barra inferior del teléfono (Inicio, Tickets, Órdenes, Agenda y «Más»). */
export function barraInferior(page: Page) {
    return page.getByRole("navigation", { name: "Accesos rápidos" });
}

/** Hoja «Más» del teléfono (resto del menú, empresa, cuenta y tema). */
export function hojaMas(page: Page) {
    return page.getByRole("dialog", { name: "Más" });
}

/** En el teléfono abre «Más»; en escritorio no hace nada (el menú está en la barra lateral). */
export async function abrirMenu(page: Page) {
    if (esTelefono(page) && (await hojaMas(page).count()) === 0) {
        await barraInferior(page).getByRole("button", { name: "Más" }).click();
        await expect(hojaMas(page)).toBeVisible();
    }
}

/** Cierra «Más» si está abierta. */
export async function cerrarMenu(page: Page) {
    if (esTelefono(page) && (await hojaMas(page).count()) > 0) {
        await page.keyboard.press("Escape");
        await expect(hojaMas(page)).toHaveCount(0);
    }
}

/** Menú principal visible (barra lateral, o en el teléfono la lista de «Más»). */
export function menu(page: Page) {
    return page.getByRole("navigation", { name: "Principal" }).filter({ visible: true });
}

/**
 * Enlace de una entrada del menú: en escritorio, la barra lateral; en el teléfono, la barra
 * inferior si está ahí y, si no, la lista de «Más» (que deja abierta).
 */
export async function enlaceMenu(page: Page, nombre: Nombre) {
    if (!esTelefono(page)) return menu(page).getByRole("link", { name: nombre });
    if ((await hojaMas(page).count()) === 0) {
        await expect(barraInferior(page)).toBeVisible();
        const enBarra = barraInferior(page).getByRole("link", { name: nombre });
        if ((await enBarra.count()) > 0) return enBarra;
        await abrirMenu(page);
    }
    return menu(page).getByRole("link", { name: nombre });
}

/** Va a una entrada del menú, esté donde esté según el tamaño de la pantalla. */
export async function irA(page: Page, nombre: Nombre) {
    await (await enlaceMenu(page, nombre)).click();
}

/** Ninguna de estas entradas está en el menú (en el teléfono: ni en la barra inferior ni en «Más»). */
export async function sinEntradas(page: Page, nombres: readonly Nombre[]) {
    if (!esTelefono(page)) {
        for (const n of nombres) await expect(menu(page).getByRole("link", { name: n })).toHaveCount(0);
        return;
    }
    await cerrarMenu(page);
    await expect(barraInferior(page)).toBeVisible();
    for (const n of nombres) await expect(barraInferior(page).getByRole("link", { name: n })).toHaveCount(0);
    await abrirMenu(page);
    for (const n of nombres) await expect(hojaMas(page).getByRole("link", { name: n })).toHaveCount(0);
    await cerrarMenu(page);
}

/** Abre el menú de la cuenta: «Tu cuenta» en escritorio, «Más» en el teléfono (con la cuenta como menú). */
export async function abrirCuenta(page: Page) {
    if (esTelefono(page)) {
        await abrirMenu(page);
        await expect(hojaMas(page).getByRole("menu", { name: "Tu cuenta" })).toBeVisible();
    } else {
        await page.getByRole("button", { name: /^Tu cuenta/ }).click();
    }
}

/** Selector de la empresa activa (en el teléfono está en «Más»). */
export async function selectorEmpresa(page: Page) {
    await abrirMenu(page);
    return page.getByLabel("Empresa activa").filter({ visible: true });
}

export async function entrar(page: Page, correo: string, contrasena: string) {
    await page.getByLabel("Correo").fill(correo);
    await page.getByLabel("Contraseña").fill(contrasena);
    await page.getByRole("button", { name: "Entrar" }).click();
}

/**
 * Confirmación abierta (`role="alertdialog"`), sin contar los avisos emergentes: HeroUI los
 * anuncia también como `alertdialog` (no modales) dentro de la región de notificaciones.
 */
export function confirmacion(page: Page) {
    return page.getByRole("alertdialog").and(page.locator(':not([data-slot="toast"])'));
}

/** Aviso de error de la pantalla (no el anunciador de rutas de Next, que también es role=alert). */
export function avisoError(page: Page) {
    return page.locator('[data-slot="aviso"][role="alert"]');
}
