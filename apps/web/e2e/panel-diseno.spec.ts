import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, simularApi } from "./api-falsa";
import { abrirMenu, barraInferior, esTelefono, hojaMas, menu } from "./ayudas";
import { JORGE, LUCIA } from "./datos-agenda";
import { simularTeselas } from "./teselas";

/*
 * Nuevo diseño del panel (nov. 2026): una sola estructura (barra lateral con contadores y «Mis
 * vistas», barra superior con migas, buscador, avisos y «Crear»), Inicio con indicadores, la
 * bandeja a todo el ancho con pestañas y selección múltiple, la ficha con barra de acciones y
 * diálogos cortos, la agenda del mes con el resumen del día y el Mapa. En los 4 proyectos.
 */

const carpeta = process.env.IRIS_CAPTURAS_DISENO ?? path.join("test-results", "capturas-diseno");
const captura = (page: Page, info: TestInfo, que: string) => page.screenshot({ path: path.join(carpeta, `${que}-${info.project.name}.png`), fullPage: true });
const visible = (page: Page, selector: string) => page.locator(selector).filter({ visible: true });
const avisos = (page: Page) => page.getByRole("region", { name: /notificaci/ });
const hoyGt = () => new Intl.DateTimeFormat("en-CA", { timeZone: "America/Guatemala" }).format(new Date());

/** Sin desbordamiento horizontal de la página (390 px en teléfono). */
async function sinDesborde(page: Page) {
    const ancho = page.viewportSize()?.width ?? 0;
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(ancho);
}

test.beforeEach(async ({ context, baseURL }) => {
    await conSesion(context, baseURL ?? "", ADMIN.correo);
});

test.describe("estructura del panel", () => {
    test("barra superior con migas, buscador, avisos y «Crear»; la barra lateral con contadores y «Mis vistas»", async ({ page }) => {
        const api = await simularApi(page, { despacho: true });
        await page.goto("/");
        await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();
        await expect.poll(() => api.peticiones.some((p) => p.ruta === "/api/v1/tickets/resumen")).toBe(true);

        // Avisos: un punto discreto porque hay un SLA vencido; el panel lo explica y lleva a la bandeja.
        const botonAvisos = page.getByRole("button", { name: /^Avisos/ });
        await expect(botonAvisos).toHaveAccessibleName(/necesita|necesitan/);
        await expect(page.getByTestId("punto-avisos")).toBeVisible();
        await botonAvisos.click();
        const panel = page.getByRole("dialog", { name: "Avisos" });
        await expect(panel.getByRole("link", { name: /con el SLA vencido/ })).toBeVisible();
        await page.keyboard.press("Escape");

        // «Crear»: lo que se puede crear, cada uno en su pantalla.
        await page.getByRole("button", { name: "Crear" }).click();
        await expect(page.getByRole("menuitem")).toHaveText(["Nuevo ticket", "Nueva orden", "Nuevo prospecto"]);
        await page.getByRole("menuitem", { name: "Nueva orden" }).click();
        await expect(page).toHaveURL(/\/ordenes\/nueva$/);

        // «Mis vistas»: una vista guardada en la bandeja aparece en la barra lateral y abre la bandeja con sus filtros.
        await page.goto("/tickets");
        await page.getByLabel("Prioridad").selectOption("critica");
        await page.getByRole("group", { name: "Vistas" }).getByRole("button", { name: "Guardar vista" }).click();
        await page.getByRole("dialog").getByLabel("Nombre de la vista").fill("Críticas");
        await page.getByRole("dialog").getByRole("button", { name: "Guardar vista" }).click();
        await page.goto("/");
        await abrirMenu(page);
        const misVistas = page.getByRole("navigation", { name: "Mis vistas" }).filter({ visible: true });
        await misVistas.getByRole("link", { name: "Críticas" }).click();
        await expect(page).toHaveURL(/\/tickets\?vista=v-/);
        await expect(page.getByLabel("Prioridad")).toHaveValue("critica");
        await expect(page.locator("[data-ticket]").filter({ visible: true })).toHaveCount(1);
    });

    test("en el teléfono la barra lateral entera va en la hoja «Más», con sus contadores", async ({ page }) => {
        test.skip(!esTelefono(page), "Solo en el teléfono");
        await simularApi(page, { despacho: true });
        await page.goto("/");
        await expect(barraInferior(page).getByRole("link")).toHaveText(["Inicio", "Tickets", "Órdenes", "Agenda"]);
        await abrirMenu(page);
        const nav = hojaMas(page).getByRole("navigation", { name: "Principal" });
        await expect(nav.getByRole("link", { name: /^Mapa/ })).toBeVisible();
        await expect(nav.getByRole("link", { name: /^Tickets/ }).locator('[data-slot="contador"]')).toHaveText(/^\d+$/);
        await expect(menu(page).getByRole("list", { name: "Negocio" })).toBeVisible();
    });
});

