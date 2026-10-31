import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { ADMIN, CSRF, DESPACHO, LECTOR, conSesion, simularApi, type ApiFalsa } from "./api-falsa";
import { irA } from "./ayudas";

/*
 * Plantillas de formulario, varios formularios por orden y pausa (PR #29): editor con versiones,
 * activar y desactivar, formularios en el detalle de la orden, pausar y reanudar, y la orden
 * pausada en el tablero y en la agenda (moverla = reprogramarla).
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const captura = (page: Page, info: TestInfo, que: string) => page.screenshot({ path: path.join(carpeta, `formularios-${que}-${info.project.name}.png`), fullPage: true });
const enTelefono = (info: TestInfo) => info.project.name.startsWith("telefono");
const posts = (api: ApiFalsa, sufijo: string) => api.peticiones.filter((p) => p.metodo === "POST" && p.ruta.endsWith(sufijo));
const plantilla = (page: Page, codigo: string) => page.locator(`[data-plantilla="${codigo}"]`).filter({ visible: true });

async function sinDesborde(page: Page) {
    const ancho = page.viewportSize()?.width ?? 0;
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(ancho);
}

test.describe("plantillas de formulario", () => {
    test("menú → lista con filtro por tipo y activas o todas", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await irA(page, /Administración/);
        await page.getByRole("link", { name: /Plantillas de formulario/ }).click();
        await expect(page).toHaveURL(/\/administracion\/plantillas$/);
        await expect(plantilla(page, "instalacion_basica")).toBeVisible();
        await expect(plantilla(page, "instalacion_basica")).toContainText("Por defecto del tipo");
        await expect(plantilla(page, "encuesta_vieja")).toHaveCount(0);
        await page.getByLabel("Incluir las inactivas").check();
        await expect(plantilla(page, "encuesta_vieja")).toContainText("Inactiva");
        await page.getByLabel("Tipo de orden").selectOption("visita");
        await expect(plantilla(page, "visita_diagnostico")).toBeVisible();
        await expect(plantilla(page, "seguridad")).toBeVisible();
        await expect(plantilla(page, "instalacion_basica")).toHaveCount(0);
        const ultima = api.peticiones.filter((p) => p.ruta === "/api/v1/plantillas").at(-1);
        expect(ultima?.query.get("tipo")).toBe("visita");
        expect(ultima?.query.get("todas")).toBe("true");
        await sinDesborde(page);
        await captura(page, info, "01-lista");
    });

    test("la lista pagina por nombre con «Cargar más»", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        api.plantillas.agregarRelleno(30);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/plantillas");
        const filas = page.locator("[data-plantilla]").filter({ visible: true });
        await expect(filas).toHaveCount(25);
        await expect(filas.first()).toHaveAttribute("data-plantilla", "visita_diagnostico");
        await expect(plantilla(page, "seguridad")).toHaveCount(0);
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(filas).toHaveCount(33);
        await expect(plantilla(page, "seguridad")).toBeVisible();
        await expect(page.getByRole("button", { name: "Cargar más" })).toHaveCount(0);
        const lecturas = api.peticiones.filter((p) => p.ruta === "/api/v1/plantillas");
        expect(lecturas.map((p) => [p.query.get("limit"), p.query.get("cursor")])).toEqual([
            ["25", null],
            ["25", "c:25"],
        ]);
        await sinDesborde(page);
        await captura(page, info, "06-lista-paginada");
    });

    test("sin plantillas.ver no aparece en Administración", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", LECTOR.correo);
        await page.goto("/administracion/plantillas");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });

    test("crear como borrador: valida como la API, el código duplicado va a su campo; editar el borrador no crea versión y luego se activa", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/plantillas");
        await page.getByRole("link", { name: "Nueva plantilla" }).click();
        const form = page.getByRole("form", { name: "Plantilla" });
        await form.getByRole("button", { name: "Crear plantilla" }).click();
        await expect(form.getByText("Escribe el nombre")).toBeVisible();
        await expect(form.getByText("Escribe la pregunta o la indicación")).toBeVisible();

        await form.getByLabel("Nombre").fill("Revisión de poste");
        await form.getByLabel("Código", { exact: true }).fill("seguridad");
        const items = form.getByRole("list", { name: "Ítems de la plantilla" }).getByRole("listitem");
        await items.nth(0).getByLabel("Pregunta o indicación").fill("Potencia óptica");
        await items.nth(0).getByLabel("Pregunta o indicación").blur();
        await expect(items.nth(0).getByLabel("Código del ítem")).toHaveValue("potencia_optica");
        await items.nth(0).getByLabel("Tipo de respuesta").selectOption("medicion");
        await items.nth(0).getByLabel("Unidad (opcional)").fill("dBm");
        await items.nth(0).getByLabel("Obligatorio para cerrar la orden").check();
        await form.getByRole("button", { name: "Agregar ítem" }).click();
        await items.nth(1).getByLabel("Pregunta o indicación").fill("Estado del poste");
        await items.nth(1).getByLabel("Pregunta o indicación").blur();
        await expect(items.nth(1).getByLabel("Código del ítem")).toHaveValue("estado_del_poste");
        await items.nth(1).getByLabel("Código del ítem").fill("estado_poste");
        await items.nth(1).getByLabel("Tipo de respuesta").selectOption("seleccion");
        await items.nth(1).getByLabel("Opciones").fill("Bueno\nBueno");
        await form.getByRole("button", { name: "Crear plantilla" }).click();
        await expect(form.getByText("Hay opciones repetidas")).toBeVisible();
        await items.nth(1).getByLabel("Opciones").fill("Bueno\nRegular\nMalo");
        await expect(form.getByLabel("Guardar como borrador")).toBeChecked();
        await form.getByRole("button", { name: "Crear plantilla" }).click();
        await expect(form.getByText("Ya hay una plantilla con el código seguridad")).toBeVisible();
        await form.getByLabel("Código", { exact: true }).fill("revision_poste");
        await sinDesborde(page);
        await captura(page, info, "02-crear");
        await form.getByRole("button", { name: "Crear plantilla" }).click();

        await expect(page.getByRole("heading", { name: "Revisión de poste" })).toBeVisible();
        await expect(page.getByText("Borrador", { exact: true })).toBeVisible();
        const alta = posts(api, "/api/v1/plantillas");
        expect(alta.at(-1)?.cuerpo).toEqual({
            codigo: "revision_poste",
            nombre: "Revisión de poste",
            requiereFirma: false,
            borrador: true,
            items: [
                { codigo: "potencia_optica", etiqueta: "Potencia óptica", tipo: "medicion", obligatorio: true, unidad: "dBm" },
                { codigo: "estado_poste", etiqueta: "Estado del poste", tipo: "seleccion", obligatorio: false, opciones: ["Bueno", "Regular", "Malo"] },
            ],
        });
        expect(alta.at(-1)?.idempotencia).toBeTruthy();

        await page.getByRole("button", { name: "Editar" }).click();
        await expect(page.getByText("Es un borrador: se reescribe la versión actual")).toBeVisible();
        await page.getByRole("form", { name: "Plantilla" }).getByLabel("Nombre").fill("Revisión de poste y caja");
        await page.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByRole("heading", { name: "Revisión de poste y caja" })).toBeVisible();
        await expect(page.getByRole("list", { name: "Versiones" }).getByRole("listitem")).toHaveCount(1);

        await page.getByRole("button", { name: "Activar" }).click();
        await expect(page.getByText("Activa", { exact: true })).toBeVisible();
    });

    test("editar una plantilla usada crea la versión nueva, reordena ítems y deja ver la anterior", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/plantillas/1");
        await expect(page.getByRole("heading", { name: "Instalación básica" })).toBeVisible();
        await expect(page.getByRole("list", { name: "Versiones" }).getByRole("listitem")).toHaveCount(2);
        await page.getByRole("button", { name: "Editar" }).click();
        await expect(page.getByText("Se crea la versión 3. Las 5 órdenes que ya la usan conservan la versión 2.")).toBeVisible();
        await page.getByRole("button", { name: "Bajar el ítem 1" }).click();
        await captura(page, info, "03-editar");
        await page.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByText("Guardada como versión 3")).toBeVisible();
        await expect(page.getByRole("list", { name: "Versiones" }).getByRole("listitem")).toHaveCount(3);
        const editar = posts(api, "/plantillas/1/editar")[0]?.cuerpo as { version: number; items: { codigo: string }[] };
        expect(editar.version).toBe(2);
        expect(editar.items.map((i) => i.codigo)).toEqual(["antena", "potencia", "navega"]);

        await page.locator('[data-version="1"]').getByRole("button", { name: "Ver" }).click();
        await expect(page).toHaveURL(/\/administracion\/plantillas\/1\?version=1$/);
        await expect(page.getByText("Estás viendo la versión 1; la actual es la 3")).toBeVisible();
        await expect(page.getByRole("button", { name: "Editar" })).toHaveCount(0);
        await expect(page.getByTestId("items-plantilla").locator("[data-item]")).toHaveCount(2);
    });

    test("la por defecto no ofrece desactivar y un 409 ofrece «Recargar»", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/plantillas/3");
        await expect(page.getByText("Es la plantilla por defecto del tipo: no se puede desactivar.")).toBeVisible();
        await expect(page.getByRole("button", { name: "Editar" })).toBeVisible();
        await expect(page.getByRole("button", { name: "Desactivar" })).toHaveCount(0);

        await page.goto("/administracion/plantillas/2");
        await expect(page.getByRole("heading", { name: "Seguridad en altura" })).toBeVisible();
        api.plantillas.cambiarVersion(2);
        // Desactivar es destructivo: pide confirmación.
        await page.getByRole("button", { name: "Desactivar" }).click();
        const confirmar = page.getByRole("alertdialog", { name: "¿Desactivar «Seguridad en altura»?" });
        await confirmar.getByRole("button", { name: "Desactivar" }).click();
        await expect(page.getByText("Alguien más cambió esta plantilla mientras la veías")).toBeVisible();
        await page.getByRole("button", { name: "Recargar" }).click();
        await expect(page.getByText("Alguien más cambió esta plantilla mientras la veías")).toHaveCount(0);
        await page.getByRole("button", { name: "Desactivar" }).click();
        await confirmar.getByRole("button", { name: "Desactivar" }).click();
        await expect(page.getByText("Inactiva", { exact: true })).toBeVisible();
        expect(posts(api, "/plantillas/2/desactivar").map((p) => (p.cuerpo as { version: number }).version)).toEqual([1, 2]);
    });
});

test.describe("formularios de la orden", () => {
    test("muestra origen y versión; quita uno de despacho (nunca el del tipo) y agrega otro", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", DESPACHO.correo);
        await page.goto("/ordenes/4");
        const formularios = page.getByTestId("formularios");
        const tipo = formularios.locator('[data-formulario="cambio_equipo"]');
        const seguridad = formularios.locator('[data-formulario="seguridad"]');
        await expect(tipo).toContainText("Del tipo de orden");
        await expect(tipo).toContainText("versión 1");
        await expect(tipo.getByRole("button", { name: /Quitar/ })).toHaveCount(0);
        await expect(seguridad).toContainText("Agregado por despacho");
        await expect(seguridad.locator('[data-item="escalera"]')).toBeVisible();
        await captura(page, info, "04-orden-formularios");

        await seguridad.getByRole("button", { name: "Quitar Seguridad en altura" }).click();
        await page.getByRole("dialog").getByRole("button", { name: "Quitar" }).click();
        await expect(formularios.locator('[data-formulario="seguridad"]')).toHaveCount(0);
        const quitar = posts(api, "/ordenes/4/formularios/f-4b/quitar")[0];
        expect(quitar?.cuerpo).toEqual({ version: 4 });
        expect(quitar?.idempotencia).toBeTruthy();

        await formularios.getByRole("button", { name: "Agregar formulario" }).click();
        const dialogo = page.getByRole("dialog", { name: "Agregar formulario" });
        const opciones = dialogo.getByLabel("Formulario", { exact: true }).locator("option");
        await expect(opciones).toHaveText(["Elige…", "Seguridad en altura · v1 · común"]);
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/plantillas").at(-1)?.query.get("tipo")).toBe("cambio_equipo");
        // Sin formulario elegido no se puede agregar: botón deshabilitado y el motivo a la vista.
        await expect(dialogo.getByRole("button", { name: "Agregar" })).toBeDisabled();
        await expect(dialogo.getByRole("button", { name: "Agregar" })).toHaveAccessibleDescription("Falta elegir el formulario para guardar.");
        await expect(dialogo.getByLabel("Formulario", { exact: true })).toHaveAccessibleDescription("Elige el formulario que llenará el técnico.");
        await dialogo.getByLabel("Formulario", { exact: true }).selectOption({ label: "Seguridad en altura · v1 · común" });
        await dialogo.getByRole("button", { name: "Agregar" }).click();
        await expect(formularios.locator('[data-formulario="seguridad"]')).toContainText("Agregado por despacho");
        expect(posts(api, "/ordenes/4/formularios")[0]?.cuerpo).toEqual({ version: 5, plantillaId: 2 });
        expect(api.mutaciones.filter((m) => m.ruta.startsWith("/api/v1/ordenes/4/")).every((m) => m.csrf === CSRF)).toBe(true);
        await sinDesborde(page);
    });

    test("«Agregar formulario» trae más plantillas con «Cargar más»", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.plantillas.agregarRelleno(60);
        await conSesion(context, baseURL ?? "", DESPACHO.correo);
        await page.goto("/ordenes/4");
        await page.getByTestId("formularios").getByRole("button", { name: "Agregar formulario" }).click();
        const dialogo = page.getByRole("dialog", { name: "Agregar formulario" });
        const opciones = dialogo.getByLabel("Formulario", { exact: true }).locator("option");
        await expect(opciones).toHaveCount(51);
        await expect(dialogo.getByLabel("Formulario", { exact: true }).locator("option", { hasText: "Relleno 51" })).toHaveCount(0);
        await dialogo.getByRole("button", { name: "Cargar más" }).click();
        await expect(opciones).toHaveCount(61);
        await expect(dialogo.getByRole("button", { name: "Cargar más" })).toHaveCount(0);
        await dialogo.getByLabel("Formulario", { exact: true }).selectOption({ label: "Relleno 55 · v1 · común" });
        await dialogo.getByRole("button", { name: "Agregar" }).click();
        await expect(page.getByTestId("formularios").locator('[data-formulario="relleno_55"]')).toContainText("Agregado por despacho");
        const lecturas = api.peticiones.filter((p) => p.ruta === "/api/v1/plantillas");
        expect(lecturas.map((p) => [p.query.get("tipo"), p.query.get("limit"), p.query.get("cursor")])).toEqual([
            ["cambio_equipo", "50", null],
            ["cambio_equipo", "50", "c:50"],
        ]);
    });

    test("una orden cerrada no ofrece agregar ni quitar", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1");
        const formularios = page.getByTestId("formularios");
        await expect(formularios.locator('[data-formulario="instalacion_basica"]')).toContainText("versión 2");
        await expect(formularios.getByRole("button", { name: "Agregar formulario" })).toHaveCount(0);
        await expect(formularios.getByRole("button", { name: /Quitar/ })).toHaveCount(0);
    });
});

test.describe("pausa", () => {
    test("pausar con motivo y nota; pausada ofrece reanudar, reprogramar y cancelar (no asignar); reanudar vuelve a en sitio", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", DESPACHO.correo);
        await page.goto("/ordenes/5");
        const acciones = page.getByRole("group", { name: "Acciones de la orden" });
        await acciones.getByRole("button", { name: "Pausar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Pausar la orden" });
        await dialogo.getByLabel("Motivo").selectOption("otro");
        await dialogo.getByRole("button", { name: "Pausar" }).click();
        await expect(dialogo.getByText("Con «Otro», explica el motivo")).toBeVisible();
        await dialogo.getByLabel("Nota").fill("El cliente fue a traer la llave del techo");
        await dialogo.getByRole("button", { name: "Pausar" }).click();
        await expect(page.locator("header").getByText("Pausada", { exact: true })).toBeVisible();
        expect(posts(api, "/ordenes/5/pausar")[0]?.cuerpo).toEqual({ version: 5, motivo: "otro", nota: "El cliente fue a traer la llave del techo" });

        const pausas = page.getByTestId("pausas");
        await expect(pausas.getByText(/Pausada: Otro/)).toBeVisible();
        await expect(pausas.getByRole("list", { name: "Historial de pausas" })).toContainText("en curso");
        await expect(acciones.getByRole("button", { name: "Reanudar" })).toBeVisible();
        await expect(acciones.getByRole("button", { name: "Reprogramar" })).toBeVisible();
        await expect(acciones.getByRole("button", { name: "Cancelar orden" })).toBeVisible();
        await expect(acciones.getByRole("button", { name: "Asignar" })).toHaveCount(0);
        await expect(acciones.getByRole("button", { name: "Pausar" })).toHaveCount(0);
        await captura(page, info, "05-pausada");

        await acciones.getByRole("button", { name: "Reanudar" }).click();
        await expect(page.getByRole("dialog", { name: "Reanudar la orden" })).toContainText("«En sitio»");
        await page.getByRole("dialog").getByRole("button", { name: "Reanudar" }).click();
        await expect(page.locator("header").getByText("En sitio", { exact: true })).toBeVisible();
        await expect(pausas.getByRole("list", { name: "Historial de pausas" })).toContainText("Reanudada");
        expect(posts(api, "/ordenes/5/reanudar")[0]?.cuerpo).toEqual({ version: 6 });
    });

    test("la orden pausada aparece en el tablero (columna y filtro de activas)", async ({ page, context, baseURL }, info) => {
        test.skip(enTelefono(info), "Las columnas son de pantalla ancha.");
        const api = await simularApi(page);
        const o = api.ordenes.ordenes.find((x) => x.id === 5);
        if (o) Object.assign(o, { estado: "pausada", pausadaDesde: "en_sitio", pausadaEn: new Date().toISOString() });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes");
        await page.getByLabel("Estado", { exact: true }).selectOption("activas");
        await page.getByRole("radio", { name: "Columnas" }).click();
        await expect(page.locator('[data-columna="pausada"]')).toContainText("OT-000005");
        await page.getByLabel("Estado", { exact: true }).selectOption("pausada");
        await expect(page.locator('[data-columna="pausada"]')).toContainText("OT-000005");
    });

    test("en la agenda: estado «Pausada» y moverla es reprogramarla (con motivo)", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        const o = api.ordenes.ordenes.find((x) => x.id === 3);
        if (o) Object.assign(o, { estado: "pausada", pausadaDesde: "en_camino", pausadaEn: new Date().toISOString(), pausas: [{ id: 1, motivo: "clima", nota: null, desdeEstado: "en_camino", inicio: new Date().toISOString(), fin: null, duracionSeg: null, cierre: null, pausadaPor: 3, cerradaPor: null }] });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/agenda?vista=dia");
        const cita = page.locator('[data-cita="OT-000003"]').filter({ visible: true });
        await expect(cita).toHaveAttribute("data-estado", "pausada");
        if (enTelefono(info)) {
            await expect(page.getByText("Pausada", { exact: true }).filter({ visible: true }).first()).toBeVisible();
            await page.getByRole("button", { name: "Mover a… OT-000003" }).click();
        } else {
            await cita.click();
        }
        const d = page.getByRole("dialog");
        await expect(d.getByRole("button", { name: "Reprogramar" })).toBeVisible();
        await expect(d.getByLabel("Solo cambiar el técnico, a la misma hora")).toHaveCount(0);
        await d.getByLabel("Motivo", { exact: true }).fill("Lluvia fuerte, se mueve a la tarde");
        await d.getByLabel("Hora", { exact: true }).fill("15:30");
        await d.getByRole("button", { name: "Reprogramar" }).click();
        await expect(page.getByTestId("movida")).toContainText("OT-000003 reprogramada");
        expect(posts(api, "/ordenes/3/reprogramar")[0]?.cuerpo).toMatchObject({ version: 3, motivo: "Lluvia fuerte, se mueve a la tarde" });
        expect(api.ordenes.ordenes.find((x) => x.id === 3)?.pausas?.[0]?.cierre).toBe("reprogramada");
    });
});
