import path from "node:path";
import { expect, test, type Locator, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { png, PDF, simularAdjuntos, type AdjuntosFalsos } from "./api-adjuntos-conv";
import { esTelefono } from "./ayudas";
import { escribirFecha } from "./controles";
import { AHORA_CONV, mensaje } from "./datos-conversaciones";

/*
 * Pulido de la bandeja de conversaciones (lo que reportó el PM usando Iris real): el hilo abre y se
 * queda en el último mensaje, el campo para escribir no se desborda, las etiquetas y demás chips no
 * se salen de su contenedor, y adjuntar archivos con vista previa antes de enviar. En los 4
 * proyectos (escritorio y teléfono, claro y oscuro).
 */

type Mensaje = Schema<"MensajeConversacion">;
type Api = Awaited<ReturnType<typeof simularApi>>;

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "bandeja-pulido");
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });
const ID_FOTO = "0193a000-0000-7000-8000-00000000f070";
const ULTIMO = "Este es el último mensaje del hilo.";

type Preparar = (api: Api) => void;

async function abrir(
    page: Page,
    preparar?: Preparar,
    opciones: Parameters<typeof simularApi>[1] = {},
    adjuntos: Parameters<typeof simularAdjuntos>[1] = {},
    /** Rutas propias de la prueba: se registran al final para que ganen a la API falsa. */
    antesDeIr?: (page: Page, api: Api) => Promise<void>,
): Promise<{ api: Api; adjuntos: AdjuntosFalsos }> {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, opciones);
    const falsos = await simularAdjuntos(page, adjuntos);
    preparar?.(api);
    await antesDeIr?.(page, api);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto("/conversaciones?c=41");
    return { api, adjuntos: falsos };
}

const conv = (page: Page) => page.getByRole("region", { name: /^Conversación con / });
const hilo = (page: Page) => conv(page).getByRole("log", { name: "Mensajes" });
const compositor = (page: Page) => conv(page).locator('[data-slot="compositor"]');
const caja = (page: Page) => compositor(page).getByRole("textbox");

/** Un hilo largo: 40 mensajes, una foto cerca del final (que carga tarde) y el último bien marcado. */
function hiloLargo(api: Api, extra: Mensaje[] = []) {
    const lista: Mensaje[] = [];
    for (let i = 1; i <= 38; i++) {
        lista.push(mensaje(41, i % 2 ? { direccion: "entrante", texto: `Mensaje ${i} del cliente, con algo de texto para que ocupe.` } : { direccion: "saliente", texto: `Respuesta ${i} del agente.`, estadoEntrega: "leido" }));
    }
    lista.push(mensaje(41, { direccion: "entrante", tipo: "imagen", adjunto: { id: ID_FOTO, estado: "completo", mime: "image/png", tamano: 48_213, urlDescarga: `/api/v1/adjuntos/${ID_FOTO}` } }));
    lista.push(mensaje(41, { direccion: "entrante", texto: ULTIMO }));
    api.conversaciones.mensajes.set(41, [...lista, ...extra]);
}

/** Cuánto falta para el final del hilo, en px (0 = pegado abajo). */
const faltaParaElFinal = (page: Page) => hilo(page).evaluate((el) => Math.round(el.scrollHeight - el.scrollTop - el.clientHeight));

/** Nada dentro de `zona` excede su ancho: ni su contenido (`scrollWidth`) ni la caja de un descendiente visible. */
async function sinDesbordeHorizontal(zona: Locator, que: string) {
    await expect
        .poll(
            () =>
                zona.evaluate((el) => {
                    const fuera: string[] = [];
                    if (el.scrollWidth > el.clientWidth + 1) fuera.push(`scrollWidth ${el.scrollWidth} > clientWidth ${el.clientWidth}`);
                    const caja = el.getBoundingClientRect();
                    for (const hijo of el.querySelectorAll<HTMLElement>("*")) {
                        const r = hijo.getBoundingClientRect();
                        if (r.width === 0 || r.height === 0) continue;
                        // Lo que se desplaza a propósito (con su máscara) no cuenta.
                        if (hijo.closest("[data-desborde-permitido]") || hijo.closest(".sr-only")) continue;
                        if (r.right > caja.right + 1 || r.left < caja.left - 1) fuera.push(`${hijo.tagName.toLowerCase()}[${hijo.dataset.slot ?? hijo.className.toString().slice(0, 30)}] ${Math.round(r.left)}–${Math.round(r.right)} fuera de ${Math.round(caja.left)}–${Math.round(caja.right)}`);
                        if (hijo.scrollWidth > hijo.clientWidth + 4 && getComputedStyle(hijo).overflowX === "visible" && getComputedStyle(hijo).display !== "inline") fuera.push(`${hijo.tagName.toLowerCase()} se sale: scrollWidth ${hijo.scrollWidth} > clientWidth ${hijo.clientWidth}`);
                    }
                    return fuera.slice(0, 6);
                }),
            { message: `${que}: nada excede el ancho de su contenedor`, timeout: 4_000 },
        )
        .toEqual([]);
}

const sinScrollDePagina = (page: Page) => expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), { message: "la página no se desplaza a lo ancho" }).toBe(true);

