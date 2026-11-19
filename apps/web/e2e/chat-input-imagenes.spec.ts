import path from "node:path";
import { expect, test, type Page, type Route } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { png, simularAdjuntos } from "./api-adjuntos-conv";
import { esTelefono } from "./ayudas";
import { AHORA_CONV, mensaje } from "./datos-conversaciones";

/*
 * Pedido del PM (nov. 2026): «el input del chat está desbordado, la visualización de imágenes no
 * se siente natural». El compositor de la bandeja (una fila en el teléfono, texto de 1 a 6 líneas
 * con tope, foco en la caja, chips solo con la caja vacía) no desplaza la página a lo ancho ni se
 * come el hilo, tampoco con el teclado abierto (390×600); las fotos toman su proporción antes de
 * cargar, van en cuadrícula si son seguidas, avisan si fallan con «Reintentar» y se abren en un
 * visor a pantalla completa (flechas, Esc y «Atrás» cierran sin salir de la conversación). En los
 * 4 proyectos.
 */

type Mensaje = Schema<"MensajeConversacion">;
type Api = Awaited<ReturnType<typeof simularApi>>;

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "chat-input-imagenes");
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });

const conv = (page: Page) => page.getByRole("region", { name: /^Conversación con / });
const hilo = (page: Page) => conv(page).getByRole("log", { name: "Mensajes" });
const compositor = (page: Page) => conv(page).locator('[data-slot="compositor"]');
const caja = (page: Page) => compositor(page).getByRole("textbox");
const enviar = (page: Page) => compositor(page).getByRole("button", { name: /^Enviar/ });

const LARGO = Array.from({ length: 14 }, (_, i) => `Línea ${i + 1}: el cliente reporta intermitencia desde el martes por la noche.`).join("\n");
const URL_LARGA = `Mira: https://soporte.ejemplo.com/diagnostico?router=ZTE-F660-AB12CD34EF56&token=${"eyJhbGciOiJIUzI1NiJ9".repeat(8)}`;

const id = (n: number) => `0193a000-0000-7000-8000-0000000fa${String(n).padStart(3, "0")}`;
const enMin = (min: number) => new Date(AHORA_CONV - min * 60_000).toISOString();

function foto(n: number, datos: Partial<Mensaje> & { ancho?: number | null; alto?: number | null; min: number; direccion?: "entrante" | "saliente" }): Mensaje {
    const { ancho = null, alto = null, min, direccion = "entrante", ...resto } = datos;
    return mensaje(41, {
        direccion,
        tipo: "imagen",
        creadoEn: enMin(min),
        estadoEntrega: direccion === "saliente" ? "leido" : null,
        adjunto: { id: id(n), estado: "completo", mime: "image/png", tamano: 48_000, urlDescarga: `/api/v1/adjuntos/${id(n)}`, ancho, alto },
        ...resto,
    });
}

/** Medidas reales de cada foto servida (`png()` de ese tamaño), y cuáles fallan. */
const SERVIDAS: Record<string, [number, number]> = {
    [id(1)]: [160, 90],
    [id(2)]: [90, 160],
    [id(3)]: [120, 90],
    [id(4)]: [80, 80],
    [id(5)]: [100, 100],
    [id(6)]: [120, 80],
    [id(7)]: [80, 120],
    [id(8)]: [160, 90],
};

async function abrir(page: Page, preparar?: (api: Api) => void, opciones: Parameters<typeof simularApi>[1] = {}, rutas?: (page: Page) => Promise<void>) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, opciones);
    await simularAdjuntos(page, {});
    await page.route(/\/api\/v1\/adjuntos\/0193a000-0000-7000-8000-0000000fa\d{3}$/, async (route: Route) => {
        const ruta = new URL(route.request().url()).pathname;
        const clave = ruta.slice(ruta.lastIndexOf("/") + 1);
        const [w, h] = SERVIDAS[clave] ?? [40, 30];
        await route.fulfill({ status: 200, contentType: "image/png", body: png(w, h) });
    });
    await rutas?.(page);
    preparar?.(api);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto("/conversaciones?c=41");
    await expect(caja(page)).toBeVisible();
    return api;
}

