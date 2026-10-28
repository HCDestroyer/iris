import { expect, test, type Page } from "@playwright/test";
import { conSesionTecnico, declinarUbicacion, simularApiCampo, type ApiCampo } from "./api-campo";
import { TECNICO, TECNICO_B } from "./datos-campo";

/*
 * Aislamiento entre técnicos que comparten el teléfono (390 px): la cola de uno nunca sale con la
 * sesión de otro, cerrar sesión borra el teléfono (o conserva solo lo pendiente) y sin señal no se
 * abre con datos sin una verificación reciente del mismo usuario.
 */

const BASE_A = `iris-campo-${TECNICO.id}-0`;

// eslint-disable-next-line no-empty-pattern -- Playwright exige desestructurar los fixtures.
test.beforeEach(({}, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
});

async function sinSenal(page: Page, api: ApiCampo) {
    api.sinRed = true;
    await page.context().setOffline(true);
    await expect(page.getByTestId("estado-conexion")).toHaveText("Sin conexión");
}

async function conSenal(page: Page, api: ApiCampo) {
    api.sinRed = false;
    await page.context().setOffline(false);
}

const bases = (page: Page) => page.evaluate(async () => (await indexedDB.databases()).map((d) => d.name ?? "").sort());

/** Cuenta filas de una tabla de la base de un técnico (IndexedDB crudo, sin la app). */
const filas = (page: Page, base: string, tabla: string) =>
    page.evaluate(
        ([b, t]) =>
            new Promise<number>((resolver, rechazar) => {
                const r = indexedDB.open(b);
                r.onerror = () => rechazar(r.error);
                r.onsuccess = () => {
                    const db = r.result;
                    const c = db.transaction(t, "readonly").objectStore(t).count();
                    c.onsuccess = () => {
                        resolver(c.result);
                        db.close();
                    };
                };
            }),
        [base, tabla] as const,
    );

async function abrirComo(page: Page, nombre: string) {
    await page.goto("/campo");
    await expect(page.getByRole("heading", { name: "Mis órdenes de hoy" })).toBeVisible();
    await expect(page.getByText(new RegExp(`^${nombre}`))).toBeVisible();
    // Shell en caché antes de quitar la señal (el service worker toma la página con clientsClaim).
    await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
}

test("la cola de A nunca se envía con la sesión de B, no se pierde y A no ve órdenes de B", async ({ page, context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "", TECNICO);
    const api = await simularApiCampo(page);
    await abrirComo(page, TECNICO.nombre);
    await sinSenal(page, api);
    await page.getByRole("link", { name: /OT-000101/ }).click();
    await page.getByRole("button", { name: "En camino" }).click();
    await declinarUbicacion(page);
    await page.getByLabel("Nueva nota").fill("Nota de Tomás");
    await page.getByRole("button", { name: "Agregar nota" }).click();
    await expect(page.getByTestId("pendientes-por-enviar")).toHaveText("2 pendientes por enviar");

    // Beatriz entra en el mismo navegador (otra pestaña) y vuelve la señal.
    await conSesionTecnico(context, baseURL ?? "", TECNICO_B);
    await conSenal(page, api);
    await expect(page.getByText("Entró otra persona en este navegador")).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText(`Lo pendiente de ${TECNICO.nombre} sigue guardado`)).toBeVisible();
    expect(api.lotes).toHaveLength(0);
    expect(api.peticiones.filter((p) => p.usuario === TECNICO_B.correo).every((p) => p.ruta === "/api/v1/yo")).toBe(true);
    expect(await filas(page, BASE_A, "operaciones")).toBe(2);

    // Continuar abre la app de Beatriz: su orden, nada de Tomás.
    await page.getByRole("button", { name: "Continuar" }).click();
    await abrirComo(page, TECNICO_B.nombre);
    await expect(page.getByRole("link", { name: /OT-000201/ })).toBeVisible();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Todo enviado/ })).toBeVisible();
    expect(await filas(page, BASE_A, "operaciones")).toBe(2);
    expect(api.lotes).toHaveLength(0);

    // Vuelve Tomás: su cola sale con su sesión.
    await conSesionTecnico(context, baseURL ?? "", TECNICO);
    await abrirComo(page, TECNICO.nombre);
    await expect(page.getByRole("button", { name: /Todo enviado/ })).toBeVisible({ timeout: 15_000 });
    expect(api.lotesDe.every((c) => c === TECNICO.correo)).toBe(true);
    expect(api.ordenes.get(101)?.estado).toBe("en_camino");
    expect(api.ordenes.get(101)?.notas?.map((n) => n.texto)).toEqual(["Nota de Tomás"]);
});

