import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ACTUAL_BLOQUEA, CODIGO_ACTIVAR } from "./api-cuenta";
import { ADMIN, CON_MFA, CONTRASENA, CSRF, ELENA, LECTOR, SOPORTE, TEMPORAL, conSesion, simularApi } from "./api-falsa";
import { avisoError, entrar } from "./ayudas";

/*
 * Cuenta propia y reinicio de MFA (PR #21 de I1): cambio de contraseña obligatorio con una
 * contraseña temporal, cambio voluntario desde el menú del usuario, configurar el MFA propio y
 * «Reiniciar MFA» desde Usuarios.
 */

const NUEVA = "una frase bien larga 2026";
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function llenarCambio(page: Page, actual: string, nueva: string, confirmar = nueva) {
    const formulario = page.getByRole("form", { name: "Cambiar contraseña" });
    await formulario.getByLabel(/^Contraseña (temporal|actual)$/).fill(actual);
    await formulario.getByLabel("Contraseña nueva", { exact: true }).fill(nueva);
    await formulario.getByLabel("Repite la contraseña nueva").fill(confirmar);
    await formulario.getByRole("button", { name: "Cambiar contraseña" }).click();
}

async function sinDesbordar(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

test.describe("contraseña temporal", () => {
    test("el login lleva a cambiarla, valida como la API, ofrece el MFA y vuelve a donde iba", async ({ page }) => {
        const api = await simularApi(page);
        await page.goto("/entrar?siguiente=%2Fusuarios");
        await entrar(page, TEMPORAL.correo, CONTRASENA);
        await expect(page).toHaveURL(/\/cambiar-contrasena\?siguiente=%2Fusuarios$/);
        await expect(page.getByRole("heading", { name: "Cambia tu contraseña" })).toBeVisible();
        await expect(page.getByText("Entraste con una contraseña temporal")).toBeVisible();
        // Solo cambiarla o salir: ni menú ni empresa.
        await expect(page.getByRole("navigation", { name: "Principal" })).toHaveCount(0);
        await sinDesbordar(page);

        const formulario = page.getByRole("form", { name: "Cambiar contraseña" });
        await formulario.getByRole("button", { name: "Cambiar contraseña" }).click();
        await expect(formulario.getByText("Escribe tu contraseña actual")).toBeVisible();
        await expect(formulario.getByText("Usa al menos 12 caracteres")).toBeVisible();
        await llenarCambio(page, CONTRASENA, CONTRASENA);
        await expect(formulario.getByText("Debe ser distinta de la actual")).toBeVisible();
        await llenarCambio(page, CONTRASENA, NUEVA, "otra cosa distinta");
        await expect(formulario.getByText("No coincide con la contraseña nueva")).toBeVisible();
        expect(api.mutaciones.filter((m) => m.ruta === "/api/v1/yo/contrasena")).toHaveLength(0);

        // 422 de la API en su campo (la actual no es la temporal).
        await llenarCambio(page, "no-es-la-temporal", NUEVA);
        await expect(formulario.getByText("La contraseña actual no es correcta")).toBeVisible();
        await expect(formulario.getByLabel("Contraseña temporal")).toHaveValue("");

        await llenarCambio(page, CONTRASENA, NUEVA);
        await expect(page.getByRole("heading", { name: "Contraseña cambiada" })).toBeVisible();
        await expect(page.getByText("Protege tu cuenta con verificación en dos pasos")).toBeVisible();
        const envios = api.peticiones.filter((p) => p.ruta === "/api/v1/yo/contrasena");
        expect(envios).toHaveLength(2);
        expect(envios[1]?.cuerpo).toEqual({ actual: CONTRASENA, nueva: NUEVA });
        expect(envios[1]?.idempotencia).toBeTruthy();
        expect(envios[1]?.idempotencia).not.toBe(envios[0]?.idempotencia);
        expect(api.mutaciones.filter((m) => m.ruta === "/api/v1/yo/contrasena").every((m) => m.csrf === CSRF)).toBe(true);

        // Configurar el MFA: QR + clave, código inválido y luego el bueno.
        await page.getByRole("link", { name: "Configurar ahora" }).click();
        await expect(page).toHaveURL(/\/cuenta\/mfa\?siguiente=%2Fusuarios$/);
        await page.getByRole("button", { name: "Empezar" }).click();
        await expect(page.getByRole("img", { name: "Código QR para tu app de autenticación" })).toBeVisible();
        await expect(page.getByTestId("secreto-mfa")).toHaveText("JBSW Y3DP EHPK 3PXP");
        await sinDesbordar(page);
        const codigo = page.getByLabel("Código de verificación");
        await codigo.fill("000000");
        await page.getByRole("button", { name: "Activar" }).click();
        await expect(page.getByText("El código no coincide")).toBeVisible();
        await codigo.fill(CODIGO_ACTIVAR);
        await page.getByRole("button", { name: "Activar" }).click();
        await expect(page.getByRole("heading", { name: "Verificación activada" })).toBeVisible();
        await page.getByRole("button", { name: "Continuar" }).click();
        await expect(page).toHaveURL(/\/usuarios$/);
        await expect(page.getByRole("heading", { name: "Usuarios" })).toBeVisible();
    });

    test("con la sesión abierta, el panel lleva a cambiarla sin leer nada más y deja salir", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", TEMPORAL.correo);
        await page.goto("/usuarios");
        await expect(page).toHaveURL(/\/cambiar-contrasena\?siguiente=%2Fusuarios$/);
        await expect(page.getByRole("heading", { name: "Cambia tu contraseña" })).toBeVisible();
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/usuarios")).toBe(false);
        await page.getByRole("button", { name: "Cerrar sesión" }).click();
        await expect(page).toHaveURL(/\/entrar$/);
        expect(api.mutaciones).toContainEqual({ metodo: "DELETE", ruta: "/api/v1/sesion", csrf: CSRF });
    });

    test("un 403 cambio_contrasena_requerido en cualquier petición lleva a la pantalla", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        api.responderProblema("/api/v1/usuarios", { type: "urn:iris:problema:cambio_contrasena_requerido", title: "Cambio de contraseña requerido", status: 403 }, 1);
        await page.goto("/usuarios");
        await expect(page).toHaveURL(/\/cambiar-contrasena\?siguiente=%2Fusuarios$/);
        await expect(page.getByRole("form", { name: "Cambiar contraseña" })).toBeVisible();
    });

    test("429 demasiados_intentos: la API cerró la sesión, se explica y se vuelve a entrar", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", TEMPORAL.correo);
        await page.goto("/cambiar-contrasena?siguiente=%2Ftickets");
        await llenarCambio(page, ACTUAL_BLOQUEA, NUEVA);
        await expect(page.getByText("Demasiados intentos; vuelve a entrar en 15 min")).toBeVisible();
        expect((await context.cookies()).find((c) => c.name === "iris_sesion")).toBeUndefined();
        await page.getByRole("link", { name: "Ir a iniciar sesión" }).click();
        await expect(page).toHaveURL(/\/entrar\?siguiente=%2Ftickets$/);
    });
});

