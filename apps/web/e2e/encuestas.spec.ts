import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { TOKENS } from "./api-encuestas";
import { ADMIN, CONSULTA, conSesion, simularApi } from "./api-falsa";
import { abrirMenu, avisoError, menu } from "./ayudas";

/*
 * Encuesta de satisfacción (Fase 5 · punto 2): página pública `/encuesta#<token>` sin sesión
 * (estados gracias, ya respondida, vencida, inválido, sin enlace y demasiados intentos), tarjeta
 * «Encuesta» en el ticket y la orden, lista `/encuestas` y el reporte «Satisfacción».
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const OCT = "desde=2026-10-01&hasta=2026-10-28";

async function sinDesbordar(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

test.describe("página pública", () => {
    test("responde con calificación y comentario: «Gracias», sin cookies del panel y sin el token en la URL", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        // Una sesión del panel abierta en el mismo navegador no debe viajar con la encuesta.
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/encuesta#${TOKENS.pendiente}`);
        await expect(page.getByRole("heading", { name: "¿Cómo te atendimos?" })).toBeVisible();
        await expect(page.getByTestId("datos-encuesta")).toHaveText("Ticket TK-000101 · 28 de octubre de 2026 · Fibra Express");
        expect(page.url()).not.toContain(TOKENS.pendiente);
        expect(page.url()).toMatch(/\/encuesta$/);

        await page.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(page.getByText("Elige una calificación")).toBeVisible();
        expect(api.encuestas.respuestas).toHaveLength(0);

        const opciones = page.getByRole("radiogroup", { name: "Califica la atención" }).or(page.getByRole("group", { name: "Califica la atención" }));
        await expect(opciones.getByRole("radio")).toHaveCount(5);
        await opciones.getByRole("radio", { name: /^Bien/ }).check();
        await page.getByLabel("¿Algo que quieras contarnos? (opcional)").fill("Resolvieron rápido.");
        await expect(page.getByText("19 de 1000 caracteres")).toBeVisible();
        await page.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(page.getByRole("heading", { name: "¡Gracias por tu respuesta!" })).toBeVisible();
        await expect(page.getByText("Calificaste la atención con 4 · Bien.")).toBeVisible();
        expect(api.encuestas.respuestas).toEqual([{ calificacion: 4, comentario: "Resolvieron rápido." }]);
        expect(api.encuestas.publicasConSesion).toEqual([]);
        // El token va en el cuerpo, nunca en la ruta ni en la query.
        const publicas = api.peticiones.filter((p) => p.ruta.startsWith("/api/v1/encuestas/publica"));
        expect(publicas.map((p) => `${p.metodo} ${p.ruta}`)).toEqual(["POST /api/v1/encuestas/publica/consultar", "POST /api/v1/encuestas/publica/responder"]);
        expect(publicas.every((p) => !p.query.toString() && (p.cuerpo as { token?: string }).token === TOKENS.pendiente)).toBe(true);
    });

    test("el token no vuelve a la URL: ni al cargar ni tras un router.refresh() de Next", async ({ page }) => {
        await simularApi(page);
        await page.goto(`/encuesta#${TOKENS.pendiente}`);
        await expect(page.getByRole("heading", { name: "¿Cómo te atendimos?" })).toBeVisible();
        expect(await page.evaluate(() => window.location.hash)).toBe("");
        await page.evaluate(() => (window as unknown as { next: { router: { refresh(): void } } }).next.router.refresh());
        await page.waitForLoadState("networkidle");
        await page.getByRole("radio", { name: /^Bien/ }).check();
        await page.waitForLoadState("networkidle");
        expect(await page.evaluate(() => window.location.hash)).toBe("");
        expect(page.url()).not.toContain(TOKENS.pendiente);
        await expect(page.getByRole("heading", { name: "¿Cómo te atendimos?" })).toBeVisible();
    });

    test("ya respondida, vencida, enlace inválido, sin enlace y demasiados intentos: con texto", async ({ page }) => {
        const api = await simularApi(page);
        await page.goto(`/encuesta#${TOKENS.respondida}`);
        await expect(page.getByRole("heading", { name: "Esta encuesta ya fue respondida" })).toBeVisible();
        await expect(page.getByText("La calificación registrada es 5 · Muy bien.")).toBeVisible();

        await page.goto(`/encuesta#${TOKENS.vencida}`);
        await expect(page.getByRole("heading", { name: "El enlace de la encuesta venció" })).toBeVisible();

        await page.goto(`/encuesta#${TOKENS.anulada}`);
        await expect(page.getByRole("heading", { name: "Esta encuesta ya no está disponible" })).toBeVisible();

        await page.goto(`/encuesta#${"N".repeat(43)}`);
        await expect(page.getByRole("heading", { name: "El enlace no es válido" })).toBeVisible();

        const antes = api.peticiones.length;
        await page.goto("/encuesta#corto");
        await expect(page.getByRole("heading", { name: "El enlace no es válido" })).toBeVisible();
        await page.goto("/encuesta");
        await expect(page.getByRole("heading", { name: "Abre la encuesta desde tu enlace" })).toBeVisible();
        expect(api.peticiones.slice(antes).filter((p) => p.ruta.startsWith("/api/v1/encuestas"))).toHaveLength(0);

        await page.goto(`/encuesta#${TOKENS.limite}`);
        await expect(page.getByRole("heading", { name: "Demasiados intentos" })).toBeVisible();
        await expect(page.getByText("Vuelve a abrir tu enlace en 2 min.")).toBeVisible();
    });

    test("si falla la lectura: error con «Reintentar»; al enviar, 409 y 410 llevan a su pantalla", async ({ page }) => {
        const api = await simularApi(page);
        api.responderProblema("/api/v1/encuestas/publica/consultar", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await page.goto(`/encuesta#${TOKENS.pendiente}`);
        await expect(avisoError(page)).toContainText("No se pudo abrir la encuesta");
        await avisoError(page).getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByRole("heading", { name: "¿Cómo te atendimos?" })).toBeVisible();

        // Un 500 al enviar: la respuesta sigue en pantalla y el botón dice «Reintentar».
        await page.getByRole("radio", { name: /^Muy mal/ }).check();
        api.responderProblema("/api/v1/encuestas/publica/responder", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await page.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(avisoError(page)).toContainText("No se pudo enviar tu respuesta");
        await expect(page.getByRole("radio", { name: /^Muy mal/ })).toBeChecked();

        api.responderProblema("/api/v1/encuestas/publica/responder", { type: "urn:iris:problema:encuesta_vencida", title: "Encuesta vencida", status: 410 }, 1);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByRole("heading", { name: "El enlace de la encuesta venció" })).toBeVisible();

        await page.goto(`/encuesta#${TOKENS.pendiente}`);
        await page.getByRole("radio", { name: /^Regular/ }).check();
        api.responderProblema("/api/v1/encuestas/publica/responder", { type: "urn:iris:problema:encuesta_respondida", title: "Encuesta ya respondida", status: 409 }, 1);
        await page.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(page.getByRole("heading", { name: "Esta encuesta ya fue respondida" })).toBeVisible();
    });

    test("capturas de la encuesta pública sin desbordes", async ({ page }, info) => {
        const nombre = (que: string) => path.join(carpeta, `encuesta-${que}-${info.project.name}.png`);
        await simularApi(page);
        await page.goto(`/encuesta#${TOKENS.pendiente}`);
        await page.getByRole("radio", { name: /^Muy bien/ }).check();
        await sinDesbordar(page);
        await page.screenshot({ path: nombre("01-formulario"), fullPage: true });
        await page.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(page.getByRole("heading", { name: "¡Gracias por tu respuesta!" })).toBeVisible();
        await page.screenshot({ path: nombre("02-gracias"), fullPage: true });
        await page.goto(`/encuesta#${TOKENS.vencida}`);
        await expect(page.getByRole("heading", { name: "El enlace de la encuesta venció" })).toBeVisible();
        await sinDesbordar(page);
        await page.screenshot({ path: nombre("03-vencida"), fullPage: true });
    });
});

test.describe("panel", () => {
    test("tarjeta «Encuesta» en el ticket: «Copiar enlace» lo pide, lo copia y lo muestra solo en el diálogo", async ({ page, context, baseURL, browserName }) => {
        const api = await simularApi(page);
        if (browserName === "chromium") await context.grantPermissions(["clipboard-read", "clipboard-write"]);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        const tarjeta = page.getByTestId("tarjeta-encuesta");
        await expect(tarjeta).toContainText("Pendiente");
        await expect(page.getByText(TOKENS.pendiente)).toHaveCount(0);
        expect(api.encuestas.enlacesPedidos).toEqual([]);
        await tarjeta.getByRole("button", { name: "Copiar enlace" }).click();
        const dialogo = page.getByRole("dialog", { name: "Enlace de la encuesta · TK-000101" });
        await expect(dialogo.getByText("Enlace copiado al portapapeles.")).toBeVisible();
        const enlace = `http://localhost/encuesta#${TOKENS.pendiente}`;
        await expect(dialogo.getByRole("textbox", { name: "Enlace de la encuesta" })).toHaveValue(enlace);
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(enlace);
        expect(api.encuestas.enlacesPedidos).toEqual([1]);
        const pedido = api.mutaciones.find((m) => m.ruta === "/api/v1/encuestas/1/enlace");
        expect(pedido?.csrf).toBeTruthy();
        await dialogo.getByRole("button", { name: "Cerrar" }).first().click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.locator(`input[value*="${TOKENS.pendiente}"]`)).toHaveCount(0);
        expect(page.url()).not.toContain(TOKENS.pendiente);
    });

    test("tarjeta en la orden: respondida con alerta y comentario; por correo o sin enlace, sin botón", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1");
        const tarjeta = page.getByTestId("tarjeta-encuesta");
        await expect(tarjeta).toContainText("Respondida");
        await expect(tarjeta).toContainText("2 · Mal");
        await expect(tarjeta).toContainText("Alerta");
        await expect(tarjeta.getByTestId("comentario-encuesta")).toHaveText("«El técnico llegó tarde y no avisó.»");
        await expect(tarjeta.getByRole("button", { name: "Copiar enlace" })).toHaveCount(0);

        await page.goto("/tickets/2");
        await expect(page.getByTestId("tarjeta-encuesta").getByTestId("enviada-por-correo")).toHaveText("Enviada por correo");
        await expect(page.getByTestId("tarjeta-encuesta").getByRole("button", { name: "Copiar enlace" })).toHaveCount(0);
        await page.goto("/tickets/4");
        await expect(page.getByTestId("tarjeta-encuesta")).toContainText("El enlace no se puede volver a mostrar.");
        await page.goto("/tickets/6");
        await expect(page.getByTestId("tarjeta-encuesta")).toContainText("Anulada");
        await page.goto("/tickets/3");
        await expect(page.getByTestId("tarjeta-encuesta")).toContainText("Sin encuesta.");
    });

    test("si la encuesta cambió al pedir el enlace (409 respondida), lo dice y relee la tarjeta", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        const tarjeta = page.getByTestId("tarjeta-encuesta");
        await expect(tarjeta).toContainText("Pendiente");
        const e = api.encuestas.encuestas.find((x) => x.id === 1);
        if (e) {
            e.estado = "respondida";
            e.calificacion = 5;
        }
        await tarjeta.getByRole("button", { name: "Copiar enlace" }).click();
        const dialogo = page.getByRole("dialog");
        await expect(dialogo).toContainText("El cliente ya respondió la encuesta.");
        await dialogo.getByRole("button", { name: "Cerrar" }).first().click();
        await expect(tarjeta).toContainText("5 · Muy bien");
        await expect(tarjeta.getByRole("button", { name: "Copiar enlace" })).toHaveCount(0);
    });

    test("sin encuestas.ver no hay tarjeta, ni entrada en el menú, ni lista", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/tickets/1");
        await expect(page.getByRole("heading", { name: /TK-000101/ }).or(page.getByText("TK-000101").first())).toBeVisible();
        await expect(page.getByTestId("tarjeta-encuesta")).toHaveCount(0);
        await abrirMenu(page);
        await expect(menu(page).getByRole("link", { name: /^Encuestas/ })).toHaveCount(0);
        await page.goto("/encuestas");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/encuestas")).toHaveLength(0);
    });

    test("lista /encuestas: menú, «Cargar más», filtros en la URL y la consulta", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await abrirMenu(page);
        await menu(page).getByRole("link", { name: /^Encuestas/ }).click();
        await expect(page).toHaveURL(/\/encuestas$/);
        const filas = page.locator("[data-encuesta]");
        await expect(filas).toHaveCount(25);
        await expect(filas.first()).toContainText("TK-000101");
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(filas).toHaveCount(37);
        await expect(page.locator('[data-encuesta="2"]')).toContainText("Alerta");

        await page.getByRole("checkbox", { name: "1 · Muy mal" }).click();
        await expect(page.getByRole("checkbox", { name: "1 · Muy mal" })).toBeChecked();
        await page.getByRole("checkbox", { name: "2 · Mal" }).click();
        await expect(page).toHaveURL(/calificacion=1%2C2/);
        await page.getByRole("combobox", { name: "Tipo" }).selectOption("orden");
        await expect(page).toHaveURL(/entidad=orden/);
        await expect.poll(() => api.peticiones.filter((p) => p.ruta === "/api/v1/encuestas").at(-1)?.query.get("calificacion")).toBe("1,2");
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/encuestas").at(-1)?.query.get("entidad")).toBe("orden");
        await expect(filas.first()).toContainText("OT-000001");
        for (const f of await filas.all()) await expect(f).toContainText("Alerta");

        await page.getByRole("combobox", { name: "Estado" }).selectOption("pendiente");
        await expect(page.getByText("Ninguna encuesta con estos filtros")).toBeVisible();
        await page.getByRole("button", { name: "Quitar filtros" }).click();
        await expect(page).toHaveURL(/\/encuestas$/);
        await expect(filas).toHaveCount(25);
    });

    test("lista: error con «Reintentar» y sin filas", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.responderProblema("/api/v1/encuestas", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/encuestas");
        await expect(avisoError(page)).toContainText("No se pudo cargar las encuestas");
        await expect(page.locator("[data-encuesta]")).toHaveCount(0);
        await avisoError(page).getByRole("button", { name: "Reintentar" }).click();
        await expect(page.locator("[data-encuesta]")).toHaveCount(25);
    });

    test("reporte «Satisfacción»: promedio, % respondidas, distribución y alertas con icono y texto", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=satisfaccion&${OCT}`);
        await expect(page.locator('[data-indicador="Promedio"]')).toContainText("4.1 de 5");
        await expect(page.locator('[data-indicador="Respondidas"]')).toContainText("60 de 100 enviadas");
        await expect(page.locator('[data-indicador="Alertas"]')).toContainText("8");
        await expect(page.getByTestId("alertas")).toContainText("8 encuestas calificaron con 2 o menos");
        await expect(page.getByTestId("alertas").getByRole("link", { name: "Encuestas" })).toHaveAttribute("href", "/encuestas?desde=2026-10-01&hasta=2026-10-28&calificacion=1%2C2");
        const grafico = page.getByRole("figure", { name: "Distribución de calificaciones" });
        await expect(grafico.locator("[data-alerta]")).toHaveCount(2);
        await expect(grafico.getByRole("listitem").filter({ hasText: "5 · Muy bien" })).toContainText("28");
        await expect(page.getByRole("region", { name: "Distribución" }).getByRole("row").filter({ hasText: "Mal (2)" })).toContainText("Alerta");
        await expect(page.getByRole("region", { name: "Por tipo" })).toContainText("Órdenes");

        await page.getByRole("combobox", { name: "Tipo" }).selectOption("orden");
        await expect(page).toHaveURL(/entidad=orden/);
        await expect.poll(() => api.peticiones.filter((p) => p.ruta === "/api/v1/reportes/satisfaccion").at(-1)?.query.get("entidad")).toBe("orden");
        await expect(page.locator('[data-indicador="Respondidas"]')).toContainText("30 de 50 enviadas");
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `reportes-06-satisfaccion-${info.project.name}.png`), fullPage: true });
    });

    test("reporte «Satisfacción» con rol lectura: lo ve, sin enlace a la lista", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto(`/reportes?reporte=satisfaccion&${OCT}`);
        await expect(page.getByTestId("alertas")).toContainText("Pide a soporte que les dé seguimiento.");
        await expect(page.getByTestId("alertas").getByRole("link")).toHaveCount(0);
    });

    test("capturas de la lista de encuestas", async ({ page, context, baseURL }, info) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/encuestas");
        await expect(page.locator("[data-encuesta]")).toHaveCount(25);
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `encuestas-lista-${info.project.name}.png`), fullPage: true });
        await page.goto("/ordenes/1");
        await expect(page.getByTestId("tarjeta-encuesta")).toContainText("Respondida");
        await page.getByTestId("tarjeta-encuesta").scrollIntoViewIfNeeded();
        await page.getByTestId("tarjeta-encuesta").screenshot({ path: path.join(carpeta, `encuestas-tarjeta-${info.project.name}.png`) });
    });
});
