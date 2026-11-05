import { expect, test, type Locator, type Page } from "@playwright/test";
import { conSesion } from "./api-falsa";

// Pedido del PM: «existen alertas o sidebars que no tienen fondo, así que se notan como parte del
// primer plano». Todo lo flotante (diálogo, confirmación, panel lateral, panel emergente, menú,
// globo y aviso emergente) tiene un panel de color SÓLIDO, sin desenfoque, con borde y sombra; las
// capas modales llevan su telón oscurecido detrás; y los avisos de la página tienen su fondo
// tintado por tono, opaco. Se mide el estilo computado en escritorio y teléfono, claro y oscuro.

type Estilo = { fondo: string; desenfoque: string; sombra: string; borde: string; clases: string };

async function estilo(elemento: Locator): Promise<Estilo> {
    return elemento.evaluate((el) => {
        const e = getComputedStyle(el);
        return {
            fondo: e.backgroundColor,
            desenfoque: e.backdropFilter || e.getPropertyValue("-webkit-backdrop-filter") || "none",
            sombra: e.boxShadow,
            borde: `${e.borderTopWidth} ${e.borderTopStyle}`,
            clases: el.className,
        };
    });
}

/** Opacidad de un color computado: `rgb()` / `rgba()`, `color(srgb …)`, `oklab(…)` u `oklch(…)`. */
function alfa(color: string): number {
    if (color === "transparent") return 0;
    const rgba = /^rgba\([^,]+,[^,]+,[^,]+,\s*([\d.]+)\)$/.exec(color);
    if (rgba) return Number(rgba[1]);
    const barra = /\/\s*([\d.]+%?)\s*\)$/.exec(color);
    if (barra?.[1]) return barra[1].endsWith("%") ? Number(barra[1].slice(0, -1)) / 100 : Number(barra[1]);
    return 1;
}

async function esperarPanelSolido(panel: Locator, conBorde = true) {
    await expect(panel).toBeVisible();
    const e = await estilo(panel);
    expect(alfa(e.fondo), `fondo del panel: ${e.fondo}`).toBe(1);
    expect(e.desenfoque).toBe("none");
    // Borde (o el anillo de 1 px de la sombra de la capa flotante) y sombra: se separa de lo de atrás.
    expect(e.sombra, "sombra del panel").not.toBe("none");
    if (conBorde) expect(e.sombra.includes("0px 0px 0px 1px") || e.borde.startsWith("1px solid"), `borde: ${e.borde} / ${e.sombra}`).toBe(true);
    expect(e.clases).not.toContain("glass");
}

async function esperarTelon(page: Page, slot: string) {
    const telon = page.locator(`[data-slot="${slot}"]`);
    await expect(telon).toBeVisible();
    const e = await estilo(telon);
    expect(alfa(e.fondo), `telón ${slot}: ${e.fondo}`).toBeGreaterThanOrEqual(0.4);
}

test.beforeEach(async ({ context, baseURL }) => {
    await conSesion(context, baseURL ?? "", "ana@iris.test");
});

test("diálogo y confirmación: panel sólido sobre un telón oscurecido", async ({ page }) => {
    await page.goto("/demo-ui/capas");
    await page.getByRole("button", { name: "Abrir diálogo" }).click();
    const dialogo = page.getByRole("dialog", { name: "Diálogo de demostración" });
    await esperarPanelSolido(dialogo);
    await expect(dialogo).toHaveClass(/modal__dialog/);
    await esperarTelon(page, "modal-backdrop");
    await dialogo.getByRole("button", { name: "Listo" }).click();
    await expect(dialogo).toHaveCount(0);

    await page.getByRole("button", { name: "Quitar técnico" }).click();
    const confirmacion = page.getByRole("alertdialog", { name: "¿Quitar el técnico?" });
    await esperarPanelSolido(confirmacion);
    await expect(confirmacion).toHaveClass(/alert-dialog__dialog/);
    await esperarTelon(page, "alert-dialog-backdrop");
    await expect(confirmacion.getByRole("button", { name: "Cancelar" })).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(confirmacion).toHaveCount(0);
});

test("panel lateral: sólido y con telón aunque se pida `fondo=\"transparente\"`", async ({ page }) => {
    await page.goto("/demo-ui/capas");
    await page.getByRole("button", { name: "Abrir panel lateral" }).click();
    const hoja = page.getByRole("dialog", { name: "Panel lateral de demostración" });
    await esperarPanelSolido(hoja);
    await expect(hoja).toHaveClass(/drawer__dialog/);
    await esperarTelon(page, "drawer-backdrop");
    await page.keyboard.press("Escape");
    await expect(hoja).toHaveCount(0);
});

test("panel emergente, menú y globo: capa flotante sólida", async ({ page }) => {
    await page.goto("/demo-ui/capas");
    await page.getByRole("button", { name: "Filtros" }).click();
    await esperarPanelSolido(page.locator('[data-slot="panel-emergente"]'));
    await page.keyboard.press("Escape");

    await page.getByRole("button", { name: "Acciones" }).click();
    await expect(page.getByRole("menuitem", { name: "Editar" })).toBeVisible();
    await esperarPanelSolido(page.locator('[data-slot="dropdown-popover"]'));
    await page.keyboard.press("Escape");
    await expect(page.getByRole("menu")).toHaveCount(0);

    // React Aria muestra el globo al pasar el puntero una vez que la persona usó el puntero.
    await page.getByRole("heading", { name: "Capas flotantes" }).click();
    await page.getByRole("button", { name: "Agenda" }).hover();
    const globo = page.getByRole("tooltip");
    await expect(globo).toHaveText("Agenda del técnico");
    await esperarPanelSolido(globo);
});

test("aviso emergente: sólido (no la tarjeta translúcida del modo oscuro)", async ({ page }) => {
    await page.goto("/demo-ui");
    await page.getByRole("button", { name: "Crear orden" }).click();
    const aviso = page.locator('[data-slot="toast"]').filter({ hasText: "Orden de demostración creada" });
    await esperarPanelSolido(aviso, false);
});

test("avisos de la página: fondo tintado y opaco por tono, con icono, texto y borde", async ({ page }) => {
    await page.goto("/demo-ui/capas");
    const fondoPagina = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
    const fondos = new Set<string>();
    for (const tono of ["info", "success", "warning", "error"]) {
        const aviso = page.getByTestId(`aviso-${tono}`);
        await expect(aviso).toBeVisible();
        await expect(aviso).toHaveClass(/\balert\b/);
        await expect(aviso.locator("svg").first()).toBeVisible();
        const e = await estilo(aviso);
        expect(alfa(e.fondo), `fondo del aviso ${tono}: ${e.fondo}`).toBe(1);
        expect(e.fondo, `el aviso ${tono} se distingue del fondo de la página`).not.toBe(fondoPagina);
        expect(e.borde).toBe("1px solid");
        fondos.add(e.fondo);
    }
    // Cada tono tiene su propio tinte.
    expect(fondos.size).toBe(4);
});
