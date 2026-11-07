import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi, type ApiFalsa } from "./api-falsa";
import { CONV, MARIO, ORDEN_CERRADA, ORDEN_SIN_CHAT, ORDEN_VIVA, PEDRO, type ApiChatPanel } from "./api-chat-panel";
import { esTelefono } from "./ayudas";
import { elegirSugerencia } from "./controles";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Chat del equipo en el panel (`contratos/chat.md`, panel): ícono con su insignia en la barra
 * superior, panel lateral sin telón, página «Equipo», pestaña «Chat del equipo» de la orden,
 * menciones con @, eliminar con «Deshacer», hilo de orden cerrada y «Reconectando…» con
 * «Reintentar». En los 4 proyectos (escritorio y teléfono, claro y oscuro).
 */

async function abrir(page: Page, ruta = "/"): Promise<{ api: ApiFalsa; chat: ApiChatPanel }> {
    const api = await simularApi(page, { chat: true });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    if (!api.chat) throw new Error("sin chat");
    return { api, chat: api.chat };
}

const botonChat = (page: Page) => page.locator('[data-slot="boton-chat-equipo"]');
const panelChat = (page: Page) => page.getByRole("dialog", { name: "Chat del equipo" });
const fila = (page: Page, id: number) => page.locator(`[data-conversacion-chat="${id}"]`);
const compositor = (page: Page) => page.getByRole("combobox", { name: /Mensaje para/ });

