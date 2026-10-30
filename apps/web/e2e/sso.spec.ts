import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CODIGO_OK, CONSULTA, CONTRASENA, SOPORTE, SUPERVISOR_ZONA, conSesion, simularApi } from "./api-falsa";
import { TICKETS } from "./api-sso";
import { avisoError } from "./ayudas";

/*
 * Autologin desde Argos (`/sso/entrar#<ticket>`), re-autenticación de una sesión sso, cuentas
 * vinculadas (Mi perfil y detalle del usuario) y conectores (aviso de reemplazo y revisión del
 * origen antes de conectar).
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function sinDesbordar(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

/** Entra desde Argos con el ticket de Sofía (sesión sso) y espera el ticket de destino. */
async function entrarDesdeArgos(page: Page) {
    await page.goto(`/sso/entrar#${TICKETS.sesion}`);
    await expect(page).toHaveURL(/\/tickets\/1$/);
}

test.describe("/sso/entrar", () => {
    test("sesion: canjea sin cabeceras del panel, borra el ticket de la URL y va al destino", async ({ page }) => {
        const api = await simularApi(page);
        await entrarDesdeArgos(page);
        expect(api.sso.canjes).toHaveLength(1);
        expect(api.sso.canjes[0]).toMatchObject({ cuerpo: { ticket: TICKETS.sesion }, csrf: false, empresa: false });
        expect(page.url()).not.toContain(TICKETS.sesion);
        expect(api.sso.sesionesSSO.has(SOPORTE.correo)).toBe(true);
    });

    test("un destino que no es interno (otra URL) lleva al inicio", async ({ page }) => {
        await simularApi(page);
        await page.goto(`/sso/entrar#${TICKETS.destinoExterno}`);
        await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
    });

    test("mfa: pide el código como el login y sigue al destino", async ({ page }) => {
        await simularApi(page);
        await page.goto(`/sso/entrar#${TICKETS.mfa}`);
        await expect(page.getByRole("heading", { name: "Verificación en dos pasos" })).toBeVisible();
        await page.getByLabel("Código de verificación").fill("000000");
        await page.getByRole("button", { name: "Verificar" }).click();
        await expect(page.getByText("Código incorrecto o ya usado")).toBeVisible();
        await page.getByLabel("Código de verificación").fill(CODIGO_OK);
        await page.getByRole("button", { name: "Verificar" }).click();
        await expect(page).toHaveURL(/\/ordenes\/1$/);
    });

    test("confirmar_cambio: «Entrar como…» repite el canje con confirmar; «Quedarme como…» no", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/sso/entrar#${TICKETS.sesion}`);
        await expect(page.getByRole("heading", { name: "Hay una sesión abierta con otra cuenta" })).toBeVisible();
        await expect(page.getByText(`Ya hay una sesión de ${ADMIN.nombre}.`)).toBeVisible();
        await page.getByRole("button", { name: `Quedarme como ${ADMIN.nombre}` }).click();
        await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
        expect(api.sso.canjes).toHaveLength(1);

        await page.goto(`/sso/entrar#${TICKETS.sesion}`);
        await page.getByRole("button", { name: `Entrar como ${SOPORTE.nombre}` }).click();
        await expect(page).toHaveURL(/\/tickets\/1$/);
        expect(api.sso.canjes.map((c) => c.cuerpo.confirmar ?? false)).toEqual([false, false, true]);
    });

    test("vincular: datos de Argos, login, confirmación explícita y 409 vinculo_requiere_admin sin botón de vincular", async ({ page }, info) => {
        const api = await simularApi(page);
        await page.goto(`/sso/entrar#${TICKETS.vincular}`);
        await expect(page.getByRole("heading", { name: "Tu cuenta de Argos no está vinculada" })).toBeVisible();
        const argos = page.getByTestId("cuenta-argos");
        await expect(argos).toContainText("argos-77");
        await expect(argos).toContainText("Carlos Ruiz (Argos)");
        await expect(argos).toContainText("carlos@iris.test");
        await expect(argos).toContainText("Sin verificar en Argos");
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `sso-01-vincular-${info.project.name}.png`), fullPage: true });

        await page.getByRole("button", { name: "Iniciar sesión para vincular" }).click();
        await page.getByLabel("Correo").fill(CONSULTA.correo);
        await page.getByLabel("Contraseña").fill(CONTRASENA);
        await page.getByRole("button", { name: "Entrar" }).click();
        await expect(page.getByRole("heading", { name: "¿Vincular estas cuentas?" })).toBeVisible();
        await expect(page.getByTestId("texto-vincular")).toContainText(`Vincular la cuenta de Argos argos-77 (Carlos Ruiz (Argos), carlos@iris.test) con tu cuenta de Iris ${CONSULTA.nombre} (${CONSULTA.correo})`);
        // Nunca automático: hasta pulsar «Vincular» no se pide nada.
        expect(api.sso.vinculaciones).toHaveLength(0);
        await page.screenshot({ path: path.join(carpeta, `sso-02-confirmar-${info.project.name}.png`), fullPage: true });

        await page.getByRole("button", { name: "Vincular", exact: true }).click();
        await expect(page.getByRole("heading", { name: "Pide a un administrador de Iris que vincule tu cuenta" })).toBeVisible();
        await expect(page.getByRole("button", { name: "Vincular", exact: true })).toHaveCount(0);
        expect(api.sso.vinculaciones).toEqual([{ idExterno: "argos-77", correo: CONSULTA.correo, cookie: true }]);
    });

    test("vincular con correo verificado: «Vincular» lleva al destino; «No fui yo» no vincula", async ({ page }) => {
        const api = await simularApi(page);
        const hastaConfirmar = async (ticket: string) => {
            await page.goto(`/sso/entrar#${ticket}`);
            await expect(page.getByTestId("cuenta-argos")).toContainText("Verificado en Argos");
            await page.getByRole("button", { name: "Iniciar sesión para vincular" }).click();
            await page.getByLabel("Correo").fill(CONSULTA.correo);
            await page.getByLabel("Contraseña").fill(CONTRASENA);
            await page.getByRole("button", { name: "Entrar" }).click();
            await expect(page.getByRole("heading", { name: "¿Vincular estas cuentas?" })).toBeVisible();
        };
        await hastaConfirmar(TICKETS.vincularVerificado);
        await page.getByRole("button", { name: "No fui yo" }).click();
        await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
        expect(api.sso.vinculaciones).toHaveLength(0);
        expect(api.sso.descartes()).toBe(1);

        await hastaConfirmar(TICKETS.vincularVerificado2);
        await page.getByRole("button", { name: "Vincular", exact: true }).click();
        await expect(page).toHaveURL(/\/comercial\/4$/);
        expect(api.sso.vinculos.get(CONSULTA.id)?.map((v) => v.usuarioExterno)).toEqual(["argos-77"]);
    });

    test("vincular: «No fui yo / Cancelar» descarta la vinculación sin iniciar sesión", async ({ page }) => {
        const api = await simularApi(page);
        await page.goto(`/sso/entrar#${TICKETS.vincular}`);
        await expect(page.getByRole("heading", { name: "Tu cuenta de Argos no está vinculada" })).toBeVisible();
        await page.getByRole("button", { name: "No fui yo / Cancelar" }).click();
        await expect(page).toHaveURL(/\/entrar/);
        expect(api.sso.descartes()).toBe(1);
        expect(api.sso.vinculaciones).toHaveLength(0);
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/sesion")).toHaveLength(0);
    });

    test("errores con texto y enlace al login; sin ticket o con uno mal formado no se pide nada", async ({ page }) => {
        const api = await simularApi(page);
        const casos: [string, string][] = [
            [TICKETS.usado, "El enlace venció o ya se usó"],
            [TICKETS.apagado, "Iris no está conectado con Argos"],
            [TICKETS.requiereMfa, "Tu cuenta necesita doble factor para entrar desde Argos"],
            [TICKETS.inactiva, "Esta cuenta no puede entrar"],
            [TICKETS.canjes, "Demasiados intentos en este momento"],
        ];
        for (const [ticket, titulo] of casos) {
            await page.goto("/entrar");
            await page.goto(`/sso/entrar#${ticket}`);
            await expect(page.getByRole("heading", { name: titulo })).toBeVisible();
            await expect(page.getByRole("button", { name: "Entrar con mi cuenta de Iris" })).toBeVisible();
        }
        const antes = api.sso.canjes.length;
        await page.goto("/sso/entrar");
        await expect(page.getByRole("heading", { name: "Abre Iris desde Argos" })).toBeVisible();
        await page.goto("/entrar");
        await page.goto("/sso/entrar#corto");
        await expect(page.getByRole("heading", { name: "El enlace venció o ya se usó" })).toBeVisible();
        expect(api.sso.canjes).toHaveLength(antes);
    });
});

