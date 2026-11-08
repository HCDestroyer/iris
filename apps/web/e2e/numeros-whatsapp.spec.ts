import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi, type ApiFalsa, type OpcionesApi } from "./api-falsa";
import type { ResultadoMeta } from "./api-numeros";
import { esTelefono } from "./ayudas";
import { buscarDesbordes } from "./desbordes";
import { SECCIONES_GUIA } from "../lib/numeros/guia";

/*
 * Números de WhatsApp (contratos/conversaciones.md §1.1): lista con la conexión de cada número,
 * «Agregar número» en tres pasos con «Guardar y probar», cada resultado de la prueba, credenciales
 * que se quedan si el campo va vacío, desactivar con confirmación y la guía «Cómo conectar
 * WhatsApp». En escritorio y teléfono, claro y oscuro, sin desbordes.
 */

const carpeta = process.env.IRIS_CAPTURAS_NUMEROS ?? path.join("test-results", "numeros-wa");

async function abrir(page: Page, ruta: string, opciones: OpcionesApi = {}): Promise<ApiFalsa> {
    const api = await simularApi(page, opciones);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

/** Sin desbordes (se reintenta mientras entra o sale un aviso emergente, que se anima). */
async function esperarSinDesbordes(page: Page) {
    await expect.poll(() => buscarDesbordes(page), { message: "nada se desborda ni queda fuera de pantalla", timeout: 8_000 }).toEqual([]);
}

async function captura(page: Page, nombre: string) {
    await esperarSinDesbordes(page);
    await page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: true });
}

/** Llena los datos del número (paso 2) con valores válidos. */
async function llenarNumero(page: Page) {
    const form = page.getByRole("form", { name: "Agregar número" });
    await form.getByRole("textbox", { name: "Nombre" }).fill("Soporte técnico");
    await form.getByRole("textbox", { name: "Número" }).fill("+502 2200 0040");
    await form.getByRole("textbox", { name: "Phone number ID" }).fill("109876543210987");
    await form.getByRole("textbox", { name: /WABA ID/ }).fill("102345678901234");
    await form.getByRole("textbox", { name: "Token de acceso" }).fill("token-de-prueba-0123456789abcdef");
    await form.getByRole("textbox", { name: "App secret" }).fill("app-secret-de-prueba-0123");
    return form;
}

