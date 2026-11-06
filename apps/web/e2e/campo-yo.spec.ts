import path from "node:path";
import type { Schema } from "@iris/contratos";
import { expect, test, type Page, type Route } from "@playwright/test";
import { conSesionTecnico, problema, simularApiCampo } from "./api-campo";
import { avisoCampo } from "./ayudas-campo";
import { PERMISOS_TECNICO, TECNICO } from "./datos-campo";

/*
 * «Yo» de la PWA de campo (diseño aprobado por el PM, nov 2026) en 390 y 1280 px, claro y oscuro:
 * encabezado «Rol · Empresa», Apariencia (Teléfono · Claro · Oscuro, guardada y sin parpadeo al
 * cargar), «Cambiar contraseña» (ayudas en verde, 422 por campo, 429 que lleva a entrar, sin señal,
 * salir con cambios pregunta) y la verificación en dos pasos completa. Capturas en IRIS_CAPTURAS.
 */
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const captura = (page: Page, que: string, proyecto: string) => page.screenshot({ path: path.join(carpeta, `campo-yo-${que}-${proyecto}.png`), fullPage: true });

const EMPRESA: Schema<"EmpresaResumen"> = { id: 7, nombre: "Fibra GT" };
const SECRETO = "JBSWY3DPEHPK3PXP";
const URL_OTP = `otpauth://totp/Iris:${TECNICO.correo}?algorithm=SHA1&digits=6&issuer=Iris&period=30&secret=${SECRETO}`;

type Cuenta = {
    mfa: boolean;
    contrasena: (cuerpo: Schema<"EntradaCambiarContrasena">) => { status: number; cuerpo: unknown; cabeceras?: Record<string, string> };
    clavesMfa: string[];
    codigos: string[];
    clavesContrasena: string[];
};

/** Las rutas de la cuenta propia sobre la API falsa de campo (el resto sigue en `simularApiCampo`). */
async function simularCuenta(page: Page, inicial: Partial<Cuenta> = {}): Promise<Cuenta> {
    const cuenta: Cuenta = { mfa: false, contrasena: () => ({ status: 200, cuerpo: TECNICO }), clavesMfa: [], codigos: [], clavesContrasena: [], ...inicial };
    const responder = (route: Route, status: number, cuerpo: unknown, cabeceras: Record<string, string> = {}) =>
        route.fulfill({ status, contentType: status >= 400 ? "application/problem+json" : "application/json", headers: cabeceras, body: JSON.stringify(cuerpo) });
    await page.route("**/api/v1/yo**", async (route) => {
        const req = route.request();
        const ruta = new URL(req.url()).pathname;
        const clave = (await req.allHeaders())["idempotency-key"] ?? "";
        if (ruta === "/api/v1/yo" && req.method() === "GET") {
            if (!cuenta.mfa) {
                return route.fallback();
            }
            const yo: Schema<"Yo"> = {
                actor: "usuario",
                usuario: { ...TECNICO, mfaActivo: true },
                permisos: PERMISOS_TECNICO,
                empresas: [EMPRESA],
                empresaActiva: EMPRESA,
                modoEmpresa: "activa",
                todasLasEmpresas: false,
                hayDatosSinEmpresa: false,
            };
            return responder(route, 200, yo);
        }
        if (ruta === "/api/v1/yo/contrasena" && req.method() === "POST") {
            cuenta.clavesContrasena.push(clave);
            const r = cuenta.contrasena(req.postDataJSON() as Schema<"EntradaCambiarContrasena">);
            return responder(route, r.status, r.cuerpo, r.cabeceras);
        }
        if (ruta === "/api/v1/yo/mfa" && req.method() === "POST") {
            cuenta.clavesMfa.push(clave);
            return responder(route, 200, { secreto: SECRETO, url: URL_OTP } satisfies Schema<"MFAConfigurado">);
        }
        if (ruta === "/api/v1/yo/mfa/activar" && req.method() === "POST") {
            const { codigo } = req.postDataJSON() as Schema<"EntradaActivarMFA">;
            cuenta.codigos.push(codigo);
            if (codigo !== "482913") {
                return responder(route, 422, { ...problema("codigo_invalido", 422, "Regla de negocio"), detail: "El código no coincide; revisa la hora del teléfono" });
            }
            cuenta.mfa = true;
            return responder(route, 200, { ...TECNICO, mfaActivo: true });
        }
        return route.fallback();
    });
    return cuenta;
}

