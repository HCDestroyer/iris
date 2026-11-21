import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { conSesionTecnico, simularApiCampo, type ApiCampo } from "./api-campo";
import { simularTelefonoCampo, type OpcionesTelefonoCampo } from "./api-telefono-campo";
import { PERMISOS_TECNICO } from "./datos-campo";
import { buscarDesbordes } from "./desbordes";
import { simularSip, type OpcionesSip } from "./sip-falso";

/*
 * Llamar al cliente desde la app de campo (E4c fase 3, contratos/voz.md §3.12; maqueta aprobada por el
 * PM: pantallas 23 a 36). «Llamar por la empresa» con JsSIP de verdad contra una Issabel falsa
 * (e2e/sip-falso.ts) por el puente de Iris, sin REGISTER (`dispositivo: app`: solo marca), y «Llamar
 * desde mi celular», que se anota en la orden con `llamada.directa` del lote, también sin señal.
 * Solo en el teléfono (390 px), claro y oscuro, sin desbordes.
 */

test.use({
    permissions: ["microphone"],
    launchOptions: { args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream", "--autoplay-policy=no-user-gesture-required"] },
});

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-campo-telefono");
const slot = (page: Page, nombre: string) => page.locator(`[data-slot="${nombre}"]`);
const esperarSinDesbordes = (page: Page) => expect.poll(() => buscarDesbordes(page), { message: "nada se desborda", timeout: 5_000 }).toEqual([]);
const captura = async (page: Page, nombre: string) => {
    await esperarSinDesbordes(page);
    await page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });
};

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

type Opciones = { telefono?: OpcionesTelefonoCampo; sip?: OpcionesSip; sinPermiso?: boolean; antes?: (page: Page) => Promise<unknown> };

async function abrir(page: Page, o: Opciones = {}) {
    if (o.sinPermiso) await page.context().clearPermissions();
    await o.antes?.(page);
    const sip = await simularSip(page, o.sip ?? {});
    const api = await simularApiCampo(page, { permisos: [...PERMISOS_TECNICO, "centralita.llamar"] });
    const tel = await simularTelefonoCampo(page, api, o.telefono ?? {});
    await page.goto("/campo?orden=101");
    await expect(slot(page, "cliente-orden")).toBeVisible();
    return { api, sip, tel };
}

async function abrirHoja(page: Page) {
    await slot(page, "cliente-orden").getByRole("button", { name: "Llamar al cliente" }).click();
    const hoja = page.getByRole("dialog", { name: /Llamar a María Castillo/ });
    await expect(hoja).toBeVisible();
    return hoja;
}

const directas = (api: ApiCampo) => api.lotes.flatMap((l) => l.operaciones ?? []).filter((op) => op.tipo === "llamada.directa");

test("por la empresa: sin REGISTER, sonando, en curso con teclado, colgar y queda en la orden", async ({ page }) => {
    const { sip, tel } = await abrir(page);
    await expect(slot(page, "indicador-telefono")).toHaveAttribute("data-estado", "listo");
    await captura(page, "23-orden");
    const hoja = await abrirHoja(page);
    await expect(hoja.getByRole("radio", { name: /Contacto en sitio/ })).toHaveAttribute("aria-checked", "true");
    await expect(hoja.getByText("El cliente verá")).toBeVisible();
    await captura(page, "24-hoja");

    await hoja.getByRole("button", { name: /Llamar por la empresa/ }).click();
    const pantalla = slot(page, "pantalla-llamada");
    await expect(pantalla).toHaveAttribute("data-fase", "llamando", { timeout: 20_000 });
    await expect(pantalla.getByText("Sonando…")).toBeVisible();
    // Solo marca: abrió con `app` y nunca mandó REGISTER; el número lo pone el servidor.
    expect(tel.abiertos).toEqual([{ dispositivo: "app" }]);
    expect(sip.de("REGISTER")).toHaveLength(0);
    expect(tel.llamadas).toEqual([{ ordenId: 101, telefono: "contacto" }]);
    await expect.poll(() => sip.sonando()).toBe(true);
    expect(sip.de("INVITE")[0]).toContain(`sip:iris-${tel.filas[0]?.id}@pbx.oficina`);
    await captura(page, "25-sonando");

    sip.contestar();
    await expect(pantalla).toHaveAttribute("data-fase", "en_curso");
    await expect(slot(page, "reloj-llamada")).toBeVisible();
    await captura(page, "26-en-llamada");
    await pantalla.getByRole("button", { name: "Teclado" }).click();
    await pantalla.getByRole("button", { name: /^1/ }).click();
    await expect.poll(() => sip.de("INFO").length).toBe(1);
    await captura(page, "33-teclado");

    const fila = tel.filas[0];
    if (fila) Object.assign(fila, { estado: "terminada", contestadaEn: new Date().toISOString(), duracionSeg: 192 });
    await pantalla.getByRole("button", { name: "Colgar" }).click();
    await expect(pantalla).toHaveCount(0);
    await expect(page.getByText(/Llamada terminada · \d+ s · quedó en la orden/)).toBeVisible();
    await expect(slot(page, "llamadas-orden").locator('[data-slot="fila-llamada"]').first()).toContainText("Por la empresa · tú (ext. 311)");
    await expect(slot(page, "llamadas-orden")).toContainText("3 min 12 s · Contestó");
    await captura(page, "34-terminada");
    // La conexión se cierra al colgar (la app solo la abre para llamar).
    await expect.poll(() => sip.de("BYE").length).toBe(1);
});

