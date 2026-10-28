import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { conSesion } from "./api-falsa";
import { conSesionPortal, simularApiPortal, TICKET_LIMITE, TICKET_RED, TICKET_VALIDO, TICKET_VENCIDO, type ApiPortal } from "./api-portal";
import { avisoError } from "./ayudas";
import { CSRF_PORTAL } from "./datos-portal";

// Capturas para revisión visual: IRIS_CAPTURAS=<carpeta> (por defecto test-results/capturas).
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

const caso = (page: Page, numero: string) => page.locator(`[data-ticket="${numero}"]`);

/** Ningún dato del ticket de acceso salió en una URL ni en otra petición que el canje. */
function ticketSoloEnElCanje(api: ApiPortal, ticket: string) {
    for (const p of api.peticiones) {
        expect(p.url).not.toContain(ticket);
        if (!(p.metodo === "POST" && p.ruta === "/api/v1/portal/sesion")) {
            expect(JSON.stringify(p.cuerpo ?? null)).not.toContain(ticket);
        }
    }
}

test.describe("entrada al portal", () => {
    test("canjea el ticket del fragmento, lo borra de la URL y abre mis casos", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto(`/soporte/entrar#${TICKET_VALIDO}`);
        await expect(page.getByRole("heading", { name: "Hola, María" })).toBeVisible();
        expect(new URL(page.url()).pathname).toBe("/soporte");
        expect(page.url()).not.toContain("#");
        expect(await page.evaluate(() => window.location.hash)).toBe("");
        const canje = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/portal/sesion");
        expect(canje?.cuerpo).toEqual({ ticket: TICKET_VALIDO });
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/portal/sesion" && p.metodo === "POST")).toHaveLength(1);
        ticketSoloEnElCanje(api, TICKET_VALIDO);
        // Ni el panel: el portal no pide /yo ni manda X-Iris-Empresa.
        expect(api.peticiones.some((p) => !p.ruta.startsWith("/api/v1/portal/"))).toBe(false);
        expect(api.peticiones.every((p) => p.empresa === undefined)).toBe(true);
        // Volver atrás no recupera el fragmento.
        await page.goBack();
        expect(page.url()).not.toContain(TICKET_VALIDO);
    });

    test("enlace vencido o usado", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto(`/soporte/entrar#${TICKET_VENCIDO}`);
        await expect(page.getByRole("heading", { name: "El enlace venció o ya se usó" })).toBeVisible();
        await expect(page.getByText(/pide uno nuevo/)).toBeVisible();
        expect(page.url()).not.toContain("#");
        ticketSoloEnElCanje(api, TICKET_VENCIDO);
    });

    test("enlace con forma inválida no se envía", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte/entrar#corto");
        await expect(page.getByRole("heading", { name: "El enlace venció o ya se usó" })).toBeVisible();
        expect(page.url()).not.toContain("#");
        expect(api.peticiones).toHaveLength(0);
    });

    test("429 del tope global: espera N min", async ({ page }) => {
        await simularApiPortal(page);
        await page.goto(`/soporte/entrar#${TICKET_LIMITE}`);
        await expect(page.getByText("Demasiados intentos")).toBeVisible();
        await expect(page.getByText(/Espera 2 min/)).toBeVisible();
        expect(page.url()).not.toContain("#");
    });

    test("error de red con «Reintentar» (el ticket sigue en memoria, no en la URL)", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto(`/soporte/entrar#${TICKET_RED}`);
        await expect(avisoError(page)).toContainText("No pudimos abrir tu soporte");
        expect(page.url()).not.toContain("#");
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByRole("heading", { name: "Hola, María" })).toBeVisible();
        ticketSoloEnElCanje(api, TICKET_RED);
    });

    test("sin enlace: «Entra desde el portal de tu proveedor»", async ({ page }) => {
        await simularApiPortal(page);
        await page.goto("/soporte/entrar");
        await expect(page.getByRole("heading", { name: "Entra desde el portal de tu proveedor" })).toBeVisible();
    });

    test("sin sesión del portal, /soporte manda a la entrada (aunque haya sesión del panel)", async ({ page, context, baseURL }) => {
        await simularApiPortal(page);
        await conSesion(context, baseURL ?? "", "ana@iris.test");
        await page.goto("/soporte/501");
        await expect(page).toHaveURL(/\/soporte\/entrar$/);
        await expect(page.getByRole("heading", { name: "Entra desde el portal de tu proveedor" })).toBeVisible();
        // Y el portal nunca exige la sesión del panel.
        await page.goto("/soporte/nuevo");
        await expect(page).toHaveURL(/\/soporte\/entrar$/);
    });
});