test.describe("cambiar contraseña desde el menú del usuario", () => {
    test("pide la actual, muestra el 422 en su campo y confirma el cambio", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await page.getByRole("button", { name: /^Tu cuenta/ }).click();
        await expect(page.getByRole("menu").getByText(ADMIN.correo)).toBeVisible();
        await page.getByRole("menuitem", { name: "Cambiar contraseña" }).click();
        const dialogo = page.getByRole("dialog", { name: "Cambiar contraseña" });
        await expect(dialogo).toBeVisible();
        await llenarCambio(page, "mala-contrasena", NUEVA);
        await expect(dialogo.getByText("La contraseña actual no es correcta")).toBeVisible();
        await llenarCambio(page, CONTRASENA, NUEVA);
        await expect(dialogo.getByText("Contraseña cambiada")).toBeVisible();
        await dialogo.getByRole("button", { name: "Listo" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page).toHaveURL(/\/usuarios$/);
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/yo/contrasena").map((p) => p.cuerpo)).toEqual([
            { actual: "mala-contrasena", nueva: NUEVA },
            { actual: CONTRASENA, nueva: NUEVA },
        ]);
    });

    test("429 en el diálogo: avisa y lleva a entrar", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await page.getByRole("button", { name: /^Tu cuenta/ }).click();
        await page.getByRole("menuitem", { name: "Cambiar contraseña" }).click();
        await llenarCambio(page, ACTUAL_BLOQUEA, NUEVA);
        const dialogo = page.getByRole("dialog", { name: "Cambiar contraseña" });
        await expect(dialogo.getByText("Demasiados intentos; vuelve a entrar en 15 min")).toBeVisible();
        await dialogo.getByRole("link", { name: "Ir a iniciar sesión" }).click();
        await expect(page).toHaveURL(/\/entrar$/);
    });

    test("ofrece configurar el MFA solo a quien no lo tiene", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ELENA.correo);
        await page.goto("/");
        await page.getByRole("button", { name: /^Tu cuenta/ }).click();
        await expect(page.getByRole("menuitem", { name: "Verificación en dos pasos activa" })).toBeDisabled();
        await expect(page.getByRole("menuitem", { name: "Configurar verificación en dos pasos" })).toHaveCount(0);
    });
});

