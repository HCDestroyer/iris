import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, SOPORTE, simularApi } from "./api-falsa";
import type { OpcionesAsistente } from "./api-asistente";
import type { OpcionesFase3 } from "./api-asistente3";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";
import { esTelefono } from "./ayudas";

/*
 * Reconexión temporal sin personal de caja (contrato con Argos 0.8.0 §7.3, B4): el interruptor de
 * la empresa, apagado de fábrica, con su estado en llano (Argos, rol Caja, quién está), «Guardar»
 * con aviso, 409 y «Reintentar». En los 4 proyectos y sin desbordes en 390 px ni 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-asistente-reconexion");
const RUTA = "/administracion/asistente/reconexion";
const TRES_B: OpcionesFase3 = { autonomoDisponible: true, ajustes: { modoAutonomo: "sin_nadie" } };

async function abrir(page: Page, asistente: OpcionesAsistente = {}, correo = ADMIN.correo, ruta = RUTA) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, { conversaciones: { asistente: { ...asistente, fase3: TRES_B } } });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}

const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: true });
const formulario = (page: Page) => page.getByRole("form", { name: "Reconexión temporal" });
const interruptor = (page: Page) => formulario(page).getByRole("switch", { name: /También cuando nadie de caja está disponible/ });
const estado = (page: Page) => page.getByTestId("estado-sin-personal");

test.describe("Asistente › reconexión sin personal de caja", () => {
    test("apagado de fábrica; encenderlo dice si se pedirá y guarda con aviso", async ({ page }) => {
        const api = await abrir(page);
        await expect(formulario(page)).toBeVisible();
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Reconexión temporal");
        await expect(interruptor(page)).not.toBeChecked();
        await expect(estado(page)).toContainText("Apagado");
        await esperarSinDesbordes(page);
        await captura(page, "reconexion-apagado");
        await interruptor(page).click();
        await expect(estado(page)).toHaveAttribute("data-tono", "success");
        await expect(estado(page)).toContainText("2 personas de caja; ninguna está disponible ahora.");
        await esperarSinDesbordes(page);
        await captura(page, "reconexion-encendido");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Reconexión sin personal de caja: encendida")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/asistente$/);
        expect(api.conversaciones.asistente.guardadosReconexion).toEqual([{ version: 0, sinPersonal: true }]);
    });

    test("Argos viejo o sin nadie con el rol Caja: lo dice en llano", async ({ page }) => {
        await abrir(page, { reconexion: { sinPersonal: true, version: 3, argosLoPermite: false } });
        await expect(interruptor(page)).toBeChecked();
        await expect(estado(page)).toHaveAttribute("data-tono", "warning");
        await expect(estado(page)).toContainText("Argos todavía no lo permite");
        await expect(estado(page)).toContainText("0.8.0");
        await esperarSinDesbordes(page);
        await captura(page, "reconexion-argos-viejo");
    });

    test("con «Reconexión temporal» apagada en «¿Qué puede hacer solo?» no dice «Listo»", async ({ page }) => {
        await abrir(page, { reconexion: { sinPersonal: true, version: 1, accionEncendida: false } });
        await expect(estado(page)).toHaveAttribute("data-tono", "warning");
        await expect(estado(page)).toContainText("«Reconexión temporal» está apagada");
        await expect(estado(page)).not.toContainText("Listo");
    });

    test("sin nadie con el rol Caja", async ({ page }) => {
        await abrir(page, { reconexion: { sinPersonal: true, version: 1, caja: { total: 0, disponibles: 0 } } });
        await expect(estado(page)).toContainText("Nadie tiene el rol Caja");
    });

    test("409: alguien más lo cambió", async ({ page }) => {
        await abrir(page, { conflictoReconexion: true });
        await interruptor(page).click();
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Alguien más cambió este ajuste").first()).toBeVisible();
    });

    test("si no se pudo leer: error con «Reintentar» y sin estado inventado", async ({ page }) => {
        await abrir(page, { fallasReconexion: 1 });
        await expect(page.getByRole("button", { name: "Reintentar" })).toBeVisible();
        await expect(estado(page)).toHaveCount(0);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(interruptor(page)).toBeVisible();
    });

    test("solo el admin: soporte no lo ve", async ({ page }) => {
        await abrir(page, {}, SOPORTE.correo);
        await expect(formulario(page)).toHaveCount(0);
    });

    test("se llega desde el asistente", async ({ page }) => {
        await abrir(page, {}, ADMIN.correo, "/administracion/asistente");
        await page.getByRole("navigation", { name: "Ajustes avanzados" }).getByRole("link", { name: "Reconexión temporal" }).click();
        await expect(page).toHaveURL(/\/asistente\/reconexion$/);
        await expect(formulario(page)).toBeVisible();
    });
});