test("cerrar sesión sin pendientes borra la base del teléfono y la sesión guardada", async ({ page, context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "", TECNICO);
    const api = await simularApiCampo(page);
    await abrirComo(page, TECNICO.nombre);
    expect(await bases(page)).toContain(BASE_A);
    await page.getByRole("button", { name: "Menú" }).click();
    await page.getByRole("button", { name: "Cerrar sesión" }).click();
    await expect(page).toHaveURL(/\/entrar\?siguiente=%2Fcampo/);
    expect(await bases(page)).not.toContain(BASE_A);
    expect(await page.evaluate(() => [localStorage.getItem("iris-campo-sesion"), localStorage.getItem("iris-campo-empresa-51")])).toEqual([null, null]);
    expect(api.peticiones.find((p) => p.metodo === "DELETE" && p.ruta === "/api/v1/sesion")?.canal).toBe("campo");
});

test("cerrar sesión sin señal con pendientes: avisa, conserva solo la cola y no navega", async ({ page, context, baseURL }) => {
    test.setTimeout(60_000);
    await conSesionTecnico(context, baseURL ?? "", TECNICO);
    const api = await simularApiCampo(page);
    await abrirComo(page, TECNICO.nombre);
    await sinSenal(page, api);
    await page.getByRole("link", { name: /OT-000101/ }).click();
    await page.getByRole("button", { name: "En camino" }).click();
    await declinarUbicacion(page);
    await page.getByRole("button", { name: "Menú" }).click();
    await page.getByRole("button", { name: "Cerrar sesión" }).click();
    const aviso = page.getByRole("dialog").getByText("Tienes 1 cambio sin enviar");
    await expect(aviso).toBeVisible();
    await expect(page.getByRole("button", { name: "Enviar y salir" })).toHaveCount(0);
    await page.getByRole("button", { name: "Salir y conservarlos" }).click();

    await expect(page.getByText("Cerraste sesión en este teléfono")).toBeVisible();
    await expect(page).toHaveURL(/\/campo/);
    expect(await filas(page, BASE_A, "operaciones")).toBe(1);
    expect(await filas(page, BASE_A, "ordenes")).toBe(0);
    expect(await page.evaluate(() => localStorage.getItem("iris-campo-sesion"))).toBeNull();

    // Sin señal no se vuelve a abrir con datos.
    await page.reload();
    await expect(page.getByText("Cerraste sesión en este teléfono")).toBeVisible();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toHaveCount(0);

    // Con señal: se cierra la sesión en la API y se pide entrar.
    await conSenal(page, api);
    await page.getByRole("button", { name: "Reintentar" }).click();
    await expect(page).toHaveURL(/\/entrar\?siguiente=%2Fcampo/);
    const borrados = api.peticiones.filter((p) => p.metodo === "DELETE");
    expect(borrados.length).toBeGreaterThanOrEqual(2); // el intento sin señal y el de ahora
    expect(borrados.every((p) => p.canal === "campo")).toBe(true);
    expect(api.lotes).toHaveLength(0);

    // Al volver a entrar, la cola conservada se envía.
    await abrirComo(page, TECNICO.nombre);
    await expect(page.getByRole("button", { name: /Todo enviado/ })).toBeVisible({ timeout: 15_000 });
    expect(api.ordenes.get(101)?.estado).toBe("en_camino");
});

test("sin señal solo abre con una verificación reciente del mismo usuario", async ({ page, context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "", TECNICO);
    const api = await simularApiCampo(page);
    await abrirComo(page, TECNICO.nombre);
    await sinSenal(page, api);
    // Verificada hace un rato: abre con sus datos.
    await page.reload();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
    // Verificada hace 13 h (más que las 12 permitidas): «Conéctate para entrar».
    await page.evaluate(() => {
        const s = JSON.parse(localStorage.getItem("iris-campo-sesion") ?? "{}") as { verificadoEn: number };
        localStorage.setItem("iris-campo-sesion", JSON.stringify({ ...s, verificadoEn: Date.now() - 13 * 60 * 60 * 1000 }));
    });
    await page.reload();
    await expect(page.getByText("Conéctate para entrar")).toBeVisible();
    await expect(page.getByRole("link", { name: /OT-000101/ })).toHaveCount(0);
    // A los 7 días la sesión guardada se borra.
    await page.evaluate(() => {
        const s = JSON.parse(localStorage.getItem("iris-campo-sesion") ?? "{}") as { verificadoEn: number };
        localStorage.setItem("iris-campo-sesion", JSON.stringify({ ...s, verificadoEn: Date.now() - 8 * 24 * 60 * 60 * 1000 }));
    });
    await page.reload();
    await expect(page.getByText("Conéctate para entrar")).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem("iris-campo-sesion"))).toBeNull();
});
