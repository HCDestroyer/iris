import path from "node:path";
import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { conSesionTecnico, simularApiCampo, type ApiCampo } from "./api-campo";
import { TECNICO } from "./datos-campo";

/*
 * Turno de la agenda en la app de campo (`Paquete.turno`), con el reloj de Playwright: la
 * ubicación se enciende y apaga sola por tramos, un «Terminar turno» a mano se respeta hasta el
 * tramo siguiente, «En camino» fuera de tramo la enciende igual, un día sin turno sigue como
 * siempre y el día cambia a medianoche de Guatemala (paquete y turno del día nuevo).
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const GUATE = { latitude: 14.6407, longitude: -90.5133, accuracy: 12 };
const DIA = "2026-10-28";
const MANANA = "2026-10-29";

/** Hora de Guatemala (UTC−6, sin horario de verano) como `Date`. */
const GT = (hora: string, dia = DIA) => new Date(`${dia}T${hora}:00-06:00`);
const tramo = (inicio: Date, fin: Date) => ({ inicio: inicio.toISOString(), fin: fin.toISOString() });

function turnoDe(tramos: { inicio: string; fin: string }[]): Schema<"TurnoCampo"> {
    return { inicio: tramos[0]?.inicio ?? "", fin: tramos.at(-1)?.fin ?? "", tramos };
}

/** 08:00–12:00 y 13:00–17:00 ese día. */
const diaPartido = (fecha: string) => turnoDe([tramo(GT("08:00", fecha), GT("12:00", fecha)), tramo(GT("13:00", fecha), GT("17:00", fecha))]);

const indicador = (page: Page) => page.getByTestId("indicador-ubicacion");
const notificacion = (page: Page) => page.locator('[data-slot="notificacion"]');
const puntosEnviados = (api: ApiCampo) => api.posiciones.flatMap((l) => l.puntos ?? []);

/** Permiso dado y explicación aceptada en una visita anterior (así el turno puede encenderse solo). */
async function ubicacionYaPermitida(context: BrowserContext) {
    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation(GUATE);
    await context.addInitScript((clave) => window.localStorage.setItem(clave, "aceptada"), `iris-campo-ubicacion-explicada-${TECNICO.id}`);
}

async function abrirCampo(page: Page) {
    await page.goto("/campo");
    await expect(page.getByRole("heading", { name: "Mis órdenes de hoy" })).toBeVisible();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
}

/** Adelanta el reloj de la página hasta esa hora (los temporizadores vencidos corren una vez). */
async function irA(page: Page, hora: Date) {
    const ahora = await page.evaluate(() => Date.now());
    await page.clock.fastForward(hora.getTime() - ahora);
}

