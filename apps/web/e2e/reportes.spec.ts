import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, LECTOR, conSesion, simularApi, type ApiFalsa } from "./api-falsa";
import { abrirMenu, avisoError, menu } from "./ayudas";

/*
 * Reportes (Fase 5): menú con `reportes.ver`, filtros en la URL (atajos de período, zona,
 * cuadrilla, técnico, categoría, prioridad y `dias`), indicadores y tablas de cada reporte,
 * «Sin datos» en los porcentajes sin base, «Cargar más» en reincidencias, «Descargar CSV» con los
 * mismos filtros y errores con «Reintentar» sin totales.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const esTelefono = (nombre: string) => nombre.startsWith("telefono");
const OCT = "desde=2026-10-01&hasta=2026-10-28";

/** Hoy en Guatemala (UTC−6, sin horario de verano). */
const hoy = () => new Date(Date.now() - 6 * 3600_000).toISOString().slice(0, 10);

const lecturas = (api: ApiFalsa, reporte: string) => api.peticiones.filter((p) => p.ruta === `/api/v1/reportes/${reporte}` && p.query.get("formato") !== "csv");
const ultima = (api: ApiFalsa, reporte: string) => lecturas(api, reporte).at(-1)?.query;

async function elegirReporte(page: Page, nombre: string) {
    if ((page.viewportSize()?.width ?? 0) < 768) {
        await page.getByRole("combobox", { name: "Reporte" }).selectOption({ label: nombre });
    } else {
        await page.getByRole("radio", { name: nombre }).click();
    }
}

const indicador = (page: Page, etiqueta: string) => page.locator(`[data-indicador="${etiqueta}"]`);

async function sinDesbordar(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

test.describe("menú y permiso", () => {
    test("con reportes.ver (rol lectura) aparece «Reportes» y abre el de SLA del mes", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/");
        await abrirMenu(page);
        await menu(page).getByRole("link", { name: /^Reportes/ }).click();
        await expect(page).toHaveURL(/\/reportes$/);
        await expect(page.getByRole("heading", { name: "Reportes", level: 1 })).toBeVisible();
        await expect(indicador(page, "Respuesta en tiempo")).toContainText("92.7 %");
        const q = ultima(api, "sla");
        expect(q?.get("desde")).toBe(`${hoy().slice(0, 8)}01`);
        expect(q?.get("hasta")).toBe(hoy());
    });

    test("sin reportes.ver no hay entrada y la ruta dice «Sin permiso»", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", LECTOR.correo);
        await page.goto("/");
        await abrirMenu(page);
        await expect(menu(page).getByRole("link", { name: /^Inicio/ })).toBeVisible();
        await expect(menu(page).getByRole("link", { name: /^Reportes/ })).toHaveCount(0);
        await page.goto("/reportes");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        expect(api.peticiones.filter((p) => p.ruta.startsWith("/api/v1/reportes"))).toHaveLength(0);
    });
});