async function abrirYo(page: Page, context: Parameters<typeof conSesionTecnico>[0], baseURL: string | undefined, inicial: Partial<Cuenta> = {}) {
    await conSesionTecnico(context, baseURL ?? "");
    const api = await simularApiCampo(page, { empresas: [EMPRESA] });
    const cuenta = await simularCuenta(page, inicial);
    await page.goto("/campo?vista=yo");
    await expect(page.getByRole("heading", { level: 1, name: TECNICO.nombre })).toBeVisible();
    return { api, cuenta };
}

const esOscuro = (page: Page) => page.evaluate(() => document.documentElement.classList.contains("dark"));

test("«Yo»: «Rol · Empresa», secciones, dos pasos «Apagada» y «Cerrar sesión» de contorno", async ({ page, context, baseURL }, info) => {
    await abrirYo(page, context, baseURL);
    await expect(page.getByTestId("perfil-yo")).toContainText("Técnico · Fibra GT");
    for (const t of ["Apariencia", "Seguridad", "Este teléfono"]) {
        await expect(page.getByRole("heading", { level: 2, name: t })).toBeVisible();
    }
    await expect(page.getByRole("button", { name: "Verificación en dos pasos: Apagada" })).toBeVisible();
    await expect(page.getByText(/Cambiada hace/)).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Por enviar" })).toContainText("Todo enviado");
    await expect(page.getByRole("button", { name: "Cerrar sesión" })).toBeVisible();
    // Sin desbordes a 390 px.
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await captura(page, "01-yo", info.project.name);
    // Lo de abajo («Cerrar sesión») queda por encima de la isla al llegar al final.
    await page.evaluate(() => window.scrollTo({ top: document.body.scrollHeight }));
    await page.screenshot({ path: path.join(carpeta, `campo-yo-01b-yo-abajo-${info.project.name}.png`) });
});

test("Apariencia: los tres modos, guardada en el navegador y sin parpadeo al recargar", async ({ page, context, baseURL }, info) => {
    await abrirYo(page, context, baseURL);
    const sistemaOscuro = info.project.name.endsWith("oscuro");
    const grupo = page.getByRole("radiogroup", { name: "Apariencia" });
    await expect(grupo.getByRole("radio")).toHaveText(["Teléfono", "Claro", "Oscuro"]);
    await expect(grupo.getByRole("radio", { name: "Teléfono" })).toBeChecked();
    expect(await esOscuro(page)).toBe(sistemaOscuro);

    const otro = sistemaOscuro ? "Claro" : "Oscuro";
    await grupo.getByRole("radio", { name: otro }).click();
    await expect.poll(() => esOscuro(page)).toBe(!sistemaOscuro);
    expect(await page.evaluate(() => localStorage.getItem("iris-tema"))).toBe(sistemaOscuro ? "light" : "dark");
    await expect(avisoCampo(page)).toContainText(sistemaOscuro ? "Iris en modo claro." : "Iris en modo oscuro.");

    // Al recargar, la clase está antes de pintar (script de next-themes en el <head>).
    await page.reload({ waitUntil: "commit" });
    await page.waitForLoadState("domcontentloaded");
    expect(await esOscuro(page)).toBe(!sistemaOscuro);
    await expect(page.getByRole("radiogroup", { name: "Apariencia" }).getByRole("radio", { name: otro })).toBeChecked();

    await page.getByRole("radiogroup", { name: "Apariencia" }).getByRole("radio", { name: "Teléfono" }).click();
    await expect.poll(() => esOscuro(page)).toBe(sistemaOscuro);
    expect(await page.evaluate(() => localStorage.getItem("iris-tema"))).toBe("system");
});