test.describe("bandeja › el hilo abre y se queda en el último mensaje", () => {
    test("al abrir una conversación larga se ve el último mensaje, también después de que cargan la foto y la sugerencia", async ({ page }) => {
        await abrir(page, (api) => hiloLargo(api), {}, { demoraDescargaMs: 900 });
        await expect(hilo(page).getByText(ULTIMO)).toBeVisible();
        // La foto llega tarde y cambia la altura; la tarjeta del asistente y el compositor también le quitan alto al hilo.
        await expect(hilo(page).locator("img")).toHaveJSProperty("complete", true);
        await expect.poll(async () => hilo(page).locator("img").evaluate((img: HTMLImageElement) => img.naturalHeight)).toBeGreaterThan(0);
        await expect(compositor(page)).toBeVisible();
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
        await expect(hilo(page).getByText(ULTIMO)).toBeInViewport({ ratio: 1 });
        await captura(page, "hilo-abre-al-final");
    });

    test("si estás abajo, lo que llega y lo que envías te deja abajo", async ({ page }) => {
        const { api } = await abrir(page, (a) => hiloLargo(a));
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
        api.conversaciones.mensajeDelCliente(41, "Mensaje nuevo del cliente, recién llegado.");
        await expect(hilo(page).getByText("Mensaje nuevo del cliente, recién llegado.")).toBeInViewport({ ratio: 1 });
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
        await expect(conv(page).locator('[data-slot="mensajes-nuevos"]')).toHaveCount(0);

        await caja(page).fill("Respuesta mía que debe quedar a la vista.");
        await caja(page).press("Enter");
        await expect(hilo(page).getByText("Respuesta mía que debe quedar a la vista.")).toBeInViewport({ ratio: 1 });
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
    });

    test("si subiste a leer, no salta: avisa «Mensajes nuevos» y al pulsarlo baja", async ({ page }) => {
        const { api } = await abrir(page, (a) => hiloLargo(a));
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
        await hilo(page).evaluate((el) => el.scrollTo({ top: 200 }));
        await expect.poll(() => faltaParaElFinal(page)).toBeGreaterThan(300);
        const antes = await hilo(page).evaluate((el) => el.scrollTop);
        api.conversaciones.mensajeDelCliente(41, "Llegó mientras leías arriba.");
        const aviso = conv(page).locator('[data-slot="mensajes-nuevos"]');
        await expect(aviso).toBeVisible();
        await expect(aviso).toContainText("Mensajes nuevos");
        // No saltó.
        expect(Math.abs((await hilo(page).evaluate((el) => el.scrollTop)) - antes)).toBeLessThanOrEqual(2);
        await expect(hilo(page).getByText("Llegó mientras leías arriba.")).not.toBeInViewport();
        await captura(page, "hilo-mensajes-nuevos");
        await aviso.click();
        await expect(hilo(page).getByText("Llegó mientras leías arriba.")).toBeInViewport({ ratio: 1 });
        await expect(aviso).toHaveCount(0);
    });

    test("al cargar mensajes anteriores la vista no salta", async ({ page }) => {
        // La API pagina: primero los 20 últimos, luego los anteriores.
        await abrir(page, (a) => hiloLargo(a), {}, {}, async (p, api) => {
            await p.route("**/api/v1/mensajeria/conversaciones/41/mensajes*", async (route) => {
                if (route.request().method() !== "GET") return route.fallback();
                const todo = api.conversaciones.mensajes.get(41) ?? [];
                const conCursor = new URL(route.request().url()).searchParams.get("cursor");
                const data = conCursor ? todo.slice(0, -20) : todo.slice(-20);
                await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ data, nextCursor: conCursor ? null : "antes" }) });
            });
        });
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
        await hilo(page).evaluate((el) => el.scrollTo({ top: 0 }));
        const ancla = hilo(page).getByText("Mensaje 21 del cliente", { exact: false });
        await expect(ancla).toBeInViewport();
        const antes = (await ancla.boundingBox())?.y ?? 0;
        await hilo(page).getByRole("button", { name: "Ver mensajes anteriores" }).click();
        await expect(hilo(page).getByText("Mensaje 1 del cliente", { exact: false })).toBeAttached();
        // El mensaje que estabas leyendo sigue en el mismo sitio.
        await expect.poll(async () => Math.abs(((await ancla.boundingBox())?.y ?? 0) - antes)).toBeLessThanOrEqual(4);
        await expect(conv(page).locator('[data-slot="mensajes-nuevos"]')).toHaveCount(0);
    });

    test("al cambiar de conversación, la otra también abre al final", async ({ page }) => {
        await abrir(page, (a) => {
            hiloLargo(a);
            const otra: Mensaje[] = [];
            for (let i = 1; i <= 30; i++) otra.push(mensaje(42, { direccion: "entrante", texto: `Mensaje ${i} de María, con texto suficiente para llenar.` }));
            otra.push(mensaje(42, { direccion: "entrante", texto: "Lo último que dijo María." }));
            a.conversaciones.mensajes.set(42, otra);
        });
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
        // Subo en esta y paso a la otra: no hereda la posición.
        await hilo(page).evaluate((el) => el.scrollTo({ top: 0 }));
        // Sin recargar la página: por la lista, como lo hace una persona.
        if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();
        await page.locator('[data-conversacion="42"]').click();
        await expect(hilo(page).getByText("Lo último que dijo María.")).toBeInViewport({ ratio: 1 });
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
    });
});

