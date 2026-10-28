import { expect, test, type Page } from "@playwright/test";
import { fileURLToPath } from "node:url";
import { conSesionTecnico, simularApiCampo, type ApiCampo } from "./api-campo";
import { CABLE, TECNICO } from "./datos-campo";

/*
 * App de campo (/campo) en el teléfono (390 px, claro y oscuro): sin conexión con
 * context.setOffline, shell desde el service worker, cola local, envío al volver la señal,
 * rechazos visibles y faltan_adjuntos hasta que sube la foto. API simulada (api-campo.ts).
 */

const FOTO = fileURLToPath(new URL("./fixtures/foto-fachada.jpg", import.meta.url));

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

const conexion = (page: Page) => page.getByTestId("estado-conexion");
const porEnviar = (page: Page) => page.getByTestId("pendientes-por-enviar");

async function abrirCampo(page: Page) {
    await page.goto("/campo");
    await expect(page.getByRole("heading", { name: "Mis órdenes de hoy" })).toBeVisible();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
}

async function esperarServiceWorker(page: Page) {
    await page.waitForFunction(async () => {
        const r = await navigator.serviceWorker.getRegistration("/campo");
        return r?.active?.state === "activated";
    });
    // Toma el control de la página (clientsClaim); si no, al recargar.
    if (!(await page.evaluate(() => navigator.serviceWorker.controller !== null))) {
        await page.reload();
        await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
    }
}

async function sinSenal(page: Page, api: ApiCampo) {
    api.sinRed = true;
    await page.context().setOffline(true);
    await expect(conexion(page)).toHaveText("Sin conexión");
}

async function conSenal(page: Page, api: ApiCampo) {
    api.sinRed = false;
    await page.context().setOffline(false);
}

async function firmar(page: Page) {
    const lienzo = page.getByRole("img", { name: /Firma del cliente/ });
    // Al centro: que no quede bajo la barra fija de «Cerrar orden» ni bajo el aviso flotante.
    await lienzo.evaluate((el) => el.scrollIntoView({ block: "center" }));
    const caja = await lienzo.boundingBox();
    if (!caja) {
        throw new Error("sin lienzo");
    }
    await page.mouse.move(caja.x + 30, caja.y + caja.height * 0.7);
    await page.mouse.down();
    for (let i = 1; i <= 12; i += 1) {
        await page.mouse.move(caja.x + 30 + i * 22, caja.y + caja.height * (0.7 - Math.sin(i / 2) * 0.35));
    }
    await page.mouse.up();
    await expect(lienzo).toHaveAccessibleName("Firma del cliente: firmada");
}

test("lista del día con estado, conexión y «Todo enviado»", async ({ page }) => {
    const api = await simularApiCampo(page);
    await abrirCampo(page);
    await expect(conexion(page)).toHaveText(/En línea|Sincronizando…/);
    await expect(page.getByRole("button", { name: /Todo enviado/ })).toBeVisible();
    const tarjetas = page.getByRole("list", { name: "Órdenes del día" }).getByRole("link");
    await expect(tarjetas).toHaveCount(3);
    // Sin ruta: por hora de cita.
    await expect(tarjetas.nth(0)).toContainText("OT-000101");
    await expect(tarjetas.nth(1)).toContainText("OT-000102");
    await expect(tarjetas.nth(0)).toContainText("Programada");
    await expect(page.getByText("por hora de cita")).toBeVisible();
    // Canal campo y paquete del día en Guatemala.
    expect(api.peticiones.some((p) => p.ruta === "/api/v1/campo/paquete" && p.canal === "campo")).toBe(true);
});

