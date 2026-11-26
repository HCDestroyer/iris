import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { llamadaCentralita, type OpcionesCentralita } from "./api-centralita";
import { ADMIN, conSesion, EMPRESAS, simularApi } from "./api-falsa";
import type { OpcionesTelefono } from "./api-telefono";
import { buscarDesbordes } from "./desbordes";
import { simularSip, type OpcionesSip, type SipFalso } from "./sip-falso";

/*
 * El teléfono de Iris en el panel (E4c, contratos/voz.md §3.12; maqueta aprobada el 21 nov 2026):
 * encender con el permiso del micrófono, la entrante (la ventana de E4a con «Contestar» y
 * «Rechazar»), la llamada (silenciar, teclado, nota, colgar, transferir), llamar desde la ficha y
 * con el teclado, los errores en llano con «Reintentar», una sola pestaña y la administración (WSS,
 * guía de la Issabel, credencial de la extensión). JsSIP habla SIP de verdad con una Issabel falsa
 * (e2e/sip-falso.ts) por el WebSocket del puente; el micrófono es el falso de Chromium.
 * En los 4 proyectos (1280 y 390, claro y oscuro) y sin desbordes.
 */

test.use({
    permissions: ["microphone"],
    launchOptions: { args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream", "--autoplay-policy=no-user-gesture-required"] },
});

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-telefono");
const UNA = [EMPRESAS[0] ?? { id: 10, nombre: "Cable Norte" }];
const ID_LLAMADA = "0193f0aa-0000-7000-8000-000000000001";

/** Sin desbordes (espera a que terminen las animaciones de entrada de los diálogos). */
const esperarSinDesbordes = (page: Page, selector = "body") => expect.poll(() => buscarDesbordes(page, selector), { message: "nada se desborda ni queda fuera de pantalla", timeout: 5_000 }).toEqual([]);
const slot = (page: Page, nombre: string) => page.locator(`[data-slot="${nombre}"]`);
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });

type Opciones = { telefono?: OpcionesTelefono; centralita?: OpcionesCentralita; sip?: OpcionesSip; encendido?: boolean; antes?: (page: Page) => Promise<unknown> };

async function abrir(page: Page, ruta: string, o: Opciones = {}) {
    const sip = await simularSip(page, o.sip ?? {});
    if (o.encendido !== false) await page.addInitScript(() => window.localStorage.setItem("iris-telefono-1", "encendido"));
    await o.antes?.(page);
    const api = await simularApi(page, {
        centralita: o.centralita ?? { conectada: true, extensiones: { 1: "204", 14: "205" }, navegador: [1] },
        telefono: { encendido: true, ...o.telefono },
        empresas: UNA,
        conversaciones: { flujoLargo: true },
    });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return { api, sip };
}

/** El teléfono registrado en la «Issabel» y con el punto verde. */
async function listo(page: Page, sip: SipFalso) {
    await expect(slot(page, "boton-telefono")).toHaveAttribute("data-estado", "listo", { timeout: 20_000 });
    expect(sip.de("REGISTER").length).toBeGreaterThan(0);
}

