import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { errorHttp, type ServidorChatFalso } from "@iris/sync/pruebas";
import { conSesionTecnico, simularApiCampo, type OpcionesCampo } from "./api-campo";
import { ANDREA, LUIS, simularChat } from "./api-chat";
import { PERMISOS_TECNICO, TECNICO } from "./datos-campo";

/*
 * Chat interno de la app del técnico (`contratos/chat.md`, tablero C-Chat), en teléfono y
 * escritorio, claro y oscuro: lista, conversación con «visto», envío sin señal que sobrevive a
 * recargar, «no enviado — Reintentar», eliminar con el menú del mensaje, chat de la orden, nueva
 * directa y texto plano. Con IRIS_CAPTURAS_CHAT=<carpeta> deja las capturas para el PM.
 */

const CUADRILLA = 40;
const DIRECTA = 41;
const PERMISOS_CHAT = [...PERMISOS_TECNICO, "chat.ver", "chat.enviar"] as OpcionesCampo["permisos"];

test.beforeEach(async ({ context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "");
});

/** Cuadrilla Norte con dos mensajes de Andrea (uno sin leer) y una directa con Luis. */
function conversacionesBase(s: ServidorChatFalso) {
    s.crearConversacion({ id: CUADRILLA, tipo: "cuadrilla", titulo: "Cuadrilla Norte", cuadrilla_id: 3 });
    s.crearConversacion({ id: DIRECTA, tipo: "directa", titulo: LUIS.nombre, otro_usuario: LUIS });
    s.recibir(DIRECTA, LUIS, "¿Tienes conectores SC/APC?");
    s.recibir(CUADRILLA, ANDREA, "Buenos días, equipo.");
    s.recibir(CUADRILLA, ANDREA, "La caja NAP 14 tiene la potencia baja, revísenla.");
}

async function montar(page: Page, preparar: (s: ServidorChatFalso) => void = conversacionesBase, opciones: OpcionesCampo = {}) {
    const campo = await simularApiCampo(page, { permisos: PERMISOS_CHAT, ...opciones });
    const chat = await simularChat(page, preparar);
    return { campo, chat };
}

const hilo = (page: Page) => page.locator('[data-slot="hilo-chat"]');
const burbujas = (page: Page) => hilo(page).locator('[data-slot="mensaje-chat"]');
const compositor = (page: Page) => page.getByRole("textbox", { name: "Mensaje" });

async function abrirConversacion(page: Page, nombre: RegExp) {
    await page.goto("/campo?vista=chat");
    await page.getByRole("link", { name: nombre }).click();
    await expect(hilo(page)).toBeVisible();
}

async function escribir(page: Page, texto: string) {
    await compositor(page).fill(texto);
    await page.getByRole("button", { name: "Enviar" }).click();
}

test("«Yo» lleva al chat con los no leídos y la lista muestra vista previa, hora y contador", async ({ page }) => {
    await montar(page);
    await page.goto("/campo?vista=yo");
    const entrada = page.getByRole("button", { name: /^Chat del equipo \(3 sin leer\)$/ });
    await expect(entrada).toBeVisible();
    await entrada.click();
    await expect(page.getByRole("heading", { level: 1, name: "Chat del equipo" })).toBeVisible();
    const cuadrilla = page.getByRole("link", { name: "Cuadrilla Norte, 2 sin leer" });
    await expect(cuadrilla).toContainText("Andrea: La caja NAP 14 tiene la potencia baja");
    await expect(cuadrilla).toContainText(/\d{2}:\d{2}/);
    await expect(page.getByRole("link", { name: `${LUIS.nombre}, 1 sin leer` })).toContainText("¿Tienes conectores SC/APC?");
    // Los hilos de las órdenes del día se abren solos (idempotente): aparecen aunque no tengan mensajes.
    await expect(page.getByRole("link", { name: "OT-000101" })).toContainText("Sin mensajes todavía");
    // La isla no cambia mientras el PM decide (sigue con sus cuatro secciones y el «+»).
    await expect(page.getByRole("navigation", { name: "Secciones de la app" }).getByRole("link", { name: "Agenda" })).toBeVisible();
});

test("sin conversaciones: estado vacío con «Nuevo mensaje»", async ({ page }) => {
    await montar(page, () => undefined, { ordenes: [] });
    await page.goto("/campo?vista=chat");
    await expect(page.getByText("Todavía no tienes conversaciones")).toBeVisible();
    await expect(page.getByRole("button", { name: "Nuevo mensaje" })).toBeVisible();
});

