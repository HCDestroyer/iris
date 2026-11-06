import path from "node:path";
import { expect, test, type Locator, type Page, type TestInfo } from "@playwright/test";
import { ADMIN, conSesion, simularApi, type ApiFalsa } from "./api-falsa";

/*
 * Bodega de Argos por nombre (contrato 0.6.4): en el formulario del técnico se elige de la lista de
 * técnicos de Argos sin ficha («Bodega Norte · Lucía Ramírez»), con búsqueda; el ya vinculado se
 * muestra fijo con «Cambiar»; si Argos no responde, el error con «Reintentar» y el número a mano
 * como respaldo. Y la vista «Técnicos de Argos sin ficha» con «Crear ficha». Escritorio y teléfono,
 * claro y oscuro.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const captura = (page: Page, info: TestInfo, que: string) => page.screenshot({ path: path.join(carpeta, `${que}-${info.project.name}.png`), fullPage: true });
const pantalla = (page: Page, nombre: string | RegExp) => page.getByRole("form", { name: nombre });
const ultima = (api: ApiFalsa, ruta: string, metodo = "GET") => api.peticiones.filter((p) => p.ruta === ruta && p.metodo === metodo).at(-1);

/*
 * El `Autocompletar` (ComboBox de React Aria) cierra su lista si la página se desplaza (en el
 * teléfono, al enfocar el campo): se abre con flecha abajo y se elige con un clic «virtual», que no
 * desplaza (como las ayudas de la rama de HeroUI del panel).
 */
async function abrirLista(campo: Locator): Promise<Locator> {
    await expect(async () => {
        if ((await campo.getAttribute("aria-expanded")) !== "true") await campo.press("ArrowDown");
        expect(await campo.getAttribute("aria-expanded")).toBe("true");
    }).toPass({ intervals: [100, 200, 400] });
    return campo.page().locator(`[id="${(await campo.getAttribute("aria-controls")) ?? ""}"]`);
}

async function elegir(campo: Locator, texto: string): Promise<void> {
    await expect(async () => {
        if ((await campo.inputValue()) === texto) return;
        const lista = await abrirLista(campo);
        await lista.getByRole("option").filter({ hasText: texto }).first().dispatchEvent("click", undefined, { timeout: 2000 });
        await expect.poll(() => campo.inputValue(), { timeout: 1500 }).toBe(texto);
    }).toPass({ intervals: [100, 200, 400] });
}

async function sinDesborde(page: Page) {
    const ancho = page.viewportSize()?.width ?? 0;
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(ancho);
}

/** Argos ≥ 1.6.0: el conector informa la capacidad y la lista responde. */
async function conArgos(page: Page) {
    const api = await simularApi(page, { despacho: true });
    const conector = api.sso.conectores.find((c) => c.id === 1);
    if (conector) conector.capacidades = ["inventario_por_tecnico"];
    api.equipo.argos.inventarioPorTecnico = true;
    return api;
}

