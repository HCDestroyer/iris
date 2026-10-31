import { expect, test, type Page } from "@playwright/test";
import { conSesionTecnico, declinarUbicacion, problema, simularApiCampo } from "./api-campo";
import { alResumen, empezarTrabajo, firmarEn, irAPaso, pestanaCampo } from "./ayudas-campo";
import { ordenesDelDia } from "./datos-campo";

/*
 * App de campo por pasos (fase 4, teléfono): cada paso con su dirección (enlace directo y «Atrás»),
 * la firma del cliente solo en el paso final junto con el cierre, y la barra de estado discreta
 * (un punto con todo enviado; crece solo cuando hace falta).
 */

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

const firma = (page: Page) => page.getByRole("img", { name: /Firma del cliente/ });
const barra = (page: Page) => page.locator('[data-slot="barra-conexion"]');

/** OT-000102 ya en sitio: lista para llenar. */
function enSitio() {
    return ordenesDelDia().map((o) => (o.id === 102 ? { ...o, estado: "en_sitio" as const, enSitioEn: new Date().toISOString() } : o));
}

test.describe("pasos de la orden", () => {
    test("enlace directo a un paso, «Siguiente», «Anterior» y «Atrás» del navegador", async ({ page }) => {
        await simularApiCampo(page, { ordenes: enSitio() });
        await page.goto("/campo?orden=102&paso=checklist");
        await expect(page.getByRole("heading", { level: 1, name: "Checklist" })).toBeVisible();
        await expect(page.getByText("OT-000102 · Paso 2 de 5")).toBeVisible();
        await expect(page.getByRole("progressbar", { name: "Avance de la orden" })).toHaveAttribute("aria-valuetext", "3 de 4 pasos listos");
        await expect(page.getByRole("link", { name: "2. Checklist: faltan 1" })).toHaveAttribute("aria-current", "step");

        // Se guarda al tocar: el avance sube sin botón de guardar.
        await page.getByRole("radiogroup", { name: "Señal restablecida" }).getByRole("radio", { name: "Sí" }).click();
        await expect(page.getByRole("progressbar", { name: "Avance de la orden" })).toHaveAttribute("aria-valuetext", "4 de 4 pasos listos");

        await page.getByRole("button", { name: "Siguiente: materiales y equipos" }).click();
        await expect(page).toHaveURL(/\/campo\?orden=102&paso=materiales$/);
        await page.getByRole("button", { name: "Siguiente: fotos" }).click();
        await expect(page).toHaveURL(/paso=fotos$/);
        // «Atrás» del navegador vuelve al paso anterior.
        await page.goBack();
        await expect(page.getByRole("heading", { level: 1, name: "Materiales y equipos" })).toBeVisible();
        await page.getByRole("button", { name: "Anterior" }).click();
        await expect(page.getByRole("heading", { level: 1, name: "Checklist" })).toBeVisible();
        // Se puede volver a cualquier paso desde el avance.
        await irAPaso(page, "Llegada");
        await expect(page.getByText("Llegaste al sitio")).toBeVisible();
        await page.getByRole("button", { name: "Resumen", exact: true }).click();
        await expect(page).toHaveURL(/\/campo\?orden=102$/);
        await expect(page.getByRole("button", { name: "Continuar trabajo" })).toBeVisible();
    });

    test("«Empezar trabajo» lleva a la llegada; luego «Continuar trabajo» al primer paso con algo pendiente", async ({ page }) => {
        await simularApiCampo(page);
        await page.goto("/campo?orden=102");
        await empezarTrabajo(page);
        await expect(page).toHaveURL(/paso=llegada$/);
        await page.getByRole("button", { name: "Llegué" }).click();
        await declinarUbicacion(page);
        await expect(page.getByText("Llegaste al sitio")).toBeVisible();
        await alResumen(page);
        await empezarTrabajo(page);
        await expect(page).toHaveURL(/paso=checklist$/);
    });

    test("la firma del cliente solo está en el paso final, que se abre con todo lo obligatorio", async ({ page }) => {
        const api = await simularApiCampo(page, { ordenes: enSitio().map((o) => (o.id === 102 ? { ...o, requiereFirma: true } : o)) });
        await page.goto("/campo?orden=102");
        await expect(page.getByRole("heading", { name: "OT-000102" })).toBeVisible();
        await expect(firma(page)).toHaveCount(0);
        for (const paso of ["Llegada", "Checklist", "Materiales y equipos", "Fotos"] as const) {
            await irAPaso(page, paso);
            await expect(firma(page)).toHaveCount(0);
            await expect(page.getByLabel("Nombre de quien firma")).toHaveCount(0);
        }
        // Bloqueado: dice qué falta y lleva ahí.
        await irAPaso(page, "Firma del cliente y cierre");
        await expect(page.getByText("OT-000102 · Paso final")).toBeVisible();
        await expect(page.getByTestId("falta-para-firmar")).toContainText("Falta «Señal restablecida» del checklist.");
        await expect(firma(page)).toHaveCount(0);
        await page.getByRole("button", { name: "Ir a checklist" }).click();
        await page.getByRole("radiogroup", { name: "Señal restablecida" }).getByRole("radio", { name: "Sí" }).click();

        // Listo: el cliente ve el resumen, firma y en el mismo paso se cierra.
        await irAPaso(page, "Firma del cliente y cierre");
        await expect(page.locator('[data-slot="resumen-trabajo"]')).toContainText("Señal restablecida");
        await page.getByRole("button", { name: "Firmar y cerrar orden" }).click();
        await expect(page.getByText("Pide al cliente que firme en el recuadro")).toBeVisible();
        await page.getByLabel("Nombre de quien firma").fill("Jorge Méndez");
        await firmarEn(page);
        await page.getByRole("radio", { name: "Parcial" }).click();
        await page.getByRole("button", { name: "Firmar y cerrar orden" }).click();
        await expect(page.getByText("Orden cerrada", { exact: true })).toBeVisible();
        await expect.poll(() => api.ordenes.get(102)?.estado, { timeout: 15_000 }).toBe("cerrada");
        const ops = api.lotes.flatMap((l) => l.operaciones ?? []).map((o) => o.tipo);
        expect(ops.slice(-3)).toEqual(["adjunto.registrar", "firma.capturar", "orden.cerrar"]);
        expect(api.lotes.flatMap((l) => l.operaciones ?? []).find((o) => o.tipo === "orden.cerrar")?.datos).toMatchObject({ resultado: "parcial" });
        expect(api.ordenes.get(102)?.firma?.nombre).toBe("Jorge Méndez");
    });
    test("un doble toque en «Cerrar orden» (sin firma) encola un solo cierre", async ({ page }) => {
        const listo = enSitio().map((o) =>
            o.id === 102 ? { ...o, checklist: (o.checklist ?? []).map((i) => ({ ...i, valor: true as never, ocurridoEn: new Date().toISOString() })) } : o,
        );
        const api = await simularApiCampo(page, { ordenes: listo });
        await page.goto("/campo?orden=102&paso=cierre");
        const boton = page.getByRole("button", { name: "Cerrar orden" });
        await expect(boton).toBeEnabled();
        // Dos toques en el mismo instante, antes de que React deshabilite el botón.
        await boton.evaluate((b: HTMLElement) => {
            b.click();
            b.click();
        });
        await expect(page.getByText("Orden cerrada", { exact: true })).toBeVisible();
        await expect.poll(() => api.ordenes.get(102)?.estado, { timeout: 15_000 }).toBe("cerrada");
        await expect(page.getByRole("status", { name: "En línea, todo enviado" })).toBeVisible({ timeout: 15_000 });
        expect(api.lotes.flatMap((l) => l.operaciones ?? []).filter((o) => o.tipo === "orden.cerrar")).toHaveLength(1);
        expect(api.resultados.filter((r) => r.estado === "rechazada")).toHaveLength(0);
    });
});