test.describe("Teléfono: encender", () => {
    test("primera vez: la invitación, el permiso del micrófono y el punto verde", async ({ page }) => {
        const { sip } = await abrir(page, "/", { encendido: false });
        const invitacion = slot(page, "invitacion-telefono");
        await expect(invitacion).toContainText("Ya puedes contestar la centralita aquí en Iris");
        await expect(slot(page, "boton-telefono")).toHaveAttribute("data-estado", "apagado");
        // Sin encender no hay conexión con la centralita.
        expect(sip.conexiones).toHaveLength(0);
        await esperarSinDesbordes(page);
        await captura(page, "02-primera-vez");
        await invitacion.getByRole("button", { name: "Encender el teléfono" }).click();
        const dialogo = page.getByRole("dialog", { name: "Encender el teléfono de Iris" });
        await expect(dialogo).toContainText("Elige Permitir");
        await captura(page, "03-primera-vez-permiso");
        await dialogo.getByRole("button", { name: "Encender", exact: true }).click();
        await expect(page.getByText("Teléfono listo: tu extensión 204 suena aquí")).toBeVisible({ timeout: 20_000 });
        await listo(page, sip);
        await expect(invitacion).toHaveCount(0);
        // El ticket viaja como subprotocolo, nunca en la URL; el Contact lleva el usuario SIP y .invalid.
        expect(sip.conexiones[0]).toMatch(/\/api\/v1\/mensajeria\/telefono\/sip$/);
        const registro = sip.de("REGISTER")[0] ?? "";
        expect(registro).toMatch(/Contact: <sip:204@[a-z0-9]+\.invalid;transport=ws>/);
        expect(registro).toMatch(/From: (?:"[^"]*" )?<sip:204@pbx\.oficina>/);
        expect(registro).not.toMatch(/Authorization/i);
        if (test.info().project.name.startsWith("telefono")) {
            // En el teléfono el aviso «Teléfono listo» va sobre la fila del encabezado, donde está el
            // botón del teléfono: se toca cuando el aviso se fue (si no, el toque cae en el aviso).
            await expect(page.getByText("Teléfono listo: tu extensión 204 suena aquí")).toHaveCount(0, { timeout: 10_000 });
        }
        await slot(page, "boton-telefono").click();
        await expect(slot(page, "estado-telefono")).toContainText("Listo para contestar y llamar");
        await expect(slot(page, "estado-telefono")).toContainText("Tu extensión 204 suena aquí");
        await captura(page, "01-estado-listo");
        // Recargar: queda encendido (preferencia de este navegador) y se vuelve a registrar solo.
        await page.reload();
        await listo(page, sip);
        expect(sip.conexiones.length).toBe(2);
    });

    test("«Ahora no»: queda apagado en este navegador, sin conectar", async ({ page }) => {
        const { sip } = await abrir(page, "/", { encendido: false });
        await slot(page, "invitacion-telefono").getByRole("button", { name: "Ahora no" }).click();
        await expect(slot(page, "invitacion-telefono")).toHaveCount(0);
        await expect(slot(page, "boton-telefono")).toHaveAttribute("data-estado", "apagado");
        expect(sip.conexiones).toHaveLength(0);
    });

    test("sin extensión del navegador no hay teléfono", async ({ page }) => {
        await abrir(page, "/tickets", { telefono: { mio: { disponible: false, motivo: "extension_escritorio", dispositivos: null } } });
        await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
        await expect(slot(page, "boton-telefono")).toHaveCount(0);
    });
});

test.describe("Teléfono: llamadas", () => {
    test("entrante del cliente: contestar, silenciar, tonos, nota y colgar", async ({ page }) => {
        const { api, sip } = await abrir(page, "/tickets");
        await listo(page, sip);
        await expect.poll(() => api.flujoAbierto(), { timeout: 15_000 }).toBe(true);
        api.centralita.sonar(llamadaCentralita());
        const callId = sip.llamar("+50255123480", ID_LLAMADA);
        const ventana = slot(page, "entrante-telefono");
        await expect(ventana).toContainText("María Fernanda Pérez López");
        await expect(ventana).toContainText("suena en Iris (ext. 204)");
        await expect(ventana.locator('[data-slot="servicio-llamada"]')).toContainText("Fibra 50 Mbps");
        // Una sola ventana: la de E4a no sale aparte.
        await expect(slot(page, "aviso-centralita")).toHaveCount(0);
        await esperarSinDesbordes(page, '[data-slot="entrante-telefono"]');
        await captura(page, "04-entrante-cliente");
        await ventana.getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => sip.de("200").some((r) => /CSeq: 1 INVITE/.test(r) && /Content-Type: application\/sdp/.test(r)), { timeout: 15_000 }).toBe(true);
        const barra = slot(page, "barra-telefono");
        await expect(barra).toHaveAttribute("data-fase", "en_curso", { timeout: 15_000 });
        await expect(barra).toContainText("María Fernanda Pérez López");
        const panel = slot(page, "panel-llamada");
        await expect(panel.getByRole("timer")).toBeVisible();
        await esperarSinDesbordes(page, '[data-slot="panel-llamada"]');
        await captura(page, "07-en-llamada");
        await panel.getByRole("button", { name: "Silenciar" }).click();
        await expect(panel.getByRole("button", { name: "Silenciada" })).toHaveAttribute("aria-pressed", "true");
        // Tonos de a uno, por INFO.
        await panel.getByRole("button", { name: "Teclado" }).click();
        const tonos = page.getByRole("dialog", { name: "Teclado" });
        await tonos.getByRole("button", { name: "1", exact: true }).click();
        await tonos.getByRole("button", { name: "4", exact: true }).click();
        await expect(slot(page, "tonos-marcados")).toHaveText("14");
        await captura(page, "10-en-llamada-teclado");
        await expect.poll(() => sip.de("INFO").map((m) => /Signal=(\S)/.exec(m)?.[1])).toEqual(["1", "4"]);
        await tonos.getByRole("button", { name: "Cerrar teclado" }).click();
        // La nota se guarda sola.
        await panel.getByLabel("Nota de la llamada").fill("Sigue sin internet. Pide visita mañana.");
        await expect.poll(() => api.centralita.recibido.anotaciones.length, { timeout: 10_000 }).toBeGreaterThan(0);
        expect(api.centralita.recibido.anotaciones.at(-1)).toMatchObject({ nota: "Sigue sin internet. Pide visita mañana." });
        await panel.getByRole("button", { name: "Colgar" }).click();
        await expect.poll(() => sip.de("BYE").length).toBe(1);
        await expect(barra).toHaveCount(0);
        await expect(panel).toHaveCount(0);
        expect(callId).toContain("@10.20.0.5");
    });

    test("entrante sin cliente: «Rechazar» la devuelve a la centralita (486)", async ({ page }) => {
        const { sip } = await abrir(page, "/tickets");
        await listo(page, sip);
        sip.llamar("+50240217788");
        const ventana = slot(page, "entrante-telefono");
        await expect(ventana).toContainText("+502 4021 7788");
        await expect(ventana).toContainText("Este número no está en Argos");
        await captura(page, "05-entrante-sin-cliente");
        await ventana.getByRole("button", { name: "Rechazar" }).click();
        await expect.poll(() => sip.de("486").length).toBe(1);
        await expect(ventana).toHaveCount(0);
    });

    test("la Issabel cuelga: la barra y el panel se van", async ({ page }) => {
        const { sip } = await abrir(page, "/tickets");
        await listo(page, sip);
        const callId = sip.llamar("+50240217788");
        await slot(page, "entrante-telefono").getByRole("button", { name: "Contestar" }).click();
        await expect(slot(page, "barra-telefono")).toHaveAttribute("data-fase", "en_curso", { timeout: 15_000 });
        sip.colgar(callId);
        await expect(slot(page, "barra-telefono")).toHaveCount(0, { timeout: 10_000 });
    });

    test("transferir a un compañero de la lista (REFER a su extensión)", async ({ page }) => {
        const { sip } = await abrir(page, "/tickets");
        await listo(page, sip);
        sip.llamar("+50255123480");
        await slot(page, "entrante-telefono").getByRole("button", { name: "Contestar" }).click();
        const panel = slot(page, "panel-llamada");
        await expect(slot(page, "barra-telefono")).toHaveAttribute("data-fase", "en_curso", { timeout: 15_000 });
        await panel.getByRole("button", { name: "Transferir" }).click();
        const dialogo = page.getByRole("dialog", { name: /Transferir a/ });
        await expect(dialogo).toContainText("Carlos Méndez · ext. 205");
        await expect(dialogo).toContainText("Técnicos de guardia · grupo 600");
        await esperarSinDesbordes(page, '[role="dialog"]');
        await captura(page, "11-transferir");
        await dialogo.getByRole("radio", { name: /Carlos Méndez · ext\. 205/ }).check({ force: true });
        await dialogo.getByRole("button", { name: "Transferir" }).click();
        await expect(page.getByText("Llamada transferida a Carlos Méndez")).toBeVisible({ timeout: 15_000 });
        const refer = sip.de("REFER")[0] ?? "";
        expect(refer).toMatch(/Refer-To: <?sip:205@pbx\.oficina>?/);
    });

    test("llamar desde la ficha del ticket: el número lo pone el servidor", async ({ page }) => {
        const { api, sip } = await abrir(page, "/tickets/1");
        await listo(page, sip);
        const ficha = page.getByTestId("ficha-cliente");
        const boton = ficha.locator('[data-slot="llamar-telefono"]');
        await expect(boton).toBeVisible();
        await boton.click();
        const menu = page.getByRole("menu");
        if (await menu.count()) {
            await captura(page, "12-llamar-desde-ficha");
            await menu.getByRole("menuitem").first().click();
        }
        await expect.poll(() => api.telefono.recibido.llamadas.length).toBe(1);
        expect(api.telefono.recibido.llamadas[0]).toMatchObject({ ticketId: 1 });
        await expect.poll(() => sip.de("INVITE").length, { timeout: 15_000 }).toBe(1);
        expect(sip.de("INVITE")[0]).toMatch(/^INVITE sip:iris-0193f0aa-[0-9a-f-]+@pbx\.oficina SIP\/2\.0/);
        const barra = slot(page, "barra-telefono");
        await expect(barra).toHaveAttribute("data-fase", "llamando");
        await expect(barra).toContainText("Llamando a");
        await esperarSinDesbordes(page);
        await captura(page, "14-saliente-sonando");
        expect(sip.contestar()).toBe(true);
        await expect(barra).toHaveAttribute("data-fase", "en_curso", { timeout: 15_000 });
        await barra.getByRole("button", { name: "Colgar" }).click();
        await expect.poll(() => sip.de("BYE").length).toBe(1);
    });

    test("marcar con el teclado: solo Guatemala y extensiones", async ({ page }) => {
        const { api, sip } = await abrir(page, "/tickets");
        await listo(page, sip);
        await slot(page, "boton-telefono").click();
        await slot(page, "estado-telefono").getByRole("button", { name: "Marcar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Marcar" });
        const campo = dialogo.getByLabel("Número");
        await campo.fill("0019005551234");
        await expect(dialogo).toContainText("Solo números de Guatemala");
        await expect(dialogo.getByRole("button", { name: "Llamar" })).toBeDisabled();
        await campo.fill("");
        for (const d of "55123480") await dialogo.getByRole("group", { name: "Teclado para marcar" }).getByRole("button", { name: d, exact: true }).click();
        await expect(campo).toHaveValue("5512 3480");
        await esperarSinDesbordes(page, '[role="dialog"]');
        await captura(page, "13-marcar-teclado");
        await dialogo.getByRole("button", { name: "Llamar" }).click();
        await expect.poll(() => api.telefono.recibido.llamadas).toEqual([{ numero: "55123480" }]);
        await expect.poll(() => sip.de("INVITE").length, { timeout: 15_000 }).toBe(1);
    });
});