test.describe("con sesión del portal", () => {
    test.beforeEach(async ({ context, baseURL }) => {
        await conSesionPortal(context, baseURL ?? "");
    });

    test("lista: estado en lenguaje del cliente, falla general, «Cargar más» y filtro", async ({ page }) => {
        await simularApiPortal(page);
        await page.goto("/soporte");
        await expect(page.getByTestId("falla-general")).toContainText("Hay una falla general en tu zona");
        const abierto = caso(page, "TK-000501");
        await expect(abierto).toContainText("En atención");
        await expect(abierto).toContainText("Falla general");
        await expect(caso(page, "TK-000502")).toContainText("Resuelto");
        await expect(caso(page, "TK-000502")).toContainText("Confírmanos si quedó resuelto");
        await expect(caso(page, "TK-000418")).toContainText("En espera");
        await expect(caso(page, "TK-000419")).toContainText("Recibido");
        await expect(caso(page, "TK-000400")).toHaveCount(0);
        await expect(caso(page, "TK-000503")).toHaveCount(0);
        await expect(page.getByRole("list", { name: "Mis casos" }).getByRole("listitem")).toHaveCount(20);
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(page.getByRole("list", { name: "Mis casos" }).getByRole("listitem")).toHaveCount(22);
        await expect(caso(page, "TK-000400")).toBeVisible();
        await expect(page.getByRole("button", { name: "Cargar más" })).toHaveCount(0);

        await page.getByRole("radio", { name: "Todos" }).click();
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(caso(page, "TK-000503")).toContainText("Cerrado");
        await expect(page.getByRole("button", { name: "Salir" })).toBeVisible();
    });

    test("lista que no se pudo leer: error con «Reintentar» y sin datos", async ({ page }) => {
        const api = await simularApiPortal(page);
        api.responderProblema("GET", "/api/v1/portal/tickets", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 });
        await page.goto("/soporte");
        await expect(avisoError(page)).toContainText("No pudimos cargar tus casos");
        await expect(page.getByTestId("falla-general")).toHaveCount(0);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(caso(page, "TK-000501")).toBeVisible();
    });

    test("nuevo caso: validación, categorías, Idempotency-Key, CSRF propio y abre el detalle", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte");
        await page.getByRole("link", { name: "Reportar un problema" }).first().click();
        await expect(page.getByRole("heading", { name: "Reportar un problema" })).toBeVisible();
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByText("Escribe un asunto")).toBeVisible();
        await expect(page.getByText("Cuéntanos qué pasa", { exact: true })).toBeVisible();
        await page.getByLabel("Asunto").fill("ab");
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByText("El asunto necesita al menos 3 caracteres")).toBeVisible();

        await page.getByLabel("Asunto").fill("Internet muy lento de noche");
        await page.getByLabel("Tipo de problema (opcional)").selectOption({ label: "Internet lento" });
        await page.getByLabel("¿Qué está pasando?").fill("Después de las 8 p. m. no cargan los videos.");
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByRole("heading", { name: "Internet muy lento de noche" })).toBeVisible();
        await expect(page.getByTestId("cabecera-caso")).toContainText("Recibido");
        const alta = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/portal/tickets");
        expect(alta?.cuerpo).toEqual({ asunto: "Internet muy lento de noche", categoriaId: 2, descripcion: "Después de las 8 p. m. no cargan los videos." });
        expect(alta?.idempotencia).toBeTruthy();
        expect(alta?.csrf).toBe(CSRF_PORTAL);
        expect(alta?.empresa).toBeUndefined();
    });

    test("nuevo caso: 429 demasiados_tickets muestra el detail y conserva lo escrito", async ({ page }) => {
        const api = await simularApiPortal(page);
        const detalle = "Tienes 10 casos abiertos; espera a que resolvamos alguno antes de abrir otro.";
        api.responderProblema("POST", "/api/v1/portal/tickets", { type: "urn:iris:problema:demasiados_tickets", title: "Demasiados tickets", status: 429, detail: detalle });
        await page.goto("/soporte/nuevo");
        await page.getByLabel("Asunto").fill("Otro problema");
        await page.getByLabel("¿Qué está pasando?").fill("Detalle");
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByText("Ya tienes varios casos en curso")).toBeVisible();
        await expect(page.getByText(detalle)).toBeVisible();
        await expect(page.getByLabel("Asunto")).toHaveValue("Otro problema");
        // Reintentar usa otra clave (la API guardó el 429 con la primera).
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByRole("heading", { name: "Otro problema" })).toBeVisible();
        const claves = api.peticiones.filter((p) => p.metodo === "POST" && p.ruta === "/api/v1/portal/tickets").map((p) => p.idempotencia);
        expect(claves).toHaveLength(2);
        expect(claves[0]).not.toBe(claves[1]);
    });

    test("detalle: conversación diferenciada, falla general y responder", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte/501");
        await expect(page.getByRole("heading", { name: "Sin internet desde la mañana" })).toBeVisible();
        await expect(page.getByTestId("falla-general")).toBeVisible();
        const mensajes = page.getByRole("list", { name: "Mensajes" });
        await expect(mensajes.locator('[data-autor="cliente"]')).toContainText("Tú");
        await expect(mensajes.locator('[data-autor="agente"]')).toContainText("Sofía Morales");
        await expect(mensajes.locator('[data-autor="agente"]')).toContainText("Equipo de soporte");
        await expect(mensajes.locator('[data-autor="sistema"]')).toContainText("Aviso automático");
        await expect(page.getByTestId("confirmar-solucion")).toHaveCount(0);

        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByText("Escribe tu mensaje", { exact: true }).last()).toBeVisible();
        await page.getByLabel("Escribe tu mensaje").fill("Sigo sin servicio.");
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(mensajes.locator('[data-autor="cliente"]').last()).toContainText("Sigo sin servicio.");
        await expect(page.getByLabel("Escribe tu mensaje")).toHaveValue("");
        const r = api.peticiones.find((p) => p.ruta === "/api/v1/portal/tickets/501/responder");
        expect(r?.cuerpo).toEqual({ cuerpo: "Sigo sin servicio.", version: 1 });
        expect(r?.csrf).toBe(CSRF_PORTAL);
        expect(r?.idempotencia).toBeTruthy();
        expect(r?.empresa).toBeUndefined();
    });

    test("responder: 429 demasiados_mensajes muestra el detail", async ({ page }) => {
        const api = await simularApiPortal(page);
        const detalle = "Enviaste muchos mensajes en poco tiempo; espera un minuto.";
        api.responderProblema("POST", "/api/v1/portal/tickets/501/responder", {
            type: "urn:iris:problema:demasiados_mensajes",
            title: "Demasiados mensajes",
            status: 429,
            detail: detalle,
        });
        await page.goto("/soporte/501");
        await page.getByLabel("Escribe tu mensaje").fill("¿Hay novedades?");
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByText("Enviaste muchos mensajes seguidos")).toBeVisible();
        await expect(page.getByText(detalle)).toBeVisible();
        await expect(page.getByLabel("Escribe tu mensaje")).toHaveValue("¿Hay novedades?");
    });

    test("confirmar que quedó resuelto cierra el caso", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte/502");
        await expect(page.getByTestId("cabecera-caso")).toContainText("Resuelto");
        await expect(page.getByText("Si nos escribes, volveremos a abrir tu caso.")).toBeVisible();
        await page.getByRole("button", { name: "Confirmar que quedó resuelto" }).click();
        await expect(page.getByTestId("cabecera-caso")).toContainText("Cerrado");
        await expect(page.getByRole("heading", { name: "Este caso está cerrado" })).toBeVisible();
        await expect(page.getByRole("button", { name: "Confirmar que quedó resuelto" })).toHaveCount(0);
        await expect(page.getByLabel("Escribe tu mensaje")).toHaveCount(0);
        const c = api.peticiones.find((p) => p.ruta === "/api/v1/portal/tickets/502/confirmar-solucion");
        expect(c?.cuerpo).toEqual({ version: 4 });
        expect(c?.csrf).toBe(CSRF_PORTAL);
        expect(c?.idempotencia).toBeTruthy();
    });

    test("409 por versión: «Recargar» y luego confirmar", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte/502");
        await expect(page.getByRole("button", { name: "Confirmar que quedó resuelto" })).toBeVisible();
        api.cambiarVersion(502);
        await page.getByRole("button", { name: "Confirmar que quedó resuelto" }).click();
        await expect(page.getByText("Tu caso cambió mientras lo veías")).toBeVisible();
        await page.getByRole("button", { name: "Recargar" }).click();
        await expect(page.getByText("Tu caso cambió mientras lo veías")).toHaveCount(0);
        await page.getByRole("button", { name: "Confirmar que quedó resuelto" }).click();
        await expect(page.getByTestId("cabecera-caso")).toContainText("Cerrado");
        const confirmaciones = api.peticiones.filter((p) => p.ruta === "/api/v1/portal/tickets/502/confirmar-solucion").map((p) => p.cuerpo);
        expect(confirmaciones).toEqual([{ version: 4 }, { version: 5 }]);
    });

    test("409 al responder conserva el mensaje", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte/501");
        await expect(page.getByLabel("Escribe tu mensaje")).toBeVisible();
        api.cambiarVersion(501);
        await page.getByLabel("Escribe tu mensaje").fill("Ya volvió el internet");
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByText("Tu caso cambió mientras lo veías")).toBeVisible();
        await expect(page.getByLabel("Escribe tu mensaje")).toHaveValue("Ya volvió el internet");
        await page.getByRole("button", { name: "Recargar" }).click();
        await page.getByRole("button", { name: "Enviar" }).click();
        await expect(page.getByRole("list", { name: "Mensajes" }).locator('[data-autor="cliente"]').last()).toContainText("Ya volvió el internet");
    });

    test("caso ajeno o inexistente: 404 amable", async ({ page }) => {
        await simularApiPortal(page);
        await page.goto("/soporte/999");
        await expect(page.getByRole("heading", { name: "No encontramos este caso" })).toBeVisible();
    });

    test("sesión vencida: vuelve a la entrada", async ({ page }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte");
        await expect(caso(page, "TK-000501")).toBeVisible();
        api.vencerSesion();
        await caso(page, "TK-000501").click();
        await expect(page).toHaveURL(/\/soporte\/entrar\?motivo=expirada$/);
        await expect(page.getByRole("heading", { name: "Tu sesión terminó" })).toBeVisible();
    });

    test("cerrar sesión", async ({ page, context }) => {
        const api = await simularApiPortal(page);
        await page.goto("/soporte");
        await page.getByRole("button", { name: "Salir" }).click();
        await expect(page.getByRole("heading", { name: "Cerraste tu sesión" })).toBeVisible();
        const salida = api.peticiones.find((p) => p.metodo === "DELETE" && p.ruta === "/api/v1/portal/sesion");
        expect(salida?.csrf).toBe(CSRF_PORTAL);
        expect((await context.cookies()).some((c) => c.name === "iris_portal")).toBe(false);
        await page.goto("/soporte");
        await expect(page).toHaveURL(/\/soporte\/entrar$/);
    });
});

