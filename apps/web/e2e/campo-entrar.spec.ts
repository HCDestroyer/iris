import path from "node:path";
import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { simularApiCampo } from "./api-campo";
import { CSRF } from "./api-falsa";
import { TECNICO } from "./datos-campo";

/*
 * Entrada del técnico en la PWA (diseño «Login C · Combinada», nov 2026), en teléfono y escritorio,
 * claro y oscuro: vive en /campo (no la del panel), «Iris Campo», usuario recordado con «¿No
 * eres tú?», el ojo de la contraseña, el error en línea (intentos solo si la API los informa),
 * «¿Olvidaste tu contraseña?», el código de 6 casillas (HeroUI InputOTP) sin «Recordar este
 * teléfono» ni código de respaldo, el aviso «Sin conexión», y sin huella ni «Entrar con Argos».
 * Con IRIS_CAPTURAS=<carpeta> deja las capturas a 390 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function captura(page: Page, que: string, proyecto: string) {
    if (proyecto.startsWith("telefono")) {
        await page.screenshot({ path: path.join(carpeta, `login-${que}-${proyecto}.png`) });
    }
}

type Respuesta = { status: number; cuerpo: unknown };
const problema = (status: number, codigo: string, extra: Record<string, unknown> = {}): Respuesta => ({
    status,
    cuerpo: { type: `urn:iris:problema:${codigo}`, title: codigo, status, ...extra },
});

/**
 * `POST /sesion` y `/sesion/mfa` falsos (la API falsa de campo no los tiene). Al abrir la sesión
 * completa pone las cookies, como la API: luego `GET /yo` responde con el técnico.
 */
async function simularEntrada(page: Page, context: BrowserContext, baseURL: string, o: { sesion: () => Respuesta; mfa?: () => Respuesta }) {
    const cuerpos: { ruta: string; cuerpo: unknown; canal: string | undefined }[] = [];
    const abrir = async () =>
        context.addCookies([
            { name: "iris_sesion", value: `s:${TECNICO.correo}`, url: baseURL, httpOnly: true, sameSite: "Lax" },
            { name: "iris_csrf", value: CSRF, url: baseURL, sameSite: "Lax" },
        ]);
    for (const [ruta, fn] of [
        ["/api/v1/sesion", o.sesion],
        ["/api/v1/sesion/mfa", o.mfa ?? o.sesion],
    ] as const) {
        await page.route(`**${ruta}`, async (route) => {
            const req = route.request();
            if (req.method() !== "POST") {
                return route.fallback();
            }
            cuerpos.push({ ruta, cuerpo: req.postDataJSON(), canal: (await req.allHeaders())["x-iris-canal"] });
            const r = fn();
            const cuerpo = r.cuerpo as Partial<Schema<"SesionIniciada">>;
            if (r.status === 200 && cuerpo.mfaRequerido === false) {
                await abrir();
            }
            return route.fulfill({ status: r.status, contentType: r.status === 200 ? "application/json" : "application/problem+json", body: JSON.stringify(r.cuerpo) });
        });
    }
    return cuerpos;
}

const iniciada = (mfaRequerido: boolean): Respuesta => ({
    status: 200,
    cuerpo: { csrfToken: CSRF, mfaRequerido, expiraEn: "2026-11-12T15:00:00Z", ...(mfaRequerido ? {} : { usuario: TECNICO }) } satisfies Schema<"SesionIniciada">,
});

const recordarTomas = (page: Page) =>
    page.addInitScript((u) => localStorage.setItem("iris-campo-ultimo-usuario", JSON.stringify(u)), { nombre: TECNICO.nombre, correo: TECNICO.correo });

test.beforeEach(async ({ context }) => {
    await context.clearCookies();
});

