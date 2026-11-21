import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { esTelefono } from "./ayudas";

/*
 * Rediseño fase 1 (nov. 2026): marco del panel (menú isla, encabezado sin barra, scroll por dentro,
 * «?» del manual) y la app del técnico (isla Hoy · Por recibir · «+» · Agenda · Yo, tarjetas con
 * «Cómo llegar» y «Continuar», acción fija en la orden, columna de 480 px en escritorio). En los 4
 * proyectos. Con IRIS_CAPTURAS_REDISENO=<carpeta> guarda las capturas para el PM.
 */

const carpeta = process.env.IRIS_CAPTURAS_REDISENO;

async function capturar(page: Page, info: TestInfo, que: string) {
    if (!carpeta) return;
    const [tipo, tema] = info.project.name.split("-");
    const ancho = tipo === "telefono" ? 390 : 1280;
    await page.waitForTimeout(250);
    await page.screenshot({ path: path.join(carpeta, `${que}-${ancho}-${tema}.png`) });
}

/** Color que resuelve una variable CSS (para comparar con un estilo computado). */
const color = (page: Page, variable: string) =>
    page.evaluate((v) => {
        const muestra = document.createElement("div");
        muestra.style.background = `var(${v})`;
        document.body.append(muestra);
        const c = getComputedStyle(muestra).backgroundColor;
        muestra.remove();
        return c;
    }, variable);

test.describe("marco del panel", () => {
    test.beforeEach(async ({ context, baseURL }) => {
        await conSesion(context, baseURL ?? "", ADMIN.correo);
    });

    test("menú isla, encabezado sin barra, scroll por dentro, «?» del manual y ningún teal", async ({ page }, info) => {
        await simularApi(page, { despacho: true });
        await page.goto("/");
        await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();

        // El color principal es el índigo del tema (claro #4F46E5, oscuro #818CF8).
        const oscuro = info.project.name.endsWith("oscuro");
        expect(await color(page, "--iris-primario")).toBe(oscuro ? "rgb(129, 140, 248)" : "rgb(79, 70, 229)");
        expect(await color(page, "--hx-primary")).toBe(await color(page, "--iris-primario"));
        expect(await color(page, "--accent")).toBe(await color(page, "--iris-primario"));
        // El logo es el degradado índigo → cian.
        await expect(page.locator('a[aria-label="Iris, inicio"] stop').first()).toHaveAttribute("stop-color", "#4f46e5");

        // «?» junto al título: abre el manual de esta pantalla.
        await expect(page.getByRole("link", { name: "Manual de esta pantalla" })).toHaveAttribute("href", "/manual/inicio/tablero");

        // La página no se desplaza: el área de trabajo, sí (por dentro).
        const contenido = page.locator("#contenido");
        expect(await contenido.evaluate((e) => getComputedStyle(e).overflowY)).toBe("auto");
        expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight + 1)).toBe(true);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);

        // Encabezado sin barra: botones sueltos de 44 px a 16 px del borde de arriba.
        const buscar = page.getByRole("button", { name: "Buscador global" });
        const caja = await buscar.boundingBox();
        expect(caja?.y).toBe(16);
        expect(caja?.height).toBe(44);
        expect(await page.locator('[data-slot="encabezado-panel"]').evaluate((e) => getComputedStyle(e).backgroundColor)).toBe("rgba(0, 0, 0, 0)");

        if (esTelefono(page)) {
            // Isla de menú abajo y el botón redondo «Crear».
            const isla = page.getByRole("navigation", { name: "Accesos rápidos" });
            await expect(isla).toBeVisible();
            const islaCaja = await isla.boundingBox();
            expect(Math.round((islaCaja?.x ?? 0))).toBe(16);
            await expect(page.getByRole("button", { name: "Crear" })).toBeVisible();
        } else {
            await expect(page.getByRole("complementary", { name: "Barra lateral" })).toBeVisible();
            await expect(page.getByRole("link", { name: "Nuevo ticket" })).toHaveAttribute("href", "/tickets/nuevo");
            await expect(page.getByRole("link", { name: "Nueva orden" })).toHaveAttribute("href", "/ordenes/nueva");
            // La cuenta: nombre, rol y empresa; dentro, el tema, el manual y «Cerrar sesión» al final.
            await page.getByRole("button", { name: /^Tu cuenta/ }).click();
            const panel = page.getByRole("dialog", { name: /^Tu cuenta/ });
            await expect(panel.getByRole("menuitem").last()).toHaveText("Cerrar sesión");
            await expect(panel.getByRole("menuitem", { name: "Manual y atajos" })).toBeVisible();
            await expect(panel.getByRole("radiogroup", { name: "Tema" })).toBeVisible();
            await capturar(page, info, "marco-cuenta");
            await page.keyboard.press("Escape");
        }
        await capturar(page, info, "marco");

        await page.goto("/tickets");
        await expect(page.getByRole("heading", { name: "Tickets", exact: true })).toBeVisible();
        await expect(page.getByRole("link", { name: "Manual de esta pantalla" })).toHaveAttribute("href", "/manual/tickets/lista");
        await capturar(page, info, "tickets");
    });
});

test.describe("app del técnico", () => {
    test.beforeEach(async ({ context, baseURL }) => {
        await conSesionTecnico(context, baseURL ?? "");
    });

    test("Hoy: pestañas, tarjetas con «Cómo llegar» y «Continuar», isla Hoy · Por recibir · «+» · Agenda · Yo y el «?»", async ({ page }, info) => {
        await simularApiCampo(page);
        await page.goto("/campo");
        await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
        const isla = page.getByRole("navigation", { name: "Secciones de la app" });
        await expect(isla.getByRole("link")).toHaveCount(4);
        await expect(isla.getByRole("link", { name: "Por recibir" })).toBeVisible();
        await expect(isla.getByRole("button", { name: "Acciones rápidas" })).toBeVisible();
        await expect(page.getByRole("radiogroup", { name: "Día" })).toBeVisible();
        await expect(page.locator('[data-slot="encabezado-campo"]').getByRole("link", { name: "Manual de esta pantalla" })).toHaveAttribute("href", "/campo/manual/hoy");
        const siguiente = page.locator('[data-slot="siguiente-orden"]');
        await expect(siguiente.getByRole("link", { name: "Cómo llegar" })).toBeVisible();
        // En escritorio, una columna centrada de 480 px.
        const columna = await page.locator(".campo-app").first().boundingBox();
        if (!esTelefono(page)) {
            expect(columna?.width).toBe(480);
            expect(Math.round(columna?.x ?? 0)).toBe(400);
        }
        await capturar(page, info, "campo-hoy");

        // La orden: la acción principal fija abajo (en lugar de la isla).
        await page.getByRole("link", { name: /OT-000101/ }).click();
        const fija = page.locator('[data-slot="accion-fija-orden"]');
        await expect(fija).toBeInViewport();
        await expect(fija.getByRole("button", { name: "Empezar trabajo" })).toBeVisible();
        await expect(isla).toHaveCount(0);
        await expect(page.locator('[data-slot="encabezado-campo"]').getByRole("link", { name: "Manual de esta pantalla" })).toHaveAttribute("href", "/campo/manual/orden");
        await capturar(page, info, "campo-orden");
    });
});
