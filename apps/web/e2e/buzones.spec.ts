import path from "node:path";
import type { Schema } from "@iris/contratos";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, simularApi, SUPERVISOR_ZONA } from "./api-falsa";
import { irA } from "./ayudas";

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const tarjeta = (page: Page, correo: string) => page.locator(`[data-buzon="${correo}"]`);

test.describe("buzones de correo", () => {
    test("Administración en el menú con buzones.ver y lista de buzones", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await irA(page, /Administración/);
        await page.getByRole("link", { name: /Buzones de correo/ }).click();
        await expect(page.getByRole("heading", { name: "Buzones de correo" })).toBeVisible();

        const soporte = tarjeta(page, "soporte@isp.gt");
        await expect(soporte).toContainText("Activo");
        await expect(soporte).toContainText("Verifica remitentes");
        await expect(soporte).toContainText("imap.isp.gt:993");
        await expect(soporte).toContainText("Intermitencia");
        const facturas = tarjeta(page, "facturas@isp.gt");
        await expect(facturas).toContainText("Inactivo");
        await expect(facturas).toContainText("Sin verificación de remitente");
        await expect(facturas).toContainText("Sin salida: las respuestas no se envían por correo");
        await expect(facturas).toContainText("No se pudo iniciar sesión en el servidor IMAP");
        // Nunca se muestra una contraseña (la API no la manda).
        await expect(page.getByText(/contrasena|secreta/)).toHaveCount(0);
    });

    test("sin permisos de escritura: solo lectura", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/administracion/buzones");
        await expect(page.getByRole("heading", { name: "No tienes permiso para ver esta sección" })).toBeVisible();
    });

    test("alta: valida espejo del backend y manda Idempotency-Key", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/buzones");
        await page.getByRole("button", { name: "Nuevo buzón" }).click();
        const d = page.getByRole("dialog");
        await d.getByRole("button", { name: "Crear buzón" }).click();
        await expect(d.getByText("Escribe un nombre")).toBeVisible();
        await expect(d.getByText("Escribe la contraseña IMAP")).toBeVisible();
        await expect(d.getByText("Sin authserv-id no se verifica a nadie")).toBeVisible();

        await d.getByLabel("Nombre").fill("Ventas");
        await d.getByLabel("Dirección").fill("ventas@isp.gt");
        await d.getByLabel("Servidor IMAP").fill("https://imap.isp.gt");
        await d.getByLabel("Usuario IMAP").fill("ventas@isp.gt");
        await d.getByLabel("Contraseña IMAP").fill("clave-imap");
        await d.getByRole("button", { name: "Crear buzón" }).click();
        await expect(d.getByText(/Servidor inválido/)).toBeVisible();
        await d.getByLabel("Servidor IMAP").fill("IMAP.isp.gt");
        // STARTTLS cambia el puerto habitual (993 → 143).
        await d.getByLabel("Cifrado", { exact: true }).selectOption("starttls");
        await expect(d.getByLabel("Puerto", { exact: true })).toHaveValue("143");
        await d.getByLabel("Servidor SMTP").fill("smtp.isp.gt");
        await d.getByLabel("Usuario SMTP").fill("ventas@isp.gt");
        await d.getByLabel("Contraseña SMTP").fill("clave-smtp");
        await d.getByLabel("authserv-id del servidor de correo").fill("mx.isp.gt");
        await expect(d.getByText("Sin authserv-id no se verifica a nadie")).toHaveCount(0);
        await d.getByLabel("Prioridad por defecto").selectOption("alta");
        await d.getByRole("button", { name: "Crear buzón" }).click();
        await expect(d).toHaveCount(0);

        const nuevo = tarjeta(page, "ventas@isp.gt");
        await expect(nuevo).toContainText("Inactivo");
        await expect(nuevo).toContainText("imap.isp.gt:143");
        const alta = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/buzones");
        expect(alta?.idempotencia).toBeTruthy();
        expect(alta?.cuerpo).toMatchObject({
            nombre: "Ventas",
            imap: { host: "imap.isp.gt", puerto: 143, tls: "starttls", usuario: "ventas@isp.gt", contrasena: "clave-imap" },
            smtp: { host: "smtp.isp.gt", puerto: 587, tls: "starttls", usuario: "ventas@isp.gt", contrasena: "clave-smtp" },
            authservId: "mx.isp.gt",
            prioridad: "alta",
        } satisfies Partial<Schema<"EntradaCrearBuzon">>);
        // La contraseña no aparece en ninguna parte de la pantalla.
        await expect(page.getByText("clave-imap")).toHaveCount(0);
    });

    test("edición: conserva la contraseña y exige reescribirla al cambiar el servidor", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/buzones");
        await tarjeta(page, "soporte@isp.gt").getByRole("button", { name: "Editar" }).click();
        const d = page.getByRole("dialog");
        await expect(d.getByLabel("Contraseña IMAP")).toHaveValue("");
        await expect(d.getByText("Déjala vacía para conservar la guardada. Nunca se muestra.").first()).toBeVisible();

        await d.getByLabel("Servidor IMAP").fill("imap2.isp.gt");
        await expect(d.getByText("Cambiaste el servidor, el puerto, el TLS o el usuario: escribe la contraseña IMAP de nuevo.")).toBeVisible();
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(d.getByText("Cambiaste el servidor, el puerto, el TLS o el usuario: escribe la contraseña de nuevo")).toBeVisible();
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/buzones/1/editar")).toBe(false);

        await d.getByLabel("Contraseña IMAP").fill("nueva-clave");
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(d).toHaveCount(0);
        await expect(tarjeta(page, "soporte@isp.gt")).toContainText("imap2.isp.gt:993");
        const edicion = api.peticiones.find((p) => p.ruta === "/api/v1/buzones/1/editar");
        expect(edicion?.idempotencia).toBeTruthy();
        expect(edicion?.cuerpo).toMatchObject({ version: 3, imap: { host: "imap2.isp.gt", contrasena: "nueva-clave" } });
        // SMTP sin cambios: su contraseña se omite (se conserva).
        expect((edicion?.cuerpo as Schema<"EntradaEditarBuzon">).smtp?.contrasena).toBeUndefined();
    });

    test("edición con 409: «Recargar» toma la versión nueva", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/buzones");
        await tarjeta(page, "soporte@isp.gt").getByRole("button", { name: "Editar" }).click();
        const d = page.getByRole("dialog");
        api.cambiarVersionBuzon(1);
        await d.getByLabel("Nombre").fill("Soporte técnico");
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(d.getByText("Alguien más cambió este buzón")).toBeVisible();
        await d.getByRole("button", { name: "Recargar" }).click();
        await expect(d.getByText("Alguien más cambió este buzón")).toHaveCount(0);
        await expect(d.getByLabel("Nombre")).toHaveValue("Soporte técnico");
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(d).toHaveCount(0);
        await expect(tarjeta(page, "soporte@isp.gt")).toContainText("Soporte técnico");
    });

    test("probar conexión: correcta y con error, sin detalles sensibles", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/buzones");
        const soporte = tarjeta(page, "soporte@isp.gt");
        await soporte.getByRole("button", { name: "Probar conexión" }).click();
        await expect(soporte.getByTestId("resultado-prueba")).toContainText("Conexión correcta");
        await expect(soporte.locator('[data-servidor="IMAP (entrada)"]')).toHaveAttribute("data-ok", "true");
        await expect(soporte.locator('[data-servidor="SMTP (salida)"]')).toHaveAttribute("data-ok", "true");

        const facturas = tarjeta(page, "facturas@isp.gt");
        await facturas.getByRole("button", { name: "Probar conexión" }).click();
        await expect(facturas.getByTestId("resultado-prueba")).toContainText("La prueba encontró problemas");
        await expect(facturas.getByTestId("resultado-prueba")).toContainText("El servidor rechazó el usuario o la contraseña.");
        await expect(facturas.getByTestId("resultado-prueba")).toContainText("no se probó la salida");
        const pruebas = api.peticiones.filter((p) => p.ruta.endsWith("/probar"));
        expect(pruebas).toHaveLength(2);
        expect(pruebas.every((p) => p.idempotencia)).toBe(true);
    });

    test("activar y desactivar con version", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/buzones");
        const facturas = tarjeta(page, "facturas@isp.gt");
        await facturas.getByRole("button", { name: "Activar" }).click();
        await expect(facturas).toContainText("Activo");
        await expect(facturas.getByRole("button", { name: "Desactivar" })).toBeVisible();
        const activar = api.peticiones.find((p) => p.ruta === "/api/v1/buzones/2/activar");
        expect(activar?.cuerpo).toEqual({ version: 1 });
        expect(activar?.idempotencia).toBeTruthy();

        // Desactivar es destructivo: pide confirmación; «Cancelar» no hace nada.
        await facturas.getByRole("button", { name: "Desactivar" }).click();
        const confirmar = page.getByRole("alertdialog", { name: "¿Desactivar facturas@isp.gt?" });
        await confirmar.getByRole("button", { name: "Cancelar" }).click();
        await expect(confirmar).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/buzones/2/desactivar")).toBe(false);
        await facturas.getByRole("button", { name: "Desactivar" }).click();
        await confirmar.getByRole("button", { name: "Desactivar" }).click();
        await expect(facturas.getByRole("button", { name: "Activar" })).toBeVisible();
        expect(api.peticiones.find((p) => p.ruta === "/api/v1/buzones/2/desactivar")?.cuerpo).toEqual({ version: 2 });
    });

    test("403 por alcance de zona: mensaje claro", async ({ page, context, baseURL }, info) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", SUPERVISOR_ZONA.correo);
        await page.goto("/administracion/buzones");
        const soporte = tarjeta(page, "soporte@isp.gt");
        await soporte.getByRole("button", { name: "Probar conexión" }).click();
        await expect(soporte.getByText("Tu permiso no alcanza para administrar buzones")).toBeVisible();
        await expect(soporte.getByText(/alcance global/)).toBeVisible();
        await page.screenshot({ path: path.join(carpeta, `22-buzon-403-${info.project.name}.png`), fullPage: true });

        await page.getByRole("button", { name: "Nuevo buzón" }).click();
        const d = page.getByRole("dialog");
        await d.getByLabel("Nombre").fill("Ventas");
        await d.getByLabel("Dirección").fill("ventas@isp.gt");
        await d.getByLabel("Servidor IMAP").fill("imap.isp.gt");
        await d.getByLabel("Usuario IMAP").fill("ventas");
        await d.getByLabel("Contraseña IMAP").fill("x");
        await d.getByLabel("Enviar las respuestas por correo").uncheck();
        await d.getByRole("button", { name: "Crear buzón" }).click();
        await expect(d.getByText("Tu permiso no alcanza para administrar buzones")).toBeVisible();
    });
});