test("conversación: de otros con autor, propios en teal, separador de día, leído y «visto»", async ({ page }) => {
    const { chat } = await montar(page);
    await abrirConversacion(page, /^Cuadrilla Norte/);
    await expect(hilo(page).locator('[data-slot="separador-dia"]').first()).toHaveText("Hoy");
    await expect(burbujas(page).first()).toContainText(ANDREA.nombre);
    await expect(burbujas(page)).toHaveCount(2);
    // Mostrar el último mensaje lo marca leído (con el id del último recibido).
    await expect.poll(() => chat.peticiones.filter((p) => p.ruta === `/conversaciones/${CUADRILLA}/leer`).length).toBeGreaterThan(0);

    await escribir(page, "Voy para allá, llego en 20 min.");
    const mio = burbujas(page).last();
    await expect(mio).toHaveAttribute("data-propio", "true");
    await expect(mio).toContainText(/· enviado$/);
    const enviado = chat.servidor.mensajes.get(CUADRILLA)?.at(-1);
    expect(enviado?.texto).toBe("Voy para allá, llego en 20 min.");
    expect(enviado?.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7/);
    // Andrea lo lee: llega por el flujo (o el sondeo) y la burbuja dice «visto».
    chat.servidor.leyo(CUADRILLA, ANDREA, enviado?.id ?? "");
    await expect(mio).toContainText(/· visto$/, { timeout: 20_000 });
    await expect(compositor(page)).toHaveValue("");
});

test("en tiempo real llega el mensaje de otro sin recargar", async ({ page }) => {
    const { chat } = await montar(page);
    await abrirConversacion(page, /^Cuadrilla Norte/);
    chat.servidor.recibir(CUADRILLA, ANDREA, "¿Ya llegaste?");
    await expect(burbujas(page).last()).toContainText("¿Ya llegaste?", { timeout: 20_000 });
});

test("sin señal: se ve «se envía al tener señal» y sale al volver la señal, en orden", async ({ page, context }) => {
    const { chat } = await montar(page);
    await abrirConversacion(page, /^Cuadrilla Norte/);
    await context.setOffline(true);
    await escribir(page, "Primero sin señal");
    await escribir(page, "Segundo sin señal");
    await expect(burbujas(page).last()).toContainText("se envía al tener señal");
    await expect(burbujas(page).nth(-2)).toContainText("se envía al tener señal");
    await context.setOffline(false);
    await expect(burbujas(page).last()).toContainText(/· (enviado|visto)$/, { timeout: 20_000 });
    expect(chat.servidor.mensajes.get(CUADRILLA)?.slice(-2).map((m) => m.texto)).toEqual(["Primero sin señal", "Segundo sin señal"]);
});

test("lo escrito sobrevive a cerrar la app: se recarga y sale con el mismo id", async ({ page }) => {
    const { chat } = await montar(page);
    await abrirConversacion(page, /^Cuadrilla Norte/);
    chat.cortarEnvios = 1000;
    await escribir(page, "Escrito antes de cerrar");
    await expect(burbujas(page).last()).toContainText("Escrito antes de cerrar");
    await page.reload();
    await expect(burbujas(page).last()).toContainText("Escrito antes de cerrar");
    chat.cortarEnvios = 0;
    await expect(burbujas(page).last()).toContainText(/· enviado$/, { timeout: 30_000 });
    expect(chat.servidor.mensajes.get(CUADRILLA)?.filter((m) => m.texto === "Escrito antes de cerrar")).toHaveLength(1);
});

test("422: «no enviado — Reintentar» y al reintentar sale", async ({ page }) => {
    const { chat } = await montar(page);
    await abrirConversacion(page, /^Cuadrilla Norte/);
    chat.servidor.fallar("enviarMensaje", errorHttp(422, "mensaje_vacio"));
    await escribir(page, "Este falla una vez");
    const fallido = burbujas(page).last();
    await expect(fallido).toHaveAttribute("data-envio", "fallido");
    await expect(fallido).toContainText("no enviado");
    await fallido.getByRole("button", { name: "No enviado — Reintentar" }).click();
    await expect(burbujas(page).last()).toContainText(/· enviado$/);
});

test("eliminar el propio desde el menú del mensaje deja «Mensaje eliminado»", async ({ page }) => {
    const { chat } = await montar(page);
    await abrirConversacion(page, /^Cuadrilla Norte/);
    await escribir(page, "Me equivoqué de chat");
    const mio = burbujas(page).last();
    await expect(mio).toContainText(/· enviado$/);
    // Mantener presionado (o clic derecho en escritorio) abre el menú.
    await mio.locator('[aria-haspopup="dialog"]').click({ button: "right" });
    await page.getByRole("dialog", { name: "Mensaje" }).getByRole("button", { name: "Eliminar para todos" }).click();
    await expect(burbujas(page).last()).toContainText("Mensaje eliminado");
    await expect(burbujas(page).last()).not.toContainText("Me equivoqué");
    await expect.poll(() => chat.servidor.llamadasA("eliminarMensaje").length).toBe(1);
    // Los de otros no se pueden eliminar.
    await burbujas(page).first().locator('[aria-haspopup="dialog"]').click({ button: "right" });
    await expect(page.getByRole("dialog", { name: "Mensaje" }).getByRole("button", { name: "Eliminar para todos" })).toHaveCount(0);
});

