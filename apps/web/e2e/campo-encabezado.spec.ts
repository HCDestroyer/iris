import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { ServidorChatFalso } from "@iris/sync/pruebas";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { abrirConexion, avisoCampo, pestanaCampo } from "./ayudas-campo";
import { PERMISOS_TECNICO, TECNICO } from "./datos-campo";
import { LUIS, simularChat } from "./api-chat";

/*
 * Encabezado e isla de la app del técnico (opción elegida por el PM, nov 2026), claro y oscuro:
 * fecha, «Hola» y, a la derecha, la nube de sincronización y el GPS (discretos con todo bien, crecen
 * cuando hay que actuar) junto a buscar, también en la barra compacta; cada uno abre solo su hoja
 * («Ubicación» con el interruptor, «Conexión y envíos» con «Enviar ahora»), que cierra con la X y con
 * «Atrás». Isla Hoy · Chat · «+» · Agenda · Yo, sin nada encima. Con IRIS_CAPTURAS_CHAT=<carpeta>
 * deja las capturas para el PM.
 */

const GUATE = { latitude: 14.6407, longitude: -90.5133, accuracy: 12 };
const PERMISOS = [...PERMISOS_TECNICO, "chat.ver", "chat.enviar"] as NonNullable<Parameters<typeof simularApiCampo>[1]>["permisos"];

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

const encabezado = (page: Page) => page.locator('[data-slot="encabezado-campo"]');
const indicadores = (page: Page) => page.locator('[data-slot="barra-conexion"]');
const nube = (page: Page) => indicadores(page).locator('[data-indicador="sincronizacion"]');
const gps = (page: Page) => page.getByTestId("indicador-ubicacion");
const isla = (page: Page) => page.getByRole("navigation", { name: "Secciones de la app" });

/** Una nota en OT-000101 (queda por enviar si no hay señal o el servidor no responde). */
async function agregarNota(page: Page, antes?: () => Promise<void>) {
    await page.goto("/campo?orden=101");
    const menu = page.getByRole("button", { name: "Más acciones de la orden" });
    await expect(menu).toBeVisible();
    // Lo que va antes de escribir (p. ej. quitar la señal ya con la página abierta).
    await antes?.();
    // «Agregar nota» del menú «⋯» de la orden (#122) abre su hoja.
    await menu.click();
    await page.getByRole("menuitem", { name: "Agregar nota" }).click();
    const hoja = page.getByRole("dialog", { name: "Agregar nota" });
    await hoja.getByLabel("Nueva nota").fill("Portón verde");
    await hoja.getByRole("button", { name: "Agregar nota" }).click();
    await expect(hoja).toHaveCount(0);
}

/** Espera a que terminen las animaciones (la hoja entra deslizándose). */
async function quieta(page: Page) {
    // Las infinitas (un indicador que gira) no cuentan.
    await page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== "running" || a.effect?.getTiming().iterations === Infinity));
}

/**
 * La isla del PM: la activa es una píldora en «tinta» con icono y nombre; las demás, solo el icono
 * en gris; el «+» sin relleno ni borde (icono de 26 px en color de texto). Zonas de 48 px o más.
 */
/** Una directa con un mensaje sin leer (el punto teal del chat en la isla). */
function conUnMensaje(s: ServidorChatFalso) {
    s.crearConversacion({ id: 41, tipo: "directa", titulo: LUIS.nombre, otro_usuario: LUIS });
    s.recibir(41, LUIS, "¿Tienes conectores SC/APC?");
}

async function revisarIsla(page: Page, activa: string) {
    const enlaces = isla(page).getByRole("link");
    for (const enlace of await enlaces.all()) {
        const nombre = enlace.locator('[data-slot="nombre-pestana"]');
        const texto = (await nombre.textContent()) ?? "";
        const caja = await enlace.boundingBox();
        expect(caja?.height ?? 0).toBeGreaterThanOrEqual(48);
        expect(caja?.width ?? 0).toBeGreaterThanOrEqual(48);
        if (texto === activa) {
            await expect(enlace).toHaveAttribute("aria-current", "page");
            await expect(nombre).toBeVisible();
            await expect.poll(() => nombre.evaluate((e) => e.getBoundingClientRect().width)).toBeGreaterThan(16);
            const colores = await enlace.evaluate((e) => ({ fondo: getComputedStyle(e).backgroundColor, texto: getComputedStyle(e).color }));
            expect(colores.fondo).not.toBe("rgba(0, 0, 0, 0)");
            expect(colores.fondo).not.toBe(colores.texto);
        } else {
            await expect(enlace).not.toHaveAttribute("aria-current", "page");
            await expect.poll(() => nombre.evaluate((e) => e.getBoundingClientRect().width)).toBe(0);
            expect(await enlace.evaluate((e) => getComputedStyle(e).backgroundColor)).toBe("rgba(0, 0, 0, 0)");
        }
    }
    const mas = isla(page).getByRole("button", { name: "Acciones rápidas" });
    const caja = await mas.boundingBox();
    expect(caja?.height ?? 0).toBeGreaterThanOrEqual(48);
    const estilo = await mas.evaluate((e) => {
        const c = getComputedStyle(e);
        const svg = e.querySelector("svg")?.getBoundingClientRect();
        return { fondo: c.backgroundColor, borde: c.borderTopWidth, icono: Math.round(svg?.width ?? 0) };
    });
    expect(estilo.fondo).toBe("rgba(0, 0, 0, 0)");
    expect(["0px", ""]).toContain(estilo.borde);
    expect(estilo.icono).toBe(26);
}

