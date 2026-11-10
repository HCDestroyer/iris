import path from "node:path";
import type { Schema } from "@iris/contratos";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import type { OpcionesAsistente } from "./api-asistente";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Entrega 2.1 del panel: el aviso del tope del asistente en la campana (80 % y 100 %, con
 * Sparkles, enlace a los ajustes y el punto que crece solo al 100 %), el saldo de Argos en la
 * sugerencia (al día en verde, vencido en rojo, sin montos) y el diagnóstico del conector de Argos
 * (versión, última revisión, resultado con su detalle y «Revisar ahora»), al que se llega desde
 * técnicos y bodegas. En los 4 proyectos y sin desbordes en 390 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-21");
const captura = (page: Page, info: TestInfo, que: string) => page.screenshot({ path: path.join(carpeta, `${que}-${info.project.name}.png`), fullPage: true });

type AvisoPanel = Schema<"AvisoPanel">;

/** Avisos del tope como los arma el servidor (§16.9): el texto lo pone él. */
const AVISO_80: AvisoPanel = {
    id: 11,
    tipo: "asistente_tope",
    nivel: "alerta",
    titulo: "El asistente de IA de Cable Norte usó el 80 % de su tope del mes",
    enlace: "/administracion/asistente",
    datos: { porcentaje: 80, mes: "2026-10" },
    leido: false,
    leidoEn: null,
    creadoEn: "2026-10-20T15:00:00Z",
    empresaId: null,
};
const AVISO_100: AvisoPanel = {
    ...AVISO_80,
    id: 12,
    nivel: "peligro",
    titulo: "El asistente de IA de Cable Norte se pausó: llegó a su tope del mes",
    datos: { porcentaje: 100, mes: "2026-10" },
    creadoEn: "2026-10-27T15:00:00Z",
};

async function abrir(page: Page, asistente: OpcionesAsistente, ruta: string, avisosPanel: AvisoPanel[] = []) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, { conversaciones: { asistente }, avisosPanel });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

const campana = (page: Page) => page.getByRole("button", { name: /^Avisos/ });
const panelAvisos = (page: Page) => page.getByRole("dialog", { name: "Avisos" });