test.describe("reiniciar MFA desde Usuarios", () => {
    test("genera y copia la temporal, revoca tokens por omisión, manda versión e Idempotency-Key y muestra el resultado", async ({ page, context, baseURL, browserName }) => {
        const api = await simularApi(page);
        if (browserName === "chromium") {
            await context.grantPermissions(["clipboard-read", "clipboard-write"]);
        }
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await expect(page.getByRole("button", { name: `Reiniciar MFA de ${CON_MFA.nombre}` })).toBeVisible();
        // Sin MFA no hay nada que reiniciar.
        await expect(page.getByRole("button", { name: `Reiniciar MFA de ${LECTOR.nombre}` })).toHaveCount(0);
        await expect(page.getByRole("button", { name: `Reiniciar MFA de ${ADMIN.nombre}` })).toHaveCount(0);

        await page.getByRole("button", { name: `Reiniciar MFA de ${CON_MFA.nombre}` }).click();
        const dialogo = page.getByRole("dialog", { name: `Reiniciar MFA de ${CON_MFA.nombre}` });
        await expect(dialogo.getByText("Cerrará todas sus sesiones")).toBeVisible();
        await expect(dialogo.getByText("No se vuelve a mostrar")).toBeVisible();
        await expect(dialogo.getByLabel("Revocar también sus tokens de API")).toBeChecked();
        await dialogo.getByRole("button", { name: "Reiniciar MFA" }).click();
        await expect(dialogo.getByText("Escribe el motivo")).toBeVisible();
        await expect(dialogo.getByText("La contraseña temporal necesita al menos 12 caracteres")).toBeVisible();
        expect(api.cuenta.reinicios).toHaveLength(0);

        await dialogo.getByRole("button", { name: "Generar una segura" }).click();
        const temporal = dialogo.getByLabel("Contraseña temporal");
        await expect(temporal).toHaveValue(/^[^-]{4}-[^-]{4}-[^-]{4}-[^-]{4}$/);
        const generada = await temporal.inputValue();
        await expect(dialogo.getByText("La contraseña temporal necesita al menos 12 caracteres")).toHaveCount(0);
        await dialogo.getByRole("button", { name: "Copiar" }).click();
        await expect(dialogo.getByRole("button", { name: "Copiada" })).toBeVisible();
        await expect(dialogo.getByText("Se borra del portapapeles en 45 s.")).toBeVisible();
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(generada);

        await dialogo.getByLabel("Motivo").fill("Cambió de teléfono; verificado por llamada");
        await dialogo.getByRole("button", { name: "Reiniciar MFA" }).click();
        const resultado = page.getByRole("dialog", { name: "MFA reiniciado" });
        await expect(resultado.getByText(`Reiniciamos la verificación en dos pasos de ${CON_MFA.nombre}`)).toBeVisible();
        await expect(resultado.getByText("Se cerraron todas sus sesiones abiertas.")).toBeVisible();
        await expect(resultado.getByText("Se revocaron sus tokens de API.")).toBeVisible();
        await expect(resultado.getByText(generada)).toHaveCount(0);
        await expect(resultado.locator("input")).toHaveCount(0);

        expect(api.cuenta.reinicios).toHaveLength(1);
        expect(api.cuenta.reinicios[0]?.cuerpo).toEqual({
            version: 1,
            motivo: "Cambió de teléfono; verificado por llamada",
            contrasenaTemporal: generada,
            revocarTokens: true,
        });
        expect(api.cuenta.reinicios[0]?.idempotencia).toBeTruthy();
        expect(api.mutaciones).toContainEqual({ metodo: "POST", ruta: `/api/v1/usuarios/${CON_MFA.id}/reiniciar-mfa`, csrf: CSRF });

        // Pasar al resultado no borra lo copiado; cerrar el diálogo sí.
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(generada);
        await resultado.getByRole("button", { name: "Listo" }).click();
        await expect(resultado).toHaveCount(0);
        await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe("");
        await expect(page.getByRole("button", { name: `Reiniciar MFA de ${CON_MFA.nombre}` })).toHaveCount(0);
    });

    test("409: «Recargar» trae la versión nueva sin perder lo escrito; sin revocar tokens", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await page.getByRole("button", { name: `Reiniciar MFA de ${CON_MFA.nombre}` }).click();
        const dialogo = page.getByRole("dialog", { name: `Reiniciar MFA de ${CON_MFA.nombre}` });
        await dialogo.getByLabel("Motivo").fill("Perdió el teléfono");
        await dialogo.getByLabel("Contraseña temporal").fill("temporal-de-prueba-1");
        await dialogo.getByLabel("Revocar también sus tokens de API").uncheck();
        api.cuenta.cambiarVersion(CON_MFA.id);
        await dialogo.getByRole("button", { name: "Reiniciar MFA" }).click();
        await expect(dialogo.getByText("Alguien más cambió este usuario mientras lo veías")).toBeVisible();
        await dialogo.getByRole("button", { name: "Recargar" }).click();
        await expect(dialogo.getByText("Alguien más cambió este usuario")).toHaveCount(0);
        await expect(dialogo.getByLabel("Motivo")).toHaveValue("Perdió el teléfono");
        await expect(dialogo.getByLabel("Contraseña temporal")).toHaveValue("temporal-de-prueba-1");
        await dialogo.getByRole("button", { name: "Reiniciar MFA" }).click();
        await expect(page.getByRole("dialog", { name: "MFA reiniciado" }).getByText("Sus tokens de API siguen vigentes.")).toBeVisible();
        const [primero, segundo] = api.cuenta.reinicios;
        expect(primero?.cuerpo.version).toBe(1);
        expect(segundo?.cuerpo).toMatchObject({ version: 2, revocarTokens: false, contrasenaTemporal: "temporal-de-prueba-1" });
        expect(segundo?.idempotencia).not.toBe(primero?.idempotencia);
    });

    test("404 fuera de alcance: lo dice sin cambiar nada y recarga la lista", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await page.getByRole("button", { name: `Reiniciar MFA de ${CON_MFA.nombre}` }).click();
        const dialogo = page.getByRole("dialog", { name: `Reiniciar MFA de ${CON_MFA.nombre}` });
        await dialogo.getByLabel("Motivo").fill("Cambió de teléfono");
        await dialogo.getByRole("button", { name: "Generar una segura" }).click();
        api.cuenta.sacarDelAlcance(CON_MFA.id);
        await dialogo.getByRole("button", { name: "Reiniciar MFA" }).click();
        await expect(avisoError(page)).toContainText("Este usuario ya no está en tu alcance");
        await dialogo.getByRole("button", { name: "Recargar la lista" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.locator("main").getByText(LECTOR.correo).filter({ visible: true })).toBeVisible();
        await expect(page.locator("main").getByText(CON_MFA.correo)).toHaveCount(0);
    });

    test("no se ofrece sobre uno mismo ni sin usuarios.reiniciar_mfa", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ELENA.correo);
        await page.goto("/usuarios");
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(page.locator("main").getByText(ELENA.correo).filter({ visible: true })).toBeVisible();
        await expect(page.getByRole("button", { name: `Reiniciar MFA de ${CON_MFA.nombre}` })).toBeVisible();
        await expect(page.getByRole("button", { name: `Reiniciar MFA de ${ELENA.nombre}` })).toHaveCount(0);

        await context.clearCookies();
        await conSesion(context, baseURL ?? "", SOPORTE.correo);
        await page.goto("/usuarios");
        await expect(page.locator("main").getByText(CON_MFA.correo).filter({ visible: true })).toBeVisible();
        await expect(page.getByRole("button", { name: /^Reiniciar MFA/ })).toHaveCount(0);
    });
});

