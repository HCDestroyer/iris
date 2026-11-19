import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { simularTokens } from "./api-tokens";
import { ADMIN, CONSULTA, SUPERVISOR_ZONA, conSesion, simularApi } from "./api-falsa";

/*
 * Pedido del PM: «poder eliminar tokens que ya no se usan, aparte de revocarlos». Administración →
 * Tokens de API (revocar, eliminar, revocar y eliminar, y el lote) e Integraciones (eliminar
 * conectores apagados). En escritorio y teléfono, claro y oscuro.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function sinDesborde(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

async function preparar(page: Page, context: Parameters<typeof conSesion>[0], baseURL: string | undefined, correo = ADMIN.correo) {
    const general = await simularApi(page);
    const tokens = await simularTokens(page);
    await conSesion(context, baseURL ?? "", correo);
    return { general, tokens };
}

/** Abre el menú «…» del token y elige la acción. */
async function elegir(page: Page, id: number, nombre: string, accion: string) {
    await page.locator(`[data-token="${id}"]`).getByRole("button", { name: `Acciones de ${nombre}` }).click();
    await page.getByRole("menuitem", { name: accion, exact: true }).click();
}

test.describe("tokens de API", () => {
    test("Administración lleva a la pantalla; por defecto solo los vigentes", async ({ page, context, baseURL }, info) => {
        await preparar(page, context, baseURL);
        await page.goto("/administracion");
        await page.getByRole("list", { name: "Secciones de administración" }).getByRole("link", { name: /Tokens de API/ }).click();
        await expect(page).toHaveURL(/\/administracion\/tokens$/);
        await expect(page.getByRole("heading", { name: "Tokens de API" })).toBeVisible();
        const migas = page.getByRole("navigation", { name: "Ruta de navegación" }).filter({ visible: true });
        await expect(migas.locator('[aria-current="page"]')).toHaveText("Tokens de API");
        const lista = page.getByRole("list", { name: "Tokens" });
        await expect(lista.locator("[data-token]")).toHaveCount(1);
        await expect(page.locator('[data-token="1"]')).toContainText("Vigente");
        await expect(page.locator('[data-token="1"]')).toContainText("Ana López");
        await page.getByText("Mostrar revocados y vencidos").click();
        await expect(lista.locator("[data-token]")).toHaveCount(4);
        await expect(page.locator('[data-token="2"]')).toContainText("Revocado");
        await expect(page.locator('[data-token="3"]')).toContainText("Vencido");
        // 2 y 3 se pueden eliminar; 4 lo usa un conector activo.
        await expect(page.getByTestId("eliminar-inactivos")).toHaveText(/Eliminar revocados y vencidos \(2\)/);
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `tokens-${info.project.name}.png`), fullPage: true });
    });

    test("eliminar uno revocado: confirmación, aviso y sale de la lista", async ({ page, context, baseURL }) => {
        const { tokens } = await preparar(page, context, baseURL);
        await page.goto("/administracion/tokens");
        await page.getByText("Mostrar revocados y vencidos").click();
        await elegir(page, 2, "Bot de Horus 2025", "Eliminar");
        const dialogo = page.getByRole("alertdialog", { name: "¿Eliminar «Bot de Horus 2025»?" });
        await expect(dialogo).toContainText("sigue en la auditoría");
        await dialogo.getByRole("button", { name: "Eliminar" }).click();
        await expect(page.getByText("Token eliminado")).toBeVisible();
        await expect(page.locator('[data-token="2"]')).toHaveCount(0);
        const pedido = tokens.peticiones.find((p) => p.ruta === "/api/v1/tokens/2/eliminar");
        expect(pedido?.idempotencia).toBeTruthy();
        await expect(page.getByTestId("eliminar-inactivos")).toHaveText(/\(1\)/);
    });

    test("sobre uno vigente: «Revocar y eliminar» en un solo diálogo (dos acciones)", async ({ page, context, baseURL }, info) => {
        const { tokens } = await preparar(page, context, baseURL);
        await page.goto("/administracion/tokens");
        await elegir(page, 1, "Script de reportes", "Revocar y eliminar");
        const dialogo = page.getByRole("alertdialog", { name: "¿Revocar y eliminar «Script de reportes»?" });
        await expect(dialogo).toContainText("Deja de valer ahora");
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `tokens-revocar-eliminar-${info.project.name}.png`) });
        await dialogo.getByRole("button", { name: "Revocar y eliminar" }).click();
        await expect(page.getByText("Token revocado y eliminado")).toBeVisible();
        await expect(page.locator('[data-token="1"]')).toHaveCount(0);
        const rutas = tokens.peticiones.filter((p) => p.metodo === "POST").map((p) => p.ruta);
        expect(rutas).toEqual(["/api/v1/tokens/1/revocar", "/api/v1/tokens/1/eliminar"]);
    });

    test("«Revocar» deja el token en la lista como revocado", async ({ page, context, baseURL }) => {
        await preparar(page, context, baseURL);
        await page.goto("/administracion/tokens");
        await page.getByText("Mostrar revocados y vencidos").click();
        await elegir(page, 1, "Script de reportes", "Revocar");
        await page.getByRole("alertdialog", { name: "¿Revocar «Script de reportes»?" }).getByRole("button", { name: "Revocar" }).click();
        await expect(page.getByText("Token revocado", { exact: true })).toBeVisible();
        await expect(page.locator('[data-token="1"]')).toContainText("Revocado");
    });

    test("lote: dice cuántos, elimina los elegibles y avisa de los que usa un conector", async ({ page, context, baseURL }) => {
        const { tokens } = await preparar(page, context, baseURL);
        await page.goto("/administracion/tokens");
        await page.getByTestId("eliminar-inactivos").click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Eliminar 2 tokens revocados y vencidos?" });
        await dialogo.getByRole("button", { name: "Eliminar" }).click();
        await expect(page.getByText("2 tokens eliminados")).toBeVisible();
        await expect(page.getByText("1 token siguen: los usa un conector activo.")).toBeVisible();
        await expect(page.getByTestId("eliminar-inactivos")).toHaveCount(0);
        expect(tokens.peticiones.filter((p) => p.ruta === "/api/v1/tokens/eliminar-inactivos")).toHaveLength(1);
        await page.getByText("Mostrar revocados y vencidos").click();
        await expect(page.getByRole("list", { name: "Tokens" }).locator("[data-token]")).toHaveCount(2);
    });

    test("uno que usa un conector activo: el error se ve en el diálogo", async ({ page, context, baseURL }) => {
        await preparar(page, context, baseURL);
        await page.goto("/administracion/tokens");
        await page.getByText("Mostrar revocados y vencidos").click();
        await elegir(page, 4, "Argos (canje)", "Eliminar");
        const dialogo = page.getByRole("alertdialog", { name: "¿Eliminar «Argos (canje)»?" });
        await dialogo.getByRole("button", { name: "Eliminar" }).click();
        await expect(dialogo.getByText("No se pudo completar")).toBeVisible();
        await expect(dialogo).toContainText("Desactiva o reemplaza el conector");
        await dialogo.getByRole("button", { name: "Cancelar" }).click();
        await expect(page.locator('[data-token="4"]')).toBeVisible();
    });

    test("si la lista falla: error visible con «Reintentar»", async ({ page, context, baseURL }) => {
        await preparar(page, context, baseURL);
        let fallar = true;
        await page.route("**/api/v1/tokens?*", async (route) => {
            if (!fallar) return route.fallback();
            fallar = false;
            return route.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }) });
        });
        await page.goto("/administracion/tokens");
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.locator('[data-token="1"]')).toBeVisible();
    });

    test("sin tokens.ver: «Sin permiso»", async ({ page, context, baseURL }) => {
        await preparar(page, context, baseURL, CONSULTA.correo);
        await page.goto("/administracion/tokens");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });
});