test("el texto es plano: HTML no se interpreta y solo http(s) es enlace", async ({ page }) => {
    await montar(page, (s) => {
        conversacionesBase(s);
        s.recibir(CUADRILLA, ANDREA, '<img src=x onerror="window.__xss=1"> mira https://iris.gt/manual. javascript:alert(1)');
    });
    await abrirConversacion(page, /^Cuadrilla Norte/);
    const b = burbujas(page).last();
    await expect(b).toContainText('<img src=x onerror="window.__xss=1">');
    await expect(b.locator("img")).toHaveCount(0);
    await expect(b.getByRole("link")).toHaveAttribute("href", "https://iris.gt/manual");
    await expect(b.getByRole("link")).toHaveAttribute("rel", /noopener/);
    expect(await page.evaluate(() => (window as unknown as { __xss?: number }).__xss)).toBeUndefined();
});

test("«Abrir chat de la orden» desde el detalle abre su hilo", async ({ page }) => {
    const { chat } = await montar(page);
    await page.goto("/campo?orden=101");
    await page.getByRole("button", { name: /Abrir chat de la orden/ }).click();
    await expect(page.getByRole("heading", { level: 1, name: "OT-000101" })).toBeVisible();
    await expect(page.getByText("Chat de la orden con despacho · el cliente no lo ve")).toBeVisible();
    await expect(compositor(page)).toHaveAttribute("placeholder", "Escribe a despacho…");
    expect(chat.servidor.llamadasA("abrirOrden").map(([id]) => id)).toContain(101);
});

test("«Nuevo mensaje» abre una directa con alguien conocido", async ({ page }) => {
    const { chat } = await montar(page);
    await page.goto("/campo?vista=chat");
    await page.getByRole("button", { name: "Nuevo mensaje" }).click();
    await page.getByRole("button", { name: ANDREA.nombre }).click();
    await expect(page.getByRole("heading", { level: 1, name: ANDREA.nombre })).toBeVisible();
    expect(chat.servidor.llamadasA("abrirDirecta")).toEqual([[ANDREA.id]]);
    await escribir(page, "Hola Andrea");
    await expect(burbujas(page).last()).toContainText(/· enviado$/);
});

test("cerrar sesión borra la caché del chat del teléfono", async ({ page }) => {
    await montar(page);
    await abrirConversacion(page, /^Cuadrilla Norte/);
    await page.goto("/campo?vista=yo");
    await page.getByRole("button", { name: "Cerrar sesión" }).click();
    await page.waitForURL(/entrar=1/);
    const restantes = await page.evaluate(async (nombre) => {
        const abierta = await new Promise<IDBDatabase>((ok, mal) => {
            const r = indexedDB.open(nombre);
            r.onsuccess = () => ok(r.result);
            r.onerror = () => mal(r.error);
        });
        const nombres = [...abierta.objectStoreNames];
        abierta.close();
        return nombres;
    }, `iris-campo-${TECNICO.id}-0`);
    // La base se borró entera (o quedó sin tablas): no hay mensajes que leer sin entrar.
    expect(restantes.includes("chatMensajes") ? "con-tabla" : "sin-tabla").toBe("sin-tabla");
});

test("capturas del chat (390 px)", async ({ page, context }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "Capturas del teléfono.");
    const carpeta = process.env.IRIS_CAPTURAS_CHAT ?? path.join("test-results", "capturas-chat");
    const tema = info.project.name.endsWith("oscuro") ? "oscuro" : "claro";
    const nombre = (n: string) => path.join(carpeta, `${n}-${tema}.png`);
    const { chat } = await montar(page);
    await page.goto("/campo?vista=chat");
    await expect(page.getByRole("link", { name: /^Cuadrilla Norte/ })).toBeVisible();
    await page.screenshot({ path: nombre("01-lista") });

    await page.getByRole("link", { name: /^Cuadrilla Norte/ }).click();
    await escribir(page, "Voy para allá, llego en 20 min.");
    await expect(burbujas(page).last()).toContainText(/· enviado$/);
    chat.servidor.leyo(CUADRILLA, ANDREA, chat.servidor.mensajes.get(CUADRILLA)?.at(-1)?.id ?? "");
    await expect(burbujas(page).last()).toContainText(/· visto$/, { timeout: 20_000 });
    chat.servidor.recibir(CUADRILLA, ANDREA, "Perfecto, avísame la potencia cuando cambies el conector.");
    await expect(burbujas(page).last()).toContainText("Perfecto", { timeout: 20_000 });
    await page.screenshot({ path: nombre("02-conversacion-visto") });

    chat.servidor.fallar("enviarMensaje", errorHttp(422, "mensaje_vacio"));
    await escribir(page, "Este no salió");
    await expect(burbujas(page).last()).toHaveAttribute("data-envio", "fallido");
    await context.setOffline(true);
    await escribir(page, "Listo, −21 dBm. Ya navega.");
    await expect(burbujas(page).last()).toContainText("se envía al tener señal");
    await page.screenshot({ path: nombre("03-enviando-y-fallido") });

    // Teclado abierto: la ventana se encoge (`interactive-widget=resizes-content`) y el compositor queda arriba.
    await page.setViewportSize({ width: 390, height: 520 });
    await compositor(page).focus();
    await compositor(page).fill("Necesito dos conectores");
    await expect(compositor(page)).toBeInViewport();
    await page.screenshot({ path: nombre("04-compositor-teclado") });
    await context.setOffline(false);
});
