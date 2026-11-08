import path from "node:path";
import type { Schema } from "@iris/contratos";
import { expect as expectBase, test, type Page } from "@playwright/test";
import { MANUALES_INICIO } from "../lib/manuales/inicio";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { esTelefono } from "./ayudas";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

// Con 4 workers el motor del chat (flujo, cola y verificación de la sesión) y las lecturas en serie
// tardan más que los 5 s por defecto: 10 s por espera.
const expect = expectBase.configure({ timeout: 10_000 });

/*
 * «Cargar manuales de inicio» en la biblioteca (`manuales.editar`): ejemplos para un ISP con fibra
 * (FTTH) e inalámbrico (WISP). Se crean uno por llamada con su `Idempotency-Key` estable y no se
 * duplican al repetir. En los 4 proyectos (escritorio y teléfono, claro y oscuro).
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function abrir(page: Page, ruta = "/administracion/manuales") {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

const dialogoCarga = (page: Page) => page.getByRole("dialog", { name: "Cargar manuales de inicio" });
const titulo = (codigo: string) => MANUALES_INICIO.find((m) => m.codigo === codigo)?.titulo ?? codigo;
const vecesPorTitulo = (manuales: Schema<"Manual">[], t: string) => manuales.filter((m) => m.titulo === t).length;

test.describe("manuales de inicio", () => {
    test("se cargan con su avance, quedan «Del equipo» por completar y no se duplican al repetir", async ({ page }, info) => {
        const api = await abrir(page);
        const manuales = api.conversaciones.manuales;
        const antes = manuales.length;
        const claves: string[] = [];
        page.on("request", (r) => {
            if (r.method() === "POST" && new URL(r.url()).pathname === "/api/v1/mensajeria/manuales") claves.push(r.headers()["idempotency-key"] ?? "");
        });
        await expect(page.locator("[data-manual]")).toHaveCount(antes);

        // Primera vez: todos marcados; se quitan dos.
        await page.getByRole("button", { name: "Cargar manuales de inicio" }).click();
        const dialogo = dialogoCarga(page);
        await expect(dialogo.getByText(/^Ejemplos editables para un ISP/)).toBeVisible();
        const casillas = dialogo.getByRole("checkbox");
        await expect(casillas).toHaveCount(MANUALES_INICIO.length);
        for (const c of await casillas.all()) await expect(c).toBeChecked();
        await expect(dialogo.locator('[data-manual-inicio="ftth-luces-ont"]')).toContainText("Fibra (FTTH)");
        await expect(dialogo.locator('[data-manual-inicio="wisp-senal-baja"]')).toContainText("Inalámbrico (WISP)");
        await esperarSinDesbordes(page);
        await page.screenshot({
            path: path.join(carpeta, `manuales-inicio-dialogo-${info.project.name}.png`),
        });
        await dialogo.getByRole("checkbox", { name: titulo("traslado-de-domicilio") }).click();
        await dialogo.getByRole("checkbox", { name: titulo("seguridad-y-estafas") }).click();
        await expect(dialogo.getByText(`${MANUALES_INICIO.length - 2} de ${MANUALES_INICIO.length} elegidos`)).toBeVisible();
        await dialogo
            .getByRole("button", {
                name: `Cargar ${MANUALES_INICIO.length - 2} manuales`,
            })
            .click();
        await expect(page.getByText(`${MANUALES_INICIO.length - 2} manuales de inicio creados`)).toBeVisible();
        await expect(dialogo).toBeHidden();
        expect(manuales.length).toBe(antes + MANUALES_INICIO.length - 2);
        // La lista se vuelve a leer: «Del equipo» y con datos de la empresa por completar.
        await expect(page.locator("[data-manual]")).toHaveCount(antes + MANUALES_INICIO.length - 2);
        const luces = page.locator("[data-manual]").filter({ hasText: titulo("ftth-luces-ont") });
        await expect(luces).toContainText("Del equipo");
        await expect(luces).toContainText("Falta completar datos de tu empresa");
        await expect(luces).toContainText("ftth");
        // Los de antes, sin corchetes, no se marcan.
        await expect(page.locator('[data-manual="2"]')).not.toContainText("Falta completar");
        const nuevo = manuales.find((m) => m.titulo === titulo("ftth-luces-ont"));
        expect(nuevo?.visibilidad).toBe("interna");
        expect(nuevo?.etiquetas?.[0]).toBe("ftth");

        // Segunda vez: los cargados vienen deshabilitados («Ya está en la biblioteca») y solo quedan
        // los dos de antes. Mientras tanto alguien crea uno a mano con el mismo título: se omite.
        await page.getByRole("button", { name: "Cargar manuales de inicio" }).click();
        await expect(dialogo.getByRole("checkbox", { name: titulo("ftth-luces-ont") })).toBeDisabled();
        await expect(dialogo.locator('[data-manual-inicio="ftth-luces-ont"]')).toContainText("Ya está en la biblioteca");
        await expect(
            dialogo.getByRole("checkbox", {
                name: titulo("traslado-de-domicilio"),
            }),
        ).toBeChecked();
        await expect(dialogo.getByText("2 de 2 elegidos")).toBeVisible();
        manuales.push({
            ...(nuevo as Schema<"Manual">),
            id: 9_000,
            titulo: titulo("traslado-de-domicilio"),
            version: 1,
        });
        await dialogo.getByRole("button", { name: "Cargar 2 manuales" }).click();
        await expect(page.getByText("1 manual de inicio creado")).toBeVisible();
        await expect(page.getByText(`Se omitió 1 que ya estaba en la biblioteca: «${titulo("traslado-de-domicilio")}».`)).toBeVisible();

        // Tercera vez: no queda nada por cargar.
        await page.getByRole("button", { name: "Cargar manuales de inicio" }).click();
        await expect(dialogo.getByText("Ya están todos en la biblioteca")).toBeVisible();
        await expect(dialogo.getByRole("button", { name: /^Cargar \d/ })).toHaveCount(0);
        await page.keyboard.press("Escape");
        await expect(dialogo).toBeHidden();

        // Uno por título, y una clave estable por plantilla y empresa.
        for (const m of MANUALES_INICIO) expect(vecesPorTitulo(manuales, m.titulo), m.titulo).toBe(1);
        expect(claves).toHaveLength(MANUALES_INICIO.length - 1);
        expect(new Set(claves).size).toBe(claves.length);
        for (const c of claves) expect(c).toMatch(/^manual-inicio:\d+:[a-z0-9-]+:r\d+$/);
        await esperarSinDesbordes(page);
    });

    test("si una llamada falla, «Reintentar» sigue donde quedó sin repetir los creados", async ({ page }) => {
        const api = await abrir(page);
        let fallar = true;
        await page.route("**/api/v1/mensajeria/manuales", async (r) => {
            const cuerpo = r.request().method() === "POST" ? (r.request().postDataJSON() as { titulo?: string }) : {};
            if (fallar && cuerpo.titulo === titulo("ftth-luces-ont")) {
                fallar = false;
                await r.fulfill({
                    status: 503,
                    contentType: "application/problem+json",
                    body: JSON.stringify({
                        type: "about:blank",
                        title: "Servicio no disponible",
                        status: 503,
                    }),
                });
                return;
            }
            await r.fallback();
        });
        await page.getByRole("button", { name: "Cargar manuales de inicio" }).click();
        const dialogo = dialogoCarga(page);
        await dialogo
            .getByRole("button", {
                name: `Cargar ${MANUALES_INICIO.length} manuales`,
            })
            .click();
        await expect(dialogo.getByText("No se pudieron cargar todos")).toBeVisible();
        await expect(dialogo.getByText(`Se detuvo en 3 de ${MANUALES_INICIO.length}.`)).toBeVisible();
        // Con el error a la vista la selección queda congelada: «Reintentar» sigue con la misma.
        await expect(dialogo.getByRole("checkbox", { name: titulo("seguridad-y-estafas") })).toBeDisabled();
        await expect(dialogo.getByRole("button", { name: "Quitar todos" })).toBeDisabled();
        await dialogo.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText(`${MANUALES_INICIO.length} manuales de inicio creados`)).toBeVisible();
        for (const m of MANUALES_INICIO) expect(vecesPorTitulo(api.conversaciones.manuales, m.titulo), m.titulo).toBe(1);
    });

    test("capturas: biblioteca con los manuales y dos manuales abiertos", async ({ page }, info) => {
        test.setTimeout(90_000);
        await abrir(page);
        await page.getByRole("button", { name: "Cargar manuales de inicio" }).click();
        await dialogoCarga(page)
            .getByRole("button", {
                name: `Cargar ${MANUALES_INICIO.length} manuales`,
            })
            .click();
        await expect(page.getByText(`${MANUALES_INICIO.length} manuales de inicio creados`)).toBeVisible();
        await page.getByRole("radio", { name: "Del equipo" }).click();
        await expect(page.locator("[data-manual]").filter({ hasText: titulo("wisp-senal-baja") })).toBeVisible();
        await esperarSinDesbordes(page);
        await page.screenshot({
            path: path.join(carpeta, `manuales-inicio-biblioteca-${info.project.name}.png`),
            fullPage: !esTelefono(page),
        });
        for (const codigo of ["ftth-luces-ont", "wisp-antena-poe"]) {
            await page
                .locator("[data-manual]")
                .filter({ hasText: titulo(codigo) })
                .getByRole("button", { name: "Ver" })
                .click();
            const ver = page.getByRole("dialog", { name: titulo(codigo) });
            // El cuerpo se lee al abrir, a la par que la biblioteca revisa los corchetes de cada manual.
            await expect(ver.getByRole("heading", { name: "Cuándo llamar a soporte" })).toBeVisible({ timeout: 20_000 });
            // La hoja entra animada: se mide cuando ya llegó.
            await page.evaluate(() =>
                Promise.all(
                    document
                        .getAnimations()
                        .filter((a) => a.effect?.getComputedTiming().iterations !== Number.POSITIVE_INFINITY)
                        .map((a) => a.finished.catch(() => undefined)),
                ),
            );
            await esperarSinDesbordes(page);
            await page.screenshot({
                path: path.join(carpeta, `manuales-inicio-${codigo}-${info.project.name}.png`),
            });
            await page.keyboard.press("Escape");
            await expect(ver).toBeHidden();
        }
    });

    test("la bandeja no envía al cliente un manual con datos por completar", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const manual = api.conversaciones.manuales.find((m) => m.id === 2);
        if (!manual) throw new Error("sin manual");
        manual.cuerpo = "Llame al [número de soporte] si sigue sin servicio.";
        if (esTelefono(page)) await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
        else await page.getByRole("button", { name: "Manuales" }).click();
        const ficha = esTelefono(page) ? page.getByRole("dialog", { name: "Ficha del cliente" }) : page.getByRole("complementary", { name: "Ficha del cliente" });
        if (esTelefono(page)) await ficha.getByRole("button", { name: "Ayuda" }).click();
        const articulo = ficha.getByRole("region", { name: "Sugeridos para esta conversación" }).locator('[data-manual="2"]');
        await articulo.getByRole("button", { name: "Enviar al cliente" }).click();
        await expect(articulo.getByRole("alert")).toHaveText("Falta completar datos de tu empresa: [número de soporte]. Pide a un administrador que complete el manual antes de enviarlo.");
        expect(api.conversaciones.enviados.some((e) => e.ruta.endsWith("/enviar-manual"))).toBe(false);
    });
});
