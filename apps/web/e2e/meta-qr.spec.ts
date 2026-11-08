import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, CONSULTA, CREDENCIALES_WA, conSesion, simularApi, type ApiFalsa, type OpcionesApi } from "./api-falsa";
import type { ResultadoQr } from "./api-meta";
import { esTelefono } from "./ayudas";
import { elegirOpcion } from "./controles";
import { AHORA_CONV, mensaje } from "./datos-conversaciones";
import { buscarDesbordes } from "./desbordes";

/*
 * Conexión con Meta, WhatsApp por QR y préstamo de números (contratos/conversaciones.md §15).
 * El SDK de Facebook se simula (nunca hay red real): `connect.facebook.net` responde un `FB` falso
 * cuyo `login` guarda el callback; la prueba manda los `message` (con su `origin`) y el código.
 */

const carpeta = process.env.IRIS_CAPTURAS_META ?? path.join("test-results", "meta-qr");

const FB_FALSO = `(function () {
    window.__fb = { inicios: [], logins: [], cb: null };
    window.FB = {
        init: function (o) { window.__fb.inicios.push(o); },
        login: function (cb, o) { window.__fb.logins.push(o); window.__fb.cb = cb; },
    };
    window.__fbMensaje = function (origin, data) { window.dispatchEvent(new MessageEvent("message", { origin: origin, data: data })); };
    if (window.fbAsyncInit) window.fbAsyncInit();
})();`;

const FIN = { type: "WA_EMBEDDED_SIGNUP", event: "FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING", data: { waba_id: "980198427658004", phone_number_id: "106540352242922" } };

type Contexto = { api: ApiFalsa; sdk: string[] };

async function abrir(page: Page, ruta: string, opciones: OpcionesApi = {}, correo = ADMIN.correo): Promise<Contexto> {
    const sdk: string[] = [];
    await page.route("https://connect.facebook.net/**", (route) => {
        sdk.push(route.request().url());
        return route.fulfill({ status: 200, contentType: "text/javascript", body: FB_FALSO });
    });
    const api = await simularApi(page, opciones);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return { api, sdk };
}

async function esperarSinDesbordes(page: Page) {
    await expect.poll(() => buscarDesbordes(page), { message: "nada se desborda ni queda fuera de pantalla", timeout: 8_000 }).toEqual([]);
}

async function captura(page: Page, nombre: string) {
    await esperarSinDesbordes(page);
    await page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: true });
}

/** Abre «Conectar con QR», marca «Leí…» y abre la ventana de Meta (falsa). */
async function iniciarQr(page: Page, resultado?: ResultadoQr) {
    const ctx = await abrir(page, "/administracion/numeros/nuevo/qr", { meta: resultado ? { resultadoQr: resultado } : {} });
    await expect(page.getByRole("heading", { level: 1, name: "Conectar con QR" })).toBeVisible();
    const conectar = page.getByRole("button", { name: "Conectar con Meta" });
    await expect(conectar).toBeDisabled();
    await page.getByRole("checkbox", { name: "Leí lo que cambia en la app del teléfono." }).check();
    await expect(conectar).toBeEnabled();
    await conectar.click();
    await expect(page.getByRole("heading", { name: "Esperando a Meta…" })).toBeVisible();
    return ctx;
}

const mandar = (page: Page, origen: string, datos: unknown) => page.evaluate(([o, d]) => (window as unknown as { __fbMensaje: (o: string, d: unknown) => void }).__fbMensaje(o as string, d), [origen, datos] as const);
const responderLogin = (page: Page, respuesta: unknown) => page.evaluate((r) => (window as unknown as { __fb: { cb: (r: unknown) => void } }).__fb.cb(r), respuesta);

