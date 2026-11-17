import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, EMPRESAS, simularApi, SUPERVISOR_ZONA } from "./api-falsa";
import { LEGAL_COMPLETO, type OpcionesLegal } from "./api-legal";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";
import { CLAVES, IDIOMAS, RUTAS, RUTAS_PUBLICAS, TITULOS } from "../lib/legal/paginas";

/*
 * Páginas legales públicas y Administración › Datos legales (`.claude/integration/contratos/legal.md`).
 *
 * Las páginas públicas se piden de verdad al servidor de Next, sin sesión. En estas pruebas la API
 * no existe (IRIS_API_URL apunta a un puerto cerrado), así que además prueban el caso «sin datos»:
 * la página responde 200 con el texto completo y sin huecos. Con datos se prueba en Vitest
 * (componentes/legal/pagina-legal.test.tsx). En los 4 proyectos (escritorio y teléfono, claro y oscuro).
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-legal");
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: true });

test.describe("Páginas legales públicas", () => {
    test("un visitante anónimo (o un rastreador) recibe 200 con el texto en el HTML, sin JavaScript", async ({ request }) => {
        for (const clave of CLAVES) {
            for (const idioma of IDIOMAS) {
                const ruta = RUTAS[clave][idioma];
                // Sin cookies y sin seguir redirecciones: si pidiera sesión, sería un 307 a /entrar.
                const r = await request.get(ruta, { maxRedirects: 0, headers: { "User-Agent": "facebookexternalhit/1.1" } });
                expect(r.status(), ruta).toBe(200);
                const html = await r.text();
                expect(html, ruta).toContain(`data-pagina-legal="${clave}"`);
                expect(html, ruta).toContain(`<title>${TITULOS[clave][idioma]} · Iris</title>`);
                expect(html, ruta).toMatch(new RegExp(`<h1[^>]*>${TITULOS[clave][idioma]}</h1>`));
                // El cuerpo del documento está en el HTML que llega, no lo pinta el navegador después.
                expect(html, ruta).toContain(idioma === "es" ? "BAJA o ALTO" : "BAJA or ALTO");
                expect(html, ruta).toContain(idioma === "es" ? "Última actualización" : "Last updated");
                // Sin datos del titular: nombre genérico y ningún hueco.
                expect(html.toLowerCase(), ruta).toContain(idioma === "es" ? "el proveedor de este servicio" : "the provider of this service");
                const visible = html.replace(/<script[\s\S]*?<\/script>/g, "").replace(/<[^>]+>/g, " ");
                expect(visible, ruta).not.toMatch(/undefined|\bnull\b|NaN|\[object/);
                // Se puede indexar (el rastreador de Meta respeta estas marcas).
                expect(html, ruta).not.toMatch(/<meta name="robots" content="[^"]*noindex/);
            }
        }
    });

    test("robots.txt deja rastrear las páginas legales y nada más", async ({ request }) => {
        const r = await request.get("/robots.txt", { maxRedirects: 0 });
        expect(r.status()).toBe(200);
        const texto = await r.text();
        for (const ruta of RUTAS_PUBLICAS) {
            expect(texto).toContain(`Allow: ${ruta}`);
        }
        expect(texto).toContain("Disallow: /");
    });

    test("con JavaScript apagado se lee completa", async ({ browser }) => {
        const contexto = await browser.newContext({ javaScriptEnabled: false });
        const page = await contexto.newPage();
        await page.goto(RUTAS.privacidad.es);
        await expect(page.getByRole("heading", { level: 1, name: "Política de privacidad" })).toBeVisible();
        await expect(page.getByRole("heading", { level: 2, name: /Qué datos tratamos/ })).toBeVisible();
        await expect(page.getByText("responde BAJA o ALTO en el chat")).toBeVisible();
        await expect(page.getByRole("navigation", { name: "Contenido" }).getByRole("link")).toHaveCount(13);
        await contexto.close();
    });

    test("no pide sesión, enlaza entre idiomas y entre documentos, y no se desborda", async ({ page }) => {
        await page.goto(RUTAS.privacidad.es);
        await expect(page).toHaveURL(RUTAS.privacidad.es);
        await expect(page.getByRole("heading", { level: 1, name: "Política de privacidad" })).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "privacidad-es");

        // El índice lleva a su sección.
        await page.getByRole("navigation", { name: "Contenido" }).getByRole("link", { name: "Tus derechos" }).click();
        await expect(page).toHaveURL(`${RUTAS.privacidad.es}#derechos`);
        await expect(page.getByRole("heading", { level: 2, name: /Tus derechos/ })).toBeInViewport();

        // Al inglés y de vuelta.
        await page.getByRole("link", { name: "Read this page in English" }).click();
        await expect(page).toHaveURL(RUTAS.privacidad.en);
        await expect(page.getByRole("heading", { level: 1, name: "Privacy Policy" })).toBeVisible();
        await expect(page.locator("[data-pagina-legal]")).toHaveAttribute("lang", "en");
        await esperarSinDesbordes(page);
        await captura(page, "privacidad-en");

        // A los otros documentos, en el mismo idioma.
        const pie = page.getByRole("navigation", { name: "Legal documents" });
        await expect(pie.getByRole("link", { name: "Privacy Policy" })).toHaveAttribute("aria-current", "page");
        await pie.getByRole("link", { name: "Terms of Service" }).click();
        await expect(page).toHaveURL(RUTAS.terminos.en);
        await expect(page.getByRole("heading", { level: 1, name: "Terms of Service" })).toBeVisible();
        await esperarSinDesbordes(page);
        await page.getByRole("navigation", { name: "Legal documents" }).getByRole("link", { name: "Data Deletion" }).click();
        await expect(page).toHaveURL(RUTAS.eliminacion.en);
        await expect(page.getByRole("heading", { level: 2, name: /How to request deletion/ })).toBeVisible();
        await esperarSinDesbordes(page);
        await page.getByRole("link", { name: "Leer esta página en español" }).click();
        await expect(page).toHaveURL(RUTAS.eliminacion.es);
        await expect(page.getByRole("heading", { level: 1, name: "Eliminación de datos" })).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "eliminacion-es");
        await page.goto(RUTAS.terminos.es);
        await expect(page.getByRole("heading", { level: 2, name: /Ley aplicable/ })).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "terminos-es");
    });

    test("el inicio de sesión y la entrada del portal de soporte enlazan a las tres", async ({ page }) => {
        for (const entrada of ["/entrar", "/soporte/entrar"]) {
            await page.goto(entrada);
            const pie = page.getByRole("navigation", { name: "Documentos legales" });
            for (const clave of CLAVES) {
                await expect(pie.getByRole("link", { name: TITULOS[clave].es }), `${entrada} ${clave}`).toHaveAttribute("href", RUTAS[clave].es);
            }
            await esperarSinDesbordes(page);
        }
        await page.getByRole("navigation", { name: "Documentos legales" }).getByRole("link", { name: "Política de privacidad" }).click();
        await expect(page).toHaveURL(RUTAS.privacidad.es);
        await expect(page.getByRole("heading", { level: 1, name: "Política de privacidad" })).toBeVisible();
    });
});

const RUTA = "/administracion/datos-legales";
const UNA = [EMPRESAS[0] ?? { id: 10, nombre: "Cable Norte" }];

async function abrir(page: Page, legal: OpcionesLegal = {}, usuario = ADMIN) {
    const api = await simularApi(page, { legal, empresas: UNA });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", usuario.correo);
    await page.goto(RUTA);
    return api;
}

const formulario = (page: Page) => page.getByRole("form", { name: "Datos legales", exact: true });
const estado = (page: Page) => page.getByTestId("estado-legal");

test.describe("Administración › Datos legales", () => {
    test("sin datos avisa qué falta; al guardarlos, las páginas quedan listas", async ({ page }) => {
        const api = await abrir(page);
        const f = formulario(page);
        await expect(estado(page)).toHaveAttribute("data-completo", "false");
        await expect(estado(page)).toContainText("Faltan datos legales: las páginas públicas no están listas para Meta");
        await expect(estado(page).getByRole("list", { name: "Datos que faltan" }).getByRole("listitem")).toHaveText([
            "La razón social",
            "El nombre comercial",
            "El correo de privacidad",
            "La dirección",
            "El país",
        ]);
        await esperarSinDesbordes(page);
        await captura(page, "panel-sin-datos");

        // Validación espejo: un correo que no es uno solo no se manda.
        await f.getByRole("textbox", { name: "Correo de privacidad" }).fill("a@b.example, c@d.example");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Escribe un solo correo, como privacidad@tuempresa.com")).toBeVisible();
        expect(api.legal.guardados).toHaveLength(0);

        // A medias: se guarda y sigue diciendo qué falta.
        await f.getByRole("textbox", { name: "Correo de privacidad" }).fill(LEGAL_COMPLETO.correoPrivacidad);
        await f.getByRole("textbox", { name: "Nombre comercial" }).fill(LEGAL_COMPLETO.nombreComercial);
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Datos legales guardados")).toBeVisible();
        await expect(estado(page).getByRole("listitem")).toHaveText(["La razón social", "La dirección", "El país"]);
        expect(api.legal.guardados[0]?.cuerpo).toMatchObject({ version: 0, nombreComercial: "RedSur", correoPrivacidad: "privacidad@redsur.example", razonSocial: "" });
        expect(api.legal.guardados[0]?.idempotencia).toBeTruthy();

        await f.getByRole("textbox", { name: "Razón social" }).fill(`  ${LEGAL_COMPLETO.razonSocial}  `);
        await f.getByRole("textbox", { name: "Teléfono (opcional)" }).fill(LEGAL_COMPLETO.telefono);
        await f.getByRole("textbox", { name: "Dirección" }).fill(LEGAL_COMPLETO.direccion);
        await f.getByRole("textbox", { name: "País" }).fill(LEGAL_COMPLETO.pais);
        // Con Enter: en escritorio el aviso emergente del guardado anterior todavía tapa la barra de guardar.
        await f.getByRole("textbox", { name: "País" }).press("Enter");
        await expect(estado(page)).toHaveAttribute("data-completo", "true");
        await expect(estado(page)).toContainText("Datos completos: las páginas públicas están listas para Meta");
        expect(api.legal.guardados[1]?.cuerpo).toEqual({ version: 1, ...LEGAL_COMPLETO });
        expect(api.legal.guardados[1]?.idempotencia).not.toBe(api.legal.guardados[0]?.idempotencia);
        // Con sondeo: el aviso emergente todavía se está acomodando.
        await expect.poll(() => buscarDesbordes(page)).toEqual([]);
        await captura(page, "panel-completo");
    });

    test("los seis enlaces para Meta: con el dominio del panel, «Abrir», «Copiar» y si cada uno responde", async ({ page, context }) => {
        await context.grantPermissions(["clipboard-read", "clipboard-write"]);
        await abrir(page, { guardados: LEGAL_COMPLETO });
        const origen = new URL(page.url()).origin;
        const enlaces = page.locator('[data-slot="enlaces-meta"]');
        await expect(enlaces.getByRole("heading", { name: "Enlaces para Meta" })).toBeVisible();
        await expect(enlaces).toContainText("Configuración de la app › Básica");
        await expect(enlaces.locator("[data-valor-copiable]")).toHaveText(RUTAS_PUBLICAS.map((r) => `${origen}${r}`));
        // Dónde se pega cada uno.
        await expect(enlaces.locator('[data-pagina="privacidad"]')).toContainText("URL de la Política de privacidad");
        await expect(enlaces.locator('[data-pagina="terminos"]')).toContainText("URL de las Condiciones del servicio");
        await expect(enlaces.locator('[data-pagina="eliminacion"]')).toContainText("URL de instrucciones para la eliminación de datos");
        // Las pruebas corren sin https: Meta no aceptaría estos enlaces y la pantalla lo dice.
        await expect(page.getByTestId("legal-sin-https")).toBeVisible();

        // Cada página se pide de verdad (sin sesión) y responde.
        for (const ruta of RUTAS_PUBLICAS) {
            await expect(enlaces.locator(`[data-ruta="${ruta}"]`), ruta).toHaveAttribute("data-estado-enlace", "ok");
            await expect(enlaces.locator(`[data-ruta="${ruta}"]`)).toHaveText("Responde bien");
        }
        const abrirTerminos = enlaces.getByRole("link", { name: "Abrir Terms of Service" });
        await expect(abrirTerminos).toHaveAttribute("href", RUTAS.terminos.en);
        await expect(abrirTerminos).toHaveAttribute("target", "_blank");

        await enlaces.getByRole("button", { name: "Copiar inglés (el que se pega en meta)" }).first().click();
        await expect(page.getByText("Enlace copiado")).toBeVisible();
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${origen}${RUTAS.privacidad.en}`);
        await esperarSinDesbordes(page);
        await captura(page, "panel-enlaces");
    });

    test("una página que no responde se marca, y «Volver a comprobar» la revisa otra vez", async ({ page }) => {
        let caida = true;
        await page.route(`**${RUTAS.eliminacion.en}`, (route) => (caida ? route.fulfill({ status: 502, body: "caída" }) : route.fallback()));
        // Una redirección al inicio de sesión tampoco es «responde bien».
        await page.route(`**${RUTAS.terminos.es}`, (route) => (caida ? route.fulfill({ status: 200, contentType: "text/html", body: "<h1>Iniciar sesión</h1>" }) : route.fallback()));
        await abrir(page);
        const enlaces = page.locator('[data-slot="enlaces-meta"]');
        await expect(enlaces.locator(`[data-ruta="${RUTAS.eliminacion.en}"]`)).toHaveText("No responde");
        await expect(enlaces.locator(`[data-ruta="${RUTAS.terminos.es}"]`)).toHaveText("No responde");
        // Responde, pero como faltan datos no se da por lista.
        await expect(enlaces.locator(`[data-ruta="${RUTAS.privacidad.es}"]`)).toHaveText("Responde, pero faltan datos");
        await expect(enlaces).toContainText("avisa a quien administra el servidor de Iris");
        await esperarSinDesbordes(page);
        caida = false;
        await enlaces.getByRole("button", { name: "Volver a comprobar" }).click();
        await expect(enlaces.locator(`[data-ruta="${RUTAS.eliminacion.en}"]`)).toHaveText("Responde, pero faltan datos");
        await expect(enlaces.locator(`[data-ruta="${RUTAS.terminos.es}"]`)).toHaveText("Responde, pero faltan datos");
    });

    test("409 con «Recargar», 422 junto al campo y lectura fallida con «Reintentar»", async ({ page }) => {
        const api = await abrir(page, { guardados: LEGAL_COMPLETO, conflicto: true, rechaza: { field: "telefono", message: "Solo dígitos, espacios, +, guiones y paréntesis" } });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "País" }).fill("Honduras");
        await page.getByRole("button", { name: "Guardar" }).click();
        const aviso = page.getByTestId("error-legal");
        await expect(aviso).toContainText("Alguien más cambió los datos");
        await esperarSinDesbordes(page);
        await aviso.getByRole("button", { name: "Recargar" }).click();
        await expect(f.getByRole("textbox", { name: "País" })).toHaveValue("Guatemala");
        await expect(aviso).toHaveCount(0);

        await f.getByRole("textbox", { name: "País" }).fill("Honduras");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Solo dígitos, espacios, +, guiones y paréntesis")).toBeVisible();
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Datos legales guardados")).toBeVisible();
        expect(api.legal.ver().pais).toBe("Honduras");

        const otra = await page.context().newPage();
        await simularApi(otra, { legal: { fallaLeer: true }, empresas: UNA });
        await otra.goto(RUTA);
        await expect(otra.getByRole("button", { name: "Reintentar" })).toBeVisible();
        // Sin lectura no hay formulario ni un «faltan datos» inventado.
        await expect(formulario(otra)).toHaveCount(0);
        await expect(estado(otra)).toHaveCount(0);
        await otra.getByRole("button", { name: "Reintentar" }).click();
        await expect(formulario(otra)).toBeVisible();
    });

    test("con solo «ver» no se puede cambiar; sin operar en todas las empresas no se ven", async ({ page }) => {
        const api = await abrir(page, { guardados: LEGAL_COMPLETO }, CONSULTA);
        const f = formulario(page);
        await expect(f.getByRole("textbox", { name: "Razón social" })).toBeDisabled();
        await expect(f.getByRole("textbox", { name: "Razón social" })).toHaveValue(LEGAL_COMPLETO.razonSocial);
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await expect(page.getByText("Puedes verlos, pero no tienes permiso para cambiarlos.")).toBeVisible();
        // Los enlaces sí los puede copiar.
        await expect(page.locator('[data-slot="enlaces-meta"] [data-valor-copiable]')).toHaveCount(6);
        expect(api.legal.guardados).toHaveLength(0);

        const otra = await page.context().newPage();
        await simularApi(otra, { legal: { guardados: LEGAL_COMPLETO }, empresas: UNA });
        await conSesion(otra.context(), test.info().project.use.baseURL ?? "", SUPERVISOR_ZONA.correo);
        await otra.goto(RUTA);
        await expect(otra.getByTestId("legal-sin-alcance")).toContainText("los configura quien opera en todas las empresas");
        await expect(formulario(otra)).toHaveCount(0);
        await esperarSinDesbordes(otra);
    });

    test("está en el índice de Administración", async ({ page }) => {
        const api = await simularApi(page, { legal: {}, empresas: UNA });
        void api;
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion");
        await page.getByRole("list", { name: "Secciones de administración" }).getByRole("link", { name: /Datos legales/ }).click();
        await expect(page).toHaveURL(RUTA);
        await expect(page.getByRole("heading", { level: 1, name: "Datos legales" })).toBeVisible();
    });
});
