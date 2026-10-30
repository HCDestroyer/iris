import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";

/*
 * «Conectar con Argos» (contrato Iris ↔ ArgosISP 0.4.0 §1.4): el botón abre el panel de Argos en la
 * misma pestaña y la vuelta (`/administracion/integraciones/argos/vuelta#…`) completa la conexión
 * una sola vez, muestra el resumen y ofrece «Activar». La tarjeta del conector muestra panel y API
 * por separado.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const PANEL = "https://panel.argos.fibra.test";
const API_ARGOS = "https://argos.fibra.test";

async function sinDesbordar(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

/** El panel de Argos (otro origen) se simula: solo importa adónde llega el navegador. */
async function simularPanelArgos(page: Page) {
    await page.route(`${PANEL}/**`, (route) => route.fulfill({ status: 200, contentType: "text/html", body: "<!doctype html><title>Argos</title><p>Autorizar Iris</p>" }));
}

/** Pulsa «Conectar con Argos» con `panel` y devuelve el state de la URL de autorizar. */
async function conectarConArgos(page: Page, panel: string): Promise<string> {
    const campo = page.getByLabel("Dirección del panel de Argos");
    await campo.fill(panel);
    await page.getByRole("button", { name: "Conectar con Argos" }).click();
    await page.waitForURL(/panel\.argos\.fibra\.test\/integraciones\/iris\/autorizar\?/);
    const u = new URL(page.url());
    expect(u.origin).toBe(PANEL);
    expect(u.searchParams.get("code_challenge_method")).toBe("S256");
    return u.searchParams.get("state") ?? "";
}