test.describe("Teléfono: errores en llano", () => {
    test("sin conexión con la centralita: el aviso con «Reintentar»", async ({ page }) => {
        await abrir(page, "/tickets", { sip: { registro: "sin_conexion" } });
        const aviso = slot(page, "aviso-telefono");
        await expect(aviso).toHaveAttribute("data-error", "sin_conexion", { timeout: 20_000 });
        await expect(aviso).toContainText("No hay conexión con la centralita");
        await expect(aviso.getByRole("button", { name: "Reintentar" })).toBeVisible();
        await expect(slot(page, "boton-telefono")).toContainText("Sin conexión");
        await esperarSinDesbordes(page, '[data-slot="aviso-telefono"]');
        await captura(page, "19-error-sin-centralita");
    });

    test("la centralita no aceptó la contraseña: el puente corta y no se reintenta solo", async ({ page }) => {
        const { sip } = await abrir(page, "/tickets", { sip: { registro: "credenciales" } });
        const aviso = slot(page, "aviso-telefono");
        await expect(aviso).toHaveAttribute("data-error", "clave", { timeout: 20_000 });
        await expect(aviso).toContainText("La centralita no aceptó la contraseña de tu extensión");
        await expect(aviso).toContainText("un administrador tiene que escribirla otra vez");
        await expect(slot(page, "boton-telefono")).toContainText("Teléfono apagado");
        await captura(page, "20-error-contrasena");
        // fail2ban: ni JsSIP ni el panel vuelven a intentar por su cuenta.
        await page.waitForTimeout(7_000);
        expect(sip.de("REGISTER")).toHaveLength(1);
        expect(sip.conexiones).toHaveLength(1);
    });

    test("micrófono bloqueado: los pasos para permitirlo", async ({ page }) => {
        await abrir(page, "/tickets", {
            antes: (p) =>
                p.addInitScript(() => {
                    const consulta = navigator.permissions.query.bind(navigator.permissions);
                    navigator.permissions.query = (d: PermissionDescriptor) => (d.name === ("microphone" as PermissionName) ? Promise.resolve({ state: "denied" } as PermissionStatus) : consulta(d));
                }),
        });
        const aviso = slot(page, "aviso-telefono");
        await expect(aviso).toHaveAttribute("data-error", "microfono", { timeout: 20_000 });
        await expect(aviso).toContainText("Iris no tiene permiso para usar tu micrófono");
        await expect(aviso.locator("ol li")).toHaveCount(3);
        await captura(page, "21-error-microfono");
    });

    test("una sola pestaña: la otra dice «En otra pestaña» y puede tomarlo", async ({ page, context }) => {
        const { sip } = await abrir(page, "/tickets");
        await listo(page, sip);
        const otra = await context.newPage();
        const sip2 = await simularSip(otra);
        await simularApi(otra, { centralita: { conectada: true, extensiones: { 1: "204", 14: "205" }, navegador: [1] }, telefono: { encendido: true }, empresas: UNA, conversaciones: { flujoLargo: true } });
        await otra.goto("/tickets");
        const aviso = slot(otra, "aviso-telefono");
        await expect(aviso).toHaveAttribute("data-error", "pestana", { timeout: 20_000 });
        await expect(aviso).toContainText("El teléfono está abierto en otra pestaña de Iris");
        await expect(slot(otra, "boton-telefono")).toContainText("En otra pestaña");
        expect(sip2.conexiones).toHaveLength(0);
        await captura(otra, "22-error-otra-pestana");
        await aviso.getByRole("button", { name: "Usar el teléfono aquí" }).click();
        await expect(slot(otra, "boton-telefono")).toHaveAttribute("data-estado", "listo", { timeout: 20_000 });
        await expect(slot(page, "boton-telefono")).toHaveAttribute("data-estado", "pestana", { timeout: 10_000 });
    });
});

