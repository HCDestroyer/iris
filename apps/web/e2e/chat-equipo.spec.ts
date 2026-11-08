import { expect as expectBase, test, type Page } from "@playwright/test";
import { ADMIN, CONTRASENA, conSesion, EMPRESAS, simularApi, type ApiFalsa } from "./api-falsa";
import { CONV, MARIO, ORDEN_CERRADA, ORDEN_SIN_CHAT, ORDEN_VIVA, PEDRO, type ApiChatPanel } from "./api-chat-panel";
import { abrirMenu, cerrarMenu, entrar, esTelefono, hojaMas, selectorEmpresa } from "./ayudas";
import { elegirOpcion, elegirSugerencia } from "./controles";
import { esperarSinDesbordes } from "./desbordes";

// Con 4 workers el motor del chat (flujo, cola y verificación de la sesión) y las lecturas en serie
// tardan más que los 5 s por defecto: 10 s por espera.
const expect = expectBase.configure({ timeout: 10_000 });

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

async function cerrarSesion(page: Page) {
    if (esTelefono(page)) {
        await abrirMenu(page);
        await hojaMas(page).getByRole("menuitem", { name: "Cerrar sesión" }).click();
    } else {
        await page.getByRole("button", { name: /^Tu cuenta/ }).click();
        await page.getByRole("dialog", { name: "Tu cuenta y disponibilidad" }).getByRole("menuitem", { name: "Cerrar sesión" }).click();
    }
}

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
            await page
                .getByRole("complementary", { name: "Barra lateral" })
                .getByRole("link", { name: /Tickets/ })
                .click();
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

    test("panel flotante: la página no se corre, la agenda de atrás responde y «Cerrar» siempre se ve", async ({ page }) => {
        await abrir(page, "/agenda");
        await expect(page.getByRole("radio", { name: "Mes" })).toBeChecked();
        const contenido = page.locator("#contenido");
        // Ancho de la página y caja horizontal del contenido (el alto puede cambiar al llegar los datos).
        const medir = () =>
            page.evaluate(() => {
                const c = document.getElementById("contenido")?.getBoundingClientRect();
                const estilo = document.getElementById("contenido") ? getComputedStyle(document.getElementById("contenido") as HTMLElement) : null;
                return { ancho: document.documentElement.scrollWidth, x: c?.x, width: c?.width, padding: estilo?.paddingRight, margen: estilo?.marginRight };
            });
        const antes = await medir();
        await botonChat(page).click();
        const panel = panelChat(page);
        await expect(panel.getByText("En vivo")).toBeVisible({ timeout: 10_000 });
        const cerrar = panel.getByRole("button", { name: "Cerrar el chat del equipo" });
        await expect(cerrar).toBeInViewport({ ratio: 1 });
        // La página de atrás no cambia de ancho ni se reacomoda al abrir el panel.
        expect(await medir()).toEqual(antes);
        await expect(page.locator(".modal__backdrop, .drawer__backdrop")).toHaveCount(0);
        const vista = page.viewportSize() ?? { width: 0, height: 0 };
        const caja = await panel.boundingBox();
        if (!caja) throw new Error("sin panel");
        if (vista.width < 640) {
            // Teléfono: a pantalla completa bajo la barra superior.
            expect(Math.round(caja.x)).toBe(0);
            expect(Math.round(caja.width)).toBe(vista.width);
            expect(Math.round(caja.y)).toBe(56);
            expect(Math.round(caja.y + caja.height)).toBe(vista.height);
        } else {
            // Escritorio: tarjeta flotante de 380 px a 12 px de los bordes, con esquinas y sombra.
            expect(Math.round(caja.width)).toBe(380);
            expect(Math.round(vista.width - (caja.x + caja.width))).toBe(12);
            expect(Math.round(caja.y)).toBe(56 + 12);
            expect(Math.round(vista.height - (caja.y + caja.height))).toBe(12);
            const estilo = await panel.evaluate((n) => {
                const e = getComputedStyle(n);
                return { radio: Number.parseFloat(e.borderTopLeftRadius), sombra: e.boxShadow, borde: e.borderLeftWidth };
            });
            expect(estilo.radio).toBeGreaterThan(0);
            expect(estilo.sombra).not.toBe("none");
            expect(estilo.borde).toBe("1px");
            // Un clic en la agenda de atrás (fuera del panel) le llega a ella, con el panel abierto.
            await page.getByRole("button", { name: "Mes siguiente" }).click();
            await expect(page).toHaveURL(/dia=\d{4}-\d{2}-\d{2}/);
            await expect(panel).toBeVisible();
            await expect(contenido).toBeVisible();
            // A 1440 px igual: la página no se corre y «Cerrar» se ve.
            await page.setViewportSize({ width: 1440, height: 900 });
            await expect(cerrar).toBeInViewport({ ratio: 1 });
            const ancho = await medir();
            await cerrar.click();
            await expect(panel).toBeHidden();
            expect(await medir()).toEqual(ancho);
        }
        await esperarSinDesbordes(page);
    });

    test("con el panel abierto los avisos emergentes no quedan encima ni debajo de él", async ({ page }) => {
        await abrir(page);
        const panel = panelChat(page);
        const anchos = esTelefono(page) ? [null] : [1280, 800];
        for (const ancho of anchos) {
            if (ancho) await page.setViewportSize({ width: ancho, height: 800 });
            if (!(await panel.isVisible())) await botonChat(page).click();
            if (await fila(page, CONV.ordenViva).isVisible()) await fila(page, CONV.ordenViva).click();
            await compositor(page).fill(`Aviso a ${ancho ?? "teléfono"}`);
            await page.keyboard.press("Enter");
            const mensaje = page.locator("[data-mensaje]").filter({ hasText: `Aviso a ${ancho ?? "teléfono"}` });
            await expect(mensaje.locator('[data-slot="estado-envio"]')).toHaveText("enviado");
            await mensaje.getByRole("button", { name: "Opciones del mensaje" }).click();
            await page.getByRole("menuitem", { name: "Eliminar" }).click();
            const aviso = page.locator('[data-region="avisos-emergentes"] [data-slot="toast"]').filter({ hasText: "Mensaje eliminado" });
            await expect(aviso).toBeVisible();
            const [a, p] = [await aviso.boundingBox(), await panel.boundingBox()];
            if (!a || !p) throw new Error("sin cajas");
            const cruzan = a.x < p.x + p.width && p.x < a.x + a.width && a.y < p.y + p.height && p.y < a.y + a.height;
            expect(cruzan, `aviso ${JSON.stringify(a)} y panel ${JSON.stringify(p)}`).toBe(false);
            await expect(aviso).toBeInViewport({ ratio: 1 });
            await aviso.getByRole("button", { name: "Deshacer" }).click();
            // El puntero sale de los avisos (si no, HeroUI los deja expandidos).
            await page.mouse.move(1, (page.viewportSize()?.height ?? 800) - 1);
        }
    });

    test("cerrar y volver a abrir el panel conserva la conversación y lo escrito sin enviar", async ({ page }) => {
        await abrir(page);
        await botonChat(page).click();
        const panel = panelChat(page);
        await fila(page, CONV.ordenViva).click();
        await expect(panel.getByRole("heading", { name: "OT-000005 · Mantenimiento" })).toBeVisible();
        await compositor(page).fill("Voy en camino con el conector");
        await panel.getByRole("button", { name: "Cerrar el chat del equipo" }).click();
        await expect(panel).toBeHidden();
        await botonChat(page).click();
        await expect(panel.getByRole("heading", { name: "OT-000005 · Mantenimiento" })).toBeVisible();
        await expect(compositor(page)).toHaveValue("Voy en camino con el conector");
    });

    test("el borrador no pasa a otra empresa ni sobrevive a cerrar sesión", async ({ page }) => {
        test.setTimeout(60_000);
        await simularApi(page, { chat: true, empresas: EMPRESAS });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();
        const panel = panelChat(page);
        const abrirConversacion = async () => {
            await botonChat(page).click();
            const titulo = panel.getByRole("heading", { name: "OT-000005 · Mantenimiento" });
            await expect(titulo.or(fila(page, CONV.ordenViva))).toBeVisible();
            if (!(await titulo.isVisible())) await fila(page, CONV.ordenViva).click();
            await expect(titulo).toBeVisible();
        };
        const escribirBorrador = async () => {
            await abrirConversacion();
            await compositor(page).fill("Borrador de Cable Norte");
            await panel.getByRole("button", { name: "Cerrar el chat del equipo" }).click();
            await expect(panel).toBeHidden();
        };

        // Otra empresa: el chat (y su borrador) es otro.
        await escribirBorrador();
        await elegirOpcion(await selectorEmpresa(page), { label: "Fibra Sur" });
        await cerrarMenu(page);
        await abrirConversacion();
        await expect(compositor(page)).toHaveValue("");
        await panel.getByRole("button", { name: "Cerrar el chat del equipo" }).click();

        // Cerrar sesión y volver a entrar (sin recargar la página): tampoco queda.
        await elegirOpcion(await selectorEmpresa(page), { label: "Cable Norte" });
        await cerrarMenu(page);
        await escribirBorrador();
        await cerrarSesion(page);
        // Cerrar sesión espera hasta 3 s a que salga la cola del chat.
        await expect(page).toHaveURL(/\/entrar/, { timeout: 10_000 });
        await entrar(page, ADMIN.correo, CONTRASENA);
        const empresas = page.getByRole("list", { name: "Empresas" });
        await expect(empresas.or(botonChat(page)).first()).toBeVisible({ timeout: 20_000 });
        if (await empresas.isVisible()) await empresas.getByRole("button", { name: "Cable Norte" }).click();
        await abrirConversacion();
        await expect(compositor(page)).toHaveValue("");
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
        await expect
            .poll(() => chat.peticiones.find((p) => p.metodo === "POST" && p.ruta.endsWith("/mensajes"))?.cuerpo)
            .toMatchObject({ texto: "Hola @Mario Ixcot revisa la potencia", menciones: [MARIO.id] });
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

    for (const caso of [
        { nombre: "esperando su «Deshacer»", vencida: false },
        { nombre: "con el «Deshacer» recién vencido (ya en la cola del motor)", vencida: true },
    ]) {
        test(`cerrar sesión con una eliminación ${caso.nombre}: sale antes de cerrar la sesión`, async ({ page }) => {
            // Reloj controlado: la espera de 5 s del «Deshacer» vence solo cuando la prueba lo dice.
            await page.clock.install({ time: new Date() });
            const { api, chat } = await abrir(page, `/equipo?c=${CONV.directaLaura}`);
            await compositor(page).fill("Se borra al salir");
            await page.keyboard.press("Enter");
            const mensaje = page.locator("[data-mensaje]").filter({ hasText: "Se borra al salir" });
            await expect(mensaje.locator('[data-slot="estado-envio"]')).toHaveText("enviado");
            const id = await mensaje.getAttribute("data-mensaje");
            await mensaje.getByRole("button", { name: "Opciones del mensaje" }).click();
            await page.getByRole("menuitem", { name: "Eliminar" }).click();
            await expect(page.locator(`[data-mensaje="${id}"]`)).toHaveAttribute("data-eliminado", "true");
            if (caso.vencida) await page.clock.runFor(5_500);
            if (esTelefono(page)) {
                // Dentro de la conversación la barra inferior se oculta: «Atrás» vuelve a la lista.
                await page.getByRole("button", { name: "Atrás" }).click();
            }
            await cerrarSesion(page);
            // Cerrar sesión espera hasta 3 s a que salga la cola del chat.
            await expect(page).toHaveURL(/\/entrar/, { timeout: 10_000 });
            // Orden en que la API falsa recibió las peticiones: la eliminación, antes de `DELETE /sesion`.
            const orden = () => api.orden;
            const eliminar = `POST /api/v1/mensajeria/chat/mensajes/${id}/eliminar`;
            await expect.poll(() => orden().indexOf("DELETE /api/v1/sesion")).toBeGreaterThan(-1);
            await expect.poll(() => orden().indexOf(eliminar)).toBeGreaterThan(-1);
            expect(orden().indexOf(eliminar)).toBeLessThan(orden().indexOf("DELETE /api/v1/sesion"));
            expect(chat.eliminados).toEqual([id]);
        });
    }

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