/** Ni la página ni el compositor se desplazan a lo ancho, y nada se sale de la caja. */
async function sinDesbordeHorizontal(page: Page) {
    await expect
        .poll(
            () =>
                page.evaluate(() => {
                    const fuera: string[] = [];
                    const doc = document.documentElement;
                    if (doc.scrollWidth > doc.clientWidth + 1) fuera.push(`página ${doc.scrollWidth} > ${doc.clientWidth}`);
                    const c = document.querySelector<HTMLElement>('[data-slot="compositor"]');
                    if (!c) return ["sin compositor"];
                    if (c.scrollWidth > c.clientWidth + 1) fuera.push(`compositor ${c.scrollWidth} > ${c.clientWidth}`);
                    const caja = c.getBoundingClientRect();
                    for (const el of c.querySelectorAll<HTMLElement>("*")) {
                        const r = el.getBoundingClientRect();
                        if (r.width === 0 || el.closest("[data-desborde-permitido]") || el.closest(".sr-only")) continue;
                        if (r.right > caja.right + 1 || r.left < caja.left - 1) fuera.push(`${el.tagName}.${el.dataset.slot ?? ""} ${Math.round(r.left)}–${Math.round(r.right)}`);
                    }
                    return fuera.slice(0, 5);
                }),
            { message: "nada se desplaza ni se sale a lo ancho" },
        )
        .toEqual([]);
}

const alto = (page: Page, sel: string) => page.locator(sel).first().evaluate((el) => Math.round(el.getBoundingClientRect().height));

