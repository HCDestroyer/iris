import { expect, test, type Page } from "@playwright/test";
import { ADMIN, ASIGNADOR, CONSULTA, conSesion, simularApi } from "./api-falsa";
import { accionFila, esTelefono, ofreceAccionFila } from "./ayudas";
import { elegirOpcion } from "./controles";

/*
 * Listas del rediseño (fase 2): menú `⋯` de cada fila, vista rápida, «Asignar…» desde la fila y
 * la bandeja que vuelve a leer sin mezclar páginas ni perder el foco. En escritorio y teléfono.
 */

const fila = (page: Page, atributo: string, valor: string) => page.locator(`[${atributo}="${valor}"]`).filter({ visible: true });
const vistaRapida = (page: Page, titulo: string) => page.getByRole("dialog", { name: `Vista rápida: ${titulo}` });

/** Cierra la vista rápida con Escape (el foco empieza adentro) y comprueba que se fue. */
async function cerrarConEscape(page: Page, titulo: string) {
    await page.keyboard.press("Escape");
    await expect(vistaRapida(page, titulo)).toHaveCount(0);
}

test.describe("vista rápida desde el menú ⋯", () => {
    test("ticket: datos, «Abrir ficha» y cierre con Escape", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(fila(page, "data-ticket", "TK-000101")).toBeVisible();

        await accionFila(page, "TK-000101", "Vista rápida");
        const vista = vistaRapida(page, "Sin internet desde la mañana");
        await expect(vista).toBeVisible();
        await expect(vista).toContainText("TK-000101");
        await expect(vista).toContainText("Crítica");
        await expect(vista).toContainText("Responsable");
        await expect(vista.getByRole("link", { name: "Abrir ficha" })).toHaveAttribute("href", "/tickets/1");
        // No es modal: la lista sigue a la vista y la fila queda marcada.
        await expect(fila(page, "data-ticket", "TK-000101")).toHaveAttribute("data-activa", "true");
        await cerrarConEscape(page, "Sin internet desde la mañana");

        await accionFila(page, "TK-000101", "Vista rápida");
        await vistaRapida(page, "Sin internet desde la mañana").getByRole("link", { name: "Abrir ficha" }).click();
        await expect(page).toHaveURL(/\/tickets\/1$/);
    });

    test("orden: datos, «Abrir ficha», «Abrir el ticket» y cierre con Escape", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes");
        await expect(fila(page, "data-orden", "OT-000002")).toBeVisible();

        await accionFila(page, "OT-000002", "Vista rápida");
        const vista = vistaRapida(page, "Km 18 carretera a El Salvador");
        await expect(vista).toBeVisible();
        await expect(vista).toContainText("OT-000002");
        await expect(vista).toContainText("Ferretería El Tornillo");
        await expect(vista).toContainText("Sin técnico");
        await expect(vista.getByRole("link", { name: "Abrir ficha" })).toHaveAttribute("href", "/ordenes/2");
        await expect(vista.getByRole("link", { name: "Abrir el ticket" })).toHaveAttribute("href", "/tickets/2");
        await cerrarConEscape(page, "Km 18 carretera a El Salvador");
        await ofreceAccionFila(page, "OT-000002", "Abrir el ticket", true);
    });

    test("orden sin tickets.ver: ni el menú ni la vista rápida ofrecen «Abrir el ticket»", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ASIGNADOR.correo);
        await page.goto("/ordenes");
        await expect(fila(page, "data-orden", "OT-000002")).toBeVisible();

        await ofreceAccionFila(page, "OT-000002", "Abrir el ticket", false);
        await accionFila(page, "OT-000002", "Vista rápida");
        const vista = vistaRapida(page, "Km 18 carretera a El Salvador");
        await expect(vista.getByRole("link", { name: "Abrir ficha" })).toBeVisible();
        await expect(vista.getByRole("link", { name: "Abrir el ticket" })).toHaveCount(0);
        await cerrarConEscape(page, "Km 18 carretera a El Salvador");
    });

    test("prospecto: datos, «Abrir ficha» y cierre con Escape", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial?vista=lista");
        const lista = page.getByRole("list", { name: "Prospectos" });
        await expect(lista).toBeVisible();
        // En el teléfono los filtros son otros (sin buscador): Diego es el único «Cotizado».
        if (esTelefono(page)) await elegirOpcion(page.getByLabel("Estado"), "cotizado");
        else await page.getByLabel("Buscar").fill("Diego");
        await expect(lista.getByRole("listitem")).toHaveCount(1);

        await accionFila(page, "PR-000004", "Vista rápida");
        const vista = vistaRapida(page, "Diego López");
        await expect(vista).toBeVisible();
        await expect(vista).toContainText("PR-000004");
        await expect(vista).toContainText("Vendedor");
        await expect(vista.getByRole("link", { name: "Abrir ficha" })).toHaveAttribute("href", "/comercial/4");
        await cerrarConEscape(page, "Diego López");
    });
});