test.describe("conexión con Meta", () => {
    test("app propia: datos, prueba con sus comprobaciones y webhook de la app", async ({ page }) => {
        const { api } = await abrir(page, "/administracion/conexion-meta");
        const form = page.getByRole("form", { name: "Conexión con Meta" });
        await expect(form.getByRole("radio", { name: /App propia/ })).toBeChecked();
        await expect(form.getByRole("group", { name: "App secret" })).toContainText("•••• guardado el 3 nov");
        const prueba = page.getByRole("region", { name: /Probar conexión/ });
        await expect(prueba.getByRole("heading", { name: "Lista, falta confirmar la aprobación de Meta" })).toBeVisible();
        await expect(prueba.locator('[data-linea="app"]')).toHaveAttribute("data-tono", "success");
        await expect(prueba.locator('[data-linea="tech_provider"]')).toContainText("Tech Provider con acceso avanzado");
        await expect(form.getByText("https://iris.redsur.com.gt/api/v1/mensajeria/meta/webhook")).toBeVisible();
        await expect(form.getByRole("list", { name: "Campos del webhook" })).toContainText("smb_message_echoes");
        await captura(page, "conexion-propia");

        await form.getByRole("textbox", { name: /Versión de la Graph API/ }).fill("21");
        await form.getByRole("button", { name: "Guardar y probar" }).click();
        await expect(form.getByText("Como v23.0, o vacío para usar la de Iris")).toBeVisible();
        await form.getByRole("textbox", { name: /Versión de la Graph API/ }).fill("v23.0");
        await form.getByRole("button", { name: "Guardar y probar" }).click();
        await expect(page.getByText("Conexión con Meta guardada")).toBeVisible();
        const guardado = api.meta.cuerpos.find((c) => c.ruta.endsWith("/conexion-meta/actualizar"))?.cuerpo;
        expect(guardado).toMatchObject({ version: 3, modo: "propia", appId: "1298475561023344", configId: "874120336615092", graphVersion: "v23.0" });
        expect(guardado).not.toHaveProperty("appSecret");
        await expect.poll(() => api.meta.cuerpos.some((c) => c.ruta.endsWith("/conexion-meta/probar"))).toBe(true);
    });

    test("app compartida: Iris Cloud todavía no está disponible", async ({ page }) => {
        const { api } = await abrir(page, "/administracion/conexion-meta", { meta: { conexion: "sin_configurar" } });
        const form = page.getByRole("form", { name: "Conexión con Meta" });
        await form.getByRole("radio", { name: /App compartida/ }).click();
        await expect(form.getByRole("textbox", { name: "Servicio central" })).toHaveAttribute("placeholder", "https://cloud.iris.kns.gt");
        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(form.getByText("Pega la clave de enlace")).toBeVisible();
        await form.getByRole("textbox", { name: "Clave de enlace" }).fill("clave-de-enlace-de-prueba");
        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Conexión con Meta guardada")).toBeVisible();
        expect(api.meta.cuerpos.find((c) => c.ruta.endsWith("/actualizar"))?.cuerpo).toMatchObject({ version: 0, modo: "compartida", servicioUrl: null, claveEnlace: "clave-de-enlace-de-prueba" });
        const estado = page.getByRole("region", { name: "Estado del enlace" });
        await expect(estado.getByRole("heading", { name: "Servicio central no disponible todavía" })).toBeVisible();
        await expect(estado).toContainText("https://cloud.iris.kns.gt");
        // La clave nunca vuelve a la pantalla ni a la URL.
        await expect(form.getByRole("group", { name: "Clave de enlace" })).toContainText("•••• guardado");
        expect(page.url()).not.toContain("clave");
        await captura(page, "conexion-compartida");
    });

    test("en la nube: solo lectura", async ({ page }) => {
        await abrir(page, "/administracion/conexion-meta", { meta: { conexion: "saas" } });
        await expect(page.getByText("Administrado por Iris Cloud")).toBeVisible();
        await expect(page.getByRole("form")).toHaveCount(0);
        await expect(page.getByRole("region", { name: "Estado" }).getByRole("heading", { name: "Funcionando" })).toBeVisible();
        await captura(page, "conexion-saas");
    });

    test("sin `mensajeria.conexion_meta` no se ve ni se entra", async ({ page }) => {
        await abrir(page, "/administracion/numeros", {}, CREDENCIALES_WA.correo);
        await expect(page.getByRole("heading", { name: "Números de WhatsApp" })).toBeVisible();
        await expect(page.getByRole("link", { name: "Conexión con Meta" })).toHaveCount(0);
        await page.goto("/administracion/conexion-meta");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        // Sin conexión configurada, quien no es admin no ve el enlace para configurarla.
        await abrir(page, "/administracion/numeros/nuevo", { meta: { conexion: "sin_configurar" } }, CREDENCIALES_WA.correo);
        await expect(page.getByText("Falta configurar la conexión con Meta.")).toBeVisible();
        await expect(page.getByRole("link", { name: "Configurar la conexión con Meta" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Conectar con QR" })).toBeDisabled();
    });
});

test.describe("agregar número con QR", () => {
    test("elegir: QR si la conexión lo permite, si no, solo la API", async ({ page }) => {
        const { sdk } = await abrir(page, "/administracion/numeros/nuevo");
        await expect(page.locator('[data-qr="disponible"]')).toContainText("Conexión con Meta lista (app propia)");
        await expect(page.getByRole("link", { name: "Ingresar datos de la API" })).toHaveAttribute("href", "/administracion/numeros/nuevo/api");
        await captura(page, "agregar-elegir");
        // El SDK de Facebook no se carga fuera de «Conectar con QR».
        expect(sdk).toEqual([]);
        await page.getByRole("link", { name: "Conectar con QR" }).click();
        await expect(page.getByRole("heading", { level: 1, name: "Conectar con QR" })).toBeVisible();
        await expect.poll(() => sdk).toEqual(["https://connect.facebook.net/es_LA/sdk.js"]);

        await abrir(page, "/administracion/numeros/nuevo", { meta: { conexion: "compartida" } });
        await expect(page.locator('[data-qr="no_disponible"]')).toContainText("Servicio central no disponible todavía");
        await expect(page.getByRole("button", { name: "Conectar con QR" })).toBeDisabled();
    });

    test("QR de punta a punta: valida el origen, manda el código al instante y trae el historial", async ({ page }) => {
        test.slow();
        const { api } = await iniciarQr(page);
        await captura(page, "qr-inicio-esperando");
        const login = await page.evaluate(() => (window as unknown as { __fb: { inicios: unknown[]; logins: unknown[] } }).__fb);
        expect(login.inicios[0]).toMatchObject({ appId: "1298475561023344", version: "v23.0" });
        expect(login.logins[0]).toEqual({ config_id: "874120336615092", response_type: "code", override_default_response_type: true, extras: { setup: {}, featureType: "whatsapp_business_app_onboarding", sessionInfoVersion: "3" } });

        // Un mensaje igual pero de otro origen no cuenta.
        await mandar(page, "https://evil.example", FIN);
        await mandar(page, "https://www.facebook.com.evil.example", FIN);
        await responderLogin(page, { authResponse: { code: "AQB-codigo-de-prueba" }, status: "connected" });
        await page.waitForTimeout(300);
        expect(api.meta.cuerpos.some((c) => c.ruta.endsWith("/canales/qr"))).toBe(false);
        await expect(page.getByRole("heading", { name: "Esperando a Meta…" })).toBeVisible();

        await mandar(page, "https://www.facebook.com", JSON.stringify(FIN));
        await expect(page).toHaveURL(/\/administracion\/numeros\/40\/conectado$/);
        await expect(page.getByText("Número conectado").first()).toBeVisible();
        const alta = api.meta.cuerpos.find((c) => c.ruta.endsWith("/canales/qr"));
        expect(alta?.cuerpo).toMatchObject({ code: "AQB-codigo-de-prueba", wabaId: "980198427658004", phoneNumberId: "106540352242922", nombre: "WhatsApp Business" });
        expect(alta?.idempotencia).toBeTruthy();
        expect(page.url()).not.toContain("AQB");

        await expect(page.locator('[data-estado="conectado"]')).toContainText("Conectado · QR · WhatsApp Business");
        await expect(page.getByText(/Plazo:/)).toBeVisible();
        await page.getByRole("button", { name: "Traer historial" }).click();
        await expect(page.getByText("Historial pedido: acepta el aviso de Meta en el teléfono")).toBeVisible();
        expect(api.meta.cuerpos.find((c) => c.ruta.endsWith("/sincronizar"))?.cuerpo).toEqual({ contactos: true, mensajes: true });
        await expect(page.locator('[data-sincronizacion="contactos"]')).toContainText("312 contactos");
        await expect(page.locator('[data-sincronizacion="mensajes"]')).toContainText("Recibiendo · 62 %");
        await expect(page.getByRole("progressbar", { name: "Progreso de mensajes" })).toHaveAttribute("aria-valuenow", "62");
        await captura(page, "qr-listo");

        const form = page.getByRole("form", { name: "Número conectado" });
        await form.getByRole("textbox", { name: "Nombre" }).fill("Ventas tienda");
        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Número «Ventas tienda» guardado")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/numeros$/);
    });

    for (const [codigo, titulo] of [
        ["codigo_vencido", "El permiso de Meta venció"],
        ["app_no_aprobada", "La app de Meta no tiene permiso para el QR"],
        ["numero_no_elegible", "Meta dice que este número no se puede conectar con QR"],
        ["error_meta", "Meta no respondió bien"],
        ["base_url_requerida", "Falta la dirección pública de Iris"],
    ] as const) {
        test(`QR con error del servidor: ${codigo}`, async ({ page }) => {
            await iniciarQr(page, codigo);
            await mandar(page, "https://www.facebook.com", FIN);
            await responderLogin(page, { authResponse: { code: `AQB-${codigo}` } });
            const error = page.locator(`[data-error-qr="${codigo}"]`);
            await expect(error.getByRole("heading", { name: titulo })).toBeVisible();
            await expect(page.getByText("No se guardó ningún dato del número.")).toBeVisible();
            if (codigo === "numero_no_elegible" || codigo === "app_no_aprobada") await expect(error.getByRole("link", { name: "Usar datos de la API" })).toBeVisible();
            if (codigo === "error_meta") await expect(error).toContainText("fbtrace");
            if (codigo === "base_url_requerida") await expect(error).toContainText("Falta configurar la dirección pública de Iris (IRIS_BASE_URL); pide ayuda a quien administra el servidor.");
            await captura(page, `qr-error-${codigo}`);
            await error.getByRole("button", { name: "Reintentar" }).click();
            await expect(page.getByRole("button", { name: "Conectar con Meta" })).toBeVisible();
        });
    }

    test("QR cancelado o con error en la ventana de Meta", async ({ page }) => {
        const { api } = await iniciarQr(page);
        await mandar(page, "https://www.facebook.com", { type: "WA_EMBEDDED_SIGNUP", event: "CANCEL", data: { current_step: "PHONE_NUMBER_SETUP" } });
        await responderLogin(page, { authResponse: null, status: "unknown" });
        await expect(page.locator('[data-error-qr="cancelado"]').getByRole("heading", { name: "Cerraste la ventana de Meta" })).toBeVisible();
        await expect(page.getByText("Paso: PHONE_NUMBER_SETUP")).toBeVisible();
        await captura(page, "qr-error-cancelado");
        await page.getByRole("button", { name: "Reintentar" }).click();
        await page.getByRole("checkbox", { name: "Leí lo que cambia en la app del teléfono." }).check();
        await page.getByRole("button", { name: "Conectar con Meta" }).click();
        await mandar(page, "https://web.facebook.com", { type: "WA_EMBEDDED_SIGNUP", event: "ERROR", data: { error_message: "Algo falló en Meta", error_code: "524126" } });
        await responderLogin(page, { authResponse: null });
        await expect(page.locator('[data-error-qr="meta"]')).toContainText("Algo falló en Meta · Código 524126");
        expect(api.meta.cuerpos.some((c) => c.ruta.endsWith("/canales/qr"))).toBe(false);
    });
});

test.describe("compartir números entre empresas", () => {
    test("lista: insignias de QR, API, compartido y teléfono sin actividad", async ({ page }) => {
        await abrir(page, "/administracion/numeros", { meta: { qr: true, prestamos: true } });
        const fila = (id: number) => page.locator(`[data-numero="${id}"]`);
        await expect(fila(2).locator("[data-tipo]")).toHaveText("API");
        await expect(fila(2).locator('[data-insignia="compartido"]')).toContainText("Compartido con 1 empresa");
        await expect(fila(4).locator('[data-insignia="compartir"]')).toContainText("Compartir");
        await expect(fila(10).locator("[data-tipo]")).toHaveText("QR · WhatsApp Business");
        await expect(fila(10).getByRole("link", { name: /Credenciales/ })).toHaveCount(0);
        await expect(fila(11).locator("[data-aviso-telefono]")).toHaveText("Teléfono sin actividad 11 días");
        await expect(fila(12).locator("[data-conexion]")).toHaveText("Desconectado por Meta");
        await expect(fila(12)).toContainText("El teléfono pasó 14 días sin abrir la app.");
        await expect(fila(12).getByRole("link", { name: "Reconectar Norte cobros" })).toHaveAttribute("href", "/administracion/numeros/nuevo/qr");
        await expect(page.getByText(/3 compartidos con otras empresas/)).toBeVisible();
        await captura(page, "lista");
    });

    test("prestado por otra empresa: una sola fila, su aviso y el número de avisos", async ({ page }) => {
        const { api } = await abrir(page, "/administracion/numeros", { empresas: [{ id: 2, nombre: "Cable Norte" }], meta: { prestado: true } });
        await expect(page.locator('[data-numero="9"]')).toHaveCount(1);
        await expect(page.locator('[data-numero="9"] [data-insignia="prestado"]')).toHaveText("Prestado por Red Sur");
        await expect(page.locator('[data-numero="9"] [data-insignia="compartido"], [data-numero="9"] [data-insignia="compartir"]')).toHaveCount(0);
        await expect(page.locator('[data-aviso-prestado="9"]')).toContainText("Avisos de Red Sur es de Red Sur");
        await expect(page.locator('[data-aviso-prestado="9"]')).toContainText("lo puede usar para avisos y soporte");
        await expect(page.locator('[data-numero="9"]').getByRole("link", { name: /Configurar|Credenciales/ })).toHaveCount(0);
        await captura(page, "lista-prestado");
        await page.getByRole("link", { name: "Ver detalle de Avisos de Red Sur" }).click();
        await expect(page.getByRole("heading", { level: 1, name: "Avisos de Red Sur" })).toBeVisible();
        const avisos = page.getByRole("switch", { name: "Número de avisos de la empresa" });
        await avisos.click();
        await expect(avisos).toBeChecked();
        await expect(page.getByText("Los avisos de Cable Norte salen por «Avisos de Red Sur»")).toBeVisible();
        expect(api.meta.cuerpos.find((c) => c.ruta.endsWith("/prestamo/configurar"))?.cuerpo).toEqual({ version: 1, avisosEmpresa: true });
    });

    test("número prestado: credenciales, conectado y compartir llevan a su detalle", async ({ page }) => {
        await abrir(page, "/administracion/numeros", { empresas: [{ id: 2, nombre: "Cable Norte" }], meta: { prestado: true } });
        for (const sub of ["credenciales", "conectado", "compartir"]) {
            await page.goto(`/administracion/numeros/9/${sub}`);
            await expect(page).toHaveURL(/\/administracion\/numeros\/9\/prestamo$/);
            await expect(page.getByRole("heading", { level: 1, name: "Avisos de Red Sur" })).toBeVisible();
        }
    });

    test("sin `mensajeria.configurar`: ni «Ver detalle» del prestado ni se entra a préstamo o compartir", async ({ page }) => {
        // Rosa tiene canales.configurar pero no mensajeria.configurar (GET /canales/{id} lo pide).
        const { api } = await abrir(page, "/administracion", { empresas: [{ id: 2, nombre: "Cable Norte" }], meta: { prestado: true, prestamos: true } }, AGENTE_CONV.correo);
        // Sin mensajeria.configurar la lista sale de GET /conversaciones/canales, con el prestado incluido.
        const base = api.conversaciones.canales[0];
        if (base) api.conversaciones.canales.push({ ...structuredClone(base), id: 9, nombre: "Avisos de Red Sur", numero: "+50222000005", prestadoPor: { empresaId: 1, nombre: "Red Sur" } });
        await page.goto("/administracion/numeros");
        await expect(page.locator('[data-numero="9"] [data-insignia="prestado"]')).toHaveText("Prestado por Red Sur");
        await expect(page.getByRole("link", { name: /Ver detalle/ })).toHaveCount(0);
        for (const ruta of ["/administracion/numeros/9/prestamo", "/administracion/numeros/2/compartir"]) {
            await page.goto(ruta);
            await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        }
    });

    test("compartir con otra empresa y dejar de compartir con motivo", async ({ page }) => {
        test.slow();
        const { api } = await abrir(page, "/administracion/numeros/2/compartir", { meta: { prestamos: true } });
        const form = page.getByRole("form", { name: "Compartir Avisos" });
        const cable = form.locator('[data-empresa="2"]');
        await expect(cable).toContainText("Compartido desde el 2 nov");
        await expect(cable.getByRole("checkbox", { name: "Avisos", exact: true })).toBeChecked();
        await elegirOpcion(form.getByLabel("Empresa para agregar"), { label: "Fibra Occidente" });
        await form.getByRole("button", { name: "Agregar empresa" }).click();
        const fibra = form.locator('[data-empresa="3"]');
        await expect(fibra).toContainText("Sin guardar");
        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(fibra.getByRole("alert")).toHaveText("Elige al menos un uso");
        await fibra.getByRole("checkbox", { name: "Avisos", exact: true }).check();
        await elegirOpcion(fibra.getByLabel("Cola en Fibra Occidente"), { label: "Soporte Occidente" });
        await captura(page, "compartir");

        await cable.getByRole("button", { name: "Dejar de compartir con Cable Norte" }).click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Dejar de compartir con Cable Norte?" });
        await expect(dialogo).toContainText("Tiene 4 conversaciones abiertas por este número");
        await expect(dialogo).toContainText("Los 12 avisos programados de Cable Norte");
        await dialogo.getByRole("button", { name: "Dejar de compartir" }).click();
        await expect(dialogo.getByText("Escribe el motivo (al menos 5 letras)")).toBeVisible();
        await dialogo.getByRole("textbox", { name: "Motivo" }).fill("Cable Norte ya tiene su propio número");
        await captura(page, "compartir-revocar");
        await dialogo.getByRole("button", { name: "Dejar de compartir" }).click();
        await expect(page.getByText("«Avisos» ya no se comparte con Cable Norte")).toBeVisible();
        await expect(cable).toHaveCount(0);
        expect(api.meta.cuerpos.find((c) => c.ruta.endsWith("/prestamos/2/revocar"))?.cuerpo).toEqual({ version: 1, motivo: "Cable Norte ya tiene su propio número" });

        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Se guardó con quién se comparte «Avisos»")).toBeVisible();
        expect(api.meta.cuerpos.find((c) => c.ruta.endsWith("/canales/2/prestamos"))?.cuerpo).toEqual({ empresaId: 3, usos: ["avisos"], colaId: 31 });
        await expect(page).toHaveURL(/\/administracion\/numeros$/);
    });

    test("sin `canales.configurar` no hay «Compartir» ni se entra", async ({ page }) => {
        await abrir(page, "/administracion/numeros", { meta: { prestamos: true } }, CREDENCIALES_WA.correo);
        await expect(page.locator('[data-numero="2"] [data-insignia="compartido"]')).toContainText("Compartido con 1 empresa");
        await expect(page.getByRole("link", { name: /Compartir/ })).toHaveCount(0);
        await page.goto("/administracion/numeros/2/compartir");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });
});

test.describe("bandeja con números compartidos", () => {
    async function abrirConversacion(page: Page, correo = ADMIN.correo, prestamos = true, enOtraEmpresa = false) {
        await page.clock.setFixedTime(new Date(AHORA_CONV));
        const ctx = await abrir(page, "/administracion", { meta: { prestamos, ...(enOtraEmpresa ? { enOtraEmpresa: [41] } : {}) } }, correo);
        if (enOtraEmpresa) {
            const c = ctx.api.conversaciones.conversaciones.find((x) => x.id === 41);
            if (c) c.estado = "resuelta";
        }
        ctx.api.conversaciones.mensajes.get(41)?.push(mensaje(41, { direccion: "saliente", texto: "Ya voy para allá", origen: "telefono", creadoEn: new Date(AHORA_CONV - 60_000).toISOString() }));
        await page.goto("/conversaciones?c=41");
        return ctx;
    }

    test("«Desde el teléfono» y «Pasar a otra empresa»", async ({ page }) => {
        const lecturas: string[] = [];
        page.on("request", (r) => lecturas.push(new URL(r.url()).pathname));
        const { api } = await abrirConversacion(page);
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        const telefono = conv.locator('[data-slot="etiqueta-telefono"]');
        await expect(telefono).toHaveText("Desde el teléfono");
        await conv.getByRole("button", { name: "Más acciones" }).click();
        await page.getByRole("menuitem", { name: /Pasar a otra empresa/ }).click();
        const dialogo = page.getByRole("dialog", { name: "Pasar a otra empresa" });
        await expect(dialogo).toContainText("aquí queda de solo lectura");
        // Las empresas las da /empresas-destino (con conversaciones.transferir), nunca /prestamos.
        expect(lecturas).toContain("/api/v1/mensajeria/conversaciones/41/empresas-destino");
        expect(lecturas.some((r) => r.includes("/prestamos"))).toBe(false);
        await dialogo.getByRole("textbox", { name: /Nota para quien la recibe/ }).fill("Es cliente de ustedes");
        if (!esTelefono(page)) await captura(page, "pasar-empresa");
        await dialogo.getByRole("button", { name: "Pasar a Cable Norte" }).click();
        await expect(page.getByText("Conversación con Carlos Ruiz pasada a Cable Norte")).toBeVisible();
        expect(api.meta.cuerpos.find((c) => c.ruta.endsWith("/41/pasar-empresa"))?.cuerpo).toEqual({ version: 7, empresaId: 2, nota: "Es cliente de ustedes" });
        await expect(conv.locator("[data-solo-lectura]")).toHaveText("Solo lectura");
        await expect(conv.getByText(/Solo lectura: esta conversación pasó a otra empresa/)).toBeVisible();
    });

    test("409 conversacion_en_otra_empresa al reabrir: su mensaje, sin «Reintentar»", async ({ page }) => {
        const { api } = await abrirConversacion(page, ADMIN.correo, true, true);
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        // En el teléfono «Reabrir» está en «Más acciones».
        if (esTelefono(page)) {
            await conv.getByRole("button", { name: "Más acciones" }).click();
            await page.getByRole("menuitem", { name: "Reabrir" }).click();
        } else await conv.getByRole("button", { name: "Reabrir" }).click();
        const aviso = page.locator('[data-error-rapido="conversacion_en_otra_empresa"]');
        await expect(aviso).toContainText("La atiende otra empresa que usa este número");
        await expect(aviso).toContainText("Hasta que la resuelvan, aquí no se puede reabrir ni responder.");
        await expect(aviso.getByRole("button", { name: /Reintentar|Recargar/ })).toHaveCount(0);
        expect(api.meta.cuerpos.some((c) => c.ruta.endsWith("/41/reabrir"))).toBe(true);
        await esperarSinDesbordes(page);
        await captura(page, "en-otra-empresa-reabrir");
        await aviso.getByRole("button", { name: "Cerrar" }).click();
        await expect(aviso).toHaveCount(0);
    });

    test("409 conversacion_en_otra_empresa al responder una resuelta: su mensaje, sin «Reintentar»", async ({ page }) => {
        const { api } = await abrirConversacion(page, ADMIN.correo, true, true);
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        await conv.getByRole("textbox", { name: /Mensaje para/ }).fill("¿Sigue sin servicio?");
        await conv.getByRole("button", { name: "Enviar" }).click();
        const hilo = conv.getByRole("log", { name: "Mensajes" });
        await expect(hilo).toContainText("No se envió: la atiende otra empresa que usa este número");
        await expect(hilo.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
        expect(api.meta.cuerpos.some((c) => c.ruta.endsWith("/41/mensajes"))).toBe(true);
        await esperarSinDesbordes(page);
        await captura(page, "en-otra-empresa-responder");
    });

    test("sin empresas destino (lista vacía), no se ofrece", async ({ page }) => {
        const respuesta = page.waitForResponse((r) => r.url().endsWith("/conversaciones/41/empresas-destino"));
        await abrirConversacion(page, ADMIN.correo, false);
        expect(await (await respuesta).json()).toEqual({ data: [], nextCursor: null });
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        await conv.getByRole("button", { name: "Más acciones" }).click();
        await expect(page.getByRole("menuitem", { name: /Pasar a/ }).first()).toBeVisible();
        await expect(page.getByRole("menuitem", { name: /Pasar a otra empresa/ })).toHaveCount(0);
    });

    test("sin permisos para pasar, no se ofrece", async ({ page }) => {
        await abrirConversacion(page, CONSULTA.correo);
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        await expect(conv.locator('[data-slot="etiqueta-telefono"]')).toHaveText("Desde el teléfono");
        await conv.getByRole("button", { name: "Más acciones" }).click();
        await expect(page.getByRole("menuitem", { name: /Pasar a otra empresa/ })).toHaveCount(0);
    });
});