test.describe("re-autenticación y cuentas vinculadas", () => {
    test("sesión desde Argos: «Quitar» un vínculo propio pide la contraseña y repite la acción", async ({ page }, info) => {
        const api = await simularApi(page);
        await entrarDesdeArgos(page);
        await page.goto("/cuenta");
        await expect(page.getByRole("heading", { name: "Mi perfil" })).toBeVisible();
        const vinculos = page.getByTestId("vinculos-sso");
        await expect(vinculos).toContainText("argos-6");
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `sso-03-mi-perfil-${info.project.name}.png`), fullPage: true });

        // Cancelar la re-autenticación: la acción queda con su error.
        await vinculos.getByRole("button", { name: /Quitar el vínculo/ }).click();
        await page.getByRole("dialog", { name: "Quitar el vínculo" }).getByRole("button", { name: "Quitar vínculo" }).click();
        const reaut = page.getByRole("dialog", { name: "Confirma tu contraseña" });
        await expect(reaut).toBeVisible();
        await reaut.getByRole("button", { name: "Cancelar" }).click();
        await expect(page.getByRole("dialog", { name: "Quitar el vínculo" }).getByText("No se pudo quitar el vínculo")).toBeVisible();

        await page.getByRole("dialog", { name: "Quitar el vínculo" }).getByRole("button", { name: "Quitar vínculo" }).click();
        await expect(reaut).toBeVisible();
        await reaut.getByLabel("Contraseña").fill("equivocada-1");
        await reaut.getByRole("button", { name: "Confirmar y seguir" }).click();
        await expect(reaut.getByText("La contraseña no es correcta")).toBeVisible();
        await expect(reaut.getByLabel("Contraseña")).toHaveValue("");
        await reaut.getByLabel("Contraseña").fill(CONTRASENA);
        await reaut.getByRole("button", { name: "Confirmar y seguir" }).click();
        await expect(reaut).toHaveCount(0);
        await expect(vinculos).toContainText("Sin cuentas vinculadas.");
        expect(api.sso.reautenticaciones()).toBe(1);
        expect(api.sso.vinculos.get(SOPORTE.id)).toEqual([]);
    });

    test("«Mi perfil» en el menú del usuario", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/");
        await page.getByRole("button", { name: `Tu cuenta: ${CONSULTA.nombre}` }).click();
        await page.getByRole("menuitem", { name: "Mi perfil" }).click();
        await expect(page).toHaveURL(/\/cuenta$/);
        await expect(page.getByTestId("vinculos-sso")).toContainText("Sin cuentas vinculadas.");
        await expect(page.getByRole("button", { name: "Vincular cuenta de Argos" })).toHaveCount(0);
    });

    test("administrador: detalle del usuario con «Vincular cuenta de Argos» y sus errores", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/usuarios");
        await page.getByRole("link", { name: SOPORTE.nombre }).filter({ visible: true }).click();
        await expect(page).toHaveURL(new RegExp(`/usuarios/${SOPORTE.id}$`));
        await expect(page.getByTestId("vinculos-sso")).toContainText("argos-6");

        await page.goto(`/usuarios/${CONSULTA.id}`);
        await expect(page.getByRole("heading", { name: CONSULTA.nombre })).toBeVisible();
        await page.getByRole("button", { name: "Vincular cuenta de Argos" }).click();
        const dialogo = page.getByRole("dialog", { name: "Vincular cuenta de Argos" });
        await dialogo.getByRole("combobox", { name: "Conector de Argos" }).selectOption({ label: "Argos · https://argos.fibra.test" });
        await dialogo.getByLabel("Id de la cuenta en Argos").fill("argos-6");
        await dialogo.getByRole("button", { name: "Vincular", exact: true }).click();
        await expect(dialogo.getByText("Esa cuenta de Argos ya está vinculada con otro usuario de Iris.")).toBeVisible();
        await dialogo.getByLabel("Id de la cuenta en Argos").fill("argos-77");
        await dialogo.getByRole("button", { name: "Vincular", exact: true }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.getByTestId("vinculos-sso")).toContainText("argos-77");
        expect(api.sso.vinculos.get(CONSULTA.id)?.[0]).toMatchObject({ conectorId: 1, usuarioExterno: "argos-77" });
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `sso-04-usuario-${info.project.name}.png`), fullPage: true });
    });
});

