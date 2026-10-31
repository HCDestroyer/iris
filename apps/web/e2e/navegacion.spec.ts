import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { abrirMenu, barraInferior, esTelefono, hojaMas, menu } from "./ayudas";

// Fase 1 de la interfaz: barra lateral agrupada y contraíble, migas, buscador global (Ctrl/⌘ + K),
// avisos emergentes tras una acción y, en el teléfono, barra inferior con «Más».

const migas = (page: Page) => page.getByRole("navigation", { name: "Ruta de navegación" }).filter({ visible: true });
const avisos = (page: Page) => page.getByRole("region", { name: /notificaci/ });

test.beforeEach(async ({ context, baseURL }) => {
    await conSesion(context, baseURL ?? "", ADMIN.correo);
});

test("menú agrupado; en escritorio se contrae a íconos (con globo) y lo recuerda; en el teléfono hay barra inferior", async ({ page }) => {
    await simularApi(page);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();
    if (esTelefono(page)) {
        // Sin barra lateral: la barra inferior y «Más» la reemplazan.
        await expect(page.getByRole("complementary", { name: "Barra lateral" })).toBeHidden();
        await expect(barraInferior(page)).toBeVisible();
        await expect(page.getByRole("button", { name: "Contraer menú" })).toBeHidden();
        return;
    }
    const nav = menu(page);
    for (const grupo of ["Operación", "Equipo", "Ventas", "Análisis"]) {
        await expect(nav.getByRole("list", { name: grupo })).toBeVisible();
    }
    await expect(nav.getByRole("list", { name: "Operación" }).getByRole("link")).toHaveText(["Inicio", "Tickets", "Órdenes", "Agenda", "Despacho"]);
    await expect(nav.getByRole("list", { name: "Equipo" }).getByRole("link")).toHaveText(["Técnicos", "Usuarios"]);
    await expect(nav.getByRole("link", { name: "Inicio" })).toHaveAttribute("aria-current", "page");
    await expect(barraInferior(page)).toBeHidden();

    const contraer = page.getByRole("button", { name: "Contraer menú" });
    await expect(contraer).toHaveAttribute("aria-expanded", "true");
    await contraer.click();
    const barra = page.getByRole("complementary", { name: "Barra lateral" });
    await expect(barra).toHaveAttribute("data-contraida", "true");
    // Solo íconos: el nombre sigue para lectores de pantalla y aparece en un globo.
    const tickets = menu(page).getByRole("link", { name: "Tickets" });
    await expect(tickets).toBeVisible();
    expect((await barra.boundingBox())?.width).toBeLessThan(80);
    await tickets.hover();
    await expect(page.getByRole("tooltip")).toHaveText("Tickets");

    // La preferencia se queda en este navegador.
    await page.reload();
    await expect(page.getByRole("complementary", { name: "Barra lateral" })).toHaveAttribute("data-contraida", "true");
    await page.getByRole("button", { name: "Expandir menú" }).click();
    await expect(page.getByRole("complementary", { name: "Barra lateral" })).not.toHaveAttribute("data-contraida", "true");
    await expect(menu(page).getByRole("link", { name: "Tickets" })).toContainText("Tickets");
});

test("migas en cada pantalla: sección, ficha con su número y regreso; en el teléfono solo «‹ anterior»", async ({ page }) => {
    await simularApi(page);
    await page.goto("/tickets");
    await expect(page.getByRole("heading", { name: "Tickets", exact: true })).toBeVisible();
    if (esTelefono(page)) {
        // Una sección no tiene «anterior»: en el teléfono no se ocupa espacio con migas.
        await expect(migas(page)).toHaveCount(0);
    } else {
        await expect(migas(page).locator('[aria-current="page"]')).toHaveText("Tickets");
    }
    await page.goto("/tickets/1");
    await expect(page.getByRole("heading", { name: "Sin internet desde la mañana" })).toBeVisible();
    // La ficha conoce su número: «Tickets › TK-000101».
    await expect(migas(page).locator('[aria-current="page"]')).toHaveText("TK-000101");
    const anterior = migas(page).getByRole("link", { name: "Tickets" });
    await expect(anterior).toHaveAttribute("href", "/tickets");
    if (esTelefono(page)) {
        await expect(migas(page).getByRole("link")).toHaveCount(1);
    }
    await anterior.click();
    await expect(page).toHaveURL(/\/tickets$/);
    await expect(page.getByRole("heading", { name: "Tickets", exact: true })).toBeVisible();

    // Varios niveles: Administración › Roles y permisos.
    await page.goto("/administracion/roles");
    await expect(migas(page).locator('[aria-current="page"]')).toHaveText("Roles y permisos");
    await migas(page).getByRole("link", { name: "Administración" }).click();
    await expect(page).toHaveURL(/\/administracion$/);
});