test.describe("aviso del tope del asistente en la campana", () => {
    test("al 80 %: punto discreto, Sparkles, el texto del servidor y enlace a los ajustes", async ({ page }, info) => {
        await abrir(page, { inicial: null }, "/administracion", [AVISO_80]);
        await expect(campana(page)).toHaveAccessibleName(/1 aviso sin leer/);
        await expect(page.getByTestId("punto-avisos")).toHaveAttribute("data-actuar", "no");
        await campana(page).click();
        const aviso = panelAvisos(page).locator('[data-aviso="asistente_tope"]');
        await expect(aviso).toContainText("El asistente de IA de Cable Norte usó el 80 % de su tope del mes");
        await expect(aviso).toHaveAttribute("data-leido", "false");
        await expect(aviso.locator("svg.lucide-sparkles")).toBeVisible();
        await expect(aviso.getByRole("link")).toHaveAttribute("href", "/administracion/asistente");
        await captura(page, info, "campana-asistente-80");
        await esperarSinDesbordes(page);
    });

    test("al 100 %: el punto crece, dice hasta cuándo; abrirlo lo marca leído y lleva a «Uso del mes» en pausa", async ({ page }, info) => {
        const api = await abrir(page, { inicial: null, estado: { pausado: true }, uso: { porcentaje: 100, pausado: true } }, "/administracion", [AVISO_80, AVISO_100]);
        await expect(campana(page)).toHaveAccessibleName(/2 avisos sin leer/);
        await expect(page.getByTestId("punto-avisos")).toHaveAttribute("data-actuar", "si");
        await campana(page).click();
        const aviso = panelAvisos(page).locator('[data-aviso="asistente_tope"]').first();
        await expect(aviso).toContainText("El asistente de IA de Cable Norte se pausó: llegó a su tope del mes");
        // El mes del aviso es octubre de 2026.
        await expect(aviso).toContainText("Pausado hasta el 1 de noviembre o hasta que subas el tope.");
        await captura(page, info, "campana-asistente-100");
        await esperarSinDesbordes(page);
        await aviso.getByRole("link").click();
        await expect(page).toHaveURL(/\/administracion\/asistente$/);
        await expect.poll(() => api.avisosPanel.find((a) => a.id === 12)?.leido).toBe(true);
        await expect(page.getByTestId("punto-avisos")).toHaveAttribute("data-actuar", "no");
        await expect(page.getByTestId("uso-pausado")).toContainText("Pausado hasta el 1 de noviembre o hasta que subas el tope.");
        await page.getByTestId("uso-pausado").scrollIntoViewIfNeeded();
        await captura(page, info, "ajustes-asistente-pausado");
    });

    test("«Marcar todo como leído» avisa y el punto de los avisos se apaga", async ({ page }) => {
        const api = await abrir(page, { inicial: null }, "/administracion", [AVISO_80]);
        await campana(page).click();
        await panelAvisos(page).getByRole("button", { name: "Marcar todo como leído" }).click();
        await expect(page.getByText("1 aviso marcado como leído")).toBeVisible();
        expect(api.avisosPanel.every((a) => a.leido)).toBe(true);
        await expect(panelAvisos(page).locator('[data-aviso="asistente_tope"]')).toHaveAttribute("data-leido", "true");
        await expect(panelAvisos(page).getByRole("button", { name: "Marcar todo como leído" })).toHaveCount(0);
    });

    test("sin avisos del servidor no hay aviso del asistente", async ({ page }) => {
        const api = await abrir(page, { inicial: null }, "/administracion");
        await expect.poll(() => api.peticiones.some((p) => p.ruta === "/api/v1/yo/avisos")).toBe(true);
        await campana(page).click();
        await expect(panelAvisos(page).locator('[data-aviso="asistente_tope"]')).toHaveCount(0);
    });

    test("con la campana en error no se sigue sondeando; «Reintentar» lo reanuda", async ({ page }) => {
        await page.clock.install({ time: new Date(AHORA_CONV) });
        const api = await simularApi(page, { avisosPanel: [AVISO_80] });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion");
        const lecturas = () => api.peticiones.filter((p) => p.ruta === "/api/v1/yo/avisos").length;
        await expect(page.getByTestId("punto-avisos")).toBeVisible();
        // Con una lectura buena, sondea cada minuto.
        const antes = lecturas();
        await page.clock.fastForward("01:01");
        await expect.poll(lecturas).toBe(antes + 1);

        api.responderProblema("/api/v1/yo/avisos", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await campana(page).click();
        await expect(panelAvisos(page).getByText("No se pudieron leer tus avisos.")).toBeVisible();
        const enError = lecturas();
        await page.clock.fastForward("10:00");
        expect(lecturas()).toBe(enError);

        await panelAvisos(page).getByRole("button", { name: "Reintentar" }).click();
        await expect(panelAvisos(page).locator('[data-aviso="asistente_tope"]')).toBeVisible();
        const reanudado = lecturas();
        await page.clock.fastForward("01:01");
        await expect.poll(lecturas).toBe(reanudado + 1);
    });

    test("sesión vencida (401) al sondear: deja de leer y manda a entrar como «sesión expirada»", async ({ page }) => {
        await page.clock.install({ time: new Date(AHORA_CONV) });
        const api = await simularApi(page, { avisosPanel: [AVISO_80] });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion");
        await expect(page.getByTestId("punto-avisos")).toBeVisible();
        api.responderProblema("/api/v1/yo/avisos", { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 }, 1);
        await page.clock.fastForward("01:01");
        await expect(page).toHaveURL(/\/entrar\?siguiente=%2Fadministracion&motivo=expirada$/);
        const lecturas = api.peticiones.filter((p) => p.ruta === "/api/v1/yo/avisos").length;
        await page.clock.fastForward("05:00");
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/yo/avisos").length).toBe(lecturas);
    });
});

test.describe("saldo de Argos en la sugerencia", () => {
    test("con saldo vencido: rojo con CircleAlert y sin montos", async ({ page }, info) => {
        await abrir(page, { saldo: "vencido" }, "/conversaciones?c=41");
        const fuentes = page.getByRole("region", { name: "Sugerencia del asistente" }).getByRole("list", { name: "Fuentes de la sugerencia" });
        const argos = fuentes.locator('[data-fuente="argos"]');
        await expect(argos).toHaveText("Con saldo vencido (2 facturas) · Argos");
        await expect(argos).toHaveAttribute("data-tono", "error");
        await expect(argos.locator("svg.lucide-circle-alert")).toBeVisible();
        await expect(argos).not.toContainText(/Q\s?\d|\$\s?\d|\d+\.\d{2}/);
        await captura(page, info, "sugerencia-saldo-vencido");
        await esperarSinDesbordes(page);
    });

    test("al día: verde con CircleCheck", async ({ page }) => {
        await abrir(page, {}, "/conversaciones?c=41");
        const argos = page.locator('[data-fuente="argos"]');
        await expect(argos).toHaveText("Servicio activo, al día · Argos");
        await expect(argos).toHaveAttribute("data-tono", "success");
        await expect(argos.locator("svg.lucide-circle-check")).toBeVisible();
    });
});

test.describe("diagnóstico del conector de Argos", () => {
    test("versión, última revisión, resultado con detalle y «Revisar ahora» con Idempotency-Key", async ({ page }, info) => {
        await page.clock.setFixedTime(new Date(AHORA_CONV));
        const api = await simularApi(page);
        const c = api.sso.conectores.find((x) => x.id === 1);
        if (c) {
            Object.assign(c, {
                versionRemota: null,
                versionEstado: "ruta_no_existe",
                versionDetalle: "Argos respondió 404 en /integraciones/iris/estado: la URL base (https://panel.argos.fibra.test) es la del panel y no la de la API.",
                versionConsultadaEn: "2026-10-28T14:05:00Z",
                capacidades: [],
            });
        }
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/integraciones");
        const d = page.getByTestId("diagnostico-argos");
        await expect(d).toHaveCount(1);
        await expect(d.getByTestId("version-remota")).toHaveText("Sin leer");
        // 14:05 UTC = 08:05 en Guatemala.
        await expect(d.getByTestId("version-consultada")).toContainText("08:05");
        await expect(d.getByTestId("estado-version")).toContainText("La ruta de la versión no existe");
        await expect(d.getByTestId("detalle-version")).toContainText("es la del panel y no la de la API");
        await captura(page, info, "conector-argos-diagnostico");
        await esperarSinDesbordes(page);

        await d.getByRole("button", { name: "Revisar ahora" }).click();
        await expect(page.getByText("Revisando la versión de Argos")).toBeVisible();
        await expect(d.getByTestId("version-remota")).toHaveText("1.6.2");
        await expect(d.getByTestId("estado-version")).toContainText("Responde bien");
        expect(api.sso.revisiones).toHaveLength(1);
        expect(api.sso.revisiones[0]?.idempotencia).toBeTruthy();
        await captura(page, info, "conector-argos-revisado");
    });

    test("desde «Técnicos de Argos sin ficha», «Ver el conector de Argos» lleva al diagnóstico", async ({ page }, info) => {
        await simularApi(page, { despacho: true });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/argos");
        await expect(page.getByText("Argos < 1.6.0: inventario por técnico no disponible")).toBeVisible();
        await captura(page, info, "tecnicos-argos-enlace-conector");
        await esperarSinDesbordes(page);
        await page.getByRole("link", { name: "Ver el conector de Argos" }).click();
        await expect(page).toHaveURL(/\/administracion\/integraciones#diagnostico-argos$/);
        await expect(page.getByTestId("diagnostico-argos")).toBeInViewport();
    });

    test("en el alta del técnico, junto a la bodega, también está el enlace", async ({ page }) => {
        await simularApi(page, { despacho: true });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/nuevo");
        const bodega = page.getByTestId("bodega-argos");
        await expect(bodega).toContainText("Argos < 1.6.0: inventario por técnico no disponible");
        await expect(bodega.getByRole("link", { name: "Ver el conector de Argos" })).toHaveAttribute("href", "/administracion/integraciones#diagnostico-argos");
    });
});