test("sin recordar: «Iris Campo» en /campo, correo y contraseña con su teclado, el ojo y sin huella ni Argos", async ({ page, context, baseURL }, info) => {
    await simularApiCampo(page);
    const cuerpos = await simularEntrada(page, context, baseURL ?? "", { sesion: () => iniciada(false) });
    await page.goto("/campo");
    await expect(page.getByRole("heading", { level: 1, name: "Iris Campo" })).toBeVisible();
    await expect(page.getByRole("img", { name: "Iris" })).toBeVisible();
    await expect(page.getByText("Entra con tu cuenta de técnico.")).toBeVisible();
    await expect(page.getByText("Hola, técnico")).toHaveCount(0);
    // Es la entrada de la app (bajo su service worker), no la del panel.
    await expect(page).toHaveURL(/\/campo$/);
    const correo = page.getByLabel("Correo");
    await expect(correo).toHaveAttribute("type", "email");
    await expect(correo).toHaveAttribute("autocomplete", "username");
    const contrasena = page.getByLabel("Contraseña", { exact: true });
    await expect(contrasena).toHaveAttribute("type", "password");
    await expect(contrasena).toHaveAttribute("autocomplete", "current-password");
    await page.getByRole("button", { name: "Mostrar contraseña" }).click();
    await expect(contrasena).toHaveAttribute("type", "text");
    await page.getByRole("button", { name: "Ocultar contraseña" }).click();
    await expect(contrasena).toHaveAttribute("type", "password");
    await expect(page.getByRole("button", { name: /huella/i })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Argos/ })).toHaveCount(0);
    // 48 px o más para lo que se toca.
    for (const nombre of ["Entrar", "¿Olvidaste tu contraseña?"]) {
        expect((await page.getByRole("button", { name: nombre, exact: true }).boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(48);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);
    await captura(page, "02-sin-recordar", info.project.name);

    await correo.fill(TECNICO.correo);
    await contrasena.fill("secreta");
    await page.getByRole("button", { name: "Entrar", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Hola, Tomás" })).toBeVisible();
    expect(cuerpos[0]).toEqual({ ruta: "/api/v1/sesion", cuerpo: { correo: TECNICO.correo, contrasena: "secreta" }, canal: "campo" });
    // La próxima vez lo recuerda: solo nombre y correo.
    expect(await page.evaluate(() => localStorage.getItem("iris-campo-ultimo-usuario"))).toBe(JSON.stringify({ nombre: TECNICO.nombre, correo: TECNICO.correo }));
});

test("usuario recordado: su fila y solo la contraseña; «¿No eres tú?» lo olvida y pide el correo", async ({ page, context, baseURL }, info) => {
    await recordarTomas(page);
    await simularApiCampo(page);
    await simularEntrada(page, context, baseURL ?? "", { sesion: () => iniciada(false) });
    await page.goto("/campo");
    const fila = page.locator('[data-slot="usuario-recordado"]');
    await expect(fila).toContainText("TH");
    await expect(fila).toContainText(TECNICO.nombre);
    await expect(fila).toContainText(TECNICO.correo);
    await expect(page.getByLabel("Correo")).toHaveCount(0);
    await expect(page.getByLabel("Contraseña", { exact: true })).toBeFocused();
    await captura(page, "01-usuario-recordado", info.project.name);
    await fila.getByRole("button", { name: "¿No eres tú?" }).click();
    await expect(page.getByLabel("Correo")).toHaveValue("");
    expect(await page.evaluate(() => localStorage.getItem("iris-campo-ultimo-usuario"))).toBeNull();
});

test("error en línea bajo la contraseña; los intentos que quedan solo si la API los informa", async ({ page, context, baseURL }, info) => {
    let intentos: number | null = null;
    await recordarTomas(page);
    await simularApiCampo(page);
    await simularEntrada(page, context, baseURL ?? "", {
        sesion: () => problema(401, "credenciales_invalidas", intentos === null ? {} : { intentosRestantes: intentos }),
    });
    await page.goto("/campo");
    const contrasena = page.getByLabel("Contraseña", { exact: true });
    await contrasena.fill("mala");
    await page.getByRole("button", { name: "Entrar", exact: true }).click();
    const error = page.locator('[data-slot="error"]');
    await expect(error).toHaveText("Correo o contraseña incorrectos.");
    await expect(contrasena).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByTestId("aviso-entrar")).toHaveCount(0);
    intentos = 4;
    await page.getByRole("button", { name: "Entrar", exact: true }).click();
    await expect(error).toHaveText("Correo o contraseña incorrectos. Te quedan 4 intentos.");
    await captura(page, "03-error", info.project.name);
});

test("código: 6 casillas, «Verificar» y volver, sin «Recordar este teléfono» ni código de respaldo", async ({ page, context, baseURL }, info) => {
    let codigoBueno = false;
    await recordarTomas(page);
    await simularApiCampo(page);
    const cuerpos = await simularEntrada(page, context, baseURL ?? "", {
        sesion: () => iniciada(true),
        mfa: () => (codigoBueno ? iniciada(false) : problema(401, "codigo_invalido")),
    });
    await page.goto("/campo");
    await page.getByLabel("Contraseña", { exact: true }).fill("secreta");
    await page.getByRole("button", { name: "Entrar", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Código de verificación" })).toBeVisible();
    await expect(page.getByText("Solo te lo pedimos porque tienes la verificación en dos pasos activa.")).toBeVisible();
    await expect(page.locator('[data-slot="input-otp-slot"]')).toHaveCount(6);
    await expect(page.getByText(/Recordar este teléfono/)).toHaveCount(0);
    await expect(page.getByText(/código de respaldo/i)).toHaveCount(0);
    const codigo = page.getByLabel("Escribe los 6 dígitos de tu app de autenticación");
    await expect(codigo).toHaveAttribute("autocomplete", "one-time-code");
    await codigo.pressSequentially("48");
    await captura(page, "04-codigo", info.project.name);
    await codigo.pressSequentially("1234");
    await page.getByRole("button", { name: "Verificar" }).click();
    await expect(page.getByText("Código incorrecto o ya usado. Revisa tu app de autenticación.")).toBeVisible();
    expect(cuerpos.at(-1)).toMatchObject({ ruta: "/api/v1/sesion/mfa", cuerpo: { codigo: "481234" } });
    // Volver a la contraseña y entrar con el código bueno.
    await page.getByRole("button", { name: "Volver" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Iris Campo" })).toBeVisible();
    await page.getByLabel("Contraseña", { exact: true }).fill("secreta");
    await page.getByRole("button", { name: "Entrar", exact: true }).click();
    codigoBueno = true;
    await page.getByLabel("Escribe los 6 dígitos de tu app de autenticación").pressSequentially("135790");
    await page.getByRole("button", { name: "Verificar" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Hola, Tomás" })).toBeVisible();
});

test("sin conexión y sin una sesión reciente: la entrada lo dice; «¿Olvidaste tu contraseña?» explica a quién pedirla", async ({ page }, info) => {
    await simularApiCampo(page);
    await page.route("**/api/v1/yo", (route) => route.abort("internetdisconnected"));
    await page.goto("/campo");
    await expect(page.getByRole("heading", { level: 1, name: "Iris Campo" })).toBeVisible();
    await expect(page.getByTestId("aviso-sin-conexion")).toContainText("Sin conexión.");
    await expect(page.getByTestId("aviso-sin-conexion")).toContainText("Conéctate para entrar");
    await captura(page, "05-sin-conexion", info.project.name);

    await page.getByRole("button", { name: "¿Olvidaste tu contraseña?" }).click();
    const hoja = page.getByRole("dialog", { name: "¿Olvidaste tu contraseña?" });
    await expect(hoja).toContainText("contraseña temporal");
    await hoja.getByRole("button", { name: "Cerrar" }).click();
    await expect(hoja).toHaveCount(0);
});