test.describe("filtros en la URL", () => {
    test("atajos de período, zona, técnico y prioridades van a la URL y a la consulta; al recargar siguen", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/reportes");
        await expect(indicador(page, "Tickets resueltos")).toContainText("120");

        await page.getByRole("radio", { name: "Mes anterior" }).click();
        const [a, m] = hoy().split("-").map(Number) as [number, number];
        const fin = new Date(Date.UTC(a, m - 1, 0)).toISOString().slice(0, 10);
        await expect(page).toHaveURL(new RegExp(`desde=${fin.slice(0, 8)}01&hasta=${fin}`));
        await expect.poll(() => ultima(api, "sla")?.get("hasta")).toBe(fin);

        await page.getByRole("radio", { name: "Últimos 90 días" }).click();
        await expect(page).toHaveURL(new RegExp(`hasta=${hoy()}`));
        await expect(page.getByRole("radio", { name: "Últimos 90 días" })).toHaveAttribute("aria-checked", "true");

        await page.getByRole("combobox", { name: "Zona" }).selectOption({ label: "Centro" });
        await expect(page).toHaveURL(/zona=4/);
        await page.getByRole("combobox", { name: "Técnico o responsable" }).selectOption({ label: "Jorge Méndez" });
        await expect(page).toHaveURL(/tecnico=11/);
        await page.getByRole("combobox", { name: "Categoría" }).selectOption({ label: "Lentitud" });
        // La casilla refleja la URL: se marca cuando la URL cambia (por eso click y no check).
        await page.getByRole("checkbox", { name: "Crítica" }).click();
        await expect(page.getByRole("checkbox", { name: "Crítica" })).toBeChecked();
        await page.getByRole("checkbox", { name: "Alta" }).click();
        await expect(page.getByRole("checkbox", { name: "Alta" })).toBeChecked();
        await expect(page).toHaveURL(/categoria=3&prioridad=alta%2Ccritica/);
        await expect.poll(() => ultima(api, "sla")?.get("prioridad")).toBe("alta,critica");
        const q = ultima(api, "sla");
        expect(q?.get("zonaId")).toBe("4");
        expect(q?.get("tecnicoId")).toBe("11");
        expect(q?.get("categoriaId")).toBe("3");

        await page.reload();
        await expect(page.getByRole("combobox", { name: "Zona" })).toHaveValue("4");
        await expect(page.getByRole("checkbox", { name: "Alta" })).toBeChecked();
        await expect(page.getByRole("checkbox", { name: "Media" })).not.toBeChecked();

        // Los reportes de campo no llevan categoría ni prioridad (siguen en la URL para volver).
        await elegirReporte(page, "Órdenes por técnico");
        await expect(page).toHaveURL(/reporte=tecnicos.*prioridad=alta%2Ccritica/);
        await expect(page.getByRole("checkbox", { name: "Alta" })).toHaveCount(0);
        await expect.poll(() => ultima(api, "tecnicos")?.get("zonaId")).toBe("4");
        expect(ultima(api, "tecnicos")?.has("prioridad")).toBe(false);
        expect(ultima(api, "tecnicos")?.has("categoriaId")).toBe(false);

        await page.getByRole("button", { name: "Quitar filtros" }).click();
        await expect(page).not.toHaveURL(/zona=/);
        await expect(page.getByRole("combobox", { name: "Zona" })).toHaveValue("");
    });

    test("fechas a mano: un período inválido no se aplica y uno de más de 366 días en la URL no se pide", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?${OCT}`);
        await expect(indicador(page, "Tickets resueltos")).toContainText("120");
        await page.getByLabel("Desde", { exact: true }).fill("2026-11-15");
        await expect(page.getByText("«Desde» va antes de «hasta». El reporte sigue con el período anterior.")).toBeVisible();
        await expect(page).toHaveURL(/desde=2026-10-01/);
        await page.getByLabel("Desde", { exact: true }).fill("2026-10-10");
        await expect(page).toHaveURL(/desde=2026-10-10&hasta=2026-10-28/);
        await expect.poll(() => ultima(api, "sla")?.get("desde")).toBe("2026-10-10");
        await expect(page.getByText("Período: 10 oct – 28 oct 2026")).toBeVisible();

        const antes = lecturas(api, "sla").length;
        await page.goto("/reportes?desde=2025-01-01&hasta=2026-10-28");
        await expect(page.getByText("Revisa el período")).toBeVisible();
        await expect(page.getByText("El período admite hasta 366 días.")).toBeVisible();
        expect(lecturas(api, "sla").length).toBe(antes);
    });
});

test.describe("cada reporte", () => {
    test("SLA: indicadores, gráfico con leyenda y tablas; «Sin datos» donde no hay base, nunca 0 %", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=sla&${OCT}`);
        await expect(indicador(page, "Respuesta en tiempo")).toContainText("102 de 110 con SLA de respuesta");
        await expect(indicador(page, "Resolución en tiempo")).toContainText("87 %");
        const grafico = page.getByRole("figure", { name: "SLA cumplido por prioridad" });
        await expect(grafico.getByRole("list", { name: "Leyenda" })).toContainText("Respuesta");
        await expect(grafico.getByRole("listitem").filter({ hasText: "Crítica" })).toContainText("90 %");
        await expect(grafico.getByRole("listitem").filter({ hasText: "Baja" })).toContainText("Sin datos");
        const porPrioridad = page.getByRole("region", { name: "Por prioridad" });
        await expect(porPrioridad.getByRole("row").nth(1)).toContainText("Crítica");
        await expect(porPrioridad.getByRole("row").filter({ hasText: "Baja" })).toContainText("Sin datos");
        await expect(porPrioridad.getByRole("row").filter({ hasText: "Baja" })).not.toContainText("0 %");
        await expect(page.getByRole("region", { name: "Por categoría" }).getByRole("row").filter({ hasText: "Sin categoría" })).toContainText("Sin datos");
    });

    test("Resolución: mediana y promedio legibles", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=resolucion&${OCT}`);
        await expect(indicador(page, "Mediana")).toContainText("3 h 5 min");
        await expect(indicador(page, "Promedio")).toContainText("6 h 51 min");
        await expect(page.getByRole("region", { name: "Por prioridad" }).getByRole("row").filter({ hasText: "Baja" })).toContainText("1 d 1 h");
        await expect(page.getByRole("region", { name: "Por categoría" })).toContainText("Lentitud");
    });

    test("Técnicos: órdenes por técnico con su desglose; sin finalizadas, «Sin datos»", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?${OCT}`);
        await elegirReporte(page, "Órdenes por técnico");
        await expect(indicador(page, "Órdenes finalizadas")).toContainText("47");
        await expect(indicador(page, "Éxito")).toContainText("85.1 %");
        const tabla = page.getByRole("region", { name: "Desglose por técnico" });
        await expect(tabla.getByRole("row").nth(1)).toContainText("Lucía Ramírez");
        await expect(tabla.getByRole("row").filter({ hasText: "Rosa Ajú" })).toContainText("Sin datos");
        await expect(page.getByRole("figure", { name: "Órdenes finalizadas por técnico" })).toContainText("20 · 90 %");
    });

    test("Primera visita: selector de días en la URL y la consulta; en observación y sin servicio aparte", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=primera-visita&${OCT}`);
        await expect(indicador(page, "Resueltas en la primera visita")).toContainText("27 de 30 evaluadas, sin retorno en 30 días");
        expect(ultima(api, "primera-visita")?.get("dias")).toBe("30");
        await page.getByRole("combobox", { name: "Sin retorno durante" }).selectOption({ label: "60 días" });
        await expect(page).toHaveURL(/dias=60/);
        await expect(indicador(page, "En observación")).toContainText("Aún no cumplen los 60 días");
        expect(ultima(api, "primera-visita")?.get("dias")).toBe("60");
        await expect(indicador(page, "Sin servicio")).toContainText("2");
        await expect(page.getByRole("region", { name: "Por técnico" }).getByRole("row").filter({ hasText: "Rosa Ajú" })).toContainText("Sin datos");
        await expect(page.getByRole("region", { name: "Por tipo" })).toContainText("Instalación");
    });

    test("Reincidencias: totales del período y lista con «Cargar más»", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=reincidencias&${OCT}&dias=7`);
        await expect(indicador(page, "Reincidencia")).toContainText("15.8 %");
        await expect(page.getByRole("combobox", { name: "Volvió dentro de" })).toHaveValue("7");
        await expect(page.locator("[data-reincidencia]")).toHaveCount(50);
        await expect(page.locator('[data-reincidencia="7000"]')).toContainText("TK-008000");
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(page.locator("[data-reincidencia]")).toHaveCount(60);
        await expect(page.getByRole("button", { name: "Cargar más" })).toHaveCount(0);
        // El ticket siguiente fuera del alcance llega en null: se cuenta, sin enlace ni horas.
        const fuera = page.locator('[data-reincidencia="7003"]');
        await expect(fuera).toContainText("Fuera de tu zona");
        await expect(fuera).toContainText("No disponible");
        await expect(fuera.getByRole("link")).toHaveCount(1);
        const pedidas = lecturas(api, "reincidencias");
        expect(pedidas.at(-1)?.query.get("cursor")).toBe("50");
        expect(pedidas.at(-1)?.query.get("dias")).toBe("7");
        await page.locator('[data-reincidencia="7000"]').getByRole("link", { name: "TK-007000" }).click();
        await expect(page).toHaveURL(/\/tickets\/7000$/);
    });
});

