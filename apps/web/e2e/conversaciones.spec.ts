import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, simularApi } from "./api-falsa";
import { esTelefono } from "./ayudas";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

/*
 * «Conversaciones» (bandeja con clientes, fase 1): lista, conversación y ficha; enviar y anotar;
 * ventana de 24 h; permisos; errores con «Reintentar»; sin señal; tiempo real y la verificación de
 * la sesión antes de enviar. En 390, 1024 y 1280 px, claro y oscuro (los 4 proyectos más 1024 px
 * dentro de los de escritorio), con la comprobación de que nada se desborda ni queda fuera.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const fila = (page: Page, id: number) => page.locator(`[data-conversacion="${id}"]`);
const compositor = (page: Page) => page.getByRole("textbox", { name: /Mensaje para|Nota interna/ });

async function abrir(page: Page, correo = ADMIN.correo, ruta = "/conversaciones", opciones: Parameters<typeof simularApi>[1] = {}) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, opciones);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}

test.describe("bandeja de conversaciones", () => {
    test("lista de dos líneas, conversación con burbujas, nota y eventos, y ficha del cliente", async ({ page }, info) => {
        await abrir(page);
        await expect(page.getByRole("heading", { name: "Conversaciones", level: 1 })).toBeVisible();
        const carlos = fila(page, 41);
        await expect(carlos).toContainText("Carlos Ruiz");
        await expect(fila(page, 43)).toContainText("+502 5544 0018");
        await expect(carlos).toContainText("Ya lo reinicié y sigue igual.");
        await expect(carlos).toContainText("2 sin leer");
        await expect(page.getByRole("radio", { name: /Mías\s*1/ })).toBeVisible();
        await esperarSinDesbordes(page);
        if (esTelefono(page)) await page.screenshot({ path: path.join(carpeta, `bandeja-lista-${info.project.name}.png`) });

        await carlos.click();
        await expect(page).toHaveURL(/\?c=41$/);
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        await expect(conv.getByRole("heading", { name: "Carlos Ruiz" })).toBeVisible();
        const hilo = conv.getByRole("log", { name: "Mensajes" });
        await expect(hilo).toContainText("Buenos días, no tengo internet desde las 7.");
        await expect(hilo).toContainText("Ana creó TK-004812");
        await expect(hilo.locator('[data-direccion="nota"]')).toContainText("Nota interna");
        await expect(hilo.locator('[data-direccion="saliente"]')).toContainText("leído");
        await expect(conv.locator('[data-ventana="abierta"]')).toContainText(/\d+ h/);

        if (esTelefono(page)) {
            await expect(page.getByRole("link", { name: "Volver a las conversaciones" })).toBeVisible();
            await conv.getByRole("button", { name: "Ver la ficha del cliente" }).click();
            const hoja = page.getByRole("dialog", { name: "Ficha del cliente" });
            await expect(hoja).toContainText("Fibra 100 Mb · S-4101");
            await page.screenshot({ path: path.join(carpeta, `bandeja-ficha-${info.project.name}.png`) });
            await hoja.getByRole("button", { name: "Cerrar la ficha" }).click();
        } else {
            const ficha = page.getByRole("complementary", { name: "Ficha del cliente" });
            await expect(ficha).toContainText("Fibra 100 Mb · S-4101");
            await expect(ficha).toContainText("TK-004812");
            await expect(ficha.getByRole("link", { name: "Orden" })).toBeVisible();
        }
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-${info.project.name}.png`) });
    });

    test("a 1024 px la ficha pasa a una hoja y nada se corta", async ({ page }, info) => {
        test.skip(esTelefono(page), "Solo en los proyectos de escritorio");
        await page.setViewportSize({ width: 1024, height: 768 });
        await abrir(page, ADMIN.correo, "/conversaciones?c=41");
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        await expect(conv.getByRole("log", { name: "Mensajes" })).toContainText("Ya lo reinicié");
        await expect(page.getByRole("complementary", { name: "Ficha del cliente" })).toHaveCount(0);
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-1024-${info.project.name}.png`) });
        await conv.getByRole("button", { name: "Ver la ficha del cliente" }).click();
        await expect(page.getByRole("dialog", { name: "Ficha del cliente" })).toContainText("Carlos Ruiz");
    });

    test("enviar: verifica la sesión, manda el id del cliente como Idempotency-Key y aparece en el hilo", async ({ page }) => {
        const api = await abrir(page, ADMIN.correo, "/conversaciones?c=41");
        await page.getByRole("button", { name: "Visita hoy 13:30–15:00" }).click();
        await expect(compositor(page)).toHaveValue("Hola Carlos, te enviamos un técnico hoy entre 13:30 y 15:00.");
        await compositor(page).press("Enter");
        const hilo = page.getByRole("log", { name: "Mensajes" });
        await expect(hilo.locator('[data-direccion="saliente"]').last()).toContainText("te enviamos un técnico hoy");
        await expect(compositor(page)).toHaveValue("");
        const envio = api.conversaciones.enviados.find((e) => e.ruta.endsWith("/mensajes"));
        const cuerpo = envio?.cuerpo as { id: string; texto: string; respuestaRapidaId?: number };
        expect(cuerpo.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7/);
        expect(envio?.idempotencia).toBe(cuerpo.id);
        expect(cuerpo.respuestaRapidaId).toBe(1);
        // Antes del POST se leyó GET /yo (la sesión sigue siendo la misma).
        const i = api.peticiones.findIndex((p) => p.metodo === "POST" && p.ruta.endsWith("/mensajes"));
        expect(api.peticiones.slice(0, i).reverse().find((p) => p.ruta === "/api/v1/yo")).toBeTruthy();
    });

    test("nota interna en amarillo; Mayús + Enter baja de línea", async ({ page }) => {
        const api = await abrir(page, ADMIN.correo, "/conversaciones?c=41");
        await page.getByRole("radio", { name: "Nota interna" }).click();
        await compositor(page).fill("Revisar la potencia");
        await compositor(page).press("Shift+Enter");
        await compositor(page).pressSequentially("en la caja NAP");
        await page.getByRole("button", { name: "Guardar nota" }).click();
        await expect(page.getByRole("log", { name: "Mensajes" }).locator('[data-direccion="nota"]').last()).toContainText("Revisar la potencia\nen la caja NAP");
        expect(api.conversaciones.enviados.some((e) => e.ruta.endsWith("/notas"))).toBe(true);
    });

    test("ventana cerrada: solo plantillas aprobadas, explicado en una línea", async ({ page }, info) => {
        await abrir(page, ADMIN.correo, "/conversaciones?c=44");
        await expect(page.getByText("WhatsApp solo deja mandarle una plantilla aprobada en este número")).toBeVisible();
        await page.screenshot({ path: path.join(carpeta, `bandeja-ventana-cerrada-${info.project.name}.png`) });
        await expect(page.getByRole("textbox", { name: /Mensaje para/ })).toHaveCount(0);
        await page.getByRole("button", { name: "Elegir plantilla" }).click();
        await page.getByRole("button", { name: /Seguimiento de soporte/ }).click();
        await page.getByRole("button", { name: "Enviar plantilla" }).click();
        await expect(page.getByText("Obligatoria")).toBeVisible();
        await page.getByRole("textbox", { name: "nombre" }).fill("Don Pedro");
        await expect(page.getByText("Hola Don Pedro, ¿pudiste volver a conectarte?")).toBeVisible();
        await page.getByRole("button", { name: "Enviar plantilla" }).click();
        await expect(page.getByRole("log", { name: "Mensajes" })).toContainText("Plantilla «seguimiento»");
        await esperarSinDesbordes(page);
    });

    test("sin conversaciones.responder no hay compositor", async ({ page }) => {
        await abrir(page, CONSULTA.correo, "/conversaciones?c=41");
        await expect(page.getByText("Puedes ver esta conversación, pero no responderla.")).toBeVisible();
        await expect(page.getByRole("textbox", { name: /Mensaje para|Nota interna/ })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Enviar" })).toHaveCount(0);
    });

    test("error al leer la lista: aviso con «Reintentar»", async ({ page }) => {
        await page.clock.setFixedTime(new Date(AHORA_CONV));
        await simularApi(page);
        // Las dos primeras lecturas de la lista (la inicial y la de cuando se abre el flujo) fallan con 500.
        let fallos = 2;
        await page.route(/\/api\/v1\/mensajeria\/conversaciones\?/, (r) =>
            fallos-- > 0 ? r.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }) }) : r.fallback(),
        );
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/conversaciones");
        const reintentar = page.getByRole("button", { name: "Reintentar" }).first();
        await expect(reintentar).toBeVisible();
        await reintentar.click();
        await expect(fila(page, 41)).toBeVisible();
    });

    test("sin señal: el mensaje queda marcado y se reintenta con el mismo id", async ({ page }) => {
        const api = await abrir(page, ADMIN.correo, "/conversaciones?c=41");
        await page.route("**/api/v1/mensajeria/conversaciones/41/mensajes", (r) => (r.request().method() === "POST" ? r.abort("internetdisconnected") : r.fallback()));
        await compositor(page).fill("¿Sigue sin servicio?");
        await compositor(page).press("Enter");
        const pendiente = page.locator("[data-pendiente]");
        await expect(pendiente).toContainText("No se envió");
        const id = await pendiente.getAttribute("data-pendiente");
        await page.unroute("**/api/v1/mensajeria/conversaciones/41/mensajes");
        await pendiente.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByRole("log", { name: "Mensajes" }).locator(`[data-mensaje="${id}"]`)).toContainText("¿Sigue sin servicio?");
        expect(api.conversaciones.enviados.filter((e) => (e.cuerpo as { id?: string }).id === id)).toHaveLength(1);
    });

    test("si la sesión del navegador cambió, no se envía nada", async ({ page, context, baseURL }) => {
        // Con sondeo (cada 12 s) el feed aún no se enteró: es el compositor el que verifica.
        const sondeo = page.waitForRequest(/\/conversaciones\/cambios\?cursor=/);
        const api = await abrir(page, ADMIN.correo, "/conversaciones?c=41", { conversaciones: { sinFlujo: true } });
        await expect(compositor(page)).toBeVisible();
        // Tras el primer sondeo el feed espera 12 s: en ese rato solo el compositor puede notarlo.
        await sondeo;
        // En otra pestaña entró otra persona: la cookie del navegador ya es de ella.
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await compositor(page).fill("Mensaje que no debe salir");
        await compositor(page).press("Enter");
        await expect(page.locator("[data-pendiente]")).toContainText("La sesión de este navegador cambió");
        expect(api.conversaciones.enviados.filter((e) => e.ruta.endsWith("/mensajes"))).toHaveLength(0);
    });

    test("el flujo se detiene si la sesión cambió y pide recargar", async ({ page, context, baseURL }) => {
        const api = await abrir(page, ADMIN.correo, "/conversaciones?c=41");
        await expect(compositor(page)).toBeVisible();
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await expect(page.getByText("La sesión de este navegador cambió")).toBeVisible({ timeout: 10_000 });
        await expect(compositor(page)).toHaveCount(0);
        api.conversaciones.mensajeDelCliente(41, "Esto no debe verse");
        await page.waitForTimeout(1500);
        await expect(page.getByRole("log", { name: "Mensajes" })).not.toContainText("Esto no debe verse");
    });

    test("tiempo real: el mensaje del cliente llega sin recargar", async ({ page }) => {
        const api = await abrir(page, ADMIN.correo, "/conversaciones?c=41");
        await expect(page.getByRole("log", { name: "Mensajes" })).toContainText("Ya lo reinicié");
        await page.waitForTimeout(500);
        api.conversaciones.mensajeDelCliente(41, "¿A qué hora viene el técnico?");
        await expect(page.getByRole("log", { name: "Mensajes" })).toContainText("¿A qué hora viene el técnico?", { timeout: 10_000 });
    });

    test("tomar una sin asignar y filtros en un solo botón", async ({ page }, info) => {
        await abrir(page, ADMIN.correo, "/conversaciones?c=42");
        const conv = page.getByRole("region", { name: "Conversación con María López" });
        await conv.getByRole("button", { name: /Sin asignar\. Asignar/ }).click();
        await page.getByRole("menuitem", { name: "Asignarme" }).click();
        await expect(page.getByText("Tomaste la conversación con María López")).toBeVisible();
        if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();

        await page.getByRole("button", { name: "Filtros" }).click();
        await page.getByRole("checkbox", { name: "Cobros" }).click();
        await expect(page).toHaveURL(/canal=4/);
        await expect(page.getByRole("button", { name: "Filtros (1 activos)" })).toBeVisible();
        await page.screenshot({ path: path.join(carpeta, `bandeja-filtros-${info.project.name}.png`) });
        await page.keyboard.press("Escape");
        await expect(fila(page, 41)).toHaveCount(0);
        await expect(fila(page, 42)).toBeVisible();
    });

    test("Enter en la búsqueda no envía nada: filtra en el servidor", async ({ page }) => {
        const api = await abrir(page);
        const buscar = page.getByRole("searchbox", { name: "Buscar conversaciones" });
        await buscar.fill("maría");
        await buscar.press("Enter");
        await expect(page).toHaveURL(/q=mar%C3%ADa/);
        await expect(fila(page, 42)).toBeVisible();
        await expect(fila(page, 41)).toHaveCount(0);
        expect(api.mutaciones.filter((m) => m.ruta.includes("/mensajeria"))).toHaveLength(0);
    });

    test("sin cliente: coincidencias con «Asociar» y contacto de Iris", async ({ page }, info) => {
        await abrir(page, ADMIN.correo, "/conversaciones?c=43");
        if (esTelefono(page)) await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
        const ficha = esTelefono(page) ? page.getByRole("dialog", { name: "Ficha del cliente" }) : page.getByRole("complementary", { name: "Ficha del cliente" });
        await expect(ficha).toContainText("Sin cliente asociado.");
        await expect(ficha).toContainText("contacto de un servicio");
        // La hoja termina de entrar antes de medir.
        await expect(ficha).toBeInViewport({ ratio: 1 });
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-sin-cliente-${info.project.name}.png`) });
        await ficha.getByRole("button", { name: "Asociar" }).first().click();
        await expect(page.getByText("Luis Herrera asociado a la conversación")).toBeVisible();
        await esperarSinDesbordes(page);
    });

    test("cliente asociado a mano sin coincidencia: pendiente de confirmar hasta que un supervisor lo confirma", async ({ page }) => {
        await abrir(page, ADMIN.correo, "/conversaciones?c=44");
        if (esTelefono(page)) await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
        const ficha = esTelefono(page) ? page.getByRole("dialog", { name: "Ficha del cliente" }) : page.getByRole("complementary", { name: "Ficha del cliente" });
        const aviso = ficha.getByTestId("cliente-sin-confirmar");
        await expect(aviso).toContainText("Cliente pendiente de confirmar");
        await expect(ficha.getByRole("button", { name: "Boleta" })).toBeDisabled();
        await expect(ficha.getByRole("button", { name: "Promesa" })).toBeDisabled();
        await expect(ficha).toBeInViewport({ ratio: 1 });
        await esperarSinDesbordes(page);
        await aviso.getByRole("button", { name: "Confirmar" }).click();
        await page.getByRole("alertdialog").getByRole("button", { name: "Confirmar cliente" }).click();
        await expect(page.getByText(/Cliente de .* confirmado/)).toBeVisible();
        await expect(ficha.getByTestId("cliente-sin-confirmar")).toHaveCount(0);
        await expect(ficha.getByRole("button", { name: "Promesa" })).toBeEnabled();
    });
});