test.describe("compositor de la bandeja", () => {
    test("texto largo, URL sin espacios y adjuntos: el texto crece con tope, se desplaza por dentro y nada desborda a lo ancho", async ({ page }) => {
        await abrir(page);
        await expect(compositor(page).locator('[data-slot="chips-respuestas"]')).toBeVisible();
        const vacio = await alto(page, '[data-slot="compositor"]');
        // Vacío: una fila de texto (en el teléfono, con «+» y «Enviar» al lado).
        expect(vacio).toBeLessThan(esTelefono(page) ? 140 : 190);
        await sinDesbordeHorizontal(page);

        await caja(page).fill(LARGO);
        // Con texto ya no hay chips de respuestas rápidas.
        await expect(compositor(page).locator('[data-slot="chips-respuestas"]')).toHaveCount(0);
        const t = await caja(page).evaluate((el) => ({ alto: el.clientHeight, contenido: el.scrollHeight, vh: innerHeight }));
        expect(t.contenido).toBeGreaterThan(t.alto);
        expect(t.alto).toBeLessThanOrEqual(Math.ceil(t.vh * 0.3) + 1);
        expect(t.alto).toBeLessThanOrEqual(176);
        await sinDesbordeHorizontal(page);
        await captura(page, "texto-largo");

        await caja(page).fill(URL_LARGA);
        await sinDesbordeHorizontal(page);
        await captura(page, "url-larga");

        await compositor(page)
            .locator('input[data-archivos="fotos"]')
            .setInputFiles(["uno", "dos", "tres", "cuatro"].map((n) => ({ name: `foto-${n}-con-un-nombre-bastante-largo.png`, mimeType: "image/png", buffer: png(60, 40) })));
        const tira = compositor(page).getByRole("group", { name: "Archivos por enviar" });
        await expect(tira.locator("[data-archivo]")).toHaveCount(4);
        // Las miniaturas van en una tira: la vista previa no crece hacia arriba con cada archivo.
        expect(await alto(page, '[data-slot="vista-previa-archivos"]')).toBeLessThan(110);
        await sinDesbordeHorizontal(page);
        await captura(page, "adjuntos");
    });

    test("teclado del teléfono (390×600): el hilo sigue a la vista y «Enviar» no se corta, con texto largo, adjuntos y la sugerencia", async ({ page }) => {
        test.skip(!esTelefono(page), "solo el teléfono");
        await abrir(page, undefined, { conversaciones: { asistente: {} } });
        await page.setViewportSize({ width: 390, height: 600 });
        await compositor(page)
            .locator('input[data-archivos="fotos"]')
            .setInputFiles([{ name: "uno.png", mimeType: "image/png", buffer: png(60, 40) }, { name: "dos.png", mimeType: "image/png", buffer: png(40, 60) }]);
        await caja(page).fill(LARGO);
        // Con poco alto, la sugerencia queda plegada en una línea.
        const sugerencia = conv(page).getByRole("region", { name: "Sugerencia del asistente" });
        await expect(sugerencia).toBeVisible();
        await expect(sugerencia.locator('[data-slot="cuerpo-sugerencia"]')).toHaveCount(0);
        // Plegada: el comienzo del texto junto al título (recortado si no cabe).
        await expect(sugerencia.locator('[data-slot="resumen-sugerencia"]')).toHaveCount(1);
        const medidas = await page.evaluate(() => {
            const log = document.querySelector<HTMLElement>('[role="log"][aria-label="Mensajes"]');
            const boton = document.querySelector<HTMLElement>('[data-slot="enviar-telefono"]');
            return { hilo: log?.clientHeight ?? 0, abajo: boton?.getBoundingClientRect().bottom ?? 9999, vh: innerHeight };
        });
        // Antes quedaba en 0 px y «Enviar» salía cortado.
        expect(medidas.hilo, "el hilo sigue a la vista (más de un quinto del alto)").toBeGreaterThanOrEqual(130);
        expect(medidas.abajo, "«Enviar» entero dentro de la pantalla").toBeLessThanOrEqual(medidas.vh);
        await expect(enviar(page)).toBeInViewport({ ratio: 1 });
        await sinDesbordeHorizontal(page);
        await captura(page, "teclado-390x600");
    });

    test("el foco se marca en toda la caja (no en el texto recortado); en el teléfono «+» despliega las herramientas", async ({ page }) => {
        await abrir(page);
        await caja(page).focus();
        await page.keyboard.type("Hola");
        const primario = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--hx-primary").trim());
        const hex = primario.replace("#", "");
        const rgb = `rgb(${parseInt(hex.slice(0, 2), 16)}, ${parseInt(hex.slice(2, 4), 16)}, ${parseInt(hex.slice(4, 6), 16)})`;
        const estilos = () =>
            page.evaluate(() => {
                const c = document.querySelector<HTMLElement>('[data-slot="caja-compositor"]');
                const t = c?.querySelector("textarea");
                return { borde: c ? getComputedStyle(c).borderTopColor : "", contorno: t ? getComputedStyle(t).outlineStyle : "" };
            });
        // El borde de la caja pasa a teal (con una transición corta); el texto no lleva contorno propio.
        await expect.poll(estilos).toEqual({ borde: rgb, contorno: "none" });
        const adjuntar = compositor(page).locator("[data-adjuntar]");
        if (esTelefono(page)) {
            await expect(adjuntar).toBeHidden();
            await compositor(page).getByRole("button", { name: "Adjuntar y más herramientas" }).click();
            await expect(adjuntar).toBeVisible();
            await expect(compositor(page).getByRole("button", { name: "Ocultar herramientas" })).toHaveAttribute("aria-expanded", "true");
        } else {
            await expect(adjuntar).toBeVisible();
            await expect(compositor(page).getByRole("button", { name: "Adjuntar y más herramientas" })).toBeHidden();
        }
        // «Enviar» va en teal (el color de la marca), no en tinta.
        await expect.poll(() => enviar(page).evaluate((el) => getComputedStyle(el).backgroundColor)).toBe(rgb);
        await captura(page, "foco-y-herramientas");
    });
});