test.describe("Inicio", () => {
    test("indicadores con tendencia, «Necesita tu atención», «Equipo hoy», tickets por día y SLA por categoría", async ({ page }, info) => {
        await simularApi(page, { despacho: true });
        await page.goto("/");
        const indicadores = page.getByRole("region", { name: "Indicadores" });
        await expect(indicadores.locator("[data-indicador]")).toHaveCount(4);
        await expect(indicadores.locator('[data-indicador="abiertos"]')).toHaveAttribute("href", "/tickets");
        await expect(indicadores.locator('[data-indicador="abiertos"]')).toContainText(/Tickets activos\s*\+\d+ hoy\s*\d+/);
        // La tendencia se dibuja desde 640 px (en el teléfono, dos indicadores por fila sin la línea).
        if (!esTelefono(page)) await expect(indicadores.locator('[data-indicador="abiertos"] svg[role="img"]')).toHaveAccessibleName(/Tickets nuevos por día en 7 días/);
        await expect(indicadores.locator('[data-indicador="ordenes"]')).toHaveAttribute("href", "/agenda?vista=dia");
        await expect(indicadores.locator('[data-indicador="ordenes"]')).toContainText(/sin técnico asignado/);

        // TK-000102 tiene el SLA vencido: aparece con «Abrir» (ya tiene responsable).
        const atencion = page.getByRole("region", { name: "Necesita tu atención" });
        await expect(atencion.locator('[data-pendiente="ticket-2"]')).toContainText("TK-000102 · Intermitencia por las tardes");
        await expect(atencion.locator('[data-pendiente="ticket-2"]').getByRole("link", { name: /^Abrir/ })).toHaveAttribute("href", "/tickets/2");
        await expect(atencion.locator('[data-pendiente="sin-tecnico"]').getByRole("link", { name: /^Programar/ })).toHaveAttribute("href", "/agenda?vista=dia");

        // Equipo hoy: avance y estado de cada técnico (Rosa está de vacaciones).
        const equipo = page.getByRole("region", { name: "Equipo hoy" });
        await expect(equipo.locator(`[data-tecnico="${LUCIA}"]`)).toBeVisible();
        await expect(equipo.locator('[data-estado-tecnico="ausente"]')).toContainText("Rosa Ajú");
        await expect(equipo.getByRole("progressbar").first()).toHaveAttribute("aria-valuemin", "0");

        await expect(page.getByRole("region", { name: "Tickets por día" }).getByTestId("barras-tickets")).toHaveAccessibleName(/Últimos 7 días/);
        await expect(page.getByRole("region", { name: "SLA por categoría" })).toBeVisible();
        await sinDesborde(page);
        await captura(page, info, "01-inicio");
    });

    test("si el resumen de la bandeja no se puede leer, los indicadores dicen «Reintentar» y no muestran números", async ({ page }) => {
        const api = await simularApi(page, { despacho: true });
        api.responderProblema("/api/v1/tickets/resumen", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await page.goto("/");
        const indicador = page.locator('[data-indicador="abiertos"]');
        await expect(indicador).toContainText("No se pudo leer.");
        await indicador.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.locator('[data-indicador="abiertos"]')).toContainText(/\d+ tuyos/);
    });

    test("rol de solo consulta: ve los indicadores, sin «Crear» ni casillas para asignar en lote", async ({ page, context, baseURL }) => {
        await context.clearCookies();
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await simularApi(page);
        await page.goto("/");
        await expect(page.locator('[data-indicador="abiertos"]')).toBeVisible();
        await expect(page.getByRole("button", { name: "Crear" })).toHaveCount(0);
        await page.goto("/tickets");
        await expect(page.locator("[data-ticket]").filter({ visible: true }).first()).toBeVisible();
        await expect(page.getByRole("checkbox", { name: /^Seleccionar/ })).toHaveCount(0);
    });
});