test("no contestó: lo dice en la orden con «Llamar otra vez» y «Desde mi celular»", async ({ page }) => {
    const { sip } = await abrir(page);
    const hoja = await abrirHoja(page);
    await hoja.getByRole("button", { name: /Llamar por la empresa/ }).click();
    await expect(slot(page, "pantalla-llamada")).toHaveAttribute("data-fase", "llamando", { timeout: 20_000 });
    await expect.poll(() => sip.sonando()).toBe(true);
    sip.rechazarSaliente(480, "Temporarily Unavailable");
    const aviso = slot(page, "aviso-llamada");
    await expect(aviso).toHaveAttribute("data-aviso", "no_contesto");
    await expect(aviso.getByRole("button", { name: "Llamar otra vez" })).toBeVisible();
    await expect(aviso.getByRole("button", { name: "Desde mi celular" })).toBeVisible();
    await captura(page, "36-no-contesto");
});

test("desde mi celular con señal: se anota en la orden con la hora de la llamada", async ({ page }) => {
    const { api } = await abrir(page);
    const hoja = await abrirHoja(page);
    await hoja.getByRole("button", { name: "Llamar desde mi celular" }).click();
    await expect(page.getByText(/Llamada directa anotada en la orden/)).toBeVisible();
    await expect.poll(() => directas(api).length).toBe(1);
    expect(directas(api)[0]).toMatchObject({ ordenId: 101, datos: { telefono: "contacto" } });
    expect(directas(api)[0]?.ocurridoEn).toBeTruthy();
    await expect(slot(page, "llamadas-orden")).toContainText("Llamada directa desde tu celular");
    await captura(page, "35-directa");
});

test("sin señal: solo desde mi celular, se guarda en el teléfono y se anota al volver la señal", async ({ page }) => {
    const { api } = await abrir(page);
    api.sinRed = true;
    await page.context().setOffline(true);
    await expect.poll(async () => (await slot(page, "indicador-telefono").getAttribute("data-estado")) ?? "", { timeout: 15_000 }).toBe("sin_senal");
    const hoja = await abrirHoja(page);
    await expect(slot(page, "hoja-llamar")).toHaveAttribute("data-empresa", "sin_senal");
    await expect(hoja.getByRole("button", { name: /Llamar por la empresa/ })).toHaveCount(0);
    await expect(hoja.getByText("Sin señal")).toBeVisible();
    await captura(page, "31-sin-senal");
    await hoja.getByRole("button", { name: "Llamar desde mi celular" }).click();
    await expect(page.getByText(/guardada en el teléfono: se anota en la orden al volver la señal/)).toBeVisible();
    await expect(slot(page, "llamadas-orden").locator('[data-pendiente="true"]')).toContainText("Se anota al volver la señal");
    await captura(page, "35b-directa-sin-senal");
    expect(directas(api)).toHaveLength(0);

    api.sinRed = false;
    await page.context().setOffline(false);
    await expect.poll(() => directas(api).length, { timeout: 30_000 }).toBe(1);
});

test("sin extensión: el aviso y solo desde mi celular", async ({ page }) => {
    await abrir(page, { telefono: { mio: { disponible: false, motivo: "sin_extension", extension: null } } });
    const hoja = await abrirHoja(page);
    await expect(slot(page, "hoja-llamar")).toHaveAttribute("data-empresa", "sin_extension");
    await expect(hoja.getByText("Todavía no tienes extensión de la empresa")).toBeVisible();
    await expect(hoja.getByRole("button", { name: /Llamar por la empresa/ })).toHaveCount(0);
    await captura(page, "24b-sin-extension");
});