test.describe("bandeja › el campo para escribir no se desborda", () => {
    test("una sola palabra larguísima no ensancha nada y los botones siguen a la vista (con todos los botones: también «Sugerir»)", async ({ page }) => {
        // Con el asistente en modo botón la barra lleva un botón más: es cuando menos sitio hay.
        await abrir(page, undefined, { conversaciones: { asistente: { estado: { modo: "boton" }, inicial: null } } });
        await expect(compositor(page).getByRole("button", { name: "Sugerir" })).toBeVisible();
        await sinDesbordeHorizontal(compositor(page), "compositor vacío");
        await expect(compositor(page).getByRole("button", { name: /^Enviar/ })).toBeInViewport({ ratio: 1 });
        await caja(page).fill("https://ejemplo.gt/" + "a".repeat(320));
        await sinDesbordeHorizontal(compositor(page), "compositor con una palabra larga");
        await sinScrollDePagina(page);
        await expect(compositor(page).getByRole("button", { name: /^Enviar/ })).toBeInViewport({ ratio: 1 });
        await expect(compositor(page).locator("[data-adjuntar]")).toBeInViewport({ ratio: 1 });
        await captura(page, "compositor-palabra-larga");
    });

    test("con muchas líneas crece hasta un máximo y luego se desplaza por dentro, sin tapar los botones", async ({ page }) => {
        await abrir(page);
        const vacio = (await caja(page).boundingBox())?.height ?? 0;
        await caja(page).fill(Array.from({ length: 4 }, (_, i) => `Línea ${i + 1}`).join("\n"));
        await expect.poll(async () => (await caja(page).boundingBox())?.height ?? 0).toBeGreaterThan(vacio + 20);
        await caja(page).fill(Array.from({ length: 40 }, (_, i) => `Línea ${i + 1} de un mensaje largo`).join("\n"));
        const alto = (await caja(page).boundingBox())?.height ?? 0;
        expect(alto).toBeLessThanOrEqual(esTelefono(page) ? 170 : 210);
        expect(await caja(page).evaluate((el) => el.scrollHeight > el.clientHeight + 4)).toBe(true);
        await sinDesbordeHorizontal(compositor(page), "compositor con muchas líneas");
        await expect(compositor(page).getByRole("button", { name: /^Enviar/ })).toBeInViewport({ ratio: 1 });
        // El hilo no desaparece debajo del compositor.
        expect((await hilo(page).boundingBox())?.height ?? 0).toBeGreaterThan(120);
        await sinScrollDePagina(page);
        await captura(page, "compositor-muchas-lineas");
        // Al borrar vuelve a su alto de siempre.
        await caja(page).fill("");
        await expect.poll(async () => Math.round((await caja(page).boundingBox())?.height ?? 0)).toBe(Math.round(vacio));
    });

    test("con la ventana de 24 h cerrada tampoco se desborda", async ({ page }) => {
        await abrir(page, (api) => {
            const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
            if (c) c.ventana = { abierta: false, cierraEn: null };
        });
        await expect(compositor(page).locator('[data-slot="solo-plantilla"]')).toBeVisible();
        await sinDesbordeHorizontal(compositor(page), "compositor con la ventana cerrada");
        await sinScrollDePagina(page);
        await captura(page, "compositor-ventana-cerrada");
    });

    test("en nota interna, con menciones y texto largo, tampoco", async ({ page }) => {
        await abrir(page);
        await compositor(page).getByRole("radio", { name: "Nota interna" }).click();
        await caja(page).fill("Nota " + "b".repeat(280) + "\n".repeat(12) + "fin");
        await sinDesbordeHorizontal(compositor(page), "compositor en nota interna");
        await expect(compositor(page).getByRole("button", { name: "Guardar nota" })).toBeInViewport({ ratio: 1 });
        await sinScrollDePagina(page);
    });
});

const LARGA = "Cliente con historial de reclamos por intermitencia en horario nocturno desde octubre";
const MUCHAS: Schema<"EtiquetaConversacion">[] = [
    { id: 3, nombre: "Sin servicio", color: "peligro" },
    { id: 4, nombre: "Cobros", color: "alerta" },
    { id: 5, nombre: "Ventas", color: "exito" },
    { id: 6, nombre: "Falla masiva", color: "peligro" },
    { id: 7, nombre: LARGA, color: "info" },
    { id: 8, nombre: "VIP", color: "neutro" },
    { id: 9, nombre: "Reincidente", color: "alerta" },
    { id: 10, nombre: "Instalación pendiente", color: "info" },
    { id: 11, nombre: "Supercalifragilisticoespialidoso-sin-espacios-para-partir", color: "neutro" },
    { id: 12, nombre: "Cambio de plan", color: "exito" },
    { id: 13, nombre: "Zona 4", color: "neutro" },
    { id: 14, nombre: "Corporativo", color: "info" },
].map((e) => ({ activa: true, version: 1, ...e })) as Schema<"EtiquetaConversacion">[];

function conMuchasEtiquetas(api: Api) {
    // El catálogo (filtros y editor) y las de la conversación.
    api.conversaciones.etiquetas.splice(0, api.conversaciones.etiquetas.length, ...MUCHAS);
    const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
    if (c) c.etiquetas = MUCHAS.map(({ id, nombre, color }) => ({ id, nombre, color }));
}

