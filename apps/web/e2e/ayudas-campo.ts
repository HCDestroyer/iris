import { expect, type Page } from "@playwright/test";

/*
 * Ayudas de la app de campo por pasos (fase 4): resumen → llegada · checklist · materiales y equipos
 * · fotos → «Firma del cliente y cierre», y la barra inferior.
 */

export type Paso = "Llegada" | "Checklist" | "Materiales y equipos" | "Fotos" | "Firma del cliente y cierre";

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

/** Pestaña de la barra inferior. */
export async function pestanaCampo(page: Page, nombre: "Hoy" | "Por recibir" | "Pendientes" | "Yo") {
    await page.getByRole("navigation", { name: "Secciones de la app" }).getByRole("link", { name: new RegExp(`^${nombre}( \\(\\d+\\))?$`) }).click();
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