test("el micrófono se pide al usarlo, con la explicación antes del permiso", async ({ page }) => {
    const { api } = await abrir(page, { sinPermiso: true });
    const hoja = await abrirHoja(page);
    await hoja.getByRole("button", { name: /Llamar por la empresa/ }).click();
    const permiso = page.getByRole("dialog", { name: "Iris Campo necesita el micrófono para esta llamada" });
    await expect(permiso).toBeVisible();
    await captura(page, "32-permiso");
    // «Mejor desde mi celular»: la directa, sin pedir el micrófono.
    await permiso.getByRole("button", { name: "Mejor desde mi celular" }).click();
    await expect.poll(() => directas(api).length).toBe(1);
});

test("la señal muy débil no deja llamar por la empresa (2G)", async ({ page }) => {
    const { tel } = await abrir(page, { antes: (p) => p.addInitScript(() => Object.defineProperty(navigator, "connection", { value: { effectiveType: "2g", rtt: 2500 }, configurable: true })) });
    const hoja = await abrirHoja(page);
    await hoja.getByRole("button", { name: /Llamar por la empresa/ }).click();
    await expect(slot(page, "aviso-llamada")).toHaveAttribute("data-aviso", "senal");
    expect(tel.abiertos).toHaveLength(0);
    await captura(page, "29-senal");
});

test("la centralita no responde: «No se pudo llamar» con «Reintentar» y «Desde mi celular»", async ({ page }) => {
    await abrir(page, { sip: { registro: "sin_conexion" } });
    const hoja = await abrirHoja(page);
    await hoja.getByRole("button", { name: /Llamar por la empresa/ }).click();
    const aviso = slot(page, "aviso-llamada");
    await expect(aviso).toHaveAttribute("data-aviso", "iris", { timeout: 20_000 });
    await expect(aviso.getByRole("button", { name: "Reintentar" })).toBeVisible();
    await expect(slot(page, "pantalla-llamada")).toHaveCount(0);
    await captura(page, "28-sin-centralita");
});

test("el límite de llamadas del día: solo desde mi celular", async ({ page }) => {
    await abrir(page, { telefono: { limite: true } });
    const hoja = await abrirHoja(page);
    await hoja.getByRole("button", { name: /Llamar por la empresa/ }).click();
    const aviso = slot(page, "aviso-llamada");
    await expect(aviso).toHaveAttribute("data-aviso", "limite", { timeout: 20_000 });
    await expect(aviso.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
    await expect(aviso.getByRole("button", { name: "Desde mi celular" })).toBeVisible();
    await captura(page, "36b-limite");
});

// Revisión de #258, M1: cerrar sesión con una llamada en curso cuelga (BYE a la Issabel) y suelta el
// micrófono antes de esperar a nada; no queda audio abierto en el teléfono de otra persona. Cerrar
// sesión recarga la página (y eso también corta el audio), así que cada pista avisa a la prueba
// cuando la app la detiene, antes de navegar.
test("cerrar sesión con una llamada en curso cuelga y suelta el micrófono", async ({ page }) => {
    const pistas = new Map<string, string>();
    const { sip } = await abrir(page, {
        antes: async (p) => {
            await p.exposeFunction("__pista", (id: string, estado: string) => void pistas.set(id, estado));
            await p.addInitScript(() => {
                const avisar = (window as unknown as { __pista: (id: string, estado: string) => void }).__pista;
                const original = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
                navigator.mediaDevices.getUserMedia = async (c) => {
                    const s = await original(c);
                    for (const t of s.getTracks()) {
                        avisar(t.id, t.readyState);
                        const detener = t.stop.bind(t);
                        t.stop = () => {
                            detener();
                            avisar(t.id, t.readyState);
                        };
                    }
                    return s;
                };
            });
        },
    });
    const hoja = await abrirHoja(page);
    await hoja.getByRole("button", { name: /Llamar por la empresa/ }).click();
    const pantalla = slot(page, "pantalla-llamada");
    await expect.poll(() => sip.sonando(), { timeout: 20_000 }).toBe(true);
    sip.contestar();
    await expect(pantalla).toHaveAttribute("data-fase", "en_curso");
    expect([...pistas.values()]).toContain("live");

    // La llamada tapa la app: se llega a «Yo» por la isla sin colgar (como al volver de otra app).
    await page.locator('nav[aria-label="Secciones de la app"] a[href*="vista=yo"]').dispatchEvent("click");
    await page.getByRole("button", { name: "Cerrar sesión", includeHidden: true }).dispatchEvent("click");
    await expect(page).toHaveURL(/\/campo\?entrar=1$/);
    await expect.poll(() => sip.de("BYE").length).toBeGreaterThanOrEqual(1);
    expect(pistas.size).toBeGreaterThan(0);
    expect([...pistas.values()].every((e) => e === "ended")).toBe(true);
});
