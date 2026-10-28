import path from "node:path";
import { expect, test } from "@playwright/test";
import { ADMIN, BLOQUEADO, CON_MFA, CONTRASENA, LECTOR, conSesion, simularApi } from "./api-falsa";
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

    await page.goto("/tickets");
    await expect(page.getByRole("heading", { name: "Próximamente" })).toBeVisible();
    await page.screenshot({ path: nombre("05-proximamente"), fullPage: true });

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
