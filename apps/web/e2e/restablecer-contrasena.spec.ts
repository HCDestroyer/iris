import path from "node:path";
import { expect, test } from "@playwright/test";
import { ADMIN, CON_MFA, CSRF, ELENA, LECTOR, SOPORTE, conSesion, simularApi } from "./api-falsa";
import { avisoError } from "./ayudas";

/*
 * «Restablecer contraseña» de otra persona (`usuarios.restablecer_contrasena`): desde la lista
 * de usuarios y desde la ficha, con la contraseña temporal generada, copiada y mostrada por
 * última vez en el resultado.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const PATRON_TEMPORAL = /^[^-]{4}-[^-]{4}-[^-]{4}-[^-]{4}$/;

test.describe("restablecer contraseña desde Usuarios", () => {
    test("genera la temporal, no revoca tokens por omisión, manda versión e Idempotency-Key y la muestra una vez", async ({ page, context, baseURL, browserName }) => {
        const api = await simularApi(page);
        if (browserName === "chromium") {
            await context.grantPermissions(["clipboard-read", "clipboard-write"]);
        }
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        // Se ofrece con o sin MFA, nunca sobre uno mismo ni sobre una integración.
        await expect(page.getByRole("button", { name: `Restablecer contraseña de ${LECTOR.nombre}` })).toBeVisible();
        await expect(page.getByRole("button", { name: `Restablecer contraseña de ${CON_MFA.nombre}` })).toBeVisible();
        await expect(page.getByRole("button", { name: `Restablecer contraseña de ${ADMIN.nombre}` })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Restablecer contraseña de Integración Argos" })).toHaveCount(0);

        await page.getByRole("button", { name: `Restablecer contraseña de ${LECTOR.nombre}` }).click();
        const dialogo = page.getByRole("dialog", { name: `Restablecer contraseña de ${LECTOR.nombre}` });
        await expect(dialogo.getByText("cierra todas sus sesiones")).toBeVisible();
        const campo = dialogo.getByLabel("Contraseña temporal");
        await expect(campo).toHaveValue(PATRON_TEMPORAL);
        await expect(dialogo.getByLabel("Revocar también sus tokens de API")).not.toBeChecked();
        await dialogo.getByRole("button", { name: "Restablecer", exact: true }).click();
        await expect(dialogo.getByText("Escribe el motivo")).toBeVisible();
        expect(api.cuenta.restablecimientos).toHaveLength(0);

        await dialogo.getByRole("button", { name: "Generar otra" }).click();
        await expect(campo).toHaveValue(PATRON_TEMPORAL);
        const generada = await campo.inputValue();
        if (browserName === "chromium") {
            await dialogo.getByRole("button", { name: "Copiar" }).click();
            await expect(dialogo.getByRole("button", { name: "Copiada" })).toBeVisible();
            expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(generada);
        }

        await dialogo.getByLabel("Motivo").fill("Olvidó su contraseña; verificado por llamada");
        await dialogo.getByRole("button", { name: "Restablecer", exact: true }).click();
        const resultado = page.getByRole("dialog", { name: "Contraseña restablecida" });
        await expect(resultado.getByText(`${LECTOR.nombre} ya tiene una contraseña temporal.`)).toBeVisible();
        await expect(resultado.getByText("Se cerraron sus 2 sesiones abiertas.")).toBeVisible();
        await expect(resultado.getByTestId("resultado-tokens")).toHaveText("Su verificación en dos pasos y sus tokens de API siguen igual.");
        await expect(resultado.getByTestId("contrasena-temporal")).toHaveText(generada);
        await expect(resultado.getByText("Entrégala fuera de banda")).toBeVisible();
        await expect(page.getByText(`Contraseña de ${LECTOR.nombre} restablecida`)).toBeVisible();

        expect(api.cuenta.restablecimientos).toHaveLength(1);
        expect(api.cuenta.restablecimientos[0]?.cuerpo).toEqual({
            version: 1,
            motivo: "Olvidó su contraseña; verificado por llamada",
            contrasenaTemporal: generada,
            revocarTokens: false,
        });
        expect(api.cuenta.restablecimientos[0]?.idempotencia).toBeTruthy();
        expect(api.mutaciones).toContainEqual({ metodo: "POST", ruta: `/api/v1/usuarios/${LECTOR.id}/restablecer-contrasena`, csrf: CSRF });

        // Al cerrar, la contraseña ya no se ve en ningún lado.
        await resultado.getByRole("button", { name: "Listo" }).click();
        await expect(resultado).toHaveCount(0);
        await expect(page.getByText(generada)).toHaveCount(0);
    });

    test("409: «Recargar» trae la versión nueva sin perder lo escrito; con revocar tokens", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.cuenta.tokens.revocados = 2;
        api.cuenta.tokens.conservados = 0;
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await page.getByRole("button", { name: `Restablecer contraseña de ${LECTOR.nombre}` }).click();
        const dialogo = page.getByRole("dialog", { name: `Restablecer contraseña de ${LECTOR.nombre}` });
        await dialogo.getByLabel("Motivo").fill("Sospecha de acceso ajeno");
        await dialogo.getByLabel("Contraseña temporal").fill("temporal-de-prueba-1");
        await dialogo.getByLabel("Revocar también sus tokens de API").check();
        api.cuenta.cambiarVersion(LECTOR.id);
        await dialogo.getByRole("button", { name: "Restablecer", exact: true }).click();
        await expect(dialogo.getByText("Alguien más cambió este usuario mientras lo veías")).toBeVisible();
        await dialogo.getByRole("button", { name: "Recargar" }).click();
        await expect(dialogo.getByText("Alguien más cambió este usuario")).toHaveCount(0);
        await expect(dialogo.getByLabel("Motivo")).toHaveValue("Sospecha de acceso ajeno");
        await dialogo.getByRole("button", { name: "Restablecer", exact: true }).click();
        await expect(page.getByRole("dialog", { name: "Contraseña restablecida" }).getByText("Se revocaron sus tokens de API.")).toBeVisible();
        const [primero, segundo] = api.cuenta.restablecimientos;
        expect(primero?.cuerpo.version).toBe(1);
        expect(segundo?.cuerpo).toMatchObject({ version: 2, revocarTokens: true, contrasenaTemporal: "temporal-de-prueba-1" });
        expect(segundo?.idempotencia).not.toBe(primero?.idempotencia);
    });

    test("404 fuera de alcance: lo dice sin cambiar nada y recarga la lista", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await page.getByRole("button", { name: `Restablecer contraseña de ${CON_MFA.nombre}` }).click();
        const dialogo = page.getByRole("dialog", { name: `Restablecer contraseña de ${CON_MFA.nombre}` });
        await dialogo.getByLabel("Motivo").fill("Olvidó su contraseña");
        api.cuenta.sacarDelAlcance(CON_MFA.id);
        await dialogo.getByRole("button", { name: "Restablecer", exact: true }).click();
        await expect(avisoError(page)).toContainText("Este usuario ya no está en tu alcance");
        await dialogo.getByRole("button", { name: "Recargar" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.locator("main").getByText(CON_MFA.correo)).toHaveCount(0);
    });

    test("no se ofrece sobre uno mismo ni sin usuarios.restablecer_contrasena", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ELENA.correo);
        await page.goto("/usuarios");
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(page.locator("main").getByText(ELENA.correo).filter({ visible: true })).toBeVisible();
        await expect(page.getByRole("button", { name: `Restablecer contraseña de ${LECTOR.nombre}` })).toBeVisible();
        await expect(page.getByRole("button", { name: `Restablecer contraseña de ${ELENA.nombre}` })).toHaveCount(0);

        await context.clearCookies();
        await conSesion(context, baseURL ?? "", SOPORTE.correo);
        await page.goto("/usuarios");
        await expect(page.locator("main").getByText(LECTOR.correo).filter({ visible: true })).toBeVisible();
        await expect(page.getByRole("button", { name: /^Restablecer contraseña/ })).toHaveCount(0);
    });

    test("desde la ficha del usuario", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/usuarios/${LECTOR.id}`);
        await page.getByRole("button", { name: `Restablecer contraseña de ${LECTOR.nombre}` }).click();
        const dialogo = page.getByRole("dialog", { name: `Restablecer contraseña de ${LECTOR.nombre}` });
        await dialogo.getByLabel("Motivo").fill("Olvidó su contraseña");
        await dialogo.getByRole("button", { name: "Restablecer", exact: true }).click();
        await expect(page.getByRole("dialog", { name: "Contraseña restablecida" })).toBeVisible();
        expect(api.cuenta.restablecimientos).toHaveLength(1);
        expect(api.cuenta.restablecimientos[0]?.id).toBe(LECTOR.id);
    });
});

test("capturas de restablecer contraseña", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `restablecer-${que}-${info.project.name}.png`);
    await simularApi(page);
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/usuarios");
    await expect(page.getByRole("button", { name: `Restablecer contraseña de ${LECTOR.nombre}` })).toBeVisible();
    await page.screenshot({ path: nombre("01-lista"), fullPage: true });
    await page.getByRole("button", { name: `Restablecer contraseña de ${LECTOR.nombre}` }).click();
    const dialogo = page.getByRole("dialog");
    await dialogo.getByLabel("Motivo").fill("Olvidó su contraseña; verificado por llamada");
    await page.screenshot({ path: nombre("02-dialogo") });
    await dialogo.getByRole("button", { name: "Restablecer", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "Contraseña restablecida" })).toBeVisible();
    await page.screenshot({ path: nombre("03-resultado") });
});