test("cambiar contraseña: pantalla propia, ayudas en verde, 422 por campo y aviso al guardar", async ({ page, context, baseURL }, info) => {
    let intento = 0;
    const { api, cuenta } = await abrirYo(page, context, baseURL, {
        contrasena: () => {
            intento += 1;
            return intento === 1
                ? { status: 422, cuerpo: problema("validacion", 422, "Datos inválidos", [{ field: "actual", message: "La contraseña actual no es correcta" }]) }
                : { status: 200, cuerpo: TECNICO };
        },
    });
    await page.getByRole("button", { name: "Cambiar contraseña" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Cambiar contraseña" })).toBeVisible();
    await expect(page).toHaveURL(/vista=yo&seccion=contrasena/);
    await expect(page.getByText("Se cierran tus sesiones en otros equipos. En este teléfono sigues dentro.")).toBeVisible();

    await page.getByRole("button", { name: "Guardar contraseña" }).click();
    await expect(page.getByText("Escribe tu contraseña actual")).toBeVisible();
    await expect(page.getByText("Usa 12 caracteres o más")).toBeVisible();
    expect(cuenta.clavesContrasena).toHaveLength(0);

    await page.getByLabel("Contraseña actual", { exact: true }).fill("vieja-de-12-caracteres");
    await page.getByLabel("Nueva contraseña", { exact: true }).fill("vieja-de-12-caracteres");
    await expect(page.getByText("Debe ser distinta de la actual")).toBeVisible();
    await page.getByLabel("Nueva contraseña", { exact: true }).fill("una frase bastante larga");
    await expect(page.getByTestId("ayuda-largo")).toHaveText("12 caracteres o más");
    await page.getByLabel("Repite la nueva", { exact: true }).fill("una frase bastante larga");
    await expect(page.getByTestId("ayuda-coinciden")).toHaveText("Coinciden");
    await page.getByRole("button", { name: "Mostrar contraseña" }).first().click();
    await expect(page.getByLabel("Contraseña actual", { exact: true })).toHaveAttribute("type", "text");
    await captura(page, "02-contrasena", info.project.name);

    await page.getByRole("button", { name: "Guardar contraseña" }).click();
    await expect(page.getByText("La contraseña actual no es correcta")).toBeVisible();
    await expect(page.getByLabel("Contraseña actual", { exact: true })).toHaveValue("");
    await page.getByLabel("Contraseña actual", { exact: true }).fill("la-buena-de-verdad");
    await page.getByRole("button", { name: "Guardar contraseña" }).click();
    await expect(avisoCampo(page)).toContainText("Contraseña cambiada");
    await expect(page.getByRole("radiogroup", { name: "Apariencia" })).toBeVisible();
    // Una clave por intento con respuesta; canal campo y CSRF.
    expect(new Set(cuenta.clavesContrasena).size).toBe(2);
    const post = api.peticiones.filter((p) => p.ruta === "/api/v1/yo/contrasena");
    expect(post.every((p) => p.canal === "campo" && p.csrf !== undefined)).toBe(true);
});

test("salir con cambios pregunta (botón y «Atrás» del navegador)", async ({ page, context, baseURL }) => {
    await abrirYo(page, context, baseURL);
    await page.getByRole("button", { name: "Cambiar contraseña" }).click();
    await page.getByRole("button", { name: "Atrás" }).click();
    await expect(page.getByRole("radiogroup", { name: "Apariencia" })).toBeVisible();

    await page.getByRole("button", { name: "Cambiar contraseña" }).click();
    await page.getByLabel("Contraseña actual", { exact: true }).fill("algo");
    await page.getByRole("button", { name: "Atrás" }).click();
    const dialogo = page.getByRole("alertdialog", { name: "¿Salir sin guardar?" });
    await expect(dialogo).toBeVisible();
    await dialogo.getByRole("button", { name: "Seguir editando" }).click();
    await expect(page.getByLabel("Contraseña actual", { exact: true })).toHaveValue("algo");

    await page.goBack();
    await expect(dialogo).toBeVisible();
    await dialogo.getByRole("button", { name: "Salir sin guardar" }).click();
    await expect(page.getByRole("radiogroup", { name: "Apariencia" })).toBeVisible();
});

test("429 `demasiados_intentos`: explica la espera y lleva a entrar", async ({ page, context, baseURL }) => {
    await abrirYo(page, context, baseURL, {
        contrasena: () => ({ status: 429, cuerpo: problema("demasiados_intentos", 429, "Demasiados intentos"), cabeceras: { "Retry-After": "900" } }),
    });
    await page.getByRole("button", { name: "Cambiar contraseña" }).click();
    await page.getByLabel("Contraseña actual", { exact: true }).fill("equivocada");
    await page.getByLabel("Nueva contraseña", { exact: true }).fill("una frase bastante larga");
    await page.getByLabel("Repite la nueva", { exact: true }).fill("una frase bastante larga");
    await page.getByRole("button", { name: "Guardar contraseña" }).click();
    const aviso = page.getByTestId("aviso-sesion-cerrada");
    await expect(aviso).toContainText("Demasiados intentos: vuelve a entrar en 15 min");
    await expect(page.getByRole("button", { name: "Guardar contraseña" })).toHaveCount(0);
    await aviso.getByRole("button", { name: "Ir a iniciar sesión" }).click();
    await expect(page.getByRole("heading", { level: 1, name: /Hola/ })).toBeVisible();
});

test("sin señal: lo dice y no intenta (no va a la cola)", async ({ page, context, baseURL }) => {
    const { cuenta, api } = await abrirYo(page, context, baseURL);
    await page.getByRole("button", { name: "Cambiar contraseña" }).click();
    api.sinRed = true;
    await context.setOffline(true);
    await expect(page.getByTestId("aviso-sin-senal")).toContainText("necesita señal");
    await expect(page.getByRole("button", { name: "Guardar contraseña" })).toBeDisabled();
    expect(cuenta.clavesContrasena).toHaveLength(0);
    await context.setOffline(false);
    api.sinRed = false;
});

test("dos pasos: flujo completo con «Abrir app», «Copiar clave», código y activar", async ({ page, context, baseURL }, info) => {
    const { cuenta } = await abrirYo(page, context, baseURL);
    await page.getByRole("button", { name: "Verificación en dos pasos: Apagada" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Verificación en dos pasos" })).toBeVisible();
    await expect(page.getByTestId("clave-dos-pasos")).toHaveText("JBSW Y3DP EHPK 3PXP");
    await expect(page.getByRole("link", { name: "Abrir app" })).toHaveAttribute("href", URL_OTP);
    await expect(page.getByRole("button", { name: "Copiar clave" })).toBeVisible();
    const codigo = page.getByLabel("Escribe el código de 6 dígitos");
    await expect(codigo).toHaveAttribute("autocomplete", "one-time-code");
    await codigo.pressSequentially("48291");
    await captura(page, "03-dos-pasos", info.project.name);
    // Solo un secreto: ningún render volvió a pedirlo.
    expect(cuenta.clavesMfa).toHaveLength(1);

    await codigo.pressSequentially("0");
    await page.getByRole("button", { name: "Activar" }).click();
    await expect(page.getByText("El código no coincide; revisa la hora del teléfono")).toBeVisible();
    await page.getByLabel("Escribe el código de 6 dígitos").pressSequentially("482913");
    await page.getByRole("button", { name: "Activar" }).click();
    await expect(avisoCampo(page)).toContainText("Verificación en dos pasos activada");
    await expect(page.getByRole("button", { name: "Verificación en dos pasos: Activa" })).toBeVisible();
    expect(cuenta.codigos).toEqual(["482910", "482913"]);
    expect(cuenta.clavesMfa).toHaveLength(1);

    await page.getByRole("button", { name: "Verificación en dos pasos: Activa" }).click();
    await expect(page.getByTestId("dos-pasos-activa")).toBeVisible();
    await expect(page.getByText("Para cambiar de teléfono, pide a un administrador que la reinicie.")).toBeVisible();
    await expect(page.getByRole("button", { name: /Desactivar/ })).toHaveCount(0);
    await captura(page, "04-dos-pasos-activa", info.project.name);
});