test.describe("chat del equipo", () => {
    test("insignias: número en directas y menciones, punto en cuadrillas", async ({ page }) => {
        await abrir(page);
        await expect(botonChat(page)).toHaveAccessibleName("Chat del equipo: 3 piden tu respuesta");
        await expect(page.locator('[data-slot="insignia-chat-equipo"]')).toHaveText("3");
        await botonChat(page).click();
        const panel = panelChat(page);
        await expect(panel.getByRole("radio", { name: /Todas\s*3/ })).toBeVisible();
        await expect(panel.getByRole("radio", { name: /Directas\s*2/ })).toBeVisible();
        await expect(panel.getByRole("radio", { name: /Órdenes\s*1/ })).toBeVisible();
        await expect(fila(page, CONV.directaMario).locator('[data-slot="insignia-chat"]')).toHaveText(/^2/);
        await expect(fila(page, CONV.ordenViva).locator('[data-slot="insignia-chat"]')).toContainText("1 mención");
        await expect(fila(page, CONV.cuadrillaNorte).locator('[data-slot="punto-chat"]')).toBeVisible();
        await expect(fila(page, CONV.cuadrillaNorte).locator('[data-slot="insignia-chat"]')).toHaveCount(0);
        await expect(fila(page, CONV.ordenCerrada)).toContainText("Cerrada");
        // Cuadrillas, orden y directa con avatares distintos: cuadrado con icono para equipos y órdenes.
        await expect(fila(page, CONV.cuadrillaNorte).locator('[data-avatar-tipo="cuadrilla"]')).toBeVisible();
        await expect(fila(page, CONV.ordenViva).locator('[data-avatar-tipo="orden"]')).toBeVisible();

        // Leer la directa baja el número del ícono (el resumen se vuelve a pedir).
        await fila(page, CONV.directaMario).click();
        await expect(panel.getByRole("heading", { name: MARIO.nombre })).toBeVisible();
        await expect(botonChat(page)).toHaveAccessibleName("Chat del equipo: 1 pide tu respuesta", { timeout: 10_000 });
    });

    test("panel lateral: abre sin telón, foco adentro, Escape cierra y el foco vuelve al ícono", async ({ page }) => {
        await abrir(page);
        await botonChat(page).click();
        const panel = panelChat(page);
        await expect(panel).toBeVisible();
        await expect(panel).toHaveAttribute("aria-modal", "false");
        await expect(panel.getByRole("heading", { name: "Equipo" })).toBeFocused();
        await expect(page.locator(".modal__backdrop, .drawer__backdrop")).toHaveCount(0);
        await expect(panel.getByText("En vivo")).toBeVisible({ timeout: 10_000 });
        await esperarSinDesbordes(page);
        if (!esTelefono(page)) {
            // La pantalla de atrás sigue usable: la barra lateral responde con el panel abierto.
            await page.getByRole("complementary", { name: "Barra lateral" }).getByRole("link", { name: /Tickets/ }).click();
            await expect(page).toHaveURL(/\/tickets/);
            await expect(panel).toBeVisible();
            // Escape cierra el panel solo con el foco adentro (no le roba la tecla a la página).
            await page.keyboard.press("Escape");
            await expect(panel).toBeVisible();
            await panel.getByRole("heading", { name: "Equipo" }).focus();
        }
        await page.keyboard.press("Escape");
        await expect(panel).toBeHidden();
        await expect(botonChat(page)).toBeFocused();
        await expect(botonChat(page)).toHaveAttribute("aria-expanded", "false");
    });

    test("lista y conversación se turnan con «Atrás»; «Abrir en página» conserva la conversación", async ({ page }) => {
        await abrir(page);
        await botonChat(page).click();
        const panel = panelChat(page);
        await fila(page, CONV.ordenViva).click();
        await expect(panel.getByRole("heading", { name: "OT-000005 · Mantenimiento" })).toBeVisible();
        await expect(panel.locator('[data-slot="franja-interno"]')).toContainText("Chat interno: el cliente no lo ve");
        // Mensajes recibidos con nombre y rol; los propios en teal con «visto».
        await expect(panel.getByText("Técnico asignado").first()).toBeVisible();
        await expect(panel.locator('[data-slot="estado-envio"]').filter({ hasText: "visto" })).toBeVisible();
        await panel.getByRole("button", { name: "Atrás" }).click();
        await expect(panel.getByRole("heading", { name: "Equipo" })).toBeVisible();
        await fila(page, CONV.ordenViva).click();
        await panel.getByRole("button", { name: "Abrir en página" }).click();
        await expect(page).toHaveURL(new RegExp(`/equipo\\?c=${CONV.ordenViva}$`));
        await expect(panel).toBeHidden();
        const conv = page.getByRole("region", { name: "Conversación: OT-000005 · Mantenimiento" });
        await expect(conv.getByText("la bodega de Villa Nueva ya tiene conectores")).toBeVisible();
        await esperarSinDesbordes(page);
    });

    test("página Equipo: filtros, búsqueda y migas", async ({ page }) => {
        await abrir(page, "/equipo");
        await expect(page.getByRole("heading", { name: "Equipo", level: 1 })).toBeVisible();
        if (!esTelefono(page)) await expect(page.getByRole("navigation", { name: /Migas|Ruta/ }).getByText("Equipo")).toBeVisible();
        await page.getByRole("radio", { name: /Cuadrillas/ }).click();
        await expect(page).toHaveURL(/filtro=cuadrillas/);
        await expect(fila(page, CONV.cuadrillaNorte)).toBeVisible();
        await expect(fila(page, CONV.directaMario)).toHaveCount(0);
        await page.getByRole("radio", { name: /Todas/ }).click();
        await page.getByRole("searchbox", { name: "Buscar en el chat del equipo" }).fill("laura");
        await expect(fila(page, CONV.directaLaura)).toBeVisible();
        await expect(fila(page, CONV.ordenViva)).toHaveCount(0);
        await esperarSinDesbordes(page);
    });

    test("mención con @: autocompleta miembros y manda sus ids por la cola", async ({ page }) => {
        const { chat } = await abrir(page, `/equipo?c=${CONV.ordenViva}`);
        const area = compositor(page);
        await area.click();
        await area.pressSequentially("Hola @Ma");
        const lista = page.getByRole("listbox", { name: "Mencionar a…" });
        await expect(lista.getByRole("option", { name: /Mario Ixcot/ })).toBeVisible();
        await expect(area).toHaveAttribute("aria-expanded", "true");
        await page.keyboard.press("Enter");
        await expect(lista).toBeHidden();
        await expect(area).toHaveValue("Hola @Mario Ixcot ");
        await area.pressSequentially("revisa la potencia");
        await page.keyboard.press("Enter");
        await expect(area).toHaveValue("");
        await expect.poll(() => chat.peticiones.find((p) => p.metodo === "POST" && p.ruta.endsWith("/mensajes"))?.cuerpo).toMatchObject({ texto: "Hola @Mario Ixcot revisa la potencia", menciones: [MARIO.id] });
        const enviado = page.locator("[data-mensaje]").filter({ hasText: "revisa la potencia" });
        await expect(enviado.locator("[data-mencion]")).toHaveText("@Mario Ixcot");
    });

    test("eliminar un mensaje propio con «Deshacer»: nada sale; sin deshacer, sale a los 5 s", async ({ page }) => {
        const { chat } = await abrir(page, `/equipo?c=${CONV.directaLaura}`);
        const area = compositor(page);
        await area.fill("Mensaje para borrar");
        await page.keyboard.press("Enter");
        const mensaje = page.locator("[data-mensaje]").filter({ hasText: "Mensaje para borrar" });
        await expect(mensaje.locator('[data-slot="estado-envio"]')).toHaveText("enviado");
        const id = await mensaje.getAttribute("data-mensaje");

        await mensaje.getByRole("button", { name: "Opciones del mensaje" }).click();
        await page.getByRole("menuitem", { name: "Eliminar" }).click();
        await expect(page.locator(`[data-mensaje="${id}"]`)).toHaveAttribute("data-eliminado", "true");
        await page.getByRole("button", { name: "Deshacer" }).click();
        await expect(page.locator(`[data-mensaje="${id}"]`)).toContainText("Mensaje para borrar");
        await page.waitForTimeout(6_000);
        expect(chat.eliminados).toEqual([]);

        await page.locator(`[data-mensaje="${id}"]`).getByRole("button", { name: "Opciones del mensaje" }).click();
        await page.getByRole("menuitem", { name: "Eliminar" }).click();
        await expect(page.locator(`[data-mensaje="${id}"]`)).toHaveAttribute("data-eliminado", "true");
        await expect.poll(() => chat.eliminados, { timeout: 10_000 }).toEqual([id]);
        await expect(page.locator(`[data-mensaje="${id}"]`)).toContainText("Mensaje eliminado");
    });

    test("pestaña «Chat del equipo» de la orden: hilo, miembros y «Agregar persona»", async ({ page }) => {
        await abrir(page, `/ordenes/${ORDEN_VIVA}?pestana=chat`);
        const tab = page.getByRole("tab", { name: /Chat del equipo/ });
        await expect(tab).toHaveAttribute("aria-selected", "true");
        const hilo = page.getByRole("region", { name: "Chat del equipo de OT-000005" });
        await expect(hilo.locator('[data-slot="franja-interno"]')).toContainText("El técnico lo tiene en la app de campo");
        await expect(hilo.getByText("la bodega de Villa Nueva ya tiene conectores")).toBeVisible();
        await expect(compositor(page)).toBeVisible();
        const enChat = page.getByRole("complementary", { name: "En este chat" });
        await expect(enChat).toContainText("Mario Ixcot");
        await expect(enChat).toContainText("Técnico asignado");
        await enChat.getByRole("button", { name: "Agregar persona" }).click();
        const dialogo = page.getByRole("dialog", { name: "Agregar persona" });
        const campo = dialogo.getByRole("combobox", { name: "Persona" });
        await campo.fill("Pedro");
        await elegirSugerencia(campo, /Pedro Gómez/);
        await dialogo.getByRole("button", { name: "Agregar" }).click();
        await expect(dialogo).toBeHidden();
        await expect(enChat).toContainText(PEDRO.nombre);
        await expect(enChat).toContainText("Agregado por supervisión");
        // Las pestañas de la ficha se desplazan en horizontal en el teléfono (como siempre): se revisa el chat.
        await esperarSinDesbordes(page, esTelefono(page) ? '[data-slot="chat-orden"]' : "body");
    });

    test("orden sin chat: «Abrir el chat de la orden» lo crea", async ({ page }) => {
        const { chat } = await abrir(page, `/ordenes/${ORDEN_SIN_CHAT}?pestana=chat`);
        await page.getByRole("button", { name: "Abrir el chat de la orden" }).click();
        await expect(page.getByRole("region", { name: "Chat del equipo de OT-000002" })).toBeVisible();
        expect(chat.peticiones.some((p) => p.metodo === "POST" && p.ruta === `/ordenes/${ORDEN_SIN_CHAT}/abrir`)).toBe(true);
    });

    test("orden cerrada: solo lectura, aviso y sin compositor", async ({ page }) => {
        await abrir(page, `/ordenes/${ORDEN_CERRADA}?pestana=chat`);
        const hilo = page.getByRole("region", { name: "Chat del equipo de OT-000008" });
        await expect(hilo.getByText("Equipo retirado y en bodega.")).toBeVisible();
        await expect(hilo.locator('[data-slot="compositor-bloqueado"]')).toContainText("está cerrada: el chat queda de solo lectura");
        await expect(compositor(page)).toHaveCount(0);
        await expect(page.getByRole("complementary", { name: "En este chat" }).getByRole("button", { name: "Agregar persona" })).toHaveCount(0);
    });

    test("tiempo real caído: «Reconectando…» con «Reintentar» y vuelve a «En vivo»", async ({ page }) => {
        const api = await simularApi(page, { chat: true });
        if (!api.chat) throw new Error("sin chat");
        api.chat.caido = true;
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto(`/equipo?c=${CONV.ordenViva}`);
        const aviso = page.locator('[data-slot="aviso"]').filter({ hasText: "Sin conexión en vivo" });
        await expect(aviso).toBeVisible({ timeout: 15_000 });
        await expect(page.getByText("Reconectando…").filter({ visible: true }).first()).toBeVisible();
        await esperarSinDesbordes(page);
        api.chat.caido = false;
        await aviso.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText("En vivo").filter({ visible: true }).first()).toBeVisible({ timeout: 15_000 });
        await expect(page.getByText("Sin conexión en vivo")).toHaveCount(0);
    });

    test("nuevo mensaje: elegir una cuadrilla abre su chat", async ({ page }) => {
        await abrir(page, "/equipo");
        await page.getByRole("button", { name: "Nuevo mensaje" }).click();
        const dialogo = page.getByRole("dialog", { name: "Nuevo mensaje" });
        await dialogo.getByRole("radio", { name: "Cuadrilla" }).click();
        await dialogo.getByRole("radio", { name: "Cuadrilla Centro" }).click({ force: true });
        await dialogo.getByRole("button", { name: "Abrir chat de Cuadrilla Centro" }).click();
        await expect(dialogo).toBeHidden();
        await expect(page).toHaveURL(/\/equipo\?c=303$/);
        await expect(page.getByRole("region", { name: "Conversación: Cuadrilla Centro" })).toBeVisible();
    });
});