test.describe("Teléfono: administración", () => {
    const RUTA = "/administracion/integraciones/centralita";

    test("la centralita: el teléfono encendido y dónde contesta cada persona", async ({ page }) => {
        await abrir(page, RUTA, { encendido: false });
        const tarjeta = slot(page, "telefono-centralita");
        await expect(tarjeta).toContainText("wss://10.20.0.5:8089/ws");
        await expect(slot(page, "estado-telefono-centralita")).toHaveAttribute("data-estado", "encendido");
        const filas = slot(page, "extensiones-centralita").locator('[data-slot="extension"]');
        await expect(filas.filter({ hasText: "Ana López" })).toContainText("En Iris");
        await expect(filas.filter({ hasText: "Ana López" })).toContainText("Registro probado");
        await expect(filas.filter({ hasText: "Rosa Pop" })).toContainText("Teléfono de escritorio");
        await expect(slot(page, "grupos-centralita")).toContainText("Técnicos de guardia");
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `15-admin-extensiones-${test.info().project.name}.png`), fullPage: true });
    });

    test("extensión «En Iris»: la contraseña una vez y «Probar registro» al guardar", async ({ page }) => {
        const { api } = await abrir(page, RUTA, { encendido: false });
        await slot(page, "extensiones-centralita").getByRole("link", { name: "Configurar la extensión de Rosa Pop" }).click();
        await expect(page.getByRole("heading", { name: "Extensión de Rosa Pop" })).toBeVisible();
        await page.getByRole("radio", { name: /En Iris/ }).check({ force: true });
        await expect(page.getByLabel("Usuario SIP")).toHaveValue("205");
        await page.getByLabel("Contraseña SIP", { exact: true }).fill("clave-de-la-205");
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `16-admin-extension-nueva-${test.info().project.name}.png`), fullPage: true });
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText(/Extensión guardada: la centralita aceptó la extensión 205/)).toBeVisible();
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        expect(api.telefono.recibido.extensiones).toEqual([{ tipo: "navegador", sipUsuario: "205", sipSecreto: "clave-de-la-205" }]);
        expect(api.telefono.recibido.registros).toBe(1);
    });

    test("la centralita no acepta la contraseña: lo dice y solo deja cambiarla (fail2ban)", async ({ page }) => {
        const { api } = await abrir(page, RUTA, { encendido: false, telefono: { registro: "credenciales" } });
        await slot(page, "extensiones-centralita").getByRole("link", { name: "Configurar la extensión de Rosa Pop" }).click();
        await page.getByRole("radio", { name: /En Iris/ }).check({ force: true });
        await page.getByLabel("Contraseña SIP", { exact: true }).fill("clave-mala-205");
        await page.getByRole("button", { name: "Guardar" }).click();
        const r = slot(page, "resultado-registro");
        await expect(r).toHaveAttribute("data-estado", "credenciales");
        await expect(r).toContainText("La centralita no aceptó la contraseña de la extensión 205");
        // Revisión de #252, punto 1: con la misma contraseña ni «Reintentar» ni «Probar registro»; solo cambiarla.
        await expect(r.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Probar registro" })).toHaveCount(0);
        await expect(r.getByRole("button", { name: "Cambiar la contraseña" })).toBeVisible();
        await expect(page.getByText("•••• guardada")).toBeVisible();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `17-admin-extension-guardada-error-${test.info().project.name}.png`), fullPage: true });
        expect(api.telefono.recibido.registros).toBe(1);
        // Al volver a la pantalla sigue igual (lo dice `sipRechazadoEn`).
        await page.reload();
        await expect(slot(page, "resultado-registro")).toHaveAttribute("data-estado", "credenciales");
        await expect(page.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Probar registro" })).toHaveCount(0);
        await slot(page, "resultado-registro").getByRole("button", { name: "Cambiar la contraseña" }).click();
        await expect(page.getByLabel("Contraseña SIP", { exact: true })).toBeFocused();
        await expect(slot(page, "resultado-registro").getByRole("button", { name: "Cambiar la contraseña" })).toHaveCount(0);
        expect(api.telefono.recibido.registros).toBe(1);
    });

    test("guardar con la centralita en espera (429): queda guardada y dice cuánto esperar", async ({ page }) => {
        // Re-verificación de #252, B3: el 429 de «Probar registro» no es «No se pudo guardar».
        const { api } = await abrir(page, RUTA, { encendido: false, telefono: { rechazosEsperan: 540 } });
        await slot(page, "extensiones-centralita").getByRole("link", { name: "Configurar la extensión de Rosa Pop" }).click();
        await page.getByRole("radio", { name: /En Iris/ }).check({ force: true });
        await page.getByLabel("Contraseña SIP", { exact: true }).fill("clave-de-la-205");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Extensión guardada; no se pudo probar")).toBeVisible();
        await expect(page.getByText(/espera 9 min/)).toBeVisible();
        if (test.info().project.name.startsWith("telefono")) {
            // Un aviso con descripción, en el teléfono y ya en su sitio: no baja hasta la miga «‹ …»
            // de la pantalla, que se puede pulsar.
            const caja = page.locator('[data-region="avisos-emergentes"] [data-slot="toast"]').first();
            const miga = page.getByRole("navigation", { name: "Ruta de navegación" }).filter({ visible: true });
            let anterior = Number.NaN;
            await expect
                .poll(async () => {
                    const a = await caja.boundingBox();
                    const quieto = a !== null && a.y === anterior;
                    anterior = a?.y ?? Number.NaN;
                    return quieto;
                })
                .toBe(true);
            const a = await caja.boundingBox();
            const m = await miga.boundingBox();
            expect(a && m && a.y >= 0 && a.y + a.height <= m.y, JSON.stringify({ a, m })).toBe(true);
            const enlace = await miga.getByRole("link").first().boundingBox();
            const libre = enlace
                ? await page.evaluate(([x, y]) => document.elementFromPoint(x, y)?.closest("nav") !== null, [enlace.x + enlace.width / 2, enlace.y + enlace.height / 2] as const)
                : false;
            expect(libre).toBe(true);
        }
        await expect(page.locator('[data-slot="error-guardar"]')).toHaveCount(0);
        // Lo guardado: «•••• guardada» y sin cambios pendientes («Probar registro» a mano).
        await expect(page.getByText("•••• guardada")).toBeVisible();
        await expect(page.getByRole("button", { name: "Probar registro" })).toBeVisible();
        expect(api.telefono.recibido.extensiones).toHaveLength(1);
    });

    test("«Reintentar» se queda cuando la Issabel no conecta (no gasta la contraseña)", async ({ page }) => {
        const { api } = await abrir(page, RUTA, { encendido: false, telefono: { registro: "no_conecta" } });
        await slot(page, "extensiones-centralita").getByRole("link", { name: "Configurar la extensión de Rosa Pop" }).click();
        await page.getByRole("radio", { name: /En Iris/ }).check({ force: true });
        await page.getByLabel("Contraseña SIP", { exact: true }).fill("clave-de-la-205");
        await page.getByRole("button", { name: "Guardar" }).click();
        const r = slot(page, "resultado-registro");
        await expect(r).toHaveAttribute("data-estado", "no_conecta");
        await expect(r.getByRole("button", { name: "Cambiar la contraseña" })).toHaveCount(0);
        await r.getByRole("button", { name: "Reintentar" }).click();
        await expect.poll(() => api.telefono.recibido.registros).toBe(2);
    });

    test("configurar el teléfono: probar, comparar la huella, confirmar la guía y encender", async ({ page }) => {
        const { api } = await abrir(page, `${RUTA}/telefono`, { encendido: false, telefono: { encendido: false } });
        await expect(page.getByRole("heading", { name: "Teléfono en Iris" })).toBeVisible();
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await page.getByRole("button", { name: "Probar", exact: true }).click();
        await expect(slot(page, "resultado-prueba-telefono")).toHaveAttribute("data-estado", "ok");
        await page.getByText("Es la misma que muestra mi Issabel").click();
        await page.getByLabel("Prefijo de salida").fill("9");
        await expect(slot(page, "restricciones")).toContainText("TRANSFER_CONTEXT=from-iris");
        await page.getByText("Confirmo que la Issabel está configurada como dice la guía").click();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `18-admin-preparar-issabel-${test.info().project.name}.png`), fullPage: true });
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Teléfono guardado y encendido")).toBeVisible();
        expect(api.telefono.recibido.configuraciones[0]).toMatchObject({ wssPuerto: 8089, wssRuta: "/ws", prefijoSalida: "9", confirmoRestricciones: true, llamadasPorUsuarioDia: 60 });
        expect(api.telefono.recibido.activaciones).toBe(1);
    });
});
