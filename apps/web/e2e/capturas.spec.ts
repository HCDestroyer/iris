import path from "node:path";
import { expect, test } from "@playwright/test";
import { ADMIN, BLOQUEADO, CON_MFA, CONSULTA, CONTRASENA, EMPRESAS, LECTOR, conSesion, simularApi } from "./api-falsa";
import { abrirMenu, entrar } from "./ayudas";

// Capturas para revisión visual: IRIS_CAPTURAS=<carpeta> (por defecto test-results/capturas).
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

test("capturas de inicio de sesión y del panel", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `${que}-${info.project.name}.png`);
    await simularApi(page);
    await page.goto("/entrar");
    await expect(page.getByRole("heading", { name: "Iniciar sesión" })).toBeVisible();
    await page.screenshot({ path: nombre("01-entrar"), fullPage: true });

    await entrar(page, BLOQUEADO, "x");
    await expect(page.getByText(/Demasiados intentos/)).toBeVisible();
    await page.screenshot({ path: nombre("02-entrar-429"), fullPage: true });

    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();
    await page.screenshot({ path: nombre("03-inicio"), fullPage: true });

    await page.goto("/usuarios");
    await expect(page.locator("main").getByText(LECTOR.correo).filter({ visible: true })).toBeVisible();
    await page.screenshot({ path: nombre("04-usuarios"), fullPage: true });

    await page.goto("/comercial");
    await expect(page.getByRole("heading", { name: "Comercial" })).toBeVisible();
    await page.screenshot({ path: nombre("05-comercial"), fullPage: true });

    if (info.project.name.startsWith("telefono")) {
        await abrirMenu(page);
        await page.screenshot({ path: nombre("06-menu-telefono") });
    }
});

test("captura del paso MFA", async ({ page }, info) => {
    await simularApi(page);
    await page.goto("/entrar");
    await entrar(page, CON_MFA.correo, CONTRASENA);
    await page.getByLabel("Código de verificación").fill("111111");
    await page.getByRole("button", { name: "Verificar" }).click();
    await expect(page.getByText("Código incorrecto o ya usado")).toBeVisible();
    await page.screenshot({ path: path.join(carpeta, `07-mfa-${info.project.name}.png`), fullPage: true });
});

test("capturas de tickets y empresa activa", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `${que}-${info.project.name}.png`);
    const api = await simularApi(page, { empresas: EMPRESAS, hayDatosSinEmpresa: true });
    await conSesion(context, baseURL ?? "", ADMIN.correo);

    await page.goto("/tickets");
    await expect(page.getByRole("heading", { name: "Elige la empresa" })).toBeVisible();
    await page.screenshot({ path: nombre("10-elegir-empresa"), fullPage: true });
    await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();

    await expect(page.locator('[data-ticket="TK-000101"]').filter({ visible: true })).toBeVisible();
    await page.screenshot({ path: nombre("11-bandeja"), fullPage: true });
    await page.screenshot({ path: nombre("12-barra-superior"), clip: { x: 0, y: 0, width: page.viewportSize()?.width ?? 390, height: 64 } });

    await page.getByRole("link", { name: "Nuevo ticket" }).click();
    await page.getByRole("form", { name: "Nuevo ticket" }).getByLabel("Cliente en Argos").click();
    await page.getByRole("combobox", { name: "Buscar cliente" }).fill("Castillo");
    await expect(page.getByRole("option", { name: /María Castillo/ })).toBeVisible();
    await page.screenshot({ path: nombre("13-alta-rapida") });
    await page.keyboard.press("Escape");

    await page.goto("/tickets/1");
    await expect(page.getByTestId("ficha-cliente")).toBeVisible();
    await page.screenshot({ path: nombre("14-detalle"), fullPage: true });

    await page.goto("/tickets/2");
    await expect(page.getByTestId("ficha-cliente")).toContainText("Desactualizado");
    api.cambiarVersion(2);
    await page.getByRole("group", { name: "Acciones del ticket" }).getByRole("button", { name: "Escalar" }).click();
    await page.getByRole("dialog").getByLabel("Motivo").fill("El cliente insiste");
    await page.getByRole("dialog").getByRole("button", { name: "Escalar" }).click();
    await expect(page.getByRole("dialog").getByText("Alguien más cambió este ticket")).toBeVisible();
    await page.screenshot({ path: nombre("15-accion-409") });
});

test("captura del detalle sin notas internas y conector apagado", async ({ page, context, baseURL }, info) => {
    await simularApi(page, { conectorApagado: true });
    await conSesion(context, baseURL ?? "", CONSULTA.correo);
    await page.goto("/tickets/1");
    await expect(page.getByText("Conector con Argos apagado")).toBeVisible();
    await page.screenshot({ path: path.join(carpeta, `16-detalle-lectura-${info.project.name}.png`), fullPage: true });
});
