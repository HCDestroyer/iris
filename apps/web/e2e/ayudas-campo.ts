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

/** «Guardar y seguir» de la isla de pasos (solo está con algo escrito sin guardar): guarda y avanza. */
export async function guardarYSeguir(page: Page) {
    await page.getByRole("navigation", { name: "Pasos", exact: true }).getByRole("button", { name: /^Guardar y seguir/ }).click();
}

/**
 * Guarda lo escrito sin salir del paso: el campo se guarda solo al salir de él, y la isla deja de
 * ofrecer «Guardar y seguir» (sin cambios no hay «Guardar»).
 */
export async function guardarAlSalirDelCampo(page: Page) {
    await page.locator(":focus").blur();
    await expect(page.getByRole("navigation", { name: "Pasos", exact: true }).getByRole("button", { name: /Guardar/ })).toHaveCount(0);
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

/**
 * «Agregar formulario» del menú «⋯» de la orden (decisión del PM: ya no va al final del checklist).
 * Desde un paso, primero vuelve al resumen; luego regresa al paso donde estaba.
 */
export async function agregarFormularioDesdeMenu(page: Page): Promise<Locator> {
    const enPaso = await page.getByRole("button", { name: /^Resumen de OT-/ }).isVisible();
    if (enPaso) {
        await alResumen(page);
    }
    await page.getByRole("button", { name: "Más acciones de la orden" }).click();
    await page.getByRole("menuitem", { name: /^Agregar formulario/ }).click();
    const dialogo = page.getByRole("dialog", { name: "Agregar formulario" });
    await expect(dialogo).toBeVisible();
    return dialogo;
}

/** Del paso al resumen de la orden. */
export async function alResumen(page: Page) {
    await page.getByRole("button", { name: /^Resumen de OT-/ }).click();
}

/** Botón de la isla de menú (diseño «combinada»: Hoy · Por recibir · «+» · Agenda · Yo). */
export async function pestanaCampo(page: Page, nombre: "Hoy" | "Por recibir" | "Agenda" | "Yo") {
    await page.getByRole("navigation", { name: "Secciones de la app" }).getByRole("link", { name: new RegExp(`^${nombre}( \\(\\d+\\))?$`) }).click();
}

/**
 * «Por enviar» ya no está en la isla: desde cualquier vista (también un paso de la orden, sin isla)
 * se llega por la cuenta («Yo») → «Por enviar».
 */
export async function abrirPorEnviar(page: Page) {
    await page.getByRole("button", { name: "Tu cuenta" }).first().click();
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

/*
 * Material del paso «Materiales y equipos»: un `Autocompletar` (ComboBox de React Aria). Su lista
 * se cierra si la página se desplaza (en el teléfono, al enfocar el campo): se abre con flecha
 * abajo y se elige con un clic «virtual», que no desplaza.
 */
export async function abrirMateriales(page: Page, buscar = ""): Promise<Locator> {
    const campo = page.getByRole("combobox", { name: "Material" });
    if (buscar !== "") await campo.fill(buscar);
    await expect(async () => {
        if ((await campo.getAttribute("aria-expanded")) !== "true") await campo.press("ArrowDown");
        expect(await campo.getAttribute("aria-expanded")).toBe("true");
    }).toPass({ intervals: [100, 200, 400] });
    return page.locator(`[id="${(await campo.getAttribute("aria-controls")) ?? ""}"]`);
}

/** Elige el material por su nombre (escribe `buscar` antes, si se da: nombre o código). */
export async function elegirMaterial(page: Page, nombre: string, buscar = ""): Promise<void> {
    const campo = page.getByRole("combobox", { name: "Material" });
    await expect(async () => {
        if ((await campo.inputValue()) === nombre && (await page.getByTestId("material-elegido").count()) > 0) return;
        const lista = await abrirMateriales(page, buscar);
        await lista.getByRole("option").filter({ hasText: nombre }).first().dispatchEvent("click", undefined, { timeout: 2000 });
        await expect(page.getByTestId("material-elegido")).toContainText(nombre, { timeout: 1500 });
    }).toPass({ intervals: [100, 200, 400] });
}