test.describe("bandeja › etiquetas y chips no se desbordan", () => {
    test("en la ficha del cliente: muchas etiquetas envuelven en varias líneas y la de nombre largo se recorta con su nombre completo accesible", async ({ page }) => {
        await abrir(page, conMuchasEtiquetas);
        if (esTelefono(page)) await conv(page).getByRole("button", { name: "Ver la ficha del cliente" }).click();
        const ficha = esTelefono(page) ? page.getByRole("dialog", { name: "Ficha del cliente" }) : page.getByRole("complementary", { name: "Ficha del cliente" });
        const zona = ficha.locator('[data-slot="etiquetas"]');
        await expect(zona.locator("[data-etiqueta]")).toHaveCount(MUCHAS.length);
        await zona.scrollIntoViewIfNeeded();
        await sinDesbordeHorizontal(zona, "etiquetas de la ficha");
        await sinDesbordeHorizontal(ficha, "ficha del cliente");
        // Más de una línea, y el nombre largo completo en el título.
        const filas = await zona.locator("[data-etiqueta]").evaluateAll((els) => new Set(els.map((e) => Math.round(e.getBoundingClientRect().top))).size);
        expect(filas).toBeGreaterThan(1);
        await expect(zona.locator('[data-etiqueta="7"]')).toHaveAttribute("title", LARGA);
        expect(await zona.locator('[data-etiqueta="7"]').evaluate((el) => getComputedStyle(el.querySelector("[data-slot='etiqueta-texto']") ?? el).textOverflow)).toBe("ellipsis");
        await captura(page, "etiquetas-ficha");
    });

    test("en la fila de la lista: una línea con las que caben y «+N»", async ({ page }) => {
        await abrir(page, conMuchasEtiquetas);
        if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();
        const fila = page.locator('[data-conversacion="41"]');
        const zona = fila.locator('[data-slot="etiquetas-fila"]');
        await expect(zona).toBeVisible();
        await sinDesbordeHorizontal(fila, "fila de la lista");
        const mas = zona.locator('[data-slot="lista-mas"]');
        await expect(mas).toHaveText(/^\+\d+$/);
        // Las que se ven y el «+N» están en una sola línea y dentro de la zona.
        const medidas = await zona.evaluate((el) => {
            const caja = el.getBoundingClientRect();
            const vistos = [...el.querySelectorAll<HTMLElement>("[data-elemento]:not([data-oculto]) [data-etiqueta], [data-slot='lista-mas']")].map((e) => e.getBoundingClientRect());
            return { lineas: new Set(vistos.map((r) => Math.round(r.top))).size, dentro: vistos.every((r) => r.right <= caja.right + 1 && r.bottom <= caja.bottom + 1 && r.width > 0) };
        });
        expect(medidas).toEqual({ lineas: 1, dentro: true });
        const visibles = await zona.locator("[data-elemento]:not([data-oculto]) [data-etiqueta]").count();
        expect(visibles).toBeGreaterThan(0);
        expect(visibles + Number((await mas.innerText()).slice(1))).toBe(MUCHAS.length);
        // Para lectores de pantalla y al pasar el puntero: todas.
        await expect(zona).toHaveAttribute("title", new RegExp(LARGA));
        await sinScrollDePagina(page);
        await captura(page, "etiquetas-lista");
    });

    test("en la cabecera del hilo y en los filtros tampoco se salen", async ({ page }) => {
        await abrir(page, conMuchasEtiquetas);
        const cabecera = conv(page).locator("header").first();
        await sinDesbordeHorizontal(cabecera, "cabecera del hilo");
        const zona = conv(page).locator('[data-slot="etiquetas-cabecera"]');
        await expect(zona).toBeVisible();
        await sinDesbordeHorizontal(zona, "etiquetas de la cabecera");
        await captura(page, "etiquetas-cabecera");

        if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();
        await page.getByRole("button", { name: /^Filtros/ }).click();
        const panel = page.getByRole("dialog").filter({ hasText: "Etiqueta" }).last();
        await expect(panel).toContainText("Sin servicio");
        await sinDesbordeHorizontal(panel, "panel de filtros");
        await captura(page, "etiquetas-filtros");
    });

    test("en el editor de etiquetas (⋯ › Etiquetas) los nombres largos no ensanchan el diálogo", async ({ page }) => {
        await abrir(page, conMuchasEtiquetas);
        await conv(page).getByRole("button", { name: "Más acciones" }).click();
        await page.getByRole("menuitem", { name: "Etiquetas" }).click();
        const dialogo = page.getByRole("dialog", { name: /Etiquetas/ });
        await expect(dialogo.getByRole("checkbox")).toHaveCount(MUCHAS.length);
        await sinDesbordeHorizontal(dialogo, "editor de etiquetas");
        await sinScrollDePagina(page);
        await captura(page, "etiquetas-editor");
    });

    test("cola, canal, categoría y estado: nombres largos sin desborde en la lista y en la cabecera", async ({ page }) => {
        await abrir(page, (api) => {
            conMuchasEtiquetas(api);
            const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
            if (!c) return;
            c.titulo = "Carlos Alberto Ruiz Hernández de la Cruz y Asociados, Sociedad Anónima";
            c.cola = { id: 2, nombre: "Soporte técnico residencial y empresarial de segundo nivel" };
            c.canal = { ...c.canal, nombre: "Línea de soporte principal de la oficina central de Quetzaltenango" };
        });
        await sinDesbordeHorizontal(conv(page).locator("header").first(), "cabecera con nombres largos");
        await sinScrollDePagina(page);
        await captura(page, "chips-cabecera");
        if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();
        await sinDesbordeHorizontal(page.locator('[data-conversacion="41"]'), "fila con nombres largos");
        await sinScrollDePagina(page);
        await captura(page, "chips-lista");
    });
});

const foto = (nombre: string, ancho = 60, alto = 40) => ({ name: nombre, mimeType: "image/png", buffer: png(ancho, alto) });
const vista = (page: Page) => compositor(page).getByRole("group", { name: "Archivos por enviar" });
const mensajesEnviados = (api: Api) => api.conversaciones.enviados.filter((e) => e.ruta.endsWith("/41/mensajes")).map((e) => e.cuerpo as { texto?: string; adjuntoId?: string });

