import { expect, type Locator, type Page } from "@playwright/test";

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

/** Panel de la cuenta en el encabezado (escritorio): identidad, empresa, disponibilidad y tema. */
export function panelCuenta(page: Page) {
    return page.getByRole("dialog", { name: /^Tu cuenta/ });
}

/** Cierra «Más» (teléfono) o el panel de la cuenta (escritorio) si está abierto. */
export async function cerrarMenu(page: Page) {
    if (esTelefono(page) && (await hojaMas(page).count()) > 0) {
        await page.keyboard.press("Escape");
        await expect(hojaMas(page)).toHaveCount(0);
    } else if (!esTelefono(page) && (await panelCuenta(page).count()) > 0) {
        await page.keyboard.press("Escape");
        await expect(panelCuenta(page)).toHaveCount(0);
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

/** Selector de la empresa activa: en el teléfono está en «Más»; en escritorio, en el panel de la cuenta. */
export async function selectorEmpresa(page: Page) {
    if (esTelefono(page)) {
        await abrirMenu(page);
    } else if ((await panelCuenta(page).count()) === 0) {
        await page.getByRole("button", { name: /^Tu cuenta/ }).click();
        await expect(panelCuenta(page)).toBeVisible();
    }
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

/** Abre la pestaña `nombre` de una ficha (Resumen, Conversación, Argos…). */
export async function pestana(page: Page, nombre: string) {
    await page.getByRole("tab", { name: nombre, exact: true }).click();
    await expect(page.getByRole("tab", { name: nombre, exact: true })).toHaveAttribute("aria-selected", "true");
}

/** Abre el menú `⋯` de una fila de `ListaFilas` («Acciones de <nombre>») y lo devuelve. */
export async function abrirMenuFila(page: Page, nombre: string) {
    await page.getByRole("button", { name: `Acciones de ${nombre}`, exact: true }).filter({ visible: true }).click();
    const menu = page.getByRole("menu", { name: `Acciones de ${nombre}`, exact: true });
    await expect(menu).toBeVisible();
    return menu;
}

/** Elige `accion` en el menú `⋯` de la fila. */
export async function accionFila(page: Page, nombre: string, accion: string) {
    const menu = await abrirMenuFila(page, nombre);
    await menu.getByRole("menuitem", { name: accion, exact: true }).click();
}

/** Comprueba si el menú `⋯` de la fila ofrece `accion` (lo abre, mira y lo cierra con Escape). */
export async function ofreceAccionFila(page: Page, nombre: string, accion: string, ofrece: boolean) {
    const menu = await abrirMenuFila(page, nombre);
    await expect(menu.getByRole("menuitem", { name: accion, exact: true })).toHaveCount(ofrece ? 1 : 0);
    await page.keyboard.press("Escape");
    await expect(menu).toHaveCount(0);
}

/**
 * Ningún menú `⋯` de las filas visibles de `lista` ofrece `accion`: abre cada uno, mira y lo cierra.
 * Exige que haya al menos una fila con menú (si no, la prueba no probaría nada).
 */
export async function ningunaFilaOfrece(page: Page, lista: Locator, accion: string) {
    const botones = lista.getByRole("button", { name: /^Acciones de / }).filter({ visible: true });
    await expect(botones.first()).toBeVisible();
    const n = await botones.count();
    for (let i = 0; i < n; i++) {
        await botones.nth(i).click();
        const menu = page.getByRole("menu").filter({ visible: true });
        await expect(menu).toBeVisible();
        await expect(menu.getByRole("menuitem", { name: accion, exact: true })).toHaveCount(0);
        await page.keyboard.press("Escape");
        await expect(menu).toHaveCount(0);
    }
}