async function abrirHoy(page: Page) {
    await page.goto("/campo");
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
}

test("arriba: fecha, «Hola», nube y GPS discretos y buscar; sin avatar ni «nombre · empresa»; nada sobre la isla", async ({ page }) => {
    await simularApiCampo(page, { permisos: PERMISOS });
    await simularChat(page);
    await abrirHoy(page);
    await expect(encabezado(page).getByRole("heading", { level: 1, name: "Hola, Tomás" })).toBeVisible();
    await expect(encabezado(page).getByRole("button", { name: "Buscar orden" })).toBeVisible();
    await expect(encabezado(page).getByRole("button", { name: "Tu cuenta" })).toHaveCount(0);
    await expect(encabezado(page)).not.toContainText(`${TECNICO.nombre} ·`);
    // Discretos: sin número, sin «actuar»; el estado va en su nombre accesible.
    await expect(indicadores(page)).not.toHaveAttribute("data-actuar", "true");
    await expect(nube(page)).toHaveAccessibleName("Todo enviado, toca para ver el estado");
    await expect(nube(page)).toHaveAttribute("data-forma", "al_dia");
    await expect(nube(page).locator('[data-slot="marca-indicador"]')).toHaveCount(0);
    await expect(gps(page)).toHaveAccessibleName("GPS apagado, toca para ver el estado");
    // Zona táctil de 44 px aunque el icono sea más chico.
    const caja = await nube(page).boundingBox();
    expect(caja?.width).toBeGreaterThanOrEqual(44);
    expect(caja?.height).toBeGreaterThanOrEqual(44);
    // Hoy · Mañana · Semana en una barra segmentada.
    await expect(page.getByRole("radiogroup", { name: "Día" }).getByRole("radio")).toHaveText(["Hoy · 3", "Mañana", "Semana"]);
    await expect(page.getByRole("radio", { name: "Hoy · 3" })).toBeChecked();
    // La isla: Hoy · Chat · «+» · Agenda · Yo, sin nada flotando encima.
    await expect(isla(page).getByRole("link")).toHaveCount(4);
    const nombres = await isla(page).getByRole("link").evaluateAll((ls) => ls.map((l) => l.getAttribute("aria-label")));
    expect(nombres).toEqual(["Hoy", "Chat", "Agenda", `Yo · ${TECNICO.nombre}`]);
    await expect(isla(page).getByRole("button", { name: "Acciones rápidas" })).toBeVisible();
    await revisarIsla(page, "Hoy");
    const yo = await isla(page).getByRole("link", { name: /^Yo · / }).boundingBox();
    const islaCaja = await isla(page).boundingBox();
    const fila = await indicadores(page).boundingBox();
    expect(fila && islaCaja && fila.y + fila.height < islaCaja.y - 200).toBe(true);
    expect(yo?.width).toBeGreaterThanOrEqual(44);
});

test("sin señal con pendientes la nube crece (tachada, naranja, con número) y abre solo «Conexión y envíos»", async ({ page, context }) => {
    const api = await simularApiCampo(page, { permisos: PERMISOS });
    await simularChat(page);
    await abrirHoy(page);
    await agregarNota(page, async () => {
        api.sinRed = true;
        await context.setOffline(true);
    });
    await expect(nube(page)).toHaveAttribute("data-forma", "sin_senal_pendientes");
    await expect(nube(page)).toHaveAttribute("data-tono", "warning");
    await expect(nube(page)).toHaveAccessibleName("Sin señal, 1 por enviar, toca para ver el estado");
    await expect(nube(page).locator('[data-slot="marca-indicador"]')).toHaveText("1");
    await expect(indicadores(page)).toHaveAttribute("data-actuar", "true");
    await abrirConexion(page);
    const hoja = page.getByRole("dialog", { name: "Conexión y envíos" });
    await expect(hoja).toContainText("Sin conexión");
    await expect(hoja).toContainText("1 pendiente por enviar");
    // Solo su sección: nada del interruptor de ubicación.
    await expect(hoja.getByRole("switch")).toHaveCount(0);
    // Sin señal no hay «Enviar ahora».
    await expect(hoja.getByRole("button", { name: "Enviar ahora" })).toHaveCount(0);
    // «Atrás» la cierra sin cambiar de vista.
    await page.goBack();
    await expect(hoja).toHaveCount(0);
    await expect(page).toHaveURL(/orden=101/);
    // Vuelve la señal: la nube se pone al día sola.
    api.sinRed = false;
    await context.setOffline(false);
    await expect(nube(page)).toHaveAttribute("data-forma", "al_dia", { timeout: 20_000 });
});