test.describe("bodega de Argos por nombre", () => {
    test("alta: elige «Bodega Norte · Lucía Ramírez» buscando y queda vinculada al guardar", async ({ page, context, baseURL }, info) => {
        const api = await conArgos(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/nuevo");
        const d = pantalla(page, "Nuevo técnico");
        await d.getByLabel("Usuario", { exact: true }).selectOption({ label: "Pablo Xol · pablo@iris.test" });
        await d.getByLabel("Cuadrilla", { exact: true }).selectOption({ label: "Cuadrilla Sur" });
        // Ya no hay número a mano: un selector con búsqueda.
        await expect(d.getByLabel("Número de bodega en Argos")).toHaveCount(0);
        const bodega = d.getByRole("combobox", { name: "Bodega en Argos" });
        const opciones = (await abrirLista(bodega)).getByRole("option");
        // Solo los de Argos sin ficha (Jorge ya está vinculado); Pedro no tiene bodega en esta empresa.
        await expect(opciones).toHaveCount(2);
        await expect(opciones.filter({ hasText: "Jorge Méndez" })).toHaveCount(0);
        await expect(opciones.filter({ hasText: "Pedro Sic" })).toHaveAttribute("aria-disabled", "true");
        await bodega.fill("norte");
        const filtradas = (await abrirLista(bodega)).getByRole("option");
        await expect(filtradas).toHaveCount(1);
        await captura(page, info, "bodega-argos-selector");
        await elegir(bodega, "Bodega Norte · Lucía Ramírez");
        await expect(bodega).toHaveValue("Bodega Norte · Lucía Ramírez");
        await sinDesborde(page);
        await d.getByRole("button", { name: "Crear técnico" }).click();
        await expect(d).toHaveCount(0);
        // La bodega no va con la ficha: la fija el servidor al vincular.
        const alta = ultima(api, "/api/v1/tecnicos", "POST")?.cuerpo;
        expect(alta).toMatchObject({ usuarioId: 14, cuadrillaId: 2 });
        expect(alta).not.toHaveProperty("bodegaId");
        const nueva = api.equipo.tecnicos.find((t) => t.usuarioId === 14);
        const vincular = ultima(api, `/api/v1/tecnicos/${nueva?.id ?? 0}/vincular-argos`, "POST");
        expect(vincular?.cuerpo).toEqual({ version: 1, idExterno: "77" });
        expect(vincular?.idempotencia).toBeTruthy();
        expect(nueva?.bodegaId).toBe(30);
        await expect(page.getByRole("alertdialog").filter({ hasText: "Técnico Pablo Xol creado" })).toBeVisible();
        await expect(page.getByTestId("vinculo-argos")).toContainText("Vinculado con Lucía Ramírez");
    });

    test("edición: el vinculado se muestra fijo con «Cambiar»; «Mantener» vuelve sin cambios", async ({ page, context, baseURL }, info) => {
        const api = await conArgos(page);
        const jorge = api.equipo.tecnicos.find((t) => t.id === 22);
        if (jorge) jorge.argos = { idExterno: "78", nombre: "Jorge Méndez", correo: "jorge@argos.test", vinculadoEn: "2026-10-28T15:00:00Z" };
        if (jorge) jorge.bodegaId = 31;
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/22/editar");
        const d = pantalla(page, /Editar a Jorge Méndez/);
        const fija = d.getByTestId("bodega-argos");
        await expect(fija).toContainText("Bodega 31 · Jorge Méndez");
        await expect(fija).toContainText("Vinculado · jorge@argos.test");
        await captura(page, info, "bodega-argos-vinculado");
        await fija.getByRole("button", { name: "Cambiar" }).click();
        const bodega = d.getByRole("combobox", { name: "Bodega en Argos" });
        await elegir(bodega, "Bodega Norte · Lucía Ramírez");
        await expect(d.getByRole("button", { name: "Guardar cambios" })).toBeEnabled();
        await d.getByRole("button", { name: "Mantener Jorge Méndez" }).click();
        await expect(fija).toContainText("Bodega 31 · Jorge Méndez");
        await expect(d.getByRole("button", { name: "Guardar cambios" })).toBeDisabled();
    });

    test("si el vínculo falla, la ficha queda sin bodega y el aviso ofrece «Reintentar vínculo»", async ({ page, context, baseURL }) => {
        const api = await conArgos(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/nuevo");
        const d = pantalla(page, "Nuevo técnico");
        await d.getByLabel("Usuario", { exact: true }).selectOption({ label: "Pablo Xol · pablo@iris.test" });
        await d.getByLabel("Cuadrilla", { exact: true }).selectOption({ label: "Cuadrilla Sur" });
        await elegir(d.getByRole("combobox", { name: "Bodega en Argos" }), "Bodega Norte · Lucía Ramírez");
        // La ficha nueva es la 900 del simulado: su vínculo responde 503 una vez.
        api.responderProblema("/api/v1/tecnicos/900/vincular-argos", { type: "urn:iris:problema:argos_no_disponible", title: "Argos no responde", status: 503 }, 1);
        await d.getByRole("button", { name: "Crear técnico" }).click();
        const aviso = page.getByRole("alertdialog").filter({ hasText: "La ficha se guardó sin bodega" });
        await expect(aviso).toBeVisible();
        await expect(page.getByTestId("vinculo-argos")).toContainText("Sin vincular");
        expect(api.equipo.tecnicos.find((t) => t.id === 900)?.bodegaId).toBeNull();
        await aviso.getByRole("button", { name: "Reintentar vínculo" }).click();
        await expect(page.getByTestId("vinculo-argos")).toContainText("Vinculado con Lucía Ramírez");
        expect(api.equipo.tecnicos.find((t) => t.id === 900)?.bodegaId).toBe(30);
    });

    test("si Argos no responde: error con «Reintentar» y el número a mano solo como respaldo", async ({ page, context, baseURL }, info) => {
        const api = await conArgos(page);
        api.responderProblema("/api/v1/integraciones/argosisp/tecnicos", { type: "urn:iris:problema:argos_no_disponible", title: "Argos no responde", status: 503, detail: "Mantenimiento" }, 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/21/editar");
        const d = pantalla(page, /Editar a Lucía Ramírez/);
        const aviso = d.getByRole("alert").filter({ hasText: "No se pudo leer la lista de bodegas de Argos" });
        await expect(aviso).toContainText("Argos no responde");
        const manual = d.getByLabel("Número de bodega en Argos");
        await expect(manual).toHaveValue("30");
        await captura(page, info, "bodega-argos-sin-argos");
        await sinDesborde(page);
        await aviso.getByRole("button", { name: "Reintentar" }).click();
        await expect(d.getByRole("combobox", { name: "Bodega en Argos" })).toBeVisible();
        await expect(d.getByLabel("Número de bodega en Argos")).toHaveCount(0);
        await expect(d.getByTestId("bodega-argos")).toContainText("Hoy: bodega 30, escrita a mano");
    });

    test("«Técnicos de Argos sin ficha»: lista con migas y «Crear ficha» abre el alta precargada", async ({ page, context, baseURL }, info) => {
        const api = await conArgos(page);
        api.equipo.candidatos.push({ usuarioId: 16, nombre: "Lucía Ramírez (Argos)", correo: "lucia@argos.test" });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos");
        await page.getByRole("link", { name: "Técnicos de Argos sin ficha" }).click();
        await expect(page).toHaveURL(/\/tecnicos\/argos$/);
        await expect(page.getByRole("heading", { name: "Técnicos de Argos sin ficha" })).toBeVisible();
        const migas = page.getByRole("navigation", { name: "Ruta de navegación" }).filter({ visible: true });
        await expect(migas.getByRole("link", { name: "Técnicos", exact: true })).toHaveAttribute("href", "/tecnicos");
        await expect(migas.locator('[aria-current="page"]')).toHaveText("Técnicos de Argos sin ficha");
        const lista = page.getByRole("list", { name: "Técnicos de Argos sin ficha" });
        await expect(lista.locator("[data-argos]")).toHaveCount(2);
        await expect(lista).not.toContainText("Jorge Méndez");
        await expect(page.locator('[data-argos="77"]')).toContainText("Bodega Norte");
        await expect(page.locator('[data-argos="79"]')).toContainText("Sin bodega activa en esta empresa");
        await expect(page.locator('[data-argos="79"]').getByRole("link", { name: /Crear ficha/ })).toHaveCount(0);
        await captura(page, info, "bodega-argos-sin-ficha");
        await sinDesborde(page);

        await page.getByRole("link", { name: "Crear ficha de Lucía Ramírez" }).click();
        // Solo el id en la URL: el correo sale de la fila de Argos que devuelve la API.
        await expect(page).toHaveURL(/\/tecnicos\/nuevo\?argos=77$/);
        const d = pantalla(page, "Nuevo técnico");
        // El correo de Argos coincide con un usuario de Iris sin ficha: queda elegido.
        await expect(d.getByLabel("Usuario", { exact: true })).toHaveValue("16");
        await expect(d.getByRole("combobox", { name: "Bodega en Argos" })).toHaveValue("Bodega Norte · Lucía Ramírez");
    });
});