test.describe("barra de estado discreta", () => {
    test("con todo enviado solo el punto; sin señal sin nada pendiente, el punto cambia; con pendientes, la barra crece", async ({ page, context }) => {
        const api = await simularApiCampo(page);
        await page.goto("/campo");
        await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
        await expect(page.getByRole("status", { name: "En línea, todo enviado" })).toBeVisible();
        await expect(barra(page)).toHaveCount(0);

        api.sinRed = true;
        await context.setOffline(true);
        await expect(page.getByRole("status", { name: "Sin conexión, todo enviado" })).toBeVisible();
        await expect(page.getByTestId("estado-conexion")).toHaveText("Sin conexión");
        await expect(barra(page)).toHaveCount(0);

        await page.getByRole("link", { name: /OT-000102/ }).click();
        await page.getByLabel("Nueva nota").fill("Portón verde");
        await page.getByRole("button", { name: "Agregar nota" }).click();
        await expect(barra(page)).toBeVisible();
        await expect(barra(page).getByTestId("estado-conexion")).toHaveText("Sin conexión");
        await expect(page.getByTestId("pendientes-por-enviar")).toHaveText("1 pendiente por enviar");
        await expect(page.getByRole("link", { name: "Pendientes (1)" })).toBeVisible();

        api.sinRed = false;
        await context.setOffline(false);
        await expect(barra(page)).toHaveCount(0, { timeout: 15_000 });
        await expect(page.getByRole("status", { name: "En línea, todo enviado" })).toBeVisible();
    });

    test("un rechazo abre la barra con acción", async ({ page }) => {
        const api = await simularApiCampo(page);
        api.rechazar((op) => op.tipo === "nota.agregar", problema("validacion", 422, "Datos no válidos"));
        await page.goto("/campo?orden=102");
        await page.getByLabel("Nueva nota").fill("Nota rechazada");
        await page.getByRole("button", { name: "Agregar nota" }).click();
        await expect(page.getByTestId("pendientes-por-enviar")).toHaveText("1 rechazado", { timeout: 15_000 });
        await page.getByRole("button", { name: "1 rechazados. Ver detalle" }).click();
        await expect(page.getByTestId("rechazo")).toContainText("Algún dato no es válido.");
    });

    test("sesión vencida y empresa no válida abren la barra", async ({ page }) => {
        const api = await simularApiCampo(page);
        api.fallarPaquete = problema("no_autenticado", 401, "Sin sesión");
        await page.goto("/campo");
        await expect(barra(page).getByTestId("estado-conexion")).toHaveText("Sesión vencida");
        await expect(page.getByText("Tu sesión terminó")).toBeVisible();

        api.fallarPaquete = problema("empresa_no_permitida", 403, "Empresa no permitida");
        await page.reload();
        await expect(barra(page).getByTestId("estado-conexion")).toHaveText("Empresa no válida");
        await expect(page.getByText("Ya no puedes trabajar en esta empresa")).toBeVisible();
    });

    test("la barra inferior lleva a cada sección y marca la actual", async ({ page }) => {
        await simularApiCampo(page);
        await page.goto("/campo");
        const nav = page.getByRole("navigation", { name: "Secciones de la app" });
        await expect(nav.getByRole("link", { name: "Hoy" })).toHaveAttribute("aria-current", "page");
        await pestanaCampo(page, "Pendientes");
        await expect(page).toHaveURL(/vista=pendientes$/);
        await expect(page.getByRole("heading", { name: "Por enviar" })).toBeVisible();
        await pestanaCampo(page, "Yo");
        await expect(page).toHaveURL(/vista=yo$/);
        await expect(page.getByRole("button", { name: "Cerrar sesión" })).toBeVisible();
        // Zonas táctiles de al menos 44 px.
        for (const enlace of await nav.getByRole("link").all()) {
            expect((await enlace.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
        }
        await page.goBack();
        await expect(nav.getByRole("link", { name: "Pendientes" })).toHaveAttribute("aria-current", "page");
    });
});