test.describe("bandeja › adjuntar archivos con vista previa", () => {
    test("el clip abre un menú; lo elegido va a la vista previa (sin subir nada), se amplía y se quita; al enviar sale un mensaje por archivo, en orden, con el comentario en el primero", async ({ page }) => {
        const { api, adjuntos } = await abrir(page);
        await compositor(page).locator("[data-adjuntar]").click();
        await expect(page.getByRole("menuitem", { name: "Fotos" })).toBeVisible();
        await expect(page.getByRole("menuitem", { name: "Documento (PDF)" })).toBeVisible();
        // «Cámara» solo donde hay cámara de verdad (pantalla táctil).
        const tactil = await page.evaluate(() => window.matchMedia("(pointer: coarse)").matches);
        await expect(page.getByRole("menuitem", { name: "Cámara" })).toHaveCount(tactil ? 1 : 0);
        await captura(page, "adjuntar-menu");
        const [selector] = await Promise.all([page.waitForEvent("filechooser"), page.getByRole("menuitem", { name: "Fotos" }).click()]);
        expect(selector.isMultiple()).toBe(true);
        await selector.setFiles([foto("uno.png"), foto("dos.png"), foto("tres.png")]);

        await expect(vista(page).locator("[data-archivo]")).toHaveCount(3);
        await expect(vista(page).locator('[data-estado="listo"]')).toHaveCount(3);
        await expect(vista(page)).toContainText("PNG");
        await expect(vista(page)).toContainText("Cada archivo sale en su propio mensaje");
        // Todavía no se subió nada.
        expect(adjuntos.altas).toHaveLength(0);
        expect(mensajesEnviados(api)).toHaveLength(0);
        await sinDesbordeHorizontal(compositor(page), "compositor con la vista previa");
        await sinScrollDePagina(page);
        await captura(page, "adjuntar-vista-previa");

        // Ampliar una foto.
        await vista(page).getByRole("button", { name: "Ampliar uno.png" }).click();
        const grande = page.getByRole("dialog", { name: "uno.png" });
        await expect(grande.getByRole("img", { name: "Vista previa de uno.png" })).toBeVisible();
        await captura(page, "adjuntar-ampliada");
        await grande.getByRole("button", { name: "Cerrar" }).click();
        await expect(grande).toHaveCount(0);

        // Quitar una antes de enviar.
        await vista(page).getByRole("button", { name: "Quitar dos.png" }).click();
        await expect(vista(page).locator("[data-archivo]")).toHaveCount(2);

        await caja(page).fill("Aquí van las fotos de la instalación.");
        await compositor(page).getByRole("button", { name: /^Enviar/ }).click();
        await expect(vista(page)).toHaveCount(0);
        await expect.poll(() => mensajesEnviados(api).length).toBe(2);
        expect(adjuntos.altas.map((a) => [a.entidad, a.entidadId, a.tipo, a.mime])).toEqual([
            ["conversacion", "41", "foto", "image/png"],
            ["conversacion", "41", "foto", "image/png"],
        ]);
        // Un adjunto por mensaje, en orden; el comentario solo en el primero.
        expect(mensajesEnviados(api)).toEqual([
            expect.objectContaining({ adjuntoId: adjuntos.altas[0]?.id, texto: "Aquí van las fotos de la instalación." }),
            expect.objectContaining({ adjuntoId: adjuntos.altas[1]?.id }),
        ]);
        expect(mensajesEnviados(api)[1]?.texto).toBeUndefined();
        await expect(hilo(page).getByText("Aquí van las fotos de la instalación.")).toBeVisible();
        await expect(hilo(page).locator('[data-direccion="saliente"] img')).toHaveCount(2);
        await expect(caja(page)).toHaveValue("");
        await expect.poll(() => faltaParaElFinal(page)).toBeLessThanOrEqual(2);
    });

    test("valida tipo y tamaño antes de subir: el video, el SVG y el archivo enorme se marcan y no salen; lo que sirve, sí", async ({ page }) => {
        const { api, adjuntos } = await abrir(page);
        await compositor(page)
            .locator('input[data-archivos="documento"]')
            .setInputFiles([
                { name: "video-del-poste.mp4", mimeType: "video/mp4", buffer: Buffer.alloc(2048, 1) },
                { name: "logo<img src=x onerror=alert(1)>.svg", mimeType: "image/svg+xml", buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"><script>window.__svg=1</script></svg>') },
                { name: "enorme.png", mimeType: "image/png", buffer: Buffer.alloc(16 * 1024 * 1024 + 1, 7) },
                { name: "contrato-firmado-de-servicio-residencial-con-un-nombre-larguisimo-2026.pdf", mimeType: "application/pdf", buffer: PDF },
            ]);
        const filas = vista(page).locator("[data-archivo]");
        await expect(filas).toHaveCount(4);
        await expect(filas.nth(0)).toHaveAttribute("data-estado", "invalido");
        await expect(filas.nth(0)).toContainText("Los videos no se pueden enviar");
        await expect(filas.nth(1)).toHaveAttribute("data-estado", "invalido");
        await expect(filas.nth(1)).toContainText("Solo fotos (JPG, PNG, WebP) o PDF.");
        // El nombre va como texto y el SVG no se incrusta ni se ejecuta.
        await expect(filas.nth(1).locator('[data-slot="archivo-nombre"]')).toHaveText("logo<img src=x onerror=alert(1)>.svg");
        await expect(filas.nth(1).locator("img")).toHaveCount(0);
        expect(await page.evaluate(() => (window as unknown as { __svg?: number }).__svg)).toBeUndefined();
        await expect(filas.nth(2)).toContainText("El archivo pasa de 16 MB.");
        await expect(filas.nth(3)).toHaveAttribute("data-estado", "listo");
        await expect(filas.nth(3)).toContainText("PDF");
        expect(adjuntos.altas).toHaveLength(0);
        await sinDesbordeHorizontal(compositor(page), "vista previa con errores y un nombre largo");
        await sinScrollDePagina(page);
        await captura(page, "adjuntar-validacion");

        await compositor(page).getByRole("button", { name: /^Enviar/ }).click();
        await expect.poll(() => mensajesEnviados(api).length).toBe(1);
        expect(adjuntos.altas.map((a) => [a.tipo, a.mime])).toEqual([["documento", "application/pdf"]]);
        // Lo que no sirve sigue ahí, marcado, hasta quitarlo; y ya no hay nada que enviar.
        await expect(filas).toHaveCount(3);
        await expect(compositor(page).getByRole("button", { name: /^Enviar/ })).toBeDisabled();
        await expect(hilo(page).getByRole("link", { name: "Documento PDF" })).toBeVisible();
        for (const nombre of ["video-del-poste.mp4", "enorme.png"]) await vista(page).getByRole("button", { name: `Quitar ${nombre}` }).click();
        await vista(page).getByRole("button", { name: /^Quitar logo/ }).click();
        await expect(vista(page)).toHaveCount(0);
    });

    test("se ve el avance de la subida y, si falla, «Reintentar» sigue donde quedó sin repetir lo ya subido", async ({ page }) => {
        const { api, adjuntos } = await abrir(page, undefined, {}, { demoraParteMs: 350, fallarPartes: 1 });
        // 1,3 MB: tres partes de 512 KB.
        const tamano = 1_300_000;
        await compositor(page)
            .locator('input[data-archivos="fotos"]')
            .setInputFiles([{ name: "plano.png", mimeType: "image/png", buffer: Buffer.concat([png(30, 30), Buffer.alloc(tamano - png(30, 30).length, 3)]) }]);
        const fila = vista(page).locator("[data-archivo]");
        await expect(fila).toHaveAttribute("data-estado", "listo");
        await compositor(page).getByRole("button", { name: /^Enviar/ }).click();
        await expect(fila).toHaveAttribute("data-estado", "subiendo");
        await expect(fila.getByRole("progressbar")).toBeVisible();
        // La primera parte falla: lo dice y ofrece reintentar; nada se envió.
        await expect(fila).toHaveAttribute("data-estado", "error");
        await expect(fila).toContainText("No se pudo subir");
        expect(mensajesEnviados(api)).toHaveLength(0);
        await captura(page, "adjuntar-error-reintentar");
        await fila.getByRole("button", { name: "Reintentar" }).click();
        await expect(fila).toHaveAttribute("data-estado", "subiendo");
        await expect.poll(async () => Number(await fila.getByRole("progressbar").getAttribute("aria-valuenow").catch(() => "100"))).toBeGreaterThan(30);
        await captura(page, "adjuntar-subiendo");
        await expect(vista(page)).toHaveCount(0);
        expect(adjuntos.altas).toHaveLength(1);
        expect(adjuntos.partes.map((x) => x.offset)).toEqual([0, 524_288, 1_048_576]);
        expect(adjuntos.partes.reduce((n, x) => n + x.bytes, 0)).toBe(tamano);
        expect(mensajesEnviados(api)).toEqual([expect.objectContaining({ adjuntoId: adjuntos.altas[0]?.id })]);
    });

    test("pegar una imagen del portapapeles y soltar archivos sobre el hilo también abren la vista previa", async ({ page }) => {
        const { adjuntos } = await abrir(page);
        const bytes = [...png(24, 24)];
        await caja(page).evaluate((el, datos) => {
            const dt = new DataTransfer();
            dt.items.add(new File([new Uint8Array(datos)], "captura.png", { type: "image/png" }));
            el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
        }, bytes);
        await expect(vista(page).locator("[data-archivo]")).toHaveCount(1);
        await expect(vista(page)).toContainText("captura.png");

        const arrastre = await page.evaluateHandle((datos) => {
            const dt = new DataTransfer();
            dt.items.add(new File([new Uint8Array(datos)], "foto-arrastrada.png", { type: "image/png" }));
            dt.items.add(new File(["%PDF-1.4"], "factura.pdf", { type: "application/pdf" }));
            return dt;
        }, bytes);
        await hilo(page).dispatchEvent("dragenter", { dataTransfer: arrastre });
        await expect(conv(page).locator('[data-slot="soltar-archivos"]')).toBeVisible();
        await expect(conv(page).locator('[data-slot="soltar-archivos"]')).toContainText("Suelta aquí para adjuntar");
        await captura(page, "adjuntar-soltar");
        await hilo(page).dispatchEvent("drop", { dataTransfer: arrastre });
        await expect(conv(page).locator('[data-slot="soltar-archivos"]')).toHaveCount(0);
        await expect(vista(page).locator("[data-archivo]")).toHaveCount(3);
        await expect(vista(page)).toContainText("foto-arrastrada.png");
        await expect(vista(page)).toContainText("factura.pdf");
        expect(adjuntos.altas).toHaveLength(0);
        await sinDesbordeHorizontal(compositor(page), "compositor con tres archivos");
    });
});

test.describe("bandeja › adjuntos: revisión de la PR", () => {
    test("lo que se escribe mientras suben los archivos no sale como pie de foto; el comentario, la respuesta rápida van solo con el primero", async ({ page }) => {
        const { api, adjuntos } = await abrir(page, undefined, {}, { demoraParteMs: 500 });
        await compositor(page).locator('input[data-archivos="fotos"]').setInputFiles([foto("uno.png"), foto("dos.png")]);
        // Comentario desde una respuesta rápida (lleva su id).
        await compositor(page).getByRole("button", { name: "Visita hoy 13:30–15:00" }).click();
        const comentario = await caja(page).inputValue();
        expect(comentario).toContain("te enviamos un técnico hoy");
        await compositor(page).getByRole("button", { name: /^Enviar/ }).click();
        // La caja queda vacía al momento, y lo que se escriba ahora es otro mensaje.
        await expect(caja(page)).toHaveValue("");
        await caja(page).fill("Esto todavía lo estoy escribiendo");
        await expect(vista(page)).toHaveCount(0, { timeout: 15_000 });
        await expect.poll(() => mensajesEnviados(api).length).toBe(2);
        const [primero, segundo] = api.conversaciones.enviados.filter((e) => e.ruta.endsWith("/41/mensajes")).map((e) => e.cuerpo as { texto?: string; adjuntoId?: string; respuestaRapidaId?: number });
        expect(primero).toMatchObject({ adjuntoId: adjuntos.altas[0]?.id, texto: comentario, respuestaRapidaId: 1 });
        expect(segundo?.adjuntoId).toBe(adjuntos.altas[1]?.id);
        expect(segundo?.texto).toBeUndefined();
        expect(segundo?.respuestaRapidaId).toBeUndefined();
        // Lo escrito a medias sigue en la caja, sin enviar.
        await expect(caja(page)).toHaveValue("Esto todavía lo estoy escribiendo");
        await expect(hilo(page).getByText("Esto todavía lo estoy escribiendo")).toHaveCount(0);
    });

    test("si la subida falla, el comentario vuelve a la caja", async ({ page }) => {
        await abrir(page, undefined, {}, { fallarPartes: 1 });
        await compositor(page).locator('input[data-archivos="fotos"]').setInputFiles([foto("uno.png")]);
        await caja(page).fill("Foto del poste");
        await compositor(page).getByRole("button", { name: /^Enviar/ }).click();
        await expect(vista(page).locator("[data-archivo]")).toHaveAttribute("data-estado", "error");
        await expect(caja(page)).toHaveValue("Foto del poste");
    });

    test("soltar un archivo fuera de la conversación no abre el archivo (el navegador no se va del panel)", async ({ page }) => {
        await abrir(page);
        await expect(hilo(page)).toBeVisible();
        const prevenidos = await page.evaluate(() => {
            const dt = new DataTransfer();
            dt.items.add(new File(["x"], "foto.png", { type: "image/png" }));
            const destino = document.querySelector("header") ?? document.body;
            return ["dragover", "drop"].map((tipo) => {
                const e = new DragEvent(tipo, { dataTransfer: dt, bubbles: true, cancelable: true });
                destino.dispatchEvent(e);
                return e.defaultPrevented;
            });
        });
        expect(prevenidos).toEqual([true, true]);
        // Arrastrar texto (no archivos) sigue como siempre.
        expect(
            await page.evaluate(() => {
                const dt = new DataTransfer();
                dt.setData("text/plain", "hola");
                const e = new DragEvent("drop", { dataTransfer: dt, bubbles: true, cancelable: true });
                document.body.dispatchEvent(e);
                return e.defaultPrevented;
            }),
        ).toBe(false);
        await expect(vista(page)).toHaveCount(0);
        await expect(page).toHaveURL(/\/conversaciones\?c=41/);
    });

    test("con archivos por enviar, cambiar de conversación pregunta antes: «Seguir aquí» se queda y «Salir» sale sin enviar nada", async ({ page }) => {
        const { api, adjuntos } = await abrir(page);
        await compositor(page).locator('input[data-archivos="fotos"]').setInputFiles([foto("uno.png")]);
        await expect(vista(page).locator("[data-archivo]")).toHaveCount(1);
        const irAOtra = async () => {
            if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();
            else await page.locator('[data-conversacion="42"]').click();
        };
        await irAOtra();
        const pregunta = page.getByRole("alertdialog").or(page.getByRole("dialog")).filter({ hasText: "Hay archivos sin enviar" });
        await expect(pregunta).toBeVisible();
        await pregunta.getByRole("button", { name: "Seguir aquí" }).click();
        await expect(pregunta).toHaveCount(0);
        await expect(vista(page).locator("[data-archivo]")).toHaveCount(1);
        await expect(page).toHaveURL(/c=41/);
        await irAOtra();
        await pregunta.getByRole("button", { name: "Salir" }).click();
        await expect(page).not.toHaveURL(/c=41/);
        expect(adjuntos.altas).toHaveLength(0);
        expect(mensajesEnviados(api)).toHaveLength(0);
    });

    test("«La atiendo» de otra conversación con una subida en curso: pregunta, y al salir corta la subida, no envía nada y el comentario queda de borrador", async ({ page }) => {
        test.skip(esTelefono(page), "En el teléfono la lista no se ve con una conversación abierta: se sale por el enlace «Volver» (cubierto arriba).");
        const { api, adjuntos } = await abrir(page, undefined, {}, { demoraParteMs: 700 });
        const tamano = 2_000_000; // 4 partes
        await compositor(page)
            .locator('input[data-archivos="fotos"]')
            .setInputFiles([{ name: "plano.png", mimeType: "image/png", buffer: Buffer.concat([png(30, 30), Buffer.alloc(tamano - png(30, 30).length, 3)]) }, foto("otra.png")]);
        await caja(page).fill("Comentario que no debe perderse");
        await compositor(page).getByRole("button", { name: /^Enviar/ }).click();
        await expect(vista(page).locator("[data-archivo]").first()).toHaveAttribute("data-estado", "subiendo");
        // Desde la lista, «La atiendo» de otra conversación (cambia por código, sin enlace).
        const atender = page.getByRole("button", { name: /^La atiendo/ }).first();
        await atender.click();
        const pregunta = page.getByRole("alertdialog").or(page.getByRole("dialog")).filter({ hasText: "Todavía se están enviando archivos" });
        await expect(pregunta).toBeVisible();
        await expect(page).toHaveURL(/c=41/);
        await pregunta.getByRole("button", { name: "Seguir aquí" }).click();
        await expect(page).toHaveURL(/c=41/);
        await expect(vista(page).locator("[data-archivo]")).toHaveCount(2);
        await atender.click();
        await pregunta.getByRole("button", { name: "Salir" }).click();
        await expect(page).not.toHaveURL(/c=41/);
        await expect(page).toHaveURL(/c=\d+/);
        // Vuelve ENSEGUIDA (la parte en vuelo aún no terminó): el comentario ya está de borrador, y
        // sigue ahí cuando el compositor nuevo guarda el suyo.
        const partesAlSalir = adjuntos.partes.length;
        await page.locator('[data-conversacion="41"]').click();
        await expect(caja(page)).toHaveValue("Comentario que no debe perderse");
        // La subida se cortó: no llegan las 4 partes, no se registra el segundo archivo y no sale ningún mensaje.
        await page.waitForTimeout(2_500);
        await expect(caja(page)).toHaveValue("Comentario que no debe perderse");
        expect(adjuntos.partes.length).toBeLessThanOrEqual(partesAlSalir + 1);
        expect(adjuntos.partes.length).toBeLessThan(4);
        expect(adjuntos.altas).toHaveLength(1);
        expect(mensajesEnviados(api)).toHaveLength(0);
        // Y sobrevive a recargar (está guardado, no solo en pantalla).
        await page.reload();
        await expect(caja(page)).toHaveValue("Comentario que no debe perderse");
        await expect(vista(page)).toHaveCount(0);
    });

    test("si la subida falla, la respuesta rápida elegida mientras subía se conserva y el comentario solo se antepone", async ({ page }) => {
        const { api } = await abrir(page, undefined, {}, { demoraParteMs: 600, fallarPartes: 1 });
        await compositor(page).locator('input[data-archivos="fotos"]').setInputFiles([foto("uno.png")]);
        await caja(page).fill("Foto del poste");
        await compositor(page).getByRole("button", { name: /^Enviar/ }).click();
        await expect(caja(page)).toHaveValue("");
        // Mientras sube, elige una respuesta rápida para el siguiente mensaje.
        await compositor(page).getByRole("button", { name: "Visita hoy 13:30–15:00" }).click();
        const rapida = await caja(page).inputValue();
        await expect(vista(page).locator("[data-archivo]")).toHaveAttribute("data-estado", "error");
        await expect(caja(page)).toHaveValue(`Foto del poste\n\n${rapida}`);
        // Quita el archivo y manda el texto: sale con la respuesta rápida que eligió.
        await vista(page).getByRole("button", { name: "Quitar uno.png" }).click();
        await caja(page).press("Enter");
        await expect.poll(() => mensajesEnviados(api).length).toBe(1);
        expect(api.conversaciones.enviados.find((e) => e.ruta.endsWith("/41/mensajes"))?.cuerpo).toMatchObject({ respuestaRapidaId: 1 });
    });

    test("pegar desde Excel o Word (texto e imagen a la vez) pega el texto y no adjunta la imagen", async ({ page }) => {
        await abrir(page);
        const bytes = [...png(24, 24)];
        const prevenido = await caja(page).evaluate((el, datos) => {
            const dt = new DataTransfer();
            dt.setData("text/plain", "Plan\tPrecio\nFibra 100\tQ250");
            dt.items.add(new File([new Uint8Array(datos)], "image.png", { type: "image/png" }));
            const e = new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true });
            el.dispatchEvent(e);
            return e.defaultPrevented;
        }, bytes);
        // No se intercepta: el navegador pega el texto.
        expect(prevenido).toBe(false);
        await expect(vista(page)).toHaveCount(0);
    });
});

