import { expect, test } from "@playwright/test";
import { ADMIN, BLOQUEADO, CODIGO_BLOQUEA, CODIGO_OK, CON_MFA, CONTRASENA, CSRF, conSesion, simularApi } from "./api-falsa";
import { abrirMenu, avisoError, entrar, menu } from "./ayudas";

test.describe("inicio de sesión", () => {
    test("login correcto lleva al panel con el menú de sus permisos", async ({ page }) => {
        await simularApi(page);
        await page.goto("/");
        await expect(page).toHaveURL(/\/entrar$/);
        await entrar(page, ADMIN.correo, CONTRASENA);
        await expect(page).toHaveURL(/\/$/);
        await expect(page.getByRole("heading", { name: "Hola, Ana" })).toBeVisible();
        await abrirMenu(page);
        await expect(menu(page).getByRole("link", { name: "Usuarios" })).toBeVisible();
        await expect(menu(page).getByRole("link", { name: /Administración/ })).toBeVisible();
    });

    test("valida el formulario antes de llamar a la API", async ({ page }) => {
        const api = await simularApi(page);
        await page.goto("/entrar");
        await page.getByRole("button", { name: "Entrar" }).click();
        await expect(page.getByText("Escribe tu correo")).toBeVisible();
        await expect(page.getByText("Escribe tu contraseña")).toBeVisible();
        await page.getByLabel("Correo").fill("ana");
        await page.getByRole("button", { name: "Entrar" }).click();
        await expect(page.getByText("Escribe un correo válido")).toBeVisible();
        expect(api.mutaciones).toHaveLength(0);
    });

    test("credenciales inválidas muestran el error", async ({ page }) => {
        await simularApi(page);
        await page.goto("/entrar");
        await entrar(page, ADMIN.correo, "mala");
        await expect(avisoError(page)).toContainText("Correo o contraseña incorrectos");
        await expect(page).toHaveURL(/\/entrar$/);
    });

    test("429: muestra cuánto esperar y bloquea el botón", async ({ page }) => {
        await simularApi(page);
        await page.goto("/entrar");
        await entrar(page, BLOQUEADO, "loquesea");
        await expect(page.getByText("Demasiados intentos, espera 9 min")).toBeVisible();
        await expect(page.getByRole("button", { name: "Entrar" })).toBeDisabled();
    });

    test("login con MFA: código inválido y luego correcto, con X-CSRF-Token", async ({ page }) => {
        const api = await simularApi(page);
        await page.goto("/entrar?siguiente=%2Fusuarios");
        await entrar(page, CON_MFA.correo, CONTRASENA);
        await expect(page.getByRole("heading", { name: "Verificación en dos pasos" })).toBeVisible();
        const codigo = page.getByLabel("Código de verificación");
        await codigo.fill("111111");
        await page.getByRole("button", { name: "Verificar" }).click();
        await expect(page.getByText("Código incorrecto o ya usado")).toBeVisible();
        await codigo.fill(CODIGO_OK);
        await page.getByRole("button", { name: "Verificar" }).click();
        await expect(page).toHaveURL(/\/usuarios$/);
        await expect(page.getByRole("heading", { name: "Usuarios" })).toBeVisible();
        const mfa = api.mutaciones.filter((m) => m.ruta === "/api/v1/sesion/mfa");
        expect(mfa).toHaveLength(2);
        expect(mfa.every((m) => m.csrf === CSRF)).toBe(true);
    });

    test("MFA bloqueado vuelve al inicio de sesión", async ({ page }) => {
        await simularApi(page);
        await page.goto("/entrar");
        await entrar(page, CON_MFA.correo, CONTRASENA);
        await page.getByLabel("Código de verificación").fill(CODIGO_BLOQUEA);
        await page.getByRole("button", { name: "Verificar" }).click();
        await expect(page.getByRole("heading", { name: "Iniciar sesión" })).toBeVisible();
        await expect(avisoError(page)).toContainText("Demasiados códigos inválidos");
        await expect(page.getByLabel("Contraseña")).toHaveValue("");
    });

    test("sin sesión, una ruta del panel manda al inicio de sesión y vuelve al entrar", async ({ page }) => {
        await simularApi(page);
        await page.goto("/usuarios");
        await expect(page).toHaveURL(/\/entrar\?siguiente=%2Fusuarios$/);
        await entrar(page, ADMIN.correo, CONTRASENA);
        await expect(page).toHaveURL(/\/usuarios$/);
    });

    test("una cookie vencida (401 en /yo) manda al inicio de sesión", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", "desconocido@iris.test");
        await page.goto("/");
        await expect(page).toHaveURL(/\/entrar\?motivo=expirada$/);
        await expect(page.getByText("Tu sesión terminó")).toBeVisible();
    });

    test("cerrar sesión con CSRF vuelve al inicio de sesión", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await page.getByRole("button", { name: "Cerrar sesión" }).click();
        await expect(page).toHaveURL(/\/entrar$/);
        expect(api.mutaciones).toContainEqual({ metodo: "DELETE", ruta: "/api/v1/sesion", csrf: CSRF });
        const cookies = await context.cookies();
        expect(cookies.find((c) => c.name === "iris_sesion")).toBeUndefined();
        await page.goto("/usuarios");
        await expect(page).toHaveURL(/\/entrar\?siguiente=%2Fusuarios$/);
    });
});
