import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { conSesionTecnico, declinarUbicacion, simularApiCampo, type ApiCampo } from "./api-campo";
import { ordenesDelDia } from "./datos-campo";

/*
 * Ubicación en vivo y «En camino» en la app de campo (teléfono, 390 px, claro y oscuro), con la
 * API simulada (api-campo.ts): explicación antes del permiso, un punto por minuto en turno (reloj
 * de Playwright), cola sin señal y envío en lote al volver, 429 con Retry-After, permiso negado,
 * y orden del día por cita (y por ruta publicada cuando llegue en el paquete).
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const GUATE = { latitude: 14.6407, longitude: -90.5133, accuracy: 12 };

const indicador = (page: Page) => page.getByTestId("indicador-ubicacion");
const puntosEnviados = (api: ApiCampo) => api.posiciones.flatMap((l) => l.puntos ?? []);

async function abrirCampo(page: Page) {
    await page.goto("/campo");
    await expect(page.getByRole("heading", { name: "Mis órdenes de hoy" })).toBeVisible();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
}

test.describe("en el teléfono", () => {
    test.beforeEach(async ({ context, baseURL }, info) => {
        test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
        await conSesionTecnico(context, baseURL ?? "");
    });

    test("explica antes de pedir el permiso, envía un punto por minuto en turno y sin señal los acumula", async ({ page, context }) => {
        test.setTimeout(90_000);
        await page.clock.install();
        const api = await simularApiCampo(page);
        await abrirCampo(page);
        await expect(indicador(page)).toHaveText("Fuera de turno");

        // Iniciar turno: primero la explicación; el navegador no pregunta hasta «Permitir».
        await indicador(page).click();
        const panel = page.getByRole("dialog", { name: "Ubicación en vivo" });
        await expect(panel.getByTestId("estado-ubicacion")).toHaveText("Fuera de turno");
        await panel.getByRole("button", { name: "Iniciar turno" }).click();
        const explicacion = page.getByRole("dialog", { name: "¿Compartir tu ubicación?" });
        await expect(explicacion).toContainText("Fuera de turno no se registra nada");
        expect(api.intentosPosiciones).toHaveLength(0);

        // El técnico acepta en el navegador.
        await context.grantPermissions(["geolocation"]);
        await context.setGeolocation(GUATE);
        await explicacion.getByRole("button", { name: "Permitir ubicación" }).click();
        await expect(indicador(page)).toHaveText("Ubicación activa");
        await expect.poll(() => puntosEnviados(api).length).toBe(1);
        expect(puntosEnviados(api)[0]).toMatchObject({ lat: 14.6407, lon: -90.5133, precision: 12 });
        expect(api.posiciones[0]?.dispositivoId).toBeTruthy();
        expect(api.posiciones[0]?.enviadoEn).toBeTruthy();
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/campo/posiciones").every((p) => p.canal === "campo" && p.csrf)).toBe(true);

        // Un minuto después, otro punto.
        await context.setGeolocation({ ...GUATE, latitude: 14.641 });
        await page.clock.fastForward("01:00");
        await expect.poll(() => puntosEnviados(api).length).toBe(2);

        // Sin señal: los puntos se guardan en el teléfono y el indicador los cuenta.
        api.sinRed = true;
        await context.setOffline(true);
        for (const [i, lat] of [14.642, 14.643, 14.644].entries()) {
            await context.setGeolocation({ ...GUATE, latitude: lat });
            await page.clock.fastForward("01:00");
            await expect(indicador(page)).toContainText(`Pendientes de enviar: ${i + 1}`);
        }
        expect(puntosEnviados(api)).toHaveLength(2);

        // Vuelve la señal: salen en un solo lote, en orden.
        api.sinRed = false;
        await context.setOffline(false);
        await expect.poll(() => puntosEnviados(api).length).toBe(5);
        const ultimo = api.posiciones.at(-1)?.puntos ?? [];
        expect(ultimo.map((p) => p.lat)).toEqual([14.642, 14.643, 14.644]);
        await expect(indicador(page)).toHaveText("Ubicación activa");

        // Terminar turno: no se toman más puntos.
        await indicador(page).click();
        await panel.getByRole("button", { name: "Terminar turno" }).click();
        await expect(indicador(page)).toHaveText("Fuera de turno");
        await page.keyboard.press("Escape");
        await page.clock.fastForward("03:00");
        expect(puntosEnviados(api)).toHaveLength(5);
    });

    test("con 429 respeta Retry-After: guarda el punto y no reintenta antes", async ({ page, context }) => {
        test.setTimeout(90_000);
        await context.grantPermissions(["geolocation"]);
        await context.setGeolocation(GUATE);
        await page.clock.install();
        const api = await simularApiCampo(page);
        await abrirCampo(page);
        await indicador(page).click();
        await page.getByRole("dialog", { name: "Ubicación en vivo" }).getByRole("button", { name: "Iniciar turno" }).click();
        await expect.poll(() => puntosEnviados(api).length).toBe(1);

        api.limitarPosiciones(1, 30);
        await page.clock.fastForward("01:00");
        await expect.poll(() => api.intentosPosiciones.length).toBe(2);
        await expect(indicador(page)).toContainText("Pendientes de enviar: 1");
        await indicador(page).click();
        await expect(page.getByRole("dialog", { name: "Ubicación en vivo" })).toContainText("límite de envíos por minuto del servidor");
        await page.keyboard.press("Escape");

        // Antes de los 30 s no se reintenta, aunque vuelva a «haber señal».
        await page.evaluate(() => window.dispatchEvent(new Event("online")));
        await page.clock.fastForward("00:20");
        expect(api.intentosPosiciones).toHaveLength(2);
        await page.clock.fastForward("00:11");
        await expect.poll(() => puntosEnviados(api).length).toBe(2);
        await expect(indicador(page)).toHaveText("Ubicación activa");
    });

    test("«En camino» va a la cola sin conexión; si se niega el permiso la app sigue y lo dice", async ({ page, context }) => {
        const api = await simularApiCampo(page);
        await abrirCampo(page);
        await context.setOffline(true);
        api.sinRed = true;
        await page.getByRole("link", { name: /OT-000101/ }).click();
        await page.getByRole("button", { name: "En camino" }).click();

        // El navegador niega el permiso (Playwright no lo concedió).
        await page.getByRole("dialog", { name: "¿Compartir tu ubicación?" }).getByRole("button", { name: "Permitir ubicación" }).click();
        await expect(page.getByText("En camino, sin ubicación. Se enviará al volver la señal.")).toBeVisible();
        await expect(page.locator('[data-slot="insignia-estado"]', { hasText: "En camino" }).first()).toBeVisible();
        await expect(page.getByTestId("pendientes-por-enviar")).toHaveText("1 pendiente por enviar");
        // «En camino» inicia el turno; sin permiso, la ubicación queda «Sin permiso» (icono + texto + color).
        await expect(indicador(page)).toHaveText("Sin permiso");
        await expect(indicador(page)).toHaveAttribute("data-tono", "error");
        await indicador(page).click();
        await expect(page.getByRole("dialog", { name: "Ubicación en vivo" })).toContainText("El navegador tiene bloqueada la ubicación");
        await page.keyboard.press("Escape");

        // «Llegué» no vuelve a preguntar y la cola sale al volver la señal.
        await page.getByRole("button", { name: "Llegué" }).click();
        await expect(page.getByText("Llegada registrada sin ubicación. Se enviará al volver la señal.")).toBeVisible();
        api.sinRed = false;
        await context.setOffline(false);
        await expect(page.getByRole("button", { name: /Todo enviado/ })).toBeVisible({ timeout: 15_000 });
        const ops = api.lotes.flatMap((l) => l.operaciones ?? []);
        expect(ops.map((o) => [o.tipo, o.datos])).toEqual([
            ["orden.iniciar_camino", {}],
            ["orden.llegar", {}],
        ]);
        expect(api.ordenes.get(101)?.estado).toBe("en_sitio");
        expect(api.intentosPosiciones).toHaveLength(0);
    });

    test("«Ahora no»: la acción va sin GPS, no vuelve a preguntar y la ubicación queda sin permiso", async ({ page }) => {
        const api = await simularApiCampo(page);
        await abrirCampo(page);
        await page.getByRole("link", { name: /OT-000101/ }).click();
        await page.getByRole("button", { name: "En camino" }).click();
        await declinarUbicacion(page);
        await expect(page.getByText(/^En camino, sin ubicación/)).toBeVisible();
        await expect(indicador(page)).toHaveText("Sin permiso");
        await expect(indicador(page)).toHaveAttribute("data-tono", "warning");
        await page.getByRole("button", { name: "Llegué" }).click();
        await expect(page.getByRole("dialog", { name: "¿Compartir tu ubicación?" })).toHaveCount(0);
        await expect.poll(() => api.ordenes.get(101)?.estado, { timeout: 15_000 }).toBe("en_sitio");
    });

    test("orden del día: por la cita de la agenda con su ventana; con ruta publicada, por sus paradas", async ({ page }) => {
        const ordenes = ordenesDelDia();
        const hoy = ordenes[0]?.programadaInicio?.slice(0, 10) ?? "";
        const o103 = ordenes.find((o) => o.id === 103);
        if (o103) {
            // La agenda la adelantó: la cita manda sobre `programadaInicio`.
            o103.cita = { tecnicoId: 51, inicio: `${hoy}T14:00:00Z`, fin: `${hoy}T15:00:00Z`, ventanaInicio: `${hoy}T13:30:00Z`, ventanaFin: `${hoy}T15:00:00Z` };
        }
        await simularApiCampo(page, { ordenes });
        await page.goto("/campo");
        const tarjetas = page.getByRole("list", { name: "Órdenes del día" }).getByRole("link");
        await expect(tarjetas).toHaveCount(3);
        await expect(tarjetas.nth(0)).toContainText("OT-000103");
        await expect(tarjetas.nth(0)).toContainText("08:00");
        await expect(tarjetas.nth(0)).toContainText("Llegada acordada 07:30–09:00");
        await expect(tarjetas.nth(1)).toContainText("OT-000101");
        await expect(page.getByText("por hora de cita")).toBeVisible();

        await page.unrouteAll({ behavior: "ignoreErrors" });
        await simularApiCampo(page, { ordenes, ruta: { id: 7, ordenIds: [102, 101, 103] } });
        await page.getByRole("button", { name: "Menú" }).click();
        await page.getByRole("button", { name: "Actualizar órdenes del día" }).click();
        await expect(page.getByText("en orden de ruta")).toBeVisible();
        await expect(tarjetas.nth(0)).toContainText("OT-000102");
        await expect(tarjetas.nth(0).getByLabel("Parada 1")).toBeVisible();
        await expect(tarjetas.nth(2)).toContainText("OT-000103");
    });
});

test("capturas de la ubicación en vivo", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `campo-ubicacion-${que}-${info.project.name}.png`);
    await conSesionTecnico(context, baseURL ?? "");
    const api = await simularApiCampo(page);
    await abrirCampo(page);
    await indicador(page).click();
    await page.getByRole("dialog", { name: "Ubicación en vivo" }).getByRole("button", { name: "Iniciar turno" }).click();
    await expect(page.getByRole("dialog", { name: "¿Compartir tu ubicación?" })).toBeVisible();
    await page.screenshot({ path: nombre("01-explicacion") });

    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation(GUATE);
    api.sinRed = true;
    await context.setOffline(true);
    await page.getByRole("dialog", { name: "¿Compartir tu ubicación?" }).getByRole("button", { name: "Permitir ubicación" }).click();
    await expect(indicador(page)).toContainText("Ubicación activa");
    await expect(indicador(page)).toContainText("Pendientes de enviar: 1");
    await page.screenshot({ path: nombre("02-indicador") });
    await indicador(page).click();
    await expect(page.getByTestId("posiciones-pendientes")).toHaveText("1");
    await page.screenshot({ path: nombre("03-panel") });
});