test("con señal y algo pendiente, «Enviar ahora» sincroniza; la X cierra la hoja", async ({ page }) => {
    // El motor reintenta solo tras el 503 (espera de 2 s, 4 s…) y el aviso «Nota guardada.
    // Enviando…» tapa la nube unos segundos: un reintento podía enviar justo al abrir la hoja y
    // «Enviar ahora» desaparecía bajo el clic. El servidor responde 503 hasta que se toca el botón,
    // así el envío es siempre el del botón.
    const api = await simularApiCampo(page, { permisos: PERMISOS });
    await simularChat(page);
    await abrirHoy(page);
    await page.route("**/api/v1/sync/lote", async (r) => {
        const tocado = await page.evaluate(() => document.documentElement.dataset.enviarAhora === "tocado").catch(() => false);
        await (tocado
            ? r.fallback()
            : r.fulfill({ status: 503, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:no_disponible", title: "No disponible", status: 503 }) }));
    });
    await agregarNota(page);
    await expect(nube(page)).toHaveAttribute("data-forma", "pendientes", { timeout: 15_000 });
    await expect(nube(page)).toHaveAttribute("data-tono", "marca");
    await abrirConexion(page);
    const hoja = page.getByRole("dialog", { name: "Conexión y envíos" });
    // Marca el toque (antes de que el botón envíe) para que el servidor falso deje pasar el lote.
    await page.evaluate(() => {
        document.addEventListener(
            "pointerdown",
            (e) => {
                if (e.target instanceof Element && e.target.closest("button")?.textContent?.includes("Enviar ahora")) {
                    document.documentElement.dataset.enviarAhora = "tocado";
                }
            },
            { capture: true },
        );
    });
    expect(api.lotes).toHaveLength(0);
    await hoja.getByRole("button", { name: "Enviar ahora" }).click();
    await expect.poll(() => api.lotes.length).toBeGreaterThan(0);
    await expect(nube(page)).toHaveAttribute("data-forma", "al_dia");
    await hoja.getByRole("button", { name: "Cerrar" }).click();
    await expect(hoja).toHaveCount(0);
});

test("el GPS abre solo «Ubicación»: el interruptor enciende y apaga el turno; X y «Atrás» cierran", async ({ page, context }) => {
    await simularApiCampo(page, { permisos: PERMISOS });
    await simularChat(page);
    await abrirHoy(page);
    await gps(page).click();
    const hoja = page.getByRole("dialog", { name: "Ubicación" });
    await expect(hoja.getByRole("switch", { name: /Compartir mi ubicación/ })).not.toBeChecked();
    await expect(hoja).not.toContainText("por enviar");
    await page.goBack();
    await expect(hoja).toHaveCount(0);

    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation(GUATE);
    await gps(page).click();
    await hoja.getByRole("switch", { name: /Compartir mi ubicación/ }).click({ force: true });
    // La primera vez, la explicación antes de que el navegador pregunte.
    await page.getByRole("dialog", { name: "¿Compartir tu ubicación?" }).getByRole("button", { name: "Permitir ubicación" }).click();
    await expect(gps(page)).toHaveAttribute("data-forma", "activa");
    await expect(gps(page)).toHaveAccessibleName("GPS activo, toca para ver el estado");
    await gps(page).click();
    await expect(hoja.getByRole("switch", { name: /Compartir mi ubicación/ })).toBeChecked();
    await hoja.getByRole("switch", { name: /Compartir mi ubicación/ }).click({ force: true });
    await expect(gps(page)).toHaveAttribute("data-forma", "apagada");
    await hoja.getByRole("button", { name: "Cerrar" }).click();
    await expect(hoja).toHaveCount(0);
});

test("la barra compacta lleva los indicadores; «Yo» de la isla abre la cuenta; «Por recibir» desde su mosaico", async ({ page }) => {
    await simularApiCampo(page, { permisos: PERMISOS });
    await simularChat(page);
    await abrirHoy(page);
    // Contenido suficiente para desplazar.
    await page.evaluate(() => {
        document.querySelector("main")?.insertAdjacentHTML("beforeend", '<div style="height:2000px"></div>');
    });
    await page.mouse.wheel(0, 900);
    await page.mouse.wheel(0, -200);
    const compacta = page.locator('[data-slot="barra-compacta"][data-visible="true"]');
    await expect(compacta).toBeVisible();
    await expect(compacta.locator('[data-slot="indicadores-compactos"] [data-indicador]')).toHaveCount(2);
    await compacta.locator('[data-indicador="sincronizacion"]').click();
    await expect(page.getByRole("dialog", { name: "Conexión y envíos" })).toBeVisible();
    await page.getByRole("dialog", { name: "Conexión y envíos" }).getByRole("button", { name: "Cerrar" }).click();

    await pestanaCampo(page, "Yo");
    await expect(page.getByRole("heading", { level: 1, name: TECNICO.nombre })).toBeVisible();
    await expect(isla(page).getByRole("link", { name: /^Yo · / })).toHaveAttribute("aria-current", "page");
    await pestanaCampo(page, "Hoy");
    await page.locator('[data-slot="mosaico-recibir"]').click();
    await expect(page.getByRole("heading", { name: "Por recibir" })).toBeVisible();
});

test("isla: la activa en «tinta» con su nombre, que se mueve al cambiar; el punto teal del chat se oculta en Chat", async ({ page }) => {
    await simularApiCampo(page, { permisos: PERMISOS });
    await simularChat(page, conUnMensaje);
    await abrirHoy(page);
    const punto = isla(page).locator('[data-slot="punto-no-leidos"]');
    await expect(punto).toHaveCount(1);
    await revisarIsla(page, "Hoy");
    await pestanaCampo(page, "Chat");
    await expect(page.getByRole("heading", { level: 1, name: "Chat del equipo" })).toBeVisible();
    await revisarIsla(page, "Chat");
    await expect(punto).toHaveCount(0);
    await pestanaCampo(page, "Agenda");
    await revisarIsla(page, "Agenda");
    await pestanaCampo(page, "Yo");
    await revisarIsla(page, "Yo");
    // Cabe en el teléfono sin salirse (margen a los lados).
    const caja = await isla(page).boundingBox();
    const ancho = await page.evaluate(() => document.documentElement.clientWidth);
    expect(caja && caja.x >= 8 && caja.x + caja.width <= ancho - 8).toBe(true);
});

test("capturas del encabezado y la hoja de estado (390 px)", async ({ page, context }, info) => {
    const carpeta = process.env.IRIS_CAPTURAS_CHAT ?? path.join("test-results", "capturas-chat");
    const tema = info.project.name.endsWith("oscuro") ? "oscuro" : "claro";
    const nombre = (n: string) => path.join(carpeta, `${n}-${tema}.png`);
    const api = await simularApiCampo(page, { permisos: PERMISOS });
    await simularChat(page, conUnMensaje);
    await abrirHoy(page);
    await page.screenshot({ path: nombre("10-hoy") });
    await agregarNota(page, async () => {
        api.sinRed = true;
        await context.setOffline(true);
    });
    await page.evaluate(() => {
        window.history.pushState(null, "", "/campo");
        window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await expect(nube(page)).toHaveAttribute("data-forma", "sin_senal_pendientes");
    // Sin el aviso de «Nota guardada» encima y con el encabezado a la vista.
    await expect(avisoCampo(page)).toHaveCount(0, { timeout: 10_000 });
    await page.evaluate(() => window.scrollTo({ top: 0 }));
    await page.screenshot({ path: nombre("11-indicadores-actuar") });
    await abrirConexion(page);
    await quieta(page);
    await page.screenshot({ path: nombre("12-hoja-conexion") });
    await page.getByRole("dialog", { name: "Conexión y envíos" }).getByRole("button", { name: "Cerrar" }).click();
    await gps(page).click();
    await expect(page.getByRole("dialog", { name: "Ubicación" })).toBeVisible();
    await quieta(page);
    await page.screenshot({ path: nombre("13-hoja-ubicacion") });
    await page.getByRole("dialog", { name: "Ubicación" }).getByRole("button", { name: "Cerrar" }).click();
    await context.setOffline(false);
    // La isla en cada sección (recorte con margen).
    for (const [seccion, archivo] of [["Hoy", "14-isla-hoy"], ["Chat", "15-isla-chat"], ["Yo", "16-isla-yo"]] as const) {
        await pestanaCampo(page, seccion);
        await expect(isla(page).locator('[data-activa="true"] [data-slot="nombre-pestana"]')).toHaveText(seccion);
        await quieta(page);
        const caja = await isla(page).boundingBox();
        if (caja) {
            await page.screenshot({ path: nombre(archivo), clip: { x: Math.max(0, caja.x - 12), y: caja.y - 12, width: caja.width + 24, height: caja.height + 24 } });
        }
    }
});