test("capturas del portal", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `portal-${que}-${info.project.name}.png`);
    const api = await simularApiPortal(page);
    await page.goto(`/soporte/entrar#${TICKET_VENCIDO}`);
    await expect(page.getByRole("heading", { name: "El enlace venció o ya se usó" })).toBeVisible();
    await page.screenshot({ path: nombre("01-enlace-vencido"), fullPage: true });
    await page.goto(`/soporte/entrar#${TICKET_LIMITE}`);
    await expect(page.getByText(/Espera 2 min/)).toBeVisible();
    await page.screenshot({ path: nombre("02-limite"), fullPage: true });
    await page.goto("/soporte/entrar");
    await expect(page.getByRole("heading", { name: "Entra desde el portal de tu proveedor" })).toBeVisible();
    await page.screenshot({ path: nombre("03-sin-sesion"), fullPage: true });

    await conSesionPortal(context, baseURL ?? "");
    await page.goto("/soporte");
    await expect(caso(page, "TK-000501")).toBeVisible();
    await page.screenshot({ path: nombre("04-lista"), fullPage: true });
    await page.goto("/soporte/nuevo");
    await page.getByRole("button", { name: "Enviar" }).click();
    await expect(page.getByText("Escribe un asunto")).toBeVisible();
    await page.screenshot({ path: nombre("05-nuevo"), fullPage: true });
    await page.goto("/soporte/501");
    await expect(page.locator('[data-autor="sistema"]')).toBeVisible();
    await page.screenshot({ path: nombre("06-detalle-falla"), fullPage: true });
    await page.goto("/soporte/502");
    await expect(page.getByTestId("confirmar-solucion")).toBeVisible();
    await page.screenshot({ path: nombre("07-detalle-confirmar"), fullPage: true });
    api.cambiarVersion(502);
    await page.getByRole("button", { name: "Confirmar que quedó resuelto" }).click();
    await expect(page.getByText("Tu caso cambió mientras lo veías")).toBeVisible();
    await page.screenshot({ path: nombre("08-conflicto"), fullPage: true });
});