test("Ctrl + K: pantallas por permiso y un ticket por número; errores con «Reintentar»", async ({ page }) => {
    const api = await simularApi(page);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();

    await page.keyboard.press("Control+k");
    const buscador = page.getByRole("dialog", { name: "Buscar en Iris" });
    await expect(buscador).toBeVisible();
    const campo = buscador.getByRole("searchbox", { name: "Buscar en Iris" });
    await expect(campo).toBeFocused();
    await campo.fill("roles");
    await expect(buscador.getByRole("option", { name: /Roles y permisos/ })).toBeVisible();
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/administracion\/roles$/);
    await expect(buscador).toHaveCount(0);

    // Un ticket por su número; si la búsqueda falla, «Reintentar» la repite.
    api.responderProblema("/api/v1/tickets", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
    await page.getByRole("button", { name: "Buscador global" }).filter({ visible: true }).click();
    await buscador.getByRole("searchbox").fill("TK-000101");
    await expect(buscador.getByRole("alert")).toContainText("No se pudo buscar en tickets");
    await buscador.getByRole("button", { name: "Reintentar" }).click();
    const ticket = buscador.getByRole("option", { name: /TK-000101/ });
    await expect(ticket).toBeVisible();
    await ticket.click();
    await expect(page).toHaveURL(/\/tickets\/1$/);
    await expect(page.getByRole("heading", { name: "Sin internet desde la mañana" })).toBeVisible();
});

test("aviso emergente después de una acción", async ({ page }) => {
    await simularApi(page);
    await page.goto("/tickets/1");
    await page.getByRole("group", { name: "Acciones del ticket" }).getByRole("button", { name: "Responder" }).click();
    const dialogo = page.getByRole("dialog", { name: "Responder al cliente" });
    await dialogo.getByLabel("Respuesta").fill("Ya revisamos la línea.");
    await dialogo.getByRole("button", { name: "Enviar respuesta" }).click();
    await expect(dialogo).toHaveCount(0);
    await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Respuesta enviada en el ticket TK-000101" })).toBeVisible();
});

test("teléfono: barra inferior con lo principal y «Más» con el resto, la empresa y la cuenta", async ({ page }) => {
    await simularApi(page);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();
    if (!esTelefono(page)) {
        // En escritorio no hay barra inferior: todo está en la barra lateral y la barra superior.
        await expect(barraInferior(page)).toBeHidden();
        await expect(page.getByRole("button", { name: /^Tu cuenta/ })).toBeVisible();
        return;
    }
    const barra = barraInferior(page);
    await expect(barra.getByRole("link")).toHaveText(["Inicio", "Tickets", "Órdenes", "Agenda"]);
    await expect(barra.getByRole("link", { name: "Inicio" })).toHaveAttribute("aria-current", "page");
    await barra.getByRole("link", { name: "Tickets" }).click();
    await expect(page).toHaveURL(/\/tickets$/);
    await expect(barra.getByRole("link", { name: "Tickets" })).toHaveAttribute("aria-current", "page");
    // Sin hamburguesa: «Más» la reemplaza.
    await expect(page.getByRole("button", { name: "Abrir menú" })).toHaveCount(0);

    await abrirMenu(page);
    const mas = hojaMas(page);
    await expect(mas.getByRole("navigation", { name: "Principal" }).getByRole("link", { name: "Despacho" })).toBeVisible();
    await expect(mas.getByRole("navigation", { name: "Principal" }).getByRole("link", { name: "Tickets" })).toHaveCount(0);
    await expect(mas.getByRole("menu", { name: "Tu cuenta" })).toContainText(ADMIN.correo);
    await mas.getByRole("menuitem", { name: "Mi perfil" }).click();
    await expect(mas).toHaveCount(0);
    await expect(page).toHaveURL(/\/cuenta$/);

    // La barra respeta el área segura de abajo.
    const relleno = await barra.evaluate((n) => getComputedStyle(n).paddingBottom);
    expect(relleno).toMatch(/px$/);
});