async function agregarYProbar(page: Page, resultado?: ResultadoMeta) {
    const api = await abrir(page, "/administracion/numeros/nuevo", resultado ? { numeros: { resultadoNuevo: resultado } } : {});
    const form = await llenarNumero(page);
    await form.getByRole("button", { name: "Guardar y probar" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Conectar Soporte técnico" })).toBeVisible();
    return api;
}

test.describe("números de WhatsApp", () => {
    test("lista: conexión de cada número con icono y texto, credenciales y activar", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros", { numeros: { conInactivo: true } });
        await expect(page.getByRole("heading", { name: "Números de WhatsApp" })).toBeVisible();
        await expect(page.getByText("4 activos y 1 desactivado, por la API oficial de Meta (WhatsApp Cloud API)")).toBeVisible();
        const fila = (id: number) => page.locator(`[data-numero="${id}"]`);
        await expect(fila(2).locator("[data-conexion]")).toHaveText("Conectado");
        await expect(fila(4).locator("[data-conexion]")).toHaveText("Token vencido");
        await expect(fila(5).locator("[data-conexion]")).toHaveText("Webhook sin verificar");
        await expect(fila(6).locator("[data-conexion]")).toHaveText("Desactivado");
        // «Conectado» es solo un punto; lo que pide actuar lleva icono.
        await expect(fila(2).locator("[data-conexion] svg")).toHaveCount(0);
        await expect(fila(4).locator("[data-conexion] svg")).toHaveCount(1);
        await expect(fila(4).getByRole("link", { name: "Credenciales de Cobros" })).toHaveClass(/text-error/);
        await expect(fila(2).getByRole("link", { name: "Configurar Avisos" })).toBeVisible();
        await expect(fila(6).getByRole("link", { name: /Configurar/ })).toHaveCount(0);
        await expect(page.getByRole("link", { name: "Agregar número" })).toBeVisible();
        await captura(page, "lista");

        await fila(6).getByRole("button", { name: "Activar Ventas (anterior)" }).click();
        await expect(page.getByText("Se activó «Ventas (anterior)»")).toBeVisible();
        await expect(fila(6).locator("[data-conexion]")).toHaveText("Conectado");
        const activar = api.peticiones.find((p) => p.ruta.endsWith("/canales/6/activar"));
        expect(activar?.idempotencia).toBeTruthy();
    });

    test("lista vacía: ver la guía o agregar el primer número", async ({ page }) => {
        await abrir(page, "/administracion/numeros", { numeros: { vacio: true } });
        await expect(page.getByText("Todavía no hay números de WhatsApp")).toBeVisible();
        await expect(page.getByText("Conecta un número para recibir y contestar WhatsApp en la bandeja")).toBeVisible();
        await expect(page.getByRole("link", { name: "Ver la guía" })).toHaveAttribute("href", "/administracion/numeros/guia");
        await expect(page.getByRole("link", { name: "Agregar número" })).toHaveCount(1);
        await captura(page, "vacia");
    });

    test("agregar número: errores del validador espejo y pregunta antes de salir con cambios", async ({ page }, info) => {
        await abrir(page, "/administracion/numeros/nuevo");
        const form = page.getByRole("form", { name: "Agregar número" });
        await expect(form.getByRole("heading", { name: /Paso 1: En Meta/ })).toBeVisible();
        await expect(form.getByText("Próximamente: conectar con QR un número que ya usas en WhatsApp Business (coexistencia oficial de Meta).")).toBeVisible();
        // El verify token lo genera el servidor: antes de guardar no hay ninguno.
        await expect(form.getByText("Iris lo genera al guardar")).toBeVisible();
        await form.getByRole("button", { name: "Guardar y probar" }).click();
        await expect(form.getByText("Escribe un nombre")).toBeVisible();
        await expect(form.getByText("Escribe el número")).toBeVisible();
        await expect(form.getByText("Pega el Phone number ID")).toBeVisible();
        await expect(form.getByText("Pega el token de acceso")).toBeVisible();
        await expect(form.getByText("Pega el app secret")).toBeVisible();

        await form.getByRole("textbox", { name: "Número" }).fill("2200 0040");
        await form.getByRole("textbox", { name: "Phone number ID" }).fill("10987-65");
        await form.getByRole("textbox", { name: "Token de acceso" }).fill("EAAG-corto");
        await form.getByRole("textbox", { name: "App secret" }).fill("corto");
        await form.getByRole("checkbox", { name: "Soporte" }).uncheck();
        await form.getByRole("button", { name: "Guardar y probar" }).click();
        await expect(form.getByText(/E\.164/).last()).toBeVisible();
        await expect(form.getByText("Solo números (hasta 30 dígitos)")).toBeVisible();
        await expect(form.getByText("El token tiene entre 20 y 1024 caracteres")).toBeVisible();
        await expect(form.getByText("El app secret tiene entre 16 y 256 caracteres")).toBeVisible();
        await expect(form.getByText("Elige al menos un uso")).toBeVisible();
        // Los secretos no se ven, no se autocompletan y se pueden mostrar.
        const token = form.getByRole("textbox", { name: "Token de acceso" });
        await expect(token).toHaveAttribute("type", "password");
        await expect(token).toHaveAttribute("autocomplete", "off");
        await form.getByRole("button", { name: "Mostrar token de acceso" }).click();
        await expect(token).toHaveAttribute("type", "text");
        expect(page.url()).not.toContain("EAAG");
        await captura(page, "agregar-errores");

        // Salir con cambios pregunta antes.
        if (!esTelefono(page) || info.project.name.startsWith("escritorio")) {
            await form.getByRole("button", { name: "Cancelar" }).click();
            await expect(page.getByRole("alertdialog", { name: "¿Descartar los cambios?" })).toBeVisible();
            await page.getByRole("button", { name: "Seguir editando" }).click();
            await expect(form).toBeVisible();
        }
    });

    test("agregar número: «¿Dónde lo encuentro?» y la guía en otra pestaña", async ({ page }) => {
        await abrir(page, "/administracion/numeros/nuevo");
        const punto = page.locator('[data-punto="token"]');
        await punto.getByRole("button", { name: "¿Dónde lo encuentro?" }).click();
        await expect(punto.getByRole("button", { name: "¿Dónde lo encuentro?" })).toHaveAttribute("aria-expanded", "true");
        await expect(punto.getByText("Usuarios del sistema", { exact: true })).toBeVisible();
        const enlace = punto.getByRole("link", { name: /Ver el paso en la guía/ });
        await expect(enlace).toHaveAttribute("href", "/administracion/numeros/guia#token");
        await expect(enlace).toHaveAttribute("target", "_blank");
        await page.locator('[data-punto="cuenta"]').getByRole("checkbox", { name: "Cuenta de Meta Business con WhatsApp" }).check();
        await captura(page, "agregar");
    });

    test("agregar número: guardar y probar, webhook sin verificar y luego conectado", async ({ page, context }) => {
        test.slow();
        await context.grantPermissions(["clipboard-read", "clipboard-write"]);
        const api = await agregarYProbar(page);
        await expect(page.getByText("Número «Soporte técnico» guardado")).toBeVisible();
        const alta = api.numeros.cuerpos.find((c) => c.ruta === "/api/v1/mensajeria/canales");
        expect(alta?.cuerpo).toMatchObject({ nombre: "Soporte técnico", numero: "+50222000040", phoneNumberId: "109876543210987", usos: ["soporte"], colaId: 2 });
        expect(alta?.cuerpo).not.toHaveProperty("verifyToken");
        expect(alta?.idempotencia).toBeTruthy();
        const creado = api.numeros.ultimoCreado();
        expect(creado?.verifyTokenGenerado).toMatch(/^vt_/);
        // El token de verificación se ve en la pantalla, nunca en la URL.
        await expect(page).toHaveURL(new RegExp(`/administracion/numeros/${creado?.id}/conectar$`));
        expect(page.url()).not.toContain("vt_");
        const prueba = page.locator("[data-prueba]");
        await expect(prueba.getByRole("heading", { name: "Webhook sin verificar todavía" })).toBeVisible();
        await expect(prueba).toContainText("«Red Sur Soporte», calidad alta.");
        await expect(page.getByText(`https://iris.redsur.com.gt/api/v1/mensajeria/canales/${creado?.id}/webhook`)).toBeVisible();
        await expect(page.getByText(creado?.verifyTokenGenerado ?? "nunca")).toBeVisible();
        await page.getByRole("button", { name: "Copiar token de verificación" }).click();
        await expect(page.getByText("Token de verificación copiado")).toBeVisible();
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(creado?.verifyTokenGenerado);
        if (!esTelefono(page)) {
            await expect(page.getByRole("navigation", { name: /migas|Migas|Ruta/i }).getByText("Conectar")).toBeVisible();
        }
        await captura(page, "agregar-probado");

        // Meta verificó el webhook: «Volver a comprobar» dice «Conectado».
        api.numeros.resultados.set(creado?.id ?? 0, { estado: "conectado", webhookVerificado: true });
        await prueba.getByRole("button", { name: "Volver a comprobar" }).click();
        await expect(prueba.getByRole("heading", { name: "Conectado" })).toBeVisible();
        await expect(prueba).toContainText(/Meta lo verificó hoy a las \d\d:\d\d\./);
        await expect(page.getByText("Listo: el número ya recibe mensajes.")).toBeVisible();
        await expect(page.getByRole("link", { name: "Configurar uso y horario" })).toBeVisible();
        await captura(page, "agregar-probado-ok");

        // Recargar: el token de verificación ya no se muestra (está «Generar otro»).
        await page.reload();
        await expect(page.getByRole("group", { name: "Token de verificación" })).toContainText("•••• guardado");
        await expect(page.getByText(creado?.verifyTokenGenerado ?? "nunca")).toHaveCount(0);
    });

    test("agregar número: token inválido, se cambia y se vuelve a probar", async ({ page }) => {
        const api = await agregarYProbar(page, { estado: "token_invalido", webhookVerificado: false });
        const prueba = page.locator("[data-prueba]");
        await expect(prueba.getByRole("heading", { name: "Token inválido o vencido" })).toBeVisible();
        await expect(prueba).toContainText("Sin probar: primero las credenciales.");
        await expect(page.getByText("Meta lo rechazó: venció o no tiene permiso sobre este número.")).toBeVisible();
        await captura(page, "agregar-probado-token");
        await prueba.getByRole("button", { name: "Cambiar token" }).click();
        const token = page.getByRole("form", { name: "Cambiar token" }).getByRole("textbox", { name: "Token de acceso" });
        await expect(token).toBeFocused();
        await page.getByRole("button", { name: "Guardar y probar" }).click();
        await expect(page.getByText("Pega el token nuevo")).toBeVisible();
        await token.fill("token-nuevo-de-prueba-0123456789");
        await page.getByRole("button", { name: "Guardar y probar" }).click();
        await expect(page.getByText("Token guardado")).toBeVisible();
        await expect(prueba.getByRole("heading", { name: "Webhook sin verificar todavía" })).toBeVisible();
        const cambio = api.numeros.cuerpos.find((c) => c.ruta.endsWith("/actualizar"));
        expect(cambio?.cuerpo).toMatchObject({ token: "token-nuevo-de-prueba-0123456789" });
        expect(cambio?.cuerpo).not.toHaveProperty("appSecret");
    });

    for (const [estado, titulo, accion] of [
        ["numero_no_coincide", "El número no coincide", "Corregir datos"],
        ["sin_permiso", "Al token le faltan permisos", "Cambiar token"],
        ["error_meta", "Meta no respondió", "Volver a comprobar"],
    ] as const) {
        test(`agregar número: resultado «${titulo}»`, async ({ page }) => {
            await agregarYProbar(page, { estado, webhookVerificado: false });
            const prueba = page.locator("[data-prueba]");
            await expect(prueba).toHaveAttribute("data-prueba", estado);
            await expect(prueba.getByRole("heading", { name: titulo })).toBeVisible();
            await expect(prueba.getByRole(accion === "Corregir datos" ? "link" : "button", { name: accion })).toBeVisible();
            await esperarSinDesbordes(page);
        });
    }

    test("credenciales: vacío se queda el guardado; probar conexión y generar otro token", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros/2/credenciales");
        const form = page.getByRole("form", { name: "Credenciales de Avisos" });
        await expect(form).toBeVisible();
        await expect(form.getByRole("group", { name: "Token de acceso" })).toContainText("•••• guardado el 3 nov");
        await expect(form.getByRole("group", { name: "App secret" })).toContainText("•••• guardado el 3 nov");
        await expect(form.getByText(/Verificado por Meta el 3 nov/)).toBeVisible();
        await expect(form.getByText("Último mensaje recibido hace 5 minutos.")).toBeVisible();
        await captura(page, "credenciales");

        // «Cambiar» abre el campo; «No cambiar» lo cierra y lo vacía.
        await form.getByRole("button", { name: "Cambiar: token de acceso" }).click();
        await form.getByRole("textbox", { name: "Token de acceso" }).fill("token-que-no-se-guarda-0000");
        await form.getByRole("button", { name: "No cambiar token de acceso" }).click();
        await expect(form.getByRole("group", { name: "Token de acceso" })).toBeVisible();
        await form.getByRole("button", { name: "Cambiar: app secret" }).click();
        await expect(form.getByText(/Vacío: se queda el guardado el 3 nov\.? 2026\./)).toBeVisible();
        await form.getByRole("textbox", { name: "Nombre" }).fill("Avisos y soporte");
        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Credenciales de «Avisos y soporte» guardadas")).toBeVisible();
        const cuerpo = api.numeros.cuerpos.find((c) => c.ruta.endsWith("/2/actualizar"))?.cuerpo;
        expect(cuerpo).toMatchObject({ nombre: "Avisos y soporte", version: 1 });
        expect(cuerpo).not.toHaveProperty("token");
        expect(cuerpo).not.toHaveProperty("appSecret");
        expect(api.numeros.secretos.get(2)).toMatchObject({ token: "EAAG-guardado-2-xxxxxxxxxxxx", appSecret: "secreto-guardado-2" });

        await form.getByRole("button", { name: "Probar conexión" }).click();
        await expect(page.locator("[data-prueba]").getByRole("heading", { name: "Conectado" })).toBeVisible();

        await form.getByRole("button", { name: "Generar otro: token de verificación" }).click();
        await page.getByRole("alertdialog", { name: "¿Generar otro token de verificación?" }).getByRole("button", { name: "Generar otro" }).click();
        await expect(page.getByText("Token de verificación nuevo: pégalo en Meta")).toBeVisible();
        const nuevo = api.numeros.secretos.get(2)?.verifyToken ?? "";
        expect(nuevo).toMatch(/^vt_/);
        await expect(form.getByText(nuevo)).toBeVisible();
        await expect(form.getByText("Meta aún no verificó el webhook")).toBeVisible();
        const generar = api.numeros.cuerpos.filter((c) => c.ruta.endsWith("/2/actualizar")).at(-1)?.cuerpo;
        expect(generar).toMatchObject({ generarVerifyToken: true, version: 2 });
    });

    test("desactivar número: confirma y avisa si es el número de avisos", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros/2/credenciales");
        const form = page.getByRole("form", { name: "Credenciales de Avisos" });
        await form.getByRole("button", { name: "Desactivar número" }).click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Desactivar Avisos?" });
        await expect(dialogo).toContainText("Iris deja de enviar y recibir por +502 2200 0005.");
        await expect(dialogo).toContainText("Es el número de avisos de la empresa");
        await captura(page, "credenciales-desactivar");
        await dialogo.getByRole("button", { name: "Cancelar" }).click();
        await expect(dialogo).toBeHidden();
        expect(api.peticiones.some((p) => p.ruta.endsWith("/desactivar"))).toBe(false);

        await form.getByRole("button", { name: "Desactivar número" }).click();
        await dialogo.getByRole("button", { name: "Desactivar" }).click();
        await expect(page.getByText("Se desactivó «Avisos»")).toBeVisible();
        await expect(form.getByRole("button", { name: "Activar número" })).toBeVisible();
        await expect(page.locator("main [data-conexion]").first()).toHaveText("Desactivado");
    });

    test("guía «Cómo conectar WhatsApp»: pasos, anclajes y migas", async ({ page }) => {
        await abrir(page, "/administracion/numeros/guia#token");
        await expect(page.getByRole("heading", { level: 1, name: "Cómo conectar WhatsApp" })).toBeVisible();
        const token = page.locator("section#token");
        await expect(token).toBeInViewport();
        await expect(token).toContainText("whatsapp_business_messaging");
        await expect(token).toContainText("Nunca");
        await expect(page.locator("section#varios-numeros")).toContainText("override_callback_uri");
        await expect(page.locator("section#qr")).toContainText("servicio de Horus");
        await expect(page.locator("section#estados")).toContainText("Webhook sin verificar");
        await expect(page.getByRole("navigation", { name: "Contenido de la guía" }).getByRole("link")).toHaveCount(SECCIONES_GUIA.length);
        if (!esTelefono(page)) {
            await expect(page.getByRole("link", { name: "Números de WhatsApp" }).first()).toHaveAttribute("href", "/administracion/numeros");
        }
        // Enlaces a Meta en otra pestaña y sin HTML crudo.
        await expect(page.getByRole("link", { name: "developers.facebook.com/apps" })).toHaveAttribute("target", "_blank");
        await page.evaluate(() => window.scrollTo(0, 0));
        await captura(page, "guia");
    });
});