test.describe("fotos en el hilo", () => {
    function hiloConFotos(api: Api, extra: Mensaje[] = []) {
        api.conversaciones.mensajes.set(41, [
            mensaje(41, { direccion: "entrante", texto: "Les mando fotos del módem.", creadoEn: enMin(60) }),
            foto(1, { ancho: 1600, alto: 900, min: 50 }),
            foto(2, { ancho: 900, alto: 1600, min: 40 }),
            // Sin medidas (WebP o mensaje anterior): reserva 4:3.
            foto(3, { ancho: null, alto: null, min: 30, texto: "Así quedó la luz LOS en rojo, ¿qué hago?" }),
            // Tres seguidas: en cuadrícula.
            foto(5, { ancho: 100, alto: 100, min: 20 }),
            foto(6, { ancho: 1200, alto: 800, min: 20 }),
            foto(7, { ancho: 800, alto: 1200, min: 20 }),
            foto(8, { ancho: 1600, alto: 900, min: 10, direccion: "saliente", autor: { id: 1, nombre: "Ana López" } }),
            ...extra,
            mensaje(41, { direccion: "entrante", texto: "Gracias", creadoEn: enMin(5) }),
        ]);
    }

    const proporcion = (page: Page, n: number) =>
        hilo(page)
            .locator(`[data-mensaje] [data-slot="foto-mensaje"]:has(img[src$="${id(n)}"]), [data-mensaje] [data-slot="foto-mensaje"]:has(img[src*="${id(n)}#"])`)
            .first()
            .evaluate((el) => {
                const r = el.getBoundingClientRect();
                return Math.round((r.width / r.height) * 100) / 100;
            });

    test("cada foto toma su proporción con máximo de ancho y alto, la hora va sobre la foto y el pie dentro de la burbuja", async ({ page }) => {
        await abrir(page, (api) => hiloConFotos(api));
        await expect.poll(() => proporcion(page, 1)).toBeCloseTo(1.78, 1);
        await expect.poll(() => proporcion(page, 2)).toBeCloseTo(0.56, 1);
        // Sin medidas: 4:3 reservado (y no se agranda ni se deforma).
        expect(await proporcion(page, 3)).toBeGreaterThan(0.5);
        const caja1 = await hilo(page).locator(`[data-slot="foto-mensaje"]:has(img[src$="${id(1)}"])`).evaluate((el) => el.getBoundingClientRect());
        expect(caja1.width).toBeLessThanOrEqual(321);
        const caja2 = await hilo(page).locator(`[data-slot="foto-mensaje"]:has(img[src$="${id(2)}"])`).evaluate((el) => el.getBoundingClientRect());
        expect(caja2.height).toBeLessThanOrEqual(341);
        // La hora sobre la foto cuando no hay texto; con texto, el pie va dentro de la burbuja.
        await expect(hilo(page).locator(`[data-slot="foto-mensaje"]:has(img[src$="${id(1)}"]) [data-slot="hora-foto"]`)).toBeVisible();
        const conPie = hilo(page).locator("[data-mensaje]").filter({ hasText: "Así quedó la luz LOS" });
        await expect(conPie.locator('[data-slot="hora-foto"]')).toHaveCount(0);
        await expect(conPie.locator('[data-slot="foto-mensaje"]')).toBeVisible();
        // Tres seguidas del cliente: una cuadrícula.
        const album = hilo(page).locator('[data-slot="album-fotos"]');
        await expect(album).toHaveCount(1);
        await expect(album).toHaveAttribute("data-fotos", "3");
        await expect(album.locator("img")).toHaveCount(3);
        await sinDesbordeHorizontal(page);
        await hilo(page).evaluate((el) => (el.scrollTop = 0));
        await captura(page, "fotos-arriba");
        await hilo(page).evaluate((el) => (el.scrollTop = el.scrollHeight));
        await captura(page, "fotos-final");
    });

    test("mientras carga, la foto ya ocupa su lugar (sin saltos); si falla, «No se pudo cargar la foto» con «Reintentar»", async ({ page }) => {
        let fallar = true;
        let soltar: () => void = () => undefined;
        const espera = new Promise<void>((r) => (soltar = r));
        await abrir(
            page,
            (api) =>
                api.conversaciones.mensajes.set(41, [
                    mensaje(41, { direccion: "entrante", texto: "Hola", creadoEn: enMin(30) }),
                    foto(1, { ancho: 1600, alto: 900, min: 20 }),
                    foto(2, { ancho: 900, alto: 1600, min: 10 }),
                    mensaje(41, { direccion: "entrante", texto: "¿Se ven?", creadoEn: enMin(5) }),
                ]),
            {},
            async (p) => {
                await p.route(new RegExp(`${id(1)}$`), async (route) => {
                    await espera;
                    await route.fulfill({ status: 200, contentType: "image/png", body: png(160, 90) });
                });
                await p.route(new RegExp(`${id(2)}$`), (route) => (fallar ? route.fulfill({ status: 500, body: "x" }) : route.fulfill({ status: 200, contentType: "image/png", body: png(90, 160) })));
            },
        );
        const cargando = hilo(page).locator('[data-slot="foto-mensaje"][data-estado-foto="cargando"]');
        await expect(cargando).toHaveCount(1);
        const antes = await cargando.evaluate((el) => ({ w: el.getBoundingClientRect().width, h: el.getBoundingClientRect().height }));
        expect(antes.w / antes.h).toBeCloseTo(1.78, 1);
        const error = hilo(page).locator('[data-slot="foto-mensaje"][data-estado-foto="error"]');
        await expect(error).toContainText("No se pudo cargar la foto");
        await captura(page, "fotos-cargando-y-error");
        const altoHilo = await hilo(page).evaluate((el) => el.scrollHeight);
        soltar();
        await expect(hilo(page).locator('[data-slot="foto-mensaje"][data-estado-foto="lista"]')).toHaveCount(1);
        // Cargar no movió nada: el espacio ya estaba reservado.
        expect(await hilo(page).evaluate((el) => el.scrollHeight)).toBe(altoHilo);
        fallar = false;
        await error.getByRole("button", { name: "Reintentar" }).click();
        await expect(hilo(page).locator('[data-slot="foto-mensaje"][data-estado-foto="lista"]')).toHaveCount(2);
    });

    test("visor a pantalla completa: flechas, «2 de N», descargar; Escape y «Atrás» lo cierran sin salir de la conversación", async ({ page }) => {
        await abrir(page, (api) => hiloConFotos(api));
        const primera = hilo(page).getByRole("button", { name: /^Ver foto de las/ }).first();
        await primera.click();
        const visor = page.getByRole("dialog", { name: /^Foto 1 de 7/ });
        await expect(visor).toBeVisible();
        await expect(visor.locator('[data-slot="visor-posicion"]')).toContainText("1 de 7");
        const descargar = visor.getByRole("link", { name: "Descargar" });
        await expect(descargar).toHaveAttribute("href", `/api/v1/adjuntos/${id(1)}`);
        await expect(descargar).toHaveAttribute("download", "");
        await captura(page, "visor");
        await page.keyboard.press("ArrowRight");
        await expect(page.getByRole("dialog", { name: /^Foto 2 de 7/ })).toBeVisible();
        if (!esTelefono(page)) {
            await page.getByRole("button", { name: "Foto siguiente" }).click();
            await expect(page.getByRole("dialog", { name: /^Foto 3 de 7/ })).toBeVisible();
            await expect(page.getByRole("dialog", { name: /^Foto 3 de 7/ })).toContainText("Así quedó la luz LOS");
        }
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog", { name: /^Foto / })).toHaveCount(0);
        await expect(primera).toBeFocused();
        // «Atrás» del navegador (o del teléfono) cierra el visor y deja la conversación abierta.
        await primera.click();
        await expect(page.getByRole("dialog", { name: /^Foto 1 de 7/ })).toBeVisible();
        await page.goBack();
        await expect(page.getByRole("dialog", { name: /^Foto / })).toHaveCount(0);
        await expect(page).toHaveURL(/\/conversaciones\?c=41$/);
        await expect(caja(page)).toBeVisible();
        // Y cerrar con la «X» no deja una entrada de más: «Atrás» sale de verdad después.
        await primera.click();
        await page.getByRole("dialog", { name: /^Foto 1 de 7/ }).getByRole("button", { name: "Cerrar" }).click();
        await expect(page.getByRole("dialog", { name: /^Foto / })).toHaveCount(0);
        expect(await page.evaluate(() => Boolean((history.state as Record<string, unknown> | null)?.["iris.visor-fotos"]))).toBe(false);
    });
});

test.describe("chat del equipo", () => {
    test("el texto crece con tope y se desplaza por dentro; la página no se desplaza a lo ancho", async ({ page }) => {
        await simularApi(page, { chat: true });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await page.locator('[data-slot="boton-chat-equipo"]').click();
        await page.getByRole("dialog", { name: "Chat del equipo" }).locator("[data-conversacion-chat]").first().click();
        const area = page.getByRole("combobox", { name: /Mensaje para/ });
        await expect(area).toBeVisible();
        const vacio = await area.evaluate((el) => el.clientHeight);
        await area.fill(LARGO);
        const t = await area.evaluate((el) => ({ alto: el.clientHeight, contenido: el.scrollHeight, vh: innerHeight, contorno: getComputedStyle(el).outlineStyle }));
        expect(t.alto).toBeGreaterThan(vacio);
        expect(t.contenido).toBeGreaterThan(t.alto);
        expect(t.alto).toBeLessThanOrEqual(Math.ceil(t.vh * 0.3) + 1);
        expect(t.contorno).toBe("none");
        await area.fill(URL_LARGA);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true);
        await captura(page, "chat-equipo-largo");
    });
});