test.describe("en el teléfono", () => {
    test.beforeEach(async ({ context, baseURL }, info) => {
        test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
        await conSesionTecnico(context, baseURL ?? "");
    });

    test("se enciende y se apaga sola por tramos; «Terminar turno» a mano se respeta hasta el tramo siguiente", async ({ page, context }) => {
        test.setTimeout(90_000);
        await ubicacionYaPermitida(context);
        await page.clock.install({ time: GT("07:58") });
        const api = await simularApiCampo(page, { turno: (f) => (f === DIA ? diaPartido(f) : undefined) });
        await abrirCampo(page);
        await expect(indicador(page)).toHaveText("Fuera de turno · empieza 08:00");
        expect(api.fechasPaquete).toEqual([DIA]);

        // Empieza el tramo: se enciende sola, con el aviso de siempre.
        await irA(page, GT("08:00"));
        await expect(notificacion(page)).toContainText("Turno iniciado: se envía tu ubicación cada minuto");
        await expect(indicador(page)).toHaveText("Ubicación activa · En turno hasta 12:00");
        await expect.poll(() => puntosEnviados(api).length).toBe(1);

        await indicador(page).click();
        const panel = page.getByRole("dialog", { name: "Ubicación en vivo" });
        await expect(panel.getByTestId("turno-agenda")).toHaveText("Tu turno de hoy en la agenda: 08:00–12:00, 13:00–17:00.");

        // A mano dentro del tramo: no se vuelve a encender en este tramo.
        await panel.getByRole("button", { name: "Terminar turno" }).click();
        await expect(indicador(page)).toHaveText("Fuera de turno · empieza 13:00");
        await page.keyboard.press("Escape");
        await irA(page, GT("08:40"));
        await irA(page, GT("11:59"));
        await expect(indicador(page)).toHaveText("Fuera de turno · empieza 13:00");
        expect(puntosEnviados(api)).toHaveLength(1);

        // El tramo siguiente sí la enciende.
        await irA(page, GT("13:00"));
        await expect(indicador(page)).toHaveText("Ubicación activa · En turno hasta 17:00");
        await expect.poll(() => puntosEnviados(api).length).toBe(2);

        // Termina el tramo (sin trabajo en curso): se apaga sola y lo avisa.
        await irA(page, GT("17:00"));
        await expect(notificacion(page)).toContainText("Turno terminado: terminó tu horario de la agenda");
        await expect(indicador(page)).toHaveText("Fuera de turno");
        await irA(page, GT("17:05"));
        expect(puntosEnviados(api)).toHaveLength(2);
    });

    test("fuera de tramo «En camino» la enciende igual y, con órdenes abiertas, sigue al terminar el tramo", async ({ page, context }) => {
        test.setTimeout(60_000);
        await ubicacionYaPermitida(context);
        await page.clock.install({ time: GT("07:00") });
        const api = await simularApiCampo(page, { turno: (f) => (f === DIA ? turnoDe([tramo(GT("08:00"), GT("12:00"))]) : undefined) });
        await abrirCampo(page);
        await expect(indicador(page)).toHaveText("Fuera de turno · empieza 08:00");

        await page.getByRole("link", { name: /OT-000101/ }).click();
        await page.getByRole("button", { name: "En camino" }).click();
        await expect(notificacion(page)).toContainText("En camino, con ubicación. Turno iniciado: se envía tu ubicación cada minuto");
        await expect(indicador(page)).toHaveText("Ubicación activa · En turno fuera de horario");
        await expect.poll(() => puntosEnviados(api).length).toBe(1);

        await irA(page, GT("08:00"));
        await expect(indicador(page)).toHaveText("Ubicación activa · En turno hasta 12:00");
        // A las 12:00 quedan órdenes abiertas: sigue hasta cerrar la última.
        await irA(page, GT("12:00"));
        await expect(indicador(page)).toHaveText("Ubicación activa · En turno fuera de horario");
    });

    test("un día sin turno en la agenda sigue como siempre: no se enciende sola", async ({ page, context }) => {
        await ubicacionYaPermitida(context);
        await page.clock.install({ time: GT("09:00") });
        const api = await simularApiCampo(page);
        await abrirCampo(page);
        await expect(indicador(page)).toHaveText("Fuera de turno");
        await irA(page, GT("09:30"));
        await expect(indicador(page)).toHaveText("Fuera de turno");
        await indicador(page).click();
        await expect(page.getByTestId("turno-agenda")).toHaveText("Hoy no tienes turno en la agenda: el turno lo inicias tú, o «En camino» y «Llegué».");
        expect(api.intentosPosiciones).toHaveLength(0);
    });

    test("a medianoche de Guatemala cambia el día: termina el turno y baja el paquete y el turno del día nuevo", async ({ page, context }) => {
        test.setTimeout(60_000);
        await ubicacionYaPermitida(context);
        // 23:58 del 28 en Guatemala ya es el 29 en UTC: el día sigue siendo el 28.
        await page.clock.install({ time: GT("23:58") });
        const api = await simularApiCampo(page, {
            turno: (f) => (f === DIA ? turnoDe([tramo(GT("22:00"), GT("00:00", MANANA))]) : diaPartido(f)),
        });
        await abrirCampo(page);
        expect(api.fechasPaquete).toEqual([DIA]);
        await expect(indicador(page)).toHaveText("Ubicación activa · En turno hasta 00:00");
        await expect.poll(() => puntosEnviados(api).length).toBe(1);

        await irA(page, GT("00:01", MANANA));
        await expect.poll(() => api.fechasPaquete).toEqual([DIA, MANANA]);
        await expect(indicador(page)).toHaveText("Fuera de turno · empieza 08:00");
        const enviados = puntosEnviados(api).length;
        await irA(page, GT("00:05", MANANA));
        expect(puntosEnviados(api)).toHaveLength(enviados);
    });
});

test("capturas del turno de la agenda", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `campo-turno-${que}-${info.project.name}.png`);
    await conSesionTecnico(context, baseURL ?? "");
    await ubicacionYaPermitida(context);
    await page.clock.install({ time: GT("09:00") });
    await simularApiCampo(page, { turno: (f) => (f === DIA ? diaPartido(f) : undefined) });
    await abrirCampo(page);
    await expect(indicador(page)).toHaveText("Ubicación activa · En turno hasta 12:00");
    await page.screenshot({ path: nombre("01-en-turno") });
    await indicador(page).click();
    await expect(page.getByTestId("turno-agenda")).toBeVisible();
    await page.screenshot({ path: nombre("02-panel") });
});