test.describe("Tickets", () => {
    test("pestañas con contador, filtros como chips y selección múltiple con «Asignar…»", async ({ page }, info) => {
        const api = await simularApi(page);
        await page.goto("/tickets");
        const vistas = page.getByRole("group", { name: "Vistas" });
        await expect(vistas.getByRole("button", { name: /^Activos/ })).toHaveAttribute("aria-pressed", "true");
        await expect(vistas.getByRole("button", { name: /^Sin asignar/ })).toContainText(/\d+/);
        await expect(vistas.getByRole("button", { name: /^En espera/ })).toContainText("1");
        await expect(page.locator('[data-chip="Prioridad"]')).toBeVisible();
        await sinDesborde(page);
        await captura(page, info, "02-tickets-lista");

        await vistas.getByRole("button", { name: /^En espera/ }).click();
        await expect(page.locator("[data-ticket]").filter({ visible: true })).toHaveCount(1);
        await vistas.getByRole("button", { name: /^Activos/ }).click();

        // Selección: dos tickets y «Asignar…» a una persona (una acción por ticket, con su versión).
        await visible(page, '[data-ticket="TK-000101"]').getByLabel("Seleccionar TK-000101").check();
        await visible(page, '[data-ticket="TK-000105"]').getByLabel("Seleccionar TK-000105").check();
        const barra = page.getByRole("toolbar", { name: "Acciones con la selección" });
        await expect(barra).toContainText("2 tickets seleccionados");
        await captura(page, info, "03-tickets-seleccion");
        await barra.getByRole("button", { name: "Asignar…" }).click();
        const dialogo = page.getByRole("dialog", { name: "Asignar 2 tickets" });
        await dialogo.getByLabel("Responsable").selectOption(String(ADMIN.id));
        await dialogo.getByRole("button", { name: "Asignar" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "2 tickets asignados a Ana López" })).toBeVisible();
        await expect(barra).toHaveCount(0);
        const envios = api.peticiones.filter((p) => p.metodo === "POST" && /^\/api\/v1\/tickets\/(1|5)\/asignar$/.test(p.ruta));
        expect(envios).toHaveLength(2);
        expect(envios.every((e) => (e.cuerpo as { usuarioId?: number }).usuarioId === ADMIN.id && e.idempotencia)).toBe(true);
        await expect(visible(page, '[data-ticket="TK-000101"]')).toContainText("Ana López");
    });

    test("ficha: barra de acciones (Asignar, Escalar, Crear orden, Resolver y «⋯»), el resumen a la derecha y el diálogo Asignar", async ({ page }, info) => {
        await simularApi(page);
        // Desde Inicio, «Asignar» abre la ficha con el diálogo ya abierto.
        await page.goto("/tickets/1?accion=asignar");
        const dialogo = page.getByRole("dialog", { name: "Asignar ticket" });
        await expect(dialogo).toBeVisible();
        await expect(dialogo).toHaveAccessibleDescription("TK-000101 · Sin internet desde la mañana");
        await captura(page, info, "05-ticket-asignar");
        await dialogo.getByRole("button", { name: "Cancelar" }).click();
        await expect(dialogo).toHaveCount(0);

        const barra = page.getByRole("toolbar", { name: "Acciones del ticket" });
        await expect(barra.getByRole("button", { name: "Asignar" })).toBeVisible();
        await expect(barra.getByRole("button", { name: "Escalar" })).toBeVisible();
        await expect(barra.getByRole("link", { name: /Crear orden/ })).toHaveAttribute("href", "/ordenes/nueva?ticket=1");
        await expect(barra.getByRole("button", { name: "Resolver" })).toBeVisible();
        await barra.getByRole("button", { name: "Más acciones" }).click();
        await expect(page.getByRole("menuitem", { name: "Fusionar" })).toBeVisible();
        await expect(page.getByRole("menuitem", { name: "Cerrar" })).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(page.getByRole("complementary", { name: "Datos del ticket" }).getByRole("region", { name: "Detalle" })).toContainText("Responsable");
        await expect(page.getByRole("tablist", { name: "Secciones del ticket" }).getByRole("tab")).toHaveText(["Conversación", "Órdenes", "Historial"]);
        await sinDesborde(page);
        await captura(page, info, "04-ticket-ficha");

        // Resolver: diálogo corto de un campo.
        await barra.getByRole("button", { name: "Resolver" }).click();
        const resolver = page.getByRole("dialog", { name: "Resolver ticket" });
        await resolver.getByLabel("Solución").fill("Se reinició la ONU y volvió el servicio.");
        await resolver.getByRole("button", { name: "Resolver" }).click();
        await expect(resolver).toHaveCount(0);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Ticket TK-000101 resuelto" })).toBeVisible();
    });

    test("el redactor pregunta antes de salir con un mensaje sin enviar", async ({ page }) => {
        await simularApi(page);
        await page.goto("/tickets/1");
        const redactor = page.getByRole("form", { name: "Redactar" });
        await redactor.getByRole("radio", { name: "Nota interna" }).click();
        await redactor.getByLabel("Nota interna").fill("Revisar la caja NAP.");
        await page.getByRole("navigation", { name: "Ruta de navegación" }).filter({ visible: true }).getByRole("link", { name: "Tickets" }).click();
        const pregunta = page.getByRole("alertdialog", { name: "¿Descartar el mensaje?" });
        await expect(pregunta).toBeVisible();
        await pregunta.getByRole("button", { name: "Seguir escribiendo" }).click();
        await expect(redactor.getByLabel("Nota interna")).toHaveValue("Revisar la caja NAP.");
        await redactor.getByRole("button", { name: "Guardar nota" }).click();
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Nota interna agregada al ticket TK-000101" })).toBeVisible();
    });
});