test.describe("«Asignar…» desde la fila", () => {
    test("asigna un ticket con su acción, su versión y su Idempotency-Key", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(fila(page, "data-ticket", "TK-000101")).toBeVisible();

        await accionFila(page, "TK-000101", "Asignar…");
        const dialogo = page.getByRole("dialog", { name: "Asignar 1 ticket" });
        await expect(dialogo).toBeVisible();
        await elegirOpcion(dialogo.getByLabel("Responsable"), String(ADMIN.id));
        await dialogo.getByRole("button", { name: "Asignar" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(fila(page, "data-ticket", "TK-000101")).toContainText("Ana López");
        const envios = api.peticiones.filter((p) => p.metodo === "POST" && /^\/api\/v1\/tickets\/\d+\/asignar$/.test(p.ruta));
        expect(envios).toHaveLength(1);
        expect(envios[0]?.ruta).toBe("/api/v1/tickets/1/asignar");
        expect((envios[0]?.cuerpo as { usuarioId?: number }).usuarioId).toBe(ADMIN.id);
        expect(envios[0]?.idempotencia).toBeTruthy();
    });

    test("no se ofrece sin tickets.asignar", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/tickets");
        await expect(fila(page, "data-ticket", "TK-000101")).toBeVisible();
        await ofreceAccionFila(page, "TK-000101", "Asignar…", false);
        await ofreceAccionFila(page, "TK-000101", "Vista rápida", true);
    });

    test("no se ofrece en un ticket cerrado", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await page.getByRole("group", { name: "Vistas" }).getByRole("button", { name: /^Todos/ }).click();
        await page.getByLabel("Buscar").fill("ya atendida");
        await expect(fila(page, "data-ticket", "TK-000131")).toBeVisible();
        await expect(page.locator("[data-ticket]").filter({ visible: true })).toHaveCount(1);
        await ofreceAccionFila(page, "TK-000131", "Asignar…", false);
        // Ni casilla para elegirlo.
        await expect(page.getByLabel("Seleccionar TK-000131")).toHaveCount(0);
    });
});

test("bandeja: cambiar el filtro con «Cargar más» pendiente no mezcla páginas y el buscador conserva el foco", async ({ page, context, baseURL }) => {
    await simularApi(page);
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    // La página siguiente se queda esperando hasta que la prueba la suelte.
    let soltar: () => void = () => undefined;
    const suelta = new Promise<void>((resolver) => {
        soltar = resolver;
    });
    let pedida = false;
    await page.route(/\/api\/v1\/tickets\?.*cursor=/, async (route) => {
        pedida = true;
        await suelta;
        await route.fallback().catch(() => undefined);
    });

    await page.goto("/tickets");
    const filas = page.locator("[data-ticket]").filter({ visible: true });
    await expect(filas).toHaveCount(25);
    await page.getByRole("button", { name: "Cargar más" }).click();
    await expect(page.getByRole("button", { name: /Cargando/ })).toBeVisible();
    await expect.poll(() => pedida).toBe(true);

    const buscar = page.getByRole("searchbox", { name: "Buscar" });
    await buscar.fill("Router");
    await expect(filas).toHaveCount(1);
    await expect(fila(page, "data-ticket", "TK-000105")).toBeVisible();
    await expect(buscar).toBeFocused();

    // Llega la página vieja: no se agrega a la búsqueda nueva.
    soltar();
    await page.waitForTimeout(500);
    await expect(filas).toHaveCount(1);
    await expect(page.getByText("No hay más tickets.")).toBeVisible();
    await expect(buscar).toBeFocused();
    await expect(buscar).toHaveValue("Router");
});