test("capturas de la cuenta y del reinicio de MFA", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `cuenta-${que}-${info.project.name}.png`);
    const api = await simularApi(page);
    await conSesion(context, baseURL ?? "", TEMPORAL.correo);
    await page.goto("/cambiar-contrasena?siguiente=%2Fusuarios");
    await expect(page.getByRole("heading", { name: "Cambia tu contraseña" })).toBeVisible();
    await page.screenshot({ path: nombre("01-obligatorio"), fullPage: true });
    await llenarCambio(page, ACTUAL_BLOQUEA, NUEVA);
    await expect(page.getByText(/Demasiados intentos/)).toBeVisible();
    await page.screenshot({ path: nombre("02-429"), fullPage: true });

    await conSesion(context, baseURL ?? "", TEMPORAL.correo);
    await page.goto("/cambiar-contrasena?siguiente=%2Fusuarios");
    await llenarCambio(page, CONTRASENA, NUEVA);
    await expect(page.getByRole("heading", { name: "Contraseña cambiada" })).toBeVisible();
    await page.screenshot({ path: nombre("03-cambiada"), fullPage: true });
    await page.getByRole("link", { name: "Configurar ahora" }).click();
    await page.getByRole("button", { name: "Empezar" }).click();
    await expect(page.getByRole("img", { name: /Código QR/ })).toBeVisible();
    await page.screenshot({ path: nombre("04-mfa"), fullPage: true });

    await context.clearCookies();
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/usuarios");
    await page.getByRole("button", { name: /^Tu cuenta/ }).click();
    await expect(page.getByRole("menu")).toBeVisible();
    await page.screenshot({ path: nombre("05-menu-usuario") });
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: `Reiniciar MFA de ${CON_MFA.nombre}` }).click();
    const dialogo = page.getByRole("dialog");
    await dialogo.getByLabel("Motivo").fill("Cambió de teléfono; verificado por llamada");
    await dialogo.getByRole("button", { name: "Generar una segura" }).click();
    await page.screenshot({ path: nombre("06-reiniciar-mfa") });
    api.cuenta.cambiarVersion(CON_MFA.id);
    await dialogo.getByRole("button", { name: "Reiniciar MFA" }).click();
    await expect(dialogo.getByText(/Alguien más cambió/)).toBeVisible();
    await page.screenshot({ path: nombre("07-reiniciar-409") });
    await dialogo.getByRole("button", { name: "Recargar" }).click();
    await dialogo.getByRole("button", { name: "Reiniciar MFA" }).click();
    await expect(page.getByRole("dialog", { name: "MFA reiniciado" })).toBeVisible();
    await page.screenshot({ path: nombre("08-reiniciado") });
});