test.describe("conectores", () => {
    test("activar avisa que reemplaza al activo del mismo sistema", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion");
        await page.getByRole("link", { name: /Integraciones/ }).click();
        await expect(page).toHaveURL(/\/administracion\/integraciones$/);
        const nuevo = page.locator('[data-conector="2"]');
        await nuevo.getByRole("button", { name: "Activar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Activar el conector de Argos" });
        await expect(dialogo.getByTestId("aviso-reemplazo")).toContainText("Reemplazará al conector activo de Argos");
        await expect(dialogo.getByTestId("aviso-reemplazo")).toContainText("https://argos.fibra.test");
        await dialogo.getByRole("button", { name: "Activar y reemplazar" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(nuevo).toContainText("Activo");
        await expect(page.locator('[data-conector="1"]')).toContainText("Apagado");
        expect(api.sso.conectores.find((c) => c.id === 2)?.estado).toBe("activo");
        const pedido = api.peticiones.find((p) => p.ruta === "/api/v1/integraciones/conectores/2/activar");
        expect(pedido?.cuerpo).toEqual({ version: 1 });
        expect(pedido?.idempotencia).toBeTruthy();
    });

    test("conectar: revisa el origen antes de canjear; uno no permitido no deja seguir", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/integraciones");
        await page.getByRole("button", { name: "Conectar", exact: true }).click();
        const dialogo = page.getByRole("dialog", { name: "Conectar con otro sistema" });
        await dialogo.getByLabel("Enlace de conexión").fill("https://argos.malo.test/conectar#abc123");
        await dialogo.getByRole("button", { name: "Revisar enlace" }).click();
        await expect(dialogo.getByTestId("previsualizacion")).toContainText("https://argos.malo.test");
        await expect(dialogo.getByText("Este origen no está permitido")).toBeVisible();
        await expect(dialogo.getByRole("button", { name: /^Conectar con/ })).toHaveCount(0);
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `sso-05-origen-no-permitido-${info.project.name}.png`), fullPage: true });
        expect(api.peticiones.filter((p) => p.ruta.endsWith("/conectar"))).toHaveLength(0);

        await dialogo.getByRole("button", { name: "Volver" }).click();
        await dialogo.getByRole("combobox", { name: "Sistema" }).selectOption("horus");
        await dialogo.getByLabel("Enlace de conexión").fill("https://horus.fibra.test/conectar#abc123");
        await dialogo.getByRole("button", { name: "Revisar enlace" }).click();
        await expect(dialogo.getByText("El token de Iris irá a https://horus.fibra.test")).toBeVisible();
        await dialogo.getByRole("button", { name: "Conectar con https://horus.fibra.test" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.getByText("Conectado con Horus Flow (https://horus.fibra.test). Queda apagado hasta que lo actives.")).toBeVisible();
        await expect(page.locator("[data-conector]")).toHaveCount(3);
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/integraciones/horus/conectar")).toHaveLength(1);
    });

    test("con integraciones.conectar pero sin empresas.todas global no se ofrece «Conectar»", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", SUPERVISOR_ZONA.correo);
        await page.goto("/administracion/integraciones");
        await expect(page.locator("[data-conector]")).toHaveCount(2);
        await expect(page.getByRole("button", { name: "Conectar", exact: true })).toHaveCount(0);
    });

    test("sin integraciones.ver no aparece la sección", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/administracion/integraciones");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        await expect(avisoError(page)).toHaveCount(0);
    });
});