test.describe("Conectar con Argos", () => {
    test("la tarjeta muestra panel y API por separado; sin dirección válida no se puede conectar", async ({ page, context, baseURL }, info) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/integraciones");
        const activo = page.locator('[data-conector="1"]');
        await expect(activo.getByTestId("panel-conector")).toHaveText(PANEL);
        await expect(activo.getByTestId("api-conector")).toHaveText(API_ARGOS);
        // Conector anterior a 0.4.0: sin panel informado.
        await expect(page.locator('[data-conector="2"]').getByTestId("panel-conector")).toContainText("Sin informar");

        const tarjeta = page.getByTestId("conectar-argos");
        const campo = tarjeta.getByLabel("Dirección del panel de Argos");
        // Sugiere el panel del conector activo para volver a autorizar.
        await expect(campo).toHaveValue(PANEL);
        await campo.fill("");
        await expect(tarjeta.getByRole("button", { name: "Conectar con Argos" })).toBeDisabled();
        await campo.fill("argos sin esquema");
        await expect(tarjeta.getByText("Debe ser la dirección del panel")).toBeVisible();
        await expect(tarjeta.getByRole("button", { name: "Conectar con Argos" })).toBeDisabled();
        await campo.fill(PANEL);
        await expect(tarjeta.getByRole("button", { name: "Conectar con Argos" })).toBeEnabled();
        await expect(tarjeta.getByTestId("conectando-como")).toContainText(`Conectando como ${ADMIN.nombre}`);
        // El enlace manual de §1.3 sigue como opción avanzada.
        await expect(page.getByRole("button", { name: "Conectar con un enlace" })).toBeVisible();
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `argos-01-integraciones-${info.project.name}.png`), fullPage: true });
    });

    test("flujo feliz: abre Argos, vuelve, borra el fragmento, completa una vez y activa", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await simularPanelArgos(page);
        await page.goto("/administracion/integraciones");
        const state = await conectarConArgos(page, `${PANEL}/inicio`);
        expect(api.sso.autorizaciones.get(state)?.panelUrl).toBe(PANEL);
        const pedido = api.peticiones.find((p) => p.ruta === "/api/v1/integraciones/argosisp/autorizaciones");
        expect(pedido?.cuerpo).toEqual({ panelUrl: `${PANEL}/inicio` });
        expect(pedido?.idempotencia).toBeTruthy();

        // Argos aprueba y vuelve con todo en el fragmento.
        await page.goto(`/administracion/integraciones/argos/vuelta#codigo=codigo-bueno&api=${encodeURIComponent(API_ARGOS)}&state=${state}`);
        const resumen = page.getByTestId("resumen-conexion");
        await expect(resumen).toBeVisible();
        expect(page.url()).not.toContain("#");
        expect(page.url()).not.toContain("codigo-bueno");
        await expect(page.locator('meta[name="referrer"]')).toHaveAttribute("content", "no-referrer");
        await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", /noindex/);
        await expect(resumen.getByTestId("conectando-como")).toContainText(`Conectando como ${ADMIN.nombre}`);
        await expect(resumen.getByTestId("resumen-panel")).toHaveText(PANEL);
        await expect(resumen.getByTestId("resumen-api")).toHaveText(API_ARGOS);
        await expect(resumen.getByTestId("resumen-alcance")).toHaveText("Solo las empresas de la lista");
        await expect(resumen.getByRole("list", { name: "Empresas que verá Iris" })).toContainText("Fibra Norte");
        await expect(resumen.getByRole("list", { name: "Empresas que verá Iris" })).toContainText("Empresa 11");
        await expect(resumen.getByTestId("aviso-reemplazo")).toContainText(PANEL);
        expect(api.sso.completados).toHaveLength(1);
        expect(api.sso.completados[0]?.cuerpo).toEqual({ codigo: "codigo-bueno", api: API_ARGOS, state });
        expect(api.sso.completados[0]?.idempotencia).toBeTruthy();
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `argos-02-resumen-${info.project.name}.png`), fullPage: true });

        await resumen.getByRole("button", { name: "Activar" }).click();
        await expect(page.getByTestId("argos-activo")).toBeVisible();
        const nuevo = api.sso.conectores.at(-1);
        expect(nuevo?.estado).toBe("activo");
        expect(nuevo?.origen).toBe("autorizacion");
        expect(api.sso.conectores.find((c) => c.id === 1)?.estado).toBe("apagado");
        // Un solo envío de «completar» (la vuelta no se repite).
        expect(api.sso.completados).toHaveLength(1);
    });

    test("rechazo en Argos: texto claro y «Reintentar» vuelve a abrir Argos", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await simularPanelArgos(page);
        await page.goto("/administracion/integraciones");
        const state = await conectarConArgos(page, PANEL);
        await page.goto(`/administracion/integraciones/argos/vuelta#error=rechazado&state=${state}`);
        const error = page.getByTestId("error-vuelta");
        await expect(error).toContainText("Rechazaste la conexión en Argos");
        expect(page.url()).not.toContain("#");
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `argos-03-rechazo-${info.project.name}.png`), fullPage: true });
        await error.getByRole("button", { name: "Reintentar" }).click();
        await page.waitForURL(/panel\.argos\.fibra\.test\/integraciones\/iris\/autorizar\?/);
        expect(api.sso.autorizaciones.size).toBe(2);
    });

    test("errores de la vuelta: state inválido y API no permitida", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/administracion/integraciones/argos/vuelta#codigo=x&api=${encodeURIComponent(API_ARGOS)}&state=${"z".repeat(43)}`);
        await expect(page.getByTestId("error-vuelta")).toContainText("Esta vuelta de Argos no es válida");
        await expect(page.getByTestId("error-vuelta").getByRole("button", { name: "Reintentar" })).toBeVisible();

        api.sso.autorizaciones.set("y".repeat(43), { panelUrl: PANEL, usada: false });
        // Otra carga completa (cambiar solo el fragmento no vuelve a montar la página).
        await page.goto("/administracion/integraciones");
        await page.goto(`/administracion/integraciones/argos/vuelta#codigo=x&api=${encodeURIComponent("https://argos.malo.test")}&state=${"y".repeat(43)}`);
        await expect(page.getByTestId("error-vuelta")).toContainText("La API de Argos no está permitida");
    });

    test("un panel que no es el de ningún conector pide confirmación antes de ir", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.route("https://otro-argos.test/**", (route) => route.fulfill({ status: 200, contentType: "text/html", body: "<!doctype html><title>Otro</title>" }));
        await page.goto("/administracion/integraciones");
        const tarjeta = page.getByTestId("conectar-argos");
        await tarjeta.getByLabel("Dirección del panel de Argos").fill("https://otro-argos.test");
        await tarjeta.getByRole("button", { name: "Conectar con Argos" }).click();
        const aviso = tarjeta.getByTestId("confirmar-panel");
        await expect(aviso).toContainText("https://otro-argos.test no es el panel de tu conector de Argos");
        await expect(aviso).toContainText(PANEL);
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/integraciones/argosisp/autorizaciones")).toHaveLength(0);
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `argos-04-confirmar-panel-${info.project.name}.png`), fullPage: true });
        await aviso.getByRole("button", { name: "Cancelar" }).click();
        await expect(aviso).toHaveCount(0);
        await tarjeta.getByRole("button", { name: "Conectar con Argos" }).click();
        await tarjeta.getByTestId("confirmar-panel").getByRole("button", { name: "Sí, es mi Argos" }).click();
        await page.waitForURL(/otro-argos\.test\/integraciones\/iris\/autorizar\?/);
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/integraciones/argosisp/autorizaciones")).toHaveLength(1);
    });

    test("409 idempotencia_en_proceso: espera y repite con la misma clave", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        const state = "p".repeat(43);
        api.sso.autorizaciones.set(state, { panelUrl: PANEL, usada: false });
        api.sso.enProceso.add(state);
        await page.goto(`/administracion/integraciones/argos/vuelta#codigo=codigo-bueno&api=${encodeURIComponent(API_ARGOS)}&state=${state}`);
        await expect(page.getByTestId("resumen-conexion")).toBeVisible();
        expect(api.sso.completados).toHaveLength(2);
        expect(api.sso.completados[0]?.idempotencia).toBeTruthy();
        expect(api.sso.completados[1]?.idempotencia).toBe(api.sso.completados[0]?.idempotencia);
    });

    test("sin fragmento no hay nada que completar", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/integraciones/argos/vuelta");
        await expect(page.getByText("No hay nada que completar")).toBeVisible();
        expect(api.sso.completados).toHaveLength(0);
    });
});
