import { expect, test } from "@playwright/test";
import { ADMIN, LECTOR, PAGINA_1, PAGINA_2, conSesion, simularApi } from "./api-falsa";
import { abrirMenu, avisoError, menu } from "./ayudas";

test.describe("menú por permisos", () => {
    test("sin usuarios.ver, tokens.ver, tickets.ver, ordenes.*, agenda.ver prospectos.ver ni reportes.ver no aparecen Usuarios, Administración, Tickets, Órdenes, Agenda, Despacho, Comercial ni Reportes", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", LECTOR.correo);
        await page.goto("/");
        await abrirMenu(page);
        const nav = menu(page);
        await expect(nav.getByRole("link", { name: /^Inicio/ })).toBeVisible();
        await expect(nav.getByRole("link", { name: /^Comercial/ })).toHaveCount(0);
        await expect(nav.getByRole("link", { name: /^Agenda/ })).toHaveCount(0);
        await expect(nav.getByRole("link", { name: /^Despacho/ })).toHaveCount(0);
        await expect(nav.getByRole("link", { name: /^Tickets/ })).toHaveCount(0);
        await expect(nav.getByRole("link", { name: /^Órdenes/ })).toHaveCount(0);
        await expect(nav.getByRole("link", { name: "Usuarios" })).toHaveCount(0);
        await expect(nav.getByRole("link", { name: /Administración/ })).toHaveCount(0);
        await expect(nav.getByRole("link", { name: /^Reportes/ })).toHaveCount(0);
    });

    test("entrar directo a una sección sin permiso muestra «Sin permiso»", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", LECTOR.correo);
        await page.goto("/usuarios");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });

    test("Comercial ya tiene pantalla (sin «Pronto»)", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await abrirMenu(page);
        await menu(page).getByRole("link", { name: /^Comercial/ }).click();
        await expect(page).toHaveURL(/\/comercial$/);
        await expect(page.getByRole("heading", { name: "Comercial" })).toBeVisible();
        await expect(page.getByRole("heading", { name: "Próximamente" })).toHaveCount(0);
    });

    test("si /yo falla no se dibuja el menú y «Reintentar» lo recupera", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.fallar("/api/v1/yo", 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await expect(avisoError(page)).toContainText("No se pudo cargar tu sesión");
        await expect(page.getByRole("navigation", { name: "Principal" })).toHaveCount(0);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();
    });
});

test.describe("usuarios", () => {
    test("lista con paginación por cursor", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        const visibles = page.locator("main").getByText(/@/).filter({ visible: true });
        await expect(visibles).toHaveCount(PAGINA_1.data?.length ?? 0);
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(visibles).toHaveCount((PAGINA_1.data?.length ?? 0) + (PAGINA_2.data?.length ?? 0));
        await expect(page.getByText("No hay más usuarios.")).toBeVisible();
        await expect(page.getByText("Inactivo").filter({ visible: true })).toBeVisible();
    });

    test("error al leer muestra «Reintentar» y no muestra datos", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.fallar("/api/v1/usuarios", 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await expect(avisoError(page)).toContainText("No se pudo cargar los usuarios");
        await expect(page.locator("main").getByText(LECTOR.correo)).toHaveCount(0);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText(LECTOR.correo).filter({ visible: true })).toBeVisible();
    });
});

test("el selector de tema cambia entre claro y oscuro", async ({ page }) => {
    await simularApi(page);
    await page.goto("/entrar");
    await page.getByRole("radio", { name: "Tema oscuro" }).click();
    await expect(page.locator("html")).toHaveClass(/dark/);
    await page.getByRole("radio", { name: "Tema claro" }).click();
    await expect(page.locator("html")).not.toHaveClass(/dark/);
});