test("capturas de buzones", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `${que}-${info.project.name}.png`);
    await simularApi(page);
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/administracion");
    await expect(page.getByRole("link", { name: /Buzones de correo/ })).toBeVisible();
    await page.screenshot({ path: nombre("17-administracion"), fullPage: true });

    await page.goto("/administracion/buzones");
    await tarjeta(page, "soporte@isp.gt").getByRole("button", { name: "Probar conexión" }).click();
    await tarjeta(page, "facturas@isp.gt").getByRole("button", { name: "Probar conexión" }).click();
    await expect(tarjeta(page, "facturas@isp.gt").getByTestId("resultado-prueba")).toBeVisible();
    await expect(tarjeta(page, "soporte@isp.gt").getByTestId("resultado-prueba")).toBeVisible();
    await page.screenshot({ path: nombre("18-buzones"), fullPage: true });

    await page.getByRole("button", { name: "Nuevo buzón" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Crear buzón" }).click();
    await page.screenshot({ path: nombre("19-buzon-alta") });
    await page.keyboard.press("Escape");

    await tarjeta(page, "soporte@isp.gt").getByRole("button", { name: "Editar" }).click();
    await page.getByRole("dialog").getByLabel("Usuario IMAP").fill("otro@isp.gt");
    await page.getByRole("dialog").getByRole("button", { name: "Guardar cambios" }).click();
    await expect(page.getByRole("dialog").getByText("escribe la contraseña de nuevo", { exact: false }).first()).toBeVisible();
    await page.getByRole("dialog").getByLabel("Contraseña IMAP").scrollIntoViewIfNeeded();
    await page.screenshot({ path: nombre("20-buzon-edicion") });
});
