import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion } from "./api-falsa";

/*
 * Manual de Iris (/manual y /campo/manual): índice con buscador, una página, una clave sin página
 * y la sección del técnico. Las páginas son estáticas (no llaman a la API). Corre en los 4
 * proyectos: escritorio y teléfono, claro y oscuro.
 */

/** Sin desplazamiento horizontal de la página (390 px y 1280 px). */
async function sinDesbordeHorizontal(page: Page) {
    const { ancho, visible } = await page.evaluate(() => ({ ancho: document.documentElement.scrollWidth, visible: document.documentElement.clientWidth }));
    expect(ancho).toBeLessThanOrEqual(visible);
}

/** El tema del proyecto llega a la página (clase `.dark` de next-themes). */
async function temaDelProyecto(page: Page, oscuro: boolean) {
    if (oscuro) await expect(page.locator("html")).toHaveClass(/\bdark\b/);
    else await expect(page.locator("html")).not.toHaveClass(/\bdark\b/);
}

test.beforeEach(async ({ context, baseURL }) => {
    await conSesion(context, baseURL ?? "", ADMIN.correo);
});

test("índice del manual con sus secciones", async ({ page }, info) => {
    await page.goto("/manual");
    await expect(page).toHaveTitle("Manual · Iris");
    await expect(page.getByRole("heading", { level: 1, name: "Manual de Iris" })).toBeVisible();
    for (const seccion of ["Soporte", "Órdenes, agenda y despacho", "App del técnico", "Administración"]) {
        await expect(page.getByRole("heading", { level: 2, name: seccion, exact: true })).toBeVisible();
    }
    await expect(page.getByRole("button", { name: "Atrás" })).toBeVisible();
    await temaDelProyecto(page, info.project.name.endsWith("oscuro"));
    await sinDesbordeHorizontal(page);

    await page.getByRole("link", { name: "Bandeja de tickets" }).click();
    await expect(page).toHaveURL(/\/manual\/tickets\/lista$/);
    await expect(page.getByRole("heading", { level: 1, name: "Bandeja de tickets" })).toBeVisible();
});

test("buscador: encuentra sin acentos, dice cuando no hay nada y vuelve al índice", async ({ page }) => {
    await page.goto("/manual");
    const buscar = page.getByRole("searchbox", { name: "Buscar en el manual" });
    await buscar.fill("firma del cliente");
    const resultados = page.getByRole("list", { name: "Resultados de la búsqueda" });
    await expect(resultados.getByRole("link", { name: "Firma del cliente y cierre", exact: true })).toBeVisible();
    await expect(page.getByRole("status").filter({ hasText: /resultado/ })).toHaveText(/\d+ resultados?/);

    await buscar.fill("ordenes sin senal");
    await expect(resultados.getByRole("link").first()).toBeVisible();

    await buscar.fill("palabra-que-no-existe");
    await expect(page.getByText("Nada coincide con tu búsqueda")).toBeVisible();

    await buscar.fill("");
    await expect(page.getByRole("heading", { level: 2, name: "Soporte", exact: true })).toBeVisible();

    // Un resultado de la app del técnico abre su versión sin señal.
    await buscar.fill("firma del cliente");
    await resultados.getByRole("link", { name: "Firma del cliente y cierre", exact: true }).click();
    await expect(page).toHaveURL(/\/campo\/manual\/cierre$/);
});

test("una página: partes, migas, enlaces internos y «Atrás»", async ({ page }) => {
    await page.goto("/manual");
    await page.goto("/manual/tickets/detalle");
    await expect(page).toHaveTitle("Ficha del ticket · Manual · Iris");
    const articulo = page.locator("article[data-manual='pagina']");
    await expect(articulo.getByRole("heading", { level: 1, name: "Ficha del ticket" })).toBeVisible();
    for (const parte of ["Para qué sirve", "Cómo se usa", "Qué significa cada estado", "Preguntas frecuentes", "Permisos necesarios"]) {
        await expect(articulo.getByRole("heading", { level: 2, name: parte })).toBeVisible();
    }
    await expect(articulo.getByRole("list").first()).toBeVisible();
    // Migas: en escritorio la ruta completa; en el teléfono, el enlace a la anterior.
    await expect(page.getByRole("navigation", { name: "Ruta de navegación" }).filter({ visible: true }).getByRole("link").first()).toBeVisible();
    await sinDesbordeHorizontal(page);

    await articulo.getByRole("link", { name: "Bandeja de tickets" }).first().click();
    await expect(page).toHaveURL(/\/manual\/tickets\/lista$/);
    await page.getByRole("button", { name: "Atrás" }).click();
    await expect(page).toHaveURL(/\/manual\/tickets\/detalle$/);
});

test("clave sin página: lo dice y lleva al índice", async ({ page }) => {
    const respuesta = await page.goto("/manual/tickets/pantalla-que-no-existe");
    expect(respuesta?.status()).toBe(404);
    await expect(page.getByText("Esta pantalla todavía no tiene manual")).toBeVisible();
    await page.getByRole("link", { name: "Ir al índice del manual" }).click();
    await expect(page).toHaveURL(/\/manual$/);
    await expect(page.getByRole("heading", { level: 1, name: "Manual de Iris" })).toBeVisible();
});

test("sin sesión, /manual pide entrar", async ({ browser, baseURL }) => {
    const contexto = await browser.newContext({ baseURL: baseURL ?? "" });
    const pagina = await contexto.newPage();
    await pagina.goto("/manual/tickets/lista");
    await expect(pagina).toHaveURL(/\/entrar\?siguiente=%2Fmanual%2Ftickets%2Flista$/);
    await contexto.close();
});

test("app del técnico: /manual/campo/… abre /campo/manual/… a pantalla completa con «Atrás»", async ({ page }, info) => {
    await page.goto("/manual/campo/hoy");
    await expect(page).toHaveURL(/\/campo\/manual\/hoy$/);
    await expect(page.getByRole("heading", { level: 1, name: "Hoy" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Atrás" })).toBeVisible();
    await temaDelProyecto(page, info.project.name.endsWith("oscuro"));
    await sinDesbordeHorizontal(page);

    await page.goto("/campo/manual");
    await expect(page.getByRole("heading", { level: 1, name: "Manual de la app del técnico" })).toBeVisible();
    await page.getByRole("list", { name: "Páginas de la app del técnico" }).getByRole("link", { name: "Trabajar sin señal" }).click();
    await expect(page).toHaveURL(/\/campo\/manual\/sin-senal$/);
    await expect(page.getByRole("heading", { level: 1, name: "Trabajar sin señal" })).toBeVisible();

    const sinPagina = await page.goto("/campo/manual/no-existe");
    expect(sinPagina?.status()).toBe(404);
    await expect(page.getByText("Esta pantalla todavía no tiene manual")).toBeVisible();
});
