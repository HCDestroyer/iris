import { expect, test, type Page } from "@playwright/test";
import { conSesion } from "./api-falsa";

// Prueba de humo de la fase 0 de HeroUI: el tema Horus llega a los componentes y los componentes
// base nuevos (pestañas, avisos emergentes, migas, fecha, autocompletar, globo, panel emergente y
// formulario en pantalla propia con su protección al salir) funcionan en escritorio y teléfono, en
// claro y oscuro. La página /demo-ui solo existe con IRIS_DEMO_UI=1 (playwright.config.ts).

async function sinDesborde(page: Page) {
    const ancho = page.viewportSize()?.width ?? 0;
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(ancho);
}

test.beforeEach(async ({ context, baseURL }) => {
    await conSesion(context, baseURL ?? "", "ana@iris.test");
});

test("el tema Horus pinta los componentes de HeroUI (teal único, claro y oscuro)", async ({ page }, info) => {
    await page.goto("/demo-ui");
    const crear = page.getByRole("button", { name: "Crear orden" });
    await expect(crear).toBeVisible();
    await expect(crear).toHaveClass(/button--primary/);
    const oscuro = info.project.name.includes("oscuro");
    // Teal 700 en claro, teal 400 (la «luz») en oscuro: el mismo token --hx-primary.
    await expect(crear).toHaveCSS("background-color", oscuro ? "rgb(45, 212, 191)" : "rgb(15, 118, 110)");
    await expect(page.locator("html")).toHaveClass(oscuro ? /\bdark\b/ : /\blight\b/);
    await sinDesborde(page);
});

test("migas, pestañas, globo, panel emergente, fecha y autocompletar", async ({ page }) => {
    await page.goto("/demo-ui");
    const migas = page.getByRole("navigation", { name: "Ruta de navegación" });
    await expect(migas.getByRole("link", { name: "Destino" })).toHaveAttribute("href", "/demo-ui/destino");
    await expect(migas.getByText("Nueva orden")).toHaveAttribute("aria-current", "page");

    const pestanas = page.getByRole("tablist", { name: "Secciones de la orden" });
    await expect(pestanas.getByRole("tab", { name: "Resumen" })).toHaveAttribute("aria-selected", "true");
    await pestanas.getByRole("tab", { name: "Conversación" }).click();
    await expect(page.getByRole("tabpanel")).toHaveText("Mensajes del ticket.");
    await page.keyboard.press("ArrowRight");
    await expect(pestanas.getByRole("tab", { name: "Historial" })).toBeFocused();

    // React Aria muestra el globo al pasar el puntero una vez que la persona usa el puntero (no en
    // la primera carga, antes de cualquier clic), o al enfocar con el teclado.
    await page.getByRole("heading", { name: "Nueva orden de demostración" }).click();
    await page.getByRole("button", { name: "Agenda" }).hover();
    await expect(page.getByRole("tooltip")).toHaveText("Agenda del técnico");
    await page.mouse.move(0, 0);
    await expect(page.getByRole("tooltip")).toHaveCount(0);

    // «Filtros» es un Boton sin `type` dentro del <form>: es type="button" y no envía el formulario.
    const filtros = page.getByRole("button", { name: "Filtros" });
    await expect(filtros).toHaveAttribute("type", "button");
    await filtros.click();
    const panel = page.getByRole("dialog", { name: "Filtros rápidos" });
    await expect(panel).toContainText("Solo órdenes de hoy.");
    // El foco entra al panel (diálogo no modal) y Escape lo cierra y vuelve al disparador.
    await expect(panel).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(panel).toHaveCount(0);
    await expect(filtros).toBeFocused();
    await expect(page.getByRole("region", { name: /notificaci/ }).getByText("Orden de demostración creada")).toHaveCount(0);

    // Fecha en formato de Guatemala (día/mes/año) y calendario.
    const fecha = page.getByRole("group", { name: "Fecha de la visita" });
    await expect(fecha.getByRole("spinbutton", { name: /día/ })).toHaveText("4");
    await expect(fecha.getByRole("spinbutton", { name: /mes/ })).toHaveText("11");
    await page.getByRole("button", { name: "Abrir calendario" }).click();
    await expect(page.getByRole("dialog").getByRole("grid")).toBeVisible();
    await page.keyboard.press("Escape");

    const tecnico = page.getByRole("combobox", { name: "Técnico" });
    await tecnico.fill("an");
    await page.getByRole("option", { name: /Ana Cux/ }).click();
    await expect(tecnico).toHaveValue("Ana Cux");
    await sinDesborde(page);
});

test("aviso emergente al guardar, con «Deshacer»", async ({ page }) => {
    await page.goto("/demo-ui");
    await page.getByRole("button", { name: "Crear orden" }).click();
    // Cada aviso es un `alertdialog` no modal dentro de la región de notificaciones (Alt+T).
    const region = page.getByRole("region", { name: /notificaci/ });
    const aviso = region.getByRole("alertdialog").filter({ hasText: "Orden de demostración creada" });
    await expect(aviso).toBeVisible();
    await aviso.getByRole("button", { name: "Deshacer" }).click();
    await expect(region.getByRole("alertdialog").filter({ hasText: "Se deshizo" })).toBeVisible();
});

test("formulario en pantalla propia: salir con cambios pregunta (enlace, migas y «Atrás»)", async ({ page }) => {
    await page.goto("/demo-ui/destino");
    await page.getByRole("link", { name: "Ir al formulario" }).click();
    await expect(page.getByRole("heading", { name: "Nueva orden de demostración" })).toBeVisible();

    // Sin cambios, «Atrás» sale a la primera.
    await page.goBack();
    await expect(page).toHaveURL(/\/demo-ui\/destino$/);
    await page.getByRole("link", { name: "Ir al formulario" }).click();

    const asunto = page.getByLabel("Asunto");
    await asunto.fill("Revisar la ONT");
    const confirmar = page.getByRole("alertdialog", { name: "¿Descartar los cambios?" });

    // Un enlace: pregunta y se puede seguir editando.
    await page.getByRole("link", { name: "Ir al destino" }).click();
    await expect(confirmar).toBeVisible();
    await expect(confirmar.getByRole("button", { name: "Seguir editando" })).toBeFocused();
    await confirmar.getByRole("button", { name: "Seguir editando" }).click();
    await expect(confirmar).toHaveCount(0);
    await expect(page).toHaveURL(/\/demo-ui$/);
    await expect(asunto).toHaveValue("Revisar la ONT");

    // «Cancelar» de la barra también pregunta.
    await page.getByRole("button", { name: "Cancelar" }).click();
    await expect(confirmar).toBeVisible();
    await confirmar.getByRole("button", { name: "Seguir editando" }).click();

    // «Atrás» del navegador: pregunta y, al descartar, sale.
    await page.goBack();
    await expect(confirmar).toBeVisible();
    await confirmar.getByRole("button", { name: "Descartar cambios" }).click();
    await expect(page).toHaveURL(/\/demo-ui\/destino$/);
});
