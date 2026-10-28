import { fileURLToPath } from "node:url";
import { expect, test, type Page } from "@playwright/test";
import { entrar } from "./ayudas";

/*
 * Recorrido completo contra la API Go real (opcional, fuera de CI): IRIS_E2E_API_REAL=1 con la
 * API (`iris servir`), el panel standalone delante (proxy /api) y la semilla
 * `iris campo semilla`; IRIS_E2E_TECNICO=<correo>:<contraseña> del técnico que imprime la semilla.
 * Sin señal: En camino, Llegué, checklist completo, foto, equipo, firma y cierre; con señal: se
 * sube, se envía y la API deja la orden cerrada.
 */

const FOTO = fileURLToPath(new URL("./fixtures/foto-fachada.jpg", import.meta.url));
const [correo = "", contrasena = ""] = (process.env.IRIS_E2E_TECNICO ?? "").split(":");

test.skip(!process.env.IRIS_E2E_API_REAL || !correo, "Solo contra la API Go real (IRIS_E2E_API_REAL=1).");

async function firmar(page: Page) {
    const lienzo = page.getByRole("img", { name: /Firma del cliente/ });
    await lienzo.evaluate((el) => el.scrollIntoView({ block: "center" }));
    const caja = await lienzo.boundingBox();
    if (!caja) {
        throw new Error("sin lienzo");
    }
    await page.mouse.move(caja.x + 30, caja.y + caja.height * 0.6);
    await page.mouse.down();
    for (let i = 1; i <= 14; i += 1) {
        await page.mouse.move(caja.x + 30 + i * 20, caja.y + caja.height * (0.6 - Math.sin(i / 2) * 0.3));
    }
    await page.mouse.up();
}