test.describe("bandeja › un corte breve del tiempo real no es «Sin conexión»", () => {
    /** Anota en la página si «Sin conexión» llegó a pintarse alguna vez. */
    const vigilar = (page: Page) =>
        page.addInitScript(() => {
            const w = window as unknown as { __sinConexion: number };
            w.__sinConexion = 0;
            new MutationObserver(() => {
                if (document.body?.innerText.includes("Sin conexión")) w.__sinConexion++;
            }).observe(document, { childList: true, subtree: true, characterData: true });
        });
    const vecesSinConexion = (page: Page) => page.evaluate(() => (window as unknown as { __sinConexion: number }).__sinConexion);
    const indicador = (page: Page) => page.getByRole("status").filter({ hasText: "Sin conexión" });

    /** Corta (como un proxy que tira la conexión) las próximas `n` aperturas del flujo. */
    async function conCortes(page: Page) {
        const estado = { cortar: 0, cortados: 0, abiertos: 0 };
        const { api } = await abrir(page, undefined, {}, {}, async (p) => {
            await vigilar(p);
            await p.route("**/api/v1/mensajeria/conversaciones/eventos*", async (route) => {
                if (estado.cortar > 0) {
                    estado.cortar--;
                    estado.cortados++;
                    return route.abort("connectionreset");
                }
                estado.abiertos++;
                return route.fallback();
            });
        });
        await expect(hilo(page)).toBeVisible();
        await expect.poll(() => estado.abiertos).toBeGreaterThan(0);
        return { api, estado };
    }

    test.setTimeout(120_000);

    test("un corte suelto: reconecta solo, nunca aparece el indicador y no se pierde ni se repite nada", async ({ page }) => {
        const { api, estado } = await conCortes(page);
        // La conexión se cae una vez (un proxy que la corta) justo cuando llega un mensaje.
        estado.cortar = 1;
        await expect.poll(() => estado.cortados, { timeout: 40_000 }).toBe(1);
        api.conversaciones.mensajeDelCliente(41, "Mensaje durante el corte.");
        await expect(hilo(page).getByText("Mensaje durante el corte.")).toHaveCount(1, { timeout: 15_000 });
        api.conversaciones.mensajeDelCliente(41, "Y otro después.");
        await expect(hilo(page).getByText("Y otro después.")).toHaveCount(1, { timeout: 40_000 });
        await expect(indicador(page)).toHaveCount(0);
        expect(await vecesSinConexion(page)).toBe(0);
        await expect(hilo(page).getByText("Mensaje durante el corte.")).toHaveCount(1);
    });

    test("una caída de verdad (varios intentos seguidos fallan) sí se avisa; al volver se pone al día y el aviso se va", async ({ page }) => {
        const { api, estado } = await conCortes(page);
        estado.cortar = 1_000;
        if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();
        await expect(indicador(page)).toBeVisible({ timeout: 15_000 });
        expect(estado.cortados).toBeGreaterThanOrEqual(2);
        api.conversaciones.mensajeDelCliente(41, "Escrito mientras no había conexión.");
        estado.cortar = 0;
        await expect(indicador(page)).toHaveCount(0, { timeout: 40_000 });
        if (esTelefono(page)) await page.locator('[data-conversacion="41"]').click();
        await expect(hilo(page).getByText("Escrito mientras no había conexión.")).toHaveCount(1);
    });
});

