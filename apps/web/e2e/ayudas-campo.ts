import { expect, type Locator, type Page } from "@playwright/test";

/*
 * Ayudas de la app de campo por pasos (fase 4): resumen → llegada · checklist · materiales y equipos
 * · fotos → «Firma del cliente y cierre», y la barra inferior.
 */

export type Paso = "Llegada" | "Checklist" | "Materiales y equipos" | "Fotos" | "Firma del cliente y cierre";

/**
 * El aviso emergente de la app de campo (el `Toast` de HeroUI arriba): el de adelante que no se
 * está yendo.
 */
export function avisoCampo(page: Page): Locator {
    return page.locator('[data-slot="toast"][data-frontmost="true"]:not([data-exiting="true"])');
}

/** El único «Guardar» de los pasos (en la isla de pasos): guarda lo escrito del paso. */
export async function guardarPaso(page: Page) {
    await page.getByRole("navigation", { name: "Pasos", exact: true }).getByRole("button", { name: /^Guardar/ }).click();
}

/** Abre un paso desde el avance de la orden (resumen o cualquier paso). */
export async function irAPaso(page: Page, paso: Paso) {
    await page.getByRole("navigation", { name: "Pasos de la orden" }).getByRole("link", { name: new RegExp(`^\\d\\. ${paso}:`) }).click();
    await expect(page.getByRole("heading", { level: 1, name: paso })).toBeVisible();
}

/** «Empezar trabajo» (o «Continuar trabajo») desde el resumen. */
export async function empezarTrabajo(page: Page) {
    await page.getByRole("button", { name: /^(Empezar|Continuar) trabajo$/ }).click();
}

/** Del paso al resumen de la orden. */
export async function alResumen(page: Page) {
    await page.getByRole("button", { name: /^Resumen de OT-/ }).click();
}

/** Botón de la isla de menú (opción del PM: Hoy · Chat · «+» · Agenda · Yo). «Yo» se llama «Yo · <nombre>». */
export async function pestanaCampo(page: Page, nombre: "Hoy" | "Chat" | "Agenda" | "Yo") {
    await page.getByRole("navigation", { name: "Secciones de la app" }).getByRole("link", { name: new RegExp(`^${nombre}( \\(\\d+ sin leer\\)| · .+)?$`) }).click();
}

/** «Por recibir» ya no está en la isla: se abre desde su mosaico en Hoy. */
export async function abrirPorRecibir(page: Page) {
    await pestanaCampo(page, "Hoy");
    await page.locator('[data-slot="mosaico-recibir"]').click();
}

/** El GPS del encabezado abre su hoja «Ubicación»; «Detalle de la ubicación» abre «Ubicación en vivo». */
export async function abrirPanelUbicacion(page: Page) {
    await page.getByTestId("indicador-ubicacion").click();
    await page.getByRole("dialog", { name: "Ubicación" }).getByRole("button", { name: "Detalle de la ubicación" }).click();
}

/** La nube de sincronización del encabezado (conexión y envíos) abre su hoja «Conexión y envíos». */
export async function abrirConexion(page: Page) {
    await page.locator('[data-slot="barra-conexion"] [data-indicador="sincronizacion"]').click();
    await expect(page.getByRole("dialog", { name: "Conexión y envíos" })).toBeVisible();
}

/**
 * «Por enviar» no está en la isla: se llega desde «Yo» → «Por enviar» (o desde la hoja de la nube).
 * Desde un paso de la orden (sin isla) se va a «Yo» por el historial, sin recargar (sirve sin señal).
 */
export async function abrirPorEnviar(page: Page) {
    const isla = page.getByRole("navigation", { name: "Secciones de la app" });
    if (await isla.isVisible()) {
        await pestanaCampo(page, "Yo");
    } else {
        await page.evaluate(() => {
            window.history.pushState(null, "", "/campo?vista=yo");
            window.dispatchEvent(new PopStateEvent("popstate"));
        });
    }
    await page.getByRole("button", { name: /^Por enviar/ }).click();
    await expect(page.getByRole("heading", { name: "Por enviar", exact: true })).toBeVisible();
}

/** Firma en el lienzo con un trazo (al centro de la pantalla). */
export async function firmarEn(page: Page, nombre: RegExp | string = /Firma del cliente/) {
    const lienzo = page.getByRole("img", { name: nombre });
    await lienzo.evaluate((el) => el.scrollIntoView({ block: "center" }));
    const caja = await lienzo.boundingBox();
    if (!caja) {
        throw new Error("sin lienzo");
    }
    await page.mouse.move(caja.x + 30, caja.y + caja.height * 0.7);
    await page.mouse.down();
    for (let i = 1; i <= 12; i += 1) {
        await page.mouse.move(caja.x + 30 + i * 22, caja.y + caja.height * (0.7 - Math.sin(i / 2) * 0.35));
    }
    await page.mouse.up();
}