test("OT de instalación de la semilla, sin conexión y de vuelta, contra la API real", async ({ page, context }, info) => {
    test.skip(info.project.name !== "telefono-claro", "Una sola pasada (cambia datos reales).");
    test.setTimeout(180_000);
    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation({ latitude: 14.6407, longitude: -90.5133, accuracy: 12 });
    await page.goto("/campo");
    await expect(page).toHaveURL(/\/entrar\?siguiente=%2Fcampo/);
    await entrar(page, correo, contrasena);
    await expect(page).toHaveURL(/\/campo$/);
    const tarjeta = page.getByRole("link", { name: /OT-000001/ });
    await expect(tarjeta).toBeVisible();
    await page.waitForFunction(async () => (await navigator.serviceWorker.getRegistration("/campo"))?.active?.state === "activated");
    // clientsClaim: el service worker toma la página sin recargar.
    await page.waitForFunction(() => navigator.serviceWorker.controller !== null);

    await context.setOffline(true);
    await page.reload();
    await expect(page.getByTestId("estado-conexion")).toHaveText("Sin conexión");
    await tarjeta.click();
    await page.getByRole("button", { name: "En camino" }).click();
    await page.getByRole("dialog", { name: "¿Compartir tu ubicación?" }).getByRole("button", { name: "Permitir ubicación" }).click();
    await page.getByRole("button", { name: "Llegué" }).click();
    await expect(page.getByRole("heading", { name: "Checklist" })).toBeVisible();
    // Ítems obligatorios de la plantilla «instalación de fibra» de la semilla.
    for (const si of ["Acometida de fibra tendida y fijada", "ONU registrada y en línea", "Wi-Fi configurado con el cliente"]) {
        await page.getByRole("radiogroup", { name: si }).getByRole("radio", { name: "Sí" }).click();
    }
    for (const [nombre, valor] of [
        ["Potencia óptica en la ONU", "-19,2"],
        ["Prueba de velocidad de bajada", "98"],
    ] as const) {
        await page.getByRole("textbox", { name: nombre, exact: true }).fill(valor);
        await page.getByRole("button", { name: `Guardar ${nombre}`, exact: true }).click();
    }
    await page.getByLabel("Tomar foto: Foto de la ONU instalada").setInputFiles(FOTO);
    await expect(page.getByRole("button", { name: "Cambiar foto: Foto de la ONU instalada" })).toBeVisible();
    await page.getByLabel("Número de serie").fill("ZTEGC8F1A2B3");
    await page.getByLabel("MAC").fill("a0b1c2d3e4f6");
    await page.getByRole("button", { name: "Registrar equipo instalado" }).click();
    await page.getByLabel("Nombre de quien firma").fill("Cliente de prueba");
    await firmar(page);
    await page.getByRole("button", { name: "Guardar firma" }).click();
    await expect(page.getByText("Firmada")).toBeVisible();
    await page.getByRole("button", { name: "Cerrar orden" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Confirmar cierre" }).click();
    await expect(page.getByTestId("pendientes-por-enviar")).toContainText("pendientes por enviar");

    await context.setOffline(false);
    await page.getByRole("button", { name: /por enviar/ }).click();
    await expect(page.getByText("No queda nada por enviar desde este teléfono.")).toBeVisible({ timeout: 60_000 });

    const orden = await page.evaluate(async () => (await fetch("/api/v1/ordenes/1", { headers: { "X-Iris-Canal": "campo" } })).json());
    expect(orden).toMatchObject({ estado: "cerrada", resultado: "exitosa" });
    expect(orden.firma?.nombre).toBe("Cliente de prueba");
    expect(orden.equipos?.[0]).toMatchObject({ accion: "instalar", serie: "ZTEGC8F1A2B3", mac: "A0:B1:C2:D3:E4:F6" });
    const adjuntos = await page.evaluate(async () => (await fetch("/api/v1/adjuntos?entidad=orden&entidadId=1")).json());
    expect(adjuntos.data.map((a: { tipo: string; estado: string }) => `${a.tipo}:${a.estado}`).sort()).toEqual(["firma:completo", "foto:completo"]);
});

const [correoB = "", contrasenaB = ""] = (process.env.IRIS_E2E_TECNICO_B ?? "").split(":");

test("dos técnicos en el mismo navegador contra la API real: la cola de A no sale con la sesión de B", async ({ browser, baseURL }, info) => {
    test.skip(info.project.name !== "telefono-claro" || !correoB, "Necesita IRIS_E2E_TECNICO_B (segundo técnico de la semilla).");
    test.setTimeout(180_000);
    const context = await browser.newContext({ baseURL: baseURL ?? "", viewport: { width: 390, height: 844 }, locale: "es-GT", timezoneId: "America/Guatemala" });
    const a = await context.newPage();
    await a.goto("/campo");
    await entrar(a, correo, contrasena);
    await expect(a.getByRole("link", { name: /OT-000002/ })).toBeVisible();
    // A sin señal (solo su pestaña): deja una nota en la cola.
    await a.route("**/api/**", (r) => r.abort("internetdisconnected"));
    await a.evaluate(() => window.dispatchEvent(new Event("offline")));
    await a.getByRole("link", { name: /OT-000002/ }).click();
    await a.getByLabel("Nueva nota").fill("Nota de A sin señal");
    await a.getByRole("button", { name: "Agregar nota" }).click();
    await expect(a.getByTestId("pendientes-por-enviar")).toHaveText("1 pendiente por enviar");

    // B entra en otra pestaña del mismo navegador (la cookie de sesión pasa a ser la de B).
    const b = await context.newPage();
    await b.goto("/entrar?siguiente=%2Fcampo");
    await entrar(b, correoB, contrasenaB);
    await expect(b.getByRole("link", { name: /OT-000003/ })).toBeVisible();
    await expect(b.getByRole("link", { name: /OT-000002/ })).toHaveCount(0);

    // Vuelve la señal a la pestaña de A: verifica, ve a B y no envía nada.
    await a.unroute("**/api/**");
    await a.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(a.getByText("Entró otra persona en este navegador")).toBeVisible({ timeout: 30_000 });

    // A vuelve a entrar: su nota sale con su sesión.
    await a.goto("/entrar?siguiente=%2Fcampo");
    await entrar(a, correo, contrasena);
    await expect(a.getByRole("button", { name: /Todo enviado/ })).toBeVisible({ timeout: 30_000 });
    const orden2 = await a.evaluate(async () => (await fetch("/api/v1/ordenes/2")).json());
    expect(orden2.notas.map((n: { texto: string; autorNombre: string }) => n.texto)).toEqual(["Nota de A sin señal"]);
    await context.close();
});

test("ubicación en vivo contra la API real: el lote llega, reenviar es seguro y el límite responde 429 con Retry-After", async ({ page, context }, info) => {
    test.skip(info.project.name !== "telefono-claro", "Una sola pasada (cambia datos reales).");
    test.setTimeout(120_000);
    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation({ latitude: 14.6407, longitude: -90.5133, accuracy: 12 });
    await page.goto("/campo");
    await entrar(page, correo, contrasena);
    await expect(page.getByRole("heading", { name: "Mis órdenes de hoy" })).toBeVisible();

    const indicador = page.getByTestId("indicador-ubicacion");
    const respuesta = page.waitForResponse((r) => r.url().endsWith("/api/v1/campo/posiciones") && r.request().method() === "POST");
    if ((await indicador.textContent())?.includes("Fuera de turno")) {
        await indicador.click();
        await page.getByRole("dialog", { name: "Ubicación en vivo" }).getByRole("button", { name: "Iniciar turno" }).click();
        const explicacion = page.getByRole("dialog", { name: "¿Compartir tu ubicación?" });
        if (await explicacion.isVisible()) {
            await explicacion.getByRole("button", { name: "Permitir ubicación" }).click();
        }
    }
    const r = await respuesta;
    expect(r.status()).toBe(200);
    const cuerpo = (await r.json()) as { aceptadas: number; duplicadas: number; descartadas: number };
    expect(cuerpo.aceptadas + cuerpo.duplicadas + cuerpo.descartadas).toBeGreaterThanOrEqual(1);
    await expect(indicador).toHaveText("Ubicación activa");

    // El mismo punto otra vez = duplicada; luego, hasta pasar el límite por minuto: 429 con Retry-After.
    const rafaga = await page.evaluate(async () => {
        const csrf = decodeURIComponent(/(?:^|;\s*)iris_csrf=([^;]+)/.exec(document.cookie)?.[1] ?? "");
        const punto = { lat: 14.6407, lon: -90.5133, registradaEn: new Date(Date.now() - 3_600_000).toISOString() };
        const salida: { status: number; retryAfter: string | null; duplicadas?: number }[] = [];
        for (let i = 0; i < 8; i += 1) {
            const res = await fetch("/api/v1/campo/posiciones", {
                method: "POST",
                headers: { "Content-Type": "application/json", "X-Iris-Canal": "campo", "X-CSRF-Token": csrf },
                body: JSON.stringify({ puntos: [punto] }),
            });
            const json = (await res.json()) as { duplicadas?: number };
            salida.push({ status: res.status, retryAfter: res.headers.get("Retry-After"), duplicadas: json.duplicadas });
        }
        return salida;
    });
    const ok = rafaga.filter((x) => x.status === 200);
    expect(ok.length).toBeGreaterThanOrEqual(1);
    expect(ok.slice(1).every((x) => x.duplicadas === 1)).toBe(true);
    const limitado = rafaga.find((x) => x.status === 429);
    expect(limitado?.retryAfter).toMatch(/^\d+$/);
});
