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

/*
 * Con IRIS_FUENTE_PRUEBA=<familia> (p. ej. «DejaVu Sans», la fuente de CI, más ancha) toda la página
 * usa esa fuente: así se revisa en local que nada se corta con la fuente de CI.
 */
const fuente = process.env.IRIS_FUENTE_PRUEBA;
test.beforeEach(async ({ page }) => {
    if (!fuente) return;
    await page.addInitScript((familia) => {
        const poner = () => {
            const estilo = document.createElement("style");
            estilo.textContent = `*, *::before, *::after { font-family: "${familia}" !important; }`;
            document.head.append(estilo);
        };
        if (document.head) poner();
        else document.addEventListener("DOMContentLoaded", poner);
    }, fuente);
});

async function capturar(page: Page, info: TestInfo, que: string) {
    if (!carpeta) return;
    const tema = info.project.name.split("-")[1];
    const ancho = page.viewportSize()?.width ?? 0;
    await page.waitForTimeout(250);
    await page.screenshot({ path: path.join(carpeta, `${que}-${ancho}-${tema}.png`) });
}

/**
 * Ningún botón ni enlace con aspecto de botón queda cortado (sin «…»): ni el control ni lo que lleva
 * dentro es más ancho que su caja (sin contar el texto solo para lectores, de 1 px). Devuelve los que
 * se cortan, con su nombre.
 */
async function botonesCortados(page: Page, dentro: string): Promise<string[]> {
    return page.locator(dentro).evaluateAll((raices) =>
        raices.flatMap((raiz) =>
            [...raiz.querySelectorAll<HTMLElement>('button, a[data-slot="boton"], [role="radio"], [role="tab"]')]
                .filter((b) => b.offsetParent !== null)
                .filter((b) => [b, ...b.querySelectorAll<HTMLElement>("*")].some((e) => e.getBoundingClientRect().width > 1 && e.scrollWidth > e.clientWidth + 1 && getComputedStyle(e).overflowX !== "visible"))
                .map((b) => (b.getAttribute("aria-label") ?? b.textContent ?? "").trim()),
        ),
    );
}

/** En el teléfono, también a 360 px (los teléfonos más angostos que se usan). */
const anchosTelefono = (page: Page) => (esTelefono(page) ? [390, 360] : [page.viewportSize()?.width ?? 1280]);

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

        // Escritorio: la página no se desplaza; el área de trabajo, sí (por dentro). En el teléfono la
        // pantalla completa es el contenedor.
        if (!esTelefono(page)) {
            const contenido = page.locator("#contenido");
            expect(await contenido.evaluate((e) => getComputedStyle(e).overflowY)).toBe("auto");
            expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight + 1)).toBe(true);
        }
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);

        // Encabezado sin barra: botones sueltos de 44 px a 16 px del borde de arriba (8 px en el teléfono).
        const buscar = page.getByRole("button", { name: "Buscador global" });
        const caja = await buscar.boundingBox();
        expect(caja?.y).toBe(esTelefono(page) ? 8 : 16);
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
            await expect(page.getByRole("link", { name: "Ticket", exact: true })).toHaveAttribute("href", "/tickets/nuevo");
            await expect(page.getByRole("link", { name: "Orden", exact: true })).toHaveAttribute("href", "/ordenes/nueva");
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
        for (const ancho of anchosTelefono(page)) {
            if (esTelefono(page)) await page.setViewportSize({ width: ancho, height: 844 });
            expect(await botonesCortados(page, "body")).toEqual([]);
            if (ancho === 360) await capturar(page, info, "marco");
        }
        if (esTelefono(page)) await page.setViewportSize({ width: 390, height: 844 });

        await page.goto("/tickets");
        await expect(page.getByRole("heading", { name: "Tickets", exact: true })).toBeVisible();
        await expect(page.getByRole("link", { name: "Manual de esta pantalla" })).toHaveAttribute("href", "/manual/tickets/lista");
        await capturar(page, info, "tickets");
        for (const ancho of anchosTelefono(page)) {
            if (esTelefono(page)) await page.setViewportSize({ width: ancho, height: 844 });
            expect(await botonesCortados(page, "#contenido")).toEqual([]);
            if (ancho === 360) await capturar(page, info, "tickets");
        }
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
        // Las acciones de la tarjeta se leen completas a 390 y a 360 px: «Cómo llegar» y «Llamar»
        // redondos (con nombre y globo) y la acción principal con su texto entero.
        for (const ancho of anchosTelefono(page)) {
            if (esTelefono(page)) await page.setViewportSize({ width: ancho, height: 844 });
            const acciones = page.locator('[data-slot="acciones-siguiente"]');
            await expect(acciones.getByRole("button", { name: "Empezar trabajo" })).toHaveText("Empezar trabajo");
            await expect(acciones.getByRole("link", { name: "Cómo llegar" })).toBeVisible();
            expect(await botonesCortados(page, ".campo-app")).toEqual([]);
            // La fecha del encabezado no se corta: en el teléfono siempre la corta («Sáb 21 nov»).
            const fecha = page.locator('[data-slot="encabezado-campo"] [data-slot="fecha-encabezado"]');
            expect(await fecha.evaluate((e) => e.scrollWidth <= e.clientWidth)).toBe(true);
            if (esTelefono(page)) await expect(fecha.locator("span").filter({ visible: true })).toHaveText(/^\p{Lu}\p{Ll}{2} \d{1,2} \p{Ll}{3}$/u);
            if (ancho === 360) await capturar(page, info, "campo-hoy");
        }
        if (esTelefono(page)) await page.setViewportSize({ width: 390, height: 844 });
        // React Aria muestra el globo al pasar el puntero cuando la persona ya usó el puntero.
        await page.getByRole("heading", { level: 1 }).click();
        await page.locator('[data-slot="acciones-siguiente"]').getByRole("link", { name: "Cómo llegar" }).hover();
        await expect(page.getByRole("tooltip")).toHaveText("Cómo llegar");
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
        for (const ancho of anchosTelefono(page)) {
            if (esTelefono(page)) await page.setViewportSize({ width: ancho, height: 844 });
            expect(await botonesCortados(page, ".campo-app")).toEqual([]);
            if (ancho === 360) await capturar(page, info, "campo-orden");
        }
    });
});