test.describe("integraciones: eliminar conectores apagados", () => {
    test("eliminar uno apagado: confirmación, aviso y sale de la lista", async ({ page, context, baseURL }, info) => {
        const { general } = await preparar(page, context, baseURL);
        await page.goto("/administracion/integraciones");
        const apagado = page.locator('[data-conector="2"]');
        await expect(page.locator('[data-conector="1"]').getByRole("button", { name: "Eliminar" })).toHaveCount(0); // activo
        await apagado.getByRole("button", { name: "Eliminar" }).click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Eliminar el conector de Argos?" });
        await expect(dialogo).toContainText("su token se elimina");
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `integraciones-eliminar-${info.project.name}.png`) });
        await dialogo.getByRole("button", { name: "Eliminar" }).click();
        await expect(page.getByText("Conector de Argos eliminado")).toBeVisible();
        await expect(apagado).toHaveCount(0);
        expect(general.sso.eliminaciones[0]?.ruta).toBe("/api/v1/integraciones/conectores/2/eliminar");
        expect(general.sso.eliminaciones[0]?.idempotencia).toBeTruthy();
    });

    test("«Eliminar apagados» dice cuántos; uno en uso por la reconexión no se ofrece", async ({ page, context, baseURL }) => {
        const { general } = await preparar(page, context, baseURL);
        const base = general.sso.conectores.find((c) => c.id === 2);
        if (!base) throw new Error("falta el conector 2 de la API falsa");
        general.sso.conectores.push({ ...structuredClone(base), id: 3, eliminable: false });
        await page.goto("/administracion/integraciones");
        await expect(page.locator('[data-conector="3"]').getByTestId("conector-en-uso")).toBeVisible();
        await expect(page.locator('[data-conector="3"]').getByRole("button", { name: "Eliminar" })).toHaveCount(0);
        await page.getByTestId("eliminar-apagados").click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Eliminar 1 conector apagado?" });
        await dialogo.getByRole("button", { name: "Eliminar" }).click();
        await expect(page.getByText("1 conector eliminado")).toBeVisible();
        await expect(page.locator('[data-conector="2"]')).toHaveCount(0);
        await expect(page.locator('[data-conector="3"]')).toBeVisible();
        await expect(page.getByTestId("eliminar-apagados")).toHaveCount(0);
    });

    test("sin integraciones.eliminar no se ofrece eliminar", async ({ page, context, baseURL }) => {
        await preparar(page, context, baseURL, SUPERVISOR_ZONA.correo);
        await page.goto("/administracion/integraciones");
        await expect(page.locator('[data-conector="2"]')).toBeVisible();
        await expect(page.getByRole("button", { name: /Eliminar/ })).toHaveCount(0);
    });
});