test.describe("Agenda", () => {
    test("pestañas Día por técnico · Semana · Mes; el mes marca hoy y el resumen del día lleva a «Abrir el día»", async ({ page }, info) => {
        await simularApi(page, { despacho: true });
        await page.goto("/agenda");
        const hoy = hoyGt();
        await expect(page.getByRole("radiogroup", { name: "Vista" }).getByRole("radio")).toHaveText(["Día por técnico", "Semana", "Mes"]);
        await expect(page.getByRole("radio", { name: "Mes" })).toBeChecked();
        // Hoy va marcado y elegido: su resumen está al lado (en el teléfono, arriba de la lista).
        const resumen = visible(page, '[data-testid="resumen-dia"]');
        await expect(resumen.getByRole("button", { name: "Abrir el día" })).toBeVisible();
        await expect(resumen.getByRole("button", { name: "Programar" })).toBeVisible();
        if (!esTelefono(page)) {
            await expect(page.getByTestId("cuadricula-mes").locator('[aria-current="date"]')).toHaveText(String(Number(hoy.slice(8))));
            const semanas = await page.getByTestId("cuadricula-mes").locator('[role="row"]').count();
            expect(semanas - 1).toBeGreaterThanOrEqual(5);
        }
        await sinDesborde(page);
        await captura(page, info, "06-agenda-mes");

        await resumen.getByRole("button", { name: "Programar" }).click();
        await expect(page).toHaveURL(/vista=dia/);
        await expect(page.getByRole("radio", { name: "Día por técnico" })).toBeChecked();
        await expect(page.getByTestId("sin-programar")).toBeVisible();
    });
});

test.describe("Mapa", () => {
    test("lista Técnicos · Órdenes · Red, leyenda y tarjeta del técnico con «Asignar orden»", async ({ page }, info) => {
        const api = await simularApi(page, { despacho: true });
        await simularTeselas(page);
        await page.goto("/mapa");
        await expect(page.getByRole("heading", { name: "Mapa", level: 1 })).toBeVisible();
        await expect(page.getByTestId("mapa")).toHaveAttribute("data-listo", "si", { timeout: 20_000 });
        const lista = page.getByRole("region", { name: "Lista del mapa" });
        await expect(lista.getByRole("radio", { name: /^Técnicos · 3$/ })).toBeChecked();
        await expect(page.getByRole("list", { name: "Leyenda" }).getByRole("listitem")).toHaveText(["En sitio", "En camino", "Sin señal", "Orden sin asignar"]);
        await expect(page.locator(`[data-marcador-tecnico="${LUCIA}"]`)).toBeAttached();

        // Tocar un técnico (en la lista o en el mapa) abre su tarjeta: orden actual, siguiente y «Asignar orden».
        await lista.locator(`[data-tecnico-mapa="${JORGE}"]`).click();
        const tarjeta = page.getByTestId("tarjeta-tecnico");
        await expect(tarjeta).toContainText("Jorge Méndez");
        await expect(tarjeta).toContainText("Siguiente:");
        await sinDesborde(page);
        await captura(page, info, "07-mapa");

        await tarjeta.getByRole("button", { name: "Asignar orden" }).click();
        const dialogo = page.getByRole("dialog", { name: "Asignar a Jorge Méndez" });
        await expect(dialogo.getByRole("button", { name: "Asignar" })).toBeDisabled();
        const opcion = dialogo.getByLabel("Orden").locator("option").nth(1);
        const valor = await opcion.getAttribute("value");
        await dialogo.getByLabel("Orden").selectOption(valor ?? "");
        await dialogo.getByRole("button", { name: "Asignar" }).click();
        await expect(dialogo).toHaveCount(0);
        const envio = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === `/api/v1/ordenes/${valor}/asignar`);
        expect((envio?.cuerpo as { tecnicoId?: number }).tecnicoId).toBe(JORGE);

        // La pestaña Red dice que todavía no hay datos de red (no se inventan).
        await lista.getByRole("radio", { name: "Red" }).click();
        await expect(lista).toContainText("Sin datos de red en el mapa");
    });
});