test("detalle: cliente, «Cómo llegar», llamar y solo las acciones válidas", async ({ page }) => {
    await simularApiCampo(page);
    await abrirCampo(page);
    await page.getByRole("link", { name: /OT-000101/ }).click();
    await expect(page).toHaveURL(/\/campo\?orden=101$/);
    await expect(page.getByRole("heading", { name: "OT-000101" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Cómo llegar" })).toHaveAttribute("href", "https://www.google.com/maps/dir/?api=1&destination=14.6407,-90.5133");
    await expect(page.getByRole("link", { name: "Llamar" })).toHaveAttribute("href", "tel:+50255551234");
    await expect(page.getByRole("button", { name: "En camino" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Llegué" })).toBeVisible();
    // En programada no hay trabajo ni cierre.
    await expect(page.getByRole("heading", { name: "Checklist" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Cerrar orden" })).toHaveCount(0);
    await page.getByRole("button", { name: "Mis órdenes" }).click();
    await expect(page).toHaveURL(/\/campo$/);
});

test("sin conexión: abre desde el service worker, todo queda pendiente y se envía al volver la señal", async ({ page, context }) => {
    test.setTimeout(120_000);
    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation({ latitude: 14.6407, longitude: -90.5133, accuracy: 12 });
    const api = await simularApiCampo(page);
    await abrirCampo(page);
    await esperarServiceWorker(page);

    // Sin señal y recargando: el shell sale del service worker y las órdenes de IndexedDB.
    await sinSenal(page, api);
    await page.reload();
    await expect(page.getByRole("heading", { name: "Mis órdenes de hoy" })).toBeVisible();
    await expect(conexion(page)).toHaveText("Sin conexión");
    await page.getByRole("link", { name: /OT-000101/ }).click();

    await page.getByRole("button", { name: "En camino" }).click();
    await expect(page.getByText("Se enviará al volver la señal")).toBeVisible();
    await page.getByRole("button", { name: "Llegué" }).click();
    await expect(page.getByRole("heading", { name: "OT-000101" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Checklist" })).toBeVisible();
    await expect(page.locator('[data-slot="insignia-estado"]', { hasText: "En sitio" }).first()).toBeVisible();

    // Checklist: sí/no, medición, foto y texto.
    await page.getByRole("radiogroup", { name: "La ONU enciende" }).getByRole("radio", { name: "Sí" }).click();
    await page.getByRole("textbox", { name: "Potencia óptica" }).fill("-18,5");
    await page.getByRole("button", { name: "Guardar Potencia óptica" }).click();
    await page.getByLabel("Tomar foto: Foto de la fachada").setInputFiles(FOTO);
    await expect(page.getByText(/^Foto guardada/)).toBeVisible();
    await expect(page.getByRole("img", { name: "Foto de la fachada" })).toBeVisible();
    await expect(page.getByText("Por subir").first()).toBeVisible();

    // Material de su bodega y equipo instalado (escrito a mano).
    await page.getByLabel("Material").selectOption(String(CABLE));
    await page.getByLabel("Cantidad").fill("35");
    await page.getByRole("button", { name: "Registrar material" }).click();
    await expect(page.getByText("Cable drop 1 hilo — quedan 85 m")).toBeAttached();
    await page.getByLabel("Número de serie").fill("ZTEGC8F1A2B3");
    await page.getByLabel("MAC").fill("a0b1c2d3e4f6");
    await page.getByLabel("Modelo").fill("ZTE F670L");
    await page.getByRole("button", { name: "Registrar equipo instalado" }).click();
    await expect(page.getByText("A0:B1:C2:D3:E4:F6")).toBeVisible();

    // Firma del cliente.
    await page.getByLabel("Nombre de quien firma").fill("María Castillo");
    await page.getByLabel("Documento (DPI)").fill("2456789010101");
    await firmar(page);
    await page.getByRole("button", { name: "Guardar firma" }).click();
    await expect(page.getByText("Firmada")).toBeVisible();

    // Cerrar.
    await page.getByRole("button", { name: "Cerrar orden" }).click();
    const dialogo = page.getByRole("dialog");
    await expect(dialogo.getByText("Las fotos o la firma aún se están subiendo")).toBeVisible();
    await dialogo.getByRole("button", { name: "Confirmar cierre" }).click();
    await expect(page.locator('[data-slot="insignia-estado"]', { hasText: "Cerrada" }).first()).toBeVisible();

    // Todo quedó en el teléfono: nada llegó a la API.
    await expect(porEnviar(page)).toContainText("pendientes por enviar");
    await expect(porEnviar(page)).toContainText("2 archivos por subir");
    expect(api.lotes).toHaveLength(0);
    await page.getByRole("button", { name: /pendientes por enviar/ }).click();
    const cola = page.getByRole("list", { name: "Cola de envío" });
    await expect(cola.getByRole("listitem")).toHaveCount(11);
    await expect(cola).toContainText("Cierre de la orden · OT-000101");

    // Vuelve la señal: se suben los archivos, se envía la cola y se reconcilia.
    await conSenal(page, api);
    await expect(page.getByText("No queda nada por enviar desde este teléfono.")).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole("button", { name: /Todo enviado/ })).toBeVisible();
    const servidor = api.ordenes.get(101);
    expect(servidor?.estado).toBe("cerrada");
    expect(servidor?.firma?.nombre).toBe("María Castillo");
    expect(servidor?.equipos?.[0]).toMatchObject({ accion: "instalar", serie: "ZTEGC8F1A2B3", mac: "A0:B1:C2:D3:E4:F6" });
    expect(servidor?.materiales?.[0]).toMatchObject({ productoId: CABLE, cantidad: 35 });
    const ops = api.lotes.flatMap((l) => l.operaciones ?? []);
    expect(ops.map((o) => o.tipo)).toEqual([
        "orden.iniciar_camino",
        "orden.llegar",
        "checklist.marcar",
        "checklist.marcar",
        "adjunto.registrar",
        "checklist.marcar",
        "material.usar",
        "equipo.instalar",
        "adjunto.registrar",
        "firma.capturar",
        "orden.cerrar",
    ]);
    expect(ops[0]?.datos).toEqual({ lat: 14.6407, lon: -90.5133, precision: 12 });
    expect(api.lotes[0]).toMatchObject({ plataforma: "pwa" });
    expect(api.lotes[0]?.enviadoEn).toBeTruthy();
    // Fotos: alta con entidad orden y claveChecklist, comprimida, con hora y GPS del EXIF.
    const subidas = [...api.subidas.values()];
    const foto = subidas.find((s) => s.entrada.tipo === "foto");
    expect(foto?.entrada).toMatchObject({ entidad: "orden", entidadId: "101", claveChecklist: "fachada", mime: "image/jpeg", lat: 14.605, lon: -90.51 });
    expect(foto?.entrada.tomadoEn?.slice(0, 16)).toBe("2026-10-28T15:15");
    expect(foto?.entrada.tamano).toBeLessThanOrEqual(310 * 1024);
    expect(foto?.recibido).toBe(foto?.entrada.tamano);
    expect(subidas.find((s) => s.entrada.tipo === "firma")?.entrada.mime).toBe("image/png");
    expect(api.peticiones.filter((p) => p.metodo !== "GET" && p.metodo !== "HEAD").every((p) => p.canal === "campo" && p.csrf)).toBe(true);
});

test("un rechazo del servidor queda visible con su motivo y se puede descartar o rehacer", async ({ page }) => {
    const api = await simularApiCampo(page);
    api.rechazar((op) => op.tipo === "orden.iniciar_camino" && op.ordenId === 102, {
        type: "urn:iris:problema:orden_cancelada",
        title: "Orden cancelada",
        status: 409,
    });
    api.rechazar((op) => op.tipo === "nota.agregar", {
        type: "urn:iris:problema:validacion",
        title: "Datos no válidos",
        status: 422,
        errors: [{ field: "datos.texto", message: "El texto tiene caracteres no permitidos" }],
    });
    await abrirCampo(page);
    await page.getByRole("link", { name: /OT-000102/ }).click();
    await page.getByRole("button", { name: "En camino" }).click();
    await page.getByLabel("Nueva nota").fill("Cliente pide llegar después de las 3");
    await page.getByRole("button", { name: "Agregar nota" }).click();

    await expect(page.getByText(/acciones rechazadas por el servidor/)).toBeVisible({ timeout: 15_000 });
    await page.getByRole("button", { name: "Revisar" }).click();
    const rechazos = page.getByTestId("rechazo");
    await expect(rechazos).toHaveCount(2);
    await expect(rechazos.nth(0)).toContainText("En camino · OT-000102");
    await expect(rechazos.nth(0)).toContainText("Despacho canceló la orden.");
    await expect(rechazos.nth(1)).toContainText("Algún dato no es válido.");
    await expect(rechazos.nth(1)).toContainText("El texto tiene caracteres no permitidos");
    await expect(page.getByRole("button", { name: /2 rechazados/ })).toBeVisible();

    // Rehacer la nota (con id nuevo) y descartar lo de la orden cancelada.
    await rechazos.nth(1).getByRole("button", { name: "Rehacer" }).click();
    await rechazos.nth(0).getByRole("button", { name: "Descartar todo de esta orden" }).click();
    await expect(page.getByText("No queda nada por enviar desde este teléfono.")).toBeVisible({ timeout: 15_000 });
    expect(api.ordenes.get(102)?.notas?.map((n) => n.texto)).toEqual(["Cliente pide llegar después de las 3"]);
});

test("faltan_adjuntos: el cierre espera hasta que termina de subir la foto", async ({ page }) => {
    test.setTimeout(90_000);
    // El servidor exige además la foto del medidor, que el paquete no marcó obligatoria.
    const ordenes = (await import("./datos-campo")).ordenesDelDia();
    const visita = ordenes.find((o) => o.id === 102);
    if (visita) {
        visita.checklist = [...(visita.checklist ?? []), { itemId: 9, codigo: "medidor", etiqueta: "Foto del equipo", tipo: "foto", obligatorio: false, unidad: "", posicion: 9, valor: null, marcadoPor: null, ocurridoEn: null }];
    }
    const api = await simularApiCampo(page, { ordenes, fotosExigidas: ["medidor"] });
    await abrirCampo(page);
    await page.getByRole("link", { name: /OT-000102/ }).click();
    await page.getByRole("button", { name: "Llegué" }).click();
    await page.getByRole("radiogroup", { name: "Señal restablecida" }).getByRole("radio", { name: "Sí" }).click();

    api.pausarSubidas = true;
    await page.getByLabel("Tomar foto: Foto del equipo").setInputFiles(FOTO);
    await expect(page.getByRole("button", { name: "Cambiar foto: Foto del equipo" })).toBeVisible();
    await page.getByRole("button", { name: "Cerrar orden" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Confirmar cierre" }).click();

    // La API responde faltan_adjuntos: queda esperando la foto, sin pedir nada al técnico.
    await page.getByRole("button", { name: /por enviar|por subir|rechazado/ }).click();
    const espera = page.getByTestId("esperando-adjuntos");
    await expect(espera).toContainText("Cierre de la orden · OT-000102", { timeout: 20_000 });
    await expect(espera).toContainText("Falta subir la foto «Foto del equipo».");
    expect(api.resultados.some((r) => r.error?.type === "urn:iris:problema:faltan_adjuntos")).toBe(true);
    expect(api.ordenes.get(102)?.estado).toBe("en_sitio");

    // Termina la subida → el cierre se reintenta solo.
    api.pausarSubidas = false;
    await page.getByRole("button", { name: "Reintentar", exact: true }).click();
    await expect(page.getByText("No queda nada por enviar desde este teléfono.")).toBeVisible({ timeout: 30_000 });
    expect(api.ordenes.get(102)?.estado).toBe("cerrada");
});

test("sin sesión manda a entrar con regreso a /campo", async ({ page, context }) => {
    await context.clearCookies();
    await simularApiCampo(page);
    await page.goto("/campo");
    await expect(page).toHaveURL(/\/entrar\?siguiente=%2Fcampo/);
});

test("varias empresas: pide elegir y manda X-Iris-Empresa", async ({ page }) => {
    const api = await simularApiCampo(page, {
        empresas: [
            { id: 10, nombre: "Cable Norte" },
            { id: 20, nombre: "Fibra Sur" },
        ],
    });
    await page.goto("/campo");
    await expect(page.getByText("Elige la empresa")).toBeVisible();
    await page.getByRole("button", { name: "Fibra Sur" }).click();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
    await expect(page.getByText(`${TECNICO.nombre} · Fibra Sur`)).toBeVisible();
    expect(api.peticiones.find((p) => p.ruta === "/api/v1/campo/paquete")?.empresa).toBe("20");
});

test("manifest de la PWA", async ({ request }) => {
    const r = await request.get("/campo/manifest.webmanifest");
    expect(r.ok()).toBe(true);
    expect(await r.json()).toMatchObject({ name: "Iris Campo", display: "standalone", start_url: "/campo", icons: [{ src: "/icon.svg" }, { src: "/icon.svg" }] });
});