test.describe("bandeja › boleta y promesa contra Argos", () => {
    test("la boleta lee las cuentas bancarias de la empresa de esa conversación", async ({ page }) => {
        const pedida = page.waitForRequest((r) => r.url().includes("/conversaciones/cuentas-bancarias"));
        await abrir(page);
        await page.goto("/conversaciones/41/boleta");
        expect(new URL((await pedida).url()).searchParams.get("conversacionId")).toBe("41");
        await expect(page.locator("main")).toContainText("Banco Industrial · 000-123456-7");
    });

    test("promesa: tras un 503 de Argos el reintento lleva la misma Idempotency-Key; tras un 409, otra", async ({ page }) => {
        const claves: (string | null)[] = [];
        const respuestas = [503, 503, 409, 409];
        await abrir(page, undefined, {}, {}, async (p) => {
            await p.route("**/api/v1/mensajeria/conversaciones/41/promesas/simular", (route) =>
                route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ resultado: "activa", motivo: null, regla: "Hasta 5 días" }) }),
            );
            await p.route("**/api/v1/mensajeria/conversaciones/41/promesas", async (route) => {
                if (route.request().method() !== "POST") return route.fallback();
                claves.push(await route.request().headerValue("idempotency-key"));
                const estado = respuestas[claves.length - 1] ?? 409;
                const codigo = estado === 503 ? "argos_no_disponible" : "promesa_ya_enviada";
                const detail = estado === 503 ? undefined : "Esta promesa ya se había enviado a Argos con otros datos. Revísala en Argos antes de pedirla otra vez.";
                await route.fulfill({ status: estado, contentType: "application/problem+json", body: JSON.stringify({ type: `urn:iris:problema:${codigo}`, title: "No se pudo", status: estado, detail }) });
            });
        });
        if (esTelefono(page)) await conv(page).getByRole("button", { name: "Ver la ficha del cliente" }).click();
        await page.getByRole("button", { name: "Promesa", exact: true }).click();
        const dialogo = page.getByRole("dialog", { name: "Promesa de pago" });
        await escribirFecha(dialogo, "Paga a más tardar el", "2026-11-25");
        await dialogo.getByRole("button", { name: "Consultar con Argos" }).click();
        const registrar = dialogo.getByRole("button", { name: "Registrar promesa" });
        for (let n = 1; n <= 4; n++) {
            await registrar.click();
            await expect.poll(() => claves.length).toBe(n);
            await expect(dialogo.getByRole("alert").or(dialogo.locator('[data-slot="aviso"]')).first()).toBeVisible();
        }
        // El 409 `promesa_ya_enviada` se explica con el texto del servidor.
        await expect(dialogo).toContainText("Esta promesa ya se había enviado a Argos con otros datos.");
        expect(claves.every((c) => c && c.length > 8)).toBe(true);
        // 503 → misma clave; 503 → misma; 409 → otra.
        expect(claves[1]).toBe(claves[0]);
        expect(claves[2]).toBe(claves[0]);
        expect(claves[3]).not.toBe(claves[2]);
    });
});