test.describe("CSV y errores", () => {
    test("«Descargar CSV» usa los mismos filtros; un 422 muestra su detalle y «Reintentar» descarga", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=reincidencias&${OCT}&zona=4&prioridad=alta&dias=15`);
        await expect(page.locator("[data-reincidencia]").first()).toBeVisible();
        const descarga = page.waitForEvent("download");
        await page.getByRole("button", { name: "Descargar CSV" }).click();
        expect((await descarga).suggestedFilename()).toBe("reporte-reincidencias_2026-10-01_2026-10-28.csv");
        const csv = api.peticiones.filter((p) => p.query.get("formato") === "csv");
        expect(csv).toHaveLength(1);
        expect(Object.fromEntries(csv[0]?.query ?? [])).toEqual({ desde: "2026-10-01", hasta: "2026-10-28", zonaId: "4", prioridad: "alta", dias: "15", formato: "csv" });

        api.reportes.exportacionExcedida.n = 1;
        await page.getByRole("button", { name: "Descargar CSV" }).click();
        await expect(avisoError(page)).toContainText("No se pudo descargar el CSV");
        await expect(avisoError(page)).toContainText("El CSV admite hasta 10 000 filas");
        const otra = page.waitForEvent("download");
        await avisoError(page).getByRole("button", { name: "Reintentar" }).click();
        expect((await otra).suggestedFilename()).toBe("reporte-reincidencias_2026-10-01_2026-10-28.csv");
        await expect(avisoError(page)).toHaveCount(0);
    });

    test("si la lectura falla: error con «Reintentar» y ningún total a la vista", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.responderProblema("/api/v1/reportes/sla", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?${OCT}`);
        await expect(avisoError(page)).toContainText("No se pudo cargar el reporte «SLA cumplido»");
        await expect(page.locator("[data-indicador]")).toHaveCount(0);
        await expect(page.getByRole("table")).toHaveCount(0);
        await avisoError(page).getByRole("button", { name: "Reintentar" }).click();
        await expect(indicador(page, "Tickets resueltos")).toContainText("120");
        await expect(avisoError(page)).toHaveCount(0);
    });

    test("503 reporte_demasiado_largo: el mensaje, la sugerencia de acotar y «Reintentar», sin totales", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.responderProblema(
            "/api/v1/reportes/primera-visita",
            { type: "urn:iris:problema:reporte_demasiado_largo", title: "El reporte tardó demasiado", status: 503, detail: "La consulta pasó el tiempo máximo (15 s)" },
            1,
        );
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=primera-visita&${OCT}`);
        await expect(avisoError(page)).toContainText("El reporte «Primera visita resuelta» tardó demasiado");
        await expect(avisoError(page)).toContainText("La consulta pasó el tiempo máximo (15 s)");
        await expect(avisoError(page)).toContainText("Acota el período o agrega filtros");
        await expect(page.locator("[data-indicador]")).toHaveCount(0);
        await avisoError(page).getByRole("button", { name: "Reintentar" }).click();
        await expect(indicador(page, "En observación")).toBeVisible();
        await expect(avisoError(page)).toHaveCount(0);
    });

    test("un 422 de la API (rango_excedido) muestra su detalle", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.responderProblema(
            "/api/v1/reportes/tecnicos",
            { type: "urn:iris:problema:rango_excedido", title: "Período demasiado largo", status: 422, detail: "El período admite como mucho 366 días" },
            1,
        );
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto(`/reportes?reporte=tecnicos&${OCT}`);
        await expect(avisoError(page)).toContainText("El período admite como mucho 366 días");
        await expect(page.locator("[data-indicador]")).toHaveCount(0);
    });
});

test("teléfono y escritorio sin desbordes, y capturas de cada reporte", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `reportes-${que}-${info.project.name}.png`);
    await simularApi(page);
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    const casos: [string, string, string][] = [
        ["sla", "01-sla", "Respuesta en tiempo"],
        ["resolucion", "02-resolucion", "Mediana"],
        ["tecnicos", "03-tecnicos", "Órdenes finalizadas"],
        ["primera-visita", "04-primera-visita", "En observación"],
        ["reincidencias", "05-reincidencias", "Reincidencia"],
    ];
    for (const [reporte, archivo, kpi] of casos) {
        await page.goto(`/reportes?reporte=${reporte}&${OCT}`);
        await expect(indicador(page, kpi)).toBeVisible();
        await sinDesbordar(page);
        await page.screenshot({ path: nombre(archivo), fullPage: true });
    }
    if (esTelefono(info.project.name)) {
        await expect(page.getByRole("combobox", { name: "Reporte" })).toBeVisible();
    } else {
        await expect(page.getByRole("radiogroup", { name: "Reporte" })).toBeVisible();
    }
});
